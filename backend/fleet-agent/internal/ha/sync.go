package ha

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/fleet-agent/internal/config"
	"backend/fleet-agent/internal/state"
	"backend/internal/fleetprotocol"

	"github.com/google/uuid"
)

const (
	syncStateIdle     = "idle"
	syncStateRunning  = "running"
	syncStateError    = "error"
	syncStateCanceled = "canceled"
	syncStateSuccess  = "succeeded"

	localSyncPeer              = "local"
	appExportStateKey          = "app-export"
	appIncomingStateKey        = "app-incoming"
	appRuntimeKey              = "app-runtime"
	appRevisionFile            = ".tier0-revision"
	fileSetManifestFile        = ".tier0-sync-manifest.json"
	fileSetPendingManifestFile = ".tier0-sync-pending.json"
	appliedStatePrefix         = "applied:"
	headStatePrefix            = "head:"

	syncDirectionOutbound = "outbound"
	syncDirectionInbound  = "inbound"
	syncTriggerPeriodic   = "periodic"
	syncTriggerFull       = "full"
	syncTriggerPromotion  = "promotion"
	syncTriggerManual     = "manual"
	syncTriggerLicense    = "license"
	syncTriggerIncoming   = "incoming"

	syncHistoryCheckpointInterval = 5 * time.Minute
)

var errInitialSyncBootstrapRequired = errors.New("Fleet HA initial synchronized head bootstrap is required")

type FileSet struct {
	Key      string   `json:"key"`
	Source   string   `json:"source"`
	Includes []string `json:"includes"`
	Excludes []string `json:"excludes"`
	Required bool     `json:"required"`
}

type FileSetConfig struct {
	FileSets []FileSet `json:"fileSets"`
}

type fileSetManifest struct {
	FileSet          string `json:"fileSet"`
	Generation       uint64 `json:"generation"`
	ParentGeneration uint64 `json:"parentGeneration"`
	SnapshotID       string `json:"snapshotId"`
	SourceMemberID   string `json:"sourceMemberId"`
	ConfigDigest     string `json:"configDigest"`
	ContentRevision  string `json:"contentRevision"`
	FileCount        int64  `json:"fileCount"`
	TotalBytes       int64  `json:"totalBytes"`
	NodeModulesFiles int64  `json:"nodeModulesFiles,omitempty"`
	NodeModulesBytes int64  `json:"nodeModulesBytes,omitempty"`
	CreatedAt        int64  `json:"createdAt"`
}

type fileSetCandidate struct {
	filename string
	manifest fileSetManifest
}

type incomingFileSetStatus struct {
	CommittedNewer bool
	Pending        *fileSetManifest
}

type fileSetInventory struct {
	ConfigDigest     string
	ContentRevision  string
	FileCount        int64
	TotalBytes       int64
	NodeModulesFiles int64
	NodeModulesBytes int64
}

type fileSetHead struct {
	FileSet          FileSet
	Generation       uint64
	ParentGeneration uint64
	Inventory        fileSetInventory
}

type syncRecord struct {
	OperationID      string
	Peer             string
	FileSet          string
	Revision         string
	ConfigDigest     string
	ContentRevision  string
	FileCount        int64
	TotalBytes       int64
	NodeModulesFiles int64
	NodeModulesBytes int64
	Direction        string
	TriggerType      string
	StartedAt        int64
	Err              error
}

type SyncStateStore interface {
	SaveHASyncState(context.Context, state.HASyncState) error
	LoadHASyncStates(context.Context) ([]state.HASyncState, error)
	SaveHASyncEvent(context.Context, state.HASyncEvent) error
	LoadHASyncEvents(context.Context, state.HASyncEventQuery) (state.HASyncEventPage, error)
}

type HAAppStore interface {
	HAAppSignature(context.Context, string) (string, error)
	ExportHAApps(context.Context, string, string) (string, error)
	ImportHAApps(context.Context, string, string) error
}

type SyncManager struct {
	cfg      config.Config
	local    SyncStateStore
	apps     HAAppStore
	fileSets []FileSet
	workDir  string
	logger   *slog.Logger

	syncMu   sync.Mutex
	cancelMu sync.Mutex
	cancel   context.CancelFunc
	mu       sync.RWMutex
	status   SyncStatus
	trigger  chan struct{}
	requests chan string
	outbound atomic.Bool

	historyMu       sync.Mutex
	lastSyncStatus  map[string]string
	lastHistoryTime map[string]int64
}

func NewSyncManager(
	cfg config.Config,
	local SyncStateStore,
	apps HAAppStore,
	logger *slog.Logger,
) (*SyncManager, error) {
	if !cfg.HA.Sync.Enabled {
		return nil, errors.New("Fleet HA rsync synchronization is not enabled")
	}
	if local == nil || apps == nil {
		return nil, errors.New("Fleet HA rsync dependencies are incomplete")
	}
	fileSets, err := loadFileSets(cfg.HA.Sync.FileSetsFile)
	if err != nil {
		return nil, err
	}
	workDir := strings.TrimSpace(cfg.HA.Sync.WorkDir)
	if workDir == "" {
		workDir = filepath.Join(cfg.DataDir, "ha-sync")
	}
	if !isSafeRemotePath(workDir) {
		return nil, errors.New("Fleet HA rsync work directory must be an absolute path without spaces or shell metacharacters")
	}
	if err := os.MkdirAll(filepath.Join(workDir, "incoming"), 0o700); err != nil {
		return nil, fmt.Errorf("create Fleet HA rsync work directory: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	syncStatus := SyncStatus{
		Enabled: true,
		Peers:   append([]string(nil), cfg.HA.Sync.Peers...),
		State:   syncStateIdle,
	}
	lastSyncStatus := make(map[string]string)
	if states, loadErr := local.LoadHASyncStates(context.Background()); loadErr != nil {
		logger.Warn("load persisted Fleet HA rsync state failed", "error", loadErr)
	} else {
		restoreSyncStatus(&syncStatus, states)
		for _, item := range states {
			key := syncStateKey(item.Peer, item.FileSet)
			lastSyncStatus[key] = item.Status
		}
	}
	return &SyncManager{
		cfg:             cfg,
		local:           local,
		apps:            apps,
		fileSets:        fileSets,
		workDir:         workDir,
		logger:          logger,
		status:          syncStatus,
		trigger:         make(chan struct{}, 1),
		requests:        make(chan string, len(fileSets)+1),
		lastSyncStatus:  lastSyncStatus,
		lastHistoryTime: make(map[string]int64),
	}, nil
}

func (m *SyncManager) Ready(context.Context) error {
	if _, err := exec.LookPath("rsync"); err != nil {
		return errors.New("rsync is not installed")
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		return errors.New("OpenSSH client is not installed")
	}
	for name, filename := range map[string]string{
		"SSH private key": m.cfg.HA.Sync.SSHPrivateKey,
		"known_hosts":     m.cfg.HA.Sync.KnownHostsFile,
	} {
		info, err := os.Stat(filename)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("Fleet HA rsync %s is unavailable", name)
		}
	}
	if err := probeWritableDirectory(m.workDir); err != nil {
		return fmt.Errorf("Fleet HA rsync work directory is not writable: %w", err)
	}
	for _, fileSet := range m.fileSets {
		info, err := os.Stat(fileSet.Source)
		if errors.Is(err, os.ErrNotExist) && !fileSet.Required {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect Fleet HA file set %s: %w", fileSet.Key, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("Fleet HA file set %s source is not a directory", fileSet.Key)
		}
	}
	return nil
}

func (m *SyncManager) Prepare(ctx context.Context) error {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	if err := m.applyIncomingFileSets(ctx); err != nil {
		return err
	}
	return m.importIncomingApps(ctx)
}

// Bootstrap records the live file-set heads on the member that already owned
// the VIP when Fleet Agent started. This is the only automatic bootstrap path:
// a standby that merely wins a later election must first inherit a complete
// head from the previous active member.
func (m *SyncManager) Bootstrap(ctx context.Context) error {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	if err := m.Ready(ctx); err != nil {
		return err
	}
	states, err := m.local.LoadHASyncStates(ctx)
	if err != nil {
		return fmt.Errorf("load Fleet HA sync heads: %w", err)
	}
	if hasCompleteLocalHeads(m.fileSets, states) {
		return nil
	}
	heads, err := m.buildFileSetHeads(ctx, m.fileSets, states)
	if err != nil {
		return err
	}
	for index := range heads {
		maximumIncoming, found, incomingErr := m.maximumIncomingManifest(heads[index].FileSet.Key)
		if incomingErr != nil {
			return incomingErr
		}
		if found && maximumIncoming.Generation >= heads[index].Generation {
			if manifestMatchesInventory(maximumIncoming, heads[index].Inventory) {
				heads[index].Generation = maximumIncoming.Generation
				heads[index].ParentGeneration = maximumIncoming.ParentGeneration
			} else {
				heads[index].ParentGeneration = maximumIncoming.Generation
				heads[index].Generation = maximumIncoming.Generation + 1
			}
		}
		head := heads[index]
		if err := m.saveLocalFileSetHead(ctx, head); err != nil {
			return err
		}
		if err := m.saveLocalAppliedFileSetHead(ctx, head); err != nil {
			return err
		}
	}
	return nil
}

