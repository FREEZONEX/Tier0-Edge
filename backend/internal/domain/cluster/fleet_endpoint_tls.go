package cluster

import (
	"backend/internal/fleetprotocol"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"backend/internal/domain/license"
	"backend/internal/repo"
	"gorm.io/gorm"
)

type fleetTLSTargetKey struct{}
type fleetTLSTarget struct{ origin, pin string }

func (s *Service) connectionEndpoint(ctx context.Context) (string, string, error) {
	mode := fleetprotocol.NetworkModeDirect
	if !license.Default().IsHost() {
		if current, err := s.storageConnection(); err == nil && current.NetworkMode != "" {
			mode = current.NetworkMode
		}
	}
	return s.connectionEndpointForMode(ctx, mode)
}
func (s *Service) connectionEndpointForMode(ctx context.Context, mode string) (string, string, error) {
	if mode == fleetprotocol.NetworkModeDirect {
		endpoint := strings.TrimRight(s.cfg.NodeAPIBaseURL, "/")
		if endpoint == "" {
			base, err := url.Parse(s.cfg.ExternalHTTPBaseURL)
			if err != nil || base.Hostname() == "" {
				return "", "", ErrInvalid
			}
			if s.cfg.IngressPort == 0 {
				endpoint = s.cfg.ExternalHTTPBaseURL
			} else {
				endpoint = "https://" + net.JoinHostPort(base.Hostname(), strconv.Itoa(s.cfg.IngressPort))
			}
		}
		if err := validateFleetEnrollmentEndpoint(endpoint); err != nil {
			return "", "", err
		}
		if s.cfg.IngressPort == 0 {
			return endpoint, "", nil
		}
		pin, err := s.fleetIngressPin()
		return endpoint, pin, err
	}
	if mode != fleetprotocol.NetworkModeTailscale {
		return "", "", ErrInvalid
	}

	if s.cfg.IngressPort == 0 {
		return s.cfg.NodeAPIBaseURL, "", validateFleetEnrollmentEndpoint(s.cfg.NodeAPIBaseURL)
	}
	agent, ok := s.localAgent.(localFleetNetwork)
	if !ok {
		return "", "", ErrFleetAgentUnavailable
	}
	status, err := agent.NetworkStatus(ctx)
	if err != nil || status.State != "online" {
		return "", "", ErrFleetPeerUnavailable
	}
	address := ""
	for _, a := range status.Addresses {
		if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
			address = ip.String()
			break
		}
	}
	if address == "" {
		return "", "", ErrFleetPeerUnavailable
	}
	pin, err := s.fleetIngressPin()
	return "https://" + net.JoinHostPort(address, "9443"), pin, err
}
func (s *Service) fleetIngressPin() (string, error) {
	raw, err := os.ReadFile(s.cfg.IngressTLSCertFile)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", ErrInvalid
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	return digest(cert.RawSubjectPublicKeyInfo), nil
}
func (s *Service) validateNetworkEndpoint(_ context.Context, p FleetEnrollmentProof) error {
	if err := validateFleetEnrollmentEndpoint(p.Endpoint); err != nil {
		return err
	}
	if p.TLSFingerprint != "" && (len(p.TLSFingerprint) != 64 || strings.Trim(p.TLSFingerprint, "0123456789abcdef") != "") {
		return ErrInvalid
	}
	// Enterprise authenticates the installation's signed endpoint and TLS pin.
	// It does not claim or require ownership of the customer's VPN identity.
	return nil
}
func (s *Service) pinFleetEndpointTLS(ctx context.Context, endpoint string, conf *tls.Config) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if target, ok := ctx.Value(fleetTLSTargetKey{}).(fleetTLSTarget); ok && target.origin == u.Scheme+"://"+u.Host && target.pin != "" {
		setFleetTLSPin(conf, target.pin)
		return nil
	}
	if !license.Default().IsHost() {
		raw, e := os.ReadFile(filepath.Join(s.cfg.IdentityDir, "connection.json"))
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		var c fleetCenterRegistration
		if json.Unmarshal(raw, &c) != nil {
			return ErrInvalid
		}
		if c.BusinessEndpoint == u.Scheme+"://"+u.Host && c.CenterTLSFingerprint != "" {
			setFleetTLSPin(conf, c.CenterTLSFingerprint)
		}
		return nil
	}
	var node repo.FleetCollaborationNode
	// Pins are eligible only for the exact authenticated, registered origin. They
	// never relax TLS verification for token bootstrap or arbitrary URLs.
	err = repo.GetCommonConn(ctx).Where("endpoint=? AND registration_status='registered' AND tls_fingerprint<>''", u.Scheme+"://"+u.Host).Take(&node).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	setFleetTLSPin(conf, node.TLSFingerprint)
	return nil
}
func setFleetTLSPin(conf *tls.Config, pin string) {
	conf.InsecureSkipVerify = true // explicit authenticated SPKI pin replaces public DNS PKI on tailnet IPs
	conf.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return ErrUnauthorized
		}
		cert := state.PeerCertificates[0]
		now := time.Now()
		if now.Before(cert.NotBefore) || now.After(cert.NotAfter) || digest(cert.RawSubjectPublicKeyInfo) != pin {
			return ErrUnauthorized
		}
		return nil
	}
}
