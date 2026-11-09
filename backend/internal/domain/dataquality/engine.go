package dataquality

import (
	"container/heap"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/internal/domain/dataingest"
)

const (
	ruleDuplicate             = "duplicate"
	ruleLateArrival           = "lateArrival"
	ruleTimestampGap          = "timestampGap"
	ruleOutOfOrder            = "outOfOrder"
	ruleMissingTelemetry      = "missingTelemetry"
	ruleStaleSignal           = "staleSignal"
	ruleIngestionDrop         = "ingestionDrop"
	ruleFlatSignal            = "flatSignal"
	ruleFrequency             = "frequencyDeviation"
	ruleMandatoryMissing      = "mandatoryFieldMissing"
	ruleInvalidValue          = "invalidValue"
	ruleValueRange            = "valueRange"
	ruleCollectionInterrupted = "collectionInterrupted"
	ruleConnectorFailure      = "connectorFailure"
	evidencePayloadFieldLimit = 8
	evidencePayloadValueLimit = 96
	sourceIntervalWindowSize  = 16
	maxEvaluationFields       = 32
	issueRecoveryHold         = 30 * time.Second
)

type PolicySnapshot struct {
	UnsID                     int64
	revision                  uint64
	ExpectedInterval          time.Duration
	MissingGrace              time.Duration
	LateThreshold             time.Duration
	ClockSkewTolerance        time.Duration
	GapTolerance              time.Duration
	OutOfOrderTolerance       time.Duration
	SequenceField             string
	FrequencyTolerancePercent int
	MinSamples                int
	FlatMinSamples            int
	FlatMinDuration           time.Duration
	MandatoryFields           []string
	FlatFields                []string
	SamplingPriority          int
	Rules                     map[string]struct{}
	Severity                  map[string]string
	Cooldown                  time.Duration
}

func (p PolicySnapshot) enabled(rule string) bool {
	_, ok := p.Rules[rule]
	return ok
}

type RuleEvent struct {
	UnsID       int64
	Revision    uint64
	RuleCode    string
	Severity    string
	State       string
	ObservedAt  time.Time
	Summary     string
	Evidence    map[string]any
	Occurrences int64
	Transition  bool
}

type NodeStatus struct {
	UnsID                  int64                   `json:"unsId"`
	Phase                  string                  `json:"phase"`
	LastIngestedAt         *time.Time              `json:"lastIngestedAt"`
	MaxSourceTime          *time.Time              `json:"maxSourceTime"`
	ReceivedCount          int64                   `json:"receivedCount"`
	PersistedCount         int64                   `json:"persistedCount"`
	DropCount              int64                   `json:"dropCount"`
	OpenRules              []string                `json:"openRules"`
	DeepSampleSize         int                     `json:"deepSampleSize"`
	DeepSampleTarget       int                     `json:"deepSampleTarget"`
	DeepState              string                  `json:"deepState"`
	DeepStartedAt          *time.Time              `json:"deepStartedAt,omitempty"`
	DeepCompletedAt        *time.Time              `json:"deepCompletedAt,omitempty"`
	DeepReason             string                  `json:"deepReason,omitempty"`
	DeepLastResult         string                  `json:"deepLastResult,omitempty"`
	DeepMaxCollectWindowMs int64                   `json:"deepMaxCollectWindowMs"`
	InsufficientEvidence   map[string]string       `json:"insufficientEvidence,omitempty"`
	CollectionState        string                  `json:"collectionState"`
	ConnectorSignals       []ConnectorSignalStatus `json:"connectorSignals"`
}

type ConnectorSignalStatus struct {
	Source     string    `json:"source"`
	State      string    `json:"state"`
	ObservedAt time.Time `json:"observedAt"`
	ErrorCode  string    `json:"errorCode,omitempty"`
	FlowID     int64     `json:"flowId,omitempty"`
}

type fingerprint struct {
	identity   string
	hash       uint64
	observedAt time.Time
	payload    evidencePayloadPreview
}

type evidencePayloadPreview struct {
	values     map[string]string
	fieldCount int
	truncated  bool
}

type valueRangeViolation struct {
	field         string
	observedValue float64
	lowerLimit    *float64
	upperLimit    *float64
	direction     string
}

func newEvidencePayloadPreview(
	values map[string]string,
	sequenceField string,
	fields []dataingest.Field,
) evidencePayloadPreview {
	preview := evidencePayloadPreview{fieldCount: len(values)}
	if len(values) == 0 {
		return preview
	}
	fieldTypes := make(map[string]string, len(fields))
	for _, field := range fields {
		fieldTypes[field.Name] = field.Type
	}
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if evidencePayloadFieldAllowed(key, value, sequenceField, fieldTypes[key]) {
			keys = append(keys, key)
		} else {
			preview.truncated = true
		}
	}
	sort.Strings(keys)
	if len(keys) > evidencePayloadFieldLimit {
		keys = keys[:evidencePayloadFieldLimit]
		preview.truncated = true
	}
	preview.values = make(map[string]string, len(keys))
	for _, key := range keys {
		preview.values[key] = safeEvidencePayloadValue(key, values[key], sequenceField)
	}
	return preview
}

func evidencePayloadFieldAllowed(key, value, sequenceField, kind string) bool {
	normalizedKey := normalizedEvidenceField(key)
	if sensitiveEvidenceField(normalizedKey) || normalizedKey == normalizedEvidenceField(sequenceField) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "int", "integer", "long", "bigint", "float", "double", "decimal", "number",
		"bool", "boolean", "datetime", "date", "time", "timestamp":
		return true
	}
	for _, marker := range []string{
		"value", "temperature", "pressure", "speed", "flow", "current", "voltage", "power", "status",
		"state", "mode", "code", "quality", "alarm", "event", "level", "rate", "count", "total",
		"humidity", "vibration", "torque", "position", "frequency", "timestamp", "time", "sequence",
	} {
		if strings.Contains(normalizedKey, marker) {
			return true
		}
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
		return true
	}
	if _, err := strconv.ParseBool(strings.TrimSpace(value)); err == nil {
		return true
	}
	return false
}

func normalizedEvidenceField(key string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.TrimSpace(key)))
}

func sensitiveEvidenceField(normalizedKey string) bool {
	if normalizedKey == "" {
		return false
	}
	for _, marker := range []string{
		"password", "passwd", "pwd", "secret", "token", "credential", "authorization", "auth", "cookie",
		"apikey", "accesskey", "privatekey", "email", "phone", "mobile", "idcard", "ssn", "operatorname",
		"firstname", "lastname", "fullname", "address", "username", "userid", "account", "customer",
		"employee", "contact", "birthday", "birthdate", "passport", "bankcard",
	} {
		if strings.Contains(normalizedKey, marker) {
			return true
		}
	}
	return false
}

