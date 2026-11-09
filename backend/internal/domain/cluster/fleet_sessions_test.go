// fleet_sessions_test.go 覆盖 fleet agent 会话注册表（fleet_sessions.go）：
// 纯逻辑：会话注册/移除/查询、心跳时间、连接身份匹配、topic kind 映射、
//
//	clientID 净化、命令错误映射、结果解包、快照/详情内存缓存、
//	容器与会话强查（requireContainer/requireSession）。
//
// 会话消息处理：伪 MQTT 客户端下的发布路径（publish/错误应答）、
//
//	无效 hello/未知会话拒绝、命令 ack/结果完成、快照身份校验。
//
// DB 路径：快照与容器详情持久化（updateSnapshot/cacheDetail）、
//
//	disconnectAndForget 清理注册表。
//
// 未覆盖：真实 broker 连接（ensureConnected/subscribe/start/watchdog）与
//
//	命令执行成功路径依赖活动 MQTT 会话与 fleet agent 进程。
package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	auditdomain "backend/internal/domain/audit"
	"backend/internal/fleetprotocol"
	"backend/internal/repo"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---- fake MQTT 客户端 ----

type fakeMQTTToken struct{}

func (fakeMQTTToken) Wait() bool                     { return true }
func (fakeMQTTToken) WaitTimeout(time.Duration) bool { return true }
func (fakeMQTTToken) Done() <-chan struct{}          { return closedCh }
func (fakeMQTTToken) Error() error                   { return nil }

var closedCh = func() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}()

type fakeMQTTClient struct {
	connected bool
	published []string
}

func (c *fakeMQTTClient) IsConnected() bool      { return c.connected }
func (c *fakeMQTTClient) IsConnectionOpen() bool { return c.connected }
func (c *fakeMQTTClient) Connect() mqtt.Token    { return fakeMQTTToken{} }
func (c *fakeMQTTClient) Disconnect(uint)        { c.connected = false }
func (c *fakeMQTTClient) Publish(topic string, _ byte, _ bool, _ any) mqtt.Token {
	c.published = append(c.published, topic)
	return fakeMQTTToken{}
}
func (c *fakeMQTTClient) Subscribe(string, byte, mqtt.MessageHandler) mqtt.Token {
	return fakeMQTTToken{}
}
func (c *fakeMQTTClient) SubscribeMultiple(map[string]byte, mqtt.MessageHandler) mqtt.Token {
	return fakeMQTTToken{}
}
func (c *fakeMQTTClient) Unsubscribe(...string) mqtt.Token { return fakeMQTTToken{} }
func (c *fakeMQTTClient) AddRoute(string, mqtt.MessageHandler) {
}
func (c *fakeMQTTClient) OptionsReader() mqtt.ClientOptionsReader { return mqtt.ClientOptionsReader{} }

type fakeMQTTMessage struct {
	topic   string
	payload []byte
}

func (m fakeMQTTMessage) Duplicate() bool   { return false }
func (m fakeMQTTMessage) Qos() byte         { return 1 }
func (m fakeMQTTMessage) Retained() bool    { return false }
func (m fakeMQTTMessage) Topic() string     { return m.topic }
func (m fakeMQTTMessage) MessageID() uint16 { return 0 }
func (m fakeMQTTMessage) Payload() []byte   { return m.payload }
func (m fakeMQTTMessage) Ack()              {}

func newTestSession(nodeID, authID string) *fleetAgentSession {
	return &fleetAgentSession{
		nodeID:         nodeID,
		authID:         authID,
		connectionID:   "conn-1",
		sessionID:      "sess-1",
		installationID: "inst-1",
		lastHeartbeat:  time.Now(),
		pending:        make(map[string]chan fleetCommandResponse),
		snapshotWait:   make(chan struct{}),
		done:           make(chan struct{}),
	}
}

func TestNewFleetSessionRegistry(t *testing.T) {
	r := newFleetSessionRegistry(&Service{})
	assert.NotNil(t, r.sessions)
	assert.NotNil(t, r.byAuthID)
	assert.NotNil(t, r.snapshots)
	assert.NotNil(t, r.details)
	assert.NotNil(t, r.stop)
	assert.NotNil(t, r.reconcile)
}

