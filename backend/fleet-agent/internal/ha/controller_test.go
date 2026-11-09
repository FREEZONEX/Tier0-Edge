package ha

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"backend/fleet-agent/internal/config"
	fleetcontainer "backend/fleet-agent/internal/container"
	"backend/fleet-agent/internal/state"
	"backend/internal/fleetprotocol"

	"github.com/google/uuid"
)

func TestPromotionUsesKeepalivedAndLocalStateWithoutLease(t *testing.T) {
	syncer := &controllerSync{}
	group := &controllerGroup{}
	controller := newTestController(t, group, syncer, controllerVIP{owned: true})

	if err := controller.Transition(context.Background(), TransitionRequest{
		KeepalivedState: KeepalivedMaster,
		TransitionID:    uuid.NewString(),
	}); err != nil {
		t.Fatal(err)
	}
	status := controller.Status()
	if status.Role != RoleActive || !status.VIPOwned {
		t.Fatalf("status after promotion = %+v", status)
	}
	if group.startCount() != 1 || !syncer.triggered {
		t.Fatalf("promotion did not start the group and trigger rsync: starts=%d triggered=%v", group.startCount(), syncer.triggered)
	}
	if !syncer.outbound {
		t.Fatal("outbound rsync remained fenced after a successful promotion")
	}
}

func TestPromotionStopsWhenIncomingStateCannotBePrepared(t *testing.T) {
	syncer := &controllerSync{prepareErr: errors.New("invalid incoming app bundle")}
	group := &controllerGroup{}
	controller := newTestController(t, group, syncer, controllerVIP{owned: true})

	err := controller.Transition(context.Background(), TransitionRequest{
		KeepalivedState: KeepalivedMaster,
		TransitionID:    uuid.NewString(),
	})
	if err == nil {
		t.Fatal("promotion unexpectedly succeeded")
	}
	status := controller.Status()
	if status.Role != RoleFaulted || status.LastErrorCode != reasonSyncNotReady {
		t.Fatalf("status after prepare failure = %+v", status)
	}
	if group.startCount() != 0 {
		t.Fatal("business group started before synchronized state was prepared")
	}
	if syncer.outbound {
		t.Fatal("outbound rsync was enabled after a failed promotion")
	}
}

func TestActiveMemberReturnsToStandbyWhenVIPIsLost(t *testing.T) {
	group := &controllerGroup{}
	syncer := &controllerSync{}
	controller := newTestController(t, group, syncer, controllerVIP{owned: false})
	controller.update(func(status *Status) {
		status.Role = RoleActive
		status.VIPOwned = true
	})

	controller.tick(context.Background())

	status := controller.Status()
	if status.Role != RoleStandby || status.LastErrorCode != "" {
		t.Fatalf("status after VIP loss = %+v", status)
	}
	if group.stopCount() != 1 {
		t.Fatalf("business group stop count = %d, want 1", group.stopCount())
	}
	if !syncer.canceled {
		t.Fatal("active rsync was not canceled during demotion")
	}
	if syncer.outbound {
		t.Fatal("outbound rsync remained enabled after demotion")
	}
}

func TestPromotionStopsWhenSynchronizedHeadIsNotReady(t *testing.T) {
	syncer := &controllerSync{promotionErr: errors.New("apps head is stale")}
	group := &controllerGroup{}
	controller := newTestController(t, group, syncer, controllerVIP{owned: true})

	err := controller.Transition(context.Background(), TransitionRequest{
		KeepalivedState: KeepalivedMaster,
		TransitionID:    uuid.NewString(),
	})
	if err == nil {
		t.Fatal("promotion unexpectedly succeeded with a stale synchronized head")
	}
	if group.startCount() != 0 || syncer.outbound {
		t.Fatalf(
			"stale member crossed the promotion barrier: starts=%d outbound=%v",
			group.startCount(),
			syncer.outbound,
		)
	}
}

func TestInitialPromotionBootstrapsOnlyAfterVIPOwnership(t *testing.T) {
	syncer := &controllerSync{promotionErr: errInitialSyncBootstrapRequired}
	group := &controllerGroup{}
	controller := newTestController(t, group, syncer, controllerVIP{owned: true})

	if err := controller.Transition(context.Background(), TransitionRequest{
		KeepalivedState: KeepalivedMaster,
		TransitionID:    uuid.NewString(),
	}); err != nil {
		t.Fatal(err)
	}
	if syncer.bootstrapCount != 1 || group.startCount() != 1 || !syncer.outbound {
		t.Fatalf(
			"initial bootstrap did not cross the promotion barrier correctly: bootstraps=%d starts=%d outbound=%v",
			syncer.bootstrapCount,
			group.startCount(),
			syncer.outbound,
		)
	}
}

