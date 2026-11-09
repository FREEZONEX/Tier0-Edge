package sourcestream

// 独立 PG、真实 Goja 与 HTTP 测试端验证多入口、Source 调用、权限与幂等。
// 此处进程内 Goja 仅供领域单测；正式子进程与 HTTP 鉴权在 OpenAPI 测试验证。
// 这是协议夹具，不是已部署设备；测试私有拨号器不改变生产网络限制。
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOperationResourceAppAndMCPFailClosed(t *testing.T) {
	s := New(nil, nil, "")
	t.Cleanup(s.Close)
	subject := contextx.Subject{UserID: 1, APIKeyAppID: "controller", APIKeyUsageType: repo.APIKeyUsageAppData,
		ResourceKeys: map[string]struct{}{"control.read": {}, "control.execute": {}, "control.write": {}}}
	ctx := contextx.WithSubject(context.Background(), subject)
	_, err := s.OperationResources(ctx, "", "", 1, 10)
	require.ErrorIs(t, err, ErrForbidden, "未装配 App 授权检查器必须拒绝")
	calls := 0
	failure := errors.New("grant database unavailable")
	s.SetControlAuthorizer(func(context.Context, string) error { calls++; return failure })
	_, err = s.OperationResources(ctx, "", "", 1, 10)
	require.ErrorIs(t, err, failure, "基础设施错误不能吞掉或伪装为无授权")
	require.Equal(t, 1, calls)
	subject.MCPRestricted = true
	_, err = s.OperationResources(contextx.WithSubject(context.Background(), subject), "", "", 1, 10)
	require.ErrorIs(t, err, ErrForbidden)
	require.Equal(t, 1, calls, "MCP 不能借 App 上下文绕过未授予的控制能力")
	subject.MCPRestricted = false
	subject.ResourceKeys = map[string]struct{}{}
	_, err = s.OperationResources(contextx.WithSubject(context.Background(), subject), "", "", 1, 10)
	require.ErrorIs(t, err, ErrForbidden, "管理员所属的受限 Key 也不能绕过范围")
}

func TestStreamRequestAuthorityIsIndependent(t *testing.T) {
	s := New(nil, nil, "")
	t.Cleanup(s.Close)
	subject := contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"stream.requests.invoke": {}, "stream.requests.write": {}}}
	ctx := contextx.WithSubject(context.Background(), subject)
	require.NoError(t, s.streamRequestAccess(ctx, "control.read", "control.execute"), "invocation does not require discovery")
	require.ErrorIs(t, s.streamRequestAccess(ctx, "control.read"), ErrForbidden)
	require.ErrorIs(t, s.operationAccess(ctx, "control.execute"), ErrForbidden, "request rights never grant legacy Control authority")
	subject.APIKeyUsageType = repo.APIKeyUsageAppData
	ctx = contextx.WithSubject(context.Background(), subject)
	require.ErrorIs(t, s.streamRequestAccess(ctx, "control.execute"), ErrForbidden, "missing live App checker is denied")
	calls := []string{}
	s.SetControlAuthorizer(func(_ context.Context, key string) error { calls = append(calls, key); return nil })
	require.NoError(t, s.streamRequestAccess(ctx, "control.read", "control.execute", "control.write"))
	require.Equal(t, []string{"stream.requests.invoke", "stream.requests.write"}, calls)
	subject.MCPRestricted = true
	require.ErrorIs(t, s.streamRequestAccess(contextx.WithSubject(context.Background(), subject), "control.execute"), ErrForbidden)
	subject.MCPRestricted = false
	subject.FleetTask = &contextx.FleetTaskSubject{NodeID: "node"}
	require.ErrorIs(t, s.streamRequestAccess(contextx.WithSubject(context.Background(), subject), "control.execute"), ErrForbidden)
	require.Len(t, calls, 2)
}

// Lose only the terminal return after real VM execution; registration still
// uses the same compiler. Transport fault coverage lives in streamexecutor.
type lostOperationTerminalExecutor struct {
	programV2RealExecutor
	calls int
}

type publisherChangeExecutor struct {
	programV2RealExecutor
	before, after func()
}

// The domain consumes explicit refreshed facts. The real Auth resolver's DB
// revocation tests live in auth; the HTTP integration wires both components.
func testProgramAuthority(ctx context.Context) func(context.Context, repo.ProgramAuthorityRef) (contextx.Subject, error) {
	subject, _ := contextx.SubjectFrom(ctx)
	return func(context.Context, repo.ProgramAuthorityRef) (contextx.Subject, error) { return subject, nil }
}

func (e *publisherChangeExecutor) ExecuteProgram(ctx context.Context, req streamexec.ProgramRequest, host streamexec.ProgramHost) (streamexec.ProgramResult, error) {
	if !req.CheckOnly && e.before != nil {
		e.before()
	}
	result, err := e.programV2RealExecutor.ExecuteProgram(ctx, req, host)
	if !req.CheckOnly && e.after != nil {
		e.after()
	}
	return result, err
}

