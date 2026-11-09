// 本文件覆盖数据质量策略持久化与 Issue 聚合查询。
package repo_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func TestDataQualityRepo_AggregateActiveIssues(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	qualityRepo := repo.NewDataQualityRepo(ctx)
	now := time.Now().UTC()

	open, created, err := qualityRepo.OpenIssue(ctx, repo.DataQualityIssue{
		UnsID: 101, RuleCode: "duplicate", Severity: "critical", State: "open",
		FirstObservedAt: now, LastObservedAt: now, Occurrences: 4,
		Evidence: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, int64(4), open.Occurrences)
	updated, created, err := qualityRepo.OpenIssue(ctx, repo.DataQualityIssue{
		UnsID: 101, RuleCode: "duplicate", Severity: "critical", State: "open",
		FirstObservedAt: now, LastObservedAt: now.Add(time.Second), Occurrences: 2,
		Evidence: json.RawMessage(`{"count":2}`),
	})
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, open.ID, updated.ID)
	require.Equal(t, int64(6), updated.Occurrences)

	ack, created, err := qualityRepo.OpenIssue(ctx, repo.DataQualityIssue{
		UnsID: 102, RuleCode: "lateArrival", Severity: "warning", State: "open",
		FirstObservedAt: now, LastObservedAt: now, Occurrences: 2,
		Evidence: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	require.True(t, created)
	_, err = qualityRepo.AcknowledgeIssue(ctx, ack.ID, 1)
	require.NoError(t, err)

	resolved, created, err := qualityRepo.OpenIssue(ctx, repo.DataQualityIssue{
		UnsID: 101, RuleCode: "ingestionDrop", Severity: "critical", State: "open",
		FirstObservedAt: now, LastObservedAt: now, Occurrences: 9,
		Evidence: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, qualityRepo.ResolveIssue(ctx, resolved.UnsID, resolved.RuleCode, now, 0))

	summary, err := qualityRepo.AggregateActiveIssues(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), summary.OpenIssues)
	require.Equal(t, int64(1), summary.AckIssues)
	require.Equal(t, int64(1), summary.CriticalIssues)
	require.Equal(t, map[string]int64{"duplicate": 6, "lateArrival": 2}, summary.ActiveRuleCounts)

	filtered, err := qualityRepo.AggregateActiveIssues(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, int64(1), filtered.OpenIssues)
	require.Zero(t, filtered.AckIssues)
	require.Equal(t, map[string]int64{"duplicate": 6}, filtered.ActiveRuleCounts)
}

func TestDataQualityRepo_ListActiveIssueNodeSummaries(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	unsRepo := repo.NewUnsRepo(store)
	root, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{Name: "LineA", Type: 1}, nil, nil)
	require.NoError(t, err)
	criticalNode, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		ParentID: root.ID, Name: "temperature", Type: 2, TopicType: 3,
		Schema: json.RawMessage(`[{"name":"value","type":"DOUBLE"}]`),
	}, nil, nil)
	require.NoError(t, err)
	warningNode, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		ParentID: root.ID, Name: "pressure", Type: 2, TopicType: 3,
		Schema: json.RawMessage(`[{"name":"value","type":"DOUBLE"}]`),
	}, nil, nil)
	require.NoError(t, err)
	resolvedNode, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		ParentID: root.ID, Name: "speed", Type: 2, TopicType: 3,
		Schema: json.RawMessage(`[{"name":"value","type":"DOUBLE"}]`),
	}, nil, nil)
	require.NoError(t, err)

	qualityRepo := repo.NewDataQualityRepo(store)
	now := time.Now().UTC()
	open := func(nodeID int64, rule, severity string) repo.DataQualityIssue {
		t.Helper()
		issue, created, openErr := qualityRepo.OpenIssue(ctx, repo.DataQualityIssue{
			UnsID: nodeID, RuleCode: rule, Severity: severity, State: "open",
			FirstObservedAt: now, LastObservedAt: now, Evidence: json.RawMessage(`{}`),
		})
		require.NoError(t, openErr)
		require.True(t, created)
		return issue
	}
	open(criticalNode.ID, "valueRange", "info")
	open(criticalNode.ID, "missingTelemetry", "critical")
	ack := open(warningNode.ID, "valueRange", "warning")
	_, err = qualityRepo.AcknowledgeIssue(ctx, ack.ID, 1)
	require.NoError(t, err)
	resolved := open(resolvedNode.ID, "missingTelemetry", "critical")
	require.NoError(t, qualityRepo.ResolveIssue(ctx, resolved.UnsID, resolved.RuleCode, now, 0))

	items, err := qualityRepo.ListActiveIssueNodeSummaries(ctx)
	require.NoError(t, err)
	require.Equal(t, []repo.DataQualityActiveNodeSummary{
		{UnsID: criticalNode.ID, Namespace: criticalNode.Namespace, Severity: "critical"},
		{UnsID: warningNode.ID, Namespace: warningNode.Namespace, Severity: "warning"},
	}, items)
}

