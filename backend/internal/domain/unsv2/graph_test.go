package unsv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/testkit"
	assert "github.com/stretchr/testify/require"
)

func TestRuntimeIdentityStringsPreservesProtocolParameters(t *testing.T) {
	value := runtimeIdentityStrings(map[string]any{
		"id":       json.Number("377546186909280"),
		"sourceId": json.Number("377546186909280"),
		"modbus":   map[string]any{"unitId": json.Number("41"), "ranges": []any{map[string]any{"address": json.Number("0"), "quantity": json.Number("3")}}},
	}).(map[string]any)
	assert.Equal(t, "377546186909280", value["id"])
	assert.Equal(t, "377546186909280", value["sourceId"])
	assert.Equal(t, json.Number("41"), value["modbus"].(map[string]any)["unitId"])
}

func TestSourceUnitIDStringCompatibility(t *testing.T) {
	op, ok := FindResourceOperation("sourceDocumentCreate")
	assert.True(t, ok)
	document := `{"name":"unit-id-check","protocol":"modbus-tcp","role":"client","runtimeLocation":"local","endpoint":"tcp://127.0.0.1:1502","modbus":{"unitId":%s,"pollIntervalMs":3000,"ranges":[{"id":"press","table":"holding-register","address":0,"quantity":3,"writable":false}]}}`
	for _, unitID := range []string{`41`, `"41"`, `"041"`} {
		for _, batch := range []bool{false, true} {
			doc := fmt.Sprintf(document, unitID)
			body := `{"document":` + doc + `,"dryRun":true}`
			if batch {
				body = `{"items":[{"document":` + doc + `}],"dryRun":true}`
			}
			fields, err := validateResourceRequest(op, []byte(body))
			assert.NoError(t, err)
			if batch {
				var items []map[string]json.RawMessage
				assert.NoError(t, json.Unmarshal(fields["items"], &items))
				fields = items[0]
			}
			var parsed struct {
				Modbus struct {
					UnitID int `json:"unitId"`
				} `json:"modbus"`
			}
			assert.NoError(t, json.Unmarshal(fields["document"], &parsed))
			assert.Equal(t, 41, parsed.Modbus.UnitID)
		}
	}
	for _, unitID := range []string{`""`, `"secret-not-a-number"`, `"41.5"`, `"-1"`, `"256"`, `true`} {
		_, err := validateResourceRequest(op, []byte(`{"document":`+fmt.Sprintf(document, unitID)+`,"dryRun":true}`))
		assert.Error(t, err)
	}
}

func TestCloudSchemaFormatsInViews(t *testing.T) {
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{
		"legacy": {Type: "number"},
		"count":  {Type: "integer", Format: unsmeta.NumericFormatInt32},
		"sensor": {Type: "object", Properties: map[string]unsmeta.Schema{
			"reading": {Type: "number", Format: unsmeta.NumericFormatFloat},
			"at":      {Type: "string", Format: unsmeta.SchemaFormatDateTime},
		}},
	}}
	var fields []any
	flattenDiscoveryFields(schema, "sample", "data", true, nil, &fields)
	byPath := map[string]map[string]any{}
	for _, field := range fields {
		item := field.(map[string]any)
		byPath[item["path"].(string)] = item
	}
	assert.Equal(t, unsmeta.NumericFormatInt32, byPath["data.count"]["format"])
	assert.Equal(t, unsmeta.NumericFormatFloat, byPath["data.sensor.reading"]["format"])
	assert.Equal(t, unsmeta.SchemaFormatDateTime, byPath["data.sensor.at"]["format"])
	assert.NotContains(t, byPath["data.legacy"], "format")
	raw, err := json.Marshal(schema)
	assert.NoError(t, err)
	var view map[string]any
	assert.NoError(t, json.Unmarshal(raw, &view))
	assert.Equal(t, view, graphSchema(view, 0))
	response := ResponseSchemas()["DiscoveredField"].(map[string]any)
	assert.Contains(t, response["properties"].(map[string]any), "format")
}

