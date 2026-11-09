package appgateway

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/repo"
)

func TestResolveRoute_Port(t *testing.T) {
	g := &Gateway{
		conf: config.AppGatewayConfig{
			PortRangeStart: 30000,
			PortRangeEnd:   30099,
		},
		router: NewRouter(),
	}

	g.router.AddRoute(&repo.AppGatewayRoute{
		AppID:         "app-001",
		AccessMode:    "port",
		Port:          30001,
		ContainerHost: "172.18.0.1",
		ContainerPort: 8080,
	})

	if r := g.resolveRoute(&http.Request{Host: "52.77.190.126:30001"}); r == nil || r.AppID != "app-001" {
		t.Errorf("resolveRoute(port) = %+v, want app-001", r)
	}

	if r := g.resolveRoute(&http.Request{Host: "52.77.190.126:39999"}); r != nil {
		t.Errorf("resolveRoute(unknown port) should be nil")
	}
}

func TestResolveRoute_Domain(t *testing.T) {
	g := &Gateway{
		conf: config.AppGatewayConfig{
			PortRangeStart: 30000,
			PortRangeEnd:   30099,
		},
		router: NewRouter(),
	}

	g.router.AddRoute(&repo.AppGatewayRoute{
		AppID:         "app-002",
		AccessMode:    "subdomain",
		Domain:        "app-002.localhost",
		ContainerHost: "172.18.0.1",
		ContainerPort: 8081,
	})

	if r := g.resolveRoute(&http.Request{Host: "app-002.localhost:8081"}); r == nil || r.AppID != "app-002" {
		t.Errorf("resolveRoute(domain with port) = %+v, want app-002", r)
	}

	if r := g.resolveRoute(&http.Request{Host: "app-002.localhost"}); r == nil || r.AppID != "app-002" {
		t.Errorf("resolveRoute(domain without port) = %+v, want app-002", r)
	}
}

func TestApplyDefaultsSchemeFollowsEntranceProtocol(t *testing.T) {
	t.Setenv("APP_GATEWAY_SCHEME", "")
	t.Setenv("ENTRANCE_PROTOCOL", "http")

	got := applyDefaults(config.AppGatewayConfig{})
	if got.Scheme != "http" {
		t.Fatalf("Scheme = %q, want http", got.Scheme)
	}
}

func TestApplyDefaultsSchemeFollowsHTTPSEntranceProtocol(t *testing.T) {
	t.Setenv("APP_GATEWAY_SCHEME", "")
	t.Setenv("APP_GATEWAY_LISTENER_SCHEME", "")
	t.Setenv("ENTRANCE_PROTOCOL", "https")

	got := applyDefaults(config.AppGatewayConfig{})
	if got.Scheme != "https" {
		t.Fatalf("Scheme = %q, want https", got.Scheme)
	}
	if got.ListenerScheme != "https" {
		t.Fatalf("ListenerScheme = %q, want https", got.ListenerScheme)
	}
}

func TestApplyDefaultsSchemeExplicitOverride(t *testing.T) {
	t.Setenv("APP_GATEWAY_SCHEME", "https")
	t.Setenv("ENTRANCE_PROTOCOL", "http")

	got := applyDefaults(config.AppGatewayConfig{})
	if got.Scheme != "https" {
		t.Fatalf("Scheme = %q, want https", got.Scheme)
	}
}

func TestApplyDefaultsExternalPortTLSUsesHTTPSListener(t *testing.T) {
	t.Setenv("APP_GATEWAY_SCHEME", "https")
	t.Setenv("APP_GATEWAY_LISTENER_SCHEME", "https")
	t.Setenv("APP_GATEWAY_EXTERNAL_PORT", "8443")

	got := applyDefaults(config.AppGatewayConfig{})
	if got.Scheme != "https" {
		t.Fatalf("Scheme = %q, want https", got.Scheme)
	}
	if got.ListenerScheme != "https" {
		t.Fatalf("ListenerScheme = %q, want https", got.ListenerScheme)
	}
	if got.ExternalPort != 8443 {
		t.Fatalf("ExternalPort = %d, want 8443", got.ExternalPort)
	}
}

func TestEnsurePortTLSConfigLoadsOnlyForPortRoutes(t *testing.T) {
	certFile, keyFile := writeTestTLSKeyPair(t)
	g := &Gateway{
		conf: config.AppGatewayConfig{
			ListenerScheme: "https",
			CertPath:       certFile,
			KeyPath:        keyFile,
		},
		router: NewRouter(),
	}

	if err := g.ensurePortTLSConfig(); err != nil {
		t.Fatalf("ensurePortTLSConfig without routes = %v", err)
	}
	if g.tlsConfig != nil {
		t.Fatal("TLS certificate loaded without a port route")
	}

	g.router.AddRoute(&repo.AppGatewayRoute{AppID: "subdomain-app", AccessMode: "subdomain", Domain: "app.apps.example.com"})
	if err := g.ensurePortTLSConfig(); err != nil {
		t.Fatalf("ensurePortTLSConfig with subdomain-only route = %v", err)
	}
	if g.tlsConfig != nil {
		t.Fatal("TLS certificate loaded for a subdomain-only route")
	}

	g.router.AddRoute(&repo.AppGatewayRoute{AppID: "port-app", AccessMode: "port", Port: 30000})
	if err := g.ensurePortTLSConfig(); err != nil {
		t.Fatalf("ensurePortTLSConfig with port route = %v", err)
	}
	if g.tlsConfig == nil {
		t.Fatal("TLS certificate was not loaded for a port route")
	}
}

