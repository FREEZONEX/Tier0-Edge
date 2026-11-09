package mqtt

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"backend/internal/contextx"
	cacheinfra "backend/internal/infra/cache"
)

type fakeAPIKeyAuthenticator struct {
	subject           contextx.Subject
	err               error
	revalidated       contextx.Subject
	revalidatedValid  bool
	revalidationError error
}

func (f fakeAPIKeyAuthenticator) AuthenticateMQTTAPIKey(context.Context, string) (contextx.Subject, error) {
	return f.subject, f.err
}

func (f fakeAPIKeyAuthenticator) AuthenticateLegacyMQTTAPIKey(context.Context, string) (contextx.Subject, error) {
	return f.subject, f.err
}

func (f fakeAPIKeyAuthenticator) RevalidateMQTTAPIKey(context.Context, int64) (contextx.Subject, bool, error) {
	return f.revalidated, f.revalidatedValid, f.revalidationError
}

type fakeOpenSessionCache struct {
	values     map[string]string
	nxValues   map[string]string
	setErr     error
	setCalls   int
	setNXCalls int
	lastTTL    time.Duration
}

func (f *fakeOpenSessionCache) GetString(_ context.Context, key string) (string, error) {
	value, ok := f.values[key]
	if !ok {
		return "", cacheinfra.ErrNotFound
	}
	return value, nil
}

func (f *fakeOpenSessionCache) SetString(_ context.Context, key, value string, _ time.Duration) error {
	f.setCalls++
	f.lastTTL = openSessionTTL
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = value
	return nil
}

func (f *fakeOpenSessionCache) SetNX(_ context.Context, key, value string, _ time.Duration) (bool, error) {
	f.setNXCalls++
	if f.nxValues == nil {
		f.nxValues = map[string]string{}
	}
	if _, exists := f.nxValues[key]; exists {
		return false, nil
	}
	f.nxValues[key] = value
	return true, nil
}

func TestOpenAPIKeyMQTTAuthAndACL(t *testing.T) {
	cache := &fakeOpenSessionCache{values: map[string]string{}}
	service := &Service{
		apiKeyAuth: fakeAPIKeyAuthenticator{subject: contextx.Subject{
			UserID:   1,
			APIKeyID: 11,
			ResourceKeys: map[string]struct{}{
				resourceUNSRead:  {},
				resourceUNSWrite: {},
			},
		}},
		cache: cache,
	}

	auth, err := service.Auth(context.Background(), "1&open", "sk-svc-ws1_token", "1&client", "")
	if err != nil {
		t.Fatalf("Auth() error = %v", err)
	}
	if allow, _ := auth["allow"].(bool); !allow {
		t.Fatalf("Auth() = %#v, want allow", auth)
	}

	for _, action := range []string{"publish", "subscribe", "pub", "sub"} {
		acl, err := service.ACL(context.Background(), "1&open", "1&client", "factory/line/1", action)
		if err != nil {
			t.Fatalf("ACL(%q) error = %v", action, err)
		}
		if allow, _ := acl["allow"].(bool); !allow {
			t.Fatalf("ACL(%q) = %#v, want allow", action, acl)
		}
	}

	acl, err := service.ACL(context.Background(), "1&open", "1&client", "$SYS/brokers", "subscribe")
	if err != nil {
		t.Fatalf("ACL($SYS) error = %v", err)
	}
	if allow, _ := acl["allow"].(bool); allow {
		t.Fatalf("ACL($SYS) = %#v, want deny", acl)
	}
}

func TestOpenAPIKeyMQTTAuthRejectsCloudContractMismatch(t *testing.T) {
	service := &Service{
		apiKeyAuth: fakeAPIKeyAuthenticator{subject: contextx.Subject{
			UserID:       1,
			APIKeyID:     11,
			ResourceKeys: map[string]struct{}{resourceUNSRead: {}},
		}},
		cache: &fakeOpenSessionCache{values: map[string]string{}},
	}

	tests := []struct {
		name     string
		username string
		apiKey   string
		clientID string
	}{
		{name: "legacy key", username: "1&open", apiKey: "t0_legacy", clientID: "1&client"},
		{name: "workspace mismatch", username: "1&open", apiKey: "sk-svc-ws2_token", clientID: "1&client"},
		{name: "client mismatch", username: "1&open", apiKey: "sk-svc-ws1_token", clientID: "2&client"},
		{name: "invalid legacy key", username: "enterprise&open", apiKey: "invalid", clientID: "enterprise&client"},
		{name: "invalid legacy client", username: "enterprise&open", apiKey: "t0_legacy", clientID: "1&client"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			auth, err := service.Auth(context.Background(), test.username, test.apiKey, test.clientID, "")
			if err != nil {
				t.Fatalf("Auth() error = %v", err)
			}
			if allow, _ := auth["allow"].(bool); allow {
				t.Fatalf("Auth() = %#v, want deny", auth)
			}
		})
	}
}

