package sourcestream

import (
	respx "backend/internal/httpx"
	core "backend/internal/logic/core/sourcestream"
	"backend/internal/svc"
	"backend/internal/types"
	"context"
	"net/http"
)

func execute(ctx context.Context, service *svc.ServiceContext, action string, request any) (*types.SourceStreamEnvelope, error) {
	switch req := request.(type) {
	case *types.OpenapiSourceListReq:
		value := types.SourceListReq(*req)
		request = &value
	case *types.OpenapiStreamListReq:
		value := types.StreamListReq(*req)
		request = &value
	case *types.OpenapiStreamVersionListReq:
		value := types.StreamVersionListReq(*req)
		request = &value
	case *types.OpenapiStreamRunListReq:
		value := types.StreamRunListReq(*req)
		request = &value
	}
	switch action {
	case "OpenapiSourceProtocolList":
		return core.ExecuteOpenAPI(ctx, service, "SourceProtocolList", request)
	case "OpenapiSourceList":
		return core.ExecuteOpenAPI(ctx, service, "SourceList", request)
	case "OpenapiSourceDetail":
		return core.ExecuteOpenAPI(ctx, service, "SourceDetail", request)
	case "OpenapiSourceImpact":
		return core.ExecuteOpenAPI(ctx, service, "SourceImpact", request)
	case "OpenapiSourceCreate":
		return core.ExecuteOpenAPI(ctx, service, "SourceCreate", request)
	case "OpenapiSourceUpdate":
		return core.ExecuteOpenAPI(ctx, service, "SourceUpdate", request)
	case "OpenapiSourceDelete":
		return core.ExecuteOpenAPI(ctx, service, "SourceDelete", request)
	case "OpenapiSourceValidate":
		return core.ExecuteOpenAPI(ctx, service, "SourceValidate", request)
	case "OpenapiSourceProbe":
		return core.ExecuteOpenAPI(ctx, service, "SourceProbe", request)
	case "OpenapiSourceDiscover":
		return core.ExecuteOpenAPI(ctx, service, "SourceDiscover", request)
	case "OpenapiSourceCredentialSave":
		return core.ExecuteOpenAPI(ctx, service, "SourceCredentialSave", request)
	case "OpenapiSourceOAuthStart":
		return core.ExecuteOpenAPI(ctx, service, "SourceOAuthStart", request)
	case "OpenapiSourceOAuthComplete":
		return core.ExecuteOpenAPI(ctx, service, "SourceOAuthComplete", request)
	case "OpenapiStreamList":
		return core.ExecuteOpenAPI(ctx, service, "StreamList", request)
	case "OpenapiStreamDetail":
		return core.ExecuteOpenAPI(ctx, service, "StreamDetail", request)
	case "OpenapiStreamVersionList":
		return core.ExecuteOpenAPI(ctx, service, "StreamVersionList", request)
	case "OpenapiStreamRunList":
		return core.ExecuteOpenAPI(ctx, service, "StreamRunList", request)
	case "OpenapiStreamRunDetail":
		return core.ExecuteOpenAPI(ctx, service, "StreamRunDetail", request)
	case "OpenapiStreamCreate":
		return core.ExecuteOpenAPI(ctx, service, "StreamCreate", request)
	case "OpenapiStreamUpdate":
		return core.ExecuteOpenAPI(ctx, service, "StreamUpdate", request)
	case "OpenapiStreamDelete":
		return core.ExecuteOpenAPI(ctx, service, "StreamDelete", request)
	case "OpenapiStreamValidate":
		return core.ExecuteOpenAPI(ctx, service, "StreamValidate", request)
	case "OpenapiStreamTest":
		return core.ExecuteOpenAPI(ctx, service, "StreamTest", request)
	case "OpenapiStreamDeploy":
		return core.ExecuteOpenAPI(ctx, service, "StreamDeploy", request)
	case "OpenapiStreamDisable":
		return core.ExecuteOpenAPI(ctx, service, "StreamDisable", request)
	case "OpenapiStreamRollback":
		return core.ExecuteOpenAPI(ctx, service, "StreamRollback", request)
	case "OpenapiStreamInvoke":
		return core.ExecuteOpenAPI(ctx, service, "StreamInvoke", request)
	default:
		return nil, respx.NewHTTPError(http.StatusBadRequest, "SourceStream.invalidRequest")
	}
}
