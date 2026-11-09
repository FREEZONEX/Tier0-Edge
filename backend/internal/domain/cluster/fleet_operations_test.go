// fleet_operations_test.go 覆盖容器操作管理（fleet_operations.go）：
// DB 路径：FleetOperation 查询与节点归属校验、FleetOperations 分页查询、
//
//	createFleetOperation 幂等/冲突判定。
//
// 纯函数：fleetOperationFailure 错误分类、fleetOperationResponse 响应构造。
// executeFleetRestart 依赖真实 fleet agent 会话（requireSession），仅覆盖
// 无会话路径无法触达，注释说明未覆盖原因。
package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"backend/internal/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleetOperation_Invalid(t *testing.T) {
	_, s := newTestCluster(t)
	_, err := s.FleetOperation(backgroundCtx(), "", "op1")
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.FleetOperation(backgroundCtx(), "fn_x", " ")
	require.ErrorIs(t, err, ErrInvalid)
}

func TestFleetOperation_NodeNotFound(t *testing.T) {
	_, s := newTestCluster(t)
	_, err := s.FleetOperation(backgroundCtx(), "fn_nope", "op1")
	require.Error(t, err)
}

func TestFleetOperations_InvalidAndNodeNotFound(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.FleetOperations(ctx, " ", 1, 20)
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.FleetOperations(ctx, "fn_nope", 1, 20)
	require.Error(t, err)
}

func TestFleetOperation_Success(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_op1", Name: "edge-1", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	op, created, err := s.fleetRepo.CreateOperation(ctx, repo.CreateFleetOperationInput{
		OperationID: "op-1", FleetNodeID: "fn_op1", ContainerID: "c1",
		OperationType: "container.restart", ActorID: 1, RequestJSON: jsonRaw("{}"),
	})
	require.NoError(t, err)
	require.True(t, created)
	_ = op

	resp, err := s.FleetOperation(ctx, "fn_op1", "op-1")
	require.NoError(t, err)
	assert.Equal(t, "op-1", resp["operationID"])
	assert.Equal(t, "fn_op1", resp["fleetNodeID"])
	assert.Equal(t, "container.restart", resp["operationType"])
	assert.Equal(t, repo.FleetOperationStatusQueued, resp["status"])
	assert.Equal(t, "1", resp["actorID"])

	// 操作不属于该节点 → 404。
	_, err = s.FleetOperation(ctx, "fn_other", "op-1")
	require.ErrorIs(t, err, repo.ErrNotFound)
}

func TestFleetOperations_List(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_op2", Name: "edge-2", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	_, _, err = s.fleetRepo.CreateOperation(ctx, repo.CreateFleetOperationInput{
		OperationID: "op-2", FleetNodeID: "fn_op2", ContainerID: "c2",
		OperationType: "container.restart", ActorID: 1, RequestJSON: jsonRaw("{}"),
	})
	require.NoError(t, err)

	// 页大小钳制：size>100 → 100。
	resp, err := s.FleetOperations(ctx, "fn_op2", 1, 500)
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp["total"])
	assert.Equal(t, int64(1), resp["page"])
	assert.Equal(t, int64(100), resp["size"])
}

func TestCreateFleetOperation_ConflictOnMismatch(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	// 先创建两个节点，满足 fleet_operation.fleet_node_id 外键约束。
	for _, n := range []string{"fn_x", "fn_y"} {
		_, err := s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
			FleetNodeID: n, Name: n, CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
		})
		require.NoError(t, err)
	}
	_, created, err := s.createFleetOperation(ctx, repo.CreateFleetOperationInput{
		OperationID: "op-dup", FleetNodeID: "fn_x", ContainerID: "c1",
		OperationType: "container.restart", ActorID: 1, RequestJSON: jsonRaw("{}"),
	})
	require.NoError(t, err)
	require.True(t, created)
	// 相同 operationID 但不同节点/容器 → 冲突。
	_, created, err = s.createFleetOperation(ctx, repo.CreateFleetOperationInput{
		OperationID: "op-dup", FleetNodeID: "fn_y", ContainerID: "c2",
		OperationType: "container.restart", ActorID: 1, RequestJSON: jsonRaw("{}"),
	})
	require.False(t, created)
	var responseErr *FleetAgentResponseError
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusConflict, responseErr.StatusCode)
	assert.Equal(t, "FLEET_CONTAINER_OPERATION_CONFLICT", responseErr.Code)
}

func TestFleetOperationFailure(t *testing.T) {
	status, code := fleetOperationFailure(nil)
	assert.Equal(t, repo.FleetOperationStatusFailed, status)
	assert.Equal(t, "FLEET_AGENT_COMMAND_FAILED", code)

	status, code = fleetOperationFailure(context.Canceled)
	assert.Equal(t, repo.FleetOperationStatusCanceled, status)
	assert.Equal(t, "FLEET_OPERATION_CANCELED", code)

	status, code = fleetOperationFailure(context.DeadlineExceeded)
	assert.Equal(t, repo.FleetOperationStatusTimedOut, status)
	assert.Equal(t, "FLEET_OPERATION_TIMEOUT", code)

	status, code = fleetOperationFailure(&FleetAgentResponseError{Code: "FLEET_OPERATION_TIMEOUT"})
	assert.Equal(t, repo.FleetOperationStatusTimedOut, status)

	status, code = fleetOperationFailure(&FleetAgentResponseError{Code: "FLEET_CONTAINER_NOT_FOUND"})
	assert.Equal(t, repo.FleetOperationStatusFailed, status)
	assert.Equal(t, "FLEET_CONTAINER_NOT_FOUND", code)

	status, code = fleetOperationFailure(errors.New("other"))
	assert.Equal(t, repo.FleetOperationStatusFailed, status)
	assert.Equal(t, "FLEET_AGENT_COMMAND_FAILED", code)
}

func TestFleetOperationResponse(t *testing.T) {
	now := time.Now()
	record := repo.FleetOperation{
		OperationID: "op-1", FleetNodeID: "fn_x", ContainerID: "c1", OperationType: "container.restart",
		Status: repo.FleetOperationStatusSucceeded, ActorID: 7, StartedAt: 1, FinishedAt: 2,
		RequestJSON: jsonRaw(`{"tail":10}`), ResultJSON: jsonRaw(`{"ok":true}`), ErrorCode: "",
		CreatedTime: now, UpdatedTime: now,
	}
	resp := fleetOperationResponse(record)
	assert.Equal(t, "op-1", resp["operationID"])
	assert.Equal(t, "7", resp["actorID"])
	assert.Equal(t, json.RawMessage(`{"tail":10}`), resp["request"])
	assert.Equal(t, json.RawMessage(`{"ok":true}`), resp["result"])
	assert.NotContains(t, resp, "errorCode")

	record.ErrorCode = "FLEET_NODE_OFFLINE"
	resp = fleetOperationResponse(record)
	assert.Equal(t, "FLEET_NODE_OFFLINE", resp["errorCode"])
	// 空 request/result 不输出。
	record.RequestJSON = nil
	record.ResultJSON = nil
	resp = fleetOperationResponse(record)
	assert.NotContains(t, resp, "request")
	assert.NotContains(t, resp, "result")
}
