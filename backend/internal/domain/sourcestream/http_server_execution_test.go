package sourcestream_test

import (
	"backend/internal/testkit"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"

	domain "backend/internal/domain/sourcestream"
	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func freeHTTPSourcePool(t *testing.T) (int, int) {
	t.Helper()
	for port := 41000; port < 42000; port += 2 {
		first, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		second, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port+1))
		_ = first.Close()
		if err != nil {
			continue
		}
		_ = second.Close()
		return port, port + 1
	}
	t.Fatal("unable to reserve a two-port HTTP fixture pool")
	return 0, 0
}

// Real TCP listeners, PostgreSQL, isolated executor child processes and UNS
// transactions. No NATS or simulated HTTP response is used in this test.
func TestHTTPServerActualExecutorIndependentPortsRoutesAndUNS(t *testing.T) {
	first, last := freeHTTPSourcePool(t)
	t.Setenv("SOURCE_HTTP_ENABLED", "true")
	t.Setenv("SOURCE_HTTP_PORT_RANGE_START", strconv.Itoa(first))
	t.Setenv("SOURCE_HTTP_PORT_RANGE_END", strconv.Itoa(last))
	s, store, _, ctx := actualProgramService(t)
	t.Cleanup(s.Close)
	s.SetAsyncReady(func() bool { return false })
	r := repo.NewSourceStreamRepo(store)
	create := func(name string) repo.Source {
		view, err := s.CreateSource(ctx, domain.SourceDefinition{
			Name: name, Protocol: domain.HTTP, Role: domain.ServerRole,
			Endpoint: "http://managed", RuntimeLocation: "local",
			HTTP: &domain.HTTPConnection{Anonymous: true},
		}, 0)
		require.NoError(t, err)
		id := view["id"].(int64)
		_, err = s.Probe(ctx, id, nil, 1000)
		require.NoError(t, err)
		_, err = testkit.SetLegacySourceState(ctx, s, id, view["revision"].(int64), "enabled")
		require.NoError(t, err)
		source, err := r.Source(ctx, id)
		require.NoError(t, err)
		return source
	}
	a, b := create("HTTP fixture A"), create("HTTP fixture B")
	topic := actualProgramTopic(t, ctx, store, "HTTP webhook output")
	code := func(id int64, reply int) string {
		return fmt.Sprintf(`module.exports=api=>{
		 const server=api.sources.use({id:%q,resources:[]});
		 const output=api.uns.use({topicId:%q});
		 api.stream.onRequest({id:'orders',source:server,method:'POST',path:'/orders/:id'},async ctx=>{
		  await output.write({value:ctx.request.body.value});
		  return {status:201,headers:{'X-Version':%q},body:{id:ctx.request.params.id,value:ctx.request.body.value,reply:%d,headers:ctx.request.headers}};
		 });
		};`, strconv.FormatInt(id, 10), strconv.FormatInt(topic, 10), strconv.Itoa(reply), reply)
	}
	sequence := 0
	deploy := func(id, revision int64, script string) map[string]any {
		sequence++
		document := actualProgramDocument(script)
		document["name"] = fmt.Sprintf("HTTP fixture Stream %d", sequence)
		view, err := s.SaveStream(ctx, id, revision, document)
		require.NoError(t, err)
		view, err = s.Deploy(ctx, view["id"].(int64), view["revision"].(int64), 0, 0)
		require.NoError(t, err)
		return view
	}
	viewA := deploy(0, 0, code(a.ID, 1))
	_ = deploy(0, 0, code(b.ID, 2))
	var defA, defB domain.SourceDefinition
	require.NoError(t, json.Unmarshal([]byte(a.Document), &defA))
	require.NoError(t, json.Unmarshal([]byte(b.Document), &defB))
	require.NotEqual(t, defA.HTTP.Port, defB.HTTP.Port)
	call := func(port int, path string) (*http.Response, []byte) {
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), strings.NewReader(`{"value":7}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer not-trusted-user-identity")
		req.Header.Set("X-Forwarded-For", "evil-proxy-header")
		response, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		return response, body
	}
	for _, fixture := range []struct{ port, reply int }{{defA.HTTP.Port, 1}, {defB.HTTP.Port, 2}} {
		response, raw := call(fixture.port, "/orders/42?q=a&q=b")
		require.Equal(t, 201, response.StatusCode, string(raw))
		var result map[string]any
		require.NoError(t, json.Unmarshal(raw, &result))
		require.Equal(t, "42", result["id"])
		require.Equal(t, float64(fixture.reply), result["reply"])
		require.NotContains(t, string(raw), "evil-proxy-header")
		require.NotContains(t, string(raw), "not-trusted-user-identity")
	}
	actualProgramOutboxCount(t, store, 2)
	var runs []repo.StreamRun
	require.NoError(t, store.UnsDB().Where("model='program'").Find(&runs).Error)
	require.Len(t, runs, 2)
	for _, run := range runs {
		require.Equal(t, "succeeded", run.Status)
		require.True(t, run.SourceID == a.ID || run.SourceID == b.ID)
		var summary map[string]any
		require.NoError(t, json.Unmarshal([]byte(run.Summary), &summary))
		require.Equal(t, false, summary["usesNATS"])
	}
	// A second Stream on the same Source may add a disjoint route, not shadow it.
	extra := fmt.Sprintf(`module.exports=api=>{const server=api.sources.use({id:%q,resources:[]});api.stream.onRequest({id:'health',source:server,method:'GET',path:'/health'},()=>({status:200,body:'ok'}));};`, strconv.FormatInt(a.ID, 10))
	_ = deploy(0, 0, extra)
	ambiguous := strings.Replace(extra, "path:'/health'", "path:'/orders/new'", 1)
	ambiguous = strings.Replace(ambiguous, "method:'GET'", "method:'POST'", 1)
	ambiguousDocument := actualProgramDocument(ambiguous)
	ambiguousDocument["name"] = "HTTP ambiguous route fixture"
	draft, err := s.SaveStream(ctx, 0, 0, ambiguousDocument)
	require.NoError(t, err)
	_, err = s.Deploy(ctx, draft["id"].(int64), draft["revision"].(int64), 0, 0)
	require.ErrorIs(t, err, repo.ErrConflict)
	oldVersion := viewA["activeVersion"].(int64)
	viewA = deploy(viewA["id"].(int64), viewA["revision"].(int64), code(a.ID, 3))
	response, raw := call(defA.HTTP.Port, "/orders/42")
	require.Equal(t, 201, response.StatusCode, string(raw))
	var updated struct {
		Reply int `json:"reply"`
	}
	require.NoError(t, json.Unmarshal(raw, &updated))
	require.Equal(t, 3, updated.Reply)
	viewA, err = s.Deploy(ctx, viewA["id"].(int64), viewA["revision"].(int64), 0, oldVersion)
	require.NoError(t, err)
	response, raw = call(defA.HTTP.Port, "/orders/42")
	require.Equal(t, 201, response.StatusCode, string(raw))
	require.NoError(t, json.Unmarshal(raw, &updated))
	require.Equal(t, 1, updated.Reply)
	impact, err := s.Impact(ctx, a.ID)
	require.NoError(t, err)
	latest, err := r.Source(ctx, a.ID)
	require.NoError(t, err)
	_, err = s.DeleteSource(ctx, a.ID, latest.Revision, impact["impactRevision"].(string))
	require.NoError(t, err)
	require.NoError(t, s.ReconcileHTTPServers(ctx))
	require.Eventually(t, func() bool {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", defA.HTTP.Port))
		if err == nil {
			_ = conn.Close()
		}
		return err != nil
	}, 3e9, 2e7)
	impact, err = s.Impact(ctx, a.ID)
	require.ErrorIs(t, err, repo.ErrNotFound)
	_, err = r.Source(ctx, a.ID)
	require.ErrorIs(t, err, repo.ErrNotFound)
	require.NoError(t, s.ReconcileHTTPServers(ctx))
	response, raw = call(defB.HTTP.Port, "/orders/42")
	require.Equal(t, 201, response.StatusCode, string(raw))
	require.NoError(t, json.Unmarshal(raw, &updated))
	require.Equal(t, 2, updated.Reply)
}
