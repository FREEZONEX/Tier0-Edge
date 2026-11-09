package dataquality

import (
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/domain/dataingest"
)

const (
	deepSampleTarget            = 100
	defaultDeepMaxCollectWindow = 10 * time.Minute
)

type deepFieldSample struct {
	hash        uint64
	present     bool
	invalidCode string
	preview     string
}

type deepSample struct {
	sourceTime    time.Time
	sourceTimeSet bool
	ingestedAt    time.Time
	replayed      bool
	qualityCode   int64
	fields        map[string]deepFieldSample
	payload       evidencePayloadPreview
}

type deepWindow struct {
	unsID       int64
	policy      PolicySnapshot
	startedAt   time.Time
	completedAt time.Time
	samples     []deepSample
	timedOut    bool
}

type deepEvaluationResult struct {
	insufficient map[string]string
}

type DeepStatus struct {
	State              string
	SampleCount        int
	SampleTarget       int
	StartedAt          *time.Time
	CompletedAt        *time.Time
	Reason             string
	LastResult         string
	MaxCollectWindowMs int64
}

type deepNode struct {
	policy      PolicySnapshot
	state       string
	queuedAt    time.Time
	queueOrder  uint64
	startedAt   time.Time
	completedAt time.Time
	immediate   bool
	sampleCount int
	reason      string
	lastResult  string
}

type deepSampler struct {
	mu               sync.Mutex
	maxSlots         int
	queueSequence    uint64
	nodes            map[int64]*deepNode
	samples          map[int64][]deepSample
	evalQ            chan deepWindow
	stop             chan struct{}
	done             chan struct{}
	evaluate         func(deepWindow) deepEvaluationResult
	drop             func(int64)
	maxCollectWindow time.Duration
}

func newDeepSampler(maxSlots int) *deepSampler {
	if maxSlots <= 0 {
		maxSlots = 16
	}
	d := &deepSampler{
		maxSlots:         maxSlots,
		nodes:            make(map[int64]*deepNode),
		samples:          make(map[int64][]deepSample),
		evalQ:            make(chan deepWindow, maxSlots),
		stop:             make(chan struct{}),
		done:             make(chan struct{}),
		maxCollectWindow: defaultDeepMaxCollectWindow,
	}
	go d.run()
	return d
}

func (d *deepSampler) SetMaxCollectWindow(value time.Duration) {
	if d == nil {
		return
	}
	if value <= 0 {
		value = defaultDeepMaxCollectWindow
	}
	d.mu.Lock()
	d.maxCollectWindow = value
	d.mu.Unlock()
}

func (d *deepSampler) Close() {
	if d == nil {
		return
	}
	select {
	case <-d.stop:
		return
	default:
		close(d.stop)
		<-d.done
	}
}

func (d *deepSampler) Configure(policy PolicySnapshot) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !hasDeepRule(policy) {
		delete(d.nodes, policy.UnsID)
		delete(d.samples, policy.UnsID)
		d.promoteLocked()
		return
	}
	now := time.Now().UTC()
	node := d.nodes[policy.UnsID]
	if node == nil {
		node = &deepNode{state: "waiting", queuedAt: now}
		d.nodes[policy.UnsID] = node
	} else {
		delete(d.samples, policy.UnsID)
		node.state = "waiting"
		node.queuedAt = now
		node.startedAt = time.Time{}
		node.completedAt = time.Time{}
		node.sampleCount = 0
		node.reason = ""
		node.lastResult = ""
	}
	node.policy = policy
	d.queueSequence++
	node.queueOrder = d.queueSequence
	d.promoteLocked()
}

func (d *deepSampler) Remove(unsID int64) {
	if d == nil {
		return
	}
	d.mu.Lock()
	delete(d.nodes, unsID)
	delete(d.samples, unsID)
	d.promoteLocked()
	d.mu.Unlock()
}

