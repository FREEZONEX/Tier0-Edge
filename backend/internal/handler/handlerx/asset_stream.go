package handlerx

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	domainasset "backend/internal/domain/asset"
	respx "backend/internal/httpx"
	"backend/internal/infra/filestore"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
)

// StreamAsset writes the file content to the response with Content-Type and
// Content-Disposition headers. An explicit contentDisposition wins; otherwise
// defaultAttachment decides between attachment download and no disposition
// header (public files default to none so browsers can render them inline).
// Active content types (HTML/SVG/JS/...) are always forced to attachment so
// an inline-served upload cannot execute script in the deployment's origin.
func StreamAsset(w http.ResponseWriter, r *http.Request, svcCtx *svc.ServiceContext, item repo.AssetFile, contentDisposition string, defaultAttachment bool) {
	_, reader, meta, err := svcCtx.App.Asset.Open(r.Context(), item.ID)
	if err != nil {
		respx.WriteError(w, logicx.Error(err))
		return
	}
	defer reader.Close()
	writeAssetStream(w, item, reader, meta, contentDisposition, defaultAttachment)
}

// StreamAssetByID 按 asset id 取文件再流式输出。调用方必须已经完成鉴权与归属校验
// （例如 Launchpad 的按 App 取图端点），本函数只负责读取与响应头。
func StreamAssetByID(w http.ResponseWriter, r *http.Request, svcCtx *svc.ServiceContext, assetID int64, defaultAttachment bool) {
	item, reader, meta, err := svcCtx.App.Asset.Open(r.Context(), assetID)
	if err != nil {
		respx.WriteError(w, logicx.Error(err))
		return
	}
	defer reader.Close()
	writeAssetStream(w, item, reader, meta, "", defaultAttachment)
}

func writeAssetStream(w http.ResponseWriter, item repo.AssetFile, reader io.Reader, meta filestore.FileMeta, contentDisposition string, defaultAttachment bool) {
	contentType := meta.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if meta.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(meta.SizeBytes, 10))
	}
	disposition := strings.TrimSpace(contentDisposition)
	if disposition == "" && (defaultAttachment || domainasset.IsActiveContentType(contentType)) {
		disposition = mime.FormatMediaType("attachment", map[string]string{"filename": item.OriginalName})
	}
	if disposition != "" {
		w.Header().Set("Content-Disposition", disposition)
	}
	_, _ = io.Copy(w, reader)
}
