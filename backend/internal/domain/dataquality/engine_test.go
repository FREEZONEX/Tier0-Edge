package dataquality

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/domain/dataingest"

	"github.com/stretchr/testify/require"
)

// 测试目标：覆盖数据质量轻量检测、每节点单 deadline、有界深采样和关闭开关零状态分配。
// 正常路径：重复、迟到、时间空洞、乱序和缺失规则产生状态转换。
// 异常路径：持续异常受 cooldown 聚合，旧 deadline 不堆积，禁用模块不分配节点状态。

func TestEngine_DuplicateCooldownAndRecovery(t *testing.T) {
	events := make([]RuleEvent, 0)
	engine := newEngine(4, 16, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{
		UnsID: 7, Rules: ruleSet(ruleDuplicate), Cooldown: time.Hour,
	})
	t0 := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	first := observation(7, t0, t0, 11)
	engine.Observe([]dataingest.QualityObservation{first})
	engine.Observe([]dataingest.QualityObservation{first})
	engine.Observe([]dataingest.QualityObservation{first})
	recovered := observation(7, t0.Add(time.Second), t0.Add(time.Second), 12)
	engine.Observe([]dataingest.QualityObservation{recovered})

	require.Len(t, events, 2)
	require.Equal(t, ruleDuplicate, events[0].RuleCode)
	require.Equal(t, "open", events[0].State)
	require.Equal(t, int64(1), events[0].Occurrences)
	require.True(t, events[0].Transition)
	require.Equal(t, "resolved", events[1].State)
	require.Equal(t, int64(1), events[1].Occurrences, "one repeated active observation is coalesced into recovery")
	require.True(t, events[1].Transition)
}

func TestEngine_DuplicateEvidenceHashesConfiguredSequence(t *testing.T) {
	events := make([]RuleEvent, 0)
	engine := newEngine(4, 1, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{
		UnsID: 71, SequenceField: "serial", Rules: ruleSet(ruleDuplicate),
	})
	now := time.Now().UTC()
	item := dataingest.QualityObservation{
		UnsID: 71, IngestedAt: now,
		Fields: []dataingest.Field{
			{Name: "serial", Type: "string"}, {Name: "temperature", Type: "double"}, {Name: "password", Type: "string"},
		},
		RawValues: map[string]string{
			"serial": "sensitive-device-id", "temperature": "12.3", "password": "do-not-store",
		},
	}
	engine.Observe([]dataingest.QualityObservation{item})
	item.IngestedAt = now.Add(time.Second)
	engine.Observe([]dataingest.QualityObservation{item})
	require.Len(t, events, 1)
	require.NotContains(t, events[0].Evidence["sourceIdentity"], "sensitive-device-id")
	require.Contains(t, events[0].Evidence["sourceIdentity"], "sequenceHash:")
	require.Equal(t, now.UnixMilli(), events[0].Evidence["firstSeenAt"])
	matched := events[0].Evidence["matchedPayloadPreview"].(map[string]string)
	current := events[0].Evidence["currentPayloadPreview"].(map[string]string)
	require.Equal(t, "12.3", matched["temperature"], "the first payload preview must be an owned copy")
	require.Equal(t, "12.3", current["temperature"])
	require.Equal(t, "[REDACTED]", current["serial"])
	require.Equal(t, "[REDACTED]", current["password"])
}

func TestEvidencePayloadPreviewKeepsOperationalValuesAndRedactsSensitiveFields(t *testing.T) {
	preview := newEvidencePayloadPreview(map[string]string{
		"temperature": "21.5", "status": "running", "operatorName": "Alice", "address": "Plant Road 1",
		"apiToken": "abc123", "note": "unclassified free text",
	}, "", nil)

	require.Equal(t, 6, preview.fieldCount)
	require.True(t, preview.truncated, "unclassified free-text fields are not persisted")
	require.Equal(t, "21.5", preview.values["temperature"])
	require.Equal(t, "running", preview.values["status"])
	require.Equal(t, "[REDACTED]", preview.values["operatorName"])
	require.Equal(t, "[REDACTED]", preview.values["address"])
	require.Equal(t, "[REDACTED]", preview.values["apiToken"])
	require.NotContains(t, preview.values, "note")
}

