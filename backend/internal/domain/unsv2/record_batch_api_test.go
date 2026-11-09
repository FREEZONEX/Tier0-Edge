package unsv2_test

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRecordQueryBatchHTTPAppScope(t *testing.T) {
	a := newAppAPI(t)
	key, _ := a.provision()
	a.apply(key, 200, order("first", 1, false))
	out := a.post(key, "records:queryBatch", object{"queries": []object{{"id": "allowed", "op": "get", "collectionId": "orders", "ids": []string{"first"}}, {"id": "denied", "op": "count", "collectionId": "private"}, {"id": "invalid", "op": "count", "collectionId": "orders", "unknown": true}}}, 200)
	results := out["results"].(map[string]any)
	require.Equal(t, true, results["allowed"].(map[string]any)["ok"])
	require.Equal(t, "FORBIDDEN", results["denied"].(map[string]any)["error"].(map[string]any)["code"])
	require.Equal(t, false, results["invalid"].(map[string]any)["ok"])
}
