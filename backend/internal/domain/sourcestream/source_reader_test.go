package sourcestream_test

// 校验平台代读的权限、资源范围、配置快照与只读操作；数据库使用隔离 testkit。
import (
	"backend/internal/testkit/scripttest"
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"backend/internal/contextx"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestSourceReaderAuthorizationAndReadOnlyBoundary(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{
		"uns.source": {}, "uns.read": {}, "uns.stream.manage": {},
	}})
	runtime := &protocolFixture{resources: []domain.NativeResource{
		{ID: "opcua:temperature", Models: []domain.AccessModel{domain.Publish, domain.RequestReply}},
		{ID: "opcua:reset", Models: []domain.AccessModel{domain.RequestReply, domain.Subscribe}},
	}}
	s := domain.New(runtime, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	def := domain.SourceDefinition{Name: "受控只读源", Protocol: domain.OPCUA, Endpoint: "opc.tcp://fixture:4840", RuntimeLocation: "local", OPCUA: &domain.OPCUAOptions{SecurityMode: "None", SecurityPolicy: "None", SamplingIntervalMs: 100, NodeIDs: []string{"temperature", "reset"}}}
	view, err := s.CreateSource(ctx, def, 0)
	require.NoError(t, err)
	id := view["id"].(int64)
	_, err = testkit.SetLegacySourceState(ctx, s, id, 1, "enabled")
	require.NoError(t, err)
	_, err = s.Discover(ctx, id, 2, "")
	require.NoError(t, err)
	grants := []streamexec.SourceGrant{{Alias: "boiler", SourceID: id, ConfigRevision: 1, ReadResources: []string{"opcua:temperature", "opcua:reset"}}}
	read, err := s.SourceReader(ctx, grants)
	require.NoError(t, err)
	call := streamexec.SourceCall{ID: 1, Alias: "boiler", ResourceRef: "opcua:temperature", Input: json.RawMessage(`{}`)}
	data, err := read(context.Background(), call)
	require.NoError(t, err)
	require.JSONEq(t, `{"applied":{"operation":"read"}}`, string(data))
	require.Equal(t, 1, runtime.calls)
	for _, input := range []string{`{"operation":"write","value":100}`, `null`, `[]`, `broken`} {
		bad := call
		bad.Input = json.RawMessage(input)
		_, err = read(ctx, bad)
		require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	}
	bad := call
	bad.ResourceRef = "opcua:reset"
	_, err = read(ctx, bad)
	require.ErrorIs(t, err, streamexec.ErrSourceDenied, "Methods cannot be read as Variables")
	bad.ResourceRef = "opcua:unknown"
	_, err = read(ctx, bad)
	require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	require.Equal(t, 1, runtime.calls, "rejected calls must never reach protocol runtime")
	// Source 配置版本变化时，原能力快照立即失效；历史启停标记不再控制准入。
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=?", id).Update("config_revision", 2).Error)
	_, err = read(ctx, call)
	require.ErrorIs(t, err, repo.ErrConflict)
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=?", id).Updates(map[string]any{"config_revision": 1, "desired_state": "disabled"}).Error)
	_, err = read(ctx, call)
	require.NoError(t, err, "legacy disabled marker does not block a live Source")
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=?", id).Update("desired_state", "enabled").Error)
	// 创建 reader 后再改原清单，不会增加可读资源。
	grants[0].ReadResources = append(grants[0].ReadResources, "opcua:unknown")
	_, err = read(ctx, bad)
	require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	_, err = s.SourceReader(context.Background(), grants)
	require.ErrorIs(t, err, domain.ErrForbidden)
	for _, missing := range []string{"uns.source", "uns.read", "uns.stream.manage"} {
		keys := map[string]struct{}{"uns.source": {}, "uns.read": {}, "uns.stream.manage": {}}
		delete(keys, missing)
		_, err = s.SourceReader(contextx.WithSubject(ctx, contextx.Subject{UserID: 1, ResourceKeys: keys}), grants)
		require.ErrorIs(t, err, domain.ErrForbidden)
	}
}
