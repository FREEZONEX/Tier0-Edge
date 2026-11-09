package ha

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"backend/fleet-agent/internal/config"
	fleetcontainer "backend/fleet-agent/internal/container"
	"backend/fleet-agent/internal/state"
	"backend/internal/fleetprotocol"

	"github.com/google/uuid"
)

const (
	reasonDockerUnavailable  = "FLEET_HA_DOCKER_UNAVAILABLE"
	reasonSyncNotReady       = "FLEET_HA_SYNC_NOT_READY"
	reasonVIPNotOwned        = "FLEET_HA_VIP_NOT_OWNED"
	reasonServiceGroupFailed = "FLEET_HA_SERVICE_GROUP_FAILED"
	reasonStartupFencing     = "FLEET_HA_STARTUP_FENCING"
	reasonKeepalivedFault    = "FLEET_HA_KEEPALIVED_FAULT"
)

type ServiceGroup interface {
	HAReady(context.Context) error
	StartHAGroup(context.Context, string, time.Duration) (fleetcontainer.HAGroupStatus, error)
	StopHAGroup(context.Context, string, time.Duration) (fleetcontainer.HAGroupStatus, error)
	HAGroupStatus(context.Context, string) (fleetcontainer.HAGroupStatus, error)
	HAGroupSnapshot(context.Context, string, string) (fleetprotocol.HAContainerSnapshot, error)
}

type MQTTGate interface {
	SetEnabled(bool)
}

type SyncGate interface {
	Ready(context.Context) error
	Bootstrap(context.Context) error
	Prepare(context.Context) error
	AcquisitionReady(context.Context) error
	PromotionReady(context.Context) error
	SetOutboundEnabled(context.Context, bool) error
	Trigger()
	Cancel()
	Status() SyncStatus
}

type NoopSyncGate struct{}

func (NoopSyncGate) Ready(context.Context) error                    { return nil }
func (NoopSyncGate) Bootstrap(context.Context) error                { return nil }
func (NoopSyncGate) Prepare(context.Context) error                  { return nil }
func (NoopSyncGate) AcquisitionReady(context.Context) error         { return nil }
func (NoopSyncGate) PromotionReady(context.Context) error           { return nil }
func (NoopSyncGate) SetOutboundEnabled(context.Context, bool) error { return nil }
func (NoopSyncGate) Trigger()                                       {}
func (NoopSyncGate) Cancel()                                        {}
func (NoopSyncGate) Status() SyncStatus {
	return SyncStatus{State: "disabled"}
}

type LocalStateStore interface {
	SaveHALocalState(context.Context, state.HALocalState) error
}

type Controller struct {
	cfg    config.Config
	local  LocalStateStore
	group  ServiceGroup
	mqtt   MQTTGate
	syncer SyncGate
	vip    VIPChecker
	logger *slog.Logger

	opMu sync.Mutex
	mu   sync.RWMutex

	state              Status
	healthFailureSince time.Time
	startupFenced      bool
	startupSyncBlocked bool
}