func TestStreamRequestPublisherRecheckedBeforeIOAndReplay(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.source", "uns.read", "uns.stream.manage", "stream.requests.invoke", "stream.requests.write")
	publisherAvailable := true
	keys := []string{"uns.source", "uns.read", "uns.stream.manage"}
	key, _, err := repo.NewAPIKeyRepo(ctx).CreateAPIKey(ctx, "request-publisher", 1, 1, "full_access", "external", "personal", keys)
	require.NoError(t, err)
	publisher, _ := contextx.SubjectFrom(ctx)
	publisher.AuthType, publisher.APIKeyID = "apiKey", key.ID
	publishCtx := contextx.WithSubject(ctx, publisher)
	executor := &publisherChangeExecutor{}
	s := New(nil, executor, "")
	t.Cleanup(s.Close)
	require.NoError(t, s.StartProgramExecutions(ctx))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	t.Cleanup(server.Close)
	httpTestManager(t, s.httpManager, server)
	source, err := s.CreateSource(ctx, SourceDefinition{Name: "Publisher authority", Protocol: HTTP,
		Endpoint: "http://fixture.internal/api", RuntimeLocation: "local", HTTP: &HTTPConnection{Authentication: "none"}}, 0)
	require.NoError(t, err)
	_, err = repo.NewSourceStreamRepo(store).ChangeSource(ctx, source["id"].(int64), source["revision"].(int64), func(_ *gorm.DB, row *repo.Source) error {
		row.Health = "healthy"
		return nil
	})
	require.NoError(t, err)
	code := fmt.Sprintf(`module.exports=api=>{const source=api.sources.use({id:%q,resources:[]});
	 api.stream.onRequest({id:"write",request:{code:"publisher.write",name:"Write",contractVersion:"1",inputSchema:{},outputSchema:{},sideEffect:"write",idempotency:"required",timeoutMs:3000}},async()=>{const response=await source.request({method:"POST",path:"action",json:{}});return response.data;});};`, strconv.FormatInt(source["id"].(int64), 10))
	view, err := s.SaveStream(publishCtx, 0, 0, map[string]any{"name": "Publisher checks", "script": code, "timeoutMs": 3000})
	require.NoError(t, err)
	view, err = s.Deploy(publishCtx, view["id"].(int64), view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	// A caller with broader current permissions cannot substitute for the Key
	// that published this immutable release.
	_, err = s.InvokeStreamRequest(ctx, "publisher.write", "missing-resolver", nil)
	require.ErrorIs(t, err, ErrForbidden)
	var reservations int64
	require.NoError(t, store.UnsDB().Model(&repo.OperationInvocation{}).Count(&reservations).Error)
	require.Zero(t, reservations)
	currentPublisher := publisher
	currentPublisher.KeyType, currentPublisher.APIKeyPermission = "personal", "full_access"
	s.SetProgramAuthorityResolver(func(_ context.Context, ref repo.ProgramAuthorityRef) (contextx.Subject, error) {
		require.Equal(t, repo.ProgramAuthorityRef{Kind: "api_key", ID: key.ID}, ref)
		if !publisherAvailable {
			return contextx.Subject{}, ErrForbidden
		}
		return currentPublisher, nil
	})
	first, err := s.InvokeStreamRequest(ctx, "publisher.write", "original", nil)
	require.NoError(t, err)
	require.False(t, first.Replayed)
	require.EqualValues(t, 1, calls.Load())
	legacyCaller := programV2ServiceContext("control.read", "control.execute", "control.write")
	legacyReceipt, err := s.OperationInvocation(legacyCaller, "publisher.write", "original")
	require.NoError(t, err)
	require.Equal(t, first.RunID, legacyReceipt["execution"].(map[string]any)["runId"])
	checkDenied := func(key string) {
		t.Helper()
		before := calls.Load()
		_, err := s.InvokeStreamRequest(ctx, "publisher.write", key, nil)
		require.ErrorIs(t, err, ErrForbidden)
		require.Equal(t, before, calls.Load())
	}
	setStatus := func(status int) {
		t.Helper()
		publisherAvailable = status == 1
	}
	setStatus(0)
	checkDenied("original")
	checkDenied("revoked-new")
	directCaller := programV2ServiceContext("uns.write", "uns.stream.execute")
	_, err = s.InvokeEntry(directCaller, view["id"].(int64), view["activeVersion"].(int64), "write", "direct-revoked", nil, 3000)
	require.ErrorIs(t, err, ErrForbidden)
	require.EqualValues(t, 1, calls.Load(), "direct Stream invocation cannot bypass publisher revocation")
	_, err = s.OperationInvocation(legacyCaller, "publisher.write", "original")
	require.ErrorIs(t, err, ErrForbidden)
	_, err = s.InvokeOperationResource(legacyCaller, "publisher.write", "legacy-route", nil)
	require.ErrorIs(t, err, ErrForbidden)
	setStatus(1)
	_, err = s.InvokeEntry(directCaller, view["id"].(int64), view["activeVersion"].(int64), "write", "direct-allowed", nil, 3000)
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	setStatus(0)
	_, err = s.InvokeEntry(directCaller, view["id"].(int64), view["activeVersion"].(int64), "write", "direct-allowed", nil, 3000)
	require.ErrorIs(t, err, ErrForbidden, "direct receipt replay also requires current publisher authority")
	setStatus(1)
	currentPublisher.APIKeyID++
	checkDenied("wrong-principal")
	currentPublisher.APIKeyID = key.ID
	currentPublisher.APIKeyPermission = "read_only"
	checkDenied("original")
	checkDenied("read-only")
	currentPublisher.APIKeyPermission = "full_access"
	delete(currentPublisher.ResourceKeys, "uns.source")
	checkDenied("role-removed")
	currentPublisher.ResourceKeys["uns.source"] = struct{}{}
	replay, err := s.InvokeStreamRequest(ctx, "publisher.write", "original", nil)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.EqualValues(t, 2, calls.Load())
	// Revocation after admission is observed by the real host before any HTTP I/O.
	executor.before = func() { setStatus(0) }
	_, err = s.InvokeStreamRequest(ctx, "publisher.write", "before-io", nil)
	require.Error(t, err)
	require.EqualValues(t, 2, calls.Load())
	executor.before = nil
	setStatus(1)
	// A completed device write is not rolled back or reported as safely retryable
	// when publication authority disappears before output persistence.
	executor.after = func() { setStatus(0) }
	_, err = s.InvokeStreamRequest(ctx, "publisher.write", "after-io", nil)
	require.ErrorIs(t, err, ErrUnknownResult)
	require.EqualValues(t, 3, calls.Load())
	executor.after = nil
	checkDenied("after-io")
	setStatus(1)
	_, err = s.InvokeStreamRequest(ctx, "publisher.write", "after-io", nil)
	require.ErrorIs(t, err, ErrUnknownResult)
	require.EqualValues(t, 3, calls.Load(), "recovery never repeats the device operation")
}

func (e *lostOperationTerminalExecutor) ExecuteProgram(ctx context.Context, req streamexec.ProgramRequest, host streamexec.ProgramHost) (streamexec.ProgramResult, error) {
	result, err := e.programV2RealExecutor.ExecuteProgram(ctx, req, host)
	if req.CheckOnly || err != nil {
		return result, err
	}
	e.calls++
	return streamexec.ProgramResult{}, ErrUnknownResult
}

func TestStreamRequestLostTerminalPreservesUnknownAndNeverReexecutes(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "stream.requests.invoke", "stream.requests.write", "control.read")
	executor := &lostOperationTerminalExecutor{}
	s := New(nil, executor, "")
	s.SetProgramAuthorityResolver(testProgramAuthority(ctx))
	t.Cleanup(s.Close)
	require.NoError(t, s.StartProgramExecutions(ctx))
	view, err := s.SaveStream(ctx, 0, 0, map[string]any{
		"name": "Lost terminal", "timeoutMs": 3000,
		"script": `module.exports=api=>api.stream.onRequest({id:"send",request:{code:"fixture.lost",name:"Send",contractVersion:"1",inputSchema:{},outputSchema:{},sideEffect:"write",idempotency:"required",timeoutMs:3000}},ctx=>ctx.input);`,
	})
	require.NoError(t, err)
	view, err = s.Deploy(ctx, view["id"].(int64), view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	_, err = s.InvokeStreamRequest(ctx, "fixture.lost", "intent", nil)
	require.ErrorIs(t, err, ErrUnknownResult)
	require.NotErrorIs(t, err, ErrControlOutputInvalid)
	require.Equal(t, 1, executor.calls)
	r := repo.NewSourceStreamRepo(store)
	invocation, err := r.OperationInvocation(ctx, "fixture.lost", "intent", 1, 0, "")
	require.NoError(t, err)
	run, err := r.Run(ctx, repo.ProgramRunIdentity(invocation.MessageID, invocation.StreamID, invocation.VersionID, invocation.EntryID))
	require.NoError(t, err)
	require.Equal(t, "unknown", run.Status, "a lost terminal must not become a durable known failure")
	for i := 0; i < 2; i++ {
		_, err = s.InvokeStreamRequest(ctx, "fixture.lost", "intent", nil)
		require.ErrorIs(t, err, ErrUnknownResult)
	}
	require.Equal(t, 1, executor.calls, "the frozen reservation prevents executing the same intent again")
	_, err = s.Disable(ctx, view["id"].(int64), view["revision"].(int64), false)
	require.NoError(t, err)
	_, err = s.InvokeStreamRequest(ctx, "fixture.lost", "intent", nil)
	require.ErrorIs(t, err, ErrUnknownResult, "disabling must preserve the original unknown outcome")
	require.Equal(t, 1, executor.calls)
}

func TestOperationImplicitRequestPublicationReplayAndLegacyRollback(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.read", "uns.write", "uns.stream.manage", "uns.stream.execute", "control.read", "control.execute", "control.write")
	s := New(nil, programV2RealExecutor{}, "")
	s.SetProgramAuthorityResolver(testProgramAuthority(ctx))
	t.Cleanup(s.Close)
	require.NoError(t, s.StartProgramExecutions(ctx))
	code := `module.exports=api=>api.stream.onRequest({id:"echo"},ctx=>ctx.input);`
	// An absent timeout exercises the persisted document's default as well.
	doc := map[string]any{"name": "Implicit request", "script": code}
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	id := view["id"].(int64)
	r := repo.NewSourceStreamRepo(store)
	row, err := r.Stream(ctx, id)
	require.NoError(t, err)
	draft, err := row.ProgramV2DraftManifest()
	require.NoError(t, err)
	require.Nil(t, draft.Entries[0].Request)
	// Reproduce a pre-migration published version through the repository path.
	staged, legacy, err := r.StageProgramV2(ctx, id, row.Revision, 0, row.Document, draft, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	row, err = r.ActivateStagedProgramV2(ctx, id, staged.Revision, legacy.ID)
	require.NoError(t, err)
	view, err = s.Deploy(ctx, id, row.Revision, 0, 0)
	require.NoError(t, err)
	row, err = r.Stream(ctx, id)
	require.NoError(t, err)
	firstVersion := row.ActiveVersion
	definition, err := streamexec.ImplicitRequestDefinition(id, "echo", streamexec.DefaultProgramTimeoutMs)
	require.NoError(t, err)
	public, err := s.OperationResource(ctx, definition.Code)
	require.NoError(t, err)
	require.Equal(t, "required", public["contract"].(map[string]any)["idempotency"])
	_, err = s.InvokeOperationResource(ctx, definition.Code, "", nil)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	first, err := s.InvokeOperationResource(ctx, definition.Code, "first", map[string]any{"value": 1})
	require.NoError(t, err)
	firstJSON, err := json.Marshal(first["result"])
	require.NoError(t, err)
	requestCtx := programV2ServiceContext("stream.requests.read", "stream.requests.invoke", "stream.requests.write")
	requestContract, err := s.DescribeStreamRequest(requestCtx, definition.Code)
	require.NoError(t, err)
	require.Equal(t, definition.ContractVersion, requestContract.ContractVersion)
	requestReceipt, err := s.InvokeStreamRequest(requestCtx, definition.Code, "cloud-request", map[string]any{"value": 1})
	require.NoError(t, err)
	require.False(t, requestReceipt.Replayed)
	doc["script"] = strings.Replace(code, "ctx=>ctx.input", `()=>({value:2})`, 1)
	doc["timeoutMs"] = 2000
	view, err = s.SaveStream(ctx, id, view["revision"].(int64), doc)
	require.NoError(t, err)
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	replayed, err := s.InvokeOperationResource(ctx, definition.Code, "first", map[string]any{"value": 1})
	require.NoError(t, err)
	replayedJSON, err := json.Marshal(replayed["result"])
	require.NoError(t, err)
	require.JSONEq(t, string(firstJSON), string(replayedJSON))
	fresh, err := s.InvokeOperationResource(ctx, definition.Code, "second", nil)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"value": float64(2)}, fresh["result"])
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, firstVersion)
	require.NoError(t, err, "rollback restores the generated contract before frozen comparison")
	fresh, err = s.InvokeOperationResource(ctx, definition.Code, "rollback", []any{"old implementation"})
	require.NoError(t, err)
	require.Equal(t, []any{"old implementation"}, fresh["result"])
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, legacy.ID)
	require.NoError(t, err, "pre-migration release stays without a request contract")
	oldVersion, err := r.Version(ctx, id, legacy.ID)
	require.NoError(t, err)
	oldManifest, err := oldVersion.ProgramV2Manifest()
	require.NoError(t, err)
	require.Nil(t, oldManifest.Entries[0].Request)
	_, err = s.DescribeStreamRequest(requestCtx, definition.Code)
	require.ErrorIs(t, err, repo.ErrNotFound)
	requestPage, err := s.DiscoverStreamRequests(requestCtx, "", 0, 0)
	require.NoError(t, err)
	require.Empty(t, requestPage.List)
	requestReplay, err := s.InvokeStreamRequest(requestCtx, definition.Code, "cloud-request", map[string]any{"value": 1})
	require.NoError(t, err)
	require.True(t, requestReplay.Replayed)
	require.Equal(t, requestReceipt.RunID, requestReplay.RunID)
	_, err = s.InvokeOperationResource(ctx, definition.Code, "after-legacy-rollback", nil)
	require.ErrorIs(t, err, ErrControlInactive)
	replayed, err = s.InvokeOperationResource(ctx, definition.Code, "first", map[string]any{"value": 1})
	require.NoError(t, err)
	replayedJSON, err = json.Marshal(replayed["result"])
	require.NoError(t, err)
	require.JSONEq(t, string(firstJSON), string(replayedJSON))
}

