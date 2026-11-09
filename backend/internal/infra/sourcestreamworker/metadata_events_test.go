package sourcestreamworker_test

// Real isolated JetStream, PostgreSQL and executor children. No shared queues
// are purged and no device or business API is contacted.
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/infra/sourcestreamworker"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestCanonicalMetadataEventsThroughRealWorkerAndRestart(t *testing.T) {
	testMetadataEventsThroughRealWorker(t, "")
}

func TestNamedMetadataMixedMessagesThroughRealWorkerAndRestart(t *testing.T) {
	testMetadataEventsThroughRealWorker(t, "plant0-equipment-state")
}

func TestNumericMetadataMixedMessagesThroughRealWorkerAndRestart(t *testing.T) {
	testMetadataEventsThroughRealWorker(t, "79")
}

func TestRecordedFailureThroughRealWorkerDoesNotBlockFollowingEvents(t *testing.T) {
	testMetadataEventsThroughRealWorker(t, "plant0-known-failure")
}

func testMetadataEventsThroughRealWorker(t *testing.T, customID string) {
	store, dsn := testkit.NewTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = contextx.WithSubject(ctx, contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}, "uns.stream.manage": {}, "uns.stream.execute": {}}})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	binary, err := exec.LookPath("nats-server")
	require.NoError(t, err, "this acceptance requires the real server, not a skipped fixture")
	command := exec.CommandContext(ctx, binary, "-js", "-sd", t.TempDir(), "-a", "127.0.0.1", "-p", strconv.Itoa(port))
	command.Stdout, command.Stderr = io.Discard, io.Discard
	require.NoError(t, command.Start())
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	url := fmt.Sprintf("nats://127.0.0.1:%d", port)
	var connection *nats.Conn
	require.Eventually(t, func() bool {
		connection, err = nats.Connect(url, nats.NoReconnect(), nats.Timeout(100*time.Millisecond))
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	t.Cleanup(connection.Close)
	js, err := connection.JetStream()
	require.NoError(t, err)
	executor := startExecutor(t, ctx)
	sink, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	t.Cleanup(sink.Close)
	service := sourcestream.New(nil, executor, "")
	require.NoError(t, service.StartProgramExecutions(ctx))
	t.Cleanup(service.Close)
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
	input, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Input", RecordSchema: schema})
	require.NoError(t, err)
	output, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Output", RecordSchema: schema, SampleSchema: schema})
	require.NoError(t, err)
	inputCollection := input["collection"].(repo.UnsMeta).ID
	inputView, err := unsmeta.New().Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Input view", CollectionID: inputCollection})
	require.NoError(t, err)
	inputTopic := inputView.ID
	outputCollection := output["collection"].(repo.UnsMeta).ID
	space := ""
	if customID != "" {
		inputCollection = "plant0-input-collection"
		inputTopic = customID
		_, err = unsmeta.New().Save(ctx, inputCollection, "collection", 0, unsmeta.Document{Name: "Named input collection", RecordSchema: schema})
		require.NoError(t, err)
		_, err = unsmeta.New().Save(ctx, inputTopic, "topic", 0, unsmeta.Document{Name: "Named input topic", CollectionID: inputCollection, ViewMode: "records"})
		require.NoError(t, err)
		space = `space: "metadata",`
	}
	worker, err := sourcestreamworker.Start(ctx, url, service, sink)
	require.NoError(t, err)
	t.Cleanup(func() { worker.Close() })
	service.SetAsyncReady(worker.Ready)
	code := fmt.Sprintf(`module.exports = (api) => {
  const input = api.uns.use({ topicId: %q, %s access: "read" });
  const output = api.uns.use({ collectionId: %q, access: "write" });
  api.stream.onTopic({ id: "process", topics: [input] }, async (context) => {
    if (context.input.record.value < 0) throw new Error("private user failure");
    const value = context.input.record.value + 1;
    await output.write({ recordId: "pump", record: { value }, sample: { value }, quality: "good" });
    await api.stream.checkpoint.set({ lastValue: context.input.record.value });
    return { processed: true };
  });
};`, inputTopic, space, outputCollection)
	definition := sourcestream.StreamDefinition{Name: "Canonical event mapping", Model: sourcestream.ProgramAccess, Script: sourcestream.ScriptDefinition{Code: code, TimeoutMs: 5000}}
	view, err := service.SaveStream(ctx, 0, 0, definition)
	require.NoError(t, err)
	streamID := view["id"].(int64)
	_, err = service.Deploy(ctx, streamID, 1, 0, 0)
	require.NoError(t, err)
	if customID != "" {
		// Poison events precede real events on the same single-pending consumer.
		var deadConfig *nats.StreamConfig
		if customID == "plant0-equipment-state" {
			info, err := js.StreamInfo("TIER0_SOURCE_DLQ")
			require.NoError(t, err)
			deadConfig = &info.Config
			// Only this test's empty, isolated stream is removed to simulate a
			// failed DLQ write. The production consumer must not ACK the input.
			require.NoError(t, js.DeleteStream("TIER0_SOURCE_DLQ"))
		}
		_, err = js.Publish(repo.MetadataEventSubject(inputTopic, inputCollection), []byte(`{`))
		require.NoError(t, err)
		if deadConfig != nil {
			require.Eventually(t, func() bool {
				info, err := js.ConsumerInfo("TIER0_UNS_EVENTS", "tier0-uns-event-match-v1")
				return err == nil && info.NumAckPending == 1
			}, 10*time.Second, 50*time.Millisecond)
			// Hold the outage beyond the worker's five-second publish budget;
			// restoring earlier can let a NATS request retry succeed in-place.
			time.Sleep(6 * time.Second)
			info, err := js.ConsumerInfo("TIER0_UNS_EVENTS", "tier0-uns-event-match-v1")
			require.NoError(t, err)
			require.EqualValues(t, 1, info.NumAckPending, "failed quarantine must leave the original unacknowledged")
			_, err = js.AddStream(deadConfig)
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				info, err := js.StreamInfo("TIER0_SOURCE_DLQ")
				return err == nil && info.State.Msgs == 1
			}, info.Config.AckWait+10*time.Second, 100*time.Millisecond, "recover the failed DLQ write without purging or recreating the consumer")
		}
		invalid := dataingest.StreamEvent{MessageID: "invalid-identity", Origin: "metadata", MetadataTopicID: "invalid/topic", CollectionID: inputCollection, ReceivedAt: time.Now(), Data: map[string]any{"value": 1}}
		payload, err := json.Marshal(invalid)
		require.NoError(t, err)
		_, err = js.Publish(repo.MetadataEventSubject(invalid.MetadataTopicID, inputCollection), payload)
		require.NoError(t, err)
	}
	verify := func(expected int64) {
		require.Eventually(t, func() bool {
			row, err := repo.NewUnsMetaRepo(store).Record(ctx, outputCollection, "pump")
			return err == nil && string(row.Data) == fmt.Sprintf(`{"value": %d}`, expected)
		}, 30*time.Second, 100*time.Millisecond, "actual worker must consume the canonical event and commit the output")
	}
	if customID == "plant0-known-failure" {
		_, err := unsmeta.New().SaveRecord(ctx, inputCollection, "failed", 0, map[string]any{"value": -1})
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			info, err := js.StreamInfo("TIER0_SOURCE_DLQ")
			return err == nil && info.State.Msgs == 3
		}, 20*time.Second, 50*time.Millisecond)
		info, err := js.StreamInfo("TIER0_SOURCE_DLQ")
		require.NoError(t, err)
		var original *nats.RawStreamMsg
		for sequence := info.State.FirstSeq; sequence <= info.State.LastSeq; sequence++ {
			message, err := js.GetMsg("TIER0_SOURCE_DLQ", sequence)
			require.NoError(t, err)
			if message.Header.Get("Tier0-Error-Reason") == "recordedExecutionFailed" {
				original = message
			}
		}
		require.NotNil(t, original, "known script failure is isolated on its first delivery")
		// Inject only into this test's private JetStream: the same logical
		// event must stay failed, not turn into result-unknown or rerun JS.
		_, err = js.Publish(original.Header.Get("Tier0-Original-Subject"), original.Data)
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			info, err := js.StreamInfo("TIER0_SOURCE_DLQ")
			return err == nil && info.State.Msgs == 4
		}, 20*time.Second, 50*time.Millisecond)
		var failedRuns int64
		require.NoError(t, store.UnsDB().Model(&repo.StreamRun{}).Where("stream_id=? AND status='failed'", streamID).Count(&failedRuns).Error)
		require.EqualValues(t, 1, failedRuns, "logical replay preserves the original execution receipt")
	}
	row, err := unsmeta.New().SaveRecord(ctx, inputCollection, "pump", 0, map[string]any{"value": 7})
	require.NoError(t, err)
	verify(8)
	worker.Close()
	worker, err = sourcestreamworker.Start(ctx, url, service, sink)
	require.NoError(t, err)
	service.SetAsyncReady(worker.Ready)
	_, err = unsmeta.New().SaveRecord(ctx, inputCollection, "pump", row.Revision, map[string]any{"value": 8})
	require.NoError(t, err)
	verify(9)
	require.Eventually(t, func() bool {
		info, err := js.ConsumerInfo("TIER0_UNS_EVENTS", "tier0-uns-event-match-v1")
		return err == nil && info.NumPending == 0 && info.NumAckPending == 0
	}, 10*time.Second, 100*time.Millisecond)
	var samples int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", outputCollection).Count(&samples).Error)
	require.EqualValues(t, 2, samples, "restart does not replay the script or duplicate committed samples")
	checkpoint, err := repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, streamID, "process")
	require.NoError(t, err)
	require.JSONEq(t, `{"lastValue":8}`, string(checkpoint.Value))
	dlq, err := js.StreamInfo("TIER0_SOURCE_DLQ")
	require.NoError(t, err)
	if customID == "" {
		require.Zero(t, dlq.State.Msgs, "canonical events are not sent through the legacy Topic matcher")
	} else {
		expectedMessages := uint64(2)
		if customID == "plant0-known-failure" {
			expectedMessages = 4
		}
		require.EqualValues(t, expectedMessages, dlq.State.Msgs, "known failed executions and invalid inputs are isolated; valid events still execute")
		reasons := map[string]bool{}
		for sequence := dlq.State.FirstSeq; sequence <= dlq.State.LastSeq; sequence++ {
			message, err := js.GetMsg("TIER0_SOURCE_DLQ", sequence)
			require.NoError(t, err)
			if customID == "plant0-equipment-state" && message.Header.Get("Tier0-Error-Reason") == "invalidJSON" {
				require.Equal(t, "2", message.Header.Get("Tier0-Delivery-Count"), "only the failed quarantine publication requires redelivery")
			} else {
				require.Equal(t, "1", message.Header.Get("Tier0-Delivery-Count"), "permanent errors must not repeat and block the following events")
			}
			require.Equal(t, "permanent", message.Header.Get("Tier0-Failure-Class"))
			reasons[message.Header.Get("Tier0-Error-Reason")] = true
		}
		expectedReasons := map[string]bool{"invalidJSON": true, "invalidContract": true}
		if customID == "plant0-known-failure" {
			expectedReasons["recordedExecutionFailed"] = true
		}
		require.Equal(t, expectedReasons, reasons)
	}
}
