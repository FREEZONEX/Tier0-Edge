package sourcestream_test

// 使用独立PG与真实Modbus TCP夹具，验证冻结授权、无副作用预览与持久操作日志。
// OPC UA/MQTT在此验证Host操作边界；它们的真实SDK协议链另由sourceprotocol测试覆盖。
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"backend/internal/contextx"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/infra/sourceprotocol"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/simonvetter/modbus"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type hostRegisters struct {
	mu     sync.Mutex
	values [2]uint16
	writes int
	calls  int
	delay  time.Duration
}

func (f *hostRegisters) HandleHoldingRegisters(r *modbus.HoldingRegistersRequest) ([]uint16, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if r.UnitId != 7 || int(r.Addr)+int(r.Quantity) > len(f.values) {
		return nil, modbus.ErrIllegalDataAddress
	}
	if r.IsWrite {
		f.writes++
		copy(f.values[r.Addr:], r.Args)
		time.Sleep(f.delay)
	}
	return append([]uint16(nil), f.values[r.Addr:int(r.Addr)+int(r.Quantity)]...), nil
}
func (f *hostRegisters) HandleInputRegisters(r *modbus.InputRegistersRequest) ([]uint16, error) {
	return f.HandleHoldingRegisters(&modbus.HoldingRegistersRequest{UnitId: r.UnitId, Addr: r.Addr, Quantity: r.Quantity})
}
func (f *hostRegisters) HandleCoils(*modbus.CoilsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalDataAddress
}
func (f *hostRegisters) HandleDiscreteInputs(*modbus.DiscreteInputsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalDataAddress
}

func hostSource(t *testing.T, store *repo.Store, id int64, def domain.SourceDefinition, resources []domain.NativeResource) {
	t.Helper()
	require.NoError(t, def.Validate())
	doc, err := json.Marshal(def)
	require.NoError(t, err)
	refs, err := json.Marshal(resources)
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(store)
	source, err := r.CreateSource(context.Background(), repo.Source{ID: id, Name: def.Name, Protocol: string(def.Protocol), Document: string(doc)})
	require.NoError(t, err)
	_, err = r.ChangeSource(context.Background(), id, source.Revision, func(_ *gorm.DB, item *repo.Source) error {
		item.Resources, item.DesiredState, item.Health = string(refs), "enabled", "unknown"
		return nil
	})
	require.NoError(t, err)
}
func hostManifest(id string, refs []string) (streamexec.ProgramManifest, []repo.StreamSourceSnapshot) {
	manifest := streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{{ID: id, Resources: refs}}, Topics: []streamexec.ProgramTopic{{TopicID: "123"}}, Entries: []streamexec.ProgramEntry{{ID: "tick", Kind: "interval", IntervalMs: 1000}}}
	numeric, _ := strconv.ParseInt(id, 10, 64)
	return manifest, []repo.StreamSourceSnapshot{{Alias: id, SourceID: numeric, ConfigRevision: 1, Resources: refs}}
}
func hostAdmit(t *testing.T, store *repo.Store, manifest streamexec.ProgramManifest, snapshots []repo.StreamSourceSnapshot, runID string) {
	t.Helper()
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	refs, err := json.Marshal(snapshots)
	require.NoError(t, err)
	var count int64
	require.NoError(t, store.UnsDB().Model(&repo.Stream{}).Where("id=8001").Count(&count).Error)
	if count == 0 {
		require.NoError(t, store.UnsDB().Create(&repo.Stream{ID: 8001, Name: "host-program", Model: "program", Document: `{}`, Revision: 1, Status: "enabled", ActiveVersion: 8002}).Error)
		require.NoError(t, store.UnsDB().Create(&repo.StreamVersion{ID: 8002, StreamID: 8001, SourceManifest: string(refs), ProgramManifest: string(encoded), Document: `{}`, ActivatedAt: time.Now().UnixMilli()}).Error)
	}
	reserved, err := repo.NewSourceStreamRepo(store).ReserveRun(context.Background(), repo.StreamRun{ID: runID, StreamID: 8001, VersionID: 8002, EntryID: "tick", Model: "program", Status: "processing", Summary: `{}`})
	require.NoError(t, err)
	require.True(t, reserved)
}
func hostPreviewCtx() context.Context {
	return contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.source": {}, "uns.stream.manage": {}}})
}

