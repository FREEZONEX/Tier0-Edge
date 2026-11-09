package sourcestream

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"backend/internal/contextx"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"
)

// ExecuteOpenAPI reuses internal business behavior after route-level API Key authorization.
func ExecuteOpenAPI(ctx context.Context, service *svc.ServiceContext, action string, request any) (*types.SourceStreamEnvelope, error) {
	return execute(ctx, service, action, request)
}

func execute(ctx context.Context, service *svc.ServiceContext, action string, request any) (*types.SourceStreamEnvelope, error) {
	if service.App == nil || service.App.SourceStream == nil {
		return nil, apiError(domain.ErrUnavailable)
	}
	s := service.App.SourceStream
	var data any
	var err error
	switch req := request.(type) {
	case *types.OperationResourceListReq:
		data, err = s.OperationResources(ctx, req.Search, req.Status, req.Page, req.Size)
	case *types.OperationResourceDetailReq:
		data, err = s.OperationResource(ctx, req.ControlCode)
	case *types.OperationResourceInvokeReq:
		data, err = s.InvokeOperationResourceWithOptions(ctx, req.ControlCode, req.IdempotencyKey, req.Payload, domain.OperationInvokeOptions{CapabilityVersion: req.CapabilityVersion, ExpiresAt: req.ExpiresAt})
	case *types.OperationResourceInvocationReq:
		data, err = s.OperationInvocation(ctx, req.ControlCode, req.IdempotencyKey)
	case *types.SourceStreamEmpty:
		if action == "SourceHIDDevices" {
			var devices []domain.HIDDevice
			devices, err = s.HIDDevices(ctx)
			data = map[string]any{"devices": devices}
			break
		}
		if action != "SourceProtocolList" {
			err = repo.ErrInvalidArgument
			break
		}
		data = domain.Protocols()
	case *types.SourceHIDPreviewReq:
		data, err = s.PreviewHID(ctx, req.Endpoint, req.TimeoutMs)
	case *types.SourceValidateReq:
		err = s.ValidateSource(req.Document)
		data = map[string]any{"valid": err == nil, "sideEffects": false}
	case *types.SourceListReq:
		data, err = s.Sources(ctx, repo.SourceFilter{Search: req.Search, Protocol: req.Protocol, Health: req.Health, Page: req.Page, Size: req.Size})
	case *types.SourceIDReq:
		if action == "SourceImpact" {
			data, err = s.Impact(ctx, req.SourceId)
		} else {
			data, err = s.Source(ctx, req.SourceId)
		}
	case *types.SourceSaveReq:
		if action == "SourceCreate" {
			data, err = s.CreateSource(ctx, req.Document, req.ExpectedRevision)
		} else {
			data, err = s.UpdateSource(ctx, req.SourceId, req.ExpectedRevision, req.Document, req.ConfirmedImpactRevision)
		}
	case *types.SourceMutationReq:
		data, err = s.DeleteSource(ctx, req.SourceId, req.ExpectedRevision, req.ConfirmedImpactRevision)
	case *types.SourceProbeReq:
		data, err = s.Probe(ctx, req.SourceId, req.Document, req.TimeoutMs)
	case *types.SourceDiscoverReq:
		if req.Cursor != "" || req.Limit != 0 {
			err = repo.ErrInvalidArgument
			break
		}
		data, err = s.Discover(ctx, req.SourceId, req.ExpectedRevision, req.ParentResourceRef)
	case *types.SourceOAuthStartReq:
		data, err = s.OAuthStart(ctx, req.SourceId, req.RedirectUri)
	case *types.SourceOAuthCompleteReq:
		data, err = s.OAuthComplete(ctx, req.SourceId, req.State, req.Code)
	case *types.SourceCredentialSaveReq:
		credential := domain.Credential{Kind: req.Kind, Username: req.Username, Secret: req.Secret, Values: req.Values, CertificateFileID: req.CertificateFileId, PrivateKeyFileID: req.PrivateKeyFileId, CAFileID: req.CaFileId}
		if req.Kind == "oauth2" {
			credential.OAuth = &domain.OAuthCredential{}
		}
		data, err = s.SaveCredential(ctx, req.SourceId, req.ExpectedRevision, credential, req.ConfirmedImpactRevision)
	case *types.StreamListReq:
		data, err = s.Streams(ctx, repo.StreamFilter{SourceID: req.SourceId, TopicID: req.TopicId, Search: req.Search, Model: req.Model, Status: req.Status, Page: req.Page, Size: req.Size})
	case *types.StreamIDReq:
		data, err = s.Stream(ctx, req.StreamId)
	case *types.StreamSaveReq:
		id := req.StreamId
		if action == "StreamCreate" {
			id = 0
		}
		data, err = s.SaveStream(ctx, id, req.ExpectedRevision, req.Document)
	case *types.StreamMutationReq:
		data, err = s.Disable(ctx, req.StreamId, req.ExpectedRevision, action == "StreamDelete")
	case *types.StreamValidateReq:
		data, err = s.ValidationView(ctx, req.Document, req.ExpectedSourceRevision)
	case *types.StreamTestReq:
		data, err = s.TestEntry(ctx, req.Document, req.Input, req.ExpectedSourceRevision, req.Phase, req.EntryId)
	case *types.StreamDeployReq:
		data, err = s.Deploy(ctx, req.StreamId, req.ExpectedRevision, req.ExpectedSourceRevision, 0)
	case *types.StreamRollbackReq:
		data, err = s.Deploy(ctx, req.StreamId, req.ExpectedRevision, req.ExpectedSourceRevision, req.VersionId)
	case *types.StreamVersionListReq:
		data, err = s.Versions(ctx, req.StreamId, req.Page, req.Size)
	case *types.StreamInvokeReq:
		data, err = s.InvokeEntry(ctx, req.StreamId, req.ExpectedActiveVersion, req.EntryId, req.IdempotencyKey, req.Input, req.TimeoutMs)
	case *types.StreamRunListReq:
		data, err = s.Runs(ctx, repo.StreamRunFilter{SourceID: req.SourceId, StreamID: req.StreamId, TopicID: req.TopicId, Model: req.Model, Status: req.Status, Page: req.Page, Size: req.Size})
	case *types.StreamRunIDReq:
		data, err = s.Run(ctx, req.RunId)
	case *types.StreamTopicRelationsReq:
		subject, ok := contextx.SubjectFrom(ctx)
		if !ok || !contextx.HasResource(subject, "uns.read") {
			err = domain.ErrForbidden
			break
		}
		data, err = s.Streams(ctx, repo.StreamFilter{TopicID: req.TopicId})
	default:
		err = repo.ErrInvalidArgument
	}
	if err != nil {
		switch request.(type) {
		case *types.OperationResourceListReq, *types.OperationResourceDetailReq, *types.OperationResourceInvokeReq, *types.OperationResourceInvocationReq:
			return nil, controlAPIError(err)
		}
		return nil, apiError(err)
	}
	return &types.SourceStreamEnvelope{Code: 200, Msg: "success", Data: data}, nil
}

