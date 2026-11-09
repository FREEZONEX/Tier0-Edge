package unsv2

// 对公开 API 验证安全错误投影，未知文本不能伪装成结构化错误码。
import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"testing"

	"backend/internal/domain/sourcestream"
	"backend/internal/infra/edgesql"
	"github.com/jackc/pgx/v5/pgconn"
	assert "github.com/stretchr/testify/require"
)

func TestSourceReadFaultSQLAndDeviceDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{edgesql.SafeError(context.Background(), &pgconn.PgError{Code: "42703", Message: "secret SQL"}), "SOURCE_SQL_OBJECT_NOT_FOUND"},
		{context.DeadlineExceeded, "SOURCE_READ_TIMEOUT"},
		{sourcestream.ErrDeviceSession, "SOURCE_DEVICE_SESSION_FAILED"},
		{edgesql.SafeError(context.Background(), edgesql.ErrResultLimit), "SOURCE_RESOURCE_LIMIT"},
		{edgesql.SafeError(context.Background(), &net.DNSError{Name: "secret hostname"}), "SOURCE_DNS_FAILED"},
		{edgesql.SafeError(context.Background(), &tls.CertificateVerificationError{Err: errors.New("secret certificate")}), "SOURCE_TLS_FAILED"},
	} {
		fault := sourceReadFault(tc.err)
		assert.NotNil(t, fault)
		assert.Equal(t, tc.code, fault.Code)
		assert.NotContains(t, fault.Message, "secret")
		assert.Empty(t, fault.Path)
		assert.Empty(t, fault.Reason)
	}
	assert.Nil(t, sourceReadFault(errors.New("SOURCE_SQL_AUTH_FAILED secret")))
}
