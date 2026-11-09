// Package repo 消息通知持久化（TASK-030 P0）。
// 语义基线：云端 syssvr relationDB 的 notifyModels/notifyConfig 系列；
// 惯例对齐本仓 audit.go / apikey.go（GetCommonConn(in any) 构造、毫秒时间戳、deleted_time=0 软删）。
package repo

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NotifyMessageRequest OpenAPI 通知请求记录；ID 即对外 messageId。
type NotifyMessageRequest struct {
	ID           int64  `gorm:"column:id;primaryKey" json:"id"`
	KeyID        int64  `gorm:"column:key_id" json:"keyId"`
	RecipientUID int64  `gorm:"column:recipient_user_id" json:"recipientUserId"`
	Type         string `gorm:"column:type" json:"type"`
	Mode         string `gorm:"column:mode" json:"mode"`
	Title        string `gorm:"column:title" json:"title"`
	Content      string `gorm:"column:content" json:"content"`
	Channels     string `gorm:"column:channels" json:"channels"`
	SenderType   string `gorm:"column:sender_type" json:"senderType"`
	SenderID     string `gorm:"column:sender_id" json:"senderId"`
	SenderName   string `gorm:"column:sender_name" json:"senderName"`
	SenderDetail string `gorm:"column:sender_detail" json:"senderDetail"`
	// SenderAppID 服务端按 API Key 解析出的 Launchpad 应用 id（0=非应用来源或解析不到）
	SenderAppID int64 `gorm:"column:sender_app_id" json:"senderAppId"`
	// SenderProjectID 该应用所属项目 id；按 App 取详情与预检的接口都要它
	SenderProjectID int64  `gorm:"column:sender_project_id" json:"senderProjectId"`
	ActionType      string `gorm:"column:action_type" json:"actionType"`
	ActionPath      string `gorm:"column:action_path" json:"actionPath"`
	Status          string `gorm:"column:status" json:"status"`
	ErrorCode       string `gorm:"column:error_code" json:"errorCode"`
	Message         string `gorm:"column:message" json:"message"`
	RequestDigest   string `gorm:"column:request_digest" json:"requestDigest"`
	IdempotencyKey  string `gorm:"column:idempotency_key" json:"idempotencyKey"`
	CreatedTime     int64  `gorm:"column:created_time" json:"createdTime"`
	UpdatedTime     int64  `gorm:"column:updated_time" json:"updatedTime"`
	CompletedTime   int64  `gorm:"column:completed_time" json:"completedTime"`
	DeletedTime     int64  `gorm:"column:deleted_time" json:"deletedTime"`
}

func (NotifyMessageRequest) TableName() string { return "sys_notify_message_request" }

// NotifyMessage 站内信本体；外部消息（OpenAPI）与管理员通知同表混排。
type NotifyMessage struct {
	ID           int64  `gorm:"column:id;primaryKey" json:"id"`
	Type         string `gorm:"column:type" json:"type"`
	GroupCode    string `gorm:"column:group_code" json:"groupCode"`
	Title        string `gorm:"column:title" json:"title"`
	Content      string `gorm:"column:content" json:"content"`
	Link         string `gorm:"column:link" json:"link"`
	AsModal      int64  `gorm:"column:as_modal" json:"asModal"`
	BannerStart  int64  `gorm:"column:banner_start" json:"bannerStart"`
	BannerEnd    int64  `gorm:"column:banner_end" json:"bannerEnd"`
	Source       string `gorm:"column:source" json:"source"`
	Mode         string `gorm:"column:mode" json:"mode"`
	SenderType   string `gorm:"column:sender_type" json:"senderType"`
	SenderID     string `gorm:"column:sender_id" json:"senderId"`
	SenderName   string `gorm:"column:sender_name" json:"senderName"`
	SenderDetail string `gorm:"column:sender_detail" json:"senderDetail"`
	// SenderAppID 服务端按 API Key 解析出的 Launchpad 应用 id（0=非应用来源或解析不到）
	SenderAppID int64 `gorm:"column:sender_app_id" json:"senderAppId"`
	// SenderProjectID 该应用所属项目 id；按 App 取详情与预检的接口都要它
	SenderProjectID int64 `gorm:"column:sender_project_id" json:"senderProjectId"`
	NotifyRequestID int64 `gorm:"column:notify_request_id" json:"notifyRequestId"`
	NotificationID  int64 `gorm:"column:notification_id" json:"notificationId"`
	CreatedTime     int64 `gorm:"column:created_time" json:"createdTime"`
	UpdatedTime     int64 `gorm:"column:updated_time" json:"updatedTime"`
	DeletedTime     int64 `gorm:"column:deleted_time" json:"deletedTime"`
}

func (NotifyMessage) TableName() string { return "sys_notify_message" }

// UserMessage 用户↔消息关联，读状态落此表。
type UserMessage struct {
	ID          int64 `gorm:"column:id;primaryKey" json:"id"`
	UserID      int64 `gorm:"column:user_id" json:"userId"`
	MessageID   int64 `gorm:"column:message_id" json:"messageId"`
	IsRead      int64 `gorm:"column:is_read" json:"isRead"`
	IsHandled   int64 `gorm:"column:is_handled" json:"isHandled"`
	ReadTime    int64 `gorm:"column:read_time" json:"readTime"`
	CreatedTime int64 `gorm:"column:created_time" json:"createdTime"`
	UpdatedTime int64 `gorm:"column:updated_time" json:"updatedTime"`
	DeletedTime int64 `gorm:"column:deleted_time" json:"deletedTime"`
}

