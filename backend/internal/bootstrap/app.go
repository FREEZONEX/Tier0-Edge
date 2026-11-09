package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"backend/internal/appgateway"
	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/aigateway"
	"backend/internal/domain/apikey"
	"backend/internal/domain/appinstall"
	"backend/internal/domain/asset"
	"backend/internal/domain/audit"
	"backend/internal/domain/auth"
	"backend/internal/domain/cluster"
	"backend/internal/domain/dataingest"
	"backend/internal/domain/dataquality"
	"backend/internal/domain/fleetapi"
	"backend/internal/domain/fleettunnel"
	domaingateway "backend/internal/domain/gateway"
	"backend/internal/domain/home"
	"backend/internal/domain/iam"
	"backend/internal/domain/launchpad"
	"backend/internal/domain/loginlog"
	"backend/internal/domain/mcpoauth"
	"backend/internal/domain/mcpserver"
	"backend/internal/domain/mqtt"
	domainnotify "backend/internal/domain/notify"
	"backend/internal/domain/oauth"
	"backend/internal/domain/project"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/uns"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
	"backend/internal/gateway"
	"backend/internal/infra/cache"
	"backend/internal/infra/filestore"
	infrafleetagent "backend/internal/infra/fleetagent"
	infraoutbox "backend/internal/infra/outbox"
	"backend/internal/infra/sourceprotocol"
	"backend/internal/infra/sourcestreamworker"
	"backend/internal/infra/streamexecutor"
	"backend/internal/permission"
	"backend/internal/repo"
	"backend/internal/service/tenantpg"

	"github.com/zeromicro/go-zero/core/logx"
)

type App struct {
	LoginLog                *loginlog.Service
	closeOnce               sync.Once
	Config                  config.Config
	Store                   *repo.Store
	Cache                   *cache.Client
	NativeApps              *appinstall.Service
	NativeAppCredentials    *appinstall.Credentials
	NativeAppManagement     *appinstall.Management
	Auth                    *auth.Service
	Audit                   *audit.Service
	APIKey                  *apikey.Service
	AIGateway               *aigateway.Service
	Asset                   *asset.Service
	Cluster                 *cluster.Service
	FleetTunnel             *fleettunnel.Service
	DataIngest              *dataingest.Service
	DataQuality             *dataquality.Service
	Home                    *home.Service
	IAM                     *iam.Service
	Launchpad               *launchpad.Service
	MQTT                    *mqtt.Service
	Notify                  *domainnotify.Service
	OAuth                   *oauth.Service
	Project                 *project.Service
	UNS                     *uns.Service
	UnsMeta                 *unsmeta.Service
	UnsV2                   *unsv2.Service
	MCP                     *mcpserver.Service
	MCPOAuth                *mcpoauth.Service
	SourceStream            *sourcestream.Service
	SourceModbus            *sourceprotocol.RemoteModbusDriver
	SourceStreamWorker      *sourcestreamworker.Worker
	Gateway                 *domaingateway.Service
	Permission              *permission.Evaluator
	HTTPGateway             *gateway.HTTPGateway
	AppGateway              *appgateway.Gateway
	OutboxWorker            *infraoutbox.Worker
	AuditOutboxWorker       *infraoutbox.Worker
	UnsPhysicalOutboxWorker *infraoutbox.Worker
	TenantPGRegistry        *tenantpg.Registry
	TenantPGLifecycle       *tenantpg.Lifecycle
	StartedAt               time.Time
}