func TestOperationRequestPublicationAndOptionalIdempotency(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.read", "uns.stream.manage", "uns.stream.execute", "control.read", "control.execute", "control.write")
	s := New(nil, programV2RealExecutor{}, "")
	s.SetProgramAuthorityResolver(testProgramAuthority(ctx))
	t.Cleanup(s.Close)
	require.NoError(t, s.StartProgramExecutions(ctx))
	code := `module.exports=api=>{
	 api.stream.onRequest({id:"read",request:{code:"fixture.request",name:"请求",contractVersion:"1",inputSchema:{},outputSchema:{},sideEffect:"read",idempotency:"optional",timeoutMs:30000}},ctx=>ctx.input);
	 api.stream.onRequest({id:"write",request:{code:"fixture.write",name:"写请求",contractVersion:"1",inputSchema:{},outputSchema:{},sideEffect:"write",idempotency:"required",timeoutMs:30000}},ctx=>ctx.input);
	};`
	doc := map[string]any{"name": "Request contracts", "script": code, "timeoutMs": 5000}
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	id := view["id"].(int64)
	_, err = s.OperationResource(ctx, "fixture.request")
	require.ErrorIs(t, err, repo.ErrNotFound, "draft contracts are not published")
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	public, err := s.OperationResource(ctx, "fixture.request")
	require.NoError(t, err)
	require.Equal(t, "read", public["contract"].(map[string]any)["sideEffect"])
	require.Equal(t, "optional", public["contract"].(map[string]any)["idempotency"])
	row, err := repo.NewSourceStreamRepo(ctx).Stream(ctx, id)
	require.NoError(t, err)
	version, err := repo.NewSourceStreamRepo(ctx).Version(ctx, id, row.ActiveVersion)
	require.NoError(t, err)
	manifest, err := version.ProgramV2Manifest()
	require.NoError(t, err)
	require.NotNil(t, manifest.Entries[0].Request)
	require.Nil(t, manifest.Entries[0].Control)
	host, err := s.ProgramV2Host(ctx, manifest, []repo.StreamSourceSnapshot{}, "", "read", nil, true, nil, nil)
	require.NoError(t, err)
	_, err = host(ctx, streamexec.ProgramCall{Kind: "checkpoint.set", Input: json.RawMessage(`{}`)})
	require.ErrorIs(t, err, streamexec.ErrSourceDenied, "trusted host also rejects read-only effects")
	input := map[string]any{"value": "first"}
	first, err := s.InvokeOperationResource(ctx, "fixture.request", "", input)
	require.NoError(t, err)
	second, err := s.InvokeOperationResource(ctx, "fixture.request", "", input)
	require.NoError(t, err)
	require.NotEqual(t, first["execution"].(map[string]any)["runId"], second["execution"].(map[string]any)["runId"])
	first, err = s.InvokeOperationResource(ctx, "fixture.request", "frozen", input)
	require.NoError(t, err)
	_, err = s.InvokeOperationResource(ctx, "fixture.write", "", input)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	doc["script"] = strings.Replace(code, "ctx=>ctx.input", `ctx=>({value:"changed"})`, 1)
	view, err = s.SaveStream(ctx, id, view["revision"].(int64), doc)
	require.NoError(t, err)
	_, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	replayed, err := s.InvokeOperationResource(ctx, "fixture.request", "frozen", input)
	require.NoError(t, err)
	require.Equal(t, first["result"], replayed["result"])
	require.Equal(t, true, replayed["execution"].(map[string]any)["replayed"])
	fresh, err := s.InvokeOperationResource(ctx, "fixture.request", "fresh", input)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"value": "changed"}, fresh["result"])
	// A replacement may change both input shape and authority. Old receipts
	// still use their own contract; fresh calls must use the new locked binding.
	replacement := strings.Replace(code, `sideEffect:"read",idempotency:"optional"`, `sideEffect:"write",idempotency:"required"`, 1)
	replacement = strings.Replace(replacement, `contractVersion:"1"`, `contractVersion:"2"`, 1)
	replacement = strings.Replace(replacement, `inputSchema:{}`, `inputSchema:{type:"object",required:["new"],properties:{new:{type:"string"}}}`, 1)
	doc["script"] = replacement
	row, err = repo.NewSourceStreamRepo(ctx).Stream(ctx, id)
	require.NoError(t, err)
	view, err = s.SaveStream(ctx, id, row.Revision, doc)
	require.NoError(t, err)
	_, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	readOnly := programV2ServiceContext("control.read", "control.execute")
	replayed, err = s.InvokeOperationResource(readOnly, "fixture.request", "frozen", input)
	require.NoError(t, err, "original read contract survives a new write-only schema")
	require.Equal(t, first["result"], replayed["result"])
	newInput := map[string]any{"new": "current"}
	_, err = s.InvokeOperationResource(readOnly, "fixture.request", "denied-write", newInput)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = s.InvokeOperationResource(ctx, "fixture.request", "", newInput)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	_, err = s.InvokeOperationResource(ctx, "fixture.request", "invalid-schema", input)
	require.ErrorIs(t, err, ErrControlInvalidInput)
	_, err = s.InvokeOperationResource(ctx, "fixture.request", "new-write", newInput)
	require.NoError(t, err)
	_, err = s.InvokeOperationResource(readOnly, "fixture.request", "new-write", newInput)
	require.ErrorIs(t, err, ErrForbidden, "write receipts also require their frozen side-effect permission")
	require.NoError(t, store.UnsDB().Model(&repo.OperationInvocation{}).Where("identity=?", first["invocationId"]).Update("contract", "{}").Error)
	replayed, err = s.InvokeOperationResource(readOnly, "fixture.request", "frozen", input)
	require.NoError(t, err, "legacy receipts resolve their original frozen version, not the current schema")
	require.Equal(t, first["result"], replayed["result"])
}

