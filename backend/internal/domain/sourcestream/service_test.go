package sourcestream_test

import (
	"backend/internal/domain/sourcehealth"
	"backend/internal/testkit/scripttest"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"backend/internal/contextx"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/permission"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

type protocolFixture struct {
	calls                   int
	invokeError, probeError error
	probeTimeout            bool
	resources               []domain.NativeResource
}

func TestSourceHealthChecksSeparateDataAndSchedule(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := programContext()
	runtime := &protocolFixture{}
	service := domain.New(runtime, nil, "")
	defer service.Close()
	makeSource := func(name string) int64 {
		created, err := service.CreateSource(ctx, domain.SourceDefinition{Name: name, Protocol: domain.ModbusTCP, Endpoint: "tcp://device:502", RuntimeLocation: "local", Modbus: &domain.ModbusConnection{UnitID: 1}}, 0)
		require.NoError(t, err)
		return created["id"].(int64)
	}
	id := makeSource("health split")
	connected, err := service.CheckSource(ctx, id, "connection", 1000)
	require.NoError(t, err)
	require.Equal(t, "healthy", connected.Health)
	runtime.probeError = errors.New("must-not-expose-password-or-upstream-text")
	data, err := service.CheckSource(ctx, id, "data", 1000)
	require.NoError(t, err)
	require.Equal(t, "unhealthy", data.Health)
	require.NotContains(t, data.Message, "password")
	view, err := service.Source(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "healthy", view["health"])
	require.NotNil(t, view["dataCheck"])
	runtime.probeTimeout = true
	failure, err := service.CheckSource(ctx, id, "connection", 100)
	require.NoError(t, err)
	require.Equal(t, "SOURCE_TIMEOUT", failure.ErrorCode)
	now := time.Now()
	store := repo.NewSourceStreamRepo(ctx)
	pending := makeSource("pending health")
	disabled := makeSource("disabled health")
	_, err = service.SetSourceState(ctx, disabled, 1, "disabled", "")
	require.NoError(t, err)
	claimed, release, err := store.ClaimSourceHealth(ctx, now, sourcehealth.Interval)
	require.NoError(t, err)
	release()
	require.Equal(t, pending, claimed)
	claimed, release, err = store.ClaimSourceHealth(ctx, now, sourcehealth.Interval)
	require.NoError(t, err)
	release()
	require.Zero(t, claimed)
	claimed, release, err = store.ClaimSourceHealth(ctx, now.Add(9*time.Minute), sourcehealth.Interval)
	require.NoError(t, err)
	release()
	require.Zero(t, claimed)
	claimed, release, err = store.ClaimSourceHealth(ctx, now.Add(11*time.Minute), sourcehealth.Interval)
	require.NoError(t, err)
	release()
	require.NotEqual(t, disabled, claimed)
	require.NotZero(t, claimed)
}

func TestSourceHealthQC2000LiveLightAndData(t *testing.T) {
	endpoint := os.Getenv("SOURCE_HEALTH_QC_ENDPOINT")
	if endpoint == "" {
		t.Skip("explicit read-only QC2000 endpoint required")
	}
	testkit.NewTestDB(t)
	ctx := programContext()
	service := domain.New(nil, nil, "")
	defer service.Close()
	created, err := service.CreateSource(ctx, domain.SourceDefinition{Name: "QC2000 health regression", Protocol: domain.FileRead, RuntimeLocation: "local", Endpoint: endpoint, File: &domain.FileSourceOptions{Resources: []domain.FileSelection{{ID: "history", Path: "file", Tables: []string{"T_JYD"}}}}}, 0)
	require.NoError(t, err)
	id := created["id"].(int64)
	connection, err := service.CheckSource(ctx, id, "connection", 10000)
	require.NoError(t, err)
	require.Equal(t, "healthy", connection.Health, "%+v", connection)
	data, err := service.CheckSource(ctx, id, "data", 1000)
	require.NoError(t, err)
	require.Equal(t, "unhealthy", data.Health)
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		require.Equal(t, "SOURCE_SNAPSHOT_PENDING", data.ErrorCode)
		require.Contains(t, data.Message, "receivedBytes=")
		require.Contains(t, data.Message, "totalBytes=")
	} else {
		require.Equal(t, "SOURCE_TIMEOUT", data.ErrorCode)
	}
	require.Equal(t, "download", data.Stage)
	view, err := service.Source(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "healthy", view["health"])
	require.Equal(t, connection.CheckedAt, view["checkedAt"])
	t.Logf("light=%s latencyMs=%d data=%s stage=%s code=%s connectionStillHealthy=true", connection.Health, connection.LatencyMs, data.Health, data.Stage, data.ErrorCode)
}

