package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	auditdomain "backend/internal/domain/audit"
	domainlicense "backend/internal/domain/license"
	"backend/internal/fleetprotocol"
	"backend/internal/repo"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

type fleetSessionRegistry struct {
	service     *Service
	mu          sync.RWMutex
	client      mqtt.Client
	connecting  bool
	subscribed  bool
	subscribing bool
	enabled     bool
	generation  uint64
	sessions    map[string]*fleetAgentSession
	byAuthID    map[string]*fleetAgentSession
	snapshots   map[string]fleetprotocol.SnapshotPayload
	details     map[string]map[string]fleetprotocol.Detail
	closed      bool
	stop        chan struct{}
	reconcile   chan struct{}
	workerOnce  sync.Once
}

type fleetAgentSession struct {
	registry       *fleetSessionRegistry
	nodeID         string
	authID         string
	connectionID   string
	sessionID      string
	installationID string
	heartbeatMu    sync.RWMutex
	lastHeartbeat  time.Time
	pendingMu      sync.Mutex
	pending        map[string]chan fleetCommandResponse
	snapshotMu     sync.Mutex
	snapshotWait   chan struct{}
	done           chan struct{}
	closeOnce      sync.Once
}

type fleetCommandResponse struct {
	result fleetprotocol.CommandResultPayload
	err    error
}

func newFleetSessionRegistry(service *Service) *fleetSessionRegistry {
	return &fleetSessionRegistry{
		service:   service,
		sessions:  make(map[string]*fleetAgentSession),
		byAuthID:  make(map[string]*fleetAgentSession),
		snapshots: make(map[string]fleetprotocol.SnapshotPayload),
		details:   make(map[string]map[string]fleetprotocol.Detail),
		stop:      make(chan struct{}),
		reconcile: make(chan struct{}, 1),
	}
}

func (r *fleetSessionRegistry) loadPersistedProjections(ctx context.Context) {
	if r == nil || r.service == nil || r.service.fleetRepo == nil {
		return
	}
	snapshots, err := r.service.fleetRepo.ListContainerSnapshots(ctx)
	if err != nil {
		logx.WithContext(ctx).Errorf("load fleet snapshots: %v", err)
	} else {
		for _, record := range snapshots {
			var snapshot fleetprotocol.SnapshotPayload
			if json.Unmarshal(record.Payload, &snapshot) != nil || snapshot.SnapshotID == "" {
				continue
			}
			r.snapshots[record.FleetNodeID] = snapshot
		}
	}
	details, err := r.service.fleetRepo.ListContainerDetails(ctx)
	if err != nil {
		logx.WithContext(ctx).Errorf("load fleet container details: %v", err)
		return
	}
	for _, record := range details {
		var detail fleetprotocol.Detail
		if json.Unmarshal(record.Payload, &detail) != nil || detail.ContainerID == "" {
			continue
		}
		if r.details[record.FleetNodeID] == nil {
			r.details[record.FleetNodeID] = make(map[string]fleetprotocol.Detail)
		}
		r.details[record.FleetNodeID][record.ContainerID] = detail
	}
}

func logFleetSessionResetError(ctx context.Context, err error) {
	logx.WithContext(ctx).Errorf("reset stale fleet sessions: %v", err)
}

func (r *fleetSessionRegistry) start(ctx context.Context) error {
	if r == nil || r.service == nil {
		return nil
	}
	r.startWorker()
	enabled := domainlicense.Default() != nil && domainlicense.Default().IsHost()
	if err := r.setEnabled(enabled); err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	return r.ensureConnected(ctx)
}

func (r *fleetSessionRegistry) reconcileLicenseRuntime() {
	if r == nil {
		return
	}
	r.startWorker()
	enabled := domainlicense.Default() != nil && domainlicense.Default().IsHost()
	if err := r.setEnabled(enabled); err != nil {
		logx.Errorf("reconcile fleet management MQTT with license: %v", err)
		return
	}
	if enabled {
		r.signalReconcile()
	}
}

