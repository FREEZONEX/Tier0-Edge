package dataquality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"backend/internal/config"
	"backend/internal/domain/common"
	"backend/internal/domain/dataingest"
	"backend/internal/repo"

	"github.com/zeromicro/go-zero/core/logx"
)

var (
	ErrInvalidPolicy   = errors.New("DataQuality.invalidPolicy")
	ErrFeatureDisabled = errors.New("DataQuality.featureDisabled")
	ErrNodeLimit       = errors.New("DataQuality.nodeLimit")
	ErrDeepUnavailable = errors.New("DataQuality.deepUnavailable")
)

var allowedRules = map[string]struct{}{
	ruleDuplicate: {}, ruleLateArrival: {}, ruleTimestampGap: {}, ruleOutOfOrder: {},
	ruleMissingTelemetry: {}, ruleStaleSignal: {}, ruleIngestionDrop: {},
	ruleFlatSignal: {}, ruleFrequency: {}, ruleMandatoryMissing: {}, ruleInvalidValue: {},
	ruleValueRange:            {},
	ruleCollectionInterrupted: {}, ruleConnectorFailure: {},
}

type PolicyInput struct {
	UnsID                     int64
	Enabled                   bool
	NotifyOnIssue             bool
	ExpectedIntervalMs        int64
	SequenceField             string
	MissingGraceMs            int64
	LateThresholdMs           int64
	ClockSkewToleranceMs      int64
	GapToleranceMs            int64
	OutOfOrderToleranceMs     int64
	FrequencyTolerancePercent int
	MinSamples                int
	FlatMinSamples            int
	FlatMinDurationMs         int64
	MandatoryFields           []string
	FlatFields                []string
	StaleFields               []string
	Rules                     []string
	SeverityOverrides         map[string]string
	SamplingPriority          int
	CooldownMs                int64
	UserID                    int64
}

type IssueQuery struct {
	UnsID    int64
	State    string
	RuleCode string
	Severity string
	Page     int
	Size     int
}

type NodeStatusView struct {
	NodeStatus
	NodeName    string `json:"nodeName"`
	DisplayName string `json:"displayName"`
	Namespace   string `json:"namespace"`
}

type IssueView struct {
	repo.DataQualityIssue
	NodeName    string `json:"nodeName"`
	DisplayName string `json:"displayName"`
	Namespace   string `json:"namespace"`
}

type StatusSummary struct {
	MonitoredNodes          int     `json:"monitoredNodes"`
	HealthyNodes            int     `json:"healthyNodes"`
	AbnormalNodes           int     `json:"abnormalNodes"`
	WarmingNodes            int     `json:"warmingNodes"`
	ReceivedCount           int64   `json:"receivedCount"`
	PersistedCount          int64   `json:"persistedCount"`
	DropCount               int64   `json:"dropCount"`
	IngestionSuccessPercent float64 `json:"ingestionSuccessPercent"`
	OpenIssues              int64   `json:"openIssues"`
	AckIssues               int64   `json:"ackIssues"`
	CriticalIssues          int64   `json:"criticalIssues"`
}

type nodeMetadata struct {
	name        string
	displayName string
	namespace   string
}

const (
	dataQualityIssueMergeWindow = 5 * time.Minute
)

type Service struct {
	enabled             bool
	maxEnabledNodes     int
	repo                *repo.DataQualityRepo
	unsRepo             *repo.UnsRepo
	engine              *Engine
	policyMu            sync.Mutex
	observationQueues   []chan []dataingest.QualityObservation
	observationStop     chan struct{}
	observationDone     sync.WaitGroup
	issueQueue          chan RuleEvent
	connectorQueue      chan dataingest.ConnectorHealthSignal
	stop                chan struct{}
	done                chan struct{}
	evaluationDrops     atomic.Int64
	connectorEventDrops atomic.Int64
	accepting           atomic.Bool
}

