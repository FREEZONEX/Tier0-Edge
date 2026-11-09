package unsv2_test

// 当前App grant撤销/身份校验；使用真实App定义和真实Key/User有效交集。
import (
	"context"
	"testing"

	"backend/internal/contextx"
	"backend/internal/domain/unsv2"
	"backend/internal/repo"

	"github.com/stretchr/testify/require"
)

func TestDiscoveryAccessAppCurrentGrant(t *testing.T) {
	a := newAppAPI(t)
	key, _ := a.provision()
	subject, err := a.svc.App.Auth.AuthenticateAPIKey(context.Background(), key)
	require.NoError(t, err)
	ctx := contextx.WithSubject(context.Background(), subject)
	namespace, app, err := unsv2.DiscoveryAccess(ctx)
	require.NoError(t, err)
	require.True(t, namespace)
	require.True(t, app)
	a.apply(a.admin, 200, object{"opId": "restrict", "action": "meta.replace", "kind": "app", "id": "maintenance", "expectedRevision": 1, "data": appDocument([]string{"record:write"})})
	namespace, app, err = unsv2.DiscoveryAccess(ctx)
	require.NoError(t, err)
	require.False(t, namespace)
	require.True(t, app)
	doc := appDocument([]string{"record:read"})
	doc["status"] = "disabled"
	a.apply(a.admin, 200, object{"opId": "disable", "action": "meta.replace", "kind": "app", "id": "maintenance", "expectedRevision": 2, "data": doc})
	_, _, err = unsv2.DiscoveryAccess(ctx)
	require.Error(t, err)
	subject.APIKeyUsageType = repo.APIKeyUsageAppData
	subject.APIKeyAppID = ""
	_, _, err = unsv2.DiscoveryAccess(contextx.WithSubject(context.Background(), subject))
	require.Error(t, err)
}
