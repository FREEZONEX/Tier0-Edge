package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func TestSourceStreamJSONParsing(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		target     any
	}{
		{"source", `{"expectedRevision":0,"document":{"vendor":{"unknown":42}}}`, &types.SourceSaveReq{}},
		{"stream", `{"expectedRevision":0,"document":{"script":"return input"}}`, &types.StreamSaveReq{}},
		{"probe", `{"timeoutMs":1000,"document":{"protocol":"mqtt"}}`, &types.SourceProbeReq{}},
		{"validate", `{"expectedSourceRevision":1,"document":{}}`, &types.StreamValidateReq{}},
		{"test", `{"expectedSourceRevision":1,"phase":"request","document":{},"input":{"values":[1,2]}}`, &types.StreamTestReq{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json; charset=utf-8")
			require.NoError(t, ParseSourceStreamRequest(r, tc.target))
		})
	}
	var req types.SourceSaveReq
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"expectedRevision":0,"document":{"vendor":{"unknown":42}}}`))
	r.Header.Set("Content-Type", "application/json")
	require.NoError(t, ParseSourceStreamRequest(r, &req))
	require.Equal(t, float64(42), req.Document.(map[string]any)["vendor"].(map[string]any)["unknown"])
}

func TestSourceStreamRejectInvalidJSON(t *testing.T) {
	for _, body := range []string{
		``, `null`, `[]`, `{}`, `{"expectedRevision":0}`, `{"expectedRevision":0,"document":null}`,
		`{"expectedRevision":"zero","document":{}}`, `{"expectedRevision":0,"document":{}} {}`,
		`{"expectedRevision":0,"document":`,
		`{"expectedRevision":0,"document":{"x":"` + strings.Repeat("x", sourceStreamBodyLimit) + `"}}`,
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		var req types.SourceSaveReq
		require.Error(t, ParseSourceStreamRequest(r, &req), "body prefix: %.80s", body)
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"expectedRevision":0,"document":{}}`))
	var req types.SourceSaveReq
	require.Error(t, ParseSourceStreamRequest(r, &req))
}

func TestSourceStreamRouteIdentityIsAuthoritative(t *testing.T) {
	for _, prefix := range []string{"/api/core/uns", "/openapi/v1/uns"} {
		r := httptest.NewRequest("POST", prefix+"/streams/37/invoke", strings.NewReader(`{"streamId":999,"StreamId":998,"expectedActiveVersion":1,"idempotencyKey":"test","input":{},"timeoutMs":1000}`))
		r.Header.Set("Content-Type", "application/json")
		r = pathvar.WithVars(r, map[string]string{"streamId": "37"})
		var req types.StreamInvokeReq
		require.NoError(t, ParseSourceStreamRequest(r, &req))
		require.EqualValues(t, 37, req.StreamId)
	}
	r := httptest.NewRequest("POST", "/sources", strings.NewReader(`{"sourceId":999,"SourceId":998,"expectedRevision":0,"document":{}}`))
	r.Header.Set("Content-Type", "application/json")
	var req types.SourceSaveReq
	require.NoError(t, ParseSourceStreamRequest(r, &req))
	require.Zero(t, req.SourceId)
}

func TestSourceStreamQueryAndPathParsing(t *testing.T) {
	r := httptest.NewRequest("GET", "/streams?sourceId=12&page=2&size=10", nil)
	var list types.StreamListReq
	require.NoError(t, ParseSourceStreamRequest(r, &list))
	require.EqualValues(t, 12, list.SourceId)
	require.Equal(t, 2, list.Page)
	r = pathvar.WithVars(httptest.NewRequest("GET", "/sources/7", nil), map[string]string{"sourceId": "7"})
	var req types.SourceIDReq
	require.NoError(t, ParseSourceStreamRequest(r, &req))
	require.EqualValues(t, 7, req.SourceId)
}

func TestCompleteProgramEntryRequestParsing(t *testing.T) {
	request := func(path, body string) *http.Request {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	var validate types.StreamValidateReq
	require.NoError(t, ParseSourceStreamRequest(request("/streams/validate", `{"document":{"name":"program","script":"module.exports=function(api){};"}}`), &validate))
	require.Zero(t, validate.ExpectedSourceRevision)
	var preview types.StreamTestReq
	require.NoError(t, ParseSourceStreamRequest(request("/streams/test", `{"document":{},"input":{"data":{}},"entryId":"read-device","phase":"input"}`), &preview))
	require.Equal(t, "read-device", preview.EntryId)
	require.Zero(t, preview.ExpectedSourceRevision)
	for _, prefix := range []string{"/api/core/uns", "/openapi/v1/uns"} {
		var invoke types.StreamInvokeReq
		r := pathvar.WithVars(request(prefix+"/streams/37/invoke", `{"expectedActiveVersion":1,"idempotencyKey":"test","input":{},"entryId":"read-device","timeoutMs":1000}`), map[string]string{"streamId": "37"})
		require.NoError(t, ParseSourceStreamRequest(r, &invoke))
		require.EqualValues(t, 37, invoke.StreamId)
		require.Equal(t, "read-device", invoke.EntryId)
	}
}