func TestProgramV2HostFrozenCatalogRejectsDeviceRetarget(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "tcp://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	registers := &hostRegisters{values: [2]uint16{31, 32}}
	server, err := modbus.NewServer(&modbus.ServerConfiguration{URL: endpoint, Timeout: time.Second, MaxClients: 10, Logger: log.New(io.Discard, "", 0)}, registers)
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() { require.NoError(t, server.Stop()) })
	runtime := sourceprotocol.New()
	runtime.Register(domain.ModbusTCP, sourceprotocol.ModbusDriver{})
	t.Cleanup(runtime.Close)
	s := domain.New(runtime, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	def := domain.SourceDefinition{Name: "Frozen Modbus", Protocol: domain.ModbusTCP, Endpoint: endpoint, RuntimeLocation: "local", Modbus: &domain.ModbusOptions{UnitID: 7, PollIntervalMs: 100, Ranges: []domain.ModbusRange{{ID: "value", Table: "holding-register", Address: 0, Quantity: 1}}}}
	// Publish and dispatch without pre-populating discovery.
	hostSource(t, store, 8101, def, []domain.NativeResource{})
	r := repo.NewSourceStreamRepo(store)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session"})
	initialSource, err := r.Source(ctx, 8101)
	require.NoError(t, err)
	require.NoError(t, r.ObserveSource(ctx, 8101, initialSource.Revision, "healthy", "", nil))
	manifest, snapshots := hostManifest("8101", []string{"modbus:value"})
	manifest.Topics = []streamexec.ProgramTopic{}
	doc := `{"script":"frozen host fixture"}`
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "frozen host", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, snapshots)
	require.NoError(t, err)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	plan, err := version.ProgramExecutionPlan()
	require.NoError(t, err)
	require.Len(t, plan.Sources, 1)
	reserved, err := r.ReserveRun(ctx, repo.StreamRun{ID: "frozen-host", StreamID: stream.ID, VersionID: version.ID, EntryID: "tick", Model: "program", Status: "processing", Summary: `{}`})
	require.NoError(t, err)
	require.True(t, reserved)
	host, err := s.ProgramV2Host(context.Background(), manifest, snapshots, "frozen-host", "tick", nil, false, nil, nil)
	require.NoError(t, err)
	read := streamexec.ProgramCall{ID: 1, Kind: "source.read", SourceID: "8101", ResourceRef: "modbus:value"}
	data, err := host(context.Background(), read)
	require.NoError(t, err)
	require.Contains(t, string(data), "31")
	write := streamexec.ProgramCall{ID: 2, Kind: "source.write", SourceID: "8101", ResourceRef: "modbus:value", Input: json.RawMessage(`{"values":[41]}`)}
	_, err = host(context.Background(), write)
	require.NoError(t, err)
	source, err := r.Source(ctx, 8101)
	require.NoError(t, err)
	resources, err := runtime.Discover(ctx, source.ID, def, domain.Credential{}, "")
	require.NoError(t, err)
	require.Len(t, resources, 1)

	observe := func() {
		t.Helper()
		raw, err := json.Marshal(resources)
		require.NoError(t, err)
		catalog := string(raw)
		require.NoError(t, r.ObserveSource(ctx, source.ID, source.Revision, "healthy", "", &catalog))
	}
	resources[0].Name = "Renamed value"
	resources[0].Metadata["metadataVerifiedAt"] = 2000
	observe()
	read.ID = 3
	_, err = host(context.Background(), read)
	require.NoError(t, err)
	registers.mu.Lock()
	callsBeforeRetarget := registers.calls
	registers.mu.Unlock()
	resources[0].Metadata["address"] = 1
	observe()
	current, err := r.Source(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, source.ConfigRevision, current.ConfigRevision)
	read.ID, write.ID = 4, 5
	_, err = host(context.Background(), read)
	require.ErrorIs(t, err, repo.ErrConflict)
	_, err = host(context.Background(), write)
	require.ErrorIs(t, err, repo.ErrConflict)
	registers.mu.Lock()
	defer registers.mu.Unlock()
	require.Equal(t, callsBeforeRetarget, registers.calls, "rejected calls must not reach the device")
	require.Equal(t, 1, registers.writes)
	require.Equal(t, [2]uint16{41, 32}, registers.values)
}

