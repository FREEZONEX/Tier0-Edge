package sourcestream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

// An in-process protocol fixture checks the HTTP adapter and actual Goja SDK
// together. This is not a deployed Plant0, authentication or XML-parser test.
func TestVendorSAPMetadataThroughStreamSDK(t *testing.T) {
	const code = `module.exports = function(api) {
  const sap = api.sources.use({id: "1", resources: []});
  api.stream.onRequest({id: "metadata"}, async function() {
    const response = await sap.request({method: "GET", path: "$metadata"});
    if (response.status < 200 || response.status >= 300) throw new Error("rejected");
    const xml = response.data;
    if (typeof xml !== "string" || xml.length > 262144 || /<!DOCTYPE|<!ENTITY/i.test(xml)) throw new Error("shape");
    const xmlEnvelope = /^\s*(?:<\?xml[^>]*>\s*)?<(?:[A-Za-z_][\w.-]*:)?Edmx(?:\s|>)/.test(xml);
    const csdlMaterials = /<(?:[A-Za-z_][\w.-]*:)?EntitySet\b[^>]*\bName\s*=\s*['"]Materials['"]/.test(xml);
    if (!xmlEnvelope || !csdlMaterials) throw new Error("contract");
    return {status: response.status, xmlEnvelope, csdlMaterials};
  });
};`
	for _, tc := range []struct {
		name, body string
		status     int
		valid      bool
	}{
		{"namespaced-xml", `<edmx:Edmx xmlns:edmx="urn:fixture"><EntitySet Name="Materials"/></edmx:Edmx>`, 200, true},
		{"xml-declaration", `<?xml version="1.0"?><Edmx xmlns:csdl="urn:fixture"><csdl:EntitySet Name='Materials'/></Edmx>`, 200, true},
		{"http-rejection", `{"error":"not acceptable"}`, 406, false},
		{"json-is-not-xml", `{"Edmx":{"EntitySet":"Materials"}}`, 200, false},
		{"html-is-not-metadata", `<html>Materials</html>`, 200, false},
		{"missing-entity", `<Edmx><EntitySet Name="Other"/></Edmx>`, 200, false},
		{"doctype-refused", `<!DOCTYPE Edmx><Edmx><EntitySet Name="Materials"/></Edmx>`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "/odata/$metadata", r.URL.Path)
				require.Empty(t, r.URL.RawQuery)
				require.Equal(t, "application/xml", r.Header.Get("Accept"))
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer fixture.Close()
			s := New(nil, nil, "")
			t.Cleanup(s.httpManager.Close)
			httpTestManager(t, s.httpManager, fixture)
			def := SourceDefinition{Protocol: HTTP, SourceType: "sap-odata", Endpoint: "http://fixture.internal/odata", HTTP: &HTTPConnection{Authentication: "none"}}
			request := streamexec.ProgramRequest{Code: code, EntryID: "metadata", TimeoutMs: 5000, Input: json.RawMessage(`{}`), Context: json.RawMessage(`{}`)}
			request.CheckOnly = true
			registered, err := streamexec.ExecuteProgram(context.Background(), request, nil)
			require.NoError(t, err)
			require.Zero(t, calls.Load(), "registration must not connect")
			request.CheckOnly = false
			request.ExpectedManifest = &registered.Manifest
			result, err := streamexec.ExecuteProgram(context.Background(), request, func(ctx context.Context, call streamexec.ProgramCall) (json.RawMessage, error) {
				require.True(t, streamexec.AllowsProgramCall(registered.Manifest, call))
				require.Equal(t, "source.request", call.Kind)
				require.Equal(t, "1", call.SourceID)
				return s.HTTPProgramCall(ctx, repo.Source{ID: 1, ConfigRevision: 1}, def, call)
			})
			require.EqualValues(t, 1, calls.Load(), "rejections must not replay the request")
			if !tc.valid {
				require.ErrorIs(t, err, streamexec.ErrExecution)
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, `{"status":200,"xmlEnvelope":true,"csdlMaterials":true}`, string(result.Data))
			require.Empty(t, result.Outputs)
			require.Empty(t, result.Logs)
		})
	}
}
