package unsmeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"backend/internal/fieldpath"
	"backend/internal/repo"
	"gorm.io/gorm"
)

// Mapping 路径相对于 Stream 转换后的 data，空路径表示整个对象。
// CollectionID 固定在版本中，禁止 Topic 重新绑定后把待处理任务写入另一个集合。
type Mapping struct {
	TopicID       string `json:"topicId,omitempty"`
	CollectionID  string `json:"collectionId,omitempty"`
	RecordID      string `json:"recordId,omitempty"`
	RecordIDPath  string `json:"recordIdPath,omitempty"`
	RecordPath    string `json:"recordPath,omitempty"`
	SampleEnabled bool   `json:"sampleEnabled,omitempty"`
	SamplePath    string `json:"samplePath,omitempty"`
	TimePath      string `json:"timePath,omitempty"`
	QualityPath   string `json:"qualityPath,omitempty"`
}

func (m Mapping) Validate() error {
	if (m.TopicID != "" && !ValidID(m.TopicID)) || !ValidID(m.CollectionID) || (m.RecordID != "" && !ValidID(m.RecordID)) || (m.RecordID != "" && m.RecordIDPath != "") {
		return ErrInvalid
	}
	for _, path := range []string{m.RecordIDPath, m.RecordPath, m.SamplePath, m.TimePath, m.QualityPath} {
		if len(path) > 512 {
			return ErrInvalid
		}
		if path != "" {
			if _, err := ParseFieldPath(path); err != nil {
				return ErrInvalid
			}
		}
	}
	if m.RecordID == "" && m.RecordIDPath == "" {
		return ErrInvalid
	}
	return nil
}

func mappingDocument(ctx context.Context, in any, m Mapping) (Document, error) {
	if err := m.Validate(); err != nil {
		return Document{}, err
	}
	if m.TopicID != "" {
		topic, err := repo.NewUnsMetaRepo(in).Get(ctx, m.TopicID, false, "topic")
		if err != nil {
			return Document{}, err
		}
		doc, err := decode(topic)
		if err != nil {
			return Document{}, err
		}
		if topic.Kind != "topic" || doc.CollectionID != m.CollectionID {
			return Document{}, repo.ErrConflict
		}
	}
	_, collectionDoc, err := writableCollection(ctx, in, m.CollectionID)
	if err == nil && m.SampleEnabled && collectionDoc.SampleSchema == nil {
		err = ErrSchema
	}
	return collectionDoc, err
}

func ValidateMapping(ctx context.Context, m Mapping) error {
	_, err := mappingDocument(ctx, ctx, m)
	return err
}

func mappingValue(data any, path string) any {
	if path == "" {
		return data
	}
	parts, err := ParseFieldPath(path)
	if err != nil {
		return nil
	}
	value, _ := fieldpath.Value(data, parts)
	return value
}

type IngestValue struct {
	RecordID string         `json:"recordId"`
	Record   map[string]any `json:"record"`
	Sample   map[string]any `json:"sample,omitempty"`
	Time     int64          `json:"time"`
	Quality  string         `json:"quality,omitempty"`
}

type sharedOrigin struct {
	SourceID, StreamID, BindingID, Release string
	Sequence                               int64
}
type sharedOriginKey struct{}

// The caller holds a runtime lease and commits its checkpoint in the same
// transaction. A per-row meta receipt would be redundant and unbounded here.
func CommitSharedMapped(ctx context.Context, sourceID, streamID, bindingID, release, eventID string, sequence int64, m Mapping, input any, receivedAt int64) error {
	return CommitMapped(context.WithValue(ctx, sharedOriginKey{}, sharedOrigin{sourceID, streamID, bindingID, release, sequence}), eventID, m, input, receivedAt)
}

