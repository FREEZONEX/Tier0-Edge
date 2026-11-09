package sourceprotocol

// 真实 Broker：受管 Server 的声明资源、采集、下发和关联 ID 请求回复。
// 设备认证/ACL 的数据库与回调测试独立覆盖，不用匿名 Broker 证明权限。
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/require"
)

func TestRealMQTTServerResourcesAndBidirectionalTraffic(t *testing.T) {
	clientDef, broker, device := mqttRecoveryFixture(t)
	def := sourcestream.SourceDefinition{Name: "managed test", Protocol: sourcestream.MQTT, Role: sourcestream.ServerRole, Endpoint: "mqtt://managed", RuntimeLocation: "local", CredentialRef: clientDef.MQTT.ClientID,
		MQTT: &sourcestream.MQTTOptions{ClientID: clientDef.MQTT.ClientID, QoS: 1, TopicFilters: []string{"telemetry/+"}, ServerSubscribeTopics: []string{"commands/reset"}, RequestTopic: "rpc/request", ReplyTopic: "rpc/reply", CorrelationField: "requestId"}}
	credential := sourcestream.Credential{Kind: "basic", Username: "device", Secret: "test-only"}
	driver := MQTTServerDriver{Endpoint: strings.Replace(broker, "tcp://", "mqtt://", 1), Credential: sourcestream.Credential{Kind: "basic", Username: "bridge", Secret: "test-only"}}
	runtime := New()
	require.ErrorIs(t, runtime.Probe(context.Background(), 9911, def, credential), sourcestream.ErrUnavailable)
	runtime.RegisterServer(sourcestream.MQTT, driver)
	prefix := sourcestream.MQTTServerPrefix(9911, def.CredentialRef)
	t.Cleanup(func() {
		cleanup := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(fmt.Sprintf("source-9911-%s-%s-acquire", def.MQTT.ClientID, def.CredentialRef)).SetCleanSession(true))
		mqttToken(t, cleanup.Connect())
		mqttToken(t, cleanup.Publish(prefix+"telemetry/temperature", 1, true, []byte{}))
		cleanup.Disconnect(100)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resources, err := runtime.Discover(ctx, 9911, def, credential, "")
	require.NoError(t, err)
	require.Len(t, resources, 3)
	for _, resource := range resources {
		switch resource.ID {
		case "mqtt:telemetry/+":
			require.Equal(t, []sourcestream.AccessModel{sourcestream.Publish}, resource.Models)
			require.Equal(t, prefix+"telemetry/+", resource.Metadata["wireTopic"])
		case "mqtt:commands/reset":
			require.Equal(t, []sourcestream.AccessModel{sourcestream.Subscribe}, resource.Models)
		case "mqtt-request-reply":
			require.Equal(t, []sourcestream.AccessModel{sourcestream.RequestReply}, resource.Models)
		default:
			t.Fatalf("unexpected resource %s", resource.ID)
		}
	}
	mqttToken(t, device.Publish(prefix+"telemetry/temperature", 1, true, `{"value":42}`))
	watchCtx, stop := context.WithCancel(ctx)
	defer stop()
	done, inputs := make(chan error, 1), make(chan Input, 1)
	go func() {
		done <- runtime.Watch(watchCtx, 9911, def, credential, func(input Input) error { inputs <- input; return nil })
	}()
	select {
	case input := <-inputs:
		require.Equal(t, "mqtt:telemetry/temperature", input.ResourceRef)
		require.EqualValues(t, 42, input.Data.(map[string]any)["value"])
	case <-ctx.Done():
		t.Fatal("managed Source did not receive device input")
	}
	mqttStopWatch(t, stop, done)
	commands := make(chan map[string]any, 1)
	mqttToken(t, device.Subscribe(prefix+"commands/reset", 1, func(_ mqtt.Client, message mqtt.Message) {
		var value map[string]any
		if json.Unmarshal(message.Payload(), &value) == nil {
			commands <- value
		}
	}))
	_, err = runtime.Invoke(ctx, 9911, def, credential, "mqtt:commands/reset", map[string]any{"command": "reset"})
	require.NoError(t, err)
	select {
	case value := <-commands:
		require.Equal(t, "reset", value["command"])
	case <-ctx.Done():
		t.Fatal("device did not receive downlink")
	}
	_, err = runtime.Invoke(ctx, 9911, def, credential, "mqtt:telemetry/temperature", nil)
	require.ErrorIs(t, err, ErrProtocol)
	mqttToken(t, device.Subscribe(prefix+"rpc/request", 1, func(c mqtt.Client, message mqtt.Message) {
		var value map[string]any
		if json.Unmarshal(message.Payload(), &value) != nil {
			return
		}
		value["accepted"] = true
		payload, marshalErr := json.Marshal(value)
		if marshalErr == nil {
			c.Publish(prefix+"rpc/reply", 1, false, payload)
		}
	}))
	result, err := runtime.Invoke(ctx, 9911, def, credential, "mqtt-request-reply", map[string]any{"command": "status"})
	require.NoError(t, err)
	require.Equal(t, true, result.(map[string]any)["accepted"])
	require.NotEmpty(t, result.(map[string]any)["requestId"])
	require.Equal(t, "mqtt://managed", def.Endpoint, "resolver must not mutate public definition")
	require.Equal(t, []string{"telemetry/+"}, def.MQTT.TopicFilters)
	rotated := def
	rotated.CredentialRef += "-rotated"
	old, err := driver.connection(9911, def, credential)
	require.NoError(t, err)
	current, err := driver.connection(9911, rotated, credential)
	require.NoError(t, err)
	require.NotEqual(t, old.MQTT.ClientID, current.MQTT.ClientID)
	require.NotEqual(t, old.MQTT.TopicFilters, current.MQTT.TopicFilters)
	require.ErrorIs(t, (MQTTServerDriver{}).Probe(ctx, 9911, def, credential), sourcestream.ErrUnavailable)
}
