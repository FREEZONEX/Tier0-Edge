package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	domainlicense "backend/internal/domain/license"
	"backend/internal/repo"

	"github.com/golang-jwt/jwt/v5"
	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// LoginSessionSettingSource reads platform-level settings; decoupled from the
// concrete repo so tests can stub the stored login-session duration.
type LoginSessionSettingSource interface {
	GetSetting(ctx context.Context, key string) (string, error)
}

type Service struct {
	iam            *repo.IAMRepo
	oauth          *repo.OAuthRepo
	apiKeys        *repo.APIKeyRepo
	appAPIKeys     *repo.AppApiKeyRepo
	systemSettings LoginSessionSettingSource
	jwtSecret      []byte
	microsoft      *microsoftOIDC
}

type LoginResult struct {
	Token               string
	User                repo.User
	ForceChangePassword bool
	ResourceKeys        []string
	UIActions           []string
	SessionExpireAt     string
}

func New(ctx context.Context, jwtSecret string) *Service {
	return &Service{
		iam:            repo.NewIAMRepo(ctx),
		oauth:          repo.NewOAuthRepo(ctx),
		apiKeys:        repo.NewAPIKeyRepo(ctx),
		appAPIKeys:     repo.NewAppApiKeyRepo(ctx),
		systemSettings: repo.NewSystemSettingRepo(ctx),
		jwtSecret:      []byte(jwtSecret),
	}
}

func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	user, err := s.iam.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return LoginResult{}, errUnauthorized
		}
		return LoginResult{}, err
	}
	if user.Status != 1 {
		return LoginResult{}, errUnauthorized
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return LoginResult{}, errUnauthorized
	}
	return s.loginUser(ctx, user, UsesDefaultPassword(user))
}

func (s *Service) loginUser(ctx context.Context, user repo.User, forceChangePassword bool) (LoginResult, error) {
	if user.Status != 1 {
		return LoginResult{}, errUnauthorized
	}
	if err := s.iam.MarkLogin(ctx, user.ID); err != nil {
		return LoginResult{}, err
	}
	keys, err := s.iam.ResourceKeysForUser(ctx, user.ID)
	if err != nil {
		return LoginResult{}, err
	}
	keys = filterLicenseResourceKeys(keys)
	uiActions, err := s.iam.UIActionsForResourceKeys(ctx, keys)
	if err != nil {
		return LoginResult{}, err
	}
	token, expireAt, err := s.issueToken(ctx, user)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		Token:               token,
		User:                user,
		ForceChangePassword: forceChangePassword,
		ResourceKeys:        keys,
		UIActions:           uiActions,
		SessionExpireAt:     expireAt.UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) CurrentUser(ctx context.Context, userID int64) (repo.User, []string, []string, bool, error) {
	user, keys, uiActions, err := s.loadUserAccess(ctx, userID)
	if err != nil {
		return repo.User{}, nil, nil, false, err
	}
	return user, keys, uiActions, UsesDefaultPassword(user), nil
}

// loadUserAccess reloads current user status and permissions on every request.
// The password reminder is presentation-only and is computed by CurrentUser.
func (s *Service) loadUserAccess(ctx context.Context, userID int64) (repo.User, []string, []string, error) {
	user, err := s.iam.GetUserByID(ctx, userID)
	if err != nil {
		return repo.User{}, nil, nil, err
	}
	if user.Status != 1 {
		return repo.User{}, nil, nil, errUnauthorized
	}
	keys, err := s.iam.ResourceKeysForUser(ctx, user.ID)
	if err != nil {
		return repo.User{}, nil, nil, err
	}
	keys = filterLicenseResourceKeys(keys)
	uiActions, err := s.iam.UIActionsForResourceKeys(ctx, keys)
	if err != nil {
		return repo.User{}, nil, nil, err
	}
	return user, keys, uiActions, nil
}