func safeEvidencePayloadValue(key, value, sequenceField string) string {
	normalizedKey := normalizedEvidenceField(key)
	sequenceKey := normalizedEvidenceField(sequenceField)
	if sensitiveEvidenceField(normalizedKey) || (sequenceKey != "" && normalizedKey == sequenceKey) {
		return "[REDACTED]"
	}
	normalizedValue := strings.ToLower(strings.TrimSpace(value))
	for _, marker := range []string{"bearer ", "password", "passwd", "token", "secret", "-----begin ", "api_key="} {
		if strings.Contains(normalizedValue, marker) {
			return "[REDACTED]"
		}
	}
	return truncateRunes(value, evidencePayloadValueLimit)
}

func addPayloadEvidence(evidence map[string]any, key string, preview evidencePayloadPreview) {
	if len(preview.values) == 0 {
		return
	}
	values := make(map[string]string, len(preview.values))
	for field, value := range preview.values {
		values[field] = value
	}
	evidence[key] = values
	evidence[key+"FieldCount"] = preview.fieldCount
	if preview.truncated {
		evidence[key+"Truncated"] = true
	}
}

func valueRangeViolations(observation dataingest.QualityObservation) (bool, bool, []valueRangeViolation) {
	configured, complete := false, true
	violations := make([]valueRangeViolation, 0)
	for _, field := range observation.Fields {
		if field.LowerLimit == nil && field.UpperLimit == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(field.Type)) {
		case "int", "integer", "long", "bigint", "float", "double", "decimal", "number":
		default:
			continue
		}
		configured = true
		if field.LowerLimit != nil && field.UpperLimit != nil && *field.LowerLimit > *field.UpperLimit {
			complete = false
			continue
		}
		raw, present := observation.RawValues[field.Name]
		if !present || strings.TrimSpace(raw) == "" || invalidFieldCode(field.Type, raw) != "" {
			complete = false
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			complete = false
			continue
		}
		violation := valueRangeViolation{
			field: field.Name, observedValue: value,
			lowerLimit: field.LowerLimit, upperLimit: field.UpperLimit,
		}
		switch {
		case field.LowerLimit != nil && value < *field.LowerLimit:
			violation.direction = "belowLower"
		case field.UpperLimit != nil && value > *field.UpperLimit:
			violation.direction = "aboveUpper"
		default:
			continue
		}
		violations = append(violations, violation)
	}
	return configured, complete, violations
}

