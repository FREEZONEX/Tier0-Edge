package project

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/config"
	domainproject "backend/internal/domain/project"
	"backend/internal/repo"
	"backend/internal/service/tenantpg"
	"backend/internal/testkit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestAppNetworkAliasValidation(t *testing.T) {
	for _, alias := range []string{"", "order-api", "a", "a1", strings.Repeat("a", appNetworkAliasMaxLen)} {
		if err := validateAppNetworkAlias(alias); err != nil {
			t.Errorf("valid alias %q: %v", alias, err)
		}
	}
	for _, alias := range []string{"订单", "Order", "order_api", "-order", "order-", "a.b", "123", "backend", "sourceflow", "app-order", "tier0-db", strings.Repeat("a", appNetworkAliasMaxLen+1), "a\nnetworks:"} {
		if err := validateAppNetworkAlias(alias); err == nil {
			t.Errorf("invalid alias accepted: %q", alias)
		}
	}
	opts, err := buildImportOptions("", "inherit", nil, "", nil, " order-api ")
	if err != nil || opts.networkAlias != "order-api" {
		t.Fatalf("normalize alias: %+v %v", opts, err)
	}
}

// 显式启用的本机 Docker smoke；只创建独立网络与一次性容器，不发布宿主机端口。
func TestAppNetworkAliasDocker(t *testing.T) {
	if os.Getenv("TEST_APP_NETWORK_ALIAS_DOCKER") != "1" {
		t.Skip("opt-in Docker smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	suffix := fmt.Sprint(time.Now().UnixNano())
	network := "tier0-alias-test-" + suffix
	server := "tier0-alias-server-" + suffix
	run("network", "create", network)
	t.Cleanup(func() { _ = exec.Command("docker", "network", "rm", network).Run() })
	t.Setenv("RUNTIME_NETWORK_NAME", network)
	run("run", "-d", "--pull", "never", "--name", server, "--network", network, "--network-alias", "order-api", "node:22-alpine", "node", "-e", `require('http').createServer((req,res)=>res.end('alias-ok')).listen(3000,'0.0.0.0')`)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", server).Run() })
	got := run("run", "--rm", "--pull", "never", "--network", network, "node:22-alpine", "node", "-e", `fetch('http://order-api:3000/api/health').then(r=>r.text()).then(t=>{if(t!=='alias-ok')process.exit(1);console.log(t)}).catch(()=>process.exit(1))`)
	if got != "alias-ok" {
		t.Fatalf("alias HTTP call = %q", got)
	}
	if err := checkDockerNetworkAlias(ctx, "order-api", "other-app"); err == nil {
		t.Fatal("existing Docker alias was not rejected")
	}
	if err := checkDockerNetworkAlias(ctx, "order-api", server); err != nil {
		t.Fatal(err)
	}
	if err := checkDockerNetworkAlias(ctx, "unused-api", "other-app"); err != nil {
		t.Fatal(err)
	}
	run("stop", server)
	if err := checkDockerNetworkAlias(ctx, "order-api", "other-app"); err == nil {
		t.Fatal("stopped container alias was not rejected")
	}
}

func TestGenerateComposeYAML_NetworkAlias(t *testing.T) {
	generate := func(alias string) string {
		return generateComposeYAML("order", "uuid", "test", "node:22-alpine", 3000, 30123,
			"/app/data/apps/uuid", "", "", nil, nil, []string{"node", "server.mjs"}, "/health", false, 0,
			"app-order-uuid", nil, alias)
	}
	original := generate("")
	aliased := generate("order-api")
	aliasBlock := "    networks:\n      runtime:\n        aliases:\n          - \"order-api\"\n"
	if !strings.Contains(aliased, aliasBlock) {
		t.Fatal("missing runtime network alias")
	}
	// 唯一变化应为 network aliases：容器名、端口映射及其他部署设置不变。
	if strings.Replace(aliased, aliasBlock, "    networks:\n      - runtime\n", 1) != original {
		t.Fatal("alias changed unrelated compose configuration")
	}
	t.Run("compose-config", func(t *testing.T) {
		docker, err := exec.LookPath("docker")
		if err != nil {
			t.Skip("docker CLI unavailable")
		}
		path := filepath.Join(t.TempDir(), "compose.yml")
		if err := os.WriteFile(path, []byte(aliased), 0600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(docker, "compose", "-f", path, "config", "--quiet").CombinedOutput(); err != nil {
			t.Fatalf("compose config: %v: %s", err, out)
		}
	})
}

func TestAllocateHostPort_UsesConfiguredRange(t *testing.T) {
	if repo.GetCommonConn(context.Background()) == nil {
		t.Skip("common DB not initialized")
	}

	conf := config.Config{
		AppGateway: config.AppGatewayConfig{
			AppHostPortRangeStart: 30100,
			AppHostPortRangeEnd:   30102,
		},
	}

	port, err := allocateHostPort(context.Background(), conf)
	if err != nil {
		t.Fatalf("allocateHostPort failed: %v", err)
	}

	if port < conf.AppGateway.AppHostPortRangeStart || port > conf.AppGateway.AppHostPortRangeEnd {
		t.Fatalf("allocated host port %d out of range %d-%d", port, conf.AppGateway.AppHostPortRangeStart, conf.AppGateway.AppHostPortRangeEnd)
	}
}

func TestAppComposeCommand_DoesNotInheritInstallationProject(t *testing.T) {
	t.Setenv("COMPOSE_PROJECT_NAME", "enterprise")
	t.Setenv("RUNTIME_NETWORK_NAME", "enterprise_runtime")

	cmd := appComposeCommand(nil, "/app/data/apps/test-app-id/docker-compose.yml", "config")
	if got := strings.Join(cmd.Args, " "); got != "docker compose -f /app/data/apps/test-app-id/docker-compose.yml config" {
		t.Fatalf("unexpected app compose command: %s", got)
	}

	hasRuntimeNetwork := false
	for _, entry := range cmd.Env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if strings.EqualFold(key, "COMPOSE_PROJECT_NAME") {
			t.Fatalf("app compose command must not inherit installation project: %s", entry)
		}
		if key == "RUNTIME_NETWORK_NAME" && value == "enterprise_runtime" {
			hasRuntimeNetwork = true
		}
	}
	if !hasRuntimeNetwork {
		t.Fatal("app compose command should preserve unrelated runtime environment")
	}
}

func TestGenerateComposeYAML_HasHostPorts(t *testing.T) {
	compose := generateComposeYAML(
		"order-manager",
		"test-app-id",
		"test-installation-id",
		"node:20-alpine",
		8080,
		30123,
		"/app/data/apps/test-app-id",
		"postgres://user:pass@db/app?options=-c%20search_path%3Dapp_71",

		"app_71", map[string]string{"TIER0_API_KEY": "t0_key"},
		map[string]string{"FOO": "bar"},
		[]string{"node", "server.mjs"},
		"/health",
		false,
		0,
		"",
		nil,
	)

	if !strings.Contains(compose, "\n    ports:") {
		t.Fatalf("generated compose should expose host ports, got:\n%s", compose)
	}
	if !strings.Contains(compose, "30123:8080") {
		t.Fatalf("generated compose should map host port 30123 to container port 8080, got:\n%s", compose)
	}
	if !strings.Contains(compose, "container_name: app-order-manager-testappid") {
		t.Fatalf("generated compose should use container name app-order-manager-testappid, got:\n%s", compose)
	}
	if !strings.Contains(compose, "\n  app-order-manager-testappid:\n") {
		t.Fatalf("generated compose should use service name app-order-manager-testappid, got:\n%s", compose)
	}
	for _, label := range []string{
		`com.tier0.fleet.managed: "true"`,
		`com.tier0.fleet.installation-id: "test-installation-id"`,
		`com.tier0.fleet.service: "app-testappid"`,
		`com.tier0.ha.group: "active"`,
		`com.tier0.ha.start-order: "40"`,
		`com.tier0.fleet.app-name: "order-manager"`,
		`com.tier0.fleet.restartable: "true"`,
	} {
		if !strings.Contains(compose, label) {
			t.Fatalf("generated compose should contain Fleet label %q, got:\n%s", label, compose)
		}
	}
	for _, label := range []string{
		`com.tier0.fleet.visible:`,
		`com.tier0.fleet.hidden:`,
		`com.tier0.ha.health-required:`,
	} {
		if strings.Contains(compose, label) {
			t.Fatalf("generated compose should omit redundant Fleet label %q, got:\n%s", label, compose)
		}
	}
}

func TestGenerateComposeYAML_ArtifactEnvsOverridePlatformDefaults(t *testing.T) {
	compose := generateComposeYAML(
		"order-manager",
		"test-app-id",
		"test-installation-id",
		"node:20-alpine",
		8080,
		30123,
		"/app/data/apps/test-app-id",
		"postgres://user:pass@db/app",

		"app_71", map[string]string{
			"TIER0_API_KEY": "t0_platform_key",
			"CUSTOM_VAR":    "from_platform",
		},
		map[string]string{
			"CUSTOM_VAR": "from_artifact",
		},
		[]string{"node", "server.mjs"},
		"/health",
		false,
		0,
		"",
		nil,
	)

	// artifact.toml 中非强制变量应覆盖平台默认值
	if !strings.Contains(compose, "CUSTOM_VAR=from_artifact") {
		t.Fatalf("artifact.toml env should override platform default CUSTOM_VAR, got:\n%s", compose)
	}
	// 平台默认值不应再出现
	if strings.Contains(compose, "CUSTOM_VAR=from_platform") {
		t.Fatalf("artifact.toml override should replace platform default CUSTOM_VAR, got:\n%s", compose)
	}
	// TIER0_API_KEY 是平台强制变量，即使 artifact.toml 写了也应被覆盖
	if !strings.Contains(compose, "TIER0_API_KEY=t0_platform_key") {
		t.Fatalf("platform forced TIER0_API_KEY should override artifact.toml, got:\n%s", compose)
	}
}

func TestGenerateComposeYAML_ForcedPlatformEnvs(t *testing.T) {
	compose := generateComposeYAML(
		"order-manager",
		"test-app-id",
		"test-installation-id",
		"node:20-alpine",
		8080,
		30123,
		"/app/data/apps/test-app-id",
		"postgres://user:pass@db/app",

		"app_71", map[string]string{"TIER0_API_KEY": "t0_key"},
		map[string]string{
			"PORT":         "9999",
			"HOSTNAME":     "wrong-host",
			"DATABASE_URL": "wrong-db",
		},
		[]string{"node", "server.mjs"},
		"/health",
		false,
		0,
		"",
		nil,
	)

	// PORT 是平台强制变量，artifact.toml 不能覆盖
	if strings.Contains(compose, "PORT=9999") {
		t.Fatalf("artifact.toml should not override platform PORT, got:\n%s", compose)
	}
	if !strings.Contains(compose, "PORT=8080") {
		t.Fatalf("platform PORT=8080 should be preserved, got:\n%s", compose)
	}
	// HOSTNAME 同样强制
	if strings.Contains(compose, "HOSTNAME=wrong-host") {
		t.Fatalf("artifact.toml should not override platform HOSTNAME, got:\n%s", compose)
	}
	// DATABASE_URL 同样强制
	if strings.Contains(compose, "DATABASE_URL=wrong-db") {
		t.Fatalf("artifact.toml should not override platform DATABASE_URL, got:\n%s", compose)
	}
}

func TestGenerateComposeYAML_ForcedInternalEnvs(t *testing.T) {
	compose := generateComposeYAML(
		"order-manager",
		"test-app-id",
		"test-installation-id",
		"node:20-alpine",
		8080,
		30123,
		"/app/data/apps/test-app-id",
		"postgres://user:pass@db/app",

		"app_71", map[string]string{
			"APP_ID":           "test-app-id",
			"TIER0_PROJECT_ID": "71",
			"TIER0_API_KEY":    "t0_platform_key",
			"TIER0_API_HOST":   "backend:8080",
			"TIER0_MQTT_HOST":  "emqx",
			"TIER0_MQTT_PORT":  "8083",
		},
		map[string]string{
			"APP_ID":           "cloud-app-id",
			"TIER0_PROJECT_ID": "999",
			"TIER0_API_HOST":   "custom-backend:9090",
			"TIER0_MQTT_HOST":  "custom-mqtt:1883",
			"TIER0_MQTT_PORT":  "1883",
		},
		[]string{"node", "server.mjs"},
		"/health",
		false,
		0,
		"",
		nil,
	)

	if strings.Contains(compose, "APP_ID=cloud-app-id") {
		t.Fatalf("artifact.toml should not override platform APP_ID, got:\n%s", compose)
	}
	if count := strings.Count(compose, "APP_ID=test-app-id"); count != 1 {
		t.Fatalf("platform APP_ID should appear exactly once, got %d occurrences:\n%s", count, compose)
	}
	if strings.Contains(compose, "TIER0_PROJECT_ID=999") {
		t.Fatalf("artifact.toml should not override platform TIER0_PROJECT_ID, got:\n%s", compose)
	}
	if !strings.Contains(compose, "TIER0_PROJECT_ID=71") {
		t.Fatalf("platform TIER0_PROJECT_ID should be preserved, got:\n%s", compose)
	}
	if strings.Contains(compose, "TIER0_API_HOST=custom-backend:9090") {
		t.Fatalf("artifact.toml should not override platform TIER0_API_HOST, got:\n%s", compose)
	}
	if !strings.Contains(compose, "TIER0_API_HOST=backend:8080") {
		t.Fatalf("platform TIER0_API_HOST should be preserved, got:\n%s", compose)
	}
	if strings.Contains(compose, "TIER0_MQTT_HOST=custom-mqtt:1883") {
		t.Fatalf("artifact.toml should not override platform TIER0_MQTT_HOST, got:\n%s", compose)
	}
	if !strings.Contains(compose, "TIER0_MQTT_HOST=emqx") {
		t.Fatalf("platform TIER0_MQTT_HOST should be preserved, got:\n%s", compose)
	}
	if strings.Contains(compose, "TIER0_MQTT_PORT=1883") {
		t.Fatalf("artifact.toml should not override platform TIER0_MQTT_PORT, got:\n%s", compose)
	}
	if !strings.Contains(compose, "TIER0_MQTT_PORT=8083") {
		t.Fatalf("platform TIER0_MQTT_PORT should be preserved, got:\n%s", compose)
	}
}

func TestGenerateComposeYAML_InjectsAppIDWhenArtifactOmitsIt(t *testing.T) {
	compose := generateComposeYAML(
		"order-manager",
		"test-app-id",
		"test-installation-id",
		"node:20-alpine",
		8080,
		30123,
		"/app/data/apps/test-app-id",
		"",
		"",
		map[string]string{"APP_ID": "test-app-id"},
		map[string]string{"CUSTOM_ENV": "value"},
		nil,
		"/health",
		false,
		0,
		"",
		nil,
	)

	if count := strings.Count(compose, "APP_ID=test-app-id"); count != 1 {
		t.Fatalf("platform APP_ID should be injected exactly once, got %d occurrences:\n%s", count, compose)
	}
}

func TestGenerateComposeYAML_MqttUsesSdkConfig(t *testing.T) {
	compose := generateComposeYAML(
		"order-manager",
		"test-app-id",
		"test-installation-id",
		"node:20-alpine",
		8080,
		30123,
		"/app/data/apps/test-app-id",
		"postgres://user:pass@db/app",

		"app_71", map[string]string{
			"TIER0_API_KEY":   "t0_key",
			"TIER0_API_HOST":  "backend:8080",
			"TIER0_MQTT_HOST": "sdk-mqtt",
			"TIER0_MQTT_PORT": "1883",
		},
		map[string]string{
			"TIER0_MQTT_HOST": "artifact-mqtt",
			"TIER0_MQTT_PORT": "8883",
		},
		[]string{"node", "server.mjs"},
		"/health",
		false,
		0,
		"",
		nil,
	)

	if strings.Contains(compose, "TIER0_MQTT_HOST=artifact-mqtt") {
		t.Fatalf("artifact.toml should not override TIER0_MQTT_HOST, got:\n%s", compose)
	}
	if !strings.Contains(compose, "TIER0_MQTT_HOST=sdk-mqtt") {
		t.Fatalf("platform should use Tier0Sdk MQTT host, got:\n%s", compose)
	}
	if strings.Contains(compose, "TIER0_MQTT_PORT=8883") {
		t.Fatalf("artifact.toml should not override TIER0_MQTT_PORT, got:\n%s", compose)
	}
	if !strings.Contains(compose, "TIER0_MQTT_PORT=1883") {
		t.Fatalf("platform should use Tier0Sdk MQTT port, got:\n%s", compose)
	}
}

func TestGenerateComposeYAML_ReuseContainerName(t *testing.T) {
	compose := generateComposeYAML(
		"renamed-app",
		"test-app-id",
		"test-installation-id",
		"node:20-alpine",
		8080,
		30123,
		"/app/data/apps/test-app-id",
		"postgres://user:pass@db/app",
		"app_71", map[string]string{"TIER0_API_KEY": "t0_key"},
		map[string]string{},
		[]string{"node", "server.mjs"},
		"/health",
		false,
		0,
		"app-original-testappid",
		nil,
	)

	if !strings.Contains(compose, "container_name: app-original-testappid") {
		t.Fatalf("generated compose should reuse provided container name, got:\n%s", compose)
	}
	if !strings.Contains(compose, "\n  app-original-testappid:\n") {
		t.Fatalf("generated compose should use provided container name as service name, got:\n%s", compose)
	}
}

func TestParseDockerPublishedPorts(t *testing.T) {
	output := `0.0.0.0:30100->5173/tcp, [::]:30100->5173/tcp
0.0.0.0:30400-30499->30400-30499/tcp, [::]:30400-30499->30400-30499/tcp
5432/tcp
0.0.0.0:8555->8555/udp, 8555/tcp
0.0.0.0:20000-40000->20000-40000/tcp
`
	ports := parseDockerPublishedPorts(output)
	for _, want := range []int{30100, 30400, 30499, 8555, 20000, 25000, 40000} {
		if !ports[want] {
			t.Fatalf("expected port %d to be parsed, got %v", want, ports)
		}
	}
	if ports[5432] {
		t.Fatalf("unpublished container port 5432 should not be included, got %v", ports)
	}
	if len(parseDockerPublishedPorts("")) != 0 {
		t.Fatalf("empty output should yield no ports")
	}
}

func TestAppSchemaName(t *testing.T) {
	if got := appSchemaName(71); got != "app_71" {
		t.Fatalf("appSchemaName(71) = %q, want app_71", got)
	}
	if got := appSchemaName(0); got != "app_0" {
		t.Fatalf("appSchemaName(0) = %q, want app_0", got)
	}
}

// App connections must never inherit the platform administrator when the
// project-role secret is missing. These pure-function cases require no database.
func TestBuildAppDatabaseURLRequiresProjectRole(t *testing.T) {
	const baseDSN = "postgresql://platform_admin:admin-password@db.internal:5432/uns?sslmode=verify-full&application_name=app-test"
	for _, secret := range []string{"", " ", "\t\n"} {
		t.Run(fmt.Sprintf("missing_secret_%d", len(secret)), func(t *testing.T) {
			dsn, err := buildAppDatabaseURL(baseDSN, secret, "project_7", "app_71")
			if err == nil || dsn != "" {
				t.Fatal("missing role secret must reject App database credentials")
			}
			if !strings.Contains(err.Error(), "TENANT_PG_ROLE_SECRET") {
				t.Fatal("configuration error must identify the missing setting")
			}
			if strings.Contains(err.Error(), "admin-password") || strings.Contains(err.Error(), baseDSN) {
				t.Fatal("configuration error leaked database credentials")
			}
		})
	}

	const secret = "test-only-project-role-secret"
	dsn, err := buildAppDatabaseURL(baseDSN, secret, "project_7", "app_71")
	if err != nil {
		t.Fatal("configured App database connection was rejected")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("configured App database connection is invalid")
	}
	user, password, err := tenantpg.DeriveCredentials(secret, "project_7")
	if err != nil {
		t.Fatal(err)
	}
	actualPassword, hasPassword := u.User.Password()
	if u.User.Username() != user || !hasPassword || actualPassword != password || actualPassword == "admin-password" {
		t.Fatal("App connection did not use the project-specific credentials")
	}
	if u.Scheme != "postgresql" || u.Host != "db.internal:5432" || u.Path != "/project_7" || u.Query().Get("sslmode") != "verify-full" || u.Query().Get("application_name") != "app-test" || u.Query().Get("options") != "-c search_path=app_71,public" {
		t.Fatal("App connection changed its target, TLS settings, or schema")
	}
	repeated, err := buildAppDatabaseURL(baseDSN, secret, "project_7", "app_71")
	if err != nil || repeated != dsn {
		t.Fatal("the same role secret must produce a stable App connection")
	}
	other, err := buildAppDatabaseURL(baseDSN, secret, "project_8", "app_72")
	if err != nil {
		t.Fatal(err)
	}
	otherURL, err := url.Parse(other)
	if err != nil {
		t.Fatal(err)
	}
	otherPassword, _ := otherURL.User.Password()
	if otherURL.User.Username() == user || otherPassword == password {
		t.Fatal("different projects shared App credentials")
	}
	t.Run("query-credentials-cannot-override-project", func(t *testing.T) {
		queryBase := baseDSN + "&user=platform_admin&password=admin-password&dbname=uns"
		appDSN, buildErr := buildAppDatabaseURL(queryBase+"&service=untrusted&servicefile=/nonexistent-project-service&passfile=/nonexistent-project-passwords", secret, "project_7", "app_71")
		require.NoError(t, buildErr)
		parsed, parseErr := pgx.ParseConfig(appDSN)
		require.NoError(t, parseErr)
		require.True(t, parsed.User == user && parsed.Password == password && parsed.Database == "project_7", "driver must use the project identity, not base DSN query credentials")
		adminTarget, buildErr := tenantpg.BuildProjectConnDSN(queryBase, "project_7")
		require.NoError(t, buildErr)
		adminConfig, parseErr := pgx.ParseConfig(adminTarget)
		require.NoError(t, parseErr)
		require.True(t, adminConfig.User == "platform_admin" && adminConfig.Password == "admin-password" && adminConfig.Database == "project_7", "trusted DDL keeps admin credentials but must target the selected project database")
	})
}

// Both managed deployment entry points must reject missing credentials before
// filesystem, database, Docker, or replacement rollback work begins.
func TestManagedAppDeploymentRejectsMissingRoleBeforePreparation(t *testing.T) {
	t.Setenv("TIER0_INSTALLATION_ID", "test-app-role-guard")
	for _, replace := range []bool{false, true} {
		name := "new"
		if replace {
			name = "replace"
		}
		t.Run(name, func(t *testing.T) {
			for _, secret := range []string{"", " \t\n"} {
				conf := config.Config{}
				conf.TenantPG.RoleSecret = secret
				// An invalid alias safely rejects the old implementation before
				// Docker access, while proving configuration is checked first.
				opts := artifactDeployOptions{networkAlias: "INVALID"}
				var id, logs string
				var port int
				var err error
				if replace {
					id, port, logs, err = artifactRedeploy(context.Background(), conf, nil, "project_7", "test", map[string]any{}, "test-app", "", 7, 71, "existing-app-id", 30123, "existing-app", opts)
				} else {
					id, port, logs, err = artifactDeploy(context.Background(), conf, nil, "project_7", "test", map[string]any{}, "test-app", "", 7, 71, opts)
				}
				if err == nil || err.Error() != appRoleSecretRequired || id != "" || port != 0 || logs != "" {
					t.Fatal("managed deployment did not reject missing project-role configuration before preparation")
				}
			}
		})
	}
}

func TestCreateAppDatabase(t *testing.T) {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		t.Skip("common DB not initialized")
	}

	const testID = 999001
	schemaName := appSchemaName(testID)
	defer func() {
		_ = db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS \"%s\" CASCADE", schemaName)).Error
	}()

	baseURL := "postgres://user:pass@db/app"
	databaseURL, err := createLegacyAppDatabase(testID, baseURL)
	if err != nil {
		t.Fatalf("createAppDatabase failed: %v", err)
	}
	wantOption := "options=-c%20search_path%3D" + schemaName
	if !strings.Contains(databaseURL, wantOption) {
		t.Fatalf("databaseURL %q should contain %q", databaseURL, wantOption)
	}

	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?", schemaName).Scan(&count).Error; err != nil {
		t.Fatalf("query schema existence failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("schema %s not created, count=%d", schemaName, count)
	}
}

