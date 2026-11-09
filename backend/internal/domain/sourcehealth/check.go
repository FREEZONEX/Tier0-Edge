package sourcehealth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"syscall"
	"time"
)

const Interval = 10 * time.Minute
const Connection = "connection"
const Data = "data"

// Result 不包含底层错误正文、连接串或凭据，可安全持久化和公开。
type Result struct {
	Mode      string `json:"mode"`
	Health    string `json:"health"`
	Stage     string `json:"stage"`
	ErrorCode string `json:"errorCode"`
	Message   string `json:"message"`
	CheckedAt int64  `json:"checkedAt"`
	LatencyMs int64  `json:"latencyMs"`
}

type Failure struct {
	Stage, Code, Message string
	Cause                error
}

func (e *Failure) Error() string { return e.Code }
func (e *Failure) Unwrap() error { return e.Cause }
func Fail(stage, code, message string, cause error) error {
	return &Failure{stage, code, message, cause}
}

func New(mode, stage string, started time.Time, err error) Result {
	r := Result{Mode: mode, Health: "healthy", Stage: stage, CheckedAt: time.Now().UnixMilli(), LatencyMs: time.Since(started).Milliseconds(), Message: "Connection check passed; data validation was not performed"}
	if mode == Data {
		r.Message = "Data validation passed for the configured resources; no business data was written"
	}
	if err == nil {
		return r
	}
	r.Health = "unhealthy"
	var failure *Failure
	if errors.As(err, &failure) {
		r.Stage, r.ErrorCode, r.Message = failure.Stage, failure.Code, failure.Message
		return r
	}
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	var network net.Error
	switch {
	case errors.As(err, &dns):
		r.Stage, r.ErrorCode, r.Message = "dns", "SOURCE_DNS_FAILED", "Source hostname could not be resolved"
	case errors.As(err, &certificate) || errors.As(err, &unknown) || errors.As(err, &host) || errors.As(err, &invalid):
		r.Stage, r.ErrorCode, r.Message = "tls", "SOURCE_TLS_FAILED", "TLS certificate, hostname or trust verification failed"
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout():
		r.ErrorCode, r.Message = "SOURCE_TIMEOUT", "Source check timed out during "+stage
	case errors.Is(err, syscall.ECONNREFUSED):
		r.Stage, r.ErrorCode, r.Message = "connect", "SOURCE_CONNECTION_REFUSED", "Source connection was refused"
	case errors.Is(err, context.Canceled):
		r.ErrorCode, r.Message = "SOURCE_CHECK_CANCELLED", "Source check was cancelled"
	default:
		r.ErrorCode, r.Message = "SOURCE_CHECK_FAILED", "Source check failed during "+stage
	}
	return r
}

// SafeCode 只允许代码自有的短标识，不能把上游正文作为错误描述。
func SafeCode(code string) string {
	if len(code) == 0 || len(code) > 64 || strings.IndexFunc(code, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_')
	}) >= 0 {
		return "SOURCE_DATA_INVALID"
	}
	return code
}
