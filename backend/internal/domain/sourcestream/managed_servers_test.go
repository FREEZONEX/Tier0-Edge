package sourcestream_test

// 验证真实配置库中的 Server 期望状态、私有凭证下发、启停/轮换及所有权退出撤权。
// Node 的真实协议权限与租约到期由 managed-server.test.mjs 覆盖。
import (
	"backend/internal/testkit/scripttest"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	domain "backend/internal/domain/sourcestream"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

type managedServerFixture struct {
	protocolFixture
	snapshots [][]domain.ManagedServerSource
	failure   error
	onSync    func([]domain.ManagedServerSource)
}

func (f *managedServerFixture) SyncServerSources(_ context.Context, sources []domain.ManagedServerSource) error {
	f.snapshots = append(f.snapshots, append([]domain.ManagedServerSource{}, sources...))
	if f.onSync != nil {
		f.onSync(sources)
	}
	return f.failure
}

func opcuaServerDefinition() domain.SourceDefinition {
	return domain.SourceDefinition{Name: "OPC UA Server", Protocol: domain.OPCUA, Role: domain.ServerRole, Endpoint: "opc.tcp://managed", RuntimeLocation: "local",
		OPCUA: &domain.OPCUAOptions{SecurityMode: "None", SecurityPolicy: "None", SamplingIntervalMs: 1000, NodeIDs: []string{"temperature", "command"},
			ServerNodes: []domain.OPCUAServerNode{{ID: "temperature", Name: "温度", NodeClass: "Variable", DataType: "Double", Direction: "ingress"},
				{ID: "command", Name: "设定值", NodeClass: "Variable", DataType: "String", Direction: "egress"}}}}
}

func TestManagedServersEnabledSnapshotCredentialsAndWithdrawal(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	runtime := &managedServerFixture{}
	svc := domain.New(runtime, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	require.NoError(t, svc.ReconcileManagedServers(ctx))
	require.Empty(t, runtime.snapshots[0])
	view, err := svc.CreateSource(ctx, opcuaServerDefinition(), 0)
	require.NoError(t, err)
	id := view["id"].(int64)
	credential := domain.Credential{Kind: "basic", Username: "test-device", Secret: "test-server-secret"}
	view, err = svc.SaveCredential(ctx, id, view["revision"].(int64), credential, "")
	require.NoError(t, err)
	raw, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(raw), credential.Secret)
	require.NotContains(t, string(raw), credential.Username)
	require.Equal(t, "urn:tier0:source-resources", view["opcuaServer"].(map[string]any)["namespaceUri"])
	require.Equal(t, "private-listener", view["opcuaServer"].(map[string]any)["access"])
	require.NoError(t, svc.ReconcileManagedServers(ctx))
	require.Len(t, runtime.snapshots[len(runtime.snapshots)-1], 1, "saved Source with valid credentials admits devices without activation")
	view, err = testkit.SetLegacySourceState(ctx, svc, id, view["revision"].(int64), "enabled")
	require.NoError(t, err)
	require.NoError(t, svc.ReconcileManagedServers(ctx))
	snapshot := runtime.snapshots[len(runtime.snapshots)-1]
	require.Len(t, snapshot, 1)
	require.Equal(t, credential, snapshot[0].Credential)
	require.Equal(t, id, snapshot[0].SourceID)
	firstRef := snapshot[0].Definition.CredentialRef
	for _, invalid := range []domain.Credential{
		{Kind: "basic", Username: strings.Repeat("x", 129), Secret: "test"},
		{Kind: "basic", Username: "test", Secret: strings.Repeat("x", 4097)},
		{Kind: "certificate", CertificateFileID: 1, PrivateKeyFileID: 2},
	} {
		_, err = svc.SaveCredential(ctx, id, view["revision"].(int64), invalid, "")
		require.ErrorIs(t, err, domain.ErrCredential)
	}
	credential.Secret = "rotated-test-secret"
	view, err = svc.SaveCredential(ctx, id, view["revision"].(int64), credential, "")
	require.NoError(t, err)
	require.NoError(t, svc.ReconcileManagedServers(ctx))
	snapshot = runtime.snapshots[len(runtime.snapshots)-1]
	require.NotEqual(t, firstRef, snapshot[0].Definition.CredentialRef)
	require.Equal(t, credential.Secret, snapshot[0].Credential.Secret)

	loopCtx, cancel := context.WithCancel(ctx)
	runtime.onSync = func(sources []domain.ManagedServerSource) {
		if len(sources) > 0 {
			cancel()
		}
	}
	svc.ManagedServersLoop(loopCtx)
	require.Empty(t, runtime.snapshots[len(runtime.snapshots)-1], "ownership exit withdraws Sources immediately")
	runtime.onSync = nil
	runtime.failure = errors.New("runtime unavailable")
	require.ErrorIs(t, svc.ReconcileManagedServers(ctx), runtime.failure)
	runtime.failure = nil
	otherDef := opcuaServerDefinition()
	otherDef.Name = "OPC UA other"
	other, err := svc.CreateSource(ctx, otherDef, 0)
	require.NoError(t, err)
	require.NoError(t, svc.ReconcileManagedServers(ctx))
	snapshot = runtime.snapshots[len(runtime.snapshots)-1]
	require.Len(t, snapshot, 1, "an unprovisioned Source cannot block renewal")
	require.Equal(t, id, snapshot[0].SourceID)
	_, err = svc.SaveCredential(ctx, other["id"].(int64), other["revision"].(int64), credential, "")
	require.NoError(t, err)
	require.NoError(t, svc.ReconcileManagedServers(ctx))
	require.Len(t, runtime.snapshots[len(runtime.snapshots)-1], 2)
	// Corrupt the fixture's canonical credential collection to exercise a
	// missing credential; the derived projection is intentionally read-only.
	currentRef := view["document"].(map[string]any)["credentialRef"].(string)
	require.NoError(t, repo.GetUnsConn(ctx).Exec(`UPDATE public.uns_meta
		SET data=jsonb_set(data,'{credentials}',(data->'credentials') - ?,true)
		WHERE kind='source' AND id=?`, currentRef, strconv.FormatInt(id, 10)).Error)
	require.NoError(t, svc.ReconcileManagedServers(ctx))
	snapshot = runtime.snapshots[len(runtime.snapshots)-1]
	require.Len(t, snapshot, 1, "missing credentials withdraw only the affected Source")
	require.Equal(t, other["id"].(int64), snapshot[0].SourceID)
	unsupported := domain.New(&protocolFixture{}, scripttest.New(t), "")
	require.ErrorIs(t, unsupported.ReconcileManagedServers(ctx), domain.ErrUnavailable)
}
