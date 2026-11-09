package sourceprotocol

import (
	"context"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"github.com/simonvetter/modbus"
	"github.com/stretchr/testify/require"
)

// Model a delayed AfterFunc callback deterministically: lifecycle cancellation
// is immediate, but the derived operation context is not yet notified.
type delayedPoolCancellation struct{ context.Context }

func (delayedPoolCancellation) Value(any) any { return nil }
func (delayedPoolCancellation) AfterFunc(func()) func() bool {
	return func() bool { return true }
}

func TestSourceConnectionPoolCancellationDoesNotDependOnAfterFunc(t *testing.T) {
	for _, phase := range []string{"connect", "action", "reconnect"} {
		t.Run(phase, func(t *testing.T) {
			pool := newSourceConnectionPool()
			t.Cleanup(pool.close)
			def := sourcestream.SourceDefinition{Protocol: sourcestream.MQTT, Endpoint: "mqtt://127.0.0.1:1883", MQTT: &sourcestream.MQTTOptions{ClientID: "delayed"}}
			entry, err := pool.entry(context.Background(), 81, def, sourcestream.Credential{})
			require.NoError(t, err)
			entry.ctx = delayedPoolCancellation{entry.ctx}
			connect := func(opCtx context.Context) (any, func(), error) {
				if phase == "connect" {
					pool.releaseSource(81)
				}
				require.NoError(t, opCtx.Err(), "the callback has deliberately not run")
				return "old", func() {}, nil
			}
			result, err := pool.withEntry(context.Background(), entry, connect, func(opCtx context.Context, value any) (any, error) {
				if phase == "action" {
					pool.releaseSource(81)
				}
				if phase == "reconnect" {
					return pool.replaceValue(opCtx, entry, func(ctx context.Context) (any, func(), error) {
						pool.releaseSource(81)
						return "reconnected old", func() {}, nil
					})
				}
				require.NoError(t, opCtx.Err())
				return value, nil
			})
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, result, "a released generation must never report success")
			current, err := pool.with(context.Background(), 81, def, sourcestream.Credential{},
				func(context.Context) (any, func(), error) { return "new", func() {}, nil },
				func(_ context.Context, value any) (any, error) { return value, nil })
			require.NoError(t, err)
			require.Equal(t, "new", current)
		})
	}
}

func TestSourceConnectionPoolIsolationLimitsAndGenerationCleanup(t *testing.T) {
	pool := newSourceConnectionPool()
	pool.limit, pool.idle = 2, 25*time.Millisecond
	defer pool.close()
	def := sourcestream.SourceDefinition{Name: "display", Protocol: sourcestream.MQTT, Endpoint: "mqtt://127.0.0.1:1883", MQTT: &sourcestream.MQTTOptions{ClientID: "one"}}
	var connected, closed atomic.Int32
	connect := func(context.Context) (any, func(), error) {
		value := connected.Add(1)
		return value, func() { closed.Add(1) }, nil
	}
	action := func(_ context.Context, value any) (any, error) { return value, nil }
	ctx := WithSourceRevision(context.Background(), 1)
	one, err := pool.with(ctx, 1, def, sourcestream.Credential{}, connect, action)
	require.NoError(t, err)
	def.Name = "renamed display"
	again, err := pool.with(ctx, 1, def, sourcestream.Credential{}, connect, action)
	require.NoError(t, err)
	require.Equal(t, one, again)
	old, err := pool.entry(ctx, 1, def, sourcestream.Credential{})
	require.NoError(t, err)
	current, err := pool.with(WithSourceRevision(ctx, 2), 1, def, sourcestream.Credential{}, connect, action)
	require.NoError(t, err)
	require.NotEqual(t, one, current)
	pool.retire(old)
	again, err = pool.with(WithSourceRevision(ctx, 2), 1, def, sourcestream.Credential{}, connect, action)
	require.NoError(t, err)
	require.Equal(t, current, again, "old cancellation cannot close a new generation")
	_, err = pool.with(ctx, 2, def, sourcestream.Credential{}, connect, action)
	require.NoError(t, err)
	_, err = pool.with(ctx, 3, def, sourcestream.Credential{}, connect, action)
	require.ErrorIs(t, err, sourcestream.ErrUnavailable)
	pool.releaseSource(1)
	_, err = pool.with(ctx, 3, def, sourcestream.Credential{}, connect, action)
	require.NoError(t, err)
	require.Eventually(t, func() bool { pool.mu.Lock(); defer pool.mu.Unlock(); return len(pool.entries) == 0 }, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return closed.Load() == connected.Load() }, time.Second, 5*time.Millisecond)
	pool.close()
	_, err = pool.with(ctx, 4, def, sourcestream.Credential{}, connect, action)
	require.ErrorIs(t, err, sourcestream.ErrUnavailable)
}

