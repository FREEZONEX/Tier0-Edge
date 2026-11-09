// fleet_remote_containers_test.go 覆盖远端容器管理（fleet_remote_containers.go）：
// DB 路径：FleetNodeContainers/Container/ContainerLogs/RestartFleetNodeContainer
//
//	的入参校验、节点不存在、快照缺失（FLEET_AGENT_UNAVAILABLE）、
//	容器不存在、重启被拒、节点离线等错误分支；带内存快照的
//	列表/详情 stale 路径。
//
// 纯函数：重启永久拒绝判定、容器快照摘要归一化、异常容器统计、
//
//	响应构造（remoteSnapshotResponse/remoteDetailResponse/structToMap）。
//
// 说明：真实 fleet agent 会话命令执行（session.execute/requestSnapshot）依赖
// 活动 MQTT 会话，测试仅注入内存快照覆盖离线分支。
package cluster

import (
	"net/http"
	"testing"

	"backend/internal/fleetprotocol"
	"backend/internal/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleetNodeContainers_InvalidAndNodeNotFound(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.FleetNodeContainers(ctx, "", false)
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.FleetNodeContainers(ctx, "fn_nope", false)
	require.Error(t, err)
}

func TestFleetNodeContainers_NoSnapshotUnavailable(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_ct1", Name: "edge-1", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	_, err = s.FleetNodeContainers(ctx, "fn_ct1", false)
	var responseErr *FleetAgentResponseError
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusServiceUnavailable, responseErr.StatusCode)
	assert.Equal(t, "FLEET_AGENT_UNAVAILABLE", responseErr.Code)
}

func TestFleetNodeContainers_StaleSnapshot(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_ct2", Name: "edge-2", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	s.fleet.snapshots["fn_ct2"] = fleetprotocol.SnapshotPayload{
		Host: fleetprotocol.HostSnapshot{Hostname: "edge-host"},
		Containers: fleetprotocol.ContainerSnapshot{Containers: []fleetprotocol.Summary{
			{ContainerID: "c1", State: "running"},
		}},
	}
	resp, err := s.FleetNodeContainers(ctx, "fn_ct2", false)
	require.NoError(t, err)
	assert.Equal(t, true, resp["stale"])
	assert.Equal(t, "edge-host", resp["host"].(fleetprotocol.HostSnapshot).Hostname)
	summary := resp["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["total"])
}

func TestFleetNodeContainer_InvalidAndNodeNotFound(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.FleetNodeContainer(ctx, "", "c1")
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.FleetNodeContainer(ctx, "fn_x", "")
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.FleetNodeContainer(ctx, "fn_nope", "c1")
	require.Error(t, err)
}

func TestFleetNodeContainer_OfflineWithCachedDetail(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_ct3", Name: "edge-3", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	s.fleet.snapshots["fn_ct3"] = fleetprotocol.SnapshotPayload{
		Containers: fleetprotocol.ContainerSnapshot{Containers: []fleetprotocol.Summary{
			{ContainerID: "c1", State: "running"},
		}},
	}
	// 无活动会话且无缓存详情 → FLEET_NODE_OFFLINE。
	_, err = s.FleetNodeContainer(ctx, "fn_ct3", "c1")
	var responseErr *FleetAgentResponseError
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusServiceUnavailable, responseErr.StatusCode)
	assert.Equal(t, "FLEET_NODE_OFFLINE", responseErr.Code)

	// 有缓存详情 → 返回 stale 详情。
	s.fleet.details["fn_ct3"] = map[string]fleetprotocol.Detail{
		"c1": {Summary: fleetprotocol.Summary{ContainerID: "c1", ServiceName: "svc"}, ImageID: "img-1"},
	}
	resp, err := s.FleetNodeContainer(ctx, "fn_ct3", "c1")
	require.NoError(t, err)
	assert.Equal(t, true, resp["stale"])
	assert.Equal(t, "img-1", resp["imageID"])

	// 快照中不存在的容器 → 404。
	_, err = s.FleetNodeContainer(ctx, "fn_ct3", "nope")
	responseErr = &FleetAgentResponseError{}
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusNotFound, responseErr.StatusCode)
}

