//go:build darwin || linux

package nativessh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestUnixCertificateTerminal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("native Edge runs as a non-root user")
	}
	t.Setenv("FLEET_AGENT_LOCAL_TOKEN", "must-not-reach-shell")
	signer := func() ssh.Signer {
		t.Helper()
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		s, err := ssh.NewSignerFromKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	server, err := New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go server.Serve(listener)
	ca, key := signer(), signer()
	if err := server.Trust(string(ssh.MarshalAuthorizedKey(ca.PublicKey()))); err != nil {
		t.Fatal(err)
	}
	dial := func(identity ssh.Signer) (*ssh.Client, error) {
		return ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
			User: Username, Auth: []ssh.AuthMethod{ssh.PublicKeys(identity)},
			HostKeyCallback: ssh.FixedHostKey(server.signer.PublicKey()), Timeout: 5 * time.Second,
		})
	}
	if rejected, err := dial(key); err == nil {
		rejected.Close()
		t.Fatal("plain user key was accepted")
	}
	cert := &ssh.Certificate{
		Key: key.PublicKey(), CertType: ssh.UserCert, ValidPrincipals: []string{Username},
		ValidAfter: uint64(time.Now().Add(-time.Minute).Unix()), ValidBefore: uint64(time.Now().Add(time.Minute).Unix()),
		Permissions: ssh.Permissions{Extensions: map[string]string{"permit-pty": ""}},
	}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		t.Fatal(err)
	}
	identity, err := ssh.NewCertSigner(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	client, err := dial(identity)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.RequestPty("xterm", 30, 120, nil); err != nil {
		t.Fatal(err)
	}
	input, err := session.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	session.Stdout = &output
	if err := session.Shell(); err != nil {
		t.Fatal(err)
	}
	size := ssh.Marshal(struct{ Cols, Rows, Width, Height uint32 }{91, 43, 0, 0})
	if ok, err := session.SendRequest("window-change", true, size); err != nil || !ok {
		t.Fatalf("resize was not accepted: %v", err)
	}
	if _, err := input.Write([]byte("stty size; printenv FLEET_AGENT_LOCAL_TOKEN; exit 7\n")); err != nil {
		t.Fatal(err)
	}
	if err, ok := session.Wait().(*ssh.ExitError); !ok || err.ExitStatus() != 7 {
		t.Fatalf("expected shell exit status 7, got %v", err)
	}
	// Bash versions differ in whether they emit a CR or CRLF before the output.
	lines := bytes.ReplaceAll(output.Bytes(), []byte("\r"), []byte("\n"))
	if !bytes.Contains(lines, []byte("\n43 91\n")) || bytes.Contains(lines, []byte("must-not-reach-shell")) {
		t.Fatalf("unexpected terminal output: %q", output.Bytes())
	}
}
