// 验证元数据 HTTP 动作、状态、原子性与响应契约。
package unsv2_test

import (
	"context"
	"encoding/json"
	"testing"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
	"backend/internal/repo"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func directoryInserts(ids ...string) []object {
	result := make([]object, 0, len(ids))
	for _, id := range ids {
		result = append(result, object{"insertOne": object{"id": id, "data": object{"name": id}}})
	}
	return result
}

func TestCloudMetadataWriteReceiptSnapshots(t *testing.T) {
	a := newAppAPI(t)
	a.post(a.admin, "metadata", object{"kind": "directory", "id": "snapshot", "data": object{"name": "Original"}}, 200)
	request := object{"kind": "directory", "filter": object{"_id": "snapshot"}, "update": object{"$set": object{"name": "Changed"}}, "returnDocument": "before", "idempotencyKey": "metadata-snapshot"}
	first := a.post(a.admin, "metadata:updateOne", request, 200)
	require.Equal(t, "Original", first["value"].(map[string]any)["data"].(map[string]any)["name"])
	require.EqualValues(t, 1, first["modifiedCount"])
	a.post(a.admin, "metadata:replaceOne", object{"kind": "directory", "filter": object{"_id": "snapshot"}, "replacement": object{"name": "Later"}}, 200)
	replayed := a.post(a.admin, "metadata:updateOne", request, 200)
	require.Equal(t, true, replayed["replayed"])
	require.Equal(t, first["value"], replayed["value"])
	missing := a.post(a.admin, "metadata:updateOne", object{"kind": "directory", "filter": object{"_id": "absent"}, "update": object{"$set": object{"name": "Missing"}}, "returnDocument": "after"}, 200)
	require.Contains(t, missing, "value")
	require.Nil(t, missing["value"])
	require.EqualValues(t, 0, missing["matchedCount"])
	a.post(a.admin, "metadata:deleteOne", object{"kind": "directory", "filter": object{"_id": "snapshot"}, "returnDocument": "after"}, 422)
	deleted := a.post(a.admin, "metadata:deleteOne", object{"kind": "directory", "filter": object{"_id": "snapshot"}, "returnDocument": "before"}, 200)
	require.EqualValues(t, 1, deleted["deletedCount"])
	require.Equal(t, "Later", deleted["value"].(map[string]any)["data"].(map[string]any)["name"])
}

func TestAppMetadataStateArrays(t *testing.T) {
	a := newAppAPI(t)
	a.post(a.admin, "metadata:bulkWrite", object{"kind": "directory", "operations": directoryInserts("restore-a", "restore-b")}, 200)
	filter := object{"_id": object{"$in": []string{"restore-a", "restore-b"}}}
	a.post(a.admin, "metadata:deleteMany", object{"kind": "directory", "filter": filter}, 200)
	for _, id := range []string{"restore-a", "restore-b"} {
		row, err := repo.NewUnsMetaRepo(context.Background()).Get(context.Background(), id, true, "directory")
		require.NoError(t, err)
		require.EqualValues(t, 2, row.Revision)
		require.NotZero(t, row.DeletedAt)
	}
	selection := []object{{"id": "restore-a"}, {"id": "restore-b"}}
	a.post(a.admin, "metadata:restore", object{"kind": "directory", "items": selection}, 200)
	a.post(a.admin, "metadata:bulkWrite", object{"kind": "directory", "operations": []object{{"deleteOne": object{"filter": object{"_id": "restore-a"}}}, {"deleteOne": object{"filter": object{"_id": "restore-b"}, "expectedRevision": 1}}}}, 409)
	for _, id := range []string{"restore-a", "restore-b"} {
		row, err := repo.NewUnsMetaRepo(context.Background()).Get(context.Background(), id, false, "directory")
		require.NoError(t, err)
		require.EqualValues(t, 3, row.Revision)
		require.Zero(t, row.DeletedAt)
	}
	a.post(a.admin, "metadata:deleteOne", object{"kind": "directory", "filter": object{"_id": "restore-a"}, "expectedRevision": 3}, 200)
	a.post(a.admin, "metadata:restore", object{"kind": "directory", "items": []object{{"id": "restore-a", "expectedRevision": 4}}}, 200)
}

func TestAppMetadataStateDryRunAndSynchronousCompletion(t *testing.T) {
	a := newAppAPI(t)
	a.post(a.admin, "metadata", object{"kind": "directory", "id": "sync-state", "data": object{"name": "Sync state"}}, 200)
	request := object{"kind": "directory", "filter": object{"_id": "sync-state"}, "dryRun": true}
	result := a.post(a.admin, "metadata:deleteOne", request, 200)
	require.Equal(t, "validated", result["status"])
	row, err := repo.NewUnsMetaRepo(context.Background()).Get(context.Background(), "sync-state", false, "directory")
	require.NoError(t, err)
	require.EqualValues(t, 1, row.Revision)
	request["dryRun"] = false
	require.Equal(t, "committed", a.post(a.admin, "metadata:deleteOne", request, 200)["status"])
	a.post(a.admin, "metadata:restore", object{"kind": "directory", "items": []object{{"id": "sync-state"}}}, 200)
	request["mode"] = "async"
	a.post(a.admin, "metadata:deleteOne", request, 422)
}

func TestAppMetadataStateAfterUpdatesAndReplay(t *testing.T) {
	a := newAppAPI(t)
	create := object{"kind": "directory", "idempotencyKey": "state-replay", "operations": directoryInserts("state-a", "state-b")}
	a.post(a.admin, "metadata:bulkWrite", create, 200)
	require.Equal(t, true, a.post(a.admin, "metadata:bulkWrite", create, 200)["replayed"])
	create["operations"] = directoryInserts("different")
	a.post(a.admin, "metadata:bulkWrite", create, 409)
	ops := []object{{"updateOne": object{"filter": object{"_id": "state-a"}, "update": object{"$set": object{"description": "A"}}}}, {"updateOne": object{"filter": object{"_id": "state-b"}, "update": object{"$set": object{"description": "B"}}, "expectedRevision": 99}}}
	a.post(a.admin, "metadata:bulkWrite", object{"kind": "directory", "operations": ops}, 409)
	ops[1]["updateOne"].(object)["expectedRevision"] = 1
	a.post(a.admin, "metadata:bulkWrite", object{"kind": "directory", "operations": ops}, 200)
	a.post(a.admin, "metadata:updateOne", object{"kind": "directory", "filter": object{"_id": "state-a"}, "update": object{"$set": object{"description": "stale"}}, "expectedRevision": 1}, 409)
	a.post(a.admin, "metadata:deleteMany", object{"kind": "directory", "filter": object{"_id": object{"$in": []string{"state-a", "state-b"}}}}, 200)
	a.post(a.admin, "metadata:restore", object{"kind": "directory", "items": []object{{"id": "state-a"}, {"id": "state-b"}}}, 200)
	for _, id := range []string{"state-a", "state-b"} {
		row, err := repo.NewUnsMetaRepo(context.Background()).Get(context.Background(), id, false, "directory")
		require.NoError(t, err)
		require.EqualValues(t, 4, row.Revision)
		require.Zero(t, row.DeletedAt)
	}
}

func TestMetadataDocumentHTTPContracts(t *testing.T) {
	a := newAppAPI(t)
	result := a.post(a.admin, "metadata:transact", object{"operations": []object{{"opId": "parent", "action": "insertOne", "kind": "directory", "id": "parent", "data": object{"name": "Parent"}}, {"opId": "child", "action": "insertOne", "kind": "directory", "id": "child", "data": object{"name": "Child", "parentId": object{"ref": "parent"}}, "dependsOn": []string{"parent"}}}}, 200)
	require.Len(t, result["results"], 2)
	require.Len(t, a.post(a.admin, "metadata:find", object{"kind": "directory", "filter": object{"parentId": "parent"}, "sort": []any{[]any{"name", 1}}}, 200)["items"], 1)
	require.EqualValues(t, 1, a.post(a.admin, "metadata:count", object{"kind": "directory", "filter": object{"parentId": "parent"}}, 200)["count"])
	require.Len(t, a.post(a.admin, "metadata:aggregate", object{"kind": "directory", "pipeline": []object{{"$distinct": "parentId"}}, "filter": object{"parentId": "parent"}}, 200)["items"], 1)
	require.Len(t, a.post(a.admin, "metadata:aggregate", object{"kind": "directory", "filter": object{"parentId": "parent"}, "pipeline": []object{{"$project": object{"name": 1}}}}, 200)["items"], 1)
	require.Empty(t, a.post(a.admin, "metadata:find", object{"kind": "directory", "filter": object{"_id": "missing"}}, 200)["items"])
	changed := a.post(a.admin, "metadata:updateOne", object{"kind": "directory", "filter": object{"_id": "child"}, "update": object{"$set": object{"description": "updated"}}, "returnDocument": "after", "projection": object{"name": 1}}, 200)
	require.Equal(t, "Child", changed["value"].(map[string]any)["data"].(map[string]any)["name"])
	a.post(a.admin, "metadata:updateMany", object{"kind": "directory", "filter": object{}, "update": object{"$set": object{"description": "many"}}, "maxDocs": 1}, 422)
	require.EqualValues(t, 2, a.post(a.admin, "metadata:updateMany", object{"kind": "directory", "filter": object{}, "update": object{"$set": object{"description": "many"}}}, 200)["matchedCount"])
	a.post(a.admin, "metadata:replaceOne", object{"kind": "directory", "filter": object{"_id": "child"}, "replacement": object{"name": "Replaced", "parentId": "parent"}, "returnDocument": "after"}, 200)
	a.post(a.admin, "metadata:updateOne", object{"kind": "directory", "filter": object{"_id": "upsert"}, "update": object{"$set": object{"name": "Upsert"}}, "upsert": true}, 200)
	a.post(a.admin, "metadata:replaceOne", object{"kind": "directory", "filter": object{"_id": "replace-upsert"}, "replacement": object{"name": "Replace upsert"}, "upsert": true}, 200)
	a.post(a.admin, "metadata:deleteOne", object{"kind": "directory", "filter": object{"_id": "upsert"}}, 200)
	a.post(a.admin, "metadata:updateOne", object{"kind": "directory", "filter": object{"_id": "upsert"}, "update": object{"$set": object{"name": "Upsert"}}, "upsert": true}, 409)
	a.post(a.admin, "metadata:restore", object{"kind": "directory", "items": []object{{"id": "upsert"}}}, 200)
	partial := object{"kind": "directory", "atomic": false, "ordered": false, "idempotencyKey": "partial", "operations": []object{{"insertOne": object{"id": "partial-a", "data": object{"name": "Partial A"}}}, {"updateOne": object{"filter": object{"_id": "partial-a"}, "update": object{"$set": object{"name": "Invalid"}}, "expectedRevision": 99}}, {"insertOne": object{"id": "partial-b", "data": object{"name": "Partial B"}}}}}
	result = a.post(a.admin, "metadata:bulkWrite", partial, 200)
	require.Equal(t, "partiallyCommitted", result["status"])
	require.Len(t, result["errors"], 1)
	require.EqualValues(t, 2, result["insertedCount"])
	require.Equal(t, true, a.post(a.admin, "metadata:bulkWrite", partial, 200)["replayed"])
	a.post(a.admin, "namespace:copy", object{"kind": "directory", "id": "parent", "data": object{"name": "Parent copy"}}, 200)
	a.post(a.admin, "metadata:transact", object{"operations": []object{{"opId": "new", "action": "insertOne", "kind": "directory", "id": "rollback", "data": object{"name": "Rollback"}}, {"opId": "stale", "action": "updateOne", "kind": "directory", "id": "parent", "expectedRevision": 999, "update": object{"$set": object{"name": "Invalid"}}}}}, 409)
	require.Empty(t, a.post(a.admin, "metadata:find", object{"kind": "directory", "filter": object{"_id": "rollback"}}, 200)["items"])
}

func TestMetadataMCPActionPermissions(t *testing.T) {
	_ = newAppAPI(t)
	actor := contextx.Subject{UserID: 1, AuthType: "mcp", MCPRestricted: true, MCPScopes: []string{"uns.read", "uns.model.manage"}, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}}}
	service := unsv2.New(unsmeta.New(), func(_ context.Context, s contextx.Subject) (contextx.Subject, error) { return s, nil })
	ctx := contextx.WithSubject(context.Background(), actor)
	raw, _ := json.Marshal(object{"operations": []object{{"opId": "insert", "action": "insertOne", "kind": "directory", "id": "mcp-created", "data": object{"name": "MCP created"}}}})
	_, err := service.Execute(ctx, "metadataTransact", raw)
	require.NoError(t, err)
	raw, _ = json.Marshal(object{"kind": "directory", "operations": []object{{"updateOne": object{"filter": object{"_id": "mcp-created"}, "update": object{"$set": object{"description": "Allowed"}}}}}})
	_, err = service.Execute(ctx, "metadataBulkWrite", raw)
	require.NoError(t, err)
	raw, _ = json.Marshal(object{"operations": []object{{"opId": "delete", "action": "deleteOne", "kind": "directory", "id": "mcp-created"}}})
	_, err = service.Execute(ctx, "metadataTransact", raw)
	require.Error(t, err)
	row, err := repo.NewUnsMetaRepo(context.Background()).Get(context.Background(), "mcp-created", false, "directory")
	require.NoError(t, err)
	require.Zero(t, row.DeletedAt)
}

