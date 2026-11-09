package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"backend/internal/operationcontract"
	"backend/internal/types"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

// ParseStreamRequest keeps path identity separate and accepts only the exact
// Cloud wire fields. Null input is a value; absent input is not a request.
func ParseStreamRequest(r *http.Request, target any) error {
	invalid := NewHTTPError(http.StatusBadRequest, "STREAM_REQUEST_INVALID_INPUT")
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.Body == nil || r.URL.RawQuery != "" {
		return invalid
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, operationcontract.MaxValueBytes+1))
	if err != nil || len(raw) > operationcontract.MaxValueBytes || !utf8.Valid(raw) {
		return invalid
	}
	allowed := map[string]bool{}
	switch target.(type) {
	case *types.StreamRequestDiscoverReq:
		allowed = map[string]bool{"page": true, "size": true, "search": true}
	case *types.StreamRequestDescribeReq:
	case *types.StreamRequestInvokeReq:
		allowed = map[string]bool{"input": true, "idempotencyKey": true}
	default:
		return invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return invalid
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return invalid
		}
		name, ok := token.(string)
		if !ok || !allowed[name] {
			return invalid
		}
		if _, exists := fields[name]; exists {
			return invalid
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return invalid
		}
		fields[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return invalid
	}
	if decoder.Decode(new(any)) != io.EOF {
		return invalid
	}
	field := func(name string, dst any) error {
		value, exists := fields[name]
		if !exists {
			return nil
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, dst) != nil {
			return invalid
		}
		return nil
	}
	switch req := target.(type) {
	case *types.StreamRequestDiscoverReq:
		if field("page", &req.Page) != nil || field("size", &req.Size) != nil || field("search", &req.Search) != nil {
			return invalid
		}
	case *types.StreamRequestDescribeReq:
		req.RequestCode = pathvar.Vars(r)["requestCode"]
		if !operationcontract.ValidRequestCode(req.RequestCode) {
			return invalid
		}
	case *types.StreamRequestInvokeReq:
		req.RequestCode = pathvar.Vars(r)["requestCode"]
		if !operationcontract.ValidRequestCode(req.RequestCode) || field("idempotencyKey", &req.IdempotencyKey) != nil {
			return invalid
		}
		input, exists := fields["input"]
		if !exists {
			return invalid
		}
		inputDecoder := json.NewDecoder(bytes.NewReader(input))
		inputDecoder.UseNumber()
		if inputDecoder.Decode(&req.Input) != nil {
			return invalid
		}
	}
	return nil
}

// Unknown errors never expose backend exceptions or execution bindings.
func WriteStreamRequestError(w http.ResponseWriter, err error) {
	code := "STREAM_REQUEST_INTERNAL_ERROR"
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		code = httpErr.Msg
	}
	status, message := http.StatusInternalServerError, "internal stream request error"
	switch code {
	case "INVALID_CREDENTIAL":
		status, message = 401, "invalid credential"
	case "STREAM_REQUEST_INVALID_INPUT":
		status, message = 400, "invalid stream request input"
	case "STREAM_REQUEST_FORBIDDEN":
		status, message = 403, "stream request access forbidden"
	case "STREAM_REQUEST_NOT_FOUND":
		status, message = 404, "stream request not found"
	case "STREAM_REQUEST_INACTIVE":
		status, message = 409, "stream request is inactive"
	case "IDEMPOTENCY_CONFLICT":
		status, message = 409, "idempotency key conflicts with a previous request"
	case "STREAM_REQUEST_OUTPUT_INVALID":
		status, message = 422, "stream request output does not match contract"
	case "STREAM_REQUEST_RATE_LIMITED":
		status, message = 429, "stream request rate limit exceeded"
	case "SOURCE_PROTOCOL_ERROR":
		status, message = 502, "source protocol failed"
	case "STREAM_REQUEST_UNAVAILABLE":
		status, message = 503, "stream request unavailable"
	case "STREAM_REQUEST_RESULT_UNKNOWN":
		status, message = 504, "stream request result is unknown"
	default:
		code = "STREAM_REQUEST_INTERNAL_ERROR"
	}
	gozerohttpx.WriteJson(w, status, map[string]string{"errorCode": code, "message": message})
}

func WriteStreamRequestAuthError(w http.ResponseWriter, r *http.Request, code string) bool {
	if !operationcontract.IsStreamRequestRoute(r.Method, r.URL.Path) {
		return false
	}
	switch code {
	case "UNAUTHENTICATED":
		code = "INVALID_CREDENTIAL"
	case "FORBIDDEN":
		code = "STREAM_REQUEST_FORBIDDEN"
	default:
		code = "STREAM_REQUEST_INTERNAL_ERROR"
	}
	WriteStreamRequestError(w, NewHTTPError(0, code))
	return true
}
