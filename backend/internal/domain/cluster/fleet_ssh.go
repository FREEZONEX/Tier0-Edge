package cluster

// Fleet SSH connects directly to an operator-approved host. Target Backend and
// Fleet Agent do not proxy shell traffic. Execs are independent, not a PTY.
import (
	"backend/internal/infra/privatefs"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/contextx"
	auditdomain "backend/internal/domain/audit"
	"backend/internal/domain/license"
	"backend/internal/repo"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
	"gorm.io/gorm"
)

var sshReference = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
var fleetSSHProcess = uuid.NewString()
var fleetSSHSlots = make(chan struct{}, 8)
var fleetSSHActive sync.Map

type FleetSSHProfile struct {
	DefaultUsername    string `json:"-"`
	DefaultPort        int    `json:"-"`
	ConnectionMode     string `json:"connectionMode"`
	Managed            bool   `json:"managed"`
	SetupState         string `json:"setupState"`
	SetupError         string `json:"setupError"`
	LastCheckedAt      int64  `json:"lastCheckedAt"`
	NodeID             string `gorm:"primaryKey" json:"nodeId"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	AuthType           string `json:"authType"`
	CredentialRef      string `json:"credentialRef"`
	HostKeyFingerprint string `json:"hostKeyFingerprint"`
	Enabled            bool   `json:"enabled"`
	Revision           int64  `json:"revision"`
}

func (FleetSSHProfile) TableName() string { return "fleet_ssh_profile" }

type FleetSSHExecution struct {
	ID         string `gorm:"primaryKey" json:"id"`
	NodeID     string `json:"nodeId"`
	ActorID    int64  `json:"actorId"`
	APIKeyID   int64  `json:"-"`
	ProcessID  string `json:"-"`
	Script     string `json:"-"`
	Status     string `json:"status"`
	Output     string `json:"-"`
	Truncated  bool   `json:"truncated"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
	FinishedAt int64  `json:"finishedAt"`
}

func (FleetSSHExecution) TableName() string { return "fleet_ssh_execution" }

type FleetSSHRequest struct {
	NodeID           string           `json:"nodeId"`
	Profile          *FleetSSHProfile `json:"profile,omitempty"`
	ExpectedRevision int64            `json:"expectedRevision"`
	ExecutionID      string           `json:"executionId"`
	Script           string           `json:"script"`
	TimeoutSeconds   int              `json:"timeoutSeconds"`
	Cursor           int              `json:"cursor"`
}