func (s *Service) AuthenticateRequest(r *http.Request) (contextx.Subject, error) {
	token := bearerToken(r)
	if token == "" {
		if cookie, err := r.Cookie("edge_session"); err == nil {
			token = cookie.Value
		}
	}
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		return contextx.Subject{}, errUnauthorized
	}
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})
	if err != nil || !parsed.Valid {
		if userID, oauthErr := s.oauth.ValidateOAuthAccessToken(r.Context(), token); oauthErr == nil {
			user, keys, uiActions, _, currentErr := s.CurrentUser(r.Context(), userID)
			if currentErr != nil {
				return contextx.Subject{}, errUnauthorized
			}
			return contextx.Subject{
				UserID:       user.ID,
				UserName:     user.UserName,
				Email:        user.Email,
				AuthType:     "oauth",
				ResourceKeys: toSet(keys),
				UIActions:    uiActions,
			}, nil
		}
		return contextx.Subject{}, errUnauthorized
	}
	idFloat, ok := claims["uid"].(float64)
	if !ok {
		return contextx.Subject{}, errUnauthorized
	}
	userID := int64(idFloat)
	user, keys, uiActions, _, err := s.CurrentUser(r.Context(), userID)
	if err != nil {
		return contextx.Subject{}, errUnauthorized
	}
	return contextx.Subject{
		UserID:       user.ID,
		UserName:     user.UserName,
		Email:        user.Email,
		AuthType:     "session",
		ResourceKeys: toSet(keys),
		UIActions:    uiActions,
	}, nil
}

func (s *Service) AuthenticateAPIKey(ctx context.Context, raw string) (contextx.Subject, error) {
	if raw == "" {
		return contextx.Subject{}, errUnauthorized
	}
	key, err := s.apiKeys.ValidateAPIKeyDetail(ctx, raw)
	if err != nil || key.UsageType == repo.APIKeyUsageAppData && strings.TrimSpace(key.AppID) == "" {
		return contextx.Subject{}, errUnauthorized
	}
	return s.apiKeySubject(ctx, key)
}

func (s *Service) apiKeySubject(ctx context.Context, key repo.APIKeyValidation) (contextx.Subject, error) {
	user, ownerKeys, uiActions, err := s.loadUserAccess(ctx, key.OwnerID)
	if err != nil {
		return contextx.Subject{}, err
	}
	effective := intersect(ownerKeys, key.ResourceKeys)
	return contextx.Subject{
		UserID:           user.ID,
		UserName:         user.UserName,
		Email:            user.Email,
		AuthType:         "apiKey",
		APIKeyID:         key.ID,
		APIKeyName:       key.Name,
		APIKeyPrefix:     key.KeyPrefix,
		APIKeyAppID:      key.AppID,
		APIKeyUsageType:  key.UsageType,
		KeyType:          key.KeyType,
		APIKeyPermission: key.Permission,
		ResourceKeys:     toSet(effective),
		UIActions:        uiActions,
	}, nil
}

// ResolveProgramAuthority reloads publication provenance without credentials or
// caller privileges. An API key remains the principal, not its owner's session.
func (s *Service) ResolveProgramAuthority(ctx context.Context, ref repo.ProgramAuthorityRef) (contextx.Subject, error) {
	if ref.ID <= 0 {
		return contextx.Subject{}, errUnauthorized
	}
	switch ref.Kind {
	case "user":
		user, keys, actions, err := s.loadUserAccess(ctx, ref.ID)
		if err != nil {
			return contextx.Subject{}, programAuthorityError(err)
		}
		if len(keys) == 0 {
			return contextx.Subject{}, errUnauthorized
		}
		return contextx.Subject{UserID: user.ID, UserName: user.UserName, Email: user.Email,
			AuthType: "session", ResourceKeys: toSet(keys), UIActions: actions}, nil
	case "api_key":
		key, err := s.apiKeys.ValidateAPIKeyByIDDetail(ctx, ref.ID)
		if err != nil {
			return contextx.Subject{}, programAuthorityError(err)
		}
		if key.AppID != "" || key.UsageType == repo.APIKeyUsageAppData ||
			(key.KeyType != "personal" && key.KeyType != "service") ||
			(key.Permission != "read_only" && key.Permission != "data_writer" && key.Permission != "full_access") {
			return contextx.Subject{}, errUnauthorized
		}
		subject, err := s.apiKeySubject(ctx, key)
		if err != nil {
			return contextx.Subject{}, programAuthorityError(err)
		}
		if len(subject.ResourceKeys) == 0 {
			return contextx.Subject{}, errUnauthorized
		}
		return subject, nil
	default:
		return contextx.Subject{}, errUnauthorized
	}
}

