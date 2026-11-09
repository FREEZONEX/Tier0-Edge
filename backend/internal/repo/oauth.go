package repo

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

type OAuthClient struct {
	ID                     int64           `gorm:"column:id;primaryKey" json:"id"`
	ClientID               string          `gorm:"column:client_id" json:"clientId"`
	ClientName             string          `gorm:"column:client_name" json:"clientName"`
	ClientType             string          `gorm:"column:client_type" json:"clientType"`
	AppID                  int64           `gorm:"column:app_id" json:"appId"`
	AppKey                 string          `gorm:"column:app_key" json:"appKey"`
	RedirectURIs           json.RawMessage `gorm:"column:redirect_uris;type:jsonb" json:"redirectUris"`
	AllowedOrigins         json.RawMessage `gorm:"column:allowed_origins;type:jsonb" json:"allowedOrigins"`
	Scopes                 json.RawMessage `gorm:"column:scopes;type:jsonb" json:"scopes"`
	Trusted                bool            `gorm:"column:trusted" json:"trusted"`
	Enabled                bool            `gorm:"column:enabled" json:"enabled"`
	SystemBuiltin          bool            `gorm:"column:system_builtin" json:"systemBuiltin"`
	AccessTokenTTLSeconds  int             `gorm:"column:access_token_ttl_seconds" json:"accessTokenTtlSeconds"`
	RefreshTokenEnabled    bool            `gorm:"column:refresh_token_enabled" json:"refreshTokenEnabled"`
	RefreshTokenTTLSeconds int             `gorm:"column:refresh_token_ttl_seconds" json:"refreshTokenTtlSeconds"`
	NoDelTime
}

func (OAuthClient) TableName() string { return "sys_oauth_client" }

type sysOAuthToken struct {
	ID                  int64  `gorm:"column:id;primaryKey"`
	TokenHash           string `gorm:"column:token_hash"`
	TokenType           string `gorm:"column:token_type"`
	ClientID            string `gorm:"column:client_id"`
	UserID              int64  `gorm:"column:user_id"`
	RedirectURI         string `gorm:"column:redirect_uri"`
	Scope               string `gorm:"column:scope"`
	CodeChallenge       string `gorm:"column:code_challenge"`
	CodeChallengeMethod string `gorm:"column:code_challenge_method"`
	ExpiresTime         int64  `gorm:"column:expires_time"`
	UsedTime            int64  `gorm:"column:used_time"`
	RevokedTime         int64  `gorm:"column:revoked_time"`
	CreateTime
}

func (sysOAuthToken) TableName() string { return "sys_oauth_token" }

type OAuthRepo struct{ db *gorm.DB }

func NewOAuthRepo(in any) *OAuthRepo { return &OAuthRepo{db: GetCommonConn(in)} }

func (r *OAuthRepo) CreateOAuthClient(ctx context.Context, client OAuthClient) (OAuthClient, error) {
	if client.ClientID == "" {
		client.ClientID = "oc_" + randomHex(16)
	}
	if len(client.RedirectURIs) == 0 {
		client.RedirectURIs = json.RawMessage(`[]`)
	}
	if len(client.AllowedOrigins) == 0 {
		client.AllowedOrigins = json.RawMessage(`[]`)
	}
	if len(client.Scopes) == 0 {
		client.Scopes = json.RawMessage(`[]`)
	}
	if client.ClientType == "" {
		client.ClientType = "public"
	}
	if client.AccessTokenTTLSeconds == 0 {
		client.AccessTokenTTLSeconds = 3600
	}
	client.Trusted = false
	client.Enabled = true
	client.SystemBuiltin = false
	client.RefreshTokenEnabled = false
	client.RefreshTokenTTLSeconds = 0
	err := r.db.WithContext(ctx).Create(&client).Error
	return client, normalizeDBError(err)
}

func (r *OAuthRepo) ListOAuthClients(ctx context.Context) ([]OAuthClient, error) {
	var out []OAuthClient
	err := r.db.WithContext(ctx).Order("id DESC").Find(&out).Error
	return out, err
}