// Read only named, regular, owner-private files inside the installation's
// protected identity directory; an API caller cannot supply an arbitrary path.
func (s *Service) sshCredential(ref string) ([]byte, error) {
	if !sshReference.MatchString(ref) || s.cfg.IdentityDir == "" {
		return nil, ErrInvalid
	}
	dir := filepath.Join(s.cfg.IdentityDir, "ssh-credentials")
	for _, p := range []string{s.cfg.IdentityDir, dir, filepath.Join(dir, ref+".json")} {
		st, err := os.Lstat(p)
		if err != nil {
			return nil, errors.New("SSH credential unavailable")
		}
		if !privatefs.IsPrivate(p, st) {
			return nil, errors.New("SSH credential permissions invalid")
		}
	}
	p := filepath.Join(dir, ref+".json")
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 128*1024 {
		return nil, ErrInvalid
	}
	return os.ReadFile(p)
}
func (s *Service) sshConnect(ctx context.Context, p FleetSSHProfile) (*ssh.Client, error) {
	raw, err := s.sshCredential(p.CredentialRef)
	if err != nil {
		return nil, sshFailure("SSH_CREDENTIAL_INVALID")
	}
	var credential struct {
		Password          string   `json:"password"`
		PrivateKey        string   `json:"privateKey"`
		Certificate       string   `json:"certificate"`
		Passphrase        string   `json:"passphrase"`
		CAPrivateKey      string   `json:"caPrivateKey"`
		AllowedPrincipals []string `json:"allowedPrincipals"`
	}
	if json.Unmarshal(raw, &credential) != nil {
		return nil, sshFailure("SSH_CREDENTIAL_INVALID")
	}
	var auth ssh.AuthMethod
	switch p.AuthType {
	case "password":
		if credential.Password == "" {
			return nil, sshFailure("SSH_CREDENTIAL_INVALID")
		}
		auth = ssh.Password(credential.Password)
	case "ephemeralCertificate":
		allowed := p.Managed && p.CredentialRef == managedSSHReference(p.NodeID)
		for _, principal := range credential.AllowedPrincipals {
			if principal == p.Username {
				allowed = true
			}
		}
		if !allowed {
			return nil, ErrUnauthorized
		}
		ca, e := ssh.ParsePrivateKey([]byte(credential.CAPrivateKey))
		if e != nil {
			return nil, sshFailure("SSH_CREDENTIAL_INVALID")
		}
		_, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return nil, e
		}
		signer, e := ssh.NewSignerFromKey(key)
		if e != nil {
			return nil, e
		}
		serial := make([]byte, 8)
		if _, e = rand.Read(serial); e != nil {
			return nil, e
		}
		now := time.Now().Unix()
		cert := &ssh.Certificate{Key: signer.PublicKey(), Serial: binary.BigEndian.Uint64(serial), CertType: ssh.UserCert, KeyId: p.NodeID + ":" + uuid.NewString(), ValidPrincipals: []string{p.Username}, ValidAfter: uint64(now - 10), ValidBefore: uint64(now + 120), Permissions: ssh.Permissions{Extensions: map[string]string{"permit-pty": ""}}}
		if e = cert.SignCert(rand.Reader, ca); e != nil {
			return nil, e
		}
		signer, e = ssh.NewCertSigner(cert, signer)
		if e != nil {
			return nil, e
		}
		auth = ssh.PublicKeys(signer)
	case "key", "certificate":
		var signer ssh.Signer
		var e error
		if credential.Passphrase != "" {
			signer, e = ssh.ParsePrivateKeyWithPassphrase([]byte(credential.PrivateKey), []byte(credential.Passphrase))
		} else {
			signer, e = ssh.ParsePrivateKey([]byte(credential.PrivateKey))
		}
		if e != nil {
			return nil, sshFailure("SSH_CREDENTIAL_INVALID")
		}
		if p.AuthType == "certificate" {
			public, _, _, _, e := ssh.ParseAuthorizedKey([]byte(credential.Certificate))
			if e != nil {
				return nil, sshFailure("SSH_CREDENTIAL_INVALID")
			}
			cert, ok := public.(*ssh.Certificate)
			if !ok || cert.CertType != ssh.UserCert {
				return nil, sshFailure("SSH_CREDENTIAL_INVALID")
			}
			signer, e = ssh.NewCertSigner(cert, signer)
			if e != nil {
				return nil, sshFailure("SSH_CREDENTIAL_INVALID")
			}
		}
		auth = ssh.PublicKeys(signer)
	default:
		return nil, sshFailure("SSH_CREDENTIAL_INVALID")
	}
	cfg := &ssh.ClientConfig{User: p.Username, Auth: []ssh.AuthMethod{auth}, Timeout: 10 * time.Second, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if ssh.FingerprintSHA256(key) != p.HostKeyFingerprint {
			return sshFailure("SSH_HOST_KEY_CHANGED")
		}
		return nil
	}}
	address := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	var dialer proxy.ContextDialer = &net.Dialer{Timeout: 10 * time.Second}
	if s.cfg.SSHSOCKSAddress != "" {
		socks, e := proxy.SOCKS5("tcp", s.cfg.SSHSOCKSAddress, nil, &net.Dialer{Timeout: 10 * time.Second})
		if e != nil {
			return nil, sshFailure("SSH_PROXY_UNAVAILABLE")
		}
		dialer = socks.(proxy.ContextDialer)
	}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, sshTransportFailure(err, false)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	cc, ch, rq, err := ssh.NewClientConn(conn, address, cfg)
	if err != nil {
		conn.Close()
		return nil, sshTransportFailure(err, true)
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(cc, ch, rq), nil
}
func (s *Service) auditSSH(actor int64, nodeID, action string, detail map[string]any) {
	if s.audit != nil {
		detail["action"] = action
		code := 200
		if detail["state"] == "failed" || detail["state"] == "unknown" || detail["status"] == "failed" {
			code = 500
		}
		s.audit.RecordDirect(&auditdomain.Operator{ID: strconv.FormatInt(actor, 10), Name: strconv.FormatInt(actor, 10)}, auditdomain.RecordInput{ScopeType: auditdomain.ScopeTypePlatform, ResType: auditdomain.ResTypeFleet, ResID: nodeID, BusinessType: auditdomain.BizUpdate, Code: code, Detail: detail})
	}
}
func (s *Service) FleetSSH(ctx context.Context, op string, req FleetSSHRequest) (any, error) {
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok || !license.Default().IsHost() || !contextx.HasResource(actor, "fleet.ssh.manage") || actor.APIKeyAppID != "" {
		return nil, ErrUnauthorized
	}
	if _, err := s.fleetRepo.CollaborationNode(ctx, req.NodeID); err != nil {
		return nil, err
	}
	db := repo.GetCommonConn(ctx)
	var p FleetSSHProfile
	if op == "sshExecutionsList" {
		rows := []FleetSSHExecution{}
		err := db.Omit("script", "output").Where("node_id=? AND actor_id=? AND api_key_id=?", req.NodeID, actor.UserID, actor.APIKeyID).Order("created_at DESC").Limit(50).Find(&rows).Error
		return map[string]any{"items": rows}, err
	}
	if op == "sshProfileSave" {
		sshSetupMu.Lock()
		defer sshSetupMu.Unlock()
		if req.Profile == nil {
			return nil, ErrInvalid
		}
		var existing FleetSSHProfile
		if err := db.Where("node_id=?", req.NodeID).Take(&existing).Error; err == nil && existing.Managed {
			return nil, ErrUnauthorized
		}
		p = *req.Profile
		p.Managed = false
		p.SetupState, p.SetupError, p.LastCheckedAt = "", "", 0
		p.NodeID = req.NodeID
		if p.Port < 1 || p.Port > 65535 || net.ParseIP(p.Host) == nil || !sshReference.MatchString(p.Username) || p.Username == "root" || !sshReference.MatchString(p.CredentialRef) || !strings.HasPrefix(p.HostKeyFingerprint, "SHA256:") || len(p.HostKeyFingerprint) != 50 || (p.AuthType != "key" && p.AuthType != "certificate" && p.AuthType != "password" && p.AuthType != "ephemeralCertificate") {
			return nil, ErrInvalid
		}
		if _, err := s.sshCredential(p.CredentialRef); err != nil {
			return nil, err
		}
		p.Revision = req.ExpectedRevision + 1
		var err error
		if req.ExpectedRevision == 0 {
			err = db.Create(&p).Error
		} else {
			r := db.Model(&FleetSSHProfile{}).Where("node_id=? AND revision=?", req.NodeID, req.ExpectedRevision).Select("host", "port", "username", "auth_type", "credential_ref", "host_key_fingerprint", "enabled", "revision").Updates(&p)
			err = r.Error
			if err == nil && r.RowsAffected != 1 {
				err = repo.ErrConflict
			}
		}
		if err == nil {
			s.auditSSH(actor.UserID, req.NodeID, "SSHProfileSave", map[string]any{"revision": p.Revision, "host": p.Host, "username": p.Username, "enabled": p.Enabled})
		}
		return p, err
	}
	if op == "sshExecutionGet" || op == "sshExecutionCancel" {
		var row FleetSSHExecution
		if err := db.Where("id=? AND node_id=? AND actor_id=? AND api_key_id=?", req.ExecutionID, req.NodeID, actor.UserID, actor.APIKeyID).Take(&row).Error; err != nil {
			return nil, err
		}
		_, live := fleetSSHActive.Load(row.ID)
		if row.Status == "running" && (row.ProcessID != fleetSSHProcess || (!live && time.Now().UnixMilli()-row.CreatedAt > 10000)) {
			if err := db.Model(&row).Where("status='running'").Updates(map[string]any{"status": "unknown", "finished_at": time.Now().UnixMilli()}).Error; err != nil {
				return nil, err
			}
			row.Status = "unknown"
		}
		if op == "sshExecutionCancel" && row.Status == "running" {
			s.auditSSH(actor.UserID, req.NodeID, "SSHExecutionCancel", map[string]any{"executionId": row.ID, "remoteTerminationConfirmed": false})
			if cancel, ok := fleetSSHActive.Load(row.ID); ok {
				cancel.(context.CancelFunc)()
			}
			// A closed transport is not proof that remote descendants have exited.
			return map[string]any{"id": row.ID, "status": "cancelRequested", "remoteTerminationConfirmed": false}, nil
		}
		if req.Cursor < 0 || req.Cursor > len(row.Output) {
			return nil, ErrInvalid
		}
		end := req.Cursor + 1024*1024
		if end > len(row.Output) {
			end = len(row.Output)
		}
		return map[string]any{"execution": row, "output": row.Output[req.Cursor:end], "cursor": end, "remoteTerminationConfirmed": false, "commandExitConfirmed": row.ExitCode != nil}, nil
	}
	if err := db.Where("node_id=?", req.NodeID).Take(&p).Error; err != nil {
		return nil, err
	}
	if op == "sshProfileGet" {
		return p, nil
	}
	if !p.Enabled || p.Managed && p.SetupState != "ready" {
		return nil, ErrUnauthorized
	}
	if err := s.requireRemoteSSH(ctx, req.NodeID); err != nil {
		return nil, err
	}
	if op == "sshProfileTest" {
		client, err := s.sshConnect(ctx, p)
		if err != nil {
			return nil, err
		}
		client.Close()
		return map[string]any{"connected": true, "username": p.Username, "mode": "independentExec"}, nil
	}
	if op != "sshExecutionCreate" || req.Script == "" || len(req.Script) > 64*1024 || req.TimeoutSeconds < 1 || req.TimeoutSeconds > 600 {
		return nil, ErrInvalid
	}
	if _, err := uuid.Parse(req.ExecutionID); err != nil {
		return nil, ErrInvalid
	}
	var existing FleetSSHExecution
	if err := db.Where("id=?", req.ExecutionID).Take(&existing).Error; err == nil {
		if existing.NodeID != req.NodeID || existing.ActorID != actor.UserID || existing.APIKeyID != actor.APIKeyID || existing.Script != req.Script {
			return nil, repo.ErrConflict
		}
		return existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	select {
	case fleetSSHSlots <- struct{}{}:
	default:
		return nil, errors.New("SSH concurrency limit reached")
	}
	row := FleetSSHExecution{ID: req.ExecutionID, NodeID: req.NodeID, ActorID: actor.UserID, APIKeyID: actor.APIKeyID, ProcessID: fleetSSHProcess, Script: req.Script, Status: "running", CreatedAt: time.Now().UnixMilli()}
	if err := db.Create(&row).Error; err != nil {
		<-fleetSSHSlots
		return nil, err
	}
	s.auditSSH(actor.UserID, req.NodeID, "SSHExecutionCreate", map[string]any{"executionId": row.ID, "host": p.Host, "port": p.Port, "username": p.Username, "profileRevision": p.Revision})
	// Detach from HTTP cancellation, retaining the DB's installation scope.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(req.TimeoutSeconds)*time.Second)
	fleetSSHActive.Store(row.ID, context.CancelFunc(cancel))
	go s.runFleetSSH(runCtx, cancel, db.WithContext(context.WithoutCancel(ctx)), p, row)
	return row, nil
}

type sshOutput struct {
	mu        sync.Mutex
	text      []byte
	truncated bool
}

func (o *sshOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := len(p)
	remaining := 1024*1024 - len(o.text)
	if len(p) > remaining {
		p = p[:remaining]
		o.truncated = true
	}
	o.text = append(o.text, p...)
	return n, nil
}
func (o *sshOutput) snapshot() (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.ToValidUTF8(string(o.text), "�"), o.truncated
}
func (s *Service) runFleetSSH(ctx context.Context, cancel context.CancelFunc, db *gorm.DB, p FleetSSHProfile, row FleetSSHExecution) {
	defer s.watchFleetSSHAccess(ctx, row.NodeID, cancel)()
	defer func() { cancel(); fleetSSHActive.Delete(row.ID); <-fleetSSHSlots }()
	status := "failed"
	var exit *int
	output := &sshOutput{}
	defer func() {
		text, truncated := output.snapshot()
		s.auditSSH(row.ActorID, row.NodeID, "SSHExecutionFinish", map[string]any{"executionId": row.ID, "status": status, "exitCode": exit, "truncated": truncated})
		db.Model(&FleetSSHExecution{}).Where("id=?", row.ID).Updates(map[string]any{"status": status, "output": text, "truncated": truncated, "exit_code": exit, "finished_at": time.Now().UnixMilli()})
	}()
	client, err := s.sshConnect(ctx, p)
	if err != nil {
		io.WriteString(output, err.Error())
		return
	}
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	go func() {
		select {
		case <-s.closed:
			cancel()
		case <-ctx.Done():
		}
	}()
	session, err := client.NewSession()
	if err != nil {
		io.WriteString(output, "SSH session rejected")
		return
	}
	defer session.Close()
	session.Stdout = output
	session.Stderr = output
	// Once Start is attempted, a transport failure may leave a running command.
	status = "unknown"
	if err = session.Start(row.Script); err != nil {
		io.WriteString(output, "SSH command start not confirmed")
		return
	}
	done := make(chan error, 1)
	go func() { done <- session.Wait() }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			io.WriteString(output, "\nSSH interrupted; remote termination unconfirmed")
			return
		case <-ticker.C:
			text, truncated := output.snapshot()
			db.Model(&FleetSSHExecution{}).Where("id=? AND status='running'", row.ID).Updates(map[string]any{"output": text, "truncated": truncated})
		case err := <-done:
			if err == nil {
				code := 0
				exit = &code
				status = "succeeded"
			} else {
				var exited *ssh.ExitError
				if errors.As(err, &exited) {
					code := exited.ExitStatus()
					exit = &code
					status = "failed"
				} else {
					io.WriteString(output, "\nSSH connection lost; result unknown")
				}
			}
			return
		}
	}
}
