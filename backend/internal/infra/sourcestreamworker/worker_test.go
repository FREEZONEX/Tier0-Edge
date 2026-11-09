package sourcestreamworker_test

// Integration uses a real MQTT broker, JetStream, PostgreSQL and the production
// embedded Goja runtime. Endpoints must refer to dedicated protocol test services.
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/infra/sourceprotocol"
	"backend/internal/infra/sourcestreamworker"
	"backend/internal/infra/streamexecutor"
	"backend/internal/repo"
	"backend/internal/testkit"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

// 真实 JetStream 验证新版三表通知直发，以及传输关闭后数据写入仍成功。
func TestRealMetadataDirectNotificationsWithoutOutbox(t *testing.T) {
	natsURL := os.Getenv("TEST_SOURCE_NATS_URL")
	if natsURL == "" {
		t.Skip("requires dedicated TEST_SOURCE_NATS_URL")
	}
	store, dsn := testkit.NewTestDB(t)
	ctx := context.Background()
	sink, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer sink.Close()
	service := sourcestream.New(nil, nil, "")
	defer service.Close()
	worker, err := sourcestreamworker.Start(ctx, natsURL, service, sink)
	require.NoError(t, err)
	defer worker.Close()
	observer, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer observer.Close()
	sub, err := observer.SubscribeSync("tier0.events.topic.*")
	require.NoError(t, err)
	require.NoError(t, observer.Flush())
	metadata := unsmeta.New()
	pair, err := metadata.CreateCollection(ctx, "", unsmeta.Document{Name: "Direct NATS notification", RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	collectionID := pair["collection"].(repo.UnsMeta).ID
	row, err := metadata.SaveRecord(ctx, collectionID, "device", 0, map[string]any{"value": 1})
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		msg, err := sub.NextMsg(3 * time.Second)
		require.NoError(t, err)
		var event dataingest.StreamEvent
		require.NoError(t, json.Unmarshal(msg.Data, &event))
		require.Equal(t, collectionID, event.CollectionID)
		require.Equal(t, "device", event.RecordID)
		stored, err := repo.NewUnsMetaRepo(store).Record(ctx, collectionID, "device")
		require.NoError(t, err)
		require.JSONEq(t, `{"value":1}`, string(stored.Data))
	}
	worker.Close()
	row, err = metadata.SaveRecord(ctx, collectionID, "device", row.Revision, map[string]any{"value": 2})
	require.NoError(t, err, "closed NATS transport cannot roll back a committed write")
	require.EqualValues(t, 2, row.Revision)
	stored, err := repo.NewUnsMetaRepo(store).Record(ctx, collectionID, "device")
	require.NoError(t, err)
	require.JSONEq(t, `{"value":2}`, string(stored.Data))
	_, err = sub.NextMsg(150 * time.Millisecond)
	require.ErrorIs(t, err, nats.ErrTimeout, "there is no database-backed replay")
	for _, table := range []string{"public.uns_metadata_event_outbox", "public.stream_event_outbox"} {
		var count int64
		require.NoError(t, store.UnsDB().Table(table).Count(&count).Error)
		require.Zero(t, count)
	}
}

func TestRealPublishCommitAndSubscriptionDelivery(t *testing.T) {
	natsURL, brokerURL := os.Getenv("TEST_SOURCE_NATS_URL"), os.Getenv("TEST_SOURCE_MQTT_URL")
	if natsURL == "" || brokerURL == "" {
		t.Skip("requires dedicated TEST_SOURCE_NATS_URL and TEST_SOURCE_MQTT_URL")
	}
	store, dsn := testkit.NewTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = contextx.WithSubject(ctx, contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}}})
	executor := startExecutor(t, ctx)
	sink, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer sink.Close()
	runtime := sourceprotocol.New()
	service := sourcestream.New(runtime, executor, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	worker, err := sourcestreamworker.Start(ctx, natsURL, service, sink)
	require.NoError(t, err)
	defer worker.Close()
	service.SetAsyncReady(worker.Ready)
	service.SetWatchState(worker.WatchState)
	prefix := "test/" + uuid.NewString()
	inputTopic, outputTopic := prefix+"/pump/input", prefix+"/output"
	inputFilter := prefix + "/+/input"
	def := sourcestream.SourceDefinition{Name: "Protocol integration", Protocol: sourcestream.MQTT, Endpoint: strings.Replace(brokerURL, "tcp://", "mqtt://", 1), RuntimeLocation: "local", MQTT: &sourcestream.MQTTConnection{ClientID: uuid.NewString()}}
	source, err := service.CreateSource(ctx, def, 0)
	require.NoError(t, err)
	sourceID := source["id"].(int64)
	source, err = testkit.SetLegacySourceState(ctx, service, sourceID, 1, "enabled")
	require.NoError(t, err)
	_, err = service.Probe(ctx, sourceID, nil, 5000)
	require.NoError(t, err)
	_, err = service.Discover(ctx, sourceID, 2, "")
	require.NoError(t, err)
	root, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{Name: "Protocol tests", Type: 1}, nil, nil)
	require.NoError(t, err)
	topic, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{ParentID: root.ID, Name: "Temperature", Alias: "protocol_temp", Type: 2, TopicType: 3, EnableHistory: 1, Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}, nil, nil)
	require.NoError(t, err)
	pub := sourcestream.StreamDefinition{Collection: &sourcestream.CollectionDefinition{MQTT: &sourcestream.MQTTCollection{QoS: 1}}, Name: "Inbound mapping", SourceID: sourceID, Model: sourcestream.Publish, NativeResourceRef: "mqtt:" + inputFilter, TopicIDs: []int64{topic.ID}, Script: sourcestream.ScriptDefinition{Code: fmt.Sprintf(`if(context.resourceRef !== %q) throw new Error("actual Topic lost"); return {data:{value:input.value*2},drop:false,logs:[]};`, "mqtt:"+inputTopic), TimeoutMs: 1000}}
	pubView, err := service.SaveStream(ctx, 0, 0, pub)
	require.NoError(t, err)
	pubView, err = service.Deploy(ctx, pubView["id"].(int64), 1, source["configRevision"].(int64), 0)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		observed, err := service.Stream(ctx, pubView["id"].(int64))
		return err == nil && observed["executionState"] == "ready"
	}, 10*time.Second, 20*time.Millisecond, "real MQTT SUBACK, not Probe or a fixed sleep, establishes readiness")
	sub := sourcestream.StreamDefinition{Collection: &sourcestream.CollectionDefinition{MQTT: &sourcestream.MQTTCollection{QoS: 1}}, Name: "Outbound mapping", SourceID: sourceID, Model: sourcestream.Subscribe, NativeResourceRef: "mqtt:" + outputTopic, TopicIDs: []int64{topic.ID}, Script: sourcestream.ScriptDefinition{Code: `return {data:{command:input.value},drop:false,logs:[]};`, TimeoutMs: 1000}}
	subView, err := service.SaveStream(ctx, 0, 0, sub)
	require.NoError(t, err)
	_, err = service.Deploy(ctx, subView["id"].(int64), 1, source["configRevision"].(int64), 0)
	require.NoError(t, err)
	// Observe actual MQTT delivery, not an HTTP stand-in for the protocol.
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(brokerURL).SetClientID("observer-" + uuid.NewString()).SetConnectTimeout(3 * time.Second))
	token := client.Connect()
	require.True(t, token.WaitTimeout(5*time.Second))
	require.NoError(t, token.Error())
	defer client.Disconnect(100)
	var lock sync.Mutex
	var received []map[string]any
	token = client.Subscribe(outputTopic, 1, func(_ mqtt.Client, msg mqtt.Message) {
		var payload map[string]any
		if json.Unmarshal(msg.Payload(), &payload) == nil {
			lock.Lock()
			received = append(received, payload)
			lock.Unlock()
		}
	})
	require.True(t, token.WaitTimeout(5*time.Second))
	require.NoError(t, token.Error())
	// First exercise a fixed RAW envelope, then redeliver that exact envelope.
	conn, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer conn.Close()
	js, err := conn.JetStream()
	require.NoError(t, err)
	for _, name := range []string{"TIER0_SOURCE_RAW", "TIER0_UNS_EVENTS", "TIER0_SOURCE_DELIVERY", "TIER0_SOURCE_DLQ"} {
		info, err := js.StreamInfo(name)
		require.NoError(t, err)
		require.Equal(t, nats.DiscardOld, info.Config.Discard)
		require.Equal(t, time.Hour, info.Config.MaxAge)
		require.EqualValues(t, 100000, info.Config.MaxMsgs)
		require.EqualValues(t, 1<<30, info.Config.MaxBytes)
	}
	raw := sourcestream.RawInput{MessageID: uuid.NewString(), SourceID: sourceID, Input: sourcestream.SourceInput{ResourceRef: "mqtt:" + inputTopic, Data: map[string]any{"value": 7}, ReceivedAt: time.Now().UnixMilli()}, Plans: []sourcestream.ExecutionRef{{SourceID: sourceID, StreamID: pubView["id"].(int64), VersionID: pubView["activeVersion"].(int64), SourceRevision: source["configRevision"].(int64)}}}
	data, _ := json.Marshal(raw)
	_, err = js.Publish("tier0.raw.source."+jsonID(sourceID), data)
	require.NoError(t, err)
	require.Eventually(t, func() bool { lock.Lock(); defer lock.Unlock(); return len(received) > 0 }, 15*time.Second, 100*time.Millisecond)
	lock.Lock()
	require.EqualValues(t, 14, received[0]["command"])
	lock.Unlock()
	_, err = js.Publish("tier0.raw.source."+jsonID(sourceID), data)
	require.NoError(t, err)
	// Wait until the duplicate has gone through the consumer, not just a timer.
	require.Eventually(t, func() bool {
		info, err := js.ConsumerInfo("TIER0_SOURCE_RAW", "tier0-source-raw-v1")
		return err == nil && info.NumPending == 0 && info.NumAckPending == 0
	}, 10*time.Second, 100*time.Millisecond)
	var count int64
	require.NoError(t, store.UnsDB().Raw(`SELECT count(*) FROM public.protocol_temp`).Scan(&count).Error)
	require.EqualValues(t, 1, count, "duplicate RAW must not write a second history row")
	lock.Lock()
	require.Len(t, received, 1, "duplicate RAW must not repeat external delivery")
	lock.Unlock()
	// A fresh NATS publish must not renew an expired ingress envelope, including
	// native App/OpenAPI events received outside the retention window.
	raw.MessageID = uuid.NewString()
	raw.Input.ReceivedAt = time.Now().Add(-2 * time.Hour).UnixMilli()
	data, _ = json.Marshal(raw)
	_, err = js.Publish("tier0.raw.source."+jsonID(sourceID), data)
	require.NoError(t, err)
	event, _ := json.Marshal(dataingest.StreamEvent{MessageID: uuid.NewString(), TopicID: topic.ID, ReceivedAt: time.Now().Add(-2 * time.Hour), Data: map[string]any{"value": 999}})
	_, err = js.Publish("tier0.events.topic."+jsonID(topic.ID), event)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		for stream, consumer := range map[string]string{"TIER0_SOURCE_RAW": "tier0-source-raw-v1", "TIER0_UNS_EVENTS": "tier0-uns-event-match-v1"} {
			info, err := js.ConsumerInfo(stream, consumer)
			if err != nil || info.NumPending != 0 || info.NumAckPending != 0 {
				return false
			}
		}
		return true
	}, 10*time.Second, 100*time.Millisecond)
	require.NoError(t, store.UnsDB().Raw(`SELECT count(*) FROM public.protocol_temp`).Scan(&count).Error)
	require.EqualValues(t, 1, count, "expired RAW must not write UNS")
	before, err := js.StreamInfo("TIER0_UNS_EVENTS")
	require.NoError(t, err)
	outboxID := uuid.NewString()
	require.NoError(t, store.UnsDB().Exec(`INSERT INTO public.stream_receipt(receipt_id,stream_id,version_id,topic_id) VALUES(?,?,?,?)`, outboxID, raw.Plans[0].StreamID, raw.Plans[0].VersionID, topic.ID).Error)
	require.NoError(t, store.UnsDB().Exec(`INSERT INTO public.stream_event_outbox(event_id,subject,payload) VALUES(?,?,?::jsonb)`, outboxID, "tier0.events.topic."+jsonID(topic.ID), string(event)).Error)
	require.Never(t, func() bool {
		var sent int64
		return store.UnsDB().Raw(`SELECT count(*) FROM public.stream_event_outbox WHERE event_id=? AND sent_at IS NOT NULL`, outboxID).Scan(&sent).Error == nil && sent > 0
	}, 300*time.Millisecond, 30*time.Millisecond, "retired Outbox is not automatically drained")
	after, err := js.StreamInfo("TIER0_UNS_EVENTS")
	require.NoError(t, err)
	require.Equal(t, before.State.LastSeq, after.State.LastSeq, "retired Outbox is preserved without publishing any event")
	// An already-sent legacy DELIVERY envelope must not revive an expired
	// durable command, even though its JetStream publish timestamp is fresh.
	oldTaskID := uuid.NewString()
	subStream, err := repo.NewSourceStreamRepo(store).Stream(ctx, subView["id"].(int64))
	require.NoError(t, err)
	require.NoError(t, store.UnsDB().Create(&repo.StreamTask{ID: oldTaskID, SourceID: sourceID, StreamID: subStream.ID, VersionID: subStream.ActiveVersion, SourceRevision: source["configRevision"].(int64), Model: "subscribe", Output: `{"data":{"command":999},"drop":false}`, ReceivedAt: raw.Input.ReceivedAt, CreatedAt: time.Now().UnixMilli(), Status: "pending", SentAt: time.Now().UnixMilli()}).Error)
	require.NoError(t, store.UnsDB().Create(&repo.StreamRun{ID: oldTaskID, SourceID: sourceID, StreamID: subStream.ID, VersionID: subStream.ActiveVersion, Model: "subscribe", Status: "processing", MessageID: oldTaskID, Summary: "{}", CreatedAt: time.Now().UnixMilli()}).Error)
	command, _ := json.Marshal(map[string]string{"id": oldTaskID})
	_, err = js.Publish("tier0.delivery.task."+oldTaskID, command)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		task, err := repo.NewSourceStreamRepo(store).Task(ctx, oldTaskID)
		return err == nil && task.Status == "dropped"
	}, 10*time.Second, 100*time.Millisecond)
	lock.Lock()
	require.Len(t, received, 1, "expired EVENTS must not operate a device")
	lock.Unlock()
	// Source ingestion uses the real managed MQTT watch -> RAW path.
	token = client.Publish(inputTopic, 1, false, []byte(`{"value":11}`))
	require.True(t, token.WaitTimeout(5*time.Second))
	require.NoError(t, token.Error())
	require.Eventually(t, func() bool {
		lock.Lock()
		defer lock.Unlock()
		for _, item := range received {
			if item["command"] == float64(22) {
				return true
			}
		}
		return false
	}, 15*time.Second, 100*time.Millisecond)
	runs, total, err := repo.NewSourceStreamRepo(store).Runs(ctx, repo.StreamRunFilter{SourceID: sourceID})
	require.NoError(t, err)
	require.GreaterOrEqual(t, total, int64(4))
	for _, run := range runs {
		require.NotContains(t, run.Summary, `"command"`)
	}
	// A burst referencing a stopped Stream must be acknowledged once, not
	// head-of-line block the next Source for the full retry/backoff window.
	retiredDocument := pub
	retiredDocument.Name = "Retired acquisition"
	retired, err := service.SaveStream(ctx, 0, 0, retiredDocument)
	require.NoError(t, err)
	retired, err = service.Deploy(ctx, retired["id"].(int64), 1, source["configRevision"].(int64), 0)
	require.NoError(t, err)
	_, err = service.Disable(ctx, retired["id"].(int64), retired["revision"].(int64), false)
	require.NoError(t, err)
	deadBefore, err := js.StreamInfo("TIER0_SOURCE_DLQ")
	require.NoError(t, err)
	raw.Input.ReceivedAt = time.Now().UnixMilli()
	raw.Plans = []sourcestream.ExecutionRef{{SourceID: sourceID, StreamID: retired["id"].(int64), VersionID: retired["activeVersion"].(int64), SourceRevision: source["configRevision"].(int64)}}
	for i := 0; i < 50; i++ {
		raw.MessageID = uuid.NewString()
		data, _ = json.Marshal(raw)
		_, err = js.Publish("tier0.raw.source."+jsonID(sourceID), data)
		require.NoError(t, err)
	}
	token = client.Publish(inputTopic, 1, false, []byte(`{"value":13}`))
	require.True(t, token.WaitTimeout(5*time.Second))
	require.NoError(t, token.Error())
	require.Eventually(t, func() bool {
		lock.Lock()
		defer lock.Unlock()
		for _, item := range received {
			if item["command"] == float64(26) {
				return true
			}
		}
		return false
	}, 10*time.Second, 100*time.Millisecond, "retired RAW must not block valid acquisition")
	retiredRuns, retiredCount, err := repo.NewSourceStreamRepo(store).Runs(ctx, repo.StreamRunFilter{StreamID: retired["id"].(int64), Size: 100})
	require.NoError(t, err)
	require.EqualValues(t, 50, retiredCount)
	for _, run := range retiredRuns {
		require.Equal(t, "dropped", run.Status)
		require.Contains(t, run.Summary, "inactiveReference")
	}
	deadAfter, err := js.StreamInfo("TIER0_SOURCE_DLQ")
	require.NoError(t, err)
	require.Equal(t, deadBefore.State.LastSeq, deadAfter.State.LastSeq, "known inactive references must not burn retries then enter DLQ")
}

