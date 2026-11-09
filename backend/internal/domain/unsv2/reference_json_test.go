// 验证空JSON容器不破坏引用查询，真实引用与不可见引用的权限边界仍保留。
package unsv2

import (
	"context"
	"encoding/json"
	"testing"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/testkit"
	testrequire "github.com/stretchr/testify/require"
)

func TestReferencesNullableContainers(t *testing.T) {
	nativeEdgeTestProduct(t)
	for _, kind := range []string{"source", "topic", "app"} {
		t.Run(kind, func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			insert := func(kind, id string, data map[string]any) {
				t.Helper()
				raw, err := json.Marshal(data)
				testrequire.NoError(t, err)
				testrequire.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: kind, ID: id, Revision: 1, Data: raw}).Error)
			}
			insert("collection", "referenced", map[string]any{"schemaVersion": 1, "name": "Target", "recordSchema": map[string]any{"type": "object"}})
			insert("collection", "other", map[string]any{"schemaVersion": 1, "name": "Other", "recordSchema": map[string]any{"type": "object"}})
			insert("topic", "topic-ref", map[string]any{"name": "Topic reference", "collectionId": "referenced"})
			insert("source", "source-ref", map[string]any{"name": "Source reference", "credentials": map[string]any{"token": "TEST_ONLY_SECRET"}, "config": map[string]any{"streams": map[string]any{
				"binding": map[string]any{"target": map[string]any{"collectionId": "referenced"}},
			}}})
			insert("app", "app-ref", map[string]any{"name": "App reference", "collections": []any{map[string]any{"collectionId": "referenced"}}})
			nullable := map[string]map[string]any{
				"source": {"name": "Unused source", "config": map[string]any{"streams": nil}},
				"topic":  {"name": "Unused topic", "collectionId": "other", "fields": nil},
				"app":    {"name": "Unused App", "collections": nil},
			}
			insert(kind, "nullable", nullable[kind])
			actor := func(keys ...string) context.Context {
				resources := map[string]struct{}{}
				for _, key := range keys {
					resources[key] = struct{}{}
				}
				return contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: resources})
			}
			query := Query{Targets: []Target{{Kind: "collection", ID: "referenced"}}, Pagination: Pagination{Page: 1, Size: 50}}
			full, err := referencesQuery(actor("uns.read", "uns.source", "uns.app.read"), query)
			testrequire.NoError(t, err)
			target := full["targets"].([]map[string]any)[0]
			testrequire.EqualValues(t, 3, target["references"].(map[string]any)["total"])
			testrequire.Equal(t, false, target["hasHiddenReferences"])
			encoded, err := json.Marshal(full)
			testrequire.NoError(t, err)
			testrequire.NotContains(t, string(encoded), "TEST_ONLY_SECRET")
			limited, err := referencesQuery(actor("uns.read"), query)
			testrequire.NoError(t, err)
			target = limited["targets"].([]map[string]any)[0]
			testrequire.EqualValues(t, 1, target["references"].(map[string]any)["total"])
			testrequire.Equal(t, true, target["hasHiddenReferences"], "hidden references must not become deletable")
			encoded, err = json.Marshal(limited)
			testrequire.NoError(t, err)
			testrequire.NotContains(t, string(encoded), "source-ref")
			testrequire.NotContains(t, string(encoded), "app-ref")
		})
	}
}