func controlAPIError(err error) error {
	switch {
	case errors.Is(err, domain.ErrExecutionFailed):
		return respx.NewHTTPError(http.StatusUnprocessableEntity, "CONTROL_EXECUTION_FAILED")
	case errors.Is(err, repo.ErrControlGrant):
		return respx.NewHTTPError(http.StatusForbidden, "CONTROL_GRANT_DENIED")
	case errors.Is(err, repo.ErrControlRate):
		return respx.NewHTTPError(http.StatusTooManyRequests, "CONTROL_RATE_LIMITED")
	case errors.Is(err, repo.ErrControlCapability):
		return respx.NewHTTPError(http.StatusConflict, "CONTROL_CAPABILITY_CHANGED")
	case errors.Is(err, repo.ErrControlExpired):
		return respx.NewHTTPError(http.StatusGone, "CONTROL_EXPIRED")
	case errors.Is(err, domain.ErrRemoteControlDisabled):
		return respx.NewHTTPError(http.StatusForbidden, "EDGE_CONTROL_DISABLED")
	case errors.Is(err, domain.ErrForbidden):
		return respx.NewHTTPError(http.StatusForbidden, "CONTROL_FORBIDDEN")
	case errors.Is(err, repo.ErrNotFound):
		return respx.NewHTTPError(http.StatusNotFound, "CONTROL_NOT_FOUND")
	case errors.Is(err, domain.ErrControlInactive):
		return respx.NewHTTPError(http.StatusConflict, "CONTROL_INACTIVE")
	case errors.Is(err, domain.ErrIdempotencyConflict):
		return respx.NewHTTPError(http.StatusConflict, "IDEMPOTENCY_CONFLICT")
	case errors.Is(err, domain.ErrControlInvalidInput), errors.Is(err, repo.ErrInvalidArgument):
		return respx.NewHTTPError(http.StatusBadRequest, "CONTROL_INVALID_INPUT")
	case errors.Is(err, domain.ErrControlOutputInvalid):
		return respx.NewHTTPError(http.StatusUnprocessableEntity, "CONTROL_OUTPUT_INVALID")
	case errors.Is(err, domain.ErrUnknownResult):
		return respx.NewHTTPError(http.StatusGatewayTimeout, "CONTROL_RESULT_UNKNOWN")
	case errors.Is(err, domain.ErrUnavailable), errors.Is(err, domain.ErrCredentialKey):
		return respx.NewHTTPError(http.StatusServiceUnavailable, "CONTROL_UNAVAILABLE")
	case strings.HasPrefix(err.Error(), "SourceStream."):
		return respx.NewHTTPError(http.StatusBadGateway, "SOURCE_PROTOCOL_ERROR")
	default:
		return logicx.Error(err)
	}
}

