package sourcestream

import (
	"backend/internal/testkit"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"github.com/stretchr/testify/require"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSourceTLSReferencesMutualTLSAndRotation(t *testing.T) {
	testkit.NewTestDB(t)
	clientCAs := x509.NewCertPool()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Len(t, r.TLS.PeerCertificates, 1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCAs, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	issuerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	issuerTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-3 * time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	issuerDER, err := x509.CreateCertificate(rand.Reader, issuerTemplate, issuerTemplate, &issuerKey.PublicKey, issuerKey)
	require.NoError(t, err)
	issuer, err := x509.ParseCertificate(issuerDER)
	require.NoError(t, err)
	clientCAs.AddCert(issuer)
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	material := func(expired bool) Credential {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		notAfter := time.Now().Add(time.Hour)
		if expired {
			notAfter = time.Now().Add(-time.Hour)
		}
		der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), NotBefore: time.Now().Add(-2 * time.Hour), NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}, issuer, &key.PublicKey, issuerKey)
		require.NoError(t, err)
		private, err := x509.MarshalPKCS8PrivateKey(key)
		require.NoError(t, err)
		return Credential{Kind: "custom", Values: map[string]string{"caCertificatePem": ca, "clientCertificatePem": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), "clientPrivateKeyPem": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))}}
	}
	s := New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	s.httpClient = server.Client()
	defer s.Close()
	ctx := context.Background()
	created, err := s.CreateSource(ctx, SourceDefinition{Name: "mTLS rotation", Protocol: HTTP, Endpoint: server.URL, RuntimeLocation: "local", HTTP: &HTTPConnection{Authentication: "none"}}, 0)
	require.NoError(t, err)
	id, revision := created["id"].(int64), created["revision"].(int64)
	_, err = s.SaveCredential(ctx, id, revision, material(true), "")
	require.Error(t, err, "expired identity must roll back credential provisioning")
	saved, err := s.SaveCredential(ctx, id, revision, material(false), "")
	require.NoError(t, err, "failed provisioning must preserve the original revision")
	resolved, _, _, err := s.connection(ctx, id, nil)
	require.NoError(t, err)
	firstRef := resolved.CredentialRef
	client, cleanup, err := s.httpClientFor(resolved)
	require.NoError(t, err)
	response, err := client.Get(server.URL)
	require.NoError(t, err)
	response.Body.Close()
	cleanup()
	_, err = s.SaveCredential(ctx, id, saved["revision"].(int64), material(false), "")
	require.NoError(t, err)
	resolved, _, _, err = s.connection(ctx, id, nil)
	require.NoError(t, err)
	refs, err := resolved.tlsReferences()
	require.NoError(t, err)
	require.NotEqual(t, firstRef, resolved.CredentialRef)
	require.Equal(t, resolved.CredentialRef, refs.CARef)
	require.Equal(t, resolved.CredentialRef, refs.ClientIdentityRef)
	client, cleanup, err = s.httpClientFor(resolved)
	require.NoError(t, err)
	defer cleanup()
	response, err = client.Get(server.URL)
	require.NoError(t, err)
	response.Body.Close()
}

func TestSourceTLSReferencesResolveOwnedCiphertext(t *testing.T) {
	testkit.NewTestDB(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) }))
	defer server.Close()
	certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	s := New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	s.httpClient = server.Client()
	defer s.Close()
	ctx := context.Background()
	definition := SourceDefinition{Name: "TLS owner", Protocol: HTTP, Endpoint: server.URL, RuntimeLocation: "local", HTTP: &HTTPConnection{Authentication: "none"}}
	created, err := s.CreateSource(ctx, definition, 0)
	require.NoError(t, err)
	id := created["id"].(int64)
	saved, err := s.SaveCredential(ctx, id, created["revision"].(int64), Credential{Kind: "custom", Values: map[string]string{"caCertificatePem": certificate}}, "")
	require.NoError(t, err)
	resolved, _, _, err := s.connection(ctx, id, nil)
	require.NoError(t, err)
	require.NotNil(t, resolved.tlsConfig)
	client, closeClient, err := s.httpClientFor(resolved)
	require.NoError(t, err)
	defer closeClient()
	response, err := client.Get(server.URL)
	require.NoError(t, err)
	response.Body.Close()
	refs, err := resolved.tlsReferences()
	require.NoError(t, err)
	require.Equal(t, resolved.CredentialRef, refs.CARef)
	definition.Name = "TLS other owner"
	other, err := s.CreateSource(ctx, definition, 0)
	require.NoError(t, err)
	definition.Extensions = map[string]json.RawMessage{"tls": resolved.Extensions["tls"]}
	_, err = s.UpdateSource(ctx, other["id"].(int64), other["revision"].(int64), definition, "")
	require.Error(t, err, "a Source cannot borrow another Source's trust material")
	raw, err := json.Marshal(saved)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "BEGIN CERTIFICATE")
	require.NotContains(t, string(raw), "PRIVATE KEY")
}