func TestCleanMQTTClientIDSuffix(t *testing.T) {
	assert.Equal(t, "abc-123", cleanMQTTClientIDSuffix("abc-123"))
	assert.Equal(t, "local", cleanMQTTClientIDSuffix(""))
	assert.Equal(t, "local", cleanMQTTClientIDSuffix("!!!@@@"))
	assert.Equal(t, "a_b", cleanMQTTClientIDSuffix("a_b"))
}

func TestCenterTopicKind(t *testing.T) {
	assert.Equal(t, fleetprotocol.TopicHelloAck, centerTopicKind(fleetprotocol.MessageTypeHelloAck))
	assert.Equal(t, fleetprotocol.TopicError, centerTopicKind(fleetprotocol.MessageTypeError))
	assert.Equal(t, fleetprotocol.TopicSnapshotRequest, centerTopicKind(fleetprotocol.MessageTypeSnapshotRequest))
	assert.Equal(t, fleetprotocol.TopicCommand, centerTopicKind(fleetprotocol.MessageTypeCommand))
	assert.Equal(t, fleetprotocol.TopicDisconnect, centerTopicKind(fleetprotocol.MessageTypeDisconnect))
	assert.Equal(t, "", centerTopicKind(fleetprotocol.MessageTypeHello))
}

func TestFleetCommandError(t *testing.T) {
	cases := []struct {
		code       string
		wantStatus int
	}{
		{"FLEET_CONTAINER_NOT_FOUND", http.StatusNotFound},
		{"FLEET_CONTAINER_NOT_MANAGED", http.StatusNotFound},
		{"FLEET_CONTAINER_RESTART_FORBIDDEN", http.StatusForbidden},
		{"FLEET_CONTAINER_OPERATION_CONFLICT", http.StatusConflict},
		{"FLEET_OPERATION_TIMEOUT", http.StatusGatewayTimeout},
		{"", http.StatusBadGateway},
		{"SOME_OTHER", http.StatusBadGateway},
	}
	for _, c := range cases {
		err := fleetCommandError(c.code)
		responseErr, ok := err.(*FleetAgentResponseError)
		require.True(t, ok)
		assert.Equal(t, c.wantStatus, responseErr.StatusCode)
	}
	assert.Equal(t, "FLEET_AGENT_COMMAND_FAILED", fleetCommandError("").(*FleetAgentResponseError).Code)
}

func TestUnmarshalFleetResult(t *testing.T) {
	assert.Error(t, unmarshalFleetResult(nil, &fleetprotocol.Detail{}))
	assert.Error(t, unmarshalFleetResult(json.RawMessage(`{bad`), &fleetprotocol.Detail{}))
	var detail fleetprotocol.Detail
	require.NoError(t, unmarshalFleetResult(json.RawMessage(`{"imageID":"img-1"}`), &detail))
	assert.Equal(t, "img-1", detail.ImageID)
}

func TestFleetAgentSession_MatchAndHeartbeat(t *testing.T) {
	session := newTestSession("fn_x", "42")
	assert.True(t, session.matches("conn-1", "sess-1"))
	assert.False(t, session.matches("conn-1", "sess-2"))
	assert.False(t, session.matches("", "sess-1"))

	now := time.Now()
	session.touchHeartbeat(now)
	assert.Equal(t, now, session.heartbeatAt())
}

func TestRegistry_RegisterRemoveActive(t *testing.T) {
	store, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	session := newTestSession("fn_x", "42")
	old, ok := r.register(session)
	assert.Nil(t, old)
	assert.True(t, ok)
	assert.Same(t, session, r.active("fn_x"))
	assert.Same(t, session, r.sessionByAuthID("42"))
	// 同节点再次注册返回旧会话。
	session2 := newTestSession("fn_x", "42")
	old, ok = r.register(session2)
	assert.Same(t, session, old)
	assert.True(t, ok)
	assert.Same(t, session2, r.active("fn_x"))

	r.remove(session2)
	assert.Nil(t, r.active("fn_x"))
	assert.Nil(t, r.sessionByAuthID("42"))
	_ = store
}