func NewController(
	cfg config.Config,
	local LocalStateStore,
	group ServiceGroup,
	mqtt MQTTGate,
	syncer SyncGate,
	vip VIPChecker,
	logger *slog.Logger,
) (*Controller, error) {
	if !cfg.HA.Enabled {
		return nil, errors.New("Fleet HA is not enabled")
	}
	if local == nil || group == nil || mqtt == nil {
		return nil, errors.New("Fleet HA dependencies are incomplete")
	}
	if syncer == nil {
		syncer = NoopSyncGate{}
	}
	if vip == nil {
		vip = NetworkVIPChecker{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{
		cfg:    cfg,
		local:  local,
		group:  group,
		mqtt:   mqtt,
		syncer: syncer,
		vip:    vip,
		logger: logger,
		state: Status{
			Enabled:  true,
			MemberID: cfg.HA.MemberID,
			Role:     RoleStandby,
			Decision: DecisionBlockAcquire,
			ServiceGroup: ServiceGroupStatus{
				Name:     cfg.HA.ServiceGroup,
				Expected: "stopped",
			},
		},
	}, nil
}

func (c *Controller) Run(ctx context.Context) {
	c.mqtt.SetEnabled(false)
	_ = c.syncer.SetOutboundEnabled(ctx, false)
	ownedAtStartup, _ := c.vip.Owns(ctx, c.cfg.HA.Interface, c.cfg.HA.VIP)
	c.opMu.Lock()
	fenced := c.tryStartupFencing(ctx)
	if fenced {
		c.prepareStartupSynchronizedState(ctx, ownedAtStartup)
	}
	c.opMu.Unlock()
	if !c.isStartupSyncBlocked() {
		c.reconcileVIP(ctx)
	}

	ticker := time.NewTicker(c.cfg.HA.ReconcileInterval)
	defer ticker.Stop()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), c.cfg.HA.OperationTimeout)
		defer cancel()
		if err := c.demote(shutdownCtx, KeepalivedStop, ""); err != nil {
			c.logger.Error("Fleet HA shutdown fencing failed", "error", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

func (c *Controller) Eligibility(ctx context.Context) Eligibility {
	canAcquire, reason := c.evaluateEligibility(ctx)
	current := c.Status()
	eligible := canAcquire
	decision := DecisionBlockAcquire
	shouldRelease := current.ShouldReleaseVIP
	if shouldRelease {
		eligible = false
		decision = DecisionRelease
		if current.DecisionReason != "" {
			reason = current.DecisionReason
		}
	} else if current.Role == RoleActive {
		eligible = true
		decision = DecisionKeep
		reason = ""
	} else if canAcquire {
		decision = DecisionKeep
		reason = ""
	}
	return Eligibility{
		Enabled:          true,
		Eligible:         eligible,
		MemberID:         c.cfg.HA.MemberID,
		Role:             current.Role,
		CanAcquireVIP:    canAcquire,
		ShouldReleaseVIP: shouldRelease,
		Decision:         decision,
		ReasonCode:       reason,
		FailureSince:     current.FailureSince,
		GraceRemainingMS: current.GraceRemainingMS,
		ServiceGroup:     current.ServiceGroup,
		CheckedAt:        time.Now().UTC().UnixMilli(),
	}
}

// Reconcile refreshes the local HA role from the actual VIP ownership.
// Keepalived notifications use this as a latency optimization; the periodic
// controller loop remains the source of eventual reconciliation.
func (c *Controller) Reconcile(ctx context.Context) {
	c.reconcileVIP(ctx)
}

func (c *Controller) Status() Status {
	c.mu.RLock()
	out := c.state
	out.ServiceGroup.Failures = append([]string(nil), c.state.ServiceGroup.Failures...)
	c.mu.RUnlock()
	out.Sync = c.syncer.Status()
	return out
}

func (c *Controller) Containers(ctx context.Context) (fleetprotocol.HAContainerSnapshot, error) {
	expected := "stopped"
	role := c.Status().Role
	if role == RoleActive || role == RolePromoting {
		expected = "running"
	}
	return c.group.HAGroupSnapshot(ctx, c.cfg.HA.ServiceGroup, expected)
}

func (c *Controller) Transition(ctx context.Context, request TransitionRequest) error {
	switch request.KeepalivedState {
	case KeepalivedMaster, KeepalivedBackup, KeepalivedFault, KeepalivedStop:
	default:
		return errors.New("invalid Keepalived state")
	}
	request.TransitionID = strings.TrimSpace(request.TransitionID)
	if request.TransitionID == "" {
		return errors.New("transitionId is required")
	}
	if _, err := uuid.Parse(request.TransitionID); err != nil {
		return errors.New("transitionId must be a UUID")
	}

	c.opMu.Lock()
	defer c.opMu.Unlock()
	if c.Status().LastTransitionID == request.TransitionID {
		return nil
	}
	if request.KeepalivedState == KeepalivedMaster {
		return c.promote(ctx, request.TransitionID)
	}
	return c.demoteLocked(ctx, request.KeepalivedState, request.TransitionID)
}

func (c *Controller) tick(ctx context.Context) {
	c.opMu.Lock()
	defer c.opMu.Unlock()

	if !c.isStartupFenced() && !c.tryStartupFencing(ctx) {
		return
	}
	if c.isStartupSyncBlocked() {
		owned, _ := c.vip.Owns(ctx, c.cfg.HA.Interface, c.cfg.HA.VIP)
		if !c.prepareStartupSynchronizedState(ctx, owned) {
			return
		}
	}
	c.tryRecoverServiceGroupFault(ctx)
	eligible, reason := c.evaluateEligibility(ctx)
	c.update(func(status *Status) {
		status.CanAcquireVIP = eligible
		status.EligibilityReason = reason
		switch status.Role {
		case RoleActive:
			status.Eligible = !status.ShouldReleaseVIP
			if !status.ShouldReleaseVIP {
				status.Decision = DecisionKeep
				status.DecisionReason = ""
			}
		case RoleStandby:
			status.Eligible = eligible
			status.ShouldReleaseVIP = false
			if eligible {
				status.Decision = DecisionKeep
				status.DecisionReason = ""
			} else {
				status.Decision = DecisionBlockAcquire
				status.DecisionReason = reason
			}
		default:
			status.Eligible = false
			status.CanAcquireVIP = false
		}
	})
	if c.Status().Role == RoleActive {
		owned, err := c.vip.Owns(ctx, c.cfg.HA.Interface, c.cfg.HA.VIP)
		if err != nil || !owned {
			// A VIP observation alone cannot distinguish an actual keepalived
			// fault from a normal priority hand-back after the preferred member
			// recovers. Return to BACKUP so the member can rejoin; an explicit
			// local health failure still uses KeepalivedFault below.
			_ = c.demoteLocked(ctx, KeepalivedBackup, "")
			return
		}
		c.checkActiveGroup(ctx)
	} else {
		c.reconcileVIPLocked(ctx)
	}
	c.refreshServiceGroup(ctx)
}

// tryRecoverServiceGroupFault returns a fenced member to standby after a
// failed promotion or active-group health failure. Without this retry a
// temporary dependency outage can leave every member permanently faulted even
// after the dependency recovers, requiring an operator to restart all agents.
func (c *Controller) tryRecoverServiceGroupFault(ctx context.Context) bool {
	current := c.Status()
	if current.Role != RoleFaulted || current.LastErrorCode != reasonServiceGroupFailed {
		return false
	}
	if current.LastTransitionAt > 0 {
		failedAt := time.UnixMilli(current.LastTransitionAt)
		if time.Since(failedAt) < c.cfg.HA.HealthFailureWindow {
			return false
		}
	}
	owned, err := c.vip.Owns(ctx, c.cfg.HA.Interface, c.cfg.HA.VIP)
	if err != nil || owned {
		return false
	}

	operationCtx, cancel := context.WithTimeout(ctx, c.cfg.HA.OperationTimeout)
	defer cancel()
	groupStatus, stopErr := c.group.StopHAGroup(operationCtx, c.cfg.HA.ServiceGroup, c.cfg.HA.OperationTimeout)
	if errors.Is(stopErr, fleetcontainer.ErrGroupEmpty) {
		stopErr = nil
		groupStatus = fleetcontainer.HAGroupStatus{
			Name:       c.cfg.HA.ServiceGroup,
			ObservedAt: time.Now().UTC().UnixMilli(),
		}
	}
	c.setGroup(groupStatus, "stopped")
	if stopErr != nil || groupStatus.Running > 0 {
		return false
	}
	if err := c.group.HAReady(operationCtx); err != nil {
		return false
	}
	if err := c.syncer.AcquisitionReady(operationCtx); err != nil {
		return false
	}

	c.healthFailureSince = time.Time{}
	c.update(func(status *Status) {
		status.Role = RoleStandby
		status.Eligible = false
		status.CanAcquireVIP = false
		status.ShouldReleaseVIP = false
		status.Decision = DecisionBlockAcquire
		status.DecisionReason = ""
		status.EligibilityReason = ""
		status.VIPOwned = false
		status.FailureSince = 0
		status.GraceRemainingMS = 0
		status.LastErrorCode = ""
	})
	c.persistLocal(operationCtx)
	c.logger.Info("Fleet HA member recovered from service-group fault and rejoined as standby", "memberID", c.cfg.HA.MemberID)
	return true
}

func (c *Controller) reconcileVIP(ctx context.Context) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.reconcileVIPLocked(ctx)
}

func (c *Controller) reconcileVIPLocked(ctx context.Context) {
	if c.Status().Role != RoleStandby {
		return
	}
	owned, err := c.vip.Owns(ctx, c.cfg.HA.Interface, c.cfg.HA.VIP)
	if err != nil || !owned {
		return
	}
	if err := c.promote(ctx, uuid.NewString()); err != nil {
		c.logger.Warn("Fleet HA VIP reconciliation could not promote this member", "error", err)
	}
}

func (c *Controller) promote(ctx context.Context, transitionID string) error {
	current := c.Status()
	if current.Role == RoleActive {
		return nil
	}
	if current.Role != RoleStandby {
		return fmt.Errorf("Fleet HA member cannot promote while role is %s", current.Role)
	}
	_ = c.syncer.SetOutboundEnabled(ctx, false)
	c.setTransition(RolePromoting, KeepalivedMaster, transitionID)
	owned, err := c.vip.Owns(ctx, c.cfg.HA.Interface, c.cfg.HA.VIP)
	if err != nil || !owned {
		c.failPromotion(reasonVIPNotOwned)
		return errors.New("Fleet HA VIP is not owned by this member")
	}
	if eligible, reason := c.evaluateEligibility(ctx); !eligible {
		c.failPromotion(reason)
		return fmt.Errorf("Fleet HA member is not eligible: %s", reason)
	}
	if err := c.syncer.Prepare(ctx); err != nil {
		c.failPromotion(reasonSyncNotReady)
		return fmt.Errorf("prepare Fleet HA synchronized state: %w", err)
	}
	if err := c.syncer.PromotionReady(ctx); err != nil {
		if !errors.Is(err, errInitialSyncBootstrapRequired) {
			c.failPromotion(reasonSyncNotReady)
			return fmt.Errorf("verify Fleet HA synchronized state: %w", err)
		}
		if bootstrapErr := c.syncer.Bootstrap(ctx); bootstrapErr != nil {
			c.failPromotion(reasonSyncNotReady)
			return fmt.Errorf("bootstrap Fleet HA synchronized state: %w", bootstrapErr)
		}
		if verifyErr := c.syncer.PromotionReady(ctx); verifyErr != nil {
			c.failPromotion(reasonSyncNotReady)
			return fmt.Errorf("verify bootstrapped Fleet HA synchronized state: %w", verifyErr)
		}
	}
	groupStatus, err := c.group.StartHAGroup(ctx, c.cfg.HA.ServiceGroup, c.cfg.HA.OperationTimeout)
	if err != nil {
		c.setGroup(groupStatus, "stopped")
		_, _ = c.group.StopHAGroup(ctx, c.cfg.HA.ServiceGroup, c.cfg.HA.OperationTimeout)
		c.failPromotion(reasonServiceGroupFailed)
		return err
	}
	c.mqtt.SetEnabled(true)
	c.update(func(status *Status) {
		status.Role = RoleActive
		status.Eligible = true
		status.CanAcquireVIP = true
		status.ShouldReleaseVIP = false
		status.Decision = DecisionKeep
		status.DecisionReason = ""
		status.EligibilityReason = ""
		status.VIPOwned = true
		status.FailureSince = 0
		status.GraceRemainingMS = 0
		status.LastErrorCode = ""
	})
	c.setGroup(groupStatus, "running")
	c.persistLocal(ctx)
	if err := c.syncer.SetOutboundEnabled(ctx, true); err != nil {
		c.logger.Warn(
			"Fleet HA member is active with outbound synchronization fenced",
			"memberID",
			c.cfg.HA.MemberID,
			"error",
			err,
		)
	} else {
		c.syncer.Trigger()
	}
	c.logger.Info("Fleet HA member became active", "memberID", c.cfg.HA.MemberID)
	return nil
}

func (c *Controller) failPromotion(reason string) {
	c.setFault(reason)
	c.persistLocal(context.Background())
}

func (c *Controller) demote(ctx context.Context, keepalivedState KeepalivedState, transitionID string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.demoteLocked(ctx, keepalivedState, transitionID)
}

func (c *Controller) demoteLocked(ctx context.Context, keepalivedState KeepalivedState, transitionID string) error {
	faultReason := c.Status().LastErrorCode
	if faultReason == "" {
		faultReason = reasonKeepalivedFault
	}
	_ = c.syncer.SetOutboundEnabled(ctx, false)
	c.mqtt.SetEnabled(false)
	c.setTransition(RoleDemoting, keepalivedState, transitionID)
	groupStatus, stopErr := c.group.StopHAGroup(ctx, c.cfg.HA.ServiceGroup, c.cfg.HA.OperationTimeout)
	if errors.Is(stopErr, fleetcontainer.ErrGroupEmpty) {
		stopErr = nil
		groupStatus = fleetcontainer.HAGroupStatus{
			Name:       c.cfg.HA.ServiceGroup,
			ObservedAt: time.Now().UTC().UnixMilli(),
		}
	}
	c.setGroup(groupStatus, "stopped")
	if stopErr != nil {
		c.setFault(reasonServiceGroupFailed)
		c.persistLocal(ctx)
		return stopErr
	}
	c.update(func(status *Status) {
		status.VIPOwned = false
		if keepalivedState == KeepalivedFault {
			status.Role = RoleFaulted
			status.Eligible = false
			status.CanAcquireVIP = false
			status.ShouldReleaseVIP = true
			status.Decision = DecisionRelease
			status.DecisionReason = faultReason
			status.EligibilityReason = faultReason
			status.LastErrorCode = faultReason
		} else {
			status.Role = RoleStandby
			status.Eligible = false
			status.CanAcquireVIP = false
			status.ShouldReleaseVIP = false
			status.Decision = DecisionBlockAcquire
			status.DecisionReason = ""
			status.LastErrorCode = ""
			status.FailureSince = 0
			status.GraceRemainingMS = 0
		}
	})
	c.healthFailureSince = time.Time{}
	c.persistLocal(ctx)
	return nil
}

func (c *Controller) tryStartupFencing(ctx context.Context) bool {
	operationCtx, cancel := context.WithTimeout(ctx, c.cfg.HA.OperationTimeout)
	defer cancel()
	status, err := c.group.StopHAGroup(operationCtx, c.cfg.HA.ServiceGroup, c.cfg.HA.OperationTimeout)
	if errors.Is(err, fleetcontainer.ErrGroupEmpty) {
		err = nil
		status = fleetcontainer.HAGroupStatus{
			Name:       c.cfg.HA.ServiceGroup,
			ObservedAt: time.Now().UTC().UnixMilli(),
		}
	}
	if err != nil {
		c.logger.Warn("Fleet HA initial fencing failed", "error", err)
		c.setFault(reasonServiceGroupFailed)
		c.setGroup(status, "stopped")
		c.persistLocal(ctx)
		return false
	}
	c.setGroup(status, "stopped")
	c.update(func(current *Status) {
		current.Role = RoleStandby
		current.Eligible = false
		current.CanAcquireVIP = false
		current.ShouldReleaseVIP = false
		current.Decision = DecisionBlockAcquire
		current.DecisionReason = reasonStartupFencing
		current.EligibilityReason = reasonStartupFencing
		current.LastErrorCode = ""
		c.startupFenced = true
	})
	c.persistLocal(ctx)
	return true
}

func (c *Controller) evaluateEligibility(ctx context.Context) (bool, string) {
	if !c.isStartupFenced() {
		return false, reasonStartupFencing
	}
	if c.isStartupSyncBlocked() {
		return false, reasonSyncNotReady
	}
	current := c.Status()
	if current.Role == RoleFaulted || current.Role == RoleDemoting {
		reason := current.LastErrorCode
		if reason == "" {
			reason = reasonKeepalivedFault
		}
		return false, reason
	}
	if err := c.group.HAReady(ctx); err != nil {
		return false, reasonDockerUnavailable
	}
	if err := c.syncer.AcquisitionReady(ctx); err != nil {
		return false, reasonSyncNotReady
	}
	return true, ""
}

func (c *Controller) isStartupFenced() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.startupFenced
}

func (c *Controller) prepareStartupSynchronizedState(ctx context.Context, ownsVIP bool) bool {
	if err := c.syncer.Prepare(ctx); err != nil {
		c.blockStartupSynchronization(ownsVIP)
		c.logger.Warn("prepare Fleet HA synchronized state during startup failed", "error", err)
		return false
	}
	if ownsVIP {
		if err := c.syncer.Bootstrap(ctx); err != nil {
			c.blockStartupSynchronization(true)
			c.logger.Warn("bootstrap Fleet HA synchronized head failed", "error", err)
			return false
		}
	}
	c.update(func(status *Status) {
		c.startupSyncBlocked = false
		if status.DecisionReason == reasonSyncNotReady {
			status.ShouldReleaseVIP = false
			status.Decision = DecisionBlockAcquire
			status.DecisionReason = ""
			status.EligibilityReason = ""
			status.LastErrorCode = ""
		}
	})
	return true
}

func (c *Controller) blockStartupSynchronization(ownsVIP bool) {
	c.update(func(status *Status) {
		c.startupSyncBlocked = true
		status.Role = RoleStandby
		status.Eligible = false
		status.CanAcquireVIP = false
		status.ShouldReleaseVIP = ownsVIP
		status.Decision = DecisionBlockAcquire
		if ownsVIP {
			status.Decision = DecisionRelease
		}
		status.DecisionReason = reasonSyncNotReady
		status.EligibilityReason = reasonSyncNotReady
		status.LastErrorCode = reasonSyncNotReady
	})
}

func (c *Controller) isStartupSyncBlocked() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.startupSyncBlocked
}

