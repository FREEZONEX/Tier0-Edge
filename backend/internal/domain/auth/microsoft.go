package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	domainlicense "backend/internal/domain/license"
	"backend/internal/infra/cache"
	"backend/internal/repo"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

const (
	microsoftProvider          = "microsoft"
	microsoftStateTTL          = 10 * time.Minute
	microsoftPendingTTL        = 15 * time.Minute
	microsoftProcessingLockTTL = 30 * time.Second
	microsoftUsernameLockTTL   = 30 * time.Second
	microsoftLinkMaxAttempts   = int64(5)
	microsoftCommonAuthority   = "common"
	microsoftIssuerTemplate    = "https://login.microsoftonline.com/{tenantid}/v2.0"
)

var (
	ErrMicrosoftSSODisabled      = errors.New("auth.microsoftSsoDisabled")
	ErrMicrosoftSSOUnavailable   = errors.New("auth.microsoftSsoUnavailable")
	ErrMicrosoftStateInvalid     = errors.New("auth.microsoftStateInvalid")
	ErrMicrosoftIdentityInvalid  = errors.New("auth.microsoftIdentityInvalid")
	ErrMicrosoftPendingInvalid   = errors.New("auth.microsoftPendingInvalid")
	ErrMicrosoftUsernameInvalid  = errors.New("auth.microsoftUsernameInvalid")
	ErrMicrosoftUsernameTaken    = errors.New("auth.microsoftUsernameTaken")
	ErrMicrosoftLinkUnauthorized = errors.New("auth.microsoftLinkUnauthorized")
	ErrMicrosoftLinkAttempts     = errors.New("auth.microsoftLinkAttemptsExceeded")
	ErrMicrosoftIdentityConflict = errors.New("auth.microsoftIdentityConflict")
	ErrMicrosoftRegistrationBusy = errors.New("auth.microsoftRegistrationBusy")
)

type MicrosoftConfig struct {
	Enabled      bool
	TenantID     string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type MicrosoftCallbackResult struct {
	Login          *LoginResult
	PendingTicket  string
	PendingBinding string
	RedirectURI    string
}

type MicrosoftAuthorizationResult struct {
	AuthorizationURL string
	BrowserBinding   string
}

type MicrosoftPendingView struct {
	DisplayName       string
	Email             string
	SuggestedUsername string
	MFAAuthenticated  bool
	RedirectURI       string
}

type microsoftOIDC struct {
	config MicrosoftConfig
	cache  *cache.Client

	mu          sync.Mutex
	provider    *oidc.Provider
	oauthConfig *oauth2.Config
}

type microsoftLoginState struct {
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"codeVerifier"`
	RedirectURI  string `json:"redirectUri"`
	BindingHash  string `json:"bindingHash"`
}

type microsoftPending struct {
	Issuer            string `json:"issuer"`
	TenantID          string `json:"tenantId"`
	SubjectID         string `json:"subjectId"`
	DisplayName       string `json:"displayName"`
	Email             string `json:"email"`
	SuggestedUsername string `json:"suggestedUsername"`
	MFAAuthenticated  bool   `json:"mfaAuthenticated"`
	RedirectURI       string `json:"redirectUri"`
	BindingHash       string `json:"bindingHash"`
}

type microsoftClaims struct {
	ObjectID          string   `json:"oid"`
	TenantID          string   `json:"tid"`
	Nonce             string   `json:"nonce"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	Email             string   `json:"email"`
	AuthMethods       []string `json:"amr"`
}

func (s *Service) ConfigureMicrosoft(cacheClient *cache.Client, config MicrosoftConfig) {
	config.TenantID = normalizeMicrosoftAuthority(config.TenantID)
	config.ClientID = strings.TrimSpace(config.ClientID)
	config.ClientSecret = strings.TrimSpace(config.ClientSecret)
	config.RedirectURL = strings.TrimSpace(config.RedirectURL)
	s.microsoft = &microsoftOIDC{config: config, cache: cacheClient}
}

func (s *Service) MicrosoftSSOEnabled() bool {
	return s != nil && s.microsoft != nil && s.microsoft.enabled()
}

func (s *Service) MicrosoftAuthorizationURL(ctx context.Context, redirectURI string) (MicrosoftAuthorizationResult, error) {
	if !s.MicrosoftSSOEnabled() {
		return MicrosoftAuthorizationResult{}, ErrMicrosoftSSODisabled
	}
	provider, oauthConfig, err := s.microsoft.providerConfig(ctx)
	if err != nil {
		return MicrosoftAuthorizationResult{}, fmt.Errorf("%w: %v", ErrMicrosoftSSOUnavailable, err)
	}
	_ = provider
	state, err := randomURLToken(32)
	if err != nil {
		return MicrosoftAuthorizationResult{}, err
	}
	nonce, err := randomURLToken(32)
	if err != nil {
		return MicrosoftAuthorizationResult{}, err
	}
	verifier, err := randomURLToken(48)
	if err != nil {
		return MicrosoftAuthorizationResult{}, err
	}
	stateBinding, err := randomURLToken(32)
	if err != nil {
		return MicrosoftAuthorizationResult{}, err
	}
	loginState := microsoftLoginState{
		Nonce: nonce, CodeVerifier: verifier, RedirectURI: normalizeMicrosoftRedirect(redirectURI),
		BindingHash: hashMicrosoftBinding(stateBinding),
	}
	payload, err := json.Marshal(loginState)
	if err != nil {
		return MicrosoftAuthorizationResult{}, err
	}
	if err := s.microsoft.cache.SetString(ctx, microsoftStateKey(state), string(payload), microsoftStateTTL); err != nil {
		return MicrosoftAuthorizationResult{}, fmt.Errorf("%w: %v", ErrMicrosoftSSOUnavailable, err)
	}
	challenge := sha256.Sum256([]byte(verifier))
	return MicrosoftAuthorizationResult{AuthorizationURL: oauthConfig.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:])),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	), BrowserBinding: stateBinding}, nil
}

