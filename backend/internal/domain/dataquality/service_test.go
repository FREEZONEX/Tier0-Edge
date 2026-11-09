package dataquality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/domain/dataingest"
	domainnotify "backend/internal/domain/notify"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func TestValidatePolicy_RejectsLateArrivalWithoutPositiveThresholdBeforeRepositoryAccess(t *testing.T) {
	service := &Service{}
	_, err := service.validatePolicy(context.Background(), PolicyInput{
		UnsID: 1, Enabled: true, Rules: []string{ruleLateArrival}, LateThresholdMs: 0,
	})
	require.ErrorIs(t, err, ErrInvalidPolicy)
}

func TestService_ConnectorHealthQueueIsBoundedAndNonBlocking(t *testing.T) {
	service := &Service{
		engine: newEngine(1, 1, nil), connectorQueue: make(chan dataingest.ConnectorHealthSignal, 1),
	}
	service.accepting.Store(true)
	defer service.engine.Close()
	signal := dataingest.ConnectorHealthSignal{
		Source: dataingest.ConnectorSourceMQTT, State: dataingest.ConnectorStateLost, ObservedAt: time.Now().UTC(),
	}
	service.RecordConnectorHealth(context.Background(), signal)
	service.RecordConnectorHealth(context.Background(), signal)
	require.Len(t, service.connectorQueue, 1)
	require.Equal(t, int64(1), service.connectorEventDrops.Load())
	require.Equal(t, int64(1), service.Status(0)["connectorEventDrops"])
}

func TestService_ObserveQueuesOwnedEvaluationBatchWithoutBlocking(t *testing.T) {
	engine := newEngine(1, 1, nil)
	defer engine.Close()
	engine.SetPolicy(PolicySnapshot{UnsID: 17, Rules: ruleSet(ruleDuplicate)})
	service := &Service{
		engine:            engine,
		observationQueues: []chan []dataingest.QualityObservation{make(chan []dataingest.QualityObservation, 1)},
	}
	service.accepting.Store(true)

	first := dataingest.QualityObservation{
		UnsID: 17, IngestedAt: time.Now().UTC(), CanonicalHash: 1,
		Fields:    []dataingest.Field{{Name: "temperature", Type: "double"}},
		RawValues: map[string]string{"temperature": "21.5"},
	}
	service.Observe(context.Background(), []dataingest.QualityObservation{first})
	first.RawValues["temperature"] = "99"
	queued := <-service.observationQueues[0]
	require.Equal(t, "21.5", queued[0].RawValues["temperature"])

	service.observationQueues[0] <- queued
	service.Observe(context.Background(), []dataingest.QualityObservation{{
		UnsID: 17, IngestedAt: time.Now().UTC(), CanonicalHash: 2,
		RawValues: map[string]string{"temperature": "22"},
	}})
	require.Equal(t, int64(1), service.evaluationDrops.Load())
	require.Equal(t, int64(2), engine.Status(17)[0].ReceivedCount)
}

func TestSanitizedEvidence_AllowlistAndBounds(t *testing.T) {
	evidence := sanitizedEvidence(ruleIngestionDrop, map[string]any{
		"stage": strings.Repeat("x", 256), "reason": "queueFull", "count": int64(2), "rawPayload": "secret",
		"currentPayloadPreview": map[string]string{
			"temperature": "21.5", "password": "do-not-store", "note": strings.Repeat("n", 256),
		},
		"currentPayloadPreviewFieldCount": 3,
	})
	require.LessOrEqual(t, len(evidence), 8192)
	require.NotContains(t, string(evidence), "rawPayload")
	require.NotContains(t, string(evidence), "do-not-store")
	require.Contains(t, string(evidence), "[REDACTED]")
	require.Contains(t, string(evidence), "21.5")
	require.NotContains(t, string(evidence), strings.Repeat("n", 129))
	require.Contains(t, string(evidence), strings.Repeat("x", evidencePayloadValueLimit))

	largePayload := make(map[string]string)
	for index := 0; index < 16; index++ {
		largePayload[fmt.Sprintf("field%02d", index)] = strings.Repeat("测", 128)
	}
	large := sanitizedEvidence(ruleMandatoryMissing, map[string]any{
		"fields": []string{"temperature"}, "count": 3, "sampleCount": 100,
		"missingExamples": []map[string]any{
			{"missingFields": []string{"temperature"}, "payloadPreview": largePayload},
			{"missingFields": []string{"temperature"}, "payloadPreview": largePayload},
			{"missingFields": []string{"temperature"}, "payloadPreview": largePayload},
		},
	})
	require.LessOrEqual(t, len(large), 8192)
	require.Contains(t, string(large), `"fields":["temperature"]`)
	require.Contains(t, string(large), `"truncated":true`)
	require.NotContains(t, string(large), "missingExamples")
}

