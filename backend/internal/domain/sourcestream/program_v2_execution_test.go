package sourcestream_test

// Actual executor server/children + independent PostgreSQL + UNS transaction
// sink. This tests public domain wiring, not a simulated JavaScript result.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/infra/streamexecutor"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func actualProgramService(t *testing.T) (*domain.Service, *repo.Store, *dataingest.Service, context.Context) {
	t.Helper()
	s, store, sink, ctx := actualUnstartedProgramService(t)
	require.NoError(t, s.StartProgramExecutions(ctx))
	return s, store, sink, ctx
}

func actualUnstartedProgramService(t *testing.T) (*domain.Service, *repo.Store, *dataingest.Service, context.Context) {
	t.Helper()
	store, dsn := testkit.NewTestDB(t)
	dir := t.TempDir()
	binary := filepath.Join(dir, "executor")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	out, err := exec.Command("go", "build", "-buildvcs=false", "-o", binary, "../../../cmd/stream-executor").CombinedOutput()
	require.NoError(t, err, string(out))
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{
		"uns.read": {}, "uns.write": {}, "uns.source": {}, "uns.stream.manage": {}, "uns.stream.execute": {},
	}})
	serverCtx, cancel := context.WithCancel(context.Background())
	// Windows AF_UNIX paths must fit sockaddr_un even for long Go test names.
	ipcDir, err := os.MkdirTemp("", "t0-exec-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(ipcDir) })
	socket := filepath.Join(ipcDir, "executor.sock")
	server := exec.CommandContext(serverCtx, binary, "-socket", socket)
	require.NoError(t, server.Start())
	t.Cleanup(func() { cancel(); _ = server.Wait() })
	client := streamexecutor.New(socket)
	require.Eventually(t, func() bool { return client.Ready(ctx) == nil }, 5*time.Second, 25*time.Millisecond)
	sink, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	t.Cleanup(sink.Close)
	require.NoError(t, sink.EnsureStreamStorage(ctx))
	captureProgramEvents(t, store, sink)
	s := domain.New(nil, client, "")
	t.Cleanup(s.Close)
	s.SetAsyncReady(func() bool { return true })
	s.SetProgramUNS(func(ctx context.Context, id int64) (json.RawMessage, error) {
		node, err := repo.NewUnsRepo(ctx).GetUnsNode(ctx, id, false)
		if err != nil {
			return nil, err
		}
		payload, err := sink.LastPayload(ctx, node)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"topicId": strconv.FormatInt(id, 10), "hasData": len(payload) > 0, "data": payload})
	}, sink)
	return s, store, sink, ctx
}

func actualProgramTopic(t *testing.T, ctx context.Context, store *repo.Store, name string) int64 {
	t.Helper()
	node, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{Name: name, Type: 2, TopicType: 3, EnableHistory: 2,
		Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}, nil, nil)
	require.NoError(t, err)
	return node.ID
}

func actualProgramDocument(code string) map[string]any {
	return map[string]any{"name": "Actual complete program", "script": map[string]any{"code": code, "timeoutMs": 3000}}
}

type recordedProgramEvents struct {
	mu       sync.Mutex
	messages []json.RawMessage
}

var programEventCaptures sync.Map // test-only transport, isolated by Store
func captureProgramEvents(t *testing.T, store *repo.Store, sink *dataingest.Service) *recordedProgramEvents {
	t.Helper()
	if existing, ok := programEventCaptures.Load(store); ok {
		return existing.(*recordedProgramEvents)
	}
	capture := &recordedProgramEvents{}
	programEventCaptures.Store(store, capture)
	t.Cleanup(func() { programEventCaptures.Delete(store) })
	publish := func(_ context.Context, _, _ string, raw []byte) error {
		capture.mu.Lock()
		defer capture.mu.Unlock()
		capture.messages = append(capture.messages, append(json.RawMessage(nil), raw...))
		return nil
	}
	require.NoError(t, repo.ConfigureResourceEventPublisher(store.UnsDB(), publish))
	sink.SetResourceEventPublisher(publish)
	return capture
}
func programEventMessages(t *testing.T, store *repo.Store) []json.RawMessage {
	t.Helper()
	value, ok := programEventCaptures.Load(store)
	require.True(t, ok)
	capture := value.(*recordedProgramEvents)
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return append([]json.RawMessage(nil), capture.messages...)
}
func actualProgramOutboxCount(t *testing.T, store *repo.Store, expected int64) {
	t.Helper()
	require.Len(t, programEventMessages(t, store), int(expected), "direct post-commit notifications")
	for _, table := range []string{"public.stream_event_outbox", "public.uns_metadata_event_outbox"} {
		var count int64
		require.NoError(t, store.UnsDB().Table(table).Count(&count).Error)
		require.Zero(t, count, "no persistent notification queue")
	}
}

