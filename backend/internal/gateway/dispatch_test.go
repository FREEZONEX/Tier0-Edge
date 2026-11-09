package gateway

import (
	"crypto/tls"
	"net/http"
	"testing"

	"backend/internal/httpx"
)

func TestMatchesLaunchpadDomain(t *testing.T) {
	tests := []struct {
		name      string
		host      string
		launchpad string
		want      bool
	}{
		{"exact match", "launchpad.example.com", "launchpad.example.com", true},
		{"case insensitive", "LAUNCHPAD.Example.COM", "launchpad.example.com", true},
		{"with port", "launchpad.example.com:8080", "launchpad.example.com", true},
		{"empty launchpad config", "launchpad.example.com", "", false},
		{"different host", "www.example.com", "launchpad.example.com", false},
		{"subdomain of launchpad", "app.launchpad.example.com", "launchpad.example.com", false},
		{"launchpad as suffix", "notlaunchpad.example.com", "launchpad.example.com", false},
		{"empty host", "", "launchpad.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesLaunchpadDomain(tt.host, tt.launchpad); got != tt.want {
				t.Errorf("matchesLaunchpadDomain(%q, %q) = %v, want %v", tt.host, tt.launchpad, got, tt.want)
			}
		})
	}
}

func TestForwardedProtoTrustBoundary(t *testing.T) {
	request := &http.Request{Header: make(http.Header)}
	request.Header.Set("X-Forwarded-Proto", "https, http")

	if got := httpx.ForwardedProto(request, false); got != "http" {
		t.Fatalf("untrusted forwarded proto = %q, want http", got)
	}
	if got := httpx.ForwardedProto(request, true); got != "https" {
		t.Fatalf("trusted forwarded proto = %q, want https", got)
	}

	request.TLS = &tls.ConnectionState{}
	request.Header.Set("X-Forwarded-Proto", "http")
	if got := httpx.ForwardedProto(request, true); got != "https" {
		t.Fatalf("native TLS proto = %q, want https", got)
	}
}

func TestForwardedHostTrustBoundary(t *testing.T) {
	request := &http.Request{Host: "backend:8080", Header: make(http.Header)}
	request.Header.Set("X-Forwarded-Host", "tier0.example.com, proxy.internal")

	if got := httpx.ForwardedHost(request, false); got != "backend:8080" {
		t.Fatalf("untrusted forwarded host = %q, want backend:8080", got)
	}
	if got := httpx.ForwardedHost(request, true); got != "tier0.example.com" {
		t.Fatalf("trusted forwarded host = %q, want tier0.example.com", got)
	}
}
