package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalize_AppGatewayPortRangeDefaults(t *testing.T) {
	c := Config{}
	c.Normalize()

	if c.AppGateway.PortRangeStart != 30000 {
		t.Errorf("PortRangeStart = %d, want 30000", c.AppGateway.PortRangeStart)
	}
	if c.AppGateway.PortRangeEnd != 30099 {
		t.Errorf("PortRangeEnd = %d, want 30099", c.AppGateway.PortRangeEnd)
	}
}

func TestNormalize_VideoStreamPort(t *testing.T) {
	t.Setenv("ZLM_RTSP_PORT", "554")
	t.Setenv("ZLM_STREAM_PORT", "854")
	c := Config{}
	c.Normalize()
	if c.Video.ZLMRTSPPort != 554 || c.Video.ZLMStreamPort != 854 {
		t.Fatalf("snapshot and worker ports must be independent: %+v", c.Video)
	}
	t.Setenv("ZLM_STREAM_PORT", "")
	c = Config{Video: VideoConf{ZLMRTSPPort: 8554}}
	c.Normalize()
	if c.Video.ZLMStreamPort != 8554 {
		t.Fatalf("legacy stream port fallback = %d", c.Video.ZLMStreamPort)
	}
}

func TestNormalize_MicrosoftSSOFromEnvironment(t *testing.T) {
	t.Setenv("MICROSOFT_SSO_ENABLED", "true")
	t.Setenv("MICROSOFT_SSO_TENANT_ID", "tenant-id")
	t.Setenv("MICROSOFT_SSO_CLIENT_ID", "client-id")
	t.Setenv("MICROSOFT_SSO_CLIENT_SECRET", "client-secret")

	c := Config{AppGateway: AppGatewayConfig{LoginURL: "https://edge.example.com/login"}}
	c.Normalize()
	if !c.MicrosoftSSO.Enabled || c.MicrosoftSSO.TenantID != "tenant-id" || c.MicrosoftSSO.ClientID != "client-id" {
		t.Fatalf("Microsoft SSO env was not normalized: %+v", c.MicrosoftSSO)
	}
	if c.MicrosoftSSO.ClientSecret != "client-secret" {
		t.Fatal("Microsoft SSO secret was not normalized")
	}
	const wantRedirectURL = "https://edge.example.com/api/core/auth/sso/microsoft/callback"
	if c.MicrosoftSSO.RedirectURL != wantRedirectURL {
		t.Fatalf("Microsoft SSO redirect URL = %q, want %q", c.MicrosoftSSO.RedirectURL, wantRedirectURL)
	}
}

func TestNormalize_MicrosoftSSORedirectURLUsesPublicGatewayPort(t *testing.T) {
	t.Setenv("APP_GATEWAY_LOGIN_URL", "")
	t.Setenv("ENTRANCE_URL", "")
	t.Setenv("ENTRANCE_DOMAIN", "")
	c := Config{AppGateway: AppGatewayConfig{
		Scheme:       "https",
		ExternalHost: "edge.example.com",
		ExternalPort: 8443,
	}}
	c.Normalize()

	const want = "https://edge.example.com:8443/api/core/auth/sso/microsoft/callback"
	if c.MicrosoftSSO.RedirectURL != want {
		t.Fatalf("Microsoft SSO redirect URL = %q, want %q", c.MicrosoftSSO.RedirectURL, want)
	}
}

func TestNormalize_MicrosoftSSORedirectURLAllowsExplicitProxyOverride(t *testing.T) {
	t.Setenv("MICROSOFT_SSO_REDIRECT_URL", "https://login.example.com/tier0/microsoft/callback")
	c := Config{AppGateway: AppGatewayConfig{LoginURL: "https://edge.example.com/login"}}
	c.Normalize()

	const want = "https://login.example.com/tier0/microsoft/callback"
	if c.MicrosoftSSO.RedirectURL != want {
		t.Fatalf("Microsoft SSO redirect URL = %q, want explicit override %q", c.MicrosoftSSO.RedirectURL, want)
	}
}