func graphContext(keys ...string) context.Context {
	resources := map[string]struct{}{}
	for _, key := range keys {
		resources[key] = struct{}{}
	}
	return contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: resources})
}

// These shared UNS tests exercise RBAC independently of host-bound activation.
// Restore product identity after each test; production capability checks remain active.
func nativeEdgeTestProduct(t *testing.T) {
	t.Helper()
	t.Setenv("TIER0_RUNTIME_MODE", "native")
}

func TestCloudStreamReceiptReplayNeverReadsCurrentHead(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := graphContext("uns.stream.manage", "uns.read")
	service := New(unsmeta.New(), nil)
	for _, legacy := range []bool{false, true} {
		key := fmt.Sprintf("stream-snapshot-%t", legacy)
		fields := map[string]json.RawMessage{"id": json.RawMessage(`"901"`), "expectedRevision": json.RawMessage(`1`), "dryRun": json.RawMessage(`false`)}
		fields["idempotencyKey"], _ = json.Marshal(key)
		op := ResourceOperation{Name: "streamDocumentDisable", Mutation: "runtime"}
		identity, err := requestIdentity(ctx, key)
		assert.NoError(t, err)
		raw, _ := json.Marshal(map[string]any{"operation": op.Name, "fields": fields})
		digest := sha256.Sum256(raw)
		value := map[string]any{"id": "901", "activeVersion": "902", "revision": 2}
		if !legacy {
			value["activeRelease"] = 1
		}
		data, _ := json.Marshal(map[string]any{"type": "apiRuntimeReceipt", "status": "completed", "digest": hex.EncodeToString(digest[:]), "value": value})
		assert.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: "task", ID: "api-runtime-" + identity, Data: data, Revision: 2}).Error)
		// No runtime or current Stream exists: a replay must depend only on its frozen receipt.
		result, err := service.runtimeResource(ctx, op, fields)
		assert.NoError(t, err)
		encoded, err := json.Marshal(result)
		assert.NoError(t, err)
		want, _ := json.Marshal(value)
		assert.JSONEq(t, string(want), string(encoded))
	}
	// Admissions written by an older process, and operations that can contact
	// external systems, remain unknown. Neither path may execute them again.
	for _, name := range []string{"sourceDocumentCreate", "sourceNativeOAuthComplete", "streamProgramDeploy"} {
		key := "unknown-" + name
		fields := map[string]json.RawMessage{"dryRun": json.RawMessage(`false`)}
		fields["idempotencyKey"], _ = json.Marshal(key)
		identity, err := requestIdentity(ctx, key)
		assert.NoError(t, err)
		raw, _ := json.Marshal(map[string]any{"operation": name, "fields": fields})
		digest := sha256.Sum256(raw)
		data, _ := json.Marshal(map[string]any{"type": "apiRuntimeReceipt", "status": "running", "digest": hex.EncodeToString(digest[:])})
		assert.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: "task", ID: "api-runtime-" + identity, Data: data, Revision: 1}).Error)
		_, err = service.runtimeResource(ctx, ResourceOperation{Name: name, Mutation: "runtime"}, fields)
		var fault *Fault
		assert.ErrorAs(t, err, &fault)
		assert.Equal(t, "RUNTIME_RESULT_UNAVAILABLE", fault.Code)
		assert.False(t, fault.RolledBack, "the old operation's outcome is still unknown")
	}
}

