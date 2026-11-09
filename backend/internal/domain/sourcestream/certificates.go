package sourcestream

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
)

const maxPublicCertificateBytes = 64 * 1024

// Only public certificates are accepted; private keys never belong in a Source document.
func publicCertificates(value string, caOnly bool) ([]*x509.Certificate, error) {
	if len(value) > maxPublicCertificateBytes {
		return nil, ErrCredential
	}
	rest := bytes.TrimSpace([]byte(value))
	var result []*x509.Certificate
	for len(rest) > 0 {
		if !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, ErrCredential
		}
		block, tail := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, ErrCredential
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || (caOnly && !cert.IsCA) {
			return nil, ErrCredential
		}
		result = append(result, cert)
		rest = bytes.TrimSpace(tail)
	}
	return result, nil
}

// Trust is scoped to this Source and keeps hostname, expiry, DNS and redirect checks.
func (s *Service) httpClientFor(d SourceDefinition) (*http.Client, func(), error) {
	if d.tlsConfig == nil && (d.HTTP == nil || d.HTTP.CACertificate == "") {
		return s.httpClient, func() {}, nil
	}
	ca := ""
	if d.HTTP != nil {
		ca = d.HTTP.CACertificate
	}
	certs, err := publicCertificates(ca, true)
	if err != nil {
		return nil, nil, ErrCredential
	}
	transport, ok := s.httpClient.Transport.(*http.Transport)
	if !ok {
		return nil, nil, ErrHTTPRequest
	}
	transport = transport.Clone()
	if d.tlsConfig != nil {
		transport.TLSClientConfig = d.tlsConfig.Clone()
	}
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	// HTTP transport derives the verified name from each URL, including a
	// separately authorized OAuth token endpoint.
	transport.TLSClientConfig.ServerName = ""
	roots := transport.TLSClientConfig.RootCAs
	if roots == nil {
		roots, err = x509.SystemCertPool()
		if err != nil {
			return nil, nil, ErrHTTPRequest
		}
	} else {
		roots = roots.Clone()
	}
	for _, cert := range certs {
		roots.AddCert(cert)
	}
	transport.TLSClientConfig.RootCAs = roots
	client := *s.httpClient
	client.Transport = transport
	return &client, transport.CloseIdleConnections, nil
}
