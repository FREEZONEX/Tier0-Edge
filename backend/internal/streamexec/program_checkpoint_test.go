package streamexec

// 检查点只在处理阶段调用，JSON 大小、引用边界和异步顺序受限。
import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgramCheckpointSDKCallsAndBounds(t *testing.T) {
	code := `module.exports=api=>{api.stream.onRequest({id:"poll"},async()=>{const cursor=await api.stream.checkpoint.get();await api.stream.checkpoint.set({page:cursor===null?1:cursor.page+1});return await api.stream.checkpoint.get();});};`
	var stored json.RawMessage = json.RawMessage(`null`)
	calls := 0
	result, err := ExecuteProgram(context.Background(), programRequest(code, "poll"), func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
		calls++
		require.True(t, AllowsProgramCall(ProgramManifest{Entries: []ProgramEntry{{ID: "poll"}}}, call))
		if call.Kind == "checkpoint.get" {
			return stored, nil
		}
		stored = call.Input
		return json.RawMessage(`{"stage":"buffered"}`), nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, calls)
	require.JSONEq(t, `{"page":1}`, string(result.Data))
	require.Nil(t, result.Checkpoint, "executor cannot author trusted cursor CAS metadata")
	for _, body := range []string{
		`api.stream.checkpoint.get(1)`, `api.stream.checkpoint.set()`, `api.stream.checkpoint.set(undefined)`, `api.stream.checkpoint.set("x".repeat(16384))`,
	} {
		_, err := ExecuteProgram(context.Background(), programRequest(`module.exports=api=>{api.stream.onRequest({id:"r"},async()=>{await `+body+`;});};`, "r"), func(context.Context, ProgramCall) (json.RawMessage, error) {
			t.Fatal("invalid call escaped")
			return nil, nil
		})
		require.Error(t, err, body)
	}
	_, err = ExecuteProgram(context.Background(), programRequest(`module.exports=api=>{api.stream.checkpoint.get();api.stream.onRequest({id:"r"},()=>1);};`, ""), nil)
	require.Error(t, err)
}

func TestProgramCheckpointCallAuthority(t *testing.T) {
	manifest := ProgramManifest{Entries: []ProgramEntry{{ID: "poll"}}}
	valid := ProgramCall{ID: 1, Kind: "checkpoint.set", Input: json.RawMessage(`null`)}
	require.True(t, AllowsProgramCall(manifest, valid))
	for _, change := range []func(*ProgramCall){
		func(c *ProgramCall) { c.SourceID = "1" }, func(c *ProgramCall) { c.TopicID = "1" }, func(c *ProgramCall) { c.ResourceRef = "forged" }, func(c *ProgramCall) { c.Options = json.RawMessage(`{}`) }, func(c *ProgramCall) { c.Input = nil }, func(c *ProgramCall) { c.Input = json.RawMessage(`bad`) },
	} {
		invalid := valid
		change(&invalid)
		require.False(t, AllowsProgramCall(manifest, invalid))
	}
	require.False(t, AllowsProgramCall(ProgramManifest{}, valid))
}
