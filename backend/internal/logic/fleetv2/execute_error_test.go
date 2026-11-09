package fleetv2

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/domain/unsv2"
	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

// 通过真实 HTTP 编解码验证固定诊断与旧客户端的兼容性，而非仅检查内部 map。
func TestWriteErrorSourceDiagnosticsHTTP(t *testing.T) {
	const secret = "private-upstream-sql-password"
	for _, tc := range []struct {
		name, code, path, reason, stage string
		status                          int
		wantValidation                  bool
	}{
		{"sql", "SOURCE_SQL_OBJECT_NOT_FOUND", secret, secret, "source_query", 422, false},
		{"unknownSQL", "SOURCE_SQL_FAILED", secret, secret, "source_query", 422, false},
		{"dns", "SOURCE_DNS_FAILED", secret, secret, "source_dns", 422, false},
		{"tls", "SOURCE_TLS_FAILED", secret, secret, "source_tls", 422, false},
		{"file", "SOURCE_FILE_CHANGED", "cursor", "directoryChanged", "source_version", 422, true},
		{"mismatchedValidation", "SOURCE_FILE_CHANGED", "cursor", "invalidOrStale", "source_version", 422, false},
		{"resourceLimit", "SOURCE_RESOURCE_LIMIT", "file", "tooLarge", "source_budget", 422, true},
		{"unknownCode", "SOURCE_SQL_UNRECOGNIZED", secret, secret, "", 400, false},
		{"executionCode", "SCRIPT_TIMEOUT", secret, secret, "", 400, false},
		{"authorization", "FORBIDDEN", secret, secret, "", 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fault := &unsv2.Fault{Code: tc.code, Message: secret, Repair: secret, Path: tc.path, Reason: tc.reason, Data: map[string]any{"secret": secret}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				WriteError(w, r, errors.Join(errors.New(secret), fault))
			}))
			defer server.Close()
			response, err := server.Client().Get(server.URL)
			require.NoError(t, err)
			defer response.Body.Close()
			raw, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, tc.status, response.StatusCode)
			require.NotContains(t, string(raw), secret)
			var envelope struct {
				Code  int            `json:"code"`
				Msg   string         `json:"msg"`
				Data  any            `json:"data"`
				Error map[string]any `json:"error"`
			}
			require.NoError(t, json.Unmarshal(raw, &envelope))
			require.Equal(t, tc.status, envelope.Code)
			require.Nil(t, envelope.Data)
			require.Equal(t, tc.code, envelope.Error["code"])
			if tc.stage != "" {
				require.Equal(t, tc.stage, envelope.Error["stage"])
				require.Equal(t, envelope.Msg, envelope.Error["message"])
				require.NotEmpty(t, envelope.Error["repair"])
			} else {
				require.NotContains(t, envelope.Error, "stage")
				require.NotContains(t, envelope.Error, "message")
				require.NotContains(t, envelope.Error, "repair")
			}
			if tc.wantValidation {
				require.Equal(t, tc.path, envelope.Error["path"])
				require.Equal(t, tc.reason, envelope.Error["reason"])
			} else {
				require.NotContains(t, envelope.Error, "path")
				require.NotContains(t, envelope.Error, "reason")
			}
			// Cloud 当前只解码错误码；新增诊断必须保持这种读取方式兼容。
			var legacy struct {
				Code  int `json:"code"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(raw, &legacy))
			require.Equal(t, tc.code, legacy.Error.Code)
			require.Equal(t, tc.status, legacy.Code)
		})
	}
}

func TestWriteErrorSourceDiagnosticDoesNotOverrideConflict(t *testing.T) {
	response := httptest.NewRecorder()
	WriteError(response, httptest.NewRequest(http.MethodPost, "/", nil), errors.Join(repo.ErrConflict, &unsv2.Fault{Code: "SOURCE_SQL_FAILED"}))
	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), `"code":"CONFLICT"`)
	require.NotContains(t, response.Body.String(), "source_query")
}
