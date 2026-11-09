package sourcestream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/url"
	"strings"
	"time"
	"unicode"

	"backend/internal/repo"
)

type sourceTLSReferences struct {
	CARef             string `json:"caRef,omitempty"`
	ClientIdentityRef string `json:"clientIdentityRef,omitempty"`
}

func (d SourceDefinition) tlsReferences() (*sourceTLSReferences, error) {
	raw := d.Extensions["tls"]
	if len(raw) == 0 {
		return nil, nil
	}
	var refs sourceTLSReferences
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&refs) != nil || refs.CARef == "" && refs.ClientIdentityRef == "" {
		return nil, invalid("extensions.tls", "invalid")
	}
	for _, ref := range []string{refs.CARef, refs.ClientIdentityRef} {
		if len(ref) > 512 || strings.ContainsAny(ref, "/\\") || strings.IndexFunc(ref, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return nil, invalid("extensions.tls", "invalid")
		}
	}
	u, err := url.Parse(d.Endpoint)
	if err != nil || d.ConnectionRole() != ClientRole {
		return nil, invalid("extensions.tls", "unsupportedCapability")
	}
	switch d.Protocol {
	case HTTP, OAuth, FileRead:
		if u.Scheme != "https" {
			return nil, invalid("extensions.tls", "verifiedTransportRequired")
		}
	case MQTT:
		if u.Scheme != "mqtts" {
			return nil, invalid("extensions.tls", "verifiedTransportRequired")
		}
	case OPCUA:
		if d.OPCUA == nil || d.OPCUA.SecurityMode == "None" {
			return nil, invalid("extensions.tls", "verifiedTransportRequired")
		}
	case EdgeSQL:
		if d.SQL == nil || d.SQL.TLSMode == "disable" {
			return nil, invalid("extensions.tls", "verifiedTransportRequired")
		}
	default:
		return nil, invalid("extensions.tls", "unsupportedCapability")
	}
	return &refs, nil
}

func tlsValueKey(key string) bool {
	return key == "caCertificatePem" || key == "clientCertificatePem" || key == "clientPrivateKeyPem"
}

func hasTLSValues(c Credential) bool {
	return c.Values["caCertificatePem"] != "" || c.Values["clientCertificatePem"] != "" || c.Values["clientPrivateKeyPem"] != ""
}

func onlyTLSValues(c Credential) bool {
	if !hasTLSValues(c) {
		return false
	}
	for key := range c.Values {
		if !tlsValueKey(key) {
			return false
		}
	}
	return true
}

// A reference selects immutable ciphertext on this Source, not a file path or
// another Source's credentials. Keep the same encryption AAD and repository scope.
func (s *Service) resolveSourceTLS(ctx context.Context, id int64, d SourceDefinition, credential Credential) (SourceDefinition, Credential, error) {
	refs, err := d.tlsReferences()
	if err != nil || refs == nil {
		return d, credential, err
	}
	read := func(ref string) (Credential, error) {
		if ref == "" {
			return Credential{}, nil
		}
		stored, e := repo.NewSourceStreamRepo(ctx).Credential(ctx, id, ref)
		if e != nil {
			return Credential{}, ErrCredential
		}
		return s.cipher.Open(id, stored.Ciphertext)
	}
	ca, err := read(refs.CARef)
	if err != nil {
		return d, credential, err
	}
	identity, err := read(refs.ClientIdentityRef)
	if err != nil {
		return d, credential, err
	}
	u, _ := url.Parse(d.Endpoint)
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	values := map[string]string{}
	for key, value := range credential.Values {
		values[key] = value
	}
	if refs.CARef != "" {
		pem := ca.Values["caCertificatePem"]
		certs, e := publicCertificates(pem, d.Protocol != OPCUA)
		if e != nil || len(certs) == 0 {
			return d, credential, ErrCredential
		}
		roots, e := x509.SystemCertPool()
		if e != nil {
			return d, credential, ErrCredential
		}
		for _, cert := range certs {
			if time.Now().Before(cert.NotBefore) || !time.Now().Before(cert.NotAfter) {
				return d, credential, ErrCredential
			}
			roots.AddCert(cert)
		}
		config.RootCAs = roots
		values["caCertificatePem"] = pem
		if d.OPCUA != nil {
			value := *d.OPCUA
			value.ServerCertificate = pem
			d.OPCUA = &value
		}
	}
	if refs.ClientIdentityRef != "" {
		cert, key := identity.Values["clientCertificatePem"], identity.Values["clientPrivateKeyPem"]
		pair, e := tls.X509KeyPair([]byte(cert), []byte(key))
		if e != nil || len(pair.Certificate) == 0 {
			return d, credential, ErrCredential
		}
		leaf, e := x509.ParseCertificate(pair.Certificate[0])
		if e != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
			return d, credential, ErrCredential
		}
		config.Certificates = []tls.Certificate{pair}
		values["clientCertificatePem"], values["clientPrivateKeyPem"] = cert, key
	}
	d.tlsConfig = config
	credential.Values = values
	return d, credential, nil
}

// Atomic Source+credential creation also returns usable TLS references. Rotation
// replaces only references to the selected old slot; explicitly selected trust
// slots are not silently changed.
func provisionTLSReferences(d *SourceDefinition, oldRef, ref string, c Credential) error {
	if (c.Values["clientCertificatePem"] != "") != (c.Values["clientPrivateKeyPem"] != "") {
		return ErrCredential
	}
	refs, err := d.tlsReferences()
	if err != nil {
		return err
	}
	if refs == nil {
		refs = &sourceTLSReferences{}
	}
	if c.Values["caCertificatePem"] != "" && (refs.CARef == "" || refs.CARef == oldRef) {
		refs.CARef = ref
	}
	if c.Values["clientCertificatePem"] != "" && (refs.ClientIdentityRef == "" || refs.ClientIdentityRef == oldRef) {
		refs.ClientIdentityRef = ref
	}
	if refs.CARef == "" && refs.ClientIdentityRef == "" {
		return nil
	}
	if d.Extensions == nil {
		d.Extensions = map[string]json.RawMessage{}
	}
	d.Extensions["tls"], err = json.Marshal(refs)
	return err
}