func TestSourceNamedMySQLReadOnlyAndExactValues(t *testing.T) {
	endpoint := os.Getenv("SOURCE_PARITY_MYSQL_URL")
	if endpoint == "" {
		t.Skip("owned MySQL endpoint required")
	}
	testkit.NewTestDB(t)
	ctx := programContext()
	s := domain.New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer s.Close()
	created, err := s.CreateSource(ctx, domain.SourceDefinition{Name: "owned mysql", Protocol: domain.EdgeSQL, Endpoint: endpoint, RuntimeLocation: "local", SQL: &domain.SQLConnection{TLSMode: "disable", Queries: map[string]string{
		"sample":        "SELECT CAST(9007199254740993 AS SIGNED) AS id, '中文验收' AS label, NULL AS note",
		"write-attempt": "INSERT INTO acceptance_values(id,label) VALUES(1,'forbidden')",
	}}}, 0)
	require.NoError(t, err)
	id := created["id"].(int64)
	saved, err := s.SaveCredential(ctx, id, created["revision"].(int64), domain.Credential{Kind: "basic", Username: "parity", Secret: "synthetic-owned-only"}, "")
	require.NoError(t, err)
	limit := 1
	page, err := s.EdgeResourceRead(ctx, id, saved["configRevision"].(int64), "sql:sample", domain.EdgeResourceReadParams{Limit: &limit}, 5000)
	require.NoError(t, err)
	view := page.(map[string]any)
	rows := view["items"].([]map[string]any)
	require.Len(t, rows, 1)
	require.Equal(t, "9007199254740993", rows[0]["id"])
	require.Equal(t, "中文验收", rows[0]["label"])
	require.Nil(t, rows[0]["note"])
	require.Positive(t, view["collectedAt"])
	_, err = s.EdgeResourceRead(ctx, id, saved["configRevision"].(int64), "sql:write-attempt", domain.EdgeResourceReadParams{Limit: &limit}, 5000)
	require.Error(t, err, "even the privileged fixture account must use a database-enforced read-only transaction")
}

