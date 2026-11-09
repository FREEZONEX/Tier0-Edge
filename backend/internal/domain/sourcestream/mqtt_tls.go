package sourcestream

import (
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"time"
)

// MQTTClientTLS builds a fresh configuration from the selected Edge credential
// on every connection. The peer must present a certificate for the broker host.
func MQTTClientTLS(def SourceDefinition, credential Credential) (*tls.Config, error) {
	endpoint, err := url.Parse(def.Endpoint)
	if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "mqtt" && endpoint.Scheme != "mqtts") {
		return nil, ErrCredential
	}
	if credential.Kind != "" && credential.Kind != "basic" && credential.Kind != "custom" {
		return nil, ErrCredential
	}
	if endpoint.Scheme != "mqtts" {
		if credential.Kind == "custom" {
			return nil, ErrCredential
		}
		return nil, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.Hostname()}
	if def.tlsConfig != nil {
		cfg = def.tlsConfig.Clone()
		if def.MQTT != nil && def.MQTT.CACertificate != "" {
			if cfg.RootCAs == nil {
				cfg.RootCAs, err = x509.SystemCertPool()
				if err != nil {
					return nil, err
				}
			} else {
				cfg.RootCAs = cfg.RootCAs.Clone()
			}
			if !cfg.RootCAs.AppendCertsFromPEM([]byte(def.MQTT.CACertificate)) {
				return nil, ErrCredential
			}
		}
		return cfg, nil
	}
	if credential.Kind != "custom" {
		if def.MQTT != nil && def.MQTT.CACertificate != "" {
			roots, err := x509.SystemCertPool()
			if err != nil {
				return nil, err
			}
			if !roots.AppendCertsFromPEM([]byte(def.MQTT.CACertificate)) {
				return nil, ErrCredential
			}
			cfg.RootCAs = roots
		}
		return cfg, nil
	}
	if def.ConnectionRole() != ClientRole || len(credential.Values) != 3 {
		return nil, ErrCredential
	}
	pair, err := tls.X509KeyPair([]byte(credential.Values["clientCertificatePem"]), []byte(credential.Values["clientPrivateKeyPem"]))
	if err != nil {
		return nil, ErrCredential
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	now := time.Now()
	if err != nil || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return nil, ErrCredential
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(credential.Values["caCertificatePem"])) {
		return nil, ErrCredential
	}
	cfg.Certificates = []tls.Certificate{pair}
	cfg.RootCAs = roots
	return cfg, nil
}
