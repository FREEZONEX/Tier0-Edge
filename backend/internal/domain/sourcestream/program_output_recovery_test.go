package sourcestream_test

// 正式执行器子进程 + 独立 PG：实际冻结、事务失败与后台恢复。
// 重启恢复使用 nil 执行器，证明不会重新调用脚本或 Source；无外部设备写回。
import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/contextx"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProgramOutputRecoveryActualFrozenResultWithoutCallerReplay(t *testing.T) {
	for _, mode := range []string{"live-owner-before-commit", "restart-before-commit", "restart-after-commit", "restart-publisher-revoked", "restart-write-revoked", "restart-authority-unavailable", "restart-caller-identity-missing"} {
		t.Run(mode, func(t *testing.T) {
			s, store, sink, ctx := actualUnstartedProgramService(t)
			publisher, _ := contextx.SubjectFrom(ctx)
			publisher.AuthType = "session"
			ctx = contextx.WithSubject(ctx, publisher)
			s.SetProgramAuthorityResolver(func(context.Context, repo.ProgramAuthorityRef) (contextx.Subject, error) { return publisher, nil })
			var reads atomic.Int32
			s.SetProgramUNS(func(context.Context, int64) (json.RawMessage, error) {
				reads.Add(1)
				return json.RawMessage(`{"value":7}`), nil
			}, sink)
			require.NoError(t, s.StartProgramExecutions(ctx))
			input := actualProgramTopic(t, ctx, store, "read witness")
			schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
			created, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Background recovery", RecordSchema: schema, SampleSchema: schema})
			require.NoError(t, err)
			collection := created["collection"].(repo.UnsMeta).ID
			code := fmt.Sprintf(`module.exports=api=>{
			 const input=api.uns.use({topicId:%q,access:'read'});const target=api.uns.use({collectionId:%q,access:'write'});
			 api.stream.onRequest({id:'run'},async()=>{await input.read();const old=await api.stream.checkpoint.get();
			 await api.stream.checkpoint.set({page:old===null?1:old.page+1,z:{b:2,a:1}});
			 await target.write({recordId:'r',record:{value:7},sample:{value:7},quality:'good'});return {marker:Math.random()};});};`, strconv.FormatInt(input, 10), collection)
			view, err := s.SaveStream(ctx, 0, 0, actualProgramDocument(code))
			require.NoError(t, err)
			view, err = s.Deploy(ctx, view["id"].(int64), view["revision"].(int64), 0, 0)
			require.NoError(t, err)
			id, version := view["id"].(int64), view["activeVersion"].(int64)
			afterCommit := mode == "restart-after-commit"
			if afterCommit {
				require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION public.reject_recovery_finish() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'finish unavailable'; END $$;
				CREATE TRIGGER reject_recovery_finish BEFORE UPDATE OF status ON public.uns_stream_task FOR EACH ROW WHEN(NEW.status='succeeded') EXECUTE FUNCTION public.reject_recovery_finish()`).Error)
			} else {
				require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION public.reject_recovery_record() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'record unavailable'; END $$;
				CREATE TRIGGER reject_recovery_record BEFORE INSERT OR UPDATE ON public.uns_record FOR EACH ROW EXECUTE FUNCTION public.reject_recovery_record()`).Error)
			}
			_, err = s.InvokeEntry(ctx, id, version, "run", "automatic-frozen-recovery", nil, 3000)
			require.Error(t, err)
			r := repo.NewSourceStreamRepo(store)
			runs, total, err := r.Runs(ctx, repo.StreamRunFilter{StreamID: id})
			require.NoError(t, err)
			require.EqualValues(t, 1, total)
			task, err := r.Task(ctx, runs[0].ID)
			require.NoError(t, err)
			frozen := task.Output
			oldOwner := lifecycleOwnerID(t, store)
			if mode != "live-owner-before-commit" {
				s.Close()
			}
			if mode == "restart-caller-identity-missing" {
				require.NoError(t, store.UnsDB().Model(&repo.StreamRun{}).Where("id=?", task.ID).Update("summary", gorm.Expr("summary - 'requestIdentity'")).Error)
			}
			if afterCommit {
				require.NoError(t, store.UnsDB().Exec(`DROP TRIGGER reject_recovery_finish ON public.uns_stream_task; DROP FUNCTION public.reject_recovery_finish()`).Error)
			} else {
				require.NoError(t, store.UnsDB().Exec(`DROP TRIGGER reject_recovery_record ON public.uns_record; DROP FUNCTION public.reject_recovery_record()`).Error)
			}
			var authorityRestored atomic.Bool
			if mode != "live-owner-before-commit" {
				resumer := domain.New(nil, nil, "")
				resumer.SetProgramUNS(nil, sink)
				resumer.SetProgramAuthorityResolver(func(context.Context, repo.ProgramAuthorityRef) (contextx.Subject, error) {
					if mode == "restart-publisher-revoked" {
						return contextx.Subject{}, domain.ErrForbidden
					}
					if mode == "restart-authority-unavailable" && !authorityRestored.Load() {
						return contextx.Subject{}, context.DeadlineExceeded
					}
					current := publisher
					if mode == "restart-write-revoked" {
						current.ResourceKeys = map[string]struct{}{"uns.read": {}}
					}
					return current, nil
				})
				require.NoError(t, resumer.StartProgramExecutions(context.Background()))
				t.Cleanup(resumer.Close)
				require.NotEqual(t, oldOwner, lifecycleOwnerID(t, store))
			}
			if mode == "restart-authority-unavailable" {
				require.Eventually(t, func() bool {
					var row struct {
						RecoveryState    string
						RecoveryAttempts int
					}
					return store.UnsDB().Table("public.uns_stream_run_owner").Where("run_id=?", task.ID).Take(&row).Error == nil && row.RecoveryState == "pending" && row.RecoveryAttempts > 0
				}, 15*time.Second, 100*time.Millisecond)
				_, err = repo.NewUnsMetaRepo(store).Record(ctx, collection, "r")
				require.ErrorIs(t, err, repo.ErrNotFound)
				checkpoint, err := r.ProgramCheckpoint(ctx, id, "run")
				require.NoError(t, err)
				require.Zero(t, checkpoint.Revision)
				authorityRestored.Store(true)
			}
			if mode == "restart-publisher-revoked" || mode == "restart-write-revoked" || mode == "restart-caller-identity-missing" {
				require.Eventually(t, func() bool {
					var state string
					return store.UnsDB().Table("public.uns_stream_run_owner").Where("run_id=?", task.ID).Pluck("recovery_state", &state).Error == nil && state == "quarantined"
				}, 15*time.Second, 100*time.Millisecond)
				_, err = repo.NewUnsMetaRepo(store).Record(ctx, collection, "r")
				require.ErrorIs(t, err, repo.ErrNotFound)
				checkpoint, err := r.ProgramCheckpoint(ctx, id, "run")
				require.NoError(t, err)
				require.Zero(t, checkpoint.Revision)
				var samples int64
				require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", collection).Count(&samples).Error)
				require.Zero(t, samples)
				require.EqualValues(t, 1, reads.Load(), "revoked recovery cannot rerun the script")
				return
			}
			require.Eventually(t, func() bool { row, e := r.Task(ctx, task.ID); return e == nil && row.Status == "succeeded" }, 45*time.Second, 100*time.Millisecond, "background output-only recovery must complete without another InvokeEntry")
			stored, err := r.Task(ctx, task.ID)
			require.NoError(t, err)
			require.Equal(t, frozen, stored.Output)
			require.EqualValues(t, 1, reads.Load(), "the actual script's host read must not run again")
			record, err := repo.NewUnsMetaRepo(store).Record(ctx, collection, "r")
			require.NoError(t, err)
			require.EqualValues(t, 1, record.Revision)
			require.JSONEq(t, `{"value":7}`, string(record.Data))
			var samples, events int64
			require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", collection).Count(&samples).Error)
			require.EqualValues(t, 1, samples)
			require.NoError(t, store.UnsDB().Table("public.uns_metadata_event_outbox").Count(&events).Error)
			require.Zero(t, events, "no notification queue")
			actualProgramOutboxCount(t, store, 1)
			checkpoint, err := r.ProgramCheckpoint(ctx, id, "run")
			require.NoError(t, err)
			require.EqualValues(t, 1, checkpoint.Revision)
			require.JSONEq(t, `{"page":1,"z":{"a":1,"b":2}}`, string(checkpoint.Value))
			run, err := r.Run(ctx, task.ID)
			require.NoError(t, err)
			require.Equal(t, "succeeded", run.Status)
			var summary map[string]any
			require.NoError(t, json.Unmarshal([]byte(run.Summary), &summary))
			require.Equal(t, false, summary["usesNATS"])
			var queue struct {
				RecoveryState    string
				RecoveryAttempts int
			}
			require.NoError(t, store.UnsDB().Table("public.uns_stream_run_owner").Where("run_id=?", task.ID).Take(&queue).Error)
			require.Equal(t, "completed", queue.RecoveryState)
			require.Positive(t, queue.RecoveryAttempts)
		})
	}
}

