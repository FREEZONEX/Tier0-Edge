CREATE TABLE IF NOT EXISTS uns_data_quality_policy (
    uns_id BIGINT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT false,
    expected_interval_ms BIGINT NOT NULL DEFAULT 0,
    sequence_field VARCHAR(128) NOT NULL DEFAULT '',
    missing_grace_ms BIGINT NOT NULL DEFAULT 0,
    late_threshold_ms BIGINT NOT NULL DEFAULT 0,
    clock_skew_tolerance_ms BIGINT NOT NULL DEFAULT 0,
    gap_tolerance_ms BIGINT NOT NULL DEFAULT 0,
    out_of_order_tolerance_ms BIGINT NOT NULL DEFAULT 0,
    frequency_tolerance_percent INTEGER NOT NULL DEFAULT 20,
    min_samples INTEGER NOT NULL DEFAULT 3,
    flat_min_samples INTEGER NOT NULL DEFAULT 0,
    flat_min_duration_ms BIGINT NOT NULL DEFAULT 0,
    mandatory_fields JSONB NOT NULL DEFAULT '[]'::jsonb,
    flat_fields JSONB NOT NULL DEFAULT '[]'::jsonb,
    stale_fields JSONB NOT NULL DEFAULT '[]'::jsonb,
    rules JSONB NOT NULL DEFAULT '[]'::jsonb,
    severity_overrides JSONB NOT NULL DEFAULT '{}'::jsonb,
    sampling_priority INTEGER NOT NULL DEFAULT 0,
    cooldown_ms BIGINT NOT NULL DEFAULT 60000,
    version BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_uns_data_quality_policy_enabled
    ON uns_data_quality_policy(enabled, uns_id);

CREATE TABLE IF NOT EXISTS uns_data_quality_run (
    id BIGSERIAL PRIMARY KEY,
    uns_id BIGINT NOT NULL,
    policy_version BIGINT NOT NULL DEFAULT 0,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    mode VARCHAR(32) NOT NULL,
    sample_count BIGINT NOT NULL DEFAULT 0,
    received_count BIGINT NOT NULL DEFAULT 0,
    persisted_count BIGINT NOT NULL DEFAULT 0,
    drop_count BIGINT NOT NULL DEFAULT 0,
    health_status VARCHAR(32) NOT NULL DEFAULT 'warming',
    metrics JSONB NOT NULL DEFAULT '{}'::jsonb,
    issue_counts JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_uns_data_quality_run_node_time
    ON uns_data_quality_run(uns_id, evaluated_at DESC);

CREATE TABLE IF NOT EXISTS uns_data_quality_issue (
    id BIGSERIAL PRIMARY KEY,
    issue_key VARCHAR(192) NOT NULL,
    uns_id BIGINT NOT NULL,
    rule_code VARCHAR(64) NOT NULL,
    severity VARCHAR(16) NOT NULL DEFAULT 'warning',
    state VARCHAR(16) NOT NULL DEFAULT 'open',
    first_observed_at TIMESTAMPTZ NOT NULL,
    last_observed_at TIMESTAMPTZ NOT NULL,
    occurrences BIGINT NOT NULL DEFAULT 1,
    summary VARCHAR(512) NOT NULL DEFAULT '',
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    policy_version BIGINT NOT NULL DEFAULT 0,
    acknowledged_by BIGINT NOT NULL DEFAULT 0,
    acknowledged_at TIMESTAMPTZ NULL,
    resolved_at TIMESTAMPTZ NULL,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_data_quality_issue_active
    ON uns_data_quality_issue(uns_id, rule_code)
    WHERE state IN ('open', 'ack');
CREATE INDEX IF NOT EXISTS idx_uns_data_quality_issue_list
    ON uns_data_quality_issue(state, severity, last_observed_at DESC);
