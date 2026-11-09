package sourcestreamworker_test

// 专用 JetStream/MQTT + 两个真实 Modbus TCP 测试服务 + 正式 Goja 子进程。
// 验证平台维护订阅、零 Source 的 UNS/定时触发、多 Source SDK 定向输出。
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	"backend/internal/domain/sourcestream"
	"backend/internal/infra/sourceprotocol"
	"backend/internal/infra/sourcestreamworker"
	"backend/internal/repo"
	"backend/internal/testkit"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/simonvetter/modbus"
	"github.com/stretchr/testify/require"
)

type readOnlyRegisters struct{ value uint16 }

func (f *readOnlyRegisters) HandleHoldingRegisters(r *modbus.HoldingRegistersRequest) ([]uint16, error) {
	if r.IsWrite || r.UnitId != 7 || r.Addr != 0 || r.Quantity != 1 {
		return nil, modbus.ErrIllegalDataAddress
	}
	return []uint16{f.value}, nil
}
func (*readOnlyRegisters) HandleInputRegisters(*modbus.InputRegistersRequest) ([]uint16, error) {
	return nil, modbus.ErrIllegalDataAddress
}
func (*readOnlyRegisters) HandleCoils(*modbus.CoilsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalDataAddress
}
func (*readOnlyRegisters) HandleDiscreteInputs(*modbus.DiscreteInputsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalDataAddress
}

func startReadOnlyModbus(t *testing.T, value uint16) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "tcp://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	server, err := modbus.NewServer(&modbus.ServerConfiguration{URL: endpoint, Timeout: time.Second, MaxClients: 10, Logger: log.New(io.Discard, "", 0)}, &readOnlyRegisters{value: value})
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() { require.NoError(t, server.Stop()) })
	return endpoint
}

