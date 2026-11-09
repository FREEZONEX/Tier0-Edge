package repo

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	postgresStartupTimeout = 180 * time.Second
	postgresConnectTimeout = 5 * time.Second
)

// Keep connection diagnostics available to errors.Is/As without putting the
// driver's DSN, username or password into startup logs and panic messages.
type postgresStartupError struct{ cause error }

func (e *postgresStartupError) Error() string {
	if errors.Is(e.cause, context.Canceled) {
		return "database startup canceled"
	}
	if errors.Is(e.cause, context.DeadlineExceeded) {
		return "database startup deadline exceeded"
	}
	var pgErr *pgconn.PgError
	if errors.As(e.cause, &pgErr) && pgErr.Code == "57P03" {
		return "database not accepting connections (SQLSTATE 57P03)"
	}
	return "database startup connection failed"
}

func (e *postgresStartupError) Unwrap() error { return e.cause }

func openPostgres(ctx context.Context, dsn string) (*gorm.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, &postgresStartupError{cause: err}
	}
	if cfg.ConnectTimeout <= 0 || cfg.ConnectTimeout > postgresConnectTimeout {
		cfg.ConnectTimeout = postgresConnectTimeout
	}
	sqlDB := stdlib.OpenDB(*cfg)
	// Keep the bounded pool warm after concurrent requests finish together.
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(10)
	if err = waitPostgresStartup(ctx, sqlDB.PingContext, 100*time.Millisecond, 2*time.Second); err != nil {
		_ = sqlDB.Close()
		return nil, &postgresStartupError{cause: err}
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
		DisableAutomaticPing:   true,
	})
	if err != nil {
		_ = sqlDB.Close()
		return nil, &postgresStartupError{cause: err}
	}
	return db, nil
}

// Only connection establishment is retried; migrations and all business work
// start after this returns and are never replayed by the startup policy.
func waitPostgresStartup(ctx context.Context, ping func(context.Context) error, initial, maximum time.Duration) error {
	backoff := initial
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attempt, cancel := context.WithTimeout(ctx, postgresConnectTimeout)
		err := ping(attempt)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return errors.Join(ctx.Err(), err)
		}
		if !transientPostgresStartup(err) {
			return err
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(ctx.Err(), err)
		case <-timer.C:
		}
		backoff = min(backoff*2, maximum)
	}
}

func transientPostgresStartup(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "57P03"
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
