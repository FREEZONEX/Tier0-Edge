package sourcestream_test

// 使用独立 PostgreSQL 和真实 UNS sink，覆盖定向数据、逐目标事务、失败恢复及输入隔离。
import (
	"backend/internal/testkit/scripttest"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

type transientTargetSink struct {
	actual    domain.StreamSink
	failTopic int64
	failed    bool
}

// 独立 Stream 使用规范 UNS 引用；样本预览不写入，真实消息幂等落库。
func TestCollectionStreamWritesMetadataTarget(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	ctx := programContext()
	s := domain.New(&protocolFixture{resources: []domain.NativeResource{{ID: "mqtt:data/value", Models: []domain.AccessModel{domain.Publish}}}}, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer s.Close()
	require.NoError(t, s.StartProgramExecutions(ctx))
	s.SetAsyncReady(func() bool { return true })
	s.SetWatchState(func(domain.WatchQuery) string { return "ready" })
	source, err := s.CreateSource(ctx, domain.SourceDefinition{Name: "atlas source", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTConnection{ClientID: "atlas"}}, 0)
	require.NoError(t, err)
	id := source["id"].(int64)
	_, err = testkit.SetLegacySourceState(ctx, s, id, source["revision"].(int64), "enabled")
	require.NoError(t, err)
	_, err = s.Probe(ctx, id, nil, 500)
	require.NoError(t, err)
	_, err = s.Discover(ctx, id, currentSourceRevision(t, ctx, s, id), "")
	require.NoError(t, err)
	metadata := unsmeta.New()
	pair, err := metadata.CreateCollection(ctx, "", unsmeta.Document{Name: "atlas output", RecordSchema: &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}})
	require.NoError(t, err)
	collection := pair["collection"].(repo.UnsMeta)
	topic, err := metadata.Save(ctx, "", "topic", 0, unsmeta.Document{Name: "atlas output view", CollectionID: collection.ID})
	require.NoError(t, err)
	doc := domain.StreamDefinition{Name: "atlas stream", Model: domain.ProgramAccess, Script: domain.ScriptDefinition{Code: fmt.Sprintf(`module.exports=api=>{
 const source=api.sources.use({id:"%d",resources:["mqtt:data/value"]}); const output=api.uns.use({topicId:%q,access:"write"});
 api.stream.onMessage({id:"telemetry",source,resourceRefs:["mqtt:data/value"],qos:1},async ctx=>{await output.write({recordId:"sensor",record:{value:ctx.input.value*2}});});
};`, id, topic.ID), TimeoutMs: 1000}}
	previousSource, err := s.Source(ctx, id)
	require.NoError(t, err)
	previousRevision := currentConfigRevision(t, ctx, s, id)
	stream, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	streamID := stream["id"].(int64)
	updatedSource, err := s.Source(ctx, id)
	require.NoError(t, err)
	require.Equal(t, previousSource["revision"], updatedSource["revision"], "独立 Stream 不修改 Source 文档")
	require.Equal(t, previousRevision, currentConfigRevision(t, ctx, s, id))
	stored, err := repo.NewSourceStreamRepo(ctx).Stream(ctx, streamID)
	require.NoError(t, err)
	var storedDocument domain.StreamDefinition
	require.NoError(t, json.Unmarshal([]byte(stored.Document), &storedDocument))
	require.Equal(t, doc.Script, storedDocument.Script)
	_, err = s.Deploy(ctx, streamID, stream["revision"].(int64)+1, 0, 0)
	require.ErrorIs(t, err, repo.ErrConflict, "发布必须校验 Stream 修订号")
	preview, err := s.Test(ctx, doc, map[string]any{"data": map[string]any{"value": 7}}, 0, "input")
	require.NoError(t, err)
	require.Len(t, preview["result"].(streamexec.ProgramResult).Outputs, 1)
	records, err := metadata.Records(ctx, collection.ID, "", 1, 10)
	require.NoError(t, err)
	require.EqualValues(t, 0, records["total"], "预览不得写入 Record")
	_, err = s.Deploy(ctx, streamID, stream["revision"].(int64), currentConfigRevision(t, ctx, s, id), 0)
	require.NoError(t, err)
	require.NoError(t, s.ActivateReadyProgramV2(ctx))
	plans, err := s.Plans(ctx)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	sink, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer sink.Close()
	raw := domain.RawInput{MessageID: "atlas-input", SourceID: id, Plans: []domain.ExecutionRef{plans[0].ExecutionRef}, Input: domain.SourceInput{ResourceRef: "mqtt:data/value", Data: map[string]any{"value": 7}, ReceivedAt: time.Now().UnixMilli()}}
	require.NoError(t, s.ProcessRaw(ctx, raw, sink))
	require.NoError(t, s.ProcessRaw(ctx, raw, sink))
	records, err = metadata.Records(ctx, collection.ID, "", 1, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, records["total"])
	rows := records["list"].([]repo.UnsRecord)
	require.Equal(t, "sensor", rows[0].ID)
	require.JSONEq(t, `{"value":14}`, string(rows[0].Data))
	require.EqualValues(t, 1, rows[0].Revision, "重复消息不重复更新 Record")
}

type subscriptionWatchFixture struct {
	protocolFixture
	filters, known []string
	qos            int
	calls          int
}

type edgeBufferFixture struct {
	protocolFixture
	ready chan struct{}
}

func (f *edgeBufferFixture) Watch(ctx context.Context, _ int64, _ domain.SourceDefinition, _ domain.Credential, emit func(domain.SourceInput) error) error {
	domain.ReportWatchReady(ctx, true)
	for i := 1; i <= 300; i++ {
		if err := emit(domain.SourceInput{ResourceRef: "mqtt:data/value", Data: map[string]any{"value": i}, ReceivedAt: time.Now().UnixMilli()}); err != nil {
			return err
		}
	}
	close(f.ready)
	<-ctx.Done()
	return ctx.Err()
}

func TestEdgeSubscriptionBoundedReplayAndOwnership(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}}})
	f := &edgeBufferFixture{ready: make(chan struct{})}
	s := domain.New(f, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer s.Close()
	created, err := s.CreateSource(ctx, domain.SourceDefinition{Name: "buffer test", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTConnection{ClientID: "buffer-test", TopicFilters: []string{"data/value"}}}, 0)
	require.NoError(t, err)
	id := created["id"].(int64)
	_, err = testkit.SetLegacySourceState(ctx, s, id, created["revision"].(int64), "disabled")
	require.NoError(t, err)
	owner := contextx.Subject{FleetTask: &contextx.FleetTaskSubject{NodeID: "center", GrantID: "actor"}, ResourceKeys: map[string]struct{}{"uns.source": {}}}
	remote := contextx.WithSubject(ctx, owner)
	req := domain.EdgeSubscriptionRequest{Action: "exchange", ID: fmt.Sprint(id), SubscriptionID: "buffer-subscription", ConfigRevision: 1, Resources: []string{"mqtt:data/value"}, QoS: 1}
	_, err = s.ExchangeEdgeSubscription(ctx, req)
	require.ErrorIs(t, err, domain.ErrForbidden)
	_, err = s.ExchangeEdgeSubscription(remote, req)
	require.ErrorIs(t, err, repo.ErrConflict, "disabled Sources cannot start remote subscriptions")
	_, err = testkit.SetLegacySourceState(ctx, s, id, currentSourceRevision(t, ctx, s, id), "enabled")
	require.NoError(t, err)
	broader := req
	broader.Resources = []string{"mqtt:data/#"}
	_, err = s.ExchangeEdgeSubscription(remote, broader)
	require.Error(t, err)
	_, err = s.ExchangeEdgeSubscription(remote, req)
	require.NoError(t, err)
	select {
	case <-f.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("capture not ready")
	}
	batch, err := s.ExchangeEdgeSubscription(remote, req)
	require.NoError(t, err)
	require.EqualValues(t, 44, batch.Dropped)
	require.Len(t, batch.Events, 64)
	require.EqualValues(t, 45, batch.Events[0].Sequence)
	replay, err := s.ExchangeEdgeSubscription(remote, req)
	require.NoError(t, err)
	require.Equal(t, batch.Events, replay.Events)
	req.Epoch = batch.Epoch
	req.AckSequence = batch.Events[63].Sequence
	next, err := s.ExchangeEdgeSubscription(remote, req)
	require.NoError(t, err)
	require.EqualValues(t, 109, next.Events[0].Sequence)
	wrong := req
	wrong.AckSequence = 10000
	_, err = s.ExchangeEdgeSubscription(remote, wrong)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	wrong = req
	wrong.ConfigRevision = 2
	_, err = s.ExchangeEdgeSubscription(remote, wrong)
	require.ErrorIs(t, err, repo.ErrConflict)
	other := owner
	other.FleetTask = &contextx.FleetTaskSubject{NodeID: "other-center", GrantID: "actor"}
	stop := req
	stop.Action = "cancel"
	_, err = s.ExchangeEdgeSubscription(contextx.WithSubject(ctx, other), stop)
	require.NoError(t, err)
	still, err := s.ExchangeEdgeSubscription(remote, req)
	require.NoError(t, err)
	require.Equal(t, next.Events, still.Events, "another owner cannot cancel this subscription")
	_, err = s.ExchangeEdgeSubscription(remote, stop)
	require.NoError(t, err)
}

