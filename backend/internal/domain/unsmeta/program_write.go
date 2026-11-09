package unsmeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"gorm.io/gorm"
)

// ProgramWrite targets a frozen collection, optionally through a records Topic.
// Record identity is explicit; a script cannot invent Source ownership.
type ProgramWrite struct {
	ProgramWriteOptions
	TopicID          string      `json:"topicId,omitempty"`
	CollectionID     string      `json:"collectionId"`
	Value            IngestValue `json:"value"`
	ExpectedRevision *int64      `json:"expectedRevision,omitempty"`
}

// Script revisions must round-trip through JavaScript numbers without rounding.
const MaxProgramExpectedRevision int64 = 9007199254740991

func (write *ProgramWrite) UnmarshalJSON(raw []byte) error {
	type plain ProgramWrite
	var value struct {
		plain
		ExpectedRevision json.RawMessage `json:"expectedRevision"`
	}
	if err := Parse(json.RawMessage(raw), &value); err != nil {
		return err
	}
	*write = ProgramWrite(value.plain)
	if len(value.ExpectedRevision) > 0 {
		var revision int64
		if string(value.ExpectedRevision) == "null" || json.Unmarshal(value.ExpectedRevision, &revision) != nil || revision < 0 || revision > MaxProgramExpectedRevision {
			return ErrInvalid
		}
		write.ExpectedRevision = &revision
	}
	return nil
}

// ProgramWriteOptions describes record mutation, separately from sampled data.
// An omitted mode retains the SDK's original whole-record replacement contract.
type ProgramWriteOptions struct {
	Mode  string   `json:"mode,omitempty"`
	Unset []string `json:"unset,omitempty"`
}

func (o ProgramWriteOptions) valid() bool {
	if o.Mode != "" && o.Mode != "create" && o.Mode != "upsert" && o.Mode != "update" && o.Mode != "replace" {
		return false
	}
	if len(o.Unset) > 128 || len(o.Unset) > 0 && o.Mode != "upsert" && o.Mode != "update" {
		return false
	}
	seen := map[string]bool{}
	for _, key := range o.Unset {
		if key == "" || len(key) > 128 || seen[key] || key == "__proto__" || key == "constructor" || key == "prototype" {
			return false
		}
		seen[key] = true
	}
	return true
}

type ProgramCommit struct {
	RunID      string                        `json:"runId"`
	StreamID   int64                         `json:"streamId"`
	VersionID  int64                         `json:"versionId"`
	EntryID    string                        `json:"entryId"`
	ReceivedAt int64                         `json:"receivedAt"`
	Writes     []ProgramWrite                `json:"writes"`
	Checkpoint *repo.ProgramCheckpointCommit `json:"checkpoint,omitempty"`
	Lineage    []int64                       `json:"lineage,omitempty"`
}

func programWriteDocument(ctx context.Context, in any, write ProgramWrite) (Document, error) {
	if !ValidID(write.CollectionID) || write.TopicID != "" && !ValidID(write.TopicID) || !ValidID(write.Value.RecordID) {
		return Document{}, ErrInvalid
	}
	if write.TopicID != "" {
		row, err := repo.NewUnsMetaRepo(in).Get(ctx, write.TopicID, false, "topic")
		if err != nil {
			return Document{}, err
		}
		doc, err := decode(row)
		if err != nil {
			return Document{}, err
		}
		if doc.ViewMode == "fields" || len(doc.Fields) != 0 || doc.CollectionID != write.CollectionID {
			return Document{}, repo.ErrConflict
		}
	}
	_, doc, err := writableCollection(ctx, in, write.CollectionID)
	return doc, err
}

func normalizeProgramWrite(write ProgramWrite, receivedAt int64) (ProgramWrite, error) {
	if write.ExpectedRevision != nil && (*write.ExpectedRevision < 0 || *write.ExpectedRevision > MaxProgramExpectedRevision) {
		return write, ErrInvalid
	}
	raw, err := json.Marshal(write)
	var normalized ProgramWrite
	if err != nil || len(raw) > 256*1024 || decodeNumbers(raw, &normalized) != nil {
		return write, ErrData
	}
	write = normalized
	if write.Value.Time == 0 {
		write.Value.Time = receivedAt
	}
	if write.Value.Time <= 0 || write.Value.Time > 253402300799999 || len(write.Value.Quality) > 64 || write.Value.Record == nil || !write.ProgramWriteOptions.valid() {
		return write, ErrData
	}
	for _, key := range write.Unset {
		if _, exists := write.Value.Record[key]; exists {
			return write, ErrData
		}
	}
	return write, nil
}

