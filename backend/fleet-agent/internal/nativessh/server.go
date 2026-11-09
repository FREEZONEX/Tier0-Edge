// Package nativessh provides a certificate-only terminal on an installation's overlay.
package nativessh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"backend/internal/fleetprotocol"
	"backend/internal/infra/privatefs"
	"golang.org/x/crypto/ssh"
)

const Username = "tier0"
const Port = 2222

type Server struct {
	mu          sync.Mutex
	signer      ssh.Signer
	ca          ssh.PublicKey
	listener    net.Listener
	connections map[net.Conn]struct{}
	closed      bool
	slots       chan struct{}
}

func New(dir string) (*Server, error) {
	if err := supported(); err != nil {
		return nil, err
	}
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() || !privatefs.IsPrivate(dir, st) {
		return nil, errors.New("SSH_STATE_PERMISSIONS_INVALID")
	}
	path := filepath.Join(dir, "terminal-host-key")
	st, err = os.Lstat(path)
	if os.IsNotExist(err) {
		_, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return nil, e
		}
		block, e := ssh.MarshalPrivateKey(key, "tier0-native")
		if e != nil {
			return nil, e
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		_, e = f.Write(pem.EncodeToMemory(block))
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return nil, e
		}
		if closeErr != nil {
			return nil, closeErr
		}
		st, err = os.Lstat(path)
	}
	if err != nil || !st.Mode().IsRegular() || st.Size() > 16384 || !privatefs.IsPrivate(path, st) {
		return nil, errors.New("SSH_HOST_KEY_INVALID")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		return nil, err
	}
	return &Server{signer: signer, connections: map[net.Conn]struct{}{}, slots: make(chan struct{}, 8)}, nil
}

func (s *Server) Advertisement() *fleetprotocol.SSHAdvertisement {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := &fleetprotocol.SSHAdvertisement{HostPublicKey: string(ssh.MarshalAuthorizedKey(s.signer.PublicKey())), Username: Username, Port: Port}
	if s.ca != nil {
		a.TrustedCAFingerprint = ssh.FingerprintSHA256(s.ca)
	}
	return a
}

func (s *Server) Trust(public string) error {
	key, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(public))
	if err != nil || len(rest) != 0 || len(public) > 16384 {
		return errors.New("SSH_CA_INVALID")
	}
	if _, ok := key.(*ssh.Certificate); ok {
		return errors.New("SSH_CA_INVALID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ca != nil && !bytes.Equal(s.ca.Marshal(), key.Marshal()) {
		for conn := range s.connections {
			_ = conn.Close()
		}
	}
	s.ca = key
	return nil
}

func (s *Server) authenticate(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	s.mu.Lock()
	ca := s.ca
	s.mu.Unlock()
	cert, ok := key.(*ssh.Certificate)
	if !ok || ca == nil || conn.User() != Username || cert.CertType != ssh.UserCert || len(cert.CriticalOptions) != 0 || len(cert.ValidPrincipals) != 1 || cert.ValidPrincipals[0] != Username {
		return nil, errors.New("SSH_CERTIFICATE_REQUIRED")
	}
	if _, ok := cert.Extensions["permit-pty"]; !ok {
		return nil, errors.New("SSH_PTY_NOT_ALLOWED")
	}
	checker := ssh.CertChecker{IsUserAuthority: func(key ssh.PublicKey) bool { return bytes.Equal(ca.Marshal(), key.Marshal()) }}
	return checker.Authenticate(conn, cert)
}

func (s *Server) Serve(listener net.Listener) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = listener.Close()
		return
	}
	s.listener = listener
	s.mu.Unlock()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		select {
		case s.slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			<-s.slots
			return
		}
		s.connections[conn] = struct{}{}
		s.mu.Unlock()
		go s.serveConn(conn)
	}
}

func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.listener != nil {
		_ = s.listener.Close()
	}
	for conn := range s.connections {
		_ = conn.Close()
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer func() { _ = conn.Close(); s.mu.Lock(); delete(s.connections, conn); s.mu.Unlock(); <-s.slots }()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	cfg := &ssh.ServerConfig{PublicKeyCallback: s.authenticate, MaxAuthTries: 3}
	cfg.AddHostKey(s.signer)
	server, channels, requests, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer server.Close()
	_ = conn.SetDeadline(time.Time{})
	go ssh.DiscardRequests(requests)
	// One terminal per authenticated connection; no forwarding/subsystems.
	used := false
	for incoming := range channels {
		if incoming.ChannelType() != "session" || used {
			_ = incoming.Reject(ssh.Prohibited, "terminal session only")
			continue
		}
		ch, req, err := incoming.Accept()
		if err != nil {
			continue
		}
		used = true
		go func() { serveSession(ch, req); _ = server.Close() }()
	}
}

type terminal interface {
	io.ReadWriteCloser
	Resize(uint32, uint32) error
	Wait() uint32
}

func serveSession(ch ssh.Channel, requests <-chan *ssh.Request) {
	defer ch.Close()
	var term terminal
	defer func() {
		if term != nil {
			_ = term.Close()
		}
	}()
	cols, rows := uint32(120), uint32(30)
	var done <-chan uint32
	for {
		select {
		case code := <-done:
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{code}))
			return
		case req, ok := <-requests:
			if !ok {
				return
			}
			accepted := false
			switch req.Type {
			case "pty-req":
				var v struct {
					Term                      string
					Cols, Rows, Width, Height uint32
					Modes                     string
				}
				if term == nil && ssh.Unmarshal(req.Payload, &v) == nil && validSize(v.Cols, v.Rows) {
					cols, rows = v.Cols, v.Rows
					accepted = true
				}
			case "window-change":
				var v struct{ Cols, Rows, Width, Height uint32 }
				if ssh.Unmarshal(req.Payload, &v) == nil && validSize(v.Cols, v.Rows) {
					cols, rows = v.Cols, v.Rows
					accepted = term == nil || term.Resize(cols, rows) == nil
				}
			case "shell", "exec":
				command := ""
				valid := req.Type == "shell" && len(req.Payload) == 0
				if req.Type == "exec" {
					var v struct{ Command string }
					valid = ssh.Unmarshal(req.Payload, &v) == nil && len(v.Command) <= 32768
					command = v.Command
				}
				if term == nil && valid {
					var err error
					term, err = startTerminal(cols, rows, command)
					if err == nil {
						accepted = true
						current := term
						finished := make(chan uint32, 1)
						done = finished
						// SSH stdin EOF is a half-close (exec clients send it before
						// waiting for output); it must not close the session channel.
						go func() { _, _ = io.Copy(current, ch) }()
						go func() { _, _ = io.Copy(ch, current); finished <- current.Wait() }()
					} else {
						_, _ = io.WriteString(ch.Stderr(), "Native terminal could not start.\r\n")
					}
				}
			}
			if req.WantReply {
				_ = req.Reply(accepted, nil)
			}
		}
	}
}

func validSize(cols, rows uint32) bool { return cols > 0 && rows > 0 && cols <= 500 && rows <= 300 }