func TestEngine_ValueRangeTriggersOnBothDirectionsAndRecoversAtInclusiveBounds(t *testing.T) {
	events := make([]RuleEvent, 0)
	engine := newEngine(4, 1, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{
		UnsID: 73, Rules: ruleSet(ruleValueRange), Cooldown: time.Hour,
	})
	lower, upper := float64(0), float64(100)
	field := dataingest.Field{Name: "temperature", Type: "double", LowerLimit: &lower, UpperLimit: &upper}
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	observe := func(value string, at time.Time) {
		engine.Observe([]dataingest.QualityObservation{{
			UnsID: 73, IngestedAt: at, Fields: []dataingest.Field{field},
			RawValues: map[string]string{"temperature": value, "password": "do-not-store"},
		}})
	}

	observe("-0.1", t0)
	for second := 1; second <= 31; second++ {
		observe("0", t0.Add(time.Duration(second)*time.Second))
	}
	observe("100.1", t0.Add(32*time.Second))
	for second := 33; second <= 63; second++ {
		observe("100", t0.Add(time.Duration(second)*time.Second))
	}

	require.Len(t, events, 4)
	require.Equal(t, []string{"open", "resolved", "open", "resolved"}, []string{
		events[0].State, events[1].State, events[2].State, events[3].State,
	})
	below := events[0]
	require.Equal(t, ruleValueRange, below.RuleCode)
	require.Equal(t, "temperature", below.Evidence["field"])
	require.Equal(t, float64(-0.1), below.Evidence["observedValue"])
	require.Equal(t, float64(0), below.Evidence["lowerLimit"])
	require.Equal(t, float64(100), below.Evidence["upperLimit"])
	require.Equal(t, "belowLower", below.Evidence["direction"])
	require.Equal(t, t0.UnixMilli(), below.Evidence["observedAt"])
	require.Equal(t, "-0.1", below.Evidence["currentPayloadPreview"].(map[string]string)["temperature"])
	require.NotContains(t, below.Evidence["currentPayloadPreview"].(map[string]string), "password")

	above := events[2]
	require.Equal(t, float64(100.1), above.Evidence["observedValue"])
	require.Equal(t, "aboveUpper", above.Evidence["direction"])
	require.Equal(t, t0.Add(32*time.Second).UnixMilli(), above.Evidence["observedAt"])
}

func TestEngine_EffectiveQualityOverlaysP0RulesAndRecovers(t *testing.T) {
	engine := newEngine(4, 1, nil)
	defer engine.Close()
	lower, upper := float64(0), float64(100)
	field := dataingest.Field{Name: "temperature", Type: "double", LowerLimit: &lower, UpperLimit: &upper}
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	engine.SetPolicy(PolicySnapshot{
		UnsID: 75, ExpectedInterval: time.Second, MissingGrace: time.Second,
		Rules: ruleSet(ruleValueRange, ruleMissingTelemetry),
	})

	// A node with no value does not gain an artificial sample; its supplied
	// source quality is merely normalized.
	require.Equal(t, int64(1), engine.EffectiveQuality(75, 0))

	badRange := dataingest.QualityObservation{
		UnsID: 75, IngestedAt: t0, QualityCode: 1, Fields: []dataingest.Field{field},
		RawValues: map[string]string{"temperature": "101"},
	}
	engine.Observe([]dataingest.QualityObservation{badRange})
	require.Equal(t, int64(2), engine.EffectiveQuality(75, 1))
	require.Equal(t, int64(3), engine.EffectiveQuality(75, 3), "platform detection never improves a source Bad value")

	healthy := badRange
	healthy.IngestedAt = t0.Add(time.Second)
	healthy.RawValues = map[string]string{"temperature": "100"}
	engine.Observe([]dataingest.QualityObservation{healthy})
	require.Equal(t, int64(1), engine.EffectiveQuality(75, 1), "range recovery falls back to source quality")

	shard := engine.shard(75)
	shard.mu.RLock()
	version := shard.nodes[75].deadlineVersion
	shard.mu.RUnlock()
	engine.deadlineReached(75, version, healthy.IngestedAt.Add(2*time.Second))
	require.Equal(t, int64(2), engine.EffectiveQuality(75, 1))

	healthy.IngestedAt = t0.Add(4 * time.Second)
	engine.Touch([]dataingest.QualityObservation{healthy})
	require.Equal(t, int64(2), engine.EffectiveQuality(75, 1), "one late sample must not clear an ongoing timeout")

	healthy.IngestedAt = t0.Add(5 * time.Second)
	engine.Touch([]dataingest.QualityObservation{healthy})
	require.Equal(t, int64(1), engine.EffectiveQuality(75, 1), "a subsequent timely sample confirms collection recovery")
}

