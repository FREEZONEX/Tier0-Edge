package sourcestream_test

// Real PostgreSQL and the isolated executor process exercise the canonical SDK
// across deploy, commit, event matching, checkpoint and immutable binding replay.
import (
	"encoding/json"
	"fmt"
	"testing"

	"backend/internal/domain/dataingest"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

// 已提交业务数据但任务完成失败时，恢复必须复用冻结结果而非再次推进游标。
func TestProgramV2CanonicalCommittedTaskFinalizationRecovery(t *testing.T) {
	for _, checkpoint := range []bool{false, true} {
		name := "without-checkpoint"
		if checkpoint {
			name = "with-checkpoint"
		}
		t.Run(name, func(t *testing.T) {
			s, store, _, ctx := actualProgramService(t)
			schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
			created, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Finalization recovery", RecordSchema: schema, SampleSchema: schema})
			require.NoError(t, err)
			collectionID := created["collection"].(repo.UnsMeta).ID
			topic, err := unsmeta.New().Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Finalization recovery view", CollectionID: collectionID})
			require.NoError(t, err)
			topicID := topic.ID
			cursorCode := ""
			if checkpoint {
				cursorCode = `const previous=await api.stream.checkpoint.get(); await api.stream.checkpoint.set({z:{b:2,a:1},ids:['r'],page:previous===null?1:previous.page+1});`
			}
			code := fmt.Sprintf(`module.exports=api=>{const target=api.uns.use({topicId:%q,access:'write'});api.stream.onRequest({id:'run'},async()=>{await target.write({recordId:'r',record:{value:1e21},sample:{value:1e21}});%s return {marker:Math.random()};});};`, topicID, cursorCode)
			view, err := s.SaveStream(ctx, 0, 0, actualProgramDocument(code))
			require.NoError(t, err)
			id := view["id"].(int64)
			view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
			require.NoError(t, err)
			version := view["activeVersion"].(int64)
			require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION public.reject_program_finish() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'finalization fixture'; END $$; CREATE TRIGGER reject_program_finish BEFORE UPDATE OF status ON public.uns_stream_task FOR EACH ROW WHEN(NEW.status='succeeded') EXECUTE FUNCTION public.reject_program_finish()`).Error)
			_, err = s.InvokeEntry(ctx, id, version, "run", "commit-before-finish", nil, 3000)
			require.Error(t, err)
			r := repo.NewSourceStreamRepo(ctx)
			runs, total, err := r.Runs(ctx, repo.StreamRunFilter{StreamID: id})
			require.NoError(t, err)
			require.EqualValues(t, 1, total)
			task, err := r.Task(ctx, runs[0].ID)
			require.NoError(t, err)
			require.Equal(t, "pending", task.Status)
			frozen := task.Output
			record, err := repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "r")
			require.NoError(t, err)
			require.EqualValues(t, 1, record.Revision)
			var samples, events int64
			require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", collectionID).Count(&samples).Error)
			require.EqualValues(t, 1, samples)
			require.NoError(t, store.UnsDB().Table("public.uns_metadata_event_outbox").Count(&events).Error)
			require.Zero(t, events, "event Outbox is retired")
			actualProgramOutboxCount(t, store, 2)
			before, err := r.ProgramCheckpoint(ctx, id, "run")
			require.NoError(t, err)
			if checkpoint {
				require.EqualValues(t, 1, before.Revision)
			} else {
				require.Zero(t, before.Revision)
			}
			require.NoError(t, store.UnsDB().Exec(`DROP TRIGGER reject_program_finish ON public.uns_stream_task; DROP FUNCTION public.reject_program_finish()`).Error)
			result, err := s.InvokeEntry(ctx, id, version, "run", "commit-before-finish", nil, 3000)
			require.NoError(t, err)
			require.True(t, result["replayed"].(bool))
			require.False(t, result["usesNATS"].(bool))
			task, err = r.Task(ctx, task.ID)
			require.NoError(t, err)
			require.Equal(t, "succeeded", task.Status)
			require.Equal(t, frozen, task.Output)
			run, err := r.Run(ctx, task.ID)
			require.NoError(t, err)
			require.Equal(t, "succeeded", run.Status)
			after, err := r.ProgramCheckpoint(ctx, id, "run")
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", collectionID).Count(&samples).Error)
			require.EqualValues(t, 1, samples)
			var afterEvents int64
			require.NoError(t, store.UnsDB().Table("public.uns_metadata_event_outbox").Count(&afterEvents).Error)
			require.Equal(t, events, afterEvents)
		})
	}
}

func TestProgramV2CanonicalWriteRevisionPayload(t *testing.T) {
	s, store, _, ctx := actualProgramService(t)
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
	created, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Revision output", RecordSchema: schema})
	require.NoError(t, err)
	collectionID := created["collection"].(repo.UnsMeta).ID
	for _, revision := range []string{"null", "-1", "1.5", `'1'`, "true", "[]", "{}", "9007199254740992", "9223372036854775808"} {
		t.Run(revision, func(t *testing.T) {
			code := fmt.Sprintf(`module.exports=api=>{const c=api.uns.use({collectionId:%q,access:'write'});api.stream.onRequest({id:'run'},async()=>{await c.write({recordId:'r',record:{value:1},expectedRevision:%s});});};`, collectionID, revision)
			_, err := s.TestEntry(ctx, actualProgramDocument(code), map[string]any{"data": map[string]any{}}, 0, "input", "run")
			require.Error(t, err)
		})
	}
	for _, revision := range []string{"0", "1", "9007199254740991"} {
		code := fmt.Sprintf(`module.exports=api=>{const c=api.uns.use({collectionId:%q,access:'write'});api.stream.onRequest({id:'run'},async()=>{await c.write({recordId:'r',record:{value:1},expectedRevision:%s});});};`, collectionID, revision)
		_, err := s.TestEntry(ctx, actualProgramDocument(code), map[string]any{"data": map[string]any{}}, 0, "input", "run")
		require.NoError(t, err)
	}
	code := fmt.Sprintf(`module.exports=api=>{const c=api.uns.use({collectionId:%q,access:'read-write'});api.stream.onRequest({id:'run'},async ctx=>{await c.write({recordId:'r',record:{value:ctx.input.value},expectedRevision:ctx.input.revision});return null;});};`, collectionID)
	doc := actualProgramDocument(code)
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	id := view["id"].(int64)
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	version := view["activeVersion"].(int64)
	_, err = s.InvokeEntry(ctx, id, version, "run", "revision-create", map[string]any{"value": 1, "revision": 0}, 3000)
	require.NoError(t, err)
	_, err = s.InvokeEntry(ctx, id, version, "run", "revision-exact", map[string]any{"value": 2, "revision": 1}, 3000)
	require.NoError(t, err)
	_, err = s.InvokeEntry(ctx, id, version, "run", "revision-stale", map[string]any{"value": 2, "revision": 1}, 3000)
	require.ErrorIs(t, err, repo.ErrConflict)
	row, err := repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "r")
	require.NoError(t, err)
	require.EqualValues(t, 2, row.Revision)
	var tasks []repo.StreamTask
	require.NoError(t, store.UnsDB().Where("stream_id=?", id).Find(&tasks).Error)
	require.Len(t, tasks, 3)
	for _, task := range tasks {
		var output streamexec.ProgramResult
		require.NoError(t, json.Unmarshal([]byte(task.Output), &output))
		require.Len(t, output.Outputs, 1)
		var value map[string]any
		require.NoError(t, json.Unmarshal(output.Outputs[0].Data, &value))
		require.Contains(t, value, "expectedRevision")
	}
}

func TestProgramV2CanonicalActualExecutorAndEvent(t *testing.T) {
	s, store, sink, ctx := actualProgramService(t)
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
	created, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Canonical output", RecordSchema: schema, SampleSchema: schema})
	require.NoError(t, err)
	collectionID := created["collection"].(repo.UnsMeta).ID
	topic, err := unsmeta.New().Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Canonical output view", CollectionID: collectionID})
	require.NoError(t, err)
	topicID := topic.ID
	input, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Canonical event input", RecordSchema: schema})
	require.NoError(t, err)
	inputCollection := input["collection"].(repo.UnsMeta).ID
	inputView, err := unsmeta.New().Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Canonical event input view", CollectionID: inputCollection})
	require.NoError(t, err)
	inputTopic := inputView.ID
	code := fmt.Sprintf(`module.exports=api=>{
 const t=api.uns.use({topicId:%q,access:'read-write'});
 const c=api.uns.use({collectionId:%q,access:'read-write'});
 const input=api.uns.use({topicId:%q,access:'read'});
 const write=async ctx=>{
  const previous=await api.stream.checkpoint.get();
  const value=ctx.input.record?ctx.input.record.value:ctx.input.value;
  await t.write({recordId:'temperature',record:{value:value},sample:{value:value}});
  await c.write({recordId:'pressure',record:{value:3},sample:{value:3}});
  await api.stream.checkpoint.set({page:previous===null?1:(previous.page||0)+1});
  return {topicId:ctx.topicId||null,value:value};
 };
 api.stream.onRequest({id:'run'},write);
 api.stream.onRequest({id:'read'},async()=>({topic:await t.read(),collection:await c.read({recordId:'temperature',limit:1})}));
 api.stream.onTopic({id:'event',topics:[input]},write);
};`, topicID, collectionID, inputTopic)
	doc := actualProgramDocument(code)
	preview, err := s.TestEntry(ctx, doc, map[string]any{"data": map[string]any{"value": 7}, "checkpointSample": map[string]any{}}, 0, "input", "run")
	require.NoError(t, err)
	require.NotNil(t, preview["targets"])
	var count int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Count(&count).Error)
	require.Zero(t, count, "dry run must not write records")
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	id := view["id"].(int64)
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	versionID := view["activeVersion"].(int64)
	result, err := s.InvokeEntry(ctx, id, versionID, "run", "canonical-sync", map[string]any{"value": 7}, 3000)
	require.NoError(t, err)
	require.False(t, result["usesNATS"].(bool))
	record, err := repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "temperature")
	require.NoError(t, err)
	require.JSONEq(t, `{"value":7}`, string(record.Data))
	cursor, err := repo.NewSourceStreamRepo(ctx).ProgramCheckpoint(ctx, id, "run")
	require.NoError(t, err)
	require.EqualValues(t, 1, cursor.Revision)
	require.JSONEq(t, `{"page":1}`, string(cursor.Value))
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	result, err = s.InvokeEntry(ctx, id, versionID, "run", "canonical-sync", map[string]any{"value": 7}, 3000)
	require.NoError(t, err)
	require.True(t, result["replayed"].(bool))
	result, err = s.InvokeEntry(ctx, id, versionID, "read", "canonical-read", nil, 3000)
	require.NoError(t, err)
	readData := result["result"].(map[string]any)["data"].(map[string]any)
	require.Equal(t, topicID, readData["topic"].(map[string]any)["topicId"])
	require.Equal(t, collectionID, readData["collection"].(map[string]any)["collectionId"])

	_, err = unsmeta.New().SaveRecord(ctx, inputCollection, "input-record", 0, map[string]any{"value": 19})
	require.NoError(t, err)
	var event dataingest.StreamEvent
	for _, raw := range programEventMessages(t, store) {
		var candidate dataingest.StreamEvent
		require.NoError(t, json.Unmarshal(raw, &candidate))
		if candidate.MetadataTopicID == inputTopic {
			event = candidate
			break
		}
	}
	require.Equal(t, inputTopic, event.MetadataTopicID, "native metadata write must emit records Topic event")
	require.NoError(t, s.MatchProgramEvent(ctx, event, sink))
	record, err = repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "temperature")
	require.NoError(t, err)
	require.JSONEq(t, `{"value":19}`, string(record.Data))
	eventCursor, err := repo.NewSourceStreamRepo(ctx).ProgramCheckpoint(ctx, id, "event")
	require.NoError(t, err)
	require.EqualValues(t, 1, eventCursor.Revision)
	require.NoError(t, s.MatchProgramEvent(ctx, event, sink))
	eventCursor, err = repo.NewSourceStreamRepo(ctx).ProgramCheckpoint(ctx, id, "event")
	require.NoError(t, err)
	require.EqualValues(t, 1, eventCursor.Revision)

	field, err := unsmeta.New().Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Read-only field", ViewMode: "fields", Fields: []unsmeta.FieldBinding{{Name: "value", Kind: "record", CollectionID: collectionID, RecordID: "temperature", FieldPath: "value"}}})
	require.NoError(t, err)
	fieldDoc := actualProgramDocument(fmt.Sprintf(`module.exports=api=>{const f=api.uns.use({topicId:%q,access:'read'});api.stream.onTopic({id:'field-event',topics:[f]},ctx=>ctx.input);};`, field.ID))
	_, err = s.ValidationView(ctx, fieldDoc, 0)
	require.Error(t, err, "unsupported fields-event trigger must not deploy silently")
	fieldDoc["script"].(map[string]any)["code"] = fmt.Sprintf(`module.exports=api=>{const f=api.uns.use({topicId:%q,access:'read'});api.stream.onRequest({id:'field-read'},()=>f.read());};`, field.ID)
	_, err = s.ValidationView(ctx, fieldDoc, 0)
	require.NoError(t, err, "fields Topic remains readable")

	other, err := unsmeta.New().Save(ctx, "", "collection", 0, unsmeta.Document{Name: "Retarget", RecordSchema: schema})
	require.NoError(t, err)
	_, err = unsmeta.New().Save(ctx, topicID, "topic", 1, unsmeta.Document{Name: "Canonical output view", CollectionID: other.ID})
	require.NoError(t, err)
	_, err = s.InvokeEntry(ctx, id, versionID, "run", "canonical-retarget", map[string]any{"value": 99}, 3000)
	require.Error(t, err, "old deployment must reject a changed Topic binding")
	result, err = s.InvokeEntry(ctx, id, versionID, "run", "canonical-sync", map[string]any{"value": 7}, 3000)
	require.NoError(t, err, "completed replay must not repeat effects after binding changes")
	require.True(t, result["replayed"].(bool))
}
