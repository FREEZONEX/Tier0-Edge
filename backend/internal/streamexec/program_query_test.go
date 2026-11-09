package streamexec

// 正式 Goja 执行查询句柄，验证声明、调用预算、错误及分页合同。
import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgramQuerySDKAndAuthority(t *testing.T) {
	const id = "22222222-2222-4222-8222-222222222222"
	code := fmt.Sprintf(`module.exports=api=>{const c=api.uns.use({collectionId:%q,access:'read'});api.stream.onRequest({id:'run'},async()=>{const page=await c.query({kind:'sample',timeRange:{from:1000,to:2000},limit:1});return page;});};`, id)
	result, err := ExecuteProgram(context.Background(), programRequest(code, "run"), func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
		require.Equal(t, "uns.query", call.Kind)
		require.Equal(t, id, call.CollectionID)
		require.JSONEq(t, `{"kind":"sample","timeRange":{"from":1000,"to":2000},"limit":1}`, string(call.Options))
		return json.RawMessage(`{"items":[{"id":"sample-1","time":1000,"quality":"good"}],"nextCursor":"","hasMore":false}`), nil
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"items":[{"id":"sample-1","time":1000,"quality":"good"}],"nextCursor":"","hasMore":false}`, string(result.Data))
	call := ProgramCall{ID: 1, Kind: "uns.query", CollectionID: id, Options: json.RawMessage(`{"kind":"record"}`)}
	require.True(t, AllowsProgramCall(result.Manifest, call))
	for _, mutate := range []func(*ProgramCall){func(c *ProgramCall) { c.Options = nil }, func(c *ProgramCall) { c.SourceID = "1" }, func(c *ProgramCall) { c.CollectionID = "33333333-3333-4333-8333-333333333333" }, func(c *ProgramCall) { c.Input = json.RawMessage(`{}`) }} {
		invalid := call
		mutate(&invalid)
		require.False(t, AllowsProgramCall(result.Manifest, invalid))
	}
	for _, body := range []string{`c.query()`, `c.query(null)`, `c.query({kind:'record'},1)`} {
		_, err = ExecuteProgram(context.Background(), programRequest(fmt.Sprintf(`module.exports=api=>{const c=api.uns.use({collectionId:%q,access:'read'});api.stream.onRequest({id:'run'},()=>%s);};`, id, body), "run"), nil)
		require.Error(t, err)
	}
	_, err = ExecuteProgram(context.Background(), programRequest(fmt.Sprintf(`module.exports=api=>{const c=api.uns.use({collectionId:%q,access:'write'});api.stream.onRequest({id:'run'},()=>c.query({kind:'record'}));};`, id), "run"), nil)
	require.Error(t, err)
	_, err = ExecuteProgram(context.Background(), programRequest(fmt.Sprintf(`module.exports=api=>{const c=api.uns.use({collectionId:%q,access:'read'});api.stream.onRequest({id:'run'},async()=>{for(let i=0;i<65;i++)await c.query({kind:'record'});});};`, id), "run"), func(context.Context, ProgramCall) (json.RawMessage, error) {
		return json.RawMessage(`{"items":[],"nextCursor":"","hasMore":false}`), nil
	})
	require.Error(t, err, "query cannot bypass the 64-call budget")
}
