package repo

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// This authorization is owned by the executing Edge's UNS database. Enrollment
// or a correctly signed center token cannot create or expand it.
type ControlGrant struct {
	AllowAutomatic    bool   `json:"allowAutomatic"`
	Principal         string `gorm:"primaryKey" json:"principal"`
	ControlCode       string `gorm:"primaryKey" json:"controlCode"`
	GrantID           string `json:"grantId"`
	Revision          int64  `json:"revision"`
	CapabilityVersion string `json:"capabilityVersion"`
	InputSchema       string `gorm:"type:jsonb" json:"-"`
	MaxTTLMillis      int64  `json:"maxTtlMs"`
	MinIntervalMillis int64  `json:"minIntervalMs"`
	Enabled           bool   `json:"enabled"`
	LastInvokedAt     int64  `json:"lastInvokedAt"`
	UpdatedBy         int64  `json:"updatedBy,string"`
	UpdatedAt         int64  `json:"updatedAt"`
}

func (ControlGrant) TableName() string { return "public.uns_control_grant" }

var ErrControlRate = errors.New("control rate limited")
var ErrControlGrant = errors.New("control grant denied")

func (r *SourceStreamRepo) ControlGrant(ctx context.Context, principal, code string) (ControlGrant, error) {
	var row ControlGrant
	err := r.db.WithContext(ctx).Where("principal=? AND control_code=?", principal, code).Take(&row).Error
	return row, normalizeDBError(err)
}

func (r *SourceStreamRepo) SaveControlGrant(ctx context.Context, row ControlGrant, expected int64) (ControlGrant, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(?)", int64(846031001)).Error; err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "control-grant:"+row.Principal+":"+row.ControlCode).Error; err != nil {
			return err
		}
		var old ControlGrant
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("principal=? AND control_code=?", row.Principal, row.ControlCode).Take(&old).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if old.Revision != expected {
			return ErrConflict
		}
		if row.Enabled {
			_, definition, err := NewSourceStreamRepo(tx).Operation(ctx, row.ControlCode)
			if err != nil {
				return err
			}
			if definition.Status != "active" || definition.CapabilityVersion() != row.CapabilityVersion {
				return ErrControlCapability
			}
		}
		if old.Revision > 0 {
			row.GrantID = old.GrantID
			row.LastInvokedAt = old.LastInvokedAt
		}
		row.Revision = expected + 1
		row.UpdatedAt = time.Now().UnixMilli()
		return tx.Save(&row).Error
	})
	return row, normalizeDBError(err)
}

// The caller supplies the live expected identity/revision from verified task
// claims. Admission atomically consumes rate allowance with its reservation.
func (r *SourceStreamRepo) CheckControlGrant(ctx context.Context, principal, code, grantID string, revision int64, capability string, expires int64, admit bool) (ControlGrant, error) {
	var row ControlGrant
	q := r.db.WithContext(ctx)
	if admit {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Where("principal=? AND control_code=?", principal, code).Take(&row).Error; err != nil {
		return row, ErrControlGrant
	}
	if !row.Enabled || row.GrantID != grantID || row.Revision != revision || row.CapabilityVersion != capability {
		return row, ErrControlGrant
	}
	if expires != 0 {
		now := time.Now().UnixMilli()
		if expires <= now {
			return row, ErrControlExpired
		}
		if expires > now+row.MaxTTLMillis {
			return row, ErrControlGrant
		}
		if admit {
			if row.LastInvokedAt+row.MinIntervalMillis > now {
				return row, ErrControlRate
			}
			if err := r.db.WithContext(ctx).Model(&ControlGrant{}).Where("principal=? AND control_code=?", principal, code).Update("last_invoked_at", now).Error; err != nil {
				return row, err
			}
		}
	}
	return row, nil
}