// 正式子进程已返回结果，但结果Task保存失败：不得留下处理中或重跑脚本。
func TestProgramV2ActualProcessTaskPersistenceFailureClosesAdmission(t *testing.T) {
	s, store, _, ctx := actualProgramService(t)
	topic := actualProgramTopic(t, ctx, store, "uncommitted result")
	doc := actualProgramDocument(fmt.Sprintf(`module.exports=api=>{
	 const output=api.uns.use({topicId:%q});
	 api.stream.onRequest({id:"run"},async()=>{await output.write({value:1});return 1;});
	};`, strconv.FormatInt(topic, 10)))
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	view, err = s.Deploy(ctx, view["id"].(int64), view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	const callback = "test:reject-program-result"
	attempts := 0
	require.NoError(t, store.UnsDB().Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "public.uns_stream_task" {
			attempts++
			tx.AddError(errors.New("test result persistence unavailable"))
		}
	}))
	t.Cleanup(func() { _ = store.UnsDB().Callback().Create().Remove(callback) })
	id, version := view["id"].(int64), view["activeVersion"].(int64)
	_, err = s.InvokeEntry(ctx, id, version, "run", "result-write-failed", nil, 3000)
	require.Error(t, err)
	require.Equal(t, 1, attempts, "failure is injected only after actual script execution")
	runs, total, err := repo.NewSourceStreamRepo(store).Runs(ctx, repo.StreamRunFilter{StreamID: id})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "unknown", runs[0].Status, "failed result persistence must not leave a durable admitted run processing")
	require.Contains(t, runs[0].Summary, "output-not-persisted")
	_, err = s.InvokeEntry(ctx, id, version, "run", "result-write-failed", nil, 3000)
	require.ErrorIs(t, err, domain.ErrUnknownResult)
	require.Equal(t, 1, attempts, "replay must not execute and prepare another result")
	actualProgramOutboxCount(t, store, 0)
}

func TestProgramV2ActualProcessSyncReplayAndDraftIsolation(t *testing.T) {
	s, store, _, ctx := actualProgramService(t)
	topic := actualProgramTopic(t, ctx, store, "actual writes")
	doc := actualProgramDocument(fmt.Sprintf(`module.exports=function(api){
	 const target=api.uns.use({topicId:%q});
	 api.stream.onRequest({id:"echo"},async function(context){
	  await target.write({value:context.input.value});
	  await target.write({value:context.input.value+1});
	  api.log.info("two outputs buffered");return {value:context.input.value*2};
	 });api.stream.onRequest({id:"other"},()=>42);
	};`, strconv.FormatInt(topic, 10)))
	preview, err := s.ValidationView(ctx, doc, 0)
	require.NoError(t, err)
	require.False(t, preview["sideEffects"].(bool))
	actualProgramOutboxCount(t, store, 0)
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	id := view["id"].(int64)
	view, err = s.Deploy(ctx, id, view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	version := view["activeVersion"].(int64)
	_, err = s.Invoke(ctx, id, version, "ambiguous-entry", map[string]any{"value": 7}, 3000)
	require.Error(t, err, "multiple request entries require explicit selection")
	result, err := s.InvokeEntry(ctx, id, version, "echo", "stable-key", map[string]any{"value": 7}, 3000)
	require.NoError(t, err)
	require.False(t, result["usesNATS"].(bool))
	require.False(t, result["replayed"].(bool))
	require.Equal(t, map[string]any{"value": float64(14)}, result["result"].(map[string]any)["data"])
	actualProgramOutboxCount(t, store, 2)
	result, err = s.InvokeEntry(ctx, id, version, "echo", "stable-key", map[string]any{"value": 7}, 3000)
	require.NoError(t, err)
	require.True(t, result["replayed"].(bool))
	actualProgramOutboxCount(t, store, 2)
	_, err = s.InvokeEntry(ctx, id, version, "echo", "stable-key", map[string]any{"value": 99}, 3000)
	require.ErrorIs(t, err, repo.ErrConflict)
	doc["script"] = `module.exports=function(api){api.stream.onRequest({id:"echo"},()=>99);};`
	view, err = s.SaveStream(ctx, id, view["revision"].(int64), doc)
	require.NoError(t, err)
	require.Equal(t, version, view["activeVersion"])
	result, err = s.InvokeEntry(ctx, id, version, "echo", "old-serving-version", map[string]any{"value": 2}, 3000)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"value": float64(4)}, result["result"].(map[string]any)["data"])
	actualProgramOutboxCount(t, store, 4)
}

