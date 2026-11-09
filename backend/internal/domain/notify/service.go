package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/appstatus"
	"backend/internal/infra/outbox"
	"backend/internal/repo"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

// Service 消息通知领域服务：OpenAPI 六步校验链、outbox 建信投递、管理端公告、用户消息。
type Service struct {
	reqs     *repo.NotifyMessageRequestRepo
	messages *repo.NotifyMessageRepo
	userMsgs *repo.NotifyMessageRepo // 复用 NotifyMessageRepo（含 user_message 方法）
	notifies *repo.NotificationRepo
	outbox   *outbox.Service
	limiter  RateLimiter
	// projects 用于把 API Key 携带的 docker app UUID 解析成 Launchpad 应用身份
	projects *repo.ProjectRepo
}

// New 构造通知领域服务（in 可为 ctx 或 *repo.Store，对齐 repo 构造惯例）。
func New(in any) *Service {
	return &Service{
		reqs:     repo.NewNotifyMessageRequestRepo(in),
		messages: repo.NewNotifyMessageRepo(in),
		userMsgs: repo.NewNotifyMessageRepo(in),
		notifies: repo.NewNotificationRepo(in),
		outbox:   outbox.New(),
		limiter:  NewMemoryRateLimiter(),
		projects: repo.NewProjectRepo(in),
	}
}

// SystemMessageInput 是同步内部站内信，不经过 OpenAPI 请求和 outbox。
type SystemMessageInput struct {
	RecipientUsername string
	Title             string
	Content           string
	Link              string
}

// SendSystemMessage 按登录用户名找到有效用户，并同步写入消息和用户关联。
func (s *Service) SendSystemMessage(ctx context.Context, in SystemMessageInput) error {
	in.RecipientUsername = strings.TrimSpace(in.RecipientUsername)
	in.Title = strings.TrimSpace(in.Title)
	in.Content = strings.TrimSpace(in.Content)
	if in.RecipientUsername == "" || in.Title == "" || in.Content == "" {
		return ErrInvalidRequest
	}
	if in.Link != "" {
		if err := validateLink(in.Link); err != nil {
			return err
		}
	}

	userID, err := s.messages.FindActiveUserIDByUsername(ctx, in.RecipientUsername)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrRecipientNotFound
	}
	if err != nil {
		return err
	}
	return s.messages.InsertTxWithUserMessages(nil, &repo.NotifyMessage{
		Type: TypeInbox, GroupCode: DefaultGroupCode,
		Title: in.Title, Content: in.Content, Link: in.Link,
		Source: MessageSourceSystem, Mode: ModeLive,
		SenderType: SenderTypeOther,
	}, []int64{userID})
}

// ErrDuplicateKey 幂等唯一索引冲突的内部标记（不外露）。
var ErrDuplicateKey = errors.New("notify: duplicate key")

// StartScheduler 启动定时发送扫描（每分钟触发 ScanScheduledDue）。
// 阻塞运行，调用方以 goroutine 启动；ctx 取消时退出。
func (s *Service) StartScheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.ScanScheduledDue(ctx); err != nil {
				logx.WithContext(ctx).Errorf("notify: scan scheduled due: %v", err)
			}
		}
	}
}