func validateProgramWrite(ctx context.Context, in any, write ProgramWrite) error {
	doc, err := programWriteDocument(ctx, in, write)
	if err != nil {
		return err
	}
	if doc.RecordSchema == nil {
		return ErrData
	}
	schema := *doc.RecordSchema
	if write.Mode == "upsert" || write.Mode == "update" {
		// Required fields apply to the merged record, not the patch.
		schema.Required = nil
	}
	if !schema.accepts(write.Value.Record) || write.Value.Sample != nil && (doc.SampleSchema == nil || !doc.SampleSchema.accepts(write.Value.Sample)) {
		return ErrData
	}
	return nil
}

func PreviewProgramWrite(ctx context.Context, write ProgramWrite, receivedAt int64) (ProgramWrite, error) {
	write, err := normalizeProgramWrite(write, receivedAt)
	if err == nil {
		err = validateProgramWrite(ctx, ctx, write)
	}
	return write, err
}

type ProgramFieldChange struct {
	Field        string `json:"field"`
	BeforeExists bool   `json:"beforeExists"`
	AfterExists  bool   `json:"afterExists"`
	Before       any    `json:"before"`
	After        any    `json:"after"`
}

type ProgramWritePreview struct {
	RecordID string               `json:"recordId"`
	Mode     string               `json:"mode"`
	Status   string               `json:"status"`
	Revision int64                `json:"revision"`
	Error    string               `json:"error,omitempty"`
	Changes  []ProgramFieldChange `json:"changes"`
}

// PreviewProgramWrites requires the caller to authorize record reads. It never
// reserves a commit, emits events or persists samples. Repeated identities see
// preceding simulated writes in one repeatable-read database snapshot.
func PreviewProgramWrites(ctx context.Context, writes []ProgramWrite, receivedAt int64) ([]ProgramWritePreview, error) {
	if len(writes) > 1000 || len(mustJSON(writes)) > 256*1024 {
		return nil, ErrData
	}
	previews := make([]ProgramWritePreview, 0, len(writes))
	err := WithProgramReadTransaction(ctx, func(readCtx context.Context) error {
		rows := map[string]repo.UnsRecord{}
		bytesRead, resultBytes := 0, 0
		for _, input := range writes {
			write, err := normalizeProgramWrite(input, receivedAt)
			if err != nil {
				return err
			}
			if err := repo.RequireLocalCollectionRead(readCtx, write.CollectionID); err != nil {
				return err
			}
			key := write.CollectionID + "/" + write.Value.RecordID
			old, loaded := rows[key]
			if !loaded {
				// Bound each JSON value inside PostgreSQL before loading it.
				err := repo.GetUnsConn(readCtx).WithContext(readCtx).Model(&repo.UnsRecord{}).
					Select("id, collection_id, source_id, revision, CASE WHEN octet_length(data::text) <= 262144 THEN data ELSE NULL END AS data").
					Where("collection_id=? AND id=?", write.CollectionID, write.Value.RecordID).Take(&old).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				bytesRead += len(old.Data)
				if old.ID != "" && len(old.Data) == 0 || bytesRead > 256*1024 {
					return ErrData
				}
				rows[key] = old
			}
			preview := ProgramWritePreview{RecordID: write.Value.RecordID, Mode: write.Mode, Revision: old.Revision, Changes: []ProgramFieldChange{}}
			if preview.Mode == "" {
				preview.Mode = "replace"
			}
			var after map[string]any
			err = validateProgramWrite(readCtx, readCtx, write)
			if err == nil {
				after, err = resolveProgramRecord(write, old)
			}
			if err == nil {
				doc, docErr := programWriteDocument(readCtx, readCtx, write)
				err = docErr
				if err == nil && (!doc.RecordSchema.accepts(after) || len(mustJSON(after)) > 256*1024) {
					err = ErrData
				}
			}
			if err != nil {
				preview.Status, preview.Error = "error", "invalidData"
				switch {
				case errors.Is(err, ErrReferenced):
					preview.Error = "managedRecord"
				case errors.Is(err, repo.ErrNotFound):
					preview.Error = "notFound"
				case errors.Is(err, repo.ErrConflict):
					preview.Error = "conflict"
				}
			} else {
				before := map[string]any{}
				if old.ID != "" && decodeNumbers(old.Data, &before) != nil {
					return ErrData
				}
				keys := map[string]bool{}
				for key := range before {
					keys[key] = true
				}
				for key := range after {
					keys[key] = true
				}
				ordered := make([]string, 0, len(keys))
				for key := range keys {
					ordered = append(ordered, key)
				}
				sort.Strings(ordered)
				for _, field := range ordered {
					beforeValue, beforeExists := before[field]
					afterValue, afterExists := after[field]
					if beforeExists != afterExists || !equalJSONValues(beforeValue, afterValue) {
						preview.Changes = append(preview.Changes, ProgramFieldChange{field, beforeExists, afterExists, beforeValue, afterValue})
					}
				}
				preview.Status = "unchanged"
				if old.ID == "" {
					preview.Status, old.Revision = "created", 1
				} else if len(preview.Changes) > 0 {
					preview.Status, old.Revision = "updated", old.Revision+1
				}
				old.ID, old.CollectionID, old.Data = write.Value.RecordID, write.CollectionID, mustJSON(after)
				rows[key] = old
			}
			resultBytes += len(mustJSON(preview))
			if resultBytes > 256*1024 {
				return ErrData
			}
			previews = append(previews, preview)
		}
		return nil
	})
	return previews, err
}

