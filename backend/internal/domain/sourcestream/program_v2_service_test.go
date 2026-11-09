package sourcestream

// 独立PG验证v2编译、权限、版本与候选状态；脚本使用真实Goja执行器。
// 进程内执行仅为单测，生产仍通过受限Executor；预览样本不冒充真实设备采集。
import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"testing"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/operationcontract"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type programV2RealExecutor struct{}

func (programV2RealExecutor) Check(ctx context.Context, script ScriptDefinition) error {
	_, err := streamexec.Execute(ctx, streamexec.Request{Code: script.Code, TimeoutMs: script.TimeoutMs, CheckOnly: true})
	return err
}
func (programV2RealExecutor) Transform(ctx context.Context, script ScriptDefinition, input any, metadata map[string]any) (ScriptResult, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return ScriptResult{}, err
	}
	info, err := json.Marshal(metadata)
	if err != nil {
		return ScriptResult{}, err
	}
	result, err := streamexec.Execute(ctx, streamexec.Request{Code: script.Code, TimeoutMs: script.TimeoutMs, Input: raw, Context: info})
	if err != nil {
		return ScriptResult{}, err
	}
	encoded, err := json.Marshal(result)
	var converted ScriptResult
	if err == nil {
		err = json.Unmarshal(encoded, &converted)
	}
	return converted, err
}
func (programV2RealExecutor) ExecuteProgram(ctx context.Context, request streamexec.ProgramRequest, host streamexec.ProgramHost) (streamexec.ProgramResult, error) {
	return streamexec.ExecuteProgram(ctx, request, host)
}

func programV2ServiceContext(keys ...string) context.Context {
	permissions := map[string]struct{}{}
	for _, key := range keys {
		permissions[key] = struct{}{}
	}
	return contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: permissions})
}
func programV2Definition(code string) StreamDefinition {
	return StreamDefinition{Model: ProgramAccess, Name: "Complete program", Script: ScriptDefinition{Code: code, TimeoutMs: 1000}}
}
func programV2Raw(t *testing.T, definition StreamDefinition) string {
	t.Helper()
	raw, err := json.Marshal(definition)
	require.NoError(t, err)
	return string(raw)
}

func TestProgramV2ServiceZeroSourceMultipleEntriesAndVersionActivation(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.read")
	s := New(nil, programV2RealExecutor{}, "")
	def := programV2Definition(`module.exports = function(api) {
	 api.stream.onRequest({id:"echo"}, context => context.input);
	 api.stream.onRequest({id:"other"}, () => ({name:"other"}));
	};`)
	manifest, snapshots, err := s.compileProgramV2(ctx, def, false)
	require.NoError(t, err)
	require.Empty(t, manifest.Sources)
	require.NotNil(t, snapshots)
	require.Len(t, manifest.Entries, 2)
	_, _, err = s.compileProgramV2(context.Background(), def, false)
	require.ErrorIs(t, err, ErrForbidden)
	_, _, err = New(nil, nil, "").compileProgramV2(ctx, def, false)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = s.testProgramV2(ctx, def, map[string]any{"data": map[string]any{"value": 7}}, "")
	require.ErrorContains(t, err, "entryId")
	_, err = s.testProgramV2(ctx, def, map[string]any{"data": nil}, "missing")
	require.ErrorContains(t, err, "entryId")
	view, err := s.testProgramV2(ctx, def, map[string]any{"data": map[string]any{"value": 7}}, "echo")
	require.NoError(t, err)
	result := view["result"].(streamexec.ProgramResult)
	require.JSONEq(t, `{"value":7}`, string(result.Data))
	require.True(t, view["dryRun"].(bool))
	require.False(t, view["sideEffects"].(bool))
	require.Empty(t, view["targets"])
	for _, payload := range []any{nil, []any{}, map[string]any{}, map[string]any{"data": 1, "password": "not-accepted"}, map[string]any{"data": 1, "sourceSamples": nil}, map[string]any{"data": 1, "topicSamples": []any{}}} {
		_, err = s.testProgramV2(ctx, def, payload, "echo")
		require.Error(t, err)
	}
	raw := programV2Raw(t, def)
	view, err = s.saveProgramV2(ctx, 0, 0, def, raw)
	require.NoError(t, err)
	require.NotContains(t, view, "activeRelease")
	id := view["id"].(int64)
	item, err := repo.NewSourceStreamRepo(store).Stream(ctx, id)
	require.NoError(t, err)
	view, err = s.deployProgramV2(ctx, item, item.Revision, 0, def, raw)
	require.NoError(t, err, "request-only program needs no NATS readiness")
	require.Equal(t, "active", view["deploymentState"])
	require.EqualValues(t, 1, view["activeRelease"])
	active, err := repo.NewSourceStreamRepo(store).Stream(ctx, id)
	require.NoError(t, err)
	require.Positive(t, active.ActiveVersion)
	require.Zero(t, active.PendingVersion)
	version, err := repo.NewSourceStreamRepo(store).Version(ctx, id, active.ActiveVersion)
	require.NoError(t, err)
	plan, err := version.ProgramExecutionPlan()
	require.NoError(t, err)
	require.Equal(t, repo.ProgramAuthorityRef{Kind: "user", ID: 1}, plan.Authority)
	policy, bindingDigest, err := version.ProgramRequestPolicy("echo")
	require.NoError(t, err)
	require.Empty(t, policy.UnsupportedReason)
	require.Equal(t, "compute", policy.Class)
	require.True(t, policy.EffectUpperBound)
	require.Len(t, bindingDigest, 64)
	history, err := s.Versions(ctx, id, 1, 20)
	require.NoError(t, err)
	historyJSON, err := json.Marshal(history)
	require.NoError(t, err)
	require.NotContains(t, string(historyJSON), "_executionPlan", "publication authority is not a public manifest field")
	require.NotContains(t, string(historyJSON), "_unsBindings")
	require.NotContains(t, string(historyJSON), "_requestPolicies")
	def.Name = "Edited draft"
	_, err = s.saveProgramV2(ctx, id, active.Revision, def, programV2Raw(t, def))
	require.NoError(t, err)
	edited, err := repo.NewSourceStreamRepo(store).Stream(ctx, id)
	require.NoError(t, err)
	require.Equal(t, active.ActiveVersion, edited.ActiveVersion, "saving a draft never replaces the serving version")
	view, err = s.deployProgramV2(ctx, edited, edited.Revision, 0, def, programV2Raw(t, def))
	require.NoError(t, err)
	require.EqualValues(t, 2, view["activeRelease"])
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, active.ActiveVersion)
	require.NoError(t, err)
	require.EqualValues(t, 1, view["activeRelease"], "rollback returns the selected historical release")
	rolledBack, err := repo.NewSourceStreamRepo(store).Version(ctx, id, active.ActiveVersion)
	require.NoError(t, err)
	rollbackPolicy, rollbackDigest, err := rolledBack.ProgramRequestPolicy("echo")
	require.NoError(t, err)
	require.Equal(t, policy, rollbackPolicy)
	require.Equal(t, bindingDigest, rollbackDigest)
	_, err = s.Disable(ctx, id, view["revision"].(int64)+1, true)
	require.ErrorIs(t, err, repo.ErrConflict)
	view, err = s.Disable(ctx, id, view["revision"].(int64), true)
	require.NoError(t, err, "active Stream can be deleted without a separate stop")
	require.Equal(t, "disabled", view["status"])
	_, err = s.Stream(ctx, id)
	require.Error(t, err)
	_, err = s.Versions(ctx, id, 1, 20)
	require.NoError(t, err, "deleted Stream retains release history")
}

