package repo

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// Zero is the public representation of "no owning Source". SQL NULL retains
// the foreign key for real Sources without creating a fake Source row.
type optionalSourceSerializer struct{}

func (optionalSourceSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, value any) error {
	if value == nil {
		value = int64(0)
	}
	return field.Set(ctx, dst, value)
}
func (optionalSourceSerializer) Value(_ context.Context, _ *schema.Field, _ reflect.Value, value any) (any, error) {
	id, ok := value.(int64)
	if !ok || id < 0 {
		return nil, ErrInvalidArgument
	}
	if id == 0 {
		return nil, nil
	}
	return id, nil
}
func init() { schema.RegisterSerializer("optional_source", optionalSourceSerializer{}) }

// This index is the union of draft and active dependencies, not SDK authority.
// Runtime authority comes only from the immutable version manifest.
type StreamSource struct {
	StreamID int64 `gorm:"column:stream_id;primaryKey" json:"streamId"`
	SourceID int64 `gorm:"column:source_id;primaryKey" json:"sourceId"`
}

func (StreamSource) TableName() string { return "public.uns_stream_source" }

type StreamSourceSnapshot struct {
	Alias          string   `json:"alias"`
	SourceID       int64    `json:"sourceId"`
	ConfigRevision int64    `json:"configRevision"`
	ReadResources  []string `json:"readResources"`
	Resources      []string `json:"resources"`
}

func (v StreamVersion) Sources() ([]StreamSourceSnapshot, error) {
	items := []StreamSourceSnapshot{}
	if v.SourceManifest == "" { // Old in-memory callers; DB defaults to [].
		return items, nil
	}
	if err := json.Unmarshal([]byte(v.SourceManifest), &items); err != nil || items == nil {
		return nil, ErrInvalidArgument
	}
	aliases, ids := map[string]bool{}, map[int64]bool{}
	for _, item := range items {
		if item.Alias == "" || item.SourceID <= 0 || item.ConfigRevision <= 0 || aliases[item.Alias] || ids[item.SourceID] {
			return nil, ErrInvalidArgument
		}
		aliases[item.Alias], ids[item.SourceID] = true, true
	}
	return items, nil
}

func programSourceIDs(document string) ([]int64, error) {
	var doc struct {
		SourceID int64 `json:"sourceId"`
		Program  *struct {
			Sources []struct {
				SourceID int64 `json:"sourceId"`
			} `json:"sources"`
		} `json:"program"`
	}
	if json.Unmarshal([]byte(document), &doc) != nil || doc.SourceID != 0 || doc.Program == nil {
		return nil, ErrInvalidArgument
	}
	ids := make([]int64, 0, len(doc.Program.Sources))
	seen := map[int64]bool{}
	for _, source := range doc.Program.Sources {
		if source.SourceID <= 0 || seen[source.SourceID] {
			return nil, ErrInvalidArgument
		}
		seen[source.SourceID] = true
		ids = append(ids, source.SourceID)
	}
	return ids, nil
}

