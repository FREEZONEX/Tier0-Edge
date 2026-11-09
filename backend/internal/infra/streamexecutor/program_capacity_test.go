package streamexecutor

// 正式四执行器 + Service + 独立 PG：真实占槽、八个排队、过载拒绝及 UNS 落库。
// 本机子进程不是 KVM cgroup 隔离证明；不连接设备、不写现有环境。
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func TestProgramCapacityActualFourSlotsAndEightWaiters(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	var notifications atomic.Int32
	require.NoError(t, repo.ConfigureResourceEventPublisher(store.UnsDB(), func(context.Context, string, string, []byte) error {
		notifications.Add(1)
		return nil
	}))
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{
		"uns.read": {}, "uns.write": {}, "uns.stream.manage": {}, "uns.stream.execute": {},
	}})
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	t.Cleanup(cancel)
	dir, binary := subprocessPaths(t)
	out, err := exec.Command("go", "build", "-buildvcs=false", "-o", binary, "../../../cmd/stream-executor").CombinedOutput()
	require.NoError(t, err, string(out))
	serverCtx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	paths := []string{}
	for index := range 4 {
		path := filepath.Join(dir, fmt.Sprintf("%d.sock", index))
		server := exec.CommandContext(serverCtx, binary, "-socket", path)
		require.NoError(t, server.Start())
		t.Cleanup(func() { stop(); _ = server.Wait() })
		paths = append(paths, path)
	}
	client := New(strings.Join(paths, ","))
	require.Eventually(t, func() bool { return client.Ready(ctx) == nil }, 5*time.Second, 25*time.Millisecond)
	sink, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	t.Cleanup(sink.Close)
	require.NoError(t, sink.EnsureStreamStorage(ctx))
	s := domain.New(nil, client, "")
	t.Cleanup(s.Close)
	s.SetAsyncReady(func() bool { return true })
	barrier := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(barrier) }) }
	t.Cleanup(release)
	var reads atomic.Int32
	s.SetProgramUNS(func(callCtx context.Context, _ int64) (json.RawMessage, error) {
		reads.Add(1)
		select {
		case <-barrier:
			return json.RawMessage(`{"value":7}`), nil
		case <-callCtx.Done():
			return nil, callCtx.Err()
		}
	}, sink)
	db, err := store.UnsDB().DB()
	require.NoError(t, err)
	db.SetMaxOpenConns(10)
	require.NoError(t, s.StartProgramExecutions(ctx))
	var originalOwner string
	require.NoError(t, store.UnsDB().Table("public.uns_stream_execution_owner").Select("id").Scan(&originalOwner).Error)
	input, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{Name: "Capacity input", Type: 2, TopicType: 3, EnableHistory: 2,
		Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}, nil, nil)
	require.NoError(t, err)
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
	created, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Capacity output", RecordSchema: schema, SampleSchema: schema})
	require.NoError(t, err)
	collection := created["collection"].(repo.UnsMeta).ID
	code := fmt.Sprintf(`module.exports=api=>{
	 const input=api.uns.use({topicId:%q,access:'read'});const target=api.uns.use({collectionId:%q,access:'write'});
	 api.stream.onRequest({id:'run'},async context=>{await input.read({timeoutMs:15000});
	 await target.write({recordId:'item-'+context.input.id,record:{value:context.input.id},sample:{value:context.input.id}});
	 return {id:context.input.id};});};`, strconv.FormatInt(input.ID, 10), collection)
	view, err := s.SaveStream(ctx, 0, 0, map[string]any{"name": "Capacity test", "script": code, "timeoutMs": 20000})
	require.NoError(t, err)
	view, err = s.Deploy(ctx, view["id"].(int64), view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	id, version := view["id"].(int64), view["activeVersion"].(int64)
	results := make(chan error, 12)
	var jobs sync.WaitGroup
	t.Cleanup(func() { cancel(); release(); jobs.Wait() })
	invoke := func(index int) {
		defer jobs.Done()
		_, err := s.InvokeEntry(ctx, id, version, "run", fmt.Sprintf("capacity-%d", index), map[string]any{"id": index}, 20000)
		results <- err
	}
	for index := range 4 {
		jobs.Add(1)
		go invoke(index)
	}
	require.Eventually(t, func() bool { return reads.Load() == 4 && len(client.admission) == 4 }, 5*time.Second, 10*time.Millisecond)
	for index := 4; index < 12; index++ {
		jobs.Add(1)
		go invoke(index)
	}
	require.Eventually(t, func() bool { return len(client.admission) == 12 }, 5*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 4, reads.Load(), "all four holders execute real scripts; queued calls have not run")
	require.Empty(t, client.available)
	require.Eventually(t, func() bool { return db.Stats().InUse == 1 }, 2*time.Second, 10*time.Millisecond,
		"only the owner holds a DB connection while scripts/queue wait")
	started := time.Now()
	_, err = s.InvokeEntry(ctx, id, version, "run", "capacity-overload", map[string]any{"id": 99}, 20000)
	require.ErrorIs(t, err, domain.ErrUnavailable)
	require.Less(t, time.Since(started), time.Second)
	require.EqualValues(t, 4, reads.Load())
	release()
	completed := make([]error, 0, 12)
	for range 12 {
		select {
		case err := <-results:
			completed = append(completed, err)
		case <-ctx.Done():
			t.Fatal("bounded capacity calls did not complete: ", ctx.Err())
		}
	}
	for _, err := range completed {
		if err != nil {
			var relations []struct {
				OID  uint32 `gorm:"column:oid"`
				Name string
			}
			require.NoError(t, store.UnsDB().Raw(`SELECT c.oid, n.nspname||'.'||c.relname AS name
			 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
			 WHERE (n.nspname='public' AND c.relname IN ('uns_record','uns_sample'))
			 OR n.nspname='_timescaledb_internal' ORDER BY c.oid`).Scan(&relations).Error)
			t.Logf("isolated failed-write relation identities: %+v", relations)
		}
		require.NoError(t, err)
	}
	require.EqualValues(t, 12, reads.Load())
	require.Empty(t, client.admission)
	require.Len(t, client.available, 4)
	r := repo.NewSourceStreamRepo(store)
	runs, count, err := r.Runs(ctx, repo.StreamRunFilter{StreamID: id})
	require.NoError(t, err)
	require.EqualValues(t, 13, count)
	require.Len(t, runs, 13)
	overloadHash := sha256.Sum256([]byte("program-sync:1:0:capacity-overload"))
	for _, run := range runs {
		if run.MessageID == hex.EncodeToString(overloadHash[:]) {
			require.Equal(t, "failed", run.Status)
			_, err := r.Task(ctx, run.ID)
			require.ErrorIs(t, err, repo.ErrNotFound)
			continue
		}
		require.Equal(t, "succeeded", run.Status)
		task, err := r.Task(ctx, run.ID)
		require.NoError(t, err)
		require.Equal(t, "succeeded", task.Status)
	}
	for index := range 12 {
		record, err := repo.NewUnsMetaRepo(store).Record(ctx, collection, fmt.Sprintf("item-%d", index))
		require.NoError(t, err)
		require.EqualValues(t, 1, record.Revision)
		require.JSONEq(t, fmt.Sprintf(`{"value":%d}`, index), string(record.Data))
		_, err = s.InvokeEntry(ctx, id, version, "run", fmt.Sprintf("capacity-%d", index), map[string]any{"id": index}, 20000)
		require.NoError(t, err)
	}
	require.EqualValues(t, 12, reads.Load(), "same-key replay must not execute again")
	var samples, owners int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", collection).Count(&samples).Error)
	require.EqualValues(t, 12, samples)
	require.EqualValues(t, 24, notifications.Load(), "committed record/sample notifications must not repeat on replay")
	require.NoError(t, store.UnsDB().Table("public.uns_stream_execution_owner").Where("id=?", originalOwner).Count(&owners).Error)
	require.EqualValues(t, 1, owners)
	require.Eventually(t, func() bool { return db.Stats().InUse == 1 }, 3*time.Second, 20*time.Millisecond)
	require.NoError(t, client.Ready(ctx))
}

