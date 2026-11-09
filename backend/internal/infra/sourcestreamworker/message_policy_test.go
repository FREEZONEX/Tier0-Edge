package sourcestreamworker

// 验证统一界限、入口时间不续期，以及真实 JetStream 的过期和超量淘汰。
import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestMessagePolicyDefaultsAndValidation(t *testing.T) {
	names := []string{"SOURCE_STREAM_MESSAGE_MAX_AGE_SECONDS", "SOURCE_STREAM_MESSAGE_MAX_MSGS", "SOURCE_STREAM_MESSAGE_MAX_BYTES"}
	for _, name := range names {
		t.Setenv(name, "")
	}
	p, err := loadMessagePolicy()
	require.NoError(t, err)
	require.Equal(t, time.Hour, p.maxAge)
	require.EqualValues(t, 100000, p.maxMsgs)
	require.EqualValues(t, 1<<30, p.maxBytes)
	for _, name := range names {
		for _, invalid := range []string{"0", "-1", "bad", "9223372036854775808"} {
			t.Setenv(name, invalid)
			_, err = loadMessagePolicy()
			require.ErrorContains(t, err, name)
		}
		t.Setenv(name, "")
	}
	t.Setenv(names[0], "31536001")
	_, err = loadMessagePolicy()
	require.Error(t, err, "duration overflow/unsupported retention must not become unlimited")
	t.Setenv(names[0], "60")
	t.Setenv(names[1], "2")
	t.Setenv(names[2], "2048")
	p, err = loadMessagePolicy()
	require.NoError(t, err)
	require.Equal(t, messagePolicy{time.Minute, 2, 2048}, p)
}

func TestMessagePolicyIngressDeadline(t *testing.T) {
	p := messagePolicy{maxAge: time.Hour}
	now := time.Now().UTC().Truncate(time.Millisecond)
	old := now.Add(-2 * time.Hour)
	for _, message := range []struct{ subject, data string }{
		{"tier0.raw.source.1", fmt.Sprintf(`{"input":{"receivedAt":%d}}`, old.UnixMilli())},
		{"tier0.events.topic.1", fmt.Sprintf(`{"receivedAt":%q}`, old.Format(time.RFC3339Nano))},
		{"tier0.delivery.task.1", fmt.Sprintf(`{"receivedAt":%d}`, old.UnixMilli())},
	} {
		require.WithinDuration(t, old.Add(time.Hour), p.deadline(message.subject, []byte(message.data), now), 0)
	}
	for _, message := range []struct{ subject, data string }{
		{"tier0.raw.source.1", fmt.Sprintf(`{"input":{"receivedAt":%d}}`, now.Add(time.Hour).UnixMilli())},
		{"tier0.events.topic.1", `{invalid`},
		{"tier0.delivery.task.1", `{"id":"old-envelope"}`},
		{"tier0.dlq.RAW", `{"input":{"receivedAt":1}}`},
	} {
		require.WithinDuration(t, now.Add(time.Hour), p.deadline(message.subject, []byte(message.data), now), 0)
	}
}

func TestMessagePolicyRealJetStreamLimits(t *testing.T) {
	url := os.Getenv("TEST_SOURCE_NATS_URL")
	if url == "" {
		t.Skip("requires dedicated TEST_SOURCE_NATS_URL")
	}
	conn, err := nats.Connect(url)
	require.NoError(t, err)
	defer conn.Close()
	js, err := conn.JetStream()
	require.NoError(t, err)
	for _, mode := range []string{"count", "bytes", "age", "update"} {
		t.Run(mode, func(t *testing.T) {
			name := "POLICY_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			subject := "test.policy." + name
			config := nats.StreamConfig{Name: name, Subjects: []string{subject}, Storage: nats.FileStorage, Retention: nats.LimitsPolicy, Replicas: 1}
			p := messagePolicy{maxAge: time.Hour, maxMsgs: 100000, maxBytes: 1 << 30}
			switch mode {
			case "count", "update":
				p.maxMsgs = 2
			case "bytes":
				p.maxBytes = 2500
			case "age":
				p.maxAge = 300 * time.Millisecond
				config.Duplicates = 2 * time.Minute
			}
			if mode != "update" {
				p.apply(&config)
			} else {
				config.Discard, config.MaxAge = nats.DiscardNew, 7*24*time.Hour
			}
			_, err := js.AddStream(&config)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, js.DeleteStream(name)) })
			if mode == "update" {
				for i := 0; i < 4; i++ {
					_, err = js.Publish(subject, []byte("before"))
					require.NoError(t, err)
				}
				info, err := js.StreamInfo(name)
				require.NoError(t, err)
				config = info.Config
				p.apply(&config)
				_, err = js.UpdateStream(&config)
				require.NoError(t, err)
			}
			for i := 0; i < 4; i++ {
				_, err = js.Publish(subject, []byte(fmt.Sprintf("%d-%s", i, strings.Repeat("x", 1000))))
				require.NoError(t, err, "DiscardOld must accept fresh messages under pressure")
			}
			info, err := js.StreamInfo(name)
			require.NoError(t, err)
			require.Equal(t, nats.DiscardOld, info.Config.Discard)
			switch mode {
			case "count", "update":
				require.EqualValues(t, 2, info.State.Msgs)
			case "bytes":
				require.LessOrEqual(t, info.State.Bytes, uint64(2500))
				require.Less(t, info.State.Msgs, uint64(4))
			case "age":
				require.Eventually(t, func() bool {
					info, err := js.StreamInfo(name)
					return err == nil && info.State.Msgs == 0
				}, 5*time.Second, 50*time.Millisecond)
			}
			if mode != "age" {
				last, err := js.GetLastMsg(name, subject)
				require.NoError(t, err)
				require.True(t, strings.HasPrefix(string(last.Data), "3-"), "retain newest message")
			}
		})
	}
}