func (r *fleetSessionRegistry) setEnabled(enabled bool) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("Fleet MQTT registry is closed")
	}
	changed := r.enabled != enabled
	r.enabled = enabled
	if changed {
		r.generation++
	}
	if enabled {
		r.mu.Unlock()
		if changed {
			logx.Infof("fleet center MQTT runtime enabled by license")
		}
		return nil
	}
	client := r.client
	r.client = nil
	r.connecting = false
	r.subscribed = false
	r.subscribing = false
	sessions := make([]*fleetAgentSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	r.mu.Unlock()
	for _, session := range sessions {
		session.close()
	}
	if client != nil {
		client.Disconnect(250)
	}
	if changed {
		logx.Infof("fleet center MQTT runtime disabled by license")
	}
	return nil
}

func (r *fleetSessionRegistry) ensureConnected(ctx context.Context) error {
	var staleClient mqtt.Client
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("Fleet MQTT registry is closed")
	}
	if !r.enabled {
		r.mu.Unlock()
		return nil
	}
	brokers := splitCSV(r.service.localMqttBrokers())
	if len(brokers) == 0 {
		r.mu.Unlock()
		return errors.New("Fleet center MQTT broker is not configured")
	}
	generation := r.generation
	if client := r.client; client != nil {
		if client.IsConnected() {
			subscribed := r.subscribed
			r.mu.Unlock()
			if subscribed {
				return nil
			}
			return r.subscribe(client, generation)
		}
		if client.IsConnectionOpen() || r.connecting {
			r.mu.Unlock()
			return nil
		}
		r.client = nil
		staleClient = client
	}
	if r.connecting {
		r.mu.Unlock()
		return nil
	}
	r.connecting = true
	options := mqtt.NewClientOptions().
		SetClientID("edge_server_fleet_center_" + cleanMQTTClientIDSuffix(r.service.cfg.NodeKey)).
		SetUsername(r.service.ingestCfg.MqttUsername).
		SetPassword(r.service.ingestCfg.MqttPassword).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(3 * time.Second).
		SetOrderMatters(false)
	for _, broker := range brokers {
		options.AddBroker(broker)
	}
	options.SetConnectionLostHandler(func(client mqtt.Client, connectionErr error) {
		r.markDisconnected(client, generation)
		if r.isActiveClient(client, generation) {
			logx.Errorf("fleet center MQTT connection lost: %v", connectionErr)
		}
	})
	options.SetOnConnectHandler(func(client mqtt.Client) {
		if !r.isActiveClient(client, generation) {
			return
		}
		if err := r.subscribe(client, generation); err != nil {
			logx.Errorf("subscribe fleet center MQTT topics: %v", err)
		}
	})
	client := mqtt.NewClient(options)
	r.client = client
	r.mu.Unlock()
	if staleClient != nil {
		staleClient.Disconnect(250)
	}

	connectToken := client.Connect()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-connectToken.Done():
		if err := connectToken.Error(); err != nil {
			return r.finishConnect(client, generation, err)
		}
	case <-ctx.Done():
		return r.finishConnect(client, generation, ctx.Err())
	case <-timer.C:
		return r.finishConnect(client, generation, errors.New("connect fleet center MQTT timeout"))
	}
	return r.finishConnect(client, generation, nil)
}

func (r *fleetSessionRegistry) finishConnect(client mqtt.Client, generation uint64, connectErr error) error {
	r.mu.Lock()
	active := !r.closed && r.enabled && r.generation == generation && r.client == client
	if active {
		r.connecting = false
		if connectErr != nil {
			r.client = nil
			r.subscribed = false
			r.subscribing = false
		}
	}
	r.mu.Unlock()
	if !active || connectErr != nil {
		client.Disconnect(250)
	}
	if !active {
		return nil
	}
	if connectErr != nil {
		return connectErr
	}
	logx.Infof("fleet center MQTT management channel started")
	return r.subscribe(client, generation)
}

func (r *fleetSessionRegistry) subscribe(client mqtt.Client, generation uint64) error {
	r.mu.Lock()
	if r.closed || !r.enabled || r.generation != generation || r.client != client {
		r.mu.Unlock()
		return nil
	}
	if r.subscribed || r.subscribing {
		r.mu.Unlock()
		return nil
	}
	r.subscribing = true
	r.mu.Unlock()

	subscriptions := map[string]byte{}
	for _, kind := range []string{
		fleetprotocol.TopicHello,
		fleetprotocol.TopicHeartbeat,
		fleetprotocol.TopicSnapshot,
		fleetprotocol.TopicCommandAck,
		fleetprotocol.TopicCommandResult,
		fleetprotocol.TopicDisconnect,
	} {
		subscriptions[fleetprotocol.ManagementTopicRoot+"/+/"+kind] = 1
	}
	token := client.SubscribeMultiple(subscriptions, r.handleMQTTMessage)
	var subscribeErr error
	if !token.WaitTimeout(fleetprotocol.WriteTimeout) {
		subscribeErr = errors.New("subscribe fleet center MQTT topics timeout")
	} else {
		subscribeErr = token.Error()
	}
	r.mu.Lock()
	active := !r.closed && r.enabled && r.generation == generation && r.client == client
	if active {
		r.subscribing = false
		r.subscribed = subscribeErr == nil
	}
	r.mu.Unlock()
	return subscribeErr
}

