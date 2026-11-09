package repo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"backend/internal/unsref"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// 新版 UNS 元数据与业务数据统一使用 UNS 连接；不复用旧 Topic 物理表。
var ErrMountedReadOnly = errors.New("mounted UNS resources are read-only")

type UnsMeta struct {
	OriginNodeID string          `gorm:"column:origin_node_id" json:"originNodeId,omitempty"`
	ID           string          `gorm:"column:id;primaryKey" json:"id"`
	MountID      *string         `gorm:"column:mount_id" json:"mountId"`
	Kind         string          `gorm:"-" json:"kind"`
	StorageKind  string          `gorm:"column:kind;primaryKey" json:"-"`
	Data         json.RawMessage `gorm:"column:data;type:jsonb" json:"data"`
	Revision     int64           `gorm:"column:revision" json:"revision"`
	CreatedAt    int64           `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	UpdatedAt    int64           `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
	DeletedAt    int64           `gorm:"column:deleted_at" json:"deletedAt"`
}

func (UnsMeta) TableName() string { return "public.uns_meta" }

// Legacy folder DTOs map to directory storage; Topic has its own storage kind.
func (m *UnsMeta) BeforeCreate(*gorm.DB) error {
	m.StorageKind = m.Kind
	if m.Kind == "folder" {
		m.StorageKind = "directory"
	}
	return nil
}
func (m *UnsMeta) AfterFind(*gorm.DB) error {
	m.Kind = m.StorageKind
	if m.Kind == "directory" {
		m.Kind = "folder"
	}
	return nil
}