func TestProgramV2DeclaredModbusRangeBeforeDiscovery(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read", "uns.write")
	r := repo.NewSourceStreamRepo(store)
	source, err := r.CreateSource(ctx, repo.Source{ID: 8101, Name: "PLC", Protocol: "modbus-tcp", Document: `{"name":"PLC","protocol":"modbus-tcp","runtimeLocation":"local","endpoint":"tcp://127.0.0.1:502","modbus":{"unitId":1,"ranges":[{"id":"value","table":"holding-register","address":5,"quantity":1,"writable":false,"unitId":7}]}}`})
	require.NoError(t, err)
	require.NoError(t, r.ObserveSource(ctx, source.ID, source.Revision, "healthy", "", nil))
	s := New(nil, programV2RealExecutor{}, "")
	program := func(ref string) StreamDefinition {
		return programV2Definition(fmt.Sprintf(`module.exports=api=>{
   const source=api.sources.use({id:"8101",resources:[%q]});
   api.stream.onRequest({id:"sample"},()=>source.read(%q));
  };`, ref, ref))
	}
	_, _, err = s.compileProgramV2(ctx, program("modbus:unregistered"), false)
	require.ErrorContains(t, err, "missingOrUnsupported")
	event := programV2Definition(`module.exports=api=>{
 const source=api.sources.use({id:"8101",resources:["modbus:value"]});
 api.stream.onMessage({id:"samples",source,resourceRefs:["modbus:value"]},()=>{});
 };`)
	_, _, err = s.compileProgramV2(ctx, event, false)
	require.NoError(t, err, "declared Modbus ranges retain their publish model")
	def := program("modbus:value")
	raw := programV2Raw(t, def)
	view, err := s.saveProgramV2(ctx, 0, 0, def, raw)
	require.NoError(t, err)
	stream, err := r.Stream(ctx, view["id"].(int64))
	require.NoError(t, err)
	view, err = s.deployProgramV2(ctx, stream, stream.Revision, 0, def, raw)
	require.NoError(t, err)
	version, err := r.Version(ctx, stream.ID, view["activeVersion"].(int64))
	require.NoError(t, err)
	policy, _, err := version.ProgramRequestPolicy("sample")
	require.NoError(t, err)
	require.Empty(t, policy.UnsupportedReason)
	require.Len(t, policy.Targets, 1)
	require.Equal(t, "read", policy.Targets[0].Action, "a read-only register cannot become a write grant")
	current, err := r.Source(ctx, source.ID)
	require.NoError(t, err)
	require.JSONEq(t, "[]", current.Resources)
	require.NoError(t, version.ValidateProgramExecutionSource(current))
	// A later observation with narrower capabilities takes precedence. Existing
	// publications must be rejected instead of replacing it with the declaration.
	current.Resources = `[{"id":"modbus:value","models":["request-reply"],"metadata":{"unitId":7,"table":"holding-register","address":5,"quantity":1,"writable":false}}]`
	require.ErrorIs(t, version.ValidateProgramExecutionSource(current), repo.ErrConflict)
}

