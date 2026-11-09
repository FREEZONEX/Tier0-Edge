package httpx_test

import (
	"backend/internal/domain/unsv2"
	respx "backend/internal/httpx"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnsV2HTTPErrorPreservesCommittedBatchProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respx.WriteUnsV2Error(w, r.Context(), &unsv2.Fault{Code: "TEMPORARILY_UNAVAILABLE", Message: "batch interrupted", Data: map[string]any{"status": "partiallyCommitted", "insertedCount": 1, "nextIndex": 1, "unknownUntil": 2}, RetryAdvice: &unsv2.RetryAdvice{CanRetrySameRequest: true, KeepIdempotencyKey: true}})
	}))
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	data := body["data"].(map[string]any)
	require.Equal(t, "partiallyCommitted", data["status"])
	require.EqualValues(t, 1, data["insertedCount"])
	require.EqualValues(t, 1, data["nextIndex"])
	require.EqualValues(t, 2, data["unknownUntil"])
	require.NotContains(t, body["error"], "rolledBack")
	require.NotContains(t, body["error"], "data")
}
