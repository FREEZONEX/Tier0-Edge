package sourcestream

// 真实后台采集循环的TTL、取消与owner失效；不启动worker/DB或设备请求。
import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type readinessTestExecutor struct {
	calls   chan struct{}
	failure bool
}

func (e readinessTestExecutor) Ready(ctx context.Context) error {
	select {
	case e.calls <- struct{}{}:
	default:
	}
	if e.failure {
		return errors.New("private runtime failure")
	}
	return nil
}
func (e readinessTestExecutor) Check(context.Context, ScriptDefinition) error { return nil }
func (e readinessTestExecutor) Transform(context.Context, ScriptDefinition, any, map[string]any) (ScriptResult, error) {
	return ScriptResult{}, nil
}
func TestCapabilityReadinessEvidenceTTLAndOwner(t *testing.T) {
	var nilService *Service
	require.Equal(t, CapabilityReadiness{}, nilService.CapabilityReadiness())
	calls := make(chan struct{}, 2)
	s := New(nil, readinessTestExecutor{calls: calls}, "")
	require.False(t, s.CapabilityReadiness().ExecutorReady)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.runExecutionEvidence(ctx) }()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("no background check")
	}
	require.Eventually(t, func() bool { return s.CapabilityReadiness().ExecutorReady }, time.Second, time.Millisecond)
	evidence := s.CapabilityReadiness()
	require.WithinDuration(t, evidence.CheckedAt.Add(programOwnerHeartbeat), evidence.ValidUntil, time.Nanosecond)
	require.False(t, evidence.ProgramExecution, "reachability is not owner admission")
	cancel()
	<-done
	require.False(t, s.CapabilityReadiness().ExecutorReady)
	s.executionEvidence.mu.Lock()
	s.executionEvidence.ready = true
	s.executionEvidence.checkedAt = time.Now().Add(-2 * programOwnerHeartbeat)
	s.executionEvidence.mu.Unlock()
	require.False(t, s.CapabilityReadiness().ExecutorReady, "discovery cannot extend expired evidence")
	epochCtx, epochCancel := context.WithCancelCause(context.Background())
	s.programExecution.epoch = &programExecutionEpoch{ctx: epochCtx, cancel: epochCancel}
	require.True(t, s.CapabilityReadiness().ProgramExecution)
	epochCancel(errProgramExecutionOwnerLost)
	require.False(t, s.CapabilityReadiness().ProgramExecution)
}
func TestCapabilityReadinessFailedBackgroundCheck(t *testing.T) {
	calls := make(chan struct{}, 1)
	s := New(nil, readinessTestExecutor{calls: calls, failure: true}, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.runExecutionEvidence(ctx) }()
	require.Eventually(t, func() bool { return !s.CapabilityReadiness().CheckedAt.IsZero() }, time.Second, time.Millisecond)
	require.False(t, s.CapabilityReadiness().ExecutorReady)
	cancel()
	<-done
}

// No protocol method may run while collecting generic platform evidence.
type discoveryNeverProbeRuntime struct{ t *testing.T }

func (r discoveryNeverProbeRuntime) Probe(context.Context, int64, RuntimeSource, Credential) error {
	r.t.Error("discovery probed a device")
	return ErrUnavailable
}
func (r discoveryNeverProbeRuntime) Discover(context.Context, int64, RuntimeSource, Credential, string) ([]NativeResource, error) {
	r.t.Error("discovery enumerated a device")
	return nil, ErrUnavailable
}
func (r discoveryNeverProbeRuntime) Invoke(context.Context, int64, RuntimeSource, Credential, string, any) (any, error) {
	r.t.Error("discovery invoked a device")
	return nil, ErrUnavailable
}
func TestCapabilityReadinessSourceUsesBoundedWorkerObservation(t *testing.T) {
	s := New(discoveryNeverProbeRuntime{t}, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	s.SetAsyncReady(func() bool { return true })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.runExecutionEvidence(ctx) }()
	require.Eventually(t, func() bool { return s.CapabilityReadiness().SourceReady }, time.Second, time.Millisecond)
	first := s.CapabilityReadiness()
	require.False(t, first.ExecutorSupported)
	require.True(t, first.CheckedAt.IsZero())
	require.WithinDuration(t, first.SourceCheckedAt.Add(programOwnerHeartbeat), first.SourceValidUntil, time.Nanosecond)
	cancel()
	<-done
	require.False(t, s.CapabilityReadiness().SourceReady)
	s.executionEvidence.mu.Lock()
	s.executionEvidence.sourceReady = true
	s.executionEvidence.sourceCheckedAt = time.Now().Add(-2 * programOwnerHeartbeat)
	s.executionEvidence.mu.Unlock()
	require.False(t, s.CapabilityReadiness().SourceReady)
}