func TestDataQualityRepo_AcknowledgeIssueIsIdempotent(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	qualityRepo := repo.NewDataQualityRepo(ctx)
	now := time.Now().UTC()

	issue, created, err := qualityRepo.OpenIssue(ctx, repo.DataQualityIssue{
		UnsID: 201, RuleCode: "valueRange", Severity: "warning", State: "open",
		FirstObservedAt: now, LastObservedAt: now, Evidence: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	require.True(t, created)

	acknowledged, err := qualityRepo.AcknowledgeIssue(ctx, issue.ID, 1)
	require.NoError(t, err)
	require.Equal(t, "ack", acknowledged.State)
	require.Equal(t, int64(1), acknowledged.AcknowledgedBy)

	repeated, err := qualityRepo.AcknowledgeIssue(ctx, issue.ID, 2)
	require.NoError(t, err)
	require.Equal(t, acknowledged, repeated)

	require.NoError(t, qualityRepo.ResolveIssue(ctx, issue.UnsID, issue.RuleCode, now.Add(time.Second), 0))
	resolved, err := qualityRepo.AcknowledgeIssue(ctx, issue.ID, 2)
	require.NoError(t, err)
	require.Equal(t, "resolved", resolved.State)
	require.Equal(t, int64(1), resolved.AcknowledgedBy)

	_, err = qualityRepo.AcknowledgeIssue(ctx, issue.ID+1, 1)
	require.ErrorIs(t, err, repo.ErrNotFound)
}

func TestDataQualityRepo_MergeWindowKeepsIdentityAndAcknowledgement(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	ctx := context.Background()
	qualityRepo := repo.NewDataQualityRepo(store)
	t0 := time.Date(2026, 9, 8, 8, 0, 0, 0, time.UTC)
	item := repo.DataQualityIssue{UnsID: 301, RuleCode: "valueRange", Severity: "warning",
		FirstObservedAt: t0, LastObservedAt: t0, Evidence: json.RawMessage(`{"observedValue":11}`)}
	original, created, err := qualityRepo.OpenOrMergeIssue(ctx, item, 5*time.Minute)
	require.NoError(t, err)
	require.True(t, created)
	ack, err := qualityRepo.AcknowledgeIssue(ctx, original.ID, 1)
	require.NoError(t, err)
	require.NoError(t, qualityRepo.ResolveIssue(ctx, item.UnsID, item.RuleCode, t0.Add(30*time.Second), 2))

	// A new connection must still find the same merge group after a restart.
	reopened, err := repo.Open(ctx, config.DatabaseConf{UnsDbUrl: dsn})
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	qualityRepo = repo.NewDataQualityRepo(reopened)
	item.LastObservedAt = t0.Add(30*time.Second + 5*time.Minute)
	item.Evidence = json.RawMessage(`{"observedValue":22}`)
	merged, created, err := qualityRepo.OpenOrMergeIssue(ctx, item, 5*time.Minute)
	require.NoError(t, err)
	require.False(t, created, "the five-minute boundary is included")
	require.Equal(t, original.ID, merged.ID)
	require.True(t, original.FirstObservedAt.Equal(merged.FirstObservedAt))
	require.Equal(t, "ack", merged.State)
	require.Equal(t, ack.AcknowledgedAt, merged.AcknowledgedAt)
	require.Equal(t, int64(1), merged.AcknowledgedBy)
	require.Equal(t, int64(4), merged.Occurrences)
	require.Nil(t, merged.ResolvedAt)
	require.JSONEq(t, string(item.Evidence), string(merged.Evidence))

	// An older queued recovery must not close a more recent recurrence.
	require.NoError(t, qualityRepo.ResolveIssue(ctx, item.UnsID, item.RuleCode, t0.Add(time.Minute), 0))
	stillActive, err := qualityRepo.GetIssue(ctx, original.ID)
	require.NoError(t, err)
	require.Equal(t, "ack", stillActive.State)
	resolvedAt := item.LastObservedAt.Add(30 * time.Second)
	require.NoError(t, qualityRepo.ResolveIssue(ctx, item.UnsID, item.RuleCode, resolvedAt, 0))
	stale, created, err := qualityRepo.OpenOrMergeIssue(ctx, item, 5*time.Minute)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, "resolved", stale.State)
	require.Equal(t, int64(4), stale.Occurrences)

	item.LastObservedAt = resolvedAt.Add(5*time.Minute + time.Millisecond)
	item.FirstObservedAt = item.LastObservedAt
	next, created, err := qualityRepo.OpenOrMergeIssue(ctx, item, 5*time.Minute)
	require.NoError(t, err)
	require.True(t, created)
	require.NotEqual(t, original.ID, next.ID)
	require.Equal(t, "open", next.State)
	require.Nil(t, next.AcknowledgedAt)
	require.Equal(t, int64(1), next.Occurrences)
	_, total, err := qualityRepo.ListIssues(ctx, repo.DataQualityIssueFilter{UnsID: item.UnsID})
	require.NoError(t, err)
	require.Equal(t, int64(2), total, "the original historical Issue is retained")
	for _, isolated := range []repo.DataQualityIssue{
		{UnsID: 302, RuleCode: "valueRange"}, {UnsID: 301, RuleCode: "missingTelemetry"},
	} {
		isolated.Evidence = json.RawMessage(`{}`)
		_, created, err = qualityRepo.OpenOrMergeIssue(ctx, isolated, 5*time.Minute)
		require.NoError(t, err)
		require.True(t, created, "different nodes and rules cannot share a merge group")
	}
}

func TestDataQualityRepo_ConcurrentOpenAndReopenCreateOnlyOnce(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	qualityRepo := repo.NewDataQualityRepo(store)
	now := time.Now().UTC().Truncate(time.Second)
	type result struct {
		issue   repo.DataQualityIssue
		created bool
		err     error
	}
	openConcurrently := func(at time.Time, wantCreated int) int64 {
		t.Helper()
		results := make(chan result, 12)
		var wg sync.WaitGroup
		for index := 0; index < cap(results); index++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				issue, created, err := qualityRepo.OpenOrMergeIssue(ctx, repo.DataQualityIssue{
					UnsID: 401, RuleCode: "missingTelemetry", Severity: "warning",
					FirstObservedAt: at, LastObservedAt: at, Evidence: json.RawMessage(`{}`),
				}, 5*time.Minute)
				results <- result{issue, created, err}
			}()
		}
		wg.Wait()
		close(results)
		createdCount := 0
		var id int64
		for result := range results {
			require.NoError(t, result.err)
			if id == 0 {
				id = result.issue.ID
			}
			require.Equal(t, id, result.issue.ID)
			if result.created {
				createdCount++
			}
		}
		require.Equal(t, wantCreated, createdCount)
		return id
	}
	id := openConcurrently(now, 1)
	require.NoError(t, qualityRepo.ResolveIssue(ctx, 401, "missingTelemetry", now.Add(30*time.Second), 0))
	require.Equal(t, id, openConcurrently(now.Add(time.Minute), 0))
	issue, err := qualityRepo.GetIssue(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(24), issue.Occurrences)
}