func TestSourceEnableDisablePreservesConfigurationAndRejectsReads(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := programContext()
	s := domain.New(&protocolFixture{}, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer s.Close()
	created, err := s.CreateSource(ctx, domain.SourceDefinition{Name: "state boundary", Protocol: domain.ModbusTCP, Endpoint: "tcp://127.0.0.1:502", RuntimeLocation: "local", Modbus: &domain.ModbusConnection{UnitID: 1}}, 0)
	require.NoError(t, err)
	id, revision := created["id"].(int64), created["revision"].(int64)
	_, err = s.Probe(ctx, id, nil, 1000)
	require.NoError(t, err)
	disabled, err := s.SetSourceState(ctx, id, revision, "disabled", "")
	require.NoError(t, err)
	require.Equal(t, "disabled", disabled["desiredState"])
	require.Equal(t, created["configRevision"], disabled["configRevision"])
	_, err = s.Probe(ctx, id, nil, 1000)
	require.Error(t, err)
	rows, total, err := repo.NewSourceStreamRepo(ctx).ListSources(ctx, repo.SourceFilter{DesiredState: "disabled"})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	_, err = s.SetSourceState(ctx, id, revision, "enabled", "")
	require.ErrorIs(t, err, repo.ErrConflict, "stale revisions cannot enable a Source")
	enabled, err := s.SetSourceState(ctx, id, disabled["revision"].(int64), "enabled", "")
	require.NoError(t, err)
	require.Equal(t, created["configRevision"], enabled["configRevision"])
	_, err = s.Probe(ctx, id, nil, 1000)
	require.NoError(t, err)
}

func (f *protocolFixture) Probe(ctx context.Context, _ int64, _ domain.RuntimeSource, _ domain.Credential) error {
	if f.probeTimeout {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.probeError
}
func (f *protocolFixture) Discover(context.Context, int64, domain.RuntimeSource, domain.Credential, string) ([]domain.NativeResource, error) {
	if f.resources != nil {
		return f.resources, nil
	}
	return []domain.NativeResource{{ID: "native", Name: "operation", Models: []domain.AccessModel{domain.Publish, domain.Subscribe, domain.RequestReply}}}, nil
}
func (f *protocolFixture) Invoke(_ context.Context, _ int64, _ domain.RuntimeSource, _ domain.Credential, _ string, input any) (any, error) {
	f.calls++
	return map[string]any{"applied": input}, f.invokeError
}

// 部署版本与真实订阅就绪分别观察；改草稿不能误用未部署触发范围。
func TestService_SourceSubscriptionReadinessIsNotProbeHealth(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}, "uns.source": {}, "uns.stream.manage": {}}})
	runtime := &protocolFixture{resources: []domain.NativeResource{{ID: "mqtt:data/+", Name: "data", Models: []domain.AccessModel{domain.Publish}}, {ID: "mqtt:draft/+", Models: []domain.AccessModel{domain.Publish}}}}
	s := domain.New(runtime, scripttest.New(t), "")
	defer s.Close()
	require.NoError(t, s.StartProgramExecutions(ctx))
	s.SetAsyncReady(func() bool { return true })
	source, err := s.CreateSource(ctx, domain.SourceDefinition{Name: "watch", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTOptions{ClientID: "watch", QoS: 1, TopicFilters: []string{"data/+"}}}, 0)
	require.NoError(t, err)
	id := source["id"].(int64)
	_, err = testkit.SetLegacySourceState(ctx, s, id, 1, "enabled")
	require.NoError(t, err)
	_, err = s.Probe(ctx, id, nil, 500)
	require.NoError(t, err)
	_, err = s.Discover(ctx, id, 2, "")
	require.NoError(t, err)
	topic, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{Name: "watch output", Type: 2, TopicType: 3, EnableHistory: 2, Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}, nil, nil)
	require.NoError(t, err)
	def := domain.StreamDefinition{Name: "watch mapping", Model: domain.ProgramAccess, Script: domain.ScriptDefinition{Code: fmt.Sprintf(`module.exports=api=>{
 const source=api.sources.use({id:"%d",resources:["mqtt:data/+"]}); const output=api.uns.use({topicId:"%d"});
 api.stream.onMessage({id:"telemetry",source,resourceRefs:["mqtt:data/+"],qos:1},async ctx=>{await output.write(ctx.input);});
};`, id, topic.ID), TimeoutMs: 1000}}
	view, err := s.SaveStream(ctx, 0, 0, def)
	require.NoError(t, err)
	streamID := view["id"].(int64)
	view, err = s.Deploy(ctx, streamID, 1, 0, 0)
	require.NoError(t, err)
	require.Equal(t, "pending", view["deploymentState"], "successful Probe is not a confirmed subscription")
	require.Zero(t, view["activeVersion"])
	plans, err := s.Plans(ctx)
	require.NoError(t, err)
	require.Empty(t, plans, "unconfirmed subscriptions cannot receive business events")
	state := "ready"
	s.SetWatchState(func(query domain.WatchQuery) string {
		require.Equal(t, domain.WatchQuery{SourceID: id, ConfigRevision: 1, ResourceRefs: []string{"mqtt:data/+"}, QoS: 1}, query)
		return state
	})
	require.NoError(t, s.ActivateReadyProgramV2(ctx))
	view, err = s.Stream(ctx, streamID)
	require.NoError(t, err)
	require.Equal(t, "ready", view["executionState"])
	state = "source-watch-failed"
	view, err = s.Stream(ctx, streamID)
	require.NoError(t, err)
	require.Equal(t, state, view["executionState"])
	state = "invalid-observer-state"
	view, err = s.Stream(ctx, streamID)
	require.NoError(t, err)
	require.Equal(t, "starting", view["executionState"])
	def.Script.Code = strings.ReplaceAll(def.Script.Code, "mqtt:data/+", "mqtt:draft/+")
	view, err = s.SaveStream(ctx, streamID, view["revision"].(int64), def)
	require.NoError(t, err, "readiness remains scoped to the active version, not this draft")
	_, err = s.Streams(ctx, repo.StreamFilter{})
	require.NoError(t, err)
}

