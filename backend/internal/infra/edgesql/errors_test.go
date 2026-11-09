package edgesql

// 覆盖三种真实驱动错误类型、未知编号、超时与取消，以及错误链中不保留秘密。
import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/microsoft/go-mssqldb"
	"github.com/stretchr/testify/require"
)

func TestSQLSafeErrors(t *testing.T) {
	const secret = "private SQL password and vendor body"
	for _, tc := range []struct {
		err  error
		code string
	}{
		{&pgconn.PgError{Code: "42601", Message: secret}, SQLInvalidCode},
		{&pgconn.PgError{Code: "42703", Message: secret}, SQLObjectNotFoundCode},
		{&pgconn.PgError{Code: "42501", Message: secret}, SQLPermissionDeniedCode},
		{&pgconn.PgError{Code: "28P01", Message: secret}, SQLAuthFailedCode},
		{&mysql.MySQLError{Number: 1064, Message: secret}, SQLInvalidCode},
		{&mysql.MySQLError{Number: 1146, Message: secret}, SQLObjectNotFoundCode},
		{&mysql.MySQLError{Number: 1142, Message: secret}, SQLPermissionDeniedCode},
		{&mysql.MySQLError{Number: 1045, Message: secret}, SQLAuthFailedCode},
		{mssql.Error{Number: 102, Message: secret}, SQLInvalidCode},
		{mssql.Error{Number: 208, Message: secret}, SQLObjectNotFoundCode},
		{mssql.Error{Number: 229, Message: secret}, SQLPermissionDeniedCode},
		{mssql.Error{Number: 18456, Message: secret}, SQLAuthFailedCode},
		{&pgconn.PgError{Code: "57014", Message: secret}, SQLFailedCode},
		{errors.New("SOURCE_SQL_INVALID " + secret), SQLFailedCode},
		{context.DeadlineExceeded, "SOURCE_READ_TIMEOUT"},
		{syscall.ECONNREFUSED, "SOURCE_CONNECTION_REFUSED"},
		{ErrResultLimit, "SOURCE_RESOURCE_LIMIT"},
		{&net.DNSError{Name: secret, Server: secret, Err: secret}, "SOURCE_DNS_FAILED"},
		{&net.DNSError{Name: secret, Server: secret, Err: secret, IsTimeout: true}, "SOURCE_DNS_FAILED"},
		{&tls.CertificateVerificationError{Err: errors.New(secret)}, "SOURCE_TLS_FAILED"},
		{x509.UnknownAuthorityError{Cert: &x509.Certificate{}}, "SOURCE_TLS_FAILED"},
		{x509.HostnameError{Certificate: &x509.Certificate{}, Host: secret}, "SOURCE_TLS_FAILED"},
		{x509.CertificateInvalidError{Cert: &x509.Certificate{}, Detail: secret}, "SOURCE_TLS_FAILED"},
		{errors.New("SOURCE_DNS_FAILED SOURCE_TLS_FAILED " + secret), SQLFailedCode},
	} {
		got := SafeError(context.Background(), fmt.Errorf("%s: %w", secret, tc.err))
		require.Equal(t, tc.code, DiagnosticCode(got))
		for e := got; e != nil; e = errors.Unwrap(e) {
			require.NotContains(t, e.Error(), secret)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dnsTimeout := SafeError(context.Background(), &net.DNSError{Name: secret, IsTimeout: true})
	require.Equal(t, "SOURCE_DNS_FAILED", DiagnosticCode(dnsTimeout))
	require.ErrorIs(t, dnsTimeout, context.DeadlineExceeded)
	require.ErrorIs(t, SafeError(ctx, errors.New(secret)), context.Canceled)
	require.ErrorIs(t, SafeError(ctx, &net.DNSError{Name: secret}), context.Canceled)
	require.ErrorIs(t, SafeError(ctx, &tls.CertificateVerificationError{Err: context.DeadlineExceeded}), context.Canceled)
	deadline, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	require.Equal(t, "SOURCE_READ_TIMEOUT", DiagnosticCode(SafeError(deadline, &net.DNSError{Name: secret})))
	require.Equal(t, "SOURCE_READ_TIMEOUT", DiagnosticCode(SafeError(context.Background(), errors.Join(context.DeadlineExceeded, &net.DNSError{Name: secret}))))
	require.Empty(t, DiagnosticCode(errors.New(SQLInvalidCode)))
}
