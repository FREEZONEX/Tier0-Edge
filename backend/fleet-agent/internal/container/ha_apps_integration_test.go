package container

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	containerapi "github.com/moby/moby/api/types/container"
	networkapi "github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
)

func TestHAAppExportImportWithCustomNetwork(t *testing.T) {
	if os.Getenv("FLEET_HA_TEST_DOCKER") != "true" {
		t.Skip("FLEET_HA_TEST_DOCKER is not enabled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	suffix := time.Now().UTC().Format("150405000000")
	installationID := "integration-" + suffix
	networkName := "tier0-ha-network-" + suffix
	containerName := "tier0-ha-app-" + suffix
	serviceName := "app-integration-" + suffix

	service := New(installationID, nil)
	if err := service.ready(); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	cleanup := func() {
		_, _ = service.client.ContainerRemove(context.Background(), containerName, dockerclient.ContainerRemoveOptions{Force: true})
		_, _ = service.client.NetworkRemove(context.Background(), networkName, dockerclient.NetworkRemoveOptions{})
	}
	t.Cleanup(cleanup)

	if _, err := service.client.NetworkCreate(ctx, networkName, dockerclient.NetworkCreateOptions{
		Driver: "bridge",
		Labels: map[string]string{"tier0.integration": "true"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.client.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Name: containerName,
		Config: &containerapi.Config{
			Image: "node:22-alpine",
			Cmd:   []string{"sleep", "300"},
			Labels: map[string]string{
				labelManaged:        "true",
				labelInstallationID: installationID,
				labelService:        serviceName,
				labelHAGroup:        "active",
				labelVisible:        "false",
				labelRestartable:    "false",
			},
		},
		HostConfig: &containerapi.HostConfig{},
		NetworkingConfig: &networkapi.NetworkingConfig{
			EndpointsConfig: map[string]*networkapi.EndpointSettings{networkName: {}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	exportDir := filepath.Join(t.TempDir(), "export")
	if _, err := service.ExportHAApps(ctx, "active", exportDir); err != nil {
		t.Fatal(err)
	}
	if _, err := service.client.ContainerRemove(ctx, containerName, dockerclient.ContainerRemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.client.NetworkRemove(ctx, networkName, dockerclient.NetworkRemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := service.ImportHAApps(ctx, "active", exportDir); err != nil {
		t.Fatal(err)
	}

	inspected, err := service.client.ContainerInspect(ctx, containerName, dockerclient.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Container.HostConfig == nil ||
		inspected.Container.HostConfig.RestartPolicy.Name != containerapi.RestartPolicyDisabled {
		t.Fatalf("imported app restart policy = %+v, want disabled", inspected.Container.HostConfig)
	}
	if _, found := inspected.Container.NetworkSettings.Networks[networkName]; !found {
		t.Fatalf("imported app is not attached to custom network %s", networkName)
	}
	if _, err := service.client.NetworkInspect(ctx, networkName, dockerclient.NetworkInspectOptions{}); err != nil {
		t.Fatalf("custom network was not recreated: %v", err)
	}
}

func TestRequiredHAContainerWithoutHealthcheckIsUnhealthy(t *testing.T) {
	if !requiredHAContainerUnhealthy(haGroupContainer{
		Required: true,
		Running:  true,
		Health:   "none",
	}) {
		t.Fatal("required running container without a healthcheck was treated as healthy")
	}
	if requiredHAContainerUnhealthy(haGroupContainer{
		Required: true,
		Running:  true,
		Health:   "healthy",
	}) {
		t.Fatal("healthy required container was treated as unhealthy")
	}
	if requiredHAContainerUnhealthy(haGroupContainer{
		Required: false,
		Running:  true,
		Health:   "none",
	}) {
		t.Fatal("optional container without a healthcheck was treated as unhealthy")
	}
}

func TestRestartPermanentlyDenied(t *testing.T) {
	for _, serviceName := range []string{"app-gis-0b9401f2bd7", "emqx", "marimo", "sourceflow", "eventflow", "uns-agent-api", "database", "db", "local-db-init"} {
		if restartPermanentlyDenied(serviceName) {
			t.Fatalf("restartPermanentlyDenied(%q) = true, want false", serviceName)
		}
	}
	for _, serviceName := range []string{"backend", "pg", "postgres", "postgresql", "tsdb", "timescale", "timescaledb", "redis"} {
		if !restartPermanentlyDenied(serviceName) {
			t.Fatalf("restartPermanentlyDenied(%q) = false, want true", serviceName)
		}
	}
}