func TestNormalize_AppGatewayPortRangeEnv(t *testing.T) {
	t.Setenv("APP_GATEWAY_PORT_RANGE_START", "31000")
	t.Setenv("APP_GATEWAY_PORT_RANGE_END", "31099")

	c := Config{}
	c.Normalize()

	if c.AppGateway.PortRangeStart != 31000 {
		t.Errorf("PortRangeStart = %d, want 31000", c.AppGateway.PortRangeStart)
	}
	if c.AppGateway.PortRangeEnd != 31099 {
		t.Errorf("PortRangeEnd = %d, want 31099", c.AppGateway.PortRangeEnd)
	}
}

func TestNormalize_AppGatewayPortRangeSwapsWhenReversed(t *testing.T) {
	c := Config{
		AppGateway: AppGatewayConfig{
			PortRangeStart: 30099,
			PortRangeEnd:   30000,
		},
	}
	c.Normalize()

	if c.AppGateway.PortRangeStart != 30000 {
		t.Errorf("PortRangeStart = %d, want 30000", c.AppGateway.PortRangeStart)
	}
	if c.AppGateway.PortRangeEnd != 30099 {
		t.Errorf("PortRangeEnd = %d, want 30099", c.AppGateway.PortRangeEnd)
	}
}

func TestNormalize_AppGatewayPortRangeResetsWhenTooSmall(t *testing.T) {
	c := Config{
		AppGateway: AppGatewayConfig{
			PortRangeStart: 30000,
			PortRangeEnd:   30003,
		},
	}
	c.Normalize()

	if c.AppGateway.PortRangeStart != 30000 {
		t.Errorf("PortRangeStart = %d, want 30000", c.AppGateway.PortRangeStart)
	}
	if c.AppGateway.PortRangeEnd != 30099 {
		t.Errorf("PortRangeEnd = %d, want 30099", c.AppGateway.PortRangeEnd)
	}
}

func TestNormalize_AppGatewayPortRangePreservesValidCustomRange(t *testing.T) {
	c := Config{
		AppGateway: AppGatewayConfig{
			PortRangeStart: 40000,
			PortRangeEnd:   40199,
		},
	}
	c.Normalize()

	if c.AppGateway.PortRangeStart != 40000 {
		t.Errorf("PortRangeStart = %d, want 40000", c.AppGateway.PortRangeStart)
	}
	if c.AppGateway.PortRangeEnd != 40199 {
		t.Errorf("PortRangeEnd = %d, want 40199", c.AppGateway.PortRangeEnd)
	}
}

func TestNormalize_AppGatewaySchemeEnv(t *testing.T) {
	t.Setenv("ENTRANCE_PROTOCOL", "https")

	c := Config{}
	c.Normalize()

	if c.AppGateway.Scheme != "https" {
		t.Errorf("Scheme = %q, want https", c.AppGateway.Scheme)
	}
}

func TestNormalizeAppGatewayScheme(t *testing.T) {
	// Clear environment to avoid interference from the host.
	os.Unsetenv("ENTRANCE_PROTOCOL")

	if got := NormalizeAppGatewayScheme(""); got != "http" {
		t.Errorf("NormalizeAppGatewayScheme(\"\") = %q, want http", got)
	}
	if got := NormalizeAppGatewayScheme("HTTPS"); got != "https" {
		t.Errorf("NormalizeAppGatewayScheme(HTTPS) = %q, want https", got)
	}
	if got := NormalizeAppGatewayScheme("http"); got != "http" {
		t.Errorf("NormalizeAppGatewayScheme(http) = %q, want http", got)
	}
}

func TestNormalizeAppGatewayListenerScheme(t *testing.T) {
	if got := NormalizeAppGatewayListenerScheme("", "https"); got != "https" {
		t.Errorf("empty listener scheme = %q, want https", got)
	}
	if got := NormalizeAppGatewayListenerScheme("http", "https"); got != "http" {
		t.Errorf("explicit listener scheme = %q, want http", got)
	}
}

