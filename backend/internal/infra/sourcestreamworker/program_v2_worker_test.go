package sourcestreamworker

// 使用独立PG、独立真实JetStream及专用MQTT测试Broker验证候选不会接收业务回调。
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/infra/sourceprotocol"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProgramV2WatchRetryBoundAndIsolation(t *testing.T) {
	var retries programWatchRetries
	now := time.Now()
	spec := sourceWatchSpec{Revision: 2, Refs: []string{"mqtt:a", "mqtt:b"}, QoS: 1}
	require.True(t, retries.allowed(10, "100:101", now))
	for attempt := 0; attempt < 3; attempt++ {
		retries.record(10, "100:101", spec, now)
		require.False(t, retries.allowed(10, "100:101", now))
		require.True(t, retries.allowed(20, "200:201", now), "another Source must not be stopped")
		now = now.Add(time.Minute)
		if attempt < 2 {
			require.True(t, retries.allowed(10, "100:101", now))
		}
	}
	require.False(t, retries.allowed(10, "100:101", now.Add(time.Hour)))
	require.True(t, retries.allowed(10, "100:102", now), "a new candidate can retry independently")
	require.Equal(t, "source-watch-failed", retries.state(sourcestream.WatchQuery{SourceID: 10, ConfigRevision: 2, ResourceRefs: []string{"mqtt:b"}, QoS: 1}))
	require.Equal(t, "starting", retries.state(sourcestream.WatchQuery{SourceID: 10, ConfigRevision: 3, ResourceRefs: []string{"mqtt:b"}}))
	retries.clear(10, "wrong-version")
	require.False(t, retries.allowed(10, "100:101", now))
	retries.clear(10, "100:101")
	require.True(t, retries.allowed(10, "100:101", now))
}

func TestProgramV2WatchScopesKeepEntryModelsSeparate(t *testing.T) {
	ref := sourcestream.ExecutionRef{SourceID: 10, SourceRevision: 2, StreamID: 100, VersionID: 101}
	plans := []sourcestream.ExecutionPlan{
		{ExecutionRef: ref, Entry: &streamexec.ProgramEntry{ID: "one", Kind: "source-event", ResourceRefs: []string{"mqtt:b", "mqtt:a"}, QoS: 0}},
		{ExecutionRef: ref, Candidate: true, Entry: &streamexec.ProgramEntry{ID: "two", Kind: "source-event", ResourceRefs: []string{"mqtt:c", "mqtt:a"}, QoS: 1}},
		{ExecutionRef: ref, Entry: &streamexec.ProgramEntry{ID: "request", Kind: "request"}},
	}
	scopes, versions := watchSpecs(plans)
	require.Len(t, scopes, 1)
	require.Equal(t, []string{"mqtt:a", "mqtt:b", "mqtt:c"}, scopes[10].Refs)
	require.Equal(t, 1, scopes[10].QoS)
	require.Equal(t, "100:101", versions[10])
}

