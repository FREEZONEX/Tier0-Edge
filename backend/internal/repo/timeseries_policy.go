package repo

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

const (
	timeseriesHypertable       = "public.uns_timeserial"
	timeseriesRetentionLockKey = "tier0.uns.timeseries.retention-policy"
)

func (s *Store) syncSampleTimeseriesPolicy(ctx context.Context) error {
	return s.UnsDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Multiple backend processes share policy initialization. This is separate
		// from the data/metadata locks acquired only when conversion is needed.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`,
			"tier0.uns.sample.storage-policy").Error; err != nil {
			return fmt.Errorf("lock sample storage policy: %w", err)
		}
		if err := tx.Exec(`SELECT public.configure_sample_timeseries(?, ?)`,
			s.timeseriesRetentionYears, s.sampleCompressionAfterDays).Error; err != nil {
			return fmt.Errorf("configure sample storage policy: %w", err)
		}
		return nil
	})
}

func (s *Store) syncTimeseriesRetentionPolicy(ctx context.Context) error {
	db := s.UnsDB()
	if db == nil {
		return fmt.Errorf("sync timeseries retention policy: database is nil")
	}

	var timescaleEnabled bool
	if err := db.WithContext(ctx).Raw(`
SELECT EXISTS (
    SELECT 1
    FROM pg_extension
    WHERE extname = 'timescaledb'
)`).Scan(&timescaleEnabled).Error; err != nil {
		return fmt.Errorf("check timescaledb extension: %w", err)
	}
	if !timescaleEnabled {
		return nil
	}

	var hypertableExists bool
	if err := db.WithContext(ctx).Raw(`
SELECT EXISTS (
    SELECT 1
    FROM timescaledb_information.hypertables
    WHERE hypertable_schema = 'public'
      AND hypertable_name = 'uns_timeserial'
)`).Scan(&hypertableExists).Error; err != nil {
		return fmt.Errorf("check timeseries hypertable: %w", err)
	}
	if !hypertableExists {
		return nil
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			`SELECT pg_advisory_xact_lock(hashtext(?))`,
			timeseriesRetentionLockKey,
		).Error; err != nil {
			return fmt.Errorf("lock timeseries retention policy: %w", err)
		}

		var policyMatches bool
		if err := tx.Raw(`
SELECT EXISTS (
    SELECT 1
    FROM timescaledb_information.jobs
    WHERE hypertable_schema = 'public'
      AND hypertable_name = 'uns_timeserial'
      AND proc_name = 'policy_retention'
      AND (config ->> 'drop_after')::interval = make_interval(years => ?)
)`, s.timeseriesRetentionYears).Scan(&policyMatches).Error; err != nil {
			return fmt.Errorf("read timeseries retention policy: %w", err)
		}
		if policyMatches {
			return nil
		}

		if err := tx.Exec(
			`SELECT remove_retention_policy(CAST(? AS regclass), if_exists => TRUE)`,
			timeseriesHypertable,
		).Error; err != nil {
			return fmt.Errorf("remove timeseries retention policy: %w", err)
		}
		if err := tx.Exec(
			`SELECT add_retention_policy(CAST(? AS regclass), make_interval(years => ?))`,
			timeseriesHypertable,
			s.timeseriesRetentionYears,
		).Error; err != nil {
			return fmt.Errorf("add timeseries retention policy: %w", err)
		}
		return nil
	})
}