func hasCompleteLocalHeads(fileSets []FileSet, states []state.HASyncState) bool {
	for _, fileSet := range fileSets {
		head, found := latestLocalFileSetHead(states, fileSet.Key)
		if !found && !fileSet.Required {
			if _, err := os.Stat(fileSet.Source); errors.Is(err, os.ErrNotExist) {
				continue
			}
		}
		if !found ||
			head.ConfigDigest == "" ||
			head.ContentRevision == "" ||
			head.FileCount < 0 ||
			head.TotalBytes < 0 ||
			head.NodeModulesFiles < 0 ||
			head.NodeModulesBytes < 0 {
			return false
		}
		configDigest, err := fileSetConfigDigest(fileSet)
		if err != nil || head.ConfigDigest != configDigest {
			return false
		}
	}
	return len(fileSets) > 0
}

// AcquisitionReady is the lightweight Keepalived gate. Snapshot application
// already verified the live tree, so the polling path only checks the
// persisted head/config identity and whether a newer snapshot is pending.
func (m *SyncManager) AcquisitionReady(ctx context.Context) error {
	if err := m.Ready(ctx); err != nil {
		return err
	}
	states, err := m.local.LoadHASyncStates(ctx)
	if err != nil {
		return fmt.Errorf("load Fleet HA acquisition heads: %w", err)
	}
	initialBootstrap := m.isInitialBootstrapCandidate(states)
	for _, fileSet := range m.fileSets {
		if _, statErr := os.Stat(fileSet.Source); errors.Is(statErr, os.ErrNotExist) && !fileSet.Required {
			continue
		}
		head, found := latestLocalFileSetHead(states, fileSet.Key)
		if !found {
			if initialBootstrap {
				continue
			}
			return fmt.Errorf("Fleet HA file set %s has no synchronized head", fileSet.Key)
		}
		configDigest, digestErr := fileSetConfigDigest(fileSet)
		if digestErr != nil {
			return digestErr
		}
		if head.ConfigDigest != configDigest ||
			head.ContentRevision == "" ||
			head.FileCount < 0 ||
			head.TotalBytes < 0 ||
			head.NodeModulesFiles < 0 ||
			head.NodeModulesBytes < 0 {
			return fmt.Errorf("Fleet HA file set %s synchronized head is incompatible", fileSet.Key)
		}
		incoming, incomingErr := m.incomingFileSetStatus(fileSet, head)
		if incomingErr != nil {
			return incomingErr
		}
		if incoming.CommittedNewer {
			return fmt.Errorf("Fleet HA file set %s has a newer incoming snapshot pending", fileSet.Key)
		}
		if incoming.Pending != nil && m.pendingManifestAge(*incoming.Pending) < m.pendingManifestGrace() {
			return fmt.Errorf(
				"Fleet HA file set %s generation %d is still transferring",
				fileSet.Key,
				incoming.Pending.Generation,
			)
		}
	}
	return nil
}

// PromotionReady is deliberately stricter than AcquisitionReady. It runs only
// after this member owns the VIP and before business containers start.
func (m *SyncManager) PromotionReady(ctx context.Context) error {
	if err := m.AcquisitionReady(ctx); err != nil {
		return err
	}
	states, err := m.local.LoadHASyncStates(ctx)
	if err != nil {
		return fmt.Errorf("load Fleet HA promotion heads: %w", err)
	}
	for _, fileSet := range m.fileSets {
		if _, statErr := os.Stat(fileSet.Source); errors.Is(statErr, os.ErrNotExist) && !fileSet.Required {
			continue
		}
		head, found := latestLocalFileSetHead(states, fileSet.Key)
		if !found {
			return errInitialSyncBootstrapRequired
		}
		inventory, inventoryErr := inspectFileSet(fileSet)
		if inventoryErr != nil {
			return fmt.Errorf("inspect Fleet HA file set %s: %w", fileSet.Key, inventoryErr)
		}
		if !syncStateMatchesInventory(head, inventory) {
			return fmt.Errorf("Fleet HA file set %s does not match its synchronized head", fileSet.Key)
		}
	}
	return nil
}

func (m *SyncManager) SetOutboundEnabled(ctx context.Context, enabled bool) error {
	if !enabled {
		m.outbound.Store(false)
		m.Cancel()
		m.updateStatus(func(status *SyncStatus) {
			status.OutboundBlockedReason = ""
		})
		return nil
	}
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	if err := m.outboundReady(ctx); err != nil {
		m.outbound.Store(false)
		m.updateStatus(func(status *SyncStatus) {
			status.OutboundBlockedReason = err.Error()
		})
		return err
	}
	m.outbound.Store(true)
	m.updateStatus(func(status *SyncStatus) {
		status.OutboundBlockedReason = ""
	})
	return nil
}

func (m *SyncManager) Trigger() {
	select {
	case m.trigger <- struct{}{}:
	default:
	}
}

func (m *SyncManager) Cancel() {
	m.cancelMu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.cancelMu.Unlock()
}

func (m *SyncManager) Status() SyncStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.status
	out.OutboundEnabled = m.outbound.Load()
	out.Peers = append([]string(nil), m.status.Peers...)
	return out
}

func (m *SyncManager) History(
	ctx context.Context,
	peer string,
	fileSet string,
	page int,
	pageSize int,
) (fleetprotocol.HASyncHistoryResponse, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	result, err := m.local.LoadHASyncEvents(ctx, state.HASyncEventQuery{
		Peer:    strings.TrimSpace(peer),
		FileSet: strings.TrimSpace(fileSet),
		Limit:   pageSize,
		Offset:  (page - 1) * pageSize,
	})
	if err != nil {
		return fleetprotocol.HASyncHistoryResponse{}, err
	}
	response := fleetprotocol.HASyncHistoryResponse{
		Items:    make([]fleetprotocol.HASyncHistoryItem, 0, len(result.Items)),
		Total:    result.Total,
		Page:     page,
		PageSize: pageSize,
	}
	for _, item := range result.Items {
		response.Items = append(response.Items, fleetprotocol.HASyncHistoryItem{
			ID:           item.ID,
			OperationID:  item.OperationID,
			Peer:         item.Peer,
			FileSet:      item.FileSet,
			Revision:     item.Revision,
			Direction:    item.Direction,
			TriggerType:  item.TriggerType,
			Status:       item.Status,
			StartedAt:    item.StartedAt,
			FinishedAt:   item.FinishedAt,
			ErrorMessage: item.ErrorMessage,
		})
	}
	return response, nil
}

func (m *SyncManager) SyncFileSet(_ context.Context, key string) error {
	key = strings.TrimSpace(key)
	if _, found := m.fileSet(key); !found {
		return fmt.Errorf("unknown Fleet HA file set: %s", key)
	}
	select {
	case m.requests <- key:
	default:
		return errors.New("Fleet HA rsync queue is full")
	}
	return nil
}

// SyncFileSetNow applies one requested file set before returning. It is used
// only for failover-critical state whose caller must not report success before
// every standby has received the update (currently the license bundle).
func (m *SyncManager) SyncFileSetNow(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if _, found := m.fileSet(key); !found {
		return fmt.Errorf("unknown Fleet HA file set: %s", key)
	}
	if !m.outbound.Load() {
		return errors.New("Fleet HA outbound synchronization is fenced")
	}
	return m.syncSelected(ctx, key, syncTriggerForFileSet(key))
}

func (m *SyncManager) Run(ctx context.Context, active func() bool) {
	syncTicker := time.NewTicker(m.cfg.HA.Sync.Interval)
	defer syncTicker.Stop()
	fullTicker := time.NewTicker(m.cfg.HA.Sync.FullInterval)
	defer fullTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.trigger:
			if active() && m.outbound.Load() {
				m.logSyncError("triggered", m.syncAll(ctx, true, syncTriggerPromotion))
			}
		case key := <-m.requests:
			if active() && m.outbound.Load() {
				m.logSyncError(key, m.syncSelected(ctx, key, syncTriggerForFileSet(key)))
			}
		case <-syncTicker.C:
			if active() {
				if !m.outbound.Load() {
					m.logSyncError("outbound fence", m.SetOutboundEnabled(ctx, true))
					continue
				}
				m.logSyncError("incremental", m.syncAll(ctx, false, syncTriggerPeriodic))
			} else {
				m.logSyncError("incoming apps", m.Prepare(ctx))
			}
		case <-fullTicker.C:
			if active() && m.outbound.Load() {
				m.logSyncError("full", m.syncAll(ctx, true, syncTriggerFull))
			}
		}
	}
}

func (m *SyncManager) syncAll(ctx context.Context, includeAppRuntime bool, triggerType string) error {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	return m.syncPeers(ctx, m.fileSets, includeAppRuntime, triggerType)
}

func (m *SyncManager) syncSelected(ctx context.Context, key string, triggerType string) error {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	fileSet, found := m.fileSet(key)
	if !found {
		return fmt.Errorf("unknown Fleet HA file set: %s", key)
	}
	return m.syncPeers(ctx, []FileSet{fileSet}, false, triggerType)
}

