package repo

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MCP 授权仅存平台库；原 App OAuth、API Key 和 UNS 数据独立维护。
type McpOAuthClient struct {
	ClientID     string `gorm:"primaryKey"`
	Name         string
	RedirectURIs json.RawMessage `gorm:"type:jsonb"`
	Scope        string
	Enabled      bool
	CreatedAt    int64
}

func (McpOAuthClient) TableName() string { return "sys_mcp_oauth_client" }

type McpOAuthRequest struct {
	RequestID   string `gorm:"primaryKey"`
	ClientID    string
	RedirectURI string
	Resource    string
	Issuer      string
	Scope       string
	State       string
	Challenge   string
	BrowserHash string
	ExpiresAt   int64
	UsedAt      int64
	CreatedAt   int64
}

func (McpOAuthRequest) TableName() string { return "sys_mcp_oauth_request" }

type McpOAuthGrant struct {
	GrantID   string `gorm:"primaryKey"`
	ClientID  string
	UserID    int64
	Resource  string
	Issuer    string
	Scope     string
	CreatedAt int64
	ExpiresAt int64
	RevokedAt int64
}

func (McpOAuthGrant) TableName() string { return "sys_mcp_oauth_grant" }

type McpOAuthToken struct {
	TokenHash   string `gorm:"primaryKey"`
	TokenType   string
	GrantID     string
	RedirectURI string
	Challenge   string
	Scope       string
	ExpiresAt   int64
	UsedAt      int64
	CreatedAt   int64
}

func (McpOAuthToken) TableName() string { return "sys_mcp_oauth_token" }

type McpOAuthRepo struct{ db *gorm.DB }

func NewMcpOAuthRepo(in any) *McpOAuthRepo { return &McpOAuthRepo{db: GetCommonConn(in)} }
func (r *McpOAuthRepo) Transaction(ctx context.Context, fn func(*McpOAuthRepo) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(NewMcpOAuthRepo(tx)) })
}
func (r *McpOAuthRepo) CreateClient(ctx context.Context, row McpOAuthClient) error {
	return r.db.WithContext(ctx).Create(&row).Error
}
func (r *McpOAuthRepo) Client(ctx context.Context, id string) (McpOAuthClient, error) {
	var row McpOAuthClient
	err := r.db.WithContext(ctx).Where("client_id=? AND enabled=true", id).Take(&row).Error
	return row, err
}
func (r *McpOAuthRepo) ClientCount(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&McpOAuthClient{}).Count(&n).Error
	return n, err
}
func (r *McpOAuthRepo) CreateRequest(ctx context.Context, row McpOAuthRequest) error {
	return r.db.WithContext(ctx).Create(&row).Error
}
func (r *McpOAuthRepo) Request(ctx context.Context, id string, lock bool) (McpOAuthRequest, error) {
	var row McpOAuthRequest
	q := r.db.WithContext(ctx).Where("request_id=?", id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Take(&row).Error
	return row, err
}
func (r *McpOAuthRepo) UseRequest(ctx context.Context, id string, now int64) error {
	res := r.db.WithContext(ctx).Model(&McpOAuthRequest{}).Where("request_id=? AND used_at=0", id).Update("used_at", now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}
func (r *McpOAuthRepo) CreateGrant(ctx context.Context, row McpOAuthGrant) error {
	return r.db.WithContext(ctx).Create(&row).Error
}
func (r *McpOAuthRepo) Grant(ctx context.Context, id string, lock bool) (McpOAuthGrant, error) {
	var row McpOAuthGrant
	q := r.db.WithContext(ctx).Where("grant_id=?", id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Take(&row).Error
	return row, err
}
func (r *McpOAuthRepo) RevokeGrant(ctx context.Context, id string, now int64) error {
	return r.db.WithContext(ctx).Model(&McpOAuthGrant{}).Where("grant_id=? AND revoked_at=0", id).Update("revoked_at", now).Error
}
func (r *McpOAuthRepo) Grants(ctx context.Context, userID int64) ([]McpOAuthGrant, error) {
	var rows []McpOAuthGrant
	err := r.db.WithContext(ctx).Where("user_id=?", userID).Order("revoked_at ASC, expires_at DESC, created_at DESC").Limit(100).Find(&rows).Error
	return rows, err
}
func (r *McpOAuthRepo) CreateToken(ctx context.Context, row McpOAuthToken) error {
	return r.db.WithContext(ctx).Create(&row).Error
}
func (r *McpOAuthRepo) Token(ctx context.Context, hash, kind string, lock bool) (McpOAuthToken, error) {
	var row McpOAuthToken
	q := r.db.WithContext(ctx).Where("token_hash=? AND token_type=?", hash, kind)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Take(&row).Error
	return row, err
}
func (r *McpOAuthRepo) UseToken(ctx context.Context, hash string, now int64) error {
	res := r.db.WithContext(ctx).Model(&McpOAuthToken{}).Where("token_hash=? AND used_at=0", hash).Update("used_at", now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}