func TestSanitizedEvidence_ValueRangeKeepsDecisionDataAndRedactsPayload(t *testing.T) {
	evidence := sanitizedEvidence(ruleValueRange, map[string]any{
		"fields": []string{"temperature"}, "field": "temperature", "observedValue": 101.5,
		"lowerLimit": 0.0, "upperLimit": 100.0, "direction": "aboveUpper", "observedAt": int64(1_756_713_600_000),
		"violations": []map[string]any{{
			"field": "temperature", "observedValue": 101.5, "lowerLimit": 0.0, "upperLimit": 100.0, "direction": "aboveUpper",
		}},
		"currentPayloadPreview":           map[string]string{"temperature": "101.5", "password": "do-not-store"},
		"currentPayloadPreviewFieldCount": 2,
		"rawPayload":                      "must-not-survive",
	})

	require.JSONEq(t, `{
		"fields":["temperature"],"field":"temperature","observedValue":101.5,"lowerLimit":0,"upperLimit":100,
		"direction":"aboveUpper","observedAt":1756713600000,
		"violations":[{"field":"temperature","observedValue":101.5,"lowerLimit":0,"upperLimit":100,"direction":"aboveUpper"}],
		"currentPayloadPreview":{"password":"[REDACTED]","temperature":"101.5"},"currentPayloadPreviewFieldCount":2
	}`, string(evidence))
	require.NotContains(t, string(evidence), "must-not-survive")
	require.NotContains(t, string(evidence), "do-not-store")
}

func TestValidatePolicy_RejectsInvalidNumericBoundsBeforeRepositoryAccess(t *testing.T) {
	service := &Service{}
	_, err := service.validatePolicy(context.Background(), PolicyInput{
		UnsID: 1, Enabled: true, Rules: []string{ruleDuplicate}, CooldownMs: -1,
	})
	require.True(t, errors.Is(err, ErrInvalidPolicy))
}

func TestValidatePolicy_RejectsEnabledPolicyWhenFeatureIsDisabled(t *testing.T) {
	service := &Service{enabled: false}
	_, err := service.validatePolicy(context.Background(), PolicyInput{
		UnsID: 1, Enabled: true, Rules: []string{ruleDuplicate},
	})
	require.ErrorIs(t, err, ErrFeatureDisabled)
}

func TestService_IssueAPIsRejectWhenFeatureIsDisabled(t *testing.T) {
	service := &Service{enabled: false}
	_, err := service.GetPolicy(context.Background(), 1)
	require.ErrorIs(t, err, ErrFeatureDisabled)

	_, err = service.SavePolicy(context.Background(), PolicyInput{UnsID: 1})
	require.ErrorIs(t, err, ErrFeatureDisabled)

	_, err = service.ListIssues(context.Background(), IssueQuery{})
	require.ErrorIs(t, err, ErrFeatureDisabled)

	_, err = service.ActiveSummary(context.Background())
	require.ErrorIs(t, err, ErrFeatureDisabled)

	_, err = service.AcknowledgeIssue(context.Background(), 1, 1)
	require.ErrorIs(t, err, ErrFeatureDisabled)
}