func TestFleetNodeContainerLogs_InvalidAndOffline(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.FleetNodeContainerLogs(ctx, FleetNodeContainerLogsCommand{FleetNodeID: "", ContainerID: "c1"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.FleetNodeContainerLogs(ctx, FleetNodeContainerLogsCommand{FleetNodeID: "fn_x", ContainerID: ""})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.FleetNodeContainerLogs(ctx, FleetNodeContainerLogsCommand{FleetNodeID: "fn_nope", ContainerID: "c1"})
	require.Error(t, err)

	// 节点存在但无会话 → FLEET_NODE_OFFLINE。
	_, err = s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_ct4", Name: "edge-4", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	s.fleet.snapshots["fn_ct4"] = fleetprotocol.SnapshotPayload{
		Containers: fleetprotocol.ContainerSnapshot{Containers: []fleetprotocol.Summary{
			{ContainerID: "c1", State: "running"},
		}},
	}
	_, err = s.FleetNodeContainerLogs(ctx, FleetNodeContainerLogsCommand{FleetNodeID: "fn_ct4", ContainerID: "c1"})
	var responseErr *FleetAgentResponseError
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, "FLEET_NODE_OFFLINE", responseErr.Code)
}

func TestRestartFleetNodeContainer_InvalidAndForbidden(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.RestartFleetNodeContainer(ctx, FleetNodeContainerRestartCommand{})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.RestartFleetNodeContainer(ctx, FleetNodeContainerRestartCommand{FleetNodeID: "fn_x", ContainerID: ""})
	require.ErrorIs(t, err, ErrInvalid)

	_, err = s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_ct5", Name: "edge-5", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	s.fleet.snapshots["fn_ct5"] = fleetprotocol.SnapshotPayload{
		Containers: fleetprotocol.ContainerSnapshot{Containers: []fleetprotocol.Summary{
			{ContainerID: "c-no-restart", State: "running", Restartable: false},
			{ContainerID: "c-backend", State: "running", Restartable: true, ServiceName: "backend"},
			{ContainerID: "c-ok", State: "running", Restartable: true, ServiceName: "app"},
			{ContainerID: "c-emqx", State: "running", Restartable: true, ServiceName: "emqx"},
		}},
	}

	// 不可重启容器 → 403。
	_, err = s.RestartFleetNodeContainer(ctx, FleetNodeContainerRestartCommand{FleetNodeID: "fn_ct5", ContainerID: "c-no-restart", UserID: 1})
	var responseErr *FleetAgentResponseError
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusForbidden, responseErr.StatusCode)

	// 永久拒绝名单内（backend）→ 403。
	_, err = s.RestartFleetNodeContainer(ctx, FleetNodeContainerRestartCommand{FleetNodeID: "fn_ct5", ContainerID: "c-backend", UserID: 1})
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusForbidden, responseErr.StatusCode)

	// 可重启但节点离线 → FLEET_NODE_OFFLINE。
	_, err = s.RestartFleetNodeContainer(ctx, FleetNodeContainerRestartCommand{FleetNodeID: "fn_ct5", ContainerID: "c-ok", UserID: 1})
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, "FLEET_NODE_OFFLINE", responseErr.Code)
	_, err = s.RestartFleetNodeContainer(ctx, FleetNodeContainerRestartCommand{FleetNodeID: "fn_ct5", ContainerID: "c-emqx", UserID: 1})
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, "FLEET_NODE_OFFLINE", responseErr.Code)

	// 容器不存在 → 404。
	_, err = s.RestartFleetNodeContainer(ctx, FleetNodeContainerRestartCommand{FleetNodeID: "fn_ct5", ContainerID: "nope", UserID: 1})
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusNotFound, responseErr.StatusCode)
}