func New(ctx context.Context, c config.Config) (*App, error) {
	if c.Redis.Driver != "" && c.Redis.Driver != "memory" && c.Redis.Driver != "redis" {
		return nil, fmt.Errorf("unsupported cache driver: %s", c.Redis.Driver)
	}
	_, err := config.ParseMQTTEndpoint(c.DataIngest.MqttBrokers)
	if err != nil {
		return nil, fmt.Errorf("DATAINGEST_MQTT_BROKERS: %w", err)
	}
	store, err := repo.Open(ctx, c.Database)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := store.Migrate(ctx); err != nil {
		store.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}
	if err := store.Seed(ctx, c.Security, c.Gateway); err != nil {
		store.Close()
		return nil, fmt.Errorf("seed db: %w", err)
	}
	cacheClient := cache.Open(c.Redis)
	app := &App{
		Config:     c,
		Store:      store,
		Cache:      cacheClient,
		Permission: permission.New(ctx),
		StartedAt:  time.Now().UTC(),
	}
	app.Auth = auth.New(ctx, c.Security.JwtSecret)
	app.Auth.ConfigureMicrosoft(cacheClient, auth.MicrosoftConfig{
		Enabled:      c.ExternalLogin.Enabled && c.MicrosoftSSO.Enabled,
		TenantID:     c.MicrosoftSSO.TenantID,
		ClientID:     c.MicrosoftSSO.ClientID,
		ClientSecret: c.MicrosoftSSO.ClientSecret,
		RedirectURL:  c.MicrosoftSSO.RedirectURL,
	})
	// This distribution does not initialize Vision, Anchor or AI Agent services.
	// Keep audit and core APIs independent of those optional runtimes.
	app.Audit = audit.New(ctx, audit.WithTrustProxyHeaders(c.Gateway.TrustProxyHeaders))
	app.LoginLog = loginlog.New(ctx)
	app.APIKey = apikey.New(ctx)
	app.Notify = domainnotify.New(ctx)
	app.Asset, err = asset.New(ctx, asset.Config{
		Driver:        c.Storage.Driver,
		FileRoot:      c.Storage.FileRoot,
		PublicBaseURL: c.Storage.PublicBaseURL,
		S3: filestore.S3Config{
			Endpoint:         c.Storage.S3.Endpoint,
			Region:           c.Storage.S3.Region,
			Bucket:           c.Storage.S3.Bucket,
			PublicBucket:     c.Storage.S3.PublicBucket,
			AccessKeyID:      c.Storage.S3.AccessKeyID,
			SecretAccessKey:  c.Storage.S3.SecretAccessKey,
			SessionToken:     c.Storage.S3.SessionToken,
			UseSSL:           c.Storage.S3.UseSSL,
			PathStyle:        c.Storage.S3.PathStyle,
			PublicEndpoint:   c.Storage.S3.PublicEndpoint,
			BootstrapBuckets: c.Storage.S3.BootstrapBuckets,
		},
	})
	if err != nil {
		_ = cacheClient.Close()
		store.Close()
		return nil, fmt.Errorf("open filestore: %w", err)
	}
	app.DataIngest, err = dataingest.New(ctx, c.DataIngest, c.Database.SinkDbUrl, cacheClient)
	if err != nil {
		_ = cacheClient.Close()
		store.Close()
		return nil, fmt.Errorf("init dataingest: %w", err)
	}
	app.DataQuality, err = dataquality.New(ctx, c.DataQuality)
	if err != nil {
		app.DataIngest.Close()
		_ = cacheClient.Close()
		store.Close()
		return nil, fmt.Errorf("init data quality: %w", err)
	}
	if app.DataQuality.Enabled() {
		app.DataIngest.SetQualityObserver(app.DataQuality)
		app.DataIngest.SetConnectorHealthObserver(app.DataQuality)
	}
	cleanupOnFailure := true
	defer func() {
		if cleanupOnFailure {
			app.Close()
		}
	}()
	app.Home = home.New(ctx)
	app.IAM = iam.New(ctx)
	app.IAM.BindAudit(app.Audit)
	fleetAgentClient := infrafleetagent.New(c.FleetAgent)
	app.Launchpad = launchpad.New(ctx, store)
	app.Launchpad.SetAppGatewayConfig(c.AppGateway)
	app.Launchpad.SetShortcutIconSecret(c.Security.JwtSecret)
	app.MQTT = mqtt.New(ctx, app.Auth, cacheClient, c.Gateway.EmqxUrl)
	app.OAuth = oauth.New(ctx)
	app.TenantPGRegistry, err = tenantpg.NewRegistry(tenantpg.Config{
		UnsDbUrl:       c.Database.UnsDbUrl,
		RoleSecret:     c.TenantPG.RoleSecret,
		ConnectTimeout: c.TenantPG.ConnectTimeout,
		MaxOpenConns:   c.TenantPG.MaxOpenConns,
		MaxIdleConns:   c.TenantPG.MaxIdleConns,
		ConnCacheSize:  c.TenantPG.ConnCacheSize,
	})
	if err != nil {
		_ = cacheClient.Close()
		store.Close()
		return nil, fmt.Errorf("init tenantpg registry: %w", err)
	}
	app.TenantPGLifecycle = tenantpg.NewLifecycle(app.TenantPGRegistry, c.TenantPG.RoleSecret)
	app.Project = project.New(ctx, app.TenantPGLifecycle, c.Database.UnsDbUrl, c.TenantPG.RoleSecret, app.IAM)
	app.Project.SetAppGatewayConfig(c.AppGateway)
	app.UNS = uns.New(ctx, app.DataIngest)
	app.UNS.DisableLegacyFlows()
	app.UnsMeta = unsmeta.New()
	app.MCPOAuth = mcpoauth.New(ctx, app.Auth, c.MCP)
	app.MCPOAuth.BindAudit(app.Audit)
	app.UnsV2 = unsv2.New(app.UnsMeta, app.MCPOAuth.RefreshSubject, app.Auth)
	app.UnsV2.SetQueryCursorSecret(c.Security.JwtSecret)
	app.UnsV2.SetRecordChangesCursorSecret(c.Security.JwtSecret)
	if err := repo.ConfigureRecordChangeNotifier(store.UnsDB(), app.UnsV2.NotifyRecordChanges); err != nil {
		app.Close()
		return nil, fmt.Errorf("initialize record notifications: %w", err)
	}
	app.MCP, err = mcpserver.New(app.UnsV2, c.ProductVersion, c.MCP)
	if err != nil {
		return nil, fmt.Errorf("initialize MCP: %w", err)
	}
	app.MCP.SetOAuthInfo(app.MCPOAuth.Info)
	if err = app.initializeNativeApps(ctx); err != nil {
		app.Close()
		return nil, fmt.Errorf("initialize native Apps: %w", err)
	}
	if c.SourceStream.NATSURL != "" {
		if err := app.DataIngest.EnableResourceEvents(ctx); err != nil {
			return nil, fmt.Errorf("enable native resource events: %w", err)
		}
	}
	sourceRuntime := sourceprotocol.New()
	app.SourceModbus = sourceprotocol.NewRemoteModbus(c.SourceStream.ModbusSocket)
	sourceRuntime.Register(sourcestream.ModbusTCP, app.SourceModbus)
	opcuaRuntime := sourceprotocol.NewOPCUA(c.SourceStream.OPCUASocket)
	sourceRuntime.Register(sourcestream.OPCUA, opcuaRuntime)
	sourceRuntime.RegisterServer(sourcestream.OPCUA, opcuaRuntime)
	// Device-facing Server Sources share EMQX, but use isolated namespaces.
	// The bridge uses private platform credentials, never device credentials.
	if broker, parseErr := config.ParseMQTTEndpoint(c.DataIngest.MqttBrokers); parseErr == nil && !broker.WebSocket() {
		if broker.TLS() {
			broker.Scheme = "mqtts"
		} else {
			broker.Scheme = "mqtt"
		}
		sourceRuntime.RegisterServer(sourcestream.MQTT, sourceprotocol.MQTTServerDriver{
			Endpoint:   broker.URL(),
			Credential: sourcestream.Credential{Kind: "basic", Username: c.DataIngest.MqttUsername, Secret: c.DataIngest.MqttPassword},
		})
	}
	app.SourceStream = sourcestream.New(sourceRuntime, streamexecutor.New(c.SourceStream.ExecutorEndpoints()), c.SourceStream.CredentialKey)
	app.SourceStream.SetProgramAuthorityResolver(func(ctx context.Context, ref repo.ProgramAuthorityRef) (contextx.Subject, error) {
		subject, err := app.Auth.ResolveProgramAuthority(ctx, ref)
		if auth.IsUnauthorized(err) {
			return contextx.Subject{}, sourcestream.ErrForbidden
		}
		return subject, err
	})
	if app.NativeAppManagement != nil {
		app.NativeAppManagement.SetSourceRuntime(app.SourceStream)
	}
	app.UnsV2.SetSourceRuntime(app.SourceStream)
	app.SourceStream.SetProgramUNS(func(ctx context.Context, id int64) (json.RawMessage, error) {
		node, err := repo.NewUnsRepo(ctx).GetUnsNode(ctx, id, false)
		if err != nil || node.Type != 2 {
			if err != nil {
				return nil, err
			}
			return nil, repo.ErrInvalidArgument
		}
		payload, err := app.DataIngest.LastPayload(ctx, node)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"topicId": fmt.Sprint(id), "hasData": len(payload) > 0, "data": payload})
	}, app.DataIngest)
	app.MQTT.SetSourceAccess(app.SourceStream)
	app.Cluster = cluster.New(ctx, c.Cluster, c.DataIngest, fleetAgentClient)
	app.Cluster.SetFleetControlSubject(app.MCPOAuth.RefreshSubject)
	app.SourceStream.SetRemoteControlAccess(app.Cluster.CheckFleetControlAccess)
	if err := repo.ConfigureCollectionStorageReader(repo.GetUnsConn(ctx), app.Cluster.ReadCollectionStorage); err != nil {
		panic(err)
	}
	app.MCP.SetFleet(&fleetapi.Service{Cluster: app.Cluster, Uns: app.UnsV2, SourceStream: app.SourceStream})
	app.Cluster.BindAudit(app.Audit)
	app.FleetTunnel = fleettunnel.New(fleettunnel.Options{
		IsCenter:         func() bool { return false },
		Authenticate:     app.Cluster.AuthorizeFleetTunnel,
		ValidateNode:     app.Cluster.ValidateFleetTunnelNode,
		LocalBackendAddr: fmt.Sprintf("127.0.0.1:%d", c.Port),
	})

	app.FleetTunnel.SetFleetRPCHandler(app.Cluster.ApplyFleetTunnelRequest)
	app.Cluster.BindFleetTunnel(app.FleetTunnel)
	app.Gateway = domaingateway.New(ctx)
	app.HTTPGateway = gateway.NewHTTPGateway(ctx, c.Gateway, app.Auth, app.Permission,
		gateway.NewS3Proxy(c.Storage.Driver, c.Storage.S3.Endpoint, c.Storage.S3.Bucket, c.Storage.S3.PublicBucket),
		gateway.NewAIProxy(app.AIGateway))
	app.HTTPGateway.BindFleetTunnel(app.FleetTunnel)
	if app.NativeApps != nil {
		app.HTTPGateway.BindNativeApps(app.NativeApps)
	}
	if c.SourceStream.NATSURL != "" {
		app.HTTPGateway.DisableLegacyFlows()
	}
	app.AppGateway = appgateway.NewGateway(c)
	registerEventHandlers(app)
	app.OutboxWorker = newOutboxWorker(app)
	app.AuditOutboxWorker = newAuditOutboxWorker(app)
	app.UnsPhysicalOutboxWorker = newUnsPhysicalOutboxWorker(app)
	if err := app.DataIngest.Start(ctx); err != nil {
		app.Close()
		return nil, fmt.Errorf("start dataingest: %w", err)
	}
	if err := app.SourceStream.StartProgramExecutions(ctx); err != nil {
		app.Close()
		return nil, fmt.Errorf("start Stream execution ownership: %w", err)
	}
	if c.SourceStream.NATSURL != "" {
		worker, err := sourcestreamworker.Start(ctx, c.SourceStream.NATSURL, app.SourceStream, app.DataIngest)
		if err != nil {
			app.Close()
			return nil, fmt.Errorf("start source stream worker: %w", err)
		}
		app.SourceStreamWorker = worker
		app.SourceStream.SetAsyncReady(worker.Ready)
		app.SourceStream.SetWatchState(worker.WatchState)
	}
	// Source HTTP routes are synchronous and remain independent of NATS.
	app.SourceStream.StartHTTPServers(ctx)
	app.UnsV2.Start(ctx)
	if app.NativeApps != nil {
		go func() {
			if err := app.NativeApps.Restore(ctx); err != nil {
				logx.Errorf("native App restore failed: %v", err)
			}
		}()
	}
	app.StartClusterAsync()
	app.StartAppGatewayAsync()
	app.StartOutboxWorkerAsync()
	if app.Notify != nil {
		// 定时发送扫描（TASK-030）：每分钟触发到期公告投递
		go app.Notify.StartScheduler(ctx)
	}
	cleanupOnFailure = false
	return app, nil
}

