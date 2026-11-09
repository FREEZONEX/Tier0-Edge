package sourcestream_test

// 真实 PG 和隔离执行器验证非 UUID Meta 的读、查、写及事件匹配，不用成功 Mock。
import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"backend/internal/domain/dataingest"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func TestProgramV2NamedMetadataActualExecution(t *testing.T) {
	s, store, sink, ctx := actualProgramService(t)
	u := unsmeta.New()
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
	for _, id := range []string{"plant0-input", "plant0-output"} {
		_, err := u.Save(ctx, id, "collection", 0, unsmeta.Document{Name: "Collection " + id, RecordSchema: schema, SampleSchema: schema})
		require.NoError(t, err)
	}
	_, err := u.Save(ctx, "plant0-input-topic", "topic", 0, unsmeta.Document{Name: "Identity input", CollectionID: "plant0-input", ViewMode: "records"})
	require.NoError(t, err)
	_, err = u.Save(ctx, "79", "topic", 0, unsmeta.Document{Name: "Numeric Meta output", CollectionID: "plant0-output", ViewMode: "records"})
	require.NoError(t, err)
	code := `module.exports=api=>{
 const input=api.uns.use({topicId:'plant0-input-topic',access:'read'});
 const output=api.uns.use({topicId:'79',space:'metadata',access:'read-write'});
 const collection=api.uns.use({collectionId:'plant0-output',access:'read-write'});
 const write=async ctx=>{
  await output.write({recordId:'named',record:{value:ctx.input.value},sample:{value:ctx.input.value},quality:'good'});
  await collection.write({recordId:'collection',record:{value:ctx.input.value+1}});
  return {accepted:true};
 };
 api.stream.onRequest({id:'run'},write);
 api.stream.onRequest({id:'read'},async()=>({topic:await output.read(),page:await collection.query({kind:'record',limit:10})}));
 api.stream.onTopic({id:'event',topics:[input]},write);
};`
	view, err := s.SaveStream(ctx, 0, 0, actualProgramDocument(code))
	require.NoError(t, err)
	id := view["id"].(int64)
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	version := view["activeVersion"].(int64)
	_, err = s.InvokeEntry(ctx, id, version, "run", "named-first", map[string]any{"value": 7}, 3000)
	require.NoError(t, err)
	read, err := s.InvokeEntry(ctx, id, version, "read", "named-read", map[string]any{}, 3000)
	require.NoError(t, err)
	readData := read["result"].(map[string]any)["data"].(map[string]any)
	require.NotNil(t, readData["topic"])
	require.Len(t, readData["page"].(map[string]any)["items"], 2)
	check := func(value int, sampleCount int64) {
		row, err := repo.NewUnsMetaRepo(ctx).Record(ctx, "plant0-output", "named")
		require.NoError(t, err)
		var data map[string]any
		require.NoError(t, json.Unmarshal(row.Data, &data))
		require.EqualValues(t, value, data["value"])
		var count int64
		require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", "plant0-output").Count(&count).Error)
		require.Equal(t, sampleCount, count)
	}
	check(7, 1)
	event := dataingest.StreamEvent{MessageID: "named-actual-event", Origin: "metadata", MetadataTopicID: "plant0-input-topic", CollectionID: "plant0-input", ReceivedAt: time.Now(), Data: map[string]any{"value": 9}}
	require.NoError(t, s.MatchProgramEvent(context.Background(), event, sink))
	check(9, 2)
	require.NoError(t, s.MatchProgramEvent(context.Background(), event, sink))
	check(9, 2)
	// A declared Meta envelope must never fall back to a same-number legacy Topic.
	invalid := event
	invalid.MetadataTopicID, invalid.CollectionID, invalid.TopicID = "", "", 79
	require.ErrorIs(t, s.MatchProgramEvent(context.Background(), invalid, sink), repo.ErrInvalidArgument)
	check(9, 2)
}
