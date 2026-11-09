package sourcestream_test

// 真实 PG/UNS sink + 测试专用 Goja 宿主：覆盖零 Source 同步、多个 Source
// 只读调用、定向输出、UNS 触发、无副作用预览、依赖失效和幂等恢复。
import (
	"backend/internal/testkit/scripttest"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func programContext() context.Context {
	return contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}, "uns.source": {}, "uns.stream.manage": {}, "uns.stream.execute": {}}})
}

func TestProgramService_SourcePreviewPreservesExplicitResourceContext(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := programContext()
	s := domain.New(&protocolFixture{resources: []domain.NativeResource{{ID: "mqtt:factory/+/telemetry", Models: []domain.AccessModel{domain.Publish}}}}, scripttest.New(t), "")
	source, err := s.CreateSource(ctx, domain.SourceDefinition{Name: "MQTT preview", Protocol: domain.MQTT, Endpoint: "mqtt://preview:1883", RuntimeLocation: "local", MQTT: &domain.MQTTOptions{ClientID: "preview", QoS: 1, TopicFilters: []string{"factory/+/telemetry"}}}, 0)
	require.NoError(t, err)
	_, err = s.Discover(ctx, source["id"].(int64), source["revision"].(int64), "")
	require.NoError(t, err)
	def := domain.StreamDefinition{Name: "preview source event", Model: domain.ProgramAccess, Script: domain.ScriptDefinition{Code: fmt.Sprintf(`module.exports=api=>{
 const source=api.sources.use({id:"%d",resources:["mqtt:factory/+/telemetry"]});
 api.stream.onMessage({id:"telemetry",source,resourceRefs:["mqtt:factory/+/telemetry"],qos:1},ctx=>({value:ctx.input.value,ref:ctx.resourceRef}));
};`, source["id"].(int64)), TimeoutMs: 1000}}
	input := map[string]any{"data": map[string]any{"value": 3}, "event": map[string]any{"resourceRef": "mqtt:factory/pump/telemetry"}}
	preview, err := s.Test(ctx, def, input, 0, "input")
	require.NoError(t, err)
	require.JSONEq(t, `{"value":3,"ref":"mqtt:factory/pump/telemetry"}`, string(preview["result"].(streamexec.ProgramResult).Data))
	input["event"] = map[string]any{"resourceRef": "mqtt:other/telemetry"}
	_, err = s.Test(ctx, def, input, 0, "input")
	require.Error(t, err)
}

func TestProgramService_ZeroSourceSynchronous(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := programContext()
	s := domain.New(nil, scripttest.New(t), "")
	defer s.Close()
	require.NoError(t, s.StartProgramExecutions(ctx))
	def := domain.StreamDefinition{Name: "zero source", Model: domain.ProgramAccess, Script: domain.ScriptDefinition{Code: `module.exports=api=>{api.stream.onRequest({id:"double"},ctx=>({value:ctx.input.value*2}));};`, TimeoutMs: 1000}}
	view, err := s.SaveStream(ctx, 0, 0, def)
	require.NoError(t, err)
	require.Zero(t, view["sourceId"])
	id := view["id"].(int64)
	preview, err := s.Test(ctx, def, map[string]any{"data": map[string]any{"value": 7}}, 0, "input")
	require.NoError(t, err)
	require.Equal(t, false, preview["sideEffects"])
	view, err = s.Deploy(ctx, id, 1, 0, 0)
	require.NoError(t, err)
	require.Equal(t, "ready", view["executionState"])
	version := view["activeVersion"].(int64)
	result, err := s.Invoke(ctx, id, version, "pure-request", map[string]any{"value": 7}, 1000)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"value": float64(14)}, result["result"].(map[string]any)["data"])
	require.Equal(t, false, result["usesNATS"])
	replay, err := s.Invoke(ctx, id, version, "pure-request", map[string]any{"value": 7}, 1000)
	require.NoError(t, err)
	require.Equal(t, true, replay["replayed"])
	_, err = s.Invoke(ctx, id, version, "pure-request", map[string]any{"value": 8}, 1000)
	require.ErrorIs(t, err, repo.ErrConflict)
	def.Script.Code = `module.exports=api=>{api.stream.onRequest({id:"double"},()=>({value:99}));};`
	view, err = s.SaveStream(ctx, id, view["revision"].(int64), def)
	require.NoError(t, err)
	require.Equal(t, version, view["activeVersion"])
	result, err = s.Invoke(ctx, id, version, "old-version", map[string]any{"value": 2}, 1000)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"value": float64(4)}, result["result"].(map[string]any)["data"])
	view, err = s.Disable(ctx, id, view["revision"].(int64), false)
	require.NoError(t, err)
	_, err = s.Invoke(ctx, id, version, "disabled", nil, 1000)
	require.ErrorIs(t, err, repo.ErrConflict)
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, version)
	require.NoError(t, err)
	require.Equal(t, version, view["activeVersion"])
}