func apiError(err error) error {
	var invalid *domain.ValidationError
	if errors.As(err, &invalid) {
		return respx.WrapHTTPError(http.StatusBadRequest, "SourceStream.invalidConfiguration", err)
	}
	switch {
	case errors.Is(err, domain.ErrControlInvalidInput):
		return respx.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrControlOutputInvalid):
		return respx.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrControlInactive), errors.Is(err, domain.ErrIdempotencyConflict):
		return respx.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, unsmeta.ErrInvalid), errors.Is(err, unsmeta.ErrData), errors.Is(err, unsmeta.ErrSchema):
		return respx.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrRemoteControlDisabled):
		return respx.NewHTTPError(http.StatusForbidden, "EDGE_CONTROL_DISABLED")
	case errors.Is(err, domain.ErrForbidden):
		return respx.NewHTTPError(http.StatusForbidden, err.Error())
	case errors.Is(err, domain.ErrReferenced):
		return respx.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrUnavailable), errors.Is(err, domain.ErrCredentialKey):
		return respx.NewHTTPError(http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, domain.ErrUnknownResult):
		return respx.NewHTTPError(http.StatusGatewayTimeout, err.Error())
	case errors.Is(err, domain.ErrExecutionFailed):
		return respx.NewHTTPError(http.StatusUnprocessableEntity, domain.ErrExecutionFailed.Error())
	case errors.Is(err, domain.ErrSubscription), errors.Is(err, domain.ErrCredential), errors.Is(err, domain.ErrOAuthState), errors.Is(err, domain.ErrOAuthRequired):
		return respx.NewHTTPError(http.StatusBadRequest, err.Error())
	case strings.HasPrefix(err.Error(), "SourceStream.script"):
		return respx.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	case strings.HasPrefix(err.Error(), "SourceStream."):
		return respx.NewHTTPError(http.StatusBadGateway, err.Error())
	default:
		return logicx.Error(err)
	}
}