func TestProgramV2ActualProcessAsyncPartialOutputResume(t *testing.T) {
	s, store, sink, ctx := actualProgramService(t)
	first := actualProgramTopic(t, ctx, store, "first ordered")
	second := actualProgramTopic(t, ctx, store, "second independent")
	doc := actualProgramDocument(fmt.Sprintf(`module.exports=function(api){
	 const first=api.uns.use({topicId:%q});const second=api.uns.use({topicId:%q});
	 api.stream.every({id:"tick",intervalMs:1000},async function(context){
	 await first.write({value:context.input.value});await first.write({value:context.input.value+1});
	 await second.write({value:context.input.value+2});return context.runId;
	 });api.stream.onRequest({id:"fail"},async()=>{await first.write({value:99});throw new Error("script failed");});
	};`, strconv.FormatInt(first, 10), strconv.FormatInt(second, 10)))
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	view, err = s.Deploy(ctx, view["id"].(int64), view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	ref := domain.ExecutionRef{StreamID: view["id"].(int64), VersionID: view["activeVersion"].(int64), EntryID: "tick"}
	raw := domain.RawInput{MessageID: "actual-interval", Trigger: domain.IntervalTrigger, Plans: []domain.ExecutionRef{ref},
		Lineage: []int64{19},
		Input:   domain.SourceInput{ResourceRef: "interval", Data: map[string]any{"value": 7}, ReceivedAt: time.Now().UnixMilli()}}
	failing := &transientTargetSink{actual: sink, failTopic: first}
	require.Error(t, s.ProcessRaw(context.Background(), raw, failing))
	actualProgramOutboxCount(t, store, 1)
	runID := repo.ProgramRunIdentity(raw.MessageID, ref.StreamID, ref.VersionID, ref.EntryID)
	failedView, viewErr := s.Run(ctx, runID)
	require.NoError(t, viewErr)
	require.Equal(t, "partial-failure", failedView["status"])
	require.Equal(t, "SINK_COMMIT_FAILED", failedView["error"].(*domain.RunError).Code)
	task, err := repo.NewSourceStreamRepo(store).Task(ctx, runID)
	require.NoError(t, err)
	frozenOutput := task.Output
	lineage, owned, err := repo.NewSourceStreamRepo(store).ProgramRunLineage(ctx, runID)
	require.NoError(t, err)
	require.True(t, owned)
	require.Equal(t, []int64{19}, lineage)
	raw.Lineage = []int64{999}
	require.NoError(t, s.ProcessRaw(context.Background(), raw, failing))
	actualProgramOutboxCount(t, store, 3)
	var storedLineages []string
	for _, raw := range programEventMessages(t, store) {
		var event map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &event))
		storedLineages = append(storedLineages, string(event["lineage"]))
	}
	require.Len(t, storedLineages, 3)
	for _, recorded := range storedLineages {
		require.JSONEq(t, fmt.Sprintf(`[19,%d]`, ref.StreamID), recorded, "recovery must use admitted provenance, not the latest caller's lineage")
	}
	task, err = repo.NewSourceStreamRepo(store).Task(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, frozenOutput, task.Output)
	require.Equal(t, "succeeded", task.Status)
	recoveredView, viewErr := s.Run(ctx, runID)
	require.NoError(t, viewErr)
	require.NotContains(t, recoveredView, "error")
	require.NotContains(t, recoveredView, "diagnostics")
	require.NoError(t, s.ProcessRaw(context.Background(), raw, failing))
	actualProgramOutboxCount(t, store, 3)
	raw.Input.Data = map[string]any{"value": 8}
	require.Error(t, s.ProcessRaw(context.Background(), raw, failing))
	actualProgramOutboxCount(t, store, 3)
	_, err = s.InvokeEntry(ctx, ref.StreamID, ref.VersionID, "fail", "failed-before-commit", nil, 3000)
	require.Error(t, err)
	actualProgramOutboxCount(t, store, 3)
	_, err = s.InvokeEntry(ctx, ref.StreamID, ref.VersionID, "fail", "failed-before-commit", nil, 3000)
	require.ErrorIs(t, err, domain.ErrExecutionFailed, "a recorded failure remains failed and cannot silently run again")
	require.NotErrorIs(t, err, domain.ErrUnknownResult, "a completed failure is not an uncertain device result")
	actualProgramOutboxCount(t, store, 3)
}

