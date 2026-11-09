ALTER TABLE uns_namespace_node_info
    ADD COLUMN IF NOT EXISTS data_quality_policy JSONB DEFAULT NULL;

DO $$
BEGIN
    IF to_regclass('uns_data_quality_policy') IS NOT NULL THEN
        EXECUTE $migration$
            UPDATE uns_namespace_node_info AS node
            SET data_quality_policy = jsonb_build_object(
                'enabled', policy.enabled,
                'expectedIntervalMs', policy.expected_interval_ms,
                'sequenceField', policy.sequence_field,
                'missingGraceMs', policy.missing_grace_ms,
                'lateThresholdMs', policy.late_threshold_ms,
                'clockSkewToleranceMs', policy.clock_skew_tolerance_ms,
                'gapToleranceMs', policy.gap_tolerance_ms,
                'outOfOrderToleranceMs', policy.out_of_order_tolerance_ms,
                'frequencyTolerancePercent', policy.frequency_tolerance_percent,
                'minSamples', policy.min_samples,
                'flatMinSamples', policy.flat_min_samples,
                'flatMinDurationMs', policy.flat_min_duration_ms,
                'mandatoryFields', policy.mandatory_fields,
                'flatFields', policy.flat_fields,
                'staleFields', policy.stale_fields,
                'rules', policy.rules,
                'severityOverrides', policy.severity_overrides,
                'samplingPriority', policy.sampling_priority,
                'cooldownMs', policy.cooldown_ms
            )
            FROM uns_data_quality_policy AS policy
            WHERE node.id = policy.uns_id
              AND node.data_quality_policy IS NULL
        $migration$;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_uns_namespace_node_data_quality_policy
    ON uns_namespace_node_info USING GIN (data_quality_policy jsonb_path_ops)
    WHERE data_quality_policy IS NOT NULL;

ALTER TABLE uns_data_quality_issue
    DROP COLUMN IF EXISTS policy_version;

DROP TABLE IF EXISTS uns_data_quality_run;
DROP TABLE IF EXISTS uns_data_quality_policy;
