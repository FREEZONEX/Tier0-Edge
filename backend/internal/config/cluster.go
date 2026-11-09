package config

import (
	"net"
	"net/url"
	"os"
	"strings"
)

const microsoftSSOCallbackPath = "/api/core/auth/sso/microsoft/callback"

func defaultMicrosoftSSORedirectURL(appGateway AppGatewayConfig) string {
	loginURL := strings.TrimSpace(appGateway.LoginURL)
	if loginURL == "" {
		loginURL = getenv("APP_GATEWAY_LOGIN_URL", "")
	}
	if baseURL := baseURLFromRawURL(loginURL); baseURL != "" {
		return baseURL + microsoftSSOCallbackPath
	}
	if baseURL := baseURLFromRawURL(getenv("ENTRANCE_URL", "")); baseURL != "" {
		return baseURL + microsoftSSOCallbackPath
	}

	host := strings.TrimSpace(appGateway.ExternalHost)
	if host == "" {
		host = strings.TrimSpace(getenv("APP_GATEWAY_EXTERNAL_HOST", ""))
	}
	if host == "" {
		host = strings.TrimSpace(getenv("ENTRANCE_DOMAIN", ""))
	}
	if host == "" {
		return ""
	}
	if strings.Contains(host, "://") {
		if baseURL := baseURLFromRawURL(host); baseURL != "" {
			return baseURL + microsoftSSOCallbackPath
		}
		return ""
	}

	bareHost := strings.Trim(host, "[]")
	authority := AppGatewayPublicAuthority(appGateway, bareHost)
	if authority == bareHost && net.ParseIP(bareHost) != nil && strings.Contains(bareHost, ":") {
		authority = "[" + bareHost + "]"
	}
	return NormalizeAppGatewayScheme(appGateway.Scheme) + "://" + authority + microsoftSSOCallbackPath
}

func defaultExternalHTTPBaseURL(appGatewayScheme string) string {
	if value := strings.TrimSpace(getenv("CLUSTER_EXTERNAL_HTTP_BASE_URL", "")); value != "" {
		return strings.TrimRight(value, "/")
	}
	if value := strings.TrimSpace(getenv("ENTRANCE_URL", "")); value != "" {
		return strings.TrimRight(value, "/")
	}
	if value := baseURLFromRawURL(getenv("APP_GATEWAY_LOGIN_URL", "")); value != "" {
		return value
	}
	scheme := normalizeExternalHTTPScheme(appGatewayScheme)
	host := strings.TrimSpace(getenv("ENTRANCE_DOMAIN", ""))
	if host == "" {
		host = strings.TrimSpace(getenv("APP_GATEWAY_EXTERNAL_HOST", ""))
	}
	if host == "" {
		host = localIPv4()
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, "://") {
		if parsed, err := url.Parse(host); err == nil {
			host = parsed.Host
		}
	}
	port := strings.TrimSpace(getenv("ENTRANCE_PORT", "8088"))
	if splitHost, splitPort, err := net.SplitHostPort(host); err == nil {
		host = splitHost
		if splitPort != "" {
			port = splitPort
		}
	}
	if port == "" {
		port = "8088"
	}
	return scheme + "://" + net.JoinHostPort(strings.Trim(host, "[]"), port)
}

func baseURLFromRawURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func normalizeExternalHTTPScheme(scheme string) string {
	if strings.TrimSpace(strings.ToLower(scheme)) == "http" {
		return "http"
	}
	return "https"
}

func defaultExternalMqttBrokerHost() string {
	if value := strings.TrimSpace(getenv("EMQX_EXTERNAL_BROKER_HOST", "")); value != "" {
		return value
	}
	host := externalMqttHost()
	if host == "" {
		if _, err := os.Stat("/.dockerenv"); err == nil {
			host = "host.docker.internal"
		}
	}
	if host == "" {
		host = localIPv4()
	}
	if host == "" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(getenv("OS_MQTT_TCP_PORT", "1883"))
	if port == "" {
		port = "1883"
	}
	return "tcp://" + net.JoinHostPort(host, port)
}

func externalMqttHost() string {
	host := strings.TrimSpace(getenv("ENTRANCE_DOMAIN", ""))
	if host == "" {
		return ""
	}
	if strings.Contains(host, "://") {
		if parsed, err := url.Parse(host); err == nil {
			host = parsed.Host
		}
	}
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	host = strings.Trim(host, "[]")
	return strings.TrimSpace(host)
}

func localIPv4() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP != nil {
			if ip := addr.IP.To4(); ip != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip = ip.To4(); ip != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
	}
	return ""
}