// CommitProgram atomically accepts a frozen output batch and its cursor on this
// node. Storage policy routes each output to local business rows, a durable
// upload event, or both in this transaction. Center delivery is asynchronous;
// multiple outputs do not require a transaction spanning the Edge and center.
// Replay returns false before resolving today's bindings, schemas or Sources.
func CommitProgram(ctx context.Context, batch ProgramCommit) (bool, error) {
	if batch.RunID == "" || len(batch.RunID) > 64 || batch.StreamID <= 0 || batch.VersionID <= 0 || batch.EntryID == "" || len(batch.EntryID) > 128 || batch.ReceivedAt <= 0 || batch.ReceivedAt > 253402300799999 || len(batch.Writes) > 1000 || len(batch.Lineage) > 32 {
		return false, ErrInvalid
	}
	raw, err := json.Marshal(batch)
	var frozen ProgramCommit
	if err != nil || len(raw) > streamexec.MaxProgramDataBytes || decodeNumbers(raw, &frozen) != nil {
		return false, ErrData
	}
	batch = frozen
	for i := range batch.Writes {
		batch.Writes[i], err = normalizeProgramWrite(batch.Writes[i], batch.ReceivedAt)
		if err != nil {
			return false, err
		}
	}
	if batch.Checkpoint != nil {
		if batch.Checkpoint.StreamID != batch.StreamID || batch.Checkpoint.EntryID != batch.EntryID || batch.Checkpoint.RunID != batch.RunID {
			return false, ErrInvalid
		}
		batch.Checkpoint.Digest = ""
	}
	digestBytes := sha256.Sum256(mustJSON(batch))
	digest := hex.EncodeToString(digestBytes[:])
	if committed, err := repo.ProgramMetadataCommitted(ctx, batch.RunID, digest); err != nil || committed {
		return false, err
	}
	if batch.Checkpoint != nil {
		batch.Checkpoint.Digest = digest
	}
	committed := false
	err = repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, true); err != nil {
			return err
		}
		var sampleTimes []time.Time
		for _, write := range batch.Writes {
			if write.Value.Sample != nil {
				sampleTimes = append(sampleTimes, time.UnixMilli(write.Value.Time))
			}
		}
		if err := repo.PrepareUnsSampleWrites(tx, sampleTimes...); err != nil {
			return err
		}
		created, err := repo.ReserveProgramMetadataCommit(tx, batch.RunID, digest)
		if err != nil || !created {
			return err
		}
		var finish func() error
		if batch.Checkpoint != nil {
			newCursor, callback, err := repo.LockProgramCheckpointGORM(ctx, tx, *batch.Checkpoint)
			if err != nil {
				return err
			}
			if !newCursor {
				return repo.ErrConflict
			}
			finish = callback
		}
		locks := make([]repo.UnsRecordLock, 0, len(batch.Writes))
		for _, write := range batch.Writes {
			if err := validateProgramWrite(ctx, tx, write); err != nil {
				return err
			}
			locks = append(locks, repo.UnsRecordLock{CollectionID: write.CollectionID, RecordID: write.Value.RecordID})
		}
		if err := repo.LockUnsRecords(tx, locks...); err != nil {
			return err
		}
		for ordinal, write := range batch.Writes {
			if err := commitProgramWrite(ctx, tx, batch, ordinal, write); err != nil {
				return err
			}
		}
		if finish != nil {
			if err := finish(); err != nil {
				return err
			}
		}
		committed = true
		return nil
	})
	return committed && err == nil, err
}

