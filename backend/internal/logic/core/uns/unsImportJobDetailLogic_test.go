// TASK-028 unsImportJobDetailLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖导入任务详情链路（Job → GetAsyncJob）。
// 正常路径：已创建任务可按 jobKey 查询到并返回 job。
// 异常情况：不存在的 jobKey 返回 404。
package uns

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsImportJobDetail_Success(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	created, err := NewUnsImportJobCreateLogic(ctx, svcCtx).UnsImportJobCreate(
		&types.UnsImportJobReq{DryRun: true, SourceFileId: 456})
	require.NoError(t, err)
	job := created.Data.(map[string]any)["job"].(repo.AsyncJob)

	resp, err := NewUnsImportJobDetailLogic(ctx, svcCtx).UnsImportJobDetail(
		&types.JobIdReq{JobId: job.JobKey})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	got := resp.Data.(map[string]any)["job"].(repo.AsyncJob)
	require.Equal(t, job.JobKey, got.JobKey)
	require.Equal(t, "uns.import", got.JobType)
}

func TestUnsImportJobDetail_NotFound(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	_, err := NewUnsImportJobDetailLogic(ctx, svcCtx).UnsImportJobDetail(
		&types.JobIdReq{JobId: "no-such-job"})
	require.Equal(t, 404, httpErrorStatus(t, err))
}