func (c *Controller) checkActiveGroup(ctx context.Context) {
	status, err := c.group.HAGroupStatus(ctx, c.cfg.HA.ServiceGroup)
	if err != nil || status.RequiredStopped > 0 || status.Unhealthy > 0 {
		if c.healthFailureSince.IsZero() {
			c.healthFailureSince = time.Now().UTC()
		}
		elapsed := time.Since(c.healthFailureSince)
		remaining := c.cfg.HA.HealthFailureWindow - elapsed
		if remaining < 0 {
			remaining = 0
		}
		c.update(func(current *Status) {
			current.FailureSince = c.healthFailureSince.UnixMilli()
			current.GraceRemainingMS = remaining.Milliseconds()
			current.Decision = DecisionKeep
			current.DecisionReason = reasonServiceGroupFailed
		})
		if elapsed >= c.cfg.HA.HealthFailureWindow {
			c.update(func(current *Status) {
				current.ShouldReleaseVIP = true
				current.Decision = DecisionRelease
				current.DecisionReason = reasonServiceGroupFailed
			})
			c.setError(reasonServiceGroupFailed)
			_ = c.demoteLocked(ctx, KeepalivedFault, "")
		}
		return
	}
	c.healthFailureSince = time.Time{}
	c.update(func(current *Status) {
		current.FailureSince = 0
		current.GraceRemainingMS = 0
		current.ShouldReleaseVIP = false
		current.Decision = DecisionKeep
		current.DecisionReason = ""
	})
}