func TestDataQualityRepo_PolicySurvivesDatabaseReconnect(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	ctx := context.Background()
	unsRepo := repo.NewUnsRepo(store)
	root, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{Name: "LineA", Type: 1}, nil, nil)
	require.NoError(t, err)
	metric, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		ParentID: root.ID, Name: "temperature", Type: 2, TopicType: 3,
		Schema: json.RawMessage(`[{"name":"value","type":"DOUBLE"}]`),
	}, nil, nil)
	require.NoError(t, err)

	written, err := repo.NewDataQualityRepo(store).SavePolicy(ctx, repo.DataQualityPolicy{
		UnsID: metric.ID, Enabled: true, ExpectedIntervalMs: 2000, MissingGraceMs: 1000,
		Rules:             []string{"missingTelemetry", "valueRange"},
		SeverityOverrides: map[string]string{"missingTelemetry": "critical"},
	}, 0)
	require.NoError(t, err)
	require.True(t, written.Enabled)

	// Use a separate DB connection so the read does not share GORM statement or
	// schema state with the write, matching a backend process restart.
	reopened, err := repo.Open(ctx, config.DatabaseConf{UnsDbUrl: dsn})
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	loaded, err := repo.NewDataQualityRepo(reopened).GetPolicy(ctx, metric.ID)
	require.NoError(t, err)
	require.Equal(t, written, loaded)
	node, err := repo.NewUnsRepo(reopened).GetUnsNode(ctx, metric.ID, false)
	require.NoError(t, err)
	require.NotNil(t, node.DataQualityPolicy)
	require.Equal(t, written.ExpectedIntervalMs, node.DataQualityPolicy.ExpectedIntervalMs)
	unconfigured, err := unsRepo.CreateUnsNode(ctx, repo.UnsNode{
		ParentID: root.ID, Name: "pressure", Type: 2, TopicType: 3,
		Schema: json.RawMessage(`[{"name":"value","type":"DOUBLE"}]`),
	}, nil, nil)
	require.NoError(t, err)
	unconfiguredNode, err := repo.NewUnsRepo(reopened).GetUnsNode(ctx, unconfigured.ID, false)
	require.NoError(t, err)
	require.Nil(t, unconfiguredNode.DataQualityPolicy)
	_, err = repo.NewDataQualityRepo(reopened).GetPolicy(ctx, unconfigured.ID)
	require.ErrorIs(t, err, repo.ErrNotFound)

	enabled, err := repo.NewDataQualityRepo(reopened).ListEnabledPolicies(ctx, 10)
	require.NoError(t, err)
	require.Len(t, enabled, 1)
	require.Equal(t, metric.ID, enabled[0].UnsID)
}
