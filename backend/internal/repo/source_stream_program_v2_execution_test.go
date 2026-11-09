package repo_test

// 验证先登记运行意图再执行，未激活候选不可运行，输出重试不覆盖结果。
import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProgramV2FirstTaskReturnsFrozenDatabaseOutput(t *testing.T) {
	for _, tc := range []struct{ name, output string }{
		{"plain", `{"outputs":[{"topicId":"10","data":{"value":26}}]}`},
		{"nested-checkpoint", `{"outputs":[],"checkpoint":{"revision":0,"value":{"z":{"b":2,"a":1},"ids":["x"],"a":0}}}`},
		{"precision", `{"outputs":[],"data":{"large":9007199254740993123,"amount":12345678901234567890.12345678901234567890,"exponent":1e2}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			r, ctx := repo.NewSourceStreamRepo(store), context.Background()
			manifest := v2RequestManifest()
			doc := `{"script":"run"}`
			stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "freeze-probe", Model: "program", Document: doc}, 0, manifest)
			require.NoError(t, err)
			stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{})
			require.NoError(t, err)
			_, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
			require.NoError(t, err)
			run := repo.StreamRun{ID: "freeze-probe", StreamID: stream.ID, VersionID: version.ID, EntryID: "request", Model: "program", Status: "processing", Summary: `{"requestHash":"stable"}`, CreatedAt: time.Now().UnixMilli()}
			created, err := r.AdmitProgramV2Run(ctx, run, true)
			require.NoError(t, err)
			require.True(t, created)
			task := repo.StreamTask{ID: run.ID, StreamID: run.StreamID, VersionID: run.VersionID, EntryID: run.EntryID, Model: "program", Output: tc.output, ReceivedAt: time.Now().UnixMilli()}
			first, err := r.PutProgramV2Task(ctx, task, run)
			require.NoError(t, err)
			stored, err := r.Task(ctx, task.ID)
			require.NoError(t, err)
			second, err := r.PutProgramV2Task(ctx, task, run)
			require.NoError(t, err)
			require.Equal(t, stored, second, "existing task always returns the frozen row")
			recovered, found, err := r.ResolveProgramV2PreparationFailure(ctx, run)
			require.NoError(t, err)
			require.True(t, found, "an uncertain commit response must recover the actual durable result")
			require.Equal(t, stored, recovered)
			unchanged, err := r.Run(ctx, run.ID)
			require.NoError(t, err)
			require.Equal(t, "processing", unchanged.Status, "recoverable output is not finalized as unknown")
			changedIdentity := run
			changedIdentity.Summary = `{"requestHash":"different"}`
			_, _, err = r.ResolveProgramV2PreparationFailure(ctx, changedIdentity)
			require.ErrorIs(t, err, repo.ErrConflict)
			require.Equal(t, first.ID, second.ID)
			require.Equal(t, first.CreatedAt, second.CreatedAt)
			if tc.name == "precision" {
				var data struct {
					Data map[string]json.Number `json:"data"`
				}
				decoder := json.NewDecoder(bytes.NewBufferString(stored.Output))
				decoder.UseNumber()
				require.NoError(t, decoder.Decode(&data))
				require.Equal(t, "9007199254740993123", data.Data["large"].String())
				require.Equal(t, "12345678901234567890.12345678901234567890", data.Data["amount"].String())
				require.Equal(t, "100", data.Data["exponent"].String())
			}
			require.Equal(t, stored.Output, first.Output, "initial execution must use the persisted output, not an alternate in-memory representation")
		})
	}
}

// 停用已持锁阻止新结果落库；没有持久化结果的已登记运行必须明确未知，
// 不能永远 processing，也不能更改已有结果、成功记录或其他 Stream。
func TestProgramV2DisableFinalizesOnlyUnpreparedAdmissions(t *testing.T) {
	testProgramV2LifecycleFinalizesOnlyUnpreparedAdmissions(t, false)
}

func TestProgramV2DeleteFinalizesOnlyUnpreparedAdmissions(t *testing.T) {
	testProgramV2LifecycleFinalizesOnlyUnpreparedAdmissions(t, true)
}

func testProgramV2LifecycleFinalizesOnlyUnpreparedAdmissions(t *testing.T, remove bool) {
	store, _ := testkit.NewTestDB(t)
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	create := func(name string) (repo.Stream, repo.StreamVersion) {
		doc := `{"script":"run"}`
		manifest := v2RequestManifest()
		stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: name, Model: "program", Document: doc}, 0, manifest)
		require.NoError(t, err)
		stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{})
		require.NoError(t, err)
		stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
		require.NoError(t, err)
		return stream, version
	}
	stream, version := create("interrupted")
	other, otherVersion := create("unaffected")
	admit := func(id string, target repo.Stream, active repo.StreamVersion) repo.StreamRun {
		run := repo.StreamRun{ID: id, StreamID: target.ID, VersionID: active.ID, EntryID: "request", Model: "program", Status: "processing", Summary: `{"requestHash":"original-hash","phase":"admitted"}`, CreatedAt: time.Now().UnixMilli()}
		created, err := r.AdmitProgramV2Run(ctx, run, true)
		require.NoError(t, err)
		require.True(t, created)
		return run
	}
	orphan := admit("unprepared", stream, version)
	prepared := admit("prepared", stream, version)
	finished := admit("finished", stream, version)
	unaffected := admit("other-program", other, otherVersion)
	for _, run := range []repo.StreamRun{prepared, finished} {
		_, err := r.PutProgramV2Task(ctx, repo.StreamTask{ID: run.ID, StreamID: run.StreamID, VersionID: run.VersionID, EntryID: run.EntryID, Model: "program", Output: `{"outputs":[]}`, ReceivedAt: time.Now().UnixMilli()}, run)
		require.NoError(t, err)
	}
	require.NoError(t, r.FinishProgramV2Task(ctx, finished.ID, "succeeded", `{"phase":"uns-commit"}`, 1))
	frozen, err := r.Task(ctx, prepared.ID)
	require.NoError(t, err)
	_, err = r.DisableStream(ctx, stream.ID, stream.Revision+1, remove)
	require.ErrorIs(t, err, repo.ErrConflict, "stale confirmation cannot stop or delete the current release")
	stream, err = r.DisableStream(ctx, stream.ID, stream.Revision, remove)
	require.NoError(t, err)
	require.Equal(t, "disabled", stream.Status)
	require.Zero(t, stream.PendingVersion)
	_, err = r.AdmitProgramV2Run(ctx, repo.StreamRun{ID: "after-stop", StreamID: stream.ID, VersionID: version.ID, EntryID: "request", Model: "program", Status: "processing", Summary: `{}`}, true)
	require.Error(t, err, "old release must not admit new execution")
	stopped, err := r.Run(ctx, orphan.ID)
	require.NoError(t, err)
	require.Equal(t, "unknown", stopped.Status, "a fenced admission without outputs is not still executing")
	require.Contains(t, stopped.Summary, `"original-hash"`)
	require.Contains(t, stopped.Summary, `"stream-disabled-before-output"`)
	_, err = r.PutProgramV2Task(ctx, repo.StreamTask{ID: orphan.ID, StreamID: stream.ID, VersionID: version.ID, EntryID: orphan.EntryID, Model: "program", Output: `{"outputs":[]}`, ReceivedAt: time.Now().UnixMilli()}, orphan)
	if remove {
		require.ErrorIs(t, err, repo.ErrNotFound, "deleted Stream cannot accept output from its old owner")
	} else {
		require.ErrorIs(t, err, repo.ErrConflict, "the old owner cannot persist output after the stop fence")
	}
	for _, tc := range []struct{ id, status string }{{prepared.ID, "processing"}, {finished.ID, "succeeded"}, {unaffected.ID, "processing"}} {
		run, err := r.Run(ctx, tc.id)
		require.NoError(t, err)
		require.Equal(t, tc.status, run.Status)
	}
	current, err := r.Task(ctx, prepared.ID)
	require.NoError(t, err)
	require.Equal(t, frozen, current, "frozen outputs remain recoverable and unchanged")
	if remove {
		require.Positive(t, stream.DeletedAt)
		_, err = r.Stream(ctx, stream.ID)
		require.Error(t, err)
		_, err = r.Version(ctx, stream.ID, version.ID)
		require.NoError(t, err, "deletion retains immutable release history")
	} else {
		_, err = r.DisableStream(ctx, stream.ID, stream.Revision, false)
		require.NoError(t, err)
	}
	again, err := r.Run(ctx, orphan.ID)
	require.NoError(t, err)
	require.Equal(t, stopped, again, "repeated stop must not rewrite the interruption history")
}

func TestProgramV2DisableWaitsForPreparingTransaction(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	doc, manifest := `{"script":"run"}`, v2RequestManifest()
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "preparing-owner", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	run := repo.StreamRun{ID: "preparing-owner-run", StreamID: stream.ID, VersionID: version.ID, EntryID: "request", Model: "program", Status: "processing", Summary: `{"requestHash":"owner-hash","phase":"admitted"}`, CreatedAt: time.Now().UnixMilli()}
	created, err := r.AdmitProgramV2Run(ctx, run, true)
	require.NoError(t, err)
	require.True(t, created)
	tx := store.UnsDB().Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	frozen, err := repo.NewSourceStreamRepo(tx).PutProgramV2Task(ctx, repo.StreamTask{ID: run.ID, StreamID: stream.ID, VersionID: version.ID, EntryID: run.EntryID, Model: "program", Output: `{"outputs":[]}`, ReceivedAt: time.Now().UnixMilli()}, run)
	require.NoError(t, err)
	stopCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	_, err = r.DisableStream(stopCtx, stream.ID, stream.Revision, false)
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded, "stop waits for the preparing owner's Stream lock")
	require.NoError(t, tx.Commit().Error)
	_, err = r.DisableStream(ctx, stream.ID, stream.Revision, false)
	require.NoError(t, err)
	stored, err := r.Task(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, frozen, stored)
	storedRun, err := r.Run(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "processing", storedRun.Status, "an output committed before the stop is not an unknown execution")
}

// Source 停用或连接版本变化同样撤销旧执行的写入资格；改名不撤销。
// 历史生效版本即使已不在当前依赖索引，也必须按冻结清单识别。
func TestProgramV2SourceFenceFinalizesUnpreparedHistoricalRuns(t *testing.T) {
	for _, reason := range []string{"source-disabled-before-output", "source-config-changed-before-output"} {
		t.Run(reason, func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			r, ctx := repo.NewSourceStreamRepo(store), context.Background()
			source, err := r.CreateSource(ctx, repo.Source{Name: "device", Protocol: "mqtt", Document: `{}`})
			require.NoError(t, err)
			source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, s *repo.Source) error {
				s.DesiredState, s.Health = "enabled", "healthy"
				return nil
			})
			require.NoError(t, err)
			id := strconv.FormatInt(source.ID, 10)
			manifest := v2RequestManifest()
			manifest.Sources = []streamexec.ProgramSource{{ID: id, Resources: []string{"command"}}}
			doc := `{"script":"device"}`
			stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "historical-source", Model: "program", Document: doc}, 0, manifest)
			require.NoError(t, err)
			stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{{Alias: id, SourceID: source.ID, ConfigRevision: source.ConfigRevision, Resources: []string{"command"}}})
			require.NoError(t, err)
			stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
			require.NoError(t, err)
			orphan := repo.StreamRun{ID: "source-orphan", StreamID: stream.ID, VersionID: version.ID, EntryID: "request", Model: "program", Status: "processing", Summary: `{"requestHash":"source-hash","phase":"admitted"}`}
			prepared := orphan
			prepared.ID = "source-prepared"
			for _, run := range []repo.StreamRun{orphan, prepared} {
				created, err := r.AdmitProgramV2Run(ctx, run, true)
				require.NoError(t, err)
				require.True(t, created)
			}
			frozen, err := r.PutProgramV2Task(ctx, repo.StreamTask{ID: prepared.ID, StreamID: stream.ID, VersionID: version.ID, EntryID: prepared.EntryID, Model: "program", Output: `{"outputs":[]}`}, prepared)
			require.NoError(t, err)
			source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, s *repo.Source) error { s.Name = "renamed"; return nil })
			require.NoError(t, err)
			stored, err := r.Run(ctx, orphan.ID)
			require.NoError(t, err)
			require.Equal(t, "processing", stored.Status, "display edits do not revoke an execution")
			stream, err = r.SaveProgramV2(ctx, stream, stream.Revision, v2RequestManifest())
			require.NoError(t, err)
			stream, next, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, v2RequestManifest(), []repo.StreamSourceSnapshot{})
			require.NoError(t, err)
			_, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, next.ID)
			require.NoError(t, err)
			var references int64
			require.NoError(t, store.UnsDB().Model(&repo.StreamSource{}).Where("stream_id=? AND source_id=?", stream.ID, source.ID).Count(&references).Error)
			require.Zero(t, references, "the old version is no longer in the current dependency index")
			independent := orphan
			independent.ID, independent.VersionID = "independent-source-free", next.ID
			created, err := r.AdmitProgramV2Run(ctx, independent, true)
			require.NoError(t, err)
			require.True(t, created)
			source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, s *repo.Source) error {
				if reason == "source-disabled-before-output" {
					s.DesiredState = "disabled"
				} else {
					s.ConfigRevision++
				}
				return nil
			})
			require.NoError(t, err)
			stored, err = r.Run(ctx, orphan.ID)
			require.NoError(t, err)
			require.Equal(t, "unknown", stored.Status)
			require.Contains(t, stored.Summary, reason)
			require.Contains(t, stored.Summary, "source-hash")
			persisted, err := r.Task(ctx, prepared.ID)
			require.NoError(t, err)
			require.Equal(t, frozen, persisted)
			preparedRun, err := r.Run(ctx, prepared.ID)
			require.NoError(t, err)
			require.Equal(t, "processing", preparedRun.Status)
			independentRun, err := r.Run(ctx, independent.ID)
			require.NoError(t, err)
			require.Equal(t, "processing", independentRun.Status, "a Source-free execution of the same Stream is unaffected")
			if source.DesiredState == "disabled" {
				_, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, s *repo.Source) error { s.DesiredState = "enabled"; return nil })
				require.NoError(t, err)
				_, err = r.PutProgramV2Task(ctx, repo.StreamTask{ID: orphan.ID, StreamID: stream.ID, VersionID: version.ID, EntryID: orphan.EntryID, Model: "program", Output: `{"outputs":[]}`}, orphan)
				require.ErrorIs(t, err, repo.ErrConflict, "reenabling the connection does not revive the interrupted owner")
			}
		})
	}
}

func TestProgramV2ExecutionAdmissionAndOutputFreeze(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	manifest := v2RequestManifest()
	doc := `{"script":"run"}`
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "admission", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	run := repo.StreamRun{ID: "v2-run", StreamID: stream.ID, VersionID: version.ID, EntryID: "request", Model: "program", Status: "processing", Summary: `{"requestHash":"first"}`, CreatedAt: time.Now().UnixMilli()}
	_, err = r.AdmitProgramV2Run(ctx, run, false)
	require.ErrorIs(t, err, repo.ErrConflict)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	created, err := r.AdmitProgramV2Run(ctx, run, true)
	require.NoError(t, err)
	require.True(t, created)
	created, err = r.AdmitProgramV2Run(ctx, run, true)
	require.NoError(t, err)
	require.False(t, created)
	changed := run
	changed.Summary = `{"requestHash":"second"}`
	_, err = r.AdmitProgramV2Run(ctx, changed, true)
	require.ErrorIs(t, err, repo.ErrConflict)
	changed.ID = "invalid-hash"
	changed.Summary = `{"requestHash":[]}`
	_, err = r.AdmitProgramV2Run(ctx, changed, true)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	task := repo.StreamTask{ID: run.ID, StreamID: stream.ID, VersionID: version.ID, EntryID: "request", Model: "program", Output: `{"outputs":[{"topicId":"10","data":1},{"topicId":"10","data":2}]}`, ReceivedAt: time.Now().UnixMilli()}
	persisted, err := r.PutProgramV2Task(ctx, task, run)
	require.NoError(t, err)
	require.JSONEq(t, task.Output, persisted.Output)
	task.Output = `{"outputs":[]}`
	again, err := r.PutProgramV2Task(ctx, task, run)
	require.NoError(t, err)
	require.JSONEq(t, persisted.Output, again.Output)
	other := task
	other.ID = "unadmitted"
	otherRun := run
	otherRun.ID = other.ID
	_, err = r.PutProgramV2Task(ctx, other, otherRun)
	require.Error(t, err)
	effect := repo.StreamProgramEffect{RunID: run.ID, EntryID: run.EntryID, CallID: "1", OpID: "source.write", RequestHash: strings.Repeat("e", 64)}
	_, err = r.ReserveProgramEffect(ctx, effect)
	require.NoError(t, err)
	_, claimed, err := r.ClaimProgramEffect(ctx, run.ID, run.EntryID, effect.CallID, effect.RequestHash)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, r.FinishProgramEffect(ctx, run.ID, run.EntryID, effect.CallID, effect.RequestHash, "completed", `{}`))
	require.NoError(t, r.FinishProgramV2Task(ctx, run.ID, "partial-failure", `{"targets":[{"status":"failed"}]}`, 2))
	partial, err := r.Run(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "partial-failure", partial.Status)
	require.Contains(t, partial.Summary, `"first"`)
	require.NoError(t, r.FinishProgramV2Task(ctx, run.ID, "succeeded", `{"targets":[{"status":"succeeded"}]}`, 3))
	completed, err := r.Run(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", completed.Status)
	require.NoError(t, r.FinishProgramV2Task(ctx, run.ID, "partial-failure", `{"targets":[]}`, 4))
	final, err := r.Run(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, completed.Status, final.Status)
	require.JSONEq(t, completed.Summary, final.Summary)
	finalTask, err := r.Task(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", finalTask.Status)
}

func TestProgramV2HistoricalExecutionRequiresActivatedVersion(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	manifest := v2RequestManifest()
	first := `{"script":"first"}`
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "versions", Model: "program", Document: first}, 0, manifest)
	require.NoError(t, err)
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, first, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	stream.Document = `{"script":"next"}`
	stream, err = r.SaveProgramV2(ctx, stream, stream.Revision, manifest)
	require.NoError(t, err)
	stream, next, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, stream.Document, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	run := repo.StreamRun{ID: "candidate", StreamID: stream.ID, VersionID: next.ID, EntryID: "request", Model: "program", Status: "processing", Summary: `{}`}
	_, err = r.AdmitProgramV2Run(ctx, run, false)
	require.ErrorIs(t, err, repo.ErrConflict)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, next.ID)
	require.NoError(t, err)
	run.ID = "historical"
	run.VersionID = version.ID
	created, err := r.AdmitProgramV2Run(ctx, run, false)
	require.NoError(t, err)
	require.True(t, created)
	run.ID = "old-sync"
	_, err = r.AdmitProgramV2Run(ctx, run, true)
	require.ErrorIs(t, err, repo.ErrConflict)
	run.ID = "missing-entry"
	run.VersionID = next.ID
	run.EntryID = "absent"
	_, err = r.AdmitProgramV2Run(ctx, run, false)
	require.ErrorIs(t, err, repo.ErrConflict)
}
