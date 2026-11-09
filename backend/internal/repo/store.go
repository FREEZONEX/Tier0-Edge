package repo

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	"backend/internal/config"

	"gorm.io/gorm"
)

//go:embed migrations/schema.sql migrations/app_gateway_routes.sql migrations/app_api_keys.sql migrations/sink_schema.sql migrations/versions/common/*.sql migrations/versions/sink/*.sql
var migrationFS embed.FS

// ErrNotFound is returned when a single-row lookup or a conditional
// update/delete matches no row. It aliases gorm.ErrRecordNotFound so callers
// may use errors.Is(err, repo.ErrNotFound) regardless of the driver.
var ErrNotFound = gorm.ErrRecordNotFound

var ErrInvalidArgument = errors.New("invalid argument")
var ErrConflict = errors.New("conflict")

var ErrDuplicate = errors.New("common.duplicateResource")
var ErrUserAccountDuplicate = errors.New("UserManagement.accountDuplicate")
var ErrUserEmailDuplicate = errors.New("UserManagement.emailDuplicate")

var (
	commonConn *gorm.DB
	unsConn    *gorm.DB
)

// Store owns the gorm connection lifecycle and schema bootstrap only.
// Business data access is done through explicit NewXxxRepo(ctx) instances.
type Store struct {
	commonDB                   *gorm.DB
	unsDB                      *gorm.DB
	timeseriesRetentionYears   int
	sampleCompressionAfterDays int
	userContactCipher          *userContactCipher
}

func Open(ctx context.Context, c config.DatabaseConf) (*Store, error) {
	ctx, cancel := context.WithTimeout(ctx, postgresStartupTimeout)
	defer cancel()
	contactCipher, err := newUserContactCipher()
	if err != nil {
		return nil, err
	}
	commonDB, err := openPostgres(ctx, c.UnsDbUrl)
	if err != nil {
		return nil, err
	}
	unsDSN := strings.TrimSpace(c.SinkDbUrl)
	if unsDSN == "" {
		unsDSN = c.UnsDbUrl
	}
	unsDB := commonDB
	if strings.TrimSpace(unsDSN) != strings.TrimSpace(c.UnsDbUrl) {
		unsDB, err = openPostgres(ctx, unsDSN)
		if err != nil {
			closeDB(commonDB)
			return nil, err
		}
	}
	commonConn = commonDB
	unsConn = unsDB
	activeUserContactCipher = contactCipher
	return &Store{
		commonDB:                   commonDB,
		unsDB:                      unsDB,
		timeseriesRetentionYears:   config.NormalizeTimeseriesRetentionYears(c.TimeseriesRetentionYears),
		sampleCompressionAfterDays: config.SampleCompressionAfterDays(),
		userContactCipher:          contactCipher,
	}, nil
}

// SetStoreForTest 覆盖包级连接 fallback（GetCommonConn/GetUnsConn 的后备连接），
// 供测试基建（internal/testkit）把仓库查询注入到独立测试库。仅测试代码调用；
// 不影响已打开 Store 实例自身的连接，也不接管其生命周期。
func SetStoreForTest(common, uns *gorm.DB) {
	commonConn = common
	unsConn = uns
}

func GetCommonConn(in any) *gorm.DB {
	if store, ok := in.(*Store); ok {
		return store.CommonDB()
	}
	return getConn(in, commonConn)
}

func GetUnsConn(in any) *gorm.DB {
	if ctx, ok := in.(context.Context); ok {
		if tx, ok := ctx.Value(unsTransactionKey{}).(*gorm.DB); ok {
			return tx.WithContext(ctx)
		}
	}
	if store, ok := in.(*Store); ok {
		return store.UnsDB()
	}
	return getConn(in, unsConn)
}

func getConn(in any, fallback *gorm.DB) *gorm.DB {
	if db, ok := in.(*gorm.DB); ok {
		return db
	}
	ctx, ok := in.(context.Context)
	if !ok {
		if in == nil {
			ctx = context.Background()
		} else if fallback != nil {
			conn := fallback.Session(&gorm.Session{})
			conn.Error = fmt.Errorf("%w: repository input must be context.Context or *gorm.DB", ErrInvalidArgument)
			return conn
		} else {
			return nil
		}
	}
	if fallback == nil {
		return nil
	}
	return fallback.WithContext(ctx)
}

