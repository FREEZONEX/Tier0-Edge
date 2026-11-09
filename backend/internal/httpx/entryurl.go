package httpx

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"

	"backend/internal/config"
)

// RebuildGeneratedEntryURL 按"当前请求的对外入口"重建安装期烘入 DB 的生成型应用
// 入口 URL，服务器换 IP / 双网卡后读路径不再返回失效地址。写库路径不得使用本函数。
//
// 规则：
//   - port 模式：raw 为绝对 http(s) URL 且（host 等于配置 ExternalHost，或端口落在
//     应用网关端口范围 PortRange / AppHostPortRange 内）→ 用 ctx authority 的 host
//     替换、保留原端口与 path/query 重建；raw 无端口时直接沿用 ctx authority（含其端口）。
//   - subdomain 模式：raw host 形如 <label>.<配置 SubdomainSuffix>（label 为部署期生成
//     的 shortID 或旧格式 app-<shortID>）→ 请求 host 为域名时取其父域（去掉最左标签）
//     作为新 suffix；请求 host 为 IP / 单标签时用配置 SubdomainSuffix 现算。
//   - 其他（相对路径、manualExternal 手工地址、解析失败、无 ctx authority）→ 原样返回。
//
// scheme 取请求 scheme，无 ctx 时回退配置 Scheme；cfg 为零值时全部不命中、原样返回。
func RebuildGeneratedEntryURL(ctx context.Context, cfg config.AppGatewayConfig, raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return raw
	}
	scheme, authorityHost, ok := AuthorityFrom(ctx)
	if !ok {
		return raw
	}
	scheme = normalizeScheme(scheme, cfg)
	host := HostnameOnly(authorityHost)
	if host == "" {
		return raw
	}
	// subdomain 生成形态优先判定：port 模式的 ExternalHost 也可能是域名，
	// 但只有 <label>.<SubdomainSuffix>（label 不含点）才属于子域应用地址。
	// 重建永远落在当前配置的 SubdomainSuffix 上——网关只按配置 suffix 与
	// 已持久化 route domain 分发，随请求 Host 推导出的其它父域不可路由。
	if label, isSubdomain := generatedSubdomainLabel(u.Host, cfg.SubdomainSuffix); isSubdomain {
		// 请求显式携带端口时保留；否则沿用配置的对外端口归一化
		// （ExternalPort 非标准端口时入口 URL 必须携带，浏览器才能可达）。
		target := config.AppGatewayPublicAuthority(cfg, label+"."+cfg.SubdomainSuffix)
		if _, port, err := net.SplitHostPort(authorityHost); err == nil && port != "" {
			hostname := HostnameOnly(target)
			target = net.JoinHostPort(hostname, port)
		}
		return rebuildURL(u, scheme, target)
	}
	if target := rebuildPortModeHost(u, host, authorityHost, cfg); target != "" {
		return rebuildURL(u, scheme, target)
	}
	return raw
}

// normalizeScheme 校正请求 scheme；非 http/https 时回退配置 Scheme。
func normalizeScheme(scheme string, cfg config.AppGatewayConfig) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https":
		return "https"
	case "http":
		return "http"
	default:
		return config.NormalizeAppGatewayScheme(cfg.Scheme)
	}
}

// HostnameOnly 剥离 host 中的端口（含 IPv6 括号形式）；无端口时去除 IPv6
// 字面量的方括号，避免后续 JoinHostPort 产生 [[...]] 双重括号。
func HostnameOnly(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		if hostname, _, err := net.SplitHostPort(host); err == nil {
			return hostname
		}
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			return host[1 : len(host)-1]
		}
	}
	return host
}

// generatedSubdomainLabel 判断 host 是否形如 <label>.<suffix>（label 为单标签，即
// 部署生成的 shortID 或旧格式 app-<shortID>）。匹配返回 label。
func generatedSubdomainLabel(host, suffix string) (string, bool) {
	suffix = strings.ToLower(strings.TrimSpace(suffix))
	if suffix == "" {
		return "", false
	}
	hostname := strings.ToLower(strings.TrimSuffix(HostnameOnly(host), "."))
	label, ok := strings.CutSuffix(hostname, "."+suffix)
	if !ok || label == "" || strings.Contains(label, ".") {
		return "", false
	}
	return label, true
}

// rebuildPortModeHost 命中 port 模式时返回替换后的 host：raw 带端口时保留原端口
// （拼接请求主机名）；raw 无端口（入口形态地址）时沿用请求 authority 原样（含其端口）。
// 未命中返回空串。
func rebuildPortModeHost(u *url.URL, requestHostname, requestAuthority string, cfg config.AppGatewayConfig) string {
	hostname := HostnameOnly(u.Host)
	if hostname == "" {
		return ""
	}
	hostMatches := cfg.ExternalHost != "" &&
		strings.EqualFold(hostname, strings.TrimSpace(cfg.ExternalHost))
	rawPort := u.Port()
	portMatches := rawPort != "" &&
		(portInRange(rawPort, cfg.PortRangeStart, cfg.PortRangeEnd) ||
			portInRange(rawPort, cfg.AppHostPortRangeStart, cfg.AppHostPortRangeEnd))
	if !hostMatches && !portMatches {
		return ""
	}
	if rawPort != "" {
		return net.JoinHostPort(requestHostname, rawPort)
	}
	return requestAuthority
}

func portInRange(port string, start, end int) bool {
	if start <= 0 || end <= 0 || start > end {
		return false
	}
	value, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	return value >= start && value <= end
}

// rebuildURL 复用 raw 的 path/query，仅替换 scheme 与 host。
func rebuildURL(u *url.URL, scheme, host string) string {
	rebuilt := *u
	rebuilt.Scheme = scheme
	rebuilt.Host = host
	return rebuilt.String()
}
