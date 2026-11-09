package dataquality

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/domain/dataingest"

	"github.com/stretchr/testify/require"
)

func TestEngine_DeepWindowEvaluatesConfiguredRulesWithoutRawValues(t *testing.T) {
	events := make(chan RuleEvent, 16)
	engine := newEngine(4, 1, func(event RuleEvent) { events <- event })
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{
		UnsID: 31, ExpectedInterval: time.Second,
		FrequencyTolerancePercent: 20, MinSamples: 3,
		FlatMinSamples: 3, FlatMinDuration: 2 * time.Second,
		MandatoryFields: []string{"required"}, FlatFields: []string{"pressure"},
		Rules: ruleSet(ruleMandatoryMissing, ruleInvalidValue, ruleFlatSignal, ruleFrequency),
	})
	now := time.Now().UTC()
	for index := 0; index < deepSampleTarget; index++ {
		quality := int64(0)
		value := "12.5"
		if index == 0 {
			quality = 3
			value = "NaN"
		}
		engine.Observe([]dataingest.QualityObservation{{
			UnsID: 31, SourceTime: now.Add(time.Duration(index) * 2 * time.Second), SourceTimeSet: true,
			IngestedAt: now.Add(time.Duration(index) * 2 * time.Second), QualityCode: quality,
			Fields:    []dataingest.Field{{Name: "pressure", Type: "string"}, {Name: "temperature", Type: "double"}},
			RawValues: map[string]string{"pressure": "plant-secret", "temperature": value},
		}})
	}

	observed := make(map[string]RuleEvent)
	require.Eventually(t, func() bool {
		for {
			select {
			case event := <-events:
				if event.State == "open" {
					observed[event.RuleCode] = event
				}
			default:
				return len(observed) == 4
			}
		}
	}, time.Second, 10*time.Millisecond)
	for _, rule := range []string{ruleMandatoryMissing, ruleInvalidValue, ruleFlatSignal, ruleFrequency} {
		require.Contains(t, observed, rule)
		require.NotContains(t, fmt.Sprint(observed[rule].Evidence), "plant-secret")
	}
	mandatory := observed[ruleMandatoryMissing].Evidence
	require.Equal(t, []string{"required"}, mandatory["fields"])
	require.Equal(t, 100, mandatory["count"])
	require.Equal(t, 100, mandatory["missingCountsByField"].(map[string]int)["required"])
	require.Len(t, mandatory["missingExamples"], 3)

	invalid := observed[ruleInvalidValue].Evidence
	require.Equal(t, []string{"temperature"}, invalid["fields"])
	require.Contains(t, invalid["errorCodes"], "invalidNumber")
	require.Equal(t, "NaN", invalid["invalidExamples"].(map[string]string)["temperature"])
	require.Equal(t, "[REDACTED]", invalid["payloadPreview"].(map[string]string)["pressure"])

	flat := observed[ruleFlatSignal].Evidence
	require.Equal(t, int64(198000), flat["durationMs"])
	require.Equal(t, int64(2000), flat["minimumDurationMs"])
	details := flat["flatFieldDetails"].([]map[string]any)
	require.Equal(t, "pressure", details[0]["field"])
	require.Equal(t, "[REDACTED]", details[0]["valuePreview"])

	frequency := observed[ruleFrequency].Evidence
	require.Equal(t, int64(2000), frequency["observedMs"])
	require.Equal(t, int64(2000), frequency["meanObservedMs"])
	require.Equal(t, int64(800), frequency["allowedMinimumMs"])
	require.Equal(t, int64(1200), frequency["allowedMaximumMs"])
	require.Equal(t, "median", frequency["aggregationMethod"])
	require.Eventually(t, func() bool {
		status := engine.Status(31)
		return len(status) == 1 && status[0].DeepLastResult == "completed" && status[0].DeepSampleSize == 0
	}, time.Second, 10*time.Millisecond)
}

func TestDeepSampler_RotatesAndImmediateRequestUsesNextSlot(t *testing.T) {
	d := newDeepSampler(1)
	defer d.Close()
	base := time.Now().UTC()
	for _, unsID := range []int64{1, 2, 3} {
		d.Configure(PolicySnapshot{UnsID: unsID, SamplingPriority: 10, Rules: ruleSet(ruleInvalidValue)})
	}
	status, ok := d.Request(3)
	require.True(t, ok)
	require.Equal(t, "waiting", status.State)
	fillDeepWindow(d, 1, base)
	require.Equal(t, "running", d.Status(3).State)
	require.Equal(t, "waiting", d.Status(2).State)
	require.Len(t, d.samples, 1)

	fillDeepWindow(d, 3, base.Add(time.Minute))
	require.Equal(t, "running", d.Status(2).State)
	fillDeepWindow(d, 2, base.Add(2*time.Minute))
	require.Equal(t, "running", d.Status(1).State, "completed nodes must re-enter the fair rotation")
}