func TestEngine_MissingTelemetryDoesNotFlapOnSparseSamples(t *testing.T) {
	events := make([]RuleEvent, 0)
	engine := newEngine(4, 1, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	t0 := time.Date(2026, 9, 7, 9, 30, 0, 0, time.UTC)
	engine.SetPolicy(PolicySnapshot{
		UnsID: 76, ExpectedInterval: time.Second, MissingGrace: time.Second,
		Rules: ruleSet(ruleMissingTelemetry),
	})

	sample := dataingest.QualityObservation{UnsID: 76, IngestedAt: t0}
	engine.Touch([]dataingest.QualityObservation{sample})
	shard := engine.shard(76)
	shard.mu.RLock()
	version := shard.nodes[76].deadlineVersion
	shard.mu.RUnlock()
	engine.deadlineReached(76, version, t0.Add(2*time.Second))
	require.True(t, hasEvent(events, ruleMissingTelemetry, "open"))

	// The real source is ten seconds apart while the policy permits two.
	// Its next sample is still late, so the original alarm stays open rather
	// than resolving and immediately opening a new notification lifecycle.
	sample.IngestedAt = t0.Add(10 * time.Second)
	engine.Touch([]dataingest.QualityObservation{sample})
	require.False(t, hasEvent(events, ruleMissingTelemetry, "resolved"))
	require.Contains(t, engine.Status(76)[0].OpenRules, ruleMissingTelemetry)

	sample.IngestedAt = t0.Add(11 * time.Second)
	engine.Touch([]dataingest.QualityObservation{sample})
	require.False(t, hasEvent(events, ruleMissingTelemetry, "resolved"))
	require.NotContains(t, engine.Status(76)[0].OpenRules, ruleMissingTelemetry)
	for second := 12; second <= 40; second++ {
		sample.IngestedAt = t0.Add(time.Duration(second) * time.Second)
		engine.Touch([]dataingest.QualityObservation{sample})
	}
	require.False(t, hasEvent(events, ruleMissingTelemetry, "resolved"))
	sample.IngestedAt = t0.Add(41 * time.Second)
	engine.Touch([]dataingest.QualityObservation{sample})
	require.True(t, hasEvent(events, ruleMissingTelemetry, "resolved"))
}

func TestEngine_ValueRangeFlappingKeepsOneIssueWithoutDelayingQuality(t *testing.T) {
	var events []RuleEvent
	engine := newEngine(4, 1, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{UnsID: 77, Rules: ruleSet(ruleValueRange), Cooldown: time.Minute})
	lower, upper := float64(1), float64(10)
	t0 := time.Now().UTC().Add(time.Hour)
	observe := func(second int, value string) {
		sample := dataingest.QualityObservation{
			UnsID: 77, IngestedAt: t0.Add(time.Duration(second) * time.Second),
			Fields:    []dataingest.Field{{Name: "value", Type: "double", LowerLimit: &lower, UpperLimit: &upper}},
			RawValues: map[string]string{"value": value, "quality": "source-unchanged"},
		}
		engine.Observe([]dataingest.QualityObservation{sample})
		require.Equal(t, value, sample.RawValues["value"])
		require.Equal(t, "source-unchanged", sample.RawValues["quality"])
	}
	for second := 0; second < 120; second++ {
		if second%2 == 0 {
			observe(second, "11")
			require.Equal(t, int64(2), engine.EffectiveQuality(77, 1))
		} else {
			observe(second, "5")
			require.Equal(t, int64(1), engine.EffectiveQuality(77, 1))
		}
	}
	require.Len(t, events, 2, "one initial event and one throttled update, not one Issue per flap")
	require.True(t, events[0].Transition)
	require.False(t, events[1].Transition)
	for second := 120; second < 149; second++ {
		observe(second, "5")
	}
	require.False(t, hasEvent(events, ruleValueRange, "resolved"))
	observe(149, "5")
	require.Len(t, events, 3)
	require.Equal(t, "resolved", events[2].State)
	require.Equal(t, int64(60), events[0].Occurrences+events[1].Occurrences+events[2].Occurrences)
}

func TestEngine_ValueRangeRecoveryNeedsCompleteFreshEvidence(t *testing.T) {
	for _, invalid := range []map[string]string{{}, {"value": "invalid"}} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			var events []RuleEvent
			engine := newEngine(1, 1, func(event RuleEvent) { events = append(events, event) })
			defer engine.Close()
			engine.SetPolicy(PolicySnapshot{UnsID: 78, Rules: ruleSet(ruleValueRange)})
			upper := float64(10)
			t0 := time.Now().UTC().Add(time.Hour)
			sample := dataingest.QualityObservation{UnsID: 78,
				Fields: []dataingest.Field{{Name: "value", Type: "double", UpperLimit: &upper}},
			}
			observe := func(second int, values map[string]string) {
				sample.IngestedAt, sample.RawValues = t0.Add(time.Duration(second)*time.Second), values
				engine.Observe([]dataingest.QualityObservation{sample})
			}
			observe(0, map[string]string{"value": "11"})
			for second := 1; second <= 15; second++ {
				observe(second, map[string]string{"value": "5"})
			}
			observe(16, invalid)
			for second := 17; second <= 46; second++ {
				observe(second, map[string]string{"value": "5"})
			}
			observe(17, map[string]string{"value": "5"})
			observe(46, map[string]string{"value": "5"})
			sample.Replayed = true
			observe(100, map[string]string{"value": "5"})
			require.False(t, hasEvent(events, ruleValueRange, "resolved"), "replay/old timestamps cannot advance recovery")
			sample.Replayed = false
			observe(47, map[string]string{"value": "5"})
			require.True(t, hasEvent(events, ruleValueRange, "resolved"))
		})
	}
}