// PrepareMappedSamples runs before any page output mutates referenced rows.
// Later nested per-item commits must not discover a new chunk after prior items
// have already written Meta/Record in their shared outer transaction.
func PrepareMappedSamples(ctx context.Context, tx *gorm.DB, m Mapping, inputs []any, receivedAt int64) error {
	if !m.SampleEnabled || len(inputs) == 0 {
		return nil
	}
	doc, err := mappingDocument(ctx, tx, m)
	if err != nil {
		return err
	}
	times := make([]time.Time, 0, len(inputs))
	for _, input := range inputs {
		raw, err := json.Marshal(input)
		var data any
		if err != nil || len(raw) > 256*1024 || decodeNumbers(raw, &data) != nil {
			return ErrData
		}
		value, err := mappedValue(doc, m, data, receivedAt)
		if err != nil {
			return err
		}
		times = append(times, time.UnixMilli(value.Time))
	}
	return repo.PrepareUnsSampleWrites(tx, times...)
}

func mappedValue(doc Document, m Mapping, data any, receivedAt int64) (IngestValue, error) {
	value := IngestValue{RecordID: m.RecordID, Time: receivedAt}
	if value.RecordID == "" {
		switch id := mappingValue(data, m.RecordIDPath).(type) {
		case string:
			value.RecordID = id
		case float64, json.Number:
			if number, ok := exactNumber(id); ok && number.IsInt() && number.Num().IsInt64() && number.Sign() >= 0 && number.Num().Int64() <= 9007199254740991 {
				value.RecordID = number.Num().String()
			}
		}
	}
	value.Record, _ = mappingValue(data, m.RecordPath).(map[string]any)
	if !ValidID(value.RecordID) || value.Record == nil || doc.RecordSchema == nil || !doc.RecordSchema.accepts(value.Record) {
		return value, ErrData
	}
	if m.SampleEnabled {
		value.Sample, _ = mappingValue(data, m.SamplePath).(map[string]any)
		if doc.SampleSchema == nil || value.Sample == nil || !doc.SampleSchema.accepts(value.Sample) {
			return value, ErrData
		}
		if m.TimePath != "" {
			switch stamp := mappingValue(data, m.TimePath).(type) {
			case float64, json.Number:
				number, ok := exactNumber(stamp)
				if !ok || !number.IsInt() || !number.Num().IsInt64() || number.Sign() <= 0 || number.Num().Int64() > 253402300799999 {
					return value, ErrData
				}
				value.Time = number.Num().Int64()
			case string:
				parsed, err := time.Parse(time.RFC3339Nano, stamp)
				if err != nil {
					return value, ErrData
				}
				value.Time = parsed.UnixMilli()
			default:
				return value, ErrData
			}
		}
		if m.QualityPath != "" {
			var ok bool
			value.Quality, ok = mappingValue(data, m.QualityPath).(string)
			if !ok || len(value.Quality) > 64 {
				return value, ErrData
			}
		}
	}
	if value.Time <= 0 || value.Time > 253402300799999 {
		return value, ErrData
	}
	return value, nil
}

func PreviewMapping(ctx context.Context, m Mapping, input any, receivedAt int64) (IngestValue, error) {
	doc, err := mappingDocument(ctx, ctx, m)
	if err != nil {
		return IngestValue{}, err
	}
	return mappedValue(doc, m, input, receivedAt)
}

