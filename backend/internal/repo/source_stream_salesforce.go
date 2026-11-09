package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// SalesforceState serializes acquisition metadata without changing Source configuration.
func (r *SourceStreamRepo) SalesforceState(ctx context.Context, id, revision int64, fn func(map[string]json.RawMessage) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, source, err := sourceRow(tx, id, "SHARE")
		if err != nil {
			return err
		}
		if source.ConfigRevision != revision || source.DesiredState != "enabled" {
			return ErrConflict
		}
		var row UnsMeta
		if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("kind = ? AND id = ?", "source_state", fmt.Sprint(id)).Take(&row).Error; err != nil {
			return err
		}
		var outer map[string]json.RawMessage
		if err = json.Unmarshal(row.Data, &outer); err != nil {
			return err
		}
		state := map[string]json.RawMessage{}
		if len(outer["salesforce"]) > 0 {
			if err = json.Unmarshal(outer["salesforce"], &state); err != nil {
				return err
			}
		}
		if err = fn(state); err != nil {
			return err
		}
		raw, err := json.Marshal(state)
		if err != nil {
			return err
		}
		outer["salesforce"] = raw
		raw, err = json.Marshal(outer)
		if err != nil {
			return err
		}
		return tx.Model(&UnsMeta{}).Where("kind = ? AND id = ?", "source_state", row.ID).Updates(map[string]any{"data": raw, "revision": row.Revision + 1, "updated_at": time.Now().UnixMilli()}).Error
	})
}
