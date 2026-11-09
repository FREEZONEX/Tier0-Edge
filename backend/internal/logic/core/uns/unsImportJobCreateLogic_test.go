// TASK-028 unsImportJobCreateLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖导入任务创建链路（CreateImportJob → async_job + outbox 入队）。
// 正常路径：sourceFileId>0 时创建任务落库 sys_async_job（job_type=uns.import），
// 返回 job 含 jobKey。
// 异常情况：sourceFileId<=0 返回 400（ErrInvalid）。
package uns

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsImportJobCreate_Success(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	resp, err := NewUnsImportJobCreateLogic(ctx, svcCtx).UnsImportJobCreate(
		&types.UnsImportJobReq{DryRun: true, SourceFileId: 123})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	job := resp.Data.(map[string]any)["job"].(repo.AsyncJob)
	require.Equal(t, "uns.import", job.JobType)
	require.NotEmpty(t, job.JobKey)

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("sys_async_job").
		Where("job_key = ?", job.JobKey).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestUnsImportJobCreate_InvalidSourceFile(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	_, err := NewUnsImportJobCreateLogic(ctx, svcCtx).UnsImportJobCreate(
		&types.UnsImportJobReq{DryRun: false, SourceFileId: 0})
	require.Equal(t, 400, httpErrorStatus(t, err))
}