func (f *subscriptionWatchFixture) Watch(context.Context, int64, domain.SourceDefinition, domain.Credential, func(domain.SourceInput) error) error {
	return errors.New("scoped MQTT reconciliation must be used")
}

func (f *subscriptionWatchFixture) WatchWithKnownSubscriptions(_ context.Context, _ int64, def domain.SourceDefinition, _ domain.Credential, known []string, _ func(domain.SourceInput) error) error {
	f.filters = append([]string(nil), def.MQTT.TopicFilters...)
	f.known = append([]string(nil), known...)
	f.qos = def.MQTT.QoS
	f.calls++
	return nil
}

func TestMQTTWatchScopeKeepsReconciliationSeparateFromInput(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}}})
	runtime := &subscriptionWatchFixture{protocolFixture: protocolFixture{resources: []domain.NativeResource{
		{ID: "mqtt:data/+/temperature", Models: []domain.AccessModel{domain.Publish}},
		{ID: "mqtt:data/one/temperature", Models: []domain.AccessModel{domain.Publish}},
		{ID: "mqtt:data/one/pressure", Models: []domain.AccessModel{domain.Publish}},
		{ID: "mqtt-request-reply", Models: []domain.AccessModel{domain.RequestReply}},
	}}}
	service := domain.New(runtime, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	view, err := service.CreateSource(ctx, domain.SourceDefinition{Name: "MQTT reconciliation", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTOptions{ClientID: "reconciliation", QoS: 1, TopicFilters: []string{"data/#"}}}, 0)
	require.NoError(t, err)
	id := view["id"].(int64)
	_, err = testkit.SetLegacySourceState(ctx, service, id, 1, "enabled")
	require.NoError(t, err)
	_, err = service.Discover(ctx, id, 2, "")
	require.NoError(t, err)
	require.NoError(t, service.WatchInputs(ctx, id, 1, []string{"mqtt:data/one/temperature"}, 2, func(domain.SourceInput) error { return nil }))
	require.Equal(t, []string{"data/one/temperature"}, runtime.filters, "known old filters cannot broaden active Stream input")
	require.ElementsMatch(t, []string{"data/#", "data/+/temperature", "data/one/temperature", "data/one/pressure"}, runtime.known)
	require.Equal(t, 2, runtime.qos)
	require.ErrorIs(t, service.WatchInputs(ctx, id, 2, []string{"mqtt:data/one/temperature"}, 1, func(domain.SourceInput) error { return nil }), repo.ErrConflict)
	require.Equal(t, 1, runtime.calls, "mismatched Source configuration cannot reach protocol runtime")
	_, err = service.DeleteSource(ctx, id, 2, "")
	require.NoError(t, err)
	require.Error(t, service.WatchInputs(ctx, id, 1, []string{"mqtt:data/one/temperature"}, 1, func(domain.SourceInput) error { return nil }))
	require.Equal(t, 1, runtime.calls, "deleted Source cannot reconnect through reconciliation")
}

