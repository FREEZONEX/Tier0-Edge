package httpx

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/types"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func TestStreamRequestStrictWireBody(t *testing.T) {
	for _, body := range []string{`{"input":null}`, `{"input":{"id":9007199254740993},"idempotencyKey":"持久键"}`} {
		request := httptest.NewRequest("POST", "/openapi/v2/stream-requests/example.echo/invoke", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
		request = pathvar.WithVars(request, map[string]string{"requestCode": "example.echo"})
		var parsed types.StreamRequestInvokeReq
		require.NoError(t, ParseStreamRequest(request, &parsed))
		require.Equal(t, "example.echo", parsed.RequestCode)
		if parsed.Input != nil {
			require.Equal(t, json.Number("9007199254740993"), parsed.Input.(map[string]any)["id"])
		}
	}
	for _, tc := range []struct{ name, body, media, query, code string }{
		{"absent input", `{}`, "application/json", "", "example.echo"},
		{"duplicate", `{"input":null,"input":1}`, "application/json", "", "example.echo"},
		{"escaped duplicate", `{"input":null,"\u0069nput":1}`, "application/json", "", "example.echo"},
		{"case variant", `{"Input":null}`, "application/json", "", "example.echo"},
		{"path override", `{"input":null,"requestCode":"other"}`, "application/json", "", "example.echo"},
		{"query override", `{"input":null}`, "application/json", "?requestCode=other", "example.echo"},
		{"null key", `{"input":null,"idempotencyKey":null}`, "application/json", "", "example.echo"},
		{"numeric key", `{"input":null,"idempotencyKey":12}`, "application/json", "", "example.echo"},
		{"non object", `null`, "application/json", "", "example.echo"},
		{"trailing", `{"input":null} {}`, "application/json", "", "example.echo"},
		{"malformed", `{"input":`, "application/json", "", "example.echo"},
		{"invalid UTF-8", "{\"input\":\"\xff\"}", "application/json", "", "example.echo"},
		{"oversized", `{"input":"` + strings.Repeat("x", sourceStreamBodyLimit) + `"}`, "application/json", "", "example.echo"},
		{"wrong media", `{"input":null}`, "text/plain", "", "example.echo"},
		{"invalid code", `{"input":null}`, "application/json", "", "Example.echo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/openapi/v2/stream-requests/example.echo/invoke"+tc.query, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", tc.media)
			request = pathvar.WithVars(request, map[string]string{"requestCode": tc.code})
			err := ParseStreamRequest(request, &types.StreamRequestInvokeReq{})
			require.Error(t, err)
			response := httptest.NewRecorder()
			WriteStreamRequestError(response, err)
			require.Equal(t, 400, response.Code)
			require.JSONEq(t, `{"errorCode":"STREAM_REQUEST_INVALID_INPUT","message":"invalid stream request input"}`, response.Body.String())
		})
	}
	for _, body := range []string{`{"page":null}`, `{"page":1.5}`, `{"size":"20"}`, `{"search":null}`, `{"status":"active"}`} {
		request := httptest.NewRequest("POST", "/openapi/v2/stream-requests/discover", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		require.Error(t, ParseStreamRequest(request, &types.StreamRequestDiscoverReq{}), body)
	}
}

func TestStreamRequestErrorsAreStableAndPrivate(t *testing.T) {
	for code, status := range map[string]int{"INVALID_CREDENTIAL": 401, "STREAM_REQUEST_FORBIDDEN": 403, "STREAM_REQUEST_NOT_FOUND": 404, "STREAM_REQUEST_INACTIVE": 409, "IDEMPOTENCY_CONFLICT": 409, "STREAM_REQUEST_OUTPUT_INVALID": 422, "STREAM_REQUEST_RATE_LIMITED": 429, "SOURCE_PROTOCOL_ERROR": 502, "STREAM_REQUEST_UNAVAILABLE": 503, "STREAM_REQUEST_RESULT_UNKNOWN": 504} {
		response := httptest.NewRecorder()
		WriteStreamRequestError(response, NewHTTPError(599, code))
		require.Equal(t, status, response.Code)
		var value map[string]string
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &value))
		require.Len(t, value, 2)
		require.Equal(t, code, value["errorCode"])
	}
	for _, err := range []error{errors.New("secret source endpoint"), NewHTTPError(400, "secret source endpoint")} {
		response := httptest.NewRecorder()
		WriteStreamRequestError(response, err)
		require.Equal(t, 500, response.Code)
		require.NotContains(t, response.Body.String(), "secret")
		require.Contains(t, response.Body.String(), "STREAM_REQUEST_INTERNAL_ERROR")
	}
	for _, prefix := range []string{"/api/core/v2", "/openapi/v2"} {
		response := httptest.NewRecorder()
		require.True(t, WriteStreamRequestAuthError(response, httptest.NewRequest("POST", prefix+"/stream-requests/example.echo/invoke", nil), "UNAUTHENTICATED"))
		require.Equal(t, 401, response.Code)
		require.Contains(t, response.Body.String(), "INVALID_CREDENTIAL")
	}
	require.False(t, WriteStreamRequestAuthError(httptest.NewRecorder(), httptest.NewRequest("POST", "/openapi/v1/controls/discover", nil), "FORBIDDEN"))
}

func TestReadOperationPayload(t *testing.T) {
	for _, body := range []string{`null`, `[1,"two"]`, `"value"`, `{"resourceId":"body-not-routing","input":1,"idempotencyKey":"not-control","id":9007199254740993}`} {
		r := httptest.NewRequest("POST", "/resources/actual/invoke", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
		value, err := ReadOperationPayload(r)
		require.NoError(t, err)
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.JSONEq(t, body, string(encoded))
		if object, ok := value.(map[string]any); ok {
			require.Equal(t, json.Number("9007199254740993"), object["id"])
		}
	}
	for _, tc := range []struct{ media, body string }{
		{"", `{}`}, {"text/plain", `{}`}, {"application/json", ""},
		{"application/json", `{} {}`}, {"application/json", `{"invalid":`},
		{"application/json", `{"x":"` + strings.Repeat("x", sourceStreamBodyLimit) + `"}`},
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.media)
		_, err := ReadOperationPayload(r)
		require.Error(t, err)
	}
}
