// Package svctest 提供 svc.ServiceContext 的测试装配底座（TASK-028 阶段 0 交付）。
//
// 与生产 bootstrap.New 的差异（有意的裁剪，已在 04-单元测试方案 v1.1 记录）：
//   - 数据库/Redis 使用 testkit：每个测试独立 PG 库（生产迁移+种子原样执行）+ miniredis；
//   - 外部依赖（EMQX、Node-RED/Marimo、ZLM、S3、fleet agent、MQTT broker）只做惰性构造，
//     不发起连接、不启动后台协程（不调用 Start/StartBackground/RegisterRuntimeReconciler）；
//   - license 服务不装配（依赖宿主机授权文件与机器 ID），license 相关用例在包内用临时目录自建。
//
// 使用约定：测试默认串行执行，不得使用 t.Parallel()。
package svctest

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"backend/internal/bootstrap"
	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/aigateway"
	"backend/internal/domain/apikey"
	"backend/internal/domain/asset"
	"backend/internal/domain/audit"
	domainauth "backend/internal/domain/auth"
	"backend/internal/domain/cluster"
	"backend/internal/domain/dataingest"
	"backend/internal/domain/dataquality"
	domaingateway "backend/internal/domain/gateway"
	"backend/internal/domain/home"
	"backend/internal/domain/iam"
	"backend/internal/domain/launchpad"
	"backend/internal/domain/loginlog"
	"backend/internal/domain/mqtt"
	"backend/internal/domain/oauth"
	"backend/internal/domain/project"
	"backend/internal/domain/uns"
	"backend/internal/infra/fleetagent"
	"backend/internal/permission"
	"backend/internal/service/tenantpg"
	"backend/internal/svc"
	"backend/internal/testkit"
)

// testCredentialKey 是满足「base64 编码恰好 32 字节」要求的测试密钥
// （video/aigateway 的 newCredentialCipher 校验），32 个 0 字节。
var testCredentialKey = base64.StdEncoding.EncodeToString(make([]byte, 32))

// NewTestServiceContext 组装一个可用的 *svc.ServiceContext：
// 全新测试库 + miniredis + 全部可惰性构造的领域服务，中间件照常装配。
func NewTestServiceContext(t *testing.T) *svc.ServiceContext {
	t.Helper()
	store, dsn := testkit.NewTestDB(t)
	cacheClient := testkit.NewTestCache(t)
	ctx := context.Background()

	cfg := testConfig(t, dsn)
	authSvc := domainauth.New(ctx, cfg.Security.JwtSecret)
	auditSvc := audit.New(ctx)
	apiKeySvc := apikey.New(ctx)
	iamSvc := iam.New(ctx)
	permissionEvaluator := permission.New(ctx)

	aiGatewaySvc, err := aigateway.New(ctx, cfg.AIGateway)
	if err != nil {
		t.Fatalf("svctest: aigateway.New: %v", err)
	}
	assetSvc, err := asset.New(ctx, asset.Config{
		Driver:        "local",
		FileRoot:      t.TempDir(),
		PublicBaseURL: "http://localhost",
	})
	if err != nil {
		t.Fatalf("svctest: asset.New: %v", err)
	}
	// 传空 sink DSN：纯逻辑测试不建 SinkWriter（NewSinkWriter 会开 MinConns=1 的
	// pgxpool 且从不关闭，测试一多就会耗尽 PG max_connections）。writer==nil 时
	// dataingest 的物理时序表写入路径本就是安全 no-op。
	dataIngestSvc, err := dataingest.New(ctx, cfg.DataIngest, "", cacheClient)
	if err != nil {
		t.Fatalf("svctest: dataingest.New: %v", err)
	}
	dataQualitySvc, err := dataquality.New(ctx, cfg.DataQuality)
	if err != nil {
		t.Fatalf("svctest: dataquality.New: %v", err)
	}
	if dataQualitySvc.Enabled() {
		dataIngestSvc.SetQualityObserver(dataQualitySvc)
		dataIngestSvc.SetConnectorHealthObserver(dataQualitySvc)
	}
	homeSvc := home.New(ctx)
	launchpadSvc := launchpad.New(ctx, store)
	mqttSvc := mqtt.New(ctx, authSvc, cacheClient, "")
	oauthSvc := oauth.New(ctx)

	tenantRegistry, err := tenantpg.NewRegistry(tenantpg.Config{
		UnsDbUrl:      dsn,
		RoleSecret:    cfg.TenantPG.RoleSecret,
		ConnCacheSize: 8,
	})
	if err != nil {
		t.Fatalf("svctest: tenantpg.NewRegistry: %v", err)
	}
	tenantLifecycle := tenantpg.NewLifecycle(tenantRegistry, cfg.TenantPG.RoleSecret)
	projectSvc := project.New(ctx, tenantLifecycle, dsn, cfg.TenantPG.RoleSecret, iamSvc)
	unsSvc := uns.New(ctx, dataIngestSvc)
	fleetClient := fleetagent.New(config.FleetAgentConf{})
	clusterSvc := cluster.New(ctx, cfg.Cluster, cfg.DataIngest, fleetClient)
	clusterSvc.BindAudit(auditSvc)
	gatewaySvc := domaingateway.New(ctx)

	app := &bootstrap.App{
		Config:            cfg,
		Store:             store,
		Cache:             cacheClient,
		Auth:              authSvc,
		Audit:             auditSvc,
		LoginLog:          loginlog.New(ctx),
		APIKey:            apiKeySvc,
		AIGateway:         aiGatewaySvc,
		Asset:             assetSvc,
		Cluster:           clusterSvc,
		DataIngest:        dataIngestSvc,
		DataQuality:       dataQualitySvc,
		Home:              homeSvc,
		IAM:               iamSvc,
		Launchpad:         launchpadSvc,
		MQTT:              mqttSvc,
		OAuth:             oauthSvc,
		Project:           projectSvc,
		UNS:               unsSvc,
		Gateway:           gatewaySvc,
		Permission:        permissionEvaluator,
		TenantPGRegistry:  tenantRegistry,
		TenantPGLifecycle: tenantLifecycle,
		StartedAt:         time.Now().UTC(),
	}
	return svc.NewServiceContext(cfg, app)
}

// CtxWithUser 返回携带登录用户 Subject 的 context（logicx.UserID 读取的字段）。
func CtxWithUser(userID int64) context.Context {
	return contextx.WithSubject(context.Background(), contextx.Subject{
		UserID:   userID,
		UserName: "tier0",
		AuthType: "session",
	})
}

func testConfig(t *testing.T, dsn string) config.Config {
	t.Helper()
	return config.Config{
		Database: config.DatabaseConf{UnsDbUrl: dsn},
		TenantPG: config.TenantPGConf{RoleSecret: "test-role-secret"},
		Security: config.SecurityConf{
			JwtSecret:            "test-jwt-secret",
			InitialAdminPassword: "tier0",
		},
		Storage: config.StorageConf{
			Driver:        "local",
			FileRoot:      t.TempDir(),
			PublicBaseURL: "http://localhost",
		},
		AIGateway:  config.AIGatewayConf{CredentialKey: testCredentialKey},
		DataIngest: config.DataIngestConf{},
		Gateway:    config.GatewayConf{},
		Cluster:    config.ClusterConf{},
		License:    config.LicenseConf{},
		FleetAgent: config.FleetAgentConf{},
	}
}
