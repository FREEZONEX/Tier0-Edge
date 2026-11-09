package repo

import (
	"backend/internal/domain/sourcehealth"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Source 的配置、期望状态和实际健康分别保存；修改配置不会宣称连接成功。
type Source struct {
	ConnectionCheck *sourcehealth.Result `gorm:"-" json:"connectionCheck,omitempty"`
	DataCheck       *sourcehealth.Result `gorm:"-" json:"dataCheck,omitempty"`
	ID              int64                `gorm:"column:id;primaryKey;autoIncrement:false" json:"id"`
	Name            string               `gorm:"column:name" json:"name"`
	Protocol        string               `gorm:"column:protocol" json:"protocol"`
	Document        string               `gorm:"column:document" json:"-"`
	Revision        int64                `gorm:"column:revision" json:"revision"`
	ConfigRevision  int64                `gorm:"column:config_revision" json:"configRevision"`
	DesiredState    string               `gorm:"column:desired_state" json:"desiredState"`
	Health          string               `gorm:"column:health" json:"health"`
	HealthMessage   string               `gorm:"column:health_message" json:"healthMessage"`
	Resources       string               `gorm:"column:resources" json:"-"`
	LastDataAt      int64                `gorm:"column:last_data_at" json:"lastDataAt"`
	CheckedAt       int64                `gorm:"column:checked_at" json:"checkedAt"`
	CreatedAt       int64                `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt       int64                `gorm:"column:updated_at" json:"updatedAt"`
	DeletedAt       int64                `gorm:"column:deleted_at" json:"-"`
}

func (Source) TableName() string { return "public.source_projection" }

type Stream struct {
	ID             int64  `gorm:"column:id;primaryKey;autoIncrement:false" json:"id"`
	SourceID       int64  `gorm:"column:source_id;serializer:optional_source" json:"sourceId"`
	Name           string `gorm:"column:name" json:"name"`
	Model          string `gorm:"column:model" json:"model"`
	Document       string `gorm:"column:document" json:"-"`
	DraftManifest  string `gorm:"column:draft_manifest;default:'{}'" json:"-"`
	Revision       int64  `gorm:"column:revision" json:"revision"`
	ActiveVersion  int64  `gorm:"column:active_version" json:"activeVersion"`
	PendingVersion int64  `gorm:"column:pending_version;default:0" json:"pendingVersion"`
	Status         string `gorm:"column:status" json:"status"`
	CreatedAt      int64  `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt      int64  `gorm:"column:updated_at" json:"updatedAt"`
	DeletedAt      int64  `gorm:"column:deleted_at" json:"-"`
}

func (Stream) TableName() string { return "public.program_stream_projection" }

type StreamVersion struct {
	ID              int64  `gorm:"column:id;primaryKey;autoIncrement:false" json:"id"`
	StreamID        int64  `gorm:"column:stream_id" json:"streamId"`
	SourceRevision  int64  `gorm:"column:source_revision" json:"sourceRevision"`
	SourceManifest  string `gorm:"column:source_manifest;default:'[]'" json:"-"`
	ProgramManifest string `gorm:"column:program_manifest;default:'{}'" json:"-"`
	ActivatedAt     int64  `gorm:"column:activated_at;default:0" json:"activatedAt"`
	Document        string `gorm:"column:document" json:"-"`
	CreatedAt       int64  `gorm:"column:created_at" json:"createdAt"`
}

func (StreamVersion) TableName() string { return "public.program_version_projection" }

type StreamTopic struct {
	StreamID int64 `gorm:"column:stream_id;primaryKey" json:"streamId"`
	TopicID  int64 `gorm:"column:topic_id;primaryKey" json:"topicId"`
}

func (StreamTopic) TableName() string { return "public.uns_stream_topic" }

type StreamRun struct {
	ID         string `gorm:"column:id;primaryKey" json:"id"`
	SourceID   int64  `gorm:"column:source_id;serializer:optional_source" json:"sourceId"`
	StreamID   int64  `gorm:"column:stream_id" json:"streamId"`
	VersionID  int64  `gorm:"column:version_id" json:"versionId"`
	EntryID    string `gorm:"column:entry_id;default:''" json:"entryId"`
	Model      string `gorm:"column:model" json:"model"`
	Status     string `gorm:"column:status" json:"status"`
	MessageID  string `gorm:"column:message_id" json:"messageId"`
	Summary    string `gorm:"column:summary" json:"-"`
	DurationMs int64  `gorm:"column:duration_ms" json:"durationMs"`
	CreatedAt  int64  `gorm:"column:created_at" json:"createdAt"`
}

func (StreamRun) TableName() string { return "public.uns_stream_run" }

// 密文仅供协议执行层解密使用，不能作为响应返回。
type SourceCredential struct {
	Kind       string `gorm:"-" json:"kind,omitempty"`
	Revision   int64  `gorm:"-" json:"revision,omitempty"`
	ID         string `gorm:"column:id;primaryKey" json:"id"`
	SourceID   int64  `gorm:"column:source_id" json:"sourceId"`
	Ciphertext []byte `gorm:"column:ciphertext" json:"ciphertext"`
	UpdatedAt  int64  `gorm:"column:updated_at" json:"updatedAt"`
}

func (SourceCredential) TableName() string { return "public.source_credential_projection" }

// Source and Stream above are API projections. Their only configuration store
// is uns_meta. Acquisition Streams are keys in source.data.config.streams; script Streams are independent rows.
const MaxSourceStreams = 32

type sourceConfig struct {
	Connection json.RawMessage         `json:"connection"`
	Streams    map[string]streamConfig `json:"streams"`
}
type sourceMetaData struct {
	Name           string       `json:"name"`
	Adapter        string       `json:"adapter"`
	Config         sourceConfig `json:"config"`
	ConfigRevision int64        `json:"configRevision"`
	DesiredState   string       `json:"desiredState"`
	// Encrypted credentials stay private; public projections expose only refs.
	Credentials map[string]SourceCredential `json:"credentials,omitempty"`
}
type streamConfig struct {
	Kind           string                      `json:"kind"`
	CollectionID   string                      `json:"collectionId"`
	Schema         json.RawMessage             `json:"schema"`
	Name           string                      `json:"name"`
	Model          string                      `json:"model"`
	Document       json.RawMessage             `json:"document"`
	Revision       int64                       `json:"revision"`
	ActiveVersion  int64                       `json:"activeVersion"`
	PendingVersion int64                       `json:"pendingVersion"`
	DraftManifest  json.RawMessage             `json:"draftManifest"`
	Status         string                      `json:"status"`
	CreatedAt      int64                       `json:"createdAt"`
	UpdatedAt      int64                       `json:"updatedAt"`
	DeletedAt      int64                       `json:"deletedAt,omitempty"`
	Versions       map[string]streamDeployment `json:"versions,omitempty"`
}
type streamDeployment struct {
	SourceRevision  int64           `json:"sourceRevision"`
	SourceManifest  json.RawMessage `json:"sourceManifest,omitempty"`
	ProgramManifest json.RawMessage `json:"programManifest,omitempty"`
	ActivatedAt     int64           `json:"activatedAt,omitempty"`
	Document        json.RawMessage `json:"document"`
	CreatedAt       int64           `json:"createdAt"`
}
type sourceStateData struct {
	ConnectionCheck       *sourcehealth.Result       `json:"connectionCheck,omitempty"`
	DataCheck             *sourcehealth.Result       `json:"dataCheck,omitempty"`
	HealthLeaseUntil      int64                      `json:"healthLeaseUntil,omitempty"`
	ReceiveJournal        json.RawMessage            `json:"receiveJournal,omitempty"`
	ReceiveJournalHistory json.RawMessage            `json:"receiveJournalHistory,omitempty"`
	Salesforce            json.RawMessage            `json:"salesforce,omitempty"`
	Health                string                     `json:"health"`
	HealthMessage         string                     `json:"healthMessage"`
	Resources             json.RawMessage            `json:"resources"`
	LastDataAt            int64                      `json:"lastDataAt"`
	CheckedAt             int64                      `json:"checkedAt"`
	Streams               map[string]json.RawMessage `json:"streams"`
}
type SourceStreamRepo struct{ db *gorm.DB }

func NewSourceStreamRepo(in any) *SourceStreamRepo { return &SourceStreamRepo{db: GetUnsConn(in)} }

type SourceFilter struct {
	Search, Protocol, Health, DesiredState string
	Page, Size                             int
}
type StreamFilter struct {
	Search, Model, Status string
	SourceID, TopicID     int64
	Page, Size            int
}
type StreamRunFilter struct {
	SourceID, StreamID, TopicID int64
	Model, Status               string
	Page, Size                  int
}

func sourceStreamPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	return (page - 1) * size, size
}
func sourceKey(id int64) string { return strconv.FormatInt(id, 10) }
func metaQuery(db *gorm.DB, kind string) *gorm.DB {
	return db.Model(&UnsMeta{}).Where("kind = ?", kind)
}
func sourceRow(db *gorm.DB, id int64, lock string) (UnsMeta, sourceMetaData, error) {
	var row UnsMeta
	var data sourceMetaData
	q := metaQuery(db, "source").Where("id = ? AND deleted_at = 0", sourceKey(id))
	if lock != "" {
		q = q.Clauses(clause.Locking{Strength: lock})
	}
	if err := q.Take(&row).Error; err != nil {
		return row, data, err
	}
	err := json.Unmarshal(row.Data, &data)
	return row, data, err
}
func stateRow(db *gorm.DB, id int64, lock bool) (UnsMeta, sourceStateData, error) {
	var row UnsMeta
	var data sourceStateData
	q := metaQuery(db, "source_state").Where("id = ?", sourceKey(id))
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&row).Error; err != nil {
		return row, data, err
	}
	err := json.Unmarshal(row.Data, &data)
	return row, data, err
}
func saveMetaData(db *gorm.DB, row *UnsMeta, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	row.Data = raw
	row.Revision++
	row.UpdatedAt = time.Now().UnixMilli()
	return db.Model(&UnsMeta{}).Where("kind = ? AND id = ?", row.Kind, row.ID).Updates(map[string]any{"data": raw, "revision": row.Revision, "updated_at": row.UpdatedAt, "deleted_at": row.DeletedAt}).Error
}
func projectSource(row UnsMeta, data sourceMetaData, state sourceStateData) (Source, error) {
	id, err := strconv.ParseInt(row.ID, 10, 64)
	return Source{ConnectionCheck: state.ConnectionCheck, DataCheck: state.DataCheck, ID: id, Name: data.Name, Protocol: data.Adapter, Document: string(data.Config.Connection), Revision: row.Revision, ConfigRevision: data.ConfigRevision, DesiredState: data.DesiredState, Health: state.Health, HealthMessage: state.HealthMessage, Resources: string(state.Resources), LastDataAt: state.LastDataAt, CheckedAt: state.CheckedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt}, err
}
func projectStream(sourceID int64, id string, c streamConfig) (Stream, error) {
	if c.Model == "script" || c.Model == "program" {
		sourceID = 0
	}
	n, err := strconv.ParseInt(id, 10, 64)
	return Stream{ID: n, SourceID: sourceID, Name: c.Name, Model: c.Model, Document: string(c.Document), Revision: c.Revision, ActiveVersion: c.ActiveVersion, PendingVersion: c.PendingVersion, DraftManifest: string(c.DraftManifest), Status: c.Status, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, DeletedAt: c.DeletedAt}, err
}
func (r *SourceStreamRepo) Source(ctx context.Context, id int64) (Source, error) {
	row, data, err := sourceRow(r.db.WithContext(ctx), id, "")
	if err != nil {
		return Source{}, err
	}
	_, state, err := stateRow(r.db.WithContext(ctx), id, false)
	if err != nil {
		return Source{}, err
	}
	return projectSource(row, data, state)
}
func (r *SourceStreamRepo) ListSources(ctx context.Context, f SourceFilter) ([]Source, int64, error) {
	q := metaQuery(r.db.WithContext(ctx), "source").Where("deleted_at=0")
	// This numeric-ID compatibility API projects the legacy connection format.
	// Versioned Sources contain reusable Stream bindings and string IDs; read them
	// through UNS v2 instead. Filter before count/pagination, never drop data or
	// infer the format from an ID prefix.
	q = q.Where("COALESCE(data->>'schemaVersion', '') <> '1'")
	if f.Search != "" {
		q = q.Where("data->>'name' ILIKE ?", "%"+f.Search+"%")
	}
	if f.Protocol != "" {
		q = q.Where("data->>'adapter' = ?", f.Protocol)
	}
	if f.DesiredState != "" {
		q = q.Where("COALESCE(NULLIF(data->>'desiredState', ''), 'enabled') = ?", f.DesiredState)
	}
	if f.Health != "" {
		q = q.Where("id IN (SELECT id FROM public.uns_meta WHERE kind='source_state' AND data->>'health' = ?)", f.Health)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	offset, size := sourceStreamPage(f.Page, f.Size)
	var rows []UnsMeta
	if err := q.Order("created_at DESC,id DESC").Offset(offset).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := []Source{}
	for _, row := range rows {
		var d sourceMetaData
		if err := json.Unmarshal(row.Data, &d); err != nil {
			return nil, 0, err
		}
		id, err := strconv.ParseInt(row.ID, 10, 64)
		if err != nil {
			return nil, 0, err
		}
		_, state, err := stateRow(r.db.WithContext(ctx), id, false)
		if err != nil {
			return nil, 0, err
		}
		item, err := projectSource(row, d, state)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, count, nil
}
func (r *SourceStreamRepo) CreateSource(ctx context.Context, item Source) (Source, error) {
	ensureID(&item.ID)
	now := time.Now().UnixMilli()
	d := sourceMetaData{Name: item.Name, Adapter: item.Protocol, Config: sourceConfig{Connection: json.RawMessage(item.Document), Streams: map[string]streamConfig{}}, ConfigRevision: 1, DesiredState: "enabled"}
	s := sourceStateData{Health: "unverified", Resources: json.RawMessage("[]"), Streams: map[string]json.RawMessage{}}
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		raw, err := json.Marshal(d)
		if err != nil {
			return err
		}
		if err = tx.Create(&UnsMeta{ID: sourceKey(item.ID), Kind: "source", Data: raw, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			return err
		}
		raw, err = json.Marshal(s)
		if err != nil {
			return err
		}
		return tx.Create(&UnsMeta{ID: sourceKey(item.ID), Kind: "source_state", Data: raw, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error
	})
	if err != nil {
		return item, normalizeDBError(err)
	}
	return r.Source(ctx, item.ID)
}
func (r *SourceStreamRepo) ChangeSource(ctx context.Context, id, revision int64, change func(*gorm.DB, *Source) error) (Source, error) {
	var item Source
	err := streamRuntimeTransaction(ctx, r.db, true, func(tx *gorm.DB) error {
		row, data, err := sourceRow(tx, id, "UPDATE")
		if err != nil {
			return err
		}
		if row.Revision != revision {
			return ErrConflict
		}
		var format struct {
			SchemaVersion int `json:"schemaVersion"`
		}
		if json.Unmarshal(row.Data, &format) != nil || format.SchemaVersion == 1 {
			return ErrInvalidArgument
		}
		stateMeta, state, err := stateRow(tx, id, true)
		if err != nil {
			return err
		}
		item, err = projectSource(row, data, state)
		if err != nil {
			return err
		}
		if err = change(tx, &item); err != nil {
			return err
		}
		// The callback can rotate encrypted credentials in the same source row.
		_, fresh, err := sourceRow(tx, id, "")
		if err != nil {
			return err
		}
		data.Credentials = fresh.Credentials
		interruptionReason := ""
		configChanged := data.ConfigRevision != item.ConfigRevision
		if configChanged {
			interruptionReason = "source-config-changed-before-output"
			state.ConnectionCheck, state.DataCheck = nil, nil
			state.HealthLeaseUntil = 0
			item.ConnectionCheck, item.DataCheck = nil, nil
		}
		if item.DeletedAt > 0 || item.DesiredState == "disabled" {
			interruptionReason = "source-disabled-before-output"
		}
		data.Name = item.Name
		data.Adapter = item.Protocol
		data.Config.Connection = json.RawMessage(item.Document)
		data.ConfigRevision = item.ConfigRevision
		data.DesiredState = item.DesiredState
		row.DeletedAt = item.DeletedAt
		if err = saveMetaData(tx, &row, data); err != nil {
			return err
		}
		if interruptionReason != "" {
			if err = interruptUnpreparedSourceRuns(tx, item.ID, interruptionReason); err != nil {
				return err
			}
		}
		if configChanged || state.Health != item.Health || state.HealthMessage != item.HealthMessage || string(state.Resources) != item.Resources || state.CheckedAt != item.CheckedAt {
			state.Health = item.Health
			state.HealthMessage = item.HealthMessage
			state.Resources = json.RawMessage(item.Resources)
			state.CheckedAt = item.CheckedAt
			if err = saveMetaData(tx, &stateMeta, state); err != nil {
				return err
			}
		}
		item.Revision = row.Revision
		item.UpdatedAt = row.UpdatedAt
		return nil
	})
	return item, normalizeDBError(err)
}
func (r *SourceStreamRepo) ObserveSource(ctx context.Context, id, revision int64, health, message string, resources *string) error {
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		row, _, err := sourceRow(tx, id, "SHARE")
		if err != nil {
			return err
		}
		if row.Revision != revision {
			return ErrConflict
		}
		stateRow, state, err := stateRow(tx, id, true)
		if err != nil {
			return err
		}
		state.Health = health
		state.HealthMessage = message
		state.CheckedAt = time.Now().UnixMilli()
		if resources != nil {
			state.Resources = json.RawMessage(*resources)
		}
		return saveMetaData(tx, &stateRow, state)
	})
}

// Query Stream entries in JSONB, including inactive drafts; no relation table.
func streamQuery(db *gorm.DB) *gorm.DB {
	return db.Table(`(SELECT id, data->'config'->'streams' AS streams FROM public.uns_meta WHERE kind='source' AND deleted_at=0
 UNION ALL SELECT '0' AS id, jsonb_build_object(id,data) AS streams FROM public.uns_meta WHERE kind='stream' AND deleted_at=0) AS source`).Joins("CROSS JOIN LATERAL jsonb_each(source.streams) AS stream(key,value)").Where("COALESCE((stream.value->>'deletedAt')::bigint,0)=0").Where("stream.value->>'model' IN ?", []string{"script", "program", "publish", "subscribe", "request-reply"})
}

func (r *SourceStreamRepo) streamHistoryConfiguration(ctx context.Context, id int64) (Stream, streamConfig, error) {
	q := r.db.WithContext(ctx).Table(`(SELECT id,data->'config'->'streams' AS streams FROM public.uns_meta WHERE kind='source'
 UNION ALL SELECT '0' AS id,jsonb_build_object(id,data) AS streams FROM public.uns_meta WHERE kind='stream') AS source`).Joins("CROSS JOIN LATERAL jsonb_each(source.streams) AS stream(key,value)").Where("stream.key=?", sourceKey(id))
	var row streamEntry
	if err := q.Select("source.id AS source_id,stream.key AS id,stream.value AS data").Take(&row).Error; err != nil {
		return Stream{}, streamConfig{}, err
	}
	var c streamConfig
	if err := json.Unmarshal(row.Data, &c); err != nil {
		return Stream{}, c, err
	}
	sourceID, err := strconv.ParseInt(row.SourceID, 10, 64)
	if err != nil {
		return Stream{}, c, err
	}
	stream, err := projectStream(sourceID, row.ID, c)
	return stream, c, err
}

type streamEntry struct {
	SourceID string
	ID       string
	Data     json.RawMessage
}

func readStreamEntries(q *gorm.DB) ([]Stream, error) {
	var rows []streamEntry
	if err := q.Select("source.id AS source_id,stream.key AS id,stream.value AS data").Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := []Stream{}
	for _, row := range rows {
		var c streamConfig
		if err := json.Unmarshal(row.Data, &c); err != nil {
			return nil, err
		}
		sourceID, err := strconv.ParseInt(row.SourceID, 10, 64)
		if err != nil {
			return nil, err
		}
		item, err := projectStream(sourceID, row.ID, c)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// Both saved draft and active deployment retain their Topic dependencies.
const streamTopicPredicate = `(stream.value->'topicIds' @> ?::jsonb OR EXISTS (SELECT 1 FROM jsonb_each(COALESCE(NULLIF(stream.value->'versions','null'::jsonb),'{}'::jsonb)) v WHERE v.key=stream.value->>'activeVersion' AND v.value->'document'->'topicIds' @> ?::jsonb))`

func (r *SourceStreamRepo) ListStreams(ctx context.Context, f StreamFilter) ([]Stream, int64, error) {
	q := streamQuery(r.db.WithContext(ctx))
	if f.SourceID > 0 {
		q = q.Where("(source.id=? OR "+scriptSourcePredicate("stream.value")+" OR stream.key IN (SELECT stream_id::text FROM public.uns_stream_source WHERE source_id=?))", sourceKey(f.SourceID), scriptSourceRef(f.SourceID), scriptSourceRef(f.SourceID), f.SourceID)
	}
	if f.Search != "" {
		q = q.Where("stream.value->>'name' ILIKE ?", "%"+f.Search+"%")
	}
	if f.Model != "" {
		q = q.Where("stream.value->>'model'=?", f.Model)
	}
	if f.Status != "" {
		q = q.Where("stream.value->>'status'=?", f.Status)
	}
	if f.TopicID > 0 {
		ref := fmt.Sprintf("[%d]", f.TopicID)
		q = q.Where("("+streamTopicPredicate+" OR stream.key IN (SELECT stream_id::text FROM public.uns_stream_topic WHERE topic_id=?))", ref, ref, f.TopicID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset, size := sourceStreamPage(f.Page, f.Size)
	items, err := readStreamEntries(q.Order("(stream.value->>'createdAt')::bigint DESC,stream.key DESC").Offset(offset).Limit(size))
	return items, total, err
}
func (r *SourceStreamRepo) Stream(ctx context.Context, id int64) (Stream, error) {
	items, err := readStreamEntries(streamQuery(r.db.WithContext(ctx)).Where("stream.key=?", sourceKey(id)))
	if err != nil {
		return Stream{}, err
	}
	if len(items) != 1 {
		return Stream{}, gorm.ErrRecordNotFound
	}
	return items[0], nil
}
func (r *SourceStreamRepo) SourceDependencies(ctx context.Context, id int64) ([]Stream, []StreamTopic, error) {
	_, data, err := sourceRow(r.db.WithContext(ctx), id, "")
	if err != nil {
		return nil, nil, err
	}
	standalone, err := r.scriptDependencies(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if data.Config.Streams == nil {
		data.Config.Streams = map[string]streamConfig{}
	}
	for key, c := range standalone {
		data.Config.Streams[key] = c
	}
	streams := []Stream{}
	topics := []StreamTopic{}
	for key, c := range data.Config.Streams {
		if c.DeletedAt != 0 {
			continue
		}
		item, err := projectStream(id, key, c)
		if err != nil {
			return nil, nil, err
		}
		if _, independent := standalone[key]; independent {
			item.SourceID = 0
		}
		streams = append(streams, item)
		if item.SourceID == 0 && item.Model != "script" {
			var refs []StreamTopic
			if err := r.db.WithContext(ctx).Where("stream_id=?", item.ID).Find(&refs).Error; err != nil {
				return nil, nil, err
			}
			topics = append(topics, refs...)
			continue
		}
		docs := []json.RawMessage{c.Document}
		for version, v := range c.Versions {
			if version == sourceKey(c.PendingVersion) || (c.Status == "enabled" && version == sourceKey(c.ActiveVersion)) {
				docs = append(docs, v.Document)
			}
		}
		seen := map[int64]bool{}
		for _, doc := range docs {
			var refs struct {
				TopicIDs []int64 `json:"topicIds"`
			}
			if err := json.Unmarshal(doc, &refs); err != nil {
				return nil, nil, err
			}
			for _, topic := range refs.TopicIDs {
				if !seen[topic] {
					topics = append(topics, StreamTopic{StreamID: item.ID, TopicID: topic})
					seen[topic] = true
				}
			}
		}
	}
	sort.Slice(streams, func(i, j int) bool { return streams[i].ID < streams[j].ID })
	sort.Slice(topics, func(i, j int) bool {
		if topics[i].StreamID == topics[j].StreamID {
			return topics[i].TopicID < topics[j].TopicID
		}
		return topics[i].StreamID < topics[j].StreamID
	})
	return streams, topics, nil
}
func validateStreamName(streams map[string]streamConfig, id, name string) error {
	for key, c := range streams {
		if key != id && c.DeletedAt == 0 && strings.EqualFold(c.Name, name) {
			return ErrConflict
		}
	}
	return nil
}
func (r *SourceStreamRepo) CreateStream(ctx context.Context, item Stream, topicIDs []int64) (Stream, error) {
	if item.Model == "script" {
		return r.createScriptStream(ctx, item)
	}
	ensureID(&item.ID)
	now := time.Now().UnixMilli()
	key := sourceKey(item.ID)
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		row, data, err := sourceRow(tx, item.SourceID, "UPDATE")
		if err != nil {
			return err
		}
		count := 0
		for _, c := range data.Config.Streams {
			if c.DeletedAt == 0 {
				count++
			}
		}
		if count >= MaxSourceStreams {
			return ErrInvalidArgument
		}
		if err = validateStreamName(data.Config.Streams, key, item.Name); err != nil {
			return err
		}
		if data.Config.Streams == nil {
			data.Config.Streams = map[string]streamConfig{}
		}
		c := streamConfig{Name: item.Name, Model: item.Model, Document: json.RawMessage(item.Document), Revision: 1, Status: "draft", CreatedAt: now, UpdatedAt: now}
		if err = bindStreamSchema(tx, &c); err != nil {
			return err
		}
		data.Config.Streams[key] = c
		if err = saveMetaData(tx, &row, data); err != nil {
			return err
		}
		stateMeta, state, err := stateRow(tx, item.SourceID, true)
		if err != nil {
			return err
		}
		if state.Streams == nil {
			state.Streams = map[string]json.RawMessage{}
		}
		state.Streams[key] = json.RawMessage(`{"imported":false,"ackSeq":0}`)
		if err = saveMetaData(tx, &stateMeta, state); err != nil {
			return err
		}
		item, err = projectStream(item.SourceID, key, c)
		if err != nil {
			return err
		}
		return replaceStreamTopics(tx, item.ID, topicIDs)
	})
	return item, normalizeDBError(err)
}
func (r *SourceStreamRepo) changeStream(ctx context.Context, id, revision int64, change func(Source, *streamConfig) error, topicIDs ...[]int64) (Stream, error) {
	current, err := r.Stream(ctx, id)
	if err != nil {
		return Stream{}, err
	}
	if current.Model == "script" {
		return r.changeScriptStream(ctx, id, revision, change)
	}
	var result Stream
	err = streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		row, data, err := sourceRow(tx, current.SourceID, "UPDATE")
		if err != nil {
			return err
		}
		key := sourceKey(id)
		c, ok := data.Config.Streams[key]
		if !ok || c.DeletedAt != 0 {
			return gorm.ErrRecordNotFound
		}
		if c.Revision != revision {
			return ErrConflict
		}
		_, state, err := stateRow(tx, current.SourceID, false)
		if err != nil {
			return err
		}
		source, err := projectSource(row, data, state)
		if err != nil {
			return err
		}
		if err = change(source, &c); err != nil {
			return err
		}
		if err = bindStreamSchema(tx, &c); err != nil {
			return err
		}
		if err = validateStreamName(data.Config.Streams, key, c.Name); err != nil {
			return err
		}
		c.Revision++
		c.UpdatedAt = time.Now().UnixMilli()
		data.Config.Streams[key] = c
		if err = saveMetaData(tx, &row, data); err != nil {
			return err
		}
		result, err = projectStream(current.SourceID, key, c)
		if err != nil {
			return err
		}
		if len(topicIDs) > 0 {
			return replaceStreamTopics(tx, id, topicIDs[0])
		}
		return nil
	})
	return result, normalizeDBError(err)
}
func (r *SourceStreamRepo) SaveStream(ctx context.Context, item Stream, revision int64, topicIDs []int64) (Stream, error) {
	return r.changeStream(ctx, item.ID, revision, func(source Source, c *streamConfig) error {
		if source.ID != item.SourceID || (c.Model == "script") != (item.Model == "script") {
			return ErrConflict
		}
		c.Name = item.Name
		c.Model = item.Model
		c.Document = json.RawMessage(item.Document)
		return nil
	}, topicIDs)
}
func (r *SourceStreamRepo) ActivateStream(ctx context.Context, id, revision, sourceRevision, versionID int64, document string, topicIDs []int64) (Stream, error) {
	return r.changeStream(ctx, id, revision, func(source Source, c *streamConfig) error {
		if c.Model != "script" && (source.ConfigRevision != sourceRevision || source.Health != "healthy") {
			return ErrConflict
		}
		if c.Versions == nil {
			c.Versions = map[string]streamDeployment{}
		}
		if versionID == 0 {
			ensureID(&versionID)
			c.Versions[sourceKey(versionID)] = streamDeployment{SourceRevision: source.ConfigRevision, Document: json.RawMessage(document), CreatedAt: time.Now().UnixMilli()}
		} else {
			v, ok := c.Versions[sourceKey(versionID)]
			if !ok || v.SourceRevision != source.ConfigRevision || !sameJSON(v.Document, []byte(document)) {
				return ErrConflict
			}
		}
		c.ActiveVersion = versionID
		c.Status = "enabled"
		return nil
	}, topicIDs)
}
func sameJSON(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	x, _ := json.Marshal(left)
	y, _ := json.Marshal(right)
	return string(x) == string(y)
}
func (r *SourceStreamRepo) DisableStream(ctx context.Context, id, revision int64, remove bool) (Stream, error) {
	current, err := r.Stream(ctx, id)
	if err != nil {
		return Stream{}, err
	}
	if current.SourceID == 0 && current.Model != "script" {
		return r.disableProgramV2(ctx, id, revision, remove)
	}
	return r.changeStream(ctx, id, revision, func(_ Source, c *streamConfig) error {
		c.Status = "disabled"
		c.PendingVersion = 0
		if remove {
			c.DeletedAt = time.Now().UnixMilli()
		}
		return nil
	})
}
func (r *SourceStreamRepo) Versions(ctx context.Context, id int64, page, size int) ([]StreamVersion, int64, error) {
	current, config, err := r.streamHistoryConfiguration(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	if current.SourceID == 0 && current.Model != "script" {
		var total int64
		q := r.db.WithContext(ctx).Model(&StreamVersion{}).Where("stream_id=?", id)
		if err := q.Count(&total).Error; err != nil {
			return nil, 0, err
		}
		offset, limit := sourceStreamPage(page, size)
		items := []StreamVersion{}
		err := q.Order("created_at DESC,id DESC").Offset(offset).Limit(limit).Find(&items).Error
		return items, total, err
	}
	items := []StreamVersion{}
	for key, v := range config.Versions {
		versionID, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, projectDeployment(id, versionID, v))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	total := int64(len(items))
	offset, limit := sourceStreamPage(page, size)
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], total, nil
}
func (r *SourceStreamRepo) Version(ctx context.Context, streamID, id int64) (StreamVersion, error) {
	current, config, err := r.streamHistoryConfiguration(ctx, streamID)
	if err != nil {
		return StreamVersion{}, err
	}
	if current.SourceID == 0 && current.Model != "script" {
		var v StreamVersion
		err := r.db.WithContext(ctx).Where("id=? AND stream_id=?", id, streamID).Take(&v).Error
		return v, err
	}
	v, ok := config.Versions[sourceKey(id)]
	if !ok {
		return StreamVersion{}, gorm.ErrRecordNotFound
	}
	return projectDeployment(streamID, id, v), nil
}

// ReleaseNumber 将不可变版本 ID 映射为发布序号。历史版本不物理删除，
// 因而后续发布不会改变旧版本的序号；调用方必须传入回执快照中的版本。
func (r *SourceStreamRepo) ReleaseNumber(ctx context.Context, streamID, versionID int64) (int64, error) {
	current, config, err := r.streamHistoryConfiguration(ctx, streamID)
	if err != nil {
		return 0, err
	}
	if current.SourceID == 0 && current.Model != "script" {
		if _, err := r.Version(ctx, streamID, versionID); err != nil {
			return 0, err
		}
		var release int64
		err := r.db.WithContext(ctx).Model(&StreamVersion{}).Where("stream_id=? AND id<=?", streamID, versionID).Count(&release).Error
		return release, err
	}
	if _, ok := config.Versions[sourceKey(versionID)]; !ok {
		return 0, gorm.ErrRecordNotFound
	}
	var release int64
	for key := range config.Versions {
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			return 0, err
		}
		if id <= versionID {
			release++
		}
	}
	return release, nil
}

func projectDeployment(streamID, id int64, v streamDeployment) StreamVersion {
	sources, manifest := string(v.SourceManifest), string(v.ProgramManifest)
	if sources == "" {
		sources = "[]"
	}
	if manifest == "" {
		manifest = "{}"
	}
	return StreamVersion{ID: id, StreamID: streamID, SourceRevision: v.SourceRevision, SourceManifest: sources, ProgramManifest: manifest, ActivatedAt: v.ActivatedAt, Document: string(v.Document), CreatedAt: v.CreatedAt}
}
func (r *SourceStreamRepo) Credential(ctx context.Context, sourceID int64, id string) (SourceCredential, error) {
	_, data, err := sourceRow(r.db.WithContext(ctx), sourceID, "")
	if err != nil {
		return SourceCredential{}, err
	}
	c, ok := data.Credentials[id]
	if !ok {
		return c, gorm.ErrRecordNotFound
	}
	return c, nil
}
func (r *SourceStreamRepo) SaveCredential(ctx context.Context, item SourceCredential) error {
	if item.SourceID <= 0 || item.ID == "" || len(item.ID) > 128 || len(item.Ciphertext) == 0 {
		return ErrInvalidArgument
	}
	// Keep the row lock through the JSON write even when called outside
	// ChangeSource. A nested transaction preserves the caller's outer lock.
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "source-credential:"+item.ID).Error; err != nil {
			return err
		}
		var owners int64
		if err := metaQuery(tx, "source").Where("id<>? AND jsonb_exists(data->'credentials',?)", sourceKey(item.SourceID), item.ID).Count(&owners).Error; err != nil {
			return err
		}
		if owners > 0 {
			return ErrConflict
		}
		row, data, err := sourceRow(tx, item.SourceID, "UPDATE")
		if err != nil {
			return err
		}
		if data.Credentials == nil {
			data.Credentials = map[string]SourceCredential{}
		}
		data.Credentials[item.ID] = item
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return metaQuery(tx, "source").Where("id=?", row.ID).Update("data", raw).Error
	})
}
func IsSourceStreamMissing(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
