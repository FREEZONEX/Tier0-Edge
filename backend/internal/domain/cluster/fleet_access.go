package cluster

import (
	"backend/internal/domain/license"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsv2"
	"backend/internal/infra/privatefs"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var fleetAccessMu sync.Mutex
var ErrFleetSSHNotAllowed = errors.New("EDGE_SSH_DISABLED")
var ErrFleetControlNotAllowed = sourcestream.ErrRemoteControlDisabled
var ErrFleetMetadataNotAllowed = errors.New("EDGE_METADATA_READ_ONLY")

type FleetAccess struct {
	AllowControl       bool  `json:"allowControl"`
	AllowSSH           bool  `json:"allowSSH"`
	AllowMetadataWrite bool  `json:"allowMetadataWrite"`
	Revision           int64 `json:"revision"`
}
type FleetAccessRequest struct {
	AllowControl       *bool `json:"allowControl,omitempty"`
	ExpectedRevision   int64 `json:"expectedRevision"`
	AllowSSH           bool  `json:"allowSSH"`
	AllowMetadataWrite bool  `json:"allowMetadataWrite"`
}

func (s *Service) readFleetAccess() (FleetAccess, error) {
	// Keep legacy SSH/metadata defaults; remote Control remains denied for new
	// installations and old configuration files that omit allowControl.
	value := FleetAccess{AllowSSH: true, AllowMetadataWrite: true}
	raw, err := os.ReadFile(filepath.Join(s.cfg.IdentityDir, "remote-access.json"))
	if os.IsNotExist(err) {
		return value, nil
	}
	if err != nil {
		return FleetAccess{}, err
	}
	if err = json.Unmarshal(raw, &value); err != nil {
		return FleetAccess{}, err
	}
	return value, nil
}
func (s *Service) FleetAccess() (FleetAccess, error) {
	fleetAccessMu.Lock()
	defer fleetAccessMu.Unlock()
	return s.readFleetAccess()
}
func (s *Service) SaveFleetAccess(ctx context.Context, req FleetAccessRequest) (FleetAccess, error) {
	if s.cfg.IdentityDir == "" {
		return FleetAccess{}, ErrInvalid
	}
	if license.Default().IsHost() {
		return FleetAccess{}, ErrHostCannotConnect
	}
	fleetAccessMu.Lock()
	defer fleetAccessMu.Unlock()
	old, err := s.readFleetAccess()
	if err != nil {
		return old, err
	}
	if old.Revision != req.ExpectedRevision {
		return old, &FleetRevisionConflict{CurrentRevision: old.Revision}
	}
	next := FleetAccess{AllowSSH: req.AllowSSH, AllowMetadataWrite: req.AllowMetadataWrite, AllowControl: old.AllowControl, Revision: old.Revision + 1}
	if req.AllowControl != nil {
		next.AllowControl = *req.AllowControl
	}
	raw, _ := json.Marshal(next)
	if err = os.MkdirAll(s.cfg.IdentityDir, 0700); err != nil {
		return old, err
	}
	st, err := os.Lstat(s.cfg.IdentityDir)
	if err != nil || !st.IsDir() || !privatefs.IsPrivate(s.cfg.IdentityDir, st) {
		return old, ErrInvalid
	}
	f, err := os.CreateTemp(s.cfg.IdentityDir, ".remote-access-*")
	if err != nil {
		return old, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return old, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return old, err
	}
	f.Close()
	if err = os.Rename(f.Name(), filepath.Join(s.cfg.IdentityDir, "remote-access.json")); err != nil {
		return old, err
	}
	return next, nil
}
func (s *Service) CheckFleetMetadataAccess(operation string) error {
	op, ok := unsv2.FindResourceOperation(operation)
	if !ok {
		return nil
	} // ExecuteFleetRuntime separately validates its allowlist.
	if op.Mutation == "" {
		return nil
	}
	// Record/Sample data writes and remote Control keep their own authorization.
	if op.Resource == "record" || op.Resource == "sample" {
		return nil
	}
	access, err := s.FleetAccess()
	if err != nil {
		return err
	}
	if !access.AllowMetadataWrite {
		return ErrFleetMetadataNotAllowed
	}
	return nil
}
func (s *Service) requireRemoteSSH(ctx context.Context, nodeID string) error {
	raw, err := s.CallFleetNode(ctx, nodeID, "capabilities.get", "node", struct{}{})
	if err != nil {
		return err
	}
	var view struct {
		Access *FleetAccess `json:"access"`
	}
	if json.Unmarshal(raw, &view) != nil || view.Access == nil || !view.Access.AllowSSH {
		closeNodeTerminals(nodeID)
		return ErrFleetSSHNotAllowed
	}
	return nil
}

// Recheck unattended SSH sessions too; a disconnected browser must not keep a
// revoked platform session alive. Closing SSH does not prove descendants exited.
func (s *Service) watchFleetSSHAccess(ctx context.Context, nodeID string, cancel context.CancelFunc) func() {
	watchCtx, stop := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				// A relayed overlay handshake can exceed three seconds. A missing
				// authorization still closes the session; never reuse a stale grant.
				probeCtx, done := context.WithTimeout(watchCtx, 10*time.Second)
				err := s.requireRemoteSSH(probeCtx, nodeID)
				done()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return stop
}

// 只用于远程新调用的准入；不影响已接收动作的执行及结果查询。
func (s *Service) CheckFleetControlAccess() error {
	access, err := s.FleetAccess()
	if err != nil {
		return err
	}
	if !access.AllowControl {
		return ErrFleetControlNotAllowed
	}
	return nil
}
func (s *Service) requireRemoteControl(ctx context.Context, nodeID string) error {
	raw, err := s.CallFleetNode(ctx, nodeID, "capabilities.get", "node", struct{}{})
	if err != nil {
		return err
	}
	var view struct {
		Access *FleetAccess `json:"access"`
	}
	if json.Unmarshal(raw, &view) != nil || view.Access == nil || !view.Access.AllowControl {
		return ErrFleetControlNotAllowed
	}
	return nil
}