func TestFleetContainerRestartPermanentlyDenied(t *testing.T) {
	denied := []fleetprotocol.Summary{
		{ServiceName: "backend"},
		{ServiceName: "pg"},
		{DisplayName: "PostgreSQL"},
		{ServiceName: "api", Labels: map[string]string{"compose.service": "tsdb"}},
		{ComposeService: "timescaledb"},
		{ServiceName: "redis"},
	}
	for _, summary := range denied {
		assert.True(t, fleetContainerRestartPermanentlyDenied(summary), "should deny %+v", summary)
	}
	for _, serviceName := range []string{"app-gis", "emqx", "marimo", "sourceflow", "eventflow", "uns-agent-api", "database", "db", "local-db-init"} {
		assert.False(t, fleetContainerRestartPermanentlyDenied(fleetprotocol.Summary{ServiceName: serviceName}), serviceName)
	}
	assert.False(t, fleetContainerRestartPermanentlyDenied(fleetprotocol.Summary{}))
}

func TestNormalizeFleetContainerSummary(t *testing.T) {
	var nilSnapshot *fleetprotocol.ContainerSnapshot
	normalizeFleetContainerSummary(nilSnapshot) // 不应 panic

	snapshot := fleetprotocol.ContainerSnapshot{Containers: []fleetprotocol.Summary{
		{ContainerID: "c1", State: "running"},
		{ContainerID: "c2", State: "exited"},
		{ContainerID: "c3", State: "running", HealthStatus: "unhealthy"},
		{ContainerID: "c4", State: "RUNNING"},
	}}
	normalizeFleetContainerSummary(&snapshot)
	assert.Equal(t, 4, snapshot.Summary.Total)
	assert.Equal(t, 3, snapshot.Summary.Running)
	assert.Equal(t, 2, snapshot.Summary.Unhealthy)
}

func TestFleetAbnormalContainerCount(t *testing.T) {
	containers := []fleetprotocol.Summary{
		{State: "running"},
		{State: "exited"},
		{State: "running", HealthStatus: "unhealthy"},
		{State: "running", HealthStatus: "healthy"},
	}
	assert.Equal(t, 2, fleetAbnormalContainerCount(containers))
	assert.Equal(t, 0, fleetAbnormalContainerCount(nil))
}

func TestRemoteResponsesAndStructToMap(t *testing.T) {
	detail := fleetprotocol.Detail{
		Summary: fleetprotocol.Summary{ContainerID: "c1", State: "running"},
		ImageID: "img-9",
	}
	resp := remoteDetailResponse(detail, true)
	assert.Equal(t, true, resp["stale"])
	assert.Equal(t, "img-9", resp["imageID"])

	raw := structToMap(map[string]any{"a": 1})
	assert.Equal(t, map[string]any{"a": float64(1)}, raw)
	// 无法序列化的值 → 空 map。
	assert.Empty(t, structToMap(make(chan int)))
}

func TestContainerDeploymentBackupsRemainInspectable(t *testing.T) {
	containers := []fleetprotocol.Summary{
		{ContainerID: "old", ContainerName: "edge-backend-before-20261027093319", ServiceName: "backend", State: "exited"},
		{ContainerID: "current", ContainerName: "edge-backend", ServiceName: "backend", State: "stopped"},
		{ContainerID: "other", ContainerName: "edge-backend-replica", ServiceName: "backend", State: "running"},
		{ContainerID: "unmatched", ContainerName: "db-before-20261027093319", ServiceName: "db", State: "exited"},
		{ContainerID: "liveBackup", ContainerName: "edge-backend-before-20261027093320", ServiceName: "backend", State: "running"},
	}
	counts := fleetprotocol.SummarizeContainers(containers)
	assert.Equal(t, 4, counts.Total)
	assert.Equal(t, 1, counts.Historical)
	assert.Equal(t, 2, counts.Running)
	assert.Equal(t, 2, counts.Unhealthy)
	assert.Equal(t, "current", containers[0].SupersededBy)
	assert.Empty(t, containers[1].SupersededBy)
	assert.Empty(t, containers[3].SupersededBy)
	assert.Empty(t, containers[4].SupersededBy)
	containers[1].ServiceName = "other-service"
	assert.Equal(t, 0, fleetprotocol.SummarizeContainers(containers).Historical)
}