func commitProgramWrite(ctx context.Context, tx *gorm.DB, batch ProgramCommit, ordinal int, write ProgramWrite) error {
	policy, err := repo.CollectionStorage(tx, write.CollectionID)
	if err != nil {
		return err
	}
	if policy.Mode != "local" {
		if write.ExpectedRevision != nil || len(write.Unset) > 0 || (write.Mode != "" && write.Mode != "replace" && write.Mode != "create") {
			return repo.ErrStorageOperation
		}
		kind := "upsert"
		if write.Mode == "create" {
			kind = "create"
		}
		only, err := RouteCollectionWrite(ctx, tx, write.CollectionID, programOutputID(batch.RunID, ordinal, write.CollectionID, write.Value.RecordID), kind, write.Value)
		if err != nil {
			return err
		}
		if only {
			return nil
		}
	}
	old, err := recordForWrite(tx, write.CollectionID, write.Value.RecordID)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return err
	}
	after, err := resolveProgramRecord(write, old)
	if err != nil {
		return err
	}
	doc, err := programWriteDocument(ctx, tx, write)
	if err != nil {
		return err
	}
	if doc.RecordSchema == nil || !doc.RecordSchema.accepts(after) || len(mustJSON(after)) > 256*1024 {
		return ErrData
	}
	write.Value.Record = after
	now := time.Now().UnixMilli()
	if old.ID == "" {
		row := repo.UnsRecord{CollectionID: write.CollectionID, ID: write.Value.RecordID, Data: mustJSON(write.Value.Record), Revision: 1, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	} else {
		var previous any
		if err := decodeNumbers(old.Data, &previous); err != nil {
			return err
		}
		if old.DeletedAt.Valid || !equalJSONValues(previous, write.Value.Record) {
			if err := tx.Unscoped().Model(&old).Updates(recordWriteColumns(old, mustJSON(write.Value.Record), now)).Error; err != nil {
				return err
			}
		}
	}
	id := programOutputID(batch.RunID, ordinal, write.CollectionID, write.Value.RecordID)
	change := "record"
	if write.Value.Sample != nil {
		change = "record-sample"
		row := repo.UnsSample{CollectionID: write.CollectionID, RecordID: write.Value.RecordID, ID: id,
			StreamID: strconv.FormatInt(batch.StreamID, 10), Epoch: strconv.FormatInt(batch.VersionID, 10), ProducerID: "stream:" + strconv.FormatInt(batch.StreamID, 10) + ":" + batch.EntryID,
			Time: time.UnixMilli(write.Value.Time), ReceivedAt: time.UnixMilli(batch.ReceivedAt), Data: mustJSON(write.Value.Sample), Quality: write.Value.Quality}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	}
	return repo.EnqueueMetadataResourceEvents(ctx, tx, repo.MetadataResourceEvent{MessageID: id, CollectionID: write.CollectionID, RecordID: write.Value.RecordID,
		StreamID: batch.StreamID, VersionID: batch.VersionID, EntryID: batch.EntryID, ReceivedAt: batch.ReceivedAt, Data: write.Value, Change: change, Lineage: batch.Lineage})
}

// resolveProgramRecord is shared by preview and the locked commit path. Top-level
// sets preserve other fields; nested objects are explicit whole-field values.
func resolveProgramRecord(write ProgramWrite, old repo.UnsRecord) (map[string]any, error) {
	if old.SourceID != nil {
		return nil, ErrReferenced
	}
	if old.DeletedAt.Valid {
		old = repo.UnsRecord{}
	}
	if write.ExpectedRevision != nil && ((*write.ExpectedRevision == 0 && old.ID != "") || (*write.ExpectedRevision > 0 && (old.ID == "" || old.Revision != *write.ExpectedRevision))) {
		return nil, repo.ErrConflict
	}
	if write.Mode == "create" && old.ID != "" {
		return nil, repo.ErrConflict
	}
	if write.Mode == "update" && old.ID == "" {
		return nil, repo.ErrNotFound
	}
	after := map[string]any{}
	if old.ID != "" && (write.Mode == "upsert" || write.Mode == "update") {
		if err := decodeNumbers(old.Data, &after); err != nil {
			return nil, err
		}
	}
	for key, value := range write.Value.Record {
		after[key] = value
	}
	for _, key := range write.Unset {
		delete(after, key)
	}
	return after, nil
}

func programOutputID(runID string, ordinal int, collectionID, recordID string) string {
	sum := sha256.Sum256(mustJSON([]any{runID, ordinal, collectionID, recordID}))
	return "program-output-" + hex.EncodeToString(sum[:])
}