func (UserMessage) TableName() string { return "sys_user_message" }

// Notification 管理员手动通知（公告）主记录。
type Notification struct {
	ID              int64  `gorm:"column:id;primaryKey" json:"id"`
	Type            string `gorm:"column:type" json:"type"`
	Title           string `gorm:"column:title" json:"title"`
	Content         string `gorm:"column:content" json:"content"`
	Link            string `gorm:"column:link" json:"link"`
	AsModal         int64  `gorm:"column:as_modal" json:"asModal"`
	AudienceType    int64  `gorm:"column:audience_type" json:"audienceType"`
	AudienceUserIDs string `gorm:"column:audience_user_ids" json:"audienceUserIds"`
	ScheduleTime    int64  `gorm:"column:schedule_time" json:"scheduleTime"`
	Status          int64  `gorm:"column:status" json:"status"`
	SendStatistics  string `gorm:"column:send_statistics" json:"sendStatistics"`
	CreatedBy       int64  `gorm:"column:created_by" json:"createdBy"`
	UpdatedBy       int64  `gorm:"column:updated_by" json:"updatedBy"`
	CreatedTime     int64  `gorm:"column:created_time" json:"createdTime"`
	UpdatedTime     int64  `gorm:"column:updated_time" json:"updatedTime"`
	DeletedTime     int64  `gorm:"column:deleted_time" json:"deletedTime"`
}

func (Notification) TableName() string { return "sys_notification" }

// ---------- 请求记录 repo ----------

type NotifyMessageRequestRepo struct{ db *gorm.DB }

func NewNotifyMessageRequestRepo(in any) *NotifyMessageRequestRepo {
	return &NotifyMessageRequestRepo{db: GetCommonConn(in)}
}

