// Package openapiresp 消息通知 OpenAPI 专用 HTTP 响应契约层。
//
// 与企业版其余接口的 Envelope{code,msg,data} 不同（对齐云端 notifyresp）：
// /openapi/v1/notifications/* 的成功体直接写业务 JSON，错误体为 {"errorCode":"...","message":"..."}
// + 真实 HTTP 状态码；内部错误码 3005001~3005040 ↔ errorCode ↔ HTTP 状态码三方映射逐条对齐云端。
package openapiresp

import (
	"net/http"

	domainnotify "backend/internal/domain/notify"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// notifyErrorBody OpenAPI 统一错误响应体。
type notifyErrorBody struct {
	ErrorCode string `json:"errorCode"`
	Message   string `json:"message"`
}

// errorMapping 内部错误码 ↔ OpenAPI errorCode ↔ HTTP 状态码（对齐云端 errorMapping 表）。
var errorMapping = []struct {
	code       int
	errorCode  string
	httpStatus int
}{
	{3005001, "INVALID_REQUEST", http.StatusBadRequest},
	{3005002, "INVALID_NOTIFICATION_TYPE", http.StatusBadRequest},
	{3005010, "INVALID_CREDENTIAL", http.StatusUnauthorized},
	{3005011, "NOTIFICATION_NOT_ALLOWED", http.StatusForbidden},
	{3005020, "RECIPIENT_NOT_AVAILABLE", http.StatusNotFound},
	{3005021, "CONTENT_LIMIT_EXCEEDED", http.StatusUnprocessableEntity},
	{3005030, "NOTIFICATION_RATE_LIMITED", http.StatusTooManyRequests},
	{3005031, "IDEMPOTENCY_KEY_CONFLICT", http.StatusConflict},
	{3005040, "MESSAGE_NOT_FOUND", http.StatusNotFound},
}

// WriteError 按指定状态码与 errorCode 写错误体（鉴权阶段专用）。
func WriteError(w http.ResponseWriter, statusCode int, errorCode, message string) {
	httpx.WriteJson(w, statusCode, notifyErrorBody{ErrorCode: errorCode, Message: message})
}

// WriteLogicError 业务错误入口：按映射表选状态码与 errorCode；
// message 取 err 实际消息（可含更具体的提示）；未命中映射表兜底 500 INTERNAL_ERROR，不暴露内部错误栈。
func WriteLogicError(w http.ResponseWriter, err error) {
	ce, ok := domainnotify.AsCodedError(err)
	if ok {
		for _, m := range errorMapping {
			if m.code == ce.ErrorCode() {
				WriteError(w, m.httpStatus, m.errorCode, ce.Error())
				return
			}
		}
	}
	WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
}

// WriteOK 成功体：直接写业务 JSON（不走 Envelope）。
func WriteOK(w http.ResponseWriter, v any) {
	httpx.WriteJson(w, http.StatusOK, v)
}