func TestSourceConnectionPoolCredentialRoleAndCancelledWaiter(t *testing.T) {
	pool := newSourceConnectionPool()
	defer pool.close()
	def := sourcestream.SourceDefinition{Protocol: sourcestream.MQTT, Endpoint: "mqtt://127.0.0.1:1883", MQTT: &sourcestream.MQTTOptions{ClientID: "one"}}
	ctx := context.Background()
	first, err := sourceConnectionKey(ctx, 1, def, sourcestream.Credential{Kind: "basic", Secret: "test-one"})
	require.NoError(t, err)
	changed, err := sourceConnectionKey(ctx, 1, def, sourcestream.Credential{Kind: "basic", Secret: "test-two"})
	require.NoError(t, err)
	require.NotEqual(t, first, changed)
	def.Role = sourcestream.ServerRole
	server, err := sourceConnectionKey(ctx, 1, def, sourcestream.Credential{Kind: "basic", Secret: "test-one"})
	require.NoError(t, err)
	require.NotEqual(t, first, server)
	connect := func(context.Context) (any, func(), error) { return true, func() {}, nil }
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := pool.with(ctx, 1, def, sourcestream.Credential{}, connect, func(context.Context, any) (any, error) { close(entered); <-release; return nil, nil })
		done <- err
	}()
	<-entered
	wait, cancel := context.WithCancel(ctx)
	cancel()
	_, err = pool.with(wait, 1, def, sourcestream.Credential{}, connect, func(context.Context, any) (any, error) { t.Error("cancelled operation executed"); return nil, nil })
	require.ErrorIs(t, err, context.Canceled)
	close(release)
	require.NoError(t, <-done)
}

func TestSourceConnectionPoolUnsavedProbeBoundAndClosedRuntime(t *testing.T) {
	pool := newSourceConnectionPool()
	pool.limit = 1
	defer pool.close()
	def := sourcestream.SourceDefinition{Protocol: sourcestream.MQTT, Endpoint: "mqtt://127.0.0.1:1883", MQTT: &sourcestream.MQTTOptions{ClientID: "test"}}
	entry, err := pool.entry(context.Background(), 0, def, sourcestream.Credential{})
	require.NoError(t, err)
	_, err = pool.entry(context.Background(), 0, def, sourcestream.Credential{})
	require.ErrorIs(t, err, sourcestream.ErrUnavailable, "unsaved probes cannot bypass pool capacity")
	pool.retire(entry)
	_, err = pool.with(context.Background(), 0, def, sourcestream.Credential{}, func(context.Context) (any, func(), error) { return true, func() {}, nil }, func(context.Context, any) (any, error) { return true, nil })
	require.NoError(t, err)
	require.Empty(t, pool.entries)
	runtime := New()
	runtime.Close()
	require.ErrorIs(t, runtime.Probe(context.Background(), 1, def, sourcestream.Credential{}), sourcestream.ErrUnavailable)
	_, err = runtime.Discover(context.Background(), 1, def, sourcestream.Credential{}, "")
	require.ErrorIs(t, err, sourcestream.ErrUnavailable)
	_, err = runtime.Invoke(context.Background(), 1, def, sourcestream.Credential{}, "mqtt:a", nil)
	require.ErrorIs(t, err, sourcestream.ErrUnavailable)
	require.ErrorIs(t, runtime.Watch(context.Background(), 1, def, sourcestream.Credential{}, func(Input) error { return nil }), sourcestream.ErrUnavailable)
}