func (a *App) StartClusterAsync() {
	if a == nil || a.Cluster == nil {
		return
	}
	a.Cluster.StartCollaborationWorker()
}

func (a *App) StartAppGatewayAsync() {
	if a == nil || a.AppGateway == nil {
		return
	}
	go func() {
		time.Sleep(1 * time.Second)
		if err := a.AppGateway.Start(); err != nil {
			logx.Errorf("start app gateway: %v", err)
		}
	}()
}

func (a *App) StartOutboxWorkerAsync() {
	if a == nil {
		return
	}
	if a.OutboxWorker != nil {
		a.OutboxWorker.Start(context.Background())
	}
	if a.AuditOutboxWorker != nil {
		a.AuditOutboxWorker.Start(context.Background())
	}
	if a.UnsPhysicalOutboxWorker != nil {
		a.UnsPhysicalOutboxWorker.Start(context.Background())
	}
}

func (a *App) Close() {
	if a == nil {
		return
	}
	a.closeOnce.Do(func() {
		if a.NativeAppManagement != nil {
			a.NativeAppManagement.Close()
		}
		if a.NativeApps != nil {
			a.NativeApps.Close()
		}
		if a.UnsV2 != nil {
			a.UnsV2.Close()
		}
		if a.SourceStreamWorker != nil {
			a.SourceStreamWorker.Close()
		}
		if a.SourceStream != nil {
			a.SourceStream.Close()
		}
		if a.OutboxWorker != nil {
			a.OutboxWorker.Stop()
		}
		if a.AuditOutboxWorker != nil {
			a.AuditOutboxWorker.Stop()
		}
		if a.UnsPhysicalOutboxWorker != nil {
			a.UnsPhysicalOutboxWorker.Stop()
		}
		if a.Cluster != nil {
			a.Cluster.Close()
		}
		if a.FleetTunnel != nil {
			a.FleetTunnel.Close()
		}
		if a.DataIngest != nil {
			a.DataIngest.Close()
		}
		if a.DataQuality != nil {
			a.DataQuality.Close()
		}
		if a.TenantPGRegistry != nil {
			_ = a.TenantPGRegistry.Close()
		}
		if a.Cache != nil {
			_ = a.Cache.Close()
		}
		if a.Store != nil {
			a.Store.Close()
		}
	})
}