func TestEnsureSchemaMarkerTable(t *testing.T) {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		t.Skip("common DB not initialized")
	}

	const testID = 999002
	schemaName := appSchemaName(testID)
	defer func() {
		_ = db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS \"%s\" CASCADE", schemaName)).Error
	}()

	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS \"%s\"", schemaName)).Error; err != nil {
		t.Fatalf("create schema failed: %v", err)
	}
	if err := ensureLegacySchemaMarkerTable(schemaName); err != nil {
		t.Fatalf("ensureSchemaMarkerTable failed: %v", err)
	}

	var count int64
	sql := fmt.Sprintf("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = '%s' AND table_name = '__schema_marker'", schemaName)
	if err := db.Raw(sql).Scan(&count).Error; err != nil {
		t.Fatalf("query marker table existence failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("marker table not created in schema %s, count=%d", schemaName, count)
	}
}

func TestDropAppSchema(t *testing.T) {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		t.Skip("common DB not initialized")
	}

	const testID = 999003
	schemaName := appSchemaName(testID)
	defer func() {
		_ = db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS \"%s\" CASCADE", schemaName)).Error
	}()

	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS \"%s\"", schemaName)).Error; err != nil {
		t.Fatalf("create schema failed: %v", err)
	}
	if err := db.Exec(fmt.Sprintf("CREATE TABLE \"%s\".\"tmp\" (id int)", schemaName)).Error; err != nil {
		t.Fatalf("create temp table failed: %v", err)
	}

	if err := dropLegacyAppSchema(schemaName); err != nil {
		t.Fatalf("dropAppSchema failed: %v", err)
	}

	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?", schemaName).Scan(&count).Error; err != nil {
		t.Fatalf("query schema existence failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("schema %s should be dropped, count=%d", schemaName, count)
	}
}