func valueRangeEvidence(violations []valueRangeViolation, observedAt time.Time, payload evidencePayloadPreview) map[string]any {
	if len(violations) == 0 {
		return nil
	}
	details := make([]map[string]any, 0, len(violations))
	fields := make([]string, 0, len(violations))
	for _, violation := range violations {
		detail := map[string]any{
			"field": violation.field, "observedValue": violation.observedValue, "direction": violation.direction,
		}
		if violation.lowerLimit != nil {
			detail["lowerLimit"] = *violation.lowerLimit
		}
		if violation.upperLimit != nil {
			detail["upperLimit"] = *violation.upperLimit
		}
		fields = append(fields, violation.field)
		details = append(details, detail)
	}
	primary := details[0]
	evidence := map[string]any{
		"fields": fields, "violations": details, "field": primary["field"],
		"observedValue": primary["observedValue"], "direction": primary["direction"],
		"observedAt": observedAt.UnixMilli(),
	}
	if lower, ok := primary["lowerLimit"]; ok {
		evidence["lowerLimit"] = lower
	}
	if upper, ok := primary["upperLimit"]; ok {
		evidence["upperLimit"] = upper
	}
	addPayloadEvidence(evidence, "currentPayloadPreview", payload)
	return evidence
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func sourceIntervalAverage(state *nodeState, candidate time.Duration) time.Duration {
	total := time.Duration(0)
	count := state.sourceIntervalLen
	for index := 0; index < state.sourceIntervalLen; index++ {
		total += state.sourceIntervals[index]
	}
	if candidate > 0 {
		total += candidate
		count++
	}
	if count == 0 {
		return 0
	}
	return total / time.Duration(count)
}

func recordSourceInterval(state *nodeState, interval time.Duration) {
	if interval <= 0 {
		return
	}
	state.sourceIntervals[state.sourceIntervalPos] = interval
	state.sourceIntervalPos = (state.sourceIntervalPos + 1) % len(state.sourceIntervals)
	if state.sourceIntervalLen < len(state.sourceIntervals) {
		state.sourceIntervalLen++
	}
}

type nodeState struct {
	policy            PolicySnapshot
	lastIngestedAt    time.Time
	maxSourceTime     time.Time
	lastPayload       evidencePayloadPreview
	maxSourcePayload  evidencePayloadPreview
	sourceIntervals   [sourceIntervalWindowSize]time.Duration
	sourceIntervalLen int
	sourceIntervalPos int
	recent            [32]fingerprint
	recentLength      int
	recentPosition    int
	activeRules       map[string]bool
	issueRules        map[string]bool
	recoverySince     map[string]time.Time
	recoveryLastSeen  map[string]time.Time
	ruleLastEmit      map[string]time.Time
	rulePending       map[string]int64
	receivedCount     int64
	persistedCount    int64
	dropCount         int64
	deadlineVersion   uint64
	sourceTimeSeen    bool
	identitySeen      bool
	deepInsufficient  map[string]string
	connectorSignals  map[string]ConnectorSignalStatus
}

type engineShard struct {
	mu    sync.RWMutex
	nodes map[int64]*nodeState
}

type Engine struct {
	shards           []engineShard
	revision         atomic.Uint64
	deadlines        *deadlineScheduler
	deep             *deepSampler
	emit             func(RuleEvent)
	connectorMu      sync.RWMutex
	globalConnectors map[string]ConnectorSignalStatus
}

func newEngine(shardCount, deepSlots int, emit func(RuleEvent)) *Engine {
	if shardCount <= 0 {
		shardCount = 64
	}
	shards := make([]engineShard, shardCount)
	for i := range shards {
		shards[i].nodes = make(map[int64]*nodeState)
	}
	e := &Engine{
		shards: shards, deep: newDeepSampler(deepSlots), emit: emit,
		globalConnectors: make(map[string]ConnectorSignalStatus),
	}
	e.deep.evaluate = e.evaluateDeepWindow
	e.deadlines = newDeadlineScheduler(e.deadlineReached)
	return e
}

func (e *Engine) Close() {
	if e != nil && e.deadlines != nil {
		if e.deep != nil {
			e.deep.Close()
		}
		e.deadlines.Close()
	}
}

func (e *Engine) StateCount() int {
	if e == nil {
		return 0
	}
	total := 0
	for i := range e.shards {
		e.shards[i].mu.RLock()
		total += len(e.shards[i].nodes)
		e.shards[i].mu.RUnlock()
	}
	return total
}

func (e *Engine) SetPolicy(policy PolicySnapshot) {
	if e == nil || policy.UnsID <= 0 {
		return
	}
	e.connectorMu.RLock()
	globalConnectors := make(map[string]ConnectorSignalStatus, len(e.globalConnectors))
	for key, signal := range e.globalConnectors {
		globalConnectors[key] = signal
	}
	shard := e.shard(policy.UnsID)
	shard.mu.Lock()
	state := shard.nodes[policy.UnsID]
	newNode := state == nil
	wasCollectionEnabled, wasConnectorEnabled := false, false
	events := make([]RuleEvent, 0)
	if state == nil {
		state = &nodeState{
			activeRules: make(map[string]bool), ruleLastEmit: make(map[string]time.Time), rulePending: make(map[string]int64),
			issueRules: make(map[string]bool), recoverySince: make(map[string]time.Time), recoveryLastSeen: make(map[string]time.Time),
			connectorSignals: make(map[string]ConnectorSignalStatus),
		}
		shard.nodes[policy.UnsID] = state
	} else {
		wasCollectionEnabled = state.policy.enabled(ruleCollectionInterrupted)
		wasConnectorEnabled = state.policy.enabled(ruleConnectorFailure)
		now := time.Now().UTC()
		for rule, active := range state.issueRules {
			if active && !policy.enabled(rule) {
				events = append(events, e.resolveIssueLocked(state, rule, now))
			}
		}
	}
	if state.connectorSignals == nil {
		state.connectorSignals = make(map[string]ConnectorSignalStatus)
	}
	policy.revision = e.revision.Add(1)
	if policy.revision == 0 {
		policy.revision = e.revision.Add(1)
	}
	state.policy = policy
	for index := range events {
		events[index].Revision = policy.revision
	}
	for key, signal := range globalConnectors {
		state.connectorSignals[key] = signal
	}
	interrupted, failed := connectorFaultsLocked(state)
	now := time.Now().UTC()
	if policy.enabled(ruleCollectionInterrupted) && (newNode || !wasCollectionEnabled) {
		events = append(events, e.transitionLocked(
			state, ruleCollectionInterrupted, interrupted != nil, now, connectorEvidence(interrupted),
		))
	}
	if policy.enabled(ruleConnectorFailure) && (newNode || !wasConnectorEnabled) {
		events = append(events, e.transitionLocked(
			state, ruleConnectorFailure, failed != nil, now, connectorEvidence(failed),
		))
	}
	state.deadlineVersion++
	version := state.deadlineVersion
	last := state.lastIngestedAt
	shard.mu.Unlock()
	e.connectorMu.RUnlock()
	e.deep.Configure(policy)
	if !last.IsZero() && policy.ExpectedInterval > 0 && (policy.enabled(ruleMissingTelemetry) || policy.enabled(ruleStaleSignal)) {
		e.deadlines.Schedule(policy.UnsID, last.Add(policy.ExpectedInterval+policy.MissingGrace), version)
	} else {
		e.deadlines.Remove(policy.UnsID)
	}
	e.emitEvents(events)
}

func (e *Engine) RemovePolicy(unsID int64) {
	if e == nil {
		return
	}
	shard := e.shard(unsID)
	shard.mu.Lock()
	events := make([]RuleEvent, 0)
	if state := shard.nodes[unsID]; state != nil {
		now := time.Now().UTC()
		for rule, active := range state.issueRules {
			if active {
				events = append(events, e.resolveIssueLocked(state, rule, now))
			}
		}
	}
	delete(shard.nodes, unsID)
	shard.mu.Unlock()
	e.deadlines.Remove(unsID)
	e.deep.Remove(unsID)
	e.emitEvents(events)
}

// AcceptsEvent rejects evaluator work queued for an older policy lifecycle.
// A removed node has no current state, so only its final recovery event may be
// drained; after recycle-bin restore, the new global revision prevents that old
// recovery or anomaly from affecting the restored lifecycle.
func (e *Engine) AcceptsEvent(event RuleEvent) bool {
	if e == nil || event.Revision == 0 {
		return true
	}
	shard := e.shard(event.UnsID)
	shard.mu.RLock()
	state := shard.nodes[event.UnsID]
	accepted := state != nil && state.policy.revision == event.Revision
	shard.mu.RUnlock()
	if state == nil {
		return event.State == "resolved"
	}
	return accepted
}

// RestoreActiveRules restores durable open/ack issue state without restoring
// counters, deadlines, or sample windows. The node therefore remains warming
// until new observations arrive. Core Issues must confirm recovery again after
// restart; a single normal observation does not close an existing Issue.
func (e *Engine) RestoreActiveRules(unsID int64, rules []string) {
	if e == nil || len(rules) == 0 {
		return
	}
	shard := e.shard(unsID)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state := shard.nodes[unsID]
	if state == nil {
		return
	}
	for _, rule := range rules {
		if state.policy.enabled(rule) {
			state.activeRules[rule] = true
			state.issueRules[rule] = true
		}
	}
}

func (e *Engine) Observe(observations []dataingest.QualityObservation) {
	if e == nil {
		return
	}
	e.Evaluate(e.Touch(observations))
}

// Touch is the only evaluator work kept on the ingest hot path. It updates
// per-node liveness and the missing-telemetry deadline, then creates an owned,
// bounded observation for asynchronous evaluation. Content hashing is also
// deferred to the worker and runs only for duplicate-enabled nodes.
func (e *Engine) Touch(observations []dataingest.QualityObservation) []dataingest.QualityObservation {
	if e == nil || len(observations) == 0 {
		return nil
	}
	monitored := make([]dataingest.QualityObservation, 0, len(observations))
	for _, observation := range observations {
		shard := e.shard(observation.UnsID)
		shard.mu.Lock()
		state := shard.nodes[observation.UnsID]
		if state == nil {
			shard.mu.Unlock()
			continue
		}
		now := observation.IngestedAt
		if now.IsZero() {
			now = time.Now().UTC()
		}
		state.receivedCount++
		events := make([]RuleEvent, 0, 2)
		deadlineVersion := state.deadlineVersion
		var due time.Time
		policy := state.policy
		if !observation.Replayed {
			previousIngestedAt := state.lastIngestedAt
			state.lastIngestedAt = now
			// A single late sample does not prove that collection has recovered.
			// Two consecutive timely samples recover real-time quality; the Issue
			// remains open until the recovery hold is confirmed by further timely
			// samples. Sparse sources must not flap open/resolved on each arrival.
			allowedSilence := policy.ExpectedInterval + policy.MissingGrace
			missingRecovered := !previousIngestedAt.IsZero() && allowedSilence > 0 &&
				!now.Before(previousIngestedAt) && now.Sub(previousIngestedAt) <= allowedSilence
			if state.issueRules[ruleMissingTelemetry] && missingRecovered {
				events = append(events, e.transitionLocked(state, ruleMissingTelemetry, false, now, nil))
			} else if !missingRecovered {
				resetRecoveryLocked(state, ruleMissingTelemetry)
			}
			if state.activeRules[ruleStaleSignal] {
				events = append(events, e.transitionLocked(state, ruleStaleSignal, false, now, nil))
			}
			state.deadlineVersion++
			deadlineVersion = state.deadlineVersion
			due = now.Add(policy.ExpectedInterval + policy.MissingGrace)
		}
		shard.mu.Unlock()
		monitored = append(monitored, compactQualityObservation(policy, observation))
		if !observation.Replayed && policy.ExpectedInterval > 0 &&
			(policy.enabled(ruleMissingTelemetry) || policy.enabled(ruleStaleSignal)) {
			e.deadlines.Schedule(observation.UnsID, due, deadlineVersion)
		}
		e.emitEvents(events)
	}
	return monitored
}

func compactQualityObservation(policy PolicySnapshot, observation dataingest.QualityObservation) dataingest.QualityObservation {
	compact := observation
	compact.Fields = nil
	compact.RawValues = make(map[string]string)
	compact.CanonicalHash = 0

	selected := make(map[string]struct{}, maxEvaluationFields)
	addField := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, exists := selected[name]; exists {
			return
		}
		if len(selected) >= maxEvaluationFields {
			return
		}
		selected[name] = struct{}{}
		if value, ok := observation.RawValues[name]; ok {
			compact.RawValues[name] = value
		}
	}

	addField(policy.SequenceField)
	if policy.enabled(ruleValueRange) {
		for _, field := range observation.Fields {
			if field.LowerLimit != nil || field.UpperLimit != nil {
				addField(field.Name)
			}
		}
	}
	for _, name := range policy.MandatoryFields {
		addField(name)
	}
	for _, name := range policy.FlatFields {
		addField(name)
	}
	if policy.enabled(ruleInvalidValue) {
		for _, field := range observation.Fields {
			addField(field.Name)
		}
	}
	// Keep a small representative payload for late/order/gap/duplicate evidence
	// even when those rules do not name fields explicitly.
	for _, field := range observation.Fields {
		value, ok := observation.RawValues[field.Name]
		if !ok || !evidencePayloadFieldAllowed(field.Name, value, policy.SequenceField, field.Type) {
			continue
		}
		addField(field.Name)
	}
	for _, field := range observation.Fields {
		if _, ok := selected[field.Name]; ok {
			compact.Fields = append(compact.Fields, field)
		}
	}
	return compact
}

