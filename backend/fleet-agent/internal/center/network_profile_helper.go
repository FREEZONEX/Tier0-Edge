package center

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/fleet-agent/internal/config"
	"backend/internal/fleetprotocol"

	"github.com/google/uuid"
)

func newProfileRequest(action, old, target, domain string) fleetprotocol.NetworkProfileRequest {
	return fleetprotocol.NetworkProfileRequest{
		RequestID: strings.ReplaceAll(uuid.NewString(), "-", ""), Action: action,
		ExpectedProfile: old, TargetProfile: target, StateDomain: domain, CreatedAt: time.Now().UnixMilli(),
	}
}

// Caller holds networkOperation across prepare/login/finish. Root's journal
// retains the old profile and rolls back abandoned preparations after its lease.
func requestProfileHandoff(ctx context.Context, cfg config.Config, req fleetprotocol.NetworkProfileRequest) (fleetprotocol.NetworkProfileResult, error) {
	var result fleetprotocol.NetworkProfileResult
	if cfg.NetworkOperator == "" {
		return result, errors.New("FLEET_NETWORK_OPERATOR_UNCONFIGURED")
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return result, err
	}
	file, err := os.CreateTemp(cfg.DataDir, ".network-profile-*")
	if err != nil {
		return result, err
	}
	defer os.Remove(file.Name())
	err = json.NewEncoder(file).Encode(req)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return result, errors.New("FLEET_NETWORK_PROFILE_REQUEST_WRITE_FAILED")
	}
	if err = os.Rename(file.Name(), filepath.Join(cfg.DataDir, "network-profile-request.json")); err != nil {
		return result, err
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	wait, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for {
		raw, readErr := os.ReadFile(filepath.Join(cfg.DataDir, "network-profile-result.json"))
		if readErr == nil && json.Unmarshal(raw, &result) == nil && result.RequestID == req.RequestID && result.Action == req.Action {
			if result.ErrorCode != "" {
				return result, errors.New(result.ErrorCode)
			}
			if req.Action != "inspect" && req.Action != "rollback" && result.StateDomain != req.StateDomain {
				return result, errors.New("FLEET_NETWORK_STATE_DOMAIN_CHANGED")
			}
			if result.Operator != cfg.NetworkOperator {
				return result, errors.New("FLEET_NETWORK_OPERATOR_MISMATCH")
			}
			expected := "ready"
			if req.Action == "commit" {
				expected = "committed"
			}
			if req.Action == "rollback" {
				expected = "rolledBack"
			}
			if result.Status != expected {
				return result, errors.New("FLEET_NETWORK_PROFILE_RESULT_INVALID")
			}
			return result, nil
		}
		select {
		case <-wait.Done():
			return result, errors.New("FLEET_NETWORK_PROFILE_HELPER_UNAVAILABLE")
		case <-ticker.C:
		}
	}
}