func TestOperationResourceSourceAndReplay(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.read", "uns.write", "uns.source", "uns.stream.manage", "uns.stream.execute", "control.read", "control.execute", "control.write")
	s := New(nil, programV2RealExecutor{}, "")
	s.SetProgramAuthorityResolver(testProgramAuthority(ctx))
	t.Cleanup(s.Close)
	require.NoError(t, s.StartProgramExecutions(ctx))
	var calls atomic.Int32
	thirdParty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/bad") {
			_, _ = w.Write([]byte(`{"accepted":"wrong"}`))
			return
		}
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "device-1", body["deviceId"])
		_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true, "operation": r.URL.Path})
	}))
	t.Cleanup(thirdParty.Close)
	httpTestManager(t, s.httpManager, thirdParty)
	source, err := s.CreateSource(ctx, SourceDefinition{Name: "Operation HTTP fixture", Protocol: HTTP,
		Endpoint: "http://fixture.internal/api", RuntimeLocation: "local", HTTP: &HTTPConnection{Authentication: "none"}}, 0)
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(store)
	sourceID := source["id"].(int64)
	_, err = r.ChangeSource(ctx, sourceID, source["revision"].(int64), func(_ *gorm.DB, item *repo.Source) error {
		item.DesiredState, item.Health = "enabled", "healthy"
		return nil
	})
	require.NoError(t, err)
	code := fmt.Sprintf(`module.exports=api=>{
	 const source=api.sources.use({id:%q,resources:[]});
	 for(const name of ["start","stop","bad"]){
	  api.stream.onRequest({id:name,control:{code:"fixture."+name,name,contractVersion:"1",
	   inputSchema:{type:"object",required:["deviceId"],properties:{deviceId:{type:"string"}},additionalProperties:false},
	   outputSchema:{type:"object",required:["accepted"],properties:{accepted:{type:"boolean"},operation:{type:"string"}}},
	   sideEffect:"write",idempotency:"required",timeoutMs:3000}},async context=>{
	    const response=await source.request({method:"POST",path:name,json:context.input});
	    return response.data;
	  });
	 }
	};`, strconv.FormatInt(sourceID, 10))
	doc := map[string]any{"name": "Three operations", "script": code, "timeoutMs": 3000}
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	id := view["id"].(int64)
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err, "synchronous operations need no NATS")
	public, err := s.OperationResources(ctx, "fixture.", "active", 0, 0)
	require.NoError(t, err)
	require.Len(t, public["list"], 3)
	encoded, err := json.Marshal(public)
	require.NoError(t, err)
	for _, private := range []string{`"binding":`, `"deploymentVersion":`, "fixture.internal", `"script":`} {
		require.NotContains(t, string(encoded), private)
	}
	input := map[string]any{"deviceId": "device-1"}
	_, err = s.InvokeOperationResource(programV2ServiceContext("uns.read"), "fixture.start", "denied", input)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = s.InvokeOperationResource(programV2ServiceContext("control.read", "control.execute"), "fixture.start", "denied", input)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = s.InvokeOperationResource(ctx, "fixture.start", "invalid", map[string]any{"deviceId": 123})
	require.Error(t, err)
	require.Zero(t, calls.Load())
	first, err := s.InvokeOperationResource(ctx, "fixture.start", "start-intent", input)
	require.NoError(t, err)
	require.Equal(t, false, first["execution"].(map[string]any)["usesNATS"])
	require.Equal(t, "/api/start", first["result"].(map[string]any)["operation"])
	_, err = s.InvokeOperationResource(ctx, "fixture.stop", "stop-intent", input)
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load())
	// Deploy the same public contract with a changed implementation.
	doc["script"] = strings.Replace(code, "return response.data;", `return {accepted:false,operation:"changed"};`, 1)
	view, err = s.SaveStream(ctx, id, view["revision"].(int64), doc)
	require.NoError(t, err)
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	replay, err := s.InvokeOperationResource(ctx, "fixture.start", "start-intent", input)
	require.NoError(t, err)
	require.Equal(t, true, replay["execution"].(map[string]any)["replayed"])
	require.Equal(t, first["result"], replay["result"])
	require.Equal(t, int32(2), calls.Load(), "deployment cannot replay the physical operation")
	_, err = s.InvokeOperationResource(ctx, "fixture.start", "start-intent", map[string]any{"deviceId": "other"})
	require.ErrorIs(t, err, ErrIdempotencyConflict)
	// Restore the original implementation, then exercise invalid external output.
	doc["script"] = code
	view, err = s.SaveStream(ctx, id, view["revision"].(int64), doc)
	require.NoError(t, err)
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	_, err = s.InvokeOperationResource(ctx, "fixture.bad", "bad-intent", input)
	require.Error(t, err)
	require.Equal(t, int32(3), calls.Load(), "output failure may follow a real side effect")
	_, err = s.InvokeOperationResource(ctx, "fixture.bad", "bad-intent", input)
	require.ErrorIs(t, err, ErrUnknownResult)
	require.Equal(t, int32(3), calls.Load(), "failed output must never cause automatic device replay")

	// 真实 PG + Goja + HTTP 副作用验证远程总授权与逐项 grant 的交集。
	_, definition, err := r.Operation(ctx, "fixture.start")
	require.NoError(t, err)
	task := contextx.FleetTaskSubject{NodeID: "center:fixture", Scope: "controls.invoke", ResourceID: "fixture.start", GrantID: "fixture-grant", GrantRevision: 1}
	remote := contextx.WithSubject(ctx, contextx.Subject{FleetTask: &task})
	options := OperationInvokeOptions{CapabilityVersion: definition.CapabilityVersion(), ExpiresAt: time.Now().Add(30 * time.Second).UnixMilli()}
	_, err = s.InvokeOperationResourceWithOptions(remote, "fixture.start", "remote-disabled", input, options)
	require.ErrorIs(t, err, ErrRemoteControlDisabled, "未装配总授权时默认拒绝")
	s.SetRemoteControlAccess(func() error { return nil })
	_, err = s.InvokeOperationResourceWithOptions(remote, "fixture.start", "remote-no-grant", input, options)
	require.ErrorIs(t, err, repo.ErrControlGrant, "总开关不代替逐项授权")
	_, err = r.SaveControlGrant(ctx, repo.ControlGrant{Principal: task.NodeID, ControlCode: task.ResourceID, GrantID: task.GrantID, CapabilityVersion: options.CapabilityVersion, InputSchema: `{"type":"object","required":["deviceId"],"properties":{"deviceId":{"type":"string"}},"additionalProperties":false}`, MaxTTLMillis: 30000, MinIntervalMillis: 1000, Enabled: true}, 0)
	require.NoError(t, err)
	checks := 0
	s.SetRemoteControlAccess(func() error {
		checks++
		if checks > 1 {
			return ErrRemoteControlDisabled
		}
		return nil
	})
	_, err = s.InvokeOperationResourceWithOptions(remote, "fixture.start", "remote-revoked-before-admission", input, options)
	require.ErrorIs(t, err, ErrRemoteControlDisabled, "事务准入时再次读取总授权")
	require.Equal(t, int32(3), calls.Load(), "被拒绝调用无设备副作用")
	s.SetRemoteControlAccess(func() error { return nil })
	remoteResult, err := s.InvokeOperationResourceWithOptions(remote, "fixture.start", "remote-accepted", input, options)
	require.NoError(t, err)
	require.Equal(t, int32(4), calls.Load())
	s.SetRemoteControlAccess(func() error { return ErrRemoteControlDisabled })
	_, err = s.InvokeOperationResourceWithOptions(remote, "fixture.start", "remote-after-disable", input, options)
	require.ErrorIs(t, err, ErrRemoteControlDisabled)
	queryTask := task
	queryTask.Scope = "controls.get"
	query := contextx.WithSubject(ctx, contextx.Subject{FleetTask: &queryTask})
	previous, err := s.OperationInvocation(query, "fixture.start", "remote-accepted")
	require.NoError(t, err, "关闭总授权后可查询先前结果")
	require.Equal(t, remoteResult["result"], previous["result"])
	_, err = s.InvokeOperationResource(ctx, "fixture.start", "start-intent", input)
	require.NoError(t, err, "本地用户不受远程总授权影响")
	require.Equal(t, int32(4), calls.Load())
	// Cloud requests report the overall result as unknown when a successful
	// device write precedes invalid output. Preserve legacy Control behavior.
	requestCode := strings.ReplaceAll(strings.ReplaceAll(code, "control:", "request:"), `code:"fixture."`, `code:"requestfixture."`)
	requestView, err := s.SaveStream(ctx, 0, 0, map[string]any{"name": "Request effects", "script": requestCode, "timeoutMs": 3000})
	require.NoError(t, err)
	_, err = s.Deploy(ctx, requestView["id"].(int64), requestView["revision"].(int64), 0, 0)
	require.NoError(t, err)
	requestCtx := programV2ServiceContext("stream.requests.invoke", "stream.requests.write")
	_, err = s.InvokeStreamRequest(requestCtx, "requestfixture.bad", "bad-request", input)
	require.ErrorIs(t, err, ErrUnknownResult)
	require.NotErrorIs(t, err, ErrControlOutputInvalid, "completed effects take precedence over output errors")
	requestInvocation, err := r.OperationInvocation(ctx, "requestfixture.bad", "bad-request", 1, 0, "")
	require.NoError(t, err)
	requestRun, err := r.Run(ctx, repo.ProgramRunIdentity(requestInvocation.MessageID, requestInvocation.StreamID, requestInvocation.VersionID, requestInvocation.EntryID))
	require.NoError(t, err)
	require.Equal(t, "unknown", requestRun.Status)
	var effect repo.StreamProgramEffect
	require.NoError(t, repo.GetUnsConn(ctx).Where("run_id = ?", requestRun.ID).Take(&effect).Error)
	require.Equal(t, "completed", effect.Status, "the successful external effect receipt is retained")
	_, err = s.InvokeStreamRequest(requestCtx, "requestfixture.bad", "bad-request", input)
	require.ErrorIs(t, err, ErrUnknownResult)
	require.Equal(t, int32(5), calls.Load(), "a failed request cannot repeat its completed device write")
	_, err = s.Disable(ctx, id, view["revision"].(int64), false)
	require.NoError(t, err)
	_, err = s.InvokeOperationResource(ctx, "fixture.start", "new-disabled", input)
	require.ErrorIs(t, err, ErrControlInactive)
	_, err = s.InvokeOperationResource(ctx, "fixture.start", "start-intent", input)
	require.NoError(t, err, "completed results remain readable after disable")
	require.Equal(t, int32(5), calls.Load())
}