func TestProgramV2RequestSourceTargetsFrozenWithRelease(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read", "uns.write")
	programV2SourceFixture(t, store, 8101)
	s := New(nil, programV2RealExecutor{}, "")
	def := programV2Definition(`module.exports=api=>{
	 api.sources.use({id:"8101",resources:["opcua:ns=2;s=temp"]});
	 for(const effect of ["read","write"]){api.stream.onRequest({id:effect,request:{code:"target."+effect,name:effect,contractVersion:"1",inputSchema:{},outputSchema:{},sideEffect:effect,idempotency:effect==="write"?"required":"optional",timeoutMs:1000}},ctx=>ctx.input);}
	};`)
	raw := programV2Raw(t, def)
	view, err := s.saveProgramV2(ctx, 0, 0, def, raw)
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(store)
	stream, err := r.Stream(ctx, view["id"].(int64))
	require.NoError(t, err)
	view, err = s.deployProgramV2(ctx, stream, stream.Revision, 0, def, raw)
	require.NoError(t, err)
	version, err := r.Version(ctx, stream.ID, view["activeVersion"].(int64))
	require.NoError(t, err)
	read, readDigest, err := version.ProgramRequestPolicy("read")
	require.NoError(t, err)
	require.Empty(t, read.UnsupportedReason)
	require.Equal(t, "device", read.Class)
	require.Len(t, read.Targets, 1)
	require.Equal(t, "read", read.Targets[0].Action)
	write, writeDigest, err := version.ProgramRequestPolicy("write")
	require.NoError(t, err)
	require.Empty(t, write.UnsupportedReason)
	require.Len(t, write.Targets, 2)
	require.NotEqual(t, readDigest, writeDigest)
	actions := []string{}
	for _, target := range write.Targets {
		require.Equal(t, int64(8101), target.SourceID)
		require.EqualValues(t, 1, target.ConfigRevision)
		require.Equal(t, "opcua:ns=2;s=temp", target.ResourceRef)
		require.Len(t, target.ConfigDigest, 64)
		require.Len(t, target.ResourceDigest, 64)
		require.Len(t, target.Digest, 64)
		actions = append(actions, target.Action)
	}
	require.ElementsMatch(t, []string{"read", "write"}, actions)
	require.NotContains(t, version.ProgramManifest, "opc.tcp://", "target snapshots must not expose connection values")
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(version.ProgramManifest), &fields))
	fields["_requestPolicies"] = json.RawMessage(`[]`)
	changed, err := json.Marshal(fields)
	require.NoError(t, err)
	forged := version
	forged.ProgramManifest = string(changed)
	_, _, err = forged.ProgramRequestPolicy("read")
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	_, _, err = version.ProgramRequestPolicy("missing")
	require.ErrorIs(t, err, repo.ErrNotFound)
	public, err := programVersionView(version)
	require.NoError(t, err)
	publicJSON, err := json.Marshal(public)
	require.NoError(t, err)
	require.NotContains(t, string(publicJSON), "_requestPolicies")
	require.NotContains(t, string(publicJSON), "configDigest")
}

func TestProgramV2RequestPolicyClosedCapabilities(t *testing.T) {
	entry := streamexec.ProgramEntry{ID: "run", Kind: "request", Request: &operationcontract.Resource{Code: "target.run", SideEffect: "write"}}
	manifest := streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{}, Topics: []streamexec.ProgramTopic{}, Entries: []streamexec.ProgramEntry{entry}}
	policies, err := programV2RequestPolicies(manifest, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "write-target-required", policies[0].UnsupportedReason)
	require.Empty(t, policies[0].Targets)
	manifest.Entries[0].Request.SideEffect = "read"
	policies, err = programV2RequestPolicies(manifest, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "compute", policies[0].Class)
	// Field views and legacy IDs cannot be widened into Collection grants.
	manifest.Topics = []streamexec.ProgramTopic{{TopicID: "123"}}
	policies, err = programV2RequestPolicies(manifest, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "unbound-data", policies[0].UnsupportedReason)
	ref := streamexec.ProgramTopic{CollectionID: "11111111-1111-4111-8111-111111111111", Access: "read"}
	manifest.Topics = []streamexec.ProgramTopic{ref}
	bindings := []repo.ProgramUNSBinding{{Reference: ref, CollectionID: ref.CollectionID}}
	policies, err = programV2RequestPolicies(manifest, nil, nil, bindings)
	require.NoError(t, err)
	require.Equal(t, "data", policies[0].Class)
	require.Equal(t, []string{"read", "query"}, []string{policies[0].Targets[0].Action, policies[0].Targets[1].Action})
	bindings[0].Reference.Access = "write"
	manifest.Topics[0] = bindings[0].Reference
	policies, err = programV2RequestPolicies(manifest, nil, nil, bindings)
	require.NoError(t, err)
	require.Equal(t, "read-effect-conflict", policies[0].UnsupportedReason)
	bindings[0].CollectionID = ""
	policies, err = programV2RequestPolicies(manifest, nil, nil, bindings)
	require.NoError(t, err)
	require.Equal(t, "unbound-data", policies[0].UnsupportedReason)
	// HTTP is a server-bound invoke capability, including scripts choosing GET.
	http := SourceDefinition{Name: "HTTP", Protocol: HTTP, Endpoint: "https://example.test", RuntimeLocation: "local", HTTP: &HTTPConnection{}}
	contract := repo.ProgramSourceContract{SourceID: 1, ConfigDigest: "config"}
	_, reason := programV2RequestSourceTargets(manifest, http, nil, contract, "read")
	require.Equal(t, "read-effect-conflict", reason)
	targets, reason := programV2RequestSourceTargets(manifest, http, nil, contract, "write")
	require.Empty(t, reason)
	require.Len(t, targets, 1)
	require.Equal(t, "invoke", targets[0].Action)
	require.Equal(t, "http:request", targets[0].ResourceRef)
	// A vendor template cannot silently become the generic HTTP descriptor.
	http.SourceType = "pi-web-api"
	_, reason = programV2RequestSourceTargets(manifest, http, nil, contract, "write")
	require.Equal(t, "unsupported-resource", reason)
}

