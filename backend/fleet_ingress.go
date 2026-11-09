package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"backend/internal/bootstrap"
	"backend/internal/config"
	"backend/internal/handler"
	"backend/internal/infra/privatefs"
	"backend/internal/middleware"
	"backend/internal/svc"
	"fmt"
	"github.com/zeromicro/go-zero/rest"
)

// Fleet ingress deliberately has no SPA, gateway, general management, database proxy or
// arbitrary forwarding routes. Route registration comes from the same contract
// as the normal OpenAPI listener. Domain authentication is never bypassed.
func newFleetIngressServer(c config.Config, app *bootstrap.App, ctx *svc.ServiceContext) (*rest.Server, error) {
	port := c.Cluster.IngressPort
	if port == 0 {
		return nil, nil
	}
	if !c.Cluster.CollaborationEnabled || port < 1 || port > 65535 || port == c.Https.Port {
		return nil, fmt.Errorf("invalid or conflicting Fleet ingress listener")
	}
	check := c
	check.Https.Port = port
	if err := config.ValidateHTTPSListener(check); err != nil {
		return nil, err
	}
	if err := config.ValidateTLSCert(c.Cluster.IngressTLSCertFile, c.Cluster.IngressTLSCertFile); err != nil {
		return nil, fmt.Errorf("Fleet ingress requires verified TLS configuration: %w", err)
	}
	conf := c.RestConf
	if c.Cluster.IngressHost != "" {
		conf.Host = c.Cluster.IngressHost
	}
	conf.Port, conf.CertFile, conf.KeyFile = port, c.Cluster.IngressTLSCertFile, c.Cluster.IngressTLSCertFile
	conf.Name += "-fleet"
	conf.Middlewares.Log = false
	server := rest.MustNewServer(conf)
	server.Use(middleware.NewAuditLogMiddleware(app.Audit).Handle)
	handler.RegisterFleetIngressHandlers(server, ctx)
	return server, nil
}

// The overlay listener uses an installation-owned key, authenticated by signed
// registration, independently of public web TLS and the SSH CA. One PEM file
// makes certificate/key creation atomic and restart-safe.
func prepareFleetIngressTLS(c *config.Config) error {
	if c.Cluster.IngressPort == 0 {
		return nil
	}
	if !c.Cluster.CollaborationEnabled || c.Cluster.IdentityDir == "" {
		return fmt.Errorf("Fleet ingress identity is not configured")
	}
	if err := os.MkdirAll(c.Cluster.IdentityDir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(c.Cluster.IdentityDir)
	if err != nil || !info.IsDir() || !privatefs.IsPrivate(c.Cluster.IdentityDir, info) {
		return fmt.Errorf("Fleet ingress identity directory permissions invalid")
	}
	path := filepath.Join(c.Cluster.IdentityDir, "ingress.pem")
	if stat, err := os.Lstat(path); err == nil {
		if !stat.Mode().IsRegular() || !privatefs.IsPrivate(path, stat) {
			return fmt.Errorf("Fleet ingress key permissions invalid")
		}
	} else if os.IsNotExist(err) {
		public, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if e != nil {
			return e
		}
		template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Tier0 Fleet ingress"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(5, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
		cert, e := x509.CreateCertificate(rand.Reader, &template, &template, public, key)
		if e != nil {
			return e
		}
		private, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			return e
		}
		raw := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})...)
		file, e := os.CreateTemp(c.Cluster.IdentityDir, ".ingress-*")
		if e != nil {
			return e
		}
		defer os.Remove(file.Name())
		_, w := file.Write(raw)
		syncErr := file.Sync()
		closeErr := file.Close()
		if e = errors.Join(w, syncErr, closeErr); e != nil {
			return e
		}
		if e = os.Rename(file.Name(), path); e != nil {
			return e
		}
	} else {
		return err
	}
	if _, err := tls.LoadX509KeyPair(path, path); err != nil {
		return fmt.Errorf("Fleet ingress certificate unavailable: %w", err)
	}
	c.Cluster.IngressTLSCertFile = path
	return nil
}