// IsUnauthorized distinguishes confirmed denial from operational lookup errors.
func IsUnauthorized(err error) bool { return errors.Is(err, errUnauthorized) }

func programAuthorityError(err error) error {
	if errors.Is(err, repo.ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
		return errUnauthorized
	}
	return err
}

// RefreshSubject rechecks queued work without persisting a session token/API key.
func (s *Service) RefreshSubject(ctx context.Context, previous contextx.Subject) (contextx.Subject, error) {
	user, keys, actions, err := s.loadUserAccess(ctx, previous.UserID)
	if err != nil {
		return contextx.Subject{}, errUnauthorized
	}
	if previous.APIKeyID > 0 {
		key, err := s.apiKeys.ValidateAPIKeyByIDDetail(ctx, previous.APIKeyID)
		if err != nil || key.OwnerID != previous.UserID || key.UsageType == repo.APIKeyUsageAppData && strings.TrimSpace(key.AppID) == "" {
			return contextx.Subject{}, errUnauthorized
		}
		keys = intersect(keys, key.ResourceKeys)
		previous.APIKeyPermission = key.Permission
		previous.APIKeyAppID = key.AppID
		previous.APIKeyUsageType = key.UsageType
	}
	previous.UserName = user.UserName
	previous.ResourceKeys = toSet(keys)
	previous.UIActions = actions
	return previous, nil
}

// AuthenticateMQTTAPIKey validates the same API key record as OpenAPI while
// retaining the key's own permission scope. Cloud's EMQX auth path uses the
// API key permission directly rather than intersecting it with UI role keys.
func (s *Service) AuthenticateMQTTAPIKey(ctx context.Context, raw string) (contextx.Subject, error) {
	if raw == "" {
		return contextx.Subject{}, errUnauthorized
	}
	key, err := s.apiKeys.ValidateAPIKeyDetail(ctx, raw)
	if err != nil || key.UsageType == repo.APIKeyUsageAppData {
		return contextx.Subject{}, errUnauthorized
	}
	user, err := s.iam.GetUserByID(ctx, key.OwnerID)
	if err != nil || user.Status != 1 {
		return contextx.Subject{}, errUnauthorized
	}
	return contextx.Subject{
		UserID:       user.ID,
		UserName:     user.UserName,
		Email:        user.Email,
		AuthType:     "apiKey",
		APIKeyID:     key.ID,
		APIKeyName:   key.Name,
		APIKeyPrefix: key.KeyPrefix,
		APIKeyAppID:  key.AppID,
		KeyType:      key.KeyType,
		ResourceKeys: toSet(key.ResourceKeys),
	}, nil
}

