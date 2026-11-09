package unsmeta

import (
	"context"
	"errors"
	"net/http"

	"backend/internal/contextx"
	domain "backend/internal/domain/unsmeta"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"
)

func ExecuteOpenAPI(ctx context.Context, service *svc.ServiceContext, request any) (*types.UnsMetaEnvelope, error) {
	return execute(ctx, service, request)
}

func execute(ctx context.Context, service *svc.ServiceContext, request any) (*types.UnsMetaEnvelope, error) {
	s := service.App.UnsMeta
	var data any
	var err error
	switch req := request.(type) {
	case *types.UnsTopicValidateReq:
		err = s.ValidateTopic(ctx, req.Id, req.ExpectedRevision, req.Document)
		data = map[string]any{"valid": err == nil, "sideEffects": false}
	case *types.UnsMetaListReq:
		data, err = s.List(ctx, req.Kind, req.Deleted, req.Page, req.Size)
	case *types.UnsMetaCopyReq:
		subject, ok := contextx.SubjectFrom(ctx)
		if !ok || !contextx.HasResource(subject, "uns.read") {
			return nil, respx.NewHTTPError(http.StatusForbidden, "UnsMeta.forbidden")
		}
		data, err = s.Copy(ctx, req.Id, req.ExpectedRevision, req.ParentId, req.Name, req.Description)
	case *types.UnsMetaSaveReq:
		data, err = s.Save(ctx, req.Id, req.Kind, req.ExpectedRevision, req.Document)
	case *types.UnsCollectionCreateReq:
		data, err = s.CreateCollection(ctx, req.ParentId, req.Document)
	case *types.UnsMetaStateReq:
		subject, ok := contextx.SubjectFrom(ctx)
		if !ok || (!req.Restore && !contextx.HasResource(subject, "uns.delete")) {
			return nil, respx.NewHTTPError(http.StatusForbidden, "UnsMeta.forbidden")
		}
		data, err = s.State(ctx, req.Id, req.ExpectedRevision, req.Restore)
	case *types.UnsMetaViewReq:
		data, err = s.View(ctx, req.Id)
	case *types.UnsRecordListReq:
		data, err = s.Records(ctx, req.CollectionId, req.Search, req.Page, req.Size)
	case *types.UnsRecordSaveReq:
		var row repo.UnsRecord
		row, err = s.SaveRecord(ctx, req.CollectionId, req.Id, req.ExpectedRevision, req.Data)
		data = row
		if err == nil && row.DeliveryState != "" {
			data = map[string]any{"status": "accepted", "deliveryState": row.DeliveryState, "acceptedId": row.ID, "centerCommitted": false}
		}
	case *types.UnsRecordDeleteReq:
		err = s.DeleteRecord(ctx, req.CollectionId, req.RecordId, req.ExpectedRevision)
		data = map[string]any{"deleted": err == nil}
	case *types.UnsSampleListReq:
		data, err = s.Samples(ctx, req.CollectionId, req.RecordId, req.StartTime, req.EndTime, req.Page, req.Size)
	case *types.UnsSampleAppendReq:
		data, err = s.AppendSamples(ctx, req.CollectionId, req.Samples)
	default:
		err = domain.ErrInvalid
	}
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrStorageOperation):
			return nil, respx.NewHTTPError(http.StatusBadRequest, err.Error())
		case errors.Is(err, repo.ErrStorageDefinitionPending):
			return nil, respx.NewHTTPError(http.StatusConflict, err.Error())
		case errors.Is(err, repo.ErrStorageUnavailable):
			return nil, respx.NewHTTPError(http.StatusServiceUnavailable, err.Error())
		case errors.Is(err, repo.ErrMountedReadOnly):
			return nil, respx.NewHTTPError(http.StatusForbidden, err.Error())
		case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrSchema), errors.Is(err, domain.ErrData):
			return nil, respx.NewHTTPError(http.StatusBadRequest, err.Error())
		case errors.Is(err, domain.ErrReferenced), errors.Is(err, domain.ErrCycle), errors.Is(err, domain.ErrSchemaInUse):
			return nil, respx.NewHTTPError(http.StatusConflict, err.Error())
		default:
			return nil, logicx.Error(err)
		}
	}
	return &types.UnsMetaEnvelope{Code: 200, Msg: "success", Data: data}, nil
}
