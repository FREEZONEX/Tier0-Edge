// fleet_nodes_test.go 覆盖集群节点管理（fleet_nodes.go）：
// DB 路径：CreateFleetNode 校验与配额限制、FleetNodes 状态过滤与空库摘要、
//
//	节点详情/更新/删除/恢复/UNS 作用域查询（含经 repo 直建节点的完整
//	正向流程：创建 → 详情 → 更新 → 列表 → 删除）。
//
// 纯函数：名称/描述校验、状态白名单、分页归一化、响应/快照摘要构造、
//
//	删除时间换算、UNS 作用域响应构造。
//
// 说明：CreateFleetNode 的成功分支依赖 Enterprise bundle（BranchLimit>0），
// 测试环境无 bundle（BranchLimit=0）只能覆盖配额拒绝路径；正向流程经
// fleetRepo.CreateNodeWithConnection（BranchLimit=3）等价覆盖。
package cluster

import (
	"encoding/json"
	"strings"
	"testing"

	"backend/internal/fleetprotocol"
	"backend/internal/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateFleetNode_ValidationAndQuota(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.CreateFleetNode(ctx, CreateFleetNodeCommand{Name: "", UserID: 1})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.CreateFleetNode(ctx, CreateFleetNodeCommand{Name: "ok", Description: strings.Repeat("d", 513), UserID: 1})
	require.ErrorIs(t, err, ErrInvalid)

	_, err = s.CreateFleetNode(ctx, CreateFleetNodeCommand{Name: "edge-1", UserID: 1})
	require.ErrorIs(t, err, ErrFleetNodeQuotaExceeded)
}

func TestFleetNodes_InvalidStatusAndEmpty(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.FleetNodes(ctx, ListFleetNodesQuery{Status: "bogus"})
	require.ErrorIs(t, err, ErrInvalid)
	resp, err := s.FleetNodes(ctx, ListFleetNodesQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp["total"])
	assert.Equal(t, 1, resp["page"])
	assert.Equal(t, 20, resp["size"])
	summary := resp["summary"].(map[string]any)
	assert.Equal(t, int64(0), summary["active"])
	assert.Equal(t, int64(0), summary["online"])
	assert.Equal(t, int64(0), summary["offline"])
	assert.Equal(t, int64(0), summary["unjoined"])

	assert.Equal(t, int64(0), summary["limit"])
}

func TestFleetNodeCRUD_Invalid(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.FleetNode(ctx, "", false)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	_, err = s.UpdateFleetNode(ctx, UpdateFleetNodeCommand{FleetNodeID: "", Name: "x", UserID: 1})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.UpdateFleetNode(ctx, UpdateFleetNodeCommand{FleetNodeID: "fn_x", Name: "", UserID: 1})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.DeleteFleetNode(ctx, "", 1)
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.RestoreFleetNode(ctx, "", 1)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestFleetNode_NotFound(t *testing.T) {
	_, s := newTestCluster(t)
	_, err := s.FleetNode(backgroundCtx(), "fn_nope", false)
	require.Error(t, err)
}

func TestFleetNodeRestore_NotFound(t *testing.T) {
	_, s := newTestCluster(t)
	_, err := s.RestoreFleetNode(backgroundCtx(), "fn_nope", 1)
	require.Error(t, err)
}

func TestFleetNodeCRUD_Success(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	created, err := s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_flow1",
		Name:        "edge-1",
		Description: "d",
		CenterType:  "enterprise",
		ActorID:     1,
		BranchLimit: 3,
	})
	require.NoError(t, err)
	assert.Equal(t, "fn_flow1", created.Node.FleetNodeID)
	assert.NotEmpty(t, created.Client.ClientID)

	resp, err := s.FleetNode(ctx, "fn_flow1", false)
	require.NoError(t, err)
	assert.Equal(t, "fn_flow1", resp["fleetNodeID"])
	assert.Equal(t, "edge-1", resp["name"])
	assert.Equal(t, created.Client.ClientID, resp["credentialID"])
	assert.Equal(t, "active", resp["credentialStatus"])

	resp, err = s.UpdateFleetNode(ctx, UpdateFleetNodeCommand{FleetNodeID: "fn_flow1", Name: "edge-1b", Description: "d2", UserID: 1})
	require.NoError(t, err)
	assert.Equal(t, "edge-1b", resp["name"])

	listResp, err := s.FleetNodes(ctx, ListFleetNodesQuery{Keyword: "edge-1b"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), listResp["total"])
	summary := listResp["summary"].(map[string]any)
	assert.Equal(t, int64(1), summary["unjoined"])
	assert.Equal(t, int64(1), summary["active"])

	delResp, err := s.DeleteFleetNode(ctx, "fn_flow1", 1)
	require.NoError(t, err)
	assert.Equal(t, true, delResp["deleted"])

	_, err = s.FleetNode(ctx, "fn_flow1", false)
	require.Error(t, err)
}