func TestSourceTLSReferenceShapeAndAtomicProvisioning(t *testing.T) {
	base := SourceDefinition{Protocol: HTTP, Endpoint: "https://source.example", HTTP: &HTTPConnection{Authentication: "none"}}
	for _, raw := range []string{`null`, `{}`, `{"caRef":"/tmp/private.pem"}`, `{"caRef":"ref","privateKey":"secret"}`, `{"caRef":12}`} {
		d := base
		d.Extensions = map[string]json.RawMessage{"tls": json.RawMessage(raw)}
		_, err := d.tlsReferences()
		require.Error(t, err, raw)
	}
	d := base
	credential := Credential{Kind: "custom", Values: map[string]string{"caCertificatePem": "opaque-test-material"}}
	require.NoError(t, provisionTLSReferences(&d, "", "own-slot-1", credential))
	refs, err := d.tlsReferences()
	require.NoError(t, err)
	require.Equal(t, "own-slot-1", refs.CARef)
	raw, err := json.Marshal(d)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "opaque-test-material")
	require.NoError(t, provisionTLSReferences(&d, "own-slot-1", "own-slot-2", credential))
	refs, err = d.tlsReferences()
	require.NoError(t, err)
	require.Equal(t, "own-slot-2", refs.CARef)
	d.Endpoint = "http://source.example"
	_, err = d.tlsReferences()
	require.Error(t, err, "TLS references cannot silently use an unencrypted endpoint")
	values := httpCredentialValues(Credential{Kind: "custom", Values: map[string]string{"clientPrivateKeyPem": "private", "apiKey": "authorized-value"}})
	require.NotContains(t, values, "clientPrivateKeyPem")
	require.Equal(t, "authorized-value", values["apiKey"])
	file := SourceDefinition{Protocol: FileRead, Endpoint: "https://source.example"}
	require.True(t, fileCredentialAllowed(file, credential), "HTTPS file sources accept Source-owned TLS material")
	credential.Kind, credential.Username, credential.Secret = "basic", "user", "secret"
	require.True(t, fileCredentialAllowed(file, credential))
	credential.Values["unexpected"] = "value"
	require.False(t, fileCredentialAllowed(file, credential))
}

func TestScopedCertificateTrust(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"value":1}`)) }))
	defer server.Close()
	certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	certs, err := publicCertificates(certificate, true)
	require.NoError(t, err)
	require.Len(t, certs, 1)
	for _, bad := range []string{"bad", certificate + "trailing", "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----", certificate + "\n-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"} {
		_, err := publicCertificates(bad, true)
		require.Error(t, err)
	}
	base := server.Client().Transport.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12}
	s := &Service{httpClient: &http.Client{Transport: base, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	d := SourceDefinition{Endpoint: server.URL, HTTP: &HTTPConnection{Authentication: "none", CACertificate: certificate}}
	client, cleanup, err := s.httpClientFor(d)
	require.NoError(t, err)
	defer cleanup()
	res, err := client.Get(server.URL)
	require.NoError(t, err)
	res.Body.Close()
	_, err = s.httpClient.Get(server.URL)
	require.Error(t, err, "trust must not escape the Source")
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig.ServerName = "wrong-host.example"
	_, err = client.Get(server.URL)
	require.Error(t, err, "CA trust must not bypass hostname validation")
	restricted := &Service{httpClient: sourceHTTPClient()}
	client, cleanup, err = restricted.httpClientFor(d)
	require.NoError(t, err)
	defer cleanup()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	_, err = client.Do(req)
	require.Error(t, err, "CA trust must not bypass private-address restrictions")
}