func programTestNATS(t *testing.T) (*nats.Conn, nats.JetStreamContext) {
	t.Helper()
	path, err := exec.LookPath("nats-server")
	if err != nil {
		t.Skip("real nats-server executable unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	cmd := exec.Command(path, "-js", "-sd", t.TempDir(), "-a", "127.0.0.1", "-p", strconv.Itoa(port))
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	var conn *nats.Conn
	require.Eventually(t, func() bool {
		conn, err = nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", port), nats.Timeout(100*time.Millisecond), nats.NoReconnect())
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	t.Cleanup(conn.Close)
	js, err := conn.JetStream()
	require.NoError(t, err)
	_, err = js.AddStream(&nats.StreamConfig{Name: "TEST_RAW", Subjects: []string{"tier0.raw.source.*"}, Storage: nats.FileStorage, Duplicates: time.Minute})
	require.NoError(t, err)
	return conn, js
}

func programWorkerDefinition(t *testing.T, name string) string {
	t.Helper()
	raw, err := json.Marshal(sourcestream.StreamDefinition{Model: sourcestream.ProgramAccess,
		Name: name, Script: sourcestream.ScriptDefinition{Code: `module.exports=api=>{api.stream.onRequest({id:"unused"},()=>1);};`, TimeoutMs: 1000}})
	require.NoError(t, err)
	return string(raw)
}

func programFixtureSource(t *testing.T, store *repo.Store, item repo.Source) repo.Source {
	t.Helper()
	r := repo.NewSourceStreamRepo(store)
	created, err := r.CreateSource(context.Background(), item)
	require.NoError(t, err)
	updated, err := r.ChangeSource(context.Background(), created.ID, created.Revision, func(_ *gorm.DB, target *repo.Source) error {
		target.DesiredState, target.Health = "enabled", "healthy"
		target.Resources = item.Resources
		if target.Resources == "" {
			target.Resources = "[]"
		}
		return nil
	})
	require.NoError(t, err)
	return updated
}

func programFixtureActive(t *testing.T, r *repo.SourceStreamRepo, name, document string, manifest streamexec.ProgramManifest, snapshots []repo.StreamSourceSnapshot) (repo.Stream, repo.StreamVersion) {
	t.Helper()
	ctx := context.Background()
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: name, Model: "program", Document: document}, 0, manifest)
	require.NoError(t, err)
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, document, manifest, snapshots)
	require.NoError(t, err)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	version, err = r.Version(ctx, stream.ID, version.ID)
	require.NoError(t, err)
	require.Positive(t, version.ActivatedAt)
	return stream, version
}

func TestProgramV2IntervalRealPubAckAndExpiredAudit(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	conn, js := programTestNATS(t)
	ctx := context.Background()
	manifest := streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{}, Topics: []streamexec.ProgramTopic{}, Entries: []streamexec.ProgramEntry{{ID: "tick", Kind: "interval", IntervalMs: 1000}}}
	r := repo.NewSourceStreamRepo(store)
	stream, version := programFixtureActive(t, r, "interval-worker", programWorkerDefinition(t, "interval-worker"), manifest, []repo.StreamSourceSnapshot{})
	// Advance the scheduler clock; never rewrite an immutable activated version.
	now := time.UnixMilli(version.ActivatedAt).Add(3500 * time.Millisecond)
	w := &Worker{connection: conn, js: js, policy: messagePolicy{maxAge: 2 * time.Second}}
	plan := sourcestream.ExecutionPlan{ExecutionRef: sourcestream.ExecutionRef{StreamID: stream.ID, VersionID: version.ID, EntryID: "tick"}, Entry: &manifest.Entries[0]}
	require.NoError(t, w.publishProgramIntervals(ctx, []sourcestream.ExecutionPlan{plan}, now))
	info, err := js.StreamInfo("TEST_RAW")
	require.NoError(t, err)
	require.EqualValues(t, 2, info.State.Msgs, "one expired slot is discarded, two real messages have PubAck")
	var intents []repo.StreamProgramIntervalOutbox
	require.NoError(t, store.UnsDB().Order("due_at").Find(&intents).Error)
	require.Len(t, intents, 3)
	require.EqualValues(t, -1, intents[0].SentAt)
	require.Positive(t, intents[1].SentAt)
	require.Positive(t, intents[2].SentAt)
	first, err := js.GetMsg("TEST_RAW", 1)
	require.NoError(t, err)
	var input sourcestream.RawInput
	require.NoError(t, json.Unmarshal(first.Data, &input))
	// Publication is ordered by created_at/id, not due_at. Correlate the
	// accepted message to its actual durable intent instead of assuming that
	// random hash order coincides with the schedule's due-time order.
	var accepted *repo.StreamProgramIntervalOutbox
	for i := range intents {
		if intents[i].ID == input.MessageID {
			accepted = &intents[i]
		}
	}
	require.NotNil(t, accepted)
	require.Positive(t, accepted.SentAt)
	require.Equal(t, "tick", input.Plans[0].EntryID)
	require.Equal(t, accepted.DueAt, input.Input.ReceivedAt)
	// Simulate the crash window after broker acceptance but before DB sent marker.
	require.NoError(t, store.UnsDB().Model(&repo.StreamProgramIntervalOutbox{}).Where("id=?", accepted.ID).Update("sent_at", 0).Error)
	require.NoError(t, w.publishProgramIntervals(ctx, []sourcestream.ExecutionPlan{plan}, now))
	info, err = js.StreamInfo("TEST_RAW")
	require.NoError(t, err)
	require.EqualValues(t, 2, info.State.Msgs, "stable MsgId deduplicates the replay")
	require.NoError(t, conn.Flush())
	conn.Close()
	_, err = repo.NewSourceStreamRepo(store).ScheduleProgramInterval(ctx, repo.ProgramIntervalSchedule{StreamID: stream.ID, VersionID: version.ID, EntryID: "tick", IntervalMs: 1000, FirstDueAt: version.ActivatedAt + 1000, Now: now.Add(time.Second).UnixMilli(), MaxSlots: 1})
	require.NoError(t, err)
	require.Error(t, w.publishProgramIntervals(ctx, []sourcestream.ExecutionPlan{plan}, now.Add(time.Second)))
	pending, err := repo.NewSourceStreamRepo(store).PendingProgramIntervals(ctx, 100)
	require.NoError(t, err)
	require.Len(t, pending, 1, "no PubAck leaves the durable publication intent pending")
}