func New(ctx context.Context, cfg config.DataQualityConf) (*Service, error) {
	s := &Service{
		enabled: cfg.Enabled, maxEnabledNodes: cfg.MaxEnabledNodes,
		repo: repo.NewDataQualityRepo(ctx), unsRepo: repo.NewUnsRepo(ctx),
	}
	if s.maxEnabledNodes <= 0 {
		s.maxEnabledNodes = 10000
	}
	if !cfg.Enabled {
		return s, nil
	}
	queueSize := cfg.IssueQueueSize
	if queueSize <= 0 {
		queueSize = 1024
	}
	s.issueQueue = make(chan RuleEvent, queueSize)
	s.connectorQueue = make(chan dataingest.ConnectorHealthSignal, queueSize)
	workerCount := 4
	if cfg.ShardCount > 0 && cfg.ShardCount < workerCount {
		workerCount = cfg.ShardCount
	}
	if workerCount < 1 {
		workerCount = 1
	}
	queueSizePerWorker := queueSize / workerCount
	if queueSizePerWorker < 1 {
		queueSizePerWorker = 1
	}
	s.observationQueues = make([]chan []dataingest.QualityObservation, workerCount)
	for index := range s.observationQueues {
		s.observationQueues[index] = make(chan []dataingest.QualityObservation, queueSizePerWorker)
	}
	s.observationStop = make(chan struct{})
	s.stop, s.done = make(chan struct{}), make(chan struct{})
	s.engine = newEngine(cfg.ShardCount, 16, s.enqueueEvent)
	s.engine.deep.SetMaxCollectWindow(time.Duration(cfg.MaxCollectWindowMs) * time.Millisecond)
	s.engine.SetDeepDropCallback(func(_ int64) { s.evaluationDrops.Add(1) })
	policies, err := s.repo.ListEnabledPolicies(ctx, s.maxEnabledNodes+1)
	if err != nil {
		s.engine.Close()
		return nil, err
	}
	if len(policies) > s.maxEnabledNodes {
		s.engine.Close()
		return nil, ErrNodeLimit
	}
	unsIDs := make([]int64, 0, len(policies))
	for _, policy := range policies {
		s.engine.SetPolicy(policySnapshot(policy))
		unsIDs = append(unsIDs, policy.UnsID)
	}
	activeIssues, err := s.repo.ListActiveIssuesForNodes(ctx, unsIDs)
	if err != nil {
		s.engine.Close()
		return nil, err
	}
	activeRules := make(map[int64][]string)
	for _, issue := range activeIssues {
		activeRules[issue.UnsID] = append(activeRules[issue.UnsID], issue.RuleCode)
	}
	for unsID, rules := range activeRules {
		s.engine.RestoreActiveRules(unsID, rules)
	}
	s.accepting.Store(true)
	for index := range s.observationQueues {
		s.observationDone.Add(1)
		go s.observationWorker(index)
	}
	go s.issueWorker()
	return s, nil
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	if !s.accepting.Swap(false) {
		return
	}
	if s.observationStop != nil {
		close(s.observationStop)
		s.observationDone.Wait()
	}
	if s.engine != nil {
		s.engine.Close()
	}
	if s.stop != nil {
		select {
		case <-s.stop:
		default:
			close(s.stop)
			<-s.done
		}
	}
}

func (s *Service) Enabled() bool { return s != nil && s.enabled }

func (s *Service) RuntimeStateCount() int {
	if s == nil || s.engine == nil {
		return 0
	}
	return s.engine.StateCount()
}

// EffectiveQuality implements dataingest.EffectiveQualityProvider. Reads are
// lock-safe and stay off the ingest path; the async evaluator remains solely
// responsible for changing the platform detection state.
func (s *Service) EffectiveQuality(unsID int64, sourceQuality int64) int64 {
	if s == nil || s.engine == nil {
		return sourceQuality
	}
	return s.engine.EffectiveQuality(unsID, sourceQuality)
}

// ApplyNodePolicies keeps imported, restored, and updated UNS-owned policy
// documents in sync with the in-memory evaluator without requiring a restart.
func (s *Service) ApplyNodePolicies(nodes []repo.UnsNode) {
	if s == nil || !s.enabled || s.engine == nil || !s.accepting.Load() || len(nodes) == 0 {
		return
	}
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	for _, node := range nodes {
		if node.Type == 2 && node.DataQualityPolicy != nil && node.DataQualityPolicy.Enabled {
			policy := *node.DataQualityPolicy
			policy.UnsID = node.ID
			s.engine.SetPolicy(policySnapshot(policy))
			continue
		}
		s.engine.RemovePolicy(node.ID)
	}
}

func (s *Service) RemoveNodePolicies(ctx context.Context, nodes []repo.UnsNode) {
	if s == nil || len(nodes) == 0 {
		return
	}
	unsIDs := make([]int64, 0, len(nodes))
	if s.engine != nil && s.accepting.Load() {
		s.policyMu.Lock()
		for _, node := range nodes {
			s.engine.RemovePolicy(node.ID)
		}
		s.policyMu.Unlock()
	}
	for _, node := range nodes {
		if node.ID > 0 {
			unsIDs = append(unsIDs, node.ID)
		}
	}
	if s.repo != nil {
		if err := s.repo.ResolveActiveIssuesForNodes(ctx, unsIDs, time.Now().UTC()); err != nil {
			logx.WithContext(ctx).Errorf("data quality resolve deleted node issues nodes=%v: %v", unsIDs, err)
		}
	}
}

func (s *Service) Observe(_ context.Context, observations []dataingest.QualityObservation) {
	if s == nil || s.engine == nil || !s.accepting.Load() || len(observations) == 0 {
		return
	}
	monitored := s.engine.Touch(observations)
	if len(monitored) == 0 {
		return
	}
	if len(s.observationQueues) == 0 {
		s.evaluationDrops.Add(int64(len(monitored)))
		return
	}
	byWorker := make([][]dataingest.QualityObservation, len(s.observationQueues))
	for _, observation := range monitored {
		index := int(uint64(observation.UnsID) % uint64(len(s.observationQueues)))
		byWorker[index] = append(byWorker[index], observation)
	}
	for index, batch := range byWorker {
		if len(batch) == 0 {
			continue
		}
		select {
		case s.observationQueues[index] <- batch:
		default:
			s.evaluationDrops.Add(int64(len(batch)))
		}
	}
}