func jsonID(id int64) string { raw, _ := json.Marshal(id); return string(raw) }

func startExecutor(t *testing.T, ctx context.Context) *streamexecutor.Client {
	t.Helper()
	// Short path is required by AF_UNIX's pathname limit.
	dir, err := os.MkdirTemp("", "ss-executor-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	binary, socket := filepath.Join(dir, "executor"), filepath.Join(dir, "executor.sock")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", binary, "../../../cmd/stream-executor")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))
	process := exec.CommandContext(ctx, binary, "-socket", socket)
	require.NoError(t, process.Start())
	t.Cleanup(func() { _ = process.Process.Kill(); _ = process.Wait() })
	client := streamexecutor.New(socket)
	require.Eventually(t, func() bool { return client.Ready(ctx) == nil }, 5*time.Second, 50*time.Millisecond)
	return client
}

// Use a real broker behind a transparent wire observer: assert the negotiated
// filter QoS and that rebuilding a watcher does not deliver removed filters.
func TestRealMQTTWatchReplacesSubscriptions(t *testing.T) {
	brokerURL := os.Getenv("TEST_SOURCE_MQTT_URL")
	if brokerURL == "" {
		t.Skip("requires dedicated TEST_SOURCE_MQTT_URL")
	}
	broker, err := url.Parse(brokerURL)
	require.NoError(t, err)
	require.Contains(t, []string{"mqtt", "tcp"}, broker.Scheme)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	subscriptions := make(chan map[string]byte, 2)
	ready := make(chan struct{}, 2)
	go func() {
		for {
			downstream, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer downstream.Close()
				upstream, err := net.DialTimeout("tcp", broker.Host, 3*time.Second)
				if err != nil {
					return
				}
				defer upstream.Close()
				_ = downstream.SetDeadline(time.Now().Add(20 * time.Second))
				_ = upstream.SetDeadline(time.Now().Add(20 * time.Second))
				go func() {
					defer downstream.Close()
					for {
						packet, err := packets.ReadPacket(upstream)
						if err != nil {
							return
						}
						if packet.Write(downstream) != nil {
							return
						}
						if _, ok := packet.(*packets.SubackPacket); ok {
							ready <- struct{}{}
						}
					}
				}()
				for {
					packet, err := packets.ReadPacket(downstream)
					if err != nil {
						return
					}
					if sub, ok := packet.(*packets.SubscribePacket); ok {
						filters := make(map[string]byte)
						for i, topic := range sub.Topics {
							filters[topic] = sub.Qoss[i]
						}
						subscriptions <- filters
					}
					if packet.Write(upstream) != nil {
						return
					}
				}
			}()
		}
	}()
	prefix := "test/" + uuid.NewString()
	publisher := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(brokerURL).SetClientID("publisher-" + uuid.NewString()).SetConnectTimeout(3 * time.Second))
	token := publisher.Connect()
	require.True(t, token.WaitTimeout(5*time.Second))
	require.NoError(t, token.Error())
	defer publisher.Disconnect(100)
	runtime := sourceprotocol.New()
	def := (sourcestream.SourceDefinition{Name: "subscription regression", Protocol: sourcestream.MQTT, Endpoint: "mqtt://" + listener.Addr().String(), RuntimeLocation: "local", MQTT: &sourcestream.MQTTConnection{ClientID: uuid.NewString()}}).Connection()
	for _, filters := range []map[string]byte{
		{prefix + "/removed": 0, prefix + "/shared": 2},
		{prefix + "/added": 1, prefix + "/shared": 0},
	} {
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			def.MQTT.Subscriptions = filters
			inputs := make(chan sourcestream.SourceInput, 8)
			done := make(chan error, 1)
			go func() {
				done <- runtime.Watch(ctx, 1, def, sourcestream.Credential{}, func(input sourcestream.SourceInput) error { inputs <- input; return nil })
			}()
			defer func() {
				cancel()
				select {
				case err := <-done:
					require.ErrorIs(t, err, context.Canceled)
				case <-time.After(3 * time.Second):
					t.Error("watcher did not stop")
				}
			}()
			// The adapter can issue one SUBSCRIBE per filter. Verify the complete
			// protocol scope and wait for every SUBACK before publishing.
			actual := make(map[string]byte)
			packetsSent := 0
			for len(actual) < len(filters) {
				select {
				case packet := <-subscriptions:
					packetsSent++
					for topic, qos := range packet {
						require.Contains(t, filters, topic)
						actual[topic] = qos
					}
				case <-ctx.Done():
					t.Fatal("MQTT subscription was not sent")
				}
			}
			require.Equal(t, filters, actual)
			for i := 0; i < packetsSent; i++ {
				select {
				case <-ready:
				case <-ctx.Done():
					t.Fatal("MQTT subscription was not acknowledged")
				}
			}
			for _, suffix := range []string{"removed", "added", "shared"} {
				token := publisher.Publish(prefix+"/"+suffix, 2, false, `{"value":7}`)
				require.True(t, token.WaitTimeout(3*time.Second))
				require.NoError(t, token.Error())
			}
			received := make(map[string]bool)
			for len(received) < len(filters) {
				select {
				case input := <-inputs:
					topic := input.ResourceRef[len("mqtt:"):]
					require.Contains(t, filters, topic, "removed subscriptions must not reach the ingest callback")
					received[topic] = true
				case <-ctx.Done():
					t.Fatal("active subscription did not receive data")
				}
			}
			select {
			case input := <-inputs:
				t.Fatalf("unexpected extra delivery: %s", input.ResourceRef)
			case <-time.After(200 * time.Millisecond):
			}
		}()
	}
}