func TestProgramV2HostRealModbusJournalAndAuthorization(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "tcp://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	registers := &hostRegisters{values: [2]uint16{31, 32}}
	server, err := modbus.NewServer(&modbus.ServerConfiguration{URL: endpoint, Timeout: time.Second, MaxClients: 10, Logger: log.New(io.Discard, "", 0)}, registers)
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() { require.NoError(t, server.Stop()) })
	runtime := sourceprotocol.New()
	runtime.Register(domain.ModbusTCP, sourceprotocol.ModbusDriver{})
	t.Cleanup(runtime.Close)
	s := domain.New(runtime, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	def := domain.SourceDefinition{Name: "Host Modbus fixture", Protocol: domain.ModbusTCP, Endpoint: endpoint, RuntimeLocation: "local", Modbus: &domain.ModbusOptions{UnitID: 7, PollIntervalMs: 100, Ranges: []domain.ModbusRange{{ID: "value", Table: "holding-register", Address: 0, Quantity: 2}}}}
	// Match the discovery contract consumed by the host: configuration alone
	// does not authorize an unbounded or unspecified register range.
	hostSource(t, store, 8101, def, []domain.NativeResource{{ID: "modbus:value", Models: []domain.AccessModel{domain.Publish, domain.Subscribe, domain.RequestReply},
		Metadata: map[string]any{"unitId": 7, "table": "holding-register", "address": 0, "quantity": 2, "writable": true}}})
	manifest, snapshots := hostManifest("8101", []string{"modbus:value"})
	hostAdmit(t, store, manifest, snapshots, "host-real")
	reads := 0
	reader := func(_ context.Context, id int64) (json.RawMessage, error) {
		reads++
		require.Equal(t, int64(123), id)
		return json.RawMessage(`{"hasData":false,"data":null}`), nil
	}
	host, err := s.ProgramV2Host(context.Background(), manifest, snapshots, "host-real", "tick", reader, false, nil, nil)
	require.NoError(t, err)
	data, err := host(context.Background(), streamexec.ProgramCall{ID: 1, Kind: "source.read", SourceID: "8101", ResourceRef: "modbus:value"})
	require.NoError(t, err)
	require.Contains(t, string(data), `31`)
	data, err = host(context.Background(), streamexec.ProgramCall{ID: 2, Kind: "uns.read", TopicID: "123"})
	require.NoError(t, err)
	require.JSONEq(t, `{"hasData":false,"data":null}`, string(data))
	require.Equal(t, 1, reads)
	write := streamexec.ProgramCall{ID: 3, Kind: "source.write", SourceID: "8101", ResourceRef: "modbus:value", Input: json.RawMessage(`{"values":[41,42]}`)}
	data, err = host(context.Background(), write)
	require.NoError(t, err)
	require.JSONEq(t, `{"receipt":"operation-accepted","deviceExecuted":false}`, string(data))
	data, err = host(context.Background(), write)
	require.NoError(t, err)
	require.Contains(t, string(data), "operation-accepted")
	// Display-only edits retain connection authority and replay identity.
	renamed := def
	renamed.Name, renamed.Description = "Renamed", "display only"
	renamedJSON, err := json.Marshal(renamed)
	require.NoError(t, err)
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=8101").Updates(map[string]any{"name": renamed.Name, "document": string(renamedJSON)}).Error)
	data, err = host(context.Background(), write)
	require.NoError(t, err)
	// Invocation rights do not require source/stream management permission.
	executeCtx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.stream.execute": {}}})
	executeHost, err := s.ProgramV2Host(executeCtx, manifest, snapshots, "host-real", "tick", reader, false, nil, nil)
	require.NoError(t, err)
	_, err = executeHost(context.Background(), write)
	require.NoError(t, err)
	_, err = executeHost(context.Background(), streamexec.ProgramCall{ID: 2, Kind: "uns.read", TopicID: "123"})
	require.ErrorIs(t, err, domain.ErrForbidden)
	readCtx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}}})
	readHost, err := s.ProgramV2Host(readCtx, manifest, snapshots, "host-real", "tick", reader, false, nil, nil)
	require.NoError(t, err)
	_, err = readHost(context.Background(), write)
	require.ErrorIs(t, err, domain.ErrForbidden)
	// Every new SDK call rechecks Stream lifecycle, including non-device UNS reads.
	require.NoError(t, store.CommonDB().Model(&repo.Stream{}).Where("id=8001").Update("status", "disabled").Error)
	_, err = host(context.Background(), streamexec.ProgramCall{ID: 5, Kind: "uns.read", TopicID: "123"})
	require.ErrorContains(t, err, "SourceStream.inactiveReference")
	require.NoError(t, store.CommonDB().Model(&repo.Stream{}).Where("id=8001").Updates(map[string]any{"status": "enabled", "active_version": 8999}).Error)
	_, err = host(context.Background(), streamexec.ProgramCall{ID: 5, Kind: "uns.read", TopicID: "123"})
	require.NoError(t, err, "an admitted run may finish its old activated version")
	require.ErrorContains(t, store.UnsDB().Model(&repo.StreamVersion{}).Where("id=8002").Update("activated_at", 0).Error, "immutable program version")
	// Corrupt canonical metadata only inside this isolated fixture. Normal
	// repository/projection writes cannot revoke an activated version's time.
	require.NoError(t, store.UnsDB().Exec(`UPDATE public.uns_meta SET data=jsonb_set(data,'{activatedAt}','0'::jsonb)
		WHERE kind='source_plan' AND id='8002'`).Error)
	_, err = host(context.Background(), streamexec.ProgramCall{ID: 5, Kind: "uns.read", TopicID: "123"})
	require.ErrorContains(t, err, "SourceStream.inactiveReference")
	require.NoError(t, store.CommonDB().Model(&repo.Stream{}).Where("id=8001").Update("active_version", 8002).Error)
	require.NoError(t, store.UnsDB().Model(&repo.StreamVersion{}).Where("id=8002").Update("activated_at", time.Now().UnixMilli()).Error)
	// Simulate a process crash after durable intent, before result persistence.
	require.NoError(t, store.UnsDB().Model(&repo.StreamProgramEffect{}).Where("run_id=? AND call_id=?", "host-real", "3").Update("status", "dispatching").Error)
	_, err = host(context.Background(), write)
	require.ErrorIs(t, err, domain.ErrUnknownResult)
	require.NoError(t, store.UnsDB().Model(&repo.StreamProgramEffect{}).Where("run_id=? AND call_id=?", "host-real", "3").Update("status", "completed").Error)
	registers.mu.Lock()
	require.Equal(t, 1, registers.writes)
	registers.mu.Unlock()
	changed := write
	changed.Input = json.RawMessage(`{"values":[43,44]}`)
	_, err = host(context.Background(), changed)
	require.ErrorIs(t, err, repo.ErrConflict)
	tooLong := `{"timeoutMs":` + strconv.Itoa(streamexec.MaxProgramSourceTimeoutMs+1) + `}`
	for _, options := range []string{`null`, `[]`, `{"operation":"write"}`, `{"timeoutMs":0}`, tooLong, `{"timeoutMs":1,"url":"http://secret"}`} {
		bad := write
		bad.Options = json.RawMessage(options)
		_, err = host(context.Background(), bad)
		require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	}
	for _, input := range []string{`{"values":[65536,1]}`, `{"values":[1]}`, `{"operation":"read","values":[1,2]}`, `{"values":[1,2],"address":100}`} {
		bad := write
		bad.Input = json.RawMessage(input)
		_, err = host(context.Background(), bad)
		require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	}
	bad := write
	bad.ResourceRef = "modbus:other"
	_, err = host(context.Background(), bad)
	require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	bad = write
	bad.Kind = "source.invoke"
	_, err = host(context.Background(), bad)
	require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	// Timeout may follow actual device acceptance. Persist unknown, never retry.
	registers.mu.Lock()
	registers.delay = time.Second
	registers.mu.Unlock()
	unknown := write
	unknown.ID = 4
	unknown.Input = json.RawMessage(`{"values":[49,50]}`)
	// Leave budget for actual PG authorization/intent under concurrent tests;
	// the fixture withholds its native response beyond that fixed budget.
	unknown.Options = json.RawMessage(`{"timeoutMs":500}`)
	_, err = host(context.Background(), unknown)
	require.ErrorIs(t, err, domain.ErrUnknownResult)
	_, err = host(context.Background(), unknown)
	require.ErrorIs(t, err, domain.ErrUnknownResult)
	registers.mu.Lock()
	require.Equal(t, 2, registers.writes)
	require.Equal(t, [2]uint16{49, 50}, registers.values)
	registers.mu.Unlock()
	var effect repo.StreamProgramEffect
	require.NoError(t, store.UnsDB().Where("run_id=? AND call_id=?", "host-real", "4").Take(&effect).Error)
	require.Equal(t, "unknown", effect.Status)
	// Deletion/reconfiguration invalidates frozen authority; caller mutation cannot add refs.
	manifest.Sources[0].Resources = append(manifest.Sources[0].Resources, "modbus:other")
	_, err = host(context.Background(), bad)
	require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=8101").Update("config_revision", 2).Error)
	_, err = host(context.Background(), write)
	require.ErrorIs(t, err, repo.ErrConflict)
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=8101").Updates(map[string]any{"config_revision": 1, "desired_state": "disabled"}).Error)
	_, err = host(context.Background(), write)
	require.NoError(t, err)
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=8101").Update("deleted_at", time.Now().UnixMilli()).Error)
	_, err = host(context.Background(), write)
	require.ErrorIs(t, err, repo.ErrConflict)
	_, err = s.ProgramV2Host(context.Background(), manifest, snapshots, "missing", "tick", reader, false, nil, nil)
	require.Error(t, err)
}

