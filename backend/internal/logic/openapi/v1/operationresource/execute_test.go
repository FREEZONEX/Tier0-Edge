package operationresource_test

// 独立 PG、真实 API Key 中间件、生成 handler、Node SDK 与正式执行器子进程。
// 无 NATS、无设备控制；Source HTTP 的副作用与重放另由领域测试覆盖。
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/auth"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
	coreRequests "backend/internal/handler/core/streamrequests"
	handlers "backend/internal/handler/openapi/v1/operationresource"
	requestHandlers "backend/internal/handler/openapi/v2/streamrequests"
	unshandlers "backend/internal/handler/openapi/v2/unsv2"
	"backend/internal/infra/streamexecutor"
	"backend/internal/repo"
	"backend/internal/svc/svctest"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"golang.org/x/crypto/bcrypt"
)

func TestOperationResourceAuthenticatedHTTPAndAppClient(t *testing.T) {
	t.Setenv("TIER0_RUNTIME_MODE", "native")
	service := svctest.NewTestServiceContext(t)
	backend, err := filepath.Abs("../../../../..")
	require.NoError(t, err)
	// Keep the IPC path below Windows' Unix socket limit and use an executable
	// extension; the real executor and API Key middleware remain unchanged.
	dir, err := os.MkdirTemp("", "t0-op-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	binary, socket := filepath.Join(dir, "executor"), filepath.Join(dir, "executor.sock")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, "./cmd/stream-executor")
	build.Dir = backend
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	ctx, cancel := context.WithCancel(context.Background())
	process := exec.CommandContext(ctx, binary, "-socket", socket)
	require.NoError(t, process.Start())
	t.Cleanup(func() { cancel(); _ = process.Wait() })
	client := streamexecutor.New(socket)
	require.Eventually(t, func() bool { return client.Ready(ctx) == nil }, 5*time.Second, 25*time.Millisecond)
	s := domain.New(nil, client, "")
	s.SetProgramAuthorityResolver(func(ctx context.Context, ref repo.ProgramAuthorityRef) (contextx.Subject, error) {
		subject, err := service.App.Auth.ResolveProgramAuthority(ctx, ref)
		if auth.IsUnauthorized(err) {
			return contextx.Subject{}, domain.ErrForbidden
		}
		return subject, err
	})
	t.Cleanup(s.Close)
	require.NoError(t, s.StartProgramExecutions(ctx))
	service.App.SourceStream = s
	service.App.UnsMeta = unsmeta.New()
	service.App.UnsV2 = unsv2.New(service.App.UnsMeta, service.App.Auth.RefreshSubject)
	service.App.UnsV2.SetSourceRuntime(s)
	admin := contextx.WithSubject(ctx, contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: map[string]struct{}{"uns.stream.manage": {}}})
	view, err := s.SaveStream(admin, 0, 0, map[string]any{"name": "SDK operations", "timeoutMs": 3000,
		"script": `module.exports=api=>{for(const name of ["start","status","stop"]){api.stream.onRequest({id:name,control:{
		 code:"test."+name,name,contractVersion:"1",inputSchema:{type:"object",required:["deviceId"],properties:{deviceId:{type:"string"}}},
		 outputSchema:{type:"object",required:["operation","deviceId"],properties:{operation:{type:"string"},deviceId:{type:"string"}}},
		 sideEffect:name==="status"?"none":"write",idempotency:"required",timeoutMs:3000}},ctx=>({operation:name,deviceId:ctx.input.deviceId}));}};`})
	require.NoError(t, err)
	_, err = s.Deploy(admin, view["id"].(int64), view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	requestView, err := s.SaveStream(admin, 0, 0, map[string]any{"name": "Cloud requests", "timeoutMs": 3000,
		"script": `module.exports=api=>{
		 for(const name of ["lookup","command","null","bad","timeout"]){api.stream.onRequest({id:name,request:{
		 code:"request."+name,name,contractVersion:"1",inputSchema:{},outputSchema:name==="bad"?{type:"number"}:{},
		 sideEffect:name==="command"?"write":"read",idempotency:name==="command"?"required":name==="null"?"none":"optional",timeoutMs:name==="timeout"?100:3000}},ctx=>{if(name==="timeout"){while(true){}}return name==="null"?null:name==="bad"?"wrong":ctx.input;});}
		};`})
	require.NoError(t, err)
	requestView, err = s.Deploy(admin, requestView["id"].(int64), requestView["revision"].(int64), 0, 0)
	require.NoError(t, err)
	requestVersion, err := repo.NewSourceStreamRepo(ctx).Version(ctx, requestView["id"].(int64), requestView["activeVersion"].(int64))
	require.NoError(t, err)
	publication, err := requestVersion.ProgramExecutionPlan()
	require.NoError(t, err, "HTTP and SDK invocations must exercise the new frozen publication path")
	require.Equal(t, repo.ProgramAuthorityRef{Kind: "user", ID: 1}, publication.Authority)
	keys := repo.NewAPIKeyRepo(ctx)
	requestScopes := []string{"stream.requests.read", "stream.requests.invoke", "stream.requests.write"}
	_, requestKey, err := keys.CreateAPIKey(ctx, "request-fixture", 1, 1, "full_access", "external", "personal", requestScopes)
	require.NoError(t, err)
	_, invokeKey, err := keys.CreateAPIKey(ctx, "request-invoke-fixture", 1, 1, "full_access", "external", "personal", []string{"stream.requests.invoke"})
	require.NoError(t, err)
	_, manageKey, err := keys.CreateAPIKey(ctx, "request-manage-fixture", 1, 1, "full_access", "external", "personal", []string{"stream.requests.manage"})
	require.NoError(t, err)
	_, key, err := keys.CreateAPIKey(ctx, "operation-service-fixture", 1, 1, "full_access", "external", "personal", []string{"control.read", "control.execute", "control.write"})
	require.NoError(t, err)
	_, readKey, err := keys.CreateAPIKey(ctx, "read-fixture", 1, 1, "read_only", "external", "personal", repo.APIKeyResourceKeysForPermission("read_only"))
	require.NoError(t, err)
	_, executeOnly, err := keys.CreateAPIKey(ctx, "execute-no-write-fixture", 1, 1, "full_access", "external", "personal", []string{"control.read", "control.execute"})
	require.NoError(t, err)
	mux := http.NewServeMux()
	bind := func(path string, handler http.HandlerFunc) {
		mux.HandleFunc("POST "+path, service.ApiKeyAuth(func(w http.ResponseWriter, r *http.Request) {
			handler(w, pathvar.WithVars(r, map[string]string{"controlCode": r.PathValue("controlCode")}))
		}))
	}
	bind("/openapi/v1/controls/discover", handlers.OperationResourceListHandler(service))
	bind("/openapi/v1/controls/{controlCode}/describe", handlers.OperationResourceDescribeHandler(service))
	bind("/openapi/v1/controls/{controlCode}/invoke", handlers.OperationResourceInvokeHandler(service))
	for suffix, handler := range map[string]http.HandlerFunc{
		"discover":               requestHandlers.StreamRequestDiscoverHandler(service),
		"{requestCode}/describe": requestHandlers.StreamRequestDescribeHandler(service),
		"{requestCode}/invoke":   requestHandlers.StreamRequestInvokeHandler(service),
	} {
		mux.HandleFunc("POST /openapi/v2/stream-requests/"+suffix, service.ApiKeyAuth(func(w http.ResponseWriter, r *http.Request) {
			handler(w, pathvar.WithVars(r, map[string]string{"requestCode": r.PathValue("requestCode")}))
		}))
	}
	for suffix, handler := range map[string]http.HandlerFunc{
		"discover":               coreRequests.StreamRequestDiscoverHandler(service),
		"{requestCode}/describe": coreRequests.StreamRequestDescribeHandler(service),
		"{requestCode}/invoke":   coreRequests.StreamRequestInvokeHandler(service),
	} {
		mux.HandleFunc("POST /api/core/v2/stream-requests/"+suffix, service.SessionAuth(service.Permission(func(w http.ResponseWriter, r *http.Request) {
			handler(w, pathvar.WithVars(r, map[string]string{"requestCode": r.PathValue("requestCode")}))
		})))
	}
	for path, handler := range map[string]http.HandlerFunc{
		"apps/context":                     unshandlers.UnsV2AppContextHandler(service),
		"apps/create":                      unshandlers.UnsV2AppCreateHandler(service),
		"apps/get":                         unshandlers.UnsV2AppGetHandler(service),
		"apps/update":                      unshandlers.UnsV2AppUpdateHandler(service),
		"apps/platform-permissions/update": unshandlers.UnsV2AppPlatformPermissionsUpdateHandler(service),
		"apps/keys/create":                 unshandlers.UnsV2AppKeyCreateHandler(service),
		"apps/keys/revoke":                 unshandlers.UnsV2AppKeyRevokeHandler(service),
	} {
		mux.HandleFunc("POST /openapi/v2/uns/"+path, service.ApiKeyAuth(handler))
	}
	mux.HandleFunc("POST /openapi/v1/uns/write", service.ApiKeyAuth(func(http.ResponseWriter, *http.Request) {
		t.Error("App Key reached a legacy UNS handler")
	}))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	flatPost := func(rawKey, token, path, body string, status int, code string) map[string]any {
		t.Helper()
		r, err := http.NewRequest("POST", server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", rawKey)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := server.Client().Do(r)
		require.NoError(t, err)
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.Equal(t, status, response.StatusCode, string(raw))
		var value map[string]any
		require.NoError(t, json.Unmarshal(raw, &value), string(raw))
		if code != "" {
			require.Equal(t, code, value["errorCode"])
			require.Len(t, value, 2)
		}
		require.NotContains(t, value, "data")
		require.NotContains(t, value, "binding")
		require.NotContains(t, value, "execution")
		return value
	}
	const requestBase = "/openapi/v2/stream-requests/"
	flatPost("", "", requestBase+"discover", `{}`, 401, "INVALID_CREDENTIAL")
	flatPost(key, "", requestBase+"discover", `{}`, 403, "STREAM_REQUEST_FORBIDDEN")
	flatPost(invokeKey, "", requestBase+"discover", `{}`, 403, "STREAM_REQUEST_FORBIDDEN")
	flatPost(manageKey, "", requestBase+"discover", `{}`, 200, "")
	flatPost(manageKey, "", requestBase+"request.lookup/invoke", `{"input":null}`, 403, "STREAM_REQUEST_FORBIDDEN")
	flatPost(invokeKey, "", requestBase+"request.command/invoke", `{"input":null,"idempotencyKey":"no-write"}`, 403, "STREAM_REQUEST_FORBIDDEN")
	flatPost(requestKey, "", requestBase+"request.command/invoke", `{"input":null}`, 400, "STREAM_REQUEST_INVALID_INPUT")
	flatPost(requestKey, "", requestBase+"test.start/describe", `{}`, 404, "STREAM_REQUEST_NOT_FOUND")
	flatPost(requestKey, "", requestBase+"test.start/invoke", `{"input":{},"idempotencyKey":"legacy-hidden"}`, 404, "STREAM_REQUEST_NOT_FOUND")
	flatPost(requestKey, "", requestBase+"request.bad/invoke", `{"input":null}`, 422, "STREAM_REQUEST_OUTPUT_INVALID")
	flatPost(requestKey, "", requestBase+"request.timeout/invoke", `{"input":null,"idempotencyKey":"timed-out"}`, 504, "STREAM_REQUEST_RESULT_UNKNOWN")
	flatPost(requestKey, "", requestBase+"request.timeout/invoke", `{"input":null,"idempotencyKey":"timed-out"}`, 504, "STREAM_REQUEST_RESULT_UNKNOWN")
	// The timed-out child may still be relinquishing the single execution slot.
	// /readyz only checks process health. Wait with a side-effect-free syntax
	// check before unrelated success cases; never retry the timed-out request.
	require.Eventually(t, func() bool {
		return client.Check(ctx, domain.ScriptDefinition{Code: "return {};", TimeoutMs: 1000}) == nil
	}, 5*time.Second, 25*time.Millisecond)
	flatPost(requestKey, "", requestBase+"request.lookup/invoke", `{"input":null,"requestCode":"other"}`, 400, "STREAM_REQUEST_INVALID_INPUT")
	flatPost(requestKey, "", requestBase+"discover?size=5", `{}`, 400, "STREAM_REQUEST_INVALID_INPUT")
	page := flatPost(requestKey, "", requestBase+"discover", `{}`, 200, "")
	require.Equal(t, float64(5), page["total"])
	firstRead := flatPost(invokeKey, "", requestBase+"request.lookup/invoke", `{"input":{"a":1}}`, 200, "")
	nextRead := flatPost(invokeKey, "", requestBase+"request.lookup/invoke", `{"input":{"a":1}}`, 200, "")
	require.NotEqual(t, firstRead["runID"], nextRead["runID"])
	firstWrite := flatPost(requestKey, "", requestBase+"request.command/invoke", `{"input":null,"idempotencyKey":"write"}`, 200, "")
	replay := flatPost(requestKey, "", requestBase+"request.command/invoke", `{"input":null,"idempotencyKey":"write"}`, 200, "")
	require.Equal(t, firstWrite["runID"], replay["runID"])
	require.Equal(t, true, replay["replayed"])
	require.Contains(t, replay, "result")
	require.Nil(t, replay["result"])
	flatPost(requestKey, "", requestBase+"request.command/invoke", `{"input":1,"idempotencyKey":"write"}`, 409, "IDEMPOTENCY_CONFLICT")
	flatPost("", "", "/api/core/v2/stream-requests/discover", `{}`, 401, "INVALID_CREDENTIAL")
	login, err := service.App.Auth.Login(ctx, "tier0", "tier0")
	require.NoError(t, err)
	flatPost("", login.Token, "/api/core/v2/stream-requests/discover", `{}`, 200, "")
	flatPost("", login.Token, "/api/core/v2/stream-requests/request.lookup/describe", `{}`, 200, "")
	flatPost("", login.Token, "/api/core/v2/stream-requests/request.lookup/invoke", `{"input":null}`, 200, "")
	operatorHash, err := bcrypt.GenerateFromPassword([]byte("fixture-only"), bcrypt.MinCost)
	require.NoError(t, err)
	operator, err := repo.NewIAMRepo(ctx).CreateUser(ctx, repo.UserCreate{UserName: "request-operator", Password: string(operatorHash), RoleID: repo.SystemRoleOperatorID, Status: 1})
	require.NoError(t, err)
	operatorLogin, err := service.App.Auth.Login(ctx, "request-operator", "fixture-only")
	require.NoError(t, err)
	flatPost("", operatorLogin.Token, "/api/core/v2/stream-requests/discover", `{}`, 403, "STREAM_REQUEST_FORBIDDEN")
	flatPost("", operatorLogin.Token, "/api/core/v2/stream-requests/request.lookup/invoke", `{"input":null}`, 403, "STREAM_REQUEST_FORBIDDEN")
	_, operatorKey, err := keys.CreateAPIKey(ctx, "role-limited-request", operator["userId"].(int64), 1, "full_access", "external", "personal", requestScopes)
	require.NoError(t, err)
	flatPost(operatorKey, "", requestBase+"discover", `{}`, 403, "STREAM_REQUEST_FORBIDDEN")
	flatPost(operatorKey, "", requestBase+"request.lookup/invoke", `{"input":null}`, 403, "STREAM_REQUEST_FORBIDDEN")
	for _, tc := range []struct {
		key, id, media, body, intent string
		status                       int
	}{
		{"", "test.start", "application/json", `{}`, "missing-key", 401},
		{readKey, "test.start", "application/json", `{}`, "read-denied", 403},
		{executeOnly, "test.start", "application/json", `{"deviceId":"a"}`, "write-denied", 403},
		{key, "test.start", "application/json", `{"deviceId":2}`, "invalid-input", 400},
		{key, "test.start", "text/plain", `{"deviceId":"a"}`, "invalid-media", 400},
		{key, "test.start", "application/json", `{"deviceId":"a"}`, "", 400},
		{key, "missing", "application/json", `{}`, "unknown", 404},
	} {
		request, err := http.NewRequest("POST", server.URL+"/openapi/v1/controls/"+tc.id+"/invoke", strings.NewReader(tc.body))
		require.NoError(t, err)
		request.Header.Set("Content-Type", tc.media)
		request.Header.Set("X-API-Key", tc.key)
		request.Header.Set("Idempotency-Key", tc.intent)
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Equal(t, tc.status, response.StatusCode, tc.intent)
	}
	// 真正通过正式 App 配置及 Key 签发 API，不能用普通服务端 Key 冒充 App。
	post := func(rawKey, path string, body any, status int, intent string) map[string]any {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		request, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(raw))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-API-Key", rawKey)
		if intent != "" {
			request.Header.Set("Idempotency-Key", intent)
		}
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		defer response.Body.Close()
		responseBody, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.Equal(t, status, response.StatusCode, "%s: %s", path, responseBody)
		if status != http.StatusOK {
			_, _ = io.Copy(io.Discard, response.Body)
			return nil
		}
		var envelope struct {
			Code int            `json:"code"`
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(responseBody, &envelope))
		require.Equal(t, 200, envelope.Code, path)
		return envelope.Data
	}
	_, issuer, err := keys.CreateAPIKey(ctx, "App provisioner", 1, 1, "full_access", "external", "personal",
		[]string{"uns.read", "uns.app.read", "uns.app.manage", "uns.app.credentials", "control.read", "control.execute", "control.write", "stream.requests.read", "stream.requests.invoke", "stream.requests.write"})
	require.NoError(t, err)
	const appID = "device-control-app"
	permissions := append([]string{"control.read", "control.execute", "control.write"}, requestScopes...)
	post(issuer, "/openapi/v2/uns/apps/create", map[string]any{"dryRun": false, "mode": "sync", "idempotencyKey": "create-app",
		"items": []any{map[string]any{"id": appID, "name": "Device Control", "status": "active", "platformPermissions": permissions}}}, 200, "")
	issued := post(issuer, "/openapi/v2/uns/apps/keys/create", map[string]any{"appId": appID, "name": "App runtime"}, 200, "")
	appKey := issued["apiKey"].(string)
	stored, err := keys.ValidateAPIKeyDetail(ctx, appKey)
	require.NoError(t, err)
	require.Equal(t, repo.APIKeyUsageAppData, stored.UsageType)
	require.Equal(t, appID, stored.AppID)
	require.NotContains(t, stored.ResourceKeys, "control.manage")
	require.NotContains(t, stored.ResourceKeys, "stream.requests.manage")
	commandCtx, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	script := filepath.Join(filepath.Dir(backend), "packages", "app-data-client", "operation-integration.mjs")
	command := exec.CommandContext(commandCtx, "node", script)
	command.Env = append(os.Environ(), "OPERATION_TEST_API_BASE="+server.URL+"/openapi/v2", "OPERATION_TEST_API_KEY="+appKey)
	output, err = command.CombinedOutput()
	require.NoError(t, err, string(output))
	t.Log(strings.TrimSpace(string(output)))
	mutateApp := func(path, intent string, fields map[string]any) {
		t.Helper()
		current := post(issuer, "/openapi/v2/uns/apps/get", map[string]any{"id": appID}, 200, "")
		fields["id"], fields["expectedRevision"] = appID, current["revision"]
		post(issuer, "/openapi/v2/uns/"+path, map[string]any{"dryRun": false, "mode": "sync", "idempotencyKey": intent, "items": []any{fields}}, 200, "")
	}
	invoke := func(rawKey, operation, intent string, status int) {
		t.Helper()
		post(rawKey, "/openapi/v1/controls/test."+operation+"/invoke", map[string]any{"deviceId": "fixture-1"}, status, intent)
	}
	mutateApp("apps/platform-permissions/update", "remove-write", map[string]any{"platformPermissions": []string{"control.read", "control.execute"}})
	flatPost(appKey, "", requestBase+"discover", `{}`, 403, "STREAM_REQUEST_FORBIDDEN")
	flatPost(appKey, "", requestBase+"request.lookup/invoke", `{"input":null}`, 403, "STREAM_REQUEST_FORBIDDEN")
	invoke(appKey, "start", "removed-write", 403)
	invoke(appKey, "status", "read-only-status", 200)
	limited := post(issuer, "/openapi/v2/uns/apps/keys/create", map[string]any{"appId": appID, "name": "Limited App runtime"}, 200, "")
	mutateApp("apps/platform-permissions/update", "restore-write", map[string]any{"platformPermissions": permissions})
	flatPost(limited["apiKey"].(string), "", requestBase+"request.lookup/invoke", `{"input":null}`, 403, "STREAM_REQUEST_FORBIDDEN")
	flatPost(appKey, "", requestBase+"request.lookup/invoke", `{"input":null}`, 200, "")
	invoke(limited["apiKey"].(string), "start", "old-key-cannot-expand", 403)
	invoke(appKey, "start", "restored-write", 200)
	post(appKey, "/openapi/v1/uns/write", map[string]any{}, 403, "")
	mutateApp("apps/update", "disable-app", map[string]any{"status": "disabled"})
	flatPost(appKey, "", requestBase+"discover", `{}`, 403, "STREAM_REQUEST_FORBIDDEN")
	flatPost(appKey, "", requestBase+"request.lookup/invoke", `{"input":null}`, 403, "STREAM_REQUEST_FORBIDDEN")
	post(appKey, "/openapi/v1/controls/discover", map[string]any{}, 403, "")
	post(appKey, "/openapi/v1/controls/test.start/describe", map[string]any{}, 403, "")
	invoke(appKey, "status", "disabled-app", 403)
	mutateApp("apps/update", "enable-app", map[string]any{"status": "active"})
	invoke(appKey, "status", "reenabled-app", 200)
	post(issuer, "/openapi/v2/uns/apps/keys/revoke", map[string]any{"appId": appID, "keyId": issued["keyId"]}, 200, "")
	invoke(appKey, "status", "revoked-key", 401)
	flatPost(appKey, "", requestBase+"discover", `{}`, 401, "INVALID_CREDENTIAL")
	flatPost(appKey, "", requestBase+"request.lookup/invoke", `{"input":null}`, 401, "INVALID_CREDENTIAL")
	// The caller remains authenticated while a different publication Key is
	// revoked. Both fresh execution and the frozen successful receipt must deny.
	publisherKey, publisherRaw, err := keys.CreateAPIKey(ctx, "publication-only", 1, 1, "full_access", "external", "personal", []string{"uns.stream.manage"})
	require.NoError(t, err)
	publisher, err := service.App.Auth.AuthenticateAPIKey(ctx, publisherRaw)
	require.NoError(t, err)
	publishCtx := contextx.WithSubject(ctx, publisher)
	published, err := s.SaveStream(publishCtx, 0, 0, map[string]any{"name": "Publication identity", "timeoutMs": 3000,
		"script": `module.exports=api=>api.stream.onRequest({id:"identity",request:{code:"publisher.identity",name:"Identity",contractVersion:"1",inputSchema:{},outputSchema:{},sideEffect:"read",idempotency:"optional",timeoutMs:3000}},ctx=>ctx.input);`})
	require.NoError(t, err)
	_, err = s.Deploy(publishCtx, published["id"].(int64), published["revision"].(int64), 0, 0)
	require.NoError(t, err)
	beforeRevocation := flatPost(requestKey, "", requestBase+"publisher.identity/invoke", `{"input":7,"idempotencyKey":"publisher-receipt"}`, 200, "")
	require.NoError(t, service.App.Store.CommonDB().Model(&repo.APIKey{}).Where("id=?", publisherKey.ID).Update("status", 0).Error)
	flatPost(requestKey, "", requestBase+"publisher.identity/invoke", `{"input":7,"idempotencyKey":"publisher-receipt"}`, 403, "STREAM_REQUEST_FORBIDDEN")
	flatPost(requestKey, "", requestBase+"publisher.identity/invoke", `{"input":7,"idempotencyKey":"publisher-new"}`, 403, "STREAM_REQUEST_FORBIDDEN")
	require.NoError(t, service.App.Store.CommonDB().Model(&repo.APIKey{}).Where("id=?", publisherKey.ID).Update("status", 1).Error)
	afterRestoration := flatPost(requestKey, "", requestBase+"publisher.identity/invoke", `{"input":7,"idempotencyKey":"publisher-receipt"}`, 200, "")
	require.Equal(t, beforeRevocation["runID"], afterRestoration["runID"])
	require.Equal(t, true, afterRestoration["replayed"])
	publishedRow, err := repo.NewSourceStreamRepo(ctx).Stream(ctx, published["id"].(int64))
	require.NoError(t, err)
	_, err = s.Disable(admin, publishedRow.ID, publishedRow.Revision, false)
	require.NoError(t, err)
	// Failure before dispatch is unavailable, not an invalid script result or
	// an ambiguous IPC failure. Existing successful receipts need no executor.
	require.NoError(t, process.Process.Kill())
	_ = process.Wait()
	require.Error(t, client.Ready(ctx))
	flatPost(requestKey, "", requestBase+"request.command/invoke", `{"input":null,"idempotencyKey":"executor-unavailable"}`, 503, "STREAM_REQUEST_UNAVAILABLE")
	requestStream, err := repo.NewSourceStreamRepo(ctx).Stream(ctx, requestView["id"].(int64))
	require.NoError(t, err)
	_, err = s.Disable(admin, requestStream.ID, requestStream.Revision, false)
	require.NoError(t, err)
	page = flatPost(requestKey, "", requestBase+"discover", `{}`, 200, "")
	require.Equal(t, float64(0), page["total"])
	flatPost(requestKey, "", requestBase+"request.lookup/describe", `{}`, 404, "STREAM_REQUEST_NOT_FOUND")
	flatPost(requestKey, "", requestBase+"request.lookup/invoke", `{"input":null}`, 409, "STREAM_REQUEST_INACTIVE")
	replay = flatPost(requestKey, "", requestBase+"request.command/invoke", `{"input":null,"idempotencyKey":"write"}`, 200, "")
	require.Equal(t, firstWrite["runID"], replay["runID"])
	require.Equal(t, true, replay["replayed"])
}
