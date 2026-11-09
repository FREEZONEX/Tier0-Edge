package unsv2

import (
	"backend/internal/domain/sourcestream"
	"context"
)

// 仅供签名 Fleet 业务通道调用；公开配置由云端 Stream 绑定和发布生命周期管理。
func (s *Service) exchangeSourceSubscription(ctx context.Context, raw []byte) (any, error) {
	if s.sourceRuntime == nil {
		return nil, problem("UNAVAILABLE", "source runtime unavailable")
	}
	var req sourcestream.EdgeSubscriptionRequest
	if err := Decode(raw, &req); err != nil {
		return nil, err
	}
	return s.sourceRuntime.ExchangeEdgeSubscription(ctx, req)
}