// Source state, replay receipt and business rows commit in one UNS transaction.
func CommitMapped(ctx context.Context, eventID string, m Mapping, input any, receivedAt int64, origin ...repo.StreamTask) error {
	if eventID == "" || len(eventID) > 128 {
		return ErrInvalid
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 256*1024 {
		return ErrData
	}
	var data any
	if decodeNumbers(raw, &data) != nil {
		return ErrData
	}
	digestBytes := sha256.Sum256(mustJSON([]any{m, data, receivedAt}))
	digest := hex.EncodeToString(digestBytes[:])
	return repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, true); err != nil {
			return err
		}
		var sourceID *string
		shared, isShared := ctx.Value(sharedOriginKey{}).(sharedOrigin)
		if isShared {
			sourceID = &shared.SourceID
		}
		var seq int64
		var checkpoint func() error
		if len(origin) > 0 {
			id := strconv.FormatInt(origin[0].SourceID, 10)
			sourceID = &id
			var err error
			seq, checkpoint, err = repo.LockStreamIngest(tx, origin[0])
			if err != nil {
				return err
			}
		}
		doc, err := mappingDocument(ctx, tx, m)
		if err != nil {
			return err
		}
		value, err := mappedValue(doc, m, data, receivedAt)
		if err != nil {
			return err
		}
		if value.Sample != nil {
			if err := repo.PrepareUnsSampleWrites(tx, time.UnixMilli(value.Time)); err != nil {
				return err
			}
		}
		if !isShared {
			created, err := repo.ReserveMetadataIngest(tx, eventID, digest)
			if err != nil {
				return err
			}
			if !created {
				return nil
			}
		}
		// 同一 Record 的首次创建也要串行，不能只锁不存在的行。
		if err := repo.LockUnsRecords(tx, repo.UnsRecordLock{CollectionID: m.CollectionID, RecordID: value.RecordID}); err != nil {
			return err
		}
		only, err := RouteCollectionWrite(ctx, tx, m.CollectionID, eventID, "upsert", value)
		if err != nil {
			return err
		}
		if only {
			if checkpoint != nil {
				return checkpoint()
			}
			return nil
		}
		old, err := recordForWrite(tx, m.CollectionID, value.RecordID)
		if err != nil && !errors.Is(err, repo.ErrNotFound) {
			return err
		}
		now := time.Now().UnixMilli()
		if old.ID == "" {
			row := repo.UnsRecord{SourceID: sourceID, CollectionID: m.CollectionID, ID: value.RecordID, Data: mustJSON(value.Record), Revision: 1, CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else {
			if sourceID == nil && old.SourceID != nil {
				return ErrReferenced
			}
			if sourceID != nil && (old.SourceID == nil || *old.SourceID != *sourceID) {
				return repo.ErrConflict
			}
			var previous any
			if err := decodeNumbers(old.Data, &previous); err != nil {
				return err
			}
			if old.DeletedAt.Valid || !equalJSONValues(previous, value.Record) {
				if err := tx.Unscoped().Model(&old).Updates(recordWriteColumns(old, mustJSON(value.Record), now)).Error; err != nil {
					return err
				}
			}
		}
		if m.SampleEnabled {
			row := repo.UnsSample{CollectionID: m.CollectionID, RecordID: value.RecordID, ID: eventID, Time: time.UnixMilli(value.Time), ReceivedAt: time.UnixMilli(receivedAt), Data: mustJSON(value.Sample), Quality: value.Quality}
			if isShared {
				row.SourceID = sourceID
				row.StreamID = shared.StreamID
				row.BindingID = shared.BindingID
				row.BindingID = shared.BindingID
				row.Epoch = "shared:" + shared.BindingID + ":" + shared.Release
				row.Seq = shared.Sequence
				row.ProducerID = shared.SourceID + ":" + shared.BindingID
			} else if sourceID != nil {
				row.SourceID = sourceID
				row.StreamID = strconv.FormatInt(origin[0].StreamID, 10)
				row.Epoch = strconv.FormatInt(origin[0].VersionID, 10)
				row.Seq = seq
				row.ProducerID = *sourceID + ":" + row.StreamID
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		event := repo.MetadataResourceEvent{MessageID: eventID, CollectionID: m.CollectionID, RecordID: value.RecordID,
			ReceivedAt: receivedAt, Data: value, Change: "record"}
		if m.SampleEnabled {
			event.Change = "record-sample"
		}
		if len(origin) > 0 {
			event.SourceID, event.StreamID, event.VersionID = origin[0].SourceID, origin[0].StreamID, origin[0].VersionID
			event.Lineage = []int64{origin[0].StreamID}
		}
		if err := repo.EnqueueMetadataResourceEvents(ctx, tx, event); err != nil {
			return err
		}
		if checkpoint != nil {
			return checkpoint()
		}
		return nil
	})
}
