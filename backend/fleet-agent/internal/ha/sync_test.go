package ha

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"backend/fleet-agent/internal/config"
	"backend/fleet-agent/internal/state"
)

func TestLoadFileSetsRejectsUnsafeRemotePath(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "filesets.json")
	body, err := json.Marshal(FileSetConfig{FileSets: []FileSet{{
		Key:      "sourceflow",
		Source:   "/var/lib/tier0 data/sourceflow",
		Required: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFileSets(filename); err == nil {
		t.Fatal("unsafe remote path was accepted")
	}
}

func TestRsyncArgsPreserveExcludesAndUseStrictHostChecking(t *testing.T) {
	manager := &SyncManager{cfg: config.Config{HA: config.HAConfig{Sync: config.HASyncConfig{
		SSHUser:        "tier0-sync",
		SSHPort:        22,
		SSHPrivateKey:  "/var/lib/tier0-fleet-agent/rsync_ed25519",
		KnownHostsFile: "/var/lib/tier0-fleet-agent/known_hosts",
		Timeout:        time.Minute,
	}}}}
	args := manager.rsyncArgs(
		"192.0.2.11",
		"/var/lib/tier0/apps",
		"/var/lib/tier0/apps",
		[]string{"**"},
		[]string{"**/node_modules/**"},
		"/var/lib/tier0/ha-sync/incoming/filesets/member/apps/10/data",
	)
	if !slices.Contains(args, "--exclude=**/node_modules/**") {
		t.Fatalf("rsync excludes were not preserved: %v", args)
	}
	if !slices.Contains(args, "--delete-delay") {
		t.Fatalf("rsync deletion reconciliation is missing: %v", args)
	}
	if !slices.Contains(args, "--no-owner") || !slices.Contains(args, "--no-group") {
		t.Fatalf("rsync must not require remote root ownership: %v", args)
	}
	if !slices.Contains(args, "--link-dest=/var/lib/tier0/ha-sync/incoming/filesets/member/apps/10/data") {
		t.Fatalf("rsync incremental snapshot baseline is missing: %v", args)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "StrictHostKeyChecking=yes") ||
		!strings.Contains(joined, "tier0-sync@192.0.2.11:/var/lib/tier0/apps") {
		t.Fatalf("rsync SSH transport is not strict or target is wrong: %s", joined)
	}
}

func TestSafeRemotePath(t *testing.T) {
	for _, value := range []string{"/var/lib/tier0/apps", "/data/tier0-1/source_flow"} {
		if !isSafeRemotePath(value) {
			t.Fatalf("safe path was rejected: %s", value)
		}
	}
	for _, value := range []string{"relative/path", "/data/with space", "/data/$(touch-x)"} {
		if isSafeRemotePath(value) {
			t.Fatalf("unsafe path was accepted: %s", value)
		}
	}
}

func TestRestoreSyncStatusKeepsPersistedFailureVisible(t *testing.T) {
	status := SyncStatus{Enabled: true, State: syncStateIdle}
	restoreSyncStatus(&status, []state.HASyncState{
		{Peer: "192.0.2.11", FileSet: "license", Status: syncStateSuccess, LastStartedAt: 100, LastSucceededAt: 110},
		{Peer: "192.0.2.12", FileSet: "license", Status: syncStateError, LastStartedAt: 120, ErrorMessage: "connection failed"},
	})
	if status.State != syncStateError || status.LastError != "connection failed" {
		t.Fatalf("restored status = %+v", status)
	}
	if status.LastStartedAt != 120 || status.LastSucceededAt != 110 {
		t.Fatalf("restored timestamps = %+v", status)
	}
}
