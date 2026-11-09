package sourcestream

import (
	"context"
	"sync"
	"time"
)

// Evidence is collected by the existing execution lifecycle, never by discovery.
// It proves generic worker/executor preconditions, never a Source connection or deployment.
type executionEvidence struct {
	mu              sync.Mutex
	checkedAt       time.Time
	ready           bool
	sourceCheckedAt time.Time
	sourceReady     bool
}

type CapabilityReadiness struct {
	ProtocolSupported bool
	SourceReady       bool
	SourceCheckedAt   time.Time
	SourceValidUntil  time.Time
	Credentials       bool
	ProgramExecution  bool
	AsyncWorker       bool
	ExecutorSupported bool
	ExecutorReady     bool
	CheckedAt         time.Time
	ValidUntil        time.Time
}

func (s *Service) CapabilityReadiness() CapabilityReadiness {
	if s == nil {
		return CapabilityReadiness{}
	}
	_, supported := s.executor.(interface{ Ready(context.Context) error })
	s.executionEvidence.mu.Lock()
	checked, ready := s.executionEvidence.checkedAt, s.executionEvidence.ready
	sourceChecked, sourceReady := s.executionEvidence.sourceCheckedAt, s.executionEvidence.sourceReady
	s.executionEvidence.mu.Unlock()
	var until time.Time
	if !checked.IsZero() {
		until = checked.Add(programOwnerHeartbeat)
	}
	sourceUntil := time.Time{}
	if !sourceChecked.IsZero() {
		sourceUntil = sourceChecked.Add(programOwnerHeartbeat)
	}
	return CapabilityReadiness{SourceReady: sourceReady && time.Now().Before(sourceUntil), SourceCheckedAt: sourceChecked, SourceValidUntil: sourceUntil, ProtocolSupported: s.runtime != nil, Credentials: s.cipher != nil, ProgramExecution: s.ProgramExecutionReady(),
		AsyncWorker: s.asyncWorkerReady(), ExecutorSupported: supported,
		ExecutorReady: ready && time.Now().Before(until), CheckedAt: checked, ValidUntil: until}
}

func (s *Service) runExecutionEvidence(ctx context.Context) {
	checker, supported := s.executor.(interface{ Ready(context.Context) error })
	defer func() {
		s.executionEvidence.mu.Lock()
		s.executionEvidence.ready = false
		s.executionEvidence.sourceReady = false
		s.executionEvidence.mu.Unlock()
	}()
	ticker := time.NewTicker(programOwnerHeartbeat)
	defer ticker.Stop()
	for {
		// The registered dispatcher/cipher and the existing worker's connected,
		// admitted state are the generic Source platform proof. Protocol watches and
		// listener health remain resource observations and are never probed here.
		sourceReady := s.runtime != nil && s.cipher != nil && s.asyncWorkerReady() && ctx.Err() == nil
		s.executionEvidence.mu.Lock()
		s.executionEvidence.sourceCheckedAt, s.executionEvidence.sourceReady = time.Now().UTC(), sourceReady
		s.executionEvidence.mu.Unlock()
		if supported {
			checkCtx, cancel := context.WithTimeout(ctx, programOwnerHeartbeat/2)
			err := checker.Ready(checkCtx)
			cancel()
			s.executionEvidence.mu.Lock()
			s.executionEvidence.checkedAt, s.executionEvidence.ready = time.Now().UTC(), err == nil && ctx.Err() == nil
			s.executionEvidence.mu.Unlock()
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Bootstrap installs the worker after execution ownership starts. Snapshotting
// that callback must not race with the evidence collector's first iteration.
func (s *Service) asyncWorkerReady() bool {
	s.asyncReadyMu.RLock()
	ready := s.asyncReady
	s.asyncReadyMu.RUnlock()
	return ready != nil && ready()
}
