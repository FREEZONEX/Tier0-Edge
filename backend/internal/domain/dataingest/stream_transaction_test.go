package dataingest

// Real PostgreSQL: data and receipt stay atomic; notifications are not persisted.
import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestStreamTargetTransactionWithoutOutbox(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	ctx := context.Background()
	w, err := NewSinkWriter(ctx, dsn, 100)
	require.NoError(t, err)
	defer w.Close()
	require.NoError(t, w.EnsureStreamSchema(ctx))
	item := StreamCommit{ReceiptID: "event-1-target-1", StreamID: 10, VersionID: 11, TopicID: 20, Subject: "tier0.events.topic.20", Event: json.RawMessage(`{"value":12}`), Records: []Record{{UnsID: 20, Alias: "stream_test", TopicType: metricTopicType, Time: time.Now(), Fields: []Field{{Name: "value", Type: "double"}}, Values: map[string]string{"value": "12"}}}}
	created, err := w.CommitStreamTarget(ctx, item)
	require.NoError(t, err)
	require.True(t, created)
	created, err = w.CommitStreamTarget(ctx, item)
	require.NoError(t, err)
	require.False(t, created)
	var count int
	require.NoError(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM public.stream_test`).Scan(&count))
	require.Equal(t, 1, count)
	// A database failure after inserting the receipt rolls the entire unit back.
	_, err = w.pool.Exec(ctx, `CREATE FUNCTION public.reject_stream_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END $$; CREATE TRIGGER reject_stream_event AFTER INSERT ON public.stream_receipt FOR EACH ROW EXECUTE FUNCTION public.reject_stream_event()`)
	require.NoError(t, err)
	item.ReceiptID = "event-2-target-1"
	item.Records[0].Time = item.Records[0].Time.Add(time.Second)
	_, err = w.CommitStreamTarget(ctx, item)
	require.Error(t, err)
	require.NoError(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM public.stream_receipt WHERE receipt_id=$1`, item.ReceiptID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM public.stream_test`).Scan(&count))
	require.Equal(t, 1, count)
	_, err = w.pool.Exec(ctx, `DROP TRIGGER reject_stream_event ON public.stream_receipt`)
	require.NoError(t, err)
	created, err = w.CommitStreamTarget(ctx, item)
	require.NoError(t, err)
	require.True(t, created)

	item.ReceiptID = "invalid-type"
	item.Records[0].Values["value"] = "not-a-number"
	_, err = w.CommitStreamTarget(ctx, item)
	require.ErrorContains(t, err, "schemaMismatch")
	item.Records[0].UnsID++
	_, err = w.CommitStreamTarget(ctx, item)
	require.ErrorContains(t, err, "invalidTarget")
}

func TestStreamHistoryPolicyAndFilteredReceipt(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	ctx := context.Background()
	s, err := New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.EnsureStreamStorage(ctx))
	notifications := 0
	s.SetResourceEventPublisher(func(context.Context, string, string, []byte) error { notifications++; return nil })
	node := repo.UnsNode{ID: 51, Type: 2, TopicType: 3, Alias: "stream_forward", Namespace: "factory/temp", EnableHistory: 2, Schema: json.RawMessage(`{"fields":[{"name":"temperature","type":"double"}]}`)}
	event := StreamEvent{MessageID: "forward-only", SourceID: 2, StreamID: 3, VersionID: 4, TopicID: node.ID, ReceivedAt: time.Now(), Data: map[string]any{"temperature": 25.5}}
	created, err := s.CommitStreamEvent(ctx, node, event)
	require.NoError(t, err)
	require.True(t, created)
	var count int
	require.NoError(t, s.writer.pool.QueryRow(ctx, `SELECT count(*) FROM public.stream_receipt`).Scan(&count))
	require.Equal(t, 1, count)
	var tableName *string
	require.NoError(t, s.writer.pool.QueryRow(ctx, `SELECT to_regclass('uns.stream_forward')::text`).Scan(&tableName))
	require.Nil(t, tableName, "history disabled must not create a history table")
	event.MessageID = "filtered"
	event.Drop = true
	event.Data = nil
	created, err = s.CommitStreamEvent(ctx, node, event)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, s.writer.pool.QueryRow(ctx, `SELECT count(*) FROM public.stream_event_outbox`).Scan(&count))
	require.Zero(t, count, "no event Outbox writes")
	require.Equal(t, 1, notifications, "filtered events only leave a completion receipt")
	event.Drop = false
	event.Data = map[string]any{"wrong-field": 12}
	_, err = s.CommitStreamEvent(ctx, node, event)
	require.ErrorContains(t, err, "schemaMismatch")
}