func TestServicePersistenceDeploymentAndSynchronousIdempotency(t *testing.T) {
	t.Setenv("TIER0_RUNTIME_MODE", "native")
	testkit.NewTestDB(t)
	ctx := programContext()
	runtime := &protocolFixture{resources: []domain.NativeResource{{ID: "mqtt-request-reply", Models: []domain.AccessModel{domain.RequestReply}}}}
	s := domain.New(runtime, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer s.Close()
	require.NoError(t, s.StartProgramExecutions(ctx))
	def := domain.SourceDefinition{Name: "自有测试连接", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTConnection{ClientID: "device", RequestTopic: "request", ReplyTopic: "reply", CorrelationField: "requestId"}, Extensions: map[string]json.RawMessage{"vendor": json.RawMessage(`{"future":42}`)}}
	invalid := map[string]any{"name": def.Name, "protocol": def.Protocol, "endpoint": def.Endpoint, "runtimeLocation": "local", "mqtt": def.MQTT, "extensions": map[string]any{"password": "never-store"}}
	_, err := s.CreateSource(ctx, invalid, 0)
	require.Error(t, err)
	_, err = s.CreateSource(ctx, []int{1}, 0)
	require.Error(t, err)
	// Existing Sources retain their legacy acquisition scope; newer Streams
	// may override it without mutating the shared connection document.
	for name, mqtt := range map[string]map[string]any{
		"legacy-qos":    {"clientId": "test", "qos": 1},
		"legacy-filter": {"clientId": "test", "topicFilters": []string{"data/#"}},
	} {
		compatible, e := s.CreateSource(ctx, map[string]any{"name": name, "protocol": "mqtt", "endpoint": "mqtt://broker:1883", "runtimeLocation": "local", "mqtt": mqtt}, 0)
		require.NoError(t, e)
		actual := compatible["document"].(map[string]any)["mqtt"].(map[string]any)
		for key, value := range mqtt {
			expected, _ := json.Marshal(value)
			retained, _ := json.Marshal(actual[key])
			require.JSONEq(t, string(expected), string(retained), "legacy field %s is retained", key)
		}
	}
	for _, mqtt := range []map[string]any{
		{"clientId": "test", "qos": 3},
		{"clientId": "test", "topicFilters": []string{"data/#/invalid"}},
	} {
		_, err = s.CreateSource(ctx, map[string]any{"name": "invalid-scope", "protocol": "mqtt", "endpoint": "mqtt://broker:1883", "runtimeLocation": "local", "mqtt": mqtt}, 0)
		require.Error(t, err)
	}
	view, err := s.CreateSource(ctx, def, 0)
	require.NoError(t, err)
	sourceID := view["id"].(int64)
	require.Equal(t, "unverified", view["health"])
	_, err = s.Source(ctx, sourceID)
	require.NoError(t, err)
	_, err = s.Sources(ctx, repo.SourceFilter{})
	require.NoError(t, err)
	view, err = testkit.SetLegacySourceState(ctx, s, sourceID, 1, "enabled")
	require.NoError(t, err)
	_, err = s.Probe(ctx, sourceID, nil, 500)
	require.NoError(t, err)
	_, err = s.Discover(ctx, sourceID, 2, "")
	require.NoError(t, err)
	streamDef := domain.StreamDefinition{Name: "受控操作", Model: domain.ProgramAccess, Script: domain.ScriptDefinition{Code: fmt.Sprintf(`module.exports=api=>{
 const source=api.sources.use({id:"%d",resources:["mqtt-request-reply"]});
 api.stream.onRequest({id:"double"},async ctx=>{const result=await source.invoke("mqtt-request-reply",{value:ctx.input.value*2});return result.applied;});
};`, sourceID), TimeoutMs: 1000}}
	missingResource := streamDef
	missingResource.Script.Code = strings.ReplaceAll(streamDef.Script.Code, "mqtt-request-reply", "mqtt:missing")
	_, err = s.SaveStream(ctx, 0, 0, missingResource)
	require.ErrorContains(t, err, "missingOrUnsupported")
	_, _, err = s.Validate(ctx, missingResource, currentConfigRevision(t, ctx, s, sourceID), true)
	require.ErrorContains(t, err, "missingOrUnsupported")
	stream, err := s.SaveStream(ctx, 0, 0, streamDef)
	require.NoError(t, err)
	streamID := stream["id"].(int64)
	_, err = s.DeleteSource(ctx, sourceID, currentSourceRevision(t, ctx, s, sourceID), "")
	require.ErrorIs(t, err, repo.ErrConflict, "referenced Source requires current impact confirmation")
	_, err = s.UpdateSource(ctx, sourceID, currentSourceRevision(t, ctx, s, sourceID)-1, def, "")
	require.ErrorIs(t, err, repo.ErrConflict)
	impact, err := s.Impact(ctx, sourceID)
	require.NoError(t, err)
	require.EqualValues(t, 1, impact["count"])
	_, _, err = s.Validate(ctx, streamDef, currentConfigRevision(t, ctx, s, sourceID), true)
	require.NoError(t, err)
	preview, err := s.Test(ctx, streamDef, map[string]any{"data": map[string]any{"value": 7}}, currentConfigRevision(t, ctx, s, sourceID), "request")
	require.NoError(t, err)
	require.Equal(t, false, preview["sideEffects"])
	require.Zero(t, runtime.calls)
	stream, err = s.Deploy(ctx, streamID, 1, currentConfigRevision(t, ctx, s, sourceID), 0)
	require.NoError(t, err)
	version := stream["activeVersion"].(int64)
	result, err := s.Invoke(ctx, streamID, version, "operation-1", map[string]any{"value": 7}, 1000)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"value": float64(14)}, result["result"].(map[string]any)["data"])
	require.Equal(t, 1, runtime.calls)
	duplicate, err := s.Invoke(ctx, streamID, version, "operation-1", map[string]any{"value": 7}, 1000)
	require.NoError(t, err)
	require.Equal(t, true, duplicate["replayed"])
	require.Equal(t, 1, runtime.calls)
	_, err = s.Invoke(ctx, streamID, version, "operation-1", map[string]any{"value": 8}, 1000)
	require.ErrorIs(t, err, repo.ErrConflict)
	run, err := s.Run(ctx, result["run"].(map[string]any)["id"].(string))
	require.NoError(t, err)
	require.Equal(t, "succeeded", run["status"])
	summary, _ := json.Marshal(run["summary"])
	require.NotContains(t, string(summary), `"value"`)
	_, err = s.Runs(ctx, repo.StreamRunFilter{StreamID: streamID})
	require.NoError(t, err)
	readOnly := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 2, ResourceKeys: map[string]struct{}{"uns.read": {}}})
	_, err = s.Invoke(readOnly, streamID, version, "denied", nil, 1000)
	require.ErrorIs(t, err, domain.ErrForbidden)
	runtime.invokeError = domain.ErrUnknownResult
	_, err = s.Invoke(ctx, streamID, version, "operation-timeout", map[string]any{"value": 1}, 1000)
	require.ErrorIs(t, err, domain.ErrUnknownResult)
	_, err = s.Invoke(ctx, streamID, version, "operation-timeout", map[string]any{"value": 1}, 1000)
	require.ErrorIs(t, err, domain.ErrUnknownResult)
	require.Equal(t, 2, runtime.calls)
	streamDef.Script.Code = `module.exports=api=>{api.stream.onRequest({id:"double"},()=>null);};`
	stream, err = s.SaveStream(ctx, streamID, stream["revision"].(int64), streamDef)
	require.NoError(t, err)
	require.Equal(t, version, stream["activeVersion"])
	stream, err = s.Deploy(ctx, streamID, stream["revision"].(int64), currentConfigRevision(t, ctx, s, sourceID), 0)
	require.NoError(t, err)
	filtered, err := s.Invoke(ctx, streamID, stream["activeVersion"].(int64), "drop", nil, 1000)
	require.NoError(t, err)
	require.Nil(t, filtered["result"].(map[string]any)["data"])
	require.Equal(t, 2, runtime.calls)
	_, err = s.Versions(ctx, streamID, 1, 10)
	require.NoError(t, err)
	stream, err = s.Deploy(ctx, streamID, stream["revision"].(int64), currentConfigRevision(t, ctx, s, sourceID), version)
	require.NoError(t, err)
	require.Equal(t, version, stream["activeVersion"])
	stream, err = s.Disable(ctx, streamID, stream["revision"].(int64), false)
	require.NoError(t, err)
	_, err = s.Invoke(ctx, streamID, version, "disabled", nil, 1000)
	require.ErrorIs(t, err, repo.ErrConflict)
	impact, err = s.Impact(ctx, sourceID)
	require.NoError(t, err)
	view, err = s.SaveCredential(ctx, sourceID, currentSourceRevision(t, ctx, s, sourceID), domain.Credential{Kind: "basic", Username: "user", Secret: "never-export"}, impact["impactRevision"].(string))
	require.NoError(t, err)
	encoded, _ := json.Marshal(view)
	require.NotContains(t, string(encoded), "never-export")
	def.CredentialRef = view["document"].(map[string]any)["credentialRef"].(string)
	impact, err = s.Impact(ctx, sourceID)
	require.NoError(t, err)
	view, err = s.UpdateSource(ctx, sourceID, view["revision"].(int64), def, impact["impactRevision"].(string))
	require.NoError(t, err)
	require.Equal(t, "unverified", view["health"])
	runtime.probeTimeout = true
	_, err = s.Probe(ctx, sourceID, nil, 100)
	require.True(t, errors.Is(err, context.DeadlineExceeded))
	view, err = s.Source(ctx, sourceID)
	require.NoError(t, err)
	require.Equal(t, "unhealthy", view["health"])

	evaluator := permission.New(ctx)
	readSource := contextx.Subject{ResourceKeys: map[string]struct{}{"uns.source": {}}}
	allowed, _, err := evaluator.Allow(ctx, readSource, "api", "GET", "/api/core/uns/sources/123")
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, _, err = evaluator.Allow(ctx, readSource, "api", "PUT", "/api/core/uns/sources/123")
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, _, err = evaluator.Allow(ctx, readSource, "api", "PUT", "/api/core/uns/sources/123/credentials")
	require.NoError(t, err)
	require.False(t, allowed)
}