func (s *Service) RecordDrop(_ context.Context, stage, reason string, observations []dataingest.QualityObservation) {
	if s == nil || s.engine == nil {
		return
	}
	s.engine.RecordDrop(stage, reason, observations)
}

func (s *Service) RecordPersisted(_ context.Context, observations []dataingest.QualityObservation) {
	if s == nil || s.engine == nil {
		return
	}
	s.engine.RecordPersisted(observations)
}

func (s *Service) RecordConnectorHealth(_ context.Context, signal dataingest.ConnectorHealthSignal) {
	if s == nil || s.engine == nil || s.connectorQueue == nil || !s.accepting.Load() {
		return
	}
	select {
	case s.connectorQueue <- signal:
	default:
		s.connectorEventDrops.Add(1)
	}
}

func (s *Service) GetPolicy(ctx context.Context, unsID int64) (map[string]any, error) {
	if s == nil || !s.enabled {
		return nil, ErrFeatureDisabled
	}
	policy, err := s.repo.GetPolicy(ctx, unsID)
	if err == nil {
		return policyView(policy), nil
	}
	if !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	node, err := s.unsRepo.GetUnsNode(ctx, unsID, false)
	if err != nil {
		return nil, err
	}
	if node.Type != 2 {
		return nil, ErrInvalidPolicy
	}
	return policyView(defaultPolicy(unsID)), nil
}

func defaultPolicy(unsID int64) repo.DataQualityPolicy {
	return repo.DataQualityPolicy{
		UnsID:                     unsID,
		Enabled:                   false,
		NotifyOnIssue:             false,
		ExpectedIntervalMs:        0,
		MissingGraceMs:            0,
		LateThresholdMs:           5000,
		FrequencyTolerancePercent: 20,
		MinSamples:                3,
		FlatMinSamples:            3,
		FlatMinDurationMs:         2000,
		MandatoryFields:           []string{},
		FlatFields:                []string{},
		StaleFields:               []string{},
		Rules:                     []string{},
		SeverityOverrides:         map[string]string{},
		CooldownMs:                60000,
	}
}

func (s *Service) SavePolicy(ctx context.Context, input PolicyInput) (map[string]any, error) {
	if s == nil || !s.enabled {
		return nil, ErrFeatureDisabled
	}
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	input, err := s.firstVersionPolicyInput(ctx, input)
	if err != nil {
		return nil, err
	}
	policy, err := s.validatePolicy(ctx, input)
	if err != nil {
		return nil, err
	}
	if input.Enabled {
		current, currentErr := s.repo.GetPolicy(ctx, input.UnsID)
		alreadyEnabled := currentErr == nil && current.Enabled
		if currentErr != nil && !errors.Is(currentErr, repo.ErrNotFound) {
			return nil, currentErr
		}
		if !alreadyEnabled {
			total, countErr := s.repo.CountEnabledPolicies(ctx)
			if countErr != nil {
				return nil, countErr
			}
			if total >= int64(s.maxEnabledNodes) {
				return nil, ErrNodeLimit
			}
		}
	}
	saved, err := s.repo.SavePolicy(ctx, policy, input.UserID)
	if err != nil {
		return nil, err
	}
	if s.engine != nil {
		if saved.Enabled {
			s.engine.SetPolicy(policySnapshot(saved))
		} else {
			s.engine.RemovePolicy(saved.UnsID)
		}
	}
	return policyView(saved), nil
}

// firstVersionPolicyInput derives the runtime switches from the only two
// user-facing configurations. Clients no longer control enabled/rules
// independently, so an empty or zero configuration always means disabled.
func (s *Service) firstVersionPolicyInput(ctx context.Context, input PolicyInput) (PolicyInput, error) {
	node, err := s.unsRepo.GetUnsNode(ctx, input.UnsID, false)
	if err != nil {
		return PolicyInput{}, err
	}
	if node.Type != 2 {
		return PolicyInput{}, ErrInvalidPolicy
	}
	input.Rules = FirstVersionRules(node.Schema, input.ExpectedIntervalMs)
	input.Enabled = len(input.Rules) > 0
	input.SequenceField = ""
	input.LateThresholdMs = 0
	input.ClockSkewToleranceMs = 0
	input.GapToleranceMs = 0
	input.OutOfOrderToleranceMs = 0
	input.FrequencyTolerancePercent = 20
	input.MinSamples = 3
	input.FlatMinSamples = 0
	input.FlatMinDurationMs = 0
	input.MandatoryFields = []string{}
	input.FlatFields = []string{}
	input.StaleFields = []string{}
	input.SeverityOverrides = map[string]string{}
	input.SamplingPriority = 0
	input.CooldownMs = 60000
	return input, nil
}