func TestMetadataMountedObjectsRemainReadOnly(t *testing.T) {
	a := newAppAPI(t)
	a.post(a.admin, "metadata", object{"kind": "directory", "id": "mounted", "data": object{"name": "Mounted"}}, 200)
	// Seed receiver-owned metadata through the same transaction-local privileged guard.
	err := repo.GetUnsConn(context.Background()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL public.sync_write = 'on'").Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE public.uns_meta SET mount_id='fixture-mount' WHERE kind='directory' AND id='mounted'").Error
	})
	require.NoError(t, err)
	item := a.post(a.admin, "metadata:find", object{"kind": "directory", "filter": object{"_id": "mounted"}, "limit": 1}, 200)["items"].([]any)[0].(map[string]any)
	require.Equal(t, "fixture-mount", item["mountId"])
	a.post(a.admin, "metadata:updateOne", object{"kind": "directory", "filter": object{"_id": "mounted"}, "update": object{"$set": object{"name": "changed"}}}, 403)
	a.post(a.admin, "metadata:deleteOne", object{"kind": "directory", "filter": object{"_id": "mounted"}}, 403)
	a.post(a.admin, "metadata", object{"kind": "directory", "id": "mounted-child", "data": object{"name": "Child", "parentId": "mounted"}}, 403)
	row, err := repo.NewUnsMetaRepo(context.Background()).Get(context.Background(), "mounted", false, "directory")
	require.NoError(t, err)
	require.EqualValues(t, 1, row.Revision)
}

