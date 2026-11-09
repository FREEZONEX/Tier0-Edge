package dataingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"backend/internal/config"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

// 原生写入先提交；NATS 失败不撤销数据，也不保存待补发事件。
func TestNativeWriteCommitThenBestEffortNotification(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	ctx := context.Background()
	s, err := New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer s.Close()
	s.publishOverride = func(string, byte, bool, []byte) error { return nil }
	require.NoError(t, s.EnableResourceEvents(ctx))
	offline := false
	var received []StreamEvent
	s.SetResourceEventPublisher(func(_ context.Context, subject, id string, raw []byte) error {
		require.Equal(t, "tier0.events.topic.52", subject)
		var count int
		require.NoError(t, s.writer.pool.QueryRow(ctx, `SELECT count(*) FROM public.native_event`).Scan(&count))
		require.Positive(t, count)
		var event StreamEvent
		require.NoError(t, json.Unmarshal(raw, &event))
		require.Equal(t, id, event.MessageID)
		require.Equal(t, "native", event.Origin)
		require.Zero(t, event.SourceID)
		require.Zero(t, event.StreamID)
		received = append(received, event)
		if offline {
			return errors.New("NATS unavailable")
		}
		return nil
	})
	node := repo.UnsNode{ID: 52, Type: 2, TopicType: 3, Alias: "native_event", Namespace: "factory/native", EnableHistory: 1, Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}
	require.NoError(t, s.WriteOpenAPIValue(ctx, node, map[string]any{"value": 12}, 0, 1, false))
	require.Len(t, received, 1)
	require.Equal(t, float64(12), received[0].Data.(map[string]any)["value"])
	offline = true
	require.NoError(t, s.WriteOpenAPIValue(ctx, node, map[string]any{"value": 99}, 0, 1, false), "a notification error is not a write failure")
	require.Len(t, received, 2)
	var count int
	require.NoError(t, s.writer.pool.QueryRow(ctx, `SELECT count(*) FROM public.native_event`).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, s.writer.pool.QueryRow(ctx, `SELECT count(*) FROM public.stream_receipt`).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, s.writer.pool.QueryRow(ctx, `SELECT count(*) FROM public.stream_event_outbox`).Scan(&count))
	require.Zero(t, count)
}

func TestNativeForwardOnlyEventHonorsTopicHistoryPolicy(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	ctx := context.Background()
	s, err := New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer s.Close()
	s.publishOverride = func(string, byte, bool, []byte) error { return nil }
	require.NoError(t, s.EnableResourceEvents(ctx))
	notifications := 0
	s.SetResourceEventPublisher(func(context.Context, string, string, []byte) error { notifications++; return nil })
	node := repo.UnsNode{ID: 53, Type: 2, TopicType: 3, Alias: "native_forward", Namespace: "factory/forward", EnableHistory: 2, Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}
	require.NoError(t, s.WriteOpenAPIValue(ctx, node, map[string]any{"value": 15}, 0, 1, false))
	var relation *string
	require.NoError(t, s.writer.pool.QueryRow(ctx, `SELECT to_regclass('uns.native_forward')::text`).Scan(&relation))
	require.Nil(t, relation)
	var count int
	require.NoError(t, s.writer.pool.QueryRow(ctx, `SELECT count(*) FROM public.stream_event_outbox`).Scan(&count))
	require.Zero(t, count)
	require.Equal(t, 1, notifications)
}