func TestProgramV2RequestDataTargetsFrozenWithRelease(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.read", "uns.write")
	created, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Request data", RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	collectionID := created["collection"].(repo.UnsMeta).ID
	topic, err := unsmeta.New().Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Request data view", CollectionID: collectionID})
	require.NoError(t, err)
	topicID := topic.ID
	s := New(nil, programV2RealExecutor{}, "")
	def := programV2Definition(fmt.Sprintf(`module.exports=api=>{
	 api.uns.use({topicId:%q,access:"read-write"});
	 api.stream.onRequest({id:"run"},ctx=>ctx.input);
	};`, topicID))
	raw := programV2Raw(t, def)
	view, err := s.saveProgramV2(ctx, 0, 0, def, raw)
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(store)
	stream, err := r.Stream(ctx, view["id"].(int64))
	require.NoError(t, err)
	view, err = s.deployProgramV2(ctx, stream, stream.Revision, 0, def, raw)
	require.NoError(t, err)
	version, err := r.Version(ctx, stream.ID, view["activeVersion"].(int64))
	require.NoError(t, err)
	policy, digest, err := version.ProgramRequestPolicy("run")
	require.NoError(t, err)
	require.Empty(t, policy.UnsupportedReason)
	require.Equal(t, "data", policy.Class)
	require.Len(t, policy.Targets, 3)
	actions := []string{}
	for _, target := range policy.Targets {
		require.Equal(t, collectionID, target.CollectionID)
		require.Equal(t, version.ID, target.VersionID)
		require.Len(t, target.TopicBindingHash, 64)
		require.Len(t, target.Digest, 64)
		actions = append(actions, target.Action)
	}
	require.ElementsMatch(t, []string{"read", "query", "write"}, actions)
	require.Len(t, digest, 64)
	// Changing the public draft does not rewrite the release's resolved target.
	def.Name = "Changed draft"
	_, err = s.saveProgramV2(ctx, stream.ID, view["revision"].(int64), def, programV2Raw(t, def))
	require.NoError(t, err)
	stored, err := r.Version(ctx, stream.ID, version.ID)
	require.NoError(t, err)
	after, afterDigest, err := stored.ProgramRequestPolicy("run")
	require.NoError(t, err)
	require.Equal(t, policy, after)
	require.Equal(t, digest, afterDigest)
	updated, err := r.Stream(ctx, stream.ID)
	require.NoError(t, err)
	view, err = s.deployProgramV2(ctx, updated, updated.Revision, 0, def, programV2Raw(t, def))
	require.NoError(t, err)
	next, err := r.Version(ctx, stream.ID, view["activeVersion"].(int64))
	require.NoError(t, err)
	nextPolicy, nextDigest, err := next.ProgramRequestPolicy("run")
	require.NoError(t, err)
	require.NotEqual(t, digest, nextDigest)
	for _, nextTarget := range nextPolicy.Targets {
		for _, oldTarget := range policy.Targets {
			if oldTarget.Action == nextTarget.Action {
				require.NotEqual(t, oldTarget.Digest, nextTarget.Digest, "data target authority is release-bound, even when the Collection is unchanged")
			}
		}
	}
}

func TestProgramV2NativeCapabilitiesMatchHost(t *testing.T) {
	base := SourceDefinition{Name: "Modbus", Protocol: ModbusTCP, Endpoint: "tcp://127.0.0.1:502", RuntimeLocation: "local", Modbus: &ModbusOptions{UnitID: 7}}
	for _, tc := range []struct {
		name, table string
		quantity    int
		writable    bool
		write       bool
	}{
		{"register", "holding-register", 2, true, true},
		{"read-only", "holding-register", 2, false, false},
		{"write-limit", "holding-register", 124, true, false},
		{"input", "input-register", 2, false, false},
		{"coil", "coil", 2, true, true},
		{"coil-write-limit", "coil", 1969, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resource := NativeResource{ID: "modbus:value", Models: []AccessModel{Publish, Subscribe}, Metadata: map[string]any{"unitId": 9, "table": tc.table, "address": 0, "quantity": tc.quantity, "writable": tc.writable}}
			bound, err := programV2ResourceDefinition(base, &resource)
			require.NoError(t, err)
			require.Equal(t, 9, *bound.Modbus.Ranges[0].UnitID)
			require.Empty(t, base.Modbus.Ranges, "resolution cannot mutate the shared connection document")
			actions := programV2NativeActions(bound, &resource)
			require.Contains(t, actions, "source.read")
			require.Equal(t, tc.write, slices.Contains(actions, "source.write"))
			_, err = programV2Operation(bound, &resource, streamexec.ProgramCall{Kind: "source.read", ResourceRef: resource.ID})
			require.NoError(t, err)
			if !tc.write {
				_, err = programV2Operation(bound, &resource, streamexec.ProgramCall{Kind: "source.write", ResourceRef: resource.ID, Input: json.RawMessage(`{"values":[1,2]}`)})
				require.ErrorIs(t, err, streamexec.ErrSourceDenied)
			}
		})
	}
	bad := NativeResource{ID: "modbus:value", Models: []AccessModel{Publish}, Metadata: map[string]any{"table": "holding-register", "address": 65535, "quantity": 2}}
	_, err := programV2ResourceDefinition(base, &bad)
	require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	mqtt := SourceDefinition{Protocol: MQTT, MQTT: &MQTTOptions{RequestTopic: "req", ReplyTopic: "reply", CorrelationField: "id"}}
	require.Equal(t, []string{"source.publish"}, programV2NativeActions(mqtt, &NativeResource{ID: "mqtt:factory/cmd", Models: []AccessModel{Subscribe}}))
	require.Empty(t, programV2NativeActions(mqtt, &NativeResource{ID: "mqtt:factory/+", Models: []AccessModel{Subscribe}}))
	require.Equal(t, []string{"source.invoke"}, programV2NativeActions(mqtt, &NativeResource{ID: "mqtt-request-reply", Models: []AccessModel{RequestReply}}))
	opc := SourceDefinition{Protocol: OPCUA}
	require.Equal(t, []string{"source.read", "source.write"}, programV2NativeActions(opc, &NativeResource{ID: "opcua:value", Models: []AccessModel{Publish, Subscribe, RequestReply}, Metadata: map[string]any{"nodeClass": 2}}))
	require.Equal(t, []string{"source.invoke"}, programV2NativeActions(opc, &NativeResource{ID: "opcua:method", Models: []AccessModel{Publish, RequestReply}, Metadata: map[string]any{"nodeClass": 4}}))
}