func TestValidFleetNodeNameAndDescription(t *testing.T) {
	assert.False(t, validFleetNodeNameAndDescription("", ""))
	assert.False(t, validFleetNodeNameAndDescription("x", strings.Repeat("d", 513)))
	assert.False(t, validFleetNodeNameAndDescription(strings.Repeat("名", 129), ""))
	assert.True(t, validFleetNodeNameAndDescription("edge-1", "desc"))
}

func TestValidFleetNodeStatus(t *testing.T) {
	assert.True(t, validFleetNodeStatus(repo.FleetNodeStatusUnjoined))
	assert.True(t, validFleetNodeStatus(repo.FleetNodeStatusOnline))
	assert.True(t, validFleetNodeStatus(repo.FleetNodeStatusOffline))
	assert.True(t, validFleetNodeStatus(repo.FleetNodeStatusDeleted))
	assert.False(t, validFleetNodeStatus("bogus"))
}

func TestNormalizeFleetPagination(t *testing.T) {
	page, size := normalizeFleetPagination(0, 0)
	assert.Equal(t, 1, page)
	assert.Equal(t, 20, size)
	page, size = normalizeFleetPagination(3, 500)
	assert.Equal(t, 3, page)
	assert.Equal(t, 100, size)
	page, size = normalizeFleetPagination(2, 30)
	assert.Equal(t, 2, page)
	assert.Equal(t, 30, size)
}

func TestFleetNodeResponse(t *testing.T) {
	view := repo.FleetNodeView{
		FleetNode: repo.FleetNode{
			FleetNodeID: "fn_x", Name: "n", Status: repo.FleetNodeStatusOnline,
			AssignmentRevision: 2, ConnectClientID: 5,
		},
		CredentialID: "cred-1", CredentialStatus: "active",
	}
	view.CreatedBy = 1
	view.UpdatedBy = 2
	resp := fleetNodeResponse(view)
	assert.Equal(t, "fn_x", resp["fleetNodeID"])
	assert.Equal(t, "1", resp["createdBy"])
	assert.Equal(t, "2", resp["updatedBy"])
	assert.Equal(t, int64(0), resp["deletedAt"])
	assert.Equal(t, "cred-1", resp["credentialID"])
}

func TestFleetDeletedAtMilli(t *testing.T) {
	assert.Equal(t, int64(0), fleetDeletedAtMilli(0))
	assert.Equal(t, int64(0), fleetDeletedAtMilli(-1))
	assert.Equal(t, int64(1234000), fleetDeletedAtMilli(1234))
}

func TestFleetNodeSnapshotSummaries(t *testing.T) {
	validPayload, _ := json.Marshal(fleetprotocol.SnapshotPayload{
		Containers: fleetprotocol.ContainerSnapshot{Containers: []fleetprotocol.Summary{
			{ContainerID: "c1", State: "running"},
			{ContainerID: "c2", State: "exited"},
			{ContainerID: "c3", State: "running", HealthStatus: "unhealthy"},
		}},
	})
	summaries := fleetNodeSnapshotSummaries([]repo.FleetContainerSnapshot{
		{FleetNodeID: "fn_x", ObservedAt: 123, Payload: validPayload},
		{FleetNodeID: "fn_bad", ObservedAt: 456, Payload: json.RawMessage(`{bad`)},
	})
	require.Len(t, summaries, 1)
	summary := summaries["fn_x"]
	assert.Equal(t, int64(123), summary.observedAt)
	assert.Equal(t, 3, summary.containerCount)
	assert.Equal(t, 2, summary.unhealthyContainerCount)
}