// FirstVersionRules is shared with UNS creation so persisted JSON and runtime
// policy always use the same parameter-driven enablement semantics.
func FirstVersionRules(schema json.RawMessage, expectedIntervalMs int64) []string {
	rules := make([]string, 0, 2)
	var fields []struct {
		LowerLimit *float64 `json:"lowerLimit"`
		UpperLimit *float64 `json:"upperLimit"`
	}
	if json.Unmarshal(schema, &fields) == nil {
		for _, field := range fields {
			if (field.LowerLimit != nil && *field.LowerLimit != 0) || (field.UpperLimit != nil && *field.UpperLimit != 0) {
				rules = append(rules, ruleValueRange)
				break
			}
		}
	}
	if expectedIntervalMs > 0 {
		rules = append(rules, ruleMissingTelemetry)
	}
	return rules
}

func (s *Service) Status(unsID int64) map[string]any {
	items := []NodeStatus{}
	enabled := false
	var evaluationDrops int64
	var connectorEventDrops int64
	if s != nil && s.engine != nil {
		items = s.engine.Status(unsID)
	}
	if s != nil {
		enabled = s.enabled
		evaluationDrops = s.evaluationDrops.Load()
		connectorEventDrops = s.connectorEventDrops.Load()
	}
	return map[string]any{
		"enabled": enabled, "items": items, "evaluationDrops": evaluationDrops,
		"connectorEventDrops": connectorEventDrops,
	}
}

// StatusView enriches the in-memory evaluator status with durable issue
// aggregates and the current UNS metadata needed by the overview page. Status
// remains available for evaluator callers and tests that only need runtime
// state and must not perform database access.
func (s *Service) StatusView(ctx context.Context, unsID int64) (map[string]any, error) {
	data := s.Status(unsID)
	items, _ := data["items"].([]NodeStatus)
	metadata, err := s.nodeMetadata(ctx, statusUnsIDs(items))
	if err != nil {
		return nil, err
	}

	summary := StatusSummary{MonitoredNodes: len(items)}
	views := make([]NodeStatusView, 0, len(items))
	for _, item := range items {
		meta := metadata[item.UnsID]
		views = append(views, NodeStatusView{
			NodeStatus: item, NodeName: meta.name, DisplayName: meta.displayName, Namespace: meta.namespace,
		})
		switch item.Phase {
		case "healthy":
			summary.HealthyNodes++
		case "warming":
			summary.WarmingNodes++
		default:
			summary.AbnormalNodes++
		}
		summary.ReceivedCount += item.ReceivedCount
		summary.PersistedCount += item.PersistedCount
		summary.DropCount += item.DropCount
	}
	if summary.ReceivedCount > 0 {
		summary.IngestionSuccessPercent = math.Round(
			float64(summary.PersistedCount)*10000/float64(summary.ReceivedCount),
		) / 100
	}

	issueSummary, err := s.repo.AggregateActiveIssues(ctx, unsID)
	if err != nil {
		return nil, err
	}
	summary.OpenIssues = issueSummary.OpenIssues
	summary.AckIssues = issueSummary.AckIssues
	summary.CriticalIssues = issueSummary.CriticalIssues
	data["items"] = views
	data["summary"] = summary
	data["activeRuleCounts"] = issueSummary.ActiveRuleCounts
	return data, nil
}

func (s *Service) RequestDeepRun(unsID int64) (map[string]any, error) {
	if s == nil || !s.enabled || s.engine == nil {
		return nil, ErrFeatureDisabled
	}
	status, ok := s.engine.RequestDeepRun(unsID)
	if !ok {
		return nil, ErrDeepUnavailable
	}
	return map[string]any{
		"unsId": unsID, "deepState": status.State, "deepSampleSize": status.SampleCount,
		"deepSampleTarget": status.SampleTarget, "deepStartedAt": status.StartedAt,
		"deepMaxCollectWindowMs": status.MaxCollectWindowMs,
	}, nil
}

func (s *Service) ListIssues(ctx context.Context, query IssueQuery) (map[string]any, error) {
	if s == nil || !s.enabled {
		return nil, ErrFeatureDisabled
	}
	if query.State != "" && query.State != "open" && query.State != "ack" && query.State != "resolved" {
		return nil, ErrInvalidPolicy
	}
	items, total, err := s.repo.ListIssues(ctx, repo.DataQualityIssueFilter{
		UnsID: query.UnsID, State: query.State, RuleCode: query.RuleCode, Severity: query.Severity,
		Page: query.Page, Size: query.Size,
	})
	if err != nil {
		return nil, err
	}
	metadata, err := s.nodeMetadata(ctx, issueUnsIDs(items))
	if err != nil {
		return nil, err
	}
	views := make([]IssueView, 0, len(items))
	for _, item := range items {
		meta := metadata[item.UnsID]
		views = append(views, IssueView{
			DataQualityIssue: item, NodeName: meta.name, DisplayName: meta.displayName, Namespace: meta.namespace,
		})
	}
	page, size := query.Page, query.Size
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	return map[string]any{"list": views, "total": total, "page": page, "size": size}, nil
}

func (s *Service) ActiveSummary(ctx context.Context) (map[string]any, error) {
	if s == nil || !s.enabled {
		return nil, ErrFeatureDisabled
	}
	items, err := s.repo.ListActiveIssueNodeSummaries(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"list": items}, nil
}

