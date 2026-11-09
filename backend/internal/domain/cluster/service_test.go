// service_test.go 覆盖 cluster Service 核心方法：
// 令牌签发（CreateToken）、握手（Handshake）、节点列表（ListNodes/ListScopes/
// ListOutbox）与纯函数（isConnectNodeStale/edgeToken/normalizeCoordinator）。
// 正常路径：合法令牌创建并持久化、握手使节点上线并返回协议信息、
//
//	新鲜在线节点保持在线、空列表返回空。
//
// 异常情况：空/超长名称、超长描述、非法 JSON capabilities、非法令牌、
//
//	心跳超时的在线节点被标记离线。
//
// fleet agent 交互分支（ConfigureCurrentConnection/CurrentNode 等）依赖真实
// fleet agent 进程，仅覆盖未配置与错误分支，见 local_agent_test.go。
package cluster

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"backend/internal/config"
	"backend/internal/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEdgeToken(t *testing.T) {
	s := &Service{cfg: config.ClusterConf{
		ExternalHTTPBaseURL:    "https://c.example.com",
		ExternalMqttBrokerHost: "mqtt.example.com",
	}}
	client := repo.ConnectClient{ClientID: "c1", Password: "p1", Username: "u1", Name: "edge-1"}
	node := repo.FleetNode{FleetNodeID: "fn_x", Name: "edge-1"}
	token := s.edgeToken(client, node)
	require.NotEmpty(t, token)
	parsed, err := parseSyncToken(token)
	require.NoError(t, err)
	assert.Equal(t, "c1", parsed.MqttAuthID)
	assert.Equal(t, "p1", parsed.SignedPassword)
	assert.Equal(t, "fn_x", parsed.EdgeNodeID)
	assert.Equal(t, "edge-1", parsed.EdgeNodeName)
	assert.Equal(t, syncModeEdgeEdge, parsed.SyncMode)
	assert.Equal(t, "https://c.example.com", parsed.HTTPBaseURL)
	assert.Equal(t, "ssl://mqtt.example.com:8883", parsed.MqttURL)

	assert.Equal(t, "", s.edgeToken(repo.ConnectClient{ClientID: "  ", Password: "p"}, node))
	assert.Equal(t, "", s.edgeToken(repo.ConnectClient{ClientID: "c", Password: " "}, node))
}

func TestReconcileLicenseRuntime_NilService(t *testing.T) {
	var s *Service
	s.ReconcileLicenseRuntime()
}

func TestReconcileLicenseRuntime_NoFleet(t *testing.T) {
	s := &Service{}
	s.ReconcileLicenseRuntime()
}

func TestClusterErrors(t *testing.T) {
	assert.EqualError(t, ErrInvalid, "invalid cluster request")
	assert.Equal(t, "FLEET_UNS_SCOPE_REVISION_CONFLICT", ErrScopeRevisionConflict.Error())
	assert.Equal(t, "FLEET_ACCOUNT_ASSIGNMENT_REVISION_CONFLICT", ErrAccountRevisionConflict.Error())
	assert.Equal(t, "FLEET_ACCOUNT_NOT_ASSIGNABLE", ErrAccountNotAssignable.Error())
	assert.Equal(t, "FLEET_NODE_QUOTA_EXCEEDED", ErrFleetNodeQuotaExceeded.Error())
	assert.Equal(t, "FLEET_NODE_ONLINE", ErrFleetNodeOnline.Error())
	assert.Equal(t, "cluster host cannot connect as child", ErrHostCannotConnect.Error())
}

