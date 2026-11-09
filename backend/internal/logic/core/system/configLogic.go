// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package system

import (
	"context"
	"os"
	"strconv"
	"strings"

	domainlicense "backend/internal/domain/license"
	respx "backend/internal/httpx"
	"backend/internal/runtimeenv"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	unsTreeLoadModeAuto = "auto"
	unsTreeLoadModeLazy = "lazy"
	unsTreeLoadModeFull = "full"

	unsTreeLoadMode      = unsTreeLoadModeAuto
	unsTreeAutoThreshold = int64(2000)

	systemQualityField   = "_quality"
	systemTimestampField = "_timestamp"
	systemMQTTTCPPort    = "1883"
	systemMQTTWSPort     = "8083"
	systemMQTTWSSPort    = "8084"
)

type ConfigLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ConfigLogic {
	return &ConfigLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ConfigLogic) Config() (resp *types.Envelope, err error) {
	lazyTree, activeNodeCount := l.unsTreeLoadingConfig()
	productVersion := l.productVersion()
	identity := runtimeenv.Identity()
	return respx.Envelope(map[string]any{
		"appTitle":                  runtimeenv.ProductName(),
		"runtimeMode":               runtimeenv.Mode(),
		"productEdition":            runtimeenv.Edition(),
		"isEdge":                    runtimeenv.IsEdge(),
		"licenseActivationRequired": runtimeenv.ActivationRequired(),
		"edition":                   identity.Edition,
		"platform":                  identity.Platform,
		"deployment":                identity.Deployment,
		"capabilities":              domainlicense.Default().Capabilities(),
		"authEnable":                true,
		"externalLoginProviders":    l.externalLoginProviders(),
		"loginPath":                 "/tier0-login",
		"productVersion":            productVersion,
		"version":                   productVersion,
		"lang":                      configuredLanguage(),
		"lazyTree":                  lazyTree,
		"unsTreeLoadMode":           unsTreeLoadMode,
		"unsTreeAutoThreshold":      unsTreeAutoThreshold,
		"unsTreeActiveNodeCount":    activeNodeCount,
		"enableAutoCategorization":  true,
		"aiChatConfigured":          l.aiChatConfigured(),
		"dataQualityEnabled":        l.dataQualityEnabled(),
		"qualityName":               systemQualityField,
		"timestampName":             systemTimestampField,
		"mqttTcpPort":               configuredPort("OS_MQTT_TCP_PORT", systemMQTTTCPPort),
		"mqttWebsocketPort":         configuredPort("OS_MQTT_WEBSOCKET_PORT", systemMQTTWSPort),
		"mqttWebsocketTslPort":      configuredPort("OS_MQTT_WEBSOCKET_TSL_PORT", systemMQTTWSSPort),
		"mqttWebsocketUrl":          respx.ResolvePublicMQTTWebsocketURL(l.ctx),
		"cluster":                   l.clusterRuntimeConfig(),
		"containerMap":              systemContainerMap(),
	}), nil
}

// nil 表示检查不可用，避免将数据库故障误报为用户尚未配置。
func (l *ConfigLogic) aiChatConfigured() any {
	if l.svcCtx == nil || l.svcCtx.App == nil || l.svcCtx.App.AIGateway == nil {
		return nil
	}
	// 与部署模板中 Agent 的 LLM_BASE_URL 使用同一工作空间；非法配置不能误查默认空间。
	workspaceID := int64(1)
	if value := strings.TrimSpace(os.Getenv("AI_GATEWAY_WORKSPACE_ID")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			logx.WithContext(l.ctx).Error("AI chat configuration check failed: invalid gateway workspace ID")
			return nil
		}
		workspaceID = parsed
	}
	configured, err := l.svcCtx.App.AIGateway.ChatConfigured(l.ctx, workspaceID)
	if err != nil {
		logx.WithContext(l.ctx).Error("AI chat configuration check failed")
		return nil
	}
	return configured
}

func (l *ConfigLogic) dataQualityEnabled() bool {
	return l.svcCtx != nil && l.svcCtx.Config.DataQuality.Enabled
}

func configuredPort(key, fallback string) string {
	if port := strings.TrimSpace(os.Getenv(key)); port != "" {
		return port
	}
	return fallback
}

