package sourcestream

// 独立PG、真实HTTP端验证SDK源授权、冻结版本、密钥与副作用日志，不mock网络业务。
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 真读预览须复用来源凭据和权限，同时禁止外部写入与 UNS 落库。
func TestProgramV2FetchPreviewHTTPAndPermissions(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.stream.execute", "uns.source", "uns.source.credentials", "uns.read", "uns.write")
	var reads atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "Bearer preview-test-secret", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"A","name":"Alpha"},{"id":"B","name":"Beta"}],"echo":"preview-test-secret"}`))
	}))
	defer fixture.Close()
	s := New(nil, programV2RealExecutor{}, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Cleanup(s.Close)
	httpTestManager(t, s.httpManager, fixture)
	view, err := s.CreateSource(ctx, SourceDefinition{Name: "Fetch preview", Protocol: HTTP, Endpoint: "http://fixture.internal/api", RuntimeLocation: "local", HTTP: &HTTPConnection{Authentication: "bearer"}}, 0)
	require.NoError(t, err)
	id := view["id"].(int64)
	_, err = s.SaveCredential(ctx, id, view["revision"].(int64), Credential{Kind: "bearer", Secret: "preview-test-secret"}, "")
	require.NoError(t, err)
	collection, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Preview records", RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	collectionID := collection["collection"].(repo.UnsMeta).ID
	code := fmt.Sprintf(`module.exports=api=>{
 const source=api.sources.use({id:%q,resources:[]});
 const output=api.uns.use({collectionId:%q,access:'write'});
 api.stream.onRequest({id:'read'},async()=>{
  const result=await source.request({method:'GET',path:'/customers'});
  for(const row of result.data.items) await output.write({recordId:row.id,record:row});
  return result;
 });
};`, strconv.FormatInt(id, 10), collectionID)
	doc := programV2Definition(code)
	doc.Script.TimeoutMs = 5000
	result, err := s.TestEntry(ctx, doc, map[string]any{"data": nil}, 0, "fetch", "read")
	require.NoError(t, err)
	require.Equal(t, "live-read", result["sourceInputMode"])
	require.True(t, result["dryRun"].(bool))
	require.True(t, result["externalRequests"].(bool))
	require.Len(t, result["targets"], 2)
	encoded, _ := json.Marshal(result)
	require.NotContains(t, string(encoded), "preview-test-secret")
	var count int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Count(&count).Error)
	require.Zero(t, count)
	require.EqualValues(t, 1, reads.Load())
	_, err = s.TestEntry(programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read", "uns.write"), doc, map[string]any{"data": nil}, 0, "fetch", "read")
	require.ErrorIs(t, err, ErrForbidden)
	for _, operation := range []string{"source.request({method:'POST',path:'/customers'})", "source.secrets()", "source.parameters()"} {
		blocked := programV2Definition(fmt.Sprintf(`module.exports=api=>{const source=api.sources.use({id:%q,resources:[]});api.stream.onRequest({id:'read'},async()=>await %s);};`, strconv.FormatInt(id, 10), operation))
		_, err = s.TestEntry(ctx, blocked, map[string]any{"data": nil}, 0, "fetch", "read")
		require.Error(t, err)
	}
	require.EqualValues(t, 1, reads.Load(), "denied requests must not reach the server")
	samples := map[string]any{"data": nil, "sourceSamples": map[string]any{strconv.FormatInt(id, 10): map[string]any{"request": map[string]any{"status": 200, "data": map[string]any{"items": []any{}}}}}}
	_, err = s.TestEntry(ctx, doc, samples, 0, "input", "read")
	require.NoError(t, err)
	require.EqualValues(t, 1, reads.Load(), "sample previews must remain offline")
}

// 厂商只读操作可以使用POST；权限来自后端登记，不接受脚本自称只读。
func TestProgramV2FetchPreviewVendorReadContracts(t *testing.T) {
	for _, testName := range []string{"sap-odata", "teamcenter", "teamcenter-expired"} {
		profile := strings.TrimSuffix(testName, "-expired")
		t.Run(testName, func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			ctx := programV2ServiceContext("uns.stream.manage", "uns.stream.execute", "uns.source", "uns.source.credentials", "uns.read", "uns.write")
			var reads, logins atomic.Int32
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/login") {
					logins.Add(1)
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "fixture", Path: "/", HttpOnly: true})
					_, _ = w.Write([]byte(`{"ServiceData":{"partialErrors":[]}}`))
					return
				}
				attempt := reads.Add(1)
				if profile == "teamcenter" {
					require.Equal(t, http.MethodPost, r.Method)
					require.True(t, strings.HasSuffix(r.URL.Path, "/getSavedQueries"))
					_, err := r.Cookie("session")
					require.NoError(t, err)
					if testName == "teamcenter-expired" && attempt == 1 {
						_, _ = w.Write([]byte(`{".QName":"http://teamcenter.com/Schemas/Soa/2006-03/Exceptions.InvalidUserException","code":214001}`))
						return
					}
				} else {
					require.Equal(t, http.MethodGet, r.Method)
					require.Equal(t, "/tc/Materials", r.URL.Path)
				}
				_, _ = w.Write([]byte(`{"items":[{"id":"entity-1"}],"echo":"vendor-preview-secret"}`))
			}))
			defer fixture.Close()
			s := New(nil, programV2RealExecutor{}, base64.StdEncoding.EncodeToString(make([]byte, 32)))
			t.Cleanup(s.Close)
			httpTestManager(t, s.httpManager, fixture)
			def := SourceDefinition{Name: "Vendor fetch", Protocol: HTTP, SourceType: profile, Endpoint: "http://fixture.internal/tc/", RuntimeLocation: "local", HTTP: &HTTPConnection{Authentication: "basic"}}
			view, err := s.CreateSource(ctx, def, 0)
			require.NoError(t, err)
			id := view["id"].(int64)
			_, err = s.SaveCredential(ctx, id, view["revision"].(int64), Credential{Kind: "basic", Username: "reader", Secret: "vendor-preview-secret"}, "")
			require.NoError(t, err)
			resources := vendorResources(profile)
			if profile == "sap-odata" {
				resources = append(resources, NativeResource{ID: "vendor:sap-odata:entity:Materials", Name: "Materials", Models: []AccessModel{RequestReply}})
			}
			raw, err := json.Marshal(resources)
			require.NoError(t, err)
			require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id = ?", id).Update("resources", string(raw)).Error)
			ref := "vendor:" + profile + ":queries"
			if profile == "sap-odata" {
				ref = "vendor:sap-odata:entity:Materials"
			}
			code := fmt.Sprintf(`module.exports=api=>{const source=api.sources.use({id:%q,resources:[%q]});api.stream.onRequest({id:'read'},()=>source.read(%q,{}));};`, strconv.FormatInt(id, 10), ref, ref)
			doc := programV2Definition(code)
			result, err := s.TestEntry(ctx, doc, map[string]any{"data": nil}, 0, "fetch", "read")
			require.NoError(t, err)
			require.Equal(t, "live-read", result["sourceInputMode"])
			require.True(t, result["dryRun"].(bool))
			require.False(t, result["sideEffects"].(bool))
			require.Len(t, result["targets"], 0)
			expectedReads := int32(1)
			if testName == "teamcenter-expired" {
				expectedReads = 2
				require.EqualValues(t, 2, logins.Load())
			}
			require.Equal(t, expectedReads, reads.Load())
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "vendor-preview-secret")
			beforeLogin := logins.Load()
			sample := map[string]any{"data": nil, "sourceSamples": map[string]any{strconv.FormatInt(id, 10): map[string]any{ref: map[string]any{"status": 200, "data": map[string]any{"offline": true}}}}}
			_, err = s.TestEntry(ctx, doc, sample, 0, "input", "read")
			require.NoError(t, err)
			_, err = s.TestEntry(programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read"), doc, map[string]any{"data": nil}, 0, "fetch", "read")
			require.ErrorIs(t, err, ErrForbidden)
			for _, operation := range []string{"source.request({method:'POST',path:'arbitrary'})", "source.secrets()", "source.parameters()", "source.invoke('vendor:sap-odata:update',{})"} {
				blocked := programV2Definition(fmt.Sprintf(`module.exports=api=>{const source=api.sources.use({id:%q,resources:[]});api.stream.onRequest({id:'read'},()=>%s);};`, strconv.FormatInt(id, 10), operation))
				_, err = s.TestEntry(ctx, blocked, map[string]any{"data": nil}, 0, "fetch", "read")
				require.Error(t, err)
			}
			if profile == "sap-odata" {
				// A discovered write resource and a source.read spelling do not
				// override the code-owned operation's read-only contract.
				blocked := programV2Definition(fmt.Sprintf(`module.exports=api=>{const source=api.sources.use({id:%q,resources:['vendor:sap-odata:update']});api.stream.onRequest({id:'read'},()=>source.read('vendor:sap-odata:update',{params:{entity:'Materials'},data:{id:'x'}}));};`, strconv.FormatInt(id, 10)))
				_, err = s.TestEntry(ctx, blocked, map[string]any{"data": nil}, 0, "fetch", "read")
				require.Error(t, err)
			}
			require.Equal(t, expectedReads, reads.Load(), "offline and denied calls must not reach the vendor")
			require.Equal(t, beforeLogin, logins.Load())
			var count int64
			require.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestProgramV2WriteComparisonAuthorization(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.read", "uns.write")
	collection, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Comparison", RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	id := collection["collection"].(repo.UnsMeta).ID
	_, err = unsmeta.New().SaveRecord(ctx, id, "customer", 0, map[string]any{"name": "Before", "owner": "Keep"})
	require.NoError(t, err)
	s := New(nil, programV2RealExecutor{}, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Cleanup(s.Close)
	doc := programV2Definition(fmt.Sprintf(`module.exports=api=>{const out=api.uns.use({collectionId:%q,access:'write'});api.stream.onRequest({id:'run'},async()=>{await out.write({mode:'upsert',recordId:'customer',record:{name:'After'}});});};`, id))
	payload := map[string]any{"data": nil, "previewWrites": true}
	_, err = s.TestEntry(programV2ServiceContext("uns.stream.manage", "uns.write"), doc, payload, 0, "input", "run")
	require.ErrorIs(t, err, ErrForbidden, "write permission does not expose existing records")
	view, err := s.TestEntry(ctx, doc, payload, 0, "input", "run")
	require.NoError(t, err)
	preview := view["targets"].([]map[string]any)[0]["preview"].(unsmeta.ProgramWritePreview)
	require.Equal(t, "updated", preview.Status)
	require.Len(t, preview.Changes, 1)
	require.Equal(t, "Before", preview.Changes[0].Before)
	require.Equal(t, "After", preview.Changes[0].After)
	var rows []repo.UnsRecord
	require.NoError(t, store.UnsDB().Find(&rows).Error)
	require.Len(t, rows, 1)
	require.JSONEq(t, `{"name":"Before","owner":"Keep"}`, string(rows[0].Data))
}

func TestProgramV2HTTPHostRealRequestSecretReceiptAndDryRun(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.source", "uns.source.credentials", "uns.read", "uns.stream.execute")
	var sent atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"authorizationEcho": r.Header.Get("Authorization")})
			return
		}
		sent.Add(1)
		require.Equal(t, "/api/order", r.URL.Path)
		require.Equal(t, "signed-test-value", r.Header.Get("X-Signature"))
		if r.URL.Query().Get("delay") == "1" {
			time.Sleep(3 * time.Second)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"secretEcho":"signed-test-value","order":7}`))
	}))
	defer fixture.Close()
	s := New(nil, programV2RealExecutor{}, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Cleanup(s.Close)
	httpTestManager(t, s.httpManager, fixture)
	definition := SourceDefinition{Name: "HTTP SDK source", Protocol: HTTP, Endpoint: "http://fixture.internal/api", RuntimeLocation: "local", HTTP: &HTTPConnection{Authentication: "custom", Parameters: map[string]any{"tenant": "public-test"}}}
	view, err := s.CreateSource(ctx, definition, 0)
	require.NoError(t, err)
	id := view["id"].(int64)
	view, err = s.SaveCredential(ctx, id, view["revision"].(int64), Credential{Kind: "custom", Values: map[string]string{"signingKey": "signed-test-value"}}, "")
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(store)
	_, err = r.ChangeSource(ctx, id, view["revision"].(int64), func(_ *gorm.DB, item *repo.Source) error {
		item.DesiredState = "enabled"
		item.Health = "healthy"
		return nil
	})
	require.NoError(t, err)
	source, err := r.Source(ctx, id)
	require.NoError(t, err)
	manifest := streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{{ID: strconv.FormatInt(id, 10), Resources: []string{}}}, Topics: []streamexec.ProgramTopic{}, Entries: []streamexec.ProgramEntry{{ID: "request", Kind: "request"}}}
	snapshots := []repo.StreamSourceSnapshot{{SourceID: id, Alias: strconv.FormatInt(id, 10), ConfigRevision: source.ConfigRevision, Resources: []string{}}}
	doc := programV2Raw(t, programV2Definition(`module.exports=api=>api.stream.onRequest({id:"request"},ctx=>ctx.input);`))
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "HTTP host test", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, snapshots)
	require.NoError(t, err)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	reserved, err := r.ReserveRun(ctx, repo.StreamRun{ID: "http-live", StreamID: stream.ID, VersionID: version.ID, EntryID: "request", Model: "program", Status: "processing", Summary: `{}`})
	require.NoError(t, err)
	require.True(t, reserved)
	executionCtx, guard := WithProgramSecretGuard(ctx)
	host, err := s.ProgramV2Host(executionCtx, manifest, snapshots, "http-live", "request", nil, false, nil, nil)
	require.NoError(t, err)
	call := streamexec.ProgramCall{ID: 1, Kind: "source.parameters", SourceID: strconv.FormatInt(id, 10)}
	data, err := host(executionCtx, call)
	require.NoError(t, err)
	require.JSONEq(t, `{"tenant":"public-test"}`, string(data))
	call.ID, call.Kind = 2, "source.secrets"
	data, err = host(executionCtx, call)
	require.NoError(t, err)
	require.JSONEq(t, `{"signingKey":"signed-test-value"}`, string(data))
	var effects int64
	require.NoError(t, store.UnsDB().Model(&repo.StreamProgramEffect{}).Count(&effects).Error)
	require.Zero(t, effects, "getter values never create effects")
	call.ID, call.Kind = 3, "source.request"
	call.Options = json.RawMessage(`{"method":"POST","path":"/order","headers":{"X-Signature":"signed-test-value"},"json":{"id":7}}`)
	data, err = host(executionCtx, call)
	require.NoError(t, err)
	require.Contains(t, string(data), "signed-test-value")
	require.Equal(t, int32(1), sent.Load())
	result := streamexec.ProgramResult{Data: data, Outputs: []streamexec.ProgramOutput{{TopicID: "7", Data: data}}, Logs: []streamexec.Log{{Message: "signed-test-value"}}}
	guard.RedactResult(&result)
	safe, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(safe), "signed-test-value")
	data, err = host(executionCtx, call)
	require.NoError(t, err)
	require.JSONEq(t, `{"status":201,"headers":{},"data":null,"replayed":true,"responseAvailable":false}`, string(data))
	require.Equal(t, int32(1), sent.Load(), "completed replay must not dispatch twice")
	var effect repo.StreamProgramEffect
	require.NoError(t, store.UnsDB().First(&effect).Error)
	require.Equal(t, "completed", effect.Status)
	require.NotContains(t, effect.Result, "signed-test-value")
	require.NotContains(t, effect.Result, "order")
	require.NotContains(t, effect.Result, "X-Signature")
	changed := call
	changed.Options = json.RawMessage(`{"method":"POST","path":"order","json":{"id":8}}`)
	_, err = host(executionCtx, changed)
	require.ErrorIs(t, err, repo.ErrConflict)
	require.Equal(t, int32(1), sent.Load())
	// Leave sufficient budget for real PG authorization/journaling under race;
	// the fixture remains slower than this budget to prove post-dispatch unknown.
	timeoutCtx, cancel := context.WithTimeout(executionCtx, 2*time.Second)
	call.ID = 4
	call.Options = json.RawMessage(`{"method":"POST","path":"order","query":{"delay":"1"},"headers":{"X-Signature":"signed-test-value"}}`)
	_, err = host(timeoutCtx, call)
	cancel()
	require.ErrorIs(t, err, ErrUnknownResult)
	_, err = host(executionCtx, call)
	require.ErrorIs(t, err, ErrUnknownResult)
	require.Equal(t, int32(2), sent.Load(), "unknown HTTP result must never auto retry")
	preview, err := s.ProgramV2Host(ctx, manifest, snapshots, "", "request", nil, true, map[string]map[string]json.RawMessage{strconv.FormatInt(id, 10): {
		"parameters": json.RawMessage(`{"tenant":"sample"}`), "secrets": json.RawMessage(`{"signingKey":"sample-only"}`), "request": json.RawMessage(`{"status":200,"data":{"preview":true}}`),
	}}, nil)
	require.NoError(t, err)
	for _, kind := range []string{"source.parameters", "source.secrets", "source.request"} {
		c := streamexec.ProgramCall{ID: 5, Kind: kind, SourceID: strconv.FormatInt(id, 10)}
		if kind == "source.request" {
			c.Options = json.RawMessage(`{"path":"order"}`)
		}
		data, err = preview(ctx, c)
		require.NoError(t, err)
		require.NotContains(t, string(data), "signed-test-value")
	}
	require.Equal(t, int32(2), sent.Load())
	deny := call
	deny.SourceID = "42"
	_, err = host(executionCtx, deny)
	require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	_, err = r.ChangeSource(ctx, id, source.Revision, func(_ *gorm.DB, item *repo.Source) error { item.ConfigRevision++; return nil })
	require.NoError(t, err)
	_, err = host(executionCtx, streamexec.ProgramCall{ID: 6, Kind: "source.secrets", SourceID: strconv.FormatInt(id, 10)})
	require.ErrorIs(t, err, errInactivePlan, "revoked execution authority rejects the call before reading credentials")

	// Automatic Basic auth has an encoded form that must also be registered,
	// not just the original username/password or named-secret plaintext values.
	authDef := definition
	authDef.Name = "Basic auth source"
	authDef.HTTP = &HTTPConnection{Authentication: "basic"}
	authView, err := s.CreateSource(ctx, authDef, 0)
	require.NoError(t, err)
	authID := authView["id"].(int64)
	authView, err = s.SaveCredential(ctx, authID, authView["revision"].(int64), Credential{Kind: "basic", Username: "fixture-user", Secret: "fixture-password"}, "")
	require.NoError(t, err)
	authSource, err := r.Source(ctx, authID)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(authSource.Document), &authDef))
	authCall := streamexec.ProgramCall{Kind: "source.request", SourceID: strconv.FormatInt(authID, 10), Options: json.RawMessage(`{"path":"auth"}`)}
	authResult, err := s.HTTPProgramCall(executionCtx, authSource, authDef, authCall)
	require.NoError(t, err)
	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("fixture-user:fixture-password"))
	require.Contains(t, string(authResult), expectedAuth)
	protected := streamexec.ProgramResult{Data: authResult}
	guard.RedactResult(&protected)
	require.NotContains(t, string(protected.Data), expectedAuth)
	authCall.Options = json.RawMessage(`{"path":"auth","headers":{"Authorization":"Bearer script-choice"}}`)
	authResult, err = s.HTTPProgramCall(executionCtx, authSource, authDef, authCall)
	require.NoError(t, err)
	require.Contains(t, string(authResult), "Bearer script-choice", "explicit script auth takes priority over automatic Basic auth")
}

