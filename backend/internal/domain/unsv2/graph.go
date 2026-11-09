package unsv2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
)

// Only declared reference locations participate. In particular, processor code,
// user payloads and free-form extension JSON never become graph relationships.
const graphPrefix = `WITH visible AS (
 SELECT kind,id,data,revision,created_at,updated_at,deleted_at,kind||':'||id AS key
 FROM public.uns_meta WHERE kind IN ? AND (? OR deleted_at=0)
 AND id NOT LIKE 'tier0.system.stream.%'
 AND (kind<>'task' OR COALESCE(data->>'type','') NOT IN ('queryIndexTask','streamRequestGrant'))
 AND (kind<>'task' OR COALESCE(data->>'type','')<>'apiBatchJob' OR data->>'ownerId'=?)
 AND %s
), refs AS (
 SELECT concat('directory:',data->>'parentId') AS source,key AS target,'contains' AS relation
 FROM visible WHERE kind IN ('directory','topic','collection') AND COALESCE(data->>'parentId','')<>''
 UNION ALL
 SELECT key,concat('collection:',data->>'collectionId'),'references' FROM visible
 WHERE kind='topic' AND COALESCE(data->>'collectionId','')<>''
 UNION ALL
 SELECT v.key,concat('collection:',f->>'collectionId'),'reads' FROM visible v
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(data->'fields')='array' THEN data->'fields' ELSE '[]'::jsonb END) f
 WHERE kind='topic' AND COALESCE(f->>'collectionId','')<>''
 UNION ALL
 SELECT v.key,concat('collection:',g->>'collectionId'),'reads' FROM visible v
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(data->'collections')='array' THEN data->'collections' ELSE '[]'::jsonb END) g
 WHERE kind='app' AND COALESCE(g->>'collectionId','')<>''
 UNION ALL
 SELECT v.key,concat('topic:',t #>> '{}'),'references' FROM visible v
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(data->'navigation'->'topicIds')='array' THEN data->'navigation'->'topicIds' ELSE '[]'::jsonb END) t
 WHERE kind='app' AND jsonb_typeof(t)='string'
 UNION ALL
 SELECT v.key,concat('stream:',b.value->>'streamId'),'references' FROM visible v
 CROSS JOIN LATERAL jsonb_each(CASE WHEN jsonb_typeof(data->'config'->'streams')='object' THEN data->'config'->'streams' ELSE '{}'::jsonb END) b
 WHERE kind='source' AND COALESCE(b.value->>'streamId','')<>''
 UNION ALL
 SELECT v.key,concat('collection:',COALESCE(b.value->'target'->>'collectionId',b.value->>'collectionId')),'outputs' FROM visible v
 CROSS JOIN LATERAL jsonb_each(CASE WHEN jsonb_typeof(data->'config'->'streams')='object' THEN data->'config'->'streams' ELSE '{}'::jsonb END) b
 WHERE kind='source' AND COALESCE(b.value->'target'->>'collectionId',b.value->>'collectionId','')<>''
 UNION ALL
 SELECT v.key,concat('topic:',b.value->>'topicId'),'outputs' FROM visible v
 CROSS JOIN LATERAL jsonb_each(CASE WHEN jsonb_typeof(data->'config'->'streams')='object' THEN data->'config'->'streams' ELSE '{}'::jsonb END) b
 WHERE kind='source' AND COALESCE(b.value->>'topicId','')<>''
 UNION ALL
 SELECT 'source:'||id,key,'state' FROM visible WHERE kind='source_state'
 UNION ALL
 SELECT concat('source:',data->>'sourceId'),key,CASE WHEN kind='source_version' THEN 'version' ELSE 'plan' END FROM visible
 WHERE kind IN ('source_plan','source_version') AND COALESCE(data->>'sourceId','')<>''
 UNION ALL
 SELECT concat('stream:',data->>'streamId'),key,'references' FROM visible
 WHERE kind IN ('source_plan','source_version','subscription','task') AND COALESCE(data->>'streamId','')<>''
 UNION ALL
 SELECT key,concat('collection:',data->>'collectionId'),'references' FROM visible
 WHERE kind IN ('subscription','task') AND COALESCE(data->>'collectionId','')<>''
 UNION ALL
 SELECT v.key,concat('source:',s->>'id'),'references' FROM visible v
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(COALESCE(data->'draftManifest'->'sources',data->'programManifest'->'sources'))='array' THEN COALESCE(data->'draftManifest'->'sources',data->'programManifest'->'sources') ELSE '[]'::jsonb END) s
 WHERE kind IN ('stream','source_plan') AND COALESCE(s->>'id','')<>''
 UNION ALL
 SELECT v.key,concat(CASE WHEN COALESCE(t->>'collectionId','')<>'' THEN 'collection:' ELSE 'topic:' END,COALESCE(NULLIF(t->>'collectionId',''),t->>'topicId')),
 CASE WHEN t->>'access'='read' THEN 'reads' WHEN t->>'access'='write' THEN 'outputs' ELSE 'references' END FROM visible v
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(COALESCE(data->'draftManifest'->'topics',data->'programManifest'->'topics'))='array' THEN COALESCE(data->'draftManifest'->'topics',data->'programManifest'->'topics') ELSE '[]'::jsonb END) t
 WHERE kind IN ('stream','source_plan') AND COALESCE(t->>'collectionId',t->>'topicId','')<>''
), edges AS (
 SELECT DISTINCT r.source,r.target,r.relation FROM refs r
 JOIN visible s ON s.key=r.source JOIN visible t ON t.key=r.target
 WHERE r.source<>r.target
) `