func TestProgramV2RecordedFailureNeverReexecutesOrBecomesUnknown(t *testing.T) {
	s, store, sink, ctx := actualProgramService(t)
	topic := actualProgramTopic(t, ctx, store, "failure output must not commit")
	doc := actualProgramDocument(fmt.Sprintf(`module.exports=function(api){
	 const target=api.uns.use({topicId:%q});
	 api.stream.every({id:"failed",intervalMs:1000},async()=>{
	  await target.write({value:99});throw new Error("private user failure");
	 });
	};`, strconv.FormatInt(topic, 10)))
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	view, err = s.Deploy(ctx, view["id"].(int64), view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	ref := domain.ExecutionRef{StreamID: view["id"].(int64), VersionID: view["activeVersion"].(int64), EntryID: "failed"}
	raw := domain.RawInput{MessageID: "recorded-async-failure", Trigger: domain.IntervalTrigger, Plans: []domain.ExecutionRef{ref},
		Input: domain.SourceInput{ResourceRef: "interval", Data: map[string]any{"value": 1}, ReceivedAt: time.Now().UnixMilli()}}
	err = s.ProcessRaw(ctx, raw, sink)
	require.ErrorIs(t, err, domain.ErrExecutionFailed)
	require.NotErrorIs(t, err, domain.ErrUnknownResult)
	runID := repo.ProgramRunIdentity(raw.MessageID, ref.StreamID, ref.VersionID, ref.EntryID)
	run, err := repo.NewSourceStreamRepo(store).Run(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "failed", run.Status)
	originalSummary := run.Summary
	for _, status := range []string{"failed", "processing", "unknown", "succeeded"} {
		t.Run(status, func(t *testing.T) {
			// Only the fixture alters state: model a crash/uncertain receipt and
			// confirm no missing-task state authorizes another script execution.
			require.NoError(t, store.UnsDB().Model(&repo.StreamRun{}).Where("id=?", runID).Update("status", status).Error)
			for attempt := 0; attempt < 2; attempt++ {
				err := s.ProcessRaw(ctx, raw, sink)
				if status == "failed" {
					require.ErrorIs(t, err, domain.ErrExecutionFailed)
					require.NotErrorIs(t, err, domain.ErrUnknownResult)
				} else {
					require.ErrorIs(t, err, domain.ErrUnknownResult)
					require.NotErrorIs(t, err, domain.ErrExecutionFailed)
				}
			}
			stored, err := repo.NewSourceStreamRepo(store).Run(ctx, runID)
			require.NoError(t, err)
			require.Equal(t, status, stored.Status, "replay did not execute or finalize user code")
			require.Equal(t, originalSummary, stored.Summary)
			var tasks int64
			require.NoError(t, store.UnsDB().Model(&repo.StreamTask{}).Where("id=?", runID).Count(&tasks).Error)
			require.Zero(t, tasks)
			actualProgramOutboxCount(t, store, 0)
		})
	}
}
