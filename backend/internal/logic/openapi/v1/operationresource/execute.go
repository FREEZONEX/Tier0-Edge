package operationresource

import (
	"context"

	core "backend/internal/logic/core/sourcestream"
	"backend/internal/svc"
	"backend/internal/types"
)

// Reuse the existing domain/error path; API Key authentication and permission
// intersection remain in the real middleware, never in a gateway whitelist.
func execute(ctx context.Context, service *svc.ServiceContext, action string, request any) (*types.OperationResourceEnvelope, error) {
	response, err := core.ExecuteOpenAPI(ctx, service, action, request)
	if err != nil {
		return nil, err
	}
	return &types.OperationResourceEnvelope{Code: response.Code, Msg: response.Msg, Data: response.Data}, nil
}