func TestService_GetPolicyReturnsUnsavedDefaultForExistingMetric(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	unsRepo := repo.NewUnsRepo(store)
	root, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{Name: "LineA", Type: 1}, nil, nil)
	require.NoError(t, err)
	metric, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		ParentID: root.ID, Name: "temperature", Type: 2, TopicType: 3,
		Schema: json.RawMessage(`[{"name":"value","type":"DOUBLE"}]`),
	}, nil, nil)
	require.NoError(t, err)

	qualityRepo := repo.NewDataQualityRepo(store)
	service := &Service{enabled: true, repo: qualityRepo, unsRepo: unsRepo}
	view, err := service.GetPolicy(ctx, metric.ID)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"unsId": metric.ID, "enabled": false, "notifyOnIssue": false, "expectedIntervalMs": int64(0),
		"sequenceField": "", "missingGraceMs": int64(0), "lateThresholdMs": int64(5000),
		"clockSkewToleranceMs": int64(0), "gapToleranceMs": int64(0), "outOfOrderToleranceMs": int64(0),
		"frequencyTolerancePercent": 20, "minSamples": 3, "flatMinSamples": 3,
		"flatMinDurationMs": int64(2000), "mandatoryFields": []string{}, "flatFields": []string{},
		"staleFields": []string{}, "rules": []string{}, "severityOverrides": map[string]string{},
		"samplingPriority": 0, "cooldownMs": int64(60000),
	}, view)

	_, err = qualityRepo.GetPolicy(ctx, metric.ID)
	require.ErrorIs(t, err, repo.ErrNotFound)

	_, err = service.GetPolicy(ctx, root.ID)
	require.ErrorIs(t, err, ErrInvalidPolicy)
	_, err = service.GetPolicy(ctx, metric.ID+1)
	require.ErrorIs(t, err, repo.ErrNotFound)
}

func TestValidatePolicy_RejectsDeepRulesWithoutRequiredConfiguration(t *testing.T) {
	service := &Service{enabled: true}
	for _, input := range []PolicyInput{
		{UnsID: 1, Enabled: true, Rules: []string{ruleMandatoryMissing}},
		{UnsID: 1, Enabled: true, Rules: []string{ruleFlatSignal}},
		{UnsID: 1, Enabled: true, Rules: []string{ruleFrequency}, ExpectedIntervalMs: 0},
		{UnsID: 1, Enabled: true, Rules: []string{ruleFrequency}, ExpectedIntervalMs: 1000, MinSamples: 1},
		{UnsID: 1, Enabled: true, Rules: []string{ruleFlatSignal}, FlatFields: []string{"value"}, FlatMinSamples: 1},
	} {
		_, err := service.validatePolicy(context.Background(), input)
		require.ErrorIs(t, err, ErrInvalidPolicy)
	}
}

