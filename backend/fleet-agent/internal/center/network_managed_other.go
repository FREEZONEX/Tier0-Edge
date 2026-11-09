//go:build !windows

package center

import (
	"context"
	"errors"
	"os"

	"backend/fleet-agent/internal/config"
	"backend/internal/fleetprotocol"
)

func createNetworkCredentialFile(dir string) (*os.File, error) {
	return os.CreateTemp(dir, "network-enrollment-*")
}

func prepareWindowsManagedNetwork(context.Context, config.Config, fleetprotocol.NetworkBootstrapResponse, string, func() error) error {
	return errors.New("FLEET_NETWORK_STATE_ISOLATION_REQUIRED")
}