func (m *SyncManager) syncPeers(
	ctx context.Context,
	fileSets []FileSet,
	includeAppRuntime bool,
	triggerType string,
) error {
	startedAt := time.Now().UTC().UnixMilli()
	m.updateStatus(func(status *SyncStatus) {
		status.State = syncStateRunning
		status.LastStartedAt = startedAt
		status.LastError = ""
	})

	var appRevision string
	if includeAppRuntime {
		var err error
		appRevision, err = m.ensureAppBundle(ctx)
		if err != nil {
			m.finishSync(startedAt, err)
			return err
		}
	}
	persistedStates, err := m.local.LoadHASyncStates(ctx)
	if err != nil {
		m.finishSync(startedAt, err)
		return fmt.Errorf("load Fleet HA file set generations: %w", err)
	}
	heads, err := m.buildFileSetHeads(ctx, fileSets, persistedStates)
	if err != nil {
		m.finishSync(startedAt, err)
		return err
	}
	for _, head := range heads {
		if err := m.saveLocalFileSetHead(ctx, head); err != nil {
			m.finishSync(startedAt, err)
			return err
		}
	}
	operationID := uuid.NewString()

	operationCtx, cancel := context.WithTimeout(ctx, m.cfg.HA.Sync.Timeout)
	m.cancelMu.Lock()
	m.cancel = cancel
	m.cancelMu.Unlock()
	defer func() {
		cancel()
		m.cancelMu.Lock()
		m.cancel = nil
		m.cancelMu.Unlock()
	}()
	sem := make(chan struct{}, m.cfg.HA.Sync.MaxConcurrency)
	var wg sync.WaitGroup
	var errorsMu sync.Mutex
	peerErrors := make([]error, 0)
	for _, peer := range m.cfg.HA.Sync.Peers {
		peer := peer
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-operationCtx.Done():
				errorsMu.Lock()
				peerErrors = append(peerErrors, operationCtx.Err())
				errorsMu.Unlock()
				return
			}
			defer func() { <-sem }()
			if err := m.syncPeer(
				operationCtx,
				peer,
				heads,
				appRevision,
				triggerType,
				operationID,
				persistedStates,
			); err != nil {
				errorsMu.Lock()
				peerErrors = append(peerErrors, fmt.Errorf("%s: %w", peer, err))
				errorsMu.Unlock()
			}
		}()
	}
	wg.Wait()
	err = errors.Join(peerErrors...)
	m.finishSync(startedAt, err)
	return err
}

func (m *SyncManager) syncPeer(
	ctx context.Context,
	peer string,
	heads []fileSetHead,
	appRevision string,
	triggerType string,
	operationID string,
	persistedStates []state.HASyncState,
) error {
	peerErrors := make([]error, 0)
	for _, head := range heads {
		fileSet := head.FileSet
		if _, err := os.Stat(fileSet.Source); errors.Is(err, os.ErrNotExist) && !fileSet.Required {
			continue
		}
		generation := head.Generation
		revision := strconv.FormatUint(generation, 10)
		startedAt := time.Now().UTC().UnixMilli()
		if previous, found := findPeerSyncState(persistedStates, peer, fileSet.Key); found &&
			(previous.Status == syncStateError || previous.Status == syncStateCanceled) {
			if previousGeneration, parseErr := strconv.ParseUint(previous.Revision, 10, 64); parseErr == nil {
				if err := m.removeRemoteDirectory(
					ctx,
					peer,
					m.fileSetSnapshotDirectory(fileSet.Key, previousGeneration),
				); err != nil {
					m.saveSyncState(ctx, syncRecord{
						OperationID: operationID,
						Peer:        peer,
						FileSet:     fileSet.Key,
						Revision:    previous.Revision,
						Direction:   syncDirectionOutbound,
						TriggerType: triggerType,
						StartedAt:   startedAt,
						Err:         err,
					})
					peerErrors = append(peerErrors, fmt.Errorf("%s cleanup: %w", fileSet.Key, err))
					continue
				}
			}
		}
		useLinkDest := hasSuccessfulPreviousSnapshot(
			persistedStates,
			peer,
			fileSet.Key,
			generation,
		)
		if err := m.runFileSetSync(ctx, peer, head, useLinkDest); err != nil {
			m.saveSyncState(ctx, syncRecord{
				OperationID:      operationID,
				Peer:             peer,
				FileSet:          fileSet.Key,
				Revision:         revision,
				ConfigDigest:     head.Inventory.ConfigDigest,
				ContentRevision:  head.Inventory.ContentRevision,
				FileCount:        head.Inventory.FileCount,
				TotalBytes:       head.Inventory.TotalBytes,
				NodeModulesFiles: head.Inventory.NodeModulesFiles,
				NodeModulesBytes: head.Inventory.NodeModulesBytes,
				Direction:        syncDirectionOutbound,
				TriggerType:      triggerType,
				StartedAt:        startedAt,
				Err:              err,
			})
			peerErrors = append(peerErrors, fmt.Errorf("%s: %w", fileSet.Key, err))
			continue
		}
		m.saveSyncState(ctx, syncRecord{
			OperationID:      operationID,
			Peer:             peer,
			FileSet:          fileSet.Key,
			Revision:         revision,
			ConfigDigest:     head.Inventory.ConfigDigest,
			ContentRevision:  head.Inventory.ContentRevision,
			FileCount:        head.Inventory.FileCount,
			TotalBytes:       head.Inventory.TotalBytes,
			NodeModulesFiles: head.Inventory.NodeModulesFiles,
			NodeModulesBytes: head.Inventory.NodeModulesBytes,
			Direction:        syncDirectionOutbound,
			TriggerType:      triggerType,
			StartedAt:        startedAt,
		})
	}
	if appRevision != "" {
		startedAt := time.Now().UTC().UnixMilli()
		if err := m.syncAppBundle(ctx, peer); err != nil {
			m.saveSyncState(ctx, syncRecord{
				OperationID: operationID,
				Peer:        peer,
				FileSet:     appRuntimeKey,
				Revision:    appRevision,
				Direction:   syncDirectionOutbound,
				TriggerType: triggerType,
				StartedAt:   startedAt,
				Err:         err,
			})
			peerErrors = append(peerErrors, fmt.Errorf("%s: %w", appRuntimeKey, err))
		} else {
			m.saveSyncState(ctx, syncRecord{
				OperationID: operationID,
				Peer:        peer,
				FileSet:     appRuntimeKey,
				Revision:    appRevision,
				Direction:   syncDirectionOutbound,
				TriggerType: triggerType,
				StartedAt:   startedAt,
			})
		}
	}
	return errors.Join(peerErrors...)
}

func (m *SyncManager) runFileSetSync(
	ctx context.Context,
	peer string,
	head fileSetHead,
	useLinkDest bool,
) error {
	fileSet := head.FileSet
	generation := head.Generation
	revision := strconv.FormatUint(generation, 10)
	target := m.fileSetSnapshotDirectory(fileSet.Key, generation)
	dataTarget := filepath.Join(target, "data")
	if err := m.ensureRemoteDirectory(ctx, peer, dataTarget); err != nil {
		return err
	}
	manifest := fileSetManifest{
		FileSet:          fileSet.Key,
		Generation:       generation,
		ParentGeneration: head.ParentGeneration,
		SnapshotID:       m.cfg.HA.MemberID + "-" + revision,
		SourceMemberID:   m.cfg.HA.MemberID,
		ConfigDigest:     head.Inventory.ConfigDigest,
		ContentRevision:  head.Inventory.ContentRevision,
		FileCount:        head.Inventory.FileCount,
		TotalBytes:       head.Inventory.TotalBytes,
		NodeModulesFiles: head.Inventory.NodeModulesFiles,
		NodeModulesBytes: head.Inventory.NodeModulesBytes,
		CreatedAt:        time.Now().UTC().UnixMilli(),
	}
	manifestName, err := m.writeFileSetManifest(manifest)
	if err != nil {
		return err
	}
	defer os.Remove(manifestName)
	args := m.rsyncArgs(
		peer,
		manifestName,
		filepath.Join(target, fileSetPendingManifestFile),
		nil,
		nil,
		"",
	)
	if err := runCommand(ctx, "rsync", args...); err != nil {
		return fmt.Errorf("publish Fleet HA pending manifest: %w", err)
	}
	linkDest := ""
	if useLinkDest && generation > 1 {
		linkDest = filepath.Join(m.fileSetSnapshotDirectory(fileSet.Key, generation-1), "data")
	}
	args = m.rsyncArgs(
		peer,
		fileSet.Source,
		dataTarget,
		fileSet.Includes,
		fileSet.Excludes,
		linkDest,
	)
	if err := runCommand(ctx, "rsync", args...); err != nil {
		return err
	}
	args = m.rsyncArgs(peer, manifestName, filepath.Join(target, fileSetManifestFile), nil, nil, "")
	if err := runCommand(ctx, "rsync", args...); err != nil {
		return fmt.Errorf("commit Fleet HA file set manifest: %w", err)
	}
	if err := m.removeRemoteFile(
		ctx,
		peer,
		filepath.Join(target, fileSetPendingManifestFile),
	); err != nil {
		return fmt.Errorf("clear Fleet HA pending manifest: %w", err)
	}
	return nil
}

func (m *SyncManager) writeFileSetManifest(manifest fileSetManifest) (string, error) {
	body, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("encode Fleet HA file set manifest: %w", err)
	}
	manifestFile, err := os.CreateTemp(m.workDir, ".tier0-sync-manifest-*")
	if err != nil {
		return "", fmt.Errorf("create Fleet HA file set manifest: %w", err)
	}
	manifestName := manifestFile.Name()
	closeWithError := func(input error) (string, error) {
		_ = manifestFile.Close()
		_ = os.Remove(manifestName)
		return "", input
	}
	if _, err := manifestFile.Write(append(body, '\n')); err != nil {
		return closeWithError(fmt.Errorf("write Fleet HA file set manifest: %w", err))
	}
	if err := manifestFile.Chmod(0o600); err != nil {
		return closeWithError(fmt.Errorf("secure Fleet HA file set manifest: %w", err))
	}
	if err := manifestFile.Close(); err != nil {
		_ = os.Remove(manifestName)
		return "", fmt.Errorf("close Fleet HA file set manifest: %w", err)
	}
	return manifestName, nil
}

