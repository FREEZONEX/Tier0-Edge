package sourcestream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jfjallid/go-smb/smb"
	"github.com/jfjallid/go-smb/spnego"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const FileRead Protocol = "file-read"
const maxSourceFileBytes = 32 << 20

var fileResourceID = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$")

func fileCredentialAllowed(d SourceDefinition, c Credential) bool {
	u, e := url.Parse(d.Endpoint)
	if e != nil {
		return false
	}
	if u.Scheme == "https" {
		for key := range c.Values {
			if !tlsValueKey(key) {
				return false
			}
		}
		return c.Kind == "basic" || c.Kind == "bearer" || c.Kind == "custom" && hasTLSValues(c)
	}
	if c.Kind == "custom" && u.Scheme == "sftp" {
		if len(c.Values) != 2 || c.Values["username"] == "" {
			return false
		}
		_, e := ssh.ParsePrivateKey([]byte(c.Values["privateKey"]))
		return e == nil
	}
	return len(c.Values) == 0 && (c.Kind == "basic" && (u.Scheme == "smb" || u.Scheme == "sftp" || u.Scheme == "https") || c.Kind == "bearer" && u.Scheme == "https")
}

type FileSourceOptions struct {
	Resources     []FileSelection `json:"resources,omitempty"`
	Directories   []FileDirectory `json:"directories,omitempty"`
	CACertificate string          `json:"caCertificate,omitempty"`
	SSHHostKey    string          `json:"sshHostKey,omitempty"`
}
type FileSelection struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	// An omitted table list declares a byte resource. A table list only exposes those QC2000 tables.
	Tables []string `json:"tables,omitempty"`
}

func (d SourceDefinition) validateFileSource(u *url.URL) error {
	if d.File == nil || d.Role == ServerRole || d.MQTT != nil || d.Modbus != nil || d.OPCUA != nil || d.HTTP != nil || len(d.File.Resources)+len(d.File.Directories) == 0 || len(d.File.Resources)+len(d.File.Directories) > 128 {
		return invalid("file", "invalid")
	}
	switch u.Scheme {
	case "https", "http":
		if u.Scheme == "http" && (d.CredentialRef != "" || d.File.CACertificate != "") {
			return invalid("endpoint", "tlsRequired")
		}
	case "sftp", "smb":
		if u.Scheme == "smb" && strings.Trim(u.Path, "/") == "" {
			return invalid("endpoint", "shareRequired")
		}
	default:
		return invalid("endpoint", "invalidScheme")
	}
	if _, e := publicCertificates(d.File.CACertificate, true); e != nil || d.File.CACertificate != "" && u.Scheme != "https" {
		return invalid("file.caCertificate", "invalid")
	}
	if u.Scheme == "sftp" {
		if _, _, _, rest, e := ssh.ParseAuthorizedKey([]byte(d.File.SSHHostKey)); e != nil || len(bytes.TrimSpace(rest)) != 0 {
			return invalid("file.sshHostKey", "invalid")
		}
	} else if d.File.SSHHostKey != "" {
		return invalid("file.sshHostKey", "unsupported")
	}
	seen := map[string]bool{}
	for _, directory := range d.File.Directories {
		if err := validateFileDirectory(directory, u.Scheme); err != nil {
			return err
		}
		if seen[directory.ID] {
			return invalid("file.directories", "duplicateID")
		}
		seen[directory.ID] = true
	}
	for _, r := range d.File.Resources {
		if !fileResourceID.MatchString(r.ID) || seen[r.ID] || len(r.Path) > 2048 || r.Path == "" || path.IsAbs(r.Path) || path.Clean(r.Path) != r.Path || r.Path == "." || r.Path == ".." || strings.HasPrefix(r.Path, "../") || strings.ContainsAny(r.Path, "\\:\x00\r\n") {
			return invalid("file.resources", "invalid")
		}
		seen[r.ID] = true
		tables := map[string]bool{}
		for _, t := range r.Tables {
			if _, ok := qcTables[t]; !ok || tables[t] {
				return invalid("file.resources.tables", "invalid")
			}
			tables[t] = true
		}
	}
	return nil
}

func dialFileSource(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil {
		return nil, e
	}
	ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if e != nil || len(ips) == 0 {
		return nil, ErrUnavailable
	}
	for _, ip := range ips {
		if !httpSourceAddressAllowed(host, ip) {
			return nil, ErrForbidden
		}
	}
	return new(net.Dialer).DialContext(ctx, network, net.JoinHostPort(ips[0].Unmap().String(), port))
}