func (r *fleetSessionRegistry) markDisconnected(client mqtt.Client, generation uint64) {
	r.mu.Lock()
	if r.generation == generation && r.client == client {
		r.subscribed = false
		r.subscribing = false
	}
	r.mu.Unlock()
	r.signalReconcile()
}

func (r *fleetSessionRegistry) isActiveClient(client mqtt.Client, generation uint64) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return !r.closed && r.enabled && r.generation == generation && r.client == client
}

func (r *fleetSessionRegistry) handleMQTTMessage(client mqtt.Client, message mqtt.Message) {
	r.mu.RLock()
	active := !r.closed && r.enabled && r.client == client
	r.mu.RUnlock()
	if !active {
		return
	}
	if len(message.Payload()) > fleetprotocol.MaxMessageBytes {
		logx.Errorf("reject oversized fleet MQTT message topic=%s", message.Topic())
		return
	}
	authID, kind, ok := fleetprotocol.ParseManagementTopic(message.Topic())
	if !ok {
		return
	}
	var envelope fleetprotocol.Envelope
	if json.Unmarshal(message.Payload(), &envelope) != nil || envelope.ProtocolVersion != fleetprotocol.ProtocolVersion || strings.TrimSpace(envelope.MessageID) == "" {
		_ = r.publish(authID, fleetprotocol.MessageTypeError, uuid.NewString(), envelope.MessageID, fleetprotocol.ErrorPayload{Code: "FLEET_PROTOCOL_UNSUPPORTED", Retryable: false})
		return
	}
	switch envelope.Type {
	case fleetprotocol.MessageTypeHello:
		if kind == fleetprotocol.TopicHello {
			r.handleHello(authID, envelope)
		}
	case fleetprotocol.MessageTypeHeartbeat:
		if kind == fleetprotocol.TopicHeartbeat {
			r.handleHeartbeat(authID, envelope)
		}
	case fleetprotocol.MessageTypeSnapshot:
		if kind == fleetprotocol.TopicSnapshot {
			r.handleSnapshot(authID, envelope)
		}
	case fleetprotocol.MessageTypeCommandAck:
		if kind == fleetprotocol.TopicCommandAck {
			r.handleCommandAck(authID, envelope)
		}
	case fleetprotocol.MessageTypeCommandResult:
		if kind == fleetprotocol.TopicCommandResult {
			r.handleCommandResult(authID, envelope)
		}
	case fleetprotocol.MessageTypeDisconnect:
		if kind == fleetprotocol.TopicDisconnect {
			r.handleDisconnect(authID, envelope)
		}
	}
}