// All mutation paths lock Sources in ID order, then Stream. Include the
// currently indexed dependencies to serialize Source deletion/config changes.
func lockProgramSources(tx *gorm.DB, streamID int64, document string) ([]Source, error) {
	ids, err := programSourceIDs(document)
	if err != nil {
		return nil, err
	}
	var previous []StreamSource
	if streamID > 0 {
		if err := tx.Where("stream_id=?", streamID).Find(&previous).Error; err != nil {
			return nil, err
		}
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for _, ref := range previous {
		seen[ref.SourceID] = true
	}
	ids = ids[:0]
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	items := make([]Source, 0, len(ids))
	for _, id := range ids {
		var item Source
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at=0", id).Take(&item).Error; err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func replaceProgramSources(tx *gorm.DB, stream Stream) error {
	documents := []string{stream.Document}
	if stream.ActiveVersion > 0 {
		var version StreamVersion
		if err := tx.Where("id=? AND stream_id=?", stream.ActiveVersion, stream.ID).Take(&version).Error; err != nil {
			return err
		}
		documents = append(documents, version.Document)
	}
	seen := map[int64]bool{}
	for _, document := range documents {
		ids, err := programSourceIDs(document)
		if err != nil {
			return err
		}
		for _, id := range ids {
			seen[id] = true
		}
	}
	if err := tx.Where("stream_id=?", stream.ID).Delete(&StreamSource{}).Error; err != nil {
		return err
	}
	for id := range seen {
		if err := tx.Create(&StreamSource{StreamID: stream.ID, SourceID: id}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *SourceStreamRepo) SaveProgram(ctx context.Context, item Stream, revision int64, topics []int64) (Stream, error) {
	if item.SourceID != 0 {
		return item, ErrInvalidArgument
	}
	creating := item.ID == 0
	if creating && revision != 0 {
		return item, ErrConflict
	}
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if _, err := lockProgramSources(tx, item.ID, item.Document); err != nil {
			return err
		}
		if creating {
			ensureID(&item.ID)
			item.Revision, item.Status = 1, "draft"
			item.CreatedAt = time.Now().UnixMilli()
			item.UpdatedAt = item.CreatedAt
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		} else {
			var old Stream
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at=0", item.ID).Take(&old).Error; err != nil {
				return err
			}
			if old.SourceID != 0 || old.Revision != revision {
				return ErrConflict
			}
			old.Name, old.Model, old.Document = item.Name, item.Model, item.Document
			old.Revision++
			old.UpdatedAt = time.Now().UnixMilli()
			if err := tx.Save(&old).Error; err != nil {
				return err
			}
			item = old
		}
		if err := replaceProgramSources(tx, item); err != nil {
			return err
		}
		return replaceStreamTopics(tx, item.ID, topics)
	})
	return item, normalizeDBError(err)
}

func checkProgramSnapshots(sources []Source, snapshots []StreamSourceSnapshot, requireHealthy bool) error {
	for _, snapshot := range snapshots {
		found := false
		for _, source := range sources {
			if source.ID != snapshot.SourceID {
				continue
			}
			found = true
			if source.DesiredState == "disabled" || source.ConfigRevision != snapshot.ConfigRevision || (requireHealthy && source.Health != "healthy") {
				return ErrConflict
			}
		}
		if !found {
			return ErrConflict
		}
	}
	return nil
}

func (r *SourceStreamRepo) ActivateProgram(ctx context.Context, id, revision, versionID int64, document string, topics []int64, snapshots []StreamSourceSnapshot) (Stream, error) {
	var item Stream
	manifest, err := json.Marshal(snapshots)
	if err != nil || snapshots == nil {
		return item, ErrInvalidArgument
	}
	// The manifest must exactly represent the explicit dependency declaration.
	var declared struct {
		Program *struct {
			Sources []StreamSourceSnapshot `json:"sources"`
		} `json:"program"`
	}
	if json.Unmarshal([]byte(document), &declared) != nil || declared.Program == nil || len(declared.Program.Sources) != len(snapshots) {
		return item, ErrInvalidArgument
	}
	for i, dependency := range declared.Program.Sources {
		frozen := snapshots[i]
		if dependency.Alias != frozen.Alias || dependency.SourceID != frozen.SourceID || !reflect.DeepEqual(dependency.ReadResources, frozen.ReadResources) {
			return item, ErrInvalidArgument
		}
	}
	err = streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		sources, err := lockProgramSources(tx, id, document)
		if err != nil {
			return err
		}
		if err := checkProgramSnapshots(sources, snapshots, true); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at=0", id).Take(&item).Error; err != nil {
			return err
		}
		if item.SourceID != 0 || item.Revision != revision {
			return ErrConflict
		}
		if versionID == 0 {
			version := StreamVersion{StreamID: id, SourceManifest: string(manifest), Document: document, CreatedAt: time.Now().UnixMilli()}
			ensureID(&version.ID)
			if _, err := version.Sources(); err != nil {
				return err
			}
			if err := tx.Create(&version).Error; err != nil {
				return err
			}
			versionID = version.ID
		} else {
			var version StreamVersion
			if err := tx.Where("id=? AND stream_id=?", versionID, id).Take(&version).Error; err != nil {
				return err
			}
			frozen, err := version.Sources()
			if err != nil {
				return err
			}
			if version.Document != document || !reflect.DeepEqual(frozen, snapshots) {
				return ErrConflict
			}
		}
		item.ActiveVersion, item.Status = versionID, "enabled"
		item.Revision++
		item.UpdatedAt = time.Now().UnixMilli()
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		if err := replaceProgramSources(tx, item); err != nil {
			return err
		}
		return replaceStreamTopics(tx, id, topics)
	})
	return item, normalizeDBError(err)
}