func TestAppGatewayPublicAuthority(t *testing.T) {
	tests := []struct {
		name string
		conf AppGatewayConfig
		host string
		want string
	}{
		{name: "legacy without port", conf: AppGatewayConfig{Scheme: "https"}, host: "app.example.com", want: "app.example.com"},
		{name: "standard https", conf: AppGatewayConfig{Scheme: "https", ExternalPort: 443}, host: "app.example.com", want: "app.example.com"},
		{name: "non-standard https", conf: AppGatewayConfig{Scheme: "https", ExternalPort: 8443}, host: "app.example.com", want: "app.example.com:8443"},
		{name: "standard http", conf: AppGatewayConfig{Scheme: "http", ExternalPort: 80}, host: "app.example.com", want: "app.example.com"},
		{name: "non-standard http", conf: AppGatewayConfig{Scheme: "http", ExternalPort: 8088}, host: "app.example.com", want: "app.example.com:8088"},
		{name: "ipv6", conf: AppGatewayConfig{Scheme: "https", ExternalPort: 8443}, host: "2001:db8::1", want: "[2001:db8::1]:8443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AppGatewayPublicAuthority(tt.conf, tt.host); got != tt.want {
				t.Fatalf("AppGatewayPublicAuthority() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalize_AppGatewayCanonicalEnv(t *testing.T) {
	t.Setenv("APP_GATEWAY_ACCESS_MODE", "SUBDOMAIN")
	t.Setenv("APP_GATEWAY_DOMAIN_SUFFIX", "Apps.Example.com")
	c := Config{}
	c.Normalize()
	if c.AppGateway.DefaultAccessMode != "subdomain" {
		t.Fatalf("DefaultAccessMode = %q, want subdomain", c.AppGateway.DefaultAccessMode)
	}
	if c.AppGateway.SubdomainSuffix != "apps.example.com" {
		t.Fatalf("SubdomainSuffix = %q, want apps.example.com", c.AppGateway.SubdomainSuffix)
	}
	if err := ValidateAppGatewayConfig(c.AppGateway); err != nil {
		t.Fatalf("ValidateAppGatewayConfig(valid) = %v", err)
	}
}

func TestValidateAppGatewayConfig(t *testing.T) {
	tests := []struct {
		name string
		conf AppGatewayConfig
		ok   bool
	}{
		{name: "port default", conf: AppGatewayConfig{DefaultAccessMode: "port"}, ok: true},
		{name: "port with public port", conf: AppGatewayConfig{DefaultAccessMode: "port", ExternalPort: 8443}, ok: true},
		{name: "negative public port", conf: AppGatewayConfig{DefaultAccessMode: "port", ExternalPort: -1}},
		{name: "out of range public port", conf: AppGatewayConfig{DefaultAccessMode: "port", ExternalPort: 70000}},
		{name: "subdomain", conf: AppGatewayConfig{DefaultAccessMode: "subdomain", SubdomainSuffix: "apps.example.com"}, ok: true},
		{name: "empty suffix", conf: AppGatewayConfig{DefaultAccessMode: "subdomain"}},
		{name: "wildcard suffix", conf: AppGatewayConfig{DefaultAccessMode: "subdomain", SubdomainSuffix: "*.apps.example.com"}},
		{name: "url suffix", conf: AppGatewayConfig{DefaultAccessMode: "subdomain", SubdomainSuffix: "https://apps.example.com"}},
		{name: "ip suffix", conf: AppGatewayConfig{DefaultAccessMode: "subdomain", SubdomainSuffix: "192.168.1.10"}},
		{name: "localhost", conf: AppGatewayConfig{DefaultAccessMode: "subdomain", SubdomainSuffix: "localhost"}},
		{name: "unknown mode", conf: AppGatewayConfig{DefaultAccessMode: "auto"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAppGatewayConfig(tt.conf)
			if (err == nil) != tt.ok {
				t.Fatalf("ValidateAppGatewayConfig(%+v) error = %v, want ok=%v", tt.conf, err, tt.ok)
			}
		})
	}
}

func TestNormalize_HttpsConfigDefaultsDisabled(t *testing.T) {
	os.Unsetenv("BACKEND_HTTPS_PORT")
	os.Unsetenv("BACKEND_TLS_CERT_FILE")
	os.Unsetenv("BACKEND_TLS_KEY_FILE")

	c := Config{}
	c.Normalize()

	if c.Https.Port != 0 {
		t.Errorf("Https.Port = %d, want 0 (disabled by default)", c.Https.Port)
	}
	if c.Https.CertFile != "/app/certs/cert.pem" {
		t.Errorf("Https.CertFile = %q, want /app/certs/cert.pem", c.Https.CertFile)
	}
	if c.Https.KeyFile != "/app/certs/key.pem" {
		t.Errorf("Https.KeyFile = %q, want /app/certs/key.pem", c.Https.KeyFile)
	}
	// 顶层 RestConf 证书保持为空：8080 HTTP listener 始终 HTTP
	if c.CertFile != "" || c.KeyFile != "" {
		t.Errorf("top-level CertFile/KeyFile should stay empty, got %q/%q", c.CertFile, c.KeyFile)
	}
}

func TestNormalize_HttpsConfigEnvOverride(t *testing.T) {
	t.Setenv("BACKEND_HTTPS_PORT", "8443")
	t.Setenv("BACKEND_TLS_CERT_FILE", "/tmp/custom-cert.pem")
	t.Setenv("BACKEND_TLS_KEY_FILE", "/tmp/custom-key.pem")

	c := Config{}
	c.Normalize()

	if c.Https.Port != 8443 {
		t.Errorf("Https.Port = %d, want 8443", c.Https.Port)
	}
	if c.Https.CertFile != "/tmp/custom-cert.pem" {
		t.Errorf("Https.CertFile = %q, want /tmp/custom-cert.pem", c.Https.CertFile)
	}
	if c.Https.KeyFile != "/tmp/custom-key.pem" {
		t.Errorf("Https.KeyFile = %q, want /tmp/custom-key.pem", c.Https.KeyFile)
	}
}

func TestNormalize_LaunchpadDomainEnv(t *testing.T) {
	t.Setenv("LAUNCHPAD_DOMAIN", "launchpad.example.com")

	c := Config{}
	c.Normalize()

	if c.Gateway.LaunchpadDomain != "launchpad.example.com" {
		t.Errorf("LaunchpadDomain = %q, want launchpad.example.com", c.Gateway.LaunchpadDomain)
	}
}

func TestNormalize_TrustProxyHeadersEnv(t *testing.T) {
	t.Setenv("GATEWAY_TRUST_PROXY_HEADERS", "true")

	c := Config{}
	c.Normalize()

	if !c.Gateway.TrustProxyHeaders {
		t.Fatal("Gateway.TrustProxyHeaders = false, want true")
	}
}

func TestNormalize_MqttWsUpstreamUrl(t *testing.T) {
	t.Run("explicit env wins", func(t *testing.T) {
		t.Setenv("MQTT_WS_UPSTREAM_URL", "ws://broker.internal:8083/mqtt")
		c := Config{}
		c.Normalize()
		if c.Gateway.MqttWsUpstreamUrl != "ws://broker.internal:8083/mqtt" {
			t.Fatalf("MqttWsUpstreamUrl = %q, want explicit env", c.Gateway.MqttWsUpstreamUrl)
		}
	})

	t.Run("default derives from DataIngest broker", func(t *testing.T) {
		t.Setenv("MQTT_WS_UPSTREAM_URL", "")
		c := Config{}
		c.Normalize()
		if c.Gateway.MqttWsUpstreamUrl != "ws://emqx:8083/mqtt" {
			t.Fatalf("MqttWsUpstreamUrl = %q, want ws://emqx:8083/mqtt", c.Gateway.MqttWsUpstreamUrl)
		}
	})

	t.Run("default follows DataIngest broker host", func(t *testing.T) {
		t.Setenv("MQTT_WS_UPSTREAM_URL", "")
		c := Config{}
		c.DataIngest.MqttBrokers = "tcp://10.0.0.8:1883"
		c.Normalize()
		if c.Gateway.MqttWsUpstreamUrl != "ws://10.0.0.8:8083/mqtt" {
			t.Fatalf("MqttWsUpstreamUrl = %q, want ws://10.0.0.8:8083/mqtt", c.Gateway.MqttWsUpstreamUrl)
		}
	})

	t.Run("SDK public override does not leak into internal upstream", func(t *testing.T) {
		// TIER0_SDK_MQTT_HOST/PORT 面向 App SDK 公网接入（可能是公网域名 + WSS 8084），
		// 网关上游必须始终是 backend 可达的内部明文 WS。
		t.Setenv("MQTT_WS_UPSTREAM_URL", "")
		c := Config{}
		c.Tier0Sdk.MqttHost = "mqtt-public.example.com"
		c.Tier0Sdk.MqttPort = "8084"
		c.Normalize()
		if c.Gateway.MqttWsUpstreamUrl != "ws://emqx:8083/mqtt" {
			t.Fatalf("MqttWsUpstreamUrl = %q, want ws://emqx:8083/mqtt", c.Gateway.MqttWsUpstreamUrl)
		}
	})
}

func TestValidateTLSCert_MissingFiles(t *testing.T) {
	err := ValidateTLSCert("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Fatal("ValidateTLSCert should fail for missing files")
	}
}

func TestValidateHTTPSListenerConflicts(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{name: "http", port: 8080},
		{name: "launchpad", port: 8089},
		{name: "app gateway", port: 30001},
		{name: "out of range", port: 65536},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{}
			c.Port = 8080
			c.Https.Port = tt.port
			c.Gateway.LaunchpadPort = "8089"
			c.AppGateway.PortRangeStart = 30000
			c.AppGateway.PortRangeEnd = 30099
			if err := ValidateHTTPSListener(c); err == nil {
				t.Fatalf("ValidateHTTPSListener accepted conflicting port %d", tt.port)
			}
		})
	}

	c := Config{}
	c.Port = 8080
	c.Https.Port = 8443
	c.Gateway.LaunchpadPort = "8089"
	c.AppGateway.PortRangeStart = 30000
	c.AppGateway.PortRangeEnd = 30099
	if err := ValidateHTTPSListener(c); err != nil {
		t.Fatalf("ValidateHTTPSListener rejected valid port: %v", err)
	}
}

