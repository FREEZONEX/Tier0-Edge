package sourcestream_test

// 验证受管 MQTT Server 的配置、真实数据库凭证、跨 Source ACL、轮换及启停。
// Broker 协议收发由 sourceprotocol 的真实 MQTT 集成测试覆盖。
import (
	"backend/internal/testkit/scripttest"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	domain "backend/internal/domain/sourcestream"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestMQTTServerConfigurationAndScopedCredentials(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	svc := domain.New(&protocolFixture{}, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	definition := func(name string) domain.SourceDefinition {
		return domain.SourceDefinition{Name: name, Protocol: domain.MQTT, Role: domain.ServerRole, Endpoint: "mqtt://managed", RuntimeLocation: "local", MQTT: &domain.MQTTOptions{ClientID: "bridge", QoS: 1, TopicFilters: []string{"telemetry/+", "alarm/#"}, ServerSubscribeTopics: []string{"commands/reset"}, RequestTopic: "rpc/request", ReplyTopic: "rpc/reply", CorrelationField: "requestId"}}
	}
	for _, mutate := range []func(*domain.SourceDefinition){
		func(d *domain.SourceDefinition) { d.Role = "unknown" },
		func(d *domain.SourceDefinition) { d.Endpoint = "mqtt://other:1883" },
		func(d *domain.SourceDefinition) { d.MQTT.TopicFilters = []string{"$SYS/#"} },
		func(d *domain.SourceDefinition) { d.MQTT.ServerSubscribeTopics = []string{"commands/#"} },
		func(d *domain.SourceDefinition) { d.MQTT.RequestTopic = "/request" },
		func(d *domain.SourceDefinition) { d.Role = domain.ClientRole },
		func(d *domain.SourceDefinition) { d.Protocol = domain.OPCUA },
	} {
		d := definition("invalid")
		mutate(&d)
		require.Error(t, d.Validate())
	}
	create := func(name string) map[string]any {
		view, err := svc.CreateSource(ctx, definition(name), 0)
		require.NoError(t, err)
		view, err = svc.SaveCredential(ctx, view["id"].(int64), view["revision"].(int64), domain.Credential{Kind: "basic", Username: "device:user", Secret: "test-only-password"}, "")
		require.NoError(t, err)
		return view
	}
	a, b := create("Server A"), create("Server B")
	id := a["id"].(int64)
	identity := func(view map[string]any) (string, string, string) {
		ref := view["document"].(map[string]any)["credentialRef"].(string)
		return fmt.Sprintf("source:%d:%s:device:user", view["id"], ref), domain.MQTTServerClientPrefix(view["id"].(int64), ref) + "device-1", domain.MQTTServerPrefix(view["id"].(int64), ref)
	}
	username, clientID, prefix := identity(a)
	allowed, err := svc.AuthenticateMQTTSource(ctx, username, "test-only-password", clientID)
	require.NoError(t, err)
	require.True(t, allowed, "saved Source with valid credentials admits devices without activation")
	a, err = testkit.SetLegacySourceState(ctx, svc, id, a["revision"].(int64), "enabled")
	require.NoError(t, err)
	allowed, err = svc.AuthenticateMQTTSource(ctx, username, "test-only-password", clientID)
	require.NoError(t, err)
	require.True(t, allowed)
	for _, pair := range [][2]string{{username, "other-client"}, {username, "source-device-1-other-device"}, {"source:0001:ref:user", clientID}, {"source:0:ref:user", clientID}, {"source:1:ref", clientID}} {
		allowed, err = svc.AuthenticateMQTTSource(ctx, pair[0], "test-only-password", pair[1])
		require.NoError(t, err)
		require.False(t, allowed)
	}
	allowed, err = svc.AuthenticateMQTTSource(ctx, username, "wrong", clientID)
	require.NoError(t, err)
	require.False(t, allowed)
	_, _, otherPrefix := identity(b)
	for _, tc := range []struct {
		topic, action string
		allow         bool
	}{
		{prefix + "telemetry/temperature", "publish", true}, {prefix + "alarm/line/a", "publish", true},
		{prefix + "rpc/reply", "publish", true}, {prefix + "commands/reset", "subscribe", true},
		{prefix + "rpc/request", "subscribe", true}, {prefix + "commands/reset", "publish", false},
		{prefix + "telemetry/temperature", "subscribe", false}, {prefix + "#", "subscribe", false},
		{otherPrefix + "telemetry/temperature", "publish", false}, {"$share/group/" + prefix + "commands/reset", "subscribe", false},
		{"#", "subscribe", false}, {prefix + "telemetry/+", "publish", false}, {prefix + "rpc/reply", "unknown", false},
	} {
		allowed, err = svc.AuthorizeMQTTSource(ctx, username, clientID, tc.topic, tc.action)
		require.NoError(t, err)
		require.Equal(t, tc.allow, allowed, "%s %s", tc.action, tc.topic)
	}
	encoded, err := json.Marshal(a)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "test-only-password")
	require.NotContains(t, string(encoded), "device:user")
	require.Equal(t, prefix, a["mqttServer"].(map[string]any)["topicPrefix"])
	changed := definition("Server A")
	changed.Role, changed.Endpoint = domain.ClientRole, "mqtt://other:1883"
	changed.MQTT.ServerSubscribeTopics = nil
	changed.CredentialRef = a["document"].(map[string]any)["credentialRef"].(string)
	_, err = svc.UpdateSource(ctx, id, a["revision"].(int64), changed, "")
	require.Error(t, err, "connection role is immutable")
	a, err = svc.SaveCredential(ctx, id, a["revision"].(int64), domain.Credential{Kind: "basic", Username: "device:user", Secret: "rotated-test-only"}, "")
	require.NoError(t, err)
	allowed, err = svc.AuthenticateMQTTSource(ctx, username, "test-only-password", clientID)
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = svc.AuthorizeMQTTSource(ctx, username, clientID, prefix+"rpc/request", "subscribe")
	require.NoError(t, err)
	require.False(t, allowed)
	username, clientID, prefix = identity(a)
	allowed, err = svc.AuthenticateMQTTSource(ctx, username, "rotated-test-only", clientID)
	require.NoError(t, err)
	require.True(t, allowed)
	for _, state := range []string{"disabled", "enabled"} {
		a, err = testkit.SetLegacySourceState(ctx, svc, id, a["revision"].(int64), state)
		require.NoError(t, err)
		allowed, err = svc.AuthorizeMQTTSource(ctx, username, clientID, prefix+"commands/reset", "subscribe")
		require.NoError(t, err)
		require.True(t, allowed, "legacy state markers do not replace credential validation")
	}
}