func TestService_StatusViewAndIssueListUseRuntimeAndDurableData(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	unsRepo := repo.NewUnsRepo(store)
	root, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		Name: "LineA", DisplayName: "一号产线", Type: 1,
	}, nil, nil)
	require.NoError(t, err)
	createMetric := func(name, displayName string) repo.UnsNode {
		t.Helper()
		node, createErr := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
			ParentID: root.ID, Name: name, DisplayName: displayName, Type: 2, TopicType: 3,
			Schema: json.RawMessage(`[{"name":"value","type":"DOUBLE"}]`),
		}, nil, nil)
		require.NoError(t, createErr)
		return node
	}
	healthyNode := createMetric("temperature", "温度")
	warmingNode := createMetric("pressure", "压力")
	abnormalNode := createMetric("speed", "速度")

	qualityRepo := repo.NewDataQualityRepo(store)
	for _, node := range []repo.UnsNode{healthyNode, warmingNode, abnormalNode} {
		_, err = qualityRepo.SavePolicy(ctx, repo.DataQualityPolicy{
			UnsID: node.ID, Enabled: true, MinSamples: 3, CooldownMs: 60000,
			MandatoryFields: []string{}, FlatFields: []string{},
			StaleFields: []string{}, Rules: []string{"duplicate"},
			SeverityOverrides: map[string]string{},
		}, 0)
		require.NoError(t, err)
	}

	service, err := New(ctx, config.DataQualityConf{
		Enabled: true, ShardCount: 4, MaxEnabledNodes: 100, IssueQueueSize: 16,
	})
	require.NoError(t, err)
	defer service.Close()

	observations := func(nodeID int64, count int) []dataingest.QualityObservation {
		out := make([]dataingest.QualityObservation, 0, count)
		for index := 0; index < count; index++ {
			at := time.Date(2026, 8, 31, 10, 0, index, 0, time.UTC)
			out = append(out, dataingest.QualityObservation{
				UnsID: nodeID, SourceTime: at, SourceTimeSet: true, IngestedAt: at,
				CanonicalHash: uint64(index + 1), RawValues: map[string]string{},
			})
		}
		return out
	}
	healthyObservations := observations(healthyNode.ID, 3)
	abnormalObservations := observations(abnormalNode.ID, 3)
	service.Observe(ctx, healthyObservations)
	service.RecordPersisted(ctx, healthyObservations)
	service.Observe(ctx, abnormalObservations)
	service.RecordPersisted(ctx, abnormalObservations[:2])
	service.RecordDrop(ctx, "historyWriter", "writeError", abnormalObservations[2:])
	require.Eventually(t, func() bool {
		status := service.engine.Status(abnormalNode.ID)
		return len(status) == 1 && status[0].MaxSourceTime != nil
	}, time.Second, 10*time.Millisecond)
	service.engine.RestoreActiveRules(abnormalNode.ID, []string{ruleDuplicate})

	now := time.Now().UTC()
	_, _, err = qualityRepo.OpenIssue(ctx, repo.DataQualityIssue{
		UnsID: abnormalNode.ID, RuleCode: ruleDuplicate, Severity: "critical", State: "open",
		FirstObservedAt: now, LastObservedAt: now, Occurrences: 4, Evidence: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	ack, _, err := qualityRepo.OpenIssue(ctx, repo.DataQualityIssue{
		UnsID: healthyNode.ID, RuleCode: ruleLateArrival, Severity: "warning", State: "open",
		FirstObservedAt: now, LastObservedAt: now, Occurrences: 2, Evidence: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	_, err = qualityRepo.AcknowledgeIssue(ctx, ack.ID, 1)
	require.NoError(t, err)
	resolved, _, err := qualityRepo.OpenIssue(ctx, repo.DataQualityIssue{
		UnsID: warmingNode.ID, RuleCode: ruleIngestionDrop, Severity: "critical", State: "open",
		FirstObservedAt: now, LastObservedAt: now, Occurrences: 9, Evidence: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	require.NoError(t, qualityRepo.ResolveIssue(ctx, resolved.UnsID, resolved.RuleCode, now, 0))

	view, err := service.StatusView(ctx, 0)
	require.NoError(t, err)
	summary, ok := view["summary"].(StatusSummary)
	require.True(t, ok)
	require.Equal(t, StatusSummary{
		MonitoredNodes: 3, HealthyNodes: 1, AbnormalNodes: 1, WarmingNodes: 1,
		ReceivedCount: 6, PersistedCount: 5, DropCount: 1, IngestionSuccessPercent: 83.33,
		OpenIssues: 1, AckIssues: 1, CriticalIssues: 1,
	}, summary)
	require.Equal(t, map[string]int64{ruleDuplicate: 4, ruleLateArrival: 2}, view["activeRuleCounts"])
	statusItems, ok := view["items"].([]NodeStatusView)
	require.True(t, ok)
	require.Len(t, statusItems, 3)
	byID := make(map[int64]NodeStatusView, len(statusItems))
	for _, item := range statusItems {
		byID[item.UnsID] = item
	}
	require.Equal(t, "temperature", byID[healthyNode.ID].NodeName)
	require.Equal(t, "温度", byID[healthyNode.ID].DisplayName)
	require.Equal(t, healthyNode.Namespace, byID[healthyNode.ID].Namespace)

	issues, err := service.ListIssues(ctx, IssueQuery{Page: 1, Size: 20})
	require.NoError(t, err)
	issueItems, ok := issues["list"].([]IssueView)
	require.True(t, ok)
	require.Len(t, issueItems, 3)
	for _, item := range issueItems {
		require.NotEmpty(t, item.NodeName)
		require.NotEmpty(t, item.DisplayName)
		require.NotEmpty(t, item.Namespace)
	}

	activeSummary, err := service.ActiveSummary(ctx)
	require.NoError(t, err)
	activeItems, ok := activeSummary["list"].([]repo.DataQualityActiveNodeSummary)
	require.True(t, ok)
	require.Equal(t, []repo.DataQualityActiveNodeSummary{
		{UnsID: healthyNode.ID, Namespace: healthyNode.Namespace, Severity: "warning"},
		{UnsID: abnormalNode.ID, Namespace: abnormalNode.Namespace, Severity: "critical"},
	}, activeItems)

	filtered, err := service.StatusView(ctx, abnormalNode.ID)
	require.NoError(t, err)
	filteredSummary := filtered["summary"].(StatusSummary)
	require.Equal(t, 1, filteredSummary.MonitoredNodes)
	require.Equal(t, int64(1), filteredSummary.OpenIssues)
	require.Zero(t, filteredSummary.AckIssues)
	require.Equal(t, map[string]int64{ruleDuplicate: 4}, filtered["activeRuleCounts"])
}

func TestService_PersistEventNeverSendsInboxWithLegacyOptIn(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	unsRepo := repo.NewUnsRepo(store)
	root, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{Name: "LineA", Type: 1}, nil, nil)
	require.NoError(t, err)
	metric, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		ParentID: root.ID, Name: "temperature", DisplayName: "反应釜温度", Type: 2, TopicType: 3,
		Schema: json.RawMessage(`[{"name":"temperature","type":"DOUBLE","lowerLimit":0,"upperLimit":100}]`),
	}, nil, nil)
	require.NoError(t, err)

	notifyService := domainnotify.New(store)
	qualityRepo := repo.NewDataQualityRepo(store)
	_, err = qualityRepo.SavePolicy(ctx, repo.DataQualityPolicy{
		UnsID: metric.ID, Enabled: true, NotifyOnIssue: true,
		ExpectedIntervalMs: 2000, MissingGraceMs: 1000,
		Rules: []string{ruleValueRange, ruleMissingTelemetry}, SeverityOverrides: map[string]string{},
	}, 0)
	require.NoError(t, err)
	service := &Service{enabled: true, repo: qualityRepo, unsRepo: unsRepo}

	now := time.Date(2026, 9, 5, 8, 30, 0, 0, time.UTC)
	valueEvent := RuleEvent{
		UnsID: metric.ID, RuleCode: ruleValueRange, Severity: "warning", State: "open", ObservedAt: now,
		Summary: "value range triggered", Occurrences: 1,
		Evidence: map[string]any{
			"field": "temperature", "observedValue": 101.5, "lowerLimit": 0, "upperLimit": 100,
		},
	}
	service.persistEvent(valueEvent)
	valueEvent.ObservedAt = now.Add(time.Second)
	valueEvent.Occurrences = 3
	service.persistEvent(valueEvent)

	_, total, err := notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, total)

	service.persistEvent(RuleEvent{
		UnsID: metric.ID, RuleCode: ruleDuplicate, Severity: "warning", State: "open", ObservedAt: now,
		Summary: "duplicate triggered", Occurrences: 1, Evidence: map[string]any{},
	})
	_, total, err = notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, total)

	missingEvent := RuleEvent{
		UnsID: metric.ID, RuleCode: ruleMissingTelemetry, Severity: "critical", State: "open", ObservedAt: now.Add(3 * time.Second),
		Summary: "missing telemetry triggered", Occurrences: 1,
		Evidence: map[string]any{
			"delayMs": 3000, "expectedIntervalMs": 2000, "missingGraceMs": 1000,
			"lastSeenAt": now.UnixMilli(),
		},
	}
	service.persistEvent(missingEvent)
	service.persistEvent(missingEvent)
	_, total, err = notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, total)

	service.persistEvent(RuleEvent{
		UnsID: metric.ID, RuleCode: ruleValueRange, State: "resolved", ObservedAt: now.Add(5 * time.Second),
	})
	_, total, err = notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, total)

	valueEvent.ObservedAt = now.Add(6 * time.Second)
	valueEvent.Occurrences = 1
	service.persistEvent(valueEvent)
	_, total, err = notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, total, "recurrence must not send an inbox message")
	issues, issueTotal, err := qualityRepo.ListIssues(ctx, repo.DataQualityIssueFilter{UnsID: metric.ID, RuleCode: ruleValueRange})
	require.NoError(t, err)
	require.Equal(t, int64(1), issueTotal)
	require.Equal(t, int64(5), issues[0].Occurrences)
	service.persistEvent(RuleEvent{UnsID: metric.ID, RuleCode: ruleValueRange, State: "resolved", ObservedAt: now.Add(40 * time.Second)})
	valueEvent.ObservedAt = now.Add(40*time.Second + dataQualityIssueMergeWindow + time.Millisecond)
	service.persistEvent(valueEvent)
	_, total, err = notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, total, "a new Issue must not send an inbox message")
}

