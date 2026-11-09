package ha

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"backend/fleet-agent/internal/config"
	"backend/fleet-agent/internal/state"
)

type generationStateStore struct {
	states []state.HASyncState
	events []state.HASyncEvent
}

func (store *generationStateStore) SaveHASyncState(_ context.Context, input state.HASyncState) error {
	store.states = append(store.states, input)
	return nil
}

func (store *generationStateStore) LoadHASyncStates(context.Context) ([]state.HASyncState, error) {
	return append([]state.HASyncState(nil), store.states...), nil
}

func (store *generationStateStore) SaveHASyncEvent(_ context.Context, input state.HASyncEvent) error {
	store.events = append(store.events, input)
	return nil
}

func (store *generationStateStore) LoadHASyncEvents(
	context.Context,
	state.HASyncEventQuery,
) (state.HASyncEventPage, error) {
	return state.HASyncEventPage{
		Items: append([]state.HASyncEvent(nil), store.events...),
		Total: int64(len(store.events)),
	}, nil
}

func TestIncomingOlderGenerationCannotOverwriteLiveFileSet(t *testing.T) {
	workDir := t.TempDir()
	liveDir := filepath.Join(t.TempDir(), "live")
	if err := os.MkdirAll(liveDir, 0o700); err != nil {
		t.Fatal(err)
	}
	liveFile := filepath.Join(liveDir, "value.txt")
	if err := os.WriteFile(liveFile, []byte("newer"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := &generationStateStore{states: []state.HASyncState{{
		Peer:     localSyncPeer,
		FileSet:  appliedStatePrefix + "documents",
		Revision: "10",
		Status:   syncStateSuccess,
	}}}
	fileSet := FileSet{Key: "documents", Source: liveDir, Required: true}
	manager := &SyncManager{
		cfg:      config.Config{HA: config.HAConfig{MemberID: "local-member"}},
		local:    store,
		fileSets: []FileSet{fileSet},
		workDir:  workDir,
	}
	revisionDir := filepath.Join(workDir, "incoming", "filesets", "old-member", "documents", "9")
	if err := os.MkdirAll(filepath.Join(revisionDir, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(revisionDir, "data", "value.txt"), []byte("older"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := inspectFileSetRoot(fileSet, filepath.Join(revisionDir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := manifestForTest(fileSet, inventory, 9, "old-member")
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(revisionDir, fileSetManifestFile), body, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := manager.applyIncomingFileSets(context.Background()); err != nil {
		t.Fatalf("apply older generation: %v", err)
	}
	current, err := os.ReadFile(liveFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "newer" {
		t.Fatalf("older generation overwrote live data: %q", current)
	}
	if _, err := os.Stat(revisionDir); !os.IsNotExist(err) {
		t.Fatalf("rejected staging directory was not removed: %v", err)
	}
}

func TestIncomingSnapshotsKeepOnlyHighestAppliedGeneration(t *testing.T) {
	workDir := t.TempDir()
	liveDir := filepath.Join(t.TempDir(), "live")
	if err := os.MkdirAll(liveDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store := &generationStateStore{states: []state.HASyncState{{
		Peer:     localSyncPeer,
		FileSet:  appliedStatePrefix + "documents",
		Revision: "10",
		Status:   syncStateSuccess,
	}}}
	fileSet := FileSet{Key: "documents", Source: liveDir, Required: true}
	manager := &SyncManager{
		cfg:      config.Config{HA: config.HAConfig{MemberID: "member-1"}},
		local:    store,
		fileSets: []FileSet{fileSet},
		workDir:  workDir,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	directories := make(map[uint64]string)
	for _, generation := range []uint64{8, 9, 10} {
		revision := strconv.FormatUint(generation, 10)
		revisionDir := filepath.Join(workDir, "incoming", "filesets", "member-1", "documents", revision)
		directories[generation] = revisionDir
		if err := os.MkdirAll(filepath.Join(revisionDir, "data"), 0o700); err != nil {
			t.Fatal(err)
		}
		inventory, err := inspectFileSetRoot(fileSet, filepath.Join(revisionDir, "data"))
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(manifestForTest(fileSet, inventory, generation, "member-1"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(revisionDir, fileSetManifestFile), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := manager.applyIncomingFileSets(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, generation := range []uint64{8, 9} {
		if _, err := os.Stat(directories[generation]); !os.IsNotExist(err) {
			t.Fatalf("obsolete generation %d was not removed: %v", generation, err)
		}
	}
	if _, err := os.Stat(directories[10]); err != nil {
		t.Fatalf("latest applied generation was not retained: %v", err)
	}
}

func manifestForTest(
	fileSet FileSet,
	inventory fileSetInventory,
	generation uint64,
	memberID string,
) fileSetManifest {
	return fileSetManifest{
		FileSet:          fileSet.Key,
		Generation:       generation,
		ParentGeneration: generation - 1,
		SnapshotID:       memberID + "-" + strconv.FormatUint(generation, 10),
		SourceMemberID:   memberID,
		ConfigDigest:     inventory.ConfigDigest,
		ContentRevision:  inventory.ContentRevision,
		FileCount:        inventory.FileCount,
		TotalBytes:       inventory.TotalBytes,
		NodeModulesFiles: inventory.NodeModulesFiles,
		NodeModulesBytes: inventory.NodeModulesBytes,
	}
}

func TestNextGenerationUsesLargestObservedOrAppliedGeneration(t *testing.T) {
	store := &generationStateStore{states: []state.HASyncState{
		{Peer: "192.0.2.10", FileSet: "documents", Revision: "7", Status: syncStateSuccess},
		{Peer: localSyncPeer, FileSet: appliedStatePrefix + "documents", Revision: "11", Status: syncStateSuccess},
	}}
	manager := &SyncManager{local: store}
	result, err := manager.nextFileSetGenerations(context.Background(), []FileSet{{Key: "documents"}})
	if err != nil {
		t.Fatal(err)
	}
	if result["documents"] != 12 {
		t.Fatalf("expected generation 12, got %d", result["documents"])
	}
}

func TestFileSetHeadReusesGenerationAndDetectsNodeModulesChanges(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "app", "node_modules", "package")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	moduleFile := filepath.Join(moduleDir, "index.js")
	if err := os.WriteFile(moduleFile, []byte("module-v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "ignored"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileSet := FileSet{
		Key:      "apps",
		Source:   root,
		Includes: []string{"**"},
		Excludes: []string{"**/.git/**"},
		Required: true,
	}
	inventory, err := inspectFileSet(fileSet)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.FileCount != 1 || inventory.NodeModulesFiles != 1 {
		t.Fatalf("unexpected app inventory: %+v", inventory)
	}
	states := []state.HASyncState{{
		Peer:             localSyncPeer,
		FileSet:          headStatePrefix + "apps",
		Revision:         "7",
		ConfigDigest:     inventory.ConfigDigest,
		ContentRevision:  inventory.ContentRevision,
		FileCount:        inventory.FileCount,
		TotalBytes:       inventory.TotalBytes,
		NodeModulesFiles: inventory.NodeModulesFiles,
		NodeModulesBytes: inventory.NodeModulesBytes,
		Status:           syncStateSuccess,
	}}
	manager := &SyncManager{}
	heads, err := manager.buildFileSetHeads(context.Background(), []FileSet{fileSet}, states)
	if err != nil {
		t.Fatal(err)
	}
	if heads[0].Generation != 7 {
		t.Fatalf("unchanged file set advanced generation to %d", heads[0].Generation)
	}
	if err := os.WriteFile(moduleFile, []byte("module-v2-with-more-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	heads, err = manager.buildFileSetHeads(context.Background(), []FileSet{fileSet}, states)
	if err != nil {
		t.Fatal(err)
	}
	if heads[0].Generation != 8 ||
		heads[0].Inventory.ContentRevision == inventory.ContentRevision {
		t.Fatalf("changed app file set did not advance its head: %+v", heads[0])
	}
}

func TestStalePendingGenerationIsSupersededBeforeOutboundSynchronization(t *testing.T) {
	workDir := t.TempDir()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("old-complete"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileSet := FileSet{Key: "apps", Source: root, Includes: []string{"**"}, Required: true}
	inventory, err := inspectFileSet(fileSet)
	if err != nil {
		t.Fatal(err)
	}
	head := state.HASyncState{
		Peer:             localSyncPeer,
		FileSet:          headStatePrefix + fileSet.Key,
		Revision:         "7",
		ConfigDigest:     inventory.ConfigDigest,
		ContentRevision:  inventory.ContentRevision,
		FileCount:        inventory.FileCount,
		TotalBytes:       inventory.TotalBytes,
		NodeModulesFiles: inventory.NodeModulesFiles,
		NodeModulesBytes: inventory.NodeModulesBytes,
		Status:           syncStateSuccess,
	}
	store := &generationStateStore{states: []state.HASyncState{head}}
	manager := &SyncManager{
		cfg: config.Config{HA: config.HAConfig{
			HealthFailureWindow: time.Second,
			Sync: config.HASyncConfig{
				Interval: time.Second,
			},
		}},
		local:    store,
		fileSets: []FileSet{fileSet},
		workDir:  workDir,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	pending := manifestForTest(fileSet, inventory, 8, "active-member")
	pending.ContentRevision = strings.Repeat("a", 64)
	pending.CreatedAt = time.Now().Add(-time.Minute).UnixMilli()
	revisionDir := filepath.Join(workDir, "incoming", "filesets", "active-member", "apps", "8")
	if err := os.MkdirAll(revisionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(revisionDir, fileSetPendingManifestFile),
		body,
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	incoming, err := manager.incomingFileSetStatus(fileSet, head)
	if err != nil {
		t.Fatal(err)
	}
	if incoming.Pending == nil || incoming.Pending.Generation != 8 {
		t.Fatalf("pending generation was not detected: %+v", incoming)
	}
	if manager.pendingManifestAge(*incoming.Pending) < manager.pendingManifestGrace() {
		t.Fatal("test pending generation did not exceed the acquisition grace window")
	}
	if err := manager.outboundReady(context.Background()); err != nil {
		t.Fatalf("stale pending generation was not superseded: %v", err)
	}
	latest, found := latestLocalFileSetHead(store.states, fileSet.Key)
	if !found || latest.Revision != "9" ||
		latest.ContentRevision != inventory.ContentRevision {
		t.Fatalf("unexpected superseding synchronized head: %+v", latest)
	}
}

func TestSuccessfulPreviousSnapshotRequiresExactPeerGeneration(t *testing.T) {
	states := []state.HASyncState{{
		Peer:     "192.0.2.10",
		FileSet:  "documents",
		Revision: "7",
		Status:   syncStateSuccess,
	}}
	if !hasSuccessfulPreviousSnapshot(states, "192.0.2.10", "documents", 8) {
		t.Fatal("successful previous generation was not selected as the rsync baseline")
	}
	if hasSuccessfulPreviousSnapshot(states, "192.0.2.11", "documents", 8) {
		t.Fatal("snapshot from another peer was selected as the rsync baseline")
	}
	if hasSuccessfulPreviousSnapshot(states, "192.0.2.10", "documents", 9) {
		t.Fatal("non-adjacent snapshot was selected as the rsync baseline")
	}
}

func TestPeriodicSyncHistoryIsThrottledButStateChangesAreRecorded(t *testing.T) {
	store := &generationStateStore{}
	manager := &SyncManager{
		local:           store,
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		lastSyncStatus:  make(map[string]string),
		lastHistoryTime: make(map[string]int64),
	}
	record := syncRecord{
		OperationID: "operation-1",
		Peer:        "192.0.2.10",
		FileSet:     "documents",
		Revision:    "1",
		Direction:   syncDirectionOutbound,
		TriggerType: syncTriggerPeriodic,
		StartedAt:   1,
	}
	manager.saveSyncState(context.Background(), record)
	record.OperationID = "operation-2"
	record.Revision = "2"
	manager.saveSyncState(context.Background(), record)
	if len(store.events) != 1 {
		t.Fatalf("unchanged periodic status should be throttled, got %d events", len(store.events))
	}
	record.OperationID = "operation-3"
	record.Revision = "3"
	record.Err = context.Canceled
	manager.saveSyncState(context.Background(), record)
	if len(store.events) != 2 || store.events[1].Status != syncStateCanceled {
		t.Fatalf("sync status transition was not recorded: %+v", store.events)
	}
	record.OperationID = "operation-4"
	record.Revision = "4"
	record.Err = nil
	manager.saveSyncState(context.Background(), record)
	if len(store.events) != 3 || store.events[2].Status != syncStateSuccess {
		t.Fatalf("sync recovery was not recorded: %+v", store.events)
	}
}

func TestSQLiteSyncHistoryRoundTrip(t *testing.T) {
	database, err := state.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now().UTC().UnixMilli()
	if err := database.SaveHASyncEvent(context.Background(), state.HASyncEvent{
		OperationID: "operation-1",
		Peer:        "192.0.2.10",
		FileSet:     "license",
		Revision:    "1",
		Direction:   syncDirectionOutbound,
		TriggerType: syncTriggerLicense,
		Status:      syncStateSuccess,
		StartedAt:   now,
		FinishedAt:  now + 1,
	}); err != nil {
		t.Fatal(err)
	}
	page, err := database.LoadHASyncEvents(context.Background(), state.HASyncEventQuery{
		Peer:    "192.0.2.10",
		FileSet: "license",
		Limit:   20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].OperationID != "operation-1" {
		t.Fatalf("unexpected sync history page: %+v", page)
	}
}

func TestIncomingApplyKeepsFileSetExclusions(t *testing.T) {
	args := appendFileSetFilters(
		[]string{"-a", "--delete-delay"},
		[]string{"flows*.json", "lib/**"},
		[]string{"node_modules/**", ".npm/**"},
	)
	joined := strings.Join(args, "\n")
	for _, expected := range []string{
		"--exclude=node_modules/**",
		"--exclude=.npm/**",
		"--include=flows*.json",
		"--include=lib/**",
		"--exclude=*",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing rsync filter %q in %v", expected, args)
		}
	}
}
