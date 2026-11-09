package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"backend/internal/domain/sourcehealth"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *SourceStreamRepo) ObserveSourceCheck(ctx context.Context, id, revision int64, result sourcehealth.Result) error {
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		row, data, err := sourceRow(tx, id, "SHARE")
		if err != nil {
			return err
		}
		if row.Revision != revision || data.DesiredState == "disabled" {
			return ErrConflict
		}
		stateRow, state, err := stateRow(tx, id, true)
		if err != nil {
			return err
		}
		previous := state.ConnectionCheck
		if result.Mode == sourcehealth.Data {
			previous = state.DataCheck
		}
		if previous != nil && previous.CheckedAt > result.CheckedAt {
			return ErrConflict
		}
		if result.Mode == sourcehealth.Data {
			state.DataCheck = &result
		} else {
			state.ConnectionCheck = &result
			state.Health = result.Health
			state.HealthMessage = result.Message
			if result.Health == "healthy" {
				state.HealthMessage = ""
			}
			state.CheckedAt = result.CheckedAt
		}
		return saveMetaData(tx, &stateRow, state)
	})
}

// ClaimSourceHealth 的短事务领取持久租约；网络检查在事务外执行。
func (r *SourceStreamRepo) ClaimSourceHealth(ctx context.Context, now time.Time, interval time.Duration) (int64, func(), error) {
	var id int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state UnsMeta
		query := tx.Table("public.uns_meta AS state").Select("state.*").Joins("JOIN public.uns_meta AS source ON source.kind='source' AND source.id=state.id AND source.deleted_at=0").Where("state.kind='source_state' AND state.deleted_at=0 AND COALESCE(source.data->>'schemaVersion','')<>'1' AND COALESCE(NULLIF(source.data->>'desiredState',''),'enabled')='enabled'").Where("COALESCE((state.data->>'healthLeaseUntil')::bigint,0)<=? AND COALESCE((state.data->'connectionCheck'->>'checkedAt')::bigint,0)<=?", now.UnixMilli(), now.Add(-interval).UnixMilli()).Order("COALESCE((state.data->'connectionCheck'->>'checkedAt')::bigint,0),state.id").Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "state"}, Options: "SKIP LOCKED"})
		if err := query.Take(&state).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		var data map[string]any
		decoder := json.NewDecoder(bytes.NewReader(state.Data))
		decoder.UseNumber()
		if err := decoder.Decode(&data); err != nil {
			return err
		}
		data["healthLeaseUntil"] = now.Add(interval).UnixMilli()
		if err := saveMetaData(tx, &state, data); err != nil {
			return err
		}
		var err error
		id, err = strconv.ParseInt(state.ID, 10, 64)
		return err
	})
	// 租约直到下个周期自动到期，失败也不会每分钟重复冲击设备。
	return id, func() {}, err
}