type graphEdge struct {
	Key      string `json:"key"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	Relation string `json:"relation"`
}

func graphPermission(ctx context.Context, kind string) error {
	if kind == "task" || kind == "subscription" {
		return require(ctx, "uns.read")
	}
	return readPermission(ctx, kind)
}

func graphProjection(row repo.UnsMeta) map[string]any {
	kind := row.StorageKind
	if kind == "" {
		kind = row.Kind
	}
	if kind == "folder" {
		kind = "directory"
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(row.Data, &raw)
	data := map[string]any{}
	// Unknown fields are omitted rather than relying on a secret-name blacklist.
	for _, field := range []string{"schemaVersion", "name", "adapter", "viewMode", "parentId", "collectionId", "status", "health", "revision", "configRevision", "activeVersion", "lastDataAt", "checkedAt", "createdAt", "updatedAt"} {
		if value, ok := raw[field]; ok {
			var decoded any
			if json.Unmarshal(value, &decoded) == nil {
				data[field] = decoded
			}
		}
	}
	// Schemas are contracts, not arbitrary record values or runtime payloads.
	if kind == "collection" {
		for _, field := range []string{"recordSchema", "sampleSchema"} {
			if value, ok := raw[field]; ok {
				var schema map[string]any
				if json.Unmarshal(value, &schema) == nil {
					data[field] = graphSchema(schema, 0)
				}
			}
		}
	}
	if kind == "topic" {
		var fields []unsmeta.FieldBinding
		if json.Unmarshal(raw["fields"], &fields) == nil && len(fields) <= 128 {
			data["fields"] = fields
		}
	}
	name, _ := data["name"].(string)
	if name == "" {
		name = row.ID
	}
	return map[string]any{"key": kind + ":" + row.ID, "kind": kind, "id": row.ID, "name": name, "revision": row.Revision, "archived": row.DeletedAt > 0, "data": data}
}

func graphSchema(in map[string]any, depth int) map[string]any {
	out := map[string]any{}
	if depth > 12 {
		return out
	}
	for _, k := range []string{"type", "format", "unit", "required", "additionalProperties", "minimum", "maximum"} {
		if v, ok := in[k]; ok {
			out[k] = v
		}
	}
	if properties, ok := in["properties"].(map[string]any); ok {
		result := map[string]any{}
		for k, v := range properties {
			if schema, ok := v.(map[string]any); ok {
				result[k] = graphSchema(schema, depth+1)
			}
		}
		out["properties"] = result
	}
	if items, ok := in["items"].(map[string]any); ok {
		out["items"] = graphSchema(items, depth+1)
	}
	return out
}

func graphQuery(ctx context.Context, q Query) (map[string]any, error) {
	if err := require(ctx, "uns.read"); err != nil {
		return nil, err
	}
	page, size, err := bounds(q.Pagination, 100)
	if err != nil {
		return nil, err
	}
	if q.Mode != "" && q.Mode != "overview" && q.Mode != "focus" {
		return nil, invalid("mode", "overview or focus required")
	}
	requested := q.Kinds
	if len(requested) > len(Kinds) {
		return nil, invalid("kinds", "too many kinds")
	}
	if len(requested) == 0 {
		requested = Kinds
	}
	visibleKinds := []string{}
	for _, kind := range requested {
		if !contains(Kinds, kind) {
			return nil, invalid("kinds", "unknown kind")
		}
		if graphPermission(ctx, kind) == nil && !contains(visibleKinds, kind) {
			visibleKinds = append(visibleKinds, kind)
		}
	}
	// Related nodes may have a different Kind than the selected filter.
	allowed := []string{}
	for _, kind := range Kinds {
		if graphPermission(ctx, kind) == nil {
			allowed = append(allowed, kind)
		}
	}
	subject, _ := contextx.SubjectFrom(ctx)
	ownerID := fmt.Sprint(subject.UserID)
	visibility := "TRUE"
	var visibilityArgs []any
	if contains(allowed, "app") {
		visibility, visibilityArgs, err = nativeAppVisibility(ctx)
		if err != nil {
			return nil, err
		}
	}
	graphCTE := strings.Replace(graphPrefix, "AND %s", "AND "+visibility, 1)
	baseArgs := append([]any{allowed, q.IncludeArchived, ownerID}, visibilityArgs...)
	predicate := "kind IN ? AND (data->>'name' ILIKE ? OR id ILIKE ?)"
	args := append(append([]any{}, baseArgs...), visibleKinds, "%"+q.Search+"%", "%"+q.Search+"%")
	focus := ""
	var focusRow repo.UnsMeta
	if q.Mode == "focus" {
		if len(q.Targets) != 1 || !contains(Kinds, q.Targets[0].Kind) {
			return nil, invalid("targets", "one valid focus required")
		}
		t := q.Targets[0]
		if err := graphPermission(ctx, t.Kind); err != nil {
			return nil, err
		}
		focusRow, err = meta(ctx, t.Kind, t.ID, q.IncludeArchived)
		if err != nil {
			return nil, err
		}
		if t.Kind == "app" {
			var matches int64
			if err = repo.GetUnsConn(ctx).Model(&repo.UnsMeta{}).Where("kind='app' AND id=?", t.ID).Where(visibility, visibilityArgs...).Count(&matches).Error; err != nil {
				return nil, err
			}
			if matches == 0 {
				return nil, repo.ErrNotFound
			}
		}
		if t.Kind == "task" {
			var job struct {
				Type    string `json:"type"`
				OwnerID int64  `json:"ownerId"`
			}
			_ = json.Unmarshal(focusRow.Data, &job)
			if job.Type == "queryIndexTask" || job.Type == "streamRequestGrant" || job.Type == "apiBatchJob" && job.OwnerID != subject.UserID {
				return nil, problem("NOT_FOUND", "job not found")
			}
		}
		focus = t.Kind + ":" + t.ID
		predicate = `key=? OR key IN (SELECT CASE WHEN source=? THEN target ELSE source END FROM edges WHERE source=? OR target=?)
 OR (kind='collection' AND key IN (SELECT CASE WHEN e2.source=n.key THEN e2.target ELSE e2.source END
 FROM edges e1 JOIN visible n ON n.key=CASE WHEN e1.source=? THEN e1.target ELSE e1.source END
 JOIN edges e2 ON e2.source=n.key OR e2.target=n.key WHERE e1.source=? OR e1.target=?))`
		args = append(append([]any{}, baseArgs...), focus, focus, focus, focus, focus, focus, focus)
	}
	db := repo.GetUnsConn(ctx)
	var total int64
	if err := db.Raw(graphCTE+"SELECT count(*) FROM visible WHERE "+predicate, args...).Scan(&total).Error; err != nil {
		return nil, err
	}
	rows := []repo.UnsMeta{}
	order := "kind,id"
	limit, offset := size, (page-1)*size
	if focus != "" {
		// Reserve one position for the focus on every page; no neighbor is skipped.
		predicate = "(" + predicate + ") AND key<>?"
		args = append(args, focus)
		if size < 2 {
			return nil, invalid("size", "focus requires at least two nodes")
		}
		limit, offset = size-1, (page-1)*(size-1)
	}
	selectSQL := graphCTE + "SELECT kind,id,data,revision,created_at,updated_at,deleted_at FROM visible WHERE " + predicate + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	if err := db.Raw(selectSQL, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if focus != "" {
		rows = append([]repo.UnsMeta{focusRow}, rows...)
	}
	keys := []string{}
	nodes := []map[string]any{}
	collections := []map[string]any{}
	for _, row := range rows {
		node := graphProjection(row)
		nodes = append(nodes, node)
		keys = append(keys, node["key"].(string))
		if node["kind"] == "collection" && focus != "" {
			node["collectionId"] = row.ID
			collections = append(collections, node)
		}
	}
	edges := []graphEdge{}
	unloaded := int64(0)
	byNode := []map[string]any{}
	if len(keys) > 0 {
		if err := db.Raw(graphCTE+"SELECT source,target,relation FROM edges WHERE source IN ? AND target IN ?", append(append([]any{}, baseArgs...), keys, keys)...).Scan(&edges).Error; err != nil {
			return nil, err
		}
		for i := range edges {
			edges[i].Key = fmt.Sprintf("%s|%s|%s", edges[i].Source, edges[i].Relation, edges[i].Target)
		}
		var counts []struct {
			NodeKey string
			Count   int64
		}
		if err := db.Raw(graphCTE+`SELECT CASE WHEN source IN ? THEN source ELSE target END AS node_key,count(*) AS count FROM edges WHERE (source IN ? AND target NOT IN ?) OR (target IN ? AND source NOT IN ?) GROUP BY node_key`, append(append([]any{}, baseArgs...), keys, keys, keys, keys, keys)...).Scan(&counts).Error; err != nil {
			return nil, err
		}
		for _, count := range counts {
			unloaded += count.Count
			byNode = append(byNode, map[string]any{"nodeKey": count.NodeKey, "count": count.Count})
		}
	}
	return map[string]any{"nodes": nodes, "edges": edges, "collections": collections, "total": total, "page": page, "size": size, "unloadedRelations": unloaded, "unloadedByNode": byNode}, nil
}