func (f *transientTargetSink) CommitStreamEvent(ctx context.Context, node repo.UnsNode, event dataingest.StreamEvent) (bool, error) {
	if node.ID == f.failTopic && !f.failed {
		f.failed = true
		return false, errors.New("test target temporarily unavailable")
	}
	return f.actual.CommitStreamEvent(ctx, node, event)
}

func TestMultiResourceDirectedOutputsAndPartialRetry(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	ctx := programContext()
	fixture := &protocolFixture{resources: []domain.NativeResource{
		{ID: "mqtt:temperature", Models: []domain.AccessModel{domain.Publish}},
		{ID: "mqtt:pressure", Models: []domain.AccessModel{domain.Publish}},
	}}
	s := domain.New(fixture, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer s.Close()
	require.NoError(t, s.StartProgramExecutions(ctx))
	s.SetAsyncReady(func() bool { return true })
	s.SetWatchState(func(domain.WatchQuery) string { return "ready" })
	source, err := s.CreateSource(ctx, domain.SourceDefinition{Name: "multi input", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTConnection{ClientID: "multi"}}, 0)
	require.NoError(t, err)
	sourceID := source["id"].(int64)
	_, err = testkit.SetLegacySourceState(ctx, s, sourceID, 1, "enabled")
	require.NoError(t, err)
	_, err = s.Probe(ctx, sourceID, nil, 1000)
	require.NoError(t, err)
	_, err = s.Discover(ctx, sourceID, 2, "")
	require.NoError(t, err)
	topics := []int64{}
	for _, name := range []string{"mqtt:temperature", "mqtt:pressure"} {
		topic, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{Name: name, Type: 2, TopicType: 3, EnableHistory: 2, Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}, nil, nil)
		require.NoError(t, err)
		topics = append(topics, topic.ID)
	}
	doc := domain.StreamDefinition{Name: "directed", Model: domain.ProgramAccess, Script: domain.ScriptDefinition{TimeoutMs: 1000, Code: fmt.Sprintf(`module.exports=api=>{
 const source=api.sources.use({id:"%d",resources:["mqtt:temperature","mqtt:pressure"]});
 const temperature=api.uns.use({topicId:"%d"}); const pressure=api.uns.use({topicId:"%d"});
 api.stream.onMessage({id:"telemetry",source,resourceRefs:["mqtt:temperature","mqtt:pressure"],qos:1},async ctx=>{
  const input=ctx.input;
  if(input.empty)return;
  if(input.unknown){await api.uns.use({topicId:"1"}).write({value:1});return;}
  if(ctx.resourceRef==="mqtt:pressure"){await pressure.write({value:input.value/10});return;}
  await temperature.write({value:input.value*3}); await pressure.write({value:input.bad?"invalid":input.value});
 });
};`, sourceID, topics[0], topics[1])}}
	stream, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	streamID := stream["id"].(int64)
	stream, err = s.Deploy(ctx, streamID, 1, currentConfigRevision(t, ctx, s, sourceID), 0)
	require.NoError(t, err)
	require.NoError(t, s.ActivateReadyProgramV2(ctx))
	stream, err = s.Stream(ctx, streamID)
	require.NoError(t, err)
	versionID := stream["activeVersion"].(int64)
	sink, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer sink.Close()
	require.NoError(t, sink.EnsureStreamStorage(ctx))
	captureProgramEvents(t, store, sink)
	fault := &transientTargetSink{actual: sink, failTopic: topics[1]}
	ref := domain.ExecutionRef{SourceID: sourceID, StreamID: streamID, VersionID: versionID, SourceRevision: 1, EntryID: "telemetry"}
	raw := domain.RawInput{MessageID: "multi-partial", SourceID: sourceID, Plans: []domain.ExecutionRef{ref}, Input: domain.SourceInput{ResourceRef: "mqtt:temperature", Data: map[string]any{"value": 7}, ReceivedAt: time.Now().UnixMilli()}}
	require.Error(t, s.ProcessRaw(ctx, raw, fault))
	var count int64
	require.NoError(t, store.UnsDB().Raw("SELECT count(*) FROM public.stream_receipt").Scan(&count).Error)
	require.EqualValues(t, 1, count, "successful target commits even if another target fails")
	raw.Input.Data = map[string]any{"value": 999}
	require.ErrorIs(t, s.ProcessRaw(ctx, raw, fault), repo.ErrConflict, "the same message identity cannot change its input")
	raw.Input.Data = map[string]any{"value": 7}
	require.NoError(t, s.ProcessRaw(ctx, raw, fault), "retry uses the persisted transform")
	require.NoError(t, s.ProcessRaw(ctx, raw, fault))
	require.NoError(t, store.UnsDB().Raw("SELECT count(*) FROM public.stream_receipt").Scan(&count).Error)
	require.EqualValues(t, 2, count)
	var payloads []struct {
		TopicID int64
		Value   float64
	}
	for _, raw := range programEventMessages(t, store) {
		var e dataingest.StreamEvent
		require.NoError(t, json.Unmarshal(raw, &e))
		payloads = append(payloads, struct {
			TopicID int64
			Value   float64
		}{e.TopicID, e.Data.(map[string]any)["value"].(float64)})
	}
	require.Equal(t, []struct {
		TopicID int64
		Value   float64
	}{{topics[0], 21}, {topics[1], 7}}, payloads)
	raw.MessageID, raw.Input.ResourceRef, raw.Input.Data = "multi-pressure", "mqtt:pressure", map[string]any{"value": 42}
	require.NoError(t, s.ProcessRaw(ctx, raw, sink))
	var actual float64
	for _, raw := range programEventMessages(t, store) {
		var e dataingest.StreamEvent
		require.NoError(t, json.Unmarshal(raw, &e))
		if e.TopicID == topics[1] {
			actual = e.Data.(map[string]any)["value"].(float64)
		}
	}
	require.Equal(t, 4.2, actual)
	raw.MessageID, raw.Input.ResourceRef, raw.Input.Data = "multi-schema", "mqtt:temperature", map[string]any{"value": 9, "bad": true}
	require.Error(t, s.ProcessRaw(ctx, raw, sink), "each target enforces its own real Topic schema")
	raw.MessageID, raw.Input.Data = "multi-unknown", map[string]any{"unknown": true}
	require.Error(t, s.ProcessRaw(ctx, raw, sink), "unknown destination must fail before committing anything")
	raw.MessageID, raw.Input.Data = "multi-empty", map[string]any{"empty": true}
	require.NoError(t, s.ProcessRaw(ctx, raw, sink))
	runs, _, err := repo.NewSourceStreamRepo(ctx).Runs(ctx, repo.StreamRunFilter{StreamID: streamID})
	require.NoError(t, err)
	statuses := map[string]string{}
	for _, run := range runs {
		statuses[run.MessageID] = run.Status
	}
	require.Equal(t, "succeeded", statuses["multi-partial"])
	require.Equal(t, "partial-failure", statuses["multi-schema"])
	require.Equal(t, "failed", statuses["multi-unknown"])
	require.Equal(t, "succeeded", statuses["multi-empty"], "an entry may complete without writing any outputs")
	raw.Input.ResourceRef = "another-source-node"
	require.Error(t, s.ProcessRaw(ctx, raw, sink))
	preview, err := s.Test(ctx, doc, map[string]any{"event": map[string]any{"resourceRef": "mqtt:pressure"}, "data": map[string]any{"value": 42}}, 0, "input")
	require.NoError(t, err)
	outputs := preview["result"].(streamexec.ProgramResult).Outputs
	require.Len(t, outputs, 1)
	require.Equal(t, fmt.Sprint(topics[1]), outputs[0].TopicID)
	require.JSONEq(t, `{"value":4.2}`, string(outputs[0].Data))
	_, err = s.Test(ctx, doc, map[string]any{"value": 42}, currentConfigRevision(t, ctx, s, sourceID), "input")
	require.Error(t, err, "dry-run must select a real input resource")
	// A retired path in a multi-plan envelope must not block a live path, or
	// swallow a temporary error from that live path. The discard is idempotent.
	doc.Name = "retired mapping"
	retired, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	retired, err = s.Deploy(ctx, retired["id"].(int64), 1, currentConfigRevision(t, ctx, s, sourceID), 0)
	require.NoError(t, err)
	require.NoError(t, s.ActivateReadyProgramV2(ctx))
	retired, err = s.Stream(ctx, retired["id"].(int64))
	require.NoError(t, err)
	retiredRef := domain.ExecutionRef{SourceID: sourceID, StreamID: retired["id"].(int64), VersionID: retired["activeVersion"].(int64), SourceRevision: 1, EntryID: "telemetry"}
	_, err = s.Disable(ctx, retiredRef.StreamID, retired["revision"].(int64), false)
	require.NoError(t, err)
	raw.MessageID, raw.Input.ResourceRef, raw.Input.Data = "mixed-retired-and-live", "mqtt:temperature", map[string]any{"value": 8}
	raw.Plans = []domain.ExecutionRef{retiredRef, ref}
	fault.failed = false
	require.Error(t, s.ProcessRaw(ctx, raw, fault), "temporary failures of a live path must still retry")
	require.NoError(t, s.ProcessRaw(ctx, raw, fault))
	require.NoError(t, s.ProcessRaw(ctx, raw, fault))
	runs, _, err = repo.NewSourceStreamRepo(ctx).Runs(ctx, repo.StreamRunFilter{StreamID: retiredRef.StreamID})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, "dropped", runs[0].Status)
	require.Contains(t, runs[0].Summary, "inactiveReference")
	live, _, err := repo.NewSourceStreamRepo(ctx).Runs(ctx, repo.StreamRunFilter{StreamID: streamID})
	require.NoError(t, err)
	found := false
	for _, run := range live {
		if run.MessageID == raw.MessageID {
			found = true
			require.Equal(t, "succeeded", run.Status)
		}
	}
	require.True(t, found)
}