func (r *fleetSessionRegistry) handleHello(authID string, envelope fleetprotocol.Envelope) {
	clientID, err := strconv.ParseInt(authID, 10, 64)
	if err != nil || clientID <= 0 {
		_ = r.publish(authID, fleetprotocol.MessageTypeError, uuid.NewString(), envelope.MessageID, fleetprotocol.ErrorPayload{Code: "FLEET_AUTH_INVALID", Retryable: false})
		return
	}
	var hello fleetprotocol.HelloPayload
	if json.Unmarshal(envelope.Payload, &hello) != nil || strings.TrimSpace(hello.FleetNodeID) == "" || strings.TrimSpace(hello.ConnectionID) == "" || strings.TrimSpace(hello.InstallationID) == "" {
		_ = r.publish(authID, fleetprotocol.MessageTypeError, uuid.NewString(), envelope.MessageID, fleetprotocol.ErrorPayload{Code: "FLEET_PROTOCOL_UNSUPPORTED", Retryable: false})
		return
	}
	node, err := r.service.fleetRepo.GetNodeByConnectClientID(context.Background(), clientID, false)
	if err != nil || node.FleetNodeID != strings.TrimSpace(hello.FleetNodeID) {
		_ = r.publish(authID, fleetprotocol.MessageTypeError, uuid.NewString(), envelope.MessageID, fleetprotocol.ErrorPayload{Code: "FLEET_NODE_UNAVAILABLE", Retryable: false})
		return
	}
	sessionID := uuid.NewString()
	now := time.Now().UTC()
	if err := r.service.fleetRepo.ActivateNodeSession(context.Background(), repo.ActivateFleetNodeSessionInput{
		FleetNodeID:     node.FleetNodeID,
		SessionID:       sessionID,
		AgentVersion:    hello.AgentVersion,
		ProtocolVersion: envelope.ProtocolVersion,
		OS:              hello.OS,
		Arch:            hello.Arch,
		ConnectedAt:     now.UnixMilli(),
	}); err != nil {
		_ = r.publish(authID, fleetprotocol.MessageTypeError, uuid.NewString(), envelope.MessageID, fleetprotocol.ErrorPayload{Code: "FLEET_NODE_UNAVAILABLE", Retryable: false})
		return
	}
	session := &fleetAgentSession{
		registry:       r,
		nodeID:         node.FleetNodeID,
		authID:         authID,
		connectionID:   strings.TrimSpace(hello.ConnectionID),
		sessionID:      sessionID,
		installationID: strings.TrimSpace(hello.InstallationID),
		lastHeartbeat:  now,
		pending:        make(map[string]chan fleetCommandResponse),
		snapshotWait:   make(chan struct{}),
		done:           make(chan struct{}),
	}
	old, registered := r.register(session)
	if !registered {
		_ = r.service.fleetRepo.DeactivateNodeSession(context.Background(), node.FleetNodeID, sessionID, time.Now().UTC().UnixMilli())
		return
	}
	if err := session.write(fleetprotocol.MessageTypeHelloAck, uuid.NewString(), envelope.MessageID, fleetprotocol.HelloAckPayload{
		ServerTime:               now.UnixMilli(),
		SessionID:                sessionID,
		HeartbeatIntervalSeconds: int(fleetprotocol.HeartbeatInterval / time.Second),
		SnapshotIntervalSeconds:  int(fleetprotocol.SnapshotInterval / time.Second),
		MaxMessageBytes:          fleetprotocol.MaxMessageBytes,
	}); err != nil {
		session.close()
		return
	}
	if old != nil {
		old.close()
	}
	r.service.recordFleetNodeSessionAudit(node.FleetNodeID, node.Name, auditdomain.BizActivate, "hello")
	logx.Infof("fleet agent MQTT connected nodeID=%s sessionID=%s version=%s", node.FleetNodeID, sessionID, hello.AgentVersion)
}

func (r *fleetSessionRegistry) handleHeartbeat(authID string, envelope fleetprotocol.Envelope) {
	session := r.sessionByAuthID(authID)
	if session == nil {
		r.rejectUnknownSession(authID, envelope.MessageID)
		return
	}
	var heartbeat fleetprotocol.HeartbeatPayload
	if json.Unmarshal(envelope.Payload, &heartbeat) != nil || heartbeat.SessionID != session.sessionID {
		r.rejectUnknownSession(authID, envelope.MessageID)
		return
	}
	now := time.Now().UTC()
	if err := r.service.fleetRepo.TouchNodeSession(context.Background(), session.nodeID, session.sessionID, now.UnixMilli()); err != nil {
		session.close()
		return
	}
	session.touchHeartbeat(now)
}

func (r *fleetSessionRegistry) handleSnapshot(authID string, envelope fleetprotocol.Envelope) {
	session := r.sessionByAuthID(authID)
	if session == nil {
		r.rejectUnknownSession(authID, envelope.MessageID)
		return
	}
	var snapshot fleetprotocol.SnapshotPayload
	if json.Unmarshal(envelope.Payload, &snapshot) != nil || !session.matches(snapshot.ConnectionID, snapshot.SessionID) {
		r.rejectUnknownSession(authID, envelope.MessageID)
		return
	}
	if err := r.updateSnapshot(session, snapshot); err != nil {
		logx.Errorf("fleet snapshot rejected nodeID=%s sessionID=%s error=%v", session.nodeID, session.sessionID, err)
	}
}