func (r *OAuthRepo) UpdateOAuthClient(ctx context.Context, id int64, client OAuthClient) (OAuthClient, error) {
	if len(client.RedirectURIs) == 0 {
		client.RedirectURIs = json.RawMessage(`[]`)
	}
	if len(client.AllowedOrigins) == 0 {
		client.AllowedOrigins = json.RawMessage(`[]`)
	}
	var out OAuthClient
	res := r.db.WithContext(ctx).Model(&out).
		Clauses(returningAll()).
		Where("id = ?", id).
		Updates(touchByValues(map[string]any{
			"client_name":     client.ClientName,
			"app_id":          client.AppID,
			"redirect_uris":   client.RedirectURIs,
			"allowed_origins": client.AllowedOrigins,
		}, client.UpdatedBy, 0))
	if res.Error != nil {
		return OAuthClient{}, res.Error
	}
	if res.RowsAffected == 0 {
		return OAuthClient{}, ErrNotFound
	}
	return out, nil
}

func (r *OAuthRepo) DeleteOAuthClient(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Model(&OAuthClient{}).
		Where("id = ? AND system_builtin = false", id).
		Updates(touchValues(map[string]any{"enabled": false}, 0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *OAuthRepo) GetOAuthClientByClientID(ctx context.Context, clientID string) (OAuthClient, error) {
	var client OAuthClient
	err := r.db.WithContext(ctx).Where("client_id = ? AND enabled = true", clientID).Take(&client).Error
	return client, err
}

func (r *OAuthRepo) CreateOAuthCode(ctx context.Context, clientID string, userID int64, redirectURI, scope, challenge, method string) (string, error) {
	now := time.Now().UTC().UnixMilli()
	code := "ocd_" + randomHex(24)
	row := sysOAuthToken{
		TokenHash:           hashSecret(code),
		TokenType:           "code",
		ClientID:            clientID,
		UserID:              userID,
		RedirectURI:         redirectURI,
		Scope:               scope,
		CodeChallenge:       challenge,
		CodeChallengeMethod: method,
		ExpiresTime:         now + 5*60*1000,
	}
	row.CreatedTime = repoTimeFromMilli(now)
	err := r.db.WithContext(ctx).Create(&row).Error
	return code, normalizeDBError(err)
}

func (r *OAuthRepo) ConsumeOAuthCode(ctx context.Context, code, clientID, redirectURI string) (int64, string, error) {
	now := time.Now().UTC().UnixMilli()
	var userID int64
	var scope string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var token sysOAuthToken
		if err := tx.Where("token_hash = ? AND token_type = 'code' AND client_id = ? AND redirect_uri = ? AND used_time = 0 AND revoked_time = 0 AND expires_time > ?",
			hashSecret(code), clientID, redirectURI, now).Take(&token).Error; err != nil {
			return err
		}
		userID = token.UserID
		scope = token.Scope
		return tx.Model(&sysOAuthToken{}).Where("id = ?", token.ID).Update("used_time", now).Error
	})
	if err != nil {
		return 0, "", err
	}
	return userID, scope, nil
}

func (r *OAuthRepo) CreateOAuthAccessToken(ctx context.Context, clientID string, userID int64, scope string, ttlSeconds int) (string, int, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	now := time.Now().UTC().UnixMilli()
	token := "oat_" + randomHex(32)
	row := sysOAuthToken{
		TokenHash:   hashSecret(token),
		TokenType:   "access",
		ClientID:    clientID,
		UserID:      userID,
		Scope:       scope,
		ExpiresTime: now + int64(ttlSeconds)*1000,
	}
	row.CreatedTime = repoTimeFromMilli(now)
	err := r.db.WithContext(ctx).Create(&row).Error
	return token, ttlSeconds, normalizeDBError(err)
}

func (r *OAuthRepo) ValidateOAuthAccessToken(ctx context.Context, raw string) (int64, error) {
	var row sysOAuthToken
	err := r.db.WithContext(ctx).
		Select("user_id").
		Where("token_hash = ? AND token_type = 'access' AND revoked_time = 0 AND expires_time > ?", hashSecret(raw), time.Now().UTC().UnixMilli()).
		Take(&row).Error
	return row.UserID, err
}
