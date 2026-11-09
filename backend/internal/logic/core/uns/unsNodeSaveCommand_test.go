// TASK-028 unsNodeSaveCommand.go 单测（纯函数）。
// 目标：覆盖 BuildUnsNodeSaveCommand 的请求→领域命令字段映射。
// 正常路径：全部字段一一映射；WithFlow/AddFlow/MockData 任一置位均联动 AddFlow+MockData；
// UserID 取自 ctx Subject；Persistence 指针透传。
// 异常情况：ctx 无登录用户时 UserID 为 0。
package uns

import (
	"context"
	"testing"

	"backend/internal/contextx"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestBuildUnsNodeSaveCommand_FieldMapping(t *testing.T) {
	persistence := true
	cmd := BuildUnsNodeSaveCommand(svctest.CtxWithUser(7), &types.UnsNodeSaveReq{
		NodeId:           11,
		ParentId:         22,
		Name:             "temp",
		DisplayName:      "Temp Sensor",
		Description:      "desc",
		Alias:            "alias-x",
		Namespace:        "a/b",
		NodeType:         "file",
		TopicType:        "metric",
		Schema:           `{"fields":[]}`,
		ExtendProperties: `{"k":"v"}`,
		LabelIds:         []int64{1, 2},
		AssetFileIds:     []int64{3},
		Persistence:      &persistence,
		DataQualityPolicy: &types.UnsDataQualityPolicyReq{
			NotifyOnIssue: true, ExpectedIntervalMs: 2000, MissingGraceMs: 1000,
		},
	})
	require.Equal(t, int64(11), cmd.ID)
	require.Equal(t, int64(22), cmd.ParentID)
	require.Equal(t, "temp", cmd.Name)
	require.Equal(t, "Temp Sensor", cmd.DisplayName)
	require.Equal(t, "desc", cmd.Description)
	require.Equal(t, "alias-x", cmd.Alias)
	require.Equal(t, "a/b", cmd.Namespace)
	require.Equal(t, "file", cmd.NodeType)
	require.Equal(t, "metric", cmd.TopicType)
	require.Equal(t, `{"fields":[]}`, cmd.Schema)
	require.Equal(t, `{"k":"v"}`, cmd.ExtendProperties)
	require.Equal(t, []int64{1, 2}, cmd.LabelIDs)
	require.Equal(t, []int64{3}, cmd.AssetFileIDs)
	require.NotNil(t, cmd.EnableHistory)
	require.True(t, *cmd.EnableHistory)
	require.NotNil(t, cmd.DataQualityPolicy)
	require.False(t, cmd.DataQualityPolicy.NotifyOnIssue)
	require.Equal(t, int64(2000), cmd.DataQualityPolicy.ExpectedIntervalMs)
	require.Equal(t, int64(1000), cmd.DataQualityPolicy.MissingGraceMs)
	require.Equal(t, int64(7), cmd.UserID)
	require.False(t, cmd.AddFlow)
	require.False(t, cmd.MockData)
}

func TestEmbeddedDataQualityPolicy_RequiresManagePermission(t *testing.T) {
	request := &types.UnsNodeSaveReq{DataQualityPolicy: &types.UnsDataQualityPolicyReq{ExpectedIntervalMs: 1000}}
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{
		UserID: 1, ResourceKeys: map[string]struct{}{"uns.manage": {}},
	})
	// A caller with node write access cannot bypass the policy API through create/update.
	_, err := NewUnsNodeCreateLogic(ctx, nil).UnsNodeCreate(request)
	require.Equal(t, 403, httpErrorStatus(t, err))
	_, err = NewUnsNodeUpdateLogic(ctx, nil).UnsNodeUpdate(request)
	require.Equal(t, 403, httpErrorStatus(t, err))
	ctx = contextx.WithSubject(ctx, contextx.Subject{
		UserID: 1, ResourceKeys: map[string]struct{}{"uns.dataQuality.manage": {}},
	})
	require.NoError(t, authorizeDataQualityPolicy(ctx, request))
	require.NoError(t, authorizeDataQualityPolicy(context.Background(), &types.UnsNodeSaveReq{}))
}

func TestBuildUnsNodeSaveCommand_FlowFlagsAnySet(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		withFlow, addFlow, mockData bool
	}{
		{"withFlow", true, false, false},
		{"addFlow", false, true, false},
		{"mockData", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := BuildUnsNodeSaveCommand(svctest.CtxWithUser(1), &types.UnsNodeSaveReq{
				NodeType: "file",
				WithFlow: tc.withFlow,
				AddFlow:  tc.addFlow,
				MockData: tc.mockData,
			})
			require.True(t, cmd.AddFlow, "AddFlow 应为 true")
			require.True(t, cmd.MockData, "MockData 应为 true")
		})
	}
}

func TestBuildUnsNodeSaveCommand_NoUserInCtx(t *testing.T) {
	cmd := BuildUnsNodeSaveCommand(context.Background(), &types.UnsNodeSaveReq{Name: "x", NodeType: "folder"})
	require.Equal(t, int64(0), cmd.UserID)
	require.False(t, cmd.AddFlow)
	require.False(t, cmd.MockData)
	require.Nil(t, cmd.EnableHistory)
}
