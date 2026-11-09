package repo

// 验证数据库启动等待：暂态恢复、永久拒绝、取消和截止时间；错误保留
// Is/As 语义但不泄漏驱动中的连接凭据。纯控制逻辑不连接业务数据库。
import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestPostgresStartup_TransientClassification(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		retry bool
	}{
		{"recovery", fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "57P03"}), true},
		{"password", &pgconn.PgError{Code: "28P01"}, false},
		{"authorization", &pgconn.PgError{Code: "28000"}, false},
		{"missing database", &pgconn.PgError{Code: "3D000"}, false},
		{"migration", &pgconn.PgError{Code: "42601"}, false},
		{"refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, true},
		{"reset", syscall.ECONNRESET, true},
		{"timeout", &net.DNSError{IsTimeout: true}, true},
		{"missing host", &net.DNSError{IsNotFound: true}, false},
		{"unknown", errors.New("unknown failure"), false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.retry, transientPostgresStartup(tc.err)) })
	}
}

func TestPostgresStartup_RecoveryAndPermanentFailure(t *testing.T) {
	attempts := 0
	err := waitPostgresStartup(context.Background(), func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), postgresConnectTimeout)
		attempts++
		if attempts < 3 {
			return &pgconn.PgError{Code: "57P03"}
		}
		return nil
	}, time.Millisecond, 2*time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
	denied := &pgconn.PgError{Code: "28P01"}
	attempts = 0
	err = waitPostgresStartup(context.Background(), func(context.Context) error {
		attempts++
		return denied
	}, time.Millisecond, time.Millisecond)
	require.ErrorIs(t, err, denied)
	require.Equal(t, 1, attempts)
}

func TestPostgresStartup_CancelAndDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitPostgresStartup(ctx, func(context.Context) error { t.Fatal("canceled before ping"); return nil }, time.Second, time.Second)
	require.ErrorIs(t, err, context.Canceled)
	ctx, cancel = context.WithCancel(context.Background())
	err = waitPostgresStartup(ctx, func(context.Context) error { cancel(); return syscall.ECONNREFUSED }, time.Second, time.Second)
	require.ErrorIs(t, err, context.Canceled)

	ctx, cancel = context.WithCancel(context.Background())
	pinged := make(chan struct{})
	go func() {
		<-pinged
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	err = waitPostgresStartup(ctx, func(context.Context) error { close(pinged); return syscall.ECONNREFUSED }, time.Second, time.Second)
	require.ErrorIs(t, err, context.Canceled)

	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	recovery := &pgconn.PgError{Code: "57P03"}
	err = waitPostgresStartup(ctx, func(context.Context) error { return recovery }, time.Second, time.Second)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, recovery, pgErr)

	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = waitPostgresStartup(ctx, func(attempt context.Context) error { <-attempt.Done(); return attempt.Err() }, time.Second, time.Second)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPostgresStartup_RedactsErrors(t *testing.T) {
	secret := "private-password-and-connection-url"
	for _, cause := range []error{
		errors.New(secret),
		&pgconn.PgError{Code: "57P03", Message: secret},
		&pgconn.PgError{Code: "28P01", Message: secret},
		errors.Join(context.Canceled, errors.New(secret)),
		errors.Join(context.DeadlineExceeded, errors.New(secret)),
	} {
		err := &postgresStartupError{cause: cause}
		require.NotContains(t, err.Error(), secret)
		require.NotContains(t, fmt.Sprintf("%+v", err), secret)
		require.ErrorIs(t, err, cause)
	}
	_, err := openPostgres(context.Background(), "postgres://user:private-password-and-connection-url@invalid/%zz")
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret)
}