type lifecycleFixture struct {
	Driver
	releases, closes int
}

func TestMQTTCollectionAndProgramShareConnectionIdentityNotWatchIdentity(t *testing.T) {
	ctx := WithSourceRevision(context.Background(), 4)
	base := sourcestream.SourceDefinition{Name: "shared", Protocol: sourcestream.MQTT, Endpoint: "mqtt://localhost:1883", MQTT: &sourcestream.MQTTOptions{ClientID: "shared"}}
	collection := base.Connection()
	collection.MQTT.QoS = 1
	collection.MQTT.TopicFilters = []string{"plant/temperature", "plant/pressure"}
	collection.MQTT.Subscriptions = map[string]byte{"plant/temperature": 1, "plant/pressure": 2}
	program := base.Connection()
	program.MQTT.QoS = 2
	program.MQTT.TopicFilters = []string{"plant/events/#"}
	baseKey, err := sourceConnectionKey(ctx, 7, base, sourcestream.Credential{})
	require.NoError(t, err)
	for _, def := range []sourcestream.SourceDefinition{collection, program} {
		key, err := sourceConnectionKey(ctx, 7, def, sourcestream.Credential{})
		require.NoError(t, err)
		require.Equal(t, baseKey, key, "Stream scope cannot evict the shared Source connection")
	}
	require.NotEqual(t, mqttWatchScope(collection), mqttWatchScope(program))
	reordered := collection.Connection()
	reordered.Name = "renamed"
	reordered.MQTT.TopicFilters = []string{"plant/pressure", "plant/temperature"}
	require.Equal(t, mqttWatchScope(collection), mqttWatchScope(reordered), "stable scope does not depend on ordering or display labels")
	runtime := New()
	defer runtime.Close()
	watchCtx, done, err := runtime.watchContext(ctx, 7, collection, sourcestream.Credential{})
	require.NoError(t, err)
	defer done()
	_, _, err = runtime.watchContext(ctx, 7, reordered, sourcestream.Credential{})
	require.ErrorIs(t, err, ErrProtocol, "same reordered subscription scope cannot replace a live watcher")
	require.NoError(t, watchCtx.Err())
	runtime.ReleaseSource(7)
	require.ErrorIs(t, watchCtx.Err(), context.Canceled)
	server := base.Connection()
	server.Role = sourcestream.ServerRole
	server.MQTT.ServerSubscribeTopics = []string{"permitted/one"}
	first, err := sourceConnectionKey(ctx, 7, server, sourcestream.Credential{})
	require.NoError(t, err)
	server.MQTT.ServerSubscribeTopics = []string{"permitted/two"}
	second, err := sourceConnectionKey(ctx, 7, server, sourcestream.Credential{})
	require.NoError(t, err)
	require.NotEqual(t, first, second, "Server permission scope is part of configuration identity")
}

func (fixture *lifecycleFixture) ReleaseSource(int64) { fixture.releases++ }
func (fixture *lifecycleFixture) Close()              { fixture.closes++ }

func TestRuntimeSharedRoleDriverLifecycleRunsOnce(t *testing.T) {
	driver := &lifecycleFixture{}
	runtime := New()
	runtime.Register(sourcestream.OPCUA, driver)
	runtime.RegisterServer(sourcestream.OPCUA, driver)
	runtime.ReleaseSource(1)
	require.Equal(t, 1, driver.releases)
	runtime.Close()
	runtime.Close()
	require.Equal(t, 1, driver.closes)
}

type connectionRegisterFixture struct {
	registerFixture
	connectionMu sync.Mutex
	connections  map[string]bool
}