func canonicalQualityHash(values map[string]string) uint64 {
	h := fnv.New64a()
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var length [8]byte
	for _, key := range keys {
		value := values[key]
		binary.LittleEndian.PutUint64(length[:], uint64(len(key)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(key))
		binary.LittleEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(value))
	}
	return h.Sum64()
}

// Evaluate performs payload inspection and rule transitions. Service callers
// run this from bounded workers; Engine.Observe keeps synchronous behavior for
// focused tests and non-ingest callers.
func (e *Engine) Evaluate(observations []dataingest.QualityObservation) {
	if e == nil {
		return
	}
	for _, observation := range observations {
		e.evaluateOne(observation)
	}
}

func (e *Engine) evaluateOne(observation dataingest.QualityObservation) {
	shard := e.shard(observation.UnsID)
	shard.mu.Lock()
	state := shard.nodes[observation.UnsID]
	if state == nil {
		shard.mu.Unlock()
		return
	}
	policy := state.policy
	if policy.enabled(ruleDuplicate) && observation.CanonicalHash == 0 {
		observation.CanonicalHash = canonicalQualityHash(observation.RawValues)
	}
	payload := newEvidencePayloadPreview(observation.RawValues, policy.SequenceField, observation.Fields)
	now := observation.IngestedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !observation.Replayed {
		state.lastPayload = payload
	}

	events := make([]RuleEvent, 0, 8)
	if !observation.Replayed {
		if policy.enabled(ruleValueRange) {
			configured, complete, violations := valueRangeViolations(observation)
			switch {
			case len(violations) > 0:
				events = append(events, e.transitionLocked(
					state, ruleValueRange, true, now, valueRangeEvidence(violations, now, payload),
				))
			case configured && complete:
				events = append(events, e.transitionLocked(state, ruleValueRange, false, now, nil))
			default:
				// Missing/invalid fields are not evidence of sustained recovery.
				resetRecoveryLocked(state, ruleValueRange)
			}
		}
	}

	identity := sourceIdentity(policy, observation)
	if identity != "" {
		state.identitySeen = true
	}
	duplicate := false
	matched := fingerprint{}
	if policy.enabled(ruleDuplicate) && identity != "" {
		for i := 0; i < state.recentLength; i++ {
			fp := state.recent[i]
			if fp.identity == identity && fp.hash == observation.CanonicalHash {
				duplicate = true
				matched = fp
				break
			}
		}
		evidence := map[string]any{
			"sourceIdentity": identity, "canonicalHash": strconv.FormatUint(observation.CanonicalHash, 16),
			"matchBasis": "sourceIdentityAndContentFingerprint", "duplicateSeenAt": now.UnixMilli(),
		}
		addPayloadEvidence(evidence, "currentPayloadPreview", payload)
		if duplicate {
			evidence["firstSeenAt"] = matched.observedAt.UnixMilli()
			addPayloadEvidence(evidence, "matchedPayloadPreview", matched.payload)
		}
		events = append(events, e.transitionLocked(state, ruleDuplicate, duplicate, now, evidence))
	}
	if identity != "" && !duplicate {
		state.recent[state.recentPosition] = fingerprint{
			identity: identity, hash: observation.CanonicalHash, observedAt: now, payload: payload,
		}
		state.recentPosition = (state.recentPosition + 1) % len(state.recent)
		if state.recentLength < len(state.recent) {
			state.recentLength++
		}
	}

	if observation.SourceTimeSet {
		state.sourceTimeSeen = true
		delta := time.Duration(0)
		hasSourceReference := !state.maxSourceTime.IsZero()
		if hasSourceReference {
			delta = observation.SourceTime.Sub(state.maxSourceTime)
		}
		lateBy := now.Sub(observation.SourceTime)
		late := policy.enabled(ruleLateArrival) && policy.LateThreshold > 0 && lateBy > policy.LateThreshold+policy.ClockSkewTolerance
		if policy.enabled(ruleLateArrival) {
			evidence := map[string]any{
				"sourceTimestamp": observation.SourceTime.UnixMilli(), "ingestedAt": now.UnixMilli(),
				"delayMs": lateBy.Milliseconds(), "lateThresholdMs": policy.LateThreshold.Milliseconds(),
				"skewToleranceMs":    policy.ClockSkewTolerance.Milliseconds(),
				"allowedDelayMs":     (policy.LateThreshold + policy.ClockSkewTolerance).Milliseconds(),
				"expectedIntervalMs": policy.ExpectedInterval.Milliseconds(),
			}
			if observedAverage := sourceIntervalAverage(state, delta); observedAverage > 0 {
				evidence["meanObservedIntervalMs"] = observedAverage.Milliseconds()
			}
			addPayloadEvidence(evidence, "currentPayloadPreview", payload)
			events = append(events, e.transitionLocked(state, ruleLateArrival, late, now, evidence))
		}
		if hasSourceReference {
			if policy.enabled(ruleOutOfOrder) {
				outOfOrder := delta < -policy.OutOfOrderTolerance
				evidence := map[string]any{
					"maxSourceTimestamp": state.maxSourceTime.UnixMilli(), "sourceTimestamp": observation.SourceTime.UnixMilli(),
					"deltaMs": delta.Milliseconds(), "behindMs": maxInt64(0, -delta.Milliseconds()),
					"outOfOrderToleranceMs": policy.OutOfOrderTolerance.Milliseconds(),
				}
				addPayloadEvidence(evidence, "referencePayloadPreview", state.maxSourcePayload)
				addPayloadEvidence(evidence, "currentPayloadPreview", payload)
				events = append(events, e.transitionLocked(state, ruleOutOfOrder, outOfOrder, now, evidence))
			}
			if policy.enabled(ruleTimestampGap) && delta > 0 {
				gap := policy.ExpectedInterval > 0 && delta > policy.ExpectedInterval+policy.GapTolerance
				evidence := map[string]any{
					"previousSourceTimestamp": state.maxSourceTime.UnixMilli(), "sourceTimestamp": observation.SourceTime.UnixMilli(),
					"gapMs": delta.Milliseconds(), "expectedMs": policy.ExpectedInterval.Milliseconds(),
					"gapToleranceMs": policy.GapTolerance.Milliseconds(),
					"allowedGapMs":   (policy.ExpectedInterval + policy.GapTolerance).Milliseconds(),
				}
				addPayloadEvidence(evidence, "referencePayloadPreview", state.maxSourcePayload)
				addPayloadEvidence(evidence, "currentPayloadPreview", payload)
				events = append(events, e.transitionLocked(state, ruleTimestampGap, gap, now, evidence))
			}
		}
		if delta > 0 {
			recordSourceInterval(state, delta)
		}
		if observation.SourceTime.After(state.maxSourceTime) {
			state.maxSourceTime = observation.SourceTime
			state.maxSourcePayload = payload
		}
	}
	shard.mu.Unlock()

	e.deep.Observe(observation.UnsID, observation)
	e.emitEvents(events)
}