func (a *App) Readiness(ctx context.Context) map[string]string {
	out := map[string]string{"db": "ok", "cache": "ok"}
	if err := a.Store.Ping(ctx); err != nil {
		out["db"] = err.Error()
	}
	if err := a.Cache.Ping(ctx); err != nil {
		out["cache"] = err.Error()
	}
	if a.SourceStream != nil {
		out["streamExecution"] = "ok"
		if !a.SourceStream.ProgramExecutionReady() {
			out["streamExecution"] = "SourceStream.unavailable"
		}
	}
	// Synchronous Modbus operations do not depend on the NATS worker.
	if a.SourceModbus == nil || a.SourceModbus.Ready(ctx) != nil {
		out["sourceModbus"] = "SourceStream.unavailable"
	} else {
		out["sourceModbus"] = "ok"
	}
	if a.Config.SourceStream.NATSURL != "" {
		out["sourceStream"] = "ok"
		if a.SourceStreamWorker == nil || !a.SourceStreamWorker.Ready() {
			out["sourceStream"] = "SourceStream.unavailable"
		}
		if err := streamexecutor.New(a.Config.SourceStream.ExecutorEndpoints()).Ready(ctx); err != nil {
			out["streamExecutor"] = "SourceStream.unavailable"
		} else {
			out["streamExecutor"] = "ok"
		}
		if err := sourceprotocol.NewOPCUA(a.Config.SourceStream.OPCUASocket).Ready(ctx); err != nil {
			out["sourceOPCUA"] = "SourceStream.unavailable"
		} else {
			out["sourceOPCUA"] = "ok"
		}
	}
	return out
}

func (a *App) Ready(ctx context.Context) error {
	if err := a.Store.Ping(ctx); err != nil {
		return err
	}
	if err := a.Cache.Ping(ctx); err != nil {
		return err
	}
	if a.SourceStream != nil && !a.SourceStream.ProgramExecutionReady() {
		return sourcestream.ErrUnavailable
	}
	if a.SourceModbus == nil {
		return sourcestream.ErrUnavailable
	}
	if err := a.SourceModbus.Ready(ctx); err != nil {
		return err
	}
	if a.Config.SourceStream.NATSURL != "" {
		if a.SourceStreamWorker == nil || !a.SourceStreamWorker.Ready() {
			return sourcestream.ErrUnavailable
		}
		if err := streamexecutor.New(a.Config.SourceStream.ExecutorEndpoints()).Ready(ctx); err != nil {
			return err
		}
		if err := sourceprotocol.NewOPCUA(a.Config.SourceStream.OPCUASocket).Ready(ctx); err != nil {
			return err
		}
	}
	return nil
}
