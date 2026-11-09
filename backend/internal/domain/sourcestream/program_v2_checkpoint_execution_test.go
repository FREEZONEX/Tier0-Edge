package sourcestream_test

// 独立执行器子进程与真实 PG 验证检查点、输出、重试及无副作用预览。
import (
	"fmt"
	"strconv"
	"testing"

	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func TestProgramV2ActualCheckpointResumeDryRunAndOutputFailure(t *testing.T) {
	s, store, _, ctx := actualProgramService(t)
	topic := actualProgramTopic(t, ctx, store, "checkpoint actual")
	doc := actualProgramDocument(fmt.Sprintf(`module.exports=api=>{
 const target=api.uns.use({topicId:%q});
 api.stream.onRequest({id:"poll"},async ctx=>{
 const prior=await api.stream.checkpoint.get();const page=(prior===null?0:prior.page)+1;
 await target.write({value:ctx.input.value});await api.stream.checkpoint.set({page});return {page,marker:Math.random()};
 });api.stream.onRequest({id:"other"},async()=>await api.stream.checkpoint.get());
};`, strconv.FormatInt(topic, 10)))
	preview, err := s.TestEntry(ctx, doc, map[string]any{"data": map[string]any{"value": 1}, "checkpointSample": map[string]any{"page": 8}}, 0, "request", "poll")
	require.NoError(t, err)
	require.True(t, preview["dryRun"].(bool))
	actualProgramOutboxCount(t, store, 0)
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	view, err = s.Deploy(ctx, view["id"].(int64), view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	id, version := view["id"].(int64), view["activeVersion"].(int64)
	r := repo.NewSourceStreamRepo(store)
	_, err = s.InvokeEntry(ctx, id, version, "poll", "page-one", map[string]any{"value": 1}, 3000)
	require.NoError(t, err)
	checkpoint, err := r.ProgramCheckpoint(ctx, id, "poll")
	require.NoError(t, err)
	require.JSONEq(t, `{"page":1}`, string(checkpoint.Value))
	_, err = s.InvokeEntry(ctx, id, version, "poll", "page-two", map[string]any{"value": 2}, 3000)
	require.NoError(t, err)
	checkpoint, err = repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, id, "poll")
	require.NoError(t, err)
	require.JSONEq(t, `{"page":2}`, string(checkpoint.Value))
	_, err = s.InvokeEntry(ctx, id, version, "poll", "page-two", map[string]any{"value": 2}, 3000)
	require.NoError(t, err)
	checkpoint, err = r.ProgramCheckpoint(ctx, id, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 2, checkpoint.Revision)
	other, err := r.ProgramCheckpoint(ctx, id, "other")
	require.NoError(t, err)
	require.Zero(t, other.Revision)
	// Fail after the script has completed, then recover the frozen output. A
	// fresh random marker would expose a silently re-evaluated script.
	require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION public.reject_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture'; END $$; CREATE TRIGGER reject_checkpoint BEFORE INSERT ON public.stream_receipt FOR EACH ROW EXECUTE FUNCTION public.reject_checkpoint()`).Error)
	_, err = s.InvokeEntry(ctx, id, version, "poll", "page-three", map[string]any{"value": 3}, 3000)
	require.Error(t, err)
	checkpoint, err = r.ProgramCheckpoint(ctx, id, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 2, checkpoint.Revision)
	runs, total, err := r.Runs(ctx, repo.StreamRunFilter{StreamID: id, Status: "partial-failure"})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	runID := runs[0].ID
	task, err := r.Task(ctx, runID)
	require.NoError(t, err)
	frozen := task.Output
	require.NoError(t, store.UnsDB().Exec(`DROP TRIGGER reject_checkpoint ON public.stream_receipt`).Error)
	_, err = s.InvokeEntry(ctx, id, version, "poll", "page-three", map[string]any{"value": 3}, 3000)
	require.NoError(t, err)
	task, err = r.Task(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, frozen, task.Output)
	checkpoint, err = r.ProgramCheckpoint(ctx, id, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 3, checkpoint.Revision)
	actualProgramOutboxCount(t, store, 3)
}
