package audit

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"backend/internal/repo"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

const OutboxEventType = "audit.record"

type auditEvent struct {
	Record      repo.AuditLog `json:"record"`
	CreatedTime time.Time     `json:"createdTime"`
}

// 请求完成后使用独立超时入库，客户端断开不取消审计。只有持久化失败才做有限重试。
// 业务提交和此入队不是同一事务；数据库持续不可用的失败必须保留可关联的错误。
func (s *Service) persist(parent context.Context, item repo.AuditLog) {
	if s == nil || s.outbox == nil {
		return
	}
	item.EventID = uuid.NewString()
	// 邮箱由查询时按页关联，不将临时解密的联系信息写入 outbox。
	item.OperatorEmail = ""
	payload, err := json.Marshal(auditEvent{Record: item, CreatedTime: item.CreatedTime})
	if err != nil {
		logx.WithContext(parent).Errorf("audit encode failed eventId=%s: %v", item.EventID, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	event := repo.OutboxEvent{EventID: item.EventID, EventType: OutboxEventType, AggregateType: "audit", AggregateID: item.EventID, Payload: payload}
	for attempt := 0; attempt < 3; attempt++ {
		if err = s.outbox.EnqueueOutbox(ctx, event); err == nil {
			return
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				attempt = 3
			case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
			}
		}
	}
	logx.WithContext(parent).Errorf("audit persistence failed eventId=%s requestId=%s: %v", item.EventID, item.RequestID, err)
}

// Consume 与 outbox 重试配合：消费后进程退出再次投递不会重复插入日志。
func (s *Service) Consume(ctx context.Context, event repo.OutboxEvent) error {
	var payload auditEvent
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return err
	}
	if event.EventID == "" || payload.CreatedTime.IsZero() {
		return errors.New("invalid audit event")
	}
	payload.Record.EventID = event.EventID
	payload.Record.CreatedTime = payload.CreatedTime
	return s.repo.InsertAuditLog(ctx, payload.Record)
}

func resourcePath(detail map[string]any) string {
	// 业务快照可能是具体 struct，统一转成 JSON 后读取，不依赖调用者使用 map。
	raw, err := json.Marshal(detail)
	if err != nil {
		return ""
	}
	var value struct {
		After struct {
			Path string `json:"path"`
		} `json:"after"`
		Before struct {
			Path string `json:"path"`
		} `json:"before"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	if value.After.Path != "" {
		return value.After.Path
	}
	return value.Before.Path
}

func mapFromQuery(values url.Values) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