// 内嵌 Stream 保存会增加 Source 元数据修订号；连接 configRevision 保持独立。
func currentSourceRevision(t *testing.T, ctx context.Context, service *domain.Service, id int64) int64 {
	t.Helper()
	view, err := service.Source(ctx, id)
	require.NoError(t, err)
	return view["revision"].(int64)
}

func TestIndependentScriptStreamLifecycle(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programContext()
	runtime := &protocolFixture{resources: []domain.NativeResource{{ID: "modbus:value", Models: []domain.AccessModel{domain.Publish}, Metadata: map[string]any{"table": "holding-register", "address": 0, "quantity": 1}}}}
	s := domain.New(runtime, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer s.Close()
	require.NoError(t, s.StartProgramExecutions(ctx))
	def := domain.StreamDefinition{Name: "Independent", Model: domain.ProgramAccess, Script: domain.ScriptDefinition{Code: `module.exports=api=>{api.stream.onRequest({id:"read"},ctx=>ctx.input);};`, TimeoutMs: 1000}}
	saved, err := s.SaveStream(ctx, 0, 0, def)
	require.NoError(t, err)
	id := saved["id"].(int64)
	require.Zero(t, saved["sourceId"])
	// 未发布草稿的 versions 为 JSON null，不应使其他 Source 的依赖/主题查询失败。
	unrelated, err := s.CreateSource(ctx, domain.SourceDefinition{Name: "Draft dependency check", Protocol: domain.ModbusTCP, Endpoint: "tcp://127.0.0.1:502", RuntimeLocation: "local", Modbus: &domain.ModbusConnection{UnitID: 1}}, 0)
	require.NoError(t, err)
	_, err = s.Impact(ctx, unrelated["id"].(int64))
	require.NoError(t, err)
	_, _, err = repo.NewSourceStreamRepo(store).ListStreams(ctx, repo.StreamFilter{SourceID: unrelated["id"].(int64)})
	require.NoError(t, err)
	_, _, err = repo.NewSourceStreamRepo(store).ListStreams(ctx, repo.StreamFilter{TopicID: 123})
	require.NoError(t, err)
	saved, err = s.Deploy(ctx, id, saved["revision"].(int64), 0, 0)
	require.NoError(t, err)
	result, err := s.Invoke(ctx, id, saved["activeVersion"].(int64), "first", map[string]any{"value": 42}, 1000)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"value": float64(42)}, result["result"].(map[string]any)["data"])
	r := repo.NewSourceStreamRepo(store)
	ids := []int64{}
	for _, alias := range []string{"first", "second"} {
		source, err := s.CreateSource(ctx, domain.SourceDefinition{Name: alias, Protocol: domain.ModbusTCP, Endpoint: "tcp://127.0.0.1:502", RuntimeLocation: "local", Modbus: &domain.ModbusConnection{UnitID: 1}}, 0)
		require.NoError(t, err)
		sourceID := source["id"].(int64)
		_, err = testkit.SetLegacySourceState(ctx, s, sourceID, source["revision"].(int64), "enabled")
		require.NoError(t, err)
		_, err = s.Probe(ctx, sourceID, nil, 500)
		require.NoError(t, err)
		_, err = s.Discover(ctx, sourceID, currentSourceRevision(t, ctx, s, sourceID), "")
		require.NoError(t, err)
		ids = append(ids, sourceID)
	}
	def.Script.Code = fmt.Sprintf(`module.exports=api=>{
 const first=api.sources.use({id:"%d",resources:["modbus:value"]}); const second=api.sources.use({id:"%d",resources:["modbus:value"]});
 api.stream.onRequest({id:"read"},async()=>({a:await first.read("modbus:value"),b:await second.read("modbus:value")}));
};`, ids[0], ids[1])
	saved, err = s.SaveStream(ctx, id, saved["revision"].(int64), def)
	require.NoError(t, err)
	list, total, err := r.ListStreams(ctx, repo.StreamFilter{SourceID: ids[0]})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, id, list[0].ID)
	impact, err := s.Impact(ctx, ids[0])
	require.NoError(t, err)
	require.EqualValues(t, 1, impact["count"])
	saved, err = s.Deploy(ctx, id, saved["revision"].(int64), 0, 0)
	require.NoError(t, err)
	_, err = s.Invoke(ctx, id, saved["activeVersion"].(int64), "two", nil, 1000)
	require.NoError(t, err)
	require.Equal(t, 2, runtime.calls)
	_, err = s.Invoke(ctx, id, saved["activeVersion"].(int64), "two", nil, 1000)
	require.NoError(t, err)
	require.Equal(t, 2, runtime.calls)
	denied := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 2, ResourceKeys: map[string]struct{}{"uns.write": {}}})
	_, err = s.Invoke(denied, id, saved["activeVersion"].(int64), "denied", nil, 1000)
	require.ErrorIs(t, err, domain.ErrForbidden)
	source, err := r.Source(ctx, ids[0])
	require.NoError(t, err)
	impact, err = s.Impact(ctx, source.ID)
	require.NoError(t, err)
	_, err = s.SaveCredential(ctx, source.ID, source.Revision, domain.Credential{Kind: "basic", Username: "user", Secret: "secret"}, impact["impactRevision"].(string))
	require.NoError(t, err)
	_, err = s.Invoke(ctx, id, saved["activeVersion"].(int64), "stale", nil, 1000)
	require.ErrorIs(t, err, repo.ErrConflict)
	saved, err = s.Disable(ctx, id, saved["revision"].(int64), false)
	require.NoError(t, err)
	_, err = s.Disable(ctx, id, saved["revision"].(int64), true)
	require.NoError(t, err)
}

