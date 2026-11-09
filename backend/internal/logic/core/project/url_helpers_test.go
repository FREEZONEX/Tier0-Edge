package project

import (
	"testing"

	"backend/internal/config"
)

func TestAppGatewaySchemeFollowsEntranceProtocol(t *testing.T) {
	t.Setenv("ENTRANCE_PROTOCOL", "http")

	got := appGatewayScheme(config.Config{})
	if got != "http" {
		t.Fatalf("appGatewayScheme() = %q, want http", got)
	}
}

func TestAppGatewaySchemeExplicitOverride(t *testing.T) {
	t.Setenv("ENTRANCE_PROTOCOL", "http")

	got := appGatewayScheme(config.Config{
		AppGateway: config.AppGatewayConfig{Scheme: "https"},
	})
	if got != "https" {
		t.Fatalf("appGatewayScheme() = %q, want https", got)
	}
}
