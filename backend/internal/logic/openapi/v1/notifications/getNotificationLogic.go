// Package notifications 消息通知 OpenAPI logic（TASK-030）。
// 手写增量：本机 goctl fork 无法全量生成（存量 .api 缺 summary），生成器恢复后可迁移。
// 错误响应走 openapiresp（{errorCode,message} + 真实 HTTP 状态码，不走 Envelope）。
package notifications

import (
	"context"

	"backend/internal/contextx"
	domainnotify "backend/internal/domain/notify"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetNotificationLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetNotificationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetNotificationLogic {
	return &GetNotificationLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// GetNotification 按 messageId 查询发送状态；归属锚定发起请求的 API Key。
func (l *GetNotificationLogic) GetNotification(req *types.GetNotificationReq) (*types.GetNotificationResp, error) {
	subject, ok := contextx.SubjectFrom(l.ctx)
	if !ok {
		return nil, domainnotify.ErrInvalidCredential
	}
	result, err := l.svcCtx.App.Notify.GetNotification(l.ctx, subject.APIKeyID, req.MessageId)
	if err != nil {
		return nil, err
	}
	return &types.GetNotificationResp{
		MessageId:   result.MessageID,
		Type:        result.Type,
		Status:      result.Status,
		CreatedAt:   result.CreatedAt,
		CompletedAt: result.CompletedAt,
		ErrorCode:   result.ErrorCode,
		Message:     result.Message,
	}, nil
}