func TestService_DeletedNodeRejectsQueuedEventsAndRestoreReappliesPolicy(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	unsRepo := repo.NewUnsRepo(store)
	root, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{Name: "RecycleLine", Type: 1}, nil, nil)
	require.NoError(t, err)
	metric, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		ParentID: root.ID, Name: "temperature", DisplayName: "回收站温度", Type: 2, TopicType: 3,
		Schema: json.RawMessage(`[{"name":"temperature","type":"DOUBLE","lowerLimit":0,"upperLimit":100}]`),
	}, nil, nil)
	require.NoError(t, err)

	qualityRepo := repo.NewDataQualityRepo(store)
	policy, err := qualityRepo.SavePolicy(ctx, repo.DataQualityPolicy{
		UnsID: metric.ID, Enabled: true, NotifyOnIssue: true,
		Rules: []string{ruleValueRange}, SeverityOverrides: map[string]string{}, CooldownMs: 60000,
	}, 0)
	require.NoError(t, err)
	notifyService := domainnotify.New(store)
	service := &Service{enabled: true, repo: qualityRepo, unsRepo: unsRepo, engine: newEngine(1, 1, nil)}
	service.accepting.Store(true)

	defer service.engine.Close()
	service.engine.SetPolicy(policySnapshot(policy))
	shard := service.engine.shard(metric.ID)
	shard.mu.RLock()
	initialRevision := shard.nodes[metric.ID].policy.revision
	shard.mu.RUnlock()

	t0 := time.Now().UTC()
	openEvent := RuleEvent{
		UnsID: metric.ID, Revision: initialRevision, RuleCode: ruleValueRange, Severity: "warning", State: "open", ObservedAt: t0,
		Summary: "value range triggered", Occurrences: 1,
		Evidence: map[string]any{"field": "temperature", "observedValue": 101, "lowerLimit": 0, "upperLimit": 100},
	}
	service.persistEvent(openEvent)
	_, totalMessages, err := notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, totalMessages)

	deleted, err := unsRepo.DeleteUnsNode(ctx, metric.ID, 1)
	require.NoError(t, err)
	service.RemoveNodePolicies(ctx, deleted)
	require.Zero(t, service.RuntimeStateCount())
	issues, totalIssues, err := qualityRepo.ListIssues(ctx, repo.DataQualityIssueFilter{UnsID: metric.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), totalIssues)
	require.Equal(t, "resolved", issues[0].State)

	// A delayed event already queued before deletion must neither reopen the
	// historical Issue nor send another notification for the deleted node.
	openEvent.ObservedAt = t0.Add(dataQualityIssueMergeWindow + time.Minute)
	openEvent.Revision = 0 // Exercise the durable repository guard independently.
	service.persistEvent(openEvent)
	issues, totalIssues, err = qualityRepo.ListIssues(ctx, repo.DataQualityIssueFilter{UnsID: metric.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), totalIssues)
	require.Equal(t, "resolved", issues[0].State)
	_, totalMessages, err = notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, totalMessages)

	restored, err := unsRepo.RestoreUnsNode(ctx, metric.ID, 1, false)
	require.NoError(t, err)
	require.Len(t, restored, 1)
	require.NotNil(t, restored[0].DataQualityPolicy)
	service.ApplyNodePolicies(restored)
	require.Equal(t, 1, service.RuntimeStateCount())

	// An old event can still be waiting in the service queue when restore wins
	// the race. Its observation predates the restored node lifecycle and must be
	// rejected even though the preserved policy is active again.
	openEvent.ObservedAt = t0
	openEvent.Revision = initialRevision
	service.persistEvent(openEvent)
	issues, totalIssues, err = qualityRepo.ListIssues(ctx, repo.DataQualityIssueFilter{UnsID: metric.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), totalIssues)
	require.Equal(t, "resolved", issues[0].State)
	_, totalMessages, err = notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, totalMessages)

	// Restore preserves the JSON policy and accepts a genuinely new evaluator
	// event again, proving that recycle-bin recovery re-enables detection.
	shard.mu.RLock()
	openEvent.Revision = shard.nodes[metric.ID].policy.revision
	shard.mu.RUnlock()
	openEvent.ObservedAt = time.Now().UTC().Add(dataQualityIssueMergeWindow + time.Minute)
	service.persistEvent(openEvent)
	issues, totalIssues, err = qualityRepo.ListIssues(ctx, repo.DataQualityIssueFilter{UnsID: metric.ID})
	require.NoError(t, err)
	require.Equal(t, int64(2), totalIssues)
	require.Equal(t, "open", issues[0].State)
	_, totalMessages, err = notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, totalMessages)
}

