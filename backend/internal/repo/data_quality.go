package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DataQualityPolicy struct {
	// UnsID only identifies the owning node at runtime. It is deliberately not
	// serialized into uns_namespace_node_info.data_quality_policy.
	UnsID                     int64             `gorm:"-" json:"-"`
	Enabled                   bool              `json:"enabled"`
	NotifyOnIssue             bool              `json:"notifyOnIssue"`
	ExpectedIntervalMs        int64             `json:"expectedIntervalMs"`
	SequenceField             string            `json:"sequenceField"`
	MissingGraceMs            int64             `json:"missingGraceMs"`
	LateThresholdMs           int64             `json:"lateThresholdMs"`
	ClockSkewToleranceMs      int64             `json:"clockSkewToleranceMs"`
	GapToleranceMs            int64             `json:"gapToleranceMs"`
	OutOfOrderToleranceMs     int64             `json:"outOfOrderToleranceMs"`
	FrequencyTolerancePercent int               `json:"frequencyTolerancePercent"`
	MinSamples                int               `json:"minSamples"`
	FlatMinSamples            int               `json:"flatMinSamples"`
	FlatMinDurationMs         int64             `json:"flatMinDurationMs"`
	MandatoryFields           []string          `json:"mandatoryFields"`
	FlatFields                []string          `json:"flatFields"`
	StaleFields               []string          `json:"staleFields"`
	Rules                     []string          `json:"rules"`
	SeverityOverrides         map[string]string `json:"severityOverrides"`
	SamplingPriority          int               `json:"samplingPriority"`
	CooldownMs                int64             `json:"cooldownMs"`
}

type dataQualityPolicyRow struct {
	ID                int64           `gorm:"column:id"`
	Type              int16           `gorm:"column:type"`
	DataQualityPolicy json.RawMessage `gorm:"column:data_quality_policy"`
}

func policyFromRow(row dataQualityPolicyRow) (DataQualityPolicy, error) {
	if row.Type != 2 || len(row.DataQualityPolicy) == 0 || string(row.DataQualityPolicy) == "null" {
		return DataQualityPolicy{}, ErrNotFound
	}
	var item DataQualityPolicy
	if err := json.Unmarshal(row.DataQualityPolicy, &item); err != nil {
		return DataQualityPolicy{}, fmt.Errorf("decode data quality policy for UNS node %d: %w", row.ID, err)
	}
	item.UnsID = row.ID
	return item, nil
}

type DataQualityIssue struct {
	ID              int64           `gorm:"column:id;primaryKey" json:"id"`
	IssueKey        string          `gorm:"column:issue_key" json:"issueKey"`
	UnsID           int64           `gorm:"column:uns_id" json:"unsId"`
	RuleCode        string          `gorm:"column:rule_code" json:"ruleCode"`
	Severity        string          `gorm:"column:severity" json:"severity"`
	State           string          `gorm:"column:state" json:"state"`
	FirstObservedAt time.Time       `gorm:"column:first_observed_at" json:"firstObservedAt"`
	LastObservedAt  time.Time       `gorm:"column:last_observed_at" json:"lastObservedAt"`
	Occurrences     int64           `gorm:"column:occurrences" json:"occurrences"`
	Summary         string          `gorm:"column:summary" json:"summary"`
	Evidence        json.RawMessage `gorm:"column:evidence;type:jsonb" json:"evidence"`
	AcknowledgedBy  int64           `gorm:"column:acknowledged_by" json:"acknowledgedBy"`
	AcknowledgedAt  *time.Time      `gorm:"column:acknowledged_at" json:"acknowledgedAt"`
	ResolvedAt      *time.Time      `gorm:"column:resolved_at" json:"resolvedAt"`
	CreatedTime     time.Time       `gorm:"column:created_time" json:"createdTime"`
	UpdatedTime     time.Time       `gorm:"column:updated_time" json:"updatedTime"`
}

func (DataQualityIssue) TableName() string { return "uns_data_quality_issue" }

