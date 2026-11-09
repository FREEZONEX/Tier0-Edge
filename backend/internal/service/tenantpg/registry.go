package tenantpg

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Config Registry 配置
type Config struct {
	UnsDbUrl       string
	RoleSecret     string
	ConnectTimeout time.Duration
	MaxOpenConns   int
	MaxIdleConns   int
	ConnCacheSize  int
}

// Registry 管理 admin 短连接与 project DB 连接池缓存
type Registry struct {
	cfg        Config
	baseDSN    string
	adminDB    string
	mu         sync.Mutex
	conns      map[string]*gorm.DB
	shortConns int // 仅用于统计，不限制
}

// NewRegistry 创建 Registry
func NewRegistry(cfg Config) (*Registry, error) {
	if cfg.UnsDbUrl == "" {
		return nil, fmt.Errorf("tenantpg: UnsDbUrl is required")
	}
	u, err := ParsePostgresDSN(cfg.UnsDbUrl)
	if err != nil {
		return nil, fmt.Errorf("tenantpg: invalid UnsDbUrl: %w", err)
	}
	adminDB := u.Path
	if adminDB == "" || adminDB == "/" {
		adminDB = "postgres"
	} else {
		adminDB = adminDB[1:] // 去掉前导 /
	}
	// Preserve the driver's effective base database before explicit project
	// connections remove this query override.
	if configuredDatabase := u.Query().Get("dbname"); configuredDatabase != "" {
		adminDB = configuredDatabase
	}
	return &Registry{
		cfg:     cfg,
		baseDSN: cfg.UnsDbUrl,
		adminDB: adminDB,
		conns:   make(map[string]*gorm.DB),
	}, nil
}

// GetAdminDB 打开 admin database 的短连接（用于 CREATE DATABASE / CREATE ROLE）
func (r *Registry) GetAdminDB(ctx context.Context) (*gorm.DB, func(), error) {
	return r.openShortConn(ctx, r.adminDB)
}

// GetAdminProjectDB 打开指定 project database 的 admin 短连接（用于 CREATE SCHEMA / GRANT）
func (r *Registry) GetAdminProjectDB(ctx context.Context, projectDB string) (*gorm.DB, func(), error) {
	return r.openShortConn(ctx, projectDB)
}

// GetProjectConn 获取指定 project database 的连接池（使用受限 role），带简单缓存
func (r *Registry) GetProjectConn(ctx context.Context, projectDB string) (*gorm.DB, error) {
	if projectDB == "" {
		return nil, fmt.Errorf("tenantpg: projectDB is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if db, ok := r.conns[projectDB]; ok {
		return db, nil
	}

	var connDSN string
	var err error
	if r.cfg.RoleSecret != "" {
		user, pass, credErr := DeriveCredentials(r.cfg.RoleSecret, projectDB)
		if credErr != nil {
			return nil, fmt.Errorf("tenantpg: derive credentials: %w", credErr)
		}
		connDSN, err = BuildProjectConnDSNWithCreds(r.baseDSN, projectDB, user, pass)
	} else {
		connDSN, err = BuildProjectConnDSN(r.baseDSN, projectDB)
	}
	if err != nil {
		return nil, err
	}

	db, err := r.dialWithDSN(connDSN, projectDB)
	if err != nil {
		return nil, err
	}

	// 简单 LRU：超过容量时随机淘汰一个
	if r.cfg.ConnCacheSize > 0 && len(r.conns) >= r.cfg.ConnCacheSize {
		for k, old := range r.conns {
			if sqlDB, err := old.DB(); err == nil {
				_ = sqlDB.Close()
			}
			delete(r.conns, k)
			break
		}
	}
	r.conns[projectDB] = db
	return db, nil
}

// CloseProjectConn 关闭指定 project database 的缓存连接
func (r *Registry) CloseProjectConn(projectDB string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db, ok := r.conns[projectDB]; ok {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		delete(r.conns, projectDB)
	}
	return nil
}

// Close 关闭所有缓存连接
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, db := range r.conns {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	r.conns = make(map[string]*gorm.DB)
	return nil
}

func (r *Registry) openShortConn(ctx context.Context, dbName string) (*gorm.DB, func(), error) {
	dsn, err := BuildProjectConnDSN(r.baseDSN, dbName)
	if err != nil {
		return nil, nil, err
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("tenantpg: open %s: %w", dbName, err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("tenantpg: gorm.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(0)
	return gdb.WithContext(ctx), func() { _ = sqlDB.Close() }, nil
}

func (r *Registry) dialWithDSN(connDSN, dbName string) (*gorm.DB, error) {
	gdb, err := gorm.Open(postgres.Open(connDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("tenantpg: open %s: %w", dbName, err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("tenantpg: gorm.DB: %w", err)
	}
	maxOpen := r.cfg.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 5
	}
	maxIdle := r.cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 2
	}
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)
	sqlDB.SetConnMaxLifetime(1 * time.Hour)

	timeout := r.cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("tenantpg: ping %s: %w", dbName, err)
	}
	return gdb, nil
}