func TestEngine_CollectionRecoveryRestartsAfterAnotherGap(t *testing.T) {
	var events []RuleEvent
	engine := newEngine(1, 1, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{UnsID: 79, ExpectedInterval: time.Second, MissingGrace: time.Second,
		Rules: ruleSet(ruleMissingTelemetry), Cooldown: time.Hour})
	engine.RestoreActiveRules(79, []string{ruleMissingTelemetry})
	t0 := time.Now().UTC().Add(time.Hour)
	observe := func(second int) {
		engine.Touch([]dataingest.QualityObservation{{UnsID: 79, IngestedAt: t0.Add(time.Duration(second) * time.Second)}})
	}
	for second := 0; second <= 20; second++ {
		observe(second)
	}
	// Even if the scheduler has not run yet, a late arrival breaks continuity.
	observe(25)
	for second := 26; second <= 55; second++ {
		observe(second)
	}
	require.False(t, hasEvent(events, ruleMissingTelemetry, "resolved"))
	observe(56)
	require.True(t, hasEvent(events, ruleMissingTelemetry, "resolved"))
}

func TestEngine_DisablingRuleClosesIssueDuringRecovery(t *testing.T) {
	for _, removeNode := range []bool{false, true} {
		t.Run(fmt.Sprint(removeNode), func(t *testing.T) {
			var events []RuleEvent
			engine := newEngine(1, 1, func(event RuleEvent) { events = append(events, event) })
			defer engine.Close()
			engine.SetPolicy(PolicySnapshot{UnsID: 80, Rules: ruleSet(ruleValueRange)})
			engine.RestoreActiveRules(80, []string{ruleValueRange})
			upper := float64(10)
			engine.Observe([]dataingest.QualityObservation{{UnsID: 80, IngestedAt: time.Now().UTC(),
				Fields:    []dataingest.Field{{Name: "value", Type: "double", UpperLimit: &upper}},
				RawValues: map[string]string{"value": "5"},
			}})
			require.Equal(t, int64(1), engine.EffectiveQuality(80, 1))
			require.Empty(t, events)
			if removeNode {
				engine.RemovePolicy(80)
			} else {
				engine.SetPolicy(PolicySnapshot{UnsID: 80, Rules: ruleSet()})
			}
			require.Len(t, events, 1)
			require.Equal(t, "resolved", events[0].State)
		})
	}
}

func TestEngine_ValueRangeIgnoresReplayMissingInvalidUnboundedAndNonNumericFields(t *testing.T) {
	events := make([]RuleEvent, 0)
	engine := newEngine(4, 1, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{UnsID: 74, Rules: ruleSet(ruleValueRange)})
	lower, upper := float64(0), float64(10)
	now := time.Now().UTC()
	engine.Observe([]dataingest.QualityObservation{
		{UnsID: 74, IngestedAt: now, Fields: []dataingest.Field{{Name: "temperature", Type: "double"}}, RawValues: map[string]string{"temperature": "99"}},
		{UnsID: 74, IngestedAt: now, Fields: []dataingest.Field{{Name: "state", Type: "string", LowerLimit: &lower, UpperLimit: &upper}}, RawValues: map[string]string{"state": "99"}},
		{UnsID: 74, IngestedAt: now, Fields: []dataingest.Field{{Name: "temperature", Type: "double", LowerLimit: &lower, UpperLimit: &upper}}, RawValues: map[string]string{}},
		{UnsID: 74, IngestedAt: now, Fields: []dataingest.Field{{Name: "temperature", Type: "double", LowerLimit: &lower, UpperLimit: &upper}}, RawValues: map[string]string{"temperature": "not-a-number"}},
		{UnsID: 74, IngestedAt: now, Replayed: true, Fields: []dataingest.Field{{Name: "temperature", Type: "double", LowerLimit: &lower, UpperLimit: &upper}}, RawValues: map[string]string{"temperature": "99"}},
	})

	require.Empty(t, events)
	require.NotContains(t, engine.Status(74)[0].OpenRules, ruleValueRange)
}

