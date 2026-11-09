package mcpoauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/types"
)

func (s *Service) Exchange(ctx context.Context, input types.McpOAuthTokenReq) (any, error) {
	resource, issuer, err := endpoints(ctx, s.configuration)
	if err != nil {
		return nil, err
	}
	target, err := canonical(input.Resource)
	if err != nil || target != resource {
		return nil, invalid("invalid_target")
	}
	client, err := s.repo.Client(ctx, input.ClientId)
	if err != nil {
		return nil, invalid("invalid_client")
	}
	kind, raw := "code", input.Code
	switch input.GrantType {
	case "authorization_code":
		if !verifierPattern.MatchString(input.CodeVerifier) || len(raw) != 69 || !strings.HasPrefix(raw, "mccd_") {
			return nil, invalid("invalid_grant")
		}
	case "refresh_token":
		kind, raw = "refresh", input.RefreshToken
		if len(raw) != 69 || !strings.HasPrefix(raw, "mcrt_") {
			return nil, invalid("invalid_grant")
		}
	default:
		return nil, invalid("unsupported_grant_type")
	}
	var result map[string]any
	var replay bool
	err = s.repo.Transaction(ctx, func(tx *repo.McpOAuthRepo) error {
		t, err := tx.Token(ctx, hash(raw), kind, true)
		if err != nil {
			return invalid("invalid_grant")
		}
		g, err := tx.Grant(ctx, t.GrantID, true)
		if err != nil || g.ClientID != client.ClientID || g.Resource != resource || g.Issuer != issuer || g.RevokedAt != 0 || g.ExpiresAt <= now() {
			return invalid("invalid_grant")
		}
		// 刷新重放撤销必须提交事务；不要用 error 把撤销回滚。
		if t.UsedAt != 0 {
			if kind == "refresh" {
				replay = true
				return tx.RevokeGrant(ctx, g.GrantID, now())
			}
			return invalid("invalid_grant")
		}
		if t.ExpiresAt <= now() {
			return invalid("invalid_grant")
		}
		if kind == "code" {
			b := sha256.Sum256([]byte(input.CodeVerifier))
			if input.RedirectUri != t.RedirectURI || !equal(base64.RawURLEncoding.EncodeToString(b[:]), t.Challenge) {
				return invalid("invalid_grant")
			}
		}
		previous, _ := scopeList(t.Scope)
		scopes := previous
		if input.Scope != "" {
			scopes, err = scopeList(input.Scope)
			if err != nil || !subset(scopes, previous) {
				return invalid("invalid_scope")
			}
		}
		registered, _ := scopeList(client.Scope)
		scopes = intersect(scopes, registered)
		subject, err := s.subject(ctx, g, strings.Join(scopes, " "))
		if err != nil || !contextx.HasResource(subject, "uns.read") {
			return invalid("invalid_grant")
		}
		scopes = subjectScopes(subject, scopes)
		if !has(scopes, "uns.read") {
			return invalid("invalid_scope")
		}
		access, err := secret("mcat_")
		if err != nil {
			return err
		}
		created := now()
		accessExpires := created + accessTTL.Milliseconds()
		if accessExpires > g.ExpiresAt {
			accessExpires = g.ExpiresAt
		}
		if err := tx.CreateToken(ctx, repo.McpOAuthToken{TokenHash: hash(access), TokenType: "access", GrantID: g.GrantID, Scope: strings.Join(scopes, " "), CreatedAt: created, ExpiresAt: accessExpires}); err != nil {
			return err
		}
		result = map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": (accessExpires - created) / 1000, "scope": strings.Join(scopes, " ")}
		if has(scopes, "offline_access") {
			refresh, err := secret("mcrt_")
			if err != nil {
				return err
			}
			if err := tx.CreateToken(ctx, repo.McpOAuthToken{TokenHash: hash(refresh), TokenType: "refresh", GrantID: g.GrantID, Scope: strings.Join(scopes, " "), CreatedAt: created, ExpiresAt: g.ExpiresAt}); err != nil {
				return err
			}
			result["refresh_token"] = refresh
		}
		return tx.UseToken(ctx, t.TokenHash, created)
	})
	if err != nil {
		return nil, err
	}
	if replay {
		return nil, invalid("invalid_grant")
	}
	return result, nil
}

func (s *Service) Revoke(ctx context.Context, input types.McpOAuthRevokeReq) (any, error) {
	if _, _, err := endpoints(ctx, s.configuration); err != nil {
		return nil, err
	}
	// RFC 7009：未知 token 与非匹配 client 不可被当作凭据存在性探针。
	if input.ClientId == "" || len(input.Token) > 256 {
		return map[string]any{}, nil
	}
	kind := "access"
	if strings.HasPrefix(input.Token, "mcrt_") {
		kind = "refresh"
	}
	if strings.HasPrefix(input.Token, "mccd_") {
		kind = "code"
	}
	err := s.repo.Transaction(ctx, func(tx *repo.McpOAuthRepo) error {
		t, err := tx.Token(ctx, hash(input.Token), kind, false)
		if err != nil {
			return nil
		}
		g, err := tx.Grant(ctx, t.GrantID, true)
		if err != nil || g.ClientID != input.ClientId {
			return nil
		}
		return tx.RevokeGrant(ctx, g.GrantID, now())
	})
	return map[string]any{}, err
}