func TestEnsureProjectDatabase(t *testing.T) {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		t.Skip("common DB not initialized")
	}

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	// 创建一个模拟旧项目的记录，database_name 为空
	project, err := repo.NewProjectRepo(ctx).CreateProject(ctx, repo.Project{
		Name:        "test-ensure-project-db",
		Description: "test",
		OwnerUserID: 1,
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := project.ID
	expectedDBName := fmt.Sprintf("project_%d", projectID)
	defer func() {
		_ = db.Exec("DELETE FROM project WHERE id = ?", projectID).Error
	}()

	registry, err := tenantpg.NewRegistry(tenantpg.Config{UnsDbUrl: dsn})
	if err != nil {
		t.Fatalf("create registry failed: %v", err)
	}
	defer func() { _ = registry.Close() }()

	lifecycle := tenantpg.NewLifecycle(registry, "")
	defer func() {
		_ = lifecycle.DropDatabase(ctx, expectedDBName)
	}()

	databaseName, err := ensureProjectDatabase(ctx, lifecycle, projectID)
	if err != nil {
		t.Fatalf("ensureProjectDatabase failed: %v", err)
	}
	if databaseName != expectedDBName {
		t.Fatalf("expected database name %q, got %q", expectedDBName, databaseName)
	}

	// 验证 project database 已被创建
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", expectedDBName).Scan(&count).Error; err != nil {
		t.Fatalf("query database existence failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("project database %s not created, count=%d", expectedDBName, count)
	}

	// 验证 project.database_name 已回写
	updated, err := repo.NewProjectRepo(ctx).GetProject(ctx, projectID)
	if err != nil {
		t.Fatalf("get project failed: %v", err)
	}
	if updated.DatabaseName != expectedDBName {
		t.Fatalf("expected project.database_name=%q, got %q", expectedDBName, updated.DatabaseName)
	}
}

func TestRedactDeployLogs(t *testing.T) {
	logs := `Error: DATABASE_URL=postgresql://user:secret@db/app?sslmode=disable
TIER0_API_KEY=ak_live_12345
{"DATABASE_URL":"postgresql://u:p@db"}`
	redacted := redactDeployLogs(logs)
	if strings.Contains(redacted, "secret") || strings.Contains(redacted, "ak_live_12345") || strings.Contains(redacted, ":p@") {
		t.Fatalf("sensitive values should be redacted, got: %s", redacted)
	}
	if !strings.Contains(redacted, "DATABASE_URL=***REDACTED***") {
		t.Fatalf("DATABASE_URL should be redacted, got: %s", redacted)
	}
	if !strings.Contains(redacted, "TIER0_API_KEY=***REDACTED***") {
		t.Fatalf("TIER0_API_KEY should be redacted, got: %s", redacted)
	}
}

func TestMain(m *testing.M) {
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		store, err := repo.Open(context.Background(), config.DatabaseConf{UnsDbUrl: dsn})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open test database: %v\n", err)
			os.Exit(1)
		}
		defer store.Close()
		// 多个包测试可能共用同一测试库：schema 已迁移时跳过迁移，避免重复迁移失败
		if !store.CommonDB().Migrator().HasTable("project_app") {
			if err := store.Migrate(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "failed to migrate test database: %v\n", err)
				os.Exit(1)
			}
		}
	}
	os.Exit(m.Run())
}

