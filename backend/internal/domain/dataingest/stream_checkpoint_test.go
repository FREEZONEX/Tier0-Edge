package dataingest

// 多目标写入失败时整页与检查点回滚；通知只在整页提交后发送。
import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestStreamCheckpointOutputsRollbackAndResume(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	ctx := context.Background()
	s, err := New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.EnsureStreamStorage(ctx))
	notifications := 0
	s.SetResourceEventPublisher(func(context.Context, string, string, []byte) error { notifications++; return nil })
	r := repo.NewUnsRepo(store)
	first, err := r.CreateUnsNode(ctx, repo.UnsNode{Name: "cursor first", Alias: "cursor_first", Type: 2, TopicType: 3, EnableHistory: 1, Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}, nil, nil)
	require.NoError(t, err)
	second, err := r.CreateUnsNode(ctx, repo.UnsNode{Name: "cursor second", Alias: "cursor_second", Type: 2, TopicType: 3, EnableHistory: 1, Schema: first.Schema}, nil, nil)
	require.NoError(t, err)
	events := []ProgramStreamEvent{}
	for i, node := range []repo.UnsNode{first, second} {
		events = append(events, ProgramStreamEvent{Node: node, Event: StreamEvent{MessageID: "cursor-output-" + strconv.FormatInt(node.ID, 10), StreamID: 10, VersionID: 11, TopicID: node.ID, ReceivedAt: time.Now(), Data: map[string]any{"value": i + 1}}})
	}
	change := repo.ProgramCheckpointCommit{StreamID: 10, EntryID: "poll", RunID: "batch1", Digest: strings.Repeat("a", 64), Value: json.RawMessage(`{"page":1}`)}
	require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION public.reject_cursor_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.topic_id=`+strconv.FormatInt(second.ID, 10)+` THEN RAISE EXCEPTION 'test cursor rollback'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_cursor_event BEFORE INSERT ON public.stream_receipt FOR EACH ROW EXECUTE FUNCTION public.reject_cursor_event()`).Error)
	_, err = s.CommitProgramCheckpoint(ctx, events, change)
	require.Error(t, err)
	checkpoint, err := repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.Zero(t, checkpoint.Revision)
	var count int64
	require.NoError(t, store.UnsDB().Table("public.stream_receipt").Count(&count).Error)
	require.Zero(t, count)
	for _, node := range []repo.UnsNode{first, second} {
		require.NotEmpty(t, node.Alias)
		require.NoError(t, store.UnsDB().Table("public."+node.Alias).Count(&count).Error)
		require.Zero(t, count, "history must roll back with the cursor and receipt")
	}
	require.NoError(t, store.UnsDB().Exec(`DROP TRIGGER reject_cursor_event ON public.stream_receipt`).Error)
	created, err := s.CommitProgramCheckpoint(ctx, events, change)
	require.NoError(t, err)
	require.True(t, created)
	checkpoint, err = repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 1, checkpoint.Revision)
	created, err = s.CommitProgramCheckpoint(ctx, events, change)
	require.NoError(t, err)
	require.False(t, created)
	require.NoError(t, store.UnsDB().Table("public.stream_event_outbox").Count(&count).Error)
	require.Zero(t, count, "no notification queue")
	require.Equal(t, 2, notifications, "replay does not republish notifications")
	for _, node := range []repo.UnsNode{first, second} {
		require.NoError(t, store.UnsDB().Table("public."+node.Alias).Count(&count).Error)
		require.EqualValues(t, 1, count, "replay must not append duplicate history")
	}
	change.RunID = "filtered"
	change.ExpectedRevision = 1
	change.Digest = strings.Repeat("b", 64)
	change.Value = json.RawMessage(`{"page":2}`)
	created, err = s.CommitProgramCheckpoint(ctx, nil, change)
	require.NoError(t, err)
	require.True(t, created)
	checkpoint, err = repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 2, checkpoint.Revision)
}

func TestStreamCheckpointSeparateSinkDatabaseFailsClosed(t *testing.T) {
	ctx := context.Background()
	previousCommon, previousUNS := repo.GetCommonConn(ctx), repo.GetUnsConn(ctx)
	canonical, _ := testkit.NewTestDB(t)
	sink, dsn := testkit.NewTestDB(t)
	t.Cleanup(func() { repo.SetStoreForTest(previousCommon, previousUNS) })
	repo.SetStoreForTest(canonical.CommonDB(), canonical.UnsDB())
	s, err := New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer s.Close()
	change := repo.ProgramCheckpointCommit{StreamID: 10, EntryID: "poll", RunID: "separate", Digest: strings.Repeat("a", 64), Value: json.RawMessage(`{"page":1}`)}
	_, err = s.CommitProgramCheckpoint(ctx, nil, change)
	require.ErrorContains(t, err, "checkpointDatabaseMismatch")
	for _, store := range []*repo.Store{canonical, sink} {
		cp, err := repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "poll")
		require.NoError(t, err)
		require.Zero(t, cp.Revision)
	}
}
