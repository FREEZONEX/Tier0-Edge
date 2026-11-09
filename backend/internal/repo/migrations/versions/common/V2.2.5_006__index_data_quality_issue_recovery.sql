-- Support bounded lookups of the last recovered Issue for recurrence merging.
CREATE INDEX IF NOT EXISTS idx_uns_data_quality_issue_recovery
    ON uns_data_quality_issue(uns_id, rule_code, resolved_at DESC, id DESC)
    WHERE state = 'resolved';
