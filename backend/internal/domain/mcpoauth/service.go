package mcpoauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/audit"
	authdomain "backend/internal/domain/auth"
	domaincommon "backend/internal/domain/common"
	"backend/internal/mcpaccess"
	"backend/internal/repo"
	"backend/internal/types"
	"golang.org/x/time/rate"
)

type Service struct {
	repo          *repo.McpOAuthRepo
	auth          *authdomain.Service
	audit         *audit.Service
	configuration config.MCPConf
	registration  *rate.Limiter
	authorization *rate.Limiter
}

func (s *Service) BindAudit(auditSvc *audit.Service) { s.audit = auditSvc }

func New(ctx context.Context, auth *authdomain.Service, c config.MCPConf) *Service {
	return &Service{repo: repo.NewMcpOAuthRepo(ctx), auth: auth, configuration: c, registration: rate.NewLimiter(1, 20), authorization: rate.NewLimiter(5, 50)}
}
func (s *Service) Info(ctx context.Context) map[string]any {
	resource, issuer, err := endpoints(ctx, s.configuration)
	if err != nil {
		return map[string]any{"available": false, "reason": err.Error(), "defaultAuthentication": "oauth"}
	}
	return map[string]any{"available": true, "defaultAuthentication": "oauth", "resource": resource, "issuer": issuer, "registration": "dcr", "pkce": "S256", "scopes": supportedScopes, "accessTokenSeconds": int(accessTTL.Seconds()), "refreshRotation": true, "metadataUrl": issuer + "/.well-known/oauth-protected-resource", "localClientsOnly": true}
}
func (s *Service) ResourceMetadata(ctx context.Context) (any, error) {
	resource, issuer, err := endpoints(ctx, s.configuration)
	if err != nil {
		return nil, err
	}
	// 列出可申请的资源范围，实际授权仍由用户主动选择并与当前权限取交集。
	// offline_access 属于授权服务器的刷新能力，不是资源访问范围。
	scopes := make([]string, 0, len(supportedScopes))
	for _, scope := range supportedScopes {
		if scope != "offline_access" {
			scopes = append(scopes, scope)
		}
	}
	return map[string]any{"resource": resource, "resource_name": "Tier0 UNS MCP", "authorization_servers": []string{issuer}, "scopes_supported": scopes, "bearer_methods_supported": []string{"header"}}, nil
}
func (s *Service) ServerMetadata(ctx context.Context) (any, error) {
	_, issuer, err := endpoints(ctx, s.configuration)
	if err != nil {
		return nil, err
	}
	return map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/mcp/oauth/authorize", "token_endpoint": issuer + "/mcp/oauth/token", "registration_endpoint": issuer + "/mcp/oauth/register", "revocation_endpoint": issuer + "/mcp/oauth/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none"}, "revocation_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": supportedScopes, "authorization_response_iss_parameter_supported": true, "client_id_metadata_document_supported": false}, nil
}
func (s *Service) Register(ctx context.Context, input types.McpOAuthRegisterReq) (any, error) {
	if _, _, err := endpoints(ctx, s.configuration); err != nil {
		return nil, err
	}
	if !s.registration.Allow() {
		return nil, &Failure{Code: "temporarily_unavailable", Status: 429}
	}
	if input.TokenEndpointAuthMethod != "" && input.TokenEndpointAuthMethod != "none" {
		return nil, invalid("invalid_client_metadata")
	}
	if len(input.RedirectUris) < 1 || len(input.RedirectUris) > 4 || !subset(input.GrantTypes, []string{"authorization_code", "refresh_token"}) || !subset(input.ResponseTypes, []string{"code"}) {
		return nil, invalid("invalid_client_metadata")
	}
	for _, uri := range input.RedirectUris {
		if !validRedirect(uri) {
			return nil, invalid("invalid_redirect_uri")
		}
	}
	name := strings.TrimSpace(input.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if domaincommon.ExceedMaxLength(name, domaincommon.ConnectionNameMaxLen) {
		return nil, invalid("invalid_client_metadata")
	}
	scope := input.Scope
	if scope == "" {
		scope = strings.Join(supportedScopes, " ")
	}
	scopes, err := scopeList(scope)
	if err != nil || !has(scopes, "uns.read") {
		return nil, invalid("invalid_scope")
	}
	count, err := s.repo.ClientCount(ctx)
	if err != nil {
		return nil, err
	}
	if count >= 5000 {
		return nil, unavailable()
	}
	id, err := secret("mcpc_")
	if err != nil {
		return nil, err
	}
	redirects, _ := json.Marshal(input.RedirectUris)
	client := repo.McpOAuthClient{ClientID: id, Name: name, RedirectURIs: redirects, Scope: strings.Join(scopes, " "), Enabled: true, CreatedAt: now()}
	if err := s.repo.CreateClient(ctx, client); err != nil {
		return nil, err
	}
	return map[string]any{"client_id": id, "client_name": name, "client_id_issued_at": client.CreatedAt / 1000, "redirect_uris": input.RedirectUris, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "scope": client.Scope}, nil
}
func (s *Service) Begin(ctx context.Context, input types.McpOAuthAuthorizeReq) (repo.McpOAuthRequest, string, error) {
	empty := repo.McpOAuthRequest{}
	resource, issuer, err := endpoints(ctx, s.configuration)
	if err != nil {
		return empty, "", err
	}
	if !s.authorization.Allow() {
		return empty, "", &Failure{Code: "temporarily_unavailable", Status: 429}
	}
	if input.ResponseType != "code" || input.CodeChallengeMethod != "S256" || !challengePattern.MatchString(input.CodeChallenge) || len(input.State) == 0 || len(input.State) > 1024 {
		return empty, "", invalid("invalid_request")
	}
	challengeBytes, err := base64.RawURLEncoding.DecodeString(input.CodeChallenge)
	if err != nil || len(challengeBytes) != sha256.Size {
		return empty, "", invalid("invalid_request")
	}
	requestedResource, err := canonical(input.Resource)
	if err != nil || requestedResource != resource {
		return empty, "", invalid("invalid_target")
	}
	client, err := s.repo.Client(ctx, input.ClientId)
	if err != nil {
		return empty, "", invalid("invalid_client")
	}
	var redirects []string
	_ = json.Unmarshal(client.RedirectURIs, &redirects)
	if !redirectMatches(redirects, input.RedirectUri) {
		return empty, "", invalid("invalid_redirect_uri")
	}
	scope := input.Scope
	if scope == "" {
		// 未指定 scope 时沿用客户端注册上限；显式只读请求保持只读。
		// 这只是待确认的候选范围，不会跳过授权页选择或扩大已有 grant。
		scope = client.Scope
	}
	scopes, err := scopeList(scope)
	allowed, _ := scopeList(client.Scope)
	if err != nil || !has(scopes, "uns.read") || !subset(scopes, allowed) {
		return empty, "", invalid("invalid_scope")
	}
	id, err := secret("mcor_")
	if err != nil {
		return empty, "", err
	}
	browser, err := secret("")
	if err != nil {
		return empty, "", err
	}
	pending := repo.McpOAuthRequest{RequestID: id, ClientID: client.ClientID, RedirectURI: input.RedirectUri, Resource: resource, Issuer: issuer, Scope: strings.Join(scopes, " "), State: input.State, Challenge: input.CodeChallenge, BrowserHash: hash(browser), ExpiresAt: now() + requestTTL.Milliseconds(), CreatedAt: now()}
	if err := s.repo.CreateRequest(ctx, pending); err != nil {
		return empty, "", err
	}
	return pending, browser, nil
}
func cookieName(id string) string {
	if len(id) < 16 {
		return "mcp_oauth_invalid"
	}
	return "mcp_oauth_" + id[len(id)-16:]
}
func browserSecret(r *http.Request, id string) string {
	c, err := r.Cookie(cookieName(id))
	if err != nil {
		return ""
	}
	return c.Value
}
func (s *Service) pending(ctx context.Context, id, browser string, lock bool, r *repo.McpOAuthRepo) (repo.McpOAuthRequest, error) {
	if !idPattern.MatchString(id) || !strings.HasPrefix(id, "mcor_") || len(browser) != 64 {
		return repo.McpOAuthRequest{}, invalid("invalid_request")
	}
	row, err := r.Request(ctx, id, lock)
	if err != nil || row.ExpiresAt <= now() || row.UsedAt != 0 || !equal(row.BrowserHash, hash(browser)) {
		return repo.McpOAuthRequest{}, invalid("invalid_request")
	}
	resource, issuer, err := endpoints(ctx, s.configuration)
	if err != nil || row.Resource != resource || row.Issuer != issuer {
		return repo.McpOAuthRequest{}, invalid("invalid_target")
	}
	return row, nil
}
func sessionSubject(ctx context.Context) (contextx.Subject, error) {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.AuthType != "session" {
		return contextx.Subject{}, &Failure{Code: "session_required", Status: 401}
	}
	return subject, nil
}
func (s *Service) Request(ctx context.Context, id, browser string) (any, error) {
	subject, err := sessionSubject(ctx)
	if err != nil {
		return nil, err
	}
	pending, err := s.pending(ctx, id, browser, false, s.repo)
	if err != nil {
		return nil, err
	}
	client, err := s.repo.Client(ctx, pending.ClientID)
	if err != nil {
		return nil, invalid("invalid_client")
	}
	requested, _ := scopeList(pending.Scope)
	limited := mcpaccess.Constrain(subject, requested)
	return map[string]any{"requestId": id, "clientName": client.Name, "clientVerified": false, "resource": pending.Resource, "redirectUri": pending.RedirectURI, "userName": subject.UserName, "requestedScopes": requested, "availableScopes": subjectScopes(subject, requested), "expiresAt": pending.ExpiresAt, "readOnly": !mcpaccess.CanWrite(limited)}, nil
}
func (s *Service) Decide(ctx context.Context, input types.McpOAuthDecisionReq, browser string) (any, error) {
	subject, err := sessionSubject(ctx)
	if err != nil {
		return nil, err
	}
	var redirect string
	err = s.repo.Transaction(ctx, func(tx *repo.McpOAuthRepo) error {
		pending, err := s.pending(ctx, input.RequestId, browser, true, tx)
		if err != nil {
			return err
		}
		client, err := tx.Client(ctx, pending.ClientID)
		if err != nil {
			return invalid("invalid_client")
		}
		requested, _ := scopeList(pending.Scope)
		clientScopes, _ := scopeList(client.Scope)
		if !input.Allow {
			if err := tx.UseRequest(ctx, pending.RequestID, now()); err != nil {
				return err
			}
			redirect = callback(pending.RedirectURI, pending.State, pending.Issuer, "", true)
			return nil
		}
		user, keys, actions, _, err := s.auth.CurrentUser(ctx, subject.UserID)
		if err != nil {
			return &Failure{Code: "access_denied", Status: 403}
		}
		current := contextx.Subject{UserID: user.ID, UserName: user.UserName, AuthType: "session", ResourceKeys: map[string]struct{}{}, UIActions: actions}
		for _, key := range keys {
			current.ResourceKeys[key] = struct{}{}
		}
		scopes, err := scopeList(strings.Join(input.Scopes, " "))
		if err != nil || !has(scopes, "uns.read") || !subset(scopes, intersect(requested, clientScopes)) || !subset(scopes, subjectScopes(current, requested)) {
			return invalid("invalid_scope")
		}
		if has(requested, "offline_access") && has(clientScopes, "offline_access") && !has(scopes, "offline_access") {
			scopes = append(scopes, "offline_access")
		}
		id, err := secret("mcog_")
		if err != nil {
			return err
		}
		code, err := secret("mccd_")
		if err != nil {
			return err
		}
		created := now()
		grant := repo.McpOAuthGrant{GrantID: id, ClientID: client.ClientID, UserID: subject.UserID, Resource: pending.Resource, Issuer: pending.Issuer, Scope: strings.Join(scopes, " "), CreatedAt: created, ExpiresAt: created + grantTTL.Milliseconds()}
		if err := tx.CreateGrant(ctx, grant); err != nil {
			return err
		}
		if err := tx.CreateToken(ctx, repo.McpOAuthToken{TokenHash: hash(code), TokenType: "code", GrantID: id, RedirectURI: pending.RedirectURI, Challenge: pending.Challenge, Scope: grant.Scope, CreatedAt: created, ExpiresAt: created + codeTTL.Milliseconds()}); err != nil {
			return err
		}
		if err := tx.UseRequest(ctx, pending.RequestID, created); err != nil {
			return err
		}
		redirect = callback(pending.RedirectURI, pending.State, pending.Issuer, code, false)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"redirectUri": redirect, "allowed": input.Allow}, nil
}
func (s *Service) subject(ctx context.Context, g repo.McpOAuthGrant, scope string) (contextx.Subject, error) {
	user, keys, actions, _, err := s.auth.CurrentUser(ctx, g.UserID)
	if err != nil {
		return contextx.Subject{}, invalid("invalid_grant")
	}
	allowed, _ := scopeList(scope)
	out := contextx.Subject{UserID: user.ID, UserName: user.UserName, Email: user.Email, AuthType: "mcpOAuth", ResourceKeys: map[string]struct{}{}, UIActions: actions}
	for _, key := range keys {
		out.ResourceKeys[key] = struct{}{}
	}
	out.MCPGrantID = g.GrantID
	return mcpaccess.Constrain(out, allowed), nil
}

// 异步任务保持提交时的范围上限，重新读取 grant 和实际权限，不保存 access token。
func (s *Service) RefreshSubject(ctx context.Context, previous contextx.Subject) (contextx.Subject, error) {
	if previous.MCPRestricted && s.configuration.Disabled {
		return contextx.Subject{}, unavailable()
	}
	if previous.AuthType == "mcpOAuth" {
		g, err := s.repo.Grant(ctx, previous.MCPGrantID, false)
		if err != nil || g.UserID != previous.UserID || g.RevokedAt != 0 || g.ExpiresAt <= now() {
			return contextx.Subject{}, invalid("invalid_grant")
		}
		if _, err := s.repo.Client(ctx, g.ClientID); err != nil {
			return contextx.Subject{}, invalid("invalid_grant")
		}
		granted, _ := scopeList(g.Scope)
		return s.subject(ctx, g, strings.Join(intersect(previous.MCPScopes, granted), " "))
	}
	current, err := s.auth.RefreshSubject(ctx, previous)
	if err != nil {
		return contextx.Subject{}, err
	}
	if previous.MCPRestricted {
		current = mcpaccess.Constrain(current, previous.MCPScopes)
	}
	return current, nil
}
func (s *Service) Authenticate(r *http.Request, raw string) (contextx.Subject, error) {
	if !strings.HasPrefix(raw, "mcat_") || len(raw) != 69 {
		return contextx.Subject{}, invalid("invalid_token")
	}
	resource, issuer, err := endpoints(r.Context(), s.configuration)
	if err != nil {
		return contextx.Subject{}, err
	}
	t, err := s.repo.Token(r.Context(), hash(raw), "access", false)
	if err != nil || t.ExpiresAt <= now() {
		return contextx.Subject{}, invalid("invalid_token")
	}
	g, err := s.repo.Grant(r.Context(), t.GrantID, false)
	if err != nil || g.RevokedAt != 0 || g.ExpiresAt <= now() || g.Resource != resource || g.Issuer != issuer {
		return contextx.Subject{}, invalid("invalid_token")
	}
	if _, err = s.repo.Client(r.Context(), g.ClientID); err != nil {
		return contextx.Subject{}, invalid("invalid_token")
	}
	return s.subject(r.Context(), g, t.Scope)
}
func (s *Service) Grants(ctx context.Context) (any, error) {
	subject, err := sessionSubject(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.Grants(ctx, subject.UserID)
	if err != nil {
		return nil, err
	}
	list := []map[string]any{}
	for _, g := range rows {
		client, err := s.repo.Client(ctx, g.ClientID)
		name := "MCP client"
		if err == nil {
			name = client.Name
		}
		scopes, _ := scopeList(g.Scope)
		list = append(list, map[string]any{"grantId": g.GrantID, "clientName": name, "resource": g.Resource, "scopes": scopes, "createdAt": g.CreatedAt, "expiresAt": g.ExpiresAt, "revokedAt": g.RevokedAt, "expired": g.ExpiresAt <= now()})
	}
	return map[string]any{"list": list}, nil
}
func (s *Service) RevokeGrant(ctx context.Context, id string) (any, error) {
	subject, err := sessionSubject(ctx)
	if err != nil {
		return nil, err
	}
	err = s.repo.Transaction(ctx, func(tx *repo.McpOAuthRepo) error {
		g, err := tx.Grant(ctx, id, true)
		if err != nil || g.UserID != subject.UserID {
			return &Failure{Code: "not_found", Status: 404}
		}
		return tx.RevokeGrant(ctx, id, now())
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"revoked": true}, nil
}
func failure(err error) *Failure {
	var f *Failure
	if errors.As(err, &f) {
		return f
	}
	return &Failure{Code: "server_error", Status: 500}
}