func TestProgramOutputRecoveryActualInvalidResultDoesNotBlockFollowingResult(t *testing.T) {
	s, store, sink, ctx := actualProgramService(t)
	caller, _ := contextx.SubjectFrom(ctx)
	caller.AuthType = "session"
	ctx = contextx.WithSubject(ctx, caller)
	resolveCaller := func(context.Context, repo.ProgramAuthorityRef) (contextx.Subject, error) { return caller, nil }
	s.SetProgramAuthorityResolver(resolveCaller)
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
	created, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Independent output recovery", RecordSchema: schema, SampleSchema: schema})
	require.NoError(t, err)
	collection := created["collection"].(repo.UnsMeta).ID
	require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION public.reject_recovery_finish() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'finish unavailable'; END $$;
		CREATE TRIGGER reject_recovery_finish BEFORE UPDATE OF status ON public.uns_stream_task FOR EACH ROW WHEN(NEW.status='succeeded') EXECUTE FUNCTION public.reject_recovery_finish()`).Error)
	ids := map[string]string{}
	for _, item := range []struct{ name, value string }{{"invalid", `"not a number"`}, {"valid", "7"}} {
		doc := actualProgramDocument(fmt.Sprintf(`module.exports=api=>{const target=api.uns.use({collectionId:%q,access:'write'});api.stream.onRequest({id:'run'},async()=>{await target.write({recordId:%q,record:{value:%s},sample:{value:%s}});return null;});};`, collection, item.name, item.value, item.value))
		doc["name"] = "Recovery " + item.name
		view, err := s.SaveStream(ctx, 0, 0, doc)
		require.NoError(t, err)
		view, err = s.Deploy(ctx, view["id"].(int64), view["revision"].(int64), 0, 0)
		require.NoError(t, err)
		_, err = s.InvokeEntry(ctx, view["id"].(int64), view["activeVersion"].(int64), "run", item.name, nil, 3000)
		require.Error(t, err)
		runs, total, err := repo.NewSourceStreamRepo(store).Runs(ctx, repo.StreamRunFilter{StreamID: view["id"].(int64)})
		require.NoError(t, err)
		require.EqualValues(t, 1, total)
		ids[item.name] = runs[0].ID
	}
	s.Close()
	require.NoError(t, store.UnsDB().Exec(`DROP TRIGGER reject_recovery_finish ON public.uns_stream_task; DROP FUNCTION public.reject_recovery_finish()`).Error)
	resumer := domain.New(nil, nil, "")
	resumer.SetProgramUNS(nil, sink)
	resumer.SetProgramAuthorityResolver(resolveCaller)
	require.NoError(t, resumer.StartProgramExecutions(context.Background()))
	t.Cleanup(resumer.Close)
	require.Eventually(t, func() bool {
		var rows []struct {
			RunID         string
			RecoveryState string
		}
		if store.UnsDB().Table("public.uns_stream_run_owner").Where("run_id IN ?", []string{ids["valid"], ids["invalid"]}).Find(&rows).Error != nil {
			return false
		}
		states := map[string]string{}
		for _, row := range rows {
			states[row.RunID] = row.RecoveryState
		}
		return states[ids["valid"]] == "completed" && states[ids["invalid"]] == "quarantined"
	}, 45*time.Second, 100*time.Millisecond)
	record, err := repo.NewUnsMetaRepo(store).Record(ctx, collection, "valid")
	require.NoError(t, err)
	require.EqualValues(t, 1, record.Revision)
	_, err = repo.NewUnsMetaRepo(store).Record(ctx, collection, "invalid")
	require.ErrorIs(t, err, repo.ErrNotFound)
	var count int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", collection).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