func (fixture *connectionRegisterFixture) HandleHoldingRegisters(request *modbus.HoldingRegistersRequest) ([]uint16, error) {
	fixture.connectionMu.Lock()
	fixture.connections[request.ClientAddr] = true
	fixture.connectionMu.Unlock()
	return fixture.registerFixture.HandleHoldingRegisters(request)
}
func (fixture *connectionRegisterFixture) count() int {
	fixture.connectionMu.Lock()
	defer fixture.connectionMu.Unlock()
	return len(fixture.connections)
}

func TestRealModbusWatchAndSDKOperationsReuseSourceConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "tcp://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	fixture := &connectionRegisterFixture{connections: make(map[string]bool)}
	server, err := modbus.NewServer(&modbus.ServerConfiguration{URL: endpoint, Timeout: 5 * time.Second, MaxClients: 10, Logger: log.New(io.Discard, "", 0)}, fixture)
	require.NoError(t, err)
	require.NoError(t, server.Start())
	defer server.Stop()
	runtime := New()
	runtime.Register(sourcestream.ModbusTCP, ModbusDriver{})
	defer runtime.Close()
	def := sourcestream.SourceDefinition{Protocol: sourcestream.ModbusTCP, Endpoint: endpoint, Modbus: &sourcestream.ModbusOptions{UnitID: 7, PollIntervalMs: 100, Ranges: []sourcestream.ModbusRange{{ID: "point", Table: "holding-register", Address: 0, Quantity: 1}, {ID: "second", Table: "holding-register", Address: 1, Quantity: 1}}}}
	ctx, cancel := context.WithTimeout(WithSourceRevision(context.Background(), 1), 5*time.Second)
	defer cancel()
	require.NoError(t, runtime.Probe(ctx, 41, def, sourcestream.Credential{}))
	_, err = runtime.Discover(ctx, 41, def, sourcestream.Credential{}, "")
	require.NoError(t, err)
	for i := 0; i < 4; i++ {
		_, err = runtime.Invoke(ctx, 41, def, sourcestream.Credential{}, "modbus:point", map[string]any{"operation": "write", "values": []int{i}})
		require.NoError(t, err)
	}
	require.Equal(t, 1, fixture.count(), "actual TCP peer address remains unchanged across Probe/Discover/Invoke")
	watch, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	ready := make(chan struct{}, 1)
	watch = sourcestream.WithWatchObserver(watch, func(value bool) {
		if value {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	done := make(chan error, 1)
	watchDef := def
	watchOptions := *def.Modbus
	watchOptions.Ranges = watchOptions.Ranges[:1]
	watchDef.Modbus = &watchOptions
	runtime.drivers[sourcestream.ModbusTCP].(ModbusDriver).pool.idle = 25 * time.Millisecond
	go func() {
		done <- runtime.Watch(watch, 41, watchDef, sourcestream.Credential{}, func(Input) error { return nil })
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("Watch was not ready")
	}
	time.Sleep(50 * time.Millisecond)
	read, err := runtime.Invoke(ctx, 41, def, sourcestream.Credential{}, "modbus:point", map[string]any{"operation": "read"})
	require.NoError(t, err)
	require.Equal(t, []uint16{3}, read.(map[string]any)["values"])
	require.Equal(t, 1, fixture.count(), "active scoped watch pins the Source transport beyond its idle timeout")
	runtime.ReleaseSource(41)
	require.ErrorIs(t, <-done, context.Canceled)
	_, err = runtime.Invoke(ctx, 41, def, sourcestream.Credential{}, "modbus:point", map[string]any{"operation": "read"})
	require.NoError(t, err)
	require.Equal(t, 2, fixture.count())
	_, err = runtime.Invoke(ctx, 42, def, sourcestream.Credential{}, "modbus:point", map[string]any{"operation": "read"})
	require.NoError(t, err)
	require.Equal(t, 3, fixture.count(), "separate Source owns a separate transport")
	_, err = runtime.Invoke(WithSourceRevision(ctx, 2), 41, def, sourcestream.Credential{}, "modbus:point", map[string]any{"operation": "read"})
	require.NoError(t, err)
	require.Equal(t, 4, fixture.count(), "connection revision creates a new transport")
}