func (e *Engine) RecordDrop(stage, reason string, observations []dataingest.QualityObservation) {
	type droppedBatch struct {
		count      int64
		observedAt time.Time
		payload    evidencePayloadPreview
	}
	byNode := make(map[int64]droppedBatch, len(observations))
	for _, observation := range observations {
		batch := byNode[observation.UnsID]
		batch.count++
		observedAt := observation.IngestedAt
		if observedAt.IsZero() {
			observedAt = time.Now().UTC()
		}
		if observedAt.After(batch.observedAt) {
			batch.observedAt = observedAt
			batch.payload = newEvidencePayloadPreview(observation.RawValues, "", observation.Fields)
		}
		byNode[observation.UnsID] = batch
	}
	events := make([]RuleEvent, 0, len(byNode))
	for unsID, batch := range byNode {
		shard := e.shard(unsID)
		shard.mu.Lock()
		state := shard.nodes[unsID]
		if state == nil {
			shard.mu.Unlock()
			continue
		}
		state.dropCount += batch.count
		evidence := map[string]any{
			"stage": stage, "reason": reason, "count": state.dropCount,
		}
		addPayloadEvidence(evidence, "currentPayloadPreview", batch.payload)
		events = append(events, e.transitionLocked(state, ruleIngestionDrop, state.policy.enabled(ruleIngestionDrop), batch.observedAt, evidence))
		shard.mu.Unlock()
	}
	e.emitEvents(events)
}

func (e *Engine) RecordPersisted(observations []dataingest.QualityObservation) {
	type persistedBatch struct {
		count      int64
		observedAt time.Time
	}
	byNode := make(map[int64]persistedBatch, len(observations))
	for _, observation := range observations {
		batch := byNode[observation.UnsID]
		batch.count++
		observedAt := observation.IngestedAt
		if observedAt.IsZero() {
			observedAt = time.Now().UTC()
		}
		if observedAt.After(batch.observedAt) {
			batch.observedAt = observedAt
		}
		byNode[observation.UnsID] = batch
	}
	events := make([]RuleEvent, 0, len(byNode))
	for unsID, batch := range byNode {
		shard := e.shard(unsID)
		shard.mu.Lock()
		if state := shard.nodes[unsID]; state != nil {
			state.persistedCount += batch.count
			events = append(events, e.transitionLocked(state, ruleIngestionDrop, false, batch.observedAt, nil))
		}
		shard.mu.Unlock()
	}
	e.emitEvents(events)
}

