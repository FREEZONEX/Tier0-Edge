package sourcestream

import (
	"context"
	"errors"
	"sync"
	"time"

	"backend/internal/repo"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

const programOwnerHeartbeat = 2 * time.Second
const programRetirementInterval = 2 * time.Second
const programRetirementBatch = 16

var errProgramExecutionOwnerLost = errors.New("Stream execution owner lost")

type programExecutionEpoch struct {
	owner  *repo.ProgramExecutionOwner
	ctx    context.Context
	cancel context.CancelCauseFunc
}

type programExecutionLifecycle struct {
	mu      sync.Mutex
	started bool
	closed  bool
	epoch   *programExecutionEpoch
	cancel  context.CancelCauseFunc
	wg      sync.WaitGroup
}

type programExecutionEpochKey struct{}

func openProgramExecutionEpoch(ctx context.Context) (*programExecutionEpoch, error) {
	for i := 0; i < 3; i++ {
		owner, err := repo.NewSourceStreamRepo(ctx).OpenProgramExecutionOwner(ctx, uuid.NewString())
		if errors.Is(err, repo.ErrConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		epochCtx, cancel := context.WithCancelCause(ctx)
		return &programExecutionEpoch{owner: owner, ctx: epochCtx, cancel: cancel}, nil
	}
	return nil, repo.ErrConflict
}

// Explicit bootstrap lifecycle, independent of NATS and lazy constructors.
func (s *Service) StartProgramExecutions(ctx context.Context) error {
	l := &s.programExecution
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || ctx.Err() != nil {
		return ErrUnavailable
	}
	if l.started {
		if l.epoch == nil || l.epoch.ctx.Err() != nil {
			return ErrUnavailable
		}
		return nil
	}
	runCtx, cancel := context.WithCancelCause(ctx)
	epoch, err := openProgramExecutionEpoch(runCtx)
	if err != nil {
		cancel(err)
		return err
	}
	l.started, l.epoch, l.cancel = true, epoch, cancel
	l.wg.Add(5)
	go func() { defer l.wg.Done(); s.runProgramOwnerHeartbeat(runCtx) }()
	go func() { defer l.wg.Done(); s.runExecutionEvidence(runCtx) }()
	go func() { defer l.wg.Done(); s.runProgramRetirement(runCtx) }()
	go func() { defer l.wg.Done(); s.runProgramOutputRecovery(runCtx) }()
	go func() { defer l.wg.Done(); s.runSourceHealth(runCtx) }()
	return nil
}

func (s *Service) ProgramExecutionReady() bool {
	l := &s.programExecution
	l.mu.Lock()
	defer l.mu.Unlock()
	return !l.closed && l.epoch != nil && l.epoch.ctx.Err() == nil
}

func (s *Service) acquireProgramExecution(ctx context.Context) (context.Context, string, func(), error) {
	l := &s.programExecution
	l.mu.Lock()
	epoch := l.epoch
	ready := !l.closed && epoch != nil && epoch.ctx.Err() == nil
	l.mu.Unlock()
	if !ready {
		return nil, "", nil, ErrUnavailable
	}
	runCtx, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(epoch.ctx, func() { cancel(errProgramExecutionOwnerLost) })
	if epoch.ctx.Err() != nil {
		stop()
		cancel(errProgramExecutionOwnerLost)
		return nil, "", nil, ErrUnavailable
	}
	return context.WithValue(runCtx, programExecutionEpochKey{}, epoch), epoch.owner.ID(), func() { stop(); cancel(context.Canceled) }, nil
}

func (s *Service) closeProgramExecutions() {
	l := &s.programExecution
	l.mu.Lock()
	l.closed = true
	epoch := l.epoch
	l.epoch = nil
	if l.cancel != nil {
		l.cancel(errProgramExecutionOwnerLost)
	}
	l.mu.Unlock()
	if epoch != nil {
		epoch.cancel(errProgramExecutionOwnerLost)
		_ = epoch.owner.Close()
	}
	l.wg.Wait()
}

func (s *Service) discardProgramExecutionEpoch() {
	l := &s.programExecution
	l.mu.Lock()
	epoch := l.epoch
	l.epoch = nil
	l.mu.Unlock()
	if epoch != nil {
		epoch.cancel(errProgramExecutionOwnerLost)
		_ = epoch.owner.Close()
	}
}

func (s *Service) runProgramOwnerHeartbeat(ctx context.Context) {
	defer s.discardProgramExecutionEpoch()
	ticker := time.NewTicker(programOwnerHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		l := &s.programExecution
		l.mu.Lock()
		epoch := l.epoch
		l.mu.Unlock()
		if epoch != nil && epoch.owner.Ping(ctx) == nil {
			continue
		}
		l.mu.Lock()
		if l.epoch == epoch {
			l.epoch = nil
		}
		l.mu.Unlock()
		if epoch != nil {
			epoch.cancel(errProgramExecutionOwnerLost)
			_ = epoch.owner.Close()
		}
		if ctx.Err() != nil {
			return
		}
		replacement, err := openProgramExecutionEpoch(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logx.WithContext(ctx).Error("Stream execution owner unavailable; admissions remain closed")
			continue
		}
		l.mu.Lock()
		if l.closed || ctx.Err() != nil {
			l.mu.Unlock()
			replacement.cancel(errProgramExecutionOwnerLost)
			_ = replacement.owner.Close()
			return
		}
		l.epoch = replacement
		l.mu.Unlock()
	}
}

func (s *Service) runProgramRetirement(ctx context.Context) {
	ticker := time.NewTicker(programRetirementInterval)
	defer ticker.Stop()
	after := ""
	for {
		stepCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		ids, err := repo.NewSourceStreamRepo(stepCtx).UnpreparedProgramExecutionOwners(stepCtx, after, programRetirementBatch)
		cancel()
		if err == nil {
			if len(ids) == 0 {
				after = ""
			}
			for _, id := range ids {
				after = id
				stepCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				_, err := repo.NewSourceStreamRepo(stepCtx).RetireUnpreparedProgramExecutionOwner(stepCtx, id, programRetirementBatch)
				cancel()
				if err != nil && ctx.Err() == nil {
					logx.WithContext(ctx).Errorf("Stream unprepared retirement failed for owner %s", id)
				}
			}
		} else if ctx.Err() == nil {
			logx.WithContext(ctx).Error("Stream execution owner scan failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
