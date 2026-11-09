package repo

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// PrepareUnsSampleWrites must precede Meta/Record DML and record identity locks.
// A first Timescale chunk takes locks on its FK parents; taking its creation
// lock after updating those parents lets concurrent writers deadlock. Existing
// chunks retain parallel writes, and plain PostgreSQL needs no chunk gate.
func PrepareUnsSampleWrites(tx *gorm.DB, times ...time.Time) error {
	if len(times) == 0 {
		return nil
	}
	if tx == nil || tx.Statement == nil || len(times) > 10000 {
		return ErrInvalidArgument
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return ErrInvalidArgument
	}
	values := make([]string, 0, len(times))
	for _, stamp := range times {
		if stamp.UnixMilli() <= 0 || stamp.UnixMilli() > 253402300799999 {
			return ErrInvalidArgument
		}
		values = append(values, stamp.UTC().Format(time.RFC3339Nano))
	}
	var enabled bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='timescaledb')`).Scan(&enabled).Error; err != nil || !enabled {
		return err
	}
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM timescaledb_information.hypertables WHERE hypertable_schema='public' AND hypertable_name='uns_sample')`).Scan(&enabled).Error; err != nil || !enabled {
		return err
	}
	raw, _ := json.Marshal(values)
	var missing bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM jsonb_array_elements_text(?::jsonb) AS wanted(stamp)
	 WHERE NOT EXISTS(SELECT 1 FROM timescaledb_information.chunks chunk
	 WHERE chunk.hypertable_schema='public' AND chunk.hypertable_name='uns_sample'
	 AND chunk.range_start<=wanted.stamp::timestamptz AND wanted.stamp::timestamptz<chunk.range_end))`, string(raw)).Scan(&missing).Error; err != nil || !missing {
		return err
	}
	return tx.Exec(`LOCK TABLE public.uns_sample IN SHARE UPDATE EXCLUSIVE MODE`).Error
}
