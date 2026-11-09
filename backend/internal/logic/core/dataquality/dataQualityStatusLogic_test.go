// 本文件覆盖 dataQualityStatusLogic.go 的总览响应组装：
// 正常路径：启用模块时返回 StatusView 的 summary 和规则聚合；
// 异常边界：空策略库返回结构完整的零值总览，而不是缺失字段。
package dataquality

import (
	"context"
	"testing"

	"backend/internal/config"
	domaindataquality "backend/internal/domain/dataquality"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestDataQualityStatusLogic_ReturnsOverviewShape(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	service, err := domaindataquality.New(context.Background(), config.DataQualityConf{
		Enabled: true, ShardCount: 1, MaxEnabledNodes: 100, IssueQueueSize: 8,
	})
	require.NoError(t, err)
	defer service.Close()
	svcCtx.App.DataQuality = service

	resp, err := NewDataQualityStatusLogic(svctest.CtxWithUser(1), svcCtx).
		DataQualityStatus(&types.DataQualityStatusReq{})
	require.NoError(t, err)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, domaindataquality.StatusSummary{}, data["summary"])
	require.Equal(t, map[string]int64{}, data["activeRuleCounts"])
	require.Empty(t, data["items"])
}

func TestDataQualityActiveSummaryLogic_ReturnsStableEmptyList(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	service, err := domaindataquality.New(context.Background(), config.DataQualityConf{
		Enabled: true, ShardCount: 1, MaxEnabledNodes: 100, IssueQueueSize: 8,
	})
	require.NoError(t, err)
	defer service.Close()
	svcCtx.App.DataQuality = service

	resp, err := NewDataQualityActiveSummaryLogic(svctest.CtxWithUser(1), svcCtx).
		DataQualityActiveSummary()
	require.NoError(t, err)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.NotNil(t, data["list"])
	require.Empty(t, data["list"])
}
