package dataquality

import (
	"math"
	"sort"
	"time"
)

func (e *Engine) SetDeepDropCallback(drop func(int64)) {
	if e == nil || e.deep == nil {
		return
	}
	e.deep.mu.Lock()
	e.deep.drop = drop
	e.deep.mu.Unlock()
}

func (e *Engine) RequestDeepRun(unsID int64) (DeepStatus, bool) {
	if e == nil || e.deep == nil {
		return DeepStatus{}, false
	}
	return e.deep.Request(unsID)
}

func (e *Engine) evaluateDeepWindow(window deepWindow) deepEvaluationResult {
	shard := e.shard(window.unsID)
	shard.mu.Lock()
	state := shard.nodes[window.unsID]
	if state == nil || state.policy.revision != window.policy.revision {
		shard.mu.Unlock()
		return deepEvaluationResult{insufficient: map[string]string{"deepSampling": "policyChanged"}}
	}
	policy := state.policy
	insufficient := make(map[string]string)
	events := make([]RuleEvent, 0, 4)
	if window.timedOut {
		for _, rule := range []string{ruleMandatoryMissing, ruleInvalidValue, ruleFlatSignal, ruleFrequency} {
			if policy.enabled(rule) {
				insufficient[rule] = "collectionTimeout"
			}
		}
	}

	if policy.enabled(ruleMandatoryMissing) && !window.timedOut {
		abnormal, evidence := evaluateMandatory(window.samples, policy.MandatoryFields)
		events = append(events, e.transitionLocked(state, ruleMandatoryMissing, abnormal, window.completedAt, evidence))
	}
	if policy.enabled(ruleInvalidValue) && !window.timedOut {
		abnormal, evidence := evaluateInvalid(window.samples)
		events = append(events, e.transitionLocked(state, ruleInvalidValue, abnormal, window.completedAt, evidence))
	}
	if policy.enabled(ruleFlatSignal) && !window.timedOut {
		abnormal, evidence, reason := evaluateFlat(window.samples, policy)
		if reason != "" {
			insufficient[ruleFlatSignal] = reason
		} else {
			events = append(events, e.transitionLocked(state, ruleFlatSignal, abnormal, window.completedAt, evidence))
		}
	}
	if policy.enabled(ruleFrequency) && !window.timedOut {
		abnormal, evidence, reason := evaluateFrequency(window.samples, policy)
		if reason != "" {
			insufficient[ruleFrequency] = reason
		} else {
			events = append(events, e.transitionLocked(state, ruleFrequency, abnormal, window.completedAt, evidence))
		}
	}
	state.deepInsufficient = insufficient
	shard.mu.Unlock()
	e.emitEvents(events)
	return deepEvaluationResult{insufficient: insufficient}
}

func evaluateMandatory(samples []deepSample, fields []string) (bool, map[string]any) {
	missing := make(map[string]struct{})
	missingCounts := make(map[string]int)
	examples := make([]map[string]any, 0, 3)
	count := 0
	for _, sample := range samples {
		sampleMissing := make([]string, 0)
		for _, field := range fields {
			if entry, ok := sample.fields[field]; !ok || !entry.present {
				missing[field] = struct{}{}
				missingCounts[field]++
				sampleMissing = append(sampleMissing, field)
				count++
			}
		}
		if len(sampleMissing) > 0 && len(examples) < 3 {
			example := map[string]any{"missingFields": sampleMissing, "observedAt": sample.ingestedAt.UnixMilli()}
			if sample.sourceTimeSet {
				example["sourceTimestamp"] = sample.sourceTime.UnixMilli()
			}
			addPayloadEvidence(example, "payloadPreview", sample.payload)
			examples = append(examples, example)
		}
	}
	evidence := map[string]any{
		"fields": sortedStrings(missing), "missingCountsByField": missingCounts,
		"count": count, "sampleCount": len(samples), "configuredFields": fields,
	}
	if len(examples) > 0 {
		evidence["missingExamples"] = examples
	}
	return count > 0, evidence
}

func evaluateInvalid(samples []deepSample) (bool, map[string]any) {
	fields := make(map[string]struct{})
	codes := make(map[string]struct{})
	count := 0
	badQualityCount := 0
	invalidExamples := make(map[string]string)
	var representative evidencePayloadPreview
	for _, sample := range samples {
		if sample.qualityCode == 3 {
			codes["badQuality"] = struct{}{}
			badQualityCount++
			count++
			if len(representative.values) == 0 {
				representative = sample.payload
			}
		}
		for field, entry := range sample.fields {
			if entry.invalidCode == "" {
				continue
			}
			fields[field] = struct{}{}
			codes[entry.invalidCode] = struct{}{}
			count++
			if _, exists := invalidExamples[field]; !exists {
				invalidExamples[field] = entry.preview
			}
			if len(representative.values) == 0 {
				representative = sample.payload
			}
		}
	}
	evidence := map[string]any{
		"fields": sortedStrings(fields), "errorCodes": sortedStrings(codes), "count": count,
		"badQualityCount": badQualityCount, "sampleCount": len(samples), "invalidExamples": invalidExamples,
	}
	addPayloadEvidence(evidence, "payloadPreview", representative)
	return count > 0, evidence
}