// SendNotification OpenAPI 发送主链路：六步校验顺序不可变（对齐云端 sendNotificationLogic）。
// ①本地校验 → ②限流 → ③幂等预查 → ④收件人校验 → ⑤幂等写事务+outbox → ⑥返回 accepted。
func (s *Service) SendNotification(ctx context.Context, subject contextx.Subject, n *NormalizedRequest) (*NotificationResult, error) {
	// ② 限流：保留 API Key 总量保护，同时按可信 app_id 限制单 App 聚合流量。
	limited, err := s.limiter.Hit(ctx, fmt.Sprintf("key:%d", subject.APIKeyID))
	if err != nil {
		return nil, err
	}
	if limited {
		return nil, ErrRateLimited
	}
	if appScope := appRateLimitScope(subject); appScope != "" {
		limited, err = s.limiter.Hit(ctx, appScope)
		if err != nil {
			return nil, err
		}
		if limited {
			return nil, ErrRateLimited
		}
	}

	// 发信应用身份：SDK 自报的 sender.id 是平台注入容器的 docker app UUID，既不是
	// Launchpad 的应用 id 也不可信（同 appRateLimitScope 的口径）。这里按 API Key
	// 携带的可信 App 归属反查一次，把平台身份和显示名固化进这条请求：
	//   - sender_app_id 让客户端能取应用图标、把应用发来的相对路径落回该应用内；
	//   - sender.name 调用方没填时用应用名补上，避免端上只能显示通用文案。
	// 解析不到（非应用来源、应用已删）保持 0 与原值，按原有兜底展示。
	senderName := n.Sender.Name
	senderAppID := int64(0)
	senderProjectID := int64(0)
	if n.Sender.Type == SenderTypeApp {
		if app, aerr := s.resolveSenderApp(ctx, subject.APIKeyAppID); aerr == nil {
			senderAppID = app.ID
			senderProjectID = app.ProjectID
			if senderName == "" {
				senderName = app.Name
			}
		}
	}

	// 幂等内容摘要（归一化后计算）
	digest, err := ComputeDigest(n)
	if err != nil {
		return nil, err
	}

	// ③ 幂等预查：窗口内命中 → digest 一致返回原行 / 不一致 409
	if existing, ferr := s.reqs.FindActiveByKeyIdempotency(ctx, subject.APIKeyID, n.IdempotencyKey); ferr == nil {
		if time.Since(time.UnixMilli(existing.CreatedTime)) < IdempotencyWindow {
			if existing.RequestDigest != digest {
				return nil, ErrIdempotencyConflict
			}
			return resultFromRequest(existing), nil
		}
	} else if !errors.Is(ferr, gorm.ErrRecordNotFound) {
		return nil, ferr
	}

	// ④ 收件人校验：平台有效用户
	if ok, uerr := s.reqs.ActiveUserExists(ctx, n.RecipientUserID); uerr != nil {
		return nil, uerr
	} else if !ok {
		return nil, ErrRecipientNotFound
	}

	// ⑤ 幂等写事务：FOR UPDATE 复核 + 三情况 + 唯一索引冲突回退；新行同事务落 outbox 任务
	now := time.Now().UnixMilli()
	var created *repo.NotifyMessageRequest
	txErr := s.reqs.Transaction(ctx, func(tx *gorm.DB) error {
		row, ferr := s.reqs.LockByKeyIdempotency(tx, subject.APIKeyID, n.IdempotencyKey)
		switch {
		case ferr == nil:
			// 存在活跃行：窗口内 digest 一致→命中返回；不一致→409；超窗口→退休+插新行
			if time.Since(time.UnixMilli(row.CreatedTime)) < IdempotencyWindow {
				if row.RequestDigest != digest {
					return ErrIdempotencyConflict
				}
				created = row
				return nil
			}
			if uerr := tx.Model(&repo.NotifyMessageRequest{}).
				Where("id = ?", row.ID).
				Update("deleted_time", now).Error; uerr != nil {
				return uerr
			}
		case errors.Is(ferr, gorm.ErrRecordNotFound):
			// 无活跃行，走插入
		default:
			return ferr
		}
		newRow := &repo.NotifyMessageRequest{
			KeyID:           subject.APIKeyID,
			RecipientUID:    n.RecipientUserID,
			Type:            n.Type,
			Mode:            n.Mode,
			Title:           n.Title,
			Content:         n.Content,
			Channels:        n.Channels,
			SenderType:      n.Sender.Type,
			SenderID:        n.Sender.ID,
			SenderName:      senderName,
			SenderDetail:    n.Sender.Detail,
			SenderAppID:     senderAppID,
			SenderProjectID: senderProjectID,
			ActionType:      n.ActionType,
			ActionPath:      n.Link,
			Status:          StatusAccepted,
			RequestDigest:   digest,
			IdempotencyKey:  n.IdempotencyKey,
			CreatedTime:     now,
			UpdatedTime:     now,
		}
		if ierr := s.reqs.InsertTx(tx, newRow); ierr != nil {
			if isDuplicateKeyErr(ierr) {
				// 并发同键冲突：回退按幂等命中处理（由预查/复核兜底语义）
				return ErrDuplicateKey
			}
			return ierr
		}
		if eerr := s.outbox.EnqueueOnTx(tx, OutboxCreateInboxEvent, "notify_request",
			strconv.FormatInt(newRow.ID, 10), CreateInboxPayload{RequestID: newRow.ID}); eerr != nil {
			return eerr
		}
		created = newRow
		return nil
	})
	if txErr != nil {
		if errors.Is(txErr, ErrIdempotencyConflict) {
			return nil, ErrIdempotencyConflict
		}
		if errors.Is(txErr, ErrDuplicateKey) {
			// 并发冲突：回查活跃行，digest 一致才返回幂等结果，不一致按冲突拒绝
			if existing, ferr := s.reqs.FindActiveByKeyIdempotency(ctx, subject.APIKeyID, n.IdempotencyKey); ferr == nil {
				if existing.RequestDigest != digest {
					return nil, ErrIdempotencyConflict
				}
				return resultFromRequest(existing), nil
			}
			return nil, ErrIdempotencyConflict
		}
		return nil, txErr
	}
	return resultFromRequest(created), nil
}