func (r *fleetSessionRegistry) rejectUnknownSession(authID, replyTo string) {
	_ = r.publish(authID, fleetprotocol.MessageTypeError, uuid.NewString(), replyTo, fleetprotocol.ErrorPayload{
		Code:      fleetprotocol.ErrorCodeSessionUnknown,
		Retryable: true,
	})
}

func (r *fleetSessionRegistry) handleCommandAck(authID string, envelope fleetprotocol.Envelope) {
	session := r.sessionByAuthID(authID)
	if session == nil {
		return
	}
	var ack fleetprotocol.CommandAckPayload
	if json.Unmarshal(envelope.Payload, &ack) == nil && session.matches(ack.ConnectionID, ack.SessionID) && !ack.Accepted {
		session.complete(ack.OperationID, fleetCommandResponse{err: fleetCommandError(ack.ErrorCode)})
	}
}

func (r *fleetSessionRegistry) handleCommandResult(authID string, envelope fleetprotocol.Envelope) {
	session := r.sessionByAuthID(authID)
	if session == nil {
		return
	}
	var result fleetprotocol.CommandResultPayload
	if json.Unmarshal(envelope.Payload, &result) == nil && session.matches(result.ConnectionID, result.SessionID) {
		session.complete(result.OperationID, fleetCommandResponse{result: result})
	}
}

func (r *fleetSessionRegistry) handleDisconnect(authID string, envelope fleetprotocol.Envelope) {
	session := r.sessionByAuthID(authID)
	if session == nil {
		return
	}
	var payload fleetprotocol.DisconnectPayload
	if json.Unmarshal(envelope.Payload, &payload) != nil || strings.TrimSpace(payload.ConnectionID) != session.connectionID {
		return
	}
	session.close()
}

func (r *fleetSessionRegistry) register(session *fleetAgentSession) (*fleetAgentSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || !r.enabled {
		return nil, false
	}
	session.registry = r
	old := r.sessions[session.nodeID]
	r.sessions[session.nodeID] = session
	r.byAuthID[session.authID] = session
	return old, true
}

func (r *fleetSessionRegistry) remove(session *fleetAgentSession) {
	r.mu.Lock()
	active := r.sessions[session.nodeID] == session
	if active {
		delete(r.sessions, session.nodeID)
	}
	if r.byAuthID[session.authID] == session {
		delete(r.byAuthID, session.authID)
	}
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = r.service.fleetRepo.DeactivateNodeSession(ctx, session.nodeID, session.sessionID, time.Now().UTC().UnixMilli())
	if active {
		r.service.recordFleetNodeSessionAudit(session.nodeID, "", auditdomain.BizStop, "session-close")
	}
}

func (r *fleetSessionRegistry) closeAll() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.stop)
	client := r.client
	r.client = nil
	r.connecting = false
	r.subscribed = false
	r.subscribing = false
	r.enabled = false
	r.generation++
	sessions := make([]*fleetAgentSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	r.mu.Unlock()
	for _, session := range sessions {
		session.close()
	}
	if client != nil {
		client.Disconnect(250)
	}
}

func (r *fleetSessionRegistry) startWorker() {
	r.workerOnce.Do(func() {
		go r.watchdog()
	})
}

func (r *fleetSessionRegistry) signalReconcile() {
	if r == nil {
		return
	}
	select {
	case r.reconcile <- struct{}{}:
	default:
	}
}

func (r *fleetSessionRegistry) watchdog() {
	ticker := time.NewTicker(fleetprotocol.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-r.reconcile:
			if err := r.ensureConnected(context.Background()); err != nil {
				logx.Errorf("reconcile fleet center MQTT connection: %v", err)
			}
		case now := <-ticker.C:
			r.mu.RLock()
			sessions := make([]*fleetAgentSession, 0, len(r.sessions))
			for _, session := range r.sessions {
				sessions = append(sessions, session)
			}
			r.mu.RUnlock()
			for _, session := range sessions {
				if now.Sub(session.heartbeatAt()) > fleetprotocol.OfflineThreshold {
					session.close()
				}
			}
			if err := r.ensureConnected(context.Background()); err != nil {
				logx.Errorf("watch fleet center MQTT connection: %v", err)
			}
		}
	}
}

func (r *fleetSessionRegistry) active(nodeID string) *fleetAgentSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[strings.TrimSpace(nodeID)]
}

func (r *fleetSessionRegistry) sessionByAuthID(authID string) *fleetAgentSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byAuthID[strings.TrimSpace(authID)]
}