func TestAppRequestGrantRechecksExternalEffectsAndRecoveryIdentity(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.source", "uns.read", "uns.write", "uns.stream.manage", "stream.requests.read", "stream.requests.invoke", "stream.requests.write")
	executor := &publisherChangeExecutor{}
	s := New(nil, executor, "")
	t.Cleanup(s.Close)
	s.SetProgramAuthorityResolver(testProgramAuthority(ctx))
	require.NoError(t, s.StartProgramExecutions(ctx))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	t.Cleanup(server.Close)
	httpTestManager(t, s.httpManager, server)
	source, err := s.CreateSource(ctx, SourceDefinition{Name: "App target", Protocol: HTTP, Endpoint: "http://fixture.internal/api", RuntimeLocation: "local", HTTP: &HTTPConnection{Authentication: "none"}}, 0)
	require.NoError(t, err)
	_, err = repo.NewSourceStreamRepo(store).ChangeSource(ctx, source["id"].(int64), source["revision"].(int64), func(_ *gorm.DB, row *repo.Source) error { row.Health = "healthy"; return nil })
	require.NoError(t, err)
	code := fmt.Sprintf(`module.exports=api=>{const source=api.sources.use({id:%q,resources:[]});api.stream.onRequest({id:"write",request:{code:"app.device",name:"Device",contractVersion:"1",inputSchema:{},outputSchema:{},sideEffect:"write",idempotency:"required",timeoutMs:3000}},async()=>{const response=await source.request({method:"POST",path:"action",json:{}});return response.data;});};`, strconv.FormatInt(source["id"].(int64), 10))
	stream, err := s.SaveStream(ctx, 0, 0, map[string]any{"name": "Delegated target", "script": map[string]any{"code": code, "timeoutMs": 3000}})
	require.NoError(t, err)
	_, err = s.Deploy(ctx, stream["id"].(int64), stream["revision"].(int64), 0, 0)
	require.NoError(t, err)
	app := contextx.Subject{UserID: 1, AuthType: "apiKey", APIKeyID: 100, APIKeyAppID: "fc9c19c3-41e7-4cb4-8334-39cd1c074a5e", APIKeyUsageType: repo.APIKeyUsageAppData, ResourceKeys: map[string]struct{}{"stream.requests.read": {}, "stream.requests.invoke": {}, "stream.requests.write": {}}}
	appCtx := contextx.WithSubject(ctx, app)
	s.SetControlAuthorizer(func(context.Context, string) error { return nil })
	s.SetRequestAuthorizer(func(context.Context, string, []string, []repo.ProgramRequestTarget) error { return nil }, func(_ context.Context, previous contextx.Subject) (contextx.Subject, error) {
		require.Equal(t, app.APIKeyID, previous.APIKeyID)
		return app, nil
	})
	_, err = s.InvokeStreamRequest(appCtx, "app.device", "missing", nil)
	require.ErrorIs(t, err, ErrForbidden)
	require.Zero(t, calls.Load())
	definition, version, policy, digest, err := s.RequestGrantPlan(ctx, "app.device")
	require.NoError(t, err)
	require.NotEmpty(t, policy.Targets)
	setActive := func(active bool) {
		t.Helper()
		require.NoError(t, store.UnsDB().Transaction(func(tx *gorm.DB) error {
			r := repo.NewSourceStreamRepo(tx)
			if err := r.LockRequestGrant(ctx, app.APIKeyAppID, "app.device"); err != nil {
				return err
			}
			old, err := r.RequestGrant(ctx, app.APIKeyAppID, "app.device")
			if err != nil {
				return err
			}
			grant := repo.RequestGrant{AppID: app.APIKeyAppID, Code: "app.device", BindingDigest: digest, StreamID: definition.Binding.StreamID, VersionID: definition.Binding.DeploymentVersion, EntryID: definition.Binding.EntryID, Active: active, Actions: []string{"stream.requests.read", "stream.requests.invoke", "stream.requests.write"}, Targets: []repo.RequestTargetGrant{}, UpdatedPrincipal: "user:1"}
			for _, target := range policy.Targets {
				grant.Targets = append(grant.Targets, repo.RequestTargetGrant{Digest: target.Digest, Action: target.Action})
			}
			_, err = r.PutRequestGrant(ctx, grant, old)
			return err
		}))
	}
	setActive(true)
	executor.before = func() { setActive(false) }
	_, err = s.InvokeStreamRequest(appCtx, "app.device", "revoke-before", nil)
	require.Error(t, err)
	require.Zero(t, calls.Load())
	executor.before = nil
	setActive(true)
	first, err := s.InvokeStreamRequest(appCtx, "app.device", "allowed", nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
	replay, err := s.InvokeStreamRequest(appCtx, "app.device", "allowed", nil)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.EqualValues(t, 1, calls.Load())
	require.False(t, contextx.HasResource(app, "uns.source"), "the grant never installs publisher Source permissions")
	checked, err := s.withRequestPublisher(appCtx, version, definition.Binding.EntryID)
	require.NoError(t, err)
	require.ErrorIs(t, s.checkAppRequestCall(checked, version, definition.Binding.EntryID, streamexec.ProgramCall{Kind: "source.request", SourceID: "999999"}), ErrForbidden)
	run, err := repo.NewSourceStreamRepo(store).Run(ctx, first.RunID)
	require.NoError(t, err)
	restored, err := s.restoreAppRequestSubject(ctx, run.Summary)
	require.NoError(t, err)
	restoredSubject, _ := contextx.SubjectFrom(restored)
	require.Equal(t, app.APIKeyAppID, restoredSubject.APIKeyAppID)
	setActive(false)
	_, err = s.withRequestPublisher(restored, version, definition.Binding.EntryID)
	require.ErrorIs(t, err, ErrForbidden, "frozen-output recovery must retain App revocation")
	_, err = s.InvokeStreamRequest(appCtx, "app.device", "allowed", nil)
	require.ErrorIs(t, err, ErrForbidden)
	setActive(true)
	executor.after = func() { setActive(false) }
	_, err = s.InvokeStreamRequest(appCtx, "app.device", "revoke-after", nil)
	require.ErrorIs(t, err, ErrUnknownResult)
	require.EqualValues(t, 2, calls.Load())
	executor.after = nil
	setActive(true)
	_, err = s.InvokeStreamRequest(appCtx, "app.device", "revoke-after", nil)
	require.ErrorIs(t, err, ErrUnknownResult)
	require.EqualValues(t, 2, calls.Load(), "uncertain effects must never be dispatched again")
}