func programV2SourceFixture(t *testing.T, store *repo.Store, id int64) {
	t.Helper()
	def := SourceDefinition{Name: fmt.Sprintf("OPC scope fixture %d", id), Protocol: OPCUA, Endpoint: "opc.tcp://127.0.0.1:4840", RuntimeLocation: "local",
		OPCUA: &OPCUAOptions{SecurityMode: "None", SecurityPolicy: "None", SamplingIntervalMs: 100, NodeIDs: []string{"ns=2;s=temp"}}}
	require.NoError(t, def.Validate())
	document, err := json.Marshal(def)
	require.NoError(t, err)
	resources, err := json.Marshal([]NativeResource{{ID: "opcua:ns=2;s=temp", Models: []AccessModel{Publish, Subscribe, RequestReply}, Metadata: map[string]any{"nodeClass": 2}}})
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(store)
	source, err := r.CreateSource(context.Background(), repo.Source{ID: id, Name: def.Name, Protocol: string(def.Protocol), Document: string(document)})
	require.NoError(t, err)
	_, err = r.ChangeSource(context.Background(), id, source.Revision, func(_ *gorm.DB, item *repo.Source) error {
		item.Resources, item.DesiredState, item.Health = string(resources), "enabled", "healthy"
		return nil
	})
	require.NoError(t, err)
}

func TestProgramBindingAcquisitionParity(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read")
	programV2SourceFixture(t, store, 8101)
	service := New(nil, programV2RealExecutor{}, "")
	binding := ProgramSourceBinding{Alias: "sensor", SourceID: "8101", ConfigRevision: 1, Resources: []string{"opcua:ns=2;s=temp"}, Acquisition: map[string]json.RawMessage{"samplingIntervalMs": json.RawMessage("2000"), "nodeIds": json.RawMessage(`["ns=2;s=temp"]`)}}
	for _, field := range []string{"sources", "sourceBindings"} {
		definition := programV2Definition(`module.exports=api=>{const source=api.sources.use('sensor');api.stream.onMessage({id:'samples',source,resourceRefs:['opcua:ns=2;s=temp']},()=>{});};`)
		if field == "sources" {
			definition.ProgramSources = []ProgramSourceBinding{binding}
		} else {
			definition.ProgramSourceBindings = []ProgramSourceBinding{binding}
		}
		manifest, _, err := service.compileProgramV2(ctx, definition, false)
		require.NoError(t, err, field)
		require.Len(t, manifest.Sources, 1)
		plan := ExecutionPlan{ExecutionRef: ExecutionRef{SourceID: 8101, SourceRevision: 1}, Definition: definition, Manifest: &manifest, Entry: &manifest.Entries[0]}
		source := opcuaSource()
		source.OPCUA.NodeIDs = []string{"ns=2;s=temp"}
		effective, err := plan.programWatchSource(source)
		require.NoError(t, err)
		require.Equal(t, 2000, effective.OPCUA.SamplingIntervalMs)
		require.Equal(t, 1000, source.OPCUA.SamplingIntervalMs, "Source defaults must not be mutated")
		require.NotEmpty(t, plan.ProgramWatchQuery().AcquisitionKey)
	}
	source := modbusSource()
	source.Modbus.Ranges[0].ID = "temperature"
	refs := []string{"modbus:temperature"}
	acquisition := map[string]json.RawMessage{"pollIntervalMs": json.RawMessage("2500"), "ranges": json.RawMessage(`[{"id":"temperature","table":"holding-register","address":0,"quantity":2}]`)}
	effective, err := programAcquiredSource(source, refs, acquisition)
	require.NoError(t, err)
	require.Equal(t, 2500, effective.Modbus.PollIntervalMs)
	require.Equal(t, 1000, source.Modbus.PollIntervalMs)
	require.Equal(t, source.Modbus.UnitID, effective.Modbus.UnitID)
	for _, raw := range []string{`{"unitId":2}`, `{"endpoint":"tcp://other:502"}`, `{"pollIntervalMs":99}`, `{"pollIntervalMs":null}`, `{"qos":1}`, `{"ranges":[{"id":"temperature","table":"holding-register","address":4,"quantity":2}]}`, `{"ranges":[{"id":"temperature","table":"holding-register","address":0,"quantity":2,"writable":true}]}`, `{"resourceRefs":["modbus:other"]}`} {
		var invalidAcquisition map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(raw), &invalidAcquisition))
		_, err := programAcquiredSource(source, refs, invalidAcquisition)
		require.Error(t, err, raw)
	}
}

func TestProgramAcquisitionOtherProtocols(t *testing.T) {
	for _, protocol := range []Protocol{HTTP, OAuth, EdgeSQL, FileRead, USBHID, OpenProtocol} {
		t.Run(string(protocol), func(t *testing.T) {
			source := SourceDefinition{Protocol: protocol, Endpoint: "unchanged", CredentialRef: "edge-owned"}
			refs := []string{"declared-resource"}
			for _, acquisition := range []map[string]json.RawMessage{nil, {}, {"resourceRefs": json.RawMessage(`["declared-resource"]`)}} {
				effective, err := programAcquiredSource(source, refs, acquisition)
				require.NoError(t, err)
				require.Equal(t, source, effective)
			}
			for _, raw := range []string{`{"pollIntervalMs":1000}`, `{"samplingIntervalMs":1000}`, `{"qos":1}`, `{"endpoint":"other"}`, `{"resourceRefs":["undeclared"]}`} {
				var acquisition map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(raw), &acquisition))
				_, err := programAcquiredSource(source, refs, acquisition)
				require.Error(t, err, raw)
			}
		})
	}
	resources := declaredDeviceResources(SourceDefinition{Protocol: MQTT, MQTT: &MQTTOptions{QoS: 0, Subscriptions: map[string]byte{"plant/state": 2}}}, 7)
	require.Len(t, resources, 1)
	require.Equal(t, 2, resources[0].Metadata["qos"], "catalog must preserve per-filter QoS")
}

