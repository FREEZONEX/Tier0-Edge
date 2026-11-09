package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	domaincommon "backend/internal/domain/common"
	"backend/internal/repo"
)

const maxRedirectURILength = 512

type Service struct {
	oauth *repo.OAuthRepo
	iam   *repo.IAMRepo
}

type ClientCommand struct {
	ID             int64
	ClientName     string
	RedirectURIs   []string
	AllowedOrigins []string
	AppID          int64
	UserID         int64
}

type AuthorizeCommand struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	UserID              int64
}

type TokenCommand struct {
	GrantType    string
	Code         string
	RedirectURI  string
	ClientID     string
	CodeVerifier string
}

func New(ctx context.Context) *Service {
	return &Service{oauth: repo.NewOAuthRepo(ctx), iam: repo.NewIAMRepo(ctx)}
}

func (s *Service) ListClients(ctx context.Context) (map[string]any, error) {
	items, err := s.oauth.ListOAuthClients(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]any, 0, len(items))
	for _, item := range items {
		list = append(list, item)
	}
	return map[string]any{"list": list, "total": len(list)}, nil
}

func (s *Service) CreateClient(ctx context.Context, cmd ClientCommand) (map[string]any, error) {
	clientName := strings.TrimSpace(cmd.ClientName)
	if clientName == "" || domaincommon.ExceedMaxLength(clientName, domaincommon.ConnectionNameMaxLen) {
		return nil, ErrInvalid
	}
	redirectURIs, err := normalizeRedirectURIs(cmd.RedirectURIs)
	if err != nil {
		return nil, err
	}
	redirects, _ := json.Marshal(redirectURIs)
	origins, _ := json.Marshal(cmd.AllowedOrigins)
	req := repo.OAuthClient{
		ClientName:     clientName,
		AppID:          cmd.AppID,
		RedirectURIs:   redirects,
		AllowedOrigins: origins,
	}
	req.CreatedBy = cmd.UserID
	client, err := s.oauth.CreateOAuthClient(ctx, req)
	if err != nil {
		return nil, err
	}
	return map[string]any{"client": client}, nil
}

func (s *Service) UpdateClient(ctx context.Context, cmd ClientCommand) (map[string]any, error) {
	clientName := strings.TrimSpace(cmd.ClientName)
	if clientName == "" || domaincommon.ExceedMaxLength(clientName, domaincommon.ConnectionNameMaxLen) {
		return nil, ErrInvalid
	}
	redirectURIs, err := normalizeRedirectURIs(cmd.RedirectURIs)
	if err != nil {
		return nil, err
	}
	redirects, _ := json.Marshal(redirectURIs)
	origins, _ := json.Marshal(cmd.AllowedOrigins)
	req := repo.OAuthClient{
		ClientName:     clientName,
		AppID:          cmd.AppID,
		RedirectURIs:   redirects,
		AllowedOrigins: origins,
	}
	req.UpdatedBy = cmd.UserID
	client, err := s.oauth.UpdateOAuthClient(ctx, cmd.ID, req)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return map[string]any{"client": client}, nil
}

func (s *Service) DeleteClient(ctx context.Context, id int64) (map[string]any, error) {
	if err := s.oauth.DeleteOAuthClient(ctx, id); err != nil {
		return nil, normalizeNotFound(err)
	}
	return map[string]any{"deleted": true}, nil
}

func (s *Service) Authorize(ctx context.Context, cmd AuthorizeCommand) (map[string]any, error) {
	if strings.ToLower(cmd.ResponseType) != "code" || cmd.UserID == 0 {
		return nil, ErrInvalid
	}
	client, err := s.oauth.GetOAuthClientByClientID(ctx, cmd.ClientID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if !redirectAllowed(client.RedirectURIs, cmd.RedirectURI) {
		return nil, ErrInvalidRedirect
	}
	code, err := s.oauth.CreateOAuthCode(ctx, client.ClientID, cmd.UserID, cmd.RedirectURI, cmd.Scope, cmd.CodeChallenge, cmd.CodeChallengeMethod)
	if err != nil {
		return nil, err
	}
	redirect := cmd.RedirectURI
	if redirect != "" {
		u, err := url.Parse(redirect)
		if err == nil {
			q := u.Query()
			q.Set("code", code)
			if cmd.State != "" {
				q.Set("state", cmd.State)
			}
			u.RawQuery = q.Encode()
			redirect = u.String()
		}
	}
	return map[string]any{"code": code, "state": cmd.State, "redirectUri": redirect}, nil
}

func (s *Service) Token(ctx context.Context, cmd TokenCommand) (map[string]any, error) {
	if strings.ToLower(cmd.GrantType) != "authorization_code" {
		return nil, ErrInvalid
	}
	client, err := s.oauth.GetOAuthClientByClientID(ctx, cmd.ClientID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	userID, scope, err := s.oauth.ConsumeOAuthCode(ctx, cmd.Code, cmd.ClientID, cmd.RedirectURI)
	if err != nil {
		return nil, ErrInvalidCode
	}
	token, ttl, err := s.oauth.CreateOAuthAccessToken(ctx, client.ClientID, userID, scope, client.AccessTokenTTLSeconds)
	if err != nil {
		return nil, err
	}
	return map[string]any{"accessToken": token, "tokenType": "Bearer", "expiresIn": ttl, "scope": scope}, nil
}

func (s *Service) UserInfo(ctx context.Context, userID int64) (map[string]any, error) {
	info, err := s.iam.GetUserInfoMap(ctx, userID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if info == nil {
		return nil, ErrNotFound
	}
	// Keep the standard OIDC-style "sub" field that callers (e.g. app gateway) rely on.
	if _, ok := info["sub"]; !ok {
		info["sub"] = info["userId"]
	}
	return info, nil
}

func redirectAllowed(raw json.RawMessage, redirect string) bool {
	if redirect == "" {
		return false
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return false
	}
	for _, item := range list {
		if item == redirect {
			return true
		}
	}
	return false
}

func normalizeRedirectURIs(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if utf8.RuneCountInString(value) > maxRedirectURILength {
			return nil, ErrInvalidRedirect
		}
		out = append(out, value)
	}
	return out, nil
}

func normalizeNotFound(err error) error {
	if errors.Is(err, repo.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

var (
	ErrInvalid         = errors.New("invalid oauth request")
	ErrNotFound        = errors.New("oauth client not found")
	ErrInvalidRedirect = errors.New("invalid redirect uri")
	ErrInvalidCode     = errors.New("invalid authorization code")
)
