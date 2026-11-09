package mqttauth

import (
	"context"

	auditdomain "backend/internal/domain/audit"
	"backend/internal/svc"
)

// recordMQTTAuthAudit 记录 OpenAPI MQTT Auth 操作的审计埋点；operator 由 ApiKeyAuth
// middleware 统一绑定（APIKeyName 作为 operator 名），这里只负责成功路径上的记录。
func recordMQTTAuthAudit(ctx context.Context, svcCtx *svc.ServiceContext, resID, resName, businessType string, detail map[string]any) {
	if svcCtx == nil || svcCtx.App == nil || svcCtx.App.Audit == nil {
		return
	}
	svcCtx.App.Audit.Record(ctx, auditdomain.RecordInput{
		ScopeType:    auditdomain.ScopeTypePlatform,
		ResType:      auditdomain.ResTypeMQTTAuth,
		ResID:        resID,
		ResName:      resName,
		BusinessType: businessType,
		Detail:       detail,
	})
}