func (d *deepSampler) Request(unsID int64) (DeepStatus, bool) {
	if d == nil {
		return DeepStatus{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	node := d.nodes[unsID]
	if node == nil {
		return DeepStatus{}, false
	}
	if node.state != "running" {
		node.state = "waiting"
		node.queuedAt = time.Now().UTC()
		d.queueSequence++
		node.queueOrder = d.queueSequence
		node.startedAt = time.Time{}
		node.sampleCount = 0
		node.reason = ""
		delete(d.samples, unsID)
	}
	node.immediate = true
	d.promoteLocked()
	status := deepStatusLocked(node)
	status.MaxCollectWindowMs = d.maxCollectWindow.Milliseconds()
	return status, true
}

func (d *deepSampler) Observe(unsID int64, observation dataingest.QualityObservation) {
	if d == nil {
		return
	}
	d.mu.Lock()
	node := d.nodes[unsID]
	if node == nil || node.state != "running" {
		d.mu.Unlock()
		return
	}
	sample := summarizeDeepObservation(node.policy, observation)
	samples := append(d.samples[unsID], sample)
	node.sampleCount = len(samples)
	if len(samples) < deepSampleTarget {
		d.samples[unsID] = samples
		d.mu.Unlock()
		return
	}
	delete(d.samples, unsID)
	completedAt := time.Now().UTC()
	window := deepWindow{
		unsID:       unsID,
		policy:      node.policy,
		startedAt:   node.startedAt,
		completedAt: completedAt,
		samples:     samples,
	}
	node.state = "completed"
	node.completedAt = completedAt
	node.queuedAt = completedAt
	d.queueSequence++
	node.queueOrder = d.queueSequence
	node.sampleCount = len(samples)
	node.immediate = false
	node.lastResult = "completed"
	dropped := false
	select {
	case d.evalQ <- window:
	default:
		node.state = "insufficientEvidence"
		node.reason = "evaluationQueueFull"
		node.lastResult = "insufficientEvidence"
		dropped = true
	}
	d.promoteLocked()
	drop := d.drop
	d.mu.Unlock()
	if dropped && drop != nil {
		drop(unsID)
	}
}

func (d *deepSampler) Status(unsID int64) DeepStatus {
	if d == nil {
		return DeepStatus{State: "inactive", SampleTarget: deepSampleTarget, MaxCollectWindowMs: defaultDeepMaxCollectWindow.Milliseconds()}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	node := d.nodes[unsID]
	if node == nil {
		return DeepStatus{State: "inactive", SampleTarget: deepSampleTarget, MaxCollectWindowMs: d.maxCollectWindow.Milliseconds()}
	}
	status := deepStatusLocked(node)
	status.MaxCollectWindowMs = d.maxCollectWindow.Milliseconds()
	return status
}

func (d *deepSampler) Size(unsID int64) int {
	return d.Status(unsID).SampleCount
}

func deepStatusLocked(node *deepNode) DeepStatus {
	status := DeepStatus{
		State: node.state, SampleCount: node.sampleCount, SampleTarget: deepSampleTarget,
		Reason: node.reason, LastResult: node.lastResult,
	}
	if !node.startedAt.IsZero() {
		value := node.startedAt
		status.StartedAt = &value
	}
	if !node.completedAt.IsZero() {
		value := node.completedAt
		status.CompletedAt = &value
	}
	return status
}

func (d *deepSampler) promoteLocked() {
	for len(d.samples) < d.maxSlots {
		var selectedID int64
		var selected *deepNode
		for unsID, node := range d.nodes {
			if node.state == "running" {
				continue
			}
			if selected == nil || deepNodeBefore(node, unsID, selected, selectedID) {
				selectedID, selected = unsID, node
			}
		}
		if selected == nil {
			return
		}
		selected.state = "running"
		selected.startedAt = time.Now().UTC()
		selected.sampleCount = 0
		selected.immediate = false
		d.samples[selectedID] = make([]deepSample, 0, deepSampleTarget)
	}
}

func deepNodeBefore(left *deepNode, leftID int64, right *deepNode, rightID int64) bool {
	if left.immediate != right.immediate {
		return left.immediate
	}
	if !left.queuedAt.Equal(right.queuedAt) {
		return left.queuedAt.Before(right.queuedAt)
	}
	if left.policy.SamplingPriority != right.policy.SamplingPriority {
		return left.policy.SamplingPriority > right.policy.SamplingPriority
	}
	// Coarse clocks can give multiple rotations the same timestamp. Keep FIFO
	// order so a small UNS ID cannot repeatedly take the next collection slot.
	if left.queueOrder != right.queueOrder {
		return left.queueOrder < right.queueOrder
	}
	return leftID < rightID
}

func (d *deepSampler) run() {
	defer close(d.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case window := <-d.evalQ:
			d.evaluateWindow(window)
		case now := <-ticker.C:
			d.expire(now.UTC())
		case <-d.stop:
			for {
				select {
				case window := <-d.evalQ:
					d.evaluateWindow(window)
				default:
					return
				}
			}
		}
	}
}

func (d *deepSampler) evaluateWindow(window deepWindow) {
	result := deepEvaluationResult{}
	if d.evaluate != nil {
		result = d.evaluate(window)
	}
	d.mu.Lock()
	if node := d.nodes[window.unsID]; node != nil && node.policy.revision == window.policy.revision {
		if len(result.insufficient) > 0 {
			node.lastResult = "insufficientEvidence"
			node.reason = firstInsufficientReason(result.insufficient)
			if node.state != "running" {
				node.state = "insufficientEvidence"
			}
		} else {
			node.lastResult = "completed"
			node.reason = ""
			if node.state != "running" {
				node.state = "completed"
			}
		}
		node.completedAt = window.completedAt
		d.promoteLocked()
	}
	d.mu.Unlock()
}

func (d *deepSampler) expire(now time.Time) {
	d.mu.Lock()
	windowLimit := d.maxCollectWindow
	if windowLimit <= 0 {
		windowLimit = defaultDeepMaxCollectWindow
	}
	timedOut := make([]deepWindow, 0)
	dropped := make([]int64, 0)
	for unsID, node := range d.nodes {
		if node.state != "running" || node.startedAt.IsZero() || now.Sub(node.startedAt) < windowLimit {
			continue
		}
		samples := d.samples[unsID]
		delete(d.samples, unsID)
		timedOut = append(timedOut, deepWindow{
			unsID: unsID, policy: node.policy, startedAt: node.startedAt,
			completedAt: now, samples: samples, timedOut: true,
		})
		node.state = "insufficientEvidence"
		node.lastResult = "insufficientEvidence"
		node.reason = "collectionTimeout"
		node.completedAt = now
		node.queuedAt = now
		d.queueSequence++
		node.queueOrder = d.queueSequence
		node.sampleCount = len(samples)
	}
	for _, window := range timedOut {
		select {
		case d.evalQ <- window:
		default:
			if node := d.nodes[window.unsID]; node != nil {
				node.reason = "evaluationQueueFull"
			}
			dropped = append(dropped, window.unsID)
		}
	}
	d.promoteLocked()
	drop := d.drop
	d.mu.Unlock()
	if drop != nil {
		for _, unsID := range dropped {
			drop(unsID)
		}
	}
}

func firstInsufficientReason(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return values[keys[0]]
}

func hasDeepRule(policy PolicySnapshot) bool {
	return policy.enabled(ruleFlatSignal) || policy.enabled(ruleFrequency) ||
		policy.enabled(ruleMandatoryMissing) || policy.enabled(ruleInvalidValue)
}

func summarizeDeepObservation(policy PolicySnapshot, observation dataingest.QualityObservation) deepSample {
	payload := newEvidencePayloadPreview(observation.RawValues, policy.SequenceField, observation.Fields)
	fieldTypes := make(map[string]string, len(observation.Fields))
	for _, field := range observation.Fields {
		fieldTypes[field.Name] = field.Type
	}
	configured := make([]string, 0, 32)
	seen := make(map[string]struct{}, 32)
	for _, field := range append(append([]string{}, policy.MandatoryFields...), policy.FlatFields...) {
		if _, ok := seen[field]; !ok && len(configured) < 32 {
			seen[field] = struct{}{}
			configured = append(configured, field)
		}
	}
	if policy.enabled(ruleInvalidValue) {
		for _, field := range observation.Fields {
			if _, ok := seen[field.Name]; ok || len(configured) >= 32 {
				continue
			}
			seen[field.Name] = struct{}{}
			configured = append(configured, field.Name)
		}
	}
	fields := make(map[string]deepFieldSample, len(configured))
	for _, field := range configured {
		raw, present := observation.RawValues[field]
		entry := deepFieldSample{present: present}
		if present {
			entry.hash = hashFieldValue(raw)
			entry.invalidCode = invalidFieldCode(fieldTypes[field], raw)
			entry.preview = safeEvidencePayloadValue(field, raw, policy.SequenceField)
		}
		fields[field] = entry
	}
	return deepSample{
		sourceTime:    observation.SourceTime,
		sourceTimeSet: observation.SourceTimeSet,
		ingestedAt:    observation.IngestedAt,
		replayed:      observation.Replayed,
		qualityCode:   observation.QualityCode,
		fields:        fields,
		payload:       payload,
	}
}

func hashFieldValue(value string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(value))
	return h.Sum64()
}

func invalidFieldCode(kind, raw string) string {
	raw = strings.TrimSpace(raw)
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "int", "integer":
		if _, err := strconv.ParseInt(raw, 10, 32); err != nil {
			return "invalidInteger"
		}
	case "long", "bigint":
		if _, err := strconv.ParseInt(raw, 10, 64); err != nil {
			return "invalidLong"
		}
	case "float", "double", "decimal", "number":
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return "invalidNumber"
		}
	case "bool", "boolean":
		if _, err := strconv.ParseBool(raw); err != nil {
			return "invalidBoolean"
		}
	case "datetime", "date", "time", "timestamp":
		if !validDeepTime(raw) {
			return "invalidTime"
		}
	}
	return ""
}

func validDeepTime(raw string) bool {
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil && value > 1_000_000_000 {
		return true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.000", "2006-01-02 15:04:05"} {
		if _, err := time.Parse(layout, raw); err == nil {
			return true
		}
	}
	return false
}

func sortedStrings(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
