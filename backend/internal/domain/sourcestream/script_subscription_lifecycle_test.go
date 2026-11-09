package sourcestream_test

// 使用隔离数据库覆盖脚本订阅部署、逐消息处理、幂等、停用和依赖变更。
import (
	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"backend/internal/testkit/scripttest"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type subscriptionFixture struct {
	protocolFixture
	ephemeral bool
}

func (f *subscriptionFixture) Watch(ctx context.Context, id int64, source domain.RuntimeSource, credential domain.Credential, emit func(domain.SourceInput) error) error {
	f.ephemeral = source.MQTT.Ephemeral
	return emit(domain.SourceInput{ResourceRef: "mqtt:iot/pump/telemetry", Data: map[string]any{"value": 7}, ReceivedAt: time.Now().UnixMilli()})
}

type unusedScriptSink struct{}

func (unusedScriptSink) CommitStreamEvent(context.Context, repo.UnsNode, dataingest.StreamEvent) (bool, error) {
	panic("script must not write undeclared outputs")
}
func TestScriptSubscriptionLifecycle(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.source": {}, "uns.read": {}, "uns.stream.manage": {}, "uns.stream.execute": {}, "uns.write": {}}})
	runtime := &subscriptionFixture{protocolFixture: protocolFixture{resources: []domain.NativeResource{{ID: "mqtt:iot/+/telemetry", Models: []domain.AccessModel{domain.Publish}}}}}
	s := domain.New(runtime, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer s.Close()
	require.NoError(t, s.StartProgramExecutions(ctx))
	source, err := s.CreateSource(ctx, domain.SourceDefinition{Name: "subscription source", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTConnection{ClientID: "subscription-test"}}, 0)
	require.NoError(t, err)
	id := source["id"].(int64)
	source, err = testkit.SetLegacySourceState(ctx, s, id, source["revision"].(int64), "enabled")
	require.NoError(t, err)
	_, err = s.Probe(ctx, id, nil, 500)
	require.NoError(t, err)
	_, err = s.Discover(ctx, id, source["revision"].(int64), "")
	require.NoError(t, err)
	doc := domain.StreamDefinition{Name: "subscription", Model: domain.ProgramAccess, Script: domain.ScriptDefinition{Code: fmt.Sprintf(`module.exports=api=>{
 const source=api.sources.use({id:"%d",resources:["mqtt:iot/+/telemetry"]});
 api.stream.onMessage({id:"telemetry",source,resourceRefs:["mqtt:iot/+/telemetry"],qos:1},ctx=>({value:ctx.input.value*2,topic:ctx.resourceRef}));
};`, id), TimeoutMs: 1000}}
	preview, err := s.Test(ctx, doc, map[string]any{"data": map[string]any{"value": 7}, "event": map[string]any{"resourceRef": "mqtt:iot/pump/telemetry"}}, 0, "input")
	require.NoError(t, err)
	require.False(t, runtime.ephemeral, "sample preview does not subscribe to a live broker")
	require.False(t, preview["sideEffects"].(bool))
	result := preview["result"].(streamexec.ProgramResult)
	require.JSONEq(t, `{"value":14,"topic":"mqtt:iot/pump/telemetry"}`, string(result.Data))
	stream, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	sid := stream["id"].(int64)
	_, err = s.Deploy(ctx, sid, stream["revision"].(int64), 0, 0)
	require.ErrorIs(t, err, domain.ErrUnavailable)
	s.SetAsyncReady(func() bool { return true })
	stream, err = s.Deploy(ctx, sid, stream["revision"].(int64), 0, 0)
	require.NoError(t, err)
	require.Equal(t, "pending", stream["deploymentState"])
	s.SetWatchState(func(query domain.WatchQuery) string {
		require.Equal(t, domain.WatchQuery{SourceID: id, ConfigRevision: 1, ResourceRefs: []string{"mqtt:iot/+/telemetry"}, QoS: 1}, query)
		return "ready"
	})
	require.NoError(t, s.ActivateReadyProgramV2(ctx))
	stream, err = s.Stream(ctx, sid)
	require.NoError(t, err)
	_, err = s.Invoke(ctx, sid, stream["activeVersion"].(int64), "manual", map[string]any{}, 1000)
	require.ErrorContains(t, err, "requestEntryRequired")
	plans, err := s.Plans(ctx)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, domain.ProgramAccess, plans[0].Definition.Model)
	raw := domain.RawInput{MessageID: "message-1", SourceID: id, Input: domain.SourceInput{ResourceRef: "mqtt:iot/pump/telemetry", Data: map[string]any{"value": 7}, ReceivedAt: time.Now().UnixMilli()}, Plans: []domain.ExecutionRef{plans[0].ExecutionRef}}
	require.NoError(t, s.ProcessRaw(context.Background(), raw, unusedScriptSink{}))
	require.NoError(t, s.ProcessRaw(context.Background(), raw, unusedScriptSink{}))
	runs, total, err := repo.NewSourceStreamRepo(ctx).Runs(ctx, repo.StreamRunFilter{StreamID: sid, Page: 1, Size: 20})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "succeeded", runs[0].Status)
	require.Equal(t, id, runs[0].SourceID)
	var summary map[string]any
	require.NoError(t, json.Unmarshal([]byte(runs[0].Summary), &summary))
	require.Equal(t, true, summary["usesNATS"])
	raw.Input.ResourceRef = "mqtt:other"
	require.Error(t, s.ProcessRaw(ctx, raw, unusedScriptSink{}))
	raw.Input.ResourceRef = "mqtt:iot/pump/telemetry"
	stream, err = s.Disable(ctx, sid, stream["revision"].(int64), false)
	require.NoError(t, err)
	plans, err = s.Plans(ctx)
	require.NoError(t, err)
	require.Empty(t, plans)
	raw.MessageID = "after-disable"
	require.NoError(t, s.ProcessRaw(ctx, raw, unusedScriptSink{}), "retired inputs are terminally acknowledged, not retried forever")
	runs, _, err = repo.NewSourceStreamRepo(ctx).Runs(ctx, repo.StreamRunFilter{StreamID: sid, Page: 1, Size: 20})
	require.NoError(t, err)
	var retired *repo.StreamRun
	for i := range runs {
		if runs[i].MessageID == "after-disable" {
			retired = &runs[i]
		}
	}
	require.NotNil(t, retired, "terminal discard retains an audit record")
	require.Equal(t, "dropped", retired.Status)
	require.Contains(t, retired.Summary, "inactiveReference")
	stream, err = s.Deploy(ctx, sid, stream["revision"].(int64), 0, 0)
	require.NoError(t, err)
	require.NoError(t, s.ActivateReadyProgramV2(ctx))
	plans, err = s.Plans(ctx)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	impact, err := s.Impact(ctx, id)
	require.NoError(t, err)
	source, err = s.Source(ctx, id)
	require.NoError(t, err)
	_, err = s.DeleteSource(ctx, id, source["revision"].(int64), impact["impactRevision"].(string))
	require.NoError(t, err)
	plans, err = s.Plans(ctx)
	require.NoError(t, err)
	require.Empty(t, plans)
}
