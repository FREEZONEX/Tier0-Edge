package unsmeta

// 使用隔离数据库测试 Topic facade 的类型约束及共享管理逻辑。
import (
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTopicFacade(t *testing.T) {
	svc := svctest.NewTestServiceContext(t)
	svc.App.UnsMeta = unsmeta.New()
	ctx := context.Background()
	doc := map[string]any{"name": "Temperature"}
	validated, err := execute(ctx, svc, &types.UnsTopicValidateReq{Document: doc})
	require.NoError(t, err)
	require.Equal(t, 200, validated.Code)
	created, err := execute(ctx, svc, &types.OpenapiTopicCreateReq{Document: doc})
	require.NoError(t, err)
	row := created.Data.(repo.UnsMeta)
	_, err = execute(ctx, svc, &types.OpenapiTopicDetailReq{Id: row.ID})
	require.NoError(t, err)
	_, err = execute(ctx, svc, &types.OpenapiTopicUpdateReq{Id: row.ID, ExpectedRevision: row.Revision, Document: map[string]any{"name": "Renamed"}})
	require.NoError(t, err)
	_, err = execute(ctx, svc, &types.OpenapiTopicUpdateReq{Id: row.ID, ExpectedRevision: row.Revision, Document: doc})
	require.Error(t, err)
	_, err = execute(ctx, svc, &types.OpenapiTopicUpdateReq{Id: row.ID, Document: doc})
	require.Error(t, err)
	_, err = execute(ctx, svc, &types.OpenapiTopicCreateReq{ExpectedRevision: 1, Document: doc})
	require.Error(t, err)
	list, err := execute(ctx, svc, &types.OpenapiTopicListReq{Page: 1, Size: 1})
	require.NoError(t, err)
	require.EqualValues(t, 1, list.Data.(map[string]any)["total"])
	folder, err := svc.App.UnsMeta.Save(ctx, "", "folder", 0, unsmeta.Document{Name: "folder"})
	require.NoError(t, err)
	_, err = execute(ctx, svc, &types.OpenapiTopicDetailReq{Id: folder.ID})
	require.Error(t, err)
	_, err = execute(ctx, svc, &types.OpenapiUnsMetaListReq{Kind: "source"})
	require.Error(t, err)
	_, err = execute(ctx, svc, struct{}{})
	require.Error(t, err)
}
