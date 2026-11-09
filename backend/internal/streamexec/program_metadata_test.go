package streamexec

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgramCanonicalUNSReferencesAndAccess(t *testing.T) {
	const topic = "11111111-1111-4111-8111-111111111111"
	const collection = "22222222-2222-4222-8222-222222222222"
	code := fmt.Sprintf(`module.exports=api=>{
 const t=api.uns.use({topicId:%q,access:'read-write'});
 const c=api.uns.use({collectionId:%q,access:'write'});
 api.stream.onTopic({id:'event',topics:[t]},ctx=>ctx.input);
 api.stream.onRequest({id:'run'},async ctx=>{
  const current=await t.read();
  await t.write({recordId:'temperature',record:{value:ctx.input.value}});
  await c.write({recordId:'pressure',record:{value:3}});
  return current;
 });
};`, topic, collection)
	request := programRequest(code, "run")
	request.CheckOnly = true
	compiled, err := ExecuteProgram(context.Background(), request, nil)
	require.NoError(t, err)
	require.Len(t, compiled.Manifest.Topics, 2)
	request.CheckOnly, request.ExpectedManifest = false, &compiled.Manifest
	result, err := ExecuteProgram(context.Background(), request, func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
		require.Equal(t, "uns.read", call.Kind)
		require.Equal(t, topic, call.TopicID)
		require.Empty(t, call.CollectionID)
		return json.RawMessage(`{"records":[]}`), nil
	})
	require.NoError(t, err)
	require.Equal(t, compiled.Manifest, result.Manifest)
	require.Len(t, result.Outputs, 2)
	require.Equal(t, topic, result.Outputs[0].TopicID)
	require.Equal(t, collection, result.Outputs[1].CollectionID)
	require.Empty(t, result.Outputs[1].TopicID)
	require.True(t, AllowsProgramCall(compiled.Manifest, ProgramCall{ID: 1, Kind: "uns.read", TopicID: topic}))
	require.False(t, AllowsProgramCall(compiled.Manifest, ProgramCall{ID: 1, Kind: "uns.read", CollectionID: collection}))
	require.False(t, AllowsProgramCall(compiled.Manifest, ProgramCall{ID: 1, Kind: "checkpoint.get", CollectionID: collection}))

	for _, declaration := range []string{
		fmt.Sprintf(`{topicId:%q}`, topic),
		fmt.Sprintf(`{topicId:%q,collectionId:%q,access:'read'}`, topic, collection),
		`{topicId:'00000000-0000-0000-0000-000000000000',access:'read'}`,
		`{topicId:'11111111-1111-4111-8111-11111111111A',access:'read'}`,
	} {
		_, err := ExecuteProgram(context.Background(), programRequest(`module.exports=api=>{api.uns.use(`+declaration+`);api.stream.onRequest({id:'run'},()=>1);};`, "run"), nil)
		require.Error(t, err, declaration)
	}
	for _, body := range []string{
		fmt.Sprintf(`const t=api.uns.use({topicId:%q,access:'read'});api.stream.onRequest({id:'run'},()=>t.write({}));`, topic),
		fmt.Sprintf(`const t=api.uns.use({topicId:%q,access:'write'});api.stream.onRequest({id:'run'},()=>t.read());`, topic),
		fmt.Sprintf(`const t=api.uns.use({collectionId:%q,access:'read'});api.stream.onTopic({id:'run',topics:[t]},()=>1);`, collection),
		fmt.Sprintf(`const t=api.uns.use({topicId:%q,access:'write'});api.stream.onTopic({id:'run',topics:[t]},()=>1);`, topic),
		fmt.Sprintf(`api.uns.use({topicId:%q,access:'read'});api.uns.use({topicId:%q,access:'write'});api.stream.onRequest({id:'run'},()=>1);`, topic, topic),
	} {
		_, err := ExecuteProgram(context.Background(), programRequest(`module.exports=api=>{`+body+`};`, "run"), nil)
		require.Error(t, err, body)
	}
	manifest := ProgramManifest{Sources: []ProgramSource{{ID: "1", Resources: []string{}}}}
	require.False(t, AllowsProgramCall(manifest, ProgramCall{ID: 1, Kind: "source.request", SourceID: "1", CollectionID: collection}))
}