// FindActiveByKeyIdempotency 幂等预查：同 Key 同幂等键的活跃行（deleted_time=0）。
func (r *NotifyMessageRequestRepo) FindActiveByKeyIdempotency(ctx context.Context, keyID int64, idemKey string) (*NotifyMessageRequest, error) {
	var row NotifyMessageRequest
	err := r.db.WithContext(ctx).
		Where("key_id = ? AND idempotency_key = ? AND deleted_time = 0", keyID, idemKey).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// LockByKeyIdempotency 幂等写事务内的 FOR UPDATE 复核。
func (r *NotifyMessageRequestRepo) LockByKeyIdempotency(tx *gorm.DB, keyID int64, idemKey string) (*NotifyMessageRequest, error) {
	var row NotifyMessageRequest
	err := tx.
		Where("key_id = ? AND idempotency_key = ? AND deleted_time = 0", keyID, idemKey).
		Clauses(lockingClause()).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// InsertTx 事务内插入新请求行。
func (r *NotifyMessageRequestRepo) InsertTx(tx *gorm.DB, row *NotifyMessageRequest) error {
	if row.CreatedTime > 0 && row.UpdatedTime == 0 {
		row.UpdatedTime = row.CreatedTime
	}
	return tx.Create(row).Error
}

// Transaction 在请求记录 repo 的公共连接上开事务。
func (r *NotifyMessageRequestRepo) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

// MarkSent 置 sent 终态。
func (r *NotifyMessageRequestRepo) MarkSent(ctx context.Context, id int64) error {
	now := time.Now().UnixMilli()
	return r.db.WithContext(ctx).Model(&NotifyMessageRequest{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": "sent", "completed_time": now, "updated_time": now}).Error
}

// MarkFailed 置 failed 终态并记录原因。
func (r *NotifyMessageRequestRepo) MarkFailed(ctx context.Context, id int64, code, msg string) error {
	now := time.Now().UnixMilli()
	return r.db.WithContext(ctx).Model(&NotifyMessageRequest{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": "failed", "error_code": code, "message": msg, "completed_time": now, "updated_time": now}).Error
}

// FindOneByID 仅按 ID 查询（outbox 消费侧，无 Key 归属约束）。
func (r *NotifyMessageRequestRepo) FindOneByID(ctx context.Context, id int64) (*NotifyMessageRequest, error) {
	var row NotifyMessageRequest
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_time = 0", id).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Save 更新行（终态回写等）。
func (r *NotifyMessageRequestRepo) Save(ctx context.Context, row *NotifyMessageRequest) error {
	row.UpdatedTime = time.Now().UnixMilli()
	return r.db.WithContext(ctx).Save(row).Error
}

// FindOneByIDAndKey 按 messageId 查询并校验 Key 归属（get 接口锚定发起请求的 Key）。
func (r *NotifyMessageRequestRepo) FindOneByIDAndKey(ctx context.Context, id, keyID int64) (*NotifyMessageRequest, error) {
	var row NotifyMessageRequest
	err := r.db.WithContext(ctx).
		Where("id = ? AND key_id = ? AND deleted_time = 0", id, keyID).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// RetireExpired 将创建时间早于 before 的活跃行软删（幂等窗口退休）；返回影响行数。
func (r *NotifyMessageRequestRepo) RetireExpired(ctx context.Context, before time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&NotifyMessageRequest{}).
		Where("deleted_time = 0 AND created_time < ?", before.UnixMilli()).
		Update("deleted_time", time.Now().UnixMilli())
	return res.RowsAffected, res.Error
}

// DeleteOlderThan 物理删除早于 before 的行（保留期清理）。
func (r *NotifyMessageRequestRepo) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res := r.db.WithContext(ctx).
		Where("deleted_time > 0 AND deleted_time < ? OR created_time < ?", before.UnixMilli(), before.UnixMilli()).
		Delete(&NotifyMessageRequest{})
	return res.RowsAffected, res.Error
}

// lockingClause 幂等写事务的 SELECT ... FOR UPDATE。
func lockingClause() *clause.Locking {
	return &clause.Locking{Strength: "UPDATE"}
}

// ---------- 站内信 / 用户消息 repo ----------

type NotifyMessageRepo struct{ db *gorm.DB }

func NewNotifyMessageRepo(in any) *NotifyMessageRepo {
	return &NotifyMessageRepo{db: GetCommonConn(in)}
}

// FindActiveUserIDByUsername 按稳定登录名解析站内信收件人，并排除停用或删除用户。
func (r *NotifyMessageRepo) FindActiveUserIDByUsername(ctx context.Context, username string) (int64, error) {
	var user struct {
		ID int64 `gorm:"column:user_id"`
	}
	err := r.db.WithContext(ctx).Table("sys_user_info").
		Select("user_id").
		Where("user_name = ? AND status = 1 AND deleted_time = 0", username).
		Take(&user).Error
	return user.ID, err
}

// InsertTxWithUserMessages 事务内建站内信 + 批量写用户关联（管理端分批调用与 OpenAPI 建信共用）。
// tx 为 nil 时内部自动开事务；收件人去重由调用方保证。
func (r *NotifyMessageRepo) InsertTxWithUserMessages(tx *gorm.DB, msg *NotifyMessage, userIDs []int64) error {
	if tx == nil {
		tx = r.db
	}
	return tx.Transaction(func(sp *gorm.DB) error {
		msg.CreatedTime = time.Now().UnixMilli()
		msg.UpdatedTime = msg.CreatedTime
		if err := sp.Create(msg).Error; err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		links := make([]UserMessage, 0, len(userIDs))
		for _, uid := range userIDs {
			if uid <= 0 {
				continue
			}
			links = append(links, UserMessage{UserID: uid, MessageID: msg.ID, CreatedTime: now, UpdatedTime: now})
		}
		if len(links) == 0 {
			return nil
		}
		return sp.CreateInBatches(links, 500).Error
	})
}

// SoftDeleteByNotificationID 撤回时按公告 ID 整体软删站内信与用户关联。
func (r *NotifyMessageRepo) SoftDeleteByNotificationID(ctx context.Context, notificationID int64) error {
	now := time.Now().UnixMilli()
	return r.db.WithContext(ctx).Transaction(func(sp *gorm.DB) error {
		if err := sp.Model(&NotifyMessage{}).
			Where("notification_id = ? AND deleted_time = 0", notificationID).
			Updates(map[string]any{"deleted_time": now, "updated_time": now}).Error; err != nil {
			return err
		}
		return sp.Exec(`UPDATE sys_user_message um SET deleted_time = ?, updated_time = ?
			FROM sys_notify_message m WHERE um.message_id = m.id AND m.notification_id = ? AND um.deleted_time = 0`,
			now, now, notificationID).Error
	})
}

// NotifyMessageFilter 用户消息列表过滤条件。
// Keyword/StartTime/EndTime/SenderType 对齐前端消息中心筛选（关键词/时间范围/来源类型）。
type NotifyMessageFilter struct {
	UserID     int64
	Group      string
	UnreadOnly bool
	// Keyword 标题/内容模糊匹配（ILIKE）
	Keyword string
	// StartTime/EndTime 创建时间范围（毫秒，0=不限）
	StartTime int64
	EndTime   int64
	// SenderType 来源类型：'app'=应用发送；'platform'=平台/系统发送（空=全部）
	SenderType string
	// SenderID / SenderIDs 使用可信来源 ID 筛选（映射 sourceId/sourceIds，平台来源为 platform）。
	SenderID  string
	SenderIDs []string
	PageNo    int
	PageSize  int
}

// UserMessageItem 用户消息列表项：关联行（Link，读状态）+ 消息本体（Message）。
type UserMessageItem struct {
	Link    UserMessage
	Message NotifyMessage
	Source  NotificationMessageSource
}

// NotificationMessageSource 只从鉴权 Key 和平台应用记录解析，不使用请求自报的 sender 元数据。
type NotificationMessageSource struct {
	SourceType         string          `gorm:"column:source_type"`
	SourceID           string          `gorm:"column:source_id"`
	SourceName         string          `gorm:"column:source_name"`
	ProjectID          int64           `gorm:"column:source_project_id"`
	AppID              int64           `gorm:"column:source_app_id"`
	AppStatus          string          `gorm:"column:source_app_status"`
	AppManifest        json.RawMessage `gorm:"column:source_app_manifest"`
	AppEntry           string          `gorm:"column:source_app_entry"`
	AppDeletedTime     int64           `gorm:"column:source_app_deleted_time"`
	ProjectDeletedTime int64           `gorm:"column:source_project_deleted_time"`
}

type sourcedNotifyMessage struct {
	NotifyMessage             `gorm:"embedded"`
	NotificationMessageSource `gorm:"embedded"`
}

// ListByUser 用户消息列表（联表过滤 + 分页取关联行，再批量取消息本体两步完成）。
func (r *NotifyMessageRepo) ListByUser(ctx context.Context, f NotifyMessageFilter) ([]UserMessageItem, int64, error) {
	if f.PageNo <= 0 {
		f.PageNo = 1
	}
	if f.PageSize <= 0 || f.PageSize > 100 {
		f.PageSize = 20
	}
	base := func() *gorm.DB {
		q := r.messageHistoryBase(ctx).
			Joins("JOIN sys_user_message um ON um.message_id = m.id").
			Where("um.user_id = ? AND um.deleted_time = 0 AND m.deleted_time = 0", f.UserID)
		q = applyUserMessageFilters(q, f)
		return q
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var links []UserMessage
	err := base().Select("um.id, um.user_id, um.message_id, um.is_read, um.is_handled, um.read_time, um.created_time, um.updated_time").
		Order("um.id DESC").
		Offset((f.PageNo - 1) * f.PageSize).Limit(f.PageSize).
		Scan(&links).Error
	if err != nil {
		return nil, 0, err
	}
	ids := make([]int64, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.MessageID)
	}
	msgs, err := r.ItemsByMessageIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	items := make([]UserMessageItem, 0, len(links))
	for _, l := range links {
		if msg, ok := msgs[l.MessageID]; ok {
			items = append(items, UserMessageItem{Link: l, Message: msg.NotifyMessage, Source: msg.NotificationMessageSource})
		}
	}
	return items, total, nil
}

// applyUserMessageFilters 消息中心列表/批量已读共用的筛选条件（关键词/时间范围/来源类型/分组/未读）。
func applyUserMessageFilters(q *gorm.DB, f NotifyMessageFilter) *gorm.DB {
	if f.Group != "" {
		q = q.Where("m.group_code = ?", f.Group)
	}
	if f.UnreadOnly {
		q = q.Where("um.is_read = 0")
	}
	if f.Keyword != "" {
		kw := "%" + f.Keyword + "%"
		q = q.Where("(m.title ILIKE ? OR m.content ILIKE ?)", kw, kw)
	}
	if f.StartTime > 0 {
		q = q.Where("m.created_time >= ?", f.StartTime)
	}
	if f.EndTime > 0 {
		q = q.Where("m.created_time < ?", f.EndTime)
	}
	switch f.SenderType {
	case "app":
		q = q.Where(historyAppSourceCondition)
	case "platform":
		q = q.Where("NOT " + historyAppSourceCondition)
	}
	if f.SenderID != "" {
		q = q.Where("("+historySourceIDExpression+") = ?", f.SenderID)
	}
	if len(f.SenderIDs) > 0 {
		q = q.Where("("+historySourceIDExpression+") IN ?", f.SenderIDs)
	}
	return q
}

// NotificationSourceItem 消息来源聚合项（来源下拉数据）。
type NotificationSourceItem struct {
	SourceType string `gorm:"column:sourceType" json:"sourceType"`
	SourceID   string `gorm:"column:sourceId" json:"sourceId"`
	SourceName string `gorm:"column:sourceName" json:"sourceName"`
}

// MessageHistoryFilter 管理端统一消息历史筛选条件。
type MessageHistoryFilter struct {
	Keyword         string
	SourceType      string
	SourceID        string
	Type            string
	Status          string
	RecipientUserID int64
	StartTime       int64
	EndTime         int64
	Page            int
	Size            int
}

// MessageHistoryItem 是平台、系统与 App 消息统一展示的管理端历史项。
type MessageHistoryItem struct {
	MessageID       int64  `gorm:"column:message_id" json:"messageId"`
	Type            string `gorm:"column:type" json:"type"`
	Title           string `gorm:"column:title" json:"title"`
	Content         string `gorm:"column:content" json:"content"`
	Link            string `gorm:"column:link" json:"link"`
	Origin          string `gorm:"column:origin" json:"origin"`
	Mode            string `gorm:"column:mode" json:"mode"`
	SourceType      string `gorm:"column:source_type" json:"sourceType"`
	SourceID        string `gorm:"column:source_id" json:"sourceId"`
	SourceName      string `gorm:"column:source_name" json:"sourceName"`
	SenderDetail    string `gorm:"column:sender_detail" json:"senderDetail"`
	RecipientCount  int64  `gorm:"column:recipient_count" json:"recipientCount"`
	ReadCount       int64  `gorm:"column:read_count" json:"readCount"`
	NotifyRequestID int64  `gorm:"column:notify_request_id" json:"notifyRequestId"`
	NotificationID  int64  `gorm:"column:notification_id" json:"notificationId"`
	Status          string `gorm:"column:status" json:"status"`
	CreatedTime     int64  `gorm:"column:created_time" json:"createdTime"`
}

// 历史来源身份只使用 API Key 鉴权记录中的可信 App 归属；sender_type/id/name 均为调用方展示元数据。
// V2.2.4 保留未重新部署应用的 service 型 Key，不回填 app_id，需兼容旧注册形态。
const historyLegacyAppKeyCondition = `(COALESCE(ak.owner_type, '') = 'service'
	AND COALESCE(ak.usage_type, '') = 'internal' AND COALESCE(ak.name, '') LIKE 'app-%')`

const historyAppSourceCondition = "(COALESCE(ak.app_id, '') <> '' OR COALESCE(ak.owner_type, '') = 'app' OR " + historyLegacyAppKeyCondition + ")"

const historySourceIDExpression = `CASE WHEN ` + historyAppSourceCondition + `
	THEN COALESCE(NULLIF(ak.app_id, ''), NULLIF(app_meta.runtime_app_id, ''), CASE WHEN mr.key_id > 0 THEN 'api-key:' || mr.key_id::text END, 'legacy-app')
	ELSE 'platform' END`

const historySourceNameExpression = `CASE WHEN ` + historyAppSourceCondition + `
	THEN COALESCE(NULLIF(app_meta.app_name, ''), NULLIF(ak.name, ''), NULLIF(ak.app_id, ''), 'App')
	ELSE 'Platform' END`

func (r *NotifyMessageRepo) messageHistoryBase(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Table("sys_notify_message m").
		Joins("LEFT JOIN sys_notify_message_request mr ON mr.id = m.notify_request_id").
		Joins("LEFT JOIN sys_api_key ak ON ak.id = mr.key_id").
		Joins(`LEFT JOIN LATERAL (
			SELECT pa.name AS app_name, pa.id AS project_app_id, pa.project_id,
				pa.manifest_json->>'app_id' AS runtime_app_id,
				pa.status AS app_status, pa.manifest_json AS app_manifest,
				COALESCE(NULLIF(pa.entry_url, ''), NULLIF(pa.entry_path, ''), pa.route_prefix) AS app_entry,
				pa.deleted_time AS app_deleted_time, p.deleted_time AS project_deleted_time
			FROM project_app pa
			JOIN project p ON p.id = pa.project_id
			WHERE pa.manifest_json->>'app_id' = NULLIF(ak.app_id, '')
				OR (COALESCE(ak.app_id, '') = '' AND ` + historyLegacyAppKeyCondition + `
					AND ak.name = 'app-' || NULLIF(pa.manifest_json->>'app_id', '') || '-default')
			ORDER BY CASE WHEN pa.deleted_time = 0 THEN 0 ELSE 1 END, pa.id DESC
			LIMIT 1
		) app_meta ON TRUE`)
}

// ListHistory 查询完整投递历史。消息或用户侧关联即使已软删仍保留在历史中，以便追溯撤回和用户删除。
func (r *NotifyMessageRepo) ListHistory(ctx context.Context, f MessageHistoryFilter) ([]MessageHistoryItem, int64, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.Size <= 0 || f.Size > 100 {
		f.Size = 20
	}
	base := func() *gorm.DB {
		return applyMessageHistoryFilters(r.messageHistoryBase(ctx), f)
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []MessageHistoryItem
	err := base().Select(`m.id AS message_id, m.type, m.title, m.content, m.link,
		m.source AS origin, m.mode,
		CASE WHEN ` + historyAppSourceCondition + ` THEN 'app' ELSE 'platform' END AS source_type,
		` + historySourceIDExpression + ` AS source_id,
		` + historySourceNameExpression + ` AS source_name,
		m.sender_detail,
		(SELECT COUNT(*) FROM sys_user_message um WHERE um.message_id = m.id) AS recipient_count,
		(SELECT COUNT(*) FROM sys_user_message um WHERE um.message_id = m.id AND um.is_read <> 0) AS read_count,
		m.notify_request_id, m.notification_id,
		CASE WHEN m.deleted_time > 0 THEN 'revoked' ELSE 'sent' END AS status,
		m.created_time`).
		Order("m.created_time DESC, m.id DESC").
		Offset((f.Page - 1) * f.Size).Limit(f.Size).
		Scan(&rows).Error
	return rows, total, err
}

func applyMessageHistoryFilters(q *gorm.DB, f MessageHistoryFilter) *gorm.DB {
	if keyword := strings.TrimSpace(f.Keyword); keyword != "" {
		value := "%" + keyword + "%"
		q = q.Where("(m.title ILIKE ? OR m.content ILIKE ? OR m.sender_name ILIKE ? OR m.sender_id ILIKE ? OR ak.app_id ILIKE ? OR ak.name ILIKE ? OR app_meta.app_name ILIKE ?)", value, value, value, value, value, value, value)
	}
	if f.StartTime > 0 {
		q = q.Where("m.created_time >= ?", f.StartTime)
	}
	if f.EndTime > 0 {
		q = q.Where("m.created_time < ?", f.EndTime)
	}
	switch f.SourceType {
	case "app":
		q = q.Where(historyAppSourceCondition)
	case "platform":
		q = q.Where("NOT " + historyAppSourceCondition)
	}
	if sourceID := strings.TrimSpace(f.SourceID); sourceID != "" {
		q = q.Where("("+historySourceIDExpression+") = ?", sourceID)
	}
	if messageType := strings.TrimSpace(f.Type); messageType != "" {
		q = q.Where("m.type = ?", messageType)
	}
	switch f.Status {
	case "sent":
		q = q.Where("m.deleted_time = 0")
	case "revoked":
		q = q.Where("m.deleted_time > 0")
	}
	if f.RecipientUserID > 0 {
		q = q.Where("EXISTS (SELECT 1 FROM sys_user_message um_filter WHERE um_filter.message_id = m.id AND um_filter.user_id = ?)", f.RecipientUserID)
	}
	return q
}

// AggregateHistorySources 返回完整历史中的来源下拉项，不受当前筛选条件影响。
func (r *NotifyMessageRepo) AggregateHistorySources(ctx context.Context) ([]NotificationSourceItem, error) {
	var rows []NotificationSourceItem
	sourceRows := r.messageHistoryBase(ctx).Select(
		`CASE WHEN ` + historyAppSourceCondition + ` THEN 'app' ELSE 'platform' END AS source_type, ` +
			historySourceIDExpression + ` AS source_id, ` +
			historySourceNameExpression + ` AS source_name`,
	)
	err := r.db.WithContext(ctx).Table("(?) AS history_sources", sourceRows).
		Select(`source_type AS "sourceType", source_id AS "sourceId", MAX(source_name) AS "sourceName"`).
		Group("source_type, source_id").
		Order(`MAX(source_name), source_id`).
		Scan(&rows).Error
	return rows, err
}

// NotificationRecipientUser 通知管理可选的最小用户信息，不暴露角色、联系方式等 IAM 管理字段。
type NotificationRecipientUser struct {
	UserID      int64  `gorm:"column:user_id" json:"userId"`
	UserName    string `gorm:"column:user_name" json:"userName"`
	DisplayName string `gorm:"column:display_name" json:"displayName"`
}

// NotificationRecipientGroup 通知管理可选用户组及其当前有效用户 ID。
type NotificationRecipientGroup struct {
	GroupID     int64   `gorm:"column:group_id" json:"groupId"`
	GroupName   string  `gorm:"column:group_name" json:"groupName"`
	Description string  `gorm:"column:description" json:"description"`
	MemberCount int64   `gorm:"column:member_count" json:"memberCount"`
	UserIDs     []int64 `gorm:"-" json:"userIds"`
}

// ListNotificationRecipientOptions 返回通知管理权限范围内的有效用户和用户组候选。
func (r *NotifyMessageRepo) ListNotificationRecipientOptions(ctx context.Context) ([]NotificationRecipientUser, []NotificationRecipientGroup, error) {
	var users []NotificationRecipientUser
	if err := r.db.WithContext(ctx).Table("sys_user_info").
		Select("user_id, user_name, nick_name AS display_name").
		Where("status = 1 AND deleted_time = 0").
		Order("user_name, user_id").
		Scan(&users).Error; err != nil {
		return nil, nil, err
	}

	var groups []NotificationRecipientGroup
	if err := r.db.WithContext(ctx).Table("sys_user_group g").
		Select(`g.id AS group_id, g.group_name, g.description,
			(SELECT COUNT(*) FROM sys_user_group_member gm
			 JOIN sys_user_info u ON u.user_id = gm.user_id AND u.status = 1 AND u.deleted_time = 0
			 WHERE gm.group_id = g.id) AS member_count`).
		Where("g.deleted_time = 0").
		Order("g.group_name, g.id").
		Scan(&groups).Error; err != nil {
		return nil, nil, err
	}

	type membership struct {
		GroupID int64 `gorm:"column:group_id"`
		UserID  int64 `gorm:"column:user_id"`
	}
	var memberships []membership
	if err := r.db.WithContext(ctx).Table("sys_user_group_member gm").
		Select("gm.group_id, gm.user_id").
		Joins("JOIN sys_user_group g ON g.id = gm.group_id AND g.deleted_time = 0").
		Joins("JOIN sys_user_info u ON u.user_id = gm.user_id AND u.status = 1 AND u.deleted_time = 0").
		Order("gm.group_id, gm.user_id").
		Scan(&memberships).Error; err != nil {
		return nil, nil, err
	}
	groupIndex := make(map[int64]int, len(groups))
	for i := range groups {
		groups[i].UserIDs = []int64{}
		groupIndex[groups[i].GroupID] = i
	}
	for _, item := range memberships {
		if index, ok := groupIndex[item.GroupID]; ok {
			groups[index].UserIDs = append(groups[index].UserIDs, item.UserID)
		}
	}
	return users, groups, nil
}

// AggregateUserSources 聚合当前用户消息范围内全部来源（不受来源筛选影响）。
func (r *NotifyMessageRepo) AggregateUserSources(ctx context.Context, userID int64) ([]NotificationSourceItem, error) {
	var rows []NotificationSourceItem
	sourceRows := r.messageHistoryBase(ctx).
		Joins("JOIN sys_user_message um ON um.message_id = m.id AND um.user_id = ? AND um.deleted_time = 0", userID).
		Where("m.deleted_time = 0").
		Select(historySourceIDExpression + ` AS source_id,
			CASE WHEN ` + historyAppSourceCondition + ` THEN 'app' ELSE 'platform' END AS source_type,
			` + historySourceNameExpression + ` AS source_name`)
	err := r.db.WithContext(ctx).Table("(?) AS user_sources", sourceRows).
		Select(`source_type AS "sourceType", source_id AS "sourceId", MAX(source_name) AS "sourceName"`).
		Group("source_type, source_id").Order("MAX(source_name), source_id").Scan(&rows).Error
	return rows, err
}

// MarkAllReadByFilter 按筛选条件批量已读（消息中心"全部已读"携带筛选时）；返回影响行数。
// 两步执行：先联表筛出命中的关联行 ID，再按 ID 批量更新——规避 gorm JOIN+Updates 的 SET 别名限制。
func (r *NotifyMessageRepo) MarkAllReadByFilter(ctx context.Context, userID int64, f NotifyMessageFilter) (int64, error) {
	q := r.messageHistoryBase(ctx).
		Joins("JOIN sys_user_message um ON um.message_id = m.id").
		Where("um.user_id = ? AND um.deleted_time = 0 AND um.is_read = 0 AND m.deleted_time = 0", userID)
	q = applyUserMessageFilters(q, f)
	var ids []int64
	if err := q.Pluck("um.id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	res := r.db.WithContext(ctx).Model(&UserMessage{}).
		Where("id IN ? AND is_read = 0", ids).
		Updates(map[string]any{"is_read": 1, "read_time": now, "updated_time": now})
	return res.RowsAffected, res.Error
}

// FindOneByUser 单条用户消息（本人，联表消息本体）——消息中心详情。
func (r *NotifyMessageRepo) FindOneByUser(ctx context.Context, userID, linkID int64) (*UserMessageItem, error) {
	var link UserMessage
	err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND deleted_time = 0", linkID, userID).First(&link).Error
	if err != nil {
		return nil, err
	}
	msgs, err := r.ItemsByMessageIDs(ctx, []int64{link.MessageID})
	if err != nil {
		return nil, err
	}
	msg, ok := msgs[link.MessageID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return &UserMessageItem{Link: link, Message: msg.NotifyMessage, Source: msg.NotificationMessageSource}, nil
}

// ItemsByMessageIDs 批量取消息本体并按 ID 索引。
func (r *NotifyMessageRepo) ItemsByMessageIDs(ctx context.Context, ids []int64) (map[int64]sourcedNotifyMessage, error) {
	out := make(map[int64]sourcedNotifyMessage, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []sourcedNotifyMessage
	if err := r.messageHistoryBase(ctx).
		Select(`m.*, CASE WHEN `+historyAppSourceCondition+` THEN 'app' ELSE 'platform' END AS source_type,
			`+historySourceIDExpression+` AS source_id, `+historySourceNameExpression+` AS source_name,
			app_meta.project_id AS source_project_id, app_meta.project_app_id AS source_app_id,
			app_meta.app_status AS source_app_status, app_meta.app_manifest AS source_app_manifest,
			app_meta.app_entry AS source_app_entry, app_meta.app_deleted_time AS source_app_deleted_time,
			app_meta.project_deleted_time AS source_project_deleted_time`).
		Where("m.id IN ? AND m.deleted_time = 0", ids).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}

// CountUnread 未读统计。
func (r *NotifyMessageRepo) CountUnread(ctx context.Context, userID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Table("sys_user_message um").
		Joins("JOIN sys_notify_message m ON m.id = um.message_id AND m.deleted_time = 0").
		Where("um.user_id = ? AND um.deleted_time = 0 AND um.is_read = 0", userID).
		Count(&count).Error
	return count, err
}

// MarkRead 标记指定消息已读（仅本人）。
func (r *NotifyMessageRepo) MarkRead(ctx context.Context, userID int64, ids []int64) error {
	updates := map[string]any{"is_read": 1, "read_time": time.Now().UnixMilli(), "updated_time": time.Now().UnixMilli()}
	q := r.db.WithContext(ctx).Model(&UserMessage{}).
		Where("user_id = ? AND deleted_time = 0 AND is_read = 0", userID)
	if len(ids) > 0 {
		q = q.Where("id IN ?", ids)
	}
	return q.Updates(updates).Error
}

// MarkHandled 标记已处理（Modal/Banner 关闭，仅本人）。
func (r *NotifyMessageRepo) MarkHandled(ctx context.Context, userID, linkID int64) error {
	now := time.Now().UnixMilli()
	return r.db.WithContext(ctx).Model(&UserMessage{}).
		Where("id = ? AND user_id = ? AND deleted_time = 0", linkID, userID).
		Updates(map[string]any{"is_handled": 1, "updated_time": now}).Error
}

// SoftDeleteByIDs 软删本人消息关联（消息本体保留，其他收件人不受影响）。
func (r *NotifyMessageRepo) SoftDeleteByIDs(ctx context.Context, userID int64, ids []int64) error {
	now := time.Now().UnixMilli()
	return r.db.WithContext(ctx).Model(&UserMessage{}).
		Where("user_id = ? AND id IN ? AND deleted_time = 0", userID, ids).
		Updates(map[string]any{"deleted_time": now, "updated_time": now}).Error
}

// PromptItem 待展示提醒项：横幅或未处理强提醒。
type PromptItem struct {
	NotifyMessage
	LinkID    int64 `json:"linkId"`
	IsHandled int64 `json:"isHandled"`
}

// ListPendingPrompts 当前用户待展示提醒：有效期内横幅 + 未读且未处理的强提醒（as_modal=1）。
func (r *NotifyMessageRepo) ListPendingPrompts(ctx context.Context, userID int64, now time.Time) ([]PromptItem, error) {
	ms := now.UnixMilli()
	var rows []PromptItem
	err := r.db.WithContext(ctx).
		Table("sys_notify_message m").
		Joins("JOIN sys_user_message um ON um.message_id = m.id AND um.user_id = ? AND um.deleted_time = 0", userID).
		Where(
			"(m.type = 'banner' AND m.banner_start <= ? AND m.banner_end > ?) OR (m.as_modal = 1)",
			ms, ms,
		).
		Where("m.deleted_time = 0 AND um.is_handled = 0 AND um.user_id = ?", userID).
		Select("m.*, um.id AS link_id, um.is_handled").
		Order("m.id DESC").Limit(50).
		Scan(&rows).Error
	return rows, err
}

// ListActiveBanners 当前有效的横幅（按用户过滤已处理/已删由调用层做）。
func (r *NotifyMessageRepo) ListActiveBanners(ctx context.Context, now time.Time) ([]NotifyMessage, error) {
	var rows []NotifyMessage
	ms := now.UnixMilli()
	err := r.db.WithContext(ctx).
		Where("type = 'banner' AND deleted_time = 0 AND banner_start <= ? AND banner_end > ?", ms, ms).
		Order("id DESC").Find(&rows).Error
	return rows, err
}

// ---------- 管理员公告 repo ----------

type NotificationRepo struct{ db *gorm.DB }

func NewNotificationRepo(in any) *NotificationRepo { return &NotificationRepo{db: GetCommonConn(in)} }

func (r *NotificationRepo) Create(ctx context.Context, row *Notification) error {
	row.CreatedTime = time.Now().UnixMilli()
	row.UpdatedTime = row.CreatedTime
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *NotificationRepo) Save(ctx context.Context, row *Notification) error {
	row.UpdatedTime = time.Now().UnixMilli()
	return r.db.WithContext(ctx).Save(row).Error
}

// FindOneForUpdate 行锁查询（SELECT ... FOR UPDATE），管理端发送/撤回的并发防护。
func (r *NotificationRepo) FindOneForUpdate(tx *gorm.DB, id int64) (*Notification, error) {
	var row Notification
	err := tx.
		Where("id = ? AND deleted_time = 0", id).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Transaction 在公告 repo 的公共连接上开事务。
func (r *NotificationRepo) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

// SaveTx 事务内保存。
func (r *NotificationRepo) SaveTx(tx *gorm.DB, row *Notification) error {
	row.UpdatedTime = time.Now().UnixMilli()
	return tx.Save(row).Error
}

// AdvanceStatus 条件状态推进：仅当状态仍为 expect 时才更新，返回是否推进成功（并发防双发）。
func (r *NotificationRepo) AdvanceStatus(ctx context.Context, id int64, expect, next int64) (bool, error) {
	res := r.db.WithContext(ctx).Model(&Notification{}).
		Where("id = ? AND status = ? AND deleted_time = 0", id, expect).
		Update("status", next)
	return res.RowsAffected > 0, res.Error
}

func (r *NotificationRepo) FindOne(ctx context.Context, id int64) (*Notification, error) {
	var row Notification
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_time = 0", id).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// NotificationListFilter 管理端公告列表过滤。
type NotificationListFilter struct {
	Type   string
	Status int64
	PageNo int
	// PageSize 每页大小，上限 100
	PageSize int
}

func (r *NotificationRepo) List(ctx context.Context, f NotificationListFilter) ([]Notification, int64, error) {
	if f.PageNo <= 0 {
		f.PageNo = 1
	}
	if f.PageSize <= 0 || f.PageSize > 100 {
		f.PageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&Notification{}).Where("deleted_time = 0")
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if f.Status > 0 {
		q = q.Where("status = ?", f.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Notification
	err := q.Order("id DESC").Offset((f.PageNo - 1) * f.PageSize).Limit(f.PageSize).Find(&rows).Error
	return rows, total, err
}

// ListScheduledDue 到期的已规划通知（定时发送扫描）。
func (r *NotificationRepo) ListScheduledDue(ctx context.Context, now time.Time, limit int) ([]Notification, error) {
	var rows []Notification
	err := r.db.WithContext(ctx).
		Where("status = 2 AND schedule_time > 0 AND schedule_time <= ? AND deleted_time = 0", now.UnixMilli()).
		Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

// ---------- 用户存在性（收件人校验） ----------

// ActiveUserExists 收件人校验：平台内有效用户（status=1 未删）。
func (r *NotifyMessageRequestRepo) ActiveUserExists(ctx context.Context, userID int64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Table("sys_user_info").
		Where("user_id = ? AND status = 1 AND deleted_time = 0", userID).
		Count(&count).Error
	return count > 0, err
}

// ListActiveUserIDs 分批取平台全部有效用户 ID（全部用户受众）。
func (r *NotificationRepo) ListActiveUserIDs(ctx context.Context, offset, limit int) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Table("sys_user_info").
		Where("status = 1 AND deleted_time = 0").
		Order("user_id ASC").Offset(offset).Limit(limit).
		Pluck("user_id", &ids).Error
	return ids, err
}

// CountActiveUsers 平台有效用户总数（受众预估）。
func (r *NotificationRepo) CountActiveUsers(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Table("sys_user_info").
		Where("status = 1 AND deleted_time = 0").Count(&count).Error
	return count, err
}
