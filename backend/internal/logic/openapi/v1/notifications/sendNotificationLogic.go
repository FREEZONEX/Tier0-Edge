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

type SendNotificationLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSendNotificationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendNotificationLogic {
	return &SendNotificationLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// SendNotification 六步校验链：校验归一化（①）→ 限流（②）→ 幂等预查（③）→ 收件人校验（④）
// → 幂等写事务+outbox（⑤）→ 返回 accepted（⑥）；顺序不可变，对齐云端。
func (l *SendNotificationLogic) SendNotification(req *types.SendNotificationReq) (*types.SendNotificationResp, error) {
	subject, ok := contextx.SubjectFrom(l.ctx)
	if !ok {
		return nil, domainnotify.ErrInvalidCredential
	}
	var senderType, senderID, senderName string
	var senderMeta map[string]string
	if req.Sender != nil {
		senderType, senderID, senderName, senderMeta = req.Sender.Type, req.Sender.Id, req.Sender.Name, req.Sender.Meta
	}
	n, err := domainnotify.ValidateAndNormalize(
		req.RecipientUserId, req.Type, req.Title, req.Content, req.IdempotencyKey,
		req.Source, req.Mode, req.Link, req.Channels,
		senderType, senderID, senderName, senderMeta,
	)
	if err != nil {
		return nil, err
	}
	result, err := l.svcCtx.App.Notify.SendNotification(l.ctx, subject, n)
	if err != nil {
		return nil, err
	}
	return &types.SendNotificationResp{
		MessageId: result.MessageID,
		Status:    result.Status,
		CreatedAt: result.CreatedAt,
	}, nil
}
