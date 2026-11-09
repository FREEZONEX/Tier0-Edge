package fleetprotocol

import (
	"context"
	"errors"
	"regexp"
)

var ErrRuntimeRequest = errors.New("RUNTIME_INVALID_REQUEST")
var runtimeKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)

func ExecuteRuntime(ctx context.Context, provider RuntimeProvider, req RuntimeRequest) (any, error) {
	if provider == nil {
		return nil, errors.New("RUNTIME_UNAVAILABLE")
	}
	switch req.Action {
	case "list":
		return provider.List(ctx)
	case "logs":
		if !runtimeKey.MatchString(req.ResourceID) || req.Tail < 0 || req.Tail > 2000 {
			return nil, ErrRuntimeRequest
		}
		return provider.Logs(ctx, req)
	case "control":
		if !runtimeKey.MatchString(req.ResourceID) || !runtimeKey.MatchString(req.OperationID) || (req.Verb != "start" && req.Verb != "stop" && req.Verb != "restart") {
			return nil, ErrRuntimeRequest
		}
		return provider.Control(ctx, req)
	case "operation":
		if !runtimeKey.MatchString(req.OperationID) {
			return nil, ErrRuntimeRequest
		}
		return provider.Operation(ctx, req.OperationID)
	default:
		return nil, ErrRuntimeRequest
	}
}