func TestValidateTLSCert_EmptyPaths(t *testing.T) {
	if err := ValidateTLSCert("", ""); err == nil {
		t.Fatal("ValidateTLSCert should fail for empty paths")
	}
}

func TestValidateTLSCert_ValidPair(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := genTestCertificate(t, dir)
	if err := ValidateTLSCert(certFile, keyFile); err != nil {
		t.Fatalf("ValidateTLSCert(valid test certificate) = %v, want nil", err)
	}
}

func TestValidateTLSCertNames(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := genTestCertificate(t, dir, "example.com", "launchpad.example.com", "*.apps.launchpad.com")
	if err := ValidateTLSCertNames(certFile, keyFile, "example.com", "launchpad.example.com", "*.apps.launchpad.com"); err != nil {
		t.Fatalf("ValidateTLSCertNames(valid SANs) = %v", err)
	}
	if err := ValidateTLSCertNames(certFile, keyFile, "missing.example.com"); err == nil {
		t.Fatal("ValidateTLSCertNames should reject an uncovered name")
	}
}

// genTestCertificate 生成只用于校验器测试的证书对并写入临时目录。
func genTestCertificate(t *testing.T, dir string, names ...string) (string, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-cert"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, name := range names {
		if ip := net.ParseIP(name); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, name)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}