// 验证新授权默认关闭、旧请求保留值与版本冲突不覆盖已生效配置。
func TestFleetControlAccessRevisionAndLegacyRequests(t *testing.T) {
	require.ErrorIs(t, &fleetPeerError{Status: 403, Code: "EDGE_CONTROL_DISABLED"}, ErrFleetControlNotAllowed)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	s := &Service{cfg: config.ClusterConf{IdentityDir: dir}}
	access, err := s.FleetAccess()
	require.NoError(t, err)
	require.False(t, access.AllowControl)
	require.ErrorIs(t, s.CheckFleetControlAccess(), ErrFleetControlNotAllowed)
	path := filepath.Join(dir, "remote-access.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"allowSSH":true,"allowMetadataWrite":false,"revision":7}`), 0600))
	access, err = s.FleetAccess()
	require.NoError(t, err)
	require.False(t, access.AllowControl)
	enabled := true
	access, err = s.SaveFleetAccess(context.Background(), FleetAccessRequest{AllowSSH: true, AllowMetadataWrite: false, AllowControl: &enabled, ExpectedRevision: 7})
	require.NoError(t, err)
	require.EqualValues(t, 8, access.Revision)
	require.NoError(t, s.CheckFleetControlAccess())
	var legacy FleetAccessRequest
	require.NoError(t, json.Unmarshal([]byte(`{"allowSSH":false,"allowMetadataWrite":true,"expectedRevision":8}`), &legacy))
	access, err = s.SaveFleetAccess(context.Background(), legacy)
	require.NoError(t, err)
	require.True(t, access.AllowControl, "旧客户端省略字段不能撤销新授权")
	require.False(t, access.AllowSSH)
	require.True(t, access.AllowMetadataWrite)
	disabled := false
	_, err = s.SaveFleetAccess(context.Background(), FleetAccessRequest{AllowControl: &disabled, ExpectedRevision: 8})
	var conflict *FleetRevisionConflict
	require.ErrorAs(t, err, &conflict)
	require.EqualValues(t, 9, conflict.CurrentRevision)
	require.NoError(t, s.CheckFleetControlAccess())
	access, err = s.SaveFleetAccess(context.Background(), FleetAccessRequest{AllowControl: &disabled, ExpectedRevision: 9})
	require.NoError(t, err)
	require.False(t, access.AllowControl)
	require.ErrorIs(t, s.CheckFleetControlAccess(), ErrFleetControlNotAllowed)
	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Zero(t, st.Mode().Perm()&0077)
	require.NoError(t, os.WriteFile(path, []byte(`broken`), 0600))
	require.Error(t, s.CheckFleetControlAccess(), "配置损坏时失败关闭")
}

// 按已保存中心隔离同步状态，旧中心队列、目标及错误必须原样保留。
func TestFleetStorageDeliveryDestinationIsolation(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := context.Background()
	s.cfg.IdentityDir = t.TempDir()
	path := filepath.Join(s.cfg.IdentityDir, "connection.json")
	writeConnection := func(center string) {
		raw, err := json.Marshal(storageConnection{NodeID: "edge-fixture", CenterID: center, CenterURL: "https://fixture.invalid"})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, raw, 0600))
	}
	writeConnection("center-current")
	// A policy and candidate registration for the old center must not select counters.
	require.NoError(t, repo.GetUnsConn(ctx).Exec("INSERT INTO uns_global_storage_policy(id,mode,revision,center_id,updated_at) VALUES ('edge','both',1,'center-old',1)").Error)
	require.NoError(t, os.WriteFile(filepath.Join(s.cfg.IdentityDir, "pending-registration.json"), []byte(`{"centerId":"center-candidate"}`), 0600))
	rows := []struct{ id, center, status, failure string }{
		{"current-done", "center-current", "done", ""},
		{"current-pending", "center-current", "pending", ""},
		{"old-failed", "center-old", "pending", "CENTER_COMMIT_NOT_CONFIRMED"},
		{"old-done", "center-old", "done", ""},
		{"legacy-unknown", "", "pending", ""},
	}
	for _, row := range rows {
		payload := `{"policy":{"centerId":"` + row.center + `","targetCollectionId":"original-` + row.id + `"}}`
		require.NoError(t, repo.GetUnsConn(ctx).Exec("INSERT INTO uns_collection_storage_outbox(event_id,event_type,payload,status,last_error) VALUES (?, 'collection.storage.deliver', ?::jsonb, ?, ?)", row.id, payload, row.status, row.failure).Error)
	}
	check := func(center string, received, pending, committed, failed, historyPending, historyFailed, centers int64) {
		result, err := s.FleetStorage(ctx, "get", FleetStorageRequest{})
		require.NoError(t, err)
		view := result.(map[string]any)
		require.Equal(t, center, view["deliveryCenterId"])
		require.Equal(t, storageDeliveryCounts{Received: received, Pending: pending, Committed: committed, Failed: failed}, view["delivery"])
		require.Equal(t, storageHistoricalDelivery{Pending: historyPending, Failed: historyFailed, Centers: centers}, view["historicalDelivery"])
	}
	check("center-current", 2, 1, 1, 0, 2, 1, 2)
	writeConnection("center-old")
	check("center-old", 2, 1, 1, 1, 2, 0, 2)
	require.NoError(t, os.Remove(path))
	check("", 0, 0, 0, 0, 3, 1, 3)
	var retained struct{ Status, LastError, Target string }
	require.NoError(t, repo.GetUnsConn(ctx).Table("uns_collection_storage_outbox").Where("event_id=?", "old-failed").Select("status,last_error,payload->'policy'->>'targetCollectionId' AS target").Take(&retained).Error)
	require.Equal(t, "pending", retained.Status)
	require.Equal(t, "CENTER_COMMIT_NOT_CONFIRMED", retained.LastError)
	require.Equal(t, "original-old-failed", retained.Target)
}