func TestProgramV2HTTPServerRegistrationRequiresServerAndNoNATS(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read", "uns.write")
	s := New(nil, programV2RealExecutor{}, "")
	t.Cleanup(s.Close)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	t.Setenv("SOURCE_HTTP_PORT_RANGE_START", strconv.Itoa(port))
	t.Setenv("SOURCE_HTTP_PORT_RANGE_END", strconv.Itoa(port))
	t.Setenv("SOURCE_HTTP_ENABLED", "false")
	def := SourceDefinition{Name: "HTTP listener", Protocol: HTTP, Role: ServerRole, Endpoint: "http://managed", RuntimeLocation: "local", HTTP: &HTTPConnection{Port: port, Anonymous: true}}
	raw, err := json.Marshal(def)
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(store)
	source, err := r.CreateSource(ctx, repo.Source{Name: def.Name, Protocol: string(HTTP), Document: string(raw)})
	require.NoError(t, err)
	source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error {
		item.DesiredState = "enabled"
		item.Health = "healthy"
		return nil
	})
	require.NoError(t, err)
	code := fmt.Sprintf(`module.exports=api=>{const source=api.sources.use({id:%q,resources:[]});api.stream.onRequest({id:"webhook",source,method:"POST",path:"/orders/:id"},ctx=>ctx.request);};`, strconv.FormatInt(source.ID, 10))
	streamDef := programV2Definition(code)
	_, _, err = s.compileProgramV2(ctx, streamDef, true)
	require.ErrorContains(t, err, "notConfigured")
	_, _, err = s.compileProgramV2(ctx, streamDef, false)
	require.NoError(t, err)
	t.Setenv("SOURCE_HTTP_ENABLED", "true")
	manifest, snapshots, err := s.compileProgramV2(ctx, streamDef, true)
	require.NoError(t, err)
	require.Equal(t, "http-request", manifest.Entries[0].Kind)
	saved, err := r.SaveProgramV2(ctx, repo.Stream{Name: streamDef.Name, Model: "program", Document: programV2Raw(t, streamDef)}, 0, manifest)
	require.NoError(t, err)
	view, err := s.deployProgramV2(ctx, saved, saved.Revision, 0, streamDef, programV2Raw(t, streamDef))
	require.NoError(t, err)
	require.Equal(t, "active", view["deploymentState"], "HTTP handler requires no NATS readiness")
	require.Equal(t, "listening", s.httpServers.View(source.ID, port)["state"], "deployment reconciles the listener immediately")
	require.Len(t, snapshots, 1)
	def.Role = ClientRole
	def.Endpoint = "http://fixture.internal"
	def.HTTP = &HTTPConnection{Authentication: "none"}
	raw, err = json.Marshal(def)
	require.NoError(t, err)
	_, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error {
		item.Document = string(raw)
		item.ConfigRevision++
		return nil
	})
	require.NoError(t, err)
	_, _, err = s.compileProgramV2(ctx, streamDef, false)
	require.ErrorContains(t, err, "httpServerRequired")
	_, _, err = s.compileProgramV2(ctx, programV2Definition(strings.ReplaceAll(code, `resources:[]`, `resources:["http:fake"]`)), false)
	require.Error(t, err)
}