func TestEngine_SourceTimeRules(t *testing.T) {
	events := make([]RuleEvent, 0)
	engine := newEngine(4, 16, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{
		UnsID: 8, ExpectedInterval: time.Second, LateThreshold: 2 * time.Second,
		Rules: ruleSet(ruleLateArrival, ruleTimestampGap, ruleOutOfOrder), Cooldown: time.Minute,
	})
	t0 := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	first := observation(8, t0, t0, 1)
	first.RawValues["temperature"] = "10"
	gap := observation(8, t0.Add(3*time.Second), t0.Add(6*time.Second), 2)
	gap.RawValues["temperature"] = "13"
	outOfOrder := observation(8, t0.Add(time.Second), t0.Add(time.Second), 3)
	outOfOrder.RawValues["temperature"] = "11"
	engine.Observe([]dataingest.QualityObservation{first})
	engine.Observe([]dataingest.QualityObservation{gap})
	engine.Observe([]dataingest.QualityObservation{outOfOrder})

	require.True(t, hasEvent(events, ruleLateArrival, "open"))
	require.True(t, hasEvent(events, ruleLateArrival, "resolved"))
	require.True(t, hasEvent(events, ruleTimestampGap, "open"))
	require.True(t, hasEvent(events, ruleOutOfOrder, "open"))
	late := findEvent(events, ruleLateArrival, "open")
	require.Equal(t, int64(3000), late.Evidence["delayMs"])
	require.Equal(t, int64(2000), late.Evidence["allowedDelayMs"])
	require.Equal(t, int64(3000), late.Evidence["meanObservedIntervalMs"])
	require.Equal(t, "13", late.Evidence["currentPayloadPreview"].(map[string]string)["temperature"])
	gapEvent := findEvent(events, ruleTimestampGap, "open")
	require.Equal(t, int64(3000), gapEvent.Evidence["gapMs"])
	require.Equal(t, int64(1000), gapEvent.Evidence["allowedGapMs"])
	require.Equal(t, "10", gapEvent.Evidence["referencePayloadPreview"].(map[string]string)["temperature"])
	require.Equal(t, "13", gapEvent.Evidence["currentPayloadPreview"].(map[string]string)["temperature"])
	orderEvent := findEvent(events, ruleOutOfOrder, "open")
	require.Equal(t, int64(2000), orderEvent.Evidence["behindMs"])
	require.Equal(t, int64(0), orderEvent.Evidence["outOfOrderToleranceMs"])
	require.Equal(t, "13", orderEvent.Evidence["referencePayloadPreview"].(map[string]string)["temperature"])
	require.Equal(t, "11", orderEvent.Evidence["currentPayloadPreview"].(map[string]string)["temperature"])
	status := engine.Status(8)
	require.Len(t, status, 1)
	require.Equal(t, int64(3), status[0].ReceivedCount)
	require.Equal(t, t0.Add(3*time.Second), *status[0].MaxSourceTime)
}

func TestEngine_StatusExplainsInsufficientEvidence(t *testing.T) {
	engine := newEngine(4, 16, nil)
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{
		UnsID: 9,
		Rules: ruleSet(ruleDuplicate, ruleLateArrival, ruleTimestampGap, ruleOutOfOrder),
	})
	engine.Observe([]dataingest.QualityObservation{{
		UnsID: 9, IngestedAt: time.Now().UTC(), CanonicalHash: 1,
		RawValues: map[string]string{},
	}})

	status := engine.Status(9)
	require.Len(t, status, 1)
	require.Equal(t, "sourceIdentityMissing", status[0].InsufficientEvidence[ruleDuplicate])
	require.Equal(t, "sourceTimestampMissing", status[0].InsufficientEvidence[ruleLateArrival])
	require.Equal(t, "sourceTimestampMissing", status[0].InsufficientEvidence[ruleTimestampGap])
	require.Equal(t, "sourceTimestampMissing", status[0].InsufficientEvidence[ruleOutOfOrder])
}