// resolveSenderApp 按 API Key 携带的 docker app UUID 反查 Launchpad 应用。
// projects 未注入（部分测试构造）或查不到时返回错误，调用方按"解析不到"处理。
func (s *Service) resolveSenderApp(ctx context.Context, dockerAppID string) (repo.ProjectApp, error) {
	if s == nil || s.projects == nil {
		return repo.ProjectApp{}, repo.ErrNotFound
	}
	return s.projects.GetProjectAppByDockerAppID(ctx, strings.TrimSpace(dockerAppID))
}

// appRateLimitScope 只接受 API Key 鉴权得到的可信 App 归属。
// SDK 请求里的 sender.id 是展示元数据，不得用于共享限流桶。
func appRateLimitScope(subject contextx.Subject) string {
	appID := strings.TrimSpace(subject.APIKeyAppID)
	if appID == "" {
		return ""
	}
	return "app:" + appID
}

// NotificationResult OpenAPI send/get 响应体。
type NotificationResult struct {
	MessageID   string `json:"messageId"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
	CompletedAt string `json:"completedAt,omitempty"`
	ErrorCode   string `json:"errorCode,omitempty"`
	Message     string `json:"message,omitempty"`
}

func resultFromRequest(row *repo.NotifyMessageRequest) *NotificationResult {
	res := &NotificationResult{
		MessageID: strconv.FormatInt(row.ID, 10),
		Type:      row.Type,
		Status:    row.Status,
		CreatedAt: msToISO(row.CreatedTime),
	}
	if row.CompletedTime > 0 {
		res.CompletedAt = msToISO(row.CompletedTime)
	}
	if row.Status == StatusFailed {
		res.ErrorCode = row.ErrorCode
		res.Message = row.Message
	}
	return res
}

// GetNotification 按 messageId 查询发送状态；归属锚定发起请求的 Key（非本 Key 消息 404）。
func (s *Service) GetNotification(ctx context.Context, keyID int64, messageID string) (*NotificationResult, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(messageID), 10, 64)
	if err != nil || id <= 0 {
		return nil, ErrInvalidRequest
	}
	row, ferr := s.reqs.FindOneByIDAndKey(ctx, id, keyID)
	if ferr != nil {
		if errors.Is(ferr, gorm.ErrRecordNotFound) {
			return nil, ErrMessageNotFound
		}
		return nil, ferr
	}
	return resultFromRequest(row), nil
}

// ---------- outbox 消费：建站内信 ----------

// CreateInboxPayload notify.create_inbox 事件载荷。
type CreateInboxPayload struct {
	RequestID int64 `json:"requestId"`
}

// HandleCreateInbox outbox worker 入口：按请求记录建站内信 + 用户关联，回写终态。
// 幂等守卫：sys_notify_message.notify_request_id 部分唯一索引，重复消费安全。
func (s *Service) HandleCreateInbox(ctx context.Context, payload CreateInboxPayload) error {
	row, err := s.reqs.FindOneByID(ctx, payload.RequestID)
	if err != nil {
		return fmt.Errorf("notify: load request %d: %w", payload.RequestID, err)
	}
	switch row.Status {
	case StatusSent:
		return nil // 已终态，幂等返回
	case StatusAccepted:
	default:
		return nil // failed 终态不重投
	}
	msg := &repo.NotifyMessage{
		Type:            row.Type,
		GroupCode:       DefaultGroupCode,
		Title:           row.Title,
		Content:         row.Content,
		Link:            row.ActionPath,
		Source:          MessageSourceOpenAPI,
		Mode:            row.Mode,
		SenderType:      row.SenderType,
		SenderID:        row.SenderID,
		SenderName:      row.SenderName,
		SenderDetail:    row.SenderDetail,
		SenderAppID:     row.SenderAppID,
		SenderProjectID: row.SenderProjectID,
		NotifyRequestID: row.ID,
	}
	if ierr := s.messages.InsertTxWithUserMessages(nil, msg, []int64{row.RecipientUID}); ierr != nil {
		if isDuplicateKeyErr(ierr) {
			// 消息已建（部分唯一索引拦截），仅补终态
			return s.reqs.MarkSent(ctx, row.ID)
		}
		return fmt.Errorf("notify: create inbox for request %d: %w", row.ID, ierr)
	}
	return s.reqs.MarkSent(ctx, row.ID)
}

// ---------- 管理端公告 ----------

// SaveNotificationInput 创建/编辑草稿入参。
type SaveNotificationInput struct {
	ID      int64
	Type    string
	Title   string
	Content string
	Link    string
	AsModal bool
	// Operator 操作人（管理员）
	Operator int64
}

// SaveNotification 创建或更新草稿（status=1）。
func (s *Service) SaveNotification(ctx context.Context, in SaveNotificationInput) (int64, error) {
	if !validNotificationType(in.Type) {
		return 0, ErrInvalidType
	}
	if in.Title == "" || len([]rune(in.Title)) > TitleMaxLength {
		return 0, ErrContentLimit
	}
	if in.Link != "" {
		if err := validateLink(in.Link); err != nil {
			return 0, err
		}
	}
	if in.ID > 0 {
		err := s.notifies.Transaction(ctx, func(tx *gorm.DB) error {
			row, err := s.notifies.FindOneForUpdate(tx, in.ID)
			if err != nil {
				return err
			}
			if row.Status != NotificationStatusDraft {
				return ErrInvalidRequest
			}
			row.Type, row.Title, row.Content, row.Link, row.UpdatedBy = in.Type, in.Title, in.Content, in.Link, in.Operator
			row.AsModal = boolToInt(in.AsModal)
			return s.notifies.SaveTx(tx, row)
		})
		return in.ID, err
	}
	row := &repo.Notification{
		Type:      in.Type,
		Title:     in.Title,
		Content:   in.Content,
		Link:      in.Link,
		AsModal:   boolToInt(in.AsModal),
		Status:    NotificationStatusDraft,
		CreatedBy: in.Operator,
		UpdatedBy: in.Operator,
	}
	if err := s.notifies.Create(ctx, row); err != nil {
		return 0, err
	}
	return row.ID, nil
}

// SendManualInput 管理端发送入参。
type SendManualInput struct {
	ID           int64
	AudienceType int64
	UserIDs      []int64
	ScheduleTime int64
	Operator     int64
}

// SendManualNotification 将收件箱投递与已发送状态放入同一事务。
// 行锁串行化发送、扫描和撤回；事务中断时保留原状态，避免卡在 Running。
func (s *Service) SendManualNotification(ctx context.Context, in SendManualInput) error {
	return s.notifies.Transaction(ctx, func(tx *gorm.DB) error {
		row, err := s.notifies.FindOneForUpdate(tx, in.ID)
		if err != nil {
			return err
		}
		if row.Status != NotificationStatusDraft && row.Status != NotificationStatusPlanned {
			return ErrInvalidRequest
		}
		// 受众类型白名单：非法值（0/99 等）不得落入"全部用户"分支造成误广播
		if in.AudienceType != AudienceTypeAll && in.AudienceType != AudienceTypeUsers {
			return ErrInvalidRequest
		}
		// 受众解析放锁内：全部用户分批查询期间持有行锁，阻塞第二个发送请求
		userIDs, err := resolveAudienceTx(ctx, tx, in.AudienceType, in.UserIDs)
		if err != nil {
			return err
		}
		row.AudienceType = in.AudienceType
		row.AudienceUserIDs = marshalUserIDs(in.UserIDs)
		row.UpdatedBy = in.Operator
		row.ScheduleTime = in.ScheduleTime
		if in.ScheduleTime > 0 {
			row.Status = NotificationStatusPlanned
			return s.notifies.SaveTx(tx, row)
		}
		return s.deliverManual(tx, row, userIDs)
	})
}

// deliverManual 一篇公告一条站内信 + N 条用户关联（分批插入），完成后置已发送。
func (s *Service) deliverManual(tx *gorm.DB, row *repo.Notification, userIDs []int64) error {
	msg := &repo.NotifyMessage{
		Type:           row.Type,
		GroupCode:      groupCodeForType(row.Type),
		Title:          row.Title,
		Content:        row.Content,
		Link:           row.Link,
		AsModal:        row.AsModal,
		Source:         MessageSourceAdmin,
		NotificationID: row.ID,
	}
	// banner 默认有效期 7 天（管理端未传有效期时的约定默认值），否则永不过期条件的横幅无法展示
	if row.Type == "banner" {
		msg.BannerStart = time.Now().UnixMilli()
		msg.BannerEnd = msg.BannerStart + DefaultBannerValidityMs
	}
	if err := s.messages.InsertTxWithUserMessages(tx, msg, userIDs); err != nil {
		return err
	}
	row.Status = NotificationStatusSent
	row.SendStatistics = fmt.Sprintf(`{"total":%d,"sent":%d}`, len(userIDs), len(userIDs))
	return s.notifies.SaveTx(tx, row)
}

// ScanScheduledDue 定时发送扫描：触发到期已规划公告；由 bootstrap 定时任务每分钟调用。
func (s *Service) ScanScheduledDue(ctx context.Context) error {
	rows, err := s.notifies.ListScheduledDue(ctx, time.Now(), 20)
	if err != nil {
		return err
	}
	var scanErr error
	for _, candidate := range rows {
		err := s.notifies.Transaction(ctx, func(tx *gorm.DB) error {
			row, err := s.notifies.FindOneForUpdate(tx, candidate.ID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			// 扫描结果可能已被另一实例发送、重新规划或撤回，持锁后重新检查。
			if row.Status != NotificationStatusPlanned || row.ScheduleTime <= 0 || row.ScheduleTime > time.Now().UnixMilli() {
				return nil
			}
			userIDs, err := resolveAudienceTx(ctx, tx, row.AudienceType, unmarshalUserIDs(row.AudienceUserIDs))
			if err != nil {
				return err
			}
			return s.deliverManual(tx, row, userIDs)
		})
		if err != nil {
			scanErr = errors.Join(scanErr, fmt.Errorf("notification %d: %w", candidate.ID, err))
		}
	}
	return scanErr
}

// RevokeNotification 撤回：已规划/运行中/已发送 → 已撤回（对齐云端状态机：已发送可撤回），
// 已投递消息整体软删（用户端不可见）。
func (s *Service) RevokeNotification(ctx context.Context, id int64) error {
	return s.notifies.Transaction(ctx, func(tx *gorm.DB) error {
		row, err := s.notifies.FindOneForUpdate(tx, id)
		if err != nil {
			return err
		}
		switch row.Status {
		case NotificationStatusPlanned, NotificationStatusRunning, NotificationStatusSent:
		default:
			return ErrInvalidRequest
		}
		row.Status = NotificationStatusRevoked
		if err := s.notifies.SaveTx(tx, row); err != nil {
			return err
		}
		return repo.NewNotifyMessageRepo(tx).SoftDeleteByNotificationID(ctx, row.ID)
	})
}

// DeleteNotification 删除公告（软删）。
func (s *Service) DeleteNotification(ctx context.Context, id int64) error {
	return s.notifies.Transaction(ctx, func(tx *gorm.DB) error {
		row, err := s.notifies.FindOneForUpdate(tx, id)
		if err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		row.DeletedTime = now
		row.UpdatedTime = now
		return s.notifies.SaveTx(tx, row)
	})
}

// CopyNotification 复制为新草稿。
func (s *Service) CopyNotification(ctx context.Context, id, operator int64) (int64, error) {
	row, err := s.notifies.FindOne(ctx, id)
	if err != nil {
		return 0, err
	}
	copied := &repo.Notification{
		Type:      row.Type,
		Title:     row.Title,
		Content:   row.Content,
		Link:      row.Link,
		AsModal:   row.AsModal,
		Status:    NotificationStatusDraft,
		CreatedBy: operator,
		UpdatedBy: operator,
	}
	if err := s.notifies.Create(ctx, copied); err != nil {
		return 0, err
	}
	return copied.ID, nil
}

// ListNotifications 管理端公告分页列表。
func (s *Service) ListNotifications(ctx context.Context, f repo.NotificationListFilter) ([]repo.Notification, int64, error) {
	return s.notifies.List(ctx, f)
}

// GetNotificationDetail 管理端公告详情。
func (s *Service) GetNotificationDetail(ctx context.Context, id int64) (*repo.Notification, error) {
	return s.notifies.FindOne(ctx, id)
}

// EstimateUsers 受众人数预估（P0 仅全部/指定两种受众）。
func (s *Service) EstimateUsers(ctx context.Context, audienceType int64, userIDs []int64) (int64, error) {
	if audienceType == AudienceTypeUsers {
		seen := map[int64]struct{}{}
		count := int64(0)
		for _, id := range userIDs {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ok, err := s.reqs.ActiveUserExists(ctx, id)
			if err != nil {
				return 0, err
			}
			if ok {
				count++
			}
		}
		return count, nil
	}
	return s.notifies.CountActiveUsers(ctx)
}

// ---------- 用户端消息 ----------

// UserMessageQuery 消息中心列表/批量已读共用查询条件（对齐前端筛选控件）。
type UserMessageQuery struct {
	Group      string
	UnreadOnly bool
	Keyword    string
	// StartTime/EndTime 创建时间范围（毫秒，0=不限）
	StartTime int64
	EndTime   int64
	// SourceType 来源类型：'app'=应用来源；'platform'=平台来源；空=全部
	SourceType string
	// SourceID / SourceIds 来源精确筛选（sourceIds 优先于 SourceID）
	SourceID  string
	SourceIds []string
}

func (q UserMessageQuery) toFilter(userID int64, pageNo, pageSize int) repo.NotifyMessageFilter {
	return repo.NotifyMessageFilter{
		UserID: userID, Group: q.Group, UnreadOnly: q.UnreadOnly,
		Keyword: q.Keyword, StartTime: q.StartTime, EndTime: q.EndTime,
		SenderType: q.SourceType, SenderID: q.SourceID, SenderIDs: q.SourceIds,
		PageNo: pageNo, PageSize: pageSize,
	}
}

// NotificationEntry 消息通知 REST 面（/api/core/notifications*）的条目形态。
type NotificationEntry struct {
	ID              string `json:"id"`
	MessageID       string `json:"messageId,omitempty"`
	SourceID        string `json:"sourceId"`
	SourceType      string `json:"sourceType"`
	SourceName      string `json:"sourceName"`
	Title           string `json:"title"`
	Content         string `json:"content"`
	CreatedAt       string `json:"createdAt"`
	ReadAt          string `json:"readAt,omitempty"`
	TargetAvailable bool   `json:"targetAvailable"`
	TargetPath      string `json:"targetPath,omitempty"`
	// SourceIconURL 发信应用的图标；sourceId 是 SDK 自报的 docker app UUID，
	// 前端拿它反查不到应用，只能退回通用图形，所以这里直接给地址。
	SourceIconURL string `json:"sourceIconUrl,omitempty"`
}

// SenderIconURL 发信应用的图标地址。
//
// 刻意不用 Launchpad 的 /apps/{id}/icon：那条按「能否打开该应用」授权，而收件人
// 未必是该应用所在项目的成员，非成员会拿到 403、只能看到通用图形（线上实测）。
// 这条按「这条消息是我的」授权，linkId 是 sys_user_message.id，天然限定到本人。
// appID<=0（非应用来源或解析不到）时返回空串，前端按"没有图标"兜底。
func SenderIconURL(linkID, appID int64) string {
	if linkID <= 0 || appID <= 0 {
		return ""
	}
	return fmt.Sprintf("/api/core/notify/user-message/%d/sender-icon", linkID)
}

// ToNotificationEntry 用户消息 → 前端 NotificationItem（REST 契约字段映射唯一出处）。
func ToNotificationEntry(item repo.UserMessageItem) NotificationEntry {
	sourceType := item.Source.SourceType
	if sourceType == "" {
		sourceType = "platform"
	}
	sourceID := item.Source.SourceID
	if sourceID == "" {
		sourceID = "platform"
	}
	sourceName := item.Source.SourceName
	if sourceName == "" {
		sourceName = "Platform"
	}
	targetPath := item.Message.Link
	targetAvailable := targetPath != "" && validateLink(targetPath) == nil
	if sourceType == SenderTypeApp {
		var manifest struct {
			Deploying   bool   `json:"deploying"`
			DeployError string `json:"deploy_error"`
		}
		_ = json.Unmarshal(item.Source.AppManifest, &manifest)
		targetAvailable = targetAvailable && item.Source.AppID > 0 && item.Source.ProjectID > 0 &&
			item.Source.AppDeletedTime == 0 && item.Source.ProjectDeletedTime == 0 && item.Source.AppEntry != "" &&
			appstatus.DeployStatus(item.Source.AppStatus, manifest.Deploying, manifest.DeployError) == "success"
		// SDK 的相对 link 是应用自己的路由，不能直接交给平台路由器。
		// 使用可信项目/应用 ID，访问权限仍由 Launchpad 详情及应用网关校验。
		if strings.HasPrefix(targetPath, "/") && validateLink(targetPath) == nil && item.Source.AppID > 0 && item.Source.ProjectID > 0 {
			targetPath = fmt.Sprintf("/launchpad/%d/%d?notificationPath=%s",
				item.Source.ProjectID, item.Source.AppID, url.QueryEscape(targetPath))
		}
	}
	// readAt 语义：未读必须留空（前端以 !readAt 判未读）；已读但缺时间戳（历史/handle 流程）回退创建时间
	var readAt int64
	if item.Link.IsRead != 0 {
		readAt = item.Link.ReadTime
		if readAt <= 0 {
			readAt = item.Message.CreatedTime
		}
	}
	return NotificationEntry{
		ID:              strconv.FormatInt(item.Link.ID, 10),
		MessageID:       strconv.FormatInt(item.Message.ID, 10),
		SourceID:        sourceID,
		SourceType:      sourceType,
		SourceName:      sourceName,
		Title:           item.Message.Title,
		Content:         item.Message.Content,
		CreatedAt:       msToISO(item.Message.CreatedTime),
		ReadAt:          msToISO(readAt),
		TargetAvailable: targetAvailable,
		TargetPath:      targetPath,
		// 用 Source.AppID 而不是 Message.SenderAppID：这条读出查询按 API Key 与应用
		// 元数据现算来源（见 repo 的 source_app_id 投影），存量消息一样能解析出应用；
		// 而 sender_app_id 是建信时固化的快照，压根不在这条查询的投影里。
		SourceIconURL: SenderIconURL(item.Link.ID, item.Source.AppID),
	}
}

// NotificationSources 聚合来源（REST 面）。
func (s *Service) NotificationSources(ctx context.Context, userID int64) ([]repo.NotificationSourceItem, error) {
	return s.messages.AggregateUserSources(ctx, userID)
}

// ReadNotificationsRead 按用户消息关联 id 批量已读（REST 面，ids 为 string 承载）。
func (s *Service) MarkNotificationsRead(ctx context.Context, userID int64, ids []string) (int64, error) {
	converted := make([]int64, 0, len(ids))
	for _, v := range ids {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n > 0 {
			converted = append(converted, n)
		}
	}
	if len(converted) == 0 {
		return 0, nil
	}
	if err := s.messages.MarkRead(ctx, userID, converted); err != nil {
		return 0, err
	}
	return int64(len(converted)), nil
}

// DeleteNotifications 软删本人消息关联（REST 面）。
func (s *Service) DeleteNotifications(ctx context.Context, userID int64, ids []string) error {
	converted := make([]int64, 0, len(ids))
	for _, v := range ids {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n > 0 {
			converted = append(converted, n)
		}
	}
	return s.messages.SoftDeleteByIDs(ctx, userID, converted)
}

// ListUserMessages 用户消息列表（两步查询：关联行 + 消息本体，支持筛选）。
func (s *Service) ListUserMessages(ctx context.Context, userID int64, q UserMessageQuery, pageNo, pageSize int) ([]repo.UserMessageItem, int64, error) {
	return s.messages.ListByUser(ctx, q.toFilter(userID, pageNo, pageSize))
}

// ListMessageHistory 返回管理端统一投递历史及完整来源列表。
func (s *Service) ListMessageHistory(ctx context.Context, filter repo.MessageHistoryFilter) ([]repo.MessageHistoryItem, int64, []repo.NotificationSourceItem, error) {
	items, total, err := s.messages.ListHistory(ctx, filter)
	if err != nil {
		return nil, 0, nil, err
	}
	sources, err := s.messages.AggregateHistorySources(ctx)
	if err != nil {
		return nil, 0, nil, err
	}
	return items, total, sources, nil
}

// NotificationRecipientOptions 返回通知管理页可选的最小用户与用户组信息。
func (s *Service) NotificationRecipientOptions(ctx context.Context) ([]repo.NotificationRecipientUser, []repo.NotificationRecipientGroup, error) {
	return s.messages.ListNotificationRecipientOptions(ctx)
}

// GetUserMessageDetail 单条用户消息详情（本人）；不存在返回 ErrMessageNotFound。
func (s *Service) GetUserMessageDetail(ctx context.Context, userID, linkID int64) (*repo.UserMessageItem, error) {
	item, err := s.messages.FindOneByUser(ctx, userID, linkID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMessageNotFound
		}
		return nil, err
	}
	return item, nil
}

// ReadAllByFilter 按筛选条件全部已读；返回影响条数（前端提示"已读 N 条"）。
func (s *Service) ReadAllByFilter(ctx context.Context, userID int64, q UserMessageQuery) (int64, error) {
	return s.messages.MarkAllReadByFilter(ctx, userID, q.toFilter(userID, 1, 1))
}

// UnreadCount 未读统计。
func (s *Service) UnreadCount(ctx context.Context, userID int64) (int64, error) {
	return s.messages.CountUnread(ctx, userID)
}

// ReadMessages 标记已读；ids 为空时按旧语义全量已读（保留兼容，新前端走 ReadAllByFilter）。
func (s *Service) ReadMessages(ctx context.Context, userID int64, ids []int64) error {
	return s.messages.MarkRead(ctx, userID, ids)
}

// SenderIconAssetID 取这条消息发信应用的图标资产 id。
//
// 授权口径是「这条消息是我的」，而不是「我能打开这个应用」：收件人未必是发信
// 应用所在项目的成员，但消息里本来就展示着应用名，图标不比名字多透露什么。
// 走 Launchpad 那条按 App 取图的端点会被 requireAppVisible 挡成 403，非成员
// 只能看到破图或通用图形（线上实测如此），所以这里单开一条。
func (s *Service) SenderIconAssetID(ctx context.Context, userID, linkID int64) (int64, error) {
	if userID <= 0 || linkID <= 0 {
		return 0, ErrInvalidRequest
	}
	item, err := s.GetUserMessageDetail(ctx, userID, linkID)
	if err != nil {
		return 0, err
	}
	appID := item.Message.SenderAppID
	if appID <= 0 {
		appID = item.Source.AppID
	}
	if appID <= 0 || s.projects == nil {
		return 0, ErrMessageNotFound
	}
	app, err := s.projects.GetProjectAppByID(ctx, appID)
	if err != nil || app.IconAssetID <= 0 {
		return 0, ErrMessageNotFound
	}
	return app.IconAssetID, nil
}

// HandleMessage 标记已处理（Modal/Banner 关闭）。
func (s *Service) HandleMessage(ctx context.Context, userID, linkID int64) error {
	return s.messages.MarkHandled(ctx, userID, linkID)
}

// DeleteMessages 删除本人消息（软删关联行）。
func (s *Service) DeleteMessages(ctx context.Context, userID int64, ids []int64) error {
	if len(ids) == 0 {
		return ErrInvalidRequest
	}
	return s.messages.SoftDeleteByIDs(ctx, userID, ids)
}

// ListBanners 当前有效横幅。
func (s *Service) ListBanners(ctx context.Context) ([]repo.NotifyMessage, error) {
	return s.messages.ListActiveBanners(ctx, time.Now())
}

// ListPrompts 当前用户待展示提醒：有效横幅 + 未处理强提醒。
func (s *Service) ListPrompts(ctx context.Context, userID int64) ([]repo.PromptItem, error) {
	return s.messages.ListPendingPrompts(ctx, userID, time.Now())
}

// HandlePrompt 标记提醒已处理（横幅关闭/强提醒弹窗关闭）；linkId 为用户消息关联 id。
func (s *Service) HandlePrompt(ctx context.Context, userID, linkID int64) error {
	return s.messages.MarkHandled(ctx, userID, linkID)
}

// ---------- 内部工具 ----------

// resolveAudience 受众解析：全部=平台有效用户全量；指定=去重并过滤无效用户（全无效才报错）。
// 持锁期间复用事务连接，避免并发发送耗尽连接池后互相等待。
func resolveAudienceTx(ctx context.Context, tx *gorm.DB, audienceType int64, userIDs []int64) ([]int64, error) {
	service := &Service{reqs: repo.NewNotifyMessageRequestRepo(tx), notifies: repo.NewNotificationRepo(tx)}
	return service.resolveAudience(ctx, audienceType, userIDs)
}

func (s *Service) resolveAudience(ctx context.Context, audienceType int64, userIDs []int64) ([]int64, error) {
	if audienceType == AudienceTypeUsers {
		seen := map[int64]struct{}{}
		out := make([]int64, 0, len(userIDs))
		for _, id := range userIDs {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			ok, err := s.reqs.ActiveUserExists(ctx, id)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
		if len(out) == 0 {
			return nil, ErrRecipientNotFound
		}
		return out, nil
	}
	// 全部用户：分批拉全量
	var out []int64
	offset := 0
	for {
		ids, err := s.notifies.ListActiveUserIDs(ctx, offset, ManualSendBatchSize)
		if err != nil {
			return nil, err
		}
		out = append(out, ids...)
		if len(ids) < ManualSendBatchSize {
			return out, nil
		}
		offset += ManualSendBatchSize
	}
}

func validNotificationType(t string) bool {
	return t == TypeInbox || t == "banner" || t == "news"
}

func groupCodeForType(t string) string {
	if t == "banner" {
		return "system"
	}
	return DefaultGroupCode
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func marshalUserIDs(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return ""
	}
	return string(data)
}

func unmarshalUserIDs(s string) []int64 {
	if s == "" {
		return nil
	}
	var ids []int64
	_ = json.Unmarshal([]byte(s), &ids)
	return ids
}

func msToISO(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05Z")
}

// isDuplicateKeyErr 唯一索引冲突判断（PostgreSQL SQLSTATE 23505）。
func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "23505") ||
		strings.Contains(strings.ToLower(err.Error()), "duplicate key")
}