func TestGraphQueryRelationsPermissionsAndRedaction(t *testing.T) {
	nativeEdgeTestProduct(t)
	store, _ := testkit.NewTestDB(t)
	insert := func(kind, id string, data map[string]any, archived bool) {
		t.Helper()
		raw, err := json.Marshal(data)
		assert.NoError(t, err)
		row := repo.UnsMeta{Kind: kind, ID: id, Revision: 1, Data: raw}
		if archived {
			row.DeletedAt = 1
		}
		assert.NoError(t, store.UnsDB().Create(&row).Error)
	}
	insert("directory", "shared", map[string]any{"name": "Folder"}, false)
	insert("topic", "shared", map[string]any{"name": "Temperature", "parentId": "shared", "collectionId": "readings"}, false)
	insert("collection", "readings", map[string]any{"name": "Measurements", "recordSchema": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "number"}}}}, false)
	insert("source", "mqtt", map[string]any{"name": "Broker", "credentials": map[string]any{"token": "SECRET"}, "config": map[string]any{"connection": map[string]any{"password": "SECRET"}, "streams": map[string]any{"bind": map[string]any{"topicId": "shared", "streamId": "processor", "target": map[string]any{"collectionId": "readings"}}}}}, false)
	insert("stream", "processor", map[string]any{"name": "Process", "document": map[string]any{"script": "SECRET"}, "draftManifest": map[string]any{"sources": []any{map[string]any{"id": "mqtt"}}, "topics": []any{map[string]any{"collectionId": "readings", "access": "write"}}}}, false)
	insert("source_state", "mqtt", map[string]any{"health": "online", "resources": []any{map[string]any{"password": "SECRET"}}}, false)
	insert("task", "job", map[string]any{"payload": map[string]any{"password": "SECRET"}, "sourceId": "unknown", "streamId": "processor"}, false)
	insert("app", "app", map[string]any{"name": "App", "collections": []any{map[string]any{"collectionId": "readings"}}, "navigation": map[string]any{"topicIds": []any{"shared"}}}, false)
	insert("topic", "dangling", map[string]any{"name": "Broken", "collectionId": "missing", "parentId": "missing"}, false)
	insert("source", "archived", map[string]any{"name": "Old"}, true)
	ctx := graphContext("uns.read", "uns.source", "uns.stream", "uns.app.read")
	service := &Service{}
	query := func(ctx context.Context, q Query) map[string]any {
		t.Helper()
		q.QueryID = "graph"
		out, err := service.Query(ctx, "graph", Queries{Queries: []Query{q}})
		assert.NoError(t, err)
		return out.(map[string]any)["results"].([]map[string]any)[0]
	}
	out := query(ctx, Query{Pagination: Pagination{Size: 100}})
	assert.EqualValues(t, 9, out["total"])
	nodes := out["nodes"].([]map[string]any)
	keys := map[string]bool{}
	for _, node := range nodes {
		keys[node["key"].(string)] = true
	}
	assert.True(t, keys["directory:shared"])
	assert.True(t, keys["topic:shared"])
	encoded, _ := json.Marshal(out)
	assert.NotContains(t, string(encoded), "SECRET")
	for _, edge := range out["edges"].([]graphEdge) {
		assert.True(t, keys[edge.Source])
		assert.True(t, keys[edge.Target])
		assert.NotContains(t, edge.Target, "missing")
	}
	focus := query(ctx, Query{Mode: "focus", Targets: []Target{{Kind: "source", ID: "mqtt"}}, Pagination: Pagination{Size: 100}})
	assert.NotEmpty(t, focus["collections"])
	assert.Equal(t, "source:mqtt", focus["nodes"].([]map[string]any)[0]["key"])
	limited := query(graphContext("uns.read"), Query{Pagination: Pagination{Size: 100}})
	for _, node := range limited["nodes"].([]map[string]any) {
		assert.NotContains(t, []string{"source", "stream", "source_state", "app"}, node["kind"])
	}
	_, err := service.Query(graphContext("uns.read"), "graph", Queries{Queries: []Query{{QueryID: "denied", Mode: "focus", Targets: []Target{{Kind: "source", ID: "mqtt"}}}}})
	assert.Error(t, err)
	_, err = service.Query(context.Background(), "graph", Queries{Queries: []Query{{QueryID: "denied"}}})
	assert.Error(t, err)
	archived := query(ctx, Query{IncludeArchived: true, Pagination: Pagination{Size: 100}})
	assert.EqualValues(t, 10, archived["total"])
	paged := query(ctx, Query{Pagination: Pagination{Size: 2}})
	assert.Len(t, paged["nodes"], 2)
	assert.Greater(t, paged["unloadedRelations"].(int64), int64(0))
	search := query(ctx, Query{Pagination: Pagination{Size: 100, Search: "Temperature"}})
	assert.EqualValues(t, 1, search["total"])
	_, err = service.Query(ctx, "graph", Queries{Queries: []Query{{QueryID: "too-big", Pagination: Pagination{Size: 101}}}})
	assert.Error(t, err)
	// Permission changes cannot expose the existence of hidden relations in counts.
	public := query(graphContext("uns.read"), Query{Kinds: []string{"collection"}, Pagination: Pagination{Size: 100}})
	assert.EqualValues(t, 1, public["unloadedRelations"])
}