// Every observation opens a new read handle. Remote size and modification time alone
// cannot distinguish same-name replacements on Plant0's minute-resolution directory.
func transferSourceFile(ctx context.Context, d SourceDefinition, r FileSelection, c Credential, dst io.Writer) (string, error) {
	u, err := url.Parse(d.Endpoint)
	if err != nil {
		return "", err
	}
	if c.Kind != "" && !fileCredentialAllowed(d, c) {
		return "", ErrCredential
	}
	h := sha256.New()
	copyFile := func(src io.Reader, size int64) (string, error) {
		if size > maxSourceFileBytes {
			return "", invalid("file", "tooLarge")
		}
		n, e := io.Copy(io.MultiWriter(dst, h), io.LimitReader(src, maxSourceFileBytes+1))
		if e != nil {
			return "", e
		}
		if n > maxSourceFileBytes || size >= 0 && n != size {
			return "", invalid("file", "sizeMismatch")
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	remote := path.Join("/", u.Path, r.Path)
	if u.Scheme == "http" || u.Scheme == "https" {
		res, e := fileHTTPGet(ctx, d, r, c, nil)
		if e != nil {
			return "", e
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return "", fmt.Errorf("file HTTP status %d", res.StatusCode)
		}
		return copyFile(res.Body, res.ContentLength)
	}
	if c.Kind != "basic" && !(c.Kind == "custom" && u.Scheme == "sftp") {
		return "", ErrCredential
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "sftp" {
			port = "22"
		} else {
			port = "445"
		}
	}
	address := net.JoinHostPort(u.Hostname(), port)
	conn, e := dialFileSource(ctx, "tcp", address)
	if e != nil {
		return "", e
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	if u.Scheme == "sftp" {
		key, _, _, _, e := ssh.ParseAuthorizedKey([]byte(d.File.SSHHostKey))
		if e != nil {
			return "", e
		}
		user, auth := c.Username, ssh.Password(c.Secret)
		if c.Kind == "custom" {
			signer, e := ssh.ParsePrivateKey([]byte(c.Values["privateKey"]))
			if e != nil {
				return "", ErrCredential
			}
			user, auth = c.Values["username"], ssh.PublicKeys(signer)
		}
		cc, ch, req, e := ssh.NewClientConn(conn, address, &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: ssh.FixedHostKey(key)})
		if e != nil {
			return "", e
		}
		client := ssh.NewClient(cc, ch, req)
		defer client.Close()
		fs, e := sftp.NewClient(client)
		if e != nil {
			return "", e
		}
		defer fs.Close()
		root, e := fs.RealPath(path.Join("/", u.Path))
		if e != nil {
			return "", e
		}
		actual, e := fs.RealPath(remote)
		if e != nil {
			return "", e
		}
		if !strings.HasPrefix(actual, strings.TrimRight(root, "/")+"/") {
			return "", invalid("file", "outsideRoot")
		}
		f, e := fs.Open(actual)
		if e != nil {
			return "", e
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil {
			return "", e
		}
		if !st.Mode().IsRegular() {
			return "", invalid("file", "notRegular")
		}
		return copyFile(f, st.Size())
	}
	share, name, _ := strings.Cut(strings.TrimPrefix(remote, "/"), "/")
	portNumber, e := strconv.Atoi(port)
	if e != nil {
		return "", e
	}
	session, e := smb.NewConnection(smb.Options{Host: u.Hostname(), Port: portNumber, ProxyDialer: fileSMBConnection{conn}, DialTimeout: 5 * time.Second, SMB2Only: true, Encryption: smb.EncryptionRequired, RequireMessageSigning: true, Initiator: &spnego.NTLMInitiator{User: c.Username, Password: c.Secret}})
	if e != nil {
		return "", e
	}
	defer session.Close()
	total := 0
	writer := io.MultiWriter(dst, h)
	e = session.RetrieveFileContext(ctx, share, strings.ReplaceAll(name, "/", "\\"), 0, func(b []byte) (int, error) {
		total += len(b)
		if total > maxSourceFileBytes {
			return 0, invalid("file", "tooLarge")
		}
		return writer.Write(b)
	})
	if e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileHTTPGet(ctx context.Context, d SourceDefinition, r FileSelection, c Credential, headers http.Header) (*http.Response, error) {
	if c.Kind != "" && !fileCredentialAllowed(d, c) {
		return nil, ErrCredential
	}
	u, e := url.Parse(d.Endpoint)
	if e != nil {
		return nil, e
	}
	u.Path = path.Join("/", u.Path, r.Path)
	if strings.HasSuffix(r.Path, "/") {
		u.Path = strings.TrimRight(u.Path, "/") + "/"
	}
	u.RawPath = ""
	tr := &http.Transport{DisableKeepAlives: true, DisableCompression: true, DialContext: dialFileSource, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	if d.tlsConfig != nil {
		tr.TLSClientConfig = d.tlsConfig.Clone()
	}
	tr.TLSClientConfig.ServerName = ""
	if d.File.CACertificate != "" {
		pool := tr.TLSClientConfig.RootCAs
		if pool == nil {
			pool, _ = x509.SystemCertPool()
		}
		if pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(d.File.CACertificate)) {
			return nil, ErrCredential
		}
		tr.TLSClientConfig.RootCAs = pool
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if e != nil {
		return nil, e
	}
	if headers != nil {
		req.Header = headers
	}
	if c.Kind == "basic" {
		req.SetBasicAuth(c.Username, c.Secret)
	} else if c.Kind == "bearer" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	client := http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client.Do(req)
}

// The protocol session uses the already validated destination and bounded socket.
type fileSMBConnection struct{ net.Conn }

func (d fileSMBConnection) Dial(string, string) (net.Conn, error) { return d.Conn, nil }
func (d fileSMBConnection) DialContext(context.Context, string, string) (net.Conn, error) {
	return d.Conn, nil
}

func stableSourceFile(ctx context.Context, d SourceDefinition, r FileSelection, c Credential, expected string) (*os.File, string, error) {
	f, e := os.CreateTemp("", "tier0-edge-file-*")
	if e != nil {
		return nil, "", e
	}
	fail := func(err error) (*os.File, string, error) { f.Close(); os.Remove(f.Name()); return nil, "", err }
	digest, e := transferSourceFile(ctx, d, r, c, f)
	if e != nil {
		return fail(e)
	}
	if expected != "" && expected != digest {
		return fail(invalid("cursor", "fileVersionChanged"))
	}
	select {
	case <-ctx.Done():
		return fail(ctx.Err())
	case <-time.After(250 * time.Millisecond):
	}
	confirmed, e := transferSourceFile(ctx, d, r, c, io.Discard)
	if e != nil {
		return fail(e)
	}
	if confirmed != digest {
		return fail(invalid("file", "writeInProgress"))
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return fail(e)
	}
	return f, digest, nil
}