func (m *SyncManager) nextFileSetGenerations(
	ctx context.Context,
	fileSets []FileSet,
) (map[string]uint64, error) {
	states, err := m.local.LoadHASyncStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("load Fleet HA file set generations: %w", err)
	}
	return nextFileSetGenerationsFromStates(fileSets, states), nil
}

func (m *SyncManager) buildFileSetHeads(
	ctx context.Context,
	fileSets []FileSet,
	states []state.HASyncState,
) ([]fileSetHead, error) {
	maximum := maximumFileSetGenerations(states)
	heads := make([]fileSetHead, 0, len(fileSets))
	for _, fileSet := range fileSets {
		if _, err := os.Stat(fileSet.Source); errors.Is(err, os.ErrNotExist) && !fileSet.Required {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		inventory, err := inspectFileSet(fileSet)
		if err != nil {
			return nil, fmt.Errorf("inspect Fleet HA file set %s: %w", fileSet.Key, err)
		}
		generation := maximum[fileSet.Key] + 1
		parentGeneration := maximum[fileSet.Key]
		if previous, found := latestLocalFileSetHead(states, fileSet.Key); found &&
			syncStateMatchesInventory(previous, inventory) {
			if value, parseErr := strconv.ParseUint(previous.Revision, 10, 64); parseErr == nil && value > 0 {
				generation = value
				if generation > 1 {
					parentGeneration = generation - 1
				} else {
					parentGeneration = 0
				}
			}
		}
		heads = append(heads, fileSetHead{
			FileSet:          fileSet,
			Generation:       generation,
			ParentGeneration: parentGeneration,
			Inventory:        inventory,
		})
	}
	return heads, nil
}

func maximumFileSetGenerations(states []state.HASyncState) map[string]uint64 {
	maximum := make(map[string]uint64)
	for _, item := range states {
		key := strings.TrimPrefix(strings.TrimPrefix(item.FileSet, appliedStatePrefix), headStatePrefix)
		value, err := strconv.ParseUint(item.Revision, 10, 64)
		if err == nil && value > maximum[key] {
			maximum[key] = value
		}
	}
	return maximum
}

func nextFileSetGenerationsFromStates(
	fileSets []FileSet,
	states []state.HASyncState,
) map[string]uint64 {
	maximum := maximumFileSetGenerations(states)
	result := make(map[string]uint64, len(fileSets))
	for _, fileSet := range fileSets {
		result[fileSet.Key] = maximum[fileSet.Key] + 1
	}
	return result
}

func (m *SyncManager) applyIncomingFileSets(ctx context.Context) error {
	pattern := filepath.Join(m.workDir, "incoming", "filesets", "*", "*", "*", fileSetManifestFile)
	manifestFiles, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("scan Fleet HA incoming file sets: %w", err)
	}
	candidatesByFileSet := make(map[string][]fileSetCandidate)
	for _, filename := range manifestFiles {
		body, readErr := os.ReadFile(filename)
		if readErr != nil {
			return fmt.Errorf("read Fleet HA incoming manifest: %w", readErr)
		}
		var manifest fileSetManifest
		if decodeErr := json.Unmarshal(body, &manifest); decodeErr != nil {
			return fmt.Errorf("decode Fleet HA incoming manifest: %w", decodeErr)
		}
		fileSet, found := m.fileSet(manifest.FileSet)
		if !found ||
			manifest.Generation == 0 ||
			manifest.ParentGeneration >= manifest.Generation ||
			strings.TrimSpace(manifest.SourceMemberID) == "" ||
			manifest.SnapshotID != manifest.SourceMemberID+"-"+strconv.FormatUint(manifest.Generation, 10) {
			return errors.New("Fleet HA incoming file set manifest is invalid")
		}
		parts := strings.Split(filepath.ToSlash(filename), "/")
		if len(parts) < 5 ||
			parts[len(parts)-2] != strconv.FormatUint(manifest.Generation, 10) ||
			parts[len(parts)-3] != manifest.FileSet ||
			parts[len(parts)-4] != manifest.SourceMemberID {
			return errors.New("Fleet HA incoming file set manifest path does not match its identity")
		}
		if strings.TrimSpace(manifest.ConfigDigest) == "" ||
			strings.TrimSpace(manifest.ContentRevision) == "" ||
			manifest.FileCount < 0 ||
			manifest.TotalBytes < 0 ||
			manifest.NodeModulesFiles < 0 ||
			manifest.NodeModulesBytes < 0 {
			m.logger.Warn(
				"discard legacy Fleet HA snapshot without completeness metadata",
				"fileSet",
				manifest.FileSet,
				"sourceMemberID",
				manifest.SourceMemberID,
			)
			_ = os.RemoveAll(filepath.Dir(filename))
			continue
		}
		configDigest, digestErr := fileSetConfigDigest(fileSet)
		if digestErr != nil {
			return digestErr
		}
		if manifest.ConfigDigest != configDigest {
			m.logger.Warn(
				"discard Fleet HA snapshot with incompatible file-set configuration",
				"fileSet",
				manifest.FileSet,
				"sourceMemberID",
				manifest.SourceMemberID,
			)
			_ = os.RemoveAll(filepath.Dir(filename))
			continue
		}
		candidatesByFileSet[manifest.FileSet] = append(
			candidatesByFileSet[manifest.FileSet],
			fileSetCandidate{filename: filename, manifest: manifest},
		)
	}

	states, err := m.local.LoadHASyncStates(ctx)
	if err != nil {
		return fmt.Errorf("load Fleet HA applied file set generations: %w", err)
	}
	applied := make(map[string]uint64)
	for _, item := range states {
		if item.Peer != localSyncPeer || !strings.HasPrefix(item.FileSet, appliedStatePrefix) {
			continue
		}
		value, parseErr := strconv.ParseUint(item.Revision, 10, 64)
		if parseErr == nil && value > applied[strings.TrimPrefix(item.FileSet, appliedStatePrefix)] {
			applied[strings.TrimPrefix(item.FileSet, appliedStatePrefix)] = value
		}
	}
	fileSetKeys := make([]string, 0, len(candidatesByFileSet))
	for fileSetKey := range candidatesByFileSet {
		fileSetKeys = append(fileSetKeys, fileSetKey)
	}
	sort.Strings(fileSetKeys)
	for _, fileSetKey := range fileSetKeys {
		candidates := candidatesByFileSet[fileSetKey]
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].manifest.Generation == candidates[j].manifest.Generation {
				return candidates[i].manifest.SnapshotID < candidates[j].manifest.SnapshotID
			}
			return candidates[i].manifest.Generation < candidates[j].manifest.Generation
		})
		latest := candidates[len(candidates)-1]
		manifest := latest.manifest
		for index := len(candidates) - 2; index >= 0; index-- {
			other := candidates[index].manifest
			if other.Generation != manifest.Generation {
				break
			}
			if other.ConfigDigest != manifest.ConfigDigest ||
				other.ContentRevision != manifest.ContentRevision ||
				other.FileCount != manifest.FileCount ||
				other.TotalBytes != manifest.TotalBytes ||
				other.NodeModulesFiles != manifest.NodeModulesFiles ||
				other.NodeModulesBytes != manifest.NodeModulesBytes {
				return fmt.Errorf(
					"Fleet HA file set %s has conflicting snapshots at generation %d",
					manifest.FileSet,
					manifest.Generation,
				)
			}
		}
		revisionDir := filepath.Dir(latest.filename)
		if manifest.Generation <= applied[manifest.FileSet] {
			keepDir := ""
			if manifest.Generation == applied[manifest.FileSet] {
				keepDir = revisionDir
			}
			m.pruneFileSetSnapshots(candidates, keepDir)
			continue
		}
		fileSet, _ := m.fileSet(manifest.FileSet)
		dataDir := filepath.Join(revisionDir, "data")
		info, statErr := os.Stat(dataDir)
		if statErr != nil || !info.IsDir() {
			return fmt.Errorf("Fleet HA incoming file set data is unavailable: %s", manifest.FileSet)
		}
		stagedInventory, inventoryErr := inspectFileSetRoot(fileSet, dataDir)
		if inventoryErr != nil {
			return fmt.Errorf("inspect incoming Fleet HA file set %s: %w", manifest.FileSet, inventoryErr)
		}
		if !manifestMatchesInventory(manifest, stagedInventory) {
			return fmt.Errorf("Fleet HA incoming file set %s is incomplete", manifest.FileSet)
		}
		args := []string{
			"-a",
			"--no-owner",
			"--no-group",
			"--delete-delay",
			"--delay-updates",
			"--safe-links",
		}
		args = appendFileSetFilters(args, fileSet.Includes, fileSet.Excludes)
		args = append(args,
			strings.TrimRight(dataDir, string(filepath.Separator))+string(filepath.Separator),
			strings.TrimRight(fileSet.Source, string(filepath.Separator))+string(filepath.Separator),
		)
		startedAt := time.Now().UTC().UnixMilli()
		if err := runCommand(ctx, "rsync", args...); err != nil {
			m.saveSyncState(ctx, syncRecord{
				OperationID:      uuid.NewString(),
				Peer:             localSyncPeer,
				FileSet:          appliedStatePrefix + manifest.FileSet,
				Revision:         strconv.FormatUint(manifest.Generation, 10),
				ConfigDigest:     manifest.ConfigDigest,
				ContentRevision:  manifest.ContentRevision,
				FileCount:        manifest.FileCount,
				TotalBytes:       manifest.TotalBytes,
				NodeModulesFiles: manifest.NodeModulesFiles,
				NodeModulesBytes: manifest.NodeModulesBytes,
				Direction:        syncDirectionInbound,
				TriggerType:      syncTriggerIncoming,
				StartedAt:        startedAt,
				Err:              err,
			})
			return fmt.Errorf("apply Fleet HA incoming file set %s: %w", manifest.FileSet, err)
		}
		appliedInventory, inventoryErr := inspectFileSet(fileSet)
		if inventoryErr != nil {
			return fmt.Errorf("verify applied Fleet HA file set %s: %w", manifest.FileSet, inventoryErr)
		}
		if !manifestMatchesInventory(manifest, appliedInventory) {
			return fmt.Errorf("applied Fleet HA file set %s failed completeness verification", manifest.FileSet)
		}
		m.saveSyncState(ctx, syncRecord{
			OperationID:      uuid.NewString(),
			Peer:             localSyncPeer,
			FileSet:          appliedStatePrefix + manifest.FileSet,
			Revision:         strconv.FormatUint(manifest.Generation, 10),
			ConfigDigest:     manifest.ConfigDigest,
			ContentRevision:  manifest.ContentRevision,
			FileCount:        manifest.FileCount,
			TotalBytes:       manifest.TotalBytes,
			NodeModulesFiles: manifest.NodeModulesFiles,
			NodeModulesBytes: manifest.NodeModulesBytes,
			Direction:        syncDirectionInbound,
			TriggerType:      syncTriggerIncoming,
			StartedAt:        startedAt,
		})
		if err := m.saveLocalFileSetHead(ctx, fileSetHead{
			FileSet:          fileSet,
			Generation:       manifest.Generation,
			ParentGeneration: manifest.ParentGeneration,
			Inventory:        appliedInventory,
		}); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(revisionDir, fileSetPendingManifestFile)); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			m.logger.Warn(
				"remove committed Fleet HA pending manifest failed",
				"fileSet",
				manifest.FileSet,
				"generation",
				manifest.Generation,
				"error",
				err,
			)
		}
		applied[manifest.FileSet] = manifest.Generation
		m.pruneFileSetSnapshots(candidates, revisionDir)
	}
	return nil
}