func (r *fleetSessionRegistry) publish(authID string, messageType fleetprotocol.MessageType, messageID, replyTo string, payload any) error {
	r.mu.RLock()
	client := r.client
	closed := r.closed
	r.mu.RUnlock()
	if closed || client == nil || !client.IsConnectionOpen() {
		return errors.New("Fleet center MQTT client is unavailable")
	}
	kind := centerTopicKind(messageType)
	topic := fleetprotocol.MustManagementTopic(authID, kind)
	if topic == "" {
		return errors.New("invalid Fleet center MQTT topic")
	}
	raw, err := fleetprotocol.MarshalEnvelope(messageType, messageID, replyTo, payload)
	if err != nil {
		return err
	}
	if len(raw) > fleetprotocol.MaxMessageBytes {
		return errors.New("Fleet center MQTT message is too large")
	}
	token := client.Publish(topic, 1, false, raw)
	if !token.WaitTimeout(fleetprotocol.WriteTimeout) {
		return errors.New("publish Fleet center MQTT message timeout")
	}
	return token.Error()
}

func centerTopicKind(messageType fleetprotocol.MessageType) string {
	switch messageType {
	case fleetprotocol.MessageTypeHelloAck:
		return fleetprotocol.TopicHelloAck
	case fleetprotocol.MessageTypeError:
		return fleetprotocol.TopicError
	case fleetprotocol.MessageTypeSnapshotRequest:
		return fleetprotocol.TopicSnapshotRequest
	case fleetprotocol.MessageTypeCommand:
		return fleetprotocol.TopicCommand
	case fleetprotocol.MessageTypeDisconnect:
		return fleetprotocol.TopicDisconnect
	default:
		return ""
	}
}

func cleanMQTTClientIDSuffix(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "local"
	}
	var builder strings.Builder
	for _, character := range raw {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			builder.WriteRune(character)
		}
	}
	if builder.Len() == 0 {
		return "local"
	}
	return builder.String()
}

func (r *fleetSessionRegistry) updateSnapshot(session *fleetAgentSession, snapshot fleetprotocol.SnapshotPayload) error {
	if snapshot.SnapshotID == "" || snapshot.Containers.SnapshotID != snapshot.SnapshotID || snapshot.Containers.InstallationID != session.installationID {
		return errors.New("invalid Fleet snapshot identity")
	}
	r.mu.RLock()
	if r.sessions[session.nodeID] != session {
		r.mu.RUnlock()
		return errors.New("stale Fleet session")
	}
	r.mu.RUnlock()
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	applied, err := r.service.fleetRepo.SaveContainerSnapshot(context.Background(), repo.FleetContainerSnapshot{
		FleetNodeID:    session.nodeID,
		SnapshotID:     snapshot.SnapshotID,
		ObservedAt:     snapshot.ObservedAt,
		InstallationID: session.installationID,
		Payload:        payload,
	})
	if err != nil {
		return fmt.Errorf("persist Fleet snapshot: %w", err)
	}
	if !applied {
		return nil
	}
	r.mu.Lock()
	if r.sessions[session.nodeID] != session {
		r.mu.Unlock()
		return errors.New("stale Fleet session")
	}
	r.snapshots[session.nodeID] = snapshot
	r.mu.Unlock()
	session.notifySnapshot()
	return nil
}

func (r *fleetSessionRegistry) latestSnapshot(nodeID string) (fleetprotocol.SnapshotPayload, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshot, ok := r.snapshots[strings.TrimSpace(nodeID)]
	return snapshot, ok
}

func (r *fleetSessionRegistry) cacheDetail(nodeID string, detail fleetprotocol.Detail) error {
	payload, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	if err := r.service.fleetRepo.SaveContainerDetail(context.Background(), repo.FleetContainerDetail{
		FleetNodeID: nodeID,
		ContainerID: detail.ContainerID,
		ObservedAt:  time.Now().UTC().UnixMilli(),
		Payload:     payload,
	}); err != nil {
		return fmt.Errorf("persist Fleet container detail: %w", err)
	}
	r.mu.Lock()
	if r.details[nodeID] == nil {
		r.details[nodeID] = make(map[string]fleetprotocol.Detail)
	}
	r.details[nodeID][detail.ContainerID] = detail
	r.mu.Unlock()
	return nil
}

