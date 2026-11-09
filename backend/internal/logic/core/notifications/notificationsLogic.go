// Package notifications 消息通知 REST 契约层（/api/core/notifications*，TASK-030 收敛 feat-uidesign 前端）。
//
// 该套端点为 feat-uidesign 分支消息中心/收件箱 UI 的数据源契约（作者 shimu/greta），
// 与 /api/core/notify/user-message/* 同源同库，仅对外形状不同：
//   - 条目为 NotificationItem（id=用户消息关联 id 的 string、createdAt/readAt=ISO、
//     sourceType=platform|app、sourceId=sender_id（平台来源为 'platform'）、targetPath=link）
//   - 列表响应含 sources 聚合（来源下拉）
//   - read-all 请求体即筛选条件，返回 {affected}
package notifications

import (
	"context"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	domainnotify "backend/internal/domain/notify"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

// NotificationEntry / SourceEntry 复用 domain 的 REST 契约形态。
type NotificationEntry = domainnotify.NotificationEntry

type Logic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *Logic {
	return &Logic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// PageQuery GET /notifications 查询参数。
type PageQuery struct {
	PageNo     int      `form:"pageNo,optional"`
	PageSize   int      `form:"pageSize,optional"`
	ReadStatus string   `form:"readStatus,optional"` // all | unread
	SourceType string   `form:"sourceType,optional"` // platform | app
	SourceID   string   `form:"sourceId,optional"`
	SourceIds  []string `form:"sourceIds,optional"`
	Keyword    string   `form:"keyword,optional"`
	StartTime  string   `form:"startTime,optional"` // ISO8601
	EndTime    string   `form:"endTime,optional"`   // ISO8601
}

func (q *PageQuery) toDomainQuery() domainnotify.UserMessageQuery {
	return domainnotify.UserMessageQuery{
		UnreadOnly: q.ReadStatus == "unread",
		SourceType: q.SourceType,
		SourceID:   q.SourceID,
		SourceIds:  q.SourceIds,
		Keyword:    q.Keyword,
		StartTime:  parseISOMs(q.StartTime),
		EndTime:    parseISOMs(q.EndTime),
	}
}

func parseISOMs(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UnixMilli()
	}
	return 0
}

func subject(ctx context.Context) (contextx.Subject, error) {
	sub, ok := contextx.SubjectFrom(ctx)
	if !ok {
		return contextx.Subject{}, domainnotify.ErrInvalidCredential
	}
	return sub, nil
}

// Page GET /notifications：分页 + 筛选 + 来源聚合。
func (l *Logic) Page(q *PageQuery) (*PageResp, error) {
	userID := logicx.UserID(l.ctx)
	dq := q.toDomainQuery()
	items, total, err := l.svcCtx.App.Notify.ListUserMessages(l.ctx, userID, dq, q.PageNo, q.PageSize)
	if err != nil {
		return nil, logicx.Error(err)
	}
	sources, err := l.svcCtx.App.Notify.NotificationSources(l.ctx, userID)
	if err != nil {
		return nil, logicx.Error(err)
	}
	data := make([]NotificationEntry, 0, len(items))
	for _, item := range items {
		data = append(data, entryFromItem(item))
	}
	sourceList := make([]SourceEntry, 0, len(sources))
	for _, s := range sources {
		sourceList = append(sourceList, SourceEntry{SourceID: s.SourceID, SourceType: s.SourceType, SourceName: s.SourceName})
	}
	return &PageResp{
		PageNo:   orDefault(q.PageNo, 1),
		PageSize: orDefault(q.PageSize, 20),
		Total:    total,
		Data:     data,
		Sources:  sourceList,
	}, nil
}

// PageResp GET /notifications 响应（对方 normalizePage 消费的形态）。
type PageResp struct {
	PageNo   int                 `json:"pageNo"`
	PageSize int                 `json:"pageSize"`
	Total    int64               `json:"total"`
	Data     []NotificationEntry `json:"data"`
	Sources  []SourceEntry       `json:"sources"`
}

type SourceEntry struct {
	SourceID   string `json:"sourceId"`
	SourceType string `json:"sourceType"`
	SourceName string `json:"sourceName"`
}

func entryFromItem(item repo.UserMessageItem) NotificationEntry {
	// 复用 domain 的字段映射（保持 /notify 与 REST 两面口径一致）。
	// NotificationEntry 是 domain 那个类型的别名，直接返回即可——原先在这里逐字段
	// 重建一遍，domain 上新增的字段（sourceIconUrl）会被悄悄丢掉，且两边都不会报错。
	return domainnotify.ToNotificationEntry(item)
}

// Detail GET /notifications/:id。
func (l *Logic) Detail(id string) (*NotificationEntry, error) {
	userID := logicx.UserID(l.ctx)
	linkID, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if err != nil || linkID <= 0 {
		return nil, domainnotify.ErrInvalidRequest
	}
	item, err := l.svcCtx.App.Notify.GetUserMessageDetail(l.ctx, userID, linkID)
	if err != nil {
		return nil, logicx.Error(err)
	}
	entry := entryFromItem(*item)
	return &entry, nil
}

// UnreadCount GET /notifications/unread-count。
func (l *Logic) UnreadCount() (int64, error) {
	return l.svcCtx.App.Notify.UnreadCount(l.ctx, logicx.UserID(l.ctx))
}

// Read POST /notifications/read：按 id 批量已读。
func (l *Logic) Read(ids []string) (int64, error) {
	userID := logicx.UserID(l.ctx)
	return l.svcCtx.App.Notify.MarkNotificationsRead(l.ctx, userID, ids)
}

// ReadAllFilter POST /notifications/read-all 请求体（即筛选条件，全部可选）。
type ReadAllFilter struct {
	ReadStatus string   `json:"readStatus,optional"`
	SourceType string   `json:"sourceType,optional"`
	SourceID   string   `json:"sourceId,optional"`
	SourceIds  []string `json:"sourceIds,optional"`
	Keyword    string   `json:"keyword,optional"`
	StartTime  string   `json:"startTime,optional"`
	EndTime    string   `json:"endTime,optional"`
}

// ReadAll POST /notifications/read-all：按筛选全部已读，返回影响条数。
func (l *Logic) ReadAll(f *ReadAllFilter) (int64, error) {
	userID := logicx.UserID(l.ctx)
	dq := domainnotify.UserMessageQuery{
		UnreadOnly: f.ReadStatus == "unread",
		SourceType: f.SourceType,
		SourceID:   f.SourceID,
		SourceIds:  f.SourceIds,
		Keyword:    f.Keyword,
		StartTime:  parseISOMs(f.StartTime),
		EndTime:    parseISOMs(f.EndTime),
	}
	return l.svcCtx.App.Notify.ReadAllByFilter(l.ctx, userID, dq)
}

// Delete POST /notifications/delete。
func (l *Logic) Delete(ids []string) error {
	userID := logicx.UserID(l.ctx)
	return l.svcCtx.App.Notify.DeleteNotifications(l.ctx, userID, ids)
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
