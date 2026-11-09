// Package notify 消息通知中心管理端/用户端 logic（TASK-030 P0）。
// 手写增量（goctl 生成恢复后可迁移）；薄壳转换，业务在 domain/notify。
package notify

import (
	"context"
	"strconv"

	"backend/internal/domain/notify"
	"backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type Logic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewLogic 构造通知 logic（供 handler 调用）。
func NewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *Logic {
	return &Logic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// NotificationSave 保存通知草稿。
func (l *Logic) NotificationSave(req *types.NotificationSaveReq) (*types.Envelope, error) {
	id, err := l.svcCtx.App.Notify.SaveNotification(l.ctx, notify.SaveNotificationInput{
		ID: req.Id, Type: req.Type, Title: req.Title, Content: req.Content,
		Link: req.Link, AsModal: req.AsModal == 1, Operator: logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"id": id}), nil
}

// NotificationSend 发送通知（立即/定时）。
func (l *Logic) NotificationSend(req *types.NotificationSendReq) (*types.Envelope, error) {
	if err := l.svcCtx.App.Notify.SendManualNotification(l.ctx, notify.SendManualInput{
		ID: req.Id, AudienceType: req.AudienceType, UserIDs: req.UserIds,
		ScheduleTime: req.ScheduleTime, Operator: logicx.UserID(l.ctx),
	}); err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"sent": true}), nil
}

// NotificationRevoke 撤回通知。
func (l *Logic) NotificationRevoke(req *types.NotificationRevokeReq) (*types.Envelope, error) {
	if err := l.svcCtx.App.Notify.RevokeNotification(l.ctx, req.Id); err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"revoked": true}), nil
}

// NotificationDelete 删除通知。
func (l *Logic) NotificationDelete(req *types.NotificationDetailReq) (*types.Envelope, error) {
	if err := l.svcCtx.App.Notify.DeleteNotification(l.ctx, req.Id); err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"deleted": true}), nil
}

// NotificationCopy 复制为新草稿。
func (l *Logic) NotificationCopy(req *types.NotificationCopyReq) (*types.Envelope, error) {
	id, err := l.svcCtx.App.Notify.CopyNotification(l.ctx, req.Id, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"id": id}), nil
}

// NotificationDetail 通知详情。
func (l *Logic) NotificationDetail(req *types.NotificationDetailReq) (*types.Envelope, error) {
	row, err := l.svcCtx.App.Notify.GetNotificationDetail(l.ctx, req.Id)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(row), nil
}

// NotificationList 通知分页列表。
func (l *Logic) NotificationList(req *types.NotificationListReq) (*types.Envelope, error) {
	rows, total, err := l.svcCtx.App.Notify.ListNotifications(l.ctx, repo.NotificationListFilter{
		Type: req.Type, Status: req.Status, PageNo: int(req.Page), PageSize: int(req.PageSize),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"list": rows, "total": total}), nil
}

// MessageHistoryList 统一查询平台、系统和 App SDK/OpenAPI 投递历史。
func (l *Logic) MessageHistoryList(req *types.MessageHistoryListReq) (*types.Envelope, error) {
	rows, total, sources, err := l.svcCtx.App.Notify.ListMessageHistory(l.ctx, repo.MessageHistoryFilter{
		Page: int(req.Page), Size: int(req.Size), Keyword: req.Keyword,
		SourceType: req.SourceType, SourceID: req.SourceId, Type: req.Type, Status: req.Status,
		RecipientUserID: req.RecipientUserId, StartTime: req.StartTime, EndTime: req.EndTime,
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"list": rows, "total": total, "sources": sources}), nil
}

// NotificationRecipientOptions 返回通知管理页可选的用户和用户组。
func (l *Logic) NotificationRecipientOptions() (*types.Envelope, error) {
	users, groups, err := l.svcCtx.App.Notify.NotificationRecipientOptions(l.ctx)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"users": users, "groups": groups}), nil
}

// NotificationEstimateUsers 受众人数预估。
func (l *Logic) NotificationEstimateUsers(req *types.NotificationEstimateUsersReq) (*types.Envelope, error) {
	count, err := l.svcCtx.App.Notify.EstimateUsers(l.ctx, req.AudienceType, req.UserIds)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"count": count}), nil
}

// UserMessageList 用户消息列表（支持筛选）。
func (l *Logic) UserMessageList(req *types.UserMessageListReq) (*types.Envelope, error) {
	userID := logicx.UserID(l.ctx)
	q := notify.UserMessageQuery{
		Group: req.Group, UnreadOnly: req.UnreadOnly == 1, Keyword: req.Keyword,
		StartTime: req.StartTime, EndTime: req.EndTime, SourceType: req.SourceType,
	}
	items, total, err := l.svcCtx.App.Notify.ListUserMessages(l.ctx, userID, q, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, logicx.Error(err)
	}
	list := make([]map[string]any, 0, len(items))
	for _, item := range items {
		list = append(list, map[string]any{
			"id":         item.Link.ID,
			"messageId":  item.Message.ID,
			"type":       item.Message.Type,
			"group":      item.Message.GroupCode,
			"title":      item.Message.Title,
			"content":    item.Message.Content,
			"link":       item.Message.Link,
			"isRead":     item.Link.IsRead,
			"isHandled":  item.Link.IsHandled,
			"senderType": item.Message.SenderType,
			"senderId":   item.Message.SenderID,
			"senderName": item.Message.SenderName,
			// 发信应用的平台身份与图标：sender.id 是 SDK 自报的 docker app UUID，
			// 客户端拿它反查不到应用,这两个字段才是可用的
			"senderAppId":     idOrEmpty(item.Message.SenderAppID),
			"senderProjectId": idOrEmpty(item.Message.SenderProjectID),
			"senderIconUrl":   notify.SenderIconURL(item.Link.ID, item.Message.SenderAppID),
			"createdTime":     item.Message.CreatedTime,
		})
	}
	return httpx.Envelope(map[string]any{"list": list, "total": total}), nil
}

