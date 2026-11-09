package unsv2_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func TestCloudRecordGetProjectedResponseBudget(t *testing.T) {
	a := newAppAPI(t)
	a.provision()
	data, err := json.Marshal(object{"title": "wide", "priority": 1, "done": false, "note": strings.Repeat("x", 220*1024)})
	require.NoError(t, err)
	require.NoError(t, repo.GetUnsConn(context.Background()).Exec("INSERT INTO public.uns_record(collection_id,id,data,created_at,updated_at) SELECT 'orders','wide-' || n,?::jsonb,1,1 FROM generate_series(1,100) n", string(data)).Error)
	ids := make([]string, 100)
	for i := range ids {
		ids[i] = fmt.Sprintf("wide-%d", 100-i)
	}
	for _, count := range []int{40, 100} {
		a.post(a.admin, "collections/orders/records:get", object{"ids": ids[:count]}, 413)
	}
	for _, projected := range []bool{false, true} {
		count := 30
		body := object{"ids": ids[:count]}
		if projected {
			count = 100
			body["ids"] = ids
			body["projection"] = object{"title": 1}
		}
		out := a.post(a.admin, "collections/orders/records:get", body, 200)
		items := out["items"].([]any)
		require.Len(t, items, count)
		for i, item := range items {
			doc := item.(map[string]any)
			require.Equal(t, ids[i], doc["id"])
			if projected {
				require.NotContains(t, doc["data"], "note")
			}
		}
	}
	out := a.post(a.admin, "collections/orders/records:get", object{"ids": []string{"absent", ids[0]}, "projection": object{"title": 1}}, 200)
	require.Equal(t, []any{"absent"}, out["missing"])
}

func TestCloudMetadataRejectsUnknownUpdateFields(t *testing.T) {
	a := newAppAPI(t)
	a.post(a.admin, "metadata", object{"kind": "directory", "id": "known-fields", "data": object{"name": "Known fields"}}, 200)
	for _, operator := range []string{"$set", "$unset", "$setOnInsert"} {
		result := a.post(a.admin, "metadata:updateOne", object{"kind": "directory", "filter": object{"_id": "known-fields"}, "update": object{operator: object{"unknownField": 1}}}, 422)
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.Contains(t, string(encoded), "unknown metadata field")
	}
}

func TestCloudRecordRestoreSchemaDiagnostic(t *testing.T) {
	a := newAppAPI(t)
	a.provision()
	a.post(a.admin, "records/insert-one", object{"collectionId": "orders", "id": "restore-diagnostic", "data": object{"title": "Restore", "priority": 1, "done": false}}, 200)
	a.post(a.admin, "records/delete-one", object{"collectionId": "orders", "filter": object{"_id": "restore-diagnostic"}}, 200)
	// 模拟历史导入数据与当前 Schema 不匹配；恢复必须说明路径，不能泄漏记录值。
	require.NoError(t, a.svc.App.Store.UnsDB().Exec(`UPDATE public.uns_record SET data=jsonb_set(data, '{priority}', '"sensitive-invalid-value"') WHERE collection_id='orders' AND id='restore-diagnostic'`).Error)
	result := a.post(a.admin, "collections/orders/records:restore", object{"ids": []string{"restore-diagnostic"}}, 422)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "data/priority")
	require.NotContains(t, string(encoded), "sensitive-invalid-value")
}