func TestLegacyOpenAPIKeyMQTTAuthAndACL(t *testing.T) {
	cache := &fakeOpenSessionCache{values: map[string]string{}}
	service := &Service{
		apiKeyAuth: fakeAPIKeyAuthenticator{subject: contextx.Subject{
			UserID:     1,
			APIKeyID:   11,
			APIKeyName: "app-test-default",
			ResourceKeys: map[string]struct{}{
				resourceUNSRead:  {},
				resourceUNSWrite: {},
			},
		}},
		cache: cache,
	}

	auth, err := service.Auth(context.Background(), "enterprise&open", "t0_legacy", "enterprise&client", "")
	if err != nil {
		t.Fatalf("Auth() error = %v", err)
	}
	if auth["allow"] != true {
		t.Fatalf("Auth() = %#v, want allow", auth)
	}
	for _, action := range []string{"subscribe", "publish"} {
		acl, err := service.ACL(context.Background(), "enterprise&open", "enterprise&client", "factory/line", action)
		if err != nil {
			t.Fatalf("ACL(%q) error = %v", action, err)
		}
		if acl["allow"] != true {
			t.Fatalf("ACL(%q) = %#v, want allow", action, acl)
		}
	}
}

func TestOpenAPIKeyACLMapsReadAndWritePermissions(t *testing.T) {
	cache := &fakeOpenSessionCache{values: map[string]string{}}
	service := &Service{
		apiKeyAuth: fakeAPIKeyAuthenticator{subject: contextx.Subject{
			UserID:       1,
			APIKeyID:     11,
			ResourceKeys: map[string]struct{}{resourceUNSRead: {}},
		}},
		cache: cache,
	}
	if auth, _ := service.Auth(context.Background(), "1&open", "sk-svc-ws1_token", "1&reader", ""); auth["allow"] != true {
		t.Fatalf("reader Auth() = %#v, want allow", auth)
	}
	subscribe, _ := service.ACL(context.Background(), "1&open", "1&reader", "factory/#", "subscribe")
	if subscribe["allow"] != true {
		t.Fatalf("subscribe ACL() = %#v, want allow", subscribe)
	}
	publish, _ := service.ACL(context.Background(), "1&open", "1&reader", "factory/line", "publish")
	if publish["allow"] != false {
		t.Fatalf("publish ACL() = %#v, want deny", publish)
	}
}

