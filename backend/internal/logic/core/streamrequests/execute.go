package streamrequests

import (
	"context"
	"errors"
	"strings"

	domain "backend/internal/domain/sourcestream"
	respx "backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/svc"
	"backend/internal/types"
)

// Execute is shared by session and API Key routes after their real middleware.
// Cloud v2 returns the public value directly, without an Enterprise envelope.
func Execute(ctx context.Context, service *svc.ServiceContext, request any) (any, error) {
	if service.App == nil || service.App.SourceStream == nil {
		return nil, requestError(domain.ErrUnavailable)
	}
	s := service.App.SourceStream
	var value any
	var err error
	switch req := request.(type) {
	case *types.StreamRequestDiscoverReq:
		value, err = s.DiscoverStreamRequests(ctx, req.Search, req.Page, req.Size)
	case *types.StreamRequestDescribeReq:
		value, err = s.DescribeStreamRequest(ctx, req.RequestCode)
	case *types.StreamRequestInvokeReq:
		value, err = s.InvokeStreamRequest(ctx, req.RequestCode, req.IdempotencyKey, req.Input)
	default:
		err = repo.ErrInvalidArgument
	}
	if err != nil {
		return nil, requestError(err)
	}
	return value, nil
}

func requestError(err error) error {
	code := "STREAM_REQUEST_INTERNAL_ERROR"
	switch {
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, repo.ErrControlGrant):
		code = "STREAM_REQUEST_FORBIDDEN"
	case errors.Is(err, repo.ErrNotFound):
		code = "STREAM_REQUEST_NOT_FOUND"
	case errors.Is(err, domain.ErrControlInactive):
		code = "STREAM_REQUEST_INACTIVE"
	case errors.Is(err, domain.ErrIdempotencyConflict):
		code = "IDEMPOTENCY_CONFLICT"
	case errors.Is(err, domain.ErrControlInvalidInput), errors.Is(err, repo.ErrInvalidArgument):
		code = "STREAM_REQUEST_INVALID_INPUT"
	case errors.Is(err, domain.ErrControlOutputInvalid):
		code = "STREAM_REQUEST_OUTPUT_INVALID"
	case errors.Is(err, repo.ErrControlRate):
		code = "STREAM_REQUEST_RATE_LIMITED"
	case errors.Is(err, domain.ErrUnknownResult), errors.Is(err, streamexec.ErrTimeout), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = "STREAM_REQUEST_RESULT_UNKNOWN"
	case errors.Is(err, domain.ErrUnavailable), errors.Is(err, domain.ErrCredentialKey):
		code = "STREAM_REQUEST_UNAVAILABLE"
	case errors.Is(err, domain.ErrExecutionFailed), strings.HasPrefix(err.Error(), "SourceStream."):
		code = "SOURCE_PROTOCOL_ERROR"
	}
	return respx.WrapHTTPError(0, code, err)
}
