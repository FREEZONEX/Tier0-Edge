// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/asset"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"io"
	"mime"
	"net/http"
	"strconv"
)

func AssetDownloadHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FileIdReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		item, reader, meta, err := svcCtx.App.Asset.Open(r.Context(), req.FileId)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		asset.RecordViewAudit(r.Context(), svcCtx, item.ID, item.OriginalName)
		defer reader.Close()
		contentType := meta.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		if meta.SizeBytes > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(meta.SizeBytes, 10))
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.OriginalName}))
		if _, err := io.Copy(w, reader); err != nil {
			respx.WriteError(w, err)
			return
		}
	}
}
