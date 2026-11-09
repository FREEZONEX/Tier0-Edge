package unsmeta

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct{}

type recordWriterScopeKey struct{}
type collectionSnapshot struct {
	row repo.UnsMeta
	doc Document
}
type recordWriterScope struct {
	connection  gorm.ConnPool
	locked      map[repo.UnsRecordLock]bool
	collections map[string]collectionSnapshot
}

// WithPrelockedRecords is for a pure data batch after its metadata gate and
// complete Record lock set have been acquired. Metadata mutations must not use
// this scope. Its state is bound to the actual SQL transaction and never reused
// across requests; identities not in records retain the ordinary locking path.
func WithPrelockedRecords(ctx context.Context, tx *gorm.DB, records []repo.UnsRecordLock) context.Context {
	if tx == nil || tx.Statement == nil {
		return ctx
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return ctx
	}
	scope := &recordWriterScope{connection: tx.Statement.ConnPool, locked: make(map[repo.UnsRecordLock]bool, len(records)), collections: map[string]collectionSnapshot{}}
	for _, record := range records {
		scope.locked[record] = true
	}
	return context.WithValue(ctx, recordWriterScopeKey{}, scope)
}

func scopedRecordWriter(ctx context.Context, tx *gorm.DB) *recordWriterScope {
	scope, _ := ctx.Value(recordWriterScopeKey{}).(*recordWriterScope)
	if scope == nil || tx == nil || tx.Statement == nil || scope.connection != tx.Statement.ConnPool {
		return nil
	}
	return scope
}

func New() *Service { return &Service{} }

// Namespace mutation is serialized with schema changes; data writers take the
// shared lock so a validated binding/schema cannot disappear during a commit.
func lockMetadata(tx *gorm.DB, shared bool) error {
	query := "SELECT pg_advisory_xact_lock(814206108)"
	if shared {
		query = "SELECT pg_advisory_xact_lock_shared(814206108)"
	}
	return tx.Exec(query).Error
}
func pageBounds(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 500 {
		size = 500
	}
	return page, size
}
func decode(row repo.UnsMeta) (Document, error) {
	var d Document
	err := decodeNumbers(row.Data, &d)
	return d, err
}
func collection(ctx context.Context, in any, id string) (repo.UnsMeta, Document, error) {
	scope := scopedRecordWriter(ctx, repo.GetUnsConn(in))
	if scope != nil {
		if cached, ok := scope.collections[id]; ok {
			return cached.row, cached.doc, nil
		}
	}
	row, err := repo.NewUnsMetaRepo(in).Get(ctx, id, false, "collection")
	if err != nil {
		return row, Document{}, err
	}
	if row.Kind != "collection" {
		return row, Document{}, ErrInvalid
	}
	doc, err := decode(row)
	if err == nil && scope != nil {
		scope.collections[id] = collectionSnapshot{row: row, doc: doc}
	}
	return row, doc, err
}

// RecordDefinition returns the current Collection definition, reusing it only
// within an explicitly prelocked pure-data transaction.
func RecordDefinition(ctx context.Context, id string) (Document, error) {
	_, doc, err := collection(ctx, ctx, id)
	return doc, err
}

// ReadCollectionDefinition includes recycled Collections without granting write access.
func ReadCollectionDefinition(ctx context.Context, id string) (Document, error) {
	_, doc, err := readCollection(ctx, ctx, id)
	return doc, err
}
func readCollection(ctx context.Context, in any, id string) (repo.UnsMeta, Document, error) {
	row, err := repo.NewUnsMetaRepo(in).Get(ctx, id, true, "collection")
	if err != nil {
		return row, Document{}, err
	}
	doc, err := decode(row)
	if err == nil && doc.PurgeJobID != "" {
		err = ErrPurgeChanged
	}
	return row, doc, err
}

func writableCollection(ctx context.Context, in any, id string) (repo.UnsMeta, Document, error) {
	row, doc, err := collection(ctx, in, id)
	if err == nil && row.MountID != nil {
		err = repo.ErrMountedReadOnly
	}
	return row, doc, err
}
func (s *Service) List(ctx context.Context, kind string, deleted bool, page, size int) (map[string]any, error) {
	if kind != "" && kind != "namespace" && kind != "folder" && kind != "topic" && kind != "collection" {
		return nil, ErrInvalid
	}
	page, size = pageBounds(page, size)
	rows, total, err := repo.NewUnsMetaRepo(ctx).List(ctx, kind, deleted, page, size)
	return map[string]any{"list": rows, "total": total, "page": page, "size": size}, err
}
func (s *Service) Save(ctx context.Context, id, kind string, revision int64, input any) (repo.UnsMeta, error) {
	return s.save(ctx, id, kind, revision, input, "")
}

