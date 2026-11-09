// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"net/http"

	"backend/internal/handler/handlerx"
	respx "backend/internal/httpx"
	"backend/internal/logic/openapi/v1/asset"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func OpenapiAssetUploadHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if handlerx.IsMultipart(r) {
			data, err := handlerx.UploadMultipartAssets(r, svcCtx, handlerx.AssetMultipartOptions{})
			if err != nil {
				respx.WriteError(w, err)
				return
			}
			gozerohttpx.OkJson(w, respx.Envelope(data))
			return
		}

		var req types.AssetUploadReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := asset.NewOpenapiAssetUploadLogic(r.Context(), svcCtx)
		resp, err := l.OpenapiAssetUpload(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
