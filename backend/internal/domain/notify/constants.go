// Package notify 企业版消息通知领域（TASK-030）。
//
// 语义基线为云端 syssvr 的 notifymanage（运营通知）与 notification（OpenAPI 通用消息平台）。
// OpenAPI 端点/字段/鉴权/幂等/限流/错误码与云端兼容；企业内网额外支持 HTTP 通知链接，
// 已接入云端的应用仅改 base URL 即可在企业版使用；内部实现按企业版单体架构落地。
//
// 主要文件：constants（契约常量）、errs（错误域）、validate（请求校验）、digest（幂等摘要）、
// ratelimit（限流）、openapiresp（OpenAPI HTTP 错误契约层）、service（六步校验链与建信投递）。
package notify

import "time"

// 请求状态机（sys_notify_message_request.status）：与云端一致，无 processing 中间态。
const (
	StatusAccepted = "accepted"
	StatusSent     = "sent"
	StatusFailed   = "failed"
)

// 消息类型（sys_notify_message_request.type）：本期仅 inbox，其余值 400。
const (
	TypeInbox = "inbox"
)

// 运行模式：test 自动加标题前缀且不触发外部渠道，live 为正式投递。
const (
	ModeTest = "test"
	ModeLive = "live"
)

// TestTitlePrefix test 模式标题前缀，不计入长度上限（对齐云端）。
const TestTitlePrefix = "[Test]"

// 渠道与发送方契约常量（对齐云端 domain/notification/constants.go）。
const (
	ChannelWeb    = "web"
	ChannelMobile = "mobile"

	SenderTypeApp   = "app"
	SenderTypeOther = "other"

	// ChannelsNormalized 归一化落库形态：固定序 web,mobile；空串=静默消息
	ChannelsNormalized = "web,mobile"
)

// OpenAPI 请求字段长度与语义上限（对齐云端 validate.go + domain/notification/constants.go）。
const (
	TitleMaxLength          = 50
	ContentMaxLength        = 800
	IdempotencyKeyMaxLength = 128
	SourceMaxLength         = 100
	SenderIDMaxLength       = 128
	SenderNameMaxLength     = 100
	SenderDetailMaxBytes    = 500
	LinkMaxLength           = 500

	// ActionTypeLink 外部消息 Open 跳转的动作类型（本期唯一取值）
	ActionTypeLink = "link"

	// 企业内网应用可能仅提供 HTTP；通知链接只用于用户导航，不由后端发起请求。
	LinkSchemePrefix = "https://"
	LinkHTTPPrefix   = "http://"
)

// 行为参数（对齐云端魔法值常量化清单）。
const (
	// RateLimitPerMinute 每 key_id 每分钟请求上限
	RateLimitPerMinute = 1000

	// rateLimitWindowExpireSeconds 固定窗口过期秒数（覆盖 60s 窗口的边界冗余）
	rateLimitWindowExpireSeconds = 65

	// IdempotencyWindow 幂等窗口：同 key 复用的有效时长
	IdempotencyWindow = 24 * time.Hour

	// RetentionDays 请求记录/审计保留天数
	RetentionDays = 90

	// OutboxCreateInboxEvent 建站内信的 outbox 事件类型
	OutboxCreateInboxEvent = "notify.create_inbox"

	// 消息来源线（sys_notify_message.source）
	MessageSourceOpenAPI = "openapi"
	MessageSourceAdmin   = "admin"
	MessageSourceSystem  = "system"

	// DefaultGroupCode 消息默认分组（用户端 tab）
	DefaultGroupCode = "system"

	// Notification 管理端公告状态机：1草稿 2已规划 3运行中 4已发送 5已撤回
	NotificationStatusDraft   int64 = 1
	NotificationStatusPlanned int64 = 2
	NotificationStatusRunning int64 = 3
	NotificationStatusSent    int64 = 4
	NotificationStatusRevoked int64 = 5

	// AudienceTypeAll / AudienceTypeUsers 管理端受众类型
	AudienceTypeAll   int64 = 1
	AudienceTypeUsers int64 = 2

	// ManualSendBatchSize 管理端全部用户发送的分批大小
	ManualSendBatchSize = 1000

	// DefaultBannerValidityMs 管理端横幅默认有效期：7 天
	DefaultBannerValidityMs int64 = 7 * 24 * 3600 * 1000

	// APIKey 资源键（OpenAPI 通知能力）。键名用企业版点号惯例（notifications.send/read），
	// 与 IAM seed 资源树及 APIKeyResourceKeysForPermission 档位映射一致；
	// 语义对齐云端的 notifications:send/read 权限位。
	ResourceKeySend = "notifications.send"
	ResourceKeyRead = "notifications.read"
)