func TestNormalize_ExternalLoginDefaultOff(t *testing.T) {
	t.Setenv("EXTERNAL_LOGIN_ENABLED", "")
	t.Setenv("MICROSOFT_SSO_ENABLED", "true")
	c := Config{}
	c.Normalize()
	if c.ExternalLogin.Enabled {
		t.Fatal("provider opt-in must not enable third-party login")
	}
	t.Setenv("EXTERNAL_LOGIN_ENABLED", "true")
	c.Normalize()
	if !c.ExternalLogin.Enabled {
		t.Fatal("explicit master opt-in must be retained")
	}
}

func TestNormalize_FleetHTTPSConnectionDefaults(t *testing.T) {
	for _, origin := range []string{"https://center.example.test", "https://center.example.test:8443"} {
		c := Config{Cluster: ClusterConf{ExternalHTTPBaseURL: origin}}
		c.Normalize()
		if c.Cluster.NetworkBootstrapURL != origin {
			t.Fatal("HTTPS Fleet entrance not derived")
		}
		c.Cluster.NetworkBootstrapURL = "https://bootstrap.example.test"
		c.Normalize()
		if c.Cluster.NetworkBootstrapURL != "https://bootstrap.example.test" {
			t.Fatal("explicit Fleet bootstrap override changed")
		}
	}
	c := Config{Cluster: ClusterConf{ExternalHTTPBaseURL: "http://192.0.2.1:8088"}}
	c.Normalize()
	if c.Cluster.NetworkBootstrapURL != "" {
		t.Fatal("insecure automatic enrollment enabled")
	}
}
