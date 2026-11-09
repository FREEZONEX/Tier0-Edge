// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"backend/internal/product"
	"backend/internal/runtimeenv"
	"backend/internal/secrets"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

type Config struct {
	rest.RestConf
	ProductVersion string `json:"productVersion,optional,env=PRODUCT_VERSION"`
	Database       DatabaseConf
	TenantPG       TenantPGConf
	Redis          RedisConf
	NativeApps     NativeAppsConf `json:"nativeApps,optional"`
	Security       SecurityConf
	ExternalLogin  ExternalLoginConf
	MicrosoftSSO   MicrosoftSSOConf
	Gateway        GatewayConf
	Storage        StorageConf
	DataIngest     DataIngestConf
	DataQuality    DataQualityConf
	SourceStream   SourceStreamConf `json:"sourceStream,optional"`
	MCP            MCPConf          `json:"mcp,optional"`
	Cluster        ClusterConf
	Video          VideoConf
	AIGateway      AIGatewayConf
	FleetAgent     FleetAgentConf
	License        LicenseConf
	AppGateway     AppGatewayConfig `json:"appGateway,optional"`
	Tier0Sdk       Tier0SdkConfig   `json:"tier0Sdk,optional"`
	Export         ExportConfig     `json:"export,optional"`
	Https          HttpsConfig      `json:"https,optional"`
}

// MCP 与现有 Backend 共用端口。显式公网地址用于带路径前缀的代理部署。
type MCPConf struct {
	Disabled  bool   `json:"disabled,optional"`
	PublicURL string `json:"publicUrl,optional,env=MCP_PUBLIC_URL"`
}

type SourceStreamConf struct {
	ExecutorSocket  string `json:"executorSocket,optional,env=STREAM_EXECUTOR_SOCKET"`
	ExecutorSockets string `json:"executorSockets,optional,env=STREAM_EXECUTOR_SOCKETS"`
	OPCUASocket     string `json:"opcuaSocket,optional,env=SOURCE_OPCUA_SOCKET"`
	ModbusSocket    string `json:"modbusSocket,optional,env=SOURCE_MODBUS_SOCKET"`
	CredentialKey   string `json:"credentialKey,optional,env=SOURCE_CREDENTIAL_KEY"`
	NATSURL         string `json:"natsUrl,optional,env=SOURCE_STREAM_NATS_URL"`
}

func (c SourceStreamConf) ExecutorEndpoints() string {
	if strings.TrimSpace(c.ExecutorSockets) != "" {
		return c.ExecutorSockets
	}
	return c.ExecutorSocket
}

// HttpsConfig Backend HTTPS listener 配置。
// 独立于顶层 RestConf.CertFile/KeyFile：8080 HTTP listener 始终为 HTTP，
// HTTPS 由单独端口（BACKEND_HTTPS_PORT）提供。Port<=0 时不启用 HTTPS。
type HttpsConfig struct {
	Port     int    `json:"port,optional,env=BACKEND_HTTPS_PORT"`
	CertFile string `json:"certFile,optional,env=BACKEND_TLS_CERT_FILE"`
	KeyFile  string `json:"keyFile,optional,env=BACKEND_TLS_KEY_FILE"`
}

type DatabaseConf struct {
	UnsDbUrl                 string
	SinkDbUrl                string
	TimeseriesRetentionYears int `json:"timeseriesRetentionYears,optional"`
}

// TenantPGConf 租户 PG 隔离配置
type TenantPGConf struct {
	RoleSecret     string        `json:"roleSecret,optional,env=TENANT_PG_ROLE_SECRET"`
	ConnectTimeout time.Duration `json:"connectTimeout,optional"`
	MaxOpenConns   int           `json:"maxOpenConns,optional"`
	MaxIdleConns   int           `json:"maxIdleConns,optional"`
	ConnCacheSize  int           `json:"connCacheSize,optional"`
}

type RedisConf struct {
	Driver     string `json:",optional"`
	MaxEntries int    `json:",optional"`
	Addr       string
	Password   string
	DB         int
}

type NativeAppsConf struct {
	CredentialKey string `json:"credentialKey,optional,env=APP_CREDENTIAL_KEY"`
	Root          string `json:"root,optional,env=APP_RUNTIME_ROOT"`
	Node          string `json:"node,optional,env=APP_NODE_BINARY"`
	NPMCLI        string `json:"npmCLI,optional,env=APP_NPM_CLI"`
	PNPMCLI       string `json:"pnpmCLI,optional,env=APP_PNPM_CLI"`
}

type SecurityConf struct {
	JwtSecret            string
	InitialAdminPassword string
	PluginAPIKey         string
}

// MicrosoftSSOConf configures a Microsoft Entra ID OIDC client. TenantID defaults
// to common for organization and personal accounts; a tenant GUID restricts login
// to that tenant. MFA policy stays in Entra Conditional Access; Tier0 validates
// the signed ID token and maps stable tenant/object identifiers to a local user.
type ExternalLoginConf struct {
	Enabled bool `json:"enabled,optional,env=EXTERNAL_LOGIN_ENABLED"`
}

type MicrosoftSSOConf struct {
	Enabled      bool   `json:"enabled,optional,env=MICROSOFT_SSO_ENABLED"`
	TenantID     string `json:"tenantId,optional,env=MICROSOFT_SSO_TENANT_ID"`
	ClientID     string `json:"clientId,optional,env=MICROSOFT_SSO_CLIENT_ID"`
	ClientSecret string `json:"clientSecret,optional,env=MICROSOFT_SSO_CLIENT_SECRET"`
	// RedirectURL is normally derived from the canonical Tier0 public entrance.
	// An explicit value is reserved for deployments behind a distinct public proxy.
	RedirectURL string `json:"redirectUrl,optional,env=MICROSOFT_SSO_REDIRECT_URL"`
}

