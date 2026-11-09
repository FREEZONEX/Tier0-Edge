package openapiresp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	domainnotify "backend/internal/domain/notify"
)

// TestWriteLogicError_Mapping 错误映射表逐条断言（对齐云端 notifyResponse.go）。
func TestWriteLogicError_Mapping(t *testing.T) {
	cases := []struct {
		err        error
		errorCode  string
		httpStatus int
	}{
		{domainnotify.ErrInvalidRequest, "INVALID_REQUEST", http.StatusBadRequest},
		{domainnotify.ErrInvalidType, "INVALID_NOTIFICATION_TYPE", http.StatusBadRequest},
		{domainnotify.ErrInvalidCredential, "INVALID_CREDENTIAL", http.StatusUnauthorized},
		{domainnotify.ErrNotAllowed, "NOTIFICATION_NOT_ALLOWED", http.StatusForbidden},
		{domainnotify.ErrRecipientNotFound, "RECIPIENT_NOT_AVAILABLE", http.StatusNotFound},
		{domainnotify.ErrContentLimit, "CONTENT_LIMIT_EXCEEDED", http.StatusUnprocessableEntity},
		{domainnotify.ErrRateLimited, "NOTIFICATION_RATE_LIMITED", http.StatusTooManyRequests},
		{domainnotify.ErrIdempotencyConflict, "IDEMPOTENCY_KEY_CONFLICT", http.StatusConflict},
		{domainnotify.ErrMessageNotFound, "MESSAGE_NOT_FOUND", http.StatusNotFound},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		WriteLogicError(w, c.err)
		if w.Code != c.httpStatus {
			t.Fatalf("%s: status got %d want %d", c.errorCode, w.Code, c.httpStatus)
		}
		var body struct {
			ErrorCode string `json:"errorCode"`
			Message   string `json:"message"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("body not json: %v", err)
		}
		if body.ErrorCode != c.errorCode {
			t.Fatalf("errorCode got %q want %q", body.ErrorCode, c.errorCode)
		}
		if body.Message == "" {
			t.Fatalf("message should not be empty for %s", c.errorCode)
		}
	}
}

// TestWriteLogicError_InternalFallback 未识别错误兜底 500 INTERNAL_ERROR，不暴露内部栈。
func TestWriteLogicError_InternalFallback(t *testing.T) {
	w := httptest.NewRecorder()
	WriteLogicError(w, errors.New("secret db detail"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status got %d want 500", w.Code)
	}
	var body struct {
		ErrorCode string `json:"errorCode"`
		Message   string `json:"message"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.ErrorCode != "INTERNAL_ERROR" {
		t.Fatalf("errorCode got %q want INTERNAL_ERROR", body.ErrorCode)
	}
	if body.Message != "internal error" {
		t.Fatalf("internal detail leaked: %q", body.Message)
	}
}

// TestWriteError_AuthStage 鉴权阶段错误体格式。
func TestWriteError_AuthStage(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIAL", "invalid credential")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status got %d", w.Code)
	}
	if w.Header().Get("Content-Type") == "" {
		t.Fatal("content-type should be set")
	}
}
