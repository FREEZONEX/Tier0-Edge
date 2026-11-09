package unsmeta

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"gorm.io/gorm"
)

var ErrRecordLocked = errors.New("record lease conflict")

type recordLeaseKey struct{}
type recordLeaseCredential struct {
	Token   string
	Release bool
}

func WithRecordLease(ctx context.Context, token string, release bool) context.Context {
	return context.WithValue(ctx, recordLeaseKey{}, recordLeaseCredential{token, release})
}
func RecordLeasePrincipal(ctx context.Context) string {
	s, ok := contextx.SubjectFrom(ctx)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d/%d/%s", s.UserID, s.APIKeyID, s.MCPGrantID)
}
func CheckRecordLease(ctx context.Context, row repo.UnsRecord) error {
	c, _ := ctx.Value(recordLeaseKey{}).(recordLeaseCredential)
	active := row.LockExpiresAt != nil && row.LockExpiresAt.After(time.Now())
	if !active {
		if c.Token != "" || c.Release {
			return ErrRecordLocked
		}
		return nil
	}
	sum := sha256.Sum256([]byte(c.Token))
	if c.Token == "" || row.LockPrincipal != RecordLeasePrincipal(ctx) || subtle.ConstantTimeCompare(sum[:], row.LockTokenHash) != 1 {
		return ErrRecordLocked
	}
	return nil
}
func clearRecordLeaseColumns() map[string]any {
	return map[string]any{"lock_token_hash": nil, "lock_principal": nil, "lock_label": nil, "lock_expires_at": nil, "lock_since": nil}
}
func releaseRecordLease(ctx context.Context, tx *gorm.DB, row repo.UnsRecord) error {
	c, _ := ctx.Value(recordLeaseKey{}).(recordLeaseCredential)
	if !c.Release {
		return nil
	}
	return tx.Model(&row).UpdateColumns(clearRecordLeaseColumns()).Error
}

func (s *Service) RestoreRecord(ctx context.Context, collectionID, id string) (repo.UnsRecord, error) {
	if err := repo.RequireLocalCollection(ctx, collectionID); err != nil {
		return repo.UnsRecord{}, err
	}
	var restored repo.UnsRecord
	if !ValidID(collectionID) || !ValidID(id) {
		return restored, ErrInvalid
	}
	err := repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, true); err != nil {
			return err
		}
		if err := repo.LockUnsRecords(tx, repo.UnsRecordLock{CollectionID: collectionID, RecordID: id}); err != nil {
			return err
		}
		_, doc, err := writableCollection(ctx, tx, collectionID)
		if err != nil {
			return err
		}
		row, err := recordForWrite(tx, collectionID, id)
		if err != nil {
			return err
		}
		if !row.DeletedAt.Valid {
			restored = row
			return nil
		}
		if row.SourceID != nil {
			return ErrReferenced
		}
		var value any
		if decodeNumbers(row.Data, &value) != nil || doc.RecordSchema == nil {
			return ErrData
		}
		if !doc.RecordSchema.accepts(value) {
			path, reason := doc.RecordSchema.Explain(value)
			return &SchemaMismatchError{Path: path, Reason: reason}
		}
		now := time.Now().UnixMilli()
		revision := row.Revision + 1
		updates := clearRecordLeaseColumns()
		updates["deleted_at"] = nil
		updates["revision"] = revision
		updates["updated_at"] = now
		if err := tx.Unscoped().Model(&row).UpdateColumns(updates).Error; err != nil {
			return err
		}
		row.DeletedAt = gorm.DeletedAt{}
		row.Revision = revision
		row.UpdatedAt = now
		restored = row
		return enqueueRecordEvent(ctx, tx, row)
	})
	return restored, err
}