type GatewayConf struct {
	WebDir           string
	LocalFrontendDev bool
	FrontendDevUrl   string
	// anchor 子应用的开发代理（显式设置才生效）：/anchor/* 同源代理到本机 Vite dev server，
	// 生产不设置该值、始终读镜像内静态文件
	AnchorDevProxyUrl string `json:",optional"`
	LaunchpadPort     string
	LaunchpadDomain   string `json:",optional"` // LAUNCHPAD_DOMAIN：HTTPS 主入口按 Host 分流到 Launchpad 独立站点的域名；空则仅独立 listener
	SourceFlowUrl     string
	EventFlowUrl      string
	MarimoUrl         string
	EmqxUrl           string
	UnsAgentUrl       string
	// MqttWsUpstreamUrl 是 /mqtt 固定前缀 WebSocket 代理的上游（EMQX 内部明文 WS
	// listener，如 ws://emqx:8083/mqtt）；空则代理不启用，浏览器按旧逻辑直连 8083/8084
	MqttWsUpstreamUrl string `json:",optional"`
	// 仅当 Backend 前面存在可信反向代理时才开启：开启后 X-Forwarded-For/X-Real-IP
	// 才会被采信（审计/登录日志的客户端 IP 来源）；默认关闭，直接记录 socket 地址，
	// 避免未认证调用方伪造转发头污染安全日志
	TrustProxyHeaders bool `json:",optional"`
}

type StorageConf struct {
	FileRoot       string
	MarimoFileRoot string
	Driver         string
	PublicBaseURL  string
	S3             S3StorageConf
}

type S3StorageConf struct {
	Endpoint        string
	Region          string
	Bucket          string
	PublicBucket    string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	UseSSL          bool
	PathStyle       bool
	PublicEndpoint  string
	// BootstrapBuckets 仅在内置 RustFS 模式置 true：启动时幂等创建
	// private/public bucket 并设置 public 匿名下载策略。外置 S3 保持 false，
	// bucket 与策略由运维预建，backend 不做任何 bucket 级写操作。
	BootstrapBuckets bool
}

type LicenseConf struct {
	LicenseDir    string
	MachineIDPath string
}

type FleetAgentConf struct {
	LocalURL   string
	LocalToken string
}

type DataIngestConf struct {
	Enabled         bool
	MqttBrokers     string
	MqttClientID    string
	MqttUsername    string
	MqttPassword    string
	MqttTopic       string
	QueueSize       int
	BatchSize       int
	FlushIntervalMs int
}

// DataQualityConf controls UNS data-quality evaluation. Shipping configurations
// enable the module; each node starts detection only after rules are configured.
type DataQualityConf struct {
	Enabled            bool
	ShardCount         int
	MaxEnabledNodes    int
	IssueQueueSize     int
	MaxCollectWindowMs int
}

type ClusterConf struct {
	IngressTLSCertFile     string `json:"-"`
	IngressPort            int    `json:",optional,env=FLEET_INGRESS_PORT"`
	IngressHost            string `json:",optional,env=FLEET_INGRESS_HOST"`
	NetworkBootstrapURL    string `json:",optional,env=FLEET_NETWORK_BOOTSTRAP_URL"`
	NetworkSOCKSAddress    string `json:",optional,env=FLEET_NETWORK_SOCKS_ADDRESS"`
	SSHSOCKSAddress        string `json:",optional,env=FLEET_SSH_SOCKS_ADDRESS"`
	CollaborationEnabled   bool   `json:",default=true,env=FLEET_COLLAB_ENABLED"`
	IdentityDir            string `json:",optional,env=FLEET_IDENTITY_DIR"`
	NodeAPIBaseURL         string `json:",optional,env=FLEET_NODE_API_BASE_URL"`
	TLSCAFile              string `json:",optional,env=FLEET_TLS_CA_FILE"`
	NodeKey                string `json:",optional"`
	NodeName               string `json:",optional"`
	ExternalHTTPBaseURL    string `json:",optional"`
	ExternalMqttBrokerHost string `json:",optional"`
	MountMode              string `json:",optional"`
	SyncMetricEnabled      bool   `json:",optional"`
}

type VideoConf struct {
	CredentialKey string `json:"credentialKey,optional,env=VIDEO_CREDENTIAL_KEY"`
	// ZLM 媒体平面连接配置，容器由 License modules.vision 能力启停。
	ZLMAPIURL     string `json:"zlmApiUrl,optional,env=ZLM_API_URL"`
	ZLMSecret     string `json:"zlmSecret,optional,env=ZLM_SECRET"`
	ZLMStreamHost string `json:"zlmStreamHost,optional,env=ZLM_STREAM_HOST"`
	ZLMRTSPPort   int    `json:"zlmRtspPort,optional,env=ZLM_RTSP_PORT"`
	ZLMStreamPort int    `json:"zlmStreamPort,optional,env=ZLM_STREAM_PORT"`
	ZLMRTCPort    int    `json:"zlmRtcPort,optional,env=ZLM_RTC_PORT"`
	// Vision 执行面(worker)对接:worker 回调 master 的 base URL 与内部鉴权 token。
	WorkerCallbackURL string `json:"workerCallbackUrl,optional,env=VISION_MASTER_URL"`
	WorkerToken       string `json:"workerToken,optional,env=VISION_WORKER_TOKEN"`
	// 内置算法包 catalog 目录(models.json + algorithms/*.json),启动时同步进注册表。
	CatalogDir string `json:"catalogDir,optional,env=VISION_CATALOG_DIR"`
}

// AIGatewayConf controls the Enterprise-owned AI credential vault and proxy.
// The service tokens authenticate workloads; they are never provider API keys.
type AIGatewayConf struct {
	CredentialKey string `json:"credentialKey,optional,env=AI_GATEWAY_CREDENTIAL_KEY"`
	NotebookToken string `json:"notebookToken,optional,env=AI_GATEWAY_NOTEBOOK_TOKEN"`
	UNSAgentToken string `json:"unsAgentToken,optional,env=AI_GATEWAY_UNS_AGENT_TOKEN"`
	// AllowedHosts is an optional operator-owned hard ceiling for public provider
	// hosts. Administrators maintain the normal trust list in AI Settings.
	AllowedHosts string `json:"allowedHosts,optional,env=AI_GATEWAY_ALLOWED_HOSTS"`
	// AllowedPrivateHosts contains operator-approved private-network exceptions.
	// It never replaces the administrator-managed trust list for hostnames.
	AllowedPrivateHosts string `json:"allowedPrivateHosts,optional,env=AI_GATEWAY_ALLOWED_PRIVATE_HOSTS"`
}