func (m *SyncManager) syncAppBundle(ctx context.Context, peer string) error {
	source := filepath.Join(m.workDir, "outgoing", "apps")
	target := filepath.Join(m.workDir, "incoming", "apps")
	if err := m.ensureRemoteDirectory(ctx, peer, target); err != nil {
		return err
	}
	args := m.rsyncArgs(peer, source, target, nil, []string{appRevisionFile}, "")
	if err := runCommand(ctx, "rsync", args...); err != nil {
		return err
	}
	marker := filepath.Join(source, appRevisionFile)
	args = m.rsyncArgs(peer, marker, filepath.Join(target, appRevisionFile), nil, nil, "")
	if err := runCommand(ctx, "rsync", args...); err != nil {
		return err
	}
	return nil
}

func (m *SyncManager) ensureAppBundle(ctx context.Context) (string, error) {
	startedAt := time.Now().UTC().UnixMilli()
	signature, err := m.apps.HAAppSignature(ctx, m.cfg.HA.ServiceGroup)
	if err != nil {
		return "", fmt.Errorf("inspect Fleet HA apps: %w", err)
	}
	bundleDir := filepath.Join(m.workDir, "outgoing", "apps")
	marker := filepath.Join(bundleDir, appRevisionFile)
	states, loadErr := m.local.LoadHASyncStates(ctx)
	if loadErr == nil {
		for _, item := range states {
			if item.Peer == localSyncPeer && item.FileSet == appExportStateKey &&
				item.Revision == signature && item.Status == syncStateSuccess {
				if body, readErr := os.ReadFile(marker); readErr == nil && strings.TrimSpace(string(body)) == signature {
					return signature, nil
				}
			}
		}
	}
	if err := os.RemoveAll(bundleDir); err != nil {
		return "", fmt.Errorf("clear Fleet HA app export: %w", err)
	}
	if err := os.MkdirAll(bundleDir, 0o700); err != nil {
		return "", fmt.Errorf("create Fleet HA app export: %w", err)
	}
	if _, err := m.apps.ExportHAApps(ctx, m.cfg.HA.ServiceGroup, bundleDir); err != nil {
		return "", fmt.Errorf("export Fleet HA apps: %w", err)
	}
	if err := os.WriteFile(marker, []byte(signature+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write Fleet HA app revision: %w", err)
	}
	m.saveSyncState(ctx, syncRecord{
		OperationID: uuid.NewString(),
		Peer:        localSyncPeer,
		FileSet:     appExportStateKey,
		Revision:    signature,
		Direction:   syncDirectionOutbound,
		TriggerType: syncTriggerFull,
		StartedAt:   startedAt,
	})
	return signature, nil
}

func (m *SyncManager) importIncomingApps(ctx context.Context) error {
	startedAt := time.Now().UTC().UnixMilli()
	bundleDir := filepath.Join(m.workDir, "incoming", "apps")
	body, err := os.ReadFile(filepath.Join(bundleDir, appRevisionFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read incoming Fleet HA app revision: %w", err)
	}
	revision := strings.TrimSpace(string(body))
	if revision == "" {
		return errors.New("incoming Fleet HA app revision is empty")
	}
	states, err := m.local.LoadHASyncStates(ctx)
	if err != nil {
		return err
	}
	for _, item := range states {
		if item.Peer == localSyncPeer && item.FileSet == appIncomingStateKey &&
			item.Revision == revision && item.Status == syncStateSuccess {
			return nil
		}
	}
	if err := m.apps.ImportHAApps(ctx, m.cfg.HA.ServiceGroup, bundleDir); err != nil {
		m.saveSyncState(ctx, syncRecord{
			OperationID: uuid.NewString(),
			Peer:        localSyncPeer,
			FileSet:     appIncomingStateKey,
			Revision:    revision,
			Direction:   syncDirectionInbound,
			TriggerType: syncTriggerIncoming,
			StartedAt:   startedAt,
			Err:         err,
		})
		return fmt.Errorf("import Fleet HA apps: %w", err)
	}
	m.saveSyncState(ctx, syncRecord{
		OperationID: uuid.NewString(),
		Peer:        localSyncPeer,
		FileSet:     appIncomingStateKey,
		Revision:    revision,
		Direction:   syncDirectionInbound,
		TriggerType: syncTriggerIncoming,
		StartedAt:   startedAt,
	})
	return nil
}

func (m *SyncManager) ensureRemoteDirectory(ctx context.Context, peer, directory string) error {
	if !isSafeRemotePath(directory) {
		return errors.New("Fleet HA rsync destination path is unsafe")
	}
	args := m.sshArgs(peer)
	args = append(args, "mkdir", "-p", "--", directory)
	return runCommand(ctx, "ssh", args...)
}

func (m *SyncManager) removeRemoteDirectory(ctx context.Context, peer, directory string) error {
	root := path.Clean(filepath.ToSlash(filepath.Join(
		m.workDir,
		"incoming",
		"filesets",
		m.cfg.HA.MemberID,
	)))
	target := path.Clean(filepath.ToSlash(directory))
	if !isSafeRemotePath(target) || target == root || !strings.HasPrefix(target, root+"/") {
		return errors.New("Fleet HA rsync cleanup path is unsafe")
	}
	args := m.sshArgs(peer)
	args = append(args, "rm", "-rf", "--", target)
	return runCommand(ctx, "ssh", args...)
}

func (m *SyncManager) removeRemoteFile(ctx context.Context, peer, filename string) error {
	root := path.Clean(filepath.ToSlash(filepath.Join(
		m.workDir,
		"incoming",
		"filesets",
		m.cfg.HA.MemberID,
	)))
	target := path.Clean(filepath.ToSlash(filename))
	if !isSafeRemotePath(target) || !strings.HasPrefix(target, root+"/") {
		return errors.New("Fleet HA rsync cleanup file path is unsafe")
	}
	args := m.sshArgs(peer)
	args = append(args, "rm", "-f", "--", target)
	return runCommand(ctx, "ssh", args...)
}

func (m *SyncManager) fileSetSnapshotDirectory(fileSet string, generation uint64) string {
	return filepath.Join(
		m.workDir,
		"incoming",
		"filesets",
		m.cfg.HA.MemberID,
		fileSet,
		strconv.FormatUint(generation, 10),
	)
}

func (m *SyncManager) pruneFileSetSnapshots(candidates []fileSetCandidate, keepDirectory string) {
	keepDirectory = filepath.Clean(keepDirectory)
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		directory := filepath.Clean(filepath.Dir(candidate.filename))
		if directory == keepDirectory {
			continue
		}
		if _, found := seen[directory]; found {
			continue
		}
		seen[directory] = struct{}{}
		if err := os.RemoveAll(directory); err != nil {
			m.logger.Warn(
				"remove obsolete Fleet HA file set snapshot failed",
				"fileSet",
				candidate.manifest.FileSet,
				"generation",
				candidate.manifest.Generation,
				"error",
				err,
			)
		}
	}
}

func (m *SyncManager) rsyncArgs(
	peer, source, target string,
	includes, excludes []string,
	linkDest string,
) []string {
	args := []string{
		"-a",
		"--no-owner",
		"--no-group",
		"--delete-delay",
		"--delay-updates",
		"--partial",
		"--safe-links",
		"--timeout=" + strconv.Itoa(max(1, int(m.cfg.HA.Sync.Timeout.Seconds()))),
		"-e",
		strings.Join(append([]string{"ssh"}, m.sshTransportArgs()...), " "),
	}
	args = appendFileSetFilters(args, includes, excludes)
	if linkDest = strings.TrimSpace(linkDest); linkDest != "" {
		args = append(args, "--link-dest="+filepath.ToSlash(linkDest))
	}
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		source = strings.TrimRight(source, string(filepath.Separator)) + string(filepath.Separator)
		target = strings.TrimRight(target, "/") + "/"
	}
	args = append(args, source, m.cfg.HA.Sync.SSHUser+"@"+peer+":"+target)
	return args
}