func TestRegistry_RegisterWhenClosed(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	r.closeAll()
	old, ok := r.register(newTestSession("fn_x", "42"))
	assert.Nil(t, old)
	assert.False(t, ok)
}

func TestRegistry_SetEnabledClosed(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	r.closeAll()
	err := r.setEnabled(true)
	require.Error(t, err)
}

func TestRegistry_SetEnabledDisableClosesSessions(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	session := newTestSession("fn_x", "42")
	_, ok := r.register(session)
	require.True(t, ok)
	require.NoError(t, r.setEnabled(false))
	select {
	case <-session.done:
	default:
		t.Fatal("session should be closed when registry disabled")
	}
	assert.Nil(t, r.active("fn_x"))
}

func TestRegistry_CloseAllIdempotent(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	r.closeAll()
	r.closeAll() // 幂等
	err := r.setEnabled(true)
	require.Error(t, err)
}

func TestRegistry_LatestSnapshotAndCachedDetail(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	snapshot := fleetprotocol.SnapshotPayload{SnapshotID: "s1"}
	r.snapshots["fn_x"] = snapshot
	got, ok := r.latestSnapshot("fn_x")
	assert.True(t, ok)
	assert.Equal(t, "s1", got.SnapshotID)
	_, ok = r.latestSnapshot("fn_other")
	assert.False(t, ok)

	r.details["fn_x"] = map[string]fleetprotocol.Detail{"c1": {ImageID: "img"}}
	detail, ok := r.cachedDetail("fn_x", "c1")
	assert.True(t, ok)
	assert.Equal(t, "img", detail.ImageID)
	_, ok = r.cachedDetail("fn_x", "c2")
	assert.False(t, ok)
}

func TestRegistry_RequireContainerAndSession(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	_, err := r.requireContainer("fn_x", "c1")
	var responseErr *FleetAgentResponseError
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusServiceUnavailable, responseErr.StatusCode)

	r.snapshots["fn_x"] = fleetprotocol.SnapshotPayload{Containers: fleetprotocol.ContainerSnapshot{
		Containers: []fleetprotocol.Summary{{ContainerID: "c1"}},
	}}
	summary, err := r.requireContainer("fn_x", "c1")
	require.NoError(t, err)
	assert.Equal(t, "c1", summary.ContainerID)
	_, err = r.requireContainer("fn_x", "nope")
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusNotFound, responseErr.StatusCode)

	_, err = r.requireSession("fn_offline")
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusServiceUnavailable, responseErr.StatusCode)
	r.sessions["fn_online"] = newTestSession("fn_online", "1")
	session, err := r.requireSession("fn_online")
	require.NoError(t, err)
	assert.NotNil(t, session)
}

func TestRegistry_Publish(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	// 无客户端 → 错误。
	err := r.publish("42", fleetprotocol.MessageTypeCommand, "m1", "", struct{}{})
	require.Error(t, err)

	client := &fakeMQTTClient{connected: true}
	r.client = client
	err = r.publish("42", fleetprotocol.MessageTypeCommand, "m1", "", struct{}{})
	require.NoError(t, err)
	assert.NotEmpty(t, client.published)
	// 未映射的消息类型 → topic 为空 → 错误。
	err = r.publish("42", fleetprotocol.MessageTypeHello, "m1", "", struct{}{})
	require.Error(t, err)
	// 断开连接 → 错误。
	client.connected = false
	err = r.publish("42", fleetprotocol.MessageTypeCommand, "m1", "", struct{}{})
	require.Error(t, err)
}

func TestSession_ExecuteWithoutClient(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	session := newTestSession("fn_x", "42")
	session.registry = r
	_, err := session.execute(context.Background(), fleetprotocol.OperationContainerRestart, "c1", "op-1", struct{}{}, 10*time.Second)
	var responseErr *FleetAgentResponseError
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusServiceUnavailable, responseErr.StatusCode)
	assert.Equal(t, "FLEET_AGENT_UNAVAILABLE", responseErr.Code)
}

func TestSession_RequestSnapshotWithoutClient(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	session := newTestSession("fn_x", "42")
	session.registry = r
	err := session.requestSnapshot(context.Background())
	require.Error(t, err)
}

