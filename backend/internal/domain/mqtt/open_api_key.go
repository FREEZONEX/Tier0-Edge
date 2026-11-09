package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/contextx"
	domaincommon "backend/internal/domain/common"
	cacheinfra "backend/internal/infra/cache"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/threading"
)

const (
	openSessionKeyPrefix         = "cache:uns:emqx:open"
	openSessionRecoveryKeyPrefix = "cache:uns:emqx:open:recover"
	openSessionRecheckKeyPrefix  = "cache:uns:emqx:open:recheck"
	openSessionTTL               = 24 * time.Hour
	openSessionRefreshInterval   = 12 * time.Hour
	openSessionRecoveryTTL       = 30 * time.Second
	openSessionRecheckTTL        = time.Minute
	openUsernameFlag             = "open"
	openCredentialSep            = "&"
	legacyWorkspaceName          = "enterprise"
	legacyAPIKeyPrefix           = "t0_"
	resourceUNSRead              = "uns.read"
	resourceUNSWrite             = "uns.write"
)

type apiKeyAuthenticator interface {
	AuthenticateMQTTAPIKey(ctx context.Context, raw string) (contextx.Subject, error)
	AuthenticateLegacyMQTTAPIKey(ctx context.Context, raw string) (contextx.Subject, error)
	RevalidateMQTTAPIKey(ctx context.Context, keyID int64) (contextx.Subject, bool, error)
}

type openSessionCache interface {
	GetString(ctx context.Context, key string) (string, error)
	SetString(ctx context.Context, key, value string, ttl time.Duration) error
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
}

type openAPISession struct {
	WorkspaceID  int64    `json:"workspaceID"`
	UserID       int64    `json:"userID,omitempty"`
	APIKeyID     int64    `json:"apiKeyID,omitempty"`
	ResourceKeys []string `json:"resourceKeys,omitempty"`
	RefreshAfter int64    `json:"refreshAfter,omitempty"`
}

var openSessionRecoveryAttempts sync.Map

func (s *Service) authenticateOpenAPIKey(ctx context.Context, workspaceID int64, clientID, apiKey string) bool {
	if workspaceID != domaincommon.PlatformWorkspaceID || s.apiKeyAuth == nil || s.cache == nil {
		return false
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" || domaincommon.ParseWorkspaceIDFromAPIKey(apiKey) != workspaceID {
		return false
	}
	clientWorkspaceID, clientRandom := parseOpenClientID(clientID)
	if clientWorkspaceID != workspaceID || clientRandom == "" {
		return false
	}
	subject, err := s.apiKeyAuth.AuthenticateMQTTAPIKey(ctx, apiKey)
	if err != nil || !contextx.HasResource(subject, resourceUNSRead) {
		return false
	}
	return s.saveOpenAPISession(ctx, workspaceID, clientID, subject)
}

func (s *Service) authenticateLegacyOpenAPIKey(ctx context.Context, clientID, apiKey string) bool {
	if s.apiKeyAuth == nil || s.cache == nil || !strings.HasPrefix(strings.TrimSpace(apiKey), legacyAPIKeyPrefix) {
		return false
	}
	if parseLegacyOpenClientID(clientID) == "" {
		return false
	}
	subject, err := s.apiKeyAuth.AuthenticateLegacyMQTTAPIKey(ctx, apiKey)
	if err != nil || !contextx.HasResource(subject, resourceUNSRead) {
		return false
	}
	if !s.saveOpenAPISession(ctx, domaincommon.PlatformWorkspaceID, clientID, subject) {
		return false
	}
	logx.WithContext(ctx).Infof(
		"accepted legacy mqtt application api key clientID=%s keyName=%s; redeploy application to rotate key",
		clientID,
		subject.APIKeyName,
	)
	return true
}

func (s *Service) saveOpenAPISession(ctx context.Context, workspaceID int64, clientID string, subject contextx.Subject) bool {
	session := openAPISession{
		WorkspaceID:  workspaceID,
		UserID:       subject.UserID,
		APIKeyID:     subject.APIKeyID,
		ResourceKeys: resourceKeyList(subject.ResourceKeys),
		RefreshAfter: nextOpenSessionRefresh(time.Now()),
	}
	body, err := json.Marshal(session)
	if err != nil {
		return false
	}
	if err := s.cache.SetString(ctx, openSessionKey(workspaceID, clientID), string(body), openSessionTTL); err != nil {
		logx.WithContext(ctx).Errorf("save mqtt open api session workspaceID=%d clientID=%s: %v", workspaceID, clientID, err)
		return false
	}
	return true
}

