// TASK-028 unsExportJobCreateLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖导出任务创建链路（CreateExportJob → async_job + outbox 入队）。
// 正常路径：创建任务落库 sys_async_job（job_type=uns.export），返回 job 含 jobKey。
// 异常情况：无（rootNodeId 不校验，为 0 也允许）。
package uns

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsExportJobCreate_Success(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	resp, err := NewUnsExportJobCreateLogic(ctx, svcCtx).UnsExportJobCreate(
		&types.UnsExportJobReq{RootNodeId: 0})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	job := resp.Data.(map[string]any)["job"].(repo.AsyncJob)
	require.Equal(t, "uns.export", job.JobType)
	require.NotEmpty(t, job.JobKey)
	require.Equal(t, int64(1), job.CreatedBy)

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("sys_async_job").
		Where("job_key = ?", job.JobKey).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