func TestProgramV2HostDryRunFrozenSamplesAndProtocolBoundaries(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	s := domain.New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	opc := domain.SourceDefinition{Name: "OPC preview", Protocol: domain.OPCUA, Endpoint: "opc.tcp://unreachable:4840", RuntimeLocation: "local", OPCUA: &domain.OPCUAOptions{SecurityMode: "None", SecurityPolicy: "None", SamplingIntervalMs: 100, NodeIDs: []string{"temperature", "method"}}}
	hostSource(t, store, 8201, opc, []domain.NativeResource{{ID: "opcua:temperature", Models: []domain.AccessModel{domain.Publish, domain.Subscribe, domain.RequestReply}, Metadata: map[string]any{"nodeClass": 2}}, {ID: "opcua:method", Models: []domain.AccessModel{domain.RequestReply}, Metadata: map[string]any{"nodeClass": 4}}})
	manifest, snapshots := hostManifest("8201", []string{"opcua:temperature", "opcua:method"})
	samples := map[string]map[string]json.RawMessage{"8201": {"opcua:temperature": json.RawMessage(`{"value":7,"quality":"Good"}`)}}
	topics := map[string]json.RawMessage{"123": json.RawMessage(`{"data":null,"hasData":false}`)}
	host, err := s.ProgramV2Host(hostPreviewCtx(), manifest, snapshots, "", "tick", nil, true, samples, topics)
	require.NoError(t, err)
	samples["8201"]["opcua:temperature"] = json.RawMessage(`{"value":999}`)
	topics["123"] = json.RawMessage(`999`)
	data, err := host(context.Background(), streamexec.ProgramCall{ID: 1, Kind: "source.read", SourceID: "8201", ResourceRef: "opcua:temperature"})
	require.NoError(t, err)
	require.Contains(t, string(data), `7`)
	data, err = host(context.Background(), streamexec.ProgramCall{ID: 2, Kind: "uns.read", TopicID: "123"})
	require.NoError(t, err)
	require.JSONEq(t, `{"data":null,"hasData":false}`, string(data))
	for _, call := range []streamexec.ProgramCall{{ID: 3, Kind: "source.write", SourceID: "8201", ResourceRef: "opcua:temperature", Input: json.RawMessage(`{"value":9}`)}, {ID: 4, Kind: "source.invoke", SourceID: "8201", ResourceRef: "opcua:method", Input: json.RawMessage(`{"arguments":[]}`)}} {
		data, err = host(context.Background(), call)
		require.NoError(t, err)
		require.Contains(t, string(data), `"status":"planned"`)
		require.Contains(t, string(data), `"dispatched":false`)
	}
	for _, call := range []streamexec.ProgramCall{{ID: 5, Kind: "source.read", SourceID: "8201", ResourceRef: "opcua:method"}, {ID: 6, Kind: "source.invoke", SourceID: "8201", ResourceRef: "opcua:temperature", Input: json.RawMessage(`{"arguments":[]}`)}, {ID: 7, Kind: "source.write", SourceID: "8201", ResourceRef: "opcua:temperature", Input: json.RawMessage(`{"value":1,"operation":"call"}`)}} {
		_, err = host(context.Background(), call)
		require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	}
	var count int64
	require.NoError(t, store.UnsDB().Model(&repo.StreamProgramEffect{}).Count(&count).Error)
	require.Zero(t, count)
	_, err = s.ProgramV2Host(context.Background(), manifest, snapshots, "", "tick", nil, true, nil, nil)
	require.ErrorIs(t, err, domain.ErrForbidden)
	noSamples, err := s.ProgramV2Host(hostPreviewCtx(), manifest, snapshots, "", "tick", nil, true, nil, nil)
	require.NoError(t, err)
	_, err = noSamples(context.Background(), streamexec.ProgramCall{ID: 1, Kind: "source.read", SourceID: "8201", ResourceRef: "opcua:temperature"})
	require.ErrorIs(t, err, streamexec.ErrSourceCall)
	mqtt := domain.SourceDefinition{Name: "MQTT preview", Protocol: domain.MQTT, Endpoint: "mqtt://unreachable:1883", RuntimeLocation: "local", MQTT: &domain.MQTTOptions{ClientID: "preview", QoS: 1, TopicFilters: []string{"factory/+/data"}, RequestTopic: "factory/request", ReplyTopic: "factory/reply", CorrelationField: "id"}}
	hostSource(t, store, 8202, mqtt, []domain.NativeResource{{ID: "mqtt:factory/cmd", Models: []domain.AccessModel{domain.Subscribe}}, {ID: "mqtt:factory/+/data", Models: []domain.AccessModel{domain.Publish}}, {ID: "mqtt-request-reply", Models: []domain.AccessModel{domain.RequestReply}}})
	manifest, snapshots = hostManifest("8202", []string{"mqtt:factory/cmd", "mqtt:factory/+/data", "mqtt-request-reply"})
	mqttHost, err := s.ProgramV2Host(hostPreviewCtx(), manifest, snapshots, "", "tick", nil, true, nil, nil)
	require.NoError(t, err)
	for _, call := range []streamexec.ProgramCall{{ID: 1, Kind: "source.publish", SourceID: "8202", ResourceRef: "mqtt:factory/cmd", Input: json.RawMessage(`{"run":true}`)}, {ID: 2, Kind: "source.invoke", SourceID: "8202", ResourceRef: "mqtt-request-reply", Input: json.RawMessage(`{"run":true}`)}} {
		data, err = mqttHost(context.Background(), call)
		require.NoError(t, err)
		require.Contains(t, string(data), `"status":"planned"`)
	}
	for _, call := range []streamexec.ProgramCall{{ID: 3, Kind: "source.read", SourceID: "8202", ResourceRef: "mqtt:factory/+/data"}, {ID: 4, Kind: "source.publish", SourceID: "8202", ResourceRef: "mqtt:factory/+/data", Input: json.RawMessage(`{}`)}, {ID: 5, Kind: "source.invoke", SourceID: "8202", ResourceRef: "mqtt:factory/cmd", Input: json.RawMessage(`{}`)}} {
		_, err = mqttHost(context.Background(), call)
		require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	}
}