func (r *fleetSessionRegistry) cachedDetail(nodeID, containerID string) (fleetprotocol.Detail, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	detail, ok := r.details[nodeID][containerID]
	return detail, ok
}

func (r *fleetSessionRegistry) disconnectAndForget(nodeID string) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	session := r.active(nodeID)
	if session != nil {
		_ = session.write(fleetprotocol.MessageTypeDisconnect, uuid.NewString(), "", fleetprotocol.DisconnectPayload{
			ConnectionID: session.connectionID,
			SessionID:    session.sessionID,
			Reason:       "revoked",
		})
		session.close()
	}
	r.mu.Lock()
	delete(r.snapshots, nodeID)
	delete(r.details, nodeID)
	r.mu.Unlock()
}

func (s *fleetAgentSession) execute(ctx context.Context, operationType, containerID, operationID string, parameters any, timeout time.Duration) (json.RawMessage, error) {
	if strings.TrimSpace(operationID) == "" {
		operationID = uuid.NewString()
	}
	if timeout <= 0 || timeout > fleetprotocol.CommandTimeout {
		timeout = fleetprotocol.CommandTimeout
	}
	parametersRaw, err := json.Marshal(parameters)
	if err != nil {
		return nil, err
	}
	resultCh := make(chan fleetCommandResponse, 2)
	s.pendingMu.Lock()
	if _, exists := s.pending[operationID]; exists {
		s.pendingMu.Unlock()
		return nil, &FleetAgentResponseError{StatusCode: http.StatusConflict, Code: "FLEET_CONTAINER_OPERATION_CONFLICT"}
	}
	s.pending[operationID] = resultCh
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, operationID)
		s.pendingMu.Unlock()
	}()

	messageID := uuid.NewString()
	if err := s.write(fleetprotocol.MessageTypeCommand, messageID, "", fleetprotocol.CommandPayload{
		ConnectionID:  s.connectionID,
		SessionID:     s.sessionID,
		OperationID:   operationID,
		OperationType: operationType,
		Target:        fleetprotocol.CommandTarget{ContainerID: containerID},
		Parameters:    parametersRaw,
		Deadline:      time.Now().Add(timeout).UTC().UnixMilli(),
	}); err != nil {
		return nil, &FleetAgentResponseError{StatusCode: http.StatusServiceUnavailable, Code: "FLEET_AGENT_UNAVAILABLE"}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, &FleetAgentResponseError{StatusCode: http.StatusGatewayTimeout, Code: "FLEET_OPERATION_TIMEOUT"}
	case <-s.done:
		return nil, &FleetAgentResponseError{StatusCode: http.StatusServiceUnavailable, Code: "FLEET_NODE_OFFLINE"}
	case response := <-resultCh:
		if response.err != nil {
			return nil, response.err
		}
		if response.result.Status != "succeeded" {
			return nil, fleetCommandError(response.result.ErrorCode)
		}
		return response.result.Result, nil
	}
}

func (s *fleetAgentSession) requestSnapshot(ctx context.Context) error {
	wait := s.snapshotWaiter()
	if err := s.write(fleetprotocol.MessageTypeSnapshotRequest, uuid.NewString(), "", fleetprotocol.SnapshotRequestPayload{
		ConnectionID: s.connectionID,
		SessionID:    s.sessionID,
		Reason:       "apiRefresh",
	}); err != nil {
		return err
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return &FleetAgentResponseError{StatusCode: http.StatusGatewayTimeout, Code: "FLEET_OPERATION_TIMEOUT"}
	case <-s.done:
		return &FleetAgentResponseError{StatusCode: http.StatusServiceUnavailable, Code: "FLEET_NODE_OFFLINE"}
	case <-wait:
		return nil
	}
}

func (s *fleetAgentSession) snapshotWaiter() <-chan struct{} {
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	return s.snapshotWait
}

func (s *fleetAgentSession) notifySnapshot() {
	s.snapshotMu.Lock()
	close(s.snapshotWait)
	s.snapshotWait = make(chan struct{})
	s.snapshotMu.Unlock()
}

func (s *fleetAgentSession) complete(operationID string, response fleetCommandResponse) {
	s.pendingMu.Lock()
	resultCh := s.pending[operationID]
	s.pendingMu.Unlock()
	if resultCh == nil {
		return
	}
	select {
	case resultCh <- response:
	default:
	}
}

