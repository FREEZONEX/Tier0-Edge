package appgateway

import (
	"crypto/tls"
	"fmt"

	"backend/internal/config"
)

// LoadTLSConfig loads the installer-managed certificate. Missing or invalid
// assets fail startup; the deployment must provide and validate certificates
// before startup instead of creating them silently at runtime.
func LoadTLSConfig(certPath, keyPath string) (*tls.Config, error) {
	if err := config.ValidateTLSCert(certPath, keyPath); err != nil {
		return nil, fmt.Errorf("invalid AppGateway TLS certificate: %w", err)
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load AppGateway TLS certificate: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