func (s *Store) DB() *gorm.DB {
	return s.CommonDB()
}

type unsTransactionKey struct{}

// WithUnsTransaction shares an UNS transaction without rebinding platform data.
func WithUnsTransaction(ctx context.Context, tx *gorm.DB) context.Context {
	if tx != nil && tx.Statement != nil && tx.Statement.Context != nil {
		if pending, ok := tx.Statement.Context.Value(resourceNotificationsKey{}).(*resourceNotifications); ok {
			ctx = context.WithValue(ctx, resourceNotificationsKey{}, pending)
		}
	}
	return context.WithValue(ctx, unsTransactionKey{}, tx)
}

func (s *Store) CommonDB() *gorm.DB {
	if s == nil {
		return nil
	}
	return s.commonDB
}

func (s *Store) UnsDB() *gorm.DB {
	if s == nil {
		return nil
	}
	if s.unsDB != nil {
		return s.unsDB
	}
	return s.commonDB
}

func (s *Store) Close() {
	if s == nil {
		return
	}
	if s.unsDB != nil && s.unsDB != s.commonDB {
		closeDB(s.unsDB)
	}
	if s.commonDB != nil {
		closeDB(s.commonDB)
	}
	if commonConn == s.commonDB {
		commonConn = nil
	}
	if unsConn == s.unsDB {
		unsConn = nil
	}
	if activeUserContactCipher == s.userContactCipher {
		activeUserContactCipher = nil
	}
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.commonDB == nil {
		return errors.New("database not initialized")
	}
	if err := pingDB(ctx, s.commonDB); err != nil {
		return err
	}
	if s.unsDB != nil && s.unsDB != s.commonDB {
		if err := pingDB(ctx, s.unsDB); err != nil {
			return fmt.Errorf("uns sink database: %w", err)
		}
	}
	return nil
}

func (s *Store) Migrate(ctx context.Context) error {
	if err := s.preflightMigrations(ctx); err != nil {
		return err
	}
	sqlBytes, err := migrationFS.ReadFile("migrations/schema.sql")
	if err != nil {
		return err
	}
	if err := s.commonDB.WithContext(ctx).Exec(string(sqlBytes)).Error; err != nil {
		return err
	}
	appGatewaySQLBytes, err := migrationFS.ReadFile("migrations/app_gateway_routes.sql")
	if err != nil {
		return err
	}
	if err := s.commonDB.WithContext(ctx).Exec(string(appGatewaySQLBytes)).Error; err != nil {
		return err
	}
	appApiKeySQLBytes, err := migrationFS.ReadFile("migrations/app_api_keys.sql")
	if err != nil {
		return err
	}
	if err := s.commonDB.WithContext(ctx).Exec(string(appApiKeySQLBytes)).Error; err != nil {
		return err
	}
	sinkSQLBytes, err := migrationFS.ReadFile("migrations/sink_schema.sql")
	if err != nil {
		return err
	}
	if err := s.UnsDB().WithContext(ctx).Exec(string(sinkSQLBytes)).Error; err != nil {
		return err
	}
	if err := s.runVersionedMigrations(ctx, "common", s.commonDB, "migrations/versions/common"); err != nil {
		return err
	}
	if err := s.runVersionedMigrations(ctx, "sink", s.UnsDB(), "migrations/versions/sink"); err != nil {
		return err
	}
	// The baseline also creates Fleet runtime tables. On a fresh database the
	// ACL helper only exists after V3.0.0_016; converge permissions afterwards.
	if err := s.UnsDB().WithContext(ctx).Exec("SELECT public.protect_stream_runtime_storage()").Error; err != nil {
		return err
	}
	if err := s.migrateUserContacts(ctx); err != nil {
		return err
	}
	if err := s.syncSampleTimeseriesPolicy(ctx); err != nil {
		return err
	}
	return s.syncTimeseriesRetentionPolicy(ctx)
}

func pingDB(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