func appendFileSetFilters(args, includes, excludes []string) []string {
	for _, pattern := range excludes {
		args = append(args, "--exclude="+pattern)
	}
	if len(includes) == 0 {
		return args
	}
	args = append(args, "--include=*/")
	for _, pattern := range includes {
		args = append(args, "--include="+pattern)
	}
	return append(args, "--exclude=*")
}

func (m *SyncManager) sshArgs(peer string) []string {
	args := m.sshTransportArgs()
	return append(args, m.cfg.HA.Sync.SSHUser+"@"+peer)
}

func (m *SyncManager) sshTransportArgs() []string {
	return []string{
		"-p", strconv.Itoa(m.cfg.HA.Sync.SSHPort),
		"-i", m.cfg.HA.Sync.SSHPrivateKey,
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=" + m.cfg.HA.Sync.KnownHostsFile,
		"-o", "ConnectTimeout=15",
	}
}

func (m *SyncManager) saveSyncState(ctx context.Context, record syncRecord) {
	finishedAt := time.Now().UTC().UnixMilli()
	status := syncStateSuccess
	errorMessage := ""
	switch {
	case errors.Is(record.Err, context.Canceled):
		status = syncStateCanceled
		errorMessage = record.Err.Error()
	case record.Err != nil:
		status = syncStateError
		errorMessage = record.Err.Error()
	}
	input := state.HASyncState{
		Peer:             record.Peer,
		FileSet:          record.FileSet,
		Revision:         record.Revision,
		ConfigDigest:     record.ConfigDigest,
		ContentRevision:  record.ContentRevision,
		FileCount:        record.FileCount,
		TotalBytes:       record.TotalBytes,
		NodeModulesFiles: record.NodeModulesFiles,
		NodeModulesBytes: record.NodeModulesBytes,
		Status:           status,
		LastStartedAt:    record.StartedAt,
		ErrorMessage:     errorMessage,
	}
	if status == syncStateSuccess {
		input.LastSucceededAt = finishedAt
	}
	persistCtx, cancel := syncPersistenceContext(ctx)
	defer cancel()
	if err := m.local.SaveHASyncState(persistCtx, input); err != nil {
		m.logger.Warn(
			"persist Fleet HA rsync state failed",
			"peer",
			record.Peer,
			"fileSet",
			record.FileSet,
			"error",
			err,
		)
		return
	}
	key := syncStateKey(record.Peer, record.FileSet)
	m.historyMu.Lock()
	previousStatus := m.lastSyncStatus[key]
	lastHistoryAt := m.lastHistoryTime[key]
	m.lastSyncStatus[key] = status
	importantTrigger := record.TriggerType == syncTriggerManual ||
		record.TriggerType == syncTriggerLicense ||
		record.TriggerType == syncTriggerPromotion ||
		record.TriggerType == syncTriggerIncoming
	shouldRecord := importantTrigger ||
		previousStatus != status ||
		finishedAt-lastHistoryAt >= syncHistoryCheckpointInterval.Milliseconds()
	m.historyMu.Unlock()
	if !shouldRecord {
		return
	}
	event := state.HASyncEvent{
		OperationID:  record.OperationID,
		Peer:         record.Peer,
		FileSet:      record.FileSet,
		Revision:     record.Revision,
		Direction:    record.Direction,
		TriggerType:  record.TriggerType,
		Status:       status,
		StartedAt:    record.StartedAt,
		FinishedAt:   finishedAt,
		ErrorMessage: errorMessage,
	}
	if err := m.local.SaveHASyncEvent(persistCtx, event); err != nil {
		m.logger.Warn(
			"persist Fleet HA rsync history failed",
			"peer",
			record.Peer,
			"fileSet",
			record.FileSet,
			"error",
			err,
		)
		return
	}
	m.historyMu.Lock()
	m.lastHistoryTime[key] = finishedAt
	m.historyMu.Unlock()
}

func (m *SyncManager) finishSync(startedAt int64, syncErr error) {
	m.updateStatus(func(status *SyncStatus) {
		status.LastStartedAt = startedAt
		if errors.Is(syncErr, context.Canceled) {
			status.State = syncStateIdle
			status.LastError = ""
			return
		}
		if syncErr == nil {
			status.State = syncStateSuccess
			status.LastSucceededAt = time.Now().UTC().UnixMilli()
			status.LastError = ""
			return
		}
		status.State = syncStateError
		status.LastError = syncErr.Error()
	})
}

func syncTriggerForFileSet(fileSet string) string {
	if strings.TrimSpace(fileSet) == "license" {
		return syncTriggerLicense
	}
	return syncTriggerManual
}

func findPeerSyncState(
	states []state.HASyncState,
	peer string,
	fileSet string,
) (state.HASyncState, bool) {
	for _, item := range states {
		if item.Peer == peer && item.FileSet == fileSet {
			return item, true
		}
	}
	return state.HASyncState{}, false
}

func hasSuccessfulPreviousSnapshot(
	states []state.HASyncState,
	peer string,
	fileSet string,
	generation uint64,
) bool {
	if generation <= 1 {
		return false
	}
	item, found := findPeerSyncState(states, peer, fileSet)
	return found &&
		item.Status == syncStateSuccess &&
		item.Revision == strconv.FormatUint(generation-1, 10)
}

func syncStateKey(peer string, fileSet string) string {
	return peer + "\x00" + fileSet
}

func syncPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil || ctx.Err() != nil {
		return context.WithTimeout(context.Background(), 5*time.Second)
	}
	return context.WithTimeout(ctx, 5*time.Second)
}

func (m *SyncManager) logSyncError(scope string, err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		m.logger.Warn("Fleet HA rsync failed", "scope", scope, "error", err)
	}
}

func (m *SyncManager) updateStatus(fn func(*SyncStatus)) {
	m.mu.Lock()
	fn(&m.status)
	m.mu.Unlock()
}

func (m *SyncManager) fileSet(key string) (FileSet, bool) {
	key = strings.TrimSpace(key)
	for _, fileSet := range m.fileSets {
		if fileSet.Key == key {
			return fileSet, true
		}
	}
	return FileSet{}, false
}

func restoreSyncStatus(status *SyncStatus, states []state.HASyncState) {
	var newestErrorAt int64
	for _, item := range states {
		status.LastStartedAt = max(status.LastStartedAt, item.LastStartedAt)
		status.LastSucceededAt = max(status.LastSucceededAt, item.LastSucceededAt)
		if item.Status == syncStateError && item.LastStartedAt >= newestErrorAt {
			newestErrorAt = item.LastStartedAt
			status.LastError = item.ErrorMessage
		}
	}
	switch {
	case newestErrorAt > 0:
		status.State = syncStateError
	case status.LastSucceededAt > 0:
		status.State = syncStateSuccess
	}
}

func (m *SyncManager) saveLocalFileSetHead(ctx context.Context, head fileSetHead) error {
	return m.saveLocalFileSetState(ctx, headStatePrefix, head)
}

func (m *SyncManager) saveLocalAppliedFileSetHead(ctx context.Context, head fileSetHead) error {
	return m.saveLocalFileSetState(ctx, appliedStatePrefix, head)
}

func (m *SyncManager) saveLocalFileSetState(
	ctx context.Context,
	prefix string,
	head fileSetHead,
) error {
	now := time.Now().UTC().UnixMilli()
	input := state.HASyncState{
		Peer:             localSyncPeer,
		FileSet:          prefix + head.FileSet.Key,
		Revision:         strconv.FormatUint(head.Generation, 10),
		ConfigDigest:     head.Inventory.ConfigDigest,
		ContentRevision:  head.Inventory.ContentRevision,
		FileCount:        head.Inventory.FileCount,
		TotalBytes:       head.Inventory.TotalBytes,
		NodeModulesFiles: head.Inventory.NodeModulesFiles,
		NodeModulesBytes: head.Inventory.NodeModulesBytes,
		Status:           syncStateSuccess,
		LastStartedAt:    now,
		LastSucceededAt:  now,
	}
	persistCtx, cancel := syncPersistenceContext(ctx)
	defer cancel()
	if err := m.local.SaveHASyncState(persistCtx, input); err != nil {
		return fmt.Errorf("persist Fleet HA file set %s state %s: %w", head.FileSet.Key, prefix, err)
	}
	return nil
}