func (s *Service) nodeMetadata(ctx context.Context, ids []int64) (map[int64]nodeMetadata, error) {
	result := make(map[int64]nodeMetadata, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	nodes, err := s.unsRepo.GetUnsNodesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		result[node.ID] = nodeMetadata{name: node.Name, displayName: node.DisplayName, namespace: node.Namespace}
	}
	return result, nil
}

func statusUnsIDs(items []NodeStatus) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.UnsID)
	}
	return ids
}

func issueUnsIDs(items []repo.DataQualityIssue) []int64 {
	seen := make(map[int64]struct{}, len(items))
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item.UnsID]; ok {
			continue
		}
		seen[item.UnsID] = struct{}{}
		ids = append(ids, item.UnsID)
	}
	return ids
}

func (s *Service) AcknowledgeIssue(ctx context.Context, id, userID int64) (repo.DataQualityIssue, error) {
	if s == nil || !s.enabled {
		return repo.DataQualityIssue{}, ErrFeatureDisabled
	}
	if id <= 0 || userID <= 0 {
		return repo.DataQualityIssue{}, ErrInvalidPolicy
	}
	return s.repo.AcknowledgeIssue(ctx, id, userID)
}

func (s *Service) validatePolicy(ctx context.Context, input PolicyInput) (repo.DataQualityPolicy, error) {
	if input.UnsID <= 0 {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	rules, err := normalizeStringList(input.Rules, len(allowedRules), func(value string) bool {
		_, ok := allowedRules[value]
		return ok
	})
	if err != nil {
		return repo.DataQualityPolicy{}, err
	}
	if input.Enabled && len(rules) == 0 {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	requiresInterval := containsAny(rules, ruleMissingTelemetry, ruleStaleSignal, ruleTimestampGap, ruleFrequency)
	if input.Enabled && requiresInterval && input.ExpectedIntervalMs <= 0 {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	if input.ExpectedIntervalMs < 0 || input.MissingGraceMs < 0 || input.LateThresholdMs < 0 ||
		input.ClockSkewToleranceMs < 0 || input.GapToleranceMs < 0 || input.OutOfOrderToleranceMs < 0 || input.CooldownMs < 0 {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	mandatory, err := normalizeStringList(input.MandatoryFields, 32, validFieldName)
	if err != nil {
		return repo.DataQualityPolicy{}, err
	}
	flat, err := normalizeStringList(input.FlatFields, 32, validFieldName)
	if err != nil {
		return repo.DataQualityPolicy{}, err
	}
	stale, err := normalizeStringList(input.StaleFields, 32, validFieldName)
	if err != nil || uniqueCount(mandatory, flat, stale) > 32 {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	severities := make(map[string]string, len(input.SeverityOverrides))
	for rule, severity := range input.SeverityOverrides {
		if _, ok := allowedRules[rule]; !ok || (severity != "info" && severity != "warning" && severity != "critical") {
			return repo.DataQualityPolicy{}, ErrInvalidPolicy
		}
		severities[rule] = severity
	}
	if utf8.RuneCountInString(strings.TrimSpace(input.SequenceField)) > common.AliasMaxLen {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	if input.FrequencyTolerancePercent == 0 {
		input.FrequencyTolerancePercent = 20
	}
	if input.MinSamples == 0 {
		input.MinSamples = 3
	}
	if input.CooldownMs == 0 {
		input.CooldownMs = 60000
	}
	if containsAny(rules, ruleLateArrival) && input.LateThresholdMs <= 0 {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	if input.FrequencyTolerancePercent < 0 || input.FrequencyTolerancePercent > 100 || input.MinSamples < 2 || input.MinSamples > 100 ||
		input.FlatMinSamples == 1 || input.FlatMinSamples < 0 || input.FlatMinSamples > 100 || input.FlatMinDurationMs < 0 {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	if containsAny(rules, ruleMandatoryMissing) && len(mandatory) == 0 {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	if containsAny(rules, ruleFlatSignal) && len(flat) == 0 {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	if input.SamplingPriority < -100 || input.SamplingPriority > 100 {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	// Do not accept an enabled policy that cannot take effect in this process.
	// Disabled policies may still be edited while the global feature flag is off.
	if input.Enabled && !s.enabled {
		return repo.DataQualityPolicy{}, ErrFeatureDisabled
	}
	node, err := s.unsRepo.GetUnsNode(ctx, input.UnsID, false)
	if err != nil {
		return repo.DataQualityPolicy{}, err
	}
	if node.Type != 2 {
		return repo.DataQualityPolicy{}, ErrInvalidPolicy
	}
	return repo.DataQualityPolicy{
		UnsID: input.UnsID, Enabled: input.Enabled, NotifyOnIssue: false,
		ExpectedIntervalMs: input.ExpectedIntervalMs,
		SequenceField:      strings.TrimSpace(input.SequenceField), MissingGraceMs: input.MissingGraceMs,
		LateThresholdMs: input.LateThresholdMs, ClockSkewToleranceMs: input.ClockSkewToleranceMs,
		GapToleranceMs: input.GapToleranceMs, OutOfOrderToleranceMs: input.OutOfOrderToleranceMs,
		FrequencyTolerancePercent: input.FrequencyTolerancePercent, MinSamples: input.MinSamples,
		FlatMinSamples: input.FlatMinSamples, FlatMinDurationMs: input.FlatMinDurationMs,
		MandatoryFields: mandatory, FlatFields: flat, StaleFields: stale,
		Rules: rules, SeverityOverrides: severities, SamplingPriority: input.SamplingPriority,
		CooldownMs: input.CooldownMs,
	}, nil
}

func (s *Service) observationWorker(index int) {
	defer s.observationDone.Done()
	queue := s.observationQueues[index]
	for {
		select {
		case <-s.observationStop:
			for {
				select {
				case observations := <-queue:
					s.engine.Evaluate(observations)
				default:
					return
				}
			}
		case observations := <-queue:
			s.engine.Evaluate(observations)
		}
	}
}

func (s *Service) enqueueEvent(event RuleEvent) {
	select {
	case s.issueQueue <- event:
	default:
		s.evaluationDrops.Add(1)
	}
}

func (s *Service) issueWorker() {
	defer close(s.done)
	for {
		select {
		case <-s.stop:
			for {
				select {
				case event := <-s.issueQueue:
					s.persistEvent(event)
				case signal := <-s.connectorQueue:
					s.engine.RecordConnectorHealth(signal)
				default:
					return
				}
			}
		case event := <-s.issueQueue:
			s.persistEvent(event)
		case signal := <-s.connectorQueue:
			s.engine.RecordConnectorHealth(signal)
		}
	}
}

func (s *Service) persistEvent(event RuleEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s.engine != nil && !s.engine.AcceptsEvent(event) {
		return
	}
	if event.State == "resolved" {
		if err := s.repo.ResolveIssue(ctx, event.UnsID, event.RuleCode, event.ObservedAt, event.Occurrences); err != nil {
			logx.Errorf("data quality resolve issue node=%d rule=%s: %v", event.UnsID, event.RuleCode, err)
		}
	} else {
		evidence := sanitizedEvidence(event.RuleCode, event.Evidence)
		mergeWindow := time.Duration(0)
		if event.RuleCode == ruleValueRange || event.RuleCode == ruleMissingTelemetry {
			mergeWindow = dataQualityIssueMergeWindow
		}
		_, _, err := s.repo.OpenOrMergeActiveIssue(ctx, repo.DataQualityIssue{
			UnsID: event.UnsID, RuleCode: event.RuleCode, Severity: event.Severity, State: "open",
			FirstObservedAt: event.ObservedAt, LastObservedAt: event.ObservedAt, Occurrences: event.Occurrences,
			Summary: truncateRunes(event.Summary, common.DescMaxLen), Evidence: evidence,
		}, mergeWindow)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				s.reconcileRejectedOpenEvent(ctx, event)
				return
			}
			logx.Errorf("data quality open issue node=%d rule=%s: %v", event.UnsID, event.RuleCode, err)
			return
		}
	}
}

func (s *Service) reconcileRejectedOpenEvent(ctx context.Context, event RuleEvent) {
	policy, err := s.repo.GetPolicy(ctx, event.UnsID)
	if errors.Is(err, repo.ErrNotFound) || (err == nil && !policy.Enabled) {
		if s.engine != nil {
			s.engine.RemovePolicy(event.UnsID)
		}
		if resolveErr := s.repo.ResolveActiveIssuesForNodes(ctx, []int64{event.UnsID}, time.Now().UTC()); resolveErr != nil {
			logx.WithContext(ctx).Errorf("data quality resolve inactive node issues node=%d: %v", event.UnsID, resolveErr)
		}
		return
	}
	if err != nil {
		logx.WithContext(ctx).Errorf("data quality reload rejected event policy node=%d rule=%s: %v", event.UnsID, event.RuleCode, err)
		return
	}
	if s.engine != nil {
		s.engine.SetPolicy(policySnapshot(policy))
	}
	if resolveErr := s.repo.ResolveIssue(ctx, event.UnsID, event.RuleCode, time.Now().UTC(), 0); resolveErr != nil {
		logx.WithContext(ctx).Errorf("data quality resolve disabled rule node=%d rule=%s: %v", event.UnsID, event.RuleCode, resolveErr)
	}
}

func policySnapshot(policy repo.DataQualityPolicy) PolicySnapshot {
	ruleSet := make(map[string]struct{}, len(policy.Rules))
	for _, rule := range policy.Rules {
		ruleSet[rule] = struct{}{}
	}
	severity := make(map[string]string, len(policy.SeverityOverrides))
	for rule, value := range policy.SeverityOverrides {
		severity[rule] = value
	}
	return PolicySnapshot{
		UnsID: policy.UnsID, ExpectedInterval: time.Duration(policy.ExpectedIntervalMs) * time.Millisecond,
		MissingGrace:        time.Duration(policy.MissingGraceMs) * time.Millisecond,
		LateThreshold:       time.Duration(policy.LateThresholdMs) * time.Millisecond,
		ClockSkewTolerance:  time.Duration(policy.ClockSkewToleranceMs) * time.Millisecond,
		GapTolerance:        time.Duration(policy.GapToleranceMs) * time.Millisecond,
		OutOfOrderTolerance: time.Duration(policy.OutOfOrderToleranceMs) * time.Millisecond,
		SequenceField:       policy.SequenceField, FrequencyTolerancePercent: policy.FrequencyTolerancePercent,
		MinSamples: policy.MinSamples, FlatMinSamples: policy.FlatMinSamples,
		FlatMinDuration: time.Duration(policy.FlatMinDurationMs) * time.Millisecond,
		MandatoryFields: append([]string(nil), policy.MandatoryFields...),
		FlatFields:      append([]string(nil), policy.FlatFields...), SamplingPriority: policy.SamplingPriority,
		Rules: ruleSet, Severity: severity, Cooldown: time.Duration(policy.CooldownMs) * time.Millisecond,
	}
}

func policyView(policy repo.DataQualityPolicy) map[string]any {
	return map[string]any{
		"unsId": policy.UnsID, "enabled": policy.Enabled, "notifyOnIssue": false,
		"expectedIntervalMs": policy.ExpectedIntervalMs,
		"sequenceField":      policy.SequenceField, "missingGraceMs": policy.MissingGraceMs,
		"lateThresholdMs": policy.LateThresholdMs, "clockSkewToleranceMs": policy.ClockSkewToleranceMs,
		"gapToleranceMs": policy.GapToleranceMs, "outOfOrderToleranceMs": policy.OutOfOrderToleranceMs,
		"frequencyTolerancePercent": policy.FrequencyTolerancePercent, "minSamples": policy.MinSamples,
		"flatMinSamples": policy.FlatMinSamples, "flatMinDurationMs": policy.FlatMinDurationMs,
		"mandatoryFields": policy.MandatoryFields, "flatFields": policy.FlatFields,
		"staleFields": policy.StaleFields, "rules": policy.Rules,
		"severityOverrides": policy.SeverityOverrides, "samplingPriority": policy.SamplingPriority,
		"cooldownMs": policy.CooldownMs,
	}
}

func sanitizedEvidence(rule string, evidence map[string]any) json.RawMessage {
	allowed := map[string]map[string]struct{}{
		ruleDuplicate: {
			"sourceIdentity": {}, "canonicalHash": {}, "matchBasis": {}, "duplicateSeenAt": {}, "firstSeenAt": {},
			"currentPayloadPreview": {}, "currentPayloadPreviewFieldCount": {}, "currentPayloadPreviewTruncated": {},
			"matchedPayloadPreview": {}, "matchedPayloadPreviewFieldCount": {}, "matchedPayloadPreviewTruncated": {},
		},
		ruleLateArrival: {
			"sourceTimestamp": {}, "ingestedAt": {}, "delayMs": {}, "lateThresholdMs": {}, "skewToleranceMs": {},
			"allowedDelayMs": {}, "expectedIntervalMs": {}, "meanObservedIntervalMs": {}, "currentPayloadPreview": {},
			"currentPayloadPreviewFieldCount": {}, "currentPayloadPreviewTruncated": {},
		},
		ruleTimestampGap: {
			"previousSourceTimestamp": {}, "sourceTimestamp": {}, "gapMs": {}, "expectedMs": {}, "gapToleranceMs": {},
			"allowedGapMs": {}, "referencePayloadPreview": {}, "referencePayloadPreviewFieldCount": {},
			"referencePayloadPreviewTruncated": {}, "currentPayloadPreview": {}, "currentPayloadPreviewFieldCount": {},
			"currentPayloadPreviewTruncated": {},
		},
		ruleOutOfOrder: {
			"maxSourceTimestamp": {}, "sourceTimestamp": {}, "deltaMs": {}, "behindMs": {}, "outOfOrderToleranceMs": {},
			"referencePayloadPreview": {}, "referencePayloadPreviewFieldCount": {}, "referencePayloadPreviewTruncated": {},
			"currentPayloadPreview": {}, "currentPayloadPreviewFieldCount": {}, "currentPayloadPreviewTruncated": {},
		},
		ruleMissingTelemetry: {
			"lastSeenAt": {}, "deadlineAt": {}, "delayMs": {}, "expectedIntervalMs": {}, "missingGraceMs": {}, "allowedSilenceMs": {},
			"lastPayloadPreview": {}, "lastPayloadPreviewFieldCount": {}, "lastPayloadPreviewTruncated": {},
		},
		ruleStaleSignal: {
			"lastSeenAt": {}, "deadlineAt": {}, "delayMs": {}, "expectedIntervalMs": {}, "missingGraceMs": {}, "allowedSilenceMs": {},
			"lastPayloadPreview": {}, "lastPayloadPreviewFieldCount": {}, "lastPayloadPreviewTruncated": {},
		},
		ruleIngestionDrop: {
			"stage": {}, "reason": {}, "count": {}, "currentPayloadPreview": {},
			"currentPayloadPreviewFieldCount": {}, "currentPayloadPreviewTruncated": {},
		},
		ruleMandatoryMissing: {
			"fields": {}, "configuredFields": {}, "missingCountsByField": {}, "missingExamples": {}, "count": {}, "sampleCount": {},
		},
		ruleInvalidValue: {
			"fields": {}, "errorCodes": {}, "count": {}, "badQualityCount": {}, "sampleCount": {}, "invalidExamples": {},
			"payloadPreview": {}, "payloadPreviewFieldCount": {}, "payloadPreviewTruncated": {},
		},
		ruleValueRange: {
			"fields": {}, "violations": {}, "field": {}, "observedValue": {}, "lowerLimit": {}, "upperLimit": {},
			"direction": {}, "observedAt": {}, "currentPayloadPreview": {},
			"currentPayloadPreviewFieldCount": {}, "currentPayloadPreviewTruncated": {},
		},
		ruleFlatSignal: {
			"fields": {}, "fieldCount": {}, "sampleCount": {}, "durationMs": {}, "minimumSamples": {}, "minimumDurationMs": {},
			"flatFieldDetails": {}, "payloadPreview": {}, "payloadPreviewFieldCount": {}, "payloadPreviewTruncated": {},
		},
		ruleFrequency: {
			"expectedMs": {}, "observedMs": {}, "meanObservedMs": {}, "minimumObservedMs": {}, "maximumObservedMs": {},
			"allowedMinimumMs": {}, "allowedMaximumMs": {}, "deviationPercent": {}, "tolerancePercent": {},
			"sampleCount": {}, "intervalCount": {}, "aggregationMethod": {}, "windowStartSourceTimestamp": {},
			"windowEndSourceTimestamp": {}, "payloadPreview": {}, "payloadPreviewFieldCount": {}, "payloadPreviewTruncated": {},
		},
		ruleCollectionInterrupted: {"source": {}, "state": {}, "observedAt": {}, "errorCode": {}, "flowId": {}},
		ruleConnectorFailure:      {"source": {}, "state": {}, "observedAt": {}, "errorCode": {}, "flowId": {}},
	}
	out := make(map[string]any)
	for key, value := range evidence {
		if _, ok := allowed[rule][key]; !ok {
			continue
		}
		out[key] = sanitizedEvidenceValue(key, value, 0)
	}
	data := mustJSON(out)
	if len(data) > 8192 {
		for key := range out {
			normalized := strings.ToLower(key)
			if strings.Contains(normalized, "payload") || strings.Contains(normalized, "example") ||
				strings.Contains(normalized, "detail") {
				delete(out, key)
			}
		}
		out["truncated"] = true
		data = mustJSON(out)
		if len(data) > 8192 {
			return json.RawMessage(`{"truncated":true}`)
		}
	}
	return data
}

func sanitizedEvidenceValue(key string, value any, depth int) any {
	if depth >= 6 {
		return "[TRUNCATED]"
	}
	normalized, ok := value.(map[string]any)
	if !ok {
		data, err := json.Marshal(value)
		if err == nil {
			_ = json.Unmarshal(data, &normalized)
		}
	}
	if normalized != nil {
		keys := make([]string, 0, len(normalized))
		for childKey := range normalized {
			keys = append(keys, childKey)
		}
		sort.Strings(keys)
		if len(keys) > 16 {
			keys = keys[:16]
		}
		out := make(map[string]any, len(keys))
		for _, childKey := range keys {
			out[childKey] = sanitizedEvidenceValue(childKey, normalized[childKey], depth+1)
		}
		return out
	}
	if values, ok := value.([]any); ok {
		if len(values) > 8 {
			values = values[:8]
		}
		out := make([]any, 0, len(values))
		for _, item := range values {
			out = append(out, sanitizedEvidenceValue(key, item, depth+1))
		}
		return out
	}
	if data, err := json.Marshal(value); err == nil {
		var values []any
		if json.Unmarshal(data, &values) == nil && values != nil {
			return sanitizedEvidenceValue(key, values, depth)
		}
	}
	if text, ok := value.(string); ok {
		return safeEvidencePayloadValue(key, text, "")
	}
	return value
}

func normalizeStringList(values []string, limit int, valid func(string) bool) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || !valid(value) {
			return nil, ErrInvalidPolicy
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) > limit {
		return nil, ErrInvalidPolicy
	}
	sort.Strings(out)
	return out, nil
}

func validFieldName(value string) bool {
	return value != "" && utf8.RuneCountInString(value) <= common.AliasMaxLen
}

func containsAny(values []string, candidates ...string) bool {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	for _, candidate := range candidates {
		if _, ok := set[candidate]; ok {
			return true
		}
	}
	return false
}

func uniqueCount(groups ...[]string) int {
	set := map[string]struct{}{}
	for _, values := range groups {
		for _, value := range values {
			set[value] = struct{}{}
		}
	}
	return len(set)
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal data quality value: %v", err))
	}
	return data
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}
