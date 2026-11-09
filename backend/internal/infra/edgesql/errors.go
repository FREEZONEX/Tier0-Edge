package edgesql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"syscall"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrResultLimit = errors.New("SQL result exceeds byte limit")

const (
	SQLFailedCode            = "SOURCE_SQL_FAILED"
	SQLInvalidCode           = "SOURCE_SQL_INVALID"
	SQLObjectNotFoundCode    = "SOURCE_SQL_OBJECT_NOT_FOUND"
	SQLPermissionDeniedCode  = "SOURCE_SQL_PERMISSION_DENIED"
	SQLAuthFailedCode        = "SOURCE_SQL_AUTH_FAILED"
	sqlReadTimeoutCode       = "SOURCE_READ_TIMEOUT"
	sqlDNSFailedCode         = "SOURCE_DNS_FAILED"
	sqlTLSFailedCode         = "SOURCE_TLS_FAILED"
	sqlConnectionRefusedCode = "SOURCE_CONNECTION_REFUSED"
	sqlResourceLimitCode     = "SOURCE_RESOURCE_LIMIT"
)

// 只保留驱动错误的安全分类；原始 SQL、参数、地址和错误正文不进入错误链。
type diagnosticError struct {
	code  string
	cause error
}

func (e *diagnosticError) Error() string { return e.code }
func (e *diagnosticError) Unwrap() error { return e.cause }

// DiagnosticCode 只接受协议宿主构造的类型，不依据脚本或错误字符串识别诊断。
func DiagnosticCode(err error) string {
	var diagnostic *diagnosticError
	if errors.As(err, &diagnostic) {
		return diagnostic.code
	}
	return ""
}

func SafeError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var existing *diagnosticError
	if errors.As(err, &existing) {
		return existing
	}
	code, cause := "", error(nil)
	var pg *pgconn.PgError
	var my *mysql.MySQLError
	var ms interface{ SQLErrorNumber() int32 }
	var network net.Error
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(ctx.Err(), context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		code, cause = sqlReadTimeoutCode, context.DeadlineExceeded
	case errors.As(err, &dns):
		code = sqlDNSFailedCode
		if dns.Timeout() {
			cause = context.DeadlineExceeded
		}
	case errors.As(err, &certificate), errors.As(err, &unknown), errors.As(err, &host), errors.As(err, &invalid):
		code = sqlTLSFailedCode
	case errors.Is(err, syscall.ECONNREFUSED):
		code = sqlConnectionRefusedCode
	case errors.As(err, &pg):
		switch pg.Code {
		case "42601", "42804", "42846", "22P02", "22003", "22012", "25006":
			code = SQLInvalidCode
		case "42P01", "42703", "42883", "3F000", "3D000":
			code = SQLObjectNotFoundCode
		case "42501":
			code = SQLPermissionDeniedCode
		case "28000", "28P01":
			code = SQLAuthFailedCode
		}
	case errors.As(err, &my):
		switch my.Number {
		case 1064, 1149, 1292, 1366, 1792:
			code = SQLInvalidCode
		case 1049, 1054, 1146, 1305:
			code = SQLObjectNotFoundCode
		case 1044, 1142, 1143:
			code = SQLPermissionDeniedCode
		case 1045:
			code = SQLAuthFailedCode
		case 3024:
			code, cause = sqlReadTimeoutCode, context.DeadlineExceeded
		}
	case errors.As(err, &ms):
		switch ms.SQLErrorNumber() {
		case 102, 105, 156, 245, 8114, 8115, 8134:
			code = SQLInvalidCode
		case 207, 208, 911, 2812:
			code = SQLObjectNotFoundCode
		case 229, 230, 262:
			code = SQLPermissionDeniedCode
		case 18452, 18456:
			code = SQLAuthFailedCode
		}
	case errors.As(err, &network) && network.Timeout():
		code, cause = sqlReadTimeoutCode, context.DeadlineExceeded
	}
	if errors.Is(err, ErrResultLimit) {
		code = sqlResourceLimitCode
	}
	if code == "" {
		code = SQLFailedCode
	}
	return &diagnosticError{code: code, cause: cause}
}