// 真实 PG 验证受管 App 项目角色；仅清理本用例随机创建的数据库和角色。
func TestManagedAppProjectRoleLifecycle(t *testing.T) {
	store, adminDSN := testkit.NewTestDB(t)
	admin := store.CommonDB()
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	ctx := context.Background()
	const secret = "owned-app-role-integration-key"
	registry, err := tenantpg.NewRegistry(tenantpg.Config{UnsDbUrl: adminDSN, RoleSecret: secret})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, registry.Close()) })
	lifecycle := tenantpg.NewLifecycle(registry, secret)
	project := func(t *testing.T, create bool) (string, string) {
		t.Helper()
		name := "app_role_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		role := tenantpg.DeriveRoleName(name)
		var exists bool
		require.NoError(t, admin.Raw(`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ?)`, role).Scan(&exists).Error)
		require.False(t, exists, "fixture role must not preexist")
		require.NoError(t, admin.Raw(`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = ?)`, name).Scan(&exists).Error)
		require.False(t, exists, "fixture database must not preexist")
		t.Cleanup(func() {
			_ = registry.CloseProjectConn(name)
			_, dropErr := adminSQL.ExecContext(context.Background(), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
			require.NoError(t, dropErr)
			// A wrong-target regression can leave grants in this testkit-owned
			// base database. Revoke only the fixture role's dependencies there.
			var roleExists bool
			require.NoError(t, admin.Raw(`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ?)`, role).Scan(&roleExists).Error)
			if roleExists {
				_, dropErr = adminSQL.ExecContext(context.Background(), `DROP OWNED BY "`+role+`"`)
				require.NoError(t, dropErr)
			}
			_, dropErr = adminSQL.ExecContext(context.Background(), `DROP ROLE IF EXISTS "`+role+`"`)
			require.NoError(t, dropErr)
		})
		if create {
			_, createErr := adminSQL.ExecContext(ctx, `CREATE DATABASE "`+name+`"`)
			require.NoError(t, createErr)
		}
		return name, role
	}
	schemaExists := func(t *testing.T, name, schema string) bool {
		t.Helper()
		db, closeDB, openErr := registry.GetAdminProjectDB(ctx, name)
		require.NoError(t, openErr)
		defer closeDB()
		var exists bool
		require.NoError(t, db.Raw(`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = ?)`, schema).Scan(&exists).Error)
		return exists
	}
	for _, existing := range []bool{true, false} {
		t.Run(fmt.Sprintf("existing-project-%t", existing), func(t *testing.T) {
			name, role := project(t, existing)
			if !existing {
				require.NoError(t, lifecycle.CreateDatabase(ctx, name))
			}
			require.NoError(t, lifecycle.CreateSchema(ctx, name, "app_orders"))
			require.NoError(t, lifecycle.CreateSchema(ctx, name, "app_orders"), "repeat must preserve credentials")
			dsn, buildErr := buildAppDatabaseURL(adminDSN, secret, name, "app_orders")
			require.NoError(t, buildErr)
			conn, connectErr := pgx.Connect(ctx, dsn)
			require.NoError(t, connectErr)
			defer conn.Close(ctx)
			var user string
			require.NoError(t, conn.QueryRow(ctx, `SELECT current_user`).Scan(&user))
			require.Equal(t, role, user)
			_, err = conn.Exec(ctx, `CREATE TABLE orders (id text PRIMARY KEY, amount numeric NOT NULL); INSERT INTO orders VALUES ('order-1', 10.25); UPDATE orders SET amount=11.25 WHERE id='order-1'`)
			require.NoError(t, err)
			var amount string
			require.NoError(t, conn.QueryRow(ctx, `SELECT amount::text FROM orders WHERE id='order-1'`).Scan(&amount))
			require.Equal(t, "11.25", amount)
			_, err = conn.Exec(ctx, `ALTER TABLE orders ADD COLUMN note text`)
			require.NoError(t, err)
			_, err = conn.Exec(ctx, `DELETE FROM orders WHERE id='order-1'`)
			require.NoError(t, err)
			_, err = conn.Exec(ctx, `ALTER ROLE "`+role+`" SUPERUSER`)
			require.Error(t, err, "App must not escalate itself")

			other, _ := project(t, false)
			require.NoError(t, lifecycle.CreateDatabase(ctx, other))
			require.NoError(t, lifecycle.CreateSchema(ctx, other, "app_private"))
			privateDSN, buildErr := buildAppDatabaseURL(adminDSN, secret, other, "app_private")
			require.NoError(t, buildErr)
			privateConn, privateErr := pgx.Connect(ctx, privateDSN)
			require.NoError(t, privateErr)
			defer privateConn.Close(ctx)
			_, privateErr = privateConn.Exec(ctx, `CREATE TABLE orders (id text PRIMARY KEY); INSERT INTO orders VALUES ('private-order')`)
			require.NoError(t, privateErr)
			otherDSN, buildErr := tenantpg.BuildProjectConnDSNWithCreds(adminDSN, other, role, tenantpg.DeriveRolePassword(secret, name))
			require.NoError(t, buildErr)
			otherConn, otherErr := pgx.Connect(ctx, otherDSN)
			if otherErr == nil { // PUBLIC CONNECT may exist; data access still must be denied.
				defer otherConn.Close(ctx)
				for _, query := range []string{
					`CREATE TABLE app_private.forbidden (id int)`,
					`SELECT * FROM app_private.orders`,
					`INSERT INTO app_private.orders VALUES ('forbidden')`,
					`UPDATE app_private.orders SET id='forbidden'`,
				} {
					_, otherErr = otherConn.Exec(ctx, query)
					require.Error(t, otherErr, "another project's data must not be accessible")
				}
			}
			require.Error(t, otherErr, "another project's schema must not be writable")
			var privateID string
			require.NoError(t, privateConn.QueryRow(ctx, `SELECT id FROM orders`).Scan(&privateID))
			require.Equal(t, "private-order", privateID)
		})
	}
	for _, flag := range []string{"SUPERUSER", "CREATEDB", "CREATEROLE", "REPLICATION", "BYPASSRLS", "NOLOGIN"} {
		t.Run("reject-"+flag, func(t *testing.T) {
			name, role := project(t, true)
			login := "LOGIN"
			if flag == "NOLOGIN" {
				login = ""
			}
			_, createErr := adminSQL.ExecContext(ctx, fmt.Sprintf(`CREATE ROLE "%s" %s PASSWORD '%s' %s`, role, login, tenantpg.DeriveRolePassword(secret, name), flag))
			require.NoError(t, createErr)
			roleErr := lifecycle.CreateSchema(ctx, name, "app_rejected")
			require.ErrorContains(t, roleErr, "unsafe project role")
			require.False(t, schemaExists(t, name, "app_rejected"))
		})
	}
	t.Run("query-base-targets-project", func(t *testing.T) {
		name, role := project(t, true)
		base, parseErr := url.Parse(adminDSN)
		require.NoError(t, parseErr)
		query := base.Query()
		query.Set("user", base.User.Username())
		password, _ := base.User.Password()
		query.Set("password", password)
		query.Set("dbname", strings.TrimPrefix(base.Path, "/"))
		base.Path = "/unused_base_database"
		base.RawQuery = query.Encode()
		queryRegistry, registryErr := tenantpg.NewRegistry(tenantpg.Config{UnsDbUrl: base.String(), RoleSecret: secret})
		require.NoError(t, registryErr)
		defer queryRegistry.Close()
		require.NoError(t, tenantpg.NewLifecycle(queryRegistry, secret).CreateSchema(ctx, name, "app_query"))
		require.True(t, schemaExists(t, name, "app_query"), "DDL must target the project, not base query dbname")
		dsn, buildErr := buildAppDatabaseURL(base.String(), secret, name, "app_query")
		require.NoError(t, buildErr)
		conn, connectErr := pgx.Connect(ctx, dsn)
		require.NoError(t, connectErr)
		defer conn.Close(ctx)
		var actualDatabase, actualUser string
		require.NoError(t, conn.QueryRow(ctx, `SELECT current_database(), current_user`).Scan(&actualDatabase, &actualUser))
		require.Equal(t, name, actualDatabase)
		require.Equal(t, role, actualUser)
	})
	t.Run("reject-role-membership", func(t *testing.T) {
		name, role := project(t, false)
		require.NoError(t, lifecycle.CreateDatabase(ctx, name))
		_, grantErr := adminSQL.ExecContext(ctx, `GRANT pg_read_all_data TO "`+role+`"`)
		require.NoError(t, grantErr)
		require.ErrorContains(t, lifecycle.CreateSchema(ctx, name, "app_rejected"), "unsafe project role")
		require.False(t, schemaExists(t, name, "app_rejected"))
		var membership bool
		require.NoError(t, admin.Raw(`SELECT pg_has_role(?, 'pg_read_all_data', 'MEMBER')`, role).Scan(&membership).Error)
		require.True(t, membership, "must reject, not silently remove existing privileges")
	})
	t.Run("concurrent-missing-role", func(t *testing.T) {
		name, _ := project(t, true)
		results := make(chan error, 4)
		for i := range 4 {
			go func(index int) { results <- lifecycle.CreateSchema(ctx, name, fmt.Sprintf("app_concurrent_%d", index)) }(i)
		}
		var failures []error
		for range 4 {
			if runErr := <-results; runErr != nil {
				failures = append(failures, runErr)
			}
		}
		require.Empty(t, failures)
		for i := range 4 {
			require.True(t, schemaExists(t, name, fmt.Sprintf("app_concurrent_%d", i)))
		}
	})
	t.Run("reject-credential-mismatch", func(t *testing.T) {
		name, role := project(t, false)
		require.NoError(t, lifecycle.CreateDatabase(ctx, name))
		var before, after string
		require.NoError(t, admin.Raw(`SELECT rolpassword FROM pg_authid WHERE rolname = ?`, role).Scan(&before).Error)
		wrong := tenantpg.NewLifecycle(registry, "different-owned-test-key")
		roleErr := wrong.CreateSchema(ctx, name, "app_rejected")
		require.ErrorContains(t, roleErr, "project role authentication failed")
		require.NotContains(t, roleErr.Error(), secret)
		require.NotContains(t, roleErr.Error(), tenantpg.DeriveRolePassword("different-owned-test-key", name))
		require.False(t, schemaExists(t, name, "app_rejected"))
		require.NoError(t, admin.Raw(`SELECT rolpassword FROM pg_authid WHERE rolname = ?`, role).Scan(&after).Error)
		require.Equal(t, before, after, "must not rotate existing role credentials")
	})
	t.Run("restore-contract-read-only", func(t *testing.T) {
		name, role := project(t, false)
		require.NoError(t, lifecycle.CreateDatabase(ctx, name))
		groupDB, groupRole := project(t, true)
		_, createErr := adminSQL.ExecContext(ctx, `CREATE ROLE "`+groupRole+`" NOLOGIN`)
		require.NoError(t, createErr)
		_, grantErr := adminSQL.ExecContext(ctx, `GRANT CONNECT ON DATABASE "`+name+`" TO "`+groupRole+`" WITH GRANT OPTION`)
		require.NoError(t, grantErr)
		dumpSQL := "-- synthetic input for digest binding only\nSELECT 1;\n"
		dumpHash := fmt.Sprintf("%x", sha256.Sum256([]byte(dumpSQL)))
		contract, exportErr := lifecycle.ExportRestoreContract(ctx, name, dumpHash)
		require.NoError(t, exportErr)
		require.Contains(t, contract.Roles, role)
		require.Contains(t, contract.Roles, groupRole, "database-only CONNECT dependencies must be included")
		require.Equal(t, dumpHash, contract.DumpSHA256)
		require.Equal(t, 2, contract.SchemaRevision)
		require.NotEmpty(t, contract.DatabaseOwner)
		require.Contains(t, contract.DatabaseACL, tenantpg.ProjectRestoreDatabaseGrant{Grantor: contract.DatabaseOwner, Grantee: groupRole, Privilege: "CONNECT", Grantable: true})
		require.NoError(t, contract.Validate())
		keyless := tenantpg.NewLifecycle(registry, "")
		keylessContract, exportErr := keyless.ExportRestoreContract(ctx, name, dumpHash)
		require.NoError(t, exportErr, "saving data must not require the role key")
		require.Equal(t, contract, keylessContract)
		encoded, encodeErr := json.Marshal(contract)
		require.NoError(t, encodeErr)
		require.NotContains(t, string(encoded), secret)
		require.NotContains(t, string(encoded), adminDSN)
		require.NotContains(t, string(encoded), tenantpg.DeriveRolePassword(secret, name))
		var passwordBefore, passwordAfter, aclBefore, aclAfter string
		require.NoError(t, admin.Raw(`SELECT rolpassword FROM pg_authid WHERE rolname=?`, role).Scan(&passwordBefore).Error)
		require.NoError(t, admin.Raw(`SELECT COALESCE(datacl::text,'') FROM pg_database WHERE datname=?`, name).Scan(&aclBefore).Error)
		result, checkErr := lifecycle.CheckRestoreContract(ctx, contract)
		require.NoError(t, checkErr)
		require.False(t, result.RequiresProjectRoleCreation)
		require.Equal(t, len(contract.Roles), result.RolesValidated)
		_, checkErr = keyless.CheckRestoreContract(ctx, contract)
		require.ErrorContains(t, checkErr, "project role secret")
		wrongKey := tenantpg.NewLifecycle(registry, "wrong-contract-fixture-key")
		_, checkErr = wrongKey.CheckRestoreContract(ctx, contract)
		require.ErrorContains(t, checkErr, "project role authentication failed")
		require.NotContains(t, checkErr.Error(), secret)
		_, changeErr := adminSQL.ExecContext(ctx, `ALTER ROLE "`+role+`" CREATEDB`)
		require.NoError(t, changeErr)
		_, checkErr = lifecycle.CheckRestoreContract(ctx, contract)
		require.ErrorContains(t, checkErr, "unsafe project role")
		var stillUnsafe bool
		require.NoError(t, admin.Raw(`SELECT rolcreatedb FROM pg_roles WHERE rolname=?`, role).Scan(&stillUnsafe).Error)
		require.True(t, stillUnsafe, "preflight must not silently repair a role")
		_, changeErr = adminSQL.ExecContext(ctx, `ALTER ROLE "`+role+`" NOCREATEDB`)
		require.NoError(t, changeErr) // Explicit fixture cleanup, not preflight behavior.
		t.Run("explicit-database-acl", func(t *testing.T) {
			_, err := adminSQL.ExecContext(ctx, `REVOKE CONNECT ON DATABASE "`+name+`" FROM "`+groupRole+`"`)
			require.NoError(t, err)
			changed, err := lifecycle.ExportRestoreContract(ctx, name, dumpHash)
			require.NoError(t, err)
			require.NotEqual(t, contract.DatabaseACL, changed.DatabaseACL)
			_, err = wrongKey.RestoreDatabaseACL(ctx, contract)
			require.Error(t, err)
			unchanged, err := lifecycle.ExportRestoreContract(ctx, name, dumpHash)
			require.NoError(t, err)
			require.Equal(t, changed.DatabaseACL, unchanged.DatabaseACL, "failed preflight must not change grants")
			result, err := lifecycle.RestoreDatabaseACL(ctx, contract)
			require.NoError(t, err)
			require.Equal(t, len(contract.DatabaseACL), result.GrantsRestored)
			restored, err := lifecycle.ExportRestoreContract(ctx, name, dumpHash)
			require.NoError(t, err)
			require.Equal(t, contract, restored, "owner, exact grants and grant options must survive")
			require.NoError(t, admin.Raw(`SELECT rolpassword FROM pg_authid WHERE rolname=?`, role).Scan(&passwordAfter).Error)
			require.Equal(t, passwordBefore, passwordAfter, "ACL restoration must not rotate credentials")
		})
		// The explicit mutation above may reorder pg_database.datacl without
		// changing grants. Freeze a new physical baseline for read-only checks.
		require.NoError(t, admin.Raw(`SELECT COALESCE(datacl::text,'') FROM pg_database WHERE datname=?`, name).Scan(&aclBefore).Error)
		for _, testCase := range []struct {
			name string
			edit func(*tenantpg.ProjectRestoreContract)
		}{
			{"revision", func(c *tenantpg.ProjectRestoreContract) { c.SchemaRevision++ }},
			{"database", func(c *tenantpg.ProjectRestoreContract) { c.Database = "bad/name" }},
			{"hash", func(c *tenantpg.ProjectRestoreContract) { c.DumpSHA256 = "bad" }},
			{"empty-roles", func(c *tenantpg.ProjectRestoreContract) { c.Roles = nil }},
			{"duplicate", func(c *tenantpg.ProjectRestoreContract) { c.Roles = []string{"same", "same"} }},
			{"unsorted", func(c *tenantpg.ProjectRestoreContract) { c.Roles = []string{"z", "a"} }},
			{"nul", func(c *tenantpg.ProjectRestoreContract) { c.Roles = []string{"bad\x00name"} }},
			{"role-length", func(c *tenantpg.ProjectRestoreContract) { c.Roles = []string{strings.Repeat("x", 64)} }},
			{"role-budget", func(c *tenantpg.ProjectRestoreContract) { c.Roles = make([]string, 4097) }},
			{"missing-owner", func(c *tenantpg.ProjectRestoreContract) { c.DatabaseOwner = "" }},
			{"undeclared-owner", func(c *tenantpg.ProjectRestoreContract) { c.DatabaseOwner = "unknown-owner" }},
			{"missing-acl", func(c *tenantpg.ProjectRestoreContract) { c.DatabaseACL = nil }},
			{"acl-privilege", func(c *tenantpg.ProjectRestoreContract) { c.DatabaseACL[0].Privilege = "ALL; SELECT 1" }},
			{"acl-grantee", func(c *tenantpg.ProjectRestoreContract) { c.DatabaseACL[0].Grantee = "unknown-grantee" }},
			{"acl-public-option", func(c *tenantpg.ProjectRestoreContract) {
				c.DatabaseACL[0].Grantee = ""
				c.DatabaseACL[0].Grantable = true
			}},
			{"acl-duplicate", func(c *tenantpg.ProjectRestoreContract) {
				c.DatabaseACL = append(c.DatabaseACL, c.DatabaseACL[len(c.DatabaseACL)-1])
			}},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				bad := contract
				bad.Roles = append([]string(nil), contract.Roles...)
				bad.DatabaseACL = append([]tenantpg.ProjectRestoreDatabaseGrant(nil), contract.DatabaseACL...)
				testCase.edit(&bad)
				_, err := lifecycle.CheckRestoreContract(ctx, bad)
				require.Error(t, err)
			})
		}
		t.Run("maintenance-cli", func(t *testing.T) {
			binary := os.Getenv("TEST_PROJECT_DB_MAINTENANCE_BINARY")
			if binary == "" {
				t.Skip("opt-in compiled Backend maintenance CLI")
			}
			run := func(action, database, body, key string, extra ...string) ([]byte, error) {
				args := append([]string{"-project-db-maintenance", action, "-project-database", database}, extra...)
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Dir = filepath.Join("..", "..", "..", "..")
				cmd.Env = append(os.Environ(), "UNS_DB_URL="+adminDSN, "TENANT_PG_ROLE_SECRET="+key)
				cmd.Stdin = strings.NewReader(body)
				return cmd.CombinedOutput()
			}
			out, runErr := run("export", name, dumpSQL, "")
			require.NoError(t, runErr, "export CLI: %s", out)
			var actual tenantpg.ProjectRestoreContract
			require.NoError(t, json.Unmarshal(out, &actual), "export stdout must be clean JSON")
			require.Equal(t, contract, actual)
			out, runErr = run("check", name, string(encoded), secret)
			require.NoError(t, runErr, "check CLI: %s", out)
			var checked tenantpg.ProjectRestoreCheck
			require.NoError(t, json.Unmarshal(out, &checked))
			require.Equal(t, result, checked)
			out, runErr = run("check", name, string(encoded), secret, "-project-dump-sha256", dumpHash)
			require.NoError(t, runErr, "actual file digest must match: %s", out)
			for _, hash := range []string{strings.Repeat("0", 64), "malformed-hash"} {
				out, runErr = run("check", name, string(encoded), secret, "-project-dump-sha256", hash)
				require.Error(t, runErr)
				require.Contains(t, string(out), "SQL SHA-256 does not match")
			}
			out, runErr = run("prepare", name, string(encoded), secret, "-project-db-restore-apply", "-project-dump-sha256", strings.Repeat("0", 64))
			require.Error(t, runErr)
			require.Contains(t, string(out), "SQL SHA-256 does not match")
			out, runErr = run("restore-acl", name, string(encoded), secret, "-project-db-restore-apply", "-project-dump-sha256", dumpHash)
			require.NoError(t, runErr, "explicit ACL CLI: %s", out)
			var aclResult tenantpg.ProjectRestoreACLResult
			require.NoError(t, json.Unmarshal(out, &aclResult))
			require.Equal(t, len(contract.DatabaseACL), aclResult.GrantsRestored)
			out, runErr = run("restore-acl", name, string(encoded), secret, "-project-db-restore-apply", "-project-dump-sha256", strings.Repeat("0", 64))
			require.Error(t, runErr)
			require.Contains(t, string(out), "SQL SHA-256 does not match")
			for _, body := range []string{string(encoded) + "{}", strings.TrimSuffix(string(encoded), "}") + `,"unknown":true}`, strings.Repeat("x", (1<<20)+1)} {
				_, runErr = run("check", name, body, secret)
				require.Error(t, runErr)
			}
			out, runErr = run("check", groupDB, string(encoded), secret)
			require.Error(t, runErr)
			require.NotContains(t, string(out), secret)
			_, runErr = run("export", name, "", "")
			require.Error(t, runErr)
			for _, args := range [][]string{
				{"-project-db-maintenance", "prepare", "-project-database", name},
				{"-project-db-maintenance", "restore-acl", "-project-database", name},
				{"-project-db-maintenance", "check", "-project-database", name, "-project-db-restore-apply"},
				{"-project-db-restore-apply"}, {"-project-database", name},
				{"-project-dump-sha256", dumpHash},
				{"-project-db-maintenance", "export", "-project-database", name, "-project-dump-sha256", dumpHash},
				{"-project-db-maintenance="},
				{"-project-db-maintenance", "check", "-project-database", name, "-migrate-only"},
			} {
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Stdin = strings.NewReader(string(encoded))
				out, runErr := cmd.CombinedOutput()
				require.Error(t, runErr)
				require.Contains(t, string(out), "invalid or conflicting")
			}
			for _, testCase := range []struct {
				dsn, timeout, expected string
			}{
				{"", "", "UNS_DB_URL is required"},
				{adminDSN, "not-a-duration", "invalid TENANT_PG_CONNECT_TIMEOUT"},
				{"postgres://test:never-log-this@%/db", "", "invalid database configuration"},
			} {
				cmd := exec.CommandContext(ctx, binary, "-project-db-maintenance", "check", "-project-database", name)
				cmd.Env = append(os.Environ(), "UNS_DB_URL="+testCase.dsn, "TENANT_PG_CONNECT_TIMEOUT="+testCase.timeout)
				cmd.Stdin = strings.NewReader(string(encoded))
				out, runErr := cmd.CombinedOutput()
				require.Error(t, runErr)
				require.Contains(t, string(out), testCase.expected)
				require.NotContains(t, string(out), "never-log-this")
			}
		})
		require.NoError(t, admin.Raw(`SELECT rolpassword FROM pg_authid WHERE rolname=?`, role).Scan(&passwordAfter).Error)
		require.NoError(t, admin.Raw(`SELECT COALESCE(datacl::text,'') FROM pg_database WHERE datname=?`, name).Scan(&aclAfter).Error)
		require.Equal(t, passwordBefore, passwordAfter)
		require.Equal(t, aclBefore, aclAfter, "read-only checks must not add grants")
		_, changeErr = adminSQL.ExecContext(ctx, `REVOKE CONNECT ON DATABASE "`+name+`" FROM "`+groupRole+`"`)
		require.NoError(t, changeErr)
		_, changeErr = adminSQL.ExecContext(ctx, `DROP ROLE "`+groupRole+`"`)
		require.NoError(t, changeErr)
		_, checkErr = lifecycle.CheckRestoreContract(ctx, contract)
		require.ErrorContains(t, checkErr, "is missing")
		_, checkErr = lifecycle.PrepareRestoreContract(ctx, contract)
		require.ErrorContains(t, checkErr, "is missing", "preparation must not guess unknown roles")
		var exists bool
		require.NoError(t, admin.Raw(`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname=?)`, groupRole).Scan(&exists).Error)
		require.False(t, exists, "unknown missing roles must not be created")
		require.NoError(t, lifecycle.CreateDatabase(ctx, groupDB))
		candidate, exportErr := lifecycle.ExportRestoreContract(ctx, groupDB, dumpHash)
		require.NoError(t, exportErr)
		_, changeErr = adminSQL.ExecContext(ctx, `DROP DATABASE "`+groupDB+`" WITH (FORCE)`)
		require.NoError(t, changeErr)
		_, changeErr = adminSQL.ExecContext(ctx, `DROP ROLE "`+groupRole+`"`)
		require.NoError(t, changeErr)
		_, checkErr = lifecycle.PrepareRestoreContract(ctx, candidate)
		require.ErrorContains(t, checkErr, "must already exist")
		_, changeErr = adminSQL.ExecContext(ctx, `CREATE DATABASE "`+groupDB+`"`)
		require.NoError(t, changeErr)
		missing, checkErr := lifecycle.CheckRestoreContract(ctx, candidate)
		require.NoError(t, checkErr)
		require.True(t, missing.RequiresProjectRoleCreation)
		require.NoError(t, admin.Raw(`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname=?)`, groupRole).Scan(&exists).Error)
		require.False(t, exists, "planned managed roles must not be created during preflight")
		t.Run("explicit-restore-preparation", func(t *testing.T) {
			prepared, prepareErr := lifecycle.PrepareRestoreContract(ctx, candidate)
			require.NoError(t, prepareErr)
			require.True(t, prepared.ProjectRolePrepared)
			var schemaCount int64
			projectDB, closeDB, openErr := registry.GetAdminProjectDB(ctx, groupDB)
			require.NoError(t, openErr)
			defer closeDB()
			require.NoError(t, projectDB.Raw(`SELECT count(*) FROM pg_namespace WHERE nspname NOT LIKE 'pg_%' AND nspname NOT IN ('public','information_schema')`).Scan(&schemaCount).Error)
			require.Zero(t, schemaCount, "role preparation must not create App schemas")
			_, changeErr = adminSQL.ExecContext(ctx, `REVOKE CONNECT ON DATABASE "`+groupDB+`" FROM PUBLIC, "`+groupRole+`"`)
			require.NoError(t, changeErr)
			var before, after string
			require.NoError(t, admin.Raw(`SELECT COALESCE(datacl::text,'') FROM pg_database WHERE datname=?`, groupDB).Scan(&before).Error)
			_, prepareErr = wrongKey.PrepareRestoreContract(ctx, candidate)
			require.ErrorContains(t, prepareErr, "authentication failed")
			require.NoError(t, admin.Raw(`SELECT COALESCE(datacl::text,'') FROM pg_database WHERE datname=?`, groupDB).Scan(&after).Error)
			require.Equal(t, before, after, "wrong key must not restore CONNECT")
			if binary := os.Getenv("TEST_PROJECT_DB_MAINTENANCE_BINARY"); binary != "" {
				data, err := json.Marshal(candidate)
				require.NoError(t, err)
				cmd := exec.CommandContext(ctx, binary, "-project-db-maintenance", "prepare", "-project-database", groupDB, "-project-db-restore-apply")
				cmd.Env = append(os.Environ(), "UNS_DB_URL="+adminDSN, "TENANT_PG_ROLE_SECRET="+secret)
				cmd.Stdin = strings.NewReader(string(data))
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, "explicit restore CLI: %s", out)
				var actual tenantpg.ProjectRestorePreparation
				require.NoError(t, json.Unmarshal(out, &actual))
				require.True(t, actual.ProjectRolePrepared)
			} else {
				_, prepareErr = lifecycle.PrepareRestoreContract(ctx, candidate)
				require.NoError(t, prepareErr)
			}
			_, checkErr = lifecycle.CheckRestoreContract(ctx, candidate)
			require.NoError(t, checkErr)
			projectDSN, dsnErr := tenantpg.BuildProjectConnDSNWithCreds(adminDSN, groupDB, groupRole, tenantpg.DeriveRolePassword(secret, groupDB))
			require.NoError(t, dsnErr)
			conn, connectErr := pgx.Connect(ctx, projectDSN)
			require.NoError(t, connectErr, "restored CONNECT must work with real restricted credentials")
			require.NoError(t, conn.Close(ctx))
		})
	})
	t.Run("restore-acl-transaction-rollback", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		name, managedRole := project(t, false)
		require.NoError(t, lifecycle.CreateDatabase(ctx, name))
		_, groupRole := project(t, true)
		_, err := adminSQL.ExecContext(ctx, `CREATE ROLE "`+groupRole+`" NOLOGIN`)
		require.NoError(t, err)
		_, err = adminSQL.ExecContext(ctx, `GRANT CONNECT ON DATABASE "`+name+`" TO "`+groupRole+`"`)
		require.NoError(t, err)
		contract, err := lifecycle.ExportRestoreContract(ctx, name, strings.Repeat("a", 64))
		require.NoError(t, err)
		_, err = adminSQL.ExecContext(ctx, `REVOKE CONNECT ON DATABASE "`+name+`" FROM "`+groupRole+`"`)
		require.NoError(t, err)
		before, err := lifecycle.ExportRestoreContract(ctx, name, contract.DumpSHA256)
		require.NoError(t, err)
		var passwordBefore, passwordAfter string
		require.NoError(t, admin.Raw(`SELECT rolpassword FROM pg_authid WHERE rolname=?`, managedRole).Scan(&passwordBefore).Error)

		// Hold the real provisioning lock. Once the maintenance connection is
		// visibly waiting for it, its read-only preflight has already succeeded.
		gate, err := pgx.Connect(ctx, adminDSN)
		require.NoError(t, err)
		defer gate.Close(context.Background())
		key := "tier0-project-role:" + managedRole
		_, err = gate.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, key)
		require.NoError(t, err)
		base, err := url.Parse(adminDSN)
		require.NoError(t, err)
		applicationName := "restore_acl_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		query := base.Query()
		query.Set("application_name", applicationName)
		base.RawQuery = query.Encode()
		isolated, err := tenantpg.NewRegistry(tenantpg.Config{UnsDbUrl: base.String(), RoleSecret: secret})
		require.NoError(t, err)
		defer isolated.Close()
		done := make(chan error, 1)
		go func() {
			_, err := tenantpg.NewLifecycle(isolated, secret).RestoreDatabaseACL(ctx, contract)
			done <- err
		}()
		defer func() {
			cancel()
			_, _ = gate.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key)
		}()
		require.Eventually(t, func() bool {
			var waiting bool
			if err := admin.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=? AND wait_event_type='Lock' AND query LIKE '%pg_advisory_xact_lock%')`, applicationName).Scan(&waiting).Error; err != nil {
				return false
			}
			return waiting
		}, 10*time.Second, 20*time.Millisecond)
		// Remove only our unreferenced fixture role after preflight, forcing an
		// actual GRANT failure after the target transaction has revoked grants.
		_, err = adminSQL.ExecContext(ctx, `DROP ROLE "`+groupRole+`"`)
		require.NoError(t, err)
		_, err = gate.Exec(ctx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key)
		require.NoError(t, err)
		select {
		case err := <-done:
			require.ErrorContains(t, err, "cannot restore declared database privilege")
		case <-ctx.Done():
			t.Fatal("ACL restore did not finish within its bounded context")
		}
		after, err := lifecycle.ExportRestoreContract(ctx, name, contract.DumpSHA256)
		require.NoError(t, err)
		require.Equal(t, before, after, "failed grant replay must roll back all target ACL mutations")
		require.NoError(t, admin.Raw(`SELECT rolpassword FROM pg_authid WHERE rolname=?`, managedRole).Scan(&passwordAfter).Error)
		require.Equal(t, passwordBefore, passwordAfter)
	})
	for _, preserveOwnership := range []bool{false, true} {
		caseName := "dump-restore-neutral-permissions"
		if preserveOwnership {
			caseName = "dump-restore-native-ownership"
		}
		t.Run(caseName, func(t *testing.T) {
			if os.Getenv("TEST_APP_DUMP_RESTORE_DOCKER") != "1" {
				t.Skip("opt-in real local PostgreSQL dump/restore")
			}
			ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			base, parseErr := url.Parse(adminDSN)
			require.NoError(t, parseErr)
			require.True(t, base.Hostname() == "127.0.0.1" && base.Port() == "55432" && base.User.Username() == "test", "dump fixture requires the isolated local test PostgreSQL")
			name, role := project(t, false)
			require.NoError(t, lifecycle.CreateDatabase(ctx, name))
			require.NoError(t, lifecycle.CreateSchema(ctx, name, "app_recovery"))
			dsn, buildErr := buildAppDatabaseURL(adminDSN, secret, name, "app_recovery")
			require.NoError(t, buildErr)
			conn, connectErr := pgx.Connect(ctx, dsn)
			require.NoError(t, connectErr)
			_, writeErr := conn.Exec(ctx, `CREATE TABLE recovery_orders (id text PRIMARY KEY, amount numeric NOT NULL); INSERT INTO recovery_orders VALUES ('restore-1', 123456789012345.67), ('restore-2', 0.01)`)
			require.NoError(t, writeErr)
			_, writeErr = conn.Exec(ctx, `CREATE TYPE order_state AS ENUM ('new', 'done');
				CREATE TABLE sql_verbatim (value text PRIMARY KEY);
				INSERT INTO sql_verbatim VALUES ('DROP lane');
				CREATE FUNCTION verbatim_function() RETURNS text LANGUAGE plpgsql AS $$
BEGIN
CREATE TEMP TABLE verbatim_temporary(value text);
DROP TABLE verbatim_temporary;
RETURN 'DROP preserved';
END
$$;
				CREATE SEQUENCE order_sequence START 10;
				CREATE TABLE recovery_lines (id bigserial PRIMARY KEY, order_id text REFERENCES recovery_orders(id));
				CREATE VIEW recovery_totals AS SELECT sum(amount) AS amount FROM recovery_orders;
				CREATE FUNCTION order_total() RETURNS numeric LANGUAGE SQL AS 'SELECT sum(amount) FROM app_recovery.recovery_orders';
				CREATE PROCEDURE change_amount(order_id text, new_amount numeric) LANGUAGE SQL AS 'UPDATE app_recovery.recovery_orders SET amount=new_amount WHERE id=order_id'`)
			require.NoError(t, writeErr)
			require.NoError(t, conn.Close(ctx))
			dockerArgs := []string{"--host", "unix:///var/run/docker.sock", "exec", "tier0-source-stream-test-pg"}
			dumpArgs := append(dockerArgs, "pg_dump", "-U", "test", "-d", name)
			if !preserveOwnership {
				dumpArgs = append(dumpArgs, "--no-owner", "--no-privileges")
			}
			dumpCmd := exec.CommandContext(ctx, "docker", dumpArgs...)
			dump, dumpErr := dumpCmd.Output()
			require.NoError(t, dumpErr)
			require.Contains(t, string(dump), "recovery_orders")
			var nativeContract tenantpg.ProjectRestoreContract
			if preserveOwnership {
				nativeContract, dumpErr = lifecycle.ExportRestoreContract(ctx, name, fmt.Sprintf("%x", sha256.Sum256(dump)))
				require.NoError(t, dumpErr)
				if binary := os.Getenv("TEST_PROJECT_DB_MAINTENANCE_BINARY"); binary != "" {
					cmd := exec.CommandContext(ctx, binary, "-project-db-maintenance", "export", "-project-database", name)
					cmd.Env = append(os.Environ(), "UNS_DB_URL="+adminDSN, "TENANT_PG_ROLE_SECRET=")
					cmd.Stdin = strings.NewReader(string(dump))
					out, err := cmd.CombinedOutput()
					require.NoError(t, err, "actual native dump metadata: %s", out)
					var actual tenantpg.ProjectRestoreContract
					require.NoError(t, json.Unmarshal(out, &actual))
					require.Equal(t, nativeContract, actual)
					projectDB, closeDB, err := registry.GetAdminProjectDB(ctx, name)
					require.NoError(t, err)
					var count int64
					err = projectDB.Raw(`SELECT count(*) FROM app_recovery.recovery_orders`).Scan(&count).Error
					closeDB()
					require.NoError(t, err)
					require.EqualValues(t, 2, count, "export must hash the SQL, never execute its DROP/CREATE/INSERT")
				}
			}
			require.Contains(t, string(dump), "\nDROP lane\n")
			require.Contains(t, string(dump), "\nDROP TABLE verbatim_temporary;")
			_, dropErr := adminSQL.ExecContext(ctx, `DROP DATABASE "`+name+`" WITH (FORCE)`)
			require.NoError(t, dropErr)
			if preserveOwnership {
				_, dropErr = adminSQL.ExecContext(ctx, `DROP ROLE "`+role+`"`)
				require.NoError(t, dropErr)
				preflight, err := lifecycle.CheckRestoreContract(ctx, nativeContract)
				require.NoError(t, err)
				require.True(t, preflight.RequiresProjectRoleCreation)
			}
			_, createErr := adminSQL.ExecContext(ctx, `CREATE DATABASE "`+name+`"`)
			require.NoError(t, createErr)
			if preserveOwnership {
				prepared, err := lifecycle.PrepareRestoreContract(ctx, nativeContract)
				require.NoError(t, err)
				require.True(t, prepared.ProjectRolePrepared)
			}
			restoreCmd := exec.CommandContext(ctx, "docker", "--host", "unix:///var/run/docker.sock", "exec", "-i", "tier0-source-stream-test-pg", "psql", "-X", "-U", "test", "-d", name, "-v", "ON_ERROR_STOP=1")
			restoreCmd.Stdin = bytes.NewReader(dump)
			restoreOutput, restoreErr := restoreCmd.CombinedOutput()
			require.NoError(t, restoreErr, "synthetic SQL restore: %s", restoreOutput)
			if preserveOwnership {
				_, err := lifecycle.RestoreDatabaseACL(ctx, nativeContract)
				require.NoError(t, err)
				actual, err := lifecycle.ExportRestoreContract(ctx, name, nativeContract.DumpSHA256)
				require.NoError(t, err)
				require.Equal(t, nativeContract.DatabaseACL, actual.DatabaseACL)
			}
			projectAdmin, closeAdmin, adminErr := registry.GetAdminProjectDB(ctx, name)
			require.NoError(t, adminErr)
			defer closeAdmin()
			var count int64
			require.NoError(t, projectAdmin.Raw(`SELECT count(*) FROM app_recovery.recovery_orders`).Scan(&count).Error)
			require.EqualValues(t, 2, count, "business data must actually be imported")
			var verbatim string
			require.NoError(t, projectAdmin.Raw(`SELECT value FROM app_recovery.sql_verbatim`).Scan(&verbatim).Error)
			require.Equal(t, "DROP lane", verbatim, "COPY payload must not be interpreted as cleanup SQL")
			require.NoError(t, projectAdmin.Raw(`SELECT app_recovery.verbatim_function()`).Scan(&verbatim).Error)
			require.Equal(t, "DROP preserved", verbatim, "function body must remain intact")
			// Normal preparation alone must not be mistaken for restored table
			// ownership. Exercise it before the restored App's real operations.
			require.NoError(t, lifecycle.CreateSchema(ctx, name, "app_recovery"))
			var restored struct {
				SchemaUsage bool
				TableRead   bool
				TableOwner  bool
			}
			require.NoError(t, projectAdmin.Raw(`SELECT has_schema_privilege(?, 'app_recovery', 'USAGE') AS schema_usage,
			has_table_privilege(?, 'app_recovery.recovery_orders', 'SELECT') AS table_read,
			pg_get_userbyid(c.relowner)=? AS table_owner FROM pg_class c
			JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='app_recovery' AND c.relname='recovery_orders'`, role, role, role).Scan(&restored).Error)
			t.Logf("restored rows=%d schemaUsage=%t tableRead=%t ownerMatchesRuntime=%t", count, restored.SchemaUsage, restored.TableRead, restored.TableOwner)
			conn, connectErr = pgx.Connect(ctx, dsn)
			require.NoError(t, connectErr, "login alone is not restored business access")
			defer conn.Close(ctx)
			var actualDatabase, actualUser string
			require.NoError(t, conn.QueryRow(ctx, `SELECT current_database(), current_user`).Scan(&actualDatabase, &actualUser))
			require.Equal(t, name, actualDatabase)
			require.Equal(t, role, actualUser)
			var amount string
			readErr := conn.QueryRow(ctx, `SELECT amount::text FROM recovery_orders WHERE id='restore-1'`).Scan(&amount)
			if !preserveOwnership {
				// This is the demonstrated v2 limitation, not successful App restore.
				require.Error(t, readErr, "neutral dump cannot recover App ownership/ACL")
				require.True(t, restored.SchemaUsage)
				require.False(t, restored.TableRead)
				require.False(t, restored.TableOwner)
				return
			}
			require.NoError(t, readErr, "normal App must read its restored business table")
			require.True(t, restored.TableRead)
			require.True(t, restored.TableOwner)
			require.Equal(t, "123456789012345.67", amount)
			_, alterErr := conn.Exec(ctx, `ALTER TABLE recovery_orders ADD COLUMN restored_note text`)
			require.NoError(t, alterErr, "normal App migrations must work after restore")
			var total string
			require.NoError(t, conn.QueryRow(ctx, `SELECT amount::text FROM recovery_totals`).Scan(&total))
			require.Equal(t, "123456789012345.68", total)
			require.NoError(t, conn.QueryRow(ctx, `SELECT order_total()::text`).Scan(&total))
			require.Equal(t, "123456789012345.68", total)
			var next int64
			require.NoError(t, conn.QueryRow(ctx, `SELECT nextval('order_sequence')`).Scan(&next))
			require.EqualValues(t, 10, next)
			_, updateErr := conn.Exec(ctx, `INSERT INTO recovery_lines(order_id) VALUES ('restore-1');
				ALTER TYPE order_state ADD VALUE 'cancelled';
				CALL change_amount('restore-2', 2.01)`)
			require.NoError(t, updateErr, "restored types, serial sequences and procedures must remain usable")
			require.NoError(t, conn.QueryRow(ctx, `SELECT amount::text FROM recovery_orders WHERE id='restore-2'`).Scan(&amount))
			require.Equal(t, "2.01", amount)
		})
	}
}

func TestResolveDeployFailureAction(t *testing.T) {
	cases := []struct {
		name       string
		switched   bool
		oldStopped bool
		want       deployFailureAction
	}{
		// 准备阶段失败：仅清理旁路目录，旧版本未受影响
		{"prepare failure keeps old version untouched", false, false, deployFailureCleanupWorkDir},
		// down 成功但目录交换未完成：仅用旧 compose 重启旧版本，不动目录
		{"old stopped before dir switch restarts old version", false, true, deployFailureRestartOld},
		// 目录交换完成后失败：完整回退到旧版本目录与旧镜像
		{"switched failure rolls back to old version", true, true, deployFailureRollback},
		{"switched failure rolls back even without oldStopped flag", true, false, deployFailureRollback},
	}
	for _, c := range cases {
		if got := resolveDeployFailureAction(c.switched, c.oldStopped); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestDockerImageChanged(t *testing.T) {
	cases := []struct {
		name     string
		beforeID string
		pulledID string
		want     bool
	}{
		{name: "newly pulled image", pulledID: "sha256:new", want: true},
		{name: "mutable tag updated", beforeID: "sha256:old", pulledID: "sha256:new", want: true},
		{name: "pull resolved to same image", beforeID: "sha256:same", pulledID: "sha256:same", want: false},
		{name: "pull did not complete", beforeID: "sha256:old", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dockerImageChanged(tc.beforeID, tc.pulledID); got != tc.want {
				t.Fatalf("dockerImageChanged(%q, %q) = %v, want %v", tc.beforeID, tc.pulledID, got, tc.want)
			}
		})
	}
}

func TestAppMigrationNeeded(t *testing.T) {
	tmpDir := t.TempDir()
	if appMigrationNeeded(tmpDir, "postgres://u:p@db/project_1") {
		t.Fatal("no drizzle config: migration should not be needed")
	}
	if appMigrationNeeded(tmpDir, "") {
		t.Fatal("empty database URL: migration should not be needed")
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "drizzle.config.ts"), []byte("export default {}"), 0o644); err != nil {
		t.Fatalf("write drizzle config: %v", err)
	}
	if !appMigrationNeeded(tmpDir, "postgres://u:p@db/project_1") {
		t.Fatal("drizzle config present with database URL: migration should be needed")
	}
}

func TestParseArtifactConfig(t *testing.T) {
	cases := []struct {
		name       string
		manifest   map[string]any
		wantImage  string
		wantPort   int
		wantHealth string
	}{
		{
			name:       "旧版顶层 image/port/health_path 正确映射（int port）",
			manifest:   map[string]any{"image": "demo:latest", "port": 3000, "health_path": "health"},
			wantImage:  "demo:latest",
			wantPort:   3000,
			wantHealth: "/health",
		},
		{
			name:       "旧版 float64 port 兼容",
			manifest:   map[string]any{"image": "demo:latest", "port": float64(8080), "health_path": "/hc"},
			wantImage:  "demo:latest",
			wantPort:   8080,
			wantHealth: "/hc",
		},
		{
			name: "新版 services.production 优先于顶层",
			manifest: map[string]any{
				"image":       "top:1",
				"port":        8080,
				"health_path": "/top",
				"services": []any{
					map[string]any{
						"localPort": 3000,
						"production": map[string]any{
							"run": map[string]any{
								"image": "svc:2",
								"args":  []any{"node", "main.js"},
								"env":   map[string]any{"K": "V"},
							},
							"health": map[string]any{
								"startup": map[string]any{"path": "/svc-health"},
							},
						},
					},
				},
			},
			wantImage:  "svc:2",
			wantPort:   3000,
			wantHealth: "/svc-health",
		},
		{
			name:       "缺省值：端口 8080、health /",
			manifest:   map[string]any{},
			wantImage:  "",
			wantPort:   8080,
			wantHealth: "/",
		},
	}
	for _, c := range cases {
		got := parseArtifactConfig(c.manifest)
		if got.Image != c.wantImage {
			t.Errorf("%s: Image = %q, want %q", c.name, got.Image, c.wantImage)
		}
		if got.ContainerPort != c.wantPort {
			t.Errorf("%s: ContainerPort = %d, want %d", c.name, got.ContainerPort, c.wantPort)
		}
		if got.HealthPath != c.wantHealth {
			t.Errorf("%s: HealthPath = %q, want %q", c.name, got.HealthPath, c.wantHealth)
		}
	}
}

// TestParseArtifactConfig_StartupTimeout 解析 health.startup.timeout（EIMP-057）：
// 声明正整数时进入配置，未声明/非法值保持 0（走默认超时）。
func TestParseArtifactConfig_StartupTimeout(t *testing.T) {
	withTimeout := func(timeout any) map[string]any {
		return map[string]any{
			"services": []any{
				map[string]any{
					"production": map[string]any{
						"run": map[string]any{"image": "svc:1"},
						"health": map[string]any{
							"startup": map[string]any{"path": "/hc", "timeout": timeout},
						},
					},
				},
			},
		}
	}
	if got := parseArtifactConfig(withTimeout(5)); got.StartupTimeout != 5 {
		t.Errorf("StartupTimeout = %d, want 5（int）", got.StartupTimeout)
	}
	if got := parseArtifactConfig(withTimeout(float64(120))); got.StartupTimeout != 120 {
		t.Errorf("StartupTimeout = %d, want 120（float64）", got.StartupTimeout)
	}
	if got := parseArtifactConfig(withTimeout(0)); got.StartupTimeout != 0 {
		t.Errorf("StartupTimeout = %d, want 0（非正值按未声明）", got.StartupTimeout)
	}
	if got := parseArtifactConfig(map[string]any{}); got.StartupTimeout != 0 {
		t.Errorf("StartupTimeout = %d, want 0（未声明）", got.StartupTimeout)
	}
}

// TestGenerateComposeYAML_StartupTimeoutDrivesStartPeriod 声明 startup.timeout 时
// compose start_period 按声明值生成；node_modules 安装的 180s 默认不覆盖声明值。
func TestGenerateComposeYAML_StartupTimeoutDrivesStartPeriod(t *testing.T) {
	compose := generateComposeYAML(
		"order-manager",
		"test-app-id",
		"test-installation-id",
		"node:20-alpine",
		8080,
		30123,
		"/app/data/apps/test-app-id",
		"postgres://user:pass@db/app",
		"app_71", map[string]string{"TIER0_API_KEY": "t0_key"},
		map[string]string{},
		[]string{"node", "server.mjs"},
		"/health",
		false,
		5,
		"",
		nil,
	)
	if !strings.Contains(compose, "start_period: 5s") {
		t.Fatalf("start_period should follow declared startup.timeout=5, got:\n%s", compose)
	}
}

func TestValidateArtifactManifest(t *testing.T) {
	cases := []struct {
		name     string
		manifest map[string]any
		wantErr  bool
		wantSub  string
	}{
		{
			name:     "image 缺失拒绝",
			manifest: map[string]any{"port": 8080},
			wantErr:  true,
			wantSub:  "缺少镜像",
		},
		{
			name:     "port=0 拒绝",
			manifest: map[string]any{"image": "demo:latest", "port": 0},
			wantErr:  true,
			wantSub:  "端口 0",
		},
		{
			name:     "port=65536 拒绝",
			manifest: map[string]any{"image": "demo:latest", "port": 65536},
			wantErr:  true,
			wantSub:  "65536",
		},
		{
			name: "新版 localPort 越界拒绝",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"localPort": 70000,
						"production": map[string]any{
							"run": map[string]any{"image": "svc:2"},
						},
					},
				},
			},
			wantErr: true,
			wantSub: "70000",
		},
		{
			name: "声明 build 不阻断 manifest 结构校验（产物由 validateArtifactBuiltOutput 判定）",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"localPort": 3000,
						"production": map[string]any{
							"build": map[string]any{
								"args":      []any{"sh", "-c", "npm ci && npm run build"},
								"outputDir": "production",
							},
							"run": map[string]any{"image": "node:22-alpine"},
							"health": map[string]any{
								"startup": map[string]any{"path": "/api/health"},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "声明 build 不阻断 manifest 校验（产物由 validateArtifactBuiltOutput 判定）",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"localPort": 5173,
						"production": map[string]any{
							"build": map[string]any{
								"args": []any{"sh", "-c", "npm ci && npm run build"},
							},
							"run": map[string]any{"image": "node:22-alpine"},
							"health": map[string]any{
								"startup": map[string]any{"path": "/api/health"},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "合法配置通过（新版 services）",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"localPort": 3000,
						"production": map[string]any{
							"run": map[string]any{"image": "svc:2"},
							"health": map[string]any{
								"startup": map[string]any{"path": "/health"},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "build.args 为空不视为构建步骤",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"localPort": 3000,
						"production": map[string]any{
							"build": map[string]any{
								"args":      []any{},
								"outputDir": "production",
							},
							"run": map[string]any{"image": "svc:2"},
							"health": map[string]any{
								"startup": map[string]any{"path": "/health"},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:     "health_path 无 / 自动补 / 后通过",
			manifest: map[string]any{"image": "demo:latest", "port": 8080, "health_path": "health"},
			wantErr:  false,
		},
		{
			name:     "缺省 port 沿用 8080 通过",
			manifest: map[string]any{"image": "demo:latest"},
			wantErr:  false,
		},
		{
			name:     "nil manifest 拒绝",
			manifest: nil,
			wantErr:  true,
			wantSub:  "缺失",
		},
	}
	for _, c := range cases {
		err := validateArtifactManifest(c.manifest)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got nil", c.name)
				continue
			}
			// 非法包必须包装 ErrUnsupportedPackageFormat（logicx 映射 HTTP 400），而非透传 500
			if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
				t.Errorf("%s: error %v 应包装 ErrUnsupportedPackageFormat（HTTP 400）", c.name, err)
			}
			if c.wantSub != "" && !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("%s: error %q does not contain %q", c.name, err.Error(), c.wantSub)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
		}
	}
}

func TestArtifactRunEntry(t *testing.T) {
	cases := []struct {
		name     string
		manifest map[string]any
		want     string
	}{
		{
			name: "node server.mjs",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"production": map[string]any{
							"run": map[string]any{"args": []any{"node", "server.mjs"}},
						},
					},
				},
			},
			want: "server.mjs",
		},
		{
			name: "python app.py",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"production": map[string]any{
							"run": map[string]any{"args": []any{"python3", "app.py"}},
						},
					},
				},
			},
			want: "app.py",
		},
		{
			name: "node 带解释器选项跳过",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"production": map[string]any{
							"run": map[string]any{"args": []any{"node", "--enable-source-maps", "server.mjs"}},
						},
					},
				},
			},
			want: "server.mjs",
		},
		{
			name: "python -m 模块形式不校验",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"production": map[string]any{
							"run": map[string]any{"args": []any{"python", "-m", "uvicorn", "app:app"}},
						},
					},
				},
			},
			want: "",
		},
		{
			name: "带 ./ 前缀的入口",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"production": map[string]any{
							"run": map[string]any{"args": []any{"node", "./server.mjs"}},
						},
					},
				},
			},
			want: "./server.mjs",
		},
		{
			name: "sh -c 命令不识别为入口文件",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"production": map[string]any{
							"run": map[string]any{"args": []any{"sh", "-c", "npm start"}},
						},
					},
				},
			},
			want: "",
		},
		{
			name: "参数不足不识别",
			manifest: map[string]any{
				"services": []any{
					map[string]any{
						"production": map[string]any{
							"run": map[string]any{"args": []any{"node"}},
						},
					},
				},
			},
			want: "",
		},
		{
			name:     "简单格式无 services",
			manifest: map[string]any{"image": "demo:latest"},
			want:     "",
		},
	}
	for _, c := range cases {
		got := artifactRunEntry(c.manifest)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestValidateArtifactRunEntry(t *testing.T) {
	manifest := map[string]any{
		"services": []any{
			map[string]any{
				"production": map[string]any{
					"run": map[string]any{"args": []any{"node", "server.mjs"}},
				},
			},
		},
	}
	if err := validateArtifactRunEntry(manifest, func(string) bool { return true }); err != nil {
		t.Fatalf("entry exists should pass, got %v", err)
	}
	err := validateArtifactRunEntry(manifest, func(string) bool { return false })
	if err == nil {
		t.Fatal("entry missing should fail")
	}
	if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
		t.Fatalf("error %v 应包装 ErrUnsupportedPackageFormat", err)
	}
}

func TestArtifactEntryRelPath(t *testing.T) {
	cases := []struct {
		name        string
		artifactDir string
		entry       string
		want        string
		wantOK      bool
	}{
		{name: "根目录入口", artifactDir: "", entry: "server.mjs", want: "server.mjs", wantOK: true},
		{name: "./ 前缀规范化", artifactDir: ".", entry: "./server.mjs", want: "server.mjs", wantOK: true},
		{name: "嵌套目录内入口", artifactDir: "web", entry: "server.mjs", want: "web/server.mjs", wantOK: true},
		{name: "子目录嵌套 ./", artifactDir: "web", entry: "./start.sh", want: "web/start.sh", wantOK: true},
		{name: "逃出根目录拒绝", artifactDir: "web", entry: "../server.mjs", want: "", wantOK: false},
		{name: "绝对路径拒绝", artifactDir: "", entry: "/etc/passwd", want: "", wantOK: false},
	}
	for _, c := range cases {
		got, ok := artifactEntryRelPath(c.artifactDir, c.entry)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: got (%q,%v), want (%q,%v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}

func TestValidateArtifactBuiltOutput(t *testing.T) {
	buildManifest := map[string]any{
		"services": []any{
			map[string]any{
				"production": map[string]any{
					"build": map[string]any{"args": []any{"sh", "-c", "npm ci && npm run build"}},
					"run":   map[string]any{"image": "node:22-alpine"},
				},
			},
		},
	}
	plainManifest := map[string]any{
		"services": []any{
			map[string]any{
				"production": map[string]any{
					"run": map[string]any{"image": "node:22-alpine"},
				},
			},
		},
	}
	cases := []struct {
		name        string
		manifest    map[string]any
		hasDist     bool
		hasNodeMods bool
		wantErr     bool
		wantSub     string
	}{
		{name: "未声明 build 不校验产物", manifest: plainManifest, wantErr: false},
		{name: "声明 build 且有 dist 放行", manifest: buildManifest, hasDist: true, wantErr: false},
		{name: "声明 build 且有 node_modules 放行", manifest: buildManifest, hasNodeMods: true, wantErr: false},
		{name: "声明 build 但无产物拒绝", manifest: buildManifest, wantErr: true, wantSub: "缺少构建产物"},
	}
	for _, c := range cases {
		err := validateArtifactBuiltOutput(c.manifest, func(entry string) bool {
			switch entry {
			case "dist":
				return c.hasDist
			case "node_modules":
				return c.hasNodeMods
			}
			return false
		})
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got nil", c.name)
				continue
			}
			if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
				t.Errorf("%s: error %v 应包装 ErrUnsupportedPackageFormat", c.name, err)
			}
			if c.wantSub != "" && !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("%s: error %q does not contain %q", c.name, err.Error(), c.wantSub)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
		}
	}
}

func TestZipDirOrFileExists(t *testing.T) {
	// 用 zipWriter 构造临时包验证目录与文件两种打包方式
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if _, err := zw.Create("dist/client/index.js"); err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Create("server.mjs"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	ok, err := zipDirOrFileExists(zipPath, "dist")
	if err != nil || !ok {
		t.Fatalf("dist dir should exist, ok=%v err=%v", ok, err)
	}
	ok, err = zipDirOrFileExists(zipPath, "node_modules")
	if err != nil || ok {
		t.Fatalf("node_modules should not exist, ok=%v err=%v", ok, err)
	}
	ok, err = zipDirOrFileExists(zipPath, "server.mjs")
	if err != nil || !ok {
		t.Fatalf("server.mjs file should exist, ok=%v err=%v", ok, err)
	}
}

// TestGenerateComposeYAML_CustomVolumes 用户自定义磁盘映射以引号包裹的 bind mount 形式渲染，
// 只读映射追加 :ro 后缀；未传映射时 volumes 段无额外行。
func TestGenerateComposeYAML_CustomVolumes(t *testing.T) {
	compose := generateComposeYAML(
		"order-manager",
		"test-app-id",
		"test-installation-id",
		"node:20-alpine",
		8080,
		30123,
		"/app/data/apps/test-app-id",
		"postgres://user:pass@db/app",
		"app_71", map[string]string{"TIER0_API_KEY": "t0_key"},
		map[string]string{},
		[]string{"node", "server.mjs"},
		"/health",
		false,
		0,
		"",
		[]artifactVolume{
			{HostPath: "/srv/appdata", ContainerPath: "/data"},
			{HostPath: "/mnt/logs", ContainerPath: "/var/log/app", ReadOnly: true},
		},
	)

	if !strings.Contains(compose, "      - \"/srv/appdata:/data\"\n") {
		t.Fatalf("custom bind mount should be rendered quoted, got:\n%s", compose)
	}
	if !strings.Contains(compose, "      - \"/mnt/logs:/var/log/app:ro\"\n") {
		t.Fatalf("readonly mount should carry :ro suffix, got:\n%s", compose)
	}
	// 自定义 bind mount 不需要顶层 volumes 声明
	if strings.Contains(compose, "  appdata:\n") {
		t.Fatalf("bind mount should not add top-level volume entries, got:\n%s", compose)
	}

	withoutCustom := generateComposeYAML(
		"order-manager",
		"test-app-id",
		"test-installation-id",
		"node:20-alpine",
		8080,
		30124,
		"/app/data/apps/test-app-id",
		"postgres://user:pass@db/app",
		"app_71", map[string]string{"TIER0_API_KEY": "t0_key"},
		map[string]string{},
		[]string{"node", "server.mjs"},
		"/health",
		false,
		0,
		"",
		nil,
	)
	if strings.Contains(withoutCustom, "/srv/") {
		t.Fatalf("nil volumes should not render custom mounts, got:\n%s", withoutCustom)
	}
}