func (s *Service) allowOpenAPIAccess(ctx context.Context, workspaceID int64, clientID, action string) bool {
	if workspaceID != domaincommon.PlatformWorkspaceID || s.cache == nil {
		return false
	}
	clientWorkspaceID, clientRandom := parseOpenClientID(clientID)
	if clientWorkspaceID != workspaceID || clientRandom == "" {
		return false
	}
	return s.openSessionAllows(ctx, workspaceID, clientID, action)
}

func (s *Service) allowLegacyOpenAPIAccess(ctx context.Context, clientID, action string) bool {
	if parseLegacyOpenClientID(clientID) == "" {
		return false
	}
	return s.openSessionAllows(ctx, domaincommon.PlatformWorkspaceID, clientID, action)
}

func (s *Service) openSessionAllows(ctx context.Context, workspaceID int64, clientID, action string) bool {
	if workspaceID != domaincommon.PlatformWorkspaceID || s.cache == nil {
		return false
	}
	body, err := s.cache.GetString(ctx, openSessionKey(workspaceID, clientID))
	if err != nil {
		if errors.Is(err, cacheinfra.ErrNotFound) {
			s.recoverMissingOpenAPISession(ctx, workspaceID, clientID)
		} else {
			logx.WithContext(ctx).Errorf("read mqtt open api session workspaceID=%d clientID=%s: %v", workspaceID, clientID, err)
		}
		return false
	}
	if strings.TrimSpace(body) == "" {
		s.recoverMissingOpenAPISession(ctx, workspaceID, clientID)
		return false
	}
	var session openAPISession
	if err := json.Unmarshal([]byte(body), &session); err != nil {
		logx.WithContext(ctx).Errorf("decode mqtt open api session workspaceID=%d clientID=%s: %v", workspaceID, clientID, err)
		s.recoverMissingOpenAPISession(ctx, workspaceID, clientID)
		return false
	}
	if session.WorkspaceID != workspaceID {
		return false
	}
	if !s.revalidateOpenAPISession(ctx, workspaceID, clientID, &session) {
		return false
	}
	requiredResource := openAPIRequiredResource(action)
	return requiredResource != "" && hasResourceKey(session.ResourceKeys, requiredResource)
}

func (s *Service) revalidateOpenAPISession(ctx context.Context, workspaceID int64, clientID string, session *openAPISession) bool {
	now := time.Now()
	if session == nil || session.RefreshAfter > now.Unix() {
		return true
	}
	if session.APIKeyID <= 0 {
		// Sessions created before APIKeyID was persisted cannot be revalidated
		// safely. Disconnect once so CONNECT can rebuild the session from the
		// original credential.
		s.recoverMissingOpenAPISession(ctx, workspaceID, clientID)
		return false
	}
	if s.apiKeyAuth == nil {
		logx.WithContext(ctx).Errorf("mqtt open api session recheck unavailable workspaceID=%d clientID=%s", workspaceID, clientID)
		return true
	}
	recheckKey := openSessionRecheckKey(workspaceID, clientID)
	acquired, err := s.cache.SetNX(ctx, recheckKey, "1", openSessionRecheckTTL)
	if err != nil {
		logx.WithContext(ctx).Errorf("lock mqtt open api session recheck workspaceID=%d clientID=%s: %v", workspaceID, clientID, err)
		return true
	}
	if !acquired {
		return true
	}
	subject, valid, err := s.apiKeyAuth.RevalidateMQTTAPIKey(ctx, session.APIKeyID)
	if err != nil {
		logx.WithContext(ctx).Errorf("revalidate mqtt open api session workspaceID=%d clientID=%s keyID=%d: %v", workspaceID, clientID, session.APIKeyID, err)
		return true
	}
	currentResources := resourceKeyList(subject.ResourceKeys)
	if !valid || subject.APIKeyID != session.APIKeyID || subject.UserID != session.UserID || resourcesReduced(session.ResourceKeys, currentResources) {
		logx.WithContext(ctx).Infof("disconnect mqtt open api session after credential revoke or permission reduction workspaceID=%d clientID=%s keyID=%d", workspaceID, clientID, session.APIKeyID)
		s.recoverMissingOpenAPISession(ctx, workspaceID, clientID)
		return false
	}
	session.UserID = subject.UserID
	session.ResourceKeys = currentResources
	session.RefreshAfter = nextOpenSessionRefresh(now)
	body, err := json.Marshal(session)
	if err != nil {
		logx.WithContext(ctx).Errorf("encode mqtt open api session refresh workspaceID=%d clientID=%s: %v", workspaceID, clientID, err)
		return true
	}
	if err := s.cache.SetString(ctx, openSessionKey(workspaceID, clientID), string(body), openSessionTTL); err != nil {
		logx.WithContext(ctx).Errorf("refresh mqtt open api session workspaceID=%d clientID=%s: %v", workspaceID, clientID, err)
	}
	return true
}