func (s *fleetAgentSession) write(messageType fleetprotocol.MessageType, messageID, replyTo string, payload any) error {
	return s.registry.publish(s.authID, messageType, messageID, replyTo, payload)
}

func (s *fleetAgentSession) touchHeartbeat(now time.Time) {
	s.heartbeatMu.Lock()
	s.lastHeartbeat = now
	s.heartbeatMu.Unlock()
}

func (s *fleetAgentSession) heartbeatAt() time.Time {
	s.heartbeatMu.RLock()
	defer s.heartbeatMu.RUnlock()
	return s.lastHeartbeat
}

func (s *fleetAgentSession) matches(connectionID, sessionID string) bool {
	return strings.TrimSpace(connectionID) == s.connectionID && strings.TrimSpace(sessionID) == s.sessionID
}

func (s *fleetAgentSession) close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.pendingMu.Lock()
		for _, resultCh := range s.pending {
			select {
			case resultCh <- fleetCommandResponse{err: &FleetAgentResponseError{StatusCode: http.StatusServiceUnavailable, Code: "FLEET_NODE_OFFLINE"}}:
			default:
			}
		}
		s.pendingMu.Unlock()
		s.registry.remove(s)
		logx.Infof("fleet agent MQTT disconnected nodeID=%s sessionID=%s", s.nodeID, s.sessionID)
	})
}

func fleetCommandError(code string) error {
	code = strings.TrimSpace(code)
	switch code {
	case "FLEET_CONTAINER_UNSUPPORTED":
		return &FleetAgentResponseError{StatusCode: http.StatusNotImplemented, Code: code}
	case "FLEET_CONTAINER_NOT_FOUND", "FLEET_CONTAINER_NOT_MANAGED":
		return &FleetAgentResponseError{StatusCode: http.StatusNotFound, Code: code}
	case "FLEET_CONTAINER_RESTART_FORBIDDEN":
		return &FleetAgentResponseError{StatusCode: http.StatusForbidden, Code: code}
	case "FLEET_CONTAINER_OPERATION_CONFLICT":
		return &FleetAgentResponseError{StatusCode: http.StatusConflict, Code: code}
	case "FLEET_OPERATION_TIMEOUT":
		return &FleetAgentResponseError{StatusCode: http.StatusGatewayTimeout, Code: code}
	case "":
		return &FleetAgentResponseError{StatusCode: http.StatusBadGateway, Code: "FLEET_AGENT_COMMAND_FAILED"}
	default:
		return &FleetAgentResponseError{StatusCode: http.StatusBadGateway, Code: code}
	}
}

func (r *fleetSessionRegistry) requireContainer(nodeID, containerID string) (fleetprotocol.Summary, error) {
	snapshot, ok := r.latestSnapshot(nodeID)
	if !ok {
		return fleetprotocol.Summary{}, &FleetAgentResponseError{StatusCode: http.StatusServiceUnavailable, Code: "FLEET_AGENT_UNAVAILABLE"}
	}
	for _, container := range snapshot.Containers.Containers {
		if container.ContainerID == containerID {
			return container, nil
		}
	}
	return fleetprotocol.Summary{}, &FleetAgentResponseError{StatusCode: http.StatusNotFound, Code: "FLEET_CONTAINER_NOT_FOUND"}
}

func (r *fleetSessionRegistry) requireSession(nodeID string) (*fleetAgentSession, error) {
	session := r.active(nodeID)
	if session == nil {
		return nil, &FleetAgentResponseError{StatusCode: http.StatusServiceUnavailable, Code: "FLEET_NODE_OFFLINE"}
	}
	return session, nil
}

func unmarshalFleetResult(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty Fleet Agent result")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode Fleet Agent result: %w", err)
	}
	return nil
}

// recordFleetNodeSessionAudit 记录 fleet 节点会话上下线(系统级事件,operator=system)。
func (s *Service) recordFleetNodeSessionAudit(nodeID, nodeName, businessType, reason string) {
	if s == nil || s.audit == nil {
		return
	}
	s.audit.RecordDirect(nil, auditdomain.RecordInput{
		ScopeType:    auditdomain.ScopeTypePlatform,
		ResType:      auditdomain.ResTypeFleet,
		ResID:        strings.TrimSpace(nodeID),
		ResName:      strings.TrimSpace(nodeName),
		BusinessType: businessType,
		Detail:       map[string]any{"reason": reason},
	})
}