func (m *SyncManager) maximumIncomingManifest(
	fileSet string,
) (fileSetManifest, bool, error) {
	var maximum fileSetManifest
	found := false
	for _, manifestFilename := range []string{fileSetManifestFile, fileSetPendingManifestFile} {
		manifest, manifestFound, err := m.newestIncomingManifest(fileSet, manifestFilename)
		if err != nil {
			return fileSetManifest{}, false, err
		}
		if !manifestFound {
			continue
		}
		if !found || manifest.Generation > maximum.Generation {
			maximum = manifest
			found = true
			continue
		}
		if manifest.Generation == maximum.Generation && !manifestsEquivalent(manifest, maximum) {
			return fileSetManifest{}, false, fmt.Errorf(
				"Fleet HA file set %s has conflicting incoming generation %d",
				fileSet,
				manifest.Generation,
			)
		}
	}
	return maximum, found, nil
}

func (m *SyncManager) isInitialBootstrapCandidate(states []state.HASyncState) bool {
	for _, item := range states {
		if item.Peer == localSyncPeer &&
			(strings.HasPrefix(item.FileSet, headStatePrefix) ||
				strings.HasPrefix(item.FileSet, appliedStatePrefix)) {
			return false
		}
	}
	for _, manifestFilename := range []string{fileSetManifestFile, fileSetPendingManifestFile} {
		manifestPattern := filepath.Join(
			m.workDir,
			"incoming",
			"filesets",
			"*",
			"*",
			"*",
			manifestFilename,
		)
		if manifests, err := filepath.Glob(manifestPattern); err != nil || len(manifests) > 0 {
			return false
		}
	}
	localIP, err := interfaceIPv4(m.cfg.HA.Interface, m.cfg.HA.VIP)
	if err != nil {
		return false
	}
	for _, peer := range m.cfg.HA.Sync.Peers {
		peerIP := net.ParseIP(strings.TrimSpace(peer)).To4()
		if peerIP != nil && bytes.Compare(peerIP, localIP) < 0 {
			return false
		}
	}
	return true
}

func interfaceIPv4(interfaceName string, vip string) (net.IP, error) {
	networkInterface, err := net.InterfaceByName(strings.TrimSpace(interfaceName))
	if err != nil {
		return nil, err
	}
	addresses, err := networkInterface.Addrs()
	if err != nil {
		return nil, err
	}
	vipAddress := net.ParseIP(strings.TrimSpace(vip)).To4()
	var selected net.IP
	for _, address := range addresses {
		ip, _, parseErr := net.ParseCIDR(address.String())
		if parseErr != nil {
			continue
		}
		ip = ip.To4()
		if ip == nil || ip.IsLoopback() || (vipAddress != nil && ip.Equal(vipAddress)) {
			continue
		}
		if selected == nil || bytes.Compare(ip, selected) < 0 {
			selected = append(net.IP(nil), ip...)
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("Fleet HA interface %s has no usable IPv4 address", interfaceName)
	}
	return selected, nil
}

func latestLocalFileSetHead(states []state.HASyncState, key string) (state.HASyncState, bool) {
	var result state.HASyncState
	var resultGeneration uint64
	found := false
	for _, item := range states {
		if item.Peer != localSyncPeer ||
			(item.FileSet != headStatePrefix+key && item.FileSet != appliedStatePrefix+key) ||
			item.Status != syncStateSuccess {
			continue
		}
		generation, err := strconv.ParseUint(item.Revision, 10, 64)
		if err != nil || generation == 0 || (found && generation < resultGeneration) {
			continue
		}
		if found && generation == resultGeneration &&
			item.FileSet != headStatePrefix+key {
			continue
		}
		result = item
		resultGeneration = generation
		found = true
	}
	return result, found
}

func syncStateMatchesInventory(item state.HASyncState, inventory fileSetInventory) bool {
	return item.ConfigDigest != "" &&
		item.ContentRevision != "" &&
		item.ConfigDigest == inventory.ConfigDigest &&
		item.ContentRevision == inventory.ContentRevision &&
		item.FileCount == inventory.FileCount &&
		item.TotalBytes == inventory.TotalBytes &&
		item.NodeModulesFiles == inventory.NodeModulesFiles &&
		item.NodeModulesBytes == inventory.NodeModulesBytes
}

func manifestMatchesInventory(manifest fileSetManifest, inventory fileSetInventory) bool {
	return manifest.ConfigDigest == inventory.ConfigDigest &&
		manifest.ContentRevision == inventory.ContentRevision &&
		manifest.FileCount == inventory.FileCount &&
		manifest.TotalBytes == inventory.TotalBytes &&
		manifest.NodeModulesFiles == inventory.NodeModulesFiles &&
		manifest.NodeModulesBytes == inventory.NodeModulesBytes
}

func (m *SyncManager) incomingFileSetStatus(
	fileSet FileSet,
	head state.HASyncState,
) (incomingFileSetStatus, error) {
	headGeneration, err := strconv.ParseUint(head.Revision, 10, 64)
	if err != nil || headGeneration == 0 {
		return incomingFileSetStatus{}, fmt.Errorf("Fleet HA file set %s head generation is invalid", fileSet.Key)
	}
	committed, committedFound, err := m.newestIncomingManifest(fileSet.Key, fileSetManifestFile)
	if err != nil {
		return incomingFileSetStatus{}, err
	}
	pending, pendingFound, err := m.newestIncomingManifest(fileSet.Key, fileSetPendingManifestFile)
	if err != nil {
		return incomingFileSetStatus{}, err
	}
	for _, manifest := range []fileSetManifest{committed, pending} {
		if manifest.Generation > 0 && manifest.ConfigDigest != head.ConfigDigest {
			return incomingFileSetStatus{}, fmt.Errorf(
				"Fleet HA file set %s has an incoming snapshot with incompatible configuration",
				fileSet.Key,
			)
		}
	}
	result := incomingFileSetStatus{}
	if committedFound {
		if committed.Generation > headGeneration {
			result.CommittedNewer = true
		}
		if committed.Generation == headGeneration &&
			!manifestMatchesSyncState(committed, head) {
			return incomingFileSetStatus{}, fmt.Errorf(
				"Fleet HA file set %s has a conflicting committed generation %d",
				fileSet.Key,
				headGeneration,
			)
		}
	}
	if !pendingFound {
		return result, nil
	}
	if committedFound &&
		committed.Generation == pending.Generation &&
		manifestsEquivalent(committed, pending) {
		return result, nil
	}
	if pending.Generation > headGeneration {
		result.Pending = &pending
		return result, nil
	}
	if pending.Generation == headGeneration && !manifestMatchesSyncState(pending, head) {
		return incomingFileSetStatus{}, fmt.Errorf(
			"Fleet HA file set %s has a conflicting pending generation %d",
			fileSet.Key,
			headGeneration,
		)
	}
	return result, nil
}

func (m *SyncManager) newestIncomingManifest(
	fileSet string,
	manifestFilename string,
) (fileSetManifest, bool, error) {
	pattern := filepath.Join(m.workDir, "incoming", "filesets", "*", fileSet, "*", manifestFilename)
	filenames, err := filepath.Glob(pattern)
	if err != nil {
		return fileSetManifest{}, false, fmt.Errorf("scan incoming Fleet HA file set %s: %w", fileSet, err)
	}
	var newest fileSetManifest
	found := false
	for _, filename := range filenames {
		body, readErr := os.ReadFile(filename)
		if readErr != nil {
			return fileSetManifest{}, false, readErr
		}
		var manifest fileSetManifest
		if jsonErr := json.Unmarshal(body, &manifest); jsonErr != nil {
			return fileSetManifest{}, false, jsonErr
		}
		if manifest.FileSet != fileSet || manifest.Generation == 0 {
			continue
		}
		if !found || manifest.Generation > newest.Generation {
			newest = manifest
			found = true
			continue
		}
		if manifest.Generation == newest.Generation && !manifestsEquivalent(manifest, newest) {
			return fileSetManifest{}, false, fmt.Errorf(
				"Fleet HA file set %s has conflicting incoming generation %d",
				fileSet,
				manifest.Generation,
			)
		}
	}
	return newest, found, nil
}

func manifestsEquivalent(left, right fileSetManifest) bool {
	return left.Generation == right.Generation &&
		left.ConfigDigest == right.ConfigDigest &&
		left.ContentRevision == right.ContentRevision &&
		left.FileCount == right.FileCount &&
		left.TotalBytes == right.TotalBytes &&
		left.NodeModulesFiles == right.NodeModulesFiles &&
		left.NodeModulesBytes == right.NodeModulesBytes
}

func manifestMatchesSyncState(manifest fileSetManifest, head state.HASyncState) bool {
	return manifest.ConfigDigest == head.ConfigDigest &&
		manifest.ContentRevision == head.ContentRevision &&
		manifest.FileCount == head.FileCount &&
		manifest.TotalBytes == head.TotalBytes &&
		manifest.NodeModulesFiles == head.NodeModulesFiles &&
		manifest.NodeModulesBytes == head.NodeModulesBytes
}

func (m *SyncManager) pendingManifestGrace() time.Duration {
	grace := 3 * m.cfg.HA.Sync.Interval
	if m.cfg.HA.HealthFailureWindow > grace {
		grace = m.cfg.HA.HealthFailureWindow
	}
	if grace <= 0 {
		grace = 30 * time.Second
	}
	return grace
}

func (m *SyncManager) pendingManifestAge(manifest fileSetManifest) time.Duration {
	createdAt := time.UnixMilli(manifest.CreatedAt)
	if manifest.CreatedAt <= 0 || createdAt.After(time.Now()) {
		return 0
	}
	return time.Since(createdAt)
}

func (m *SyncManager) outboundReady(ctx context.Context) error {
	states, err := m.local.LoadHASyncStates(ctx)
	if err != nil {
		return fmt.Errorf("load Fleet HA outbound heads: %w", err)
	}
	for _, fileSet := range m.fileSets {
		if _, statErr := os.Stat(fileSet.Source); errors.Is(statErr, os.ErrNotExist) && !fileSet.Required {
			continue
		}
		head, found := latestLocalFileSetHead(states, fileSet.Key)
		if !found {
			return fmt.Errorf("Fleet HA file set %s has no synchronized head", fileSet.Key)
		}
		incoming, incomingErr := m.incomingFileSetStatus(fileSet, head)
		if incomingErr != nil {
			return incomingErr
		}
		if incoming.CommittedNewer {
			return fmt.Errorf(
				"Fleet HA file set %s has a newer committed snapshot that requires controlled apply",
				fileSet.Key,
			)
		}
		if incoming.Pending != nil {
			if m.pendingManifestAge(*incoming.Pending) < m.pendingManifestGrace() {
				return fmt.Errorf(
					"Fleet HA file set %s generation %d is incomplete; outbound synchronization remains fenced",
					fileSet.Key,
					incoming.Pending.Generation,
				)
			}
			if incoming.Pending.Generation == ^uint64(0) {
				return fmt.Errorf(
					"Fleet HA file set %s pending generation cannot be superseded",
					fileSet.Key,
				)
			}
			inventory, inventoryErr := inspectFileSet(fileSet)
			if inventoryErr != nil {
				return fmt.Errorf(
					"inspect Fleet HA file set %s before superseding abandoned snapshot: %w",
					fileSet.Key,
					inventoryErr,
				)
			}
			if !syncStateMatchesInventory(head, inventory) {
				return fmt.Errorf(
					"Fleet HA file set %s changed while generation %d was incomplete; outbound synchronization remains fenced",
					fileSet.Key,
					incoming.Pending.Generation,
				)
			}
			headGeneration, parseErr := strconv.ParseUint(head.Revision, 10, 64)
			if parseErr != nil || headGeneration == 0 {
				return fmt.Errorf("Fleet HA file set %s head generation is invalid", fileSet.Key)
			}
			supersedingGeneration := incoming.Pending.Generation + 1
			if err := m.saveLocalFileSetHead(ctx, fileSetHead{
				FileSet:          fileSet,
				Generation:       supersedingGeneration,
				ParentGeneration: headGeneration,
				Inventory:        inventory,
			}); err != nil {
				return fmt.Errorf(
					"supersede abandoned Fleet HA file set %s generation %d: %w",
					fileSet.Key,
					incoming.Pending.Generation,
					err,
				)
			}
			m.logger.Warn(
				"superseded abandoned Fleet HA pending snapshot",
				"fileSet",
				fileSet.Key,
				"pendingGeneration",
				incoming.Pending.Generation,
				"newGeneration",
				supersedingGeneration,
			)
		}
	}
	return nil
}

func inspectFileSet(fileSet FileSet) (fileSetInventory, error) {
	return inspectFileSetRoot(fileSet, fileSet.Source)
}

func inspectFileSetRoot(fileSet FileSet, root string) (fileSetInventory, error) {
	configDigest, err := fileSetConfigDigest(fileSet)
	if err != nil {
		return fileSetInventory{}, err
	}
	includes, err := compileRsyncGlobs(fileSet.Includes)
	if err != nil {
		return fileSetInventory{}, err
	}
	excludes, err := compileRsyncGlobs(fileSet.Excludes)
	if err != nil {
		return fileSetInventory{}, err
	}
	hasher := sha256.New()
	inventory := fileSetInventory{ConfigDigest: configDigest}
	err = filepath.WalkDir(root, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == root {
			return nil
		}
		relative, relativeErr := filepath.Rel(root, filename)
		if relativeErr != nil {
			return relativeErr
		}
		relative = filepath.ToSlash(relative)
		if matchesRsyncGlobs(relative, entry.IsDir(), excludes) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if len(includes) > 0 && !matchesRsyncGlobs(relative, false, includes) {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		linkTarget := ""
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, infoErr = os.Readlink(filename)
			if infoErr != nil {
				return infoErr
			}
		}
		_, _ = fmt.Fprintf(
			hasher,
			"%s\x00%d\x00%d\x00%d\x00%s\x00",
			relative,
			info.Size(),
			info.ModTime().UnixNano(),
			uint32(info.Mode()),
			linkTarget,
		)
		inventory.FileCount++
		inventory.TotalBytes += info.Size()
		if pathContainsSegment(relative, "node_modules") {
			inventory.NodeModulesFiles++
			inventory.NodeModulesBytes += info.Size()
		}
		return nil
	})
	if err != nil {
		return fileSetInventory{}, err
	}
	inventory.ContentRevision = fmt.Sprintf("%x", hasher.Sum(nil))
	return inventory, nil
}