type programReadFixture struct{ protocolFixture }

func (f *programReadFixture) Invoke(_ context.Context, _ int64, _ domain.SourceDefinition, _ domain.Credential, ref string, input any) (any, error) {
	f.calls++
	if input.(map[string]any)["operation"] != "read" {
		panic("write escaped read-only broker")
	}
	value := 21.0
	if ref == "modbus:pressure" {
		value = 42
	}
	return map[string]any{"value": value}, nil
}

func TestProgramService_MultipleSourceReadsAndUNSEvents(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	ctx := programContext()
	runtime := &programReadFixture{protocolFixture: protocolFixture{resources: []domain.NativeResource{
		{ID: "opcua:temperature", Models: []domain.AccessModel{domain.Publish}, Metadata: map[string]any{"nodeClass": 2}},
		{ID: "modbus:pressure", Models: []domain.AccessModel{domain.Publish}, Metadata: map[string]any{"table": "holding-register", "address": 0, "quantity": 1}},
	}}}
	s := domain.New(runtime, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer s.Close()
	require.NoError(t, s.StartProgramExecutions(ctx))
	s.SetAsyncReady(func() bool { return true })
	ids := []int64{}
	for _, source := range []domain.SourceDefinition{
		{Name: "opc", Protocol: domain.OPCUA, Endpoint: "opc.tcp://fixture:4840", RuntimeLocation: "local", OPCUA: &domain.OPCUAOptions{SecurityMode: "None", SecurityPolicy: "None", NodeIDs: []string{"temperature"}, SamplingIntervalMs: 1000}},
		{Name: "modbus", Protocol: domain.ModbusTCP, Endpoint: "tcp://fixture:502", RuntimeLocation: "local", Modbus: &domain.ModbusOptions{UnitID: 1, PollIntervalMs: 1000, Ranges: []domain.ModbusRange{{ID: "pressure", Table: "holding-register", Address: 0, Quantity: 1}}}},
	} {
		view, err := s.CreateSource(ctx, source, 0)
		require.NoError(t, err)
		id := view["id"].(int64)
		ids = append(ids, id)
		_, err = testkit.SetLegacySourceState(ctx, s, id, 1, "enabled")
		require.NoError(t, err)
		_, err = s.Probe(ctx, id, nil, 500)
		require.NoError(t, err, "deployment requires an actually probed Source, not discovery alone")
		_, err = s.Discover(ctx, id, 2, "")
		require.NoError(t, err)
	}
	topics := []int64{}
	for _, name := range []string{"temperature", "pressure", "combined"} {
		topic, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{Name: name, Type: 2, TopicType: 3, EnableHistory: 2, Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}, nil, nil)
		require.NoError(t, err)
		topics = append(topics, topic.ID)
	}
	def := domain.StreamDefinition{Name: "read two sources", Model: domain.ProgramAccess, Script: domain.ScriptDefinition{TimeoutMs: 1000, Code: fmt.Sprintf(`module.exports=api=>{
 const boiler=api.sources.use({id:"%d",resources:["opcua:temperature"]});
 const plc=api.sources.use({id:"%d",resources:["modbus:pressure"]});
 const temperature=api.uns.use({topicId:"%d"}); const pressure=api.uns.use({topicId:"%d"});
 api.stream.onTimer({id:"tick",intervalMs:5000},async()=>{
  await temperature.write(await boiler.read("opcua:temperature"));
  await pressure.write(await plc.read("modbus:pressure"));
 });
};`, ids[0], ids[1], topics[0], topics[1])}}
	preview, err := s.Test(ctx, def, map[string]any{"data": map[string]any{}, "sourceSamples": map[string]any{fmt.Sprint(ids[0]): map[string]any{"opcua:temperature": map[string]any{"value": 7}}, fmt.Sprint(ids[1]): map[string]any{"modbus:pressure": map[string]any{"value": 9}}}}, 0, "input")
	require.NoError(t, err)
	require.False(t, preview["sideEffects"].(bool))
	require.Len(t, preview["result"].(streamexec.ProgramResult).Outputs, 2)
	require.Zero(t, runtime.calls, "preview cannot touch protocol runtime")
	_, err = s.Test(ctx, def, map[string]any{}, 0, "input")
	require.Error(t, err, "missing sample must not fall back to real reads")
	view, err := s.SaveStream(ctx, 0, 0, def)
	require.NoError(t, err)
	view, err = s.Deploy(ctx, view["id"].(int64), 1, 0, 0)
	require.NoError(t, err)
	ref := domain.ExecutionRef{StreamID: view["id"].(int64), VersionID: view["activeVersion"].(int64), EntryID: "tick"}
	sink, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer sink.Close()
	require.NoError(t, sink.EnsureStreamStorage(ctx))
	captureProgramEvents(t, store, sink)
	raw := domain.RawInput{MessageID: "interval-multi", Trigger: domain.IntervalTrigger, Plans: []domain.ExecutionRef{ref}, Input: domain.SourceInput{ResourceRef: "interval", Data: map[string]any{}, ReceivedAt: time.Now().UnixMilli()}}
	require.NoError(t, s.ProcessRaw(context.Background(), raw, sink), "background authority is the deployed manifest, not a fabricated Subject")
	require.Equal(t, 2, runtime.calls)
	require.NoError(t, s.ProcessRaw(context.Background(), raw, sink))
	require.Equal(t, 2, runtime.calls, "redelivery reuses persisted outputs")
	var events []struct{ Payload string }
	for _, raw := range programEventMessages(t, store) {
		events = append(events, struct{ Payload string }{string(raw)})
	}
	require.Len(t, events, 2)
	for _, event := range events {
		var decoded dataingest.StreamEvent
		require.NoError(t, json.Unmarshal([]byte(event.Payload), &decoded))
		if decoded.TopicID == topics[0] {
			require.EqualValues(t, 21, decoded.Data.(map[string]any)["value"])
		} else {
			require.EqualValues(t, 42, decoded.Data.(map[string]any)["value"])
		}
		require.Equal(t, []int64{ref.StreamID}, decoded.Lineage)
	}
	inputs := domain.StreamDefinition{Name: "UNS only", Model: domain.ProgramAccess, Script: domain.ScriptDefinition{Code: fmt.Sprintf(`module.exports=api=>{
 const input=api.uns.use({topicId:"%d",access:"read"}); const output=api.uns.use({topicId:"%d"});
 api.stream.onEvent({id:"combine",topics:[input]},async ctx=>{await output.write({value:ctx.input.value+1});});
};`, topics[0], topics[2]), TimeoutMs: 1000}}
	unsView, err := s.SaveStream(ctx, 0, 0, inputs)
	require.NoError(t, err)
	unsView, err = s.Deploy(ctx, unsView["id"].(int64), 1, 0, 0)
	require.NoError(t, err)
	event := dataingest.StreamEvent{MessageID: "native-event", TopicID: topics[0], ReceivedAt: time.Now(), Data: map[string]any{"value": 12}}
	require.NoError(t, s.MatchProgramEvent(context.Background(), event, sink))
	require.NoError(t, s.MatchProgramEvent(context.Background(), event, sink))
	var count int64
	require.NoError(t, store.UnsDB().Raw("SELECT count(*) FROM public.stream_receipt").Scan(&count).Error)
	require.EqualValues(t, 3, count)
	event.MessageID, event.Lineage = "feedback", []int64{unsView["id"].(int64)}
	require.NoError(t, s.MatchProgramEvent(context.Background(), event, sink))
	require.NoError(t, store.UnsDB().Raw("SELECT count(*) FROM public.stream_receipt").Scan(&count).Error)
	require.EqualValues(t, 3, count, "visited Stream cannot trigger itself again")
	impact, err := s.Impact(ctx, ids[0])
	require.NoError(t, err)
	require.EqualValues(t, 1, impact["count"])
	_, err = s.DeleteSource(ctx, ids[0], currentSourceRevision(t, ctx, s, ids[0]), impact["impactRevision"].(string))
	require.NoError(t, err)
	blocked, err := s.Stream(ctx, ref.StreamID)
	require.NoError(t, err)
	require.Equal(t, "needs-redeploy", blocked["executionState"])
	raw.MessageID = "inactive"
	require.NoError(t, s.ProcessRaw(context.Background(), raw, sink))
	require.Equal(t, 2, runtime.calls)
}