func TestEngine_IngestionDropRecoversAfterPersistedBatch(t *testing.T) {
	events := make([]RuleEvent, 0)
	engine := newEngine(4, 16, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{
		UnsID: 10, Rules: ruleSet(ruleIngestionDrop), Cooldown: time.Minute,
	})
	now := time.Now().UTC()
	item := dataingest.QualityObservation{UnsID: 10, IngestedAt: now}

	engine.RecordDrop("historyWriter", "writeError", []dataingest.QualityObservation{item, item})
	engine.RecordPersisted([]dataingest.QualityObservation{item, item})

	require.Len(t, events, 2)
	require.Equal(t, ruleIngestionDrop, events[0].RuleCode)
	require.Equal(t, "open", events[0].State)
	require.Equal(t, ruleIngestionDrop, events[1].RuleCode)
	require.Equal(t, "resolved", events[1].State)
	status := engine.Status(10)
	require.Len(t, status, 1)
	require.Equal(t, int64(2), status[0].DropCount, "dropped records remain a record count")
	require.Equal(t, int64(2), status[0].PersistedCount, "persisted records remain a record count")
	require.NotContains(t, status[0].OpenRules, ruleIngestionDrop)
}

func TestDeadlineScheduler_OneEntryPerNode(t *testing.T) {
	due := make(chan struct{}, 1)
	scheduler := newDeadlineScheduler(func(_ int64, _ uint64, _ time.Time) { due <- struct{}{} })
	defer scheduler.Close()
	for version := uint64(1); version <= 100; version++ {
		scheduler.Schedule(9, time.Now().Add(20*time.Millisecond), version)
	}
	scheduler.mu.Lock()
	require.Len(t, scheduler.heap, 1)
	require.Len(t, scheduler.byNode, 1)
	require.Equal(t, uint64(100), scheduler.byNode[9].version)
	scheduler.mu.Unlock()
	select {
	case <-due:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("latest deadline was not delivered")
	}
}

func TestEngine_DeepSamplingIsBounded(t *testing.T) {
	engine := newEngine(8, 16, nil)
	defer engine.Close()
	now := time.Now().UTC()
	for unsID := int64(1); unsID <= 20; unsID++ {
		engine.SetPolicy(PolicySnapshot{UnsID: unsID, Rules: ruleSet(ruleInvalidValue)})
		engine.Observe([]dataingest.QualityObservation{observation(unsID, now, now, uint64(unsID))})
	}
	engine.deep.mu.Lock()
	require.Len(t, engine.deep.samples, 16)
	engine.deep.mu.Unlock()
}

func TestEngine_RestartBeginsWarmingWithoutPersistedRuntimeState(t *testing.T) {
	events := make([]RuleEvent, 0)
	engine := newEngine(4, 1, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{
		UnsID: 72, MinSamples: 3, Rules: ruleSet(ruleIngestionDrop),
	})
	engine.RestoreActiveRules(72, []string{ruleIngestionDrop, "notConfigured"})

	status := engine.Status(72)
	require.Len(t, status, 1)
	require.Equal(t, "warming", status[0].Phase)
	require.Zero(t, status[0].ReceivedCount)
	require.Nil(t, status[0].LastIngestedAt)
	require.Equal(t, []string{ruleIngestionDrop}, status[0].OpenRules)

	observation := dataingest.QualityObservation{UnsID: 72, IngestedAt: time.Now().UTC()}
	engine.RecordDrop("historyWriter", "writeError", []dataingest.QualityObservation{observation})
	require.Len(t, events, 1)
	require.Equal(t, "open", events[0].State)
	require.False(t, events[0].Transition, "repository upsert deduplicates the restored issue")
	engine.RecordPersisted([]dataingest.QualityObservation{observation})
	require.Len(t, events, 2)
	require.Equal(t, "resolved", events[1].State)
}