func evaluateFlat(samples []deepSample, policy PolicySnapshot) (bool, map[string]any, string) {
	minimum := policy.FlatMinSamples
	if minimum < 2 {
		minimum = policy.MinSamples
	}
	if minimum < 2 {
		minimum = 3
	}
	flatFields := make(map[string]struct{})
	flatFieldDetails := make([]map[string]any, 0, len(policy.FlatFields))
	evaluatedFields := 0
	maxFlatDuration := time.Duration(0)
	for _, field := range policy.FlatFields {
		count := 0
		var firstHash uint64
		var firstAt, lastAt time.Time
		unchanged := true
		for _, sample := range samples {
			if sample.replayed {
				continue
			}
			entry, ok := sample.fields[field]
			if !ok || !entry.present || entry.invalidCode != "" {
				continue
			}
			if count == 0 {
				firstHash = entry.hash
				firstAt = sample.ingestedAt
			} else if entry.hash != firstHash {
				unchanged = false
			}
			lastAt = sample.ingestedAt
			count++
		}
		if count < minimum {
			continue
		}
		evaluatedFields++
		duration := lastAt.Sub(firstAt)
		if unchanged && duration >= policy.FlatMinDuration {
			flatFields[field] = struct{}{}
			if duration > maxFlatDuration {
				maxFlatDuration = duration
			}
			valuePreview := ""
			for _, sample := range samples {
				if entry, ok := sample.fields[field]; ok && entry.present && entry.invalidCode == "" {
					valuePreview = entry.preview
					break
				}
			}
			flatFieldDetails = append(flatFieldDetails, map[string]any{
				"field": field, "valuePreview": valuePreview, "sampleCount": count, "durationMs": duration.Milliseconds(),
			})
		}
	}
	if evaluatedFields == 0 {
		return false, nil, "configuredFieldMissing"
	}
	evidence := map[string]any{
		"fields": sortedStrings(flatFields), "fieldCount": len(flatFields), "sampleCount": len(samples),
		"durationMs": maxFlatDuration.Milliseconds(), "minimumSamples": minimum,
		"minimumDurationMs": policy.FlatMinDuration.Milliseconds(), "flatFieldDetails": flatFieldDetails,
	}
	if len(samples) > 0 {
		addPayloadEvidence(evidence, "payloadPreview", samples[len(samples)-1].payload)
	}
	return len(flatFields) > 0, evidence, ""
}

func evaluateFrequency(samples []deepSample, policy PolicySnapshot) (bool, map[string]any, string) {
	if policy.ExpectedInterval <= 0 {
		return false, nil, "expectedIntervalMissing"
	}
	minimum := policy.MinSamples
	if minimum <= 1 {
		minimum = 3
	}
	intervals := make([]time.Duration, 0, len(samples)-1)
	var previous time.Time
	var firstSourceTime, lastSourceTime time.Time
	var representative evidencePayloadPreview
	var intervalTotal time.Duration
	for _, sample := range samples {
		if sample.replayed || !sample.sourceTimeSet {
			continue
		}
		if !previous.IsZero() {
			delta := sample.sourceTime.Sub(previous)
			if delta > 0 {
				intervals = append(intervals, delta)
				intervalTotal += delta
			}
		}
		if sample.sourceTime.After(previous) {
			if firstSourceTime.IsZero() {
				firstSourceTime = sample.sourceTime
			}
			previous = sample.sourceTime
			lastSourceTime = sample.sourceTime
			representative = sample.payload
		}
	}
	if len(intervals)+1 < minimum {
		return false, nil, "sourceTimestampMissing"
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i] < intervals[j] })
	observed := medianDuration(intervals)
	deviation := math.Abs(float64(observed-policy.ExpectedInterval)) / float64(policy.ExpectedInterval) * 100
	tolerance := policy.FrequencyTolerancePercent
	if tolerance <= 0 {
		tolerance = 20
	}
	meanObserved := intervalTotal / time.Duration(len(intervals))
	minimumObserved, maximumObserved := intervals[0], intervals[len(intervals)-1]
	allowedMinimum := float64(policy.ExpectedInterval) * float64(100-tolerance) / 100
	if allowedMinimum < 0 {
		allowedMinimum = 0
	}
	allowedMaximum := float64(policy.ExpectedInterval) * float64(100+tolerance) / 100
	evidence := map[string]any{
		"expectedMs": policy.ExpectedInterval.Milliseconds(), "observedMs": observed.Milliseconds(),
		"deviationPercent": math.Round(deviation*100) / 100, "tolerancePercent": tolerance,
		"sampleCount": len(intervals) + 1, "intervalCount": len(intervals), "aggregationMethod": "median",
		"meanObservedMs": meanObserved.Milliseconds(), "minimumObservedMs": minimumObserved.Milliseconds(),
		"maximumObservedMs": maximumObserved.Milliseconds(), "allowedMinimumMs": time.Duration(allowedMinimum).Milliseconds(),
		"allowedMaximumMs":           time.Duration(allowedMaximum).Milliseconds(),
		"windowStartSourceTimestamp": firstSourceTime.UnixMilli(), "windowEndSourceTimestamp": lastSourceTime.UnixMilli(),
	}
	addPayloadEvidence(evidence, "payloadPreview", representative)
	return deviation > float64(tolerance), evidence, ""
}

func medianDuration(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

func activeRuleCount(values map[string]bool) int {
	total := 0
	for _, active := range values {
		if active {
			total++
		}
	}
	return total
}