// SyncCollectionDefinition is called by authenticated Fleet transport after
// deriving the receiving identity. User metadata input cannot set provenance.
func (s *Service) SyncCollectionDefinition(ctx context.Context, id string, revision int64, input any, nodeID string) (repo.UnsMeta, error) {
	if nodeID == "" {
		return repo.UnsMeta{}, ErrInvalid
	}
	return s.save(ctx, id, "collection", revision, input, nodeID)
}
func (s *Service) save(ctx context.Context, id, kind string, revision int64, input any, originNodeID string) (repo.UnsMeta, error) {
	var doc Document
	if err := Parse(input, &doc); err != nil {
		return repo.UnsMeta{}, err
	}
	if err := doc.Validate(kind); err != nil {
		return repo.UnsMeta{}, err
	}
	doc.NodeType = ""
	if doc.DirectoryTrashID != "" || doc.PurgeJobID != "" {
		return repo.UnsMeta{}, ErrInvalid
	}
	if id == "" {
		if revision != 0 {
			return repo.UnsMeta{}, repo.ErrConflict
		}
		id = uuid.NewString()
	}
	if !ValidID(id) || revision < 0 {
		return repo.UnsMeta{}, ErrInvalid
	}
	var saved repo.UnsMeta
	err := repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, false); err != nil {
			return err
		}
		r := repo.NewUnsMetaRepo(tx)
		if kind == "folder" || kind == "topic" || kind == "collection" {
			storageKind := kind
			if kind == "folder" {
				storageKind = "directory"
			}
			var count int64
			if err := tx.Model(&repo.UnsMeta{}).Where("kind IN ('directory','topic','collection') AND kind<>? AND id=?", storageKind, id).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return repo.ErrConflict
			}
		}
		old, err := r.Get(ctx, id, true, kind)
		if err == nil {
			if old.MountID != nil {
				return repo.ErrMountedReadOnly
			}
			if originNodeID != "" && old.OriginNodeID != originNodeID {
				return repo.ErrConflict
			}
			originNodeID = old.OriginNodeID
			if old.Revision != revision || old.DeletedAt != 0 || old.Kind != kind {
				return repo.ErrConflict
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		} else if revision != 0 {
			return repo.ErrNotFound
		}
		if err := validateNamespaceName(ctx, tx, id, kind, doc, originNodeID); err != nil {
			return err
		}
		if err := validateReferences(ctx, tx, id, doc, false); err != nil {
			return err
		}
		if kind == "collection" && old.ID != "" {
			oldDoc, err := decode(old)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(oldDoc.RecordSchema, doc.RecordSchema) || !reflect.DeepEqual(oldDoc.SampleSchema, doc.SampleSchema) {
				var count int64
				if err := tx.Model(&repo.UnsRecord{}).Where("collection_id = ?", id).Count(&count).Error; err != nil {
					return err
				}
				if count > 0 && (!compatibleSchema(oldDoc.RecordSchema, doc.RecordSchema) || !compatibleSchema(oldDoc.SampleSchema, doc.SampleSchema)) {
					return ErrSchemaInUse
				}
				// A referenced field may not be removed even if there are no rows.
				var topics []repo.UnsMeta
				if err := tx.Where("kind = 'topic' AND deleted_at = 0").Find(&topics).Error; err != nil {
					return err
				}
				for _, topic := range topics {
					d, err := decode(topic)
					if err != nil {
						return err
					}
					for _, field := range d.Fields {
						if field.CollectionID == id {
							schema := doc.RecordSchema
							if field.Kind == "sample" {
								schema = doc.SampleSchema
							}
							if !schemaField(schema, field.FieldPath) {
								return ErrReferenced
							}
						}
					}
				}
			}
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		saved = repo.UnsMeta{ID: id, Kind: kind, OriginNodeID: originNodeID, Data: raw, Revision: revision + 1, CreatedAt: old.CreatedAt, UpdatedAt: now}
		if kind == "collection" {
			if err := scheduleQueryIndexes(tx, id, doc, true); err != nil {
				return err
			}
		}
		if old.ID == "" {
			saved.CreatedAt = now
			return tx.Create(&saved).Error
		}
		return tx.Model(&old).Updates(map[string]any{"data": raw, "revision": saved.Revision, "updated_at": now}).Error
	})
	return saved, err
}