func (c *Controller) refreshServiceGroup(ctx context.Context) {
	groupStatus, err := c.group.HAGroupStatus(ctx, c.cfg.HA.ServiceGroup)
	if err != nil {
		return
	}
	expected := "stopped"
	if c.Status().Role == RoleActive {
		expected = "running"
	}
	c.setGroup(groupStatus, expected)
}

func (c *Controller) persistLocal(ctx context.Context) {
	current := c.Status()
	if err := c.local.SaveHALocalState(ctx, state.HALocalState{
		MemberID:         c.cfg.HA.MemberID,
		Role:             string(current.Role),
		LastTransitionID: current.LastTransitionID,
	}); err != nil {
		c.logger.Warn("persist Fleet HA local state failed", "error", err)
	}
}

func (c *Controller) setTransition(role Role, keepalivedState KeepalivedState, transitionID string) {
	c.update(func(status *Status) {
		status.Role = role
		status.KeepalivedState = keepalivedState
		status.LastTransitionAt = time.Now().UTC().UnixMilli()
		if transitionID != "" {
			status.LastTransitionID = transitionID
		}
	})
}

func (c *Controller) setFault(reason string) {
	c.update(func(status *Status) {
		status.Role = RoleFaulted
		status.Eligible = false
		status.CanAcquireVIP = false
		status.ShouldReleaseVIP = true
		status.Decision = DecisionRelease
		status.DecisionReason = reason
		status.EligibilityReason = reason
		status.LastErrorCode = reason
	})
}

func (c *Controller) setError(reason string) {
	c.update(func(status *Status) {
		status.LastErrorCode = reason
	})
}

func (c *Controller) setGroup(input fleetcontainer.HAGroupStatus, expected string) {
	c.update(func(status *Status) {
		status.ServiceGroup = ServiceGroupStatus{
			Name:            input.Name,
			Expected:        expected,
			Total:           input.Total,
			Running:         input.Running,
			Stopped:         input.Stopped,
			RequiredStopped: input.RequiredStopped,
			Unhealthy:       input.Unhealthy,
			Failures:        append([]string(nil), input.Failures...),
			ObservedAt:      input.ObservedAt,
		}
	})
}

func (c *Controller) update(fn func(*Status)) {
	c.mu.Lock()
	fn(&c.state)
	c.mu.Unlock()
}