// 无依赖计算预览不强求数据读权限；一旦实际调用 UNS.read 仍检查既有权限。
func TestProgramV2HostPurePreviewPermissions(t *testing.T) {
	manifest := streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{}, Topics: []streamexec.ProgramTopic{}, Entries: []streamexec.ProgramEntry{{ID: "request", Kind: "request"}}}
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.stream.manage": {}}})
	s := domain.New(nil, nil, "")
	_, err := s.ProgramV2Host(ctx, manifest, []repo.StreamSourceSnapshot{}, "", "request", nil, true, nil, nil)
	require.NoError(t, err)
	manifest.Topics = []streamexec.ProgramTopic{{TopicID: "123"}}
	host, err := s.ProgramV2Host(ctx, manifest, []repo.StreamSourceSnapshot{}, "", "request", nil, true, nil, map[string]json.RawMessage{"123": json.RawMessage(`{"value":42}`)})
	require.NoError(t, err)
	_, err = host(context.Background(), streamexec.ProgramCall{ID: 1, Kind: "uns.read", TopicID: "123"})
	require.ErrorIs(t, err, domain.ErrForbidden)
}

func TestProgramV2HostScheduledNativePage(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	dbURL, err := url.Parse(dsn)
	require.NoError(t, err)
	password, _ := dbURL.User.Password()
	cred := domain.Credential{Kind: "basic", Username: dbURL.User.Username(), Secret: password}
	dbURL.User, dbURL.RawQuery = nil, ""
	dbURL.Scheme = "postgresql"
	published := programContext()
	s := domain.New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer s.Close()
	def := domain.SourceDefinition{Name: "Native page", Protocol: domain.EdgeSQL, Endpoint: dbURL.String(), RuntimeLocation: "local", SQL: &domain.SQLConnection{TLSMode: "disable", Queries: map[string]string{"sample": "SELECT current_database() AS db"}}}
	created, err := s.CreateSource(published, def, 0)
	require.NoError(t, err)
	id := created["id"].(int64)
	saved, err := s.SaveCredential(published, id, created["revision"].(int64), cred, "")
	require.NoError(t, err)
	idText := strconv.FormatInt(id, 10)
	manifest, snapshots := hostManifest(idText, []string{"sql:sample"})
	revision := saved["configRevision"].(int64)
	snapshots[0].ConfigRevision = revision
	hostAdmit(t, store, manifest, snapshots, "native-page")
	worker := context.Background()
	host, err := s.ProgramV2Host(worker, manifest, snapshots, "native-page", "tick", nil, false, nil, nil)
	require.NoError(t, err)
	call := streamexec.ProgramCall{ID: 1, Kind: "source.read", SourceID: idText, ResourceRef: "sql:sample", Options: json.RawMessage(`{"limit":1}`)}
	data, err := host(worker, call)
	require.NoError(t, err)
	var page struct {
		Items []struct {
			DB string `json:"db"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(data, &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, dbURL.Path, "/"+page.Items[0].DB)
	restricted := contextx.WithSubject(worker, contextx.Subject{ResourceKeys: map[string]struct{}{"uns.source": {}}})
	_, err = host(restricted, call)
	require.ErrorIs(t, err, domain.ErrForbidden)
	callerHost, err := s.ProgramV2Host(published, manifest, snapshots, "native-page", "tick", nil, false, nil, nil)
	require.NoError(t, err)
	_, err = callerHost(worker, call) // Invocation cannot drop its caller and acquire worker authority.
	require.ErrorIs(t, err, domain.ErrForbidden)
	restrictedHost, err := s.ProgramV2Host(restricted, manifest, snapshots, "native-page", "tick", nil, false, nil, nil)
	require.NoError(t, err)
	_, err = restrictedHost(published, call)
	require.ErrorIs(t, err, domain.ErrForbidden)
	_, err = s.EdgeResourceRead(worker, id, revision, "sql:sample", domain.EdgeResourceReadParams{}, 1000)
	require.ErrorIs(t, err, domain.ErrForbidden)
	call.ResourceRef = "sql:other"
	_, err = host(worker, call)
	require.ErrorIs(t, err, streamexec.ErrSourceDenied)
}