func TestSession_Complete(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	session := newTestSession("fn_x", "42")
	session.registry = r
	resultCh := make(chan fleetCommandResponse, 1)
	session.pending["op-1"] = resultCh
	session.complete("op-1", fleetCommandResponse{err: fleetCommandError("FLEET_OPERATION_TIMEOUT")})
	response := <-resultCh
	require.Error(t, response.err)
	assert.Equal(t, http.StatusGatewayTimeout, response.err.(*FleetAgentResponseError).StatusCode)
	// 未知操作 → 无副作用。
	session.complete("op-unknown", fleetCommandResponse{})
}

func TestSession_SnapshotWaiterNotify(t *testing.T) {
	session := newTestSession("fn_x", "42")
	wait := session.snapshotWaiter()
	session.notifySnapshot()
	select {
	case <-wait:
	default:
		t.Fatal("snapshot waiter should be notified")
	}
	// 通知后可再次等待。
	wait = session.snapshotWaiter()
	select {
	case <-wait:
		t.Fatal("new waiter should not be closed")
	default:
	}
}

func TestHandleMQTTMessage_InactiveClient(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	r.handleMQTTMessage(&fakeMQTTClient{connected: true}, fakeMQTTMessage{
		topic: "fleet/x/hello", payload: []byte(`{}`),
	}) // 非当前客户端 → 忽略，不 panic
}

func TestHandleMQTTMessage_ProtocolError(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	client := &fakeMQTTClient{connected: true}
	r.client = client
	// 非法 envelope → 发布 FLEET_PROTOCOL_UNSUPPORTED 错误。
	r.handleMQTTMessage(client, fakeMQTTMessage{
		topic:   fleetprotocol.ManagementTopicRoot + "/42/" + fleetprotocol.TopicHello,
		payload: []byte(`{bad`),
	})
	assert.NotEmpty(t, client.published)
}

func TestHandleHello_InvalidAuthID(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	client := &fakeMQTTClient{connected: true}
	r.client = client
	envelope := fleetprotocol.Envelope{
		ProtocolVersion: fleetprotocol.ProtocolVersion,
		MessageID:       "m1",
		Type:            fleetprotocol.MessageTypeHello,
	}
	r.handleHello("not-a-number", envelope)
	assert.NotEmpty(t, client.published) // FLEET_AUTH_INVALID 错误应答
}

func TestHandleHello_NodeNotFound(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	client := &fakeMQTTClient{connected: true}
	r.client = client
	payload, _ := json.Marshal(fleetprotocol.HelloPayload{
		FleetNodeID: "fn_nope", ConnectionID: "conn-1", InstallationID: "inst-1",
	})
	envelope := fleetprotocol.Envelope{
		ProtocolVersion: fleetprotocol.ProtocolVersion,
		MessageID:       "m1",
		Type:            fleetprotocol.MessageTypeHello,
		Payload:         payload,
	}
	r.handleHello("42", envelope)
	assert.NotEmpty(t, client.published) // FLEET_NODE_UNAVAILABLE 错误应答
}

func TestHandleUnknownSessionRejections(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	client := &fakeMQTTClient{connected: true}
	r.client = client
	// 心跳/快照/断开来自未知会话 → 拒绝应答（发布失败被忽略，不 panic）。
	r.handleHeartbeat("unknown", fleetprotocol.Envelope{MessageID: "m1"})
	r.handleSnapshot("unknown", fleetprotocol.Envelope{MessageID: "m1"})
	r.handleCommandAck("unknown", fleetprotocol.Envelope{MessageID: "m1"})
	r.handleCommandResult("unknown", fleetprotocol.Envelope{MessageID: "m1"})
	r.handleDisconnect("unknown", fleetprotocol.Envelope{MessageID: "m1"})
	assert.NotEmpty(t, client.published) // 至少一次拒绝应答
}

func TestHandleCommandAck_RejectedCompletes(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	session := newTestSession("fn_x", "42")
	session.registry = r
	_, ok := r.register(session)
	require.True(t, ok)
	resultCh := make(chan fleetCommandResponse, 1)
	session.pending["op-1"] = resultCh

	ack := fleetprotocol.CommandAckPayload{
		ConnectionID: "conn-1", SessionID: "sess-1", OperationID: "op-1",
		Accepted: false, ErrorCode: "FLEET_OPERATION_TIMEOUT",
	}
	payload, _ := json.Marshal(ack)
	r.handleCommandAck("42", fleetprotocol.Envelope{MessageID: "m1", Type: fleetprotocol.MessageTypeCommandAck, Payload: payload})
	response := <-resultCh
	require.Error(t, response.err)
	assert.Equal(t, http.StatusGatewayTimeout, response.err.(*FleetAgentResponseError).StatusCode)
}

