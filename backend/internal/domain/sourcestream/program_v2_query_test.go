package sourcestream_test

// 正式隔离执行器、真实 PostgreSQL 验证查询、模拟无写入、分页及事务恢复。
import (
	"encoding/json"
	"fmt"
	"testing"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func TestProgramV2QueryActualExecutorAndCheckpoint(t *testing.T) {
	s, store, _, ctx := actualProgramService(t)
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
	input, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Query input", RecordSchema: schema, SampleSchema: schema})
	require.NoError(t, err)
	id := input["collection"].(repo.UnsMeta).ID
	output, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Query output", RecordSchema: schema, SampleSchema: schema})
	require.NoError(t, err)
	target := output["collection"].(repo.UnsMeta).ID
	for _, item := range []string{"a", "b"} {
		_, err = unsmeta.New().SaveRecord(ctx, id, item, 0, map[string]any{"value": 7})
		require.NoError(t, err)
	}
	code := fmt.Sprintf(`module.exports=api=>{
 const input=api.uns.use({collectionId:%q,access:'read'});
 const output=api.uns.use({collectionId:%q,access:'write'});
 api.stream.onRequest({id:'page'},async ctx=>{
  const progress=await api.stream.checkpoint.get();
  const options={kind:'record',limit:1};
  if(progress&&progress.cursor)options.cursor=progress.cursor;
  const page=await input.query(options);
  for(const item of page.items)await output.write({recordId:item.id,record:{value:ctx.input.bad?'invalid':item.data.value},sample:{value:item.data.value}});
  await api.stream.checkpoint.set({cursor:page.nextCursor,complete:!page.hasMore});
  return page;
 });
};`, id, target)
	doc := actualProgramDocument(code)
	fixture := map[string]any{"data": map[string]any{}, "checkpointSample": nil, "topicSamples": map[string]any{
		"collection:" + id: map[string]any{"queries": []any{map[string]any{"options": map[string]any{"kind": "record", "limit": 1}, "result": map[string]any{"items": []any{map[string]any{"id": "mock", "data": map[string]any{"value": 9}}}, "nextCursor": "", "hasMore": false}}}},
	}}
	preview, err := s.TestEntry(ctx, doc, fixture, 0, "input", "page")
	require.NoError(t, err)
	require.NotEmpty(t, preview["result"].(streamexec.ProgramResult).Data)
	var count int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Where("collection_id=?", target).Count(&count).Error)
	require.Zero(t, count, "dry-run query cannot write output")
	_, err = s.TestEntry(ctx, doc, map[string]any{"data": map[string]any{}}, 0, "input", "page")
	require.Error(t, err, "missing mock must not query live input")
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	streamID := view["id"].(int64)
	view, err = s.Deploy(ctx, streamID, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	version := view["activeVersion"].(int64)
	_, err = s.InvokeEntry(ctx, streamID, version, "page", "query-invalid", map[string]any{"bad": true}, 3000)
	require.Error(t, err)
	progress, err := repo.NewSourceStreamRepo(ctx).ProgramCheckpoint(ctx, streamID, "page")
	require.NoError(t, err)
	require.Zero(t, progress.Revision, "failed output must not advance page cursor")
	for i := 0; i < 2; i++ {
		_, err = s.InvokeEntry(ctx, streamID, version, "page", fmt.Sprintf("query-page-%d", i), map[string]any{}, 3000)
		require.NoError(t, err)
	}
	progress, err = repo.NewSourceStreamRepo(ctx).ProgramCheckpoint(ctx, streamID, "page")
	require.NoError(t, err)
	require.EqualValues(t, 2, progress.Revision)
	var saved map[string]any
	require.NoError(t, json.Unmarshal(progress.Value, &saved))
	require.Equal(t, true, saved["complete"])
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", target).Count(&count).Error)
	require.EqualValues(t, 2, count)
	_, err = s.InvokeEntry(ctx, streamID, version, "page", "query-page-1", map[string]any{}, 3000)
	require.NoError(t, err)
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", target).Count(&count).Error)
	require.EqualValues(t, 2, count, "idempotent replay cannot append duplicate history")
}