func TestPromotionCanServePreviousCompleteSnapshotWhileOutboundIsFenced(t *testing.T) {
	syncer := &controllerSync{outboundErr: errors.New("newer generation is incomplete")}
	group := &controllerGroup{}
	controller := newTestController(t, group, syncer, controllerVIP{owned: true})

	if err := controller.Transition(context.Background(), TransitionRequest{
		KeepalivedState: KeepalivedMaster,
		TransitionID:    uuid.NewString(),
	}); err != nil {
		t.Fatal(err)
	}
	status := controller.Status()
	if status.Role != RoleActive || group.startCount() != 1 {
		t.Fatalf("degraded promotion did not start the previous complete snapshot: %+v", status)
	}
	if syncer.outbound || syncer.triggered {
		t.Fatalf(
			"degraded promotion allowed reverse synchronization: outbound=%v triggered=%v",
			syncer.outbound,
			syncer.triggered,
		)
	}
}

func TestEligibilityWaitsForStartupFencing(t *testing.T) {
	controller := newUnfencedTestController(t, &controllerGroup{}, &controllerSync{}, controllerVIP{owned: true})
	if eligibility := controller.Eligibility(context.Background()); eligibility.Eligible ||
		eligibility.ReasonCode != reasonStartupFencing {
		t.Fatalf("eligibility before startup fencing = %+v", eligibility)
	}
	if !controller.tryStartupFencing(context.Background()) {
		t.Fatal("startup fencing unexpectedly failed")
	}
	if eligibility := controller.Eligibility(context.Background()); !eligibility.Eligible {
		t.Fatalf("eligibility after startup fencing = %+v", eligibility)
	}
}

func TestStartupPreparationFailureBlocksBootstrapUntilRetrySucceeds(t *testing.T) {
	syncer := &controllerSync{prepareErr: errors.New("incomplete incoming snapshot")}
	controller := newUnfencedTestController(t, &controllerGroup{}, syncer, controllerVIP{owned: true})
	if !controller.tryStartupFencing(context.Background()) {
		t.Fatal("startup fencing unexpectedly failed")
	}
	if controller.prepareStartupSynchronizedState(context.Background(), true) {
		t.Fatal("startup preparation unexpectedly succeeded")
	}
	if syncer.bootstrapCount != 0 {
		t.Fatal("bootstrap ran after synchronized-state preparation failed")
	}
	eligibility := controller.Eligibility(context.Background())
	if eligibility.Eligible || !eligibility.ShouldReleaseVIP ||
		eligibility.ReasonCode != reasonSyncNotReady {
		t.Fatalf("startup preparation failure did not fence the member: %+v", eligibility)
	}

	syncer.prepareErr = nil
	if !controller.prepareStartupSynchronizedState(context.Background(), true) {
		t.Fatal("startup preparation retry did not recover")
	}
	if syncer.bootstrapCount != 1 || controller.isStartupSyncBlocked() {
		t.Fatalf(
			"startup synchronization barrier was not cleared: bootstraps=%d blocked=%v",
			syncer.bootstrapCount,
			controller.isStartupSyncBlocked(),
		)
	}
}

func TestOptionalStoppedContainerDoesNotReleaseVIP(t *testing.T) {
	group := &controllerGroup{
		status: fleetcontainer.HAGroupStatus{
			Name:    "active",
			Total:   2,
			Running: 1,
			Stopped: 1,
		},
	}
	controller := newTestController(t, group, &controllerSync{}, controllerVIP{owned: true})
	controller.healthFailureSince = time.Now().Add(-time.Second)
	controller.update(func(status *Status) {
		status.Role = RoleActive
		status.VIPOwned = true
	})

	controller.tick(context.Background())

	status := controller.Status()
	if status.Role != RoleActive || status.ShouldReleaseVIP {
		t.Fatalf("optional stopped container changed active decision: %+v", status)
	}
}

func TestRequiredContainerFailureReleasesVIPAfterGraceWindow(t *testing.T) {
	group := &controllerGroup{
		status: fleetcontainer.HAGroupStatus{
			Name:            "active",
			Total:           1,
			Stopped:         1,
			RequiredStopped: 1,
		},
	}
	controller := newTestController(t, group, &controllerSync{}, controllerVIP{owned: true})
	controller.healthFailureSince = time.Now().Add(-time.Second)
	controller.update(func(status *Status) {
		status.Role = RoleActive
		status.VIPOwned = true
	})

	controller.tick(context.Background())

	status := controller.Status()
	if status.Role != RoleFaulted || !status.ShouldReleaseVIP || status.Decision != DecisionRelease {
		t.Fatalf("required container failure did not request VIP release: %+v", status)
	}
	if status.FailureSince == 0 {
		t.Fatalf("required container failure timestamp was not retained: %+v", status)
	}
}

func TestServiceGroupFaultAutomaticallyRejoinsAfterVIPRelease(t *testing.T) {
	group := &controllerGroup{}
	controller := newTestController(t, group, &controllerSync{}, controllerVIP{owned: false})
	controller.update(func(status *Status) {
		status.Role = RoleFaulted
		status.ShouldReleaseVIP = true
		status.Decision = DecisionRelease
		status.LastErrorCode = reasonServiceGroupFailed
		status.LastTransitionAt = time.Now().Add(-time.Second).UnixMilli()
	})

	controller.tick(context.Background())

	status := controller.Status()
	if status.Role != RoleStandby || status.ShouldReleaseVIP || status.LastErrorCode != "" {
		t.Fatalf("service-group fault did not rejoin as standby: %+v", status)
	}
	if group.stopCount() != 1 {
		t.Fatalf("business group stop count = %d, want 1", group.stopCount())
	}
}

