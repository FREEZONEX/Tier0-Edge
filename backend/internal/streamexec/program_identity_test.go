package streamexec

// 验证 Meta 合法身份贯穿 SDK；同号新旧资源和不同类型的引用键不能碰撞。
import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgramMetadataIdentityContract(t *testing.T) {
	code := `module.exports=api=>{
 const legacy=api.uns.use({topicId:'79'});
 const numeric=api.uns.use({topicId:'79',space:'metadata',access:'read-write'});
 const named=api.uns.use({topicId:'plant0-equipment-state-topic',access:'read-write'});
 const collection=api.uns.use({collectionId:'plant0-equipment-state',access:'read-write'});
 api.stream.onTopic({id:'changed',topics:[numeric,named]},ctx=>ctx.input);
 api.stream.onRequest({id:'run'},async()=>{
  const old=await legacy.read();
  const current=await numeric.read();
  await collection.query({kind:'record',limit:1});
  await numeric.write({recordId:'one',record:{value:7}});
  await collection.write({recordId:'two',record:{value:8}});
  return {old,current};
 });
};`
	request := programRequest(code, "run")
	request.CheckOnly = true
	compiled, err := ExecuteProgram(context.Background(), request, nil)
	require.NoError(t, err)
	require.Len(t, compiled.Manifest.Topics, 4)
	require.NotEqual(t, compiled.Manifest.Topics[0].Key(), compiled.Manifest.Topics[1].Key())
	require.Len(t, compiled.Manifest.Entries[0].TopicIDs, 2)
	require.Equal(t, compiled.Manifest.Topics[1].Key(), compiled.Manifest.Entries[0].TopicIDs[0])
	require.True(t, compiled.Manifest.MatchesTopicReference(compiled.Manifest.Topics[1].Key(), "79", true))
	require.False(t, compiled.Manifest.MatchesTopicReference(compiled.Manifest.Topics[1].Key(), "79", false))
	require.True(t, compiled.Manifest.MatchesTopicReference(compiled.Manifest.Topics[0].Key(), "79", false))
	require.False(t, compiled.Manifest.MatchesTopicReference(compiled.Manifest.Topics[0].Key(), "79", true))
	request.CheckOnly, request.ExpectedManifest = false, &compiled.Manifest
	calls := []ProgramCall{}
	result, err := ExecuteProgram(context.Background(), request, func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
		calls = append(calls, call)
		return json.RawMessage(`{"items":[],"value":7}`), nil
	})
	require.NoError(t, err)
	require.Equal(t, compiled.Manifest, result.Manifest)
	require.Len(t, calls, 3)
	require.Len(t, result.Outputs, 2)
	var wire map[string]any
	raw, err := json.Marshal(calls[1])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &wire))
	require.Equal(t, "metadata", wire["space"], "IPC must preserve the declared resource space")
	raw, err = json.Marshal(result.Outputs[0])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &wire))
	require.Equal(t, "metadata", wire["space"], "buffered writes must retain the same authority")
	metadataOnly := compiled.Manifest
	metadataOnly.Topics = metadataOnly.Topics[1:]
	require.False(t, AllowsProgramCall(metadataOnly, ProgramCall{ID: 1, Kind: "uns.read", TopicID: "79"}), "metadata authority never permits the legacy numeric namespace")
	for _, kind := range []string{"uns.read", "uns.query", "checkpoint.get", "source.request"} {
		var forged ProgramCall
		require.NoError(t, json.Unmarshal([]byte(`{"id":1,"kind":"`+kind+`","topicId":"79","space":"unknown","options":{}}`), &forged))
		require.False(t, AllowsProgramCall(compiled.Manifest, forged), kind)
	}
}

func TestProgramMetadataIdentityKeysAndSpaces(t *testing.T) {
	code := `module.exports=api=>{
 api.uns.use({collectionId:'pump',access:'read'});
 api.uns.use({topicId:'collection:pump',access:'read'});
 api.uns.use({topicId:'79',space:'metadata',access:'read'});
 api.uns.use({topicId:'metadata-topic:79',access:'read'});
 api.stream.onRequest({id:'run'},()=>1);
};`
	result, err := ExecuteProgram(context.Background(), programRequest(code, "run"), nil)
	require.NoError(t, err)
	keys := map[string]bool{}
	for _, ref := range result.Manifest.Topics {
		require.False(t, keys[ref.Key()])
		keys[ref.Key()] = true
	}
	for _, declaration := range []string{
		`{topicId:'79',space:'unknown',access:'read'}`,
		`{collectionId:'pump',space:'legacy',access:'read'}`,
		`{topicId:'pump',space:'legacy',access:'read'}`,
		`{topicId:'pump/*',space:'metadata',access:'read'}`,
	} {
		_, err := ExecuteProgram(context.Background(), programRequest(`module.exports=api=>{api.uns.use(`+declaration+`);api.stream.onRequest({id:'run'},()=>1);};`, "run"), nil)
		require.Error(t, err, declaration)
	}
}

func TestProgramMetadataIdentityNormalization(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	code := `module.exports=api=>{
 api.uns.use({topicId:'` + id + `',access:'read'});
 api.uns.use({topicId:'` + id + `',space:'metadata',access:'read'});
 api.uns.use({topicId:'79',space:'legacy'});
 api.uns.use({topicId:'79'});
 api.stream.onRequest({id:'run'},()=>1);
};`
	result, err := ExecuteProgram(context.Background(), programRequest(code, "run"), nil)
	require.NoError(t, err)
	require.Len(t, result.Manifest.Topics, 2)
	require.Equal(t, id, result.Manifest.Topics[0].Key())
	require.Empty(t, result.Manifest.Topics[0].Space)
	require.Empty(t, result.Manifest.Topics[1].Space)
}
