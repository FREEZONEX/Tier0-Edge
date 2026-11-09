package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 脚本 Stream 独立于 Source；持续采集 Stream 的协议配置仍由原采集仓库管理。
func scriptStreamRow(db *gorm.DB, id int64, lock string) (UnsMeta, streamConfig, error) {
	var row UnsMeta
	var config streamConfig
	q := metaQuery(db, "stream").Where("id=? AND deleted_at=0", sourceKey(id))
	if lock != "" {
		q = q.Clauses(clause.Locking{Strength: lock})
	}
	if err := q.Take(&row).Error; err != nil {
		return row, config, err
	}
	err := json.Unmarshal(row.Data, &config)
	return row, config, err
}

func scriptSourceRef(id int64) string { return fmt.Sprintf(`[{"sourceId":%d}]`, id) }
func scriptSourcePredicate(doc string) string {
	return "(" + doc + "->'sources' @> ?::jsonb OR EXISTS (SELECT 1 FROM jsonb_each(COALESCE(NULLIF(" + doc + "->'versions','null'::jsonb),'{}'::jsonb)) v WHERE v.key=" + doc + "->>'activeVersion' AND v.value->'document'->'sources' @> ?::jsonb))"
}

func currentSourcePredicate(doc string) string {
	return "((COALESCE(" + doc + "->'sources','[]'::jsonb) || COALESCE(" + doc + "->'document'->'sources','[]'::jsonb)) @> ?::jsonb OR EXISTS (SELECT 1 FROM jsonb_each(COALESCE(NULLIF(" + doc + "->'versions','null'::jsonb),'{}'::jsonb)) v WHERE (v.key=" + doc + "->>'pendingVersion' OR (" + doc + "->>'status'='enabled' AND v.key=" + doc + "->>'activeVersion')) AND (v.value->'document'->'sources' @> ?::jsonb OR v.value->'sourceManifest' @> ?::jsonb)))"
}

func (r *SourceStreamRepo) scriptDependencies(ctx context.Context, id int64) (map[string]streamConfig, error) {
	var rows []UnsMeta
	// Only drafts, active releases and pending deployments affect this mutation.
	// Retained releases remain auditable; rollback must acquire Source authority again.
	err := metaQuery(r.db.WithContext(ctx), "stream").Where("deleted_at=0").Where("("+currentSourcePredicate("data")+" OR id IN (SELECT stream_id::text FROM public.uns_stream_source WHERE source_id=?) OR EXISTS (SELECT 1 FROM public.uns_meta plan WHERE plan.kind='source_plan' AND plan.data->>'streamId'=uns_meta.id AND (plan.id=uns_meta.data->>'pendingVersion' OR (uns_meta.data->>'status'='enabled' AND plan.id=uns_meta.data->>'activeVersion')) AND plan.data->'sourceManifest' @> ?::jsonb))", scriptSourceRef(id), scriptSourceRef(id), scriptSourceRef(id), id, scriptSourceRef(id)).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := map[string]streamConfig{}
	for _, row := range rows {
		var c streamConfig
		if e := json.Unmarshal(row.Data, &c); e != nil {
			return nil, e
		}
		result[row.ID] = c
	}
	return result, err
}

// 行锁覆盖依赖校验和提交，防止保存/启用与 Source 删除或凭据更新交错。
func lockScriptSources(tx *gorm.DB, document []byte, enabled bool) error {
	var doc struct {
		Sources []struct {
			SourceID       int64 `json:"sourceId"`
			ConfigRevision int64 `json:"configRevision"`
		} `json:"sources"`
	}
	if json.Unmarshal(document, &doc) != nil {
		return ErrInvalidArgument
	}
	sort.Slice(doc.Sources, func(i, j int) bool { return doc.Sources[i].SourceID < doc.Sources[j].SourceID })
	for _, binding := range doc.Sources {
		_, source, err := sourceRow(tx, binding.SourceID, "SHARE")
		if err != nil {
			return err
		}
		if source.ConfigRevision != binding.ConfigRevision {
			return ErrConflict
		}
	}
	return nil
}

func (r *SourceStreamRepo) createScriptStream(ctx context.Context, item Stream) (Stream, error) {
	if item.SourceID != 0 {
		return item, ErrInvalidArgument
	}
	ensureID(&item.ID)
	now := time.Now().UnixMilli()
	c := streamConfig{Name: item.Name, Model: item.Model, Document: json.RawMessage(item.Document), Revision: 1, Status: "draft", CreatedAt: now, UpdatedAt: now}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockScriptSources(tx, c.Document, false); err != nil {
			return err
		}
		data, err := json.Marshal(c)
		if err != nil {
			return err
		}
		return tx.Create(&UnsMeta{Kind: "stream", ID: sourceKey(item.ID), Data: data, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error
	})
	if err != nil {
		return item, normalizeDBError(err)
	}
	return projectStream(0, sourceKey(item.ID), c)
}

func (r *SourceStreamRepo) changeScriptStream(ctx context.Context, id, revision int64, change func(Source, *streamConfig) error) (Stream, error) {
	var result Stream
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, c, err := scriptStreamRow(tx, id, "UPDATE")
		if err != nil {
			return err
		}
		if c.Revision != revision || c.Model != "script" {
			return ErrConflict
		}
		previousDocument, previousVersion, previousStatus := c.Document, c.ActiveVersion, c.Status
		if err = change(Source{}, &c); err != nil {
			return err
		}
		if c.Model != "script" {
			return ErrInvalidArgument
		}
		if c.DeletedAt == 0 {
			if !sameJSON(previousDocument, c.Document) {
				if err = lockScriptSources(tx, c.Document, false); err != nil {
					return err
				}
			}
			if c.Status == "enabled" && (previousStatus != "enabled" || previousVersion != c.ActiveVersion) {
				if err = lockScriptSources(tx, c.Versions[sourceKey(c.ActiveVersion)].Document, true); err != nil {
					return err
				}
			}
		}
		c.Revision++
		c.UpdatedAt = time.Now().UnixMilli()
		row.DeletedAt = c.DeletedAt
		if err = saveMetaData(tx, &row, c); err != nil {
			return err
		}
		result, err = projectStream(0, sourceKey(id), c)
		return err
	})
	return result, normalizeDBError(err)
}

func (r *SourceStreamRepo) streamConfiguration(ctx context.Context, stream Stream) (streamConfig, error) {
	if stream.Model == "script" {
		_, c, err := scriptStreamRow(r.db.WithContext(ctx), stream.ID, "")
		return c, err
	}
	_, data, err := sourceRow(r.db.WithContext(ctx), stream.SourceID, "")
	return data.Config.Streams[sourceKey(stream.ID)], err
}

func (r *SourceStreamRepo) AdmitScriptRun(ctx context.Context, run StreamRun) (bool, error) {
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, c, err := scriptStreamRow(tx, run.StreamID, "SHARE")
		if err != nil {
			return err
		}
		if c.Status != "enabled" || c.ActiveVersion != run.VersionID || c.Model != "script" {
			return ErrConflict
		}
		if err = lockScriptSources(tx, c.Versions[sourceKey(c.ActiveVersion)].Document, true); err != nil {
			return err
		}
		created, err = NewSourceStreamRepo(tx).ReserveRun(ctx, run)
		return err
	})
	return created, normalizeDBError(err)
}