// UserMessageRead 消息已读（ids 空=全部已读）。
func (l *Logic) UserMessageRead(req *types.UserMessageReadReq) (*types.Envelope, error) {
	if err := l.svcCtx.App.Notify.ReadMessages(l.ctx, logicx.UserID(l.ctx), req.Ids); err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"read": true}), nil
}

// UserMessageReadAll 全部已读（可携带筛选，返回影响条数）。
func (l *Logic) UserMessageReadAll(req *types.UserMessageReadAllReq) (*types.Envelope, error) {
	affected, err := l.svcCtx.App.Notify.ReadAllByFilter(l.ctx, logicx.UserID(l.ctx), notify.UserMessageQuery{
		Group: req.Group, UnreadOnly: req.UnreadOnly, Keyword: req.Keyword,
		StartTime: req.StartTime, EndTime: req.EndTime, SourceType: req.SourceType,
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"affected": affected}), nil
}

// UserMessageDetail 用户消息详情。
func (l *Logic) UserMessageDetail(req *types.UserMessageDetailReq) (*types.Envelope, error) {
	item, err := l.svcCtx.App.Notify.GetUserMessageDetail(l.ctx, logicx.UserID(l.ctx), req.Id)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{
		"id": item.Link.ID, "messageId": item.Message.ID, "type": item.Message.Type,
		"group": item.Message.GroupCode, "title": item.Message.Title, "content": item.Message.Content,
		"link": item.Message.Link, "isRead": item.Link.IsRead, "isHandled": item.Link.IsHandled,
		"sourceType": item.Message.SenderType, "sourceName": item.Message.SenderName,
		"senderType": item.Message.SenderType, "senderName": item.Message.SenderName,
		"senderId": item.Message.SenderID, "createdTime": item.Message.CreatedTime,
		"senderAppId":     idOrEmpty(item.Message.SenderAppID),
		"senderProjectId": idOrEmpty(item.Message.SenderProjectID),
		"senderIconUrl":   notify.SenderIconURL(item.Link.ID, item.Message.SenderAppID),
		"readTime":        item.Link.ReadTime,
	}), nil
}

// UserMessageHandle 消息已处理（Modal/Banner 关闭）。
func (l *Logic) UserMessageHandle(req *types.UserMessageHandleReq) (*types.Envelope, error) {
	if err := l.svcCtx.App.Notify.HandleMessage(l.ctx, logicx.UserID(l.ctx), req.Id); err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"handled": true}), nil
}

// UserMessageDelete 删除消息。
func (l *Logic) UserMessageDelete(req *types.UserMessageDeleteReq) (*types.Envelope, error) {
	if err := l.svcCtx.App.Notify.DeleteMessages(l.ctx, logicx.UserID(l.ctx), req.Ids); err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"deleted": true}), nil
}

// UserMessageUnread 未读数。
func (l *Logic) UserMessageUnread() (*types.Envelope, error) {
	count, err := l.svcCtx.App.Notify.UnreadCount(l.ctx, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	return httpx.Envelope(map[string]any{"count": count}), nil
}

// PromptList 待展示提醒：横幅 + 强提醒（前端全局横幅条与 Modal 数据源）。
func (l *Logic) PromptList() (*types.PromptListResp, error) {
	userID := logicx.UserID(l.ctx)
	rows, err := l.svcCtx.App.Notify.ListPrompts(l.ctx, userID)
	if err != nil {
		return nil, logicx.Error(err)
	}
	resp := &types.PromptListResp{Banners: []types.NotifyBannerItem{}, Modals: []types.NotifyPromptItem{}}
	for _, row := range rows {
		if row.Type == "banner" {
			resp.Banners = append(resp.Banners, types.NotifyBannerItem{
				Id: row.LinkID, Title: row.Title, Content: row.Content, Link: row.Link,
			})
		} else {
			resp.Modals = append(resp.Modals, types.NotifyPromptItem{
				LinkId: row.LinkID, MessageId: row.ID, Kind: "modal", Type: row.Type,
				Title: row.Title, Content: row.Content, Link: row.Link,
			})
		}
	}
	return resp, nil
}

// BannerList 当前有效横幅。

// idOrEmpty 把 id 以字符串下发，与同层其它 id 字段口径一致（大整数不被 JS 精度截断）；
// 0 表示非应用来源或解析不到，下发空串让客户端直接按"没有"处理，
// 而不是拿到 "0" 当成一个合法 id 去请求。
func idOrEmpty(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// UserMessageSenderIcon 返回这条消息发信应用的图标资产 id；handler 负责流式回图片。
// 授权按「这条消息是我的」，收件人不必是该应用所在项目的成员（见 domain 说明）。
func (l *Logic) UserMessageSenderIcon(req *types.UserMessageSenderIconReq) (int64, error) {
	assetID, err := l.svcCtx.App.Notify.SenderIconAssetID(l.ctx, logicx.UserID(l.ctx), req.Id)
	if err != nil {
		return 0, logicx.Error(err)
	}
	return assetID, nil
}
