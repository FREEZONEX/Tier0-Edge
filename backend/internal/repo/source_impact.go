package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// Includes retained releases even after a Stream stops referring to the Source.
// This is metadata-only; no code, endpoints, data or credentials are projected.
func (r *SourceStreamRepo) SourceImpactReferences(ctx context.Context, id int64) ([]map[string]any, error) {
	source, err := r.Source(ctx, id)
	if err != nil {
		return nil, err
	}
	streams, _, err := r.SourceDependencies(ctx, id)
	if err != nil {
		return nil, err
	}
	references := []map[string]any{}
	for _, stream := range streams {
		references = append(references, map[string]any{"kind": "draft", "streamID": strconv.FormatInt(stream.ID, 10), "streamRevision": stream.Revision, "release": int64(0), "alias": "", "configRevision": source.ConfigRevision, "streamDeleted": 2, "parentMissing": 2})
	}
	var versions []StreamVersion
	if err = r.db.WithContext(ctx).Where("source_manifest::jsonb @> ?::jsonb", fmt.Sprintf(`[{"sourceId":%d}]`, id)).Order("stream_id,id").Find(&versions).Error; err != nil {
		return nil, err
	}
	for _, version := range versions {
		snapshots, err := version.Sources()
		if err != nil {
			return nil, err
		}
		var parent Stream
		result := r.db.WithContext(ctx).Where("id=?", version.StreamID).Find(&parent)
		if result.Error != nil {
			return nil, result.Error
		}
		missing, deleted := 2, 2
		if result.RowsAffected == 0 {
			missing = 1
		}
		if parent.DeletedAt > 0 {
			deleted = 1
		}
		kind := "historical_release"
		if parent.DeletedAt == 0 && (parent.ActiveVersion == version.ID || parent.PendingVersion == version.ID) {
			kind = "active_release"
		}
		var release int64
		if err = r.db.WithContext(ctx).Model(&StreamVersion{}).Where("stream_id=? AND id<=?", version.StreamID, version.ID).Count(&release).Error; err != nil {
			return nil, err
		}
		for _, snapshot := range snapshots {
			if snapshot.SourceID != id {
				continue
			}
			references = append(references, map[string]any{"kind": kind, "streamID": strconv.FormatInt(version.StreamID, 10), "streamRevision": parent.Revision, "release": release, "alias": snapshot.Alias, "configRevision": snapshot.ConfigRevision, "streamDeleted": deleted, "parentMissing": missing})
		}
	}
	sort.Slice(references, func(i, j int) bool {
		a, _ := json.Marshal(references[i])
		b, _ := json.Marshal(references[j])
		return string(a) < string(b)
	})
	return references, nil
}
