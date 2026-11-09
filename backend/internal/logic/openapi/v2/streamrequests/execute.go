package streamrequests

import (
	core "backend/internal/logic/core/streamrequests"
	"backend/internal/svc"
	"context"
)

func Execute(ctx context.Context, service *svc.ServiceContext, request any) (any, error) {
	return core.Execute(ctx, service, request)
}