func (l *ConfigLogic) productVersion() string {
	if l.svcCtx != nil {
		if version := strings.TrimSpace(l.svcCtx.Config.ProductVersion); version != "" {
			return version
		}
	}
	if version := strings.TrimSpace(os.Getenv("PRODUCT_VERSION")); version != "" {
		return version
	}
	return "dev"
}

func (l *ConfigLogic) clusterRuntimeConfig() map[string]any {
	if l.svcCtx == nil {
		return map[string]any{
			"enabled": false,
			"isHost":  false,
			"role":    "edge",
		}
	}
	cfg := l.svcCtx.Config.Cluster
	licenseSvc := domainlicense.Default()
	isHost := false
	if licenseSvc != nil {
		isHost = licenseSvc.IsHost()
	}
	role := "edge"
	if isHost {
		role = "host"
	}
	return map[string]any{
		"enabled":  true,
		"isHost":   isHost,
		"role":     role,
		"nodeKey":  cfg.NodeKey,
		"nodeName": cfg.NodeName,
	}
}

func (l *ConfigLogic) unsTreeLoadingConfig() (bool, int64) {
	if l.svcCtx == nil || l.svcCtx.App == nil || l.svcCtx.App.UNS == nil {
		logx.WithContext(l.ctx).Error("UNS tree loading config fallback: UNS service is unavailable")
		return true, 0
	}
	count, err := l.svcCtx.App.UNS.ActiveNodeCount(l.ctx)
	if err != nil {
		logx.WithContext(l.ctx).Errorf("UNS tree loading config fallback: count active UNS nodes failed: %v", err)
		return true, 0
	}
	return resolveUnsTreeLazy(unsTreeLoadMode, count), count
}

func resolveUnsTreeLazy(mode string, activeNodeCount int64) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case unsTreeLoadModeLazy:
		return true
	case unsTreeLoadModeFull:
		return false
	case unsTreeLoadModeAuto:
		return activeNodeCount > unsTreeAutoThreshold
	default:
		return true
	}
}

func configuredLanguage() string {
	for _, key := range []string{"LANGUAGE", "OS_LANG"} {
		if lang := normalizeLanguage(os.Getenv(key)); lang != "" {
			return lang
		}
	}
	return "en-US"
}

func normalizeLanguage(lang string) string {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(lang), "_", "-")) {
	case "zh", "zh-cn":
		return "zh-CN"
	case "en", "en-us":
		return "en-US"
	default:
		return ""
	}
}

func systemContainerMap() map[string]any {
	return map[string]any{
		"sourceflow": map[string]any{
			"name": "sourceflow",
			"envMap": map[string]any{
				"service_is_show":      true,
				"service_redirect_url": "/nodered/home/",
			},
		},
		"eventflow": map[string]any{
			"name": "eventflow",
			"envMap": map[string]any{
				"service_is_show":      true,
				"service_redirect_url": "/eventflow/home/",
			},
		},
		"oauth": map[string]any{
			"name":        "OAuth Client",
			"description": "Manage OAuth clients for imported apps and external integrations.",
			"envMap": map[string]any{
				"service_is_show":      true,
				"service_redirect_url": "/oauth-clients",
			},
		},
		"cluster": map[string]any{
			"name":        "Cluster",
			"description": "Configure edge collaboration, metadata synchronization, and MQTT data sync.",
			"envMap": map[string]any{
				"service_is_show":      true,
				"service_redirect_url": "/cluster",
			},
		},
		"marimo": map[string]any{
			"name":        "marimo",
			"description": "Reactive Python notebook runtime.",
			"envMap": map[string]any{
				"service_is_show":      true,
				"service_redirect_url": "/marimo/home/",
			},
		},
	}
}

func (l *ConfigLogic) externalLoginProviders() []map[string]string {
	providers := []map[string]string{}
	if l.svcCtx != nil && l.svcCtx.Config.ExternalLogin.Enabled && l.svcCtx.App != nil && l.svcCtx.App.Auth != nil && l.svcCtx.App.Auth.MicrosoftSSOEnabled() {
		providers = append(providers, map[string]string{"id": "microsoft", "name": "Microsoft"})
	}
	return providers
}
