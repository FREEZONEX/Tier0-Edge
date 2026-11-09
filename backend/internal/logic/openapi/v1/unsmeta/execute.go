package unsmeta

import (
	respx "backend/internal/httpx"
	core "backend/internal/logic/core/unsmeta"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"
	"context"
	"net/http"
)

func execute(ctx context.Context, service *svc.ServiceContext, request any) (*types.UnsMetaEnvelope, error) {
	switch req := request.(type) {
	case *types.OpenapiTopicCreateReq:
		if req.ExpectedRevision != 0 {
			return nil, logicx.Error(repo.ErrConflict)
		}
		request = &types.UnsMetaSaveReq{Kind: "topic", ExpectedRevision: 0, Document: req.Document}
	case *types.OpenapiTopicUpdateReq:
		if req.ExpectedRevision <= 0 {
			return nil, logicx.Error(repo.ErrConflict)
		}
		request = &types.UnsMetaSaveReq{Id: req.Id, Kind: "topic", ExpectedRevision: req.ExpectedRevision, Document: req.Document}
	case *types.OpenapiTopicListReq:
		request = &types.UnsMetaListReq{Kind: "topic", Page: req.Page, Size: req.Size}
	case *types.OpenapiTopicDetailReq:
		row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, req.Id, false)
		if err != nil {
			return nil, logicx.Error(err)
		}
		if row.Kind != "topic" {
			return nil, logicx.Error(repo.ErrNotFound)
		}
		request = &types.UnsMetaViewReq{Id: req.Id}
	case *types.OpenapiUnsMetaListReq:
		value := types.UnsMetaListReq(*req)
		request = &value
	case *types.OpenapiUnsRecordListReq:
		value := types.UnsRecordListReq(*req)
		request = &value
	case *types.OpenapiUnsSampleListReq:
		value := types.UnsSampleListReq(*req)
		request = &value
	case *types.UnsMetaSaveReq, *types.UnsMetaViewReq, *types.UnsMetaStateReq, *types.UnsCollectionCreateReq, *types.UnsTopicValidateReq:
	default:
		return nil, respx.NewHTTPError(http.StatusBadRequest, "UnsMeta.invalid")
	}
	return core.ExecuteOpenAPI(ctx, service, request)
}
