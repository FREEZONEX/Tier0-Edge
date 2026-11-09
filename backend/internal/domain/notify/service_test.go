package notify

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestService 组装独立库 + 通知服务的测试环境，并插入两个平台用户。
func newTestService(t *testing.T) (*Service, *repo.Store, contextx.Subject, contextx.Subject) {
	t.Helper()
	store, _ := testkit.NewTestDB(t)
	svc := New(store)
	insertUser(t, store, 1001)
	insertUser(t, store, 1002)
	return svc, store, contextx.Subject{UserID: 1001, APIKeyID: 7}, contextx.Subject{UserID: 1002, APIKeyID: 8}
}

// parseID messageId 字符串转 int64。
func parseID(t *testing.T, id string) int64 {
	t.Helper()
	var v int64
	_, err := fmt.Sscan(id, &v)
	require.NoError(t, err)
	return v
}

func insertUser(t *testing.T, store *repo.Store, userID int64) {
	t.Helper()
	err := repo.GetCommonConn(store).Exec(
		`INSERT INTO sys_user_info (user_id, user_name, nick_name, password, status)
		 VALUES (?, ?, ?, 'x', 1) ON CONFLICT DO NOTHING`, userID, "user"+string(rune('0'+userID%10)), "u").Error
	require.NoError(t, err)
}

func sendReq(recipient, title, content, idem string) *NormalizedRequest {
	n, err := ValidateAndNormalize(recipient, TypeInbox, title, content, idem, "", ModeLive, "", nil, "", "", "", nil)
	if err != nil {
		panic(err)
	}
	return n
}

// TestSendNotification_FullChain 发送 → outbox 建信 → 查询 sent → 用户消息可见。
func TestSendNotification_FullChain(t *testing.T) {
	svc, _, key, _ := newTestService(t)
	ctx := context.Background()

	res, err := svc.SendNotification(ctx, key, sendReq("1001", "hello", "world", "k1"))
	require.NoError(t, err)
	assert.Equal(t, StatusAccepted, res.Status)
	assert.NotEmpty(t, res.MessageID)

	// 未消费前 get 仍为 accepted
	got, err := svc.GetNotification(ctx, key.APIKeyID, res.MessageID)
	require.NoError(t, err)
	assert.Equal(t, StatusAccepted, got.Status)

	// 模拟 outbox worker 消费建信
	var reqID int64 = parseID(t, res.MessageID)
	require.NoError(t, svc.HandleCreateInbox(ctx, CreateInboxPayload{RequestID: reqID}))

	got, err = svc.GetNotification(ctx, key.APIKeyID, res.MessageID)
	require.NoError(t, err)
	assert.Equal(t, StatusSent, got.Status)
	assert.NotEmpty(t, got.CompletedAt)

	// 用户消息可见 + 未读 1
	items, total, err := svc.ListUserMessages(ctx, 1001, UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "hello", items[0].Message.Title)
	unread, err := svc.UnreadCount(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, int64(1), unread)

	// 已读后未读归零
	require.NoError(t, svc.ReadMessages(ctx, 1001, nil))
	unread, err = svc.UnreadCount(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, int64(0), unread)
}

func TestSendSystemMessage_SynchronousStationDelivery(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()

	err := svc.SendSystemMessage(ctx, SystemMessageInput{
		RecipientUsername: "tier0",
		Title:             "数据质量异常：温度 · 数值越界",
		Content:           "字段：temperature\n检测值：101.5\n允许范围：0 ～ 100",
		Link:              "/uns?namespaceId=42",
	})
	require.NoError(t, err)

	items, total, err := svc.ListUserMessages(ctx, 1, UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "数据质量异常：温度 · 数值越界", items[0].Message.Title)
	require.Equal(t, "/uns?namespaceId=42", items[0].Message.Link)
	require.Equal(t, MessageSourceSystem, items[0].Message.Source)
	require.Empty(t, items[0].Message.SenderID)
	require.Zero(t, items[0].Message.NotifyRequestID)

	var requestCount int64
	require.NoError(t, repo.GetCommonConn(store).Table("sys_notify_message_request").Count(&requestCount).Error)
	require.Zero(t, requestCount)

	require.NoError(t, repo.GetCommonConn(store).Exec(
		"UPDATE sys_user_info SET status = 2 WHERE user_name = ?", "tier0").Error)
	err = svc.SendSystemMessage(ctx, SystemMessageInput{
		RecipientUsername: "tier0", Title: "再次异常", Content: "内容",
	})
	require.True(t, IsCode(err, 3005020), "disabled recipient should be rejected, got %v", err)
}