func TestService_SparseSamplesKeepSingleMissingTelemetryIssue(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	unsRepo := repo.NewUnsRepo(store)
	root, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{Name: "LineSparse", Type: 1}, nil, nil)
	require.NoError(t, err)
	metric, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		ParentID: root.ID, Name: "temperature", DisplayName: "稀疏温度", Type: 2, TopicType: 3,
	}, nil, nil)
	require.NoError(t, err)

	qualityRepo := repo.NewDataQualityRepo(store)
	policy, err := qualityRepo.SavePolicy(ctx, repo.DataQualityPolicy{
		UnsID: metric.ID, Enabled: true, NotifyOnIssue: true,
		ExpectedIntervalMs: 1000, MissingGraceMs: 1000,
		Rules: []string{ruleMissingTelemetry}, SeverityOverrides: map[string]string{}, CooldownMs: 60000,
	}, 0)
	require.NoError(t, err)
	notifyService := domainnotify.New(store)
	service := &Service{enabled: true, repo: qualityRepo, unsRepo: unsRepo}

	engine := newEngine(4, 1, service.persistEvent)
	defer engine.Close()
	engine.SetPolicy(policySnapshot(policy))

	t0 := time.Now().UTC().Add(time.Hour)
	sample := dataingest.QualityObservation{UnsID: metric.ID, IngestedAt: t0}
	engine.Touch([]dataingest.QualityObservation{sample})
	shard := engine.shard(metric.ID)
	shard.mu.RLock()
	version := shard.nodes[metric.ID].deadlineVersion
	shard.mu.RUnlock()
	engine.deadlineReached(metric.ID, version, t0.Add(2*time.Second))

	_, total, err := notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, total)

	// The next source sample is still far outside the configured two-second
	// window. It must keep the same Issue active and must not send another
	// "first anomaly" notification.
	sample.IngestedAt = t0.Add(10 * time.Second)
	engine.Touch([]dataingest.QualityObservation{sample})
	issues, issueTotal, err := qualityRepo.ListIssues(ctx, repo.DataQualityIssueFilter{UnsID: metric.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), issueTotal)
	require.Equal(t, "open", issues[0].State)
	_, total, err = notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, total)

	// A second timely sample recovers real-time quality but must not close
	// the Issue until thirty seconds of timely observations have followed.
	sample.IngestedAt = t0.Add(11 * time.Second)
	engine.Touch([]dataingest.QualityObservation{sample})
	issues, issueTotal, err = qualityRepo.ListIssues(ctx, repo.DataQualityIssueFilter{UnsID: metric.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), issueTotal)
	require.Equal(t, "open", issues[0].State)
	require.Equal(t, int64(1), engine.EffectiveQuality(metric.ID, 1))
	for second := 12; second <= 41; second++ {
		sample.IngestedAt = t0.Add(time.Duration(second) * time.Second)
		engine.Touch([]dataingest.QualityObservation{sample})
	}
	issues, issueTotal, err = qualityRepo.ListIssues(ctx, repo.DataQualityIssueFilter{UnsID: metric.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), issueTotal)
	require.Equal(t, "resolved", issues[0].State)
	_, total, err = notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, total)
}

