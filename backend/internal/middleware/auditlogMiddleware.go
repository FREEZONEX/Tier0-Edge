package middleware

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"backend/internal/domain/audit"
)

const auditRequestBodyLimit = 64 << 10

type AuditLogMiddleware struct {
	auditSvc *audit.Service
}

func NewAuditLogMiddleware(auditSvc *audit.Service) *AuditLogMiddleware {
	return &AuditLogMiddleware{auditSvc: auditSvc}
}

func (m *AuditLogMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.auditSvc == nil {
			next(w, r)
			return
		}
		recorder := &auditResponseWriter{ResponseWriter: w}
		if strings.TrimSpace(r.Header.Get("X-Request-ID")) == "" && strings.TrimSpace(r.Header.Get("Traceparent")) == "" {
			r.Header.Set("X-Request-ID", uuid.NewString())
		}
		correlationID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if correlationID == "" {
			correlationID = strings.TrimSpace(r.Header.Get("Traceparent"))
		}
		w.Header().Set("X-Request-ID", correlationID)
		r = r.WithContext(m.auditSvc.WithState(r.Context()))
		m.auditSvc.BindRequestInfo(r)
		if payload := captureJSONRequestPayload(r); len(payload) > 0 {
			m.auditSvc.BindRequestPayload(r.Context(), payload)
		}
		next(recorder, r)
		statusCode := recorder.StatusCode()
		jobKey, jobStatus := recorder.AsyncJob()
		m.auditSvc.FlushRequest(r, audit.ResponseMeta{
			StatusCode: statusCode,
			Status:     recorder.ResultMessage(),
			JobKey:     jobKey, JobStatus: jobStatus,
		})
	}
}

// 只读取标准异步任务响应的标识和状态，HTTP 状态码保持原样。
func (w *auditResponseWriter) AsyncJob() (string, string) {
	if w.StatusCode() < 200 || w.StatusCode() >= 300 {
		return "", ""
	}
	var body struct {
		Data struct {
			JobKey string `json:"jobKey"`
			Status string `json:"status"`
			Job    struct {
				JobKey  string `json:"jobKey"`
				JobType string `json:"jobType"`
				Status  string `json:"status"`
			} `json:"job"`
		} `json:"data"`
	}
	if json.Unmarshal(w.responseBody, &body) != nil {
		return "", ""
	}
	if body.Data.Job.JobType != "" {
		return body.Data.Job.JobKey, body.Data.Job.Status
	}
	if body.Data.JobKey != "" {
		status := body.Data.Status
		if status == "" {
			status = "pending"
		}
		return body.Data.JobKey, status
	}
	return "", ""
}

func captureJSONRequestPayload(r *http.Request) map[string]any {
	if r == nil || sensitiveSourceRequest(r) || r.Body == nil || r.ContentLength <= 0 || r.ContentLength > auditRequestBodyLimit {
		return nil
	}
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "application/json") {
		return nil
	}
	raw, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil || len(raw) == 0 {
		return nil
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil
	}
	return payload
}

type auditResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	responseBody []byte
}

func (w *auditResponseWriter) StatusCode() int {
	if w.statusCode == 0 {
		return http.StatusOK
	}
	return w.statusCode
}

func (w *auditResponseWriter) WriteHeader(code int) {
	if w.statusCode != 0 {
		return
	}
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *auditResponseWriter) Write(data []byte) (int, error) {
	if len(w.responseBody) < auditRequestBodyLimit {
		remaining := auditRequestBodyLimit - len(w.responseBody)
		if len(data) < remaining {
			remaining = len(data)
		}
		w.responseBody = append(w.responseBody, data[:remaining]...)
	}
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *auditResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *auditResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *auditResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (w *auditResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(r)
	}
	return io.Copy(w.ResponseWriter, r)
}

func (w *auditResponseWriter) ResultMessage() string {
	var body struct {
		Msg     string `json:"msg"`
		Message string `json:"message"`
	}
	if w.StatusCode() >= 400 && json.Unmarshal(w.responseBody, &body) == nil {
		if body.Msg != "" {
			return body.Msg
		}
		if body.Message != "" {
			return body.Message
		}
	}
	return http.StatusText(w.StatusCode())
}
