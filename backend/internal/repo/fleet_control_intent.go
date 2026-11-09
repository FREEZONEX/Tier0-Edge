package repo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// Intent and Collection effects reside in the same UNS database. Dispatch never
// occurs from this transaction. Unknown outcomes are read back, never resent.
type FleetControlIntent struct {
	CompletionEvidence json.RawMessage `gorm:"type:jsonb" json:"completionEvidence"`
	Automatic          bool            `json:"automatic"`
	InvocationID       string          `gorm:"primaryKey" json:"invocationId"`
	Principal          string          `json:"-"`
	IdempotencyKey     string          `json:"idempotencyKey"`
	RequestHash        string          `json:"requestHash"`
	InputHash          string          `json:"inputHash"`
	Actor              json.RawMessage `gorm:"type:jsonb" json:"-"`
	NodeID             string          `json:"nodeId"`
	ControlCode        string          `json:"controlCode"`
	CapabilityVersion  string          `json:"capabilityVersion"`
	GrantID            string          `json:"grantId"`
	GrantRevision      int64           `json:"grantRevision"`
	DeviceParameter    string          `json:"deviceParameter"`
	DeviceID           string          `json:"deviceId"`
	Input              json.RawMessage `gorm:"type:jsonb" json:"input"`
	ExpiresAt          int64           `json:"expiresAt"`
	State              string          `json:"state"`
	Revision           int64           `json:"revision"`
	CreatedBy          int64           `json:"createdBy,string"`
	ConfirmedBy        int64           `json:"confirmedBy,string"`
	CreatedAt          int64           `json:"createdAt"`
	UpdatedAt          int64           `json:"updatedAt"`
	DispatchedAt       int64           `json:"dispatchedAt"`
	QueryAfter         int64           `json:"queryAfter"`
	QueryCount         int64           `json:"queryCount"`
	LastError          string          `json:"lastError"`
	Result             json.RawMessage `gorm:"type:jsonb" json:"result"`
	BusinessCompletion string          `json:"businessCompletion"`
	Causality          json.RawMessage `gorm:"type:jsonb" json:"causality"`
}

func (FleetControlIntent) TableName() string { return "public.uns_fleet_control_intent" }

// This method accepts an existing UNS transaction, including the delivery
// effects transaction. Its digest covers every fixed request/target field.
func PutFleetControlIntent(ctx context.Context, tx *gorm.DB, row FleetControlIntent) (FleetControlIntent, bool, error) {
	if row.InvocationID == "" || row.RequestHash == "" || row.Principal == "" || row.State != "pendingConfirmation" && row.State != "queued" {
		return row, false, ErrInvalidArgument
	}
	if err := tx.Exec("SELECT pg_advisory_xact_lock(814206115)").Error; err != nil {
		return row, false, err
	}
	if err := tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,814206113))", row.InvocationID).Error; err != nil {
		return row, false, err
	}
	var old FleetControlIntent
	err := tx.WithContext(ctx).Where("invocation_id=?", row.InvocationID).Take(&old).Error
	if err == nil {
		if old.RequestHash != row.RequestHash {
			return old, false, ErrControlIdempotencyConflict
		}
		return old, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return row, false, err
	}
	// Serialize capacity admission independently from per-device cooldown.

	var pending struct {
		Total int64
		Node  int64
	}
	if err := tx.Model(&FleetControlIntent{}).Select("count(*) AS total,count(*) FILTER(WHERE node_id=?) AS node", row.NodeID).Where("state IN ? OR (state='succeeded' AND business_completion IN ('unknown','accepted') AND causality @> '{\"awaitBusinessCompletion\":true}'::jsonb)", []string{"pendingConfirmation", "queued", "dispatched", "unknown"}).Scan(&pending).Error; err != nil {
		return row, false, err
	}
	if pending.Total >= 100000 || pending.Node >= 10000 {
		return row, false, ErrFleetQueueFull
	}
	now := time.Now().UnixMilli()
	if row.ExpiresAt <= now {
		return row, false, ErrControlExpired
	}
	row.Revision = 1
	row.CreatedAt = now
	row.UpdatedAt = now
	row.BusinessCompletion = "unknown"
	if len(row.CompletionEvidence) == 0 {
		row.CompletionEvidence = json.RawMessage("{}")
	}
	if len(row.Result) == 0 {
		row.Result = json.RawMessage("null")
	}
	if len(row.Causality) == 0 {
		row.Causality = json.RawMessage("{}")
	}
	err = tx.WithContext(ctx).Create(&row).Error
	return row, false, err
}

func GetFleetControlIntent(ctx context.Context, id string) (FleetControlIntent, error) {
	var row FleetControlIntent
	err := GetUnsConn(ctx).Where("invocation_id=?", id).Take(&row).Error
	return row, normalizeDBError(err)
}

func ConfirmFleetControlIntent(ctx context.Context, id string, revision, userID int64, actor json.RawMessage) (FleetControlIntent, error) {
	var row FleetControlIntent
	err := GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("invocation_id=?", id).Take(&row).Error; err != nil {
			return err
		}
		if row.Revision != revision || row.State != "pendingConfirmation" {
			return ErrConflict
		}
		if row.ExpiresAt <= time.Now().UnixMilli() {
			return ErrControlExpired
		}
		row.State = "queued"
		row.ConfirmedBy = userID
		row.Actor = actor
		row.Revision++
		row.UpdatedAt = time.Now().UnixMilli()
		return tx.Save(&row).Error
	})
	return row, normalizeDBError(err)
}

// Queued -> dispatched is durable before network I/O. A crash at any point
// thereafter can only query this invocation, including a crash before sending.
func ClaimFleetControlIntent(ctx context.Context) (FleetControlIntent, bool, error) {
	var row FleetControlIntent
	claimed := false
	err := GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UnixMilli()
		if err := tx.Model(&FleetControlIntent{}).Where("state IN ? AND expires_at<=?", []string{"queued", "pendingConfirmation"}, now).Updates(map[string]any{"state": "expired", "updated_at": now, "revision": gorm.Expr("revision+1")}).Error; err != nil {
			return err
		}
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("state='queued' AND expires_at>?", now).Order("created_at,invocation_id").Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		row.State = "dispatched"
		row.DispatchedAt = now
		row.UpdatedAt = now
		row.QueryAfter = now + 30000
		row.Revision++
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return row, claimed, err
}

func FinishFleetControlIntent(ctx context.Context, id, state, reason string, result json.RawMessage) error {
	if state != "succeeded" && state != "failed" && state != "unknown" && state != "expired" {
		return ErrInvalidArgument
	}
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
	return GetUnsConn(ctx).Model(&FleetControlIntent{}).Where("invocation_id=? AND state IN ?", id, []string{"dispatched", "unknown"}).Updates(map[string]any{"state": state, "last_error": reason, "result": result, "updated_at": time.Now().UnixMilli(), "query_after": time.Now().Add(10 * time.Second).UnixMilli(), "revision": gorm.Expr("revision+1")}).Error
}

func FleetContentHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	// RawMessage may have been loaded from JSONB; normalize object ordering and
	// whitespace while preserving decimal integers rather than float coercion.
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&normalized); err != nil {
		return "", err
	}
	raw, err = json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// EnqueueFleetPublished runs only in the acquisition transaction, together with
// its checkpoint and optional local Collection writes. True selects V2 transport;
// false means this output has no V2 policy and retains its existing transport.
