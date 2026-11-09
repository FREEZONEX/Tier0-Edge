package unsv2

import (
	"backend/internal/repo"
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// Shared Sources use the same metadata gate, revision checks and mounted-resource guard
// as configuration writes. This never removes device configuration or data.
func (s *Service) sharedSourceState(ctx context.Context, tx *gorm.DB, c Change) (Result, error) {
	result := Result{OpID: c.OpID, Action: c.Action, Kind: "source", ID: c.ID.ID}
	restore := c.Action == "meta.restore"
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("kind='source' AND id=?", c.ID.ID)
	if restore {
		query = query.Where("deleted_at>0")
	} else {
		query = query.Where("deleted_at=0")
	}
	var row repo.UnsMeta
	if err := query.Take(&row).Error; err != nil {
		return result, err
	}
	if row.Revision != c.ExpectedRevision {
		return result, problem("VERSION_CONFLICT", "Source revision changed")
	}
	if row.MountID != nil {
		return result, problem("FORBIDDEN", "mounted Sources are read-only")
	}
	var data SourceData
	if json.Unmarshal(row.Data, &data) != nil || data.SchemaVersion != 1 || data.Adapter == "edge" {
		return result, invalid("id", "shared field Source required; other Sources retain their existing lifecycle API")
	}
	for _, b := range data.Config.Streams {
		if b.Enabled {
			return result, problem("REFERENCED", "disable every Stream before archiving")
		}
	}
	now := time.Now().UnixMilli()
	deletedAt := now
	result.Status = "archived"
	if restore {
		deletedAt = 0
		result.Status = "restored"
	}
	result.Revision = row.Revision + 1
	err := tx.Model(&row).Updates(map[string]any{"deleted_at": deletedAt, "revision": result.Revision, "updated_at": now}).Error
	return result, err
}
