package unsquery

// 真实独立 PostgreSQL 验证共享条件、稳定分页、时间边界及查询预算。
import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestProgramQueryPagesAndBoundaries(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}, "label": {Type: "string"}}}
	created, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Query acceptance", RecordSchema: schema, SampleSchema: schema})
	require.NoError(t, err)
	id := created["collection"].(repo.UnsMeta).ID
	bindings, err := repo.FreezeProgramV2Bindings(ctx, streamexec.ProgramManifest{Topics: []streamexec.ProgramTopic{{CollectionID: id, Access: "read"}}})
	require.NoError(t, err)
	require.NoError(t, ValidateProgramCollection(ctx, id, ProgramOptions{Kind: "record"}, bindings[0]))
	wrongBinding := bindings[0]
	wrongBinding.TopicBindingHash = "changed"
	_, err = QueryProgramCollection(ctx, id, ProgramOptions{Kind: "record"}, wrongBinding)
	require.ErrorIs(t, err, repo.ErrConflict)
	for _, options := range []ProgramOptions{
		{Kind: "record", Filters: []RecordFilter{{Field: "data.unknown", Op: "eq", Value: json.RawMessage(`1`)}}},
		{Kind: "record", Filters: []RecordFilter{{Field: "data.value", Op: "sql", Value: json.RawMessage(`1`)}}},
		{Kind: "record", OrderBy: []RecordOrder{{Field: "data.value", Direction: "sideways"}}},
	} {
		require.Error(t, ValidateProgramCollection(ctx, id, options, bindings[0]), "previews must validate field/operator/schema contracts")
	}
	for _, row := range []struct {
		id   string
		data map[string]any
	}{{"a", map[string]any{"value": 2}}, {"b", map[string]any{"value": 2}}, {"c", map[string]any{"value": 1}}, {"d", map[string]any{}}, {"e", map[string]any{}}} {
		_, err := unsmeta.New().SaveRecord(ctx, id, row.id, 0, row.data)
		require.NoError(t, err)
	}
	for _, direction := range []string{"asc", "desc"} {
		options := ProgramOptions{Kind: "record", Limit: 1, OrderBy: []RecordOrder{{Field: "data.value", Direction: direction}}}
		ids := []string{}
		for i := 0; i < 6; i++ {
			page, err := QueryProgramCollection(ctx, id, options)
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			ids = append(ids, page.Items[0].(repo.UnsRecord).ID)
			if !page.HasMore {
				require.Empty(t, page.NextCursor)
				break
			}
			options.Cursor = page.NextCursor
		}
		want := []string{"c", "a", "b", "d", "e"}
		if direction == "desc" {
			want = []string{"a", "b", "c", "d", "e"}
		}
		require.Equal(t, want, ids)
		// Changing either resource or conditions invalidates the cursor.
		_, err := QueryProgramCollection(ctx, "other", options)
		require.Error(t, err)
		options.Limit = 2
		_, err = QueryProgramCollection(ctx, id, options)
		require.Error(t, err)
	}
	page, err := QueryProgramCollection(ctx, id, ProgramOptions{Kind: "record", Filters: []RecordFilter{{Field: "data.value", Op: "gte", Value: json.RawMessage(`2`)}}})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.False(t, page.HasMore)
	boundOptions := ProgramOptions{Kind: "record", Limit: 1, ResourceKey: "topic:first"}
	boundPage, err := QueryProgramCollection(ctx, id, boundOptions)
	require.NoError(t, err)
	boundOptions.Cursor, boundOptions.ResourceKey = boundPage.NextCursor, "topic:second"
	_, err = QueryProgramCollection(ctx, id, boundOptions)
	require.Error(t, err, "two Topic handles to the same Collection cannot share cursors")
	for _, field := range []string{"data.unknown", "data.value);DROP TABLE public.uns_record;--"} {
		_, err = QueryProgramCollection(ctx, id, ProgramOptions{Kind: "record", Filters: []RecordFilter{{Field: field, Op: "eq", Value: json.RawMessage(`1`)}}})
		require.Error(t, err)
	}
	samples := []unsmeta.SampleInput{{ID: "start", RecordID: "a", Time: 1000, Quality: "bad", Data: map[string]any{"value": 3}}, {ID: "same", RecordID: "b", Time: 1000, Quality: "good", Data: map[string]any{"value": 4}}, {ID: "end", RecordID: "a", Time: 2000, Data: map[string]any{"value": 5}}}
	_, err = unsmeta.New().AppendSamples(ctx, id, samples)
	require.NoError(t, err)
	options := ProgramOptions{Kind: "sample", Limit: 1, TimeRange: &TimeRange{From: 1000, To: 2000}}
	page, err = QueryProgramCollection(ctx, id, options)
	require.NoError(t, err)
	require.True(t, page.HasMore)
	require.Equal(t, "same", page.Items[0].(map[string]any)["id"])
	options.Cursor = page.NextCursor
	page, err = QueryProgramCollection(ctx, id, options)
	require.NoError(t, err)
	require.False(t, page.HasMore)
	require.Equal(t, "start", page.Items[0].(map[string]any)["id"])
	require.EqualValues(t, 1000, page.Items[0].(map[string]any)["time"])
	require.Equal(t, "bad", page.Items[0].(map[string]any)["quality"])
	options.Cursor = ""
	options.RecordIDs = []string{"a"}
	page, err = QueryProgramCollection(ctx, id, options)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.False(t, page.HasMore)
	var count int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Count(&count).Error)
	require.EqualValues(t, 3, count, "queries never modify sample history")
	for _, suffix := range []string{"a", "b"} {
		_, err = unsmeta.New().SaveRecord(ctx, id, "cursor-"+suffix, 0, map[string]any{"label": strings.Repeat("z", 7000) + suffix})
		require.NoError(t, err)
	}
	_, err = QueryProgramCollection(ctx, id, ProgramOptions{Kind: "record", Limit: 1, RecordIDs: []string{"cursor-a", "cursor-b"}, OrderBy: []RecordOrder{{Field: "data.label", Direction: "asc"}}})
	require.ErrorIs(t, err, unsmeta.ErrData, "never return a cursor too large for the next request")
	for _, suffix := range []string{"x", "y"} {
		_, err = unsmeta.New().SaveRecord(ctx, id, "big-"+suffix, 0, map[string]any{"value": 1, "text": strings.Repeat("x", 140000)})
		require.NoError(t, err)
	}
	_, err = QueryProgramCollection(ctx, id, ProgramOptions{Kind: "record"})
	require.ErrorIs(t, err, unsmeta.ErrData, "aggregate payload must fail, not truncate")
}

func TestProgramQueryOptionValidation(t *testing.T) {
	for _, options := range []ProgramOptions{{Kind: "sql"}, {Kind: "record", Limit: 101}, {Kind: "record", Limit: -1}, {Kind: "sample"}, {Kind: "sample", TimeRange: &TimeRange{From: 1, To: 1}}, {Kind: "sample", TimeRange: &TimeRange{From: -1, To: 1}}, {Kind: "record", RecordIDs: []string{"bad/id"}}, {Kind: "record", RecordIDs: []string{"a", "a"}}, {Kind: "record", Cursor: "garbage"}, {Kind: "record", TimeRange: &TimeRange{From: 0, To: 1}}} {
		require.Error(t, ValidateProgramOptions("collection", options))
	}
	require.NoError(t, ValidateProgramOptions("collection", ProgramOptions{Kind: "record"}))
}