func (e *Engine) RecordConnectorHealth(signal dataingest.ConnectorHealthSignal) {
	if e == nil {
		return
	}
	status, key, ok := normalizeConnectorSignal(signal)
	if !ok {
		return
	}
	if status.Source == dataingest.ConnectorSourceMQTT {
		e.connectorMu.Lock()
		current, exists := e.globalConnectors[key]
		if exists && current.ObservedAt.After(status.ObservedAt) {
			e.connectorMu.Unlock()
			return
		}
		e.globalConnectors[key] = status
		e.connectorMu.Unlock()
		e.applyConnectorSignalToAll(key, status)
		return
	}

	targets := make(map[int64]struct{}, len(signal.UnsIDs))
	for _, unsID := range signal.UnsIDs {
		if unsID > 0 {
			targets[unsID] = struct{}{}
		}
	}
	events := make([]RuleEvent, 0)
	for i := range e.shards {
		shard := &e.shards[i]
		shard.mu.Lock()
		for unsID, state := range shard.nodes {
			current, exists := state.connectorSignals[key]
			_, targeted := targets[unsID]
			if exists && current.ObservedAt.After(status.ObservedAt) {
				continue
			}
			if targeted && exists && sameConnectorSignal(current, status) {
				continue
			}
			changed := false
			if targeted {
				state.connectorSignals[key] = status
				changed = true
			} else if exists {
				delete(state.connectorSignals, key)
				changed = true
			}
			if !changed {
				continue
			}
			events = append(events, e.connectorTransitionsLocked(state, status)...)
		}
		shard.mu.Unlock()
	}
	e.emitEvents(events)
}

func (e *Engine) applyConnectorSignalToAll(key string, status ConnectorSignalStatus) {
	events := make([]RuleEvent, 0)
	for i := range e.shards {
		shard := &e.shards[i]
		shard.mu.Lock()
		for _, state := range shard.nodes {
			if state.connectorSignals == nil {
				state.connectorSignals = make(map[string]ConnectorSignalStatus)
			}
			current, exists := state.connectorSignals[key]
			if exists && (current.ObservedAt.After(status.ObservedAt) || sameConnectorSignal(current, status)) {
				continue
			}
			state.connectorSignals[key] = status
			events = append(events, e.connectorTransitionsLocked(state, status)...)
		}
		shard.mu.Unlock()
	}
	e.emitEvents(events)
}

func sameConnectorSignal(left, right ConnectorSignalStatus) bool {
	return left.Source == right.Source && left.State == right.State && left.ObservedAt.Equal(right.ObservedAt) &&
		left.ErrorCode == right.ErrorCode && left.FlowID == right.FlowID
}

func normalizeConnectorSignal(signal dataingest.ConnectorHealthSignal) (ConnectorSignalStatus, string, bool) {
	source := strings.TrimSpace(signal.Source)
	state := strings.TrimSpace(signal.State)
	if source != dataingest.ConnectorSourceMQTT && source != dataingest.ConnectorSourceFlow {
		return ConnectorSignalStatus{}, "", false
	}
	validState := source == dataingest.ConnectorSourceMQTT &&
		(state == dataingest.ConnectorStateConnected || state == dataingest.ConnectorStateLost || state == dataingest.ConnectorStateDisabled)
	validState = validState || source == dataingest.ConnectorSourceFlow &&
		(state == dataingest.ConnectorStateDeployed || state == dataingest.ConnectorStateDisabled || state == dataingest.ConnectorStateError)
	if !validState || (source == dataingest.ConnectorSourceFlow && signal.FlowID <= 0) {
		return ConnectorSignalStatus{}, "", false
	}
	observedAt := signal.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	errorCode := strings.TrimSpace(signal.ErrorCode)
	if len(errorCode) > 128 {
		errorCode = errorCode[:128]
	}
	status := ConnectorSignalStatus{
		Source: source, State: state, ObservedAt: observedAt, ErrorCode: errorCode, FlowID: signal.FlowID,
	}
	key := source
	if source == dataingest.ConnectorSourceFlow {
		key += ":" + strconv.FormatInt(signal.FlowID, 10)
	}
	return status, key, true
}

func (e *Engine) connectorTransitionsLocked(state *nodeState, signal ConnectorSignalStatus) []RuleEvent {
	interrupted, failed := connectorFaultsLocked(state)
	events := make([]RuleEvent, 0, 2)
	collectionAbnormal := state.policy.enabled(ruleCollectionInterrupted) && interrupted != nil
	incomingInterruption := signal.State == dataingest.ConnectorStateLost || signal.State == dataingest.ConnectorStateDisabled
	if !collectionAbnormal || !state.activeRules[ruleCollectionInterrupted] || incomingInterruption {
		events = append(events, e.transitionLocked(
			state, ruleCollectionInterrupted, collectionAbnormal,
			signal.ObservedAt, connectorEvidence(interrupted),
		))
	}
	connectorAbnormal := state.policy.enabled(ruleConnectorFailure) && failed != nil
	incomingFailure := signal.State == dataingest.ConnectorStateError
	if !connectorAbnormal || !state.activeRules[ruleConnectorFailure] || incomingFailure {
		events = append(events, e.transitionLocked(
			state, ruleConnectorFailure, connectorAbnormal,
			signal.ObservedAt, connectorEvidence(failed),
		))
	}
	return events
}

func connectorFaultsLocked(state *nodeState) (*ConnectorSignalStatus, *ConnectorSignalStatus) {
	var interrupted, failed *ConnectorSignalStatus
	for _, signal := range state.connectorSignals {
		candidate := signal
		switch signal.State {
		case dataingest.ConnectorStateLost, dataingest.ConnectorStateDisabled:
			if interrupted == nil || signal.ObservedAt.After(interrupted.ObservedAt) {
				interrupted = &candidate
			}
		case dataingest.ConnectorStateError:
			if failed == nil || signal.ObservedAt.After(failed.ObservedAt) {
				failed = &candidate
			}
		}
	}
	return interrupted, failed
}

func connectorEvidence(signal *ConnectorSignalStatus) map[string]any {
	if signal == nil {
		return nil
	}
	evidence := map[string]any{
		"source": signal.Source, "state": signal.State, "observedAt": signal.ObservedAt.UnixMilli(),
	}
	if signal.ErrorCode != "" {
		evidence["errorCode"] = signal.ErrorCode
	}
	if signal.FlowID > 0 {
		evidence["flowId"] = signal.FlowID
	}
	return evidence
}