// TestListUserMessages_Filters 消息中心筛选：关键词/时间范围/来源类型。
func TestListUserMessages_Filters(t *testing.T) {
	svc, store, _, _ := newTestServiceWithSeed(t)
	ctx := context.Background()
	db := repo.GetCommonConn(store)
	require.NoError(t, db.Exec(`INSERT INTO sys_api_key (id, name, key_hash, owner_type, app_id) VALUES
		(7, 'App key', 'test-app-key-hash', 'app', 'runtime-app'),
		(8, 'Personal key', 'test-personal-key-hash', 'personal', '')`).Error)
	require.NoError(t, db.Create(&repo.Project{ID: 101, ProjectKey: "notify-project", Name: "Project", Status: "active"}).Error)
	require.NoError(t, db.Create(&repo.ProjectApp{
		ID: 201, ProjectID: 101, AppKey: "notify-app", Name: "项目通知台", Status: "active",
		EntryURL: "http://app.internal/", ManifestJSON: []byte(`{"app_id":"runtime-app"}`),
	}).Error)

	// 来源必须由 API Key 决定：App 不传名称，个人 Key 冒用 App 元数据也不能变成 App 来源。
	mkMsg := func(title string, keyID int64) {
		req := sendReq("1001", title, "c-"+title, "kf-"+title)
		req.Link = "/records"
		req.Sender = Sender{Type: "app", ID: "untrusted-app"}
		res, err := svc.SendNotification(ctx, contextx.Subject{UserID: 1001, APIKeyID: keyID}, req)
		require.NoError(t, err)
		require.NoError(t, svc.HandleCreateInbox(ctx, CreateInboxPayload{RequestID: parseID(t, res.MessageID)}))
	}
	mkMsg("Alpha", 7)
	mkMsg("Beta", 8)

	// 关键词筛选
	items, total, err := svc.ListUserMessages(ctx, 1001, UserMessageQuery{Keyword: "Alpha"}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "Alpha", items[0].Message.Title)
	entry := ToNotificationEntry(items[0])
	assert.Equal(t, "项目通知台", entry.SourceName)
	assert.Equal(t, "runtime-app", entry.SourceID)
	assert.Equal(t, "/launchpad/101/201?notificationPath=%2Frecords", entry.TargetPath)
	assert.True(t, entry.TargetAvailable)
	detail, err := svc.GetUserMessageDetail(ctx, 1001, items[0].Link.ID)
	require.NoError(t, err)
	assert.Equal(t, entry, ToNotificationEntry(*detail))
	_, err = svc.GetUserMessageDetail(ctx, 1002, items[0].Link.ID)
	require.Error(t, err)
	sources, err := svc.NotificationSources(ctx, 1001)
	require.NoError(t, err)
	assert.Len(t, sources, 2)
	history, historyTotal, _, err := svc.ListMessageHistory(ctx, repo.MessageHistoryFilter{SourceID: "runtime-app"})
	require.NoError(t, err)
	require.Equal(t, int64(1), historyTotal)
	assert.Equal(t, entry.SourceName, history[0].SourceName)

	// 来源类型：app 只命中 Alpha
	_, totalApp, err := svc.ListUserMessages(ctx, 1001, UserMessageQuery{SourceType: "app"}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalApp)
	platformItems, totalPlatform, err := svc.ListUserMessages(ctx, 1001, UserMessageQuery{SourceType: "platform"}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalPlatform)
	assert.Equal(t, "platform", ToNotificationEntry(platformItems[0]).SourceID)
	_, sourceTotal, err := svc.ListUserMessages(ctx, 1001, UserMessageQuery{SourceIds: []string{"runtime-app"}}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), sourceTotal)
	_, unreadAppTotal, err := svc.ListUserMessages(ctx, 1001, UserMessageQuery{SourceType: "app", UnreadOnly: true}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), unreadAppTotal)

	// 时间范围：未来窗口命中 0 条
	future := UserMessageQuery{StartTime: time.Now().Add(time.Hour).UnixMilli()}
	_, totalFuture, err := svc.ListUserMessages(ctx, 1001, future, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(0), totalFuture)

	// 按筛选全部已读：只影响 Alpha，Beta 保持未读
	affected, err := svc.ReadAllByFilter(ctx, 1001, UserMessageQuery{SourceID: "runtime-app"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)
	unread, err := svc.UnreadCount(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, int64(1), unread)
	_, unreadAppTotal, err = svc.ListUserMessages(ctx, 1001, UserMessageQuery{SourceType: "app", UnreadOnly: true}, 1, 20)
	require.NoError(t, err)
	assert.Zero(t, unreadAppTotal)
	affected, err = svc.ReadAllByFilter(ctx, 1001, UserMessageQuery{Keyword: "Alpha"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), affected) // Alpha 已读过

	// 历史消息也读取当前可信名称；删除或停用后保留来源，但不再提供可用跳转。
	require.NoError(t, db.Model(&repo.ProjectApp{}).Where("id = ?", 201).Updates(map[string]any{"name": "Renamed App", "status": "stopped"}).Error)
	detail, err = svc.GetUserMessageDetail(ctx, 1001, items[0].Link.ID)
	require.NoError(t, err)
	assert.Equal(t, "Renamed App", ToNotificationEntry(*detail).SourceName)
	assert.False(t, ToNotificationEntry(*detail).TargetAvailable)
}