// 离线Source的过期意图即使没有active plan也必须终结，不能用满一批阻塞后续正常定时入口。
func TestProgramV2OfflineExpiredBatchDoesNotStarveLiveInterval(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	conn, js := programTestNATS(t)
	ctx := context.Background()
	now := time.Now()
	r := repo.NewSourceStreamRepo(store)
	dependency := programFixtureSource(t, store, repo.Source{ID: 8101, Name: "offline dependency", Protocol: "mqtt", Document: `{}`, Resources: "[]"})
	var offlineID, liveID int64
	for index, id := range []int64{7101, 7201} {
		manifest := streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{}, Topics: []streamexec.ProgramTopic{}, Entries: []streamexec.ProgramEntry{{ID: "tick", Kind: "interval", IntervalMs: 1000}}}
		snapshots := []repo.StreamSourceSnapshot{}
		activated := now.Add(-1500 * time.Millisecond).UnixMilli()
		if index == 0 {
			manifest.Sources = []streamexec.ProgramSource{{ID: "8101", Resources: []string{}}}
			snapshots = []repo.StreamSourceSnapshot{{Alias: "8101", SourceID: 8101, ConfigRevision: 1, Resources: []string{}}}
			activated = now.Add(-201 * time.Second).UnixMilli()
		}
		stream, version := programFixtureActive(t, r, fmt.Sprintf("interval-%d", id), programWorkerDefinition(t, fmt.Sprintf("interval-%d", id)), manifest, snapshots)
		if index == 0 {
			offlineID = stream.ID
		} else {
			liveID = stream.ID
		}
		// Seed an existing offline cursor with old due slots; the version itself
		// remains immutable and this tests backlog recovery, not activation time.
		request := repo.ProgramIntervalSchedule{StreamID: stream.ID, VersionID: version.ID, EntryID: "tick", IntervalMs: 1000, FirstDueAt: activated + 1000, Now: now.UnixMilli(), MaxSlots: 1}
		if index == 0 {
			request.Now, request.MaxSlots = now.Add(-101*time.Second).UnixMilli(), 100
		}
		items, err := r.ScheduleProgramInterval(ctx, request)
		require.NoError(t, err)
		if index == 0 {
			require.Len(t, items, 100)
		} else {
			require.Len(t, items, 1)
		}
	}
	_, err := r.ChangeSource(ctx, dependency.ID, dependency.Revision, func(_ *gorm.DB, target *repo.Source) error { target.DeletedAt = time.Now().UnixMilli(); return nil })
	require.NoError(t, err)
	service := sourcestream.New(nil, nil, "")
	plans, err := service.Plans(ctx)
	require.NoError(t, err)
	require.Len(t, plans, 1, "offline dependency removes only that Stream from active execution plans")
	require.Equal(t, liveID, plans[0].StreamID)
	w := &Worker{connection: conn, js: js, policy: messagePolicy{maxAge: 2 * time.Second}}
	require.NoError(t, w.publishProgramIntervals(ctx, plans, now))
	var discarded int64
	require.NoError(t, store.UnsDB().Model(&repo.StreamProgramIntervalOutbox{}).Where("stream_id=? AND sent_at=-1", offlineID).Count(&discarded).Error)
	require.EqualValues(t, 100, discarded)
	require.NoError(t, w.publishProgramIntervals(ctx, plans, now))
	info, err := js.StreamInfo("TEST_RAW")
	require.NoError(t, err)
	require.EqualValues(t, 1, info.State.Msgs)
	message, err := js.GetMsg("TEST_RAW", 1)
	require.NoError(t, err)
	var input sourcestream.RawInput
	require.NoError(t, json.Unmarshal(message.Data, &input))
	require.Equal(t, liveID, input.Plans[0].StreamID)
	require.Equal(t, "tick", input.Plans[0].EntryID)
	pending, err := r.PendingProgramIntervals(ctx, 200)
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestProgramV2CandidateRealMQTTKeepsActiveVersion(t *testing.T) {
	broker := os.Getenv("TEST_SOURCE_MQTT_URL")
	if broker == "" {
		t.Skip("dedicated real MQTT test Broker not configured")
	}
	store, _ := testkit.NewTestDB(t)
	conn, js := programTestNATS(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := repo.NewSourceStreamRepo(store)
	base := "tier0-g2-test/" + uuid.NewString()
	refs := []string{"mqtt:" + base + "/a", "mqtt:" + base + "/b"}
	for i, endpoint := range []string{strings.Replace(broker, "tcp://", "mqtt://", 1), "mqtt://127.0.0.1:1"} {
		id := int64(8101 + i)
		def := sourcestream.SourceDefinition{Name: fmt.Sprintf("test-source-%d", id), Protocol: sourcestream.MQTT, Endpoint: endpoint, RuntimeLocation: "local", MQTT: &sourcestream.MQTTOptions{ClientID: "g2-source-" + uuid.NewString(), QoS: 1, TopicFilters: []string{base + "/a", base + "/b"}}}
		require.NoError(t, def.Validate())
		raw, err := json.Marshal(def)
		require.NoError(t, err)
		resources, err := json.Marshal([]sourcestream.NativeResource{{ID: refs[0], Models: []sourcestream.AccessModel{sourcestream.Publish}}, {ID: refs[1], Models: []sourcestream.AccessModel{sourcestream.Publish}}})
		require.NoError(t, err)
		programFixtureSource(t, store, repo.Source{ID: id, Name: def.Name, Protocol: "mqtt", Document: string(raw), Resources: string(resources)})
	}
	manifest := streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{{ID: "8101", Resources: refs}}, Topics: []streamexec.ProgramTopic{}, Entries: []streamexec.ProgramEntry{{ID: "active", Kind: "source-event", SourceID: "8101", ResourceRefs: refs[:1], QoS: 1}}}
	doc := programWorkerDefinition(t, "mqtt-candidate")
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "mqtt-candidate", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	snapshot := repo.StreamSourceSnapshot{Alias: "8101", SourceID: 8101, ConfigRevision: 1, Resources: refs}
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{snapshot})
	require.NoError(t, err)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	runtime := sourceprotocol.New()
	defer runtime.Close()
	service := sourcestream.New(runtime, nil, "")
	w := &Worker{connection: conn, js: js, service: service, policy: messagePolicy{maxAge: time.Minute}}
	w.ready.Store(true)
	service.SetAsyncReady(w.Ready)
	service.SetWatchState(w.WatchState)
	done := make(chan struct{})
	go func() { defer close(done); w.sourceLoop(ctx) }()
	defer func() { cancel(); <-done }()
	query := sourcestream.WatchQuery{SourceID: 8101, ConfigRevision: 1, ResourceRefs: refs[:1], QoS: 1}
	require.Eventually(t, func() bool { return w.WatchState(query) == "ready" }, 8*time.Second, 50*time.Millisecond)
	manifest.Sources = append(manifest.Sources, streamexec.ProgramSource{ID: "8102", Resources: refs})
	manifest.Entries = []streamexec.ProgramEntry{{ID: "new-a", Kind: "source-event", SourceID: "8101", ResourceRefs: refs, QoS: 1}, {ID: "offline-b", Kind: "source-event", SourceID: "8102", ResourceRefs: refs[:1], QoS: 1}}
	stream, err = r.SaveProgramV2(ctx, repo.Stream{ID: stream.ID, Name: stream.Name, Model: "program", Document: doc}, stream.Revision, manifest)
	require.NoError(t, err)
	stream, candidate, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{snapshot, {Alias: "8102", SourceID: 8102, ConfigRevision: 1, Resources: refs}})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return w.WatchState(sourcestream.WatchQuery{SourceID: 8102, ConfigRevision: 1, ResourceRefs: refs[:1], QoS: 1}) == "source-watch-failed"
	}, 10*time.Second, 50*time.Millisecond)
	// The same Source scope expansion has a truthful starting interval while
	// its union subscription receives SUBACK; do not publish before actual ready.
	require.Eventually(t, func() bool { return w.WatchState(query) == "ready" }, 8*time.Second, 50*time.Millisecond)
	current, err := r.Stream(ctx, stream.ID)
	require.NoError(t, err)
	require.Equal(t, version.ID, current.ActiveVersion)
	require.Equal(t, candidate.ID, current.PendingVersion)
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID("g2-publisher-" + uuid.NewString()).SetAutoReconnect(false).SetConnectTimeout(time.Second))
	token := client.Connect()
	require.True(t, token.WaitTimeout(3*time.Second))
	require.NoError(t, token.Error())
	defer client.Disconnect(100)
	for _, topic := range []string{base + "/a", base + "/b"} {
		token = client.Publish(topic, 1, false, `{"value":42}`)
		require.True(t, token.WaitTimeout(3*time.Second))
		require.NoError(t, token.Error())
	}
	require.Eventually(t, func() bool { info, err := js.StreamInfo("TEST_RAW"); return err == nil && info.State.Msgs == 1 }, 5*time.Second, 50*time.Millisecond)
	msg, err := js.GetMsg("TEST_RAW", 1)
	require.NoError(t, err)
	var input sourcestream.RawInput
	require.NoError(t, json.Unmarshal(msg.Data, &input))
	require.Equal(t, refs[0], input.Input.ResourceRef)
	require.Len(t, input.Plans, 1)
	require.Equal(t, "active", input.Plans[0].EntryID)
	require.Equal(t, version.ID, input.Plans[0].VersionID, "failed candidate cannot receive callbacks")
	// Repair only the private fixture, then restage a fresh frozen snapshot.
	// A real protocol Probe and both real SUBACKs are required for activation.
	offline, err := r.Source(ctx, 8102)
	require.NoError(t, err)
	var repaired sourcestream.SourceDefinition
	require.NoError(t, json.Unmarshal([]byte(offline.Document), &repaired))
	repaired.Endpoint = strings.Replace(broker, "tcp://", "mqtt://", 1)
	repairedJSON, err := json.Marshal(repaired)
	require.NoError(t, err)
	_, err = r.ChangeSource(ctx, offline.ID, offline.Revision, func(_ *gorm.DB, target *repo.Source) error {
		target.Document = string(repairedJSON)
		target.ConfigRevision = 2
		target.Health = "unverified"
		return nil
	})
	require.NoError(t, err)
	_, err = service.Probe(ctx, 8102, nil, 3000)
	require.NoError(t, err)
	current, err = r.Stream(ctx, stream.ID)
	require.NoError(t, err)
	_, replacement, err := r.StageProgramV2(ctx, current.ID, current.Revision, 0, doc, manifest,
		[]repo.StreamSourceSnapshot{snapshot, {Alias: "8102", SourceID: 8102, ConfigRevision: 2, Resources: refs}})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		item, err := r.Stream(ctx, stream.ID)
		return err == nil && item.ActiveVersion == replacement.ID && item.PendingVersion == 0
	}, 10*time.Second, 50*time.Millisecond, "candidate activates only after all real protocol watches are ready")
	token = client.Publish(base+"/b", 1, false, `{"value":43}`)
	require.True(t, token.WaitTimeout(3*time.Second))
	require.NoError(t, token.Error())
	require.Eventually(t, func() bool { info, err := js.StreamInfo("TEST_RAW"); return err == nil && info.State.Msgs == 2 }, 5*time.Second, 50*time.Millisecond)
	msg, err = js.GetMsg("TEST_RAW", 2)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(msg.Data, &input))
	require.Equal(t, refs[1], input.Input.ResourceRef)
	require.Len(t, input.Plans, 1)
	require.Equal(t, "new-a", input.Plans[0].EntryID)
	require.Equal(t, replacement.ID, input.Plans[0].VersionID)
}
