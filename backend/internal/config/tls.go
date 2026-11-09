package config

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// ValidateHTTPSListener rejects ports that would collide with another listener
// in the Backend process. Deployment scripts validate host-side mappings too,
// but direct binary users need the same process-level protection.
func ValidateHTTPSListener(c Config) error {
	port := c.Https.Port
	if port <= 0 {
		return nil
	}
	if port > 65535 {
		return fmt.Errorf("HTTPS listener port must be between 1 and 65535: %d", port)
	}
	if port == c.Port {
		return fmt.Errorf("HTTPS listener port conflicts with HTTP listener: %d", port)
	}
	if launchpadPort, err := strconv.Atoi(strings.TrimSpace(c.Gateway.LaunchpadPort)); err == nil && port == launchpadPort {
		return fmt.Errorf("HTTPS listener port conflicts with Launchpad listener: %d", port)
	}
	if port >= c.AppGateway.PortRangeStart && port <= c.AppGateway.PortRangeEnd {
		return fmt.Errorf("HTTPS listener port conflicts with AppGateway range: %d", port)
	}
	return nil
}

// ValidateTLSCert 同步校验 HTTPS listener 的证书文件：文件存在、可读、证书与私钥匹配。
// HTTPS 明确启用（Port>0）时校验失败必须让启动失败，不能静默降级为 HTTP。
func ValidateTLSCert(certFile, keyFile string) error {
	if certFile == "" || keyFile == "" {
		return fmt.Errorf("certificate paths must not be empty (cert=%q key=%q)", certFile, keyFile)
	}
	for _, f := range []string{certFile, keyFile} {
		info, err := os.Stat(f)
		if err != nil {
			return fmt.Errorf("TLS file %q: %w", f, err)
		}
		if info.IsDir() {
			return fmt.Errorf("TLS file %q is a directory", f)
		}
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("invalid TLS key pair (cert=%q key=%q): %w", certFile, keyFile, err)
	}
	if len(pair.Certificate) == 0 {
		return fmt.Errorf("TLS certificate chain is empty: %q", certFile)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("parse TLS leaf certificate %q: %w", certFile, err)
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) {
		return fmt.Errorf("TLS certificate is not valid before %s", leaf.NotBefore.Format(time.RFC3339))
	}
	if !now.Before(leaf.NotAfter) {
		return fmt.Errorf("TLS certificate expired at %s", leaf.NotAfter.Format(time.RFC3339))
	}
	return nil
}

// ValidateTLSCertNames verifies all public DNS/IP names required by one SAN
// certificate. Wildcard requirements are compared as explicit SAN entries;
// exact DNS/IP names use x509 hostname verification.
func ValidateTLSCertNames(certFile, keyFile string, names ...string) error {
	if err := ValidateTLSCert(certFile, keyFile); err != nil {
		return err
	}
	pemBytes, err := os.ReadFile(certFile)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return fmt.Errorf("TLS certificate %q does not contain a PEM certificate", certFile)
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, "*.") {
			matched := false
			for _, dnsName := range leaf.DNSNames {
				if strings.EqualFold(dnsName, name) {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("TLS certificate SAN does not include %q", name)
			}
			continue
		}
		if ip := net.ParseIP(name); ip != nil {
			if err := leaf.VerifyHostname(ip.String()); err != nil {
				return fmt.Errorf("TLS certificate SAN does not cover IP %q: %w", name, err)
			}
			continue
		}
		if err := leaf.VerifyHostname(name); err != nil {
			return fmt.Errorf("TLS certificate SAN does not cover %q: %w", name, err)
		}
	}
	return nil
}