func TestEnsurePortTLSConfigRejectsMissingCertificate(t *testing.T) {
	g := &Gateway{
		conf: config.AppGatewayConfig{
			ListenerScheme: "https",
			CertPath:       filepath.Join(t.TempDir(), "missing-cert.pem"),
			KeyPath:        filepath.Join(t.TempDir(), "missing-key.pem"),
		},
		router: NewRouter(),
	}
	g.router.AddRoute(&repo.AppGatewayRoute{AppID: "port-app", AccessMode: "port", Port: 30000})
	if err := g.ensurePortTLSConfig(); err == nil {
		t.Fatal("ensurePortTLSConfig accepted missing certificate assets")
	}
}

func TestServeListenerUsesTLSForEveryPort(t *testing.T) {
	certFile, keyFile := writeTestTLSKeyPair(t)
	tlsConfig, err := LoadTLSConfig(certFile, keyFile)
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	g := &Gateway{
		conf:      config.AppGatewayConfig{ListenerScheme: "https"},
		tlsConfig: tlsConfig,
	}

	for i := 0; i < 2; i++ {
		ln, listenErr := net.Listen("tcp", "127.0.0.1:0")
		if listenErr != nil {
			t.Fatalf("listen: %v", listenErr)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		g.serveListener(ln, port)

		dialer := &net.Dialer{Timeout: 2 * time.Second}
		conn, dialErr := tls.DialWithDialer(dialer, "tcp", ln.Addr().String(), &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // Test certificate identity is intentionally synthetic.
		})
		if dialErr != nil {
			_ = ln.Close()
			t.Fatalf("port %d did not complete a TLS handshake: %v", port, dialErr)
		}
		_ = conn.Close()
		_ = ln.Close()
	}
}

func writeTestTLSKeyPair(t *testing.T) (string, string) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	pair := server.TLS.Certificates[0]

	var certPEM []byte
	for _, der := range pair.Certificate {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		t.Fatalf("marshal test private key: %v", err)
	}
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write test certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write test private key: %v", err)
	}
	return certFile, keyFile
}

func TestMatchesSubdomainHost(t *testing.T) {
	g := &Gateway{
		conf: config.AppGatewayConfig{
			SubdomainSuffix: "apps.example.com",
		},
	}

	tests := []struct {
		name string
		host string
		want bool
	}{
		{"wildcard match", "a1b2c3d4e5f6.apps.example.com", true},
		{"wildcard with port", "a1b2c3d4e5f6.apps.example.com:8080", true},
		{"multi-level", "a.b.apps.example.com", false},
		{"other suffix", "a1b2c3d4e5f6.other.com", false},
		{"bare parent", "apps.example.com", false},
		{"empty host", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.matchesSubdomainHost(tt.host); got != tt.want {
				t.Errorf("matchesSubdomainHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestMatchesSubdomainHost_EmptySuffix(t *testing.T) {
	g := &Gateway{
		conf: config.AppGatewayConfig{SubdomainSuffix: ""},
	}
	if g.matchesSubdomainHost("a1b2c3d4e5f6.apps.example.com") {
		t.Errorf("matchesSubdomainHost should be false when suffix is empty")
	}
}

func TestMatchesDomainHost_PreservesPersistedDomainAcrossSuffixChange(t *testing.T) {
	g := &Gateway{
		conf:   config.AppGatewayConfig{SubdomainSuffix: "new-apps.example.com"},
		router: NewRouterWithSuffix("new-apps.example.com"),
	}
	g.router.AddRoute(&repo.AppGatewayRoute{
		AppID:      "app-persisted",
		AccessMode: "subdomain",
		Domain:     "persisted.old-apps.example.com",
	})

	if !g.matchesDomainHost("persisted.old-apps.example.com") {
		t.Fatal("persisted exact App domain should remain dispatchable after suffix change")
	}
	if !g.matchesDomainHost("unknown.new-apps.example.com") {
		t.Fatal("current wildcard suffix should still dispatch unknown App hosts to AppGateway")
	}
	if g.matchesDomainHost("main.example.com") {
		t.Fatal("unrelated main-site host should not dispatch to AppGateway")
	}
}

func TestResolveRoute_WildcardSubdomain(t *testing.T) {
	g := &Gateway{
		conf: config.AppGatewayConfig{
			PortRangeStart:  30000,
			PortRangeEnd:    30099,
			SubdomainSuffix: "apps.example.com",
		},
		router: NewRouterWithSuffix("apps.example.com"),
	}

	g.router.AddRoute(&repo.AppGatewayRoute{
		AppID:         "app-008",
		AccessMode:    "subdomain",
		Domain:        "a1b2c3d4e5f6.apps.example.com",
		ContainerHost: "172.18.0.1",
		ContainerPort: 8081,
	})

	// 通配子域名命中（走主入口 Host 分流场景，无端口）
	if r := g.resolveRoute(&http.Request{Host: "a1b2c3d4e5f6.apps.example.com"}); r == nil || r.AppID != "app-008" {
		t.Errorf("resolveRoute(wildcard) = %+v, want app-008", r)
	}
	// 带端口命中
	if r := g.resolveRoute(&http.Request{Host: "a1b2c3d4e5f6.apps.example.com:8088"}); r == nil || r.AppID != "app-008" {
		t.Errorf("resolveRoute(wildcard with port) = %+v, want app-008", r)
	}
	// 未注册子域名 → nil
	if r := g.resolveRoute(&http.Request{Host: "deadbeef.apps.example.com"}); r != nil {
		t.Errorf("resolveRoute(unregistered subdomain) should be nil, got %+v", r)
	}
	// 多级 → nil
	if r := g.resolveRoute(&http.Request{Host: "a.b.apps.example.com"}); r != nil {
		t.Errorf("resolveRoute(multi-level) should be nil, got %+v", r)
	}
}