type DataQualityIssueFilter struct {
	UnsID    int64
	State    string
	RuleCode string
	Severity string
	Page     int
	Size     int
}

type DataQualityActiveIssueSummary struct {
	OpenIssues       int64            `json:"openIssues"`
	AckIssues        int64            `json:"ackIssues"`
	CriticalIssues   int64            `json:"criticalIssues"`
	ActiveRuleCounts map[string]int64 `json:"activeRuleCounts"`
}

type DataQualityActiveNodeSummary struct {
	UnsID     int64  `gorm:"column:uns_id" json:"unsId"`
	Namespace string `gorm:"column:namespace" json:"namespace"`
	Severity  string `gorm:"column:severity" json:"severity"`
}

type DataQualityRepo struct{ db *gorm.DB }

func NewDataQualityRepo(in any) *DataQualityRepo { return &DataQualityRepo{db: GetCommonConn(in)} }

func (r *DataQualityRepo) GetPolicy(ctx context.Context, unsID int64) (DataQualityPolicy, error) {
	var row dataQualityPolicyRow
	err := r.db.WithContext(ctx).Model(&UnsNode{}).
		Select("id", "type", "data_quality_policy").
		Where("id = ? AND deleted_time = 0", unsID).
		Take(&row).Error
	if err != nil {
		return DataQualityPolicy{}, normalizeDBError(err)
	}
	return policyFromRow(row)
}

func (r *DataQualityRepo) ListEnabledPolicies(ctx context.Context, limit int) ([]DataQualityPolicy, error) {
	rows := make([]dataQualityPolicyRow, 0)
	query := r.db.WithContext(ctx).Model(&UnsNode{}).
		Select("id", "type", "data_quality_policy").
		Where("deleted_time = 0 AND type = 2").
		Where("data_quality_policy @> ?::jsonb", `{"enabled":true}`).
		Order("id")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, normalizeDBError(err)
	}
	items := make([]DataQualityPolicy, 0, len(rows))
	for _, row := range rows {
		item, err := policyFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// ListActiveIssuesForNodes restores only the durable rule state needed by the
// in-memory evaluator after a restart. Runtime counters and sample windows are
// intentionally not restored, so every process still starts in warming phase.
func (r *DataQualityRepo) ListActiveIssuesForNodes(ctx context.Context, unsIDs []int64) ([]DataQualityIssue, error) {
	if len(unsIDs) == 0 {
		return []DataQualityIssue{}, nil
	}
	items := make([]DataQualityIssue, 0)
	err := r.db.WithContext(ctx).
		Select("uns_id", "rule_code", "state").
		Where("uns_id IN ? AND state IN ?", unsIDs, []string{"open", "ack"}).
		Order("uns_id, rule_code").
		Find(&items).Error
	return items, normalizeDBError(err)
}

// SavePolicy replaces the node-owned JSON document. Policy writes are
// intentionally last-write-wins; the domain service serializes writes within
// a single Enterprise backend process before switching the runtime policy.
func (r *DataQualityRepo) SavePolicy(ctx context.Context, item DataQualityPolicy, userID int64) (DataQualityPolicy, error) {
	payload, err := json.Marshal(item)
	if err != nil {
		return DataQualityPolicy{}, ErrInvalidArgument
	}
	now := time.Now().UTC().UnixMilli()
	result := r.db.WithContext(ctx).Model(&UnsNode{}).
		Where("id = ? AND type = 2 AND deleted_time = 0", item.UnsID).
		Updates(touchByValues(map[string]any{"data_quality_policy": json.RawMessage(payload)}, userID, now))
	if result.Error != nil {
		return DataQualityPolicy{}, normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return DataQualityPolicy{}, ErrNotFound
	}
	return r.GetPolicy(ctx, item.UnsID)
}

func (r *DataQualityRepo) CountEnabledPolicies(ctx context.Context) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&UnsNode{}).
		Where("deleted_time = 0 AND type = 2").
		Where("data_quality_policy @> ?::jsonb", `{"enabled":true}`).
		Count(&total).Error
	return total, normalizeDBError(err)
}