type UnsRecord struct {
	// Write acknowledgement only; never persisted or exposed as a stored record field.
	DeliveryState string          `gorm:"-" json:"-"`
	DeletedAt     gorm.DeletedAt  `gorm:"column:deleted_at;index" json:"-"`
	SourceTime    *time.Time      `gorm:"column:source_time" json:"sourceTime,omitempty"`
	LockTokenHash []byte          `gorm:"column:lock_token_hash" json:"-"`
	LockPrincipal string          `gorm:"column:lock_principal" json:"-"`
	LockLabel     string          `gorm:"column:lock_label" json:"-"`
	LockExpiresAt *time.Time      `gorm:"column:lock_expires_at" json:"-"`
	LockFence     int64           `gorm:"column:lock_fence" json:"-"`
	LockSince     *time.Time      `gorm:"column:lock_since" json:"-"`
	SourceID      *string         `gorm:"column:source_id" json:"sourceId,omitempty"`
	CollectionID  string          `gorm:"column:collection_id;primaryKey" json:"collectionId"`
	ID            string          `gorm:"column:id;primaryKey" json:"id"`
	Data          json.RawMessage `gorm:"column:data;type:jsonb" json:"data"`
	Revision      int64           `gorm:"column:revision" json:"revision"`
	CreatedAt     int64           `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	UpdatedAt     int64           `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

func (UnsRecord) TableName() string { return "public.uns_record" }

type UnsSample struct {
	BindingID    string          `gorm:"column:binding_id" json:"bindingId,omitempty"`
	SourceID     *string         `gorm:"column:source_id" json:"sourceId,omitempty"`
	StreamID     string          `gorm:"column:stream_id" json:"streamId,omitempty"`
	Epoch        string          `gorm:"column:epoch" json:"epoch,omitempty"`
	Seq          int64           `gorm:"column:seq" json:"seq,omitempty"`
	ProducerID   string          `gorm:"column:producer_id" json:"producerId,omitempty"`
	CollectionID string          `gorm:"column:collection_id;primaryKey" json:"collectionId"`
	RecordID     string          `gorm:"column:record_id;primaryKey" json:"recordId"`
	ID           string          `gorm:"column:id;primaryKey" json:"id"`
	Time         time.Time       `gorm:"column:time;primaryKey" json:"-"`
	ReceivedAt   time.Time       `gorm:"column:received_at" json:"-"`
	Data         json.RawMessage `gorm:"column:data;type:jsonb" json:"data"`
	Quality      string          `gorm:"column:quality" json:"quality"`
}

func (UnsSample) TableName() string { return "public.uns_sample" }

type UnsMetaRepo struct{ db *gorm.DB }

func NewUnsMetaRepo(in any) *UnsMetaRepo { return &UnsMetaRepo{db: GetUnsConn(in)} }
func (r *UnsMetaRepo) Transaction(ctx context.Context, fn func(*gorm.DB) error) error {
	err := MetadataEventTransaction(ctx, r.db, fn)
	var constraint *pgconn.PgError
	if errors.As(err, &constraint) && constraint.Code == "42501" && constraint.ConstraintName == "sync_mount_readonly" {
		return errors.Join(ErrMountedReadOnly, err)
	}
	if errors.As(err, &constraint) && constraint.Code == "23505" && strings.HasPrefix(constraint.ConstraintName, "ux_") {
		// Preserve the explicit Collection constraint identity for callers that
		// distinguish data uniqueness from object ID/name duplication.
		return errors.Join(ErrDuplicate, err)
	}
	return normalizeDBError(err)
}
func (r *UnsMetaRepo) Get(ctx context.Context, id string, deleted bool, kinds ...string) (UnsMeta, error) {
	if unsref.IsStreamRuntimeID(id) {
		return UnsMeta{}, ErrNotFound
	}
	q := r.db.WithContext(ctx).Where("id = ? AND kind IN ('directory','topic','collection')", id)
	if len(kinds) > 0 {
		kind := kinds[0]
		if kind == "folder" {
			kind = "directory"
		}
		q = r.db.WithContext(ctx).Where("id=? AND kind=?", id, kind)
	}
	if !deleted {
		q = q.Where("deleted_at = 0")
	}
	var rows []UnsMeta
	err := q.Limit(2).Find(&rows).Error
	if err != nil {
		return UnsMeta{}, err
	}
	if len(rows) == 0 {
		return UnsMeta{}, ErrNotFound
	}
	if len(rows) > 1 {
		return UnsMeta{}, ErrConflict
	}
	return rows[0], nil
}

// 使用与目录唯一索引相同的 lower 和排序规则，避免应用层大小写转换与数据库不同。
func (r *UnsMetaRepo) NamespaceNameExists(ctx context.Context, parentID, name string) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (
		SELECT 1 FROM public.uns_meta
		WHERE kind IN ('directory','topic','collection') AND deleted_at = 0 AND mount_id IS NULL AND (kind<>'collection' OR origin_node_id='')
		AND COALESCE(data->>'parentId', '') = ?
		AND lower(data->>'name') = lower(CAST(? AS text))
	)`, parentID, name).Scan(&exists).Error
	return exists, err
}

func (r *UnsMetaRepo) List(ctx context.Context, kind string, deleted bool, page, size int) ([]UnsMeta, int64, error) {
	q := r.db.WithContext(ctx).Model(&UnsMeta{}).Where("id NOT LIKE ?", unsref.StreamRuntimePrefix+"%")
	if kind == "namespace" {
		q = q.Where("kind IN ('directory','topic','collection')")
	} else if kind == "folder" || kind == "topic" {
		if kind == "folder" {
			kind = "directory"
		}
		q = q.Where("kind=?", kind)
	} else if kind != "" {
		q = q.Where("kind = ?", kind)
	} else {
		// Generic Namespace readers must never expose source credentials/tasks.
		q = q.Where("kind IN ('directory','topic','collection')")
	}
	if deleted {
		q = q.Where("deleted_at > 0")
	} else {
		q = q.Where("deleted_at = 0")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []UnsMeta{}
	err := q.Order("created_at, id").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}
func (r *UnsMetaRepo) Records(ctx context.Context, collection, search string, page, size int) ([]UnsRecord, int64, error) {
	if err := RequireLocalCollectionRead(r.db, collection); err != nil {
		return nil, 0, err
	}
	if unsref.IsStreamRuntimeID(collection) {
		return nil, 0, ErrNotFound
	}
	q := r.db.WithContext(ctx).Model(&UnsRecord{}).Where("collection_id = ? AND deleted_at IS NULL", collection)
	if search != "" {
		q = q.Where("id ILIKE ?", "%"+search+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []UnsRecord{}
	err := q.Order("id").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}
func (r *UnsMetaRepo) Record(ctx context.Context, collection, id string) (UnsRecord, error) {
	if err := RequireLocalCollectionRead(r.db, collection); err != nil {
		return UnsRecord{}, err
	}
	if unsref.IsStreamRuntimeID(collection) {
		return UnsRecord{}, ErrNotFound
	}
	var row UnsRecord
	err := r.db.WithContext(ctx).Where("collection_id = ? AND id = ? AND deleted_at IS NULL", collection, id).Take(&row).Error
	return row, err
}
func (r *UnsMetaRepo) Samples(ctx context.Context, collection, record string, start, end int64, page, size int) ([]UnsSample, int64, error) {
	if err := RequireLocalCollectionRead(r.db, collection); err != nil {
		return nil, 0, err
	}
	if unsref.IsStreamRuntimeID(collection) {
		return nil, 0, ErrNotFound
	}
	q := r.db.WithContext(ctx).Model(&UnsSample{}).Where("collection_id = ? AND record_id = ?", collection, record)
	if start > 0 {
		q = q.Where("time >= ?", time.UnixMilli(start))
	}
	if end > 0 {
		q = q.Where("time <= ?", time.UnixMilli(end))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []UnsSample{}
	err := q.Order("time DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}
func (r *UnsMetaRepo) Latest(ctx context.Context, collection, record string) (UnsSample, error) {
	if err := RequireLocalCollectionRead(r.db, collection); err != nil {
		return UnsSample{}, err
	}
	if unsref.IsStreamRuntimeID(collection) {
		return UnsSample{}, ErrNotFound
	}
	var row UnsSample
	err := r.db.WithContext(ctx).Where("collection_id = ? AND record_id = ?", collection, record).Order("time DESC, id DESC").Take(&row).Error
	return row, err
}
