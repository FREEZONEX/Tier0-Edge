package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/license"
	"backend/internal/repo"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

type FleetTerminalRequest struct {
	NodeID      string `json:"nodeId"`
	ContainerID string `json:"containerId,omitempty"`
	SessionID   string `json:"sessionId"`
	Input       string `json:"input"`
	Sequence    int64  `json:"sequence"`
	Cursor      int64  `json:"cursor"`
	Rows        int    `json:"rows"`
	Cols        int    `json:"cols"`
}
type fleetTerminalRecord struct {
	ID          string `gorm:"primaryKey" json:"sessionId"`
	NodeID      string `json:"nodeId"`
	ContainerID string `json:"containerId,omitempty"`
	Username    string `json:"username"`
	ActorID     int64  `json:"-"`
	APIKeyID    int64  `json:"-"`
	ProcessID   string `json:"-"`
	State       string `json:"state"`
	ErrorCode   string `json:"errorCode,omitempty"`
	Output      []byte `json:"-"`
	Offset      int64  `gorm:"column:output_offset" json:"offset"`
	Sequence    int64  `json:"sequence"`
	CreatedAt   int64  `json:"createdAt"`
	ExpiresAt   int64  `json:"expiresAt"`
}

func (fleetTerminalRecord) TableName() string { return "fleet_ssh_terminal" }

type fleetTerminal struct {
	mu        sync.Mutex
	inputMu   sync.Mutex
	row       fleetTerminalRecord
	inputHash [32]byte
	session   *ssh.Session
	input     io.WriteCloser
	cancel    context.CancelFunc
	audit     terminalInputAudit
}

var fleetTerminals sync.Map

const terminalOutputLimit = 1024 * 1024

var terminalContainerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (t *fleetTerminal) Write(data []byte) (int, error) {
	t.audit.Output(data)
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(data)
	t.row.Output = append(t.row.Output, data...)
	if extra := len(t.row.Output) - terminalOutputLimit; extra > 0 {
		t.row.Output = append([]byte(nil), t.row.Output[extra:]...)
		t.row.Offset += int64(extra)
	}
	return n, nil
}
func (t *fleetTerminal) persist(db *gorm.DB) {
	t.mu.Lock()
	row := t.row
	row.Output = append([]byte(nil), row.Output...)
	t.mu.Unlock()
	db.Model(&fleetTerminalRecord{}).Where("id=?", row.ID).Updates(map[string]any{"state": row.State, "error_code": row.ErrorCode, "output": row.Output, "output_offset": row.Offset, "sequence": row.Sequence})
}
func terminalView(row fleetTerminalRecord, cursor int64) (any, error) {
	if cursor < 0 || cursor > row.Offset+int64(len(row.Output)) {
		return nil, ErrInvalid
	}
	reset := cursor < row.Offset
	if reset {
		cursor = row.Offset
	}
	return map[string]any{"sessionId": row.ID, "username": row.Username, "state": row.State, "errorCode": row.ErrorCode, "data": base64.StdEncoding.EncodeToString(row.Output[cursor-row.Offset:]), "cursor": row.Offset + int64(len(row.Output)), "reset": reset, "sequence": row.Sequence, "expiresAt": row.ExpiresAt, "remoteTerminationConfirmed": false}, nil
}
func (s *Service) FleetTerminal(ctx context.Context, op string, req FleetTerminalRequest) (any, error) {
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok || !license.Default().IsHost() || !contextx.HasResource(actor, "fleet.ssh.manage") || actor.APIKeyAppID != "" {
		return nil, ErrUnauthorized
	}
	if _, err := s.fleetRepo.CollaborationNode(ctx, req.NodeID); err != nil {
		return nil, err
	}
	// Session creation checks the remote grant synchronously. Active sessions
	// use watchFleetSSHAccess for bounded remote revocation checks; keep those
	// network round trips out of each key and output poll. Local checks remain below.
	if op == "sshTerminalCreate" {
		if err := s.requireRemoteSSH(ctx, req.NodeID); err != nil {
			if errors.Is(err, ErrFleetPeerUnavailable) {
				return nil, sshFailure("SSH_ACCESS_UNAVAILABLE")
			}
			return nil, err
		}
	}
	db := repo.GetCommonConn(ctx)
	if op == "sshTerminalCreate" {
		if _, err := uuid.Parse(req.SessionID); err != nil {
			return nil, ErrInvalid
		}
		if req.Rows < 2 || req.Rows > 250 || req.Cols < 10 || req.Cols > 500 {
			return nil, ErrInvalid
		}
		var prior fleetTerminalRecord
		if err := db.Where("id=?", req.SessionID).Take(&prior).Error; err == nil {
			if prior.ActorID != actor.UserID || prior.APIKeyID != actor.APIKeyID || prior.NodeID != req.NodeID || prior.ContainerID != req.ContainerID {
				return nil, ErrUnauthorized
			}
			return map[string]any{"sessionId": prior.ID, "username": prior.Username, "state": prior.State}, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if req.ContainerID != "" {
			// The immutable full Docker ID is the only command argument. Resolve it
			// through the existing Agent ownership check, never from a display name.
			if !terminalContainerID.MatchString(req.ContainerID) {
				return nil, ErrInvalid
			}
			detail, err := s.FleetNodeContainer(ctx, req.NodeID, req.ContainerID)
			if err != nil {
				return nil, err
			}
			if detail["stale"] != false || detail["state"] != "running" || detail["containerID"] != req.ContainerID {
				return nil, &FleetAgentResponseError{StatusCode: http.StatusConflict, Code: "FLEET_CONTAINER_NOT_RUNNING"}
			}
		}
		var profile FleetSSHProfile
		if err := db.Where("node_id=? AND enabled=true", req.NodeID).Take(&profile).Error; err != nil {
			return nil, err
		}
		if profile.Managed && profile.SetupState != "ready" {
			return nil, sshFailure(profile.SetupError)
		}
		select {
		case fleetSSHSlots <- struct{}{}:
		default:
			return nil, errors.New("SSH concurrency limit reached")
		}
		now := time.Now()
		row := fleetTerminalRecord{ID: req.SessionID, NodeID: req.NodeID, ContainerID: req.ContainerID, Username: profile.Username, ActorID: actor.UserID, APIKeyID: actor.APIKeyID, ProcessID: fleetSSHProcess, State: "connecting", Output: []byte{}, CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(30 * time.Minute).UnixMilli()}
		if err := db.Create(&row).Error; err != nil {
			<-fleetSSHSlots
			return nil, err
		}
		runCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), time.UnixMilli(row.ExpiresAt))
		terminal := &fleetTerminal{row: row, cancel: cancel}
		fleetTerminals.Store(row.ID, terminal)
		s.auditSSH(actor.UserID, req.NodeID, "SSHTerminalCreate", map[string]any{"sessionId": row.ID, "containerId": row.ContainerID, "username": profile.Username, "host": profile.Host, "profileRevision": profile.Revision})
		go s.runFleetTerminal(runCtx, db.WithContext(context.WithoutCancel(ctx)), terminal, profile, req.Rows, req.Cols)
		return map[string]any{"sessionId": row.ID, "username": row.Username, "state": row.State}, nil
	}
	var row fleetTerminalRecord
	if err := db.Where("id=? AND node_id=? AND actor_id=? AND api_key_id=?", req.SessionID, req.NodeID, actor.UserID, actor.APIKeyID).Take(&row).Error; err != nil {
		return nil, err
	}
	value, live := fleetTerminals.Load(row.ID)
	if !live {
		if (row.State == "connecting" || row.State == "running") && (row.ProcessID != fleetSSHProcess || time.Now().UnixMilli()-row.CreatedAt > 10000) {
			row.State = "unknown"
			if err := db.Model(&row).Update("state", row.State).Error; err != nil {
				return nil, err
			}
		}
		if op == "sshTerminalRead" {
			return terminalView(row, req.Cursor)
		}
		if op == "sshTerminalClose" {
			// Closing a retained session acknowledges its state, not its output history.
			return terminalView(row, row.Offset+int64(len(row.Output)))
		}
		return nil, errors.New("SSH terminal is no longer active")
	}
	terminal := value.(*fleetTerminal)
	if op == "sshTerminalRead" {
		terminal.mu.Lock()
		defer terminal.mu.Unlock()
		return terminalView(terminal.row, req.Cursor)
	}
	if op == "sshTerminalClose" {
		terminal.cancel()
		s.auditSSH(actor.UserID, req.NodeID, "SSHTerminalClose", map[string]any{"sessionId": row.ID})
		return map[string]any{"sessionId": row.ID, "state": "closing", "remoteTerminationConfirmed": false}, nil
	}
	terminal.inputMu.Lock()
	defer terminal.inputMu.Unlock()
	terminal.mu.Lock()
	session, input, state := terminal.session, terminal.input, terminal.row.State
	terminal.mu.Unlock()
	if state != "running" || session == nil || input == nil {
		return nil, errors.New("SSH terminal is not ready")
	}
	// Recheck profile revocation on every control/input call.
	var enabled int64
	if err := db.Model(&FleetSSHProfile{}).Where("node_id=? AND enabled=true", req.NodeID).Count(&enabled).Error; err != nil {
		return nil, err
	}
	if enabled != 1 {
		terminal.cancel()
		return nil, ErrUnauthorized
	}
	stopInput := context.AfterFunc(ctx, terminal.cancel)
	defer stopInput()
	if op == "sshTerminalResize" {
		if req.Rows < 2 || req.Rows > 250 || req.Cols < 10 || req.Cols > 500 {
			return nil, ErrInvalid
		}
		if err := session.WindowChange(req.Rows, req.Cols); err != nil {
			terminal.cancel()
			return nil, errors.New("SSH terminal resize unconfirmed")
		}
		return map[string]any{"sessionId": row.ID}, nil
	}
	if op != "sshTerminalWrite" || len(req.Input) == 0 || len(req.Input) > 16*1024 {
		return nil, ErrInvalid
	}
	hash := sha256.Sum256([]byte(req.Input))
	terminal.mu.Lock()
	if req.Sequence == terminal.row.Sequence && hash == terminal.inputHash {
		terminal.mu.Unlock()
		return map[string]any{"sequence": req.Sequence}, nil
	}
	if req.Sequence != terminal.row.Sequence+1 {
		terminal.mu.Unlock()
		return nil, repo.ErrConflict
	}
	// Record the attempt before touching the remote stream. Never replay uncertain input.
	terminal.row.Sequence = req.Sequence
	terminal.inputHash = hash
	terminal.mu.Unlock()
	if err := db.Model(&row).Update("sequence", req.Sequence).Error; err != nil {
		terminal.cancel()
		return nil, err
	}
	terminal.audit.Input([]byte(req.Input))
	if _, err := io.WriteString(input, req.Input); err != nil {
		terminal.cancel()
		return nil, errors.New("SSH terminal input result unknown")
	}
	return map[string]any{"sequence": req.Sequence}, nil
}
func (s *Service) runFleetTerminal(ctx context.Context, db *gorm.DB, t *fleetTerminal, p FleetSSHProfile, rows, cols int) {
	defer s.watchFleetSSHAccess(ctx, p.NodeID, t.cancel)()
	state := "failed"
	defer func() {
		t.cancel()
		t.mu.Lock()
		t.row.State = state
		t.mu.Unlock()
		t.persist(db)
		fleetTerminals.Delete(t.row.ID)
		<-fleetSSHSlots
		s.auditSSH(t.row.ActorID, t.row.NodeID, "SSHTerminalFinish", map[string]any{
			"sessionId": t.row.ID, "state": state, "containerId": t.row.ContainerID,
			"username": t.row.Username, "startedAt": t.row.CreatedAt,
			"finishedAt": time.Now().UnixMilli(), "inputSummary": t.audit.Finish(),
		})
	}()
	client, err := s.sshConnect(ctx, p)
	if err != nil {
		t.mu.Lock()
		t.row.ErrorCode = sshFailureCode(err)
		t.mu.Unlock()
		io.WriteString(t, sshFailureCode(err))
		return
	}
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { client.Close() })
	defer stop()
	go func() {
		select {
		case <-ctx.Done():
		case <-s.closed:
			t.cancel()
		}
	}()
	session, err := client.NewSession()
	if err != nil {
		return
	}
	defer session.Close()
	input, err := session.StdinPipe()
	if err != nil {
		return
	}
	session.Stdout = t
	session.Stderr = t
	if err = session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}); err != nil {
		return
	}
	state = "unknown"
	if t.row.ContainerID != "" {
		// Keep exec in the authorized SSH session; the Fleet Agent does not gain
		// a shell endpoint. Images without /bin/sh fail explicitly in the terminal.
		err = session.Start("docker exec -it -- " + t.row.ContainerID + " /bin/sh")
	} else {
		err = session.Shell()
	}
	if err != nil {
		io.WriteString(t, err.Error())
		return
	}
	t.mu.Lock()
	t.session = session
	t.input = input
	t.row.State = "running"
	t.mu.Unlock()
	t.persist(db)
	done := make(chan error, 1)
	go func() { done <- session.Wait() }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-done:
			if err == nil {
				state = "closed"
			} else {
				var exit *ssh.ExitError
				if errors.As(err, &exit) {
					state = "closed"
				}
			}
			return
		case <-ticker.C:
			t.persist(db)
		}
	}
}