func (s *Service) MicrosoftCallback(ctx context.Context, state, code, providerError, binding string) (MicrosoftCallbackResult, error) {
	if !s.MicrosoftSSOEnabled() {
		return MicrosoftCallbackResult{}, ErrMicrosoftSSODisabled
	}
	state = strings.TrimSpace(state)
	if state == "" {
		return MicrosoftCallbackResult{}, ErrMicrosoftStateInvalid
	}
	payload, err := s.microsoft.cache.TakeString(ctx, microsoftStateKey(state))
	if err != nil {
		return MicrosoftCallbackResult{}, ErrMicrosoftStateInvalid
	}
	var loginState microsoftLoginState
	if err := json.Unmarshal([]byte(payload), &loginState); err != nil || !validMicrosoftBinding(loginState.BindingHash, binding) {
		return MicrosoftCallbackResult{}, ErrMicrosoftStateInvalid
	}
	if strings.TrimSpace(providerError) != "" || strings.TrimSpace(code) == "" {
		return MicrosoftCallbackResult{}, ErrMicrosoftIdentityInvalid
	}
	provider, oauthConfig, err := s.microsoft.providerConfig(ctx)
	if err != nil {
		return MicrosoftCallbackResult{}, fmt.Errorf("%w: %v", ErrMicrosoftSSOUnavailable, err)
	}
	token, err := oauthConfig.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", loginState.CodeVerifier))
	if err != nil {
		return MicrosoftCallbackResult{}, ErrMicrosoftIdentityInvalid
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(rawIDToken) == "" {
		return MicrosoftCallbackResult{}, ErrMicrosoftIdentityInvalid
	}
	provider, expectedTenantID, err := s.microsoft.verificationProvider(ctx, provider, rawIDToken)
	if err != nil {
		return MicrosoftCallbackResult{}, ErrMicrosoftIdentityInvalid
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: s.microsoft.config.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		return MicrosoftCallbackResult{}, ErrMicrosoftIdentityInvalid
	}
	var claims microsoftClaims
	if err := idToken.Claims(&claims); err != nil {
		return MicrosoftCallbackResult{}, ErrMicrosoftIdentityInvalid
	}
	if claims.Nonce != loginState.Nonce || !strings.EqualFold(claims.TenantID, expectedTenantID) || strings.TrimSpace(claims.ObjectID) == "" {
		return MicrosoftCallbackResult{}, ErrMicrosoftIdentityInvalid
	}
	identity, err := s.iam.FindExternalIdentity(ctx, microsoftProvider, idToken.Issuer, claims.TenantID, claims.ObjectID)
	if err == nil {
		user, userErr := s.iam.GetUserByID(ctx, identity.UserID)
		if userErr != nil {
			return MicrosoftCallbackResult{}, ErrMicrosoftIdentityConflict
		}
		login, loginErr := s.loginUser(ctx, user, false)
		if loginErr != nil {
			return MicrosoftCallbackResult{}, loginErr
		}
		return MicrosoftCallbackResult{Login: &login, RedirectURI: loginState.RedirectURI}, nil
	}
	if !errors.Is(err, repo.ErrNotFound) {
		return MicrosoftCallbackResult{}, err
	}
	email := strings.TrimSpace(claims.Email)
	if email == "" && strings.Contains(claims.PreferredUsername, "@") {
		email = strings.TrimSpace(claims.PreferredUsername)
	}
	pending := microsoftPending{
		Issuer: idToken.Issuer, TenantID: claims.TenantID, SubjectID: claims.ObjectID,
		DisplayName: strings.TrimSpace(claims.Name), Email: email,
		SuggestedUsername: suggestMicrosoftUsername(claims.PreferredUsername, claims.Name),
		MFAAuthenticated:  containsFold(claims.AuthMethods, "mfa"), RedirectURI: loginState.RedirectURI,
	}
	ticket, err := randomURLToken(32)
	if err != nil {
		return MicrosoftCallbackResult{}, err
	}
	pendingBinding, err := randomURLToken(32)
	if err != nil {
		return MicrosoftCallbackResult{}, err
	}
	pending.BindingHash = hashMicrosoftBinding(pendingBinding)
	pendingPayload, err := json.Marshal(pending)
	if err != nil {
		return MicrosoftCallbackResult{}, err
	}
	if err := s.microsoft.cache.SetString(ctx, microsoftPendingKey(ticket), string(pendingPayload), microsoftPendingTTL); err != nil {
		return MicrosoftCallbackResult{}, fmt.Errorf("%w: %v", ErrMicrosoftSSOUnavailable, err)
	}
	return MicrosoftCallbackResult{PendingTicket: ticket, PendingBinding: pendingBinding, RedirectURI: loginState.RedirectURI}, nil
}

func (s *Service) MicrosoftPending(ctx context.Context, ticket, binding string) (MicrosoftPendingView, error) {
	pending, err := s.microsoftPending(ctx, ticket, binding)
	if err != nil {
		return MicrosoftPendingView{}, err
	}
	return MicrosoftPendingView{
		DisplayName: pending.DisplayName, Email: pending.Email, SuggestedUsername: pending.SuggestedUsername,
		MFAAuthenticated: pending.MFAAuthenticated, RedirectURI: pending.RedirectURI,
	}, nil
}

func (s *Service) MicrosoftUsernameAvailable(ctx context.Context, ticket, binding, username string) (bool, error) {
	if _, err := s.microsoftPending(ctx, ticket, binding); err != nil {
		return false, err
	}
	if err := validateMicrosoftUsername(username); err != nil {
		return false, err
	}
	return s.iam.UsernameAvailable(ctx, strings.TrimSpace(username))
}

func (s *Service) CompleteMicrosoftRegistration(ctx context.Context, ticket, binding, username string) (LoginResult, error) {
	pending, release, err := s.lockMicrosoftPending(ctx, ticket, binding)
	if err != nil {
		return LoginResult{}, err
	}
	succeeded := false
	defer func() { release(succeeded) }()
	if login, found, err := s.loginExistingMicrosoftIdentity(ctx, pending); err != nil {
		return LoginResult{}, err
	} else if found {
		succeeded = true
		return login, nil
	}
	username = strings.TrimSpace(username)
	if err := validateMicrosoftUsername(username); err != nil {
		return LoginResult{}, err
	}
	usernameLock := microsoftUsernameLockKey(username)
	locked, err := s.microsoft.cache.SetNX(ctx, usernameLock, "1", microsoftUsernameLockTTL)
	if err != nil {
		return LoginResult{}, fmt.Errorf("%w: %v", ErrMicrosoftSSOUnavailable, err)
	}
	if !locked {
		return LoginResult{}, ErrMicrosoftRegistrationBusy
	}
	defer func() { _ = s.microsoft.cache.Del(context.Background(), usernameLock) }()
	available, err := s.iam.UsernameAvailable(ctx, username)
	if err != nil {
		return LoginResult{}, err
	}
	if !available {
		return LoginResult{}, ErrMicrosoftUsernameTaken
	}
	if err := s.checkMicrosoftUserQuota(ctx); err != nil {
		return LoginResult{}, err
	}
	randomPassword, err := randomURLToken(48)
	if err != nil {
		return LoginResult{}, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
	if err != nil {
		return LoginResult{}, err
	}
	user, err := s.iam.CreateExternalUser(ctx, repo.UserCreate{
		UserName: username, NickName: pending.DisplayName, Email: pending.Email, Password: string(passwordHash),
		Status: 1, IsRandomPwd: false, RoleID: repo.SystemRoleOperatorID,
	}, pending.identity())
	if errors.Is(err, repo.ErrUserAccountDuplicate) {
		return LoginResult{}, ErrMicrosoftUsernameTaken
	}
	if errors.Is(err, repo.ErrDuplicate) {
		return LoginResult{}, ErrMicrosoftIdentityConflict
	}
	if err != nil {
		return LoginResult{}, err
	}
	login, err := s.loginUser(ctx, user, false)
	if err != nil {
		return LoginResult{}, err
	}
	succeeded = true
	return login, nil
}

func (s *Service) LinkMicrosoftIdentity(ctx context.Context, ticket, binding, username, password string) (LoginResult, error) {
	pending, release, err := s.lockMicrosoftPending(ctx, ticket, binding)
	if err != nil {
		return LoginResult{}, err
	}
	succeeded := false
	defer func() { release(succeeded) }()
	if login, found, err := s.loginExistingMicrosoftIdentity(ctx, pending); err != nil {
		return LoginResult{}, err
	} else if found {
		succeeded = true
		return login, nil
	}
	if s.microsoftLinkAttempts(ctx, ticket) >= microsoftLinkMaxAttempts {
		return LoginResult{}, ErrMicrosoftLinkAttempts
	}
	user, err := s.iam.GetUserByUsername(ctx, strings.TrimSpace(username))
	if err != nil || user.Status != 1 || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
		s.incrementMicrosoftLinkAttempts(ctx, ticket)
		return LoginResult{}, ErrMicrosoftLinkUnauthorized
	}
	if err := s.iam.LinkExternalIdentity(ctx, user.ID, pending.identity()); err != nil {
		if errors.Is(err, repo.ErrDuplicate) {
			return LoginResult{}, ErrMicrosoftIdentityConflict
		}
		return LoginResult{}, err
	}
	login, err := s.loginUser(ctx, user, UsesDefaultPassword(user))
	if err != nil {
		return LoginResult{}, err
	}
	succeeded = true
	return login, nil
}

func (s *Service) loginExistingMicrosoftIdentity(ctx context.Context, pending microsoftPending) (LoginResult, bool, error) {
	identity, err := s.iam.FindExternalIdentity(ctx, microsoftProvider, pending.Issuer, pending.TenantID, pending.SubjectID)
	if errors.Is(err, repo.ErrNotFound) {
		return LoginResult{}, false, nil
	}
	if err != nil {
		return LoginResult{}, false, err
	}
	user, err := s.iam.GetUserByID(ctx, identity.UserID)
	if err != nil {
		return LoginResult{}, false, ErrMicrosoftIdentityConflict
	}
	login, err := s.loginUser(ctx, user, UsesDefaultPassword(user))
	if err != nil {
		return LoginResult{}, false, err
	}
	return login, true, nil
}

func (m *microsoftOIDC) enabled() bool {
	if m == nil || m.cache == nil || !m.config.Enabled {
		return false
	}
	if m.config.ClientID == "" || m.config.ClientSecret == "" || m.config.RedirectURL == "" {
		return false
	}
	u, err := url.Parse(m.config.RedirectURL)
	if err != nil || u.IsAbs() == false || u.Host == "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))
}

func (m *microsoftOIDC) providerConfig(ctx context.Context) (*oidc.Provider, *oauth2.Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.provider != nil && m.oauthConfig != nil {
		return m.provider, m.oauthConfig, nil
	}
	authority := normalizeMicrosoftAuthority(m.config.TenantID)
	issuer := microsoftTenantIssuer(authority)
	providerContext := ctx
	if isMicrosoftCommonAuthority(authority) {
		// Microsoft 的 common 元数据使用 {tenantid} issuer 模板。这里的 Provider
		// 只负责授权和换取 Token；ID Token 会在回调中使用租户专属 Provider 验签。
		providerContext = oidc.InsecureIssuerURLContext(ctx, microsoftIssuerTemplate)
	}
	provider, err := oidc.NewProvider(providerContext, issuer)
	if err != nil {
		return nil, nil, err
	}
	m.provider = provider
	m.oauthConfig = &oauth2.Config{
		ClientID: m.config.ClientID, ClientSecret: m.config.ClientSecret, RedirectURL: m.config.RedirectURL,
		Endpoint: provider.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
	}
	return m.provider, m.oauthConfig, nil
}

func (m *microsoftOIDC) verificationProvider(ctx context.Context, defaultProvider *oidc.Provider, rawIDToken string) (*oidc.Provider, string, error) {
	authority := normalizeMicrosoftAuthority(m.config.TenantID)
	if !isMicrosoftCommonAuthority(authority) {
		return defaultProvider, authority, nil
	}
	tenantID, err := microsoftTenantIDHint(rawIDToken)
	if err != nil {
		return nil, "", err
	}
	provider, err := oidc.NewProvider(ctx, microsoftTenantIssuer(tenantID))
	if err != nil {
		return nil, "", err
	}
	return provider, tenantID, nil
}

func normalizeMicrosoftAuthority(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || isMicrosoftCommonAuthority(value) {
		return microsoftCommonAuthority
	}
	return value
}

func isMicrosoftCommonAuthority(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), microsoftCommonAuthority)
}

