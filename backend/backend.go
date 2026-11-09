// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package main

import (
	"backend/internal/bootstrap"
	"backend/internal/config"
	"backend/internal/domain/mcpserver"
	"backend/internal/gateway"
	"backend/internal/handler"
	"backend/internal/middleware"
	"backend/internal/repo"
	"backend/internal/runtimeenv"
	"backend/internal/service/tenantpg"
	"backend/internal/svc"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	_ "github.com/zeromicro/go-zero/core/proc" //开启pprof采集 https://mp.weixin.qq.com/s/yYFM3YyBbOia3qah3eRVQA

	"github.com/zeromicro/go-zero/rest"
)

func main() {
	if runtimeenv.IdentityQuery() {
		return
	}
	if mcpserver.IsSandboxChild() {
		os.Exit(mcpserver.RunSandboxChild())
	}
	migrateOnly := flag.Bool("migrate-only", false, "run database migrations and exit")
	configFile := flag.String("f", "etc/backend.yaml", "backend configuration file")
	projectMaintenance := flag.String("project-db-maintenance", "", "project restore metadata: export/check (read-only), prepare/restore-acl (explicit apply)")
	projectDatabase := flag.String("project-database", "", "explicit project database for maintenance")
	projectDumpSHA256 := flag.String("project-dump-sha256", "", "actual SQL file SHA-256 to match before restore checks or preparation")
	projectRestoreApply := flag.Bool("project-db-restore-apply", false, "explicitly allow restore role preparation; never imports SQL")
	flag.Parse()
	maintenanceRequested := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "project-db-maintenance" || f.Name == "project-database" || f.Name == "project-db-restore-apply" || f.Name == "project-dump-sha256" {
			maintenanceRequested = true
		}
	})
	mutatingAction := *projectMaintenance == "prepare" || *projectMaintenance == "restore-acl"
	validAction := *projectMaintenance == "export" || *projectMaintenance == "check" || mutatingAction
	if maintenanceRequested && (*migrateOnly || flag.NArg() != 0 || *projectDatabase == "" || !validAction || mutatingAction != *projectRestoreApply || (*projectMaintenance == "export" && *projectDumpSHA256 != "")) {
		fmt.Fprintln(os.Stderr, "invalid or conflicting project database maintenance mode")
		os.Exit(2)
	}
	logx.DisableStat()
	var c config.Config
	if *projectMaintenance != "" {
		// Administrative database checks must not depend on HTTP, Video or
		// protocol-server YAML. Read only the database environment settings;
		// never guess the database from development defaults.
		if strings.TrimSpace(os.Getenv("UNS_DB_URL")) == "" {
			fmt.Fprintln(os.Stderr, "UNS_DB_URL is required for project database maintenance")
			os.Exit(2)
		}
		c.Database.UnsDbUrl = os.Getenv("UNS_DB_URL")
		c.TenantPG.RoleSecret = os.Getenv("TENANT_PG_ROLE_SECRET")
		if value := os.Getenv("TENANT_PG_CONNECT_TIMEOUT"); value != "" {
			timeout, err := time.ParseDuration(value)
			if err != nil || timeout <= 0 {
				fmt.Fprintln(os.Stderr, "invalid TENANT_PG_CONNECT_TIMEOUT for project maintenance")
				os.Exit(2)
			}
			c.TenantPG.ConnectTimeout = timeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		stopInput := context.AfterFunc(ctx, func() { _ = os.Stdin.Close() })
		err := runProjectDBMaintenance(ctx, c, *projectMaintenance, *projectDatabase, *projectDumpSHA256, os.Stdin, os.Stdout)
		stopInput()
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	conf.MustLoad(*configFile, &c)
	c.Normalize()
	if err := config.ValidateAppGatewayConfig(c.AppGateway); err != nil {
		panic(err)
	}

	if *migrateOnly {
		if err := runMigrateOnly(context.Background(), c); err != nil {
			panic(err)
		}
		fmt.Println("database migration complete")
		return
	}

	if err := prepareFleetIngressTLS(&c); err != nil {
		panic(err)
	}
	app, err := bootstrap.New(context.Background(), c)
	if err != nil {
		panic(err)
	}
	defer app.Close()

	serviceCtx := svc.NewServiceContext(c, app)
	server := newRestServerWithCtx(c, app, serviceCtx)
	defer server.Stop()

	// HTTPS listener：独立端口 + 证书；证书异常 fail-fast（不静默降级 HTTP）
	httpsServer, err := newHTTPSRestServer(c, app, serviceCtx)
	if err != nil {
		panic(err)
	}
	if httpsServer != nil {
		defer httpsServer.Stop()
		go func() {
			fmt.Printf("Starting HTTPS server at %s:%d...\n", c.Host, c.Https.Port)
			httpsServer.StartWithOpts(gateway.WithHTTPGateway(app.HTTPGateway))
		}()
	}

	fleetIngress, err := newFleetIngressServer(c, app, serviceCtx)
	if err != nil {
		panic(err)
	}
	if fleetIngress != nil {
		defer fleetIngress.Stop()
		go fleetIngress.Start()
	}

	launchpadServer, launchpadPort := newLaunchpadServer(c, app, serviceCtx)
	if launchpadServer != nil {
		defer launchpadServer.Stop()
		go func() {
			fmt.Printf("Starting Launchpad server at %s:%d...\n", c.Host, launchpadPort)
			launchpadServer.StartWithOpts(gateway.WithLaunchpadHTTPGateway(app.HTTPGateway))
		}()
	}

	server.PrintRoutes()
	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.StartWithOpts(gateway.WithHTTPGateway(app.HTTPGateway))
}

// This administrative mode opens no HTTP listener, starts no workers and does
// not migrate/seed data. Input SQL is hashed, never executed or echoed.
func runProjectDBMaintenance(ctx context.Context, c config.Config, action, database, dumpSHA256 string, input io.Reader, output io.Writer) error {
	if err := tenantpg.ValidateIdentifier(database); err != nil {
		return fmt.Errorf("project database maintenance requires a valid explicit database")
	}
	var contract tenantpg.ProjectRestoreContract
	var dumpHash string
	switch action {
	case "export":
		hash := sha256.New()
		n, err := io.Copy(hash, input)
		if err != nil || n == 0 {
			return fmt.Errorf("project database maintenance requires a readable nonempty SQL dump")
		}
		dumpHash = hex.EncodeToString(hash.Sum(nil))
	case "check", "prepare", "restore-acl":
		const maxManifestBytes = tenantpg.ProjectRestoreMaxContractBytes
		data, err := io.ReadAll(io.LimitReader(input, maxManifestBytes+1))
		if err != nil || len(data) > maxManifestBytes {
			return fmt.Errorf("project restore contract exceeds its input budget or cannot be read")
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&contract) != nil || decoder.Decode(new(any)) != io.EOF {
			return fmt.Errorf("invalid project restore contract JSON")
		}
		if err := contract.Validate(); err != nil {
			return err
		}
		if contract.Database != database {
			return fmt.Errorf("project restore contract does not match the requested database")
		}
		if dumpSHA256 != "" && contract.DumpSHA256 != dumpSHA256 {
			return fmt.Errorf("project restore SQL SHA-256 does not match its role contract")
		}
	default:
		return fmt.Errorf("unsupported project database maintenance mode")
	}
	registry, err := tenantpg.NewRegistry(tenantpg.Config{
		UnsDbUrl:       c.Database.UnsDbUrl,
		RoleSecret:     c.TenantPG.RoleSecret,
		ConnectTimeout: c.TenantPG.ConnectTimeout,
	})
	if err != nil {
		return fmt.Errorf("invalid database configuration for project maintenance")
	}
	defer registry.Close()
	lifecycle := tenantpg.NewLifecycle(registry, c.TenantPG.RoleSecret)
	if action == "export" {
		result, err := lifecycle.ExportRestoreContract(ctx, database, dumpHash)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(result)
	}
	if action == "prepare" {
		result, err := lifecycle.PrepareRestoreContract(ctx, contract)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(result)
	}
	if action == "restore-acl" {
		result, err := lifecycle.RestoreDatabaseACL(ctx, contract)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(result)
	}
	result, err := lifecycle.CheckRestoreContract(ctx, contract)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

func runMigrateOnly(ctx context.Context, c config.Config) error {
	store, err := repo.Open(ctx, c.Database)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate db: %w", err)
	}
	return nil
}

// newRestServerWithCtx 用给定 ServiceContext 构造 rest.Server（所有 listener 共享同一 ctx）。
func newRestServerWithCtx(c config.Config, app *bootstrap.App, ctx *svc.ServiceContext) *rest.Server {
	requestLog := c.RestConf.Middlewares.Log
	c.RestConf.Middlewares.Log = false
	server := rest.MustNewServer(c.RestConf)
	if requestLog {
		server.Use(middleware.SourceSafeLog(c.Verbose))
	}
	server.Use(middleware.NewAuditLogMiddleware(app.Audit).Handle)
	server.Use(middleware.NewRequestAuthorityMiddleware(c.Gateway.TrustProxyHeaders).Handle)
	handler.RegisterHandlers(server, ctx)
	handler.RegisterExtraHandlers(server, ctx)
	return server
}

// newHTTPSRestServer 构造 HTTPS listener（独立端口 + 证书）。
// c.Https.Port<=0 时返回 nil（不启用）；启用时证书校验失败直接返回错误（fail-fast）。
func newHTTPSRestServer(c config.Config, app *bootstrap.App, ctx *svc.ServiceContext) (*rest.Server, error) {
	if c.Https.Port <= 0 {
		return nil, nil
	}
	if err := config.ValidateHTTPSListener(c); err != nil {
		return nil, err
	}
	if err := config.ValidateTLSCert(c.Https.CertFile, c.Https.KeyFile); err != nil {
		return nil, fmt.Errorf("HTTPS listener requires a valid certificate: %w", err)
	}
	httpsConf := c
	httpsConf.RestConf.Port = c.Https.Port
	httpsConf.RestConf.CertFile = c.Https.CertFile
	httpsConf.RestConf.KeyFile = c.Https.KeyFile
	if strings.TrimSpace(httpsConf.RestConf.Name) != "" {
		httpsConf.RestConf.Name += "-https"
	}
	return newRestServerWithCtx(httpsConf, app, ctx), nil
}

func newLaunchpadServer(c config.Config, app *bootstrap.App, ctx *svc.ServiceContext) (*rest.Server, int) {
	port, ok := launchpadListenPort(c)
	if !ok {
		return nil, 0
	}
	launchpadConfig := c
	launchpadConfig.RestConf.Port = port
	if strings.TrimSpace(launchpadConfig.RestConf.Name) != "" {
		launchpadConfig.RestConf.Name += "-launchpad"
	}
	return newRestServerWithCtx(launchpadConfig, app, ctx), port
}

func launchpadListenPort(c config.Config) (int, bool) {
	value := strings.TrimSpace(c.Gateway.LaunchpadPort)
	if value == "" {
		return 0, false
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 {
		logx.Errorf("invalid LAUNCHPAD_PORT %q, standalone Launchpad listener disabled", value)
		return 0, false
	}
	if port == c.Port {
		logx.Infof("LAUNCHPAD_PORT matches main server port %d, standalone Launchpad listener disabled", port)
		return 0, false
	}
	return port, true
}