func TestEngine_ConnectorHealthRequiresExplicitSignals(t *testing.T) {
	events := make([]RuleEvent, 0)
	engine := newEngine(4, 1, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{
		UnsID: 81, Rules: ruleSet(ruleCollectionInterrupted, ruleConnectorFailure), Cooldown: time.Hour,
	})

	status := engine.Status(81)
	require.Len(t, status, 1)
	require.Equal(t, "unknown", status[0].CollectionState)
	require.Empty(t, status[0].ConnectorSignals)
	require.Empty(t, events, "absence of a connector signal must not create an issue")

	t0 := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	engine.RecordConnectorHealth(dataingest.ConnectorHealthSignal{
		Source: dataingest.ConnectorSourceMQTT, State: dataingest.ConnectorStateLost,
		ObservedAt: t0, ErrorCode: "mqttConnectionLost",
	})
	require.True(t, hasEvent(events, ruleCollectionInterrupted, "open"))
	require.False(t, hasEvent(events, ruleConnectorFailure, "open"))
	require.Equal(t, dataingest.ConnectorSourceMQTT, events[0].Evidence["source"])
	require.Equal(t, t0.UnixMilli(), events[0].Evidence["observedAt"])

	// Historical replay is evidence about historical data, never an explicit
	// runtime recovery signal.
	engine.Observe([]dataingest.QualityObservation{{
		UnsID: 81, IngestedAt: t0.Add(time.Minute), Replayed: true,
	}})
	status = engine.Status(81)
	require.Contains(t, status[0].OpenRules, ruleCollectionInterrupted)
	require.Equal(t, t0, status[0].ConnectorSignals[0].ObservedAt)

	engine.RecordConnectorHealth(dataingest.ConnectorHealthSignal{
		Source: dataingest.ConnectorSourceMQTT, State: dataingest.ConnectorStateConnected,
		ObservedAt: t0.Add(2 * time.Minute),
	})
	require.True(t, hasEvent(events, ruleCollectionInterrupted, "resolved"))

	engine.RecordConnectorHealth(dataingest.ConnectorHealthSignal{
		Source: dataingest.ConnectorSourceFlow, State: dataingest.ConnectorStateError,
		ObservedAt: t0.Add(3 * time.Minute), ErrorCode: "flowDeployRuntime", FlowID: 91, UnsIDs: []int64{81},
	})
	require.True(t, hasEvent(events, ruleConnectorFailure, "open"))
	status = engine.Status(81)
	require.Equal(t, "interrupted", status[0].CollectionState)
	require.Len(t, status[0].ConnectorSignals, 2)

	engine.RecordConnectorHealth(dataingest.ConnectorHealthSignal{
		Source: dataingest.ConnectorSourceFlow, State: dataingest.ConnectorStateDeployed,
		ObservedAt: t0.Add(4 * time.Minute), FlowID: 91, UnsIDs: []int64{81},
	})
	require.True(t, hasEvent(events, ruleConnectorFailure, "resolved"))
	require.Equal(t, "observed", engine.Status(81)[0].CollectionState)
}

func TestEngine_ConnectorHealthKeepsIssueOpenUntilEveryExplicitCauseRecovers(t *testing.T) {
	events := make([]RuleEvent, 0)
	engine := newEngine(4, 1, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	t0 := time.Now().UTC()
	engine.RecordConnectorHealth(dataingest.ConnectorHealthSignal{
		Source: dataingest.ConnectorSourceMQTT, State: dataingest.ConnectorStateLost, ObservedAt: t0,
	})
	engine.SetPolicy(PolicySnapshot{UnsID: 82, Rules: ruleSet(ruleCollectionInterrupted), Cooldown: time.Hour})
	require.True(t, hasEvent(events, ruleCollectionInterrupted, "open"), "a new policy inherits the latest process-wide MQTT state")

	engine.RecordConnectorHealth(dataingest.ConnectorHealthSignal{
		Source: dataingest.ConnectorSourceFlow, State: dataingest.ConnectorStateDisabled,
		ObservedAt: t0.Add(time.Second), FlowID: 92, UnsIDs: []int64{82},
	})
	engine.RecordConnectorHealth(dataingest.ConnectorHealthSignal{
		Source: dataingest.ConnectorSourceMQTT, State: dataingest.ConnectorStateConnected,
		ObservedAt: t0.Add(2 * time.Second),
	})
	require.Contains(t, engine.Status(82)[0].OpenRules, ruleCollectionInterrupted)

	engine.RecordConnectorHealth(dataingest.ConnectorHealthSignal{
		Source: dataingest.ConnectorSourceFlow, State: dataingest.ConnectorStateDeployed,
		ObservedAt: t0.Add(3 * time.Second), FlowID: 92, UnsIDs: []int64{82},
	})
	require.NotContains(t, engine.Status(82)[0].OpenRules, ruleCollectionInterrupted)
	require.True(t, hasEvent(events, ruleCollectionInterrupted, "resolved"))
}

func TestEngine_StaleDataIsOnlySuspectedWithoutConnectorFailure(t *testing.T) {
	events := make([]RuleEvent, 0)
	engine := newEngine(4, 1, func(event RuleEvent) { events = append(events, event) })
	defer engine.Close()
	t0 := time.Now().UTC()
	engine.SetPolicy(PolicySnapshot{
		UnsID: 83, ExpectedInterval: time.Second, MissingGrace: 500 * time.Millisecond,
		Rules: ruleSet(ruleMissingTelemetry, ruleCollectionInterrupted, ruleConnectorFailure),
	})
	engine.Observe([]dataingest.QualityObservation{{
		UnsID: 83, IngestedAt: t0, Fields: []dataingest.Field{{Name: "temperature", Type: "double"}},
		RawValues: map[string]string{"temperature": "21.5"},
	}})
	shard := engine.shard(83)
	shard.mu.RLock()
	version := shard.nodes[83].deadlineVersion
	shard.mu.RUnlock()
	engine.deadlineReached(83, version, t0.Add(1500*time.Millisecond))

	status := engine.Status(83)
	require.Equal(t, "suspected", status[0].CollectionState)
	require.Contains(t, status[0].OpenRules, ruleMissingTelemetry)
	require.NotContains(t, status[0].OpenRules, ruleCollectionInterrupted)
	require.NotContains(t, status[0].OpenRules, ruleConnectorFailure)
	require.False(t, hasEvent(events, ruleCollectionInterrupted, "open"))
	require.False(t, hasEvent(events, ruleConnectorFailure, "open"))
	missing := findEvent(events, ruleMissingTelemetry, "open")
	require.Equal(t, int64(1000), missing.Evidence["expectedIntervalMs"])
	require.Equal(t, int64(500), missing.Evidence["missingGraceMs"])
	require.Equal(t, int64(1500), missing.Evidence["allowedSilenceMs"])
	require.Equal(t, "21.5", missing.Evidence["lastPayloadPreview"].(map[string]string)["temperature"])
}

func TestService_DisabledDoesNotAllocateRuntimeState(t *testing.T) {
	service, err := New(context.Background(), config.DataQualityConf{Enabled: false})
	require.NoError(t, err)
	defer service.Close()
	service.Observe(context.Background(), []dataingest.QualityObservation{observation(1, time.Now(), time.Now(), 1)})
	require.False(t, service.Enabled())
	require.Zero(t, service.RuntimeStateCount())
}

func TestEngine_ConcurrentPolicyObserveAndStatus(t *testing.T) {
	engine := newEngine(8, 1, nil)
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{UnsID: 91, Rules: ruleSet(ruleDuplicate)})

	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for index := 0; index < 100; index++ {
				switch worker % 3 {
				case 0:
					engine.SetPolicy(PolicySnapshot{UnsID: 91, Rules: ruleSet(ruleDuplicate)})
				case 1:
					at := time.Now().UTC()
					engine.Observe([]dataingest.QualityObservation{observation(91, at, at, uint64(index+1))})
				default:
					_ = engine.Status(91)
				}
			}
		}(worker)
	}
	workers.Wait()
	require.Len(t, engine.Status(91), 1)
}

