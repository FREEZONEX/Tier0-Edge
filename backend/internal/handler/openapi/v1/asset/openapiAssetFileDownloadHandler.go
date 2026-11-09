// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	domainasset "backend/internal/domain/asset"
	auditdomain "backend/internal/domain/audit"
	"backend/internal/handler/handlerx"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

// OpenapiAssetFileDownloadHandler serves file content for SDK downloadFile:
// public files are proxied as a stream; private files redirect (302) to a
// short-lived presigned URL, falling back to a proxied stream on drivers
// without presign support (local, development only).
func OpenapiAssetFileDownloadHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.OpenapiAssetFileDownloadReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		assetSvc := svcCtx.App.Asset
		item, err := assetSvc.ResolveOpenapiFile(r.Context(), req.FilePath, logicx.UserID(r.Context()))
		if err != nil {
			respx.WriteError(w, logicx.Error(err))
			return
		}
		if svcCtx.App.Audit != nil {
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
		disposition := req.ResponseContentDisposition
		if item.Visibility == domainasset.VisibilityPublic {
			handlerx.StreamAsset(w, r, svcCtx, item, disposition, true)
			return
		}
		signed, _, err := assetSvc.SignGetURL(r.Context(), item, 5*time.Minute, disposition)
		if err != nil {
			if errors.Is(err, domainasset.ErrPresignUnsupported) {
				handlerx.StreamAsset(w, r, svcCtx, item, disposition, true)
				return
			}
			respx.WriteError(w, logicx.Error(err))
			return
		}
		http.Redirect(w, r, signed, http.StatusFound)
	}
}