func TestServiceGroupFaultDoesNotRecoverWhileVIPIsStillOwned(t *testing.T) {
	group := &controllerGroup{}
	controller := newTestController(t, group, &controllerSync{}, controllerVIP{owned: true})
	controller.update(func(status *Status) {
		status.Role = RoleFaulted
		status.ShouldReleaseVIP = true
		status.Decision = DecisionRelease
		status.LastErrorCode = reasonServiceGroupFailed
		status.LastTransitionAt = time.Now().Add(-time.Second).UnixMilli()
	})

	controller.tick(context.Background())

	if status := controller.Status(); status.Role != RoleFaulted || !status.ShouldReleaseVIP {
		t.Fatalf("member recovered before releasing VIP: %+v", status)
	}
	if group.stopCount() != 0 {
		t.Fatalf("business group was touched before VIP release: stops=%d", group.stopCount())
	}
}

func newTestController(
	t *testing.T,
	group ServiceGroup,
	syncer SyncGate,
	vip VIPChecker,
) *Controller {
	controller := newUnfencedTestController(t, group, syncer, vip)
	controller.startupFenced = true
	return controller
}

func newUnfencedTestController(
	t *testing.T,
	group ServiceGroup,
	syncer SyncGate,
	vip VIPChecker,
) *Controller {
	t.Helper()
	controller, err := NewController(config.Config{
		InstallationID: "installation-a",
		HA: config.HAConfig{
			Enabled:             true,
			MemberID:            uuid.NewString(),
			Interface:           "eth0",
			VIP:                 "192.0.2.10",
			ServiceGroup:        "active",
			ReconcileInterval:   50 * time.Millisecond,
			HealthFailureWindow: 100 * time.Millisecond,
			OperationTimeout:    time.Second,
		},
	}, controllerLocal{}, group, controllerMQTT{}, syncer, vip, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return controller
}

type controllerSync struct {
	prepareErr     error
	acquisitionErr error
	promotionErr   error
	triggered      bool
	canceled       bool
	outbound       bool
	bootstrapCount int
	outboundErr    error
}

func (s *controllerSync) Ready(context.Context) error { return nil }
func (s *controllerSync) Bootstrap(context.Context) error {
	s.bootstrapCount++
	if errors.Is(s.promotionErr, errInitialSyncBootstrapRequired) {
		s.promotionErr = nil
	}
	return nil
}
func (s *controllerSync) Prepare(context.Context) error          { return s.prepareErr }
func (s *controllerSync) AcquisitionReady(context.Context) error { return s.acquisitionErr }
func (s *controllerSync) PromotionReady(context.Context) error   { return s.promotionErr }
func (s *controllerSync) SetOutboundEnabled(_ context.Context, enabled bool) error {
	if enabled && s.outboundErr != nil {
		s.outbound = false
		return s.outboundErr
	}
	s.outbound = enabled
	if !enabled {
		s.canceled = true
	}
	return nil
}
func (s *controllerSync) Trigger() { s.triggered = true }
func (s *controllerSync) Cancel()  { s.canceled = true }
func (s *controllerSync) Status() SyncStatus {
	return SyncStatus{Enabled: true, State: syncStateIdle}
}

type controllerGroup struct {
	mu      sync.Mutex
	started int
	stopped int
	status  fleetcontainer.HAGroupStatus
}

func (g *controllerGroup) HAReady(context.Context) error { return nil }
func (g *controllerGroup) StartHAGroup(context.Context, string, time.Duration) (fleetcontainer.HAGroupStatus, error) {
	g.mu.Lock()
	g.started++
	g.mu.Unlock()
	return fleetcontainer.HAGroupStatus{Name: "active", Running: 1}, nil
}
func (g *controllerGroup) StopHAGroup(context.Context, string, time.Duration) (fleetcontainer.HAGroupStatus, error) {
	g.mu.Lock()
	g.stopped++
	g.mu.Unlock()
	return fleetcontainer.HAGroupStatus{Name: "active", Stopped: 1}, nil
}
func (g *controllerGroup) HAGroupStatus(context.Context, string) (fleetcontainer.HAGroupStatus, error) {
	if g.status.Name != "" {
		return g.status, nil
	}
	return fleetcontainer.HAGroupStatus{Name: "active", Running: 1}, nil
}
func (g *controllerGroup) HAGroupSnapshot(context.Context, string, string) (fleetprotocol.HAContainerSnapshot, error) {
	return fleetprotocol.HAContainerSnapshot{Group: "active"}, nil
}
func (g *controllerGroup) startCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.started
}
func (g *controllerGroup) stopCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stopped
}

type controllerLocal struct{}

func (controllerLocal) SaveHALocalState(context.Context, state.HALocalState) error { return nil }

type controllerMQTT struct{}

func (controllerMQTT) SetEnabled(bool) {}

type controllerVIP struct {
	owned bool
}

func (v controllerVIP) Owns(context.Context, string, string) (bool, error) { return v.owned, nil }
