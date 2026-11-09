package notify

import (
	"errors"
	"testing"
)

// errsTestTarget 用于断言错误码的载体。
func TestCodedErrorCodes(t *testing.T) {
	cases := []struct {
		err      error
		code     int
		httpCode int
		msg      string
	}{
		{ErrInvalidRequest, 3005001, 400, "invalid request"},
		{ErrInvalidType, 3005002, 400, "invalid notification type"},
		{ErrInvalidCredential, 3005010, 401, "invalid credential"},
		{ErrNotAllowed, 3005011, 403, "notification capability not allowed for this API key"},
		{ErrRecipientNotFound, 3005020, 404, "recipient not available"},
		{ErrContentLimit, 3005021, 422, "content limit exceeded"},
		{ErrRateLimited, 3005030, 429, "notification rate limited"},
		{ErrIdempotencyConflict, 3005031, 409, "idempotency key conflict"},
		{ErrMessageNotFound, 3005040, 404, "message not found"},
	}
	for _, c := range cases {
		ce, ok := AsCodedError(c.err)
		if !ok {
			t.Fatalf("expect coded error for %v", c.err)
		}
		if ce.ErrorCode() != c.code {
			t.Fatalf("code mismatch: got %d want %d", ce.ErrorCode(), c.code)
		}
		if ce.HTTPStatus() != c.httpCode {
			t.Fatalf("http status mismatch: got %d want %d", ce.HTTPStatus(), c.httpCode)
		}
		if ce.Error() != c.msg {
			t.Fatalf("message mismatch: got %q want %q", ce.Error(), c.msg)
		}
	}
	if _, ok := AsCodedError(errors.New("other")); ok {
		t.Fatal("plain error should not be coded")
	}
}