func TestProgramV2DeleteAfterSourceDeletion(t *testing.T) {
	for _, repair := range []bool{false, true} {
		t.Run(fmt.Sprintf("repair=%t", repair), func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			ctx := programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read")
			programV2SourceFixture(t, store, 8101)
			s := New(nil, programV2RealExecutor{}, "")
			defer s.Close()
			def := programV2Definition(`module.exports=api=>{
 api.sources.use({id:"8101",resources:["opcua:ns=2;s=temp"]});
 api.stream.onRequest({id:"request"},ctx=>ctx.input);
};`)
			created, err := s.saveProgramV2(ctx, 0, 0, def, programV2Raw(t, def))
			require.NoError(t, err)
			r := repo.NewSourceStreamRepo(store)
			stream, err := r.Stream(ctx, created["id"].(int64))
			require.NoError(t, err)
			view, err := s.deployProgramV2(ctx, stream, stream.Revision, 0, def, stream.Document)
			require.NoError(t, err)
			source, err := r.Source(ctx, 8101)
			require.NoError(t, err)
			impact, err := s.Impact(ctx, 8101)
			require.NoError(t, err)
			require.EqualValues(t, 1, impact["count"])
			_, err = s.DeleteSource(ctx, source.ID, source.Revision, "")
			require.ErrorIs(t, err, repo.ErrConflict)
			_, err = s.DeleteSource(ctx, source.ID, source.Revision, impact["impactRevision"].(string))
			require.NoError(t, err, "confirmed deletion stops an enabled and referenced Source")
			stream, err = r.Stream(ctx, stream.ID)
			require.NoError(t, err)
			_, err = r.AdmitProgramV2Run(ctx, repo.StreamRun{ID: "deleted-source-request", StreamID: stream.ID, VersionID: stream.ActiveVersion, EntryID: "request", Model: "program", Status: "processing", Summary: `{}`}, true)
			require.Error(t, err, "a retained release cannot execute using a deleted Source")
			if repair {
				_, err = s.Disable(ctx, stream.ID, view["revision"].(int64), false)
				require.NoError(t, err, "stopping a Stream must not require its deleted dependencies")
				stopped, err := r.Stream(ctx, stream.ID)
				require.NoError(t, err)
				require.Equal(t, "disabled", stopped.Status)
				repaired := programV2Definition(`module.exports=api=>{api.stream.onRequest({id:"repaired"},ctx=>ctx.input);};`)
				_, err = s.saveProgramV2(ctx, stream.ID, stopped.Revision, repaired, programV2Raw(t, repaired))
				require.NoError(t, err, "removing a deleted Source from the draft must preserve the Stream identity")
				draft, err := r.Stream(ctx, stream.ID)
				require.NoError(t, err)
				view, err = s.deployProgramV2(ctx, draft, draft.Revision, 0, repaired, draft.Document)
				require.NoError(t, err)
				_, err = r.Version(ctx, stream.ID, stream.ActiveVersion)
				require.NoError(t, err, "repair retains the previous release history")
				_, _, err = s.compileProgramV2(ctx, def, true)
				require.Error(t, err, "the deleted Source must remain unavailable to new execution references")
			}
			_, err = s.Disable(ctx, stream.ID, view["revision"].(int64), true)
			require.NoError(t, err, "deleting a Stream must not require its deleted dependencies")
			_, err = r.Version(ctx, stream.ID, stream.ActiveVersion)
			require.NoError(t, err)
		})
	}
}

func TestProgramV2SourceEventPreviewContext(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read")
	programV2SourceFixture(t, store, 8101)
	programV2SourceFixture(t, store, 8102)
	s := New(nil, programV2RealExecutor{}, "")
	def := programV2Definition(`module.exports=api=>{
	 const source=api.sources.use({id:"8101",resources:["opcua:ns=2;s=temp"]});
	 const other=api.sources.use({id:"8102",resources:["opcua:ns=2;s=temp"]});
	 api.stream.onSource({id:"watch",source,resourceRefs:["opcua:ns=2;s=temp"]},context=>({
	  sourceId:context.sourceId,resourceRef:context.resourceRef,receivedAt:context.receivedAt,dryRun:context.dryRun,input:context.input
	 }));
	 api.stream.onRequest({id:"manual"},context=>context.input);
	 api.stream.onSource({id:"other-watch",source:other,resourceRefs:["opcua:ns=2;s=temp"]},context=>({sourceId:context.sourceId}));
	};`)
	for _, event := range []any{nil, map[string]any{"resourceRef": "opcua:ns=2;s=temp"}} {
		payload := map[string]any{"data": map[string]any{"value": 42}}
		if event != nil {
			payload["event"] = event
		}
		view, err := s.testProgramV2(ctx, def, payload, "watch")
		require.NoError(t, err)
		var result map[string]any
		require.NoError(t, json.Unmarshal(view["result"].(streamexec.ProgramResult).Data, &result))
		require.Equal(t, "8101", result["sourceId"])
		require.Equal(t, "opcua:ns=2;s=temp", result["resourceRef"])
		require.True(t, result["dryRun"].(bool))
		require.Positive(t, result["receivedAt"])
		require.Empty(t, view["targets"])
	}
	for _, event := range []any{nil, 42, []any{}, map[string]any{}, map[string]any{"resourceRef": "opcua:other"},
		map[string]any{"resourceRef": "opcua:ns=2;s=temp", "sourceId": "9999"},
		map[string]any{"resourceRef": "opcua:ns=2;s=temp", "dryRun": false},
		map[string]any{"resourceRef": "opcua:ns=2;s=temp", "receivedAt": 1}} {
		_, err := s.testProgramV2(ctx, def, map[string]any{"data": nil, "event": event}, "watch")
		require.Error(t, err)
	}
	_, err := s.testProgramV2(ctx, def, map[string]any{"data": nil, "event": map[string]any{"resourceRef": "opcua:ns=2;s=temp"}}, "manual")
	require.Error(t, err, "request previews must not impersonate Source events")
	view, err := s.testProgramV2(ctx, def, map[string]any{"data": nil}, "other-watch")
	require.NoError(t, err)
	require.JSONEq(t, `{"sourceId":"8102"}`, string(view["result"].(streamexec.ProgramResult).Data), "derive identity from the selected entry, not the first manifest Source")
}

