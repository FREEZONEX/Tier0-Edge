package unsv2_test

// 验证组合查询共享聚合后，分页/空页、精确数字和响应上限仍保持原契约。
import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"backend/internal/contextx"
	"backend/internal/domain/unsv2"
	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func TestComposedPageCountPrecisionAndBudget(t *testing.T) {
	a := newAppAPI(t)
	a.provision()
	for _, id := range []string{"a", "b", "c"} {
		a.apply(a.admin, 200, order(id, 1, false))
	}
	for i, amount := range []json.Number{"9007199254740993", "2"} {
		a.apply(a.admin, 200, object{"opId": "fact", "action": "record.create", "collectionId": "private", "id": []string{"one", "two"}[i], "data": object{"title": "a", "priority": amount, "done": false}})
	}
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: map[string]struct{}{"uns.read": {}}})
	query := func(page, size int, having string, wide, descending bool) (map[string]any, error) {
		selects := []object{{"field": "base.id", "as": "id"}, {"field": "facts.total", "as": "total"}, {"field": "facts.total", "as": "duplicate"}}
		if wide {
			selects = append(selects, object{"field": "base.data.note", "as": "note"})
		}
		body := object{"collectionId": "orders", "page": page, "size": size,
			"subqueries": []object{{"name": "totals", "collectionId": "private", "groupBy": []string{"base.data.title"}, "select": []object{{"field": "base.data.title", "as": "customer"}, {"field": "base.data.priority", "aggregate": "sum", "as": "total"}}}},
			"joins":      []object{{"subquery": "totals", "alias": "facts", "type": "left", "on": []object{{"left": "base.id", "right": "facts.customer"}}}}, "select": selects, "orderBy": []object{{"field": "id", "direction": "asc"}}}
		if having != "" {
			body["having"] = []object{{"field": "total", "op": having, "value": json.Number("9007199254740995")}}
		}
		if descending {
			body["orderBy"] = []object{{"field": "total", "direction": "desc"}, {"field": "id", "direction": "desc"}}
		}
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		var request unsv2.RecordQueryRequest
		require.NoError(t, unsv2.Decode(raw, &request))
		if having != "" {
			require.Equal(t, "9007199254740995", string(request.Having[0].Value))
		}
		result, err := a.svc.App.UnsV2.RecordQuery(ctx, "recordAggregate", request)
		if err != nil {
			return nil, err
		}
		return result.(map[string]any), nil
	}
	first, err := query(1, 2, "", false, false)
	require.NoError(t, err)
	require.Equal(t, int64(3), first["total"])
	list := first["list"].([]map[string]any)
	require.Len(t, list, 2)
	require.Equal(t, "a", list[0]["id"])
	require.Equal(t, json.Number("9007199254740995"), list[0]["total"])
	require.Equal(t, list[0]["total"], list[0]["duplicate"])
	require.Nil(t, list[1]["total"])
	last, err := query(2, 2, "", false, false)
	require.NoError(t, err)
	require.Equal(t, "c", last["list"].([]map[string]any)[0]["id"])
	empty, err := query(3, 2, "", false, false)
	require.NoError(t, err)
	require.Equal(t, int64(3), empty["total"])
	require.Empty(t, empty["list"])
	filtered, err := query(1, 2, "gt", false, false)
	require.NoError(t, err)
	require.Equal(t, int64(0), filtered["total"])
	require.Empty(t, filtered["list"])
	// Equality above 2^53 must not round the input condition to a neighboring integer.
	exact, err := query(1, 2, "eq", false, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), exact["total"])
	require.Equal(t, "a", exact["list"].([]map[string]any)[0]["id"])
	a.apply(a.admin, 200, object{"opId": "fact-b", "action": "record.create", "collectionId": "private", "id": "three", "data": object{"title": "b", "priority": json.Number("9007199254740995"), "done": false}})
	// Equal aggregate values use an explicit descending ID tie-breaker across pages.
	descFirst, err := query(1, 1, "gte", false, true)
	require.NoError(t, err)
	require.Equal(t, int64(2), descFirst["total"])
	require.Equal(t, "b", descFirst["list"].([]map[string]any)[0]["id"])
	descLast, err := query(2, 1, "gte", false, true)
	require.NoError(t, err)
	require.Equal(t, int64(2), descLast["total"])
	require.Equal(t, "a", descLast["list"].([]map[string]any)[0]["id"])
	repeated, err := query(1, 1, "gte", false, true)
	require.NoError(t, err)
	require.Equal(t, descFirst["list"], repeated["list"])
	// Each record is below the individual limit; the projected page exceeds 8 MiB.
	data, err := json.Marshal(object{"title": "wide", "priority": 1, "done": false, "note": strings.Repeat("x", 50000)})
	require.NoError(t, err)
	require.NoError(t, repo.GetUnsConn(ctx).Exec("INSERT INTO public.uns_record(collection_id,id,data,created_at,updated_at) SELECT 'orders','wide-' || n,?::jsonb,1,1 FROM generate_series(1,200) n", string(data)).Error)
	_, err = query(1, 200, "", true, false)
	var fault *unsv2.Fault
	require.ErrorAs(t, err, &fault)
	require.Equal(t, "RESPONSE_TOO_LARGE", fault.Code)
}