// Validate the persisted version, not caller-supplied grants. Status/version
// checks are repeated at task admission while dependency rows are locked.
func (r *SourceStreamRepo) ProgramVersionSources(ctx context.Context, version StreamVersion) ([]Source, error) {
	snapshots, err := version.Sources()
	if err != nil {
		return nil, err
	}
	sources := make([]Source, 0, len(snapshots))
	for _, snapshot := range snapshots {
		source, err := r.Source(ctx, snapshot.SourceID)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, checkProgramSnapshots(sources, snapshots, false)
}

// Version is immutable; Sources are locked first, then Stream. Historical RAW
// may finish its frozen version; a new synchronous request must use active.
func lockProgramExecution(tx *gorm.DB, streamID, versionID int64, activeOnly bool) (Stream, StreamVersion, error) {
	var stream Stream
	var version StreamVersion
	if err := tx.Where("id=? AND stream_id=?", versionID, streamID).Take(&version).Error; err != nil {
		return stream, version, err
	}
	sources, err := lockProgramSources(tx, streamID, version.Document)
	if err != nil {
		return stream, version, err
	}
	snapshots, err := version.Sources()
	if err != nil {
		return stream, version, err
	}
	if err := checkProgramSnapshots(sources, snapshots, false); err != nil {
		return stream, version, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at=0", streamID).Take(&stream).Error; err != nil {
		return stream, version, err
	}
	if stream.SourceID != 0 || stream.Status != "enabled" || (activeOnly && stream.ActiveVersion != versionID) {
		return stream, version, ErrConflict
	}
	return stream, version, nil
}

func (r *SourceStreamRepo) AdmitProgramRun(ctx context.Context, run StreamRun) (bool, error) {
	if run.SourceID != 0 {
		return false, ErrInvalidArgument
	}
	created := false
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		_, version, err := lockProgramExecution(tx, run.StreamID, run.VersionID, true)
		if err != nil {
			return err
		}
		var doc struct {
			Model string `json:"model"`
		}
		if json.Unmarshal([]byte(version.Document), &doc) != nil || doc.Model != "request-reply" {
			return ErrInvalidArgument
		}
		created, err = NewSourceStreamRepo(tx).ReserveRun(ctx, run)
		return err
	})
	return created, normalizeDBError(err)
}

func (r *SourceStreamRepo) PutProgramTask(ctx context.Context, item StreamTask, run StreamRun) (StreamTask, error) {
	if item.ID == "" || item.Model != "publish" || !json.Valid([]byte(item.Output)) || len(item.Output) > 1024*1024 || run.ID != item.ID || run.StreamID != item.StreamID || run.VersionID != item.VersionID || run.SourceID != item.SourceID {
		return item, ErrInvalidArgument
	}
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		_, version, err := lockProgramExecution(tx, item.StreamID, item.VersionID, false)
		if err != nil {
			return err
		}
		var doc struct {
			Model   string `json:"model"`
			Program *struct {
				Trigger struct{ Kind, SourceAlias string } `json:"trigger"`
			} `json:"program"`
		}
		if json.Unmarshal([]byte(version.Document), &doc) != nil || doc.Model != item.Model || doc.Program == nil {
			return ErrInvalidArgument
		}
		originID, originRevision := int64(0), int64(0)
		if doc.Program.Trigger.Kind == "source" {
			snapshots, err := version.Sources()
			if err != nil {
				return err
			}
			for _, snapshot := range snapshots {
				if snapshot.Alias == doc.Program.Trigger.SourceAlias {
					originID, originRevision = snapshot.SourceID, snapshot.ConfigRevision
				}
			}
			if originID == 0 {
				return ErrInvalidArgument
			}
		}
		if item.SourceID != originID || item.SourceRevision != originRevision {
			return ErrInvalidArgument
		}
		item.Status, item.CreatedAt = "pending", time.Now().UnixMilli()
		result := insertRuntimeIgnore(tx, &item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return tx.Where("id=?", item.ID).Take(&item).Error
		}
		return insertRuntimeIgnore(tx, &run).Error
	})
	return item, normalizeDBError(err)
}