func TestService_RestartPreservesIssueWithoutSendingNotifications(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	unsRepo := repo.NewUnsRepo(store)
	metric, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		Name: "RestartRange", Type: 2, TopicType: 3,
		Schema: json.RawMessage(`[{"name":"value","type":"DOUBLE","upperLimit":10}]`),
	}, nil, nil)
	require.NoError(t, err)
	qualityRepo := repo.NewDataQualityRepo(store)
	_, err = qualityRepo.SavePolicy(ctx, repo.DataQualityPolicy{
		UnsID: metric.ID, Enabled: true, NotifyOnIssue: true, Rules: []string{ruleValueRange}, CooldownMs: 60000,
	}, 0)
	require.NoError(t, err)
	notifyService := domainnotify.New(store)
	start := func() *Service {
		s, startErr := New(ctx, config.DataQualityConf{Enabled: true, ShardCount: 1, IssueQueueSize: 16})
		require.NoError(t, startErr)

		t.Cleanup(s.Close)
		return s
	}
	s := start()
	t0 := time.Now().UTC().Add(time.Hour)
	upper := float64(10)
	observe := func(second int, value string) {
		s.engine.Observe([]dataingest.QualityObservation{{
			UnsID: metric.ID, IngestedAt: t0.Add(time.Duration(second) * time.Second),
			Fields: []dataingest.Field{{Name: "value", Type: "double", UpperLimit: &upper}}, RawValues: map[string]string{"value": value},
		}})
	}
	observe(0, "11")
	s.Close() // Drain event persistence and notification before restarting.
	issues, total, err := qualityRepo.ListIssues(ctx, repo.DataQualityIssueFilter{UnsID: metric.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	issueID := issues[0].ID
	_, err = qualityRepo.AcknowledgeIssue(ctx, issueID, 1)
	require.NoError(t, err)
	assertNoNotification := func() {
		_, count, messageErr := notifyService.ListUserMessages(ctx, 1, domainnotify.UserMessageQuery{}, 1, 20)
		require.NoError(t, messageErr)
		require.Zero(t, count)
	}
	assertNoNotification()

	s = start()
	observe(1, "11")
	for second := 2; second <= 32; second++ {
		observe(second, "5")
	}
	s.Close()
	recovered, err := qualityRepo.GetIssue(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, "resolved", recovered.State)
	require.Equal(t, int64(2), recovered.Occurrences)
	assertNoNotification()

	s = start() // Resolved Issues are not in the engine; the database must merge.
	observe(33, "12")
	s.Close()
	issues, total, err = qualityRepo.ListIssues(ctx, repo.DataQualityIssueFilter{UnsID: metric.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, issueID, issues[0].ID)
	require.Equal(t, "ack", issues[0].State)
	require.Equal(t, int64(3), issues[0].Occurrences)
	assertNoNotification()
}

func TestService_SavePolicyDerivesFirstVersionRulesFromParameters(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	unsRepo := repo.NewUnsRepo(store)
	root, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{Name: "LineB", Type: 1}, nil, nil)
	require.NoError(t, err)
	metric, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		ParentID: root.ID, Name: "pressure", Type: 2, TopicType: 3,
		Schema: json.RawMessage(`[{"name":"pressure","type":"DOUBLE","lowerLimit":0,"upperLimit":10}]`),
	}, nil, nil)
	require.NoError(t, err)

	service := &Service{enabled: true, maxEnabledNodes: 100, repo: repo.NewDataQualityRepo(store), unsRepo: unsRepo}
	view, err := service.SavePolicy(ctx, PolicyInput{
		UnsID: metric.ID, NotifyOnIssue: true, ExpectedIntervalMs: 2000, MissingGraceMs: 500,
		Enabled: false, Rules: []string{ruleDuplicate},
	})
	require.NoError(t, err)
	require.Equal(t, true, view["enabled"])
	require.Equal(t, false, view["notifyOnIssue"])
	require.Equal(t, []string{ruleMissingTelemetry, ruleValueRange}, view["rules"])

	metric.Schema = json.RawMessage(`[{"name":"pressure","type":"DOUBLE","lowerLimit":0,"upperLimit":0}]`)
	metric.UpdatedBy = 1
	_, err = unsRepo.UpdateUnsNode(ctx, metric, nil, nil)
	require.NoError(t, err)
	view, err = service.SavePolicy(ctx, PolicyInput{UnsID: metric.ID, NotifyOnIssue: true})
	require.NoError(t, err)
	require.Equal(t, false, view["enabled"])
	require.Equal(t, []string{}, view["rules"])
}
