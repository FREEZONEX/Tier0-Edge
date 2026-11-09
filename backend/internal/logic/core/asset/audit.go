package asset

import (
	"context"
	"strconv"
	"strings"

	auditdomain "backend/internal/domain/audit"
	"backend/internal/svc"
)

// recordAssetAudit 记录资产域审计埋点。资产只记 fileId/fileName 等元数据，
// 不把文件本地路径塞进审计 detail；fileID 为 0（如解绑前未取到绑定行）时不记录。
func recordAssetAudit(ctx context.Context, svcCtx *svc.ServiceContext, fileID int64, fileName, businessType string, detail map[string]any) {
	if svcCtx == nil || svcCtx.App == nil || svcCtx.App.Audit == nil || fileID == 0 {
		return
	}
	if detail == nil {
		detail = map[string]any{}
	}
	detail["fileId"] = fileID
	svcCtx.App.Audit.Record(ctx, auditdomain.RecordInput{
		ScopeType:    auditdomain.ScopeTypePlatform,
		ResType:      auditdomain.ResTypeAsset,
		ResID:        strconv.FormatInt(fileID, 10),
		ResName:      fileName,
		BusinessType: businessType,
		Detail:       detail,
	})
}

// RecordUploadAudit 记录上传成功（BizCreate），AssetUpload logic 与上传 handler
// 的 multipart 分支复用；多文件合并为一条审计，detail.files 汇总各文件信息。
func RecordUploadAudit(ctx context.Context, svcCtx *svc.ServiceContext, files ...map[string]any) {
	if len(files) == 0 {
		return
	}
	first := files[0]
	detail := map[string]any{"size": auditInt64(first, "sizeBytes")}
	if len(files) > 1 {
		items := make([]map[string]any, 0, len(files))
		var total int64
		for _, item := range files {
			size := auditInt64(item, "sizeBytes")
			total += size
			items = append(items, map[string]any{
				"fileId":   auditInt64(item, "fileId"),
				"fileName": auditString(item, "fileName"),
				"size":     size,
			})
		}
		detail["files"] = items
		detail["size"] = total
	}
	recordAssetAudit(ctx, svcCtx, auditInt64(first, "fileId"), auditString(first, "fileName"), auditdomain.BizCreate, detail)
}

// RecordViewAudit 记录下载/查看（BizView），供流式下载 handler 显式埋点。
func RecordViewAudit(ctx context.Context, svcCtx *svc.ServiceContext, fileID int64, fileName string) {
	recordAssetAudit(ctx, svcCtx, fileID, fileName, auditdomain.BizView, nil)
}

func auditInt64(data map[string]any, key string) int64 {
	value, ok := data[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func auditString(data map[string]any, key string) string {
	value, ok := data[key]
	if !ok {
		return ""
	}
	typed, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(typed)
}
