package sourcestreamworker_test

// 专用 MQTT/JetStream 服务验证实际长连接：多条消息、停用、重新部署和连接共享。
import (
	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	"backend/internal/domain/sourcestream"
	"backend/internal/infra/sourceprotocol"
	"backend/internal/infra/sourcestreamworker"
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"encoding/base64"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

func TestRealScriptSubscription(t *testing.T) {
	natsURL, brokerURL := os.Getenv("TEST_SOURCE_NATS_URL"), os.Getenv("TEST_SOURCE_MQTT_URL")
	if natsURL == "" || brokerURL == "" {
		t.Skip("requires dedicated MQTT and NATS services")
	}
	_, dsn := testkit.NewTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = contextx.WithSubject(ctx, contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.source": {}, "uns.read": {}, "uns.write": {}}})
	sink, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer sink.Close()
	s := sourcestream.New(sourceprotocol.New(), startExecutor(t, ctx), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	worker, err := sourcestreamworker.Start(ctx, natsURL, s, sink)
	require.NoError(t, err)
	defer worker.Close()
	s.SetAsyncReady(worker.Ready)
	source, err := s.CreateSource(ctx, sourcestream.SourceDefinition{Name: "MQTT stream test", Protocol: sourcestream.MQTT, Endpoint: brokerURL, RuntimeLocation: "local", MQTT: &sourcestream.MQTTConnection{ClientID: uuid.NewString()}}, 0)
	require.NoError(t, err)
	id := source["id"].(int64)
	source, err = testkit.SetLegacySourceState(ctx, s, id, 1, "enabled")
	require.NoError(t, err)
	topic := "test/script/" + uuid.NewString()
	doc := sourcestream.StreamDefinition{Name: "continuous", Model: sourcestream.ScriptMode, Sources: []sourcestream.SourceBinding{{Alias: "plc", SourceID: id, ConfigRevision: source["configRevision"].(int64)}}, Script: sourcestream.ScriptDefinition{Code: fmt.Sprintf(`return sources.get("plc").subscribe(%q, (input, context)=>{if(context.topic!==%q || !input.value) throw new Error("bad message"); return {data:input.value*2,drop:false,logs:[]};}, {qos:1});`, topic, topic), TimeoutMs: 200}}
	stream, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	sid := stream["id"].(int64)
	stream, err = s.Deploy(ctx, sid, stream["revision"].(int64), 0, 0)
	require.NoError(t, err)
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(brokerURL).SetClientID(uuid.NewString()))
	token := client.Connect()
	require.True(t, token.WaitTimeout(5*time.Second))
	require.NoError(t, token.Error())
	defer client.Disconnect(100)
	publish := func() {
		token := client.Publish(topic, 1, false, `{"value":7}`)
		require.True(t, token.WaitTimeout(3*time.Second))
		require.NoError(t, token.Error())
	}
	count := func() int64 {
		_, n, err := repo.NewSourceStreamRepo(ctx).Runs(ctx, repo.StreamRunFilter{StreamID: sid, Status: "succeeded", Page: 1, Size: 20})
		require.NoError(t, err)
		return n
	}
	require.Eventually(t, func() bool { publish(); return count() > 0 }, 12*time.Second, 500*time.Millisecond)
	baseline := count()
	// 等待时间大于脚本 timeout，连接仍持续工作。
	time.Sleep(time.Second)
	publish()
	require.Eventually(t, func() bool { return count() > baseline }, 8*time.Second, 100*time.Millisecond)
	stream, err = s.Disable(ctx, sid, stream["revision"].(int64), false)
	require.NoError(t, err)
	time.Sleep(1500 * time.Millisecond)
	baseline = count()
	publish()
	time.Sleep(1200 * time.Millisecond)
	require.Equal(t, baseline, count())
	stream, err = s.Deploy(ctx, sid, stream["revision"].(int64), 0, 0)
	require.NoError(t, err)
	require.Eventually(t, func() bool { publish(); return count() > baseline }, 12*time.Second, 500*time.Millisecond)
	_, err = s.Disable(ctx, sid, stream["revision"].(int64), false)
	require.NoError(t, err)
}