func (e *Engine) Status(unsID int64) []NodeStatus {
	if e == nil {
		return []NodeStatus{}
	}
	out := make([]NodeStatus, 0)
	for i := range e.shards {
		shard := &e.shards[i]
		shard.mu.RLock()
		for id, state := range shard.nodes {
			if unsID > 0 && id != unsID {
				continue
			}
			deepStatus := e.deep.Status(id)
			collectionState, connectorSignals := connectorStatus(state)
			status := NodeStatus{
				UnsID: id, Phase: phase(state),
				ReceivedCount: state.receivedCount, PersistedCount: state.persistedCount,
				DropCount: state.dropCount, DeepSampleSize: deepStatus.SampleCount,
				DeepSampleTarget: deepStatus.SampleTarget, DeepState: deepStatus.State,
				DeepStartedAt: deepStatus.StartedAt, DeepCompletedAt: deepStatus.CompletedAt,
				DeepReason:     deepStatus.Reason,
				DeepLastResult: deepStatus.LastResult, DeepMaxCollectWindowMs: deepStatus.MaxCollectWindowMs,
				CollectionState: collectionState, ConnectorSignals: connectorSignals,
			}
			if deepStatus.LastResult == "insufficientEvidence" && status.Phase == "healthy" {
				status.Phase = "degraded"
			}
			if !state.lastIngestedAt.IsZero() {
				t := state.lastIngestedAt
				status.LastIngestedAt = &t
			}
			if !state.maxSourceTime.IsZero() {
				t := state.maxSourceTime
				status.MaxSourceTime = &t
			}
			for rule, active := range state.activeRules {
				if active {
					status.OpenRules = append(status.OpenRules, rule)
				}
			}
			if state.policy.enabled(ruleDuplicate) && !state.identitySeen {
				status.InsufficientEvidence = map[string]string{ruleDuplicate: "sourceIdentityMissing"}
			}
			if !state.sourceTimeSeen {
				for _, rule := range []string{ruleLateArrival, ruleTimestampGap, ruleOutOfOrder} {
					if state.policy.enabled(rule) {
						if status.InsufficientEvidence == nil {
							status.InsufficientEvidence = make(map[string]string)
						}
						status.InsufficientEvidence[rule] = "sourceTimestampMissing"
					}
				}
			}
			for rule, reason := range state.deepInsufficient {
				if status.InsufficientEvidence == nil {
					status.InsufficientEvidence = make(map[string]string)
				}
				status.InsufficientEvidence[rule] = reason
			}
			sort.Strings(status.OpenRules)
			out = append(out, status)
		}
		shard.mu.RUnlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UnsID < out[j].UnsID })
	return out
}

// EffectiveQuality returns the current platform quality without mutating the
// source sample or historical time-series rows. P0 detections degrade a usable
// last value to Uncertain; a source Bad value remains Bad. Missing telemetry is
// only active after at least one observation, so this method never invents a
// value for a node that has not produced data.
func (e *Engine) EffectiveQuality(unsID int64, sourceQuality int64) int64 {
	if e == nil {
		return normalizedQualityCode(sourceQuality)
	}
	quality := normalizedQualityCode(sourceQuality)
	shard := e.shard(unsID)
	shard.mu.RLock()
	state := shard.nodes[unsID]
	if state != nil && (state.activeRules[ruleValueRange] || state.activeRules[ruleMissingTelemetry]) && quality < 2 {
		quality = 2
	}
	shard.mu.RUnlock()
	return quality
}

func normalizedQualityCode(quality int64) int64 {
	switch quality {
	case 2, 3:
		return quality
	default:
		return 1
	}
}

func connectorStatus(state *nodeState) (string, []ConnectorSignalStatus) {
	signals := make([]ConnectorSignalStatus, 0, len(state.connectorSignals))
	fault := false
	for _, signal := range state.connectorSignals {
		signals = append(signals, signal)
		if signal.State == dataingest.ConnectorStateLost ||
			signal.State == dataingest.ConnectorStateDisabled ||
			signal.State == dataingest.ConnectorStateError {
			fault = true
		}
	}
	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Source != signals[j].Source {
			return signals[i].Source < signals[j].Source
		}
		return signals[i].FlowID < signals[j].FlowID
	})
	if fault {
		return "interrupted", signals
	}
	if state.activeRules[ruleMissingTelemetry] || state.activeRules[ruleStaleSignal] {
		return "suspected", signals
	}
	if len(signals) == 0 {
		return "unknown", signals
	}
	return "observed", signals
}

func (e *Engine) deadlineReached(unsID int64, version uint64, due time.Time) {
	shard := e.shard(unsID)
	shard.mu.Lock()
	state := shard.nodes[unsID]
	if state == nil || state.deadlineVersion != version || state.lastIngestedAt.IsZero() {
		shard.mu.Unlock()
		return
	}
	delay := due.Sub(state.lastIngestedAt)
	events := make([]RuleEvent, 0, 2)
	evidence := map[string]any{
		"lastSeenAt": state.lastIngestedAt.UnixMilli(), "deadlineAt": due.UnixMilli(), "delayMs": delay.Milliseconds(),
		"expectedIntervalMs": state.policy.ExpectedInterval.Milliseconds(),
		"missingGraceMs":     state.policy.MissingGrace.Milliseconds(),
		"allowedSilenceMs":   (state.policy.ExpectedInterval + state.policy.MissingGrace).Milliseconds(),
	}
	addPayloadEvidence(evidence, "lastPayloadPreview", state.lastPayload)
	if state.policy.enabled(ruleMissingTelemetry) {
		events = append(events, e.transitionLocked(state, ruleMissingTelemetry, true, due, evidence))
	}
	if state.policy.enabled(ruleStaleSignal) {
		events = append(events, e.transitionLocked(state, ruleStaleSignal, true, due, evidence))
	}
	shard.mu.Unlock()
	e.emitEvents(events)
}

func (e *Engine) transitionLocked(state *nodeState, rule string, abnormal bool, observedAt time.Time, evidence map[string]any) RuleEvent {
	// Real-time quality follows observations; the Issue may remain open while
	// normal observations confirm recovery. Do not use issueRules for _quality.
	if abnormal {
		state.activeRules[rule] = true
		resetRecoveryLocked(state, rule)
	} else {
		delete(state.activeRules, rule)
	}
	wasActive := state.issueRules[rule]
	if abnormal {
		if wasActive {
			state.rulePending[rule]++
			cooldown := state.policy.Cooldown
			if cooldown <= 0 {
				cooldown = time.Minute
			}
			if observedAt.Sub(state.ruleLastEmit[rule]) < cooldown {
				return RuleEvent{}
			}
			occurrences := state.rulePending[rule]
			state.rulePending[rule] = 0
			state.ruleLastEmit[rule] = observedAt
			return e.ruleEvent(state, rule, "open", observedAt, evidence, occurrences, false)
		}
		state.issueRules[rule] = true
		state.ruleLastEmit[rule] = observedAt
		state.rulePending[rule] = 0
	} else if wasActive {
		if (rule == ruleValueRange || rule == ruleMissingTelemetry) && !recoveryConfirmedLocked(state, rule, observedAt) {
			return RuleEvent{}
		}
		return e.resolveIssueLocked(state, rule, observedAt)
	} else {
		return RuleEvent{}
	}
	return e.ruleEvent(state, rule, "open", observedAt, evidence, 1, true)
}