func TestDeepSampler_PriorityBreaksEqualWaitTime(t *testing.T) {
	queuedAt := time.Now().UTC()
	low := &deepNode{queuedAt: queuedAt, policy: PolicySnapshot{SamplingPriority: -10}}
	high := &deepNode{queuedAt: queuedAt, policy: PolicySnapshot{SamplingPriority: 10}}
	require.True(t, deepNodeBefore(high, 2, low, 1))
	require.False(t, deepNodeBefore(low, 1, high, 2))
}

func TestDeepSampler_EqualClockTicksKeepQueueOrder(t *testing.T) {
	now := time.Now().UTC()
	waiting := &deepNode{queuedAt: now, queueOrder: 2}
	completed := &deepNode{queuedAt: now, queueOrder: 4}
	require.True(t, deepNodeBefore(waiting, 3, completed, 1))
	require.False(t, deepNodeBefore(completed, 1, waiting, 3))
}

func TestDeepSampler_TimeoutReleasesSlotAndMarksInsufficient(t *testing.T) {
	d := newDeepSampler(1)
	defer d.Close()
	d.SetMaxCollectWindow(20 * time.Millisecond)
	d.evaluate = func(window deepWindow) deepEvaluationResult {
		if window.timedOut {
			return deepEvaluationResult{insufficient: map[string]string{ruleInvalidValue: "collectionTimeout"}}
		}
		return deepEvaluationResult{}
	}
	d.Configure(PolicySnapshot{UnsID: 1, Rules: ruleSet(ruleInvalidValue)})
	d.Configure(PolicySnapshot{UnsID: 2, Rules: ruleSet(ruleInvalidValue)})
	d.mu.Lock()
	d.nodes[1].startedAt = time.Now().Add(-time.Second)
	d.mu.Unlock()
	d.expire(time.Now().UTC())
	require.Equal(t, "running", d.Status(2).State)
	require.Eventually(t, func() bool {
		status := d.Status(1)
		return status.LastResult == "insufficientEvidence" && status.Reason == "collectionTimeout"
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, int64(20), d.Status(2).MaxCollectWindowMs)
	require.Len(t, d.samples, 1)
}

func TestDeepSampler_EvaluationQueueFullIsObservable(t *testing.T) {
	d := newDeepSampler(1)
	blocked := make(chan struct{})
	started := make(chan struct{}, 1)
	d.evaluate = func(deepWindow) deepEvaluationResult {
		select {
		case started <- struct{}{}:
		default:
		}
		<-blocked
		return deepEvaluationResult{}
	}
	var drops atomic.Int64
	d.drop = func(int64) { drops.Add(1) }
	for _, unsID := range []int64{1, 2, 3} {
		d.Configure(PolicySnapshot{UnsID: unsID, Rules: ruleSet(ruleInvalidValue)})
	}
	base := time.Now().UTC()
	fillDeepWindow(d, 1, base)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("evaluation worker did not start")
	}
	fillDeepWindow(d, 2, base.Add(time.Minute))
	fillDeepWindow(d, 3, base.Add(2*time.Minute))
	require.Equal(t, int64(1), drops.Load())
	require.Equal(t, "insufficientEvidence", d.Status(3).LastResult)
	require.Equal(t, "evaluationQueueFull", d.Status(3).Reason)
	close(blocked)
	d.Close()
}

func TestEngine_ReplayedObservationDoesNotRefreshRealtimeDeadline(t *testing.T) {
	engine := newEngine(4, 1, nil)
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{
		UnsID: 41, ExpectedInterval: time.Second,
		Rules: ruleSet(ruleMissingTelemetry, ruleLateArrival),
	})
	now := time.Now().UTC()
	engine.Observe([]dataingest.QualityObservation{{UnsID: 41, IngestedAt: now, SourceTime: now, SourceTimeSet: true}})
	status := engine.Status(41)[0]
	engine.Observe([]dataingest.QualityObservation{{
		UnsID: 41, IngestedAt: now.Add(-time.Hour), SourceTime: now.Add(-2 * time.Hour),
		SourceTimeSet: true, Replayed: true,
	}})
	afterReplay := engine.Status(41)[0]
	require.Equal(t, *status.LastIngestedAt, *afterReplay.LastIngestedAt)
}

func fillDeepWindow(d *deepSampler, unsID int64, startedAt time.Time) {
	for index := 0; index < deepSampleTarget; index++ {
		d.Observe(unsID, dataingest.QualityObservation{
			UnsID: unsID, SourceTime: startedAt.Add(time.Duration(index) * time.Second),
			SourceTimeSet: true, IngestedAt: startedAt.Add(time.Duration(index) * time.Second),
		})
	}
}
