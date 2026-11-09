// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package assetpublic

import (
	"net/http"
	"strconv"

	auditdomain "backend/internal/domain/audit"
	"backend/internal/handler/handlerx"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

// OpenapiAssetFilePublicDownloadHandler serves public-visibility files
// without authentication. It is the fallback path for deployments without a
// public bucket (local/single-bucket dev): when FILESTORE_S3_PUBLIC_BUCKET is
// configured, public file URLs are bucket direct links and this route is not
// part of the normal flow. The random storage key acts as an unguessable
// capability token; private files are never served here.
func OpenapiAssetFilePublicDownloadHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.OpenapiAssetFileDownloadReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		item, err := svcCtx.App.Asset.ResolvePublicFile(r.Context(), req.FilePath)
		if err != nil {
			respx.WriteError(w, logicx.Error(err))
			return
		}
		if svcCtx.App.Audit != nil {
			// 公开下载无认证中间件，operator 缺省为空，仍记录 View。
			svcCtx.App.Audit.Record(r.Context(), auditdomain.RecordInput{
				ScopeType:    auditdomain.ScopeTypePlatform,
				ResType:      auditdomain.ResTypeAsset,
				ResID:        strconv.FormatInt(item.ID, 10),
				ResName:      item.OriginalName,
				BusinessType: auditdomain.BizView,
				Detail: map[string]any{
					"filePath": req.FilePath,
					"fileName": item.OriginalName,
				},
			})
		}
		handlerx.StreamAsset(w, r, svcCtx, item, req.ResponseContentDisposition, false)
	}
}