func microsoftTenantIssuer(tenantID string) string {
	return fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", url.PathEscape(tenantID))
}

// microsoftTenantIDHint only selects Microsoft's tenant-specific discovery endpoint.
// The returned tenant ID is trusted only after that provider verifies the token signature,
// issuer, audience and the signed tid claim is compared with this value.
func microsoftTenantIDHint(rawIDToken string) (string, error) {
	parts := strings.Split(rawIDToken, ".")
	if len(parts) != 3 {
		return "", errors.New("invalid Microsoft ID token format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("invalid Microsoft ID token payload")
	}
	var claims struct {
		TenantID string `json:"tid"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.New("invalid Microsoft ID token claims")
	}
	tenantID := strings.TrimSpace(claims.TenantID)
	parsed, err := uuid.Parse(tenantID)
	if err != nil || parsed == uuid.Nil || !strings.EqualFold(tenantID, parsed.String()) {
		return "", errors.New("invalid Microsoft tenant ID")
	}
	return parsed.String(), nil
}

func (s *Service) microsoftPending(ctx context.Context, ticket, binding string) (microsoftPending, error) {
	if !s.MicrosoftSSOEnabled() || strings.TrimSpace(ticket) == "" {
		return microsoftPending{}, ErrMicrosoftPendingInvalid
	}
	payload, err := s.microsoft.cache.GetString(ctx, microsoftPendingKey(ticket))
	if err != nil {
		return microsoftPending{}, ErrMicrosoftPendingInvalid
	}
	var pending microsoftPending
	if err := json.Unmarshal([]byte(payload), &pending); err != nil || !validPending(pending) || !validMicrosoftBinding(pending.BindingHash, binding) {
		return microsoftPending{}, ErrMicrosoftPendingInvalid
	}
	return pending, nil
}

func (s *Service) lockMicrosoftPending(ctx context.Context, ticket, binding string) (microsoftPending, func(bool), error) {
	pending, err := s.microsoftPending(ctx, ticket, binding)
	if err != nil {
		return microsoftPending{}, nil, err
	}
	lockKey := microsoftProcessingKey(ticket)
	locked, err := s.microsoft.cache.SetNX(ctx, lockKey, "1", microsoftProcessingLockTTL)
	if err != nil {
		return microsoftPending{}, nil, fmt.Errorf("%w: %v", ErrMicrosoftSSOUnavailable, err)
	}
	if !locked {
		return microsoftPending{}, nil, ErrMicrosoftRegistrationBusy
	}
	release := func(succeeded bool) {
		keys := []string{lockKey}
		if succeeded {
			keys = append(keys, microsoftPendingKey(ticket), microsoftAttemptsKey(ticket))
		}
		_ = s.microsoft.cache.Del(context.Background(), keys...)
	}
	return pending, release, nil
}

func (s *Service) microsoftLinkAttempts(ctx context.Context, ticket string) int64 {
	value, err := s.microsoft.cache.GetString(ctx, microsoftAttemptsKey(ticket))
	if err != nil {
		return 0
	}
	var attempts int64
	_, _ = fmt.Sscan(value, &attempts)
	return attempts
}

func (s *Service) incrementMicrosoftLinkAttempts(ctx context.Context, ticket string) {
	_, _ = s.microsoft.cache.Increment(ctx, microsoftAttemptsKey(ticket), microsoftPendingTTL)
}

func (s *Service) checkMicrosoftUserQuota(ctx context.Context) error {
	licenseService := domainlicense.Default()
	if licenseService == nil {
		return nil
	}
	count, err := s.iam.CountEnabledUsers(ctx)
	if err != nil {
		return err
	}
	return licenseService.CheckQuota("max_users", count)
}

func (p microsoftPending) identity() repo.ExternalIdentity {
	return repo.ExternalIdentity{Provider: microsoftProvider, Issuer: p.Issuer, TenantID: p.TenantID, SubjectID: p.SubjectID}
}

func validPending(p microsoftPending) bool {
	return p.Issuer != "" && p.TenantID != "" && p.SubjectID != "" && p.BindingHash != ""
}

func hashMicrosoftBinding(binding string) string {
	sum := sha256.Sum256([]byte(binding))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func validMicrosoftBinding(expectedHash, binding string) bool {
	if strings.TrimSpace(binding) == "" {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(expectedHash)
	if err != nil {
		return false
	}
	actual := sha256.Sum256([]byte(binding))
	return len(expected) == len(actual) && subtle.ConstantTimeCompare(expected, actual[:]) == 1
}

func validateMicrosoftUsername(username string) error {
	username = strings.TrimSpace(username)
	length := utf8.RuneCountInString(username)
	if length < 3 || length > 60 {
		return ErrMicrosoftUsernameInvalid
	}
	for _, r := range username {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune("._-@", r) {
			continue
		}
		return ErrMicrosoftUsernameInvalid
	}
	return nil
}

func suggestMicrosoftUsername(preferredUsername, displayName string) string {
	candidate := strings.TrimSpace(preferredUsername)
	if before, _, ok := strings.Cut(candidate, "@"); ok {
		candidate = before
	}
	if candidate == "" {
		candidate = strings.TrimSpace(displayName)
	}
	var builder strings.Builder
	for _, r := range candidate {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune("._-", r) {
			builder.WriteRune(r)
		} else if unicode.IsSpace(r) {
			builder.WriteRune('-')
		}
	}
	result := strings.Trim(builder.String(), ".-_")
	if utf8.RuneCountInString(result) > 60 {
		result = string([]rune(result)[:60])
	}
	if utf8.RuneCountInString(result) < 3 {
		return "user"
	}
	return result
}

func normalizeMicrosoftRedirect(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if value == "" || err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") ||
		strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\\r\n") || strings.Contains(strings.ToLower(value), "%5c") {
		return ""
	}
	path := parsed.Path
	if path == "/" || path == "/login" || path == "/tier0-login" {
		return ""
	}
	return value
}

func containsFold(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}

func randomURLToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func microsoftStateKey(state string) string       { return "auth:microsoft:state:" + state }
func microsoftPendingKey(ticket string) string    { return "auth:microsoft:pending:" + ticket }
func microsoftProcessingKey(ticket string) string { return "auth:microsoft:processing:" + ticket }
func microsoftAttemptsKey(ticket string) string   { return "auth:microsoft:attempts:" + ticket }
func microsoftUsernameLockKey(username string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(username))))
	return "auth:microsoft:username:" + base64.RawURLEncoding.EncodeToString(sum[:])
}