func TestHandleCommandResult_Succeeds(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	session := newTestSession("fn_x", "42")
	session.registry = r
	_, ok := r.register(session)
	require.True(t, ok)
	resultCh := make(chan fleetCommandResponse, 1)
	session.pending["op-2"] = resultCh

	result := fleetprotocol.CommandResultPayload{
		ConnectionID: "conn-1", SessionID: "sess-1", OperationID: "op-2",
		Status: "succeeded", Result: json.RawMessage(`{"ok":true}`),
	}
	payload, _ := json.Marshal(result)
	r.handleCommandResult("42", fleetprotocol.Envelope{MessageID: "m1", Type: fleetprotocol.MessageTypeCommandResult, Payload: payload})
	response := <-resultCh
	require.NoError(t, response.err)
	assert.Equal(t, `{"ok":true}`, string(response.result.Result))
}

func TestHandleDisconnect_ClosesSession(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	session := newTestSession("fn_x", "42")
	session.registry = r
	_, ok := r.register(session)
	require.True(t, ok)
	payload, _ := json.Marshal(fleetprotocol.DisconnectPayload{ConnectionID: "conn-1"})
	r.handleDisconnect("42", fleetprotocol.Envelope{MessageID: "m1", Type: fleetprotocol.MessageTypeDisconnect, Payload: payload})
	select {
	case <-session.done:
	default:
		t.Fatal("session should be closed on disconnect")
	}
	assert.Nil(t, r.active("fn_x"))
}

func TestUpdateSnapshot_InvalidIdentity(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	session := newTestSession("fn_x", "42")
	session.registry = r
	err := r.updateSnapshot(session, fleetprotocol.SnapshotPayload{})
	require.Error(t, err)
	// 未注册的会话 → stale。
	err = r.updateSnapshot(session, fleetprotocol.SnapshotPayload{
		SnapshotID: "s1",
		Containers: fleetprotocol.ContainerSnapshot{SnapshotID: "s1", InstallationID: "inst-1"},
	})
	require.Error(t, err)
}