// OpenIssue 创建节点规则的活动 Issue，已有活动 Issue 时只更新聚合信息。
// created 仅在插入新记录时返回 true。
func (r *DataQualityRepo) OpenIssue(ctx context.Context, item DataQualityIssue) (issue DataQualityIssue, created bool, err error) {
	return r.OpenOrMergeIssue(ctx, item, 0)
}

// OpenOrMergeIssue reopens the most recently resolved Issue within the caller's
// merge window. Reopening retains identity and acknowledgement, returns created
// false, and must not trigger another first-Issue notification.
func (r *DataQualityRepo) OpenOrMergeIssue(ctx context.Context, item DataQualityIssue, mergeWindow time.Duration) (issue DataQualityIssue, created bool, err error) {
	return r.openOrMergeIssue(ctx, item, mergeWindow, false)
}

// OpenOrMergeActiveIssue persists an evaluator event only while its owning UNS
// node and rule are still active. The shared row lock serializes this check with
// moving the node to the recycle bin, so an event already queued at deletion
// time cannot reopen an Issue after the delete transaction commits.
func (r *DataQualityRepo) OpenOrMergeActiveIssue(ctx context.Context, item DataQualityIssue, mergeWindow time.Duration) (issue DataQualityIssue, created bool, err error) {
	return r.openOrMergeIssue(ctx, item, mergeWindow, true)
}

func (r *DataQualityRepo) openOrMergeIssue(ctx context.Context, item DataQualityIssue, mergeWindow time.Duration, requireActivePolicy bool) (issue DataQualityIssue, created bool, err error) {
	if item.IssueKey == "" {
		item.IssueKey = fmt.Sprintf("%d:%s", item.UnsID, item.RuleCode)
	}
	if item.State == "" {
		item.State = "open"
	}
	if item.Occurrences <= 0 {
		item.Occurrences = 1
	}
	now := time.Now().UTC()
	if item.LastObservedAt.IsZero() {
		item.LastObservedAt = now
	}
	if item.FirstObservedAt.IsZero() {
		item.FirstObservedAt = item.LastObservedAt
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if requireActivePolicy {
			if err := lockActiveDataQualityPolicy(tx, item.UnsID, item.RuleCode); err != nil {
				return err
			}
		}
		if err := lockDataQualityIssue(tx, item.UnsID, item.RuleCode); err != nil {
			return err
		}
		var active DataQualityIssue
		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("uns_id = ? AND rule_code = ? AND state IN ?", item.UnsID, item.RuleCode, []string{"open", "ack"}).
			Take(&active).Error
		if findErr != nil && findErr != gorm.ErrRecordNotFound {
			return findErr
		}
		if findErr == gorm.ErrRecordNotFound && mergeWindow > 0 {
			// Include recoveries newer than this event so stale queued events
			// cannot accidentally resurrect an already ended Issue.
			findErr = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("uns_id = ? AND rule_code = ? AND state = ?", item.UnsID, item.RuleCode, "resolved").
				Order("resolved_at DESC, id DESC").Take(&active).Error
			if findErr != nil && findErr != gorm.ErrRecordNotFound {
				return findErr
			}
			if findErr == nil {
				if active.ResolvedAt != nil && item.LastObservedAt.Before(*active.ResolvedAt) {
					issue = active
					return nil
				}
				if active.ResolvedAt == nil || item.LastObservedAt.Sub(*active.ResolvedAt) > mergeWindow {
					findErr = gorm.ErrRecordNotFound
				}
			}
		}
		if findErr == nil {
			updates := map[string]any{
				"occurrences": gorm.Expr("occurrences + ?", item.Occurrences), "updated_time": now,
			}
			if !item.LastObservedAt.Before(active.LastObservedAt) {
				updates["last_observed_at"] = item.LastObservedAt
				updates["severity"], updates["summary"], updates["evidence"] = item.Severity, item.Summary, item.Evidence
			}
			if active.State == "resolved" {
				state := "open"
				if active.AcknowledgedAt != nil {
					state = "ack"
				}
				updates["state"], updates["resolved_at"] = state, nil
			}
			if err := tx.Model(&DataQualityIssue{}).Where("id = ?", active.ID).Updates(updates).Error; err != nil {
				return err
			}
			return tx.Where("id = ?", active.ID).Take(&issue).Error
		}
		item.CreatedTime, item.UpdatedTime = now, now
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		issue, created = item, true
		return nil
	})
	if err != nil {
		return DataQualityIssue{}, false, normalizeDBError(err)
	}
	return issue, created, nil
}

