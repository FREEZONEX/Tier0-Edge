// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	domainasset "backend/internal/domain/asset"
	respx "backend/internal/httpx"
	"backend/internal/logic/core/asset"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"errors"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

// maxAssetUploadBytes 单文件与整个 multipart 请求体的 500MiB 硬上限，
// 与前端 ImportAppModal/ReplaceAppModal 的 MAX_IMPORT_FILE_SIZE 对齐；
// 额外 1MiB 余量用于容纳 multipart 分隔符/表单字段等开销。
const maxAssetUploadBytes = 500 << 20

func AssetUploadHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
			r.Body = http.MaxBytesReader(w, r.Body, maxAssetUploadBytes+(1<<20))
			if err := r.ParseMultipartForm(64 << 20); err != nil {
				var maxBytesErr *http.MaxBytesError
				if errors.As(err, &maxBytesErr) {
					respx.WriteError(w, respx.NewHTTPError(http.StatusRequestEntityTooLarge, "asset.fileTooLarge"))
					return
				}
				respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid multipart request: "+err.Error()))
				return
			}
			defer r.MultipartForm.RemoveAll()
			files := r.MultipartForm.File["files"]
			if len(files) == 0 {
				files = r.MultipartForm.File["file"]
			}
			if len(files) == 0 {
				respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "file is required"))
				return
			}
			ownerType := strings.TrimSpace(r.FormValue("ownerType"))
			ownerID := domainasset.ParseOwnerID(r.FormValue("ownerId"))
			list := make([]map[string]any, 0, len(files))
			for _, header := range files {
				if header.Size > maxAssetUploadBytes {
					respx.WriteError(w, respx.NewHTTPError(http.StatusRequestEntityTooLarge, "asset.fileTooLarge"))
					return
				}
				file, err := header.Open()
				if err != nil {
					respx.WriteError(w, err)
					return
				}
				contentType := header.Header.Get("Content-Type")
				if contentType == "" {
					contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(header.Filename)))
				}
				if contentType == "" {
					contentType = "application/octet-stream"
				}
				data, err := svcCtx.App.Asset.UploadContent(r.Context(), domainasset.UploadContentCommand{
					FileName:    header.Filename,
					ContentType: contentType,
					Size:        header.Size,
					Reader:      file,
					UserID:      logicx.UserID(r.Context()),
				})
				_ = file.Close()
				if err != nil {
					respx.WriteError(w, err)
					return
				}
				if ownerType != "" && ownerID != 0 {
					bound, err := svcCtx.App.Asset.Bind(r.Context(), domainasset.BindCommand{
						FileID:    domainasset.ParseOwnerID(toString(data["fileId"])),
						OwnerType: ownerType,
						OwnerID:   ownerID,
						UserID:    logicx.UserID(r.Context()),
					})
					if err != nil {
						respx.WriteError(w, err)
						return
					}
					if binding, ok := bound["binding"]; ok {
						data["binding"] = binding
					}
				}
				list = append(list, data)
			}
			asset.RecordUploadAudit(r.Context(), svcCtx, list...)
			gozerohttpx.OkJson(w, respx.Envelope(map[string]any{"list": list, "total": len(list)}))
			return
		}

		var req types.AssetUploadReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := asset.NewAssetUploadLogic(r.Context(), svcCtx)
		resp, err := l.AssetUpload(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}

func toString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.FormatInt(int64(v), 10)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	default:
		return ""
	}
}
