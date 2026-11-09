package sourcestream_test

// 真实PG + 独立执行器子进程：Worker/NATS缺失时同步配置、发布、读写DB及重放仍可用。
import (
	"encoding/json"
	"fmt"
	"testing"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"

	"github.com/stretchr/testify/require"
)

func TestCapabilityActualSyncDBWithoutWorkerOrPublisher(t *testing.T) {
	s, store, _, ctx := actualProgramService(t)
	s.SetAsyncReady(func() bool { return false })
	// The helper captures events for unrelated tests. This case deliberately
	// removes that transport from this isolated Store to model absent NATS.
	delete(store.UnsDB().Config.Plugins, "uns-direct-resource-events")
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
	created, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Sync without worker", RecordSchema: schema})
	require.NoError(t, err)
	collectionID := created["collection"].(repo.UnsMeta).ID
	code := fmt.Sprintf(`module.exports=api=>{
 const c=api.uns.use({collectionId:%q,access:'read-write'});
 api.stream.onRequest({id:'write'},async ctx=>{await c.write({recordId:'r',record:{value:ctx.input.value}});return {value:ctx.input.value};});
 api.stream.onRequest({id:'read'},()=>c.read({recordId:'r',limit:1}));
 };`, collectionID)
	view, err := s.SaveStream(ctx, 0, 0, actualProgramDocument(code))
	require.NoError(t, err)
	id := view["id"].(int64)
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	version := view["activeVersion"].(int64)
	for _, entry := range []string{"write", "read"} {
		result, err := s.InvokeEntry(ctx, id, version, entry, "sync-"+entry, map[string]any{"value": 7}, 3000)
		require.NoError(t, err)
		require.False(t, result["usesNATS"].(bool))
		encoded, err := json.Marshal(result["result"])
		require.NoError(t, err)
		require.Contains(t, string(encoded), `"value":7`)
	}
	record, err := repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "r")
	require.NoError(t, err)
	require.EqualValues(t, 1, record.Revision)
	result, err := s.InvokeEntry(ctx, id, version, "write", "sync-write", map[string]any{"value": 7}, 3000)
	require.NoError(t, err)
	require.True(t, result["replayed"].(bool))
	record, err = repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "r")
	require.NoError(t, err)
	require.EqualValues(t, 1, record.Revision)
	require.False(t, s.CapabilityReadiness().AsyncWorker)
}