func nextOpenSessionRefresh(now time.Time) int64 {
	return now.Add(openSessionRefreshInterval).Unix()
}

func (s *Service) recoverMissingOpenAPISession(ctx context.Context, workspaceID int64, clientID string) {
	recoveryKey := openSessionRecoveryKey(workspaceID, clientID)
	if !beginOpenSessionRecovery(recoveryKey) {
		return
	}
	acquired, err := s.cache.SetNX(
		ctx,
		recoveryKey,
		"1",
		openSessionRecoveryTTL,
	)
	if err != nil {
		logx.WithContext(ctx).Errorf("lock mqtt open api session recovery workspaceID=%d clientID=%s: %v", workspaceID, clientID, err)
		return
	}
	if !acquired {
		return
	}
	recoveryCtx := context.WithoutCancel(ctx)
	threading.GoSafe(func() {
		s.kickClient(recoveryCtx, clientID)
	})
}

func beginOpenSessionRecovery(key string) bool {
	if _, loaded := openSessionRecoveryAttempts.LoadOrStore(key, struct{}{}); loaded {
		return false
	}
	time.AfterFunc(openSessionRecoveryTTL, func() {
		openSessionRecoveryAttempts.Delete(key)
	})
	return true
}

func parseOpenUsername(username string) (workspaceID int64, isOpen bool) {
	parts := strings.Split(strings.TrimSpace(username), openCredentialSep)
	if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[1]), openUsernameFlag) {
		return 0, false
	}
	workspaceID, _ = strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	return workspaceID, true
}

func parseOpenClientID(clientID string) (int64, string) {
	parts := strings.Split(strings.TrimSpace(clientID), openCredentialSep)
	if len(parts) != 2 {
		return 0, ""
	}
	workspaceID, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	return workspaceID, strings.TrimSpace(parts[1])
}

func isLegacyOpenUsername(username string) bool {
	return strings.EqualFold(
		strings.TrimSpace(username),
		legacyWorkspaceName+openCredentialSep+openUsernameFlag,
	)
}

func parseLegacyOpenClientID(clientID string) string {
	parts := strings.Split(strings.TrimSpace(clientID), openCredentialSep)
	if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), legacyWorkspaceName) {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func openSessionKey(workspaceID int64, clientID string) string {
	return fmt.Sprintf("%s:%d:%s", openSessionKeyPrefix, workspaceID, strings.TrimSpace(clientID))
}

func openSessionRecoveryKey(workspaceID int64, clientID string) string {
	return fmt.Sprintf("%s:%d:%s", openSessionRecoveryKeyPrefix, workspaceID, strings.TrimSpace(clientID))
}

func openSessionRecheckKey(workspaceID int64, clientID string) string {
	return fmt.Sprintf("%s:%d:%s", openSessionRecheckKeyPrefix, workspaceID, strings.TrimSpace(clientID))
}

func openAPIRequiredResource(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "publish", "pub":
		return resourceUNSWrite
	case "subscribe", "sub":
		return resourceUNSRead
	default:
		return ""
	}
}

func hasResourceKey(resources []string, target string) bool {
	for _, resource := range resources {
		if strings.TrimSpace(resource) == target {
			return true
		}
	}
	return false
}

func resourceKeyList(resources map[string]struct{}) []string {
	out := make([]string, 0, len(resources))
	for resource := range resources {
		resource = strings.TrimSpace(resource)
		if resource != "" {
			out = append(out, resource)
		}
	}
	return out
}

func resourcesReduced(previous, current []string) bool {
	for _, resource := range previous {
		if !hasResourceKey(current, strings.TrimSpace(resource)) {
			return true
		}
	}
	return false
}