// RevalidateMQTTAPIKey reloads an established MQTT session's API key by its
// non-secret record ID. Invalid or disabled credentials are returned as
// valid=false, while repository failures remain errors so callers do not
// disconnect healthy clients during a transient database outage.
func (s *Service) RevalidateMQTTAPIKey(ctx context.Context, keyID int64) (contextx.Subject, bool, error) {
	key, err := s.apiKeys.ValidateAPIKeyByIDDetail(ctx, keyID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return contextx.Subject{}, false, nil
		}
		return contextx.Subject{}, false, err
	}
	if key.UsageType == repo.APIKeyUsageAppData {
		return contextx.Subject{}, false, nil
	}
	user, err := s.iam.GetUserByID(ctx, key.OwnerID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return contextx.Subject{}, false, nil
		}
		return contextx.Subject{}, false, err
	}
	if user.Status != 1 {
		return contextx.Subject{}, false, nil
	}
	return contextx.Subject{
		UserID:       user.ID,
		UserName:     user.UserName,
		Email:        user.Email,
		AuthType:     "apiKey",
		APIKeyID:     key.ID,
		APIKeyName:   key.Name,
		APIKeyPrefix: key.KeyPrefix,
		APIKeyAppID:  key.AppID,
		KeyType:      key.KeyType,
		ResourceKeys: toSet(key.ResourceKeys),
	}, true, nil
}

// AuthenticateLegacyMQTTAPIKey is a migration-only path for application keys
// created before Enterprise adopted Cloud's workspace-encoded key format.
func (s *Service) AuthenticateLegacyMQTTAPIKey(ctx context.Context, raw string) (contextx.Subject, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "t0_") || s.appAPIKeys == nil {
		return contextx.Subject{}, errUnauthorized
	}
	appKey, err := s.appAPIKeys.FindByAPIKey(ctx, raw)
	if err != nil || appKey == nil || strings.TrimSpace(appKey.AppID) == "" {
		return contextx.Subject{}, errUnauthorized
	}
	return s.AuthenticateMQTTAPIKey(ctx, raw)
}

func UsesDefaultPassword(user repo.User) bool {
	if user.IsRandomPwd {
		return true
	}
	if strings.TrimSpace(user.Password) == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("tier0")) == nil
}

// issueToken signs a login session JWT whose lifetime comes from the stored
// login-session-duration setting ("仅对新会话生效" — existing tokens keep their exp).
func (s *Service) issueToken(ctx context.Context, user repo.User) (string, time.Time, error) {
	expireAt := time.Now().Add(s.loginSessionTTL(ctx))
	claims := jwt.MapClaims{
		"uid": user.ID,
		"sub": strconv.FormatInt(user.ID, 10),
		"exp": expireAt.Unix(),
		"iat": time.Now().Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	return token, expireAt, err
}

// loginSessionTTL resolves the configured duration; any read failure falls back
// to the 12h default so a broken settings row cannot block logins entirely.
func (s *Service) loginSessionTTL(ctx context.Context) time.Duration {
	value, err := s.systemSettings.GetSetting(ctx, SystemSettingKeyLoginSessionDuration)
	if err != nil {
		logx.WithContext(ctx).Errorf("read login session duration fallback to %s: %v", DefaultLoginSessionDuration, err)
		return TTLForLoginSessionDuration("")
	}
	return TTLForLoginSessionDuration(value)
}

func bearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return ""
}

func APIKeyFromRequest(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-API-Key")); value != "" {
		return value
	}
	if value := strings.TrimSpace(r.URL.Query().Get("apikey")); value != "" {
		return value
	}
	if value := strings.TrimSpace(r.URL.Query().Get("apiKey")); value != "" {
		return value
	}
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "apikey ") {
		return strings.TrimSpace(value[7:])
	}
	return bearerToken(r)
}

func toSet(keys []string) map[string]struct{} {
	out := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		out[key] = struct{}{}
	}
	return out
}

func filterLicenseResourceKeys(keys []string) []string {
	licenseSvc := domainlicense.Default()
	if licenseSvc == nil {
		return keys
	}
	return licenseSvc.FilterResourceKeys(keys)
}

func intersect(left, right []string) []string {
	rightSet := toSet(right)
	var out []string
	for _, key := range left {
		if _, ok := rightSet[key]; ok {
			out = append(out, key)
		}
	}
	return out
}

var errUnauthorized = errors.New("unauthorized")