func lockActiveDataQualityPolicy(tx *gorm.DB, unsID int64, ruleCode string) error {
	var row dataQualityPolicyRow
	err := tx.Model(&UnsNode{}).
		Clauses(clause.Locking{Strength: "SHARE"}).
		Select("id", "type", "data_quality_policy").
		Where("id = ? AND deleted_time = 0 AND type = 2", unsID).
		Take(&row).Error
	if err != nil {
		return err
	}
	policy, err := policyFromRow(row)
	if err != nil || !policy.Enabled {
		return ErrNotFound
	}
	for _, enabledRule := range policy.Rules {
		if enabledRule == ruleCode {
			return nil
		}
	}
	return ErrNotFound
}

func lockDataQualityIssue(tx *gorm.DB, unsID int64, ruleCode string) error {
	key := fmt.Sprintf("data-quality-issue:%d:%s", unsID, ruleCode)
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", key).Error
}

func (r *DataQualityRepo) ResolveIssue(ctx context.Context, unsID int64, ruleCode string, observedAt time.Time, pendingOccurrences int64) error {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	updates := map[string]any{"state": "resolved", "resolved_at": observedAt, "last_observed_at": observedAt, "updated_time": observedAt}
	if pendingOccurrences > 0 {
		updates["occurrences"] = gorm.Expr("occurrences + ?", pendingOccurrences)
	}
	return normalizeDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockDataQualityIssue(tx, unsID, ruleCode); err != nil {
			return err
		}
		return tx.Model(&DataQualityIssue{}).
			Where("uns_id = ? AND rule_code = ? AND state IN ?", unsID, ruleCode, []string{"open", "ack"}).
			Where("last_observed_at <= ?", observedAt).
			Updates(updates).Error
	}))
}

// ResolveActiveIssuesForNodes ends every active Issue when UNS nodes leave the
// active namespace. Historical Issue rows remain available for audit, while
// restore starts a fresh evaluator lifecycle from the preserved node policy.
func (r *DataQualityRepo) ResolveActiveIssuesForNodes(ctx context.Context, unsIDs []int64, observedAt time.Time) error {
	if len(unsIDs) == 0 {
		return nil
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	return normalizeDBError(r.db.WithContext(ctx).Model(&DataQualityIssue{}).
		Where("uns_id IN ? AND state IN ?", unsIDs, []string{"open", "ack"}).
		Updates(map[string]any{
			"state": "resolved", "resolved_at": observedAt, "last_observed_at": observedAt, "updated_time": observedAt,
		}).Error)
}

func (r *DataQualityRepo) AcknowledgeIssue(ctx context.Context, id, userID int64) (DataQualityIssue, error) {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&DataQualityIssue{}).
		Where("id = ? AND state = ?", id, "open").
		Updates(map[string]any{"state": "ack", "acknowledged_by": userID, "acknowledged_at": now, "updated_time": now})
	if result.Error != nil {
		return DataQualityIssue{}, normalizeDBError(result.Error)
	}
	issue, err := r.GetIssue(ctx, id)
	if err != nil {
		return DataQualityIssue{}, err
	}
	if result.RowsAffected > 0 || issue.State == "ack" || issue.State == "resolved" {
		return issue, nil
	}
	return DataQualityIssue{}, ErrConflict
}