func TestOpenAPIKeyACLRefreshesActiveSession(t *testing.T) {
	clientID := "1&refresh"
	key := openSessionKey(1, clientID)
	body, err := json.Marshal(openAPISession{
		WorkspaceID:  1,
		UserID:       1,
		APIKeyID:     11,
		ResourceKeys: []string{resourceUNSRead, resourceUNSWrite},
		RefreshAfter: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	cache := &fakeOpenSessionCache{values: map[string]string{key: string(body)}}
	service := &Service{
		cache: cache,
		apiKeyAuth: fakeAPIKeyAuthenticator{
			revalidated: contextx.Subject{
				UserID:   1,
				APIKeyID: 11,
				ResourceKeys: map[string]struct{}{
					resourceUNSRead:  {},
					resourceUNSWrite: {},
				},
			},
			revalidatedValid: true,
		},
	}

	acl, err := service.ACL(context.Background(), "1&open", clientID, "factory/line", "publish")
	if err != nil {
		t.Fatalf("ACL() error = %v", err)
	}
	if acl["allow"] != true {
		t.Fatalf("ACL() = %#v, want allow", acl)
	}
	if cache.setCalls != 1 || cache.lastTTL != openSessionTTL {
		t.Fatalf("session refresh writes = %d ttl = %v, want 1 and %v", cache.setCalls, cache.lastTTL, openSessionTTL)
	}
	var refreshed openAPISession
	if err := json.Unmarshal([]byte(cache.values[key]), &refreshed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if refreshed.RefreshAfter <= time.Now().Unix() {
		t.Fatalf("RefreshAfter = %d, want future timestamp", refreshed.RefreshAfter)
	}

	if acl, _ := service.ACL(context.Background(), "1&open", clientID, "factory/line", "publish"); acl["allow"] != true {
		t.Fatalf("second ACL() = %#v, want allow", acl)
	}
	if cache.setCalls != 1 {
		t.Fatalf("second ACL() refresh writes = %d, want 1", cache.setCalls)
	}
}

func TestOpenAPIKeyACLDeniedActionStillRevalidates(t *testing.T) {
	clientID := "1&read-only"
	key := openSessionKey(1, clientID)
	body, err := json.Marshal(openAPISession{
		WorkspaceID:  1,
		UserID:       1,
		APIKeyID:     11,
		ResourceKeys: []string{resourceUNSRead},
		RefreshAfter: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	cache := &fakeOpenSessionCache{values: map[string]string{key: string(body)}}
	service := &Service{
		cache: cache,
		apiKeyAuth: fakeAPIKeyAuthenticator{
			revalidated: contextx.Subject{
				UserID:       1,
				APIKeyID:     11,
				ResourceKeys: map[string]struct{}{resourceUNSRead: {}},
			},
			revalidatedValid: true,
		},
	}

	acl, err := service.ACL(context.Background(), "1&open", clientID, "factory/line", "publish")
	if err != nil {
		t.Fatalf("ACL() error = %v", err)
	}
	if acl["allow"] != false {
		t.Fatalf("ACL() = %#v, want deny", acl)
	}
	if cache.setCalls != 1 {
		t.Fatalf("valid read-only session refresh writes = %d, want 1", cache.setCalls)
	}
}

func TestOpenAPIKeyACLPermissionReductionStartsRecovery(t *testing.T) {
	clientID := "1&downgraded"
	recoveryKey := openSessionRecoveryKey(1, clientID)
	openSessionRecoveryAttempts.Delete(recoveryKey)
	defer openSessionRecoveryAttempts.Delete(recoveryKey)
	key := openSessionKey(1, clientID)
	body, err := json.Marshal(openAPISession{
		WorkspaceID:  1,
		UserID:       1,
		APIKeyID:     11,
		ResourceKeys: []string{resourceUNSRead, resourceUNSWrite},
		RefreshAfter: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	cache := &fakeOpenSessionCache{values: map[string]string{key: string(body)}}
	service := &Service{
		cache: cache,
		apiKeyAuth: fakeAPIKeyAuthenticator{
			revalidated: contextx.Subject{
				UserID:       1,
				APIKeyID:     11,
				ResourceKeys: map[string]struct{}{resourceUNSRead: {}},
			},
			revalidatedValid: true,
		},
	}

	acl, err := service.ACL(context.Background(), "1&open", clientID, "factory/line", "publish")
	if err != nil {
		t.Fatalf("ACL() error = %v", err)
	}
	if acl["allow"] != false {
		t.Fatalf("ACL() = %#v, want deny", acl)
	}
	if cache.setCalls != 0 || cache.setNXCalls != 2 {
		t.Fatalf("downgraded session writes=%d locks=%d, want 0 writes and recheck+recovery locks", cache.setCalls, cache.setNXCalls)
	}
}

func TestOpenAPIKeyACLTransientRevalidationFailureUsesCachedPermission(t *testing.T) {
	clientID := "1&db-outage"
	key := openSessionKey(1, clientID)
	body, err := json.Marshal(openAPISession{
		WorkspaceID:  1,
		UserID:       1,
		APIKeyID:     11,
		ResourceKeys: []string{resourceUNSWrite},
		RefreshAfter: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	cache := &fakeOpenSessionCache{values: map[string]string{key: string(body)}}
	service := &Service{
		cache: cache,
		apiKeyAuth: fakeAPIKeyAuthenticator{
			revalidationError: context.DeadlineExceeded,
		},
	}

	acl, err := service.ACL(context.Background(), "1&open", clientID, "factory/line", "publish")
	if err != nil {
		t.Fatalf("ACL() error = %v", err)
	}
	if acl["allow"] != true {
		t.Fatalf("ACL() = %#v, want cached allow during transient failure", acl)
	}
	if cache.setCalls != 0 || cache.setNXCalls != 1 {
		t.Fatalf("transient failure writes=%d locks=%d, want no TTL extension and one retry throttle", cache.setCalls, cache.setNXCalls)
	}
}

func TestOpenAPIKeyACLMissingSessionStartsOneRecovery(t *testing.T) {
	clientID := "1&missing"
	recoveryKey := openSessionRecoveryKey(1, clientID)
	openSessionRecoveryAttempts.Delete(recoveryKey)
	defer openSessionRecoveryAttempts.Delete(recoveryKey)
	cache := &fakeOpenSessionCache{values: map[string]string{}}
	service := &Service{cache: cache}

	for range 2 {
		acl, err := service.ACL(context.Background(), "1&open", clientID, "factory/line", "publish")
		if err != nil {
			t.Fatalf("ACL() error = %v", err)
		}
		if acl["allow"] != false {
			t.Fatalf("ACL() = %#v, want deny while reconnect is pending", acl)
		}
	}
	if cache.setNXCalls != 1 {
		t.Fatalf("session recovery lock attempts = %d, want 1", cache.setNXCalls)
	}
}
