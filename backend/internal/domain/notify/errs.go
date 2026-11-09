package notify

import "errors"

// 错误哨兵：错误码段 3005001~3005040 与云端 share/terror/notification.go 一一对应，
// message 文案保持一致（OpenAPI 兼容验收以 errorCode+HTTP 状态码为准，message 允许更具体的提示）。
// 管理端/用户端（core 面）通过 logicx.errorStatus 映射为 HTTP 状态码。
var (
	// ErrInvalidRequest 请求体校验失败（3005001 / 400）
	ErrInvalidRequest = newCodeError(3005001, "invalid request")
	// ErrInvalidType 通知类型非法（3005002 / 400）
	ErrInvalidType = newCodeError(3005002, "invalid notification type")
	// ErrInvalidCredential API Key 无效（3005010 / 401）
	ErrInvalidCredential = newCodeError(3005010, "invalid credential")
	// ErrNotAllowed Key 无通知能力权限位（3005011 / 403）
	ErrNotAllowed = newCodeError(3005011, "notification capability not allowed for this API key")
	// ErrRecipientNotFound 收件人不存在或非有效用户（3005020 / 404）
	ErrRecipientNotFound = newCodeError(3005020, "recipient not available")
	// ErrContentLimit 内容超限（3005021 / 422）
	ErrContentLimit = newCodeError(3005021, "content limit exceeded")
	// ErrRateLimited 限流（3005030 / 429）
	ErrRateLimited = newCodeError(3005030, "notification rate limited")
	// ErrIdempotencyConflict 同键不同内容（3005031 / 409）
	ErrIdempotencyConflict = newCodeError(3005031, "idempotency key conflict")
	// ErrMessageNotFound 消息不存在或不属于该 Key（3005040 / 404）
	ErrMessageNotFound = newCodeError(3005040, "message not found")
)

// codedError 携带内部错误码的错误；openapiresp 据此映射 errorCode+HTTP 状态码，
// logicx 据 HTTPStatus() 映射 core 面状态码。
type codedError struct {
	code    int
	message string
}

func newCodeError(code int, message string) error {
	return &codedError{code: code, message: message}
}

func (e *codedError) Error() string { return e.message }

// ErrorCode 返回内部错误码（3005001~3005040）。
func (e *codedError) ErrorCode() int { return e.code }

// HTTPStatus 返回 core 面（管理端/用户端）对应的 HTTP 状态码（与 OpenAPI 映射一致）。
func (e *codedError) HTTPStatus() int {
	switch e.code {
	case 3005001, 3005002:
		return 400
	case 3005010:
		return 401
	case 3005011:
		return 403
	case 3005020, 3005040:
		return 404
	case 3005021:
		return 422
	case 3005030:
		return 429
	case 3005031:
		return 409
	default:
		return 500
	}
}

// AsCodedError 从错误链提取 codedError；不存在返回 nil。
func AsCodedError(err error) (*codedError, bool) {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce, true
	}
	return nil, false
}

// IsCode 判断错误链中是否含指定内部错误码（3005001~3005040）。
func IsCode(err error, code int) bool {
	ce, ok := AsCodedError(err)
	return ok && ce.ErrorCode() == code
}