func fileSetConfigDigest(fileSet FileSet) (string, error) {
	body, err := json.Marshal(fileSet)
	if err != nil {
		return "", fmt.Errorf("encode Fleet HA file set configuration: %w", err)
	}
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:]), nil
}

type rsyncGlob struct {
	expression *regexp.Regexp
}

func compileRsyncGlobs(patterns []string) ([]rsyncGlob, error) {
	result := make([]rsyncGlob, 0, len(patterns))
	for _, pattern := range patterns {
		expression, err := regexp.Compile(rsyncGlobExpression(pattern))
		if err != nil {
			return nil, fmt.Errorf("compile Fleet HA file-set filter %q: %w", pattern, err)
		}
		result = append(result, rsyncGlob{expression: expression})
	}
	return result, nil
}

func rsyncGlobExpression(pattern string) string {
	pattern = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(pattern)), "/")
	var output strings.Builder
	output.WriteString("^")
	if !strings.Contains(pattern, "/") {
		output.WriteString("(?:.*/)?")
	}
	for index := 0; index < len(pattern); {
		switch {
		case strings.HasPrefix(pattern[index:], "**/"):
			output.WriteString("(?:.*/)?")
			index += 3
		case strings.HasPrefix(pattern[index:], "**"):
			output.WriteString(".*")
			index += 2
		case pattern[index] == '*':
			output.WriteString("[^/]*")
			index++
		case pattern[index] == '?':
			output.WriteString("[^/]")
			index++
		default:
			output.WriteString(regexp.QuoteMeta(string(pattern[index])))
			index++
		}
	}
	output.WriteString("$")
	return output.String()
}

func matchesRsyncGlobs(relative string, directory bool, patterns []rsyncGlob) bool {
	relative = filepath.ToSlash(relative)
	for _, pattern := range patterns {
		if pattern.expression.MatchString(relative) ||
			(directory && pattern.expression.MatchString(relative+"/")) {
			return true
		}
	}
	return false
}

func pathContainsSegment(filename, segment string) bool {
	for _, item := range strings.Split(filepath.ToSlash(filename), "/") {
		if item == segment {
			return true
		}
	}
	return false
}

func loadFileSets(filename string) ([]FileSet, error) {
	body, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read Fleet HA file sets: %w", err)
	}
	var input FileSetConfig
	if err := json.Unmarshal(body, &input); err != nil {
		return nil, fmt.Errorf("decode Fleet HA file sets: %w", err)
	}
	seen := make(map[string]struct{})
	for index := range input.FileSets {
		fileSet := &input.FileSets[index]
		fileSet.Key = strings.TrimSpace(fileSet.Key)
		fileSet.Source = filepath.Clean(strings.TrimSpace(fileSet.Source))
		if fileSet.Key == "" || strings.ContainsAny(fileSet.Key, `/\`) {
			return nil, errors.New("Fleet HA file set key is invalid")
		}
		if !path.IsAbs(filepath.ToSlash(fileSet.Source)) || !isSafeRemotePath(fileSet.Source) {
			return nil, fmt.Errorf("Fleet HA file set source must be a safe absolute path: %s", fileSet.Key)
		}
		if _, found := seen[fileSet.Key]; found {
			return nil, fmt.Errorf("duplicate Fleet HA file set key: %s", fileSet.Key)
		}
		seen[fileSet.Key] = struct{}{}
		for _, pattern := range append(append([]string(nil), fileSet.Includes...), fileSet.Excludes...) {
			if strings.ContainsAny(pattern, "\r\n") {
				return nil, fmt.Errorf("Fleet HA file set %s contains an invalid filter", fileSet.Key)
			}
		}
	}
	if len(input.FileSets) == 0 {
		return nil, errors.New("Fleet HA file set list must not be empty")
	}
	return input.FileSets, nil
}

func probeWritableDirectory(directory string) error {
	probe, err := os.CreateTemp(directory, ".tier0-ha-write-probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if closeErr := probe.Close(); closeErr != nil {
		_ = os.Remove(name)
		return closeErr
	}
	return os.Remove(name)
}

func isSafeRemotePath(value string) bool {
	value = filepath.ToSlash(value)
	if !path.IsAbs(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '/' || character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func runCommand(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	command.Stdout = nil
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		message := strings.TrimSpace(stderr.String())
		if len(message) > 1024 {
			message = message[:1024]
		}
		if message == "" {
			return fmt.Errorf("%s failed: %w", name, err)
		}
		return fmt.Errorf("%s failed: %w: %s", name, err, message)
	}
	return nil
}