// compatibleSchema 判断 after 是否接受 before 接受的所有值（加宽关系 W）。注解不参与；before 为空视为可加宽。
func compatibleSchema(before, after *Schema) bool {
	if before == nil {
		return true
	}
	if after == nil {
		return false
	}
	if len(before.Enum) > 0 {
		for _, value := range before.Enum {
			if !after.Accepts(value) {
				return false
			}
		}
		return true
	}
	if len(after.Enum) > 0 {
		return false
	}
	if before.Type != after.Type && (before.Type != "integer" || after.Type != "number") {
		return false
	}
	if !looserMinimum(before.Minimum, after.Minimum) || !looserMaximum(before.Maximum, after.Maximum) {
		return false
	}
	switch before.Type {
	case "object":
		allowed := map[string]bool{}
		for _, key := range before.Required {
			allowed[key] = true
		}
		for _, key := range after.Required {
			if !allowed[key] {
				return false
			}
		}
		for key, property := range before.Properties {
			next, ok := after.Properties[key]
			if !ok || !compatibleSchema(&property, &next) {
				return false
			}
		}
		if !(before.AdditionalProperties != nil && !*before.AdditionalProperties) {
			// 开放对象里已存在的任意值可能占用新声明的属性，只有保持开放且不新增属性才是加宽。
			if after.AdditionalProperties != nil && !*after.AdditionalProperties {
				return false
			}
			for key := range after.Properties {
				if _, ok := before.Properties[key]; !ok {
					return false
				}
			}
		}
	case "array":
		return compatibleSchema(before.Items, after.Items)
	}
	return true
}

func looserMinimum(before, after *float64) bool {
	return after == nil || before != nil && *after <= *before
}

func looserMaximum(before, after *float64) bool {
	return after == nil || before != nil && *after >= *before
}

func (s *Service) State(ctx context.Context, id string, revision int64, restore bool, kinds ...string) (repo.UnsMeta, error) {
	var saved repo.UnsMeta
	if !ValidID(id) || revision <= 0 {
		return saved, ErrInvalid
	}
	err := repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, false); err != nil {
			return err
		}
		row, err := repo.NewUnsMetaRepo(tx).Get(ctx, id, true, kinds...)
		if err != nil {
			return err
		}
		if row.Revision != revision || (restore && row.DeletedAt == 0) || (!restore && row.DeletedAt != 0) {
			return repo.ErrConflict
		}
		if row.MountID != nil {
			return repo.ErrMountedReadOnly
		}
		doc, err := decode(row)
		if err != nil {
			return err
		}
		if doc.PurgeJobID != "" {
			return ErrPurgeChanged
		}
		if restore && doc.DirectoryTrashID != "" && doc.DirectoryTrashID != id {
			return ErrTrashDirectory
		}
		if row.Kind == "folder" {
			saved, err = s.directoryState(ctx, tx, row, restore)
			return err
		}
		if restore {
			if err := validateRestoredNamespace(ctx, tx, row, doc); err != nil {
				return err
			}
		}

		row.Revision++
		row.UpdatedAt = time.Now().UnixMilli()
		row.DeletedAt = row.UpdatedAt
		if restore {
			row.DeletedAt = 0
		}
		if err := tx.Model(&row).Updates(map[string]any{"revision": row.Revision, "updated_at": row.UpdatedAt, "deleted_at": row.DeletedAt}).Error; err != nil {
			return err
		}
		saved = row
		return nil
	})
	return saved, err
}
func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }

func (s *Service) View(ctx context.Context, id string, kinds ...string) (map[string]any, error) {
	r := repo.NewUnsMetaRepo(ctx)
	row, err := r.Get(ctx, id, false, kinds...)
	if err != nil {
		return nil, err
	}
	doc, err := decode(row)
	if err != nil {
		return nil, err
	}
	fields := []map[string]any{}
	for _, f := range doc.Fields {
		item := map[string]any{"binding": f, "status": "missing", "value": nil}
		if _, _, err := readCollection(ctx, ctx, f.CollectionID); err != nil {
			if !errors.Is(err, repo.ErrNotFound) {
				return nil, err
			}
			fields = append(fields, item)
			continue
		}
		var raw json.RawMessage
		if f.Kind == "record" {
			record, err := r.Record(ctx, f.CollectionID, f.RecordID)
			if err == nil {
				raw = record.Data
				item["time"] = record.UpdatedAt
			} else if !errors.Is(err, repo.ErrNotFound) {
				return nil, err
			}
		} else {
			sample, err := r.Latest(ctx, f.CollectionID, f.RecordID)
			if err == nil {
				raw = sample.Data
				item["time"] = sample.Time.UnixMilli()
				item["quality"] = sample.Quality
				item["receivedAt"] = sample.ReceivedAt.UnixMilli()
			} else if !errors.Is(err, repo.ErrNotFound) {
				return nil, err
			}
		}
		var value any
		if len(raw) > 0 && json.Unmarshal(raw, &value) == nil {
			parts, parseErr := ParseFieldPath(f.FieldPath)
			resolved, found := fieldPathValue(value, parts)
			value = resolved
			found = found && parseErr == nil
			if found {
				item["status"] = "available"
				item["value"] = value
			}
		}
		fields = append(fields, item)
	}
	return map[string]any{"meta": row, "fields": fields}, nil
}

func recordForUpdate(tx *gorm.DB, collectionID, recordID string) (repo.UnsRecord, error) {
	var row repo.UnsRecord
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("collection_id = ? AND id = ?", collectionID, recordID).Take(&row).Error
	return row, err
}
