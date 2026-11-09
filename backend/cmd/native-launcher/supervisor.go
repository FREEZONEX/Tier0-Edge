//go:build windows || darwin || linux

// Shared native service orchestration.
package main

import (
	"backend/internal/fleetprotocol"
	"backend/internal/nativeui"
	"backend/internal/product"
	"backend/internal/runtimeenv"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

type supervisor struct {
	installationID string
	ctx            context.Context
	root           string
	programRoot    string
	env            []string
	job            processJob
	failures       chan string
	progress       func(string, string, string)
	mu             sync.Mutex
	controlMu      sync.Mutex
	services       map[string]*managedService
	operations     map[string]fleetprotocol.RuntimeOperation
	runtimeURL     string
	runtimeToken   string
	ready          bool
	closing        bool
}

func token() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func run(root string, noBrowser bool) error {
	return runManaged(context.Background(), root, noBrowser, nil)
}
func runManaged(parent context.Context, root string, noBrowser bool, observer func(nativeui.Progress)) error {
	return runManagedAt(parent, root, root, noBrowser, observer)
}
func runManagedAt(parent context.Context, root, programRoot string, noBrowser bool, observer func(nativeui.Progress)) (resultErr error) {
	distribution, err := runtimeenv.ReadDistribution(programRoot)
	if err != nil {
		return err
	}
	if err = runtimeenv.ValidateDistribution(distribution); err != nil {
		return err
	}
	backendIdentity, err := runtimeenv.ReadArtifactIdentity(filepath.Join(programRoot, "backend", binaryName("backend")))
	if err != nil {
		return err
	}
	if backendIdentity != runtimeenv.Identity() {
		return errors.New("backend and launcher product identities differ")
	}
	identityPath := root
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		identityPath = strings.ToLower(root)
	}
	sum := sha256.Sum256([]byte(identityPath))
	id := hex.EncodeToString(sum[:6])
	unlock, err := lockInstallation(root, id)
	if err != nil {
		return err
	}
	defer unlock()
	persistPorts, err := loadServicePorts(root)
	if err != nil {
		return err
	}
	ctx, stopSignals := signal.NotifyContext(parent, terminationSignals()...)
	defer stopSignals()
	progress := nativeui.NewProgress()
	publish := func() {
		progress.UpdatedAt = time.Now().UnixMilli()
		_ = nativeui.WriteProgress(root, progress)
		if observer != nil {
			snapshot := progress
			snapshot.Services = append([]nativeui.ServiceProgress(nil), progress.Services...)
			observer(snapshot)
		}
	}
	defer func() {
		if resultErr != nil && ctx.Err() == nil && !errors.Is(resultErr, context.Canceled) {
			progress.Phase = "failed"
			progress.Error = resultErr.Error()
			progress.Message = "启动或运行失败，请查看日志后重试"
		} else {
			progress.Phase = "stopped"
			progress.Message = "服务已停止，数据已保留"
		}
		for i := range progress.Services {
			if progress.Services[i].State != "pending" {
				progress.Services[i].State = "stopped"
			}
		}
		publish()
	}()
	progress.Message = "正在检查版本、运行实例、磁盘空间及升级备份"
	publish()
	finishInstall, failInstall, err := prepareInstallation(root, programRoot)
	if err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			failInstall(resultErr)
		}
	}()
	if persistPorts != nil {
		if err := persistPorts(); err != nil {
			return fmt.Errorf("save native service ports: %w", err)
		}
	}
	progress.Message = "正在检查安装文件和端口"
	publish()
	if _, err := os.Stat(filepath.Join(root, ".installing")); err == nil {
		return fmt.Errorf("installation is incomplete; refusing to start")
	}
	// 仅持有本安装互斥锁的监督进程消费停止请求，不触碰其他安装。
	_ = os.Remove(filepath.Join(root, "stop.request"))
	go func() {
		timer := time.NewTicker(200 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if _, err := os.Stat(filepath.Join(root, "stop.request")); err == nil {
					stopSignals()
					return
				}
			}
		}
	}()
	if _, err := os.Stat(filepath.Join(programRoot, "package.json")); err != nil {
		return fmt.Errorf("package manifest missing")
	}
	for _, dir := range []string{"data", "logs", "state", "backend/etc"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			return err
		}
	}
	if err := protectState(root); err != nil {
		return err
	}
	settings := map[string]string{}
	machineID, err := hostIdentity()
	if err != nil {
		return err
	}
	machineFile := filepath.Join(root, "state", "machine-id")
	if _, err := os.Stat(machineFile); os.IsNotExist(err) {
		if err = os.WriteFile(machineFile, []byte(machineID), 0600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	configPath := filepath.Join(root, "state", "environment.json")
	raw, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		for _, key := range []string{"PGPASSWORD", "NATS_PASSWORD", "APP_CREDENTIAL_KEY", "JWT_SECRET", "ADMIN_INITIAL_PASSWORD", "TIER0_API_KEY", "SOURCE_CREDENTIAL_KEY", "VIDEO_CREDENTIAL_KEY", "AI_GATEWAY_CREDENTIAL_KEY", "TENANT_PG_ROLE_SECRET", "MQTT_BACKEND_PASSWORD", "EMQX_API_SECRET", "FLEET_AGENT_LOCAL_TOKEN"} {
			settings[key] = token()
		}
		for _, key := range []string{"SOURCE_CREDENTIAL_KEY", "VIDEO_CREDENTIAL_KEY", "AI_GATEWAY_CREDENTIAL_KEY"} {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return err
			}
			settings[key] = base64.StdEncoding.EncodeToString(b)
		}
		settings["NATIVE_HTTP_PORT"] = servicePort("18080")
		settings["ADMIN_INITIAL_PASSWORD"] = "tier0"
		raw, _ = json.MarshalIndent(settings, "", "  ")
		if err := os.WriteFile(configPath, raw, 0600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if err := json.Unmarshal(raw, &settings); err != nil {
		return err
	}
	changed := false
	for _, key := range []string{"NATS_PASSWORD", "APP_CREDENTIAL_KEY"} {
		if settings[key] == "" {
			settings[key] = token()
			if key == "NATS_PASSWORD" && settings["REDIS_PASSWORD"] != "" {
				settings[key] = settings["REDIS_PASSWORD"]
			}
			changed = true
		}
	}
	if changed {
		raw, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return err
		}
		if err = os.WriteFile(configPath+".tmp", raw, 0600); err != nil {
			return err
		}
		if err = os.Rename(configPath+".tmp", configPath); err != nil {
			return err
		}
	}
	for _, key := range []string{"PGPASSWORD", "NATS_PASSWORD", "APP_CREDENTIAL_KEY", "JWT_SECRET", "ADMIN_INITIAL_PASSWORD", "TENANT_PG_ROLE_SECRET", "SOURCE_CREDENTIAL_KEY", "FLEET_AGENT_LOCAL_TOKEN"} {
		if settings[key] == "" {
			return fmt.Errorf("missing private setting %s", key)
		}
	}
	port := settings["NATIVE_HTTP_PORT"]
	for _, p := range []string{port, servicePort("15432"), servicePort("14222"), servicePort("11883"), servicePort("18083"), servicePort("19732"), servicePort("19443"), servicePort("19733")} {
		host := "127.0.0.1"
		if p == port {
			host = nativeHTTPListenHost
		}
		l, err := net.Listen("tcp", net.JoinHostPort(host, p))
		if err != nil {
			return fmt.Errorf("port %s is occupied; no existing service was stopped", p)
		}
		l.Close()
	}
	socketDir := filepath.Join(os.TempDir(), "t0-"+id)
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return err
	}
	env := map[string]string{}
	for k, v := range settings {
		env[k] = v
	}
	data := func(p string) string { return filepath.Join(root, "data", p) }
	for _, dir := range []string{"license", "files", "fleet"} {
		if err := os.MkdirAll(data(dir), 0700); err != nil {
			return err
		}
	}
	base := "http://127.0.0.1:" + port
	fleetEndpoint := settings["FLEET_NODE_API_BASE_URL"]
	if fleetEndpoint == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return err
		}
		fleetEndpoint = "https://" + net.JoinHostPort(hostname, servicePort("19443"))
	}
	defaults := map[string]string{
		"TIER0_RUNTIME_MODE":    "native",
		"TIER0_INSTALLATION_ID": id,
		// Center bootstrap requires HTTPS; avoid port-80 Noise upgrade probes
		// through desktop HTTP proxies during cold start and identity restore.
		"TS_FORCE_NOISE_443": "true",
		"SOURCE_OPCUA_PKI":   data("opcua/pki"),
		// Backend's shared schema still has numeric Video fields even without Vision services.
		"ZLM_RTSP_PORT": "554", "ZLM_STREAM_PORT": "554", "ZLM_RTC_PORT": "8000",
		"UNS_DB_URL": "postgres://postgres:" + env["PGPASSWORD"] + "@127.0.0.1:15432/edge?sslmode=disable", "SINK_DB_URL": "postgres://postgres:" + env["PGPASSWORD"] + "@127.0.0.1:15432/edge?sslmode=disable",
		"CACHE_DRIVER": product.ProfileFor(runtimeenv.Edition()).Adapters["cache"], "REDIS_ADDR": "", "WEB_DIR": filepath.Join(programRoot, "web"), "BACKEND_LOG_PATH": filepath.Join(root, "logs", "backend"), "BACKEND_LOG_MODE": "console",
		"FILESTORE_DRIVER": product.ProfileFor(runtimeenv.Edition()).Adapters["storage"], "FILESTORE_S3_ENDPOINT": "", "FILESTORE_S3_BOOTSTRAP_BUCKETS": "false",
		"LICENSE_DIR": data("license"), "MACHINE_ID_PATH": filepath.Join(root, "state", "machine-id"), "FILESTORE_LOCAL_ROOT": data("files"),
		"APP_RUNTIME_ROOT": filepath.Join(root, "state", "apps"), "APP_NODE_BINARY": filepath.Join(programRoot, "node", binaryName("node")),
		"APP_NPM_CLI": filepath.Join(programRoot, "node", "node_modules", "npm", "bin", "npm-cli.js"), "APP_PNPM_CLI": filepath.Join(programRoot, "node", "node_modules", "pnpm", "bin", "pnpm.cjs"),
		"DATAINGEST_MQTT_BROKERS": "tcp://127.0.0.1:11883", "DATAINGEST_MQTT_USERNAME": "backend", "DATAINGEST_MQTT_PASSWORD": env["MQTT_BACKEND_PASSWORD"], "DATAINGEST_MQTT_CLIENT_ID": "edge_server_native_" + id, "DATAINGEST_ENABLED": "true",
		"NATIVE_BACKEND_URL": base, "NATIVE_MQTT_PORT": "11883", "NATIVE_MQTT_WS_PORT": "18083", "EMQX_API_KEY": "native", "EMQX_URL": "http://127.0.0.1:18083", "MQTT_WS_UPSTREAM_URL": "ws://127.0.0.1:18083/mqtt",
		"SOURCE_STREAM_NATS_URL": "nats://tier0:" + env["NATS_PASSWORD"] + "@127.0.0.1:14222", "SOURCE_OPCUA_SOCKET": opcuaSocket(socketDir, id), "SOURCE_MODBUS_SOCKET": filepath.Join(socketDir, "m.sock"), "STREAM_EXECUTOR_SOCKET": filepath.Join(socketDir, "e.sock"),
		"FLEET_AGENT_LOCAL_URL": "http://127.0.0.1:19732", "FLEET_AGENT_HTTP_ADDR": "127.0.0.1:19732", "FLEET_AGENT_DATA_DIR": data("fleet"), "FLEET_COLLAB_ENABLED": "true",
		"FLEET_IDENTITY_DIR": filepath.Join(root, "state", "fleet-identity"), "FLEET_INGRESS_PORT": "19443", "FLEET_INGRESS_HOST": "127.0.0.1", "FLEET_NODE_API_BASE_URL": fleetEndpoint, "FLEET_BACKEND_INGRESS_PORT": "19443",
		"FLEET_USERSPACE_NETWORK_DIR": filepath.Join(root, "state", "fleet-network"), "FLEET_NETWORK_SOCKS_ADDRESS": "127.0.0.1:19733", "FLEET_SSH_SOCKS_ADDRESS": "127.0.0.1:19733",
		"APP_GATEWAY_ACCESS_MODE": "port", "APP_GATEWAY_SCHEME": "http", "APP_GATEWAY_LISTENER_SCHEME": "http", "APP_GATEWAY_EXTERNAL_HOST": "127.0.0.1", "APP_GATEWAY_EXTERNAL_PORT": port, "PRODUCT_VERSION": productVersion(programRoot),
	}
	for k, v := range defaults {
		env[k] = v
		switch k {
		case "UNS_DB_URL", "SINK_DB_URL", "DATAINGEST_MQTT_BROKERS", "NATIVE_MQTT_PORT", "NATIVE_MQTT_WS_PORT", "EMQX_URL", "MQTT_WS_UPSTREAM_URL", "SOURCE_STREAM_NATS_URL", "FLEET_AGENT_LOCAL_URL", "FLEET_AGENT_HTTP_ADDR", "FLEET_INGRESS_PORT", "FLEET_BACKEND_INGRESS_PORT", "FLEET_NETWORK_SOCKS_ADDRESS", "FLEET_SSH_SOCKS_ADDRESS":
			env[k] = mapServicePorts(v)
		}
	}
	// User-supplied advertised URL is already absolute and must not be rewritten.
	env["FLEET_NODE_API_BASE_URL"] = fleetEndpoint
	// Keep OS runtime settings; never inherit another Enterprise's DB/Fleet/Docker configuration.
	inherited := map[string]string{}
	for _, k := range []string{"SYSTEMROOT", "WINDIR", "SYSTEMDRIVE", "COMSPEC", "PATH", "PATHEXT", "TEMP", "TMP", "USERPROFILE", "USERNAME", "USERDOMAIN", "APPDATA", "LOCALAPPDATA", "PROGRAMDATA", "PROGRAMFILES", "PROGRAMFILES(X86)", "PROCESSOR_ARCHITECTURE", "NUMBER_OF_PROCESSORS", "HOME", "USER", "TMPDIR", "LANG", "LC_ALL", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"} {
		if value, ok := os.LookupEnv(k); ok {
			inherited[k] = value
		}
	}
	for k, v := range env {
		inherited[k] = v
	}
	environment := []string{}
	for k, v := range inherited {
		environment = append(environment, k+"="+v)
	}
	job, err := newServiceJob()
	if err != nil {
		return err
	}
	defer closeJob(job)
	s := &supervisor{installationID: id, ctx: ctx, root: root, programRoot: programRoot, env: environment, job: job, failures: make(chan string, 20), progress: func(id, state, message string) {
		progress.Set(id, state, message)
		progress.Message = message
		publish()
	}}
	defer s.shutdown()
	if err := s.serveRuntime(); err != nil {
		return err
	}
	pgBin := filepath.Join(programRoot, "postgres", "bin")
	if _, err := os.Stat(data("postgres/PG_VERSION")); os.IsNotExist(err) {
		s.progress("postgres", "starting", "正在初始化数据库；日志：initdb.log")
		if entries, e := os.ReadDir(data("postgres")); e == nil && len(entries) > 0 {
			return fmt.Errorf("partial database initialization; retained for diagnosis, refusing to overwrite")
		}
		passwordFile := filepath.Join(root, "state", "pg-password.tmp")
		if err := os.WriteFile(passwordFile, []byte(env["PGPASSWORD"]), 0600); err != nil {
			return err
		}
		err = s.once("initdb", filepath.Join(pgBin, binaryName("initdb")), "-D", data("postgres"), "-U", "postgres", "--encoding=UTF8", "--locale=C", "--auth=scram-sha-256", "--no-clean", "--pwfile="+passwordFile)
		os.Remove(passwordFile)
		if err != nil {
			return err
		}
		pgConf := "\nlisten_addresses = '127.0.0.1'\nport = " + servicePort("15432") + "\nshared_preload_libraries = 'timescaledb,pg_stat_statements'\nmax_connections = 100\nshared_buffers = '128MB'\n"
		f, err := os.OpenFile(data("postgres/postgresql.conf"), os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.WriteString(pgConf)
		f.Close()
		if err != nil {
			return err
		}
	}
	if err := s.start("postgres", filepath.Join(pgBin, binaryName("postgres")), "-D", data("postgres")); err != nil {
		return err
	}
	if err := s.waitTCP(servicePort("15432")); err != nil {
		return err
	}
	// SELECT checks an existing DB; creation is only attempted when it is absent.
	var out []byte
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); {
		check := s.command(filepath.Join(pgBin, binaryName("psql")), "-h", "127.0.0.1", "-p", servicePort("15432"), "-U", "postgres", "-d", "postgres", "-tAc", "SELECT 1 FROM pg_database WHERE datname='edge'")
		out, err = check.Output()
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("database readiness: %w", err)
	}
	if strings.TrimSpace(string(out)) != "1" {
		if err := s.once("createdb", filepath.Join(pgBin, binaryName("createdb")), "-h", "127.0.0.1", "-p", servicePort("15432"), "-U", "postgres", "edge"); err != nil {
			return err
		}
	}
	s.progress("postgres", "ready", "数据库连接检查通过")
	natsConf := fmt.Sprintf("host: 127.0.0.1\nport: %s\nauthorization { user: tier0, password: %q }\njetstream { store_dir: %q, max_memory_store: 67108864, max_file_store: 8589934592 }\n", servicePort("14222"), env["NATS_PASSWORD"], filepath.ToSlash(data("nats")))
	natsFile := filepath.Join(root, "state", "nats.conf")
	if err := os.WriteFile(natsFile, []byte(natsConf), 0600); err != nil {
		return err
	}
	if err := s.start("nats", filepath.Join(programRoot, "nats", binaryName("nats-server")), "-c", natsFile); err != nil {
		return err
	}
	if err := s.waitTCP(servicePort("14222")); err != nil {
		return err
	}
	node := filepath.Join(programRoot, "node", binaryName("node"))
	if err := s.start("mqtt", node, filepath.Join(programRoot, "mqtt", "server.mjs")); err != nil {
		return err
	}
	if err := s.waitTCP(servicePort("11883")); err != nil {
		return err
	}
	if err := s.start("opcua", node, filepath.Join(programRoot, "opcua", "server.mjs")); err != nil {
		return err
	}
	if err := s.start("modbus", filepath.Join(programRoot, "backend", binaryName("source-modbus")), "--socket", env["SOURCE_MODBUS_SOCKET"]); err != nil {
		return err
	}
	if err := s.start("executor", filepath.Join(programRoot, "backend", binaryName("stream-executor")), "--socket", env["STREAM_EXECUTOR_SOCKET"]); err != nil {
		return err
	}
	if err := s.start("fleet", filepath.Join(programRoot, "backend", binaryName("fleet-agent"))); err != nil {
		return err
	}
	if err := s.waitTCP(servicePort("19732")); err != nil {
		return err
	}
	template, err := os.ReadFile(filepath.Join(programRoot, "backend", "backend.template.yaml"))
	if err != nil {
		return err
	}
	yaml := strings.Replace(string(template), "Host: 0.0.0.0", "Host: "+nativeHTTPListenHost, 1)
	yaml = strings.Replace(yaml, "Port: 8080", "Port: "+port, 1)
	variable := regexp.MustCompile(`\$\{([A-Z0-9_]+)(?::-([^}]*))?\}`)
	yaml = variable.ReplaceAllStringFunc(yaml, func(value string) string {
		parts := variable.FindStringSubmatch(value)
		if v := inherited[parts[1]]; v != "" {
			return v
		}
		return parts[2]
	})
	if err := os.WriteFile(filepath.Join(root, "backend", "etc", "backend.yaml"), []byte(yaml), 0600); err != nil {
		return err
	}
	if err := s.start("backend", filepath.Join(programRoot, "backend", binaryName("backend")), "-f", filepath.Join(root, "backend", "etc", "backend.yaml")); err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	ready := false
	for deadline := time.Now().Add(3 * time.Minute); time.Now().Before(deadline); {
		if s.stopRequested() {
			return context.Canceled
		}
		select {
		case failure := <-s.failures:
			return fmt.Errorf("%s exited; inspect its log", failure)
		default:
		}
		resp, err := client.Get(base + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(time.Second)
	}
	if !ready {
		return fmt.Errorf("backend readiness timed out")
	}
	s.progress("backend", "ready", "HTTP 就绪检查通过")
	s.progress("apps", "ready", "原生 App 运行环境已初始化；已安装应用按保存状态恢复")
	access := "URL: " + base + "\nUsername: tier0\nInitial password: " + env["ADMIN_INITIAL_PASSWORD"] + "\n"
	if err := os.WriteFile(filepath.Join(root, "state", "LOGIN.txt"), []byte(access), 0600); err != nil {
		return err
	}
	fmt.Println("Tier0 native validation is ready:", base, "\nLogin details:", filepath.Join(root, "state", "LOGIN.txt"))
	if !noBrowser {
		if err := openBrowser(base); err != nil {
			fmt.Println("Open browser:", err)
		}
	}
	if err := finishInstall(); err != nil {
		return err
	}
	progress.Phase = "ready"
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
	progress.Message = "Tier0 已就绪，关闭此窗口后继续在托盘运行"
	if runtime.GOOS == "linux" {
		progress.Message = "Tier0 已就绪，可使用 tier0 status、logs、stop 管理服务"
	}
	publish()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case failure := <-s.failures:
			fmt.Println("Service exited; other services remain available:", failure)
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(root, "stop.request")); err == nil {
				os.Remove(filepath.Join(root, "stop.request"))
				return nil
			}
			// The desktop is a live service monitor, not just a startup transcript.
			// Reuse the registered runtime provider so remote controls and child
			// exits appear without granting the GUI another control endpoint.
			snapshot, e := s.List(ctx)
			if e == nil {
				for _, item := range snapshot.Items {
					state, message := item.State, ""
					if state == "running" {
						message = "进程运行中；未提供独立就绪探针"
						if item.Health == "ready" {
							state = "ready"
							message = "本机端口检查通过"
						}
						if item.Health == "unavailable" {
							state = "failed"
							message = "进程存在，但本机端口检查未通过"
						}
						if item.ID == "backend" {
							probe := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
							response, err := probe.Get(base + "/readyz")
							state = "starting"
							message = "等待 HTTP 就绪检查"
							if err == nil {
								response.Body.Close()
								if response.StatusCode == http.StatusOK {
									state = "ready"
									message = "HTTP 就绪检查通过"
								}
							}
						}
					} else {
						message = nativeui.StateLabel(state)
					}
					progress.Set(item.ID, state, message)
				}
				for _, service := range progress.Services {
					if service.ID == "backend" {
						if service.State == "ready" {
							progress.Set("apps", "ready", "原生 App 运行环境可用；各应用状态请在平台查看")
						} else {
							progress.Set("apps", "unknown", "平台未就绪，无法确认应用运行环境")
						}
						break
					}
				}
				publish()
			}
		}
	}
}
func writeJSONAtomic(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(name+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(name+".tmp", name)
}

func (s *supervisor) command(exe string, args ...string) *exec.Cmd {
	c := exec.Command(exe, args...)
	c.Dir = filepath.Dir(exe)
	c.Env = s.env
	configureCommand(c)
	return c
}
func (s *supervisor) once(name, exe string, args ...string) error {
	c := exec.CommandContext(s.ctx, exe, args...)
	c.Dir = filepath.Dir(exe)
	c.Env = s.env
	configureCommand(c)
	f, err := os.OpenFile(filepath.Join(s.root, "logs", name+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	c.Stdout = f
	c.Stderr = f
	if err := startInJob(c, s.job); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if err := c.Wait(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
func (s *supervisor) start(name, exe string, args ...string) error {
	s.mu.Lock()
	if s.services == nil {
		s.services = map[string]*managedService{}
	}
	s.services[name] = &managedService{exe: exe, args: args, item: fleetprotocol.RuntimeItem{ID: name, Name: name, State: "stopped", Health: "unknown"}}
	s.mu.Unlock()
	return s.startService(name)
}
func (s *supervisor) startService(name string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	service := s.services[name]
	if service == nil || s.closing {
		return fmt.Errorf("RUNTIME_UNAVAILABLE")
	}
	if service.cmd != nil {
		return nil
	}
	fmt.Println("Starting", name)
	if !s.ready {
		s.progress(name, "starting", "正在启动 "+name)
	}
	c := s.command(service.exe, service.args...)
	if name == "fleet" {
		// ts_omit_useproxy omits WinHTTP hooks, but some tsnet transports
		// still clone Go's DefaultTransport (and its environment proxy).
		// Native Fleet has a direct control-plane policy; keep desktop proxy
		// settings for other children without inheriting them into the Agent.
		c.Env = make([]string, 0, len(s.env))
		for _, entry := range s.env {
			key, _, _ := strings.Cut(entry, "=")
			switch strings.ToUpper(key) {
			case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
				continue
			}
			c.Env = append(c.Env, entry)
		}
		c.Env = append(c.Env, "TIER0_RUNTIME_URL="+s.runtimeURL, "TIER0_RUNTIME_TOKEN="+s.runtimeToken)
	}
	f, err := os.OpenFile(filepath.Join(s.root, "logs", name+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	c.Stdout = f
	c.Stderr = f
	serviceJob, err := newServiceJob()
	if err != nil {
		f.Close()
		return err
	}
	if err := startInJob(c, s.job, serviceJob); err != nil {
		f.Close()
		closeJob(serviceJob)
		return fmt.Errorf("%s: %w", name, err)
	}
	service.cmd, service.job, service.done, service.stopping = c, serviceJob, make(chan struct{}), false
	if service.item.StartedAt != 0 {
		service.item.RestartCount++
	}
	service.item.PID, service.item.StartedAt, service.item.State = c.Process.Pid, time.Now().UnixMilli(), "running"
	if !s.ready {
		s.progress(name, "running", "进程已启动")
	}
	go func() {
		waitErr := c.Wait()
		f.Close()
		s.mu.Lock()
		closeJob(service.job)
		service.job = 0
		service.cmd = nil
		service.item.PID = 0
		service.item.State = "stopped"
		if !service.stopping && waitErr != nil {
			service.item.State = "failed"
		}
		close(service.done)
		intentional := service.stopping
		s.mu.Unlock()
		if !intentional {
			select {
			case s.failures <- name:
			default:
			}
		}
	}()
	return nil
}
func (s *supervisor) waitTCP(port string) error {
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); {
		if s.stopRequested() {
			return context.Canceled
		}
		select {
		case name := <-s.failures:
			return fmt.Errorf("%s exited; inspect logs", name)
		default:
		}
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second)
		if err == nil {
			c.Close()
			id := map[string]string{servicePort("15432"): "postgres", servicePort("14222"): "nats", servicePort("11883"): "mqtt", servicePort("19732"): "fleet"}[port]
			if id != "" {
				s.progress(id, "ready", "本机端口检查通过")
			}
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("port %s did not become ready", port)
}

func (s *supervisor) stopRequested() bool {
	if s.ctx.Err() != nil {
		return true
	}
	_, err := os.Stat(filepath.Join(s.root, "stop.request"))
	return err == nil
}
func (s *supervisor) shutdown() {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	for _, name := range []string{"backend", "fleet", "executor", "modbus", "opcua", "mqtt", "nats", "postgres"} {
		if err := s.stopService(name); err != nil {
			fmt.Println("Shutdown:", name, err)
		}
	}
}