func TestRealProgramTriggersAndMultipleSourceSDK(t *testing.T) {
	natsURL, brokerURL := os.Getenv("TEST_SOURCE_NATS_URL"), os.Getenv("TEST_SOURCE_MQTT_URL")
	if natsURL == "" || brokerURL == "" {
		t.Skip("requires dedicated NATS and MQTT protocol test services")
	}
	store, dsn := testkit.NewTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = contextx.WithSubject(ctx, contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}, "uns.source": {}, "uns.stream.manage": {}}})
	executor := startExecutor(t, ctx)
	sink, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer sink.Close()
	runtime := sourceprotocol.New()
	runtime.Register(sourcestream.ModbusTCP, sourceprotocol.ModbusDriver{})
	service := sourcestream.New(runtime, executor, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	worker, err := sourcestreamworker.Start(ctx, natsURL, service, sink)
	require.NoError(t, err)
	defer worker.Close()
	observerConn, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer observerConn.Close()
	observer, err := observerConn.SubscribeSync("tier0.events.topic.*")
	require.NoError(t, err)
	require.NoError(t, observerConn.Flush())
	service.SetAsyncReady(worker.Ready)
	require.Eventually(t, worker.Ready, 10*time.Second, 50*time.Millisecond)
	topics := []int64{}
	for _, name := range []string{"mqtt input", "UNS processed", "clock", "first read", "second read"} {
		topic, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{Name: name, Type: 2, TopicType: 3, EnableHistory: 2, Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}, nil, nil)
		require.NoError(t, err)
		topics = append(topics, topic.ID)
	}
	createSource := func(def sourcestream.SourceDefinition) int64 {
		view, err := service.CreateSource(ctx, def, 0)
		require.NoError(t, err)
		id := view["id"].(int64)
		_, err = testkit.SetLegacySourceState(ctx, service, id, 1, "enabled")
		require.NoError(t, err)
		_, err = service.Discover(ctx, id, 2, "")
		require.NoError(t, err)
		return id
	}
	views := []map[string]any{}
	deploy := func(def sourcestream.StreamDefinition) map[string]any {
		view, err := service.SaveStream(ctx, 0, 0, def)
		require.NoError(t, err)
		view, err = service.Deploy(ctx, view["id"].(int64), 1, 0, 0)
		require.NoError(t, err)
		views = append(views, view)
		return view
	}
	prefix := "program/" + uuid.NewString()
	filter, topic := prefix+"/+/telemetry", prefix+"/pump/telemetry"
	sourceEndpoint := strings.Replace(brokerURL, "tcp://", "mqtt://", 1)
	sourceID := createSource(sourcestream.SourceDefinition{Name: "MQTT source", Protocol: sourcestream.MQTT, Endpoint: sourceEndpoint, RuntimeLocation: "local", MQTT: &sourcestream.MQTTOptions{ClientID: uuid.NewString(), QoS: 1, TopicFilters: []string{filter}}})
	deploy(sourcestream.StreamDefinition{Name: "MQTT trigger", Model: sourcestream.Publish, TopicIDs: topics[:1], Program: &sourcestream.ProgramDefinition{Sources: []sourcestream.SourceDependency{{Alias: "mqtt", SourceID: sourceID}}, Trigger: sourcestream.TriggerDefinition{Kind: sourcestream.SourceTrigger, SourceAlias: "mqtt", ResourceRefs: []string{"mqtt:" + filter}, QoS: 1}}, Script: sourcestream.ScriptDefinition{TimeoutMs: 5000, Code: fmt.Sprintf(`if(context.resourceRef!==%q) throw new Error("actual topic lost"); return {data:{value:input.value*2},drop:false};`, "mqtt:"+topic)}})
	deploy(sourcestream.StreamDefinition{Name: "UNS trigger", Model: sourcestream.Publish, TopicIDs: []int64{topics[1]}, Program: &sourcestream.ProgramDefinition{Sources: []sourcestream.SourceDependency{}, Trigger: sourcestream.TriggerDefinition{Kind: sourcestream.UNSTrigger, TopicIDs: topics[:1]}}, Script: sourcestream.ScriptDefinition{TimeoutMs: 5000, Code: `return {data:{value:input.value+3},drop:false};`}})
	deploy(sourcestream.StreamDefinition{Name: "zero-source interval", Model: sourcestream.Publish, TopicIDs: []int64{topics[2]}, Program: &sourcestream.ProgramDefinition{Sources: []sourcestream.SourceDependency{}, Trigger: sourcestream.TriggerDefinition{Kind: sourcestream.IntervalTrigger, IntervalMs: 1000}}, Script: sourcestream.ScriptDefinition{TimeoutMs: 5000, Code: `return {data:{value:9},drop:false};`}})
	readIDs := []int64{}
	for i, value := range []uint16{31, 47} {
		readIDs = append(readIDs, createSource(sourcestream.SourceDefinition{Name: fmt.Sprintf("Modbus %d", i), Protocol: sourcestream.ModbusTCP, Endpoint: startReadOnlyModbus(t, value), RuntimeLocation: "local", Modbus: &sourcestream.ModbusOptions{UnitID: 7, PollIntervalMs: 1000, Ranges: []sourcestream.ModbusRange{{ID: "value", Table: "holding-register", Address: 0, Quantity: 1}}}}))
	}
	deploy(sourcestream.StreamDefinition{Name: "two-source interval", Model: sourcestream.Publish, TopicIDs: topics[3:], Program: &sourcestream.ProgramDefinition{Sources: []sourcestream.SourceDependency{{Alias: "first", SourceID: readIDs[0], ReadResources: []string{"modbus:value"}}, {Alias: "second", SourceID: readIDs[1], ReadResources: []string{"modbus:value"}}}, Trigger: sourcestream.TriggerDefinition{Kind: sourcestream.IntervalTrigger, IntervalMs: 1000}}, Script: sourcestream.ScriptDefinition{TimeoutMs: 5000, Code: fmt.Sprintf(`const a=sources.get("first").read("modbus:value"); const b=sources.get("second").read("modbus:value"); return {outputs:[{topicId:%d,data:{value:a.values[0]}},{topicId:%d,data:{value:b.values[0]}}],drop:false};`, topics[3], topics[4])}})
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(brokerURL).SetClientID("program-publisher-" + uuid.NewString()).SetConnectTimeout(3 * time.Second))
	token := client.Connect()
	require.True(t, token.WaitTimeout(5*time.Second))
	require.NoError(t, token.Error())
	defer client.Disconnect(100)
	token = client.Publish(topic, 1, true, []byte(`{"value":11}`))
	require.True(t, token.WaitTimeout(5*time.Second))
	require.NoError(t, token.Error())
	defer func() { token := client.Publish(topic, 1, true, nil); _ = token.WaitTimeout(5 * time.Second) }()
	// Stop producer Streams before clearing retained fixture data.
	defer func() {
		for _, view := range views {
			_, err := service.Disable(ctx, view["id"].(int64), view["revision"].(int64), false)
			require.NoError(t, err)
		}
	}()
	observed := map[int64]float64{}
	require.Eventually(t, func() bool {
		for i := 0; i < 100; i++ {
			msg, err := observer.NextMsg(5 * time.Millisecond)
			if err == nats.ErrTimeout {
				break
			}
			require.NoError(t, err)
			var event dataingest.StreamEvent
			require.NoError(t, json.Unmarshal(msg.Data, &event))
			if data, ok := event.Data.(map[string]any); ok {
				if value, ok := data["value"].(float64); ok {
					observed[event.TopicID] = value
				}
			}
		}
		for i, expected := range []float64{22, 25, 9, 31, 47} {
			if observed[topics[i]] != expected {
				return false
			}
		}
		return true
	}, 30*time.Second, 100*time.Millisecond, "MQTT -> RAW -> Stream -> EVENTS -> zero-source Stream, plus two-source Modbus SDK must reach UNS")
	manual := deploy(sourcestream.StreamDefinition{Name: "zero-source sync", Model: sourcestream.RequestReply, Program: &sourcestream.ProgramDefinition{Sources: []sourcestream.SourceDependency{}, Trigger: sourcestream.TriggerDefinition{Kind: sourcestream.ManualTrigger}}, Script: sourcestream.ScriptDefinition{Code: `return {data:{value:input.value+1},drop:false};`, TimeoutMs: 5000}})
	response, err := service.Invoke(ctx, manual["id"].(int64), manual["activeVersion"].(int64), "sync-no-nats", map[string]any{"value": 5}, 5000)
	require.NoError(t, err)
	require.Equal(t, false, response["usesNATS"])
	require.Equal(t, map[string]any{"value": float64(6)}, response["data"])
}
