package repo

import (
	"context"
	"encoding/hex"
	"sort"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EDIContentIdentity struct {
	SourceID       int64  `gorm:"column:source_id;primaryKey"`
	SelectionID    string `gorm:"column:selection_id;primaryKey"`
	IdentitySHA256 string `gorm:"column:identity_sha256;primaryKey"`
	ContentSHA256  string `gorm:"column:content_sha256"`
	ObservedAt     int64  `gorm:"column:observed_at"`
}

func (EDIContentIdentity) TableName() string { return "public.source_edi_content_identity" }
func validEDIDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

// ObserveEDIContent never suppresses replay. The unique key arbitrates competing
// first observations and the returned original hash exposes content reuse.
// Nothing here acknowledges cloud persistence or marks a remote file processed.
func (r *SourceStreamRepo) ObserveEDIContent(ctx context.Context, sourceID int64, selection string, content map[string]string, observe bool) (map[string]string, error) {
	if sourceID <= 0 || selection == "" || utf8.RuneCountInString(selection) > 63 || len(content) > 20000 {
		return nil, ErrInvalidArgument
	}
	rows := make([]EDIContentIdentity, 0, len(content))
	keys := make([]string, 0, len(content))
	now := time.Now().UnixMilli()
	for identity, digest := range content {
		if !validEDIDigest(identity) || !validEDIDigest(digest) {
			return nil, ErrInvalidArgument
		}
		rows = append(rows, EDIContentIdentity{sourceID, selection, identity, digest, now})
		keys = append(keys, identity)
	}
	found := map[string]string{}
	sort.Slice(rows, func(i, j int) bool { return rows[i].IdentitySHA256 < rows[j].IdentitySHA256 })
	if len(rows) == 0 {
		return found, nil
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if observe {
			if e := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 100).Error; e != nil {
				return e
			}
		}
		var originals []EDIContentIdentity
		if e := tx.Where("source_id=? AND selection_id=? AND identity_sha256 IN ?", sourceID, selection, keys).Find(&originals).Error; e != nil {
			return e
		}
		for _, original := range originals {
			found[original.IdentitySHA256] = original.ContentSHA256
		}
		if observe && len(found) != len(content) {
			return ErrConflict
		}
		return nil
	})
	return found, err
}