func TestGraphSchemaWhitelist(t *testing.T) {
	row := repo.UnsMeta{Kind: "collection", ID: "safe", Data: json.RawMessage(`{"recordSchema":{"type":"object","default":{"password":"SECRET"},"properties":{"value":{"type":"number","description":"SECRET"}}}}`)}
	encoded, err := json.Marshal(graphProjection(row))
	assert.NoError(t, err)
	assert.NotContains(t, string(encoded), "SECRET")
	assert.Contains(t, string(encoded), "number")
}

func TestGraphAllKindsFocusPaginationAndJobOwnership(t *testing.T) {
	nativeEdgeTestProduct(t)
	store, _ := testkit.NewTestDB(t)
	assert.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: "task", ID: "private-request-grant", Data: json.RawMessage(`{"type":"streamRequestGrant","code":"private.operation"}`), Revision: 1}).Error)
	for _, kind := range Kinds {
		raw, _ := json.Marshal(map[string]any{"name": kind})
		assert.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: kind, ID: "same", Data: raw, Revision: 1}).Error)
	}
	for i := 0; i < 7; i++ {
		raw, _ := json.Marshal(map[string]any{"name": fmt.Sprint("Child ", i), "parentId": "same"})
		assert.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: "topic", ID: fmt.Sprint(i), Data: raw, Revision: 1}).Error)
	}
	for _, owner := range []int64{1, 2} {
		raw, _ := json.Marshal(map[string]any{"type": "apiBatchJob", "ownerId": owner})
		assert.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: "task", ID: fmt.Sprint(owner), Data: raw, Revision: 1}).Error)
	}
	ctx := graphContext("uns.read", "uns.source", "uns.stream", "uns.app.read")
	seen := map[string]bool{}
	for page := 1; page <= 4; page++ {
		out, err := graphQuery(ctx, Query{Mode: "focus", Targets: []Target{{Kind: "directory", ID: "same"}}, Pagination: Pagination{Size: 3, Page: page}})
		assert.NoError(t, err)
		assert.LessOrEqual(t, len(out["nodes"].([]map[string]any)), 3)
		for _, node := range out["nodes"].([]map[string]any) {
			seen[node["key"].(string)] = true
		}
	}
	assert.Len(t, seen, 8)
	out, err := graphQuery(ctx, Query{Pagination: Pagination{Size: 100}})
	assert.NoError(t, err)
	kinds := map[string]bool{}
	for _, node := range out["nodes"].([]map[string]any) {
		kinds[node["kind"].(string)] = true
		assert.NotEqual(t, "task:2", node["key"])
		assert.NotEqual(t, "task:private-request-grant", node["key"])
	}
	assert.Len(t, kinds, len(Kinds))
	_, err = graphQuery(ctx, Query{Mode: "focus", Targets: []Target{{Kind: "task", ID: "2"}}})
	assert.Error(t, err)
	_, err = graphQuery(ctx, Query{Mode: "focus", Targets: []Target{{Kind: "task", ID: "private-request-grant"}}})
	assert.Error(t, err)
}