func TestNamespaceCollectionTrashHTTP(t *testing.T) {
	a := newAppAPI(t)
	a.post(a.admin, "metadata", object{"kind": "directory", "id": "root", "data": object{"name": "Root"}}, 200)
	a.post(a.admin, "metadata", object{"kind": "directory", "id": "child", "data": object{"name": "Child", "parentId": "root"}}, 200)
	a.post(a.admin, "metadata", object{"kind": "collection", "id": "data", "data": object{"name": "Data", "parentId": "child", "recordSchema": object{"type": "object"}}}, 200)
	a.post(a.admin, "records/insert-one", object{"collectionId": "data", "id": "record", "data": object{"value": 7}}, 200)
	selected := object{"kind": "directory", "filter": object{"_id": object{"$in": []string{"root", "child"}}}}
	a.post(a.admin, "metadata:deleteMany", selected, 200)
	deleted := a.post(a.admin, "metadata:find", object{"kinds": []string{"directory", "collection"}, "state": "deleted", "filter": object{}}, 200)
	require.Len(t, deleted["items"], 3)
	found := a.post(a.admin, "collections/:collectionId/records:get", object{"collectionId": "data", "ids": []string{"record"}}, 200)
	require.Len(t, found["items"], 1)
	a.post(a.admin, "records/insert-one", object{"collectionId": "data", "id": "blocked", "data": object{}}, 404)
	restored := a.post(a.admin, "metadata:restore", object{"kind": "directory", "items": []object{{"id": "child"}, {"id": "root"}}}, 200)
	require.EqualValues(t, 2, restored["restoredCount"])
	a.post(a.admin, "metadata:deleteOne", object{"kind": "directory", "filter": object{"_id": "root"}}, 200)
	preview := a.post(a.admin, "metadata:purge", object{"kind": "directory", "ids": []string{"root"}, "dryRun": true}, 200)
	require.Equal(t, "validated", preview["status"])
	accepted := a.post(a.admin, "metadata:purge", object{"kind": "directory", "ids": []string{"root"}, "idempotencyKey": "http-purge"}, 200)
	require.Equal(t, "accepted", accepted["status"])
	require.NotEmpty(t, accepted["jobId"])
	a.post(a.admin, "metadata:restore", object{"kind": "directory", "items": []object{{"id": "root"}}}, 409)
}