// AppGatewayConfig 应用网关配置
type AppGatewayConfig struct {
	Enabled               bool   `json:"enabled,optional,env=APP_GATEWAY_ENABLED,default=true"`
	SkipAuth              bool   `json:"skipAuth,optional,env=APP_GATEWAY_SKIP_AUTH"`
	CertPath              string `json:"certPath,optional,env=APP_GATEWAY_CERT_PATH,default=/app/certs/cert.pem"`
	KeyPath               string `json:"keyPath,optional,env=APP_GATEWAY_KEY_PATH,default=/app/certs/key.pem"`
	DefaultAccessMode     string `json:"defaultAccessMode,optional,env=APP_GATEWAY_ACCESS_MODE,default=port"` // port | subdomain
	SubdomainSuffix       string `json:"subdomainSuffix,optional,env=APP_GATEWAY_DOMAIN_SUFFIX"`
	Scheme                string `json:"scheme,optional,env=APP_GATEWAY_SCHEME"`
	ListenerScheme        string `json:"listenerScheme,optional,env=APP_GATEWAY_LISTENER_SCHEME"`
	PortRangeStart        int    `json:"portRangeStart,optional,env=APP_GATEWAY_PORT_RANGE_START,default=30000"`
	PortRangeEnd          int    `json:"portRangeEnd,optional,env=APP_GATEWAY_PORT_RANGE_END,default=30099"`
	AppHostPortRangeStart int    `json:"appHostPortRangeStart,optional,env=APP_HOST_PORT_RANGE_START,default=30100"`
	AppHostPortRangeEnd   int    `json:"appHostPortRangeEnd,optional,env=APP_HOST_PORT_RANGE_END,default=30199"`
	LoginURL              string `json:"loginURL,optional,env=APP_GATEWAY_LOGIN_URL"`
	ExternalHost          string `json:"externalHost,optional,env=APP_GATEWAY_EXTERNAL_HOST"`
	ExternalPort          int    `json:"externalPort,optional,env=APP_GATEWAY_EXTERNAL_PORT"`
}

// NormalizeAppGatewayScheme resolves the public AppGateway protocol. APP_GATEWAY_SCHEME
// takes precedence; when it is empty, imported app URLs follow the platform
// entrance protocol.
func NormalizeAppGatewayScheme(scheme string) string {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme == "" {
		scheme = strings.ToLower(strings.TrimSpace(os.Getenv("ENTRANCE_PROTOCOL")))
	}
	if scheme == "https" {
		return "https"
	}
	return "http"
}

// AppGatewayPublicAuthority returns the public host[:port] used by subdomain
// App URLs. Legacy environments without APP_GATEWAY_EXTERNAL_PORT keep the
// historical host-only form, while non-standard public ports stay reachable.
func AppGatewayPublicAuthority(c AppGatewayConfig, host string) string {
	host = strings.TrimSpace(host)
	port := c.ExternalPort
	scheme := NormalizeAppGatewayScheme(c.Scheme)
	if host == "" || port <= 0 || (scheme == "http" && port == 80) || (scheme == "https" && port == 443) {
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// NormalizeAppGatewayListenerScheme resolves the transport used by AppGateway
// listeners. The installer provides the derived internal value; direct binary
// users fall back to the public scheme when it is omitted.
func NormalizeAppGatewayListenerScheme(listenerScheme, publicScheme string) string {
	listenerScheme = strings.ToLower(strings.TrimSpace(listenerScheme))
	if listenerScheme == "" {
		listenerScheme = strings.ToLower(strings.TrimSpace(publicScheme))
	}
	if listenerScheme == "https" {
		return "https"
	}
	return "http"
}

// ValidateAppGatewayConfig validates the canonical public access-mode contract.
// Domain suffixes are always explicit; the Backend never derives them from the
// entrance domain because doing so changes DNS and certificate requirements.
func ValidateAppGatewayConfig(c AppGatewayConfig) error {
	if c.ExternalPort < 0 || c.ExternalPort > 65535 {
		return fmt.Errorf("APP_GATEWAY_EXTERNAL_PORT must be between 1 and 65535 when set, got %d", c.ExternalPort)
	}
	switch c.DefaultAccessMode {
	case "port":
		return nil
	case "subdomain":
		if err := validateDomainSuffix(c.SubdomainSuffix); err != nil {
			return fmt.Errorf("APP_GATEWAY_ACCESS_MODE=subdomain requires a valid APP_GATEWAY_DOMAIN_SUFFIX: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("APP_GATEWAY_ACCESS_MODE must be port or subdomain, got %q", c.DefaultAccessMode)
	}
}

func validateDomainSuffix(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("value is empty")
	}
	if strings.EqualFold(value, "localhost") || net.ParseIP(value) != nil {
		return fmt.Errorf("value must be a DNS parent domain, got %q", value)
	}
	if len(value) > 253 || strings.ContainsAny(value, "*/:\\") || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return fmt.Errorf("value must not contain a wildcard, scheme, port, path, or trailing dot: %q", value)
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return fmt.Errorf("value must contain at least two DNS labels: %q", value)
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("invalid DNS label %q", label)
		}
		for _, ch := range label {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
				return fmt.Errorf("invalid DNS label %q", label)
			}
		}
	}
	return nil
}

// Tier0SdkConfig Tier0 SDK 环境变量配置
type Tier0SdkConfig struct {
	ApiHost  string `json:"apiHost,optional,env=TIER0_SDK_API_HOST"`   // host:port，不带协议前缀
	MqttHost string `json:"mqttHost,optional,env=TIER0_SDK_MQTT_HOST"` // empty: derive from DataIngest
	MqttPort string `json:"mqttPort,optional,env=TIER0_SDK_MQTT_PORT"` // internal WebSocket listener override
}

// ExportConfig UNS 导入导出配置
type ExportConfig struct {
	BuffeSize          int   `json:"buffeSize,optional,default=4096"`
	BatchSize          int   `json:"batchSize,optional,default=1000"`
	LimitSmallFileRows int64 `json:"limitSmallFileRows,optional,default=10000"`
}