// Capture the actual driver input after database-backed plan assembly.
type watchPlanFixture struct {
	protocolFixture
	definition domain.RuntimeSource
}

func (f *watchPlanFixture) Watch(_ context.Context, _ int64, def domain.RuntimeSource, _ domain.Credential, _ func(domain.SourceInput) error) error {
	f.definition = def
	return nil
}

func TestMQTTWatchPlansPreservesPerFilterQoS(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	fixture := &watchPlanFixture{}
	s := domain.New(fixture, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	source, err := s.CreateSource(ctx, domain.SourceDefinition{Name: "shared broker", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTConnection{ClientID: "shared"}}, 0)
	require.NoError(t, err)
	id := source["id"].(int64)
	source, err = testkit.SetLegacySourceState(ctx, s, id, source["revision"].(int64), "enabled")
	require.NoError(t, err)
	plan := func(qos int, refs ...string) domain.ExecutionPlan {
		return domain.ExecutionPlan{Definition: domain.StreamDefinition{Model: domain.Publish, NativeResourceRefs: refs, Collection: &domain.CollectionDefinition{MQTT: &domain.MQTTCollection{QoS: qos}}}}
	}
	plans := []domain.ExecutionPlan{plan(0, "mqtt:low/#", "mqtt:shared/+"), plan(2, "mqtt:high/#", "mqtt:shared/+"), plan(1, "mqtt:shared/+")}
	for _, order := range [][]domain.ExecutionPlan{plans, {plans[2], plans[1], plans[0]}} {
		require.NoError(t, s.WatchMappedInputs(ctx, id, source["configRevision"].(int64), order, func(domain.SourceInput) error { return nil }))
		require.Equal(t, map[string]byte{"low/#": 0, "high/#": 2, "shared/+": 2}, fixture.definition.MQTT.Subscriptions)
		require.Equal(t, "shared", fixture.definition.MQTT.ClientID)
	}
	// Removing a plan must also remove its filters and allow shared QoS to decrease.
	require.NoError(t, s.WatchMappedInputs(ctx, id, source["configRevision"].(int64), plans[:1], func(domain.SourceInput) error { return nil }))
	require.Equal(t, map[string]byte{"low/#": 0, "shared/+": 0}, fixture.definition.MQTT.Subscriptions)
}

func TestProgramWatchAppliesBindingAcquisition(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	fixture := &watchPlanFixture{}
	service := domain.New(fixture, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	for _, tc := range []struct {
		source      domain.SourceDefinition
		resource    string
		acquisition string
	}{
		{domain.SourceDefinition{Name: "bound modbus", Protocol: domain.ModbusTCP, Endpoint: "tcp://plc:502", RuntimeLocation: "local", Modbus: &domain.ModbusOptions{UnitID: 7, PollIntervalMs: 1000, Ranges: []domain.ModbusRange{{ID: "temperature", Table: "holding-register", Address: 0, Quantity: 2}}}}, "modbus:temperature", `{"pollIntervalMs":2500}`},
		{domain.SourceDefinition{Name: "bound opcua", Protocol: domain.OPCUA, Endpoint: "opc.tcp://plc:4840", RuntimeLocation: "local", OPCUA: &domain.OPCUAOptions{SecurityMode: "None", SecurityPolicy: "None", SamplingIntervalMs: 1000, NodeIDs: []string{"ns=2;s=temp"}}}, "opcua:ns=2;s=temp", `{"samplingIntervalMs":2500}`},
		{domain.SourceDefinition{Name: "bound mqtt", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTOptions{ClientID: "bound", QoS: 0, TopicFilters: []string{"plant/temp"}}}, "mqtt:plant/temp", `{"qos":2}`},
	} {
		source, err := service.CreateSource(ctx, tc.source, 0)
		require.NoError(t, err)
		id := source["id"].(int64)
		var acquisition map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(tc.acquisition), &acquisition))
		binding := domain.ProgramSourceBinding{Alias: "sensor", SourceID: fmt.Sprint(id), ConfigRevision: source["configRevision"].(int64), Resources: []string{tc.resource}, Acquisition: acquisition}
		plan := domain.ExecutionPlan{ExecutionRef: domain.ExecutionRef{SourceID: id, SourceRevision: binding.ConfigRevision}, Definition: domain.StreamDefinition{ProgramSources: []domain.ProgramSourceBinding{binding}}, Entry: &streamexec.ProgramEntry{ID: "samples", Kind: "source-event", SourceID: binding.SourceID, ResourceRefs: binding.Resources}}
		require.NoError(t, service.WatchMappedInputs(ctx, id, binding.ConfigRevision, []domain.ExecutionPlan{plan}, func(domain.SourceInput) error { return nil }))
		switch tc.source.Protocol {
		case domain.ModbusTCP:
			require.Equal(t, 2500, fixture.definition.Modbus.PollIntervalMs)
			require.Equal(t, 7, fixture.definition.Modbus.UnitID)
			require.Equal(t, tc.source.Modbus.Ranges, fixture.definition.Modbus.Ranges)
		case domain.OPCUA:
			require.Equal(t, 2500, fixture.definition.OPCUA.SamplingIntervalMs)
		case domain.MQTT:
			require.Equal(t, map[string]byte{"plant/temp": 2}, fixture.definition.MQTT.Subscriptions)
		}
		stored, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
		require.NoError(t, err)
		var unchanged domain.SourceDefinition
		require.NoError(t, json.Unmarshal([]byte(stored.Document), &unchanged))
		require.Equal(t, tc.source, unchanged)
	}
}