func resetRecoveryLocked(state *nodeState, rule string) {
	delete(state.recoverySince, rule)
	delete(state.recoveryLastSeen, rule)
}

// Recovery only advances on fresh, valid observations, never on a timer.
// A configured collection interval also limits gaps between normal samples.
func recoveryConfirmedLocked(state *nodeState, rule string, observedAt time.Time) bool {
	last := state.recoveryLastSeen[rule]
	if !last.IsZero() && !observedAt.After(last) {
		return false
	}
	allowedGap := state.policy.ExpectedInterval + state.policy.MissingGrace
	if last.IsZero() || (state.policy.ExpectedInterval > 0 && observedAt.Sub(last) > allowedGap) {
		state.recoverySince[rule] = observedAt
	}
	state.recoveryLastSeen[rule] = observedAt
	return observedAt.Sub(state.recoverySince[rule]) >= issueRecoveryHold
}

// Disabling/removing a rule explicitly ends its Issue without waiting for
// observations that can no longer be evaluated.
func (e *Engine) resolveIssueLocked(state *nodeState, rule string, observedAt time.Time) RuleEvent {
	delete(state.activeRules, rule)
	delete(state.issueRules, rule)
	resetRecoveryLocked(state, rule)
	occurrences := state.rulePending[rule]
	state.rulePending[rule] = 0
	return e.ruleEvent(state, rule, "resolved", observedAt, nil, occurrences, true)
}

func (e *Engine) ruleEvent(state *nodeState, rule, transition string, observedAt time.Time, evidence map[string]any, occurrences int64, isTransition bool) RuleEvent {
	severity := state.policy.Severity[rule]
	if severity == "" {
		severity = "warning"
	}
	return RuleEvent{
		UnsID:    state.policy.UnsID,
		Revision: state.policy.revision,
		RuleCode: rule, Severity: severity, State: transition,
		ObservedAt: observedAt, Summary: ruleSummary(rule, transition == "open"), Evidence: evidence,
		Occurrences: occurrences, Transition: isTransition,
	}
}

func (e *Engine) emitEvents(events []RuleEvent) {
	if e.emit == nil {
		return
	}
	for _, event := range events {
		if event.RuleCode != "" {
			e.emit(event)
		}
	}
}

func (e *Engine) shard(unsID int64) *engineShard {
	index := uint64(unsID)
	return &e.shards[index%uint64(len(e.shards))]
}

func phase(state *nodeState) string {
	if state.lastIngestedAt.IsZero() {
		return "warming"
	}
	if len(state.connectorSignals) == 0 &&
		(state.policy.enabled(ruleCollectionInterrupted) || state.policy.enabled(ruleConnectorFailure)) {
		return "warming"
	}
	if state.policy.MinSamples > 0 && state.receivedCount < int64(state.policy.MinSamples) {
		return "warming"
	}
	if len(state.activeRules) > 0 {
		return "degraded"
	}
	return "healthy"
}

func sourceIdentity(policy PolicySnapshot, observation dataingest.QualityObservation) string {
	if field := strings.TrimSpace(policy.SequenceField); field != "" {
		if sequence := strings.TrimSpace(observation.RawValues[field]); sequence != "" {
			return "sequenceHash:" + strconv.FormatUint(hashFieldValue(sequence), 16)
		}
	}
	if observation.SourceTimeSet {
		return fmt.Sprintf("sourceTime:%d", observation.SourceTime.UnixNano())
	}
	return ""
}

func ruleSummary(rule string, abnormal bool) string {
	if abnormal {
		return "data quality rule triggered: " + rule
	}
	return "data quality rule recovered: " + rule
}

type deadlineEntry struct {
	unsID   int64
	due     time.Time
	version uint64
	index   int
}

type deadlineHeap []*deadlineEntry

func (h deadlineHeap) Len() int           { return len(h) }
func (h deadlineHeap) Less(i, j int) bool { return h[i].due.Before(h[j].due) }
func (h deadlineHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}
func (h *deadlineHeap) Push(value any) {
	entry := value.(*deadlineEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}
func (h *deadlineHeap) Pop() any {
	old := *h
	entry := old[len(old)-1]
	entry.index = -1
	*h = old[:len(old)-1]
	return entry
}

type deadlineScheduler struct {
	mu     sync.Mutex
	heap   deadlineHeap
	byNode map[int64]*deadlineEntry
	stop   chan struct{}
	done   chan struct{}
	onDue  func(int64, uint64, time.Time)
}

func newDeadlineScheduler(onDue func(int64, uint64, time.Time)) *deadlineScheduler {
	d := &deadlineScheduler{byNode: make(map[int64]*deadlineEntry), stop: make(chan struct{}), done: make(chan struct{}), onDue: onDue}
	heap.Init(&d.heap)
	go d.run()
	return d
}

func (d *deadlineScheduler) Schedule(unsID int64, due time.Time, version uint64) {
	d.mu.Lock()
	if entry := d.byNode[unsID]; entry != nil {
		entry.due, entry.version = due, version
		heap.Fix(&d.heap, entry.index)
	} else {
		entry := &deadlineEntry{unsID: unsID, due: due, version: version}
		d.byNode[unsID] = entry
		heap.Push(&d.heap, entry)
	}
	d.mu.Unlock()
}

func (d *deadlineScheduler) Remove(unsID int64) {
	d.mu.Lock()
	if entry := d.byNode[unsID]; entry != nil {
		heap.Remove(&d.heap, entry.index)
		delete(d.byNode, unsID)
	}
	d.mu.Unlock()
}

func (d *deadlineScheduler) Close() {
	select {
	case <-d.stop:
		return
	default:
		close(d.stop)
		<-d.done
	}
}

func (d *deadlineScheduler) run() {
	defer close(d.done)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-d.stop:
			return
		case now := <-ticker.C:
			for {
				d.mu.Lock()
				if len(d.heap) == 0 || d.heap[0].due.After(now) {
					d.mu.Unlock()
					break
				}
				entry := heap.Pop(&d.heap).(*deadlineEntry)
				delete(d.byNode, entry.unsID)
				d.mu.Unlock()
				if d.onDue != nil {
					d.onDue(entry.unsID, entry.version, entry.due)
				}
			}
		}
	}
}