func TestUpdateSnapshot_Success(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	// fleet_container_snapshot 有外键约束，需先创建 fleet_node。
	_, err := s.fleetRepo.CreateNodeWithConnection(backgroundCtx(), repo.CreateFleetNodeInput{
		FleetNodeID: "fn_x", Name: "edge-1", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	session := newTestSession("fn_x", "42")
	session.registry = r
	_, ok := r.register(session)
	require.True(t, ok)
	snapshot := fleetprotocol.SnapshotPayload{
		ConnectionID: "conn-1", SessionID: "sess-1", SnapshotID: "snap-1", ObservedAt: 123,
		Containers: fleetprotocol.ContainerSnapshot{
			SnapshotID: "snap-1", InstallationID: "inst-1",
			Containers: []fleetprotocol.Summary{{ContainerID: "c1"}},
		},
	}
	require.NoError(t, r.updateSnapshot(session, snapshot))
	got, ok := r.latestSnapshot("fn_x")
	assert.True(t, ok)
	assert.Equal(t, "snap-1", got.SnapshotID)
}

func TestCacheDetail(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	// fleet_container_detail 有外键约束，需先创建 fleet_node。
	_, err := s.fleetRepo.CreateNodeWithConnection(backgroundCtx(), repo.CreateFleetNodeInput{
		FleetNodeID: "fn_x", Name: "edge-1", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	detail := fleetprotocol.Detail{
		Summary: fleetprotocol.Summary{ContainerID: "c1", State: "running"},
		ImageID: "img-9",
	}
	require.NoError(t, r.cacheDetail("fn_x", detail))
	got, ok := r.cachedDetail("fn_x", "c1")
	assert.True(t, ok)
	assert.Equal(t, "img-9", got.ImageID)
}

func TestLoadPersistedProjections_Empty(t *testing.T) {
	_, s := newTestCluster(t)
	s.fleet.loadPersistedProjections(backgroundCtx())
	_, ok := s.fleet.latestSnapshot("fn_x")
	assert.False(t, ok)
}

func TestDisconnectAndForget(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	session := newTestSession("fn_x", "42")
	session.registry = r
	_, ok := r.register(session)
	require.True(t, ok)
	r.snapshots["fn_x"] = fleetprotocol.SnapshotPayload{SnapshotID: "s1"}
	r.details["fn_x"] = map[string]fleetprotocol.Detail{"c1": {}}
	r.disconnectAndForget("fn_x")
	assert.Nil(t, r.active("fn_x"))
	_, ok = r.latestSnapshot("fn_x")
	assert.False(t, ok)
	_, ok = r.cachedDetail("fn_x", "c1")
	assert.False(t, ok)
	// 空 nodeID → no-op。
	r.disconnectAndForget("")
}

func TestRegistry_IsActiveClientAndMarkDisconnected(t *testing.T) {
	_, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	client := &fakeMQTTClient{connected: true}
	r.client = client
	assert.True(t, r.isActiveClient(client, r.generation))
	other := &fakeMQTTClient{connected: true}
	assert.False(t, r.isActiveClient(other, r.generation))
	r.markDisconnected(client, r.generation)
	assert.False(t, r.subscribed)
	// 过期 generation 不影响当前状态。
	r.subscribed = true
	r.markDisconnected(client, r.generation+1)
	assert.True(t, r.subscribed)
}

func TestRegistry_SignalReconcile(t *testing.T) {
	var nilRegistry *fleetSessionRegistry
	nilRegistry.signalReconcile() // 不应 panic
	_, s := newTestCluster(t)
	r := s.fleet
	r.signalReconcile()
	select {
	case <-r.reconcile:
	default:
		t.Fatal("reconcile signal should be queued")
	}
	r.signalReconcile() // 缓冲已满 → 不阻塞
}

func TestFleetAgentResponseError_Error(t *testing.T) {
	var nilErr *FleetAgentResponseError
	assert.Equal(t, ErrFleetAgentUnavailable.Error(), nilErr.Error())
	assert.Equal(t, "SOME_CODE", (&FleetAgentResponseError{Code: " SOME_CODE "}).Error())
	assert.Equal(t, ErrFleetAgentUnavailable.Error(), (&FleetAgentResponseError{}).Error())
}

func TestRegistryReplacingSessionDoesNotAuditStop(t *testing.T) {
	store, s := newTestCluster(t)
	s.BindAudit(auditdomain.New(context.Background()))
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	first := newTestSession("fn_review", "42")
	_, ok := r.register(first)
	require.True(t, ok)
	second := newTestSession("fn_review", "42")
	second.sessionID = "sess-2"
	_, ok = r.register(second)
	require.True(t, ok)
	r.remove(first)
	var count int64
	require.NoError(t, store.CommonDB().Model(&repo.OutboxEvent{}).Where("event_type = ?", auditdomain.OutboxEventType).Count(&count).Error)
	require.Zero(t, count)
	require.Same(t, second, r.active("fn_review"))
}

func TestRegistryDisconnectDoesNotHoldLockDuringDatabaseWrite(t *testing.T) {
	store, s := newTestCluster(t)
	r := s.fleet
	require.NoError(t, r.setEnabled(true))
	session := newTestSession("fn_slow", "42")
	_, ok := r.register(session)
	require.True(t, ok)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	db := store.CommonDB()
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("slow_disconnect", func(tx *gorm.DB) {
		close(entered)
		<-release
	}))
	defer db.Callback().Update().Remove("slow_disconnect")
	go func() { r.remove(session); close(done) }()
	defer func() { close(release); <-done }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not reach database")
	}
	registered := make(chan bool, 1)
	go func() { _, ok := r.register(newTestSession("fn_other", "43")); registered <- ok }()
	select {
	case ok := <-registered:
		require.True(t, ok)
	case <-time.After(time.Second):
		t.Fatal("another node was blocked by database persistence")
	}
}
