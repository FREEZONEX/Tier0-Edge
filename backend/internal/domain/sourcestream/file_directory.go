package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jfjallid/go-smb/smb"
	"github.com/jfjallid/go-smb/spnego"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/html"
)

const maxDirectoryEntries = 2000

type FileDirectory struct {
	ID         string   `json:"id"`
	Path       string   `json:"path"`
	Extensions []string `json:"extensions"`
	Format     string   `json:"format"`
}

func validFileRelative(p string) bool {
	return len(p) <= 2048 && p != "" && p != "." && p != ".." && !path.IsAbs(p) && path.Clean(p) == p && !strings.HasPrefix(p, "../") && !strings.ContainsAny(p, "\\:\x00\r\n")
}
func validateFileDirectory(d FileDirectory, scheme string) error {
	if !fileResourceID.MatchString(d.ID) || (d.Path != "." && !validFileRelative(d.Path)) || len(d.Extensions) == 0 || len(d.Extensions) > 16 {
		return invalid("file.directories", "invalid")
	}
	if d.Format != "bytes" && d.Format != "edifact" {
		return invalid("file.directories.format", "unsupported")
	}
	for _, ext := range d.Extensions {
		if len(ext) < 2 || len(ext) > 16 || ext[0] != '.' || strings.IndexFunc(ext[1:], func(c rune) bool { return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') }) >= 0 {
			return invalid("file.directories.extensions", "invalid")
		}
	}
	if d.Format == "edifact" && (scheme != "sftp" || len(d.Extensions) != 1 || d.Extensions[0] != ".edi") {
		return invalid("file.directories", "sftpEDIRequired")
	}
	return nil
}
func (d FileDirectory) ref() string {
	if d.Format == "edifact" {
		return "edi:" + d.ID
	}
	return "file-directory:" + d.ID
}
func (d FileDirectory) accepts(name string) bool {
	if len(name) > 255 || strings.HasPrefix(name, ".") || path.Base(name) != name || !validFileRelative(name) {
		return false
	}
	for _, ext := range d.Extensions {
		if path.Ext(name) == ext {
			return true
		}
	}
	return false
}

// Cap the encrypted directory response as well as entry count. SFTP/SMB client
// APIs accumulate a whole native directory, so a hostile listing cannot grow it
// without bound before the application applies its declared discovery limit.
type directoryConn struct {
	net.Conn
	remaining int
}

func (c *directoryConn) Read(p []byte) (int, error) {
	if c.remaining <= 0 {
		return 0, invalid("directory", "responseTooLarge")
	}
	if len(p) > c.remaining {
		p = p[:c.remaining]
	}
	n, e := c.Conn.Read(p)
	c.remaining -= n
	return n, e
}
func listFileDirectory(ctx context.Context, d SourceDefinition, selection FileDirectory, c Credential) ([]string, error) {
	u, e := url.Parse(d.Endpoint)
	if e != nil {
		return nil, e
	}
	if c.Kind != "" && !fileCredentialAllowed(d, c) {
		return nil, ErrCredential
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return listHTTPDirectory(ctx, d, selection, c)
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
	socket, e := dialFileSource(ctx, "tcp", address)
	if e != nil {
		return nil, e
	}
	defer socket.Close()
	stop := context.AfterFunc(ctx, func() { socket.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		socket.SetDeadline(deadline)
	}
	conn := &directoryConn{Conn: socket, remaining: 4 * 1024 * 1024}
	remote := path.Join("/", u.Path, selection.Path)
	names := []string{}
	if u.Scheme == "sftp" {
		hostKey, _, _, _, e := ssh.ParseAuthorizedKey([]byte(d.File.SSHHostKey))
		if e != nil {
			return nil, ErrCredential
		}
		user, auth := c.Username, ssh.Password(c.Secret)
		if c.Kind == "custom" {
			signer, e := ssh.ParsePrivateKey([]byte(c.Values["privateKey"]))
			if e != nil {
				return nil, ErrCredential
			}
			user, auth = c.Values["username"], ssh.PublicKeys(signer)
		} else if c.Kind != "basic" {
			return nil, ErrCredential
		}
		cc, ch, requests, e := ssh.NewClientConn(conn, address, &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: ssh.FixedHostKey(hostKey)})
		if e != nil {
			return nil, e
		}
		client := ssh.NewClient(cc, ch, requests)
		defer client.Close()
		fs, e := sftp.NewClient(client)
		if e != nil {
			return nil, e
		}
		defer fs.Close()
		root, e := fs.RealPath(path.Join("/", u.Path))
		if e != nil {
			return nil, e
		}
		actual, e := fs.RealPath(remote)
		if e != nil {
			return nil, e
		}
		if actual != root && !strings.HasPrefix(actual, strings.TrimRight(root, "/")+"/") {
			return nil, invalid("directory", "outsideRoot")
		}
		entries, e := fs.ReadDirContext(ctx, actual)
		if e != nil {
			return nil, e
		}
		if len(entries) > maxDirectoryEntries {
			return nil, invalid("directory", "entryLimitExceeded")
		}
		for _, entry := range entries {
			if entry.Mode().IsRegular() && selection.accepts(entry.Name()) {
				names = append(names, entry.Name())
			}
		}
	} else {
		if c.Kind != "basic" {
			return nil, ErrCredential
		}
		portNumber, e := strconv.Atoi(port)
		if e != nil {
			return nil, e
		}
		session, e := smb.NewConnection(smb.Options{Host: u.Hostname(), Port: portNumber, ProxyDialer: fileSMBConnection{conn}, DialTimeout: 5 * time.Second, SMB2Only: true, Encryption: smb.EncryptionRequired, RequireMessageSigning: true, Initiator: &spnego.NTLMInitiator{User: c.Username, Password: c.Secret}})
		if e != nil {
			return nil, e
		}
		defer session.Close()
		share, dir, _ := strings.Cut(strings.TrimPrefix(remote, "/"), "/")
		if e = session.TreeConnect(share); e != nil {
			return nil, e
		}
		defer session.TreeDisconnect(share)
		entries, e := session.ListDirectory(share, strings.ReplaceAll(dir, "/", "\\"), "*")
		if e != nil {
			return nil, e
		}
		if len(entries) > maxDirectoryEntries {
			return nil, invalid("directory", "entryLimitExceeded")
		}
		for _, entry := range entries {
			if !entry.IsDir && selection.accepts(entry.Name) {
				names = append(names, entry.Name)
			}
		}
	}
	sort.Strings(names)
	return slices.Compact(names), nil
}

func listHTTPDirectory(ctx context.Context, d SourceDefinition, selection FileDirectory, c Credential) ([]string, error) {
	res, e := fileHTTPGet(ctx, d, FileSelection{Path: selection.Path + "/"}, c, nil)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("directory HTTP status %d", res.StatusCode)
	}
	if encoding := res.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return nil, invalid("directory", "unsupportedEncoding")
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, 1024*1024+1))
	if e != nil {
		return nil, e
	}
	if len(raw) > 1024*1024 {
		return nil, invalid("directory", "responseTooLarge")
	}
	base, e := url.Parse(d.Endpoint)
	if e != nil {
		return nil, e
	}
	base.Path = strings.TrimRight(path.Join("/", base.Path, selection.Path), "/") + "/"
	base.RawPath = ""
	tokenizer := html.NewTokenizer(strings.NewReader(string(raw)))
	unique := map[string]bool{}
	for token := tokenizer.Next(); token != html.ErrorToken; token = tokenizer.Next() {
		if token != html.StartTagToken {
			continue
		}
		tag := tokenizer.Token()
		if tag.Data != "a" {
			continue
		}
		for _, attr := range tag.Attr {
			if attr.Key != "href" {
				continue
			}
			link, e := base.Parse(attr.Val)
			if e != nil || link.Scheme != base.Scheme || link.Host != base.Host || link.User != nil || link.RawQuery != "" || link.Fragment != "" {
				continue
			}
			name, ok := strings.CutPrefix(link.Path, base.Path)
			if !ok || !selection.accepts(name) {
				continue
			}
			unique[name] = true
			if len(unique) > maxDirectoryEntries {
				return nil, invalid("directory", "entryLimitExceeded")
			}
		}
	}
	if e = tokenizer.Err(); e != nil && e != io.EOF {
		return nil, e
	}
	names := make([]string, 0, len(unique))
	for name := range unique {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func directoryFileSelection(d FileDirectory, name string) FileSelection {
	sum := sha256.Sum256([]byte(d.ID + "\x00" + name))
	return FileSelection{ID: "directory-" + hex.EncodeToString(sum[:]), Path: path.Join(d.Path, name)}
}