// 新旧应用凭证共存时使用相同来源；旧 Key 无需重新签发，类似命名的普通 Key 不得冒充应用。
func TestListUserMessages_LegacyAppKeys(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()
	db := repo.GetCommonConn(store)
	require.NoError(t, db.Create(&repo.Project{ID: 101, ProjectKey: "legacy-notify", Name: "Project", Status: "active"}).Error)
	for i, runtimeID := range []string{"runtime-app", "other-app"} {
		require.NoError(t, db.Create(&repo.ProjectApp{
			ID: int64(201 + i), ProjectID: 101, AppKey: runtimeID, Name: "App " + runtimeID, Status: "active",
			EntryURL: "http://app.internal/", ManifestJSON: []byte(fmt.Sprintf(`{"app_id":%q}`, runtimeID)),
		}).Error)
	}
	cases := []struct {
		id                                                      int64
		name, ownerType, usageType, appID, sourceType, sourceID string
		projectAppID                                            int64
	}{
		{41, "New app key", "app", "internal", "runtime-app", "app", "runtime-app", 201},
		{42, "app-runtime-app-default", "service", "internal", "", "app", "runtime-app", 201},
		{43, "app-missing-default", "service", "internal", "", "app", "api-key:43", 0},
		{44, "app-runtime-app-default", "personal", "internal", "", "platform", "platform", 0},
		{45, "app-runtime-app-default", "service", "external", "", "platform", "platform", 0},
		{46, "runtime-app-default", "service", "internal", "", "platform", "platform", 0},
		// 结构化归属优先，不能因历史名称指向另一应用而误绑定。
		{47, "app-runtime-app-default", "service", "internal", "other-app", "app", "other-app", 202},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.id), func(t *testing.T) {
			// 使用不同 owner_id 避免注册名唯一约束，与归属解析无关。
			require.NoError(t, db.Exec(`INSERT INTO sys_api_key (id, name, key_hash, owner_id, owner_type, usage_type, app_id)
				VALUES (?, ?, ?, ?, ?, ?, ?)`, tc.id, tc.name, fmt.Sprintf("test-key-%d", tc.id), tc.id, tc.ownerType, tc.usageType, tc.appID).Error)
			req := sendReq("1001", fmt.Sprintf("key-%d", tc.id), "content", fmt.Sprintf("legacy-%d", tc.id))
			req.Link = "/records?tab=all#latest"
			req.Sender = Sender{Type: "app", ID: "spoofed", Name: "Untrusted App"}
			res, err := svc.SendNotification(ctx, contextx.Subject{UserID: 1001, APIKeyID: tc.id}, req)
			require.NoError(t, err)
			require.NoError(t, svc.HandleCreateInbox(ctx, CreateInboxPayload{RequestID: parseID(t, res.MessageID)}))
			items, total, err := svc.ListUserMessages(ctx, 1001, UserMessageQuery{Keyword: req.Title}, 1, 20)
			require.NoError(t, err)
			require.Equal(t, int64(1), total)
			require.Len(t, items, 1)
			entry := ToNotificationEntry(items[0])
			assert.Equal(t, tc.sourceType, entry.SourceType)
			assert.Equal(t, tc.sourceID, entry.SourceID)
			if tc.projectAppID > 0 {
				assert.Equal(t, "App "+tc.sourceID, entry.SourceName)
				assert.Equal(t, fmt.Sprintf("/launchpad/101/%d?notificationPath=%s", tc.projectAppID, url.QueryEscape(req.Link)), entry.TargetPath)
				assert.True(t, entry.TargetAvailable)
			} else if tc.sourceType == "app" {
				assert.False(t, entry.TargetAvailable)
			} else {
				assert.Equal(t, "Platform", entry.SourceName)
				assert.Equal(t, req.Link, entry.TargetPath)
			}
			detail, err := svc.GetUserMessageDetail(ctx, 1001, items[0].Link.ID)
			require.NoError(t, err)
			assert.Equal(t, entry, ToNotificationEntry(*detail))
		})
	}
	sources, err := svc.NotificationSources(ctx, 1001)
	require.NoError(t, err)
	assert.Len(t, sources, 4) // 同一应用的新旧 Key 聚合为一个来源。
	history, total, historySources, err := svc.ListMessageHistory(ctx, repo.MessageHistoryFilter{SourceID: "runtime-app"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.ElementsMatch(t, sources, historySources)
	for _, item := range history {
		assert.Equal(t, "App runtime-app", item.SourceName)
	}
	_, total, err = svc.ListUserMessages(ctx, 1001, UserMessageQuery{SourceIds: []string{"runtime-app"}}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	_, total, err = svc.ListUserMessages(ctx, 1001, UserMessageQuery{SourceType: "app", UnreadOnly: true}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)
	affected, err := svc.ReadAllByFilter(ctx, 1001, UserMessageQuery{SourceID: "runtime-app"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), affected)
	_, total, err = svc.ListUserMessages(ctx, 1001, UserMessageQuery{SourceType: "app", UnreadOnly: true}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	var legacyAppID string
	require.NoError(t, db.Table("sys_api_key").Where("id = ?", 42).Select("app_id").Scan(&legacyAppID).Error)
	assert.Empty(t, legacyAppID, "读取历史消息不得回填或轮换旧 Key")
}

func TestToNotificationEntry_AppTargets(t *testing.T) {
	for _, tc := range []struct {
		name      string
		link      string
		mutate    func(*repo.NotificationMessageSource)
		available bool
	}{
		{name: "app route", link: "/records", available: true},
		{name: "query and hash", link: "/records?project=一&tab=all#latest", available: true},
		{name: "app root", link: "/", available: true},
		{name: "intranet http", link: "http://app.internal/records", available: true},
		{name: "https", link: "https://app.example/records", available: true},
		{name: "no link"},
		{name: "unsafe scheme", link: "javascript:alert(1)"},
		{name: "protocol relative", link: "//evil.example/records"},
		{name: "stopped", link: "/records", mutate: func(s *repo.NotificationMessageSource) { s.AppStatus = "stopped" }},
		{name: "deploying", link: "/records", mutate: func(s *repo.NotificationMessageSource) { s.AppManifest = []byte(`{"deploying":true}`) }},
		{name: "failed deployment", link: "/records", mutate: func(s *repo.NotificationMessageSource) { s.AppManifest = []byte(`{"deploy_error":"failed"}`) }},
		{name: "deleted app", link: "/records", mutate: func(s *repo.NotificationMessageSource) { s.AppDeletedTime = 1 }},
		{name: "deleted project", link: "/records", mutate: func(s *repo.NotificationMessageSource) { s.ProjectDeletedTime = 1 }},
		{name: "unresolved app", link: "/records", mutate: func(s *repo.NotificationMessageSource) { s.AppID = 0 }},
		{name: "missing entry", link: "/records", mutate: func(s *repo.NotificationMessageSource) { s.AppEntry = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := repo.NotificationMessageSource{SourceType: "app", SourceID: "runtime-app", SourceName: "Trusted App", ProjectID: 101, AppID: 201, AppStatus: "active", AppEntry: "http://app.internal/"}
			if tc.mutate != nil {
				tc.mutate(&source)
			}
			entry := ToNotificationEntry(repo.UserMessageItem{Source: source, Message: repo.NotifyMessage{Link: tc.link, SenderName: "spoofed"}})
			assert.Equal(t, "Trusted App", entry.SourceName)
			assert.Equal(t, tc.available, entry.TargetAvailable)
			if tc.available && tc.link[0] == '/' {
				parsed, err := url.Parse(entry.TargetPath)
				require.NoError(t, err)
				assert.Equal(t, "/launchpad/101/201", parsed.Path)
				assert.Equal(t, tc.link, parsed.Query().Get("notificationPath"))
			} else if tc.link == "" || tc.available {
				assert.Equal(t, tc.link, entry.TargetPath)
			}
		})
	}
}

// newTestServiceWithSeed 与 newTestService 相同（别名便于语义化调用）。
func newTestServiceWithSeed(t *testing.T) (*Service, *repo.Store, contextx.Subject, contextx.Subject) {
	return newTestService(t)
}

// TestSendNotification_Idempotent 三情况：命中返回原行 / 同键不同体 409 / 超窗口退休+新行。
func TestSendNotification_Idempotent(t *testing.T) {
	svc, store, key, _ := newTestService(t)
	ctx := context.Background()

	first, err := svc.SendNotification(ctx, key, sendReq("1001", "t", "c", "k1"))
	require.NoError(t, err)

	// 情况二：窗口内同键同内容 → 原 messageId
	again, err := svc.SendNotification(ctx, key, sendReq("1001", "t", "c", "k1"))
	require.NoError(t, err)
	assert.Equal(t, first.MessageID, again.MessageID)

	// 同键不同内容 → 409
	_, err = svc.SendNotification(ctx, key, sendReq("1001", "t", "CHANGED", "k1"))
	require.True(t, IsCode(err, 3005031), "expect conflict, got %v", err)

	// 情况三：超 24h 窗口 → 旧行退休 + 新行（新 messageId）
	old := parseID(t, first.MessageID)
	require.NoError(t, repo.GetCommonConn(store).Exec(
		`UPDATE sys_notify_message_request SET created_time = ? WHERE id = ?`,
		time.Now().Add(-25*time.Hour).UnixMilli(), old).Error)
	renewed, err := svc.SendNotification(ctx, key, sendReq("1001", "t", "c", "k1"))
	require.NoError(t, err)
	assert.NotEqual(t, first.MessageID, renewed.MessageID)
}

// TestSendNotification_RecipientValidation 收件人校验：不存在 404；他 Key 的消息查询 404。
func TestSendNotification_RecipientValidation(t *testing.T) {
	svc, _, key, key2 := newTestService(t)
	ctx := context.Background()

	_, err := svc.SendNotification(ctx, key, sendReq("9999", "t", "c", "k9"))
	require.True(t, IsCode(err, 3005020), "expect recipient not available, got %v", err)

	res, err := svc.SendNotification(ctx, key, sendReq("1001", "t", "c", "k1"))
	require.NoError(t, err)
	_, err = svc.GetNotification(ctx, key2.APIKeyID, res.MessageID)
	require.True(t, IsCode(err, 3005040), "other key should get 404, got %v", err)
}

// TestSendNotification_RateLimited 限流命中返回 429（缩阈值用独立 service）。
func TestSendNotification_RateLimited(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	insertUser(t, store, 1001)
	svc := New(store)
	svc.limiter = &stubLimiter{limited: true}
	_, err := svc.SendNotification(context.Background(),
		contextx.Subject{APIKeyID: 7}, sendReq("1001", "t", "c", "k1"))
	require.True(t, IsCode(err, 3005030), "expect rate limited, got %v", err)
}

type stubLimiter struct{ limited bool }

func (s *stubLimiter) Hit(context.Context, string) (bool, error) { return s.limited, nil }

// TestManualNotificationFlow 管理端：草稿 → 发送指定用户 → 用户可见 → 撤回 → 不可见。
func TestManualNotificationFlow(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()

	id, err := svc.SaveNotification(ctx, SaveNotificationInput{
		Type: TypeInbox, Title: "公告", Content: "内容", Operator: 1,
	})
	require.NoError(t, err)

	// 受众预估：指定 2 人
	count, err := svc.EstimateUsers(ctx, AudienceTypeUsers, []int64{1001, 1002, 9999})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	require.NoError(t, svc.SendManualNotification(ctx, SendManualInput{
		ID: id, AudienceType: AudienceTypeUsers, UserIDs: []int64{1001, 1002, 9999}, Operator: 1,
	}))

	row, err := svc.GetNotificationDetail(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, NotificationStatusSent, row.Status)

	// 两个用户各一条，未读各 1
	for _, uid := range []int64{1001, 1002} {
		items, total, err := svc.ListUserMessages(ctx, uid, UserMessageQuery{}, 1, 20)
		require.NoError(t, err)
		assert.Equal(t, int64(1), total)
		assert.Equal(t, "公告", items[0].Message.Title)
		unread, err := svc.UnreadCount(ctx, uid)
		require.NoError(t, err)
		assert.Equal(t, int64(1), unread)
	}

	// 撤回后不可见
	require.NoError(t, svc.RevokeNotification(ctx, id))
	items, total, err := svc.ListUserMessages(ctx, 1001, UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, items)
}

// TestScheduledNotification 定时公告：发送置已规划，扫描后投递。
func TestScheduledNotification(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()

	id, err := svc.SaveNotification(ctx, SaveNotificationInput{Type: TypeInbox, Title: "定时", Content: "c", Operator: 1})
	require.NoError(t, err)
	require.NoError(t, svc.SendManualNotification(ctx, SendManualInput{
		ID: id, AudienceType: AudienceTypeAll, ScheduleTime: time.Now().Add(-time.Minute).UnixMilli(), Operator: 1,
	}))
	row, err := svc.GetNotificationDetail(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, NotificationStatusPlanned, row.Status)

	require.NoError(t, svc.ScanScheduledDue(ctx))
	row, err = svc.GetNotificationDetail(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, NotificationStatusSent, row.Status)
	// 全部用户（2 人）各一条
	unread, err := svc.UnreadCount(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, int64(1), unread)
}

// 多个调度实例同时扫描同一公告，最终只能创建一封消息。
func TestScheduledNotificationConcurrentScanners(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()
	id, err := svc.SaveNotification(ctx, SaveNotificationInput{Type: TypeInbox, Title: "scheduled", Content: "content", Operator: 1})
	require.NoError(t, err)
	require.NoError(t, svc.SendManualNotification(ctx, SendManualInput{ID: id, AudienceType: AudienceTypeUsers, UserIDs: []int64{1001}, ScheduleTime: time.Now().Add(-time.Minute).UnixMilli(), Operator: 1}))
	start := make(chan struct{})
	errors := make(chan error, 6)
	var workers sync.WaitGroup
	for range 6 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errors <- New(store).ScanScheduledDue(ctx)
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var count int64
	require.NoError(t, repo.GetCommonConn(store).Model(&repo.NotifyMessage{}).Where("notification_id = ?", id).Count(&count).Error)
	require.Equal(t, int64(1), count)
	unread, err := svc.UnreadCount(ctx, 1001)
	require.NoError(t, err)
	require.Equal(t, int64(1), unread)
}

// 已创建消息后若状态落库失败，整笔事务回滚，下一次扫描可完整重试。
func TestScheduledNotificationRollsBackPartialDelivery(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()
	id, err := svc.SaveNotification(ctx, SaveNotificationInput{Type: TypeInbox, Title: "retry", Content: "content", Operator: 1})
	require.NoError(t, err)
	require.NoError(t, svc.SendManualNotification(ctx, SendManualInput{ID: id, AudienceType: AudienceTypeUsers, UserIDs: []int64{1001}, ScheduleTime: time.Now().Add(-time.Minute).UnixMilli(), Operator: 1}))
	db := repo.GetCommonConn(store)
	require.NoError(t, db.Exec("ALTER TABLE sys_notification ADD CONSTRAINT test_reject_sent CHECK (status <> 4)").Error)
	require.Error(t, svc.ScanScheduledDue(ctx))
	row, err := svc.GetNotificationDetail(ctx, id)
	require.NoError(t, err)
	require.Equal(t, NotificationStatusPlanned, row.Status)
	var count int64
	require.NoError(t, db.Model(&repo.NotifyMessage{}).Where("notification_id = ?", id).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Exec("ALTER TABLE sys_notification DROP CONSTRAINT test_reject_sent").Error)
	require.NoError(t, svc.ScanScheduledDue(ctx))
	require.NoError(t, db.Model(&repo.NotifyMessage{}).Where("notification_id = ?", id).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

// TestHandleCreateInbox_IdempotentGuard outbox 重复消费不重复建信。
func TestHandleCreateInbox_IdempotentGuard(t *testing.T) {
	svc, _, key, _ := newTestService(t)
	ctx := context.Background()

	res, err := svc.SendNotification(ctx, key, sendReq("1001", "t", "c", "k1"))
	require.NoError(t, err)
	reqID := parseID(t, res.MessageID)

	require.NoError(t, svc.HandleCreateInbox(ctx, CreateInboxPayload{RequestID: reqID}))
	require.NoError(t, svc.HandleCreateInbox(ctx, CreateInboxPayload{RequestID: reqID}))

	_, total, err := svc.ListUserMessages(ctx, 1001, UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
}

// TestDeleteMessagesAndHandle 删除/已处理仅影响本人。
func TestDeleteMessagesAndHandle(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()

	id, err := svc.SaveNotification(ctx, SaveNotificationInput{Type: TypeInbox, Title: "t", Content: "c", Operator: 1})
	require.NoError(t, err)
	require.NoError(t, svc.SendManualNotification(ctx, SendManualInput{
		ID: id, AudienceType: AudienceTypeUsers, UserIDs: []int64{1001, 1002}, Operator: 1,
	}))
	items, _, err := svc.ListUserMessages(ctx, 1001, UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	linkID := items[0].Link.ID

	require.NoError(t, svc.DeleteMessages(ctx, 1001, []int64{linkID}))
	_, total, err := svc.ListUserMessages(ctx, 1001, UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	// 用户 1002 不受影响
	_, total2, err := svc.ListUserMessages(ctx, 1002, UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total2)
}
