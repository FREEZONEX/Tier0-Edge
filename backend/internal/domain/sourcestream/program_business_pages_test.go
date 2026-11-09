package sourcestream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

// Discover through real vendor HTTP, publish the public catalog ref, then run
// the actual Goja program against the frozen host and source-owned credentials.
func TestProgramBusinessCatalogPublishedRead(t *testing.T) {
	for _, vendor := range []string{"salesforce", "teamcenter"} {
		t.Run(vendor, func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			ctx := programV2ServiceContext("uns.stream.manage", "uns.stream.execute", "uns.source", "uns.source.credentials", "uns.read")
			var queries atomic.Int32
			fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case request.URL.Path == "/token":
					require.NoError(t, request.ParseForm())
					require.Equal(t, "client_credentials", request.Form.Get("grant_type"))
					require.Equal(t, "reader-secret", request.Form.Get("client_secret"))
					fmt.Fprint(w, `{"access_token":"reader-token","token_type":"Bearer","expires_in":3600}`)
				case request.URL.Path == "/services/data/v67.0/sobjects":
					require.Equal(t, "Bearer reader-token", request.Header.Get("Authorization"))
					fmt.Fprint(w, `{"sobjects":[{"name":"Quote","label":"Quotes"}]}`)
				case request.URL.Path == "/services/data/v67.0/sobjects/Quote/describe":
					require.Equal(t, "Bearer reader-token", request.Header.Get("Authorization"))
					fmt.Fprint(w, `{"fields":[{"name":"Name"},{"name":"SystemModstamp","filterable":true}]}`)
				case request.URL.Path == "/services/data/v67.0/query":
					require.Equal(t, "Bearer reader-token", request.Header.Get("Authorization"))
					require.Equal(t, "SELECT Id,Name,SystemModstamp FROM Quote ORDER BY SystemModstamp,Id LIMIT 2", request.URL.Query().Get("q"))
					queries.Add(1)
					fmt.Fprint(w, `{"done":true,"records":[{"Id":"quote-1","Name":"fixture","SystemModstamp":"2026-11-08T00:00:00Z"}]}`)
				case strings.HasSuffix(request.URL.Path, "/login"):
					http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "fixture-session", Path: "/"})
					fmt.Fprint(w, `{"ServiceData":{"partialErrors":[]}}`)
				case strings.HasSuffix(request.URL.Path, "/getSavedQueries"):
					fmt.Fprint(w, `{"queries":[{"query":"approved-query","name":"Approved query"}]}`)
				case strings.HasSuffix(request.URL.Path, "/executeSavedQueries"):
					var body struct {
						Body struct {
							Queries []struct {
								Query          string
								Entries        []string
								Values         []string
								MaxNumToReturn int
							}
						}
					}
					require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
					require.Len(t, body.Body.Queries, 1)
					require.Equal(t, "approved-query", body.Body.Queries[0].Query)
					require.Equal(t, []string{"Item ID"}, body.Body.Queries[0].Entries)
					require.Equal(t, []string{"PART-1"}, body.Body.Queries[0].Values)
					require.Equal(t, 2, body.Body.Queries[0].MaxNumToReturn)
					queries.Add(1)
					fmt.Fprint(w, `{"arrayOfResults":[{"objectUIDS":["item-1"]}]}`)
				case strings.HasSuffix(request.URL.Path, "/loadObjects"):
					fmt.Fprint(w, `{"modelObjects":{"item-1":{"uid":"item-1","type":"Item","props":{"item_id":{"dbValues":["PART-1"]}}}}}`)
				default:
					t.Errorf("unexpected vendor request: %s %s", request.Method, request.URL.Path)
					http.Error(w, "unexpected request", http.StatusBadRequest)
				}
			}))
			defer fixture.Close()
			s := New(nil, programV2RealExecutor{}, base64.StdEncoding.EncodeToString(make([]byte, 32)))
			httpTestManager(t, s.httpManager, fixture)
			s.httpClient = fixture.Client()
			def := SourceDefinition{Name: vendor, Protocol: HTTP, SourceType: vendor, RuntimeLocation: "local", Endpoint: "https://example.com", HTTP: &HTTPConnection{Authentication: "none", CACertificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fixture.Certificate().Raw}))}}
			ref, options, identity := tcQueryPrefix+"approved-query", `{"limit":1,"itemId":"PART-1"}`, "item-1"
			if vendor == "salesforce" {
				def.Protocol = OAuth
				def.HTTP.Authentication = ""
				def.HTTP.OAuth = &OAuthConfig{GrantType: "client_credentials", TokenURL: fixture.URL + "/token", ClientID: "client", ClientAuthentication: "client_secret_post"}
				ref, options, identity = crmObjectPrefix+"Quote", `{"limit":1}`, "quote-1"
			}
			view, err := s.CreateSource(ctx, def, 0)
			require.NoError(t, err)
			id := view["id"].(int64)
			if vendor == "salesforce" {
				view, err = s.SaveCredential(ctx, id, view["revision"].(int64), Credential{Kind: "oauth2", Secret: "reader-secret", OAuth: &OAuthCredential{}}, "")
				require.NoError(t, err)
			}
			_, err = s.Discover(ctx, id, view["revision"].(int64), "")
			require.NoError(t, err)
			r := repo.NewSourceStreamRepo(store)
			source, err := r.Source(ctx, id)
			require.NoError(t, err)
			catalog, err := s.EdgeResourceCatalog(ctx, id, source.ConfigRevision)
			require.NoError(t, err)
			publicRefs := []string{}
			for _, item := range catalog.(map[string]any)["resources"].([]edgeResource) {
				publicRefs = append(publicRefs, item.ResourceRef)
			}
			require.Contains(t, publicRefs, ref)
			program := func(selected string) StreamDefinition {
				return programV2Definition(fmt.Sprintf(`module.exports=api=>{const source=api.sources.use({id:"%d",resources:[%q]});api.stream.onRequest({id:"read"},()=>source.read(%q,%s));};`, id, selected, selected, options))
			}
			_, _, err = s.compileProgramV2(ctx, program(ref+"-undeclared"), false)
			require.ErrorContains(t, err, "missingOrUnsupported")
			definition := program(ref)
			sampleInput := map[string]any{"data": nil, "sourceSamples": map[string]any{fmt.Sprint(id): map[string]any{ref: map[string]any{"offline": true}}}}
			preview, err := s.TestEntry(ctx, definition, sampleInput, 0, "input", "read")
			require.NoError(t, err)
			require.Equal(t, "provided-samples", preview["sourceInputMode"])
			require.JSONEq(t, `{"offline":true}`, string(preview["result"].(streamexec.ProgramResult).Data))
			require.Zero(t, queries.Load(), "ordinary previews must not contact the vendor")
			preview, err = s.TestEntry(ctx, definition, sampleInput, 0, "fetch", "read")
			require.NoError(t, err)
			require.Equal(t, "live-read", preview["sourceInputMode"])
			require.True(t, preview["dryRun"].(bool))
			require.False(t, preview["sideEffects"].(bool))
			require.Empty(t, preview["targets"])
			require.Contains(t, string(preview["result"].(streamexec.ProgramResult).Data), identity)
			require.Equal(t, int32(1), queries.Load(), "fetch previews must read the vendor instead of provided samples")
			for _, denied := range []context.Context{
				programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read"),
				programV2ServiceContext("uns.stream.manage", "uns.stream.execute", "uns.source"),
			} {
				_, err = s.TestEntry(denied, definition, sampleInput, 0, "fetch", "read")
				require.ErrorIs(t, err, ErrForbidden)
			}
			writePreview := definition
			writePreview.Script.Code = strings.Replace(definition.Script.Code, "source.read(", "source.write(", 1)
			_, err = s.TestEntry(ctx, writePreview, sampleInput, 0, "fetch", "read")
			require.Error(t, err, "live-fetch preview does not permit writes")
			require.Equal(t, int32(1), queries.Load())
			raw := programV2Raw(t, definition)
			view, err = s.saveProgramV2(ctx, 0, 0, definition, raw)
			require.NoError(t, err)
			stream, err := r.Stream(ctx, view["id"].(int64))
			require.NoError(t, err)
			view, err = s.deployProgramV2(ctx, stream, stream.Revision, 0, definition, raw)
			require.NoError(t, err)
			version, err := r.Version(ctx, stream.ID, view["activeVersion"].(int64))
			require.NoError(t, err)
			manifest, err := version.ProgramV2Manifest()
			require.NoError(t, err)
			snapshots, err := version.Sources()
			require.NoError(t, err)
			reserved, err := r.ReserveRun(ctx, repo.StreamRun{ID: "business-" + vendor, StreamID: stream.ID, VersionID: version.ID, EntryID: "read", Model: "program", Status: "processing", Summary: `{}`})
			require.NoError(t, err)
			require.True(t, reserved)
			host, err := s.ProgramV2Host(context.Background(), manifest, snapshots, "business-"+vendor, "read", nil, false, nil, nil)
			require.NoError(t, err)
			result, err := streamexec.ExecuteProgram(context.Background(), streamexec.ProgramRequest{Code: definition.Script.Code, Input: json.RawMessage(`null`), TimeoutMs: definition.Script.TimeoutMs, EntryID: "read", ExpectedManifest: &manifest}, host)
			require.NoError(t, err)
			require.Empty(t, result.Error)
			require.Contains(t, string(result.Data), identity)
			require.Contains(t, string(result.Data), `"resourceRef":"`+ref+`"`)
			require.Equal(t, int32(2), queries.Load())
			expectedQueries := int32(2)
			if vendor == "salesforce" {
				page, err := host(context.Background(), streamexec.ProgramCall{ID: 2, Kind: "source.read", SourceID: fmt.Sprint(id), ResourceRef: ref, Options: json.RawMessage(`{"mode":"scan","limit":200,"maxItems":2}`)})
				require.NoError(t, err)
				require.Contains(t, string(page), `"hasMore":false`)
				require.Contains(t, string(page), identity)
				expectedQueries++
			}

			// Rejected inputs never reach the vendor, even for an unattended run.
			for _, options := range []string{`{"query":{"q":"SELECT Id FROM Account"}}`, `{"limit":101}`, `{"timeoutMs":10001}`, `{"itemId":"../other"}`, `{"mode":"scan","limit":1}`} {
				_, err = host(context.Background(), streamexec.ProgramCall{ID: 2, Kind: "source.read", SourceID: fmt.Sprint(id), ResourceRef: ref, Options: json.RawMessage(options)})
				require.Error(t, err, options)
			}
			if vendor == "teamcenter" {
				_, err = host(context.Background(), streamexec.ProgramCall{ID: 2, Kind: "source.read", SourceID: fmt.Sprint(id), ResourceRef: ref, Options: json.RawMessage(`{"createdDateTimeBegin":"2026-11-07T00:00:00Z","createdDateTimeEnd":"2026-11-08T00:00:00Z"}`)})
				require.Error(t, err, "Teamcenter must reject unsupported time filters instead of ignoring them")
			}
			_, err = host(context.Background(), streamexec.ProgramCall{ID: 2, Kind: "source.write", SourceID: fmt.Sprint(id), ResourceRef: ref})
			require.Error(t, err)
			callerHost, err := s.ProgramV2Host(programV2ServiceContext("uns.source"), manifest, snapshots, "business-"+vendor, "read", nil, false, nil, nil)
			require.NoError(t, err)
			_, err = callerHost(context.Background(), streamexec.ProgramCall{ID: 2, Kind: "source.read", SourceID: fmt.Sprint(id), ResourceRef: ref, Options: json.RawMessage(options)})
			require.ErrorIs(t, err, ErrForbidden)
			require.Equal(t, expectedQueries, queries.Load())

			// An observed physical descriptor change (without a configuration
			// revision bump) still invalidates the published execution contract.
			changed := strings.ReplaceAll(source.Resources, `"nativeIdentity":`, `"contractChanged":true,"nativeIdentity":`)
			require.NotEqual(t, source.Resources, changed)
			require.NoError(t, r.ObserveSource(ctx, id, source.Revision, "healthy", "", &changed))
			_, err = host(context.Background(), streamexec.ProgramCall{ID: 3, Kind: "source.read", SourceID: fmt.Sprint(id), ResourceRef: ref, Options: json.RawMessage(options)})
			require.ErrorIs(t, err, repo.ErrConflict)
			require.Equal(t, expectedQueries, queries.Load())
		})
	}
}