func TestProgramCapacityOrdinaryPoolExhaustionKeepsOwnerHeartbeat(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := store.UnsDB().DB()
	require.NoError(t, err)
	db.SetMaxOpenConns(2)
	s := domain.New(nil, nil, "")
	t.Cleanup(s.Close)
	require.NoError(t, s.StartProgramExecutions(ctx))
	ordinary, err := db.Conn(ctx)
	require.NoError(t, err)
	defer ordinary.Close()
	var ownerID string
	var ownerPID int
	require.NoError(t, ordinary.QueryRowContext(ctx, `SELECT owner.id,lock.pid FROM public.uns_stream_execution_owner owner
	 JOIN pg_locks lock ON lock.locktype='advisory' AND lock.granted AND lock.objsubid=2
	 AND lock.classid::bigint=$1 AND lock.objid::bigint=(owner.lock_id::bigint & 4294967295)
	 JOIN pg_stat_activity activity ON activity.pid=lock.pid AND activity.datname=current_database()
	 WHERE owner.state='active'`, int32(0x5354524d)).Scan(&ownerID, &ownerPID))
	var before time.Time
	require.NoError(t, ordinary.QueryRowContext(ctx, `SELECT query_start FROM pg_stat_activity WHERE pid=$1`, ownerPID).Scan(&before))
	require.Equal(t, 2, db.Stats().InUse)
	blocked, stop := context.WithTimeout(ctx, 150*time.Millisecond)
	_, err = db.Conn(blocked)
	stop()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	// Observe real dedicated-session queries advancing past two heartbeat ticks;
	// no sleeps, fake clocks or manual Ping calls can satisfy this assertion.
	require.Eventually(t, func() bool {
		var latest time.Time
		var query string
		err := ordinary.QueryRowContext(ctx, `SELECT query_start,query FROM pg_stat_activity WHERE pid=$1`, ownerPID).Scan(&latest, &query)
		return err == nil && latest.After(before.Add(3*time.Second)) && strings.Contains(query, "state='active'") && s.ProgramExecutionReady()
	}, 10*time.Second, 50*time.Millisecond)
	require.NoError(t, ordinary.Close())
	var ids []string
	require.NoError(t, store.UnsDB().Table("public.uns_stream_execution_owner").Pluck("id", &ids).Error)
	require.Equal(t, []string{ownerID}, ids, "pool saturation must not invent a lost owner or new generation")
	_, _, err = repo.NewSourceStreamRepo(store).Runs(ctx, repo.StreamRunFilter{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return db.Stats().InUse == 1 }, 5*time.Second, 20*time.Millisecond)
}