func TestEngine_TouchCreatesBoundedOwnedObservationAndDefersHash(t *testing.T) {
	engine := newEngine(2, 1, nil)
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{UnsID: 92, Rules: ruleSet(ruleDuplicate, ruleInvalidValue)})
	fields := make([]dataingest.Field, 0, 40)
	values := make(map[string]string, 40)
	for index := 0; index < 40; index++ {
		name := fmt.Sprintf("field%02d", index)
		fields = append(fields, dataingest.Field{Name: name, Type: "double"})
		values[name] = fmt.Sprintf("%d", index)
	}
	owned := engine.Touch([]dataingest.QualityObservation{{
		UnsID: 92, IngestedAt: time.Now().UTC(), Fields: fields, RawValues: values,
	}})
	require.Len(t, owned, 1)
	require.LessOrEqual(t, len(owned[0].Fields), maxEvaluationFields)
	require.LessOrEqual(t, len(owned[0].RawValues), maxEvaluationFields)
	require.Zero(t, owned[0].CanonicalHash)
	values["field00"] = "mutated"
	require.Equal(t, "0", owned[0].RawValues["field00"])
}

func observation(unsID int64, sourceTime, ingestedAt time.Time, hash uint64) dataingest.QualityObservation {
	return dataingest.QualityObservation{
		UnsID: unsID, SourceTime: sourceTime, SourceTimeSet: true, IngestedAt: ingestedAt,
		CanonicalHash: hash,
		Fields:        []dataingest.Field{{Name: "sequence", Type: "string"}, {Name: "temperature", Type: "double"}},
		RawValues:     map[string]string{"sequence": ""},
	}
}

func ruleSet(rules ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		out[rule] = struct{}{}
	}
	return out
}

func hasEvent(events []RuleEvent, rule, state string) bool {
	for _, event := range events {
		if event.RuleCode == rule && event.State == state {
			return true
		}
	}
	return false
}

func findEvent(events []RuleEvent, rule, state string) RuleEvent {
	for _, event := range events {
		if event.RuleCode == rule && event.State == state {
			return event
		}
	}
	return RuleEvent{}
}
