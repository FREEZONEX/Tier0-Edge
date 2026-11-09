package cluster

import (
	"context"
	"encoding/json"
	"testing"

	"backend/internal/config"
	"backend/internal/repo"
	"backend/internal/testkit"
)

// newTestCluster 创建独立测试库（testkit.NewTestDB，t.Cleanup 自动 DROP）并
// 装配 *cluster.Service：与生产 New 相同的 repo 注入（走 testkit 包级连接
// fallback），不启动任何后台协程/外部连接。
func newTestCluster(t *testing.T) (*repo.Store, *Service) {
	t.Helper()
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	s := &Service{
		cfg:       config.ClusterConf{},
		ingestCfg: config.DataIngestConf{},
		repo:      repo.NewClusterRepo(ctx),
		fleetRepo: repo.NewFleetRepo(ctx),
		closed:    make(chan struct{}),
	}
	s.fleet = newFleetSessionRegistry(s)
	return store, s
}

// backgroundCtx 返回测试用上下文（cluster 服务不依赖登录 Subject）。
func backgroundCtx() context.Context {
	return context.Background()
}

// jsonRaw 构造 json.RawMessage 测试辅助。
func jsonRaw(value string) json.RawMessage {
	return json.RawMessage(value)
}

// jsonMarshal 序列化并忽略错误（测试辅助，调用方负责断言）。
func jsonMarshal(value any) ([]byte, error) {
	return json.Marshal(value)
}
