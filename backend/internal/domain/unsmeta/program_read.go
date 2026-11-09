package unsmeta

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"backend/internal/repo"
	"gorm.io/gorm"
)

// ProgramReadOptions is bounded data access, not an arbitrary query language.
type ProgramReadOptions struct {
	RecordID string `json:"recordId,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}

func ProgramCollectionForTopic(ctx context.Context, id string) (string, error) {
	if !ValidID(id) {
		return "", ErrInvalid
	}
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, id, false, "topic")
	if err != nil {
		return "", err
	}
	doc, err := decode(row)
	if err != nil {
		return "", err
	}
	if doc.ViewMode == "fields" || len(doc.Fields) != 0 || !ValidID(doc.CollectionID) {
		return "", ErrInvalid
	}
	if _, _, err := readCollection(ctx, ctx, doc.CollectionID); err != nil {
		return "", err
	}
	return doc.CollectionID, nil
}

func ReadProgramCollection(ctx context.Context, id string, options ProgramReadOptions) (map[string]any, error) {
	var result map[string]any
	err := programReadTransaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, true); err != nil {
			return err
		}
		var err error
		result, err = readProgramCollection(repo.WithUnsTransaction(ctx, tx), id, options)
		return err
	})
	return result, err
}

func readProgramCollection(ctx context.Context, id string, options ProgramReadOptions) (map[string]any, error) {
	if out, routed, err := remoteStorageMap(ctx, id, "program", options); routed || err != nil {
		return out, err
	}
	if !ValidID(id) || options.RecordID != "" && !ValidID(options.RecordID) || options.Limit < 0 || options.Limit > 100 || options.Offset < 0 || options.Offset > 10000 {
		return nil, ErrInvalid
	}
	if options.Limit == 0 {
		options.Limit = 50
	}
	if _, _, err := readCollection(ctx, ctx, id); err != nil {
		return nil, err
	}
	q := repo.GetUnsConn(ctx).WithContext(ctx).Model(&repo.UnsRecord{}).Where("collection_id=? AND deleted_at IS NULL", id)
	if options.RecordID != "" {
		q = q.Where("id=?", options.RecordID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	// Check the page in PostgreSQL before loading JSON into the Backend. The
	// repeatable-read transaction keeps this budget check and fetch consistent.
	page := q.Session(&gorm.Session{}).Select("data").Order("id").Offset(options.Offset).Limit(options.Limit)
	var dataBytes int64
	if err := repo.GetUnsConn(ctx).WithContext(ctx).Table("(?) AS program_read_page", page).
		Select("COALESCE(SUM(octet_length(data::text)), 0)").Scan(&dataBytes).Error; err != nil {
		return nil, err
	}
	if dataBytes > 256*1024 {
		return nil, ErrData
	}
	rows := []repo.UnsRecord{}
	if err := q.Order("id").Offset(options.Offset).Limit(options.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return boundedProgramRead(map[string]any{"collectionId": id, "records": rows, "total": total, "limit": options.Limit, "offset": options.Offset, "hasData": len(rows) > 0})
}

func ReadProgramTopic(ctx context.Context, id string, expectedBindingHash ...string) (map[string]any, error) {
	if len(expectedBindingHash) > 1 || len(expectedBindingHash) == 1 && len(expectedBindingHash[0]) != 64 {
		return nil, ErrInvalid
	}
	var result map[string]any
	err := programReadTransaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, true); err != nil {
			return err
		}
		var err error
		result, err = readProgramTopic(repo.WithUnsTransaction(ctx, tx), id, expectedBindingHash)
		return err
	})
	return result, err
}

func programReadTransaction(ctx context.Context, fn func(*gorm.DB) error) error {
	return repo.GetUnsConn(ctx).WithContext(ctx).Transaction(fn, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
}

// WithProgramReadTransaction keeps frozen metadata and queried data consistent.
// The callback must remain read-only and use the supplied transaction context.
func WithProgramReadTransaction(ctx context.Context, fn func(context.Context) error) error {
	return programReadTransaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, true); err != nil {
			return err
		}
		return fn(repo.WithUnsTransaction(ctx, tx))
	})
}

func readProgramTopic(ctx context.Context, id string, expectedBindingHash []string) (map[string]any, error) {
	if !ValidID(id) {
		return nil, ErrInvalid
	}
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, id, false, "topic")
	if err != nil {
		return nil, err
	}
	doc, err := decode(row)
	if err != nil {
		return nil, err
	}
	if len(expectedBindingHash) == 1 {
		var binding struct {
			ViewMode     string            `json:"viewMode"`
			CollectionID string            `json:"collectionId"`
			Fields       []json.RawMessage `json:"fields"`
		}
		if json.Unmarshal(row.Data, &binding) != nil {
			return nil, ErrInvalid
		}
		sum := sha256.Sum256(mustJSON(binding))
		if hex.EncodeToString(sum[:]) != expectedBindingHash[0] {
			return nil, repo.ErrConflict
		}
	}
	if err := preflightProgramFields(ctx, doc.Fields); err != nil {
		return nil, err
	}
	view, err := New().View(ctx, id, "topic")
	if err != nil {
		return nil, err
	}
	_, ok := view["meta"].(repo.UnsMeta)
	if !ok {
		return nil, ErrInvalid
	}
	view["topicId"] = id
	if doc.CollectionID != "" {
		data, err := readProgramCollection(ctx, doc.CollectionID, ProgramReadOptions{})
		if err != nil {
			return nil, err
		}
		view["data"] = data
	}
	return boundedProgramRead(view)
}

// View resolves each field separately, including repeated references. Account
// for every full JSON body it will decode, not just the selected scalar values.
func preflightProgramFields(ctx context.Context, fields []FieldBinding) error {
	if len(fields) > 128 {
		return ErrData
	}
	var total int64
	for _, field := range fields {
		if _, _, err := readCollection(ctx, ctx, field.CollectionID); err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				continue
			}
			return err
		}
		db := repo.GetUnsConn(ctx).WithContext(ctx)
		var body *gorm.DB
		switch field.Kind {
		case "record":
			body = db.Model(&repo.UnsRecord{}).Select("data").Where("collection_id=? AND id=? AND deleted_at IS NULL", field.CollectionID, field.RecordID).Limit(1)
		case "sample":
			body = db.Model(&repo.UnsSample{}).Select("data").Where("collection_id=? AND record_id=?", field.CollectionID, field.RecordID).
				Order("time DESC, id DESC").Limit(1)
		default:
			return ErrInvalid
		}
		var size int64
		if err := db.Table("(?) AS program_field_body", body).Select("COALESCE(SUM(octet_length(data::text)), 0)").Scan(&size).Error; err != nil {
			return err
		}
		total += size
		if total > 256*1024 {
			return ErrData
		}
	}
	return nil
}

func boundedProgramRead(value map[string]any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 256*1024 {
		return nil, ErrData
	}
	return value, nil
}