func TestProgramV2PreviewMQTTConcreteScope(t *testing.T) {
	entry := streamexec.ProgramEntry{ID: "telemetry", Kind: "source-event", SourceID: "8101", ResourceRefs: []string{"mqtt:factory/+/telemetry", "mqtt:vibration/#"}}
	for _, ref := range []string{"mqtt:factory/press/telemetry", "mqtt:vibration/motor/data"} {
		metadata, err := programV2PreviewEvent(entry, json.RawMessage(fmt.Sprintf(`{"resourceRef":%q}`, ref)))
		require.NoError(t, err)
		require.Equal(t, ref, metadata["resourceRef"])
		require.Equal(t, "8101", metadata["sourceId"])
	}
	for _, raw := range []string{"", `null`, `{}`, `{"resourceRef":"mqtt:factory/+/telemetry"}`, `{"resourceRef":"mqtt:foreign/data"}`, `{"resourceRef":"mqtt:$SYS/stats"}`, `{"resourceRef":"mqtt:vibration/#"}`} {
		_, err := programV2PreviewEvent(entry, json.RawMessage(raw))
		require.Error(t, err)
	}
	entry.ResourceRefs = []string{"mqtt:factory/+/telemetry"}
	_, err := programV2PreviewEvent(entry, nil)
	require.Error(t, err, "a wildcard is never a concrete event identity")
	entry.ResourceRefs = []string{"mqtt:factory/press/telemetry"}
	metadata, err := programV2PreviewEvent(entry, nil)
	require.NoError(t, err)
	require.Equal(t, "mqtt:factory/press/telemetry", metadata["resourceRef"])
	entry.ResourceRefs = []string{"opcua:one", "opcua:two"}
	_, err = programV2PreviewEvent(entry, nil)
	require.Error(t, err, "multiple inputs require explicit selection")
	entry.SourceID = "not-a-source"
	_, err = programV2PreviewEvent(entry, json.RawMessage(`{"resourceRef":"opcua:one"}`))
	require.Error(t, err)
}

func TestProgramV2ServiceSourceTopicManifestPreviewAndPendingWatch(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read", "uns.write")
	programV2SourceFixture(t, store, 8101)
	topic, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{Name: "V2 output", Type: 2, TopicType: 3, EnableHistory: 2,
		Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}, nil, nil)
	require.NoError(t, err)
	s := New(nil, programV2RealExecutor{}, "")
	code := fmt.Sprintf(`module.exports = function(api) {
	 const source=api.sources.use({id:"8101",resources:["opcua:ns=2;s=temp"]});
	 const target=api.uns.use({topicId:%q});
	 api.stream.onRequest({id:"sample"},async () => { const data=await source.read("opcua:ns=2;s=temp"); await target.write(data); return data; });
	 api.stream.onSource({id:"watch",source,resourceRefs:["opcua:ns=2;s=temp"]},async context => { await target.write(context.input); });
	};`, strconv.FormatInt(topic.ID, 10))
	def := programV2Definition(code)
	manifest, snapshots, err := s.compileProgramV2(ctx, def, true)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatInt(topic.ID, 10), manifest.Topics[0].TopicID)
	require.Equal(t, []repo.StreamSourceSnapshot{{Alias: "8101", SourceID: 8101, ConfigRevision: 1, Resources: []string{"opcua:ns=2;s=temp"}}}, snapshots)
	_, _, err = s.compileProgramV2(programV2ServiceContext("uns.stream.manage", "uns.read", "uns.source"), def, false)
	require.ErrorIs(t, err, ErrForbidden)
	payload := map[string]any{"data": nil, "sourceSamples": map[string]any{"8101": map[string]any{"opcua:ns=2;s=temp": map[string]any{"value": 42}}}}
	preview, err := s.testProgramV2(ctx, def, payload, "sample")
	require.NoError(t, err)
	result := preview["result"].(streamexec.ProgramResult)
	require.JSONEq(t, `{"value":42}`, string(result.Data))
	require.Len(t, result.Outputs, 1)
	require.Equal(t, strconv.FormatInt(topic.ID, 10), result.Outputs[0].TopicID)
	require.JSONEq(t, `{"value":42}`, string(result.Outputs[0].Data))
	targets := preview["targets"].([]map[string]any)
	require.Equal(t, strconv.FormatInt(topic.ID, 10), targets[0]["topicId"], "public target IDs retain string precision")
	_, err = s.testProgramV2(ctx, def, map[string]any{"data": nil}, "sample")
	require.Error(t, err, "missing read samples must not silently read the live Source")
	readDef := programV2Definition(fmt.Sprintf(`module.exports=api=>{
	 const target=api.uns.use({topicId:%q});api.stream.onRequest({id:"current"},async()=>await target.read());
	};`, strconv.FormatInt(topic.ID, 10)))
	current, err := s.testProgramV2(ctx, readDef, map[string]any{"data": nil, "topicSamples": map[string]any{
		strconv.FormatInt(topic.ID, 10): map[string]any{"hasData": false, "data": nil},
	}}, "")
	require.NoError(t, err)
	require.JSONEq(t, `{"hasData":false,"data":null}`, string(current["result"].(streamexec.ProgramResult).Data))
	raw := programV2Raw(t, def)
	created, err := s.saveProgramV2(ctx, 0, 0, def, raw)
	require.NoError(t, err)
	item, err := repo.NewSourceStreamRepo(store).Stream(ctx, created["id"].(int64))
	require.NoError(t, err)
	_, err = s.deployProgramV2(ctx, item, item.Revision, 0, def, raw)
	require.ErrorIs(t, err, ErrUnavailable)
	s.SetAsyncReady(func() bool { return true })
	deployed, err := s.deployProgramV2(ctx, item, item.Revision, 0, def, raw)
	require.NoError(t, err)
	require.Equal(t, "pending", deployed["deploymentState"])
	staged, err := repo.NewSourceStreamRepo(store).Stream(ctx, item.ID)
	require.NoError(t, err)
	require.Zero(t, staged.ActiveVersion, "a healthy Source is not a ready Watch")
	require.Positive(t, staged.PendingVersion)
	require.Equal(t, "draft", staged.Status)
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=8101").Update("health", "unverified").Error)
	_, _, err = s.compileProgramV2(ctx, def, true)
	require.ErrorIs(t, err, repo.ErrConflict)
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=8101").Update("resources", "[]").Error)
	_, _, err = s.compileProgramV2(ctx, def, false)
	require.ErrorContains(t, err, "missingOrUnsupported")
}