func (r *DataQualityRepo) GetIssue(ctx context.Context, id int64) (DataQualityIssue, error) {
	var item DataQualityIssue
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&item).Error
	return item, normalizeDBError(err)
}

func (r *DataQualityRepo) ListIssues(ctx context.Context, filter DataQualityIssueFilter) ([]DataQualityIssue, int64, error) {
	query := r.db.WithContext(ctx).Model(&DataQualityIssue{})
	if filter.UnsID > 0 {
		query = query.Where("uns_id = ?", filter.UnsID)
	}
	if filter.State != "" {
		query = query.Where("state = ?", filter.State)
	}
	if filter.RuleCode != "" {
		query = query.Where("rule_code = ?", filter.RuleCode)
	}
	if filter.Severity != "" {
		query = query.Where("severity = ?", filter.Severity)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	page, size := filter.Page, filter.Size
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	items := make([]DataQualityIssue, 0)
	err := query.Order("last_observed_at DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error
	return items, total, normalizeDBError(err)
}

// ListActiveIssueNodeSummaries returns one compact row per UNS node for tree
// decoration. Acknowledged issues remain active until they are resolved.
func (r *DataQualityRepo) ListActiveIssueNodeSummaries(ctx context.Context) ([]DataQualityActiveNodeSummary, error) {
	items := make([]DataQualityActiveNodeSummary, 0)
	err := r.db.WithContext(ctx).
		Table("uns_data_quality_issue AS issue").
		Select(`issue.uns_id, node.namespace,
			CASE MAX(CASE issue.severity
				WHEN 'critical' THEN 3
				WHEN 'warning' THEN 2
				ELSE 1
			END)
				WHEN 3 THEN 'critical'
				WHEN 2 THEN 'warning'
				ELSE 'info'
			END AS severity`).
		Joins("JOIN uns_namespace_node_info AS node ON node.id = issue.uns_id AND node.deleted_time = 0").
		Where("issue.state IN ?", []string{"open", "ack"}).
		Group("issue.uns_id, node.namespace").
		Order("issue.uns_id").
		Limit(10000).
		Scan(&items).Error
	return items, normalizeDBError(err)
}

// AggregateActiveIssues returns the durable active issue totals used by the
// data-quality overview. Occurrences are summed per rule in PostgreSQL so the
// overview does not need to load every issue row into memory.
func (r *DataQualityRepo) AggregateActiveIssues(ctx context.Context, unsID int64) (DataQualityActiveIssueSummary, error) {
	type aggregateRow struct {
		State       string `gorm:"column:state"`
		RuleCode    string `gorm:"column:rule_code"`
		Severity    string `gorm:"column:severity"`
		IssueCount  int64  `gorm:"column:issue_count"`
		Occurrences int64  `gorm:"column:occurrences"`
	}

	query := r.db.WithContext(ctx).Model(&DataQualityIssue{}).
		Select("state, rule_code, severity, COUNT(*) AS issue_count, COALESCE(SUM(occurrences), 0) AS occurrences").
		Where("state IN ?", []string{"open", "ack"})
	if unsID > 0 {
		query = query.Where("uns_id = ?", unsID)
	}
	rows := make([]aggregateRow, 0)
	if err := query.Group("state, rule_code, severity").Scan(&rows).Error; err != nil {
		return DataQualityActiveIssueSummary{}, normalizeDBError(err)
	}

	summary := DataQualityActiveIssueSummary{ActiveRuleCounts: make(map[string]int64)}
	for _, row := range rows {
		switch row.State {
		case "open":
			summary.OpenIssues += row.IssueCount
		case "ack":
			summary.AckIssues += row.IssueCount
		}
		if row.Severity == "critical" {
			summary.CriticalIssues += row.IssueCount
		}
		summary.ActiveRuleCounts[row.RuleCode] += row.Occurrences
	}
	return summary, nil
}