func currentConfigRevision(t *testing.T, ctx context.Context, service *domain.Service, id int64) int64 {
	t.Helper()
	view, err := service.Source(ctx, id)
	require.NoError(t, err)
	return view["configRevision"].(int64)
}

func TestServiceReadReferenceFencesAndReadOnly(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.source": {}, "uns.read": {}}})
	runtime := &protocolFixture{}
	service := domain.New(runtime, nil, "")
	selected := domain.ModbusRange{ID: "temperature", Table: "holding-register", Address: 0, Quantity: 2}
	created, err := service.CreateSource(ctx, domain.SourceDefinition{Name: "reference", Protocol: domain.ModbusTCP, Endpoint: "tcp://edge-device:502", RuntimeLocation: "local", Modbus: &domain.ModbusOptions{UnitID: 7, Ranges: []domain.ModbusRange{selected}}}, 0)
	require.NoError(t, err)
	id := created["id"].(int64)
	_, err = testkit.SetLegacySourceState(ctx, service, id, 1, "enabled")
	require.NoError(t, err)
	value, err := service.ReadReference(ctx, id, 1, selected, 500)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"applied": map[string]any{"operation": "read"}}, value)
	require.Equal(t, 1, runtime.calls)
	_, err = service.ReadReference(ctx, id, 2, selected, 500)
	require.ErrorIs(t, err, repo.ErrConflict)
	other := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 2, ResourceKeys: map[string]struct{}{}})
	_, err = service.ReadReference(other, id, 1, selected, 500)
	require.ErrorIs(t, err, domain.ErrForbidden)
	write := true
	override := 8
	for _, invalid := range []domain.ModbusRange{
		{ID: "temperature", Table: "holding-register", Address: 1, Quantity: 2},
		{ID: "write", Table: "holding-register", Address: 0, Quantity: 1, Writable: &write},
		{ID: "unit", Table: "holding-register", Address: 0, Quantity: 1, UnitID: &override},
		{ID: "overflow", Table: "holding-register", Address: 65535, Quantity: 2},
		{ID: "oversize", Table: "holding-register", Address: 0, Quantity: 126},
	} {
		_, err = service.ReadReference(ctx, id, 1, invalid, 500)
		require.Error(t, err)
	}
	require.Equal(t, 1, runtime.calls, "rejected references never reach the device")
}