func TestProgramV2ServicePendingCandidatePreservesServingManifest(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read")
	programV2SourceFixture(t, store, 8101)
	s := New(nil, programV2RealExecutor{}, "")
	s.SetAsyncReady(func() bool { return true })
	activeDef := programV2Definition(`module.exports=api=>{api.stream.onRequest({id:"original"},()=>({version:"original"}));};`)
	created, err := s.saveProgramV2(ctx, 0, 0, activeDef, programV2Raw(t, activeDef))
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(store)
	item, err := r.Stream(ctx, created["id"].(int64))
	require.NoError(t, err)
	_, err = s.deployProgramV2(ctx, item, item.Revision, 0, activeDef, item.Document)
	require.NoError(t, err)
	active, err := r.Stream(ctx, item.ID)
	require.NoError(t, err)
	candidate := programV2Definition(`module.exports=api=>{
	 const source=api.sources.use({id:"8101",resources:["opcua:ns=2;s=temp"]});
	 api.stream.onSource({id:"replacement",source,resourceRefs:["opcua:ns=2;s=temp"]},context=>context.input);
	};`)
	_, err = s.saveProgramV2(ctx, item.ID, active.Revision, candidate, programV2Raw(t, candidate))
	require.NoError(t, err)
	draft, err := r.Stream(ctx, item.ID)
	require.NoError(t, err)
	view, err := s.deployProgramV2(ctx, draft, draft.Revision, 0, candidate, draft.Document)
	require.NoError(t, err)
	require.Equal(t, "pending", view["deploymentState"])
	pending, err := r.Stream(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, active.ActiveVersion, pending.ActiveVersion)
	require.NotEqual(t, pending.ActiveVersion, pending.PendingVersion)
	serving, err := r.Version(ctx, item.ID, pending.ActiveVersion)
	require.NoError(t, err)
	servingManifest, err := serving.ProgramV2Manifest()
	require.NoError(t, err)
	require.Equal(t, servingManifest, view["manifest"], "candidate cannot replace the public serving manifest")
	require.Equal(t, "original", servingManifest.Entries[0].ID)
	require.Equal(t, "replacement", view["pendingManifest"].(streamexec.ProgramManifest).Entries[0].ID)
}

func TestProgramV2ServiceRejectsInvalidRegistrationAndNonTopicReferences(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.read", "uns.write")
	s := New(nil, programV2RealExecutor{}, "")
	for _, code := range []string{`invalid JavaScript(`, `module.exports = api => {};`, `module.exports=api=>{api.sources.use({id:"01",resources:[]});api.stream.onRequest({id:"r"},()=>1);};`} {
		_, _, err := s.compileProgramV2(ctx, programV2Definition(code), false)
		require.Error(t, err)
	}
	namespace, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{Name: "V2 namespace", Type: 1}, nil, nil)
	require.NoError(t, err)
	code := fmt.Sprintf(`module.exports=api=>{api.uns.use({topicId:%q});api.stream.onRequest({id:"r"},()=>1);};`, strconv.FormatInt(namespace.ID, 10))
	_, _, err = s.compileProgramV2(ctx, programV2Definition(code), false)
	require.ErrorContains(t, err, "topicRequired")
}

func TestProgramV2ServiceEmptyResourceGrantPreservesCanonicalSnapshot(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read")
	programV2SourceFixture(t, store, 8101)
	s := New(nil, programV2RealExecutor{}, "")
	def := programV2Definition(`module.exports=api=>{
	 api.sources.use({id:"8101",resources:[]});api.stream.onRequest({id:"noop"},()=>({ready:true}));
	};`)
	preview, err := s.testProgramV2(ctx, def, map[string]any{"data": nil}, "noop")
	require.NoError(t, err)
	require.JSONEq(t, `{"ready":true}`, string(preview["result"].(streamexec.ProgramResult).Data))
	created, err := s.saveProgramV2(ctx, 0, 0, def, programV2Raw(t, def))
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(store)
	item, err := r.Stream(ctx, created["id"].(int64))
	require.NoError(t, err)
	_, err = s.deployProgramV2(ctx, item, item.Revision, 0, def, item.Document)
	require.NoError(t, err)
	active, err := r.Stream(ctx, item.ID)
	require.NoError(t, err)
	version, err := r.Version(ctx, item.ID, active.ActiveVersion)
	require.NoError(t, err)
	snapshots, err := version.Sources()
	require.NoError(t, err)
	require.Equal(t, []string{}, snapshots[0].Resources, "empty grant must stay [], never silently expand or become nil")
}