func (c *Config) Normalize() {
	c.NativeApps.CredentialKey = expandEnv(c.NativeApps.CredentialKey)
	if c.NativeApps.CredentialKey == "" {
		c.NativeApps.CredentialKey = getenv("APP_CREDENTIAL_KEY", "")
	}
	c.NativeApps.Root = expandEnv(c.NativeApps.Root)
	c.NativeApps.Node = expandEnv(c.NativeApps.Node)
	c.NativeApps.NPMCLI = expandEnv(c.NativeApps.NPMCLI)
	c.NativeApps.PNPMCLI = expandEnv(c.NativeApps.PNPMCLI)
	if c.NativeApps.Root == "" {
		c.NativeApps.Root = getenv("APP_RUNTIME_ROOT", "")
	}
	if c.NativeApps.Node == "" {
		c.NativeApps.Node = getenv("APP_NODE_BINARY", "")
	}
	if c.NativeApps.NPMCLI == "" {
		c.NativeApps.NPMCLI = getenv("APP_NPM_CLI", "")
	}
	if c.NativeApps.PNPMCLI == "" {
		c.NativeApps.PNPMCLI = getenv("APP_PNPM_CLI", "")
	}
	c.SourceStream.ExecutorSockets = expandEnv(c.SourceStream.ExecutorSockets)
	if c.SourceStream.ExecutorSockets == "" {
		c.SourceStream.ExecutorSockets = getenv("STREAM_EXECUTOR_SOCKETS", "")
	}
	c.SourceStream.ExecutorSocket = expandEnv(c.SourceStream.ExecutorSocket)
	if c.SourceStream.ExecutorSocket == "" {
		c.SourceStream.ExecutorSocket = getenv("STREAM_EXECUTOR_SOCKET", "/run/stream-executor/executor.sock")
	}
	c.SourceStream.OPCUASocket = expandEnv(c.SourceStream.OPCUASocket)
	if c.SourceStream.OPCUASocket == "" {
		c.SourceStream.OPCUASocket = getenv("SOURCE_OPCUA_SOCKET", "/run/source-opcua/runtime.sock")
	}
	c.SourceStream.CredentialKey = expandEnv(c.SourceStream.CredentialKey)
	c.SourceStream.ModbusSocket = expandEnv(c.SourceStream.ModbusSocket)
	if c.SourceStream.ModbusSocket == "" {
		c.SourceStream.ModbusSocket = getenv("SOURCE_MODBUS_SOCKET", "/run/source-modbus/runtime.sock")
	}
	if c.SourceStream.CredentialKey == "" {
		c.SourceStream.CredentialKey = getenv("SOURCE_CREDENTIAL_KEY", "")
	}
	c.SourceStream.NATSURL = expandEnv(c.SourceStream.NATSURL)
	if c.SourceStream.NATSURL == "" {
		c.SourceStream.NATSURL = getenv("SOURCE_STREAM_NATS_URL", "")
	}
	if c.Name == "" {
		c.Name = "backend"
	}
	c.ProductVersion = strings.TrimSpace(getenv("PRODUCT_VERSION", c.ProductVersion))
	c.MCP.Disabled = !getenvBool("MCP_ENABLED", !c.MCP.Disabled)
	c.MCP.PublicURL = strings.TrimSpace(getenv("MCP_PUBLIC_URL", c.MCP.PublicURL))
	if c.ProductVersion == "" {
		c.ProductVersion = "dev"
	}
	if c.Host == "" {
		c.Host = "0.0.0.0"
	}
	if c.Port == 0 {
		c.Port = 8080
	}
	c.Database.UnsDbUrl = expandEnv(c.Database.UnsDbUrl)
	c.Database.SinkDbUrl = expandEnv(c.Database.SinkDbUrl)
	c.Database.TimeseriesRetentionYears = NormalizeTimeseriesRetentionYears(c.Database.TimeseriesRetentionYears)
	c.TenantPG.RoleSecret = expandEnv(c.TenantPG.RoleSecret)
	if c.TenantPG.RoleSecret == "" {
		c.TenantPG.RoleSecret = getenv("TENANT_PG_ROLE_SECRET", "")
	}
	if c.TenantPG.ConnectTimeout <= 0 {
		c.TenantPG.ConnectTimeout = getenvDuration("TENANT_PG_CONNECT_TIMEOUT", 10*time.Second)
	}
	if c.TenantPG.MaxOpenConns <= 0 {
		c.TenantPG.MaxOpenConns = getenvInt("TENANT_PG_MAX_OPEN_CONNS", 5)
	}
	if c.TenantPG.MaxIdleConns <= 0 {
		c.TenantPG.MaxIdleConns = getenvInt("TENANT_PG_MAX_IDLE_CONNS", 2)
	}
	if c.TenantPG.ConnCacheSize <= 0 {
		c.TenantPG.ConnCacheSize = getenvInt("TENANT_PG_CONN_CACHE_SIZE", 100)
	}
	c.Log.Mode = getenv("BACKEND_LOG_MODE", c.Log.Mode)
	c.Log.Encoding = getenv("BACKEND_LOG_ENCODING", c.Log.Encoding)
	c.Log.Path = expandEnv(getenv("BACKEND_LOG_PATH", c.Log.Path))
	c.Log.Level = getenv("BACKEND_LOG_LEVEL", c.Log.Level)
	c.Log.Rotation = getenv("BACKEND_LOG_ROTATION", c.Log.Rotation)
	c.Log.Compress = getenvBool("BACKEND_LOG_COMPRESS", c.Log.Compress)
	c.Log.KeepDays = getenvInt("BACKEND_LOG_KEEP_DAYS", c.Log.KeepDays)
	c.Redis.Addr = expandEnv(c.Redis.Addr)
	c.Redis.Password = expandEnv(c.Redis.Password)
	c.Security.JwtSecret = expandEnv(c.Security.JwtSecret)
	c.Security.InitialAdminPassword = expandEnv(c.Security.InitialAdminPassword)
	c.Security.PluginAPIKey = expandEnv(c.Security.PluginAPIKey)
	c.ExternalLogin.Enabled = getenvBool("EXTERNAL_LOGIN_ENABLED", c.ExternalLogin.Enabled)
	c.MicrosoftSSO.Enabled = getenvBool("MICROSOFT_SSO_ENABLED", c.MicrosoftSSO.Enabled)
	c.MicrosoftSSO.TenantID = strings.TrimSpace(expandEnv(getenv("MICROSOFT_SSO_TENANT_ID", c.MicrosoftSSO.TenantID)))
	c.MicrosoftSSO.ClientID = strings.TrimSpace(expandEnv(getenv("MICROSOFT_SSO_CLIENT_ID", c.MicrosoftSSO.ClientID)))
	c.MicrosoftSSO.ClientSecret = strings.TrimSpace(expandEnv(getenv("MICROSOFT_SSO_CLIENT_SECRET", c.MicrosoftSSO.ClientSecret)))
	c.MicrosoftSSO.RedirectURL = strings.TrimSpace(expandEnv(getenv("MICROSOFT_SSO_REDIRECT_URL", c.MicrosoftSSO.RedirectURL)))
	c.CertFile = expandEnv(c.CertFile)
	c.KeyFile = expandEnv(c.KeyFile)
	// Backend HTTPS listener 独立配置：8080 HTTP listener 不读顶层证书。
	c.Https.Port = getenvInt("BACKEND_HTTPS_PORT", c.Https.Port)
	c.Https.CertFile = expandEnv(getenv("BACKEND_TLS_CERT_FILE", c.Https.CertFile))
	c.Https.KeyFile = expandEnv(getenv("BACKEND_TLS_KEY_FILE", c.Https.KeyFile))
	if c.Https.CertFile == "" {
		c.Https.CertFile = "/app/certs/cert.pem"
	}
	if c.Https.KeyFile == "" {
		c.Https.KeyFile = "/app/certs/key.pem"
	}
	c.Gateway.WebDir = expandEnv(c.Gateway.WebDir)
	c.Gateway.FrontendDevUrl = expandEnv(c.Gateway.FrontendDevUrl)
	c.Gateway.LaunchpadPort = expandEnv(c.Gateway.LaunchpadPort)
	c.Gateway.LaunchpadDomain = strings.TrimSpace(getenv("LAUNCHPAD_DOMAIN", c.Gateway.LaunchpadDomain))
	c.Gateway.SourceFlowUrl = expandEnv(c.Gateway.SourceFlowUrl)
	c.Gateway.EventFlowUrl = expandEnv(c.Gateway.EventFlowUrl)
	c.Gateway.MarimoUrl = expandEnv(c.Gateway.MarimoUrl)
	c.Gateway.EmqxUrl = expandEnv(c.Gateway.EmqxUrl)
	c.Gateway.UnsAgentUrl = expandEnv(c.Gateway.UnsAgentUrl)
	c.Gateway.TrustProxyHeaders = getenvBool("GATEWAY_TRUST_PROXY_HEADERS", c.Gateway.TrustProxyHeaders)
	c.Storage.FileRoot = expandEnv(c.Storage.FileRoot)
	c.Storage.MarimoFileRoot = expandEnv(c.Storage.MarimoFileRoot)
	c.Storage.Driver = expandEnv(c.Storage.Driver)
	c.Storage.PublicBaseURL = expandEnv(c.Storage.PublicBaseURL)
	c.Storage.S3.Endpoint = expandEnv(c.Storage.S3.Endpoint)
	c.Storage.S3.Region = expandEnv(c.Storage.S3.Region)
	c.Storage.S3.Bucket = expandEnv(c.Storage.S3.Bucket)
	c.Storage.S3.PublicBucket = expandEnv(c.Storage.S3.PublicBucket)
	c.Storage.S3.AccessKeyID = expandEnv(c.Storage.S3.AccessKeyID)
	c.Storage.S3.SecretAccessKey = expandEnv(c.Storage.S3.SecretAccessKey)
	c.Storage.S3.SessionToken = expandEnv(c.Storage.S3.SessionToken)
	c.Storage.S3.PublicEndpoint = expandEnv(c.Storage.S3.PublicEndpoint)
	c.License.LicenseDir = expandEnv(c.License.LicenseDir)
	c.License.MachineIDPath = expandEnv(c.License.MachineIDPath)
	c.FleetAgent.LocalURL = expandEnv(c.FleetAgent.LocalURL)
	c.FleetAgent.LocalToken = expandEnv(c.FleetAgent.LocalToken)
	c.DataIngest.MqttBrokers = expandEnv(c.DataIngest.MqttBrokers)
	c.DataIngest.MqttClientID = expandEnv(c.DataIngest.MqttClientID)
	c.DataIngest.MqttUsername = expandEnv(c.DataIngest.MqttUsername)
	c.DataIngest.MqttPassword = expandEnv(c.DataIngest.MqttPassword)
	c.DataIngest.MqttTopic = expandEnv(c.DataIngest.MqttTopic)
	c.Cluster.NodeKey = expandEnv(c.Cluster.NodeKey)
	c.Cluster.NodeName = expandEnv(c.Cluster.NodeName)
	c.Cluster.ExternalHTTPBaseURL = expandEnv(c.Cluster.ExternalHTTPBaseURL)
	c.Cluster.ExternalMqttBrokerHost = expandEnv(c.Cluster.ExternalMqttBrokerHost)
	c.Cluster.MountMode = expandEnv(c.Cluster.MountMode)
	c.Video.CredentialKey = expandEnv(c.Video.CredentialKey)
	if c.Video.CredentialKey == "" {
		c.Video.CredentialKey = getenv("VIDEO_CREDENTIAL_KEY", "")
	}
	c.Video.ZLMAPIURL = expandEnv(c.Video.ZLMAPIURL)
	if c.Video.ZLMAPIURL == "" {
		c.Video.ZLMAPIURL = getenv("ZLM_API_URL", "http://zlm")
	}
	c.Video.ZLMSecret = expandEnv(c.Video.ZLMSecret)
	if c.Video.ZLMSecret == "" {
		c.Video.ZLMSecret = getenv("ZLM_SECRET", "")
	}
	c.Video.ZLMStreamHost = expandEnv(c.Video.ZLMStreamHost)
	if c.Video.ZLMStreamHost == "" {
		c.Video.ZLMStreamHost = getenv("ZLM_STREAM_HOST", "zlm")
	}
	if c.Video.ZLMRTSPPort == 0 {
		c.Video.ZLMRTSPPort = getenvInt("ZLM_RTSP_PORT", 554)
	}
	if c.Video.ZLMStreamPort == 0 {
		c.Video.ZLMStreamPort = getenvInt("ZLM_STREAM_PORT", c.Video.ZLMRTSPPort)
	}
	if c.Video.ZLMRTCPort == 0 {
		c.Video.ZLMRTCPort = getenvInt("ZLM_RTC_PORT", 8000)
	}
	c.Video.WorkerCallbackURL = expandEnv(c.Video.WorkerCallbackURL)
	if c.Video.WorkerCallbackURL == "" {
		c.Video.WorkerCallbackURL = getenv("VISION_MASTER_URL", "http://127.0.0.1:8288")
	}
	c.Video.WorkerToken = expandEnv(c.Video.WorkerToken)
	if c.Video.WorkerToken == "" {
		c.Video.WorkerToken = getenv("VISION_WORKER_TOKEN", "")
	}
	c.Video.CatalogDir = expandEnv(c.Video.CatalogDir)
	if c.Video.CatalogDir == "" {
		c.Video.CatalogDir = getenv("VISION_CATALOG_DIR", "etc/vision/catalog")
	}
	c.AIGateway.CredentialKey = expandEnv(c.AIGateway.CredentialKey)
	if c.AIGateway.CredentialKey == "" {
		c.AIGateway.CredentialKey = getenv("AI_GATEWAY_CREDENTIAL_KEY", "")
	}
	c.AIGateway.NotebookToken = expandEnv(c.AIGateway.NotebookToken)
	if c.AIGateway.NotebookToken == "" {
		c.AIGateway.NotebookToken = getenv("AI_GATEWAY_NOTEBOOK_TOKEN", "")
	}
	c.AIGateway.UNSAgentToken = expandEnv(c.AIGateway.UNSAgentToken)
	if c.AIGateway.UNSAgentToken == "" {
		c.AIGateway.UNSAgentToken = getenv("AI_GATEWAY_UNS_AGENT_TOKEN", "")
	}
	c.AIGateway.AllowedHosts = expandEnv(c.AIGateway.AllowedHosts)
	if c.AIGateway.AllowedHosts == "" {
		c.AIGateway.AllowedHosts = getenv("AI_GATEWAY_ALLOWED_HOSTS", "")
	}
	c.AIGateway.AllowedPrivateHosts = expandEnv(c.AIGateway.AllowedPrivateHosts)
	if c.AIGateway.AllowedPrivateHosts == "" {
		c.AIGateway.AllowedPrivateHosts = getenv("AI_GATEWAY_ALLOWED_PRIVATE_HOSTS", "")
	}
	if c.Database.UnsDbUrl == "" {
		c.Database.UnsDbUrl = getenv("UNS_DB_URL", "postgres://postgres:postgres@localhost:5432/edge?sslmode=disable")
	}
	if c.Database.SinkDbUrl == "" {
		c.Database.SinkDbUrl = getenv("SINK_DB_URL", c.Database.UnsDbUrl)
	}
	if c.Redis.Driver == "" {
		c.Redis.Driver = getenv("CACHE_DRIVER", product.ProfileFor(runtimeenv.Edition()).Adapters["cache"])
	}
	if c.Redis.Addr == "" {
		c.Redis.Addr = getenv("REDIS_ADDR", "localhost:6379")
	}
	if c.Redis.Password == "" {
		c.Redis.Password = getenv("REDIS_PASSWORD", "")
	}
	if c.Security.JwtSecret == "" {
		c.Security.JwtSecret = getenv("JWT_SECRET", "dev-only-change-me")
	}
	if c.Security.InitialAdminPassword == "" {
		c.Security.InitialAdminPassword = getenv("ADMIN_INITIAL_PASSWORD", "tier0")
	}
	if c.Security.PluginAPIKey == "" {
		c.Security.PluginAPIKey = getenv("TIER0_API_KEY", "")
	}
	if c.Gateway.WebDir == "" {
		c.Gateway.WebDir = getenv("WEB_DIR", "./web")
	}
	c.Gateway.LocalFrontendDev = getenvBool("LOCAL_FRONTEND_DEV", c.Gateway.LocalFrontendDev)
	if c.Gateway.FrontendDevUrl == "" {
		c.Gateway.FrontendDevUrl = getenv("FRONTEND_DEV_PROXY_URL", getenv("FRONTEND_DEV_URL", ""))
	}
	if c.Gateway.AnchorDevProxyUrl == "" {
		c.Gateway.AnchorDevProxyUrl = getenv("ANCHOR_DEV_PROXY_URL", "")
	}
	if c.Gateway.LaunchpadPort == "" {
		c.Gateway.LaunchpadPort = getenv("LAUNCHPAD_PORT", "")
	}
	if c.Gateway.SourceFlowUrl == "" {
		c.Gateway.SourceFlowUrl = getenv("SOURCEFLOW_URL", "http://sourceflow:1880")
	}
	if c.Gateway.EventFlowUrl == "" {
		c.Gateway.EventFlowUrl = getenv("EVENTFLOW_URL", "http://eventflow:1880")
	}
	if c.Gateway.MarimoUrl == "" {
		c.Gateway.MarimoUrl = getenv("MARIMO_URL", "http://marimo:8080")
	}
	if c.Gateway.EmqxUrl == "" {
		c.Gateway.EmqxUrl = getenv("EMQX_URL", "http://emqx:18083")
	}
	if c.Gateway.UnsAgentUrl == "" {
		c.Gateway.UnsAgentUrl = getenv("UNS_AGENT_URL", "http://uns-agent-api:9650")
	}
	if c.Storage.FileRoot == "" {
		c.Storage.FileRoot = getenv("FILESTORE_LOCAL_ROOT", "./data/files")
	}
	if c.Storage.MarimoFileRoot == "" {
		c.Storage.MarimoFileRoot = getenv("MARIMO_FILE_ROOT", "./data/marimo")
	}
	if c.Storage.Driver == "" {
		c.Storage.Driver = getenv("FILESTORE_DRIVER", product.ProfileFor(runtimeenv.Edition()).Adapters["storage"])
	}
	if c.Storage.PublicBaseURL == "" {
		c.Storage.PublicBaseURL = getenv("FILESTORE_PUBLIC_BASE_URL", "")
	}
	if c.Storage.S3.Endpoint == "" {
		c.Storage.S3.Endpoint = getenv("FILESTORE_S3_ENDPOINT", "")
	}
	if c.Storage.S3.Region == "" {
		c.Storage.S3.Region = getenv("FILESTORE_S3_REGION", "")
	}
	if c.Storage.S3.Bucket == "" {
		c.Storage.S3.Bucket = getenv("FILESTORE_S3_BUCKET", "")
	}
	if c.Storage.S3.PublicBucket == "" {
		c.Storage.S3.PublicBucket = getenv("FILESTORE_S3_PUBLIC_BUCKET", "")
	}
	if c.Storage.S3.AccessKeyID == "" {
		c.Storage.S3.AccessKeyID = getenv("FILESTORE_S3_ACCESS_KEY_ID", "")
	}
	if c.Storage.S3.SecretAccessKey == "" {
		c.Storage.S3.SecretAccessKey = getenv("FILESTORE_S3_SECRET_ACCESS_KEY", "")
	}
	if c.Storage.S3.SessionToken == "" {
		c.Storage.S3.SessionToken = getenv("FILESTORE_S3_SESSION_TOKEN", "")
	}
	if c.Storage.S3.PublicEndpoint == "" {
		c.Storage.S3.PublicEndpoint = getenv("FILESTORE_S3_PUBLIC_ENDPOINT", "")
	}
	c.Storage.S3.UseSSL = getenvBool("FILESTORE_S3_USE_SSL", true)
	c.Storage.S3.PathStyle = getenvBool("FILESTORE_S3_PATH_STYLE", c.Storage.S3.PathStyle)
	c.Storage.S3.BootstrapBuckets = getenvBool("FILESTORE_S3_BOOTSTRAP_BUCKETS", c.Storage.S3.BootstrapBuckets)
	if c.License.LicenseDir == "" {
		c.License.LicenseDir = getenv("LICENSE_DIR", "/app/go-edge/license")
	}
	if c.License.MachineIDPath == "" {
		c.License.MachineIDPath = getenv("MACHINE_ID_PATH", "/host/machine-id")
	}
	c.DataIngest.Enabled = getenvBool("DATAINGEST_ENABLED", c.DataIngest.Enabled)
	c.DataQuality.Enabled = getenvBool("DATA_QUALITY_ENABLED", c.DataQuality.Enabled)
	if c.DataQuality.ShardCount <= 0 {
		c.DataQuality.ShardCount = getenvInt("DATA_QUALITY_SHARD_COUNT", 64)
	}
	if c.DataQuality.MaxEnabledNodes <= 0 {
		c.DataQuality.MaxEnabledNodes = getenvInt("DATA_QUALITY_MAX_ENABLED_NODES", 10000)
	}
	if c.DataQuality.IssueQueueSize <= 0 {
		c.DataQuality.IssueQueueSize = getenvInt("DATA_QUALITY_ISSUE_QUEUE_SIZE", 1024)
	}
	c.DataQuality.MaxCollectWindowMs = getenvInt("DATA_QUALITY_MAX_COLLECT_WINDOW_MS", c.DataQuality.MaxCollectWindowMs)
	if c.DataQuality.MaxCollectWindowMs <= 0 {
		c.DataQuality.MaxCollectWindowMs = 600000
	}
	if c.DataIngest.MqttBrokers == "" {
		c.DataIngest.MqttBrokers = getenv("DATAINGEST_MQTT_BROKERS", DefaultMQTTBrokers)
	}
	if c.DataIngest.MqttClientID == "" {
		c.DataIngest.MqttClientID = getenv("DATAINGEST_MQTT_CLIENT_ID", "edge-backend-sink")
	}
	if c.DataIngest.MqttUsername == "" {
		c.DataIngest.MqttUsername = getenv("DATAINGEST_MQTT_USERNAME", "backend")
	}
	if c.DataIngest.MqttPassword == "" {
		c.DataIngest.MqttPassword = getenv("DATAINGEST_MQTT_PASSWORD", secrets.InternalMQTTPassword("backend"))
	}
	if c.DataIngest.MqttTopic == "" {
		c.DataIngest.MqttTopic = getenv("DATAINGEST_MQTT_TOPIC", "#")
	}
	if c.DataIngest.QueueSize <= 0 {
		c.DataIngest.QueueSize = getenvInt("DATAINGEST_QUEUE_SIZE", 20000)
	}
	if c.DataIngest.BatchSize <= 0 {
		c.DataIngest.BatchSize = getenvInt("DATAINGEST_BATCH_SIZE", 5000)
	}
	if c.DataIngest.FlushIntervalMs <= 0 {
		c.DataIngest.FlushIntervalMs = getenvInt("DATAINGEST_FLUSH_INTERVAL_MS", 1000)
	}
	if c.Cluster.NodeKey == "" {
		c.Cluster.NodeKey = defaultNodeKey(c.License.LicenseDir)
	}
	if c.Cluster.NodeName == "" {
		c.Cluster.NodeName = c.Cluster.NodeKey
	}
	if c.Cluster.ExternalHTTPBaseURL == "" {
		c.Cluster.ExternalHTTPBaseURL = defaultExternalHTTPBaseURL(c.AppGateway.Scheme)
	}
	if c.Cluster.ExternalMqttBrokerHost == "" {
		c.Cluster.ExternalMqttBrokerHost = defaultExternalMqttBrokerHost()
	}
	if c.Cluster.MountMode == "" {
		c.Cluster.MountMode = "multiRoots"
	}
	if !c.Cluster.SyncMetricEnabled {
		c.Cluster.SyncMetricEnabled = true
	}

	// AppGateway
	c.AppGateway.Enabled = getenvBool("APP_GATEWAY_ENABLED", c.AppGateway.Enabled)
	c.AppGateway.PortRangeStart = getenvInt("APP_GATEWAY_PORT_RANGE_START", c.AppGateway.PortRangeStart)
	c.AppGateway.PortRangeEnd = getenvInt("APP_GATEWAY_PORT_RANGE_END", c.AppGateway.PortRangeEnd)
	if c.AppGateway.PortRangeStart == 0 {
		logx.Infof("[AppGateway] [WARN] APP_GATEWAY_PORT_RANGE_START 未配置或为零，使用默认值 30000")
		c.AppGateway.PortRangeStart = 30000
	}
	if c.AppGateway.PortRangeEnd == 0 {
		logx.Infof("[AppGateway] [WARN] APP_GATEWAY_PORT_RANGE_END 未配置或为零，使用默认值 30099")
		c.AppGateway.PortRangeEnd = 30099
	}
	if c.AppGateway.PortRangeStart > c.AppGateway.PortRangeEnd {
		logx.Infof("[AppGateway] [WARN] 端口范围起始值(%d)大于结束值(%d)，已自动交换", c.AppGateway.PortRangeStart, c.AppGateway.PortRangeEnd)
		c.AppGateway.PortRangeStart, c.AppGateway.PortRangeEnd = c.AppGateway.PortRangeEnd, c.AppGateway.PortRangeStart
	}
	if c.AppGateway.PortRangeEnd-c.AppGateway.PortRangeStart+1 < 10 {
		logx.Infof("[AppGateway] [WARN] 端口范围数量不足 10 个(%d-%d)，已重置为默认值 30000-30099", c.AppGateway.PortRangeStart, c.AppGateway.PortRangeEnd)
		c.AppGateway.PortRangeStart = 30000
		c.AppGateway.PortRangeEnd = 30099
	}
	c.AppGateway.AppHostPortRangeStart = getenvInt("APP_HOST_PORT_RANGE_START", c.AppGateway.AppHostPortRangeStart)
	c.AppGateway.AppHostPortRangeEnd = getenvInt("APP_HOST_PORT_RANGE_END", c.AppGateway.AppHostPortRangeEnd)
	if c.AppGateway.AppHostPortRangeStart == 0 {
		logx.Infof("[AppGateway] [WARN] APP_HOST_PORT_RANGE_START 未配置或为零，使用默认值 30100")
		c.AppGateway.AppHostPortRangeStart = 30100
	}
	if c.AppGateway.AppHostPortRangeEnd == 0 {
		logx.Infof("[AppGateway] [WARN] APP_HOST_PORT_RANGE_END 未配置或为零，使用默认值 30199")
		c.AppGateway.AppHostPortRangeEnd = 30199
	}
	if c.AppGateway.AppHostPortRangeStart > c.AppGateway.AppHostPortRangeEnd {
		logx.Infof("[AppGateway] [WARN] 应用宿主机端口范围起始值(%d)大于结束值(%d)，已自动交换", c.AppGateway.AppHostPortRangeStart, c.AppGateway.AppHostPortRangeEnd)
		c.AppGateway.AppHostPortRangeStart, c.AppGateway.AppHostPortRangeEnd = c.AppGateway.AppHostPortRangeEnd, c.AppGateway.AppHostPortRangeStart
	}
	if c.AppGateway.AppHostPortRangeEnd-c.AppGateway.AppHostPortRangeStart+1 < 10 {
		logx.Infof("[AppGateway] [WARN] 应用宿主机端口范围数量不足 10 个(%d-%d)，已重置为默认值 30100-30199", c.AppGateway.AppHostPortRangeStart, c.AppGateway.AppHostPortRangeEnd)
		c.AppGateway.AppHostPortRangeStart = 30100
		c.AppGateway.AppHostPortRangeEnd = 30199
	}
	c.AppGateway.DefaultAccessMode = strings.ToLower(strings.TrimSpace(
		getenv("APP_GATEWAY_ACCESS_MODE", c.AppGateway.DefaultAccessMode),
	))
	if c.AppGateway.DefaultAccessMode == "" {
		c.AppGateway.DefaultAccessMode = "port"
	}
	c.AppGateway.CertPath = expandEnv(c.AppGateway.CertPath)
	c.AppGateway.KeyPath = expandEnv(c.AppGateway.KeyPath)
	c.AppGateway.SubdomainSuffix = strings.ToLower(strings.TrimSpace(
		getenv("APP_GATEWAY_DOMAIN_SUFFIX", expandEnv(c.AppGateway.SubdomainSuffix)),
	))
	c.AppGateway.LoginURL = expandEnv(c.AppGateway.LoginURL)
	c.AppGateway.ExternalHost = expandEnv(c.AppGateway.ExternalHost)
	c.AppGateway.ExternalPort = getenvInt("APP_GATEWAY_EXTERNAL_PORT", c.AppGateway.ExternalPort)
	c.AppGateway.Scheme = NormalizeAppGatewayScheme(expandEnv(c.AppGateway.Scheme))
	c.AppGateway.ListenerScheme = NormalizeAppGatewayListenerScheme(
		expandEnv(getenv("APP_GATEWAY_LISTENER_SCHEME", c.AppGateway.ListenerScheme)),
		c.AppGateway.Scheme,
	)
	if c.MicrosoftSSO.RedirectURL == "" {
		c.MicrosoftSSO.RedirectURL = defaultMicrosoftSSORedirectURL(c.AppGateway)
	}

	// Tier0Sdk
	c.Tier0Sdk.ApiHost = expandEnv(c.Tier0Sdk.ApiHost)
	c.Tier0Sdk.MqttHost = expandEnv(c.Tier0Sdk.MqttHost)
	c.Tier0Sdk.MqttPort = expandEnv(c.Tier0Sdk.MqttPort)

	// /mqtt 网关代理上游：显式配置优先，默认取 backend 自身 DataIngest broker 的主机
	// + EMQX 内部明文 WS 端口（该 listener 随 EMQX 常开）。不能用 SDKMQTTAddress：
	// TIER0_SDK_MQTT_HOST/PORT 面向 App SDK 的公网接入，可能指向公网域名/WSS 端口，
	// 不是 backend 容器内可达的明文 WS。须在 DataIngest 默认值生效之后取值。
	c.Gateway.MqttWsUpstreamUrl = strings.TrimSpace(expandEnv(getenv("MQTT_WS_UPSTREAM_URL", c.Gateway.MqttWsUpstreamUrl)))
	if c.Gateway.MqttWsUpstreamUrl == "" {
		if endpoint, err := ParseMQTTEndpoint(c.DataIngest.MqttBrokers); err == nil {
			c.Gateway.MqttWsUpstreamUrl = (&url.URL{Scheme: "ws", Host: net.JoinHostPort(endpoint.Host, DefaultMQTTWSPort), Path: "/mqtt"}).String()
		}
	}
	if u, err := url.Parse(c.Cluster.ExternalHTTPBaseURL); err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && (u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == "" {
		if c.Cluster.NetworkBootstrapURL == "" {
			c.Cluster.NetworkBootstrapURL = strings.TrimRight(u.String(), "/")
		}
	}
}
