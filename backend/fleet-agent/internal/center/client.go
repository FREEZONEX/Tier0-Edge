package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"backend/fleet-agent/internal/config"
	fleetcontainer "backend/fleet-agent/internal/container"
	"backend/fleet-agent/internal/hostinfo"
	"backend/internal/fleetprotocol"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

const mqttConnectTimeout = 10 * time.Second

var (
	errCenterDisconnected = errors.New("Fleet Center requested disconnect")
	errCenterSessionReset = errors.New("Fleet Center session no longer exists")
)

type statusUpdater interface {
	SetConnectionState(string)
}

type containerService interface {
	Snapshot(context.Context) (fleetcontainer.Snapshot, error)
	Detail(context.Context, string) (fleetcontainer.Detail, error)
	Logs(context.Context, string, fleetcontainer.LogsRequest) (fleetcontainer.LogsResult, error)
	Restart(context.Context, string, fleetcontainer.RestartRequest) (fleetcontainer.RestartResult, error)
}

type Client struct {
	networkMu        sync.Mutex
	networkOperation sync.Mutex
	networkCfg       config.Config
	networkEnabled   bool
	networkWake      chan struct{}
	cfg              config.Config
	updates          chan config.Config
	enables          chan bool
	enableMu         sync.Mutex
	version          string
	status           statusUpdater
	containers       containerService
	logger           *slog.Logger
}

type inboundMessage struct {
	topic   string
	payload []byte
}

func New(cfg config.Config, version string, status statusUpdater, containers containerService, logger *slog.Logger) *Client {
	return &Client{
		cfg:        cfg,
		networkCfg: cfg, networkEnabled: !cfg.HA.Enabled, networkWake: make(chan struct{}, 1),
		updates:    make(chan config.Config, 1),
		enables:    make(chan bool, 1),
		version:    version,
		status:     status,
		containers: containers,
		logger:     logger,
	}
}

func (c *Client) Run(ctx context.Context) {
	cfg := c.cfg
	enabled := !cfg.HA.Enabled
	delay := time.Second
	for {
		if !enabled {
			c.status.SetConnectionState(fleetprotocol.ConnectionStandby)
			select {
			case <-ctx.Done():
				return
			case cfg = <-c.updates:
				continue
			case enabled = <-c.enables:
				delay = time.Second
				continue
			}
		}
		if cfg.CenterConfigured() && !cfg.DesiredConnected {
			c.status.SetConnectionState(fleetprotocol.ConnectionDisconnected)
			select {
			case <-ctx.Done():
				return
			case cfg = <-c.updates:
				delay = time.Second
				continue
			case enabled = <-c.enables:
				delay = time.Second
				continue
			}
		}
		if !cfg.CenterConfigured() {
			c.status.SetConnectionState(fleetprotocol.ConnectionUnconfigured)
			select {
			case <-ctx.Done():
				return
			case cfg = <-c.updates:
				delay = time.Second
				continue
			case enabled = <-c.enables:
				delay = time.Second
				continue
			}
		}
		if cfg.TokenNetworkBootstrapURL != "" {
			// HTTP-enrolled installations use the overlay/local Agent management
			// path. RunNetwork owns connection status; no broker connection.
			select {
			case <-ctx.Done():
				return
			case cfg = <-c.updates:
				continue
			case enabled = <-c.enables:
				continue
			}
		}
		attemptStartedAt := time.Now()
		c.status.SetConnectionState(fleetprotocol.ConnectionConnecting)
		attemptCtx, cancel := context.WithCancel(ctx)
		result := make(chan error, 1)
		go func(current config.Config) { result <- c.connect(attemptCtx, current) }(cfg)
		var err error
		select {
		case <-ctx.Done():
			cancel()
			return
		case cfg = <-c.updates:
			cancel()
			<-result
			delay = time.Second
			continue
		case enabled = <-c.enables:
			cancel()
			<-result
			delay = time.Second
			continue
		case err = <-result:
			cancel()
		}
		c.status.SetConnectionState(fleetprotocol.ConnectionDisconnected)
		if time.Since(attemptStartedAt) >= 2*fleetprotocol.OfflineThreshold {
			delay = time.Second
		}
		c.logger.Warn("fleet center MQTT connection ended", "error", sanitizeError(err), "retryDelay", delay.String())
		timer := time.NewTimer(withJitter(delay))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case cfg = <-c.updates:
			timer.Stop()
			delay = time.Second
			continue
		case enabled = <-c.enables:
			timer.Stop()
			delay = time.Second
			continue
		case <-timer.C:
		}
		delay *= 2
		if delay > fleetprotocol.ReconnectMaxDelay {
			delay = fleetprotocol.ReconnectMaxDelay
		}
	}
}

func (c *Client) SetEnabled(enabled bool) {
	c.networkMu.Lock()
	c.networkEnabled = enabled
	c.networkMu.Unlock()
	select {
	case c.networkWake <- struct{}{}:
	default:
	}
	c.enableMu.Lock()
	defer c.enableMu.Unlock()
	select {
	case c.enables <- enabled:
	default:
		select {
		case <-c.enables:
		default:
		}
		c.enables <- enabled
	}
}

func (c *Client) UpdateConfig(cfg config.Config) {
	c.networkMu.Lock()
	c.networkCfg = cfg
	c.networkMu.Unlock()
	select {
	case c.networkWake <- struct{}{}:
	default:
	}
	select {
	case c.updates <- cfg:
	default:
		select {
		case <-c.updates:
		default:
		}
		c.updates <- cfg
	}
}

func (c *Client) connect(ctx context.Context, cfg config.Config) error {
	connectionID := uuid.NewString()
	inbound := make(chan inboundMessage, 64)
	lost := make(chan error, 1)

	options := mqtt.NewClientOptions().
		SetClientID(cfg.MQTTClientID).
		SetUsername(cfg.MQTTUsername).
		SetPassword(cfg.MQTTPassword).
		SetCleanSession(true).
		SetAutoReconnect(false).
		SetConnectTimeout(mqttConnectTimeout).
		SetOrderMatters(false)
	for _, broker := range cfg.MQTTBrokers {
		options.AddBroker(broker)
	}
	disconnectPayload, err := fleetprotocol.MarshalEnvelope(
		fleetprotocol.MessageTypeDisconnect,
		uuid.NewString(),
		"",
		fleetprotocol.DisconnectPayload{ConnectionID: connectionID, Reason: "connectionLost"},
	)
	if err != nil {
		return err
	}
	disconnectTopic := fleetprotocol.MustManagementTopic(cfg.MQTTAuthID, fleetprotocol.TopicDisconnect)
	options.SetWill(disconnectTopic, string(disconnectPayload), 1, false)
	options.SetConnectionLostHandler(func(_ mqtt.Client, connectionErr error) {
		select {
		case lost <- connectionErr:
		default:
		}
	})
	options.SetDefaultPublishHandler(func(_ mqtt.Client, message mqtt.Message) {
		payload := append([]byte(nil), message.Payload()...)
		select {
		case inbound <- inboundMessage{topic: message.Topic(), payload: payload}:
		default:
			c.logger.Warn("fleet MQTT inbound queue is full", "topic", message.Topic())
		}
	})

	client := mqtt.NewClient(options)
	connectToken := client.Connect()
	if !connectToken.WaitTimeout(mqttConnectTimeout) {
		return errors.New("connect Fleet MQTT timeout")
	}
	if err := connectToken.Error(); err != nil {
		return fmt.Errorf("connect Fleet MQTT: %w", err)
	}
	defer client.Disconnect(250)

	subscriptions := map[string]byte{}
	for _, kind := range []string{
		fleetprotocol.TopicHelloAck,
		fleetprotocol.TopicError,
		fleetprotocol.TopicSnapshotRequest,
		fleetprotocol.TopicCommand,
		fleetprotocol.TopicDisconnect,
	} {
		subscriptions[fleetprotocol.MustManagementTopic(cfg.MQTTAuthID, kind)] = 1
	}
	subscribeToken := client.SubscribeMultiple(subscriptions, nil)
	if !subscribeToken.WaitTimeout(fleetprotocol.WriteTimeout) {
		return errors.New("subscribe Fleet MQTT topics timeout")
	}
	if err := subscribeToken.Error(); err != nil {
		return fmt.Errorf("subscribe Fleet MQTT topics: %w", err)
	}

	writer := &mqttWriter{client: client, authID: cfg.MQTTAuthID, connectionID: connectionID}
	helloID := uuid.NewString()
	host := hostinfo.Collect()
	if err := writer.write(fleetprotocol.MessageTypeHello, helloID, "", fleetprotocol.HelloPayload{
		FleetNodeID:    cfg.FleetNodeID,
		ConnectionID:   connectionID,
		AgentVersion:   c.version,
		OS:             host.OS,
		Arch:           host.Arch,
		InstallationID: cfg.InstallationID,
		Capabilities:   []string{"container.list", "container.detail", "container.logs", "container.restart"},
	}); err != nil {
		return fmt.Errorf("publish Fleet hello: %w", err)
	}

	ack, err := c.waitHelloAck(ctx, inbound, lost, helloID)
	if err != nil {
		return err
	}
	writer.setSession(ack.SessionID)
	heartbeatInterval := durationSeconds(ack.HeartbeatIntervalSeconds, fleetprotocol.HeartbeatInterval)
	snapshotInterval := durationSeconds(ack.SnapshotIntervalSeconds, fleetprotocol.SnapshotInterval)
	c.status.SetConnectionState(fleetprotocol.ConnectionConnected)
	c.logger.Info("fleet center MQTT connected", "sessionID", ack.SessionID)

	if err := c.sendSnapshot(ctx, writer, ""); err != nil {
		c.logger.Warn("initial fleet snapshot failed", "sessionID", ack.SessionID, "error", sanitizeError(err))
	}
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer heartbeatTicker.Stop()
	snapshotTicker := time.NewTicker(snapshotInterval)
	defer snapshotTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = writer.write(fleetprotocol.MessageTypeDisconnect, uuid.NewString(), "", fleetprotocol.DisconnectPayload{
				ConnectionID: connectionID,
				SessionID:    ack.SessionID,
				Reason:       "shutdown",
			})
			return ctx.Err()
		case connectionErr := <-lost:
			return fmt.Errorf("Fleet MQTT connection lost: %w", connectionErr)
		case message := <-inbound:
			if err := c.handleMessage(ctx, writer, message); err != nil {
				if errors.Is(err, errCenterDisconnected) || errors.Is(err, errCenterSessionReset) {
					return err
				}
				c.logger.Warn("fleet center MQTT message rejected", "topic", message.topic, "error", sanitizeError(err))
			}
		case <-heartbeatTicker.C:
			if err := writer.write(fleetprotocol.MessageTypeHeartbeat, uuid.NewString(), "", fleetprotocol.HeartbeatPayload{
				SessionID: ack.SessionID,
				AgentTime: time.Now().UTC().UnixMilli(),
			}); err != nil {
				return fmt.Errorf("publish Fleet heartbeat: %w", err)
			}
		case <-snapshotTicker.C:
			if err := c.sendSnapshot(ctx, writer, ""); err != nil {
				c.logger.Warn("scheduled fleet snapshot failed", "sessionID", ack.SessionID, "error", sanitizeError(err))
			}
		}
	}
}

func (c *Client) waitHelloAck(ctx context.Context, inbound <-chan inboundMessage, lost <-chan error, helloID string) (fleetprotocol.HelloAckPayload, error) {
	timer := time.NewTimer(mqttConnectTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return fleetprotocol.HelloAckPayload{}, ctx.Err()
		case connectionErr := <-lost:
			return fleetprotocol.HelloAckPayload{}, fmt.Errorf("Fleet MQTT connection lost before hello ack: %w", connectionErr)
		case <-timer.C:
			return fleetprotocol.HelloAckPayload{}, errors.New("Fleet Center hello acknowledgement timeout")
		case message := <-inbound:
			var envelope fleetprotocol.Envelope
			if len(message.payload) > fleetprotocol.MaxMessageBytes || json.Unmarshal(message.payload, &envelope) != nil {
				continue
			}
			if envelope.Type == fleetprotocol.MessageTypeError && envelope.ReplyTo == helloID {
				var payload fleetprotocol.ErrorPayload
				_ = json.Unmarshal(envelope.Payload, &payload)
				return fleetprotocol.HelloAckPayload{}, fmt.Errorf("Fleet Center rejected hello: %s", payload.Code)
			}
			if envelope.ProtocolVersion != fleetprotocol.ProtocolVersion || envelope.Type != fleetprotocol.MessageTypeHelloAck || envelope.ReplyTo != helloID {
				continue
			}
			var ack fleetprotocol.HelloAckPayload
			if json.Unmarshal(envelope.Payload, &ack) != nil || strings.TrimSpace(ack.SessionID) == "" {
				continue
			}
			return ack, nil
		}
	}
}

func (c *Client) handleMessage(ctx context.Context, writer *mqttWriter, message inboundMessage) error {
	if len(message.payload) > fleetprotocol.MaxMessageBytes {
		return errors.New("Fleet MQTT message is too large")
	}
	_, kind, ok := fleetprotocol.ParseManagementTopic(message.topic)
	if !ok {
		return errors.New("invalid Fleet MQTT topic")
	}
	var envelope fleetprotocol.Envelope
	if err := json.Unmarshal(message.payload, &envelope); err != nil {
		return fmt.Errorf("decode Fleet MQTT envelope: %w", err)
	}
	if envelope.ProtocolVersion != fleetprotocol.ProtocolVersion {
		return errors.New("unsupported protocol version")
	}
	switch envelope.Type {
	case fleetprotocol.MessageTypeSnapshotRequest:
		if kind != fleetprotocol.TopicSnapshotRequest {
			return errors.New("Fleet MQTT topic and message type mismatch")
		}
		var request fleetprotocol.SnapshotRequestPayload
		if json.Unmarshal(envelope.Payload, &request) != nil || !writer.matches(request.ConnectionID, request.SessionID) {
			return errors.New("stale Fleet snapshot request")
		}
		return c.sendSnapshot(ctx, writer, envelope.MessageID)
	case fleetprotocol.MessageTypeCommand:
		if kind != fleetprotocol.TopicCommand {
			return errors.New("Fleet MQTT topic and message type mismatch")
		}
		var command fleetprotocol.CommandPayload
		if err := json.Unmarshal(envelope.Payload, &command); err != nil {
			return fmt.Errorf("decode command: %w", err)
		}
		if !writer.matches(command.ConnectionID, command.SessionID) {
			return errors.New("stale Fleet command")
		}
		go c.executeCommand(ctx, writer, envelope.MessageID, command)
		return nil
	case fleetprotocol.MessageTypeDisconnect:
		var payload fleetprotocol.DisconnectPayload
		if json.Unmarshal(envelope.Payload, &payload) != nil || !writer.matches(payload.ConnectionID, payload.SessionID) {
			return errors.New("stale Fleet disconnect")
		}
		return errCenterDisconnected
	case fleetprotocol.MessageTypeError:
		if kind != fleetprotocol.TopicError {
			return errors.New("Fleet MQTT topic and message type mismatch")
		}
		var payload fleetprotocol.ErrorPayload
		_ = json.Unmarshal(envelope.Payload, &payload)
		if payload.Code == fleetprotocol.ErrorCodeSessionUnknown {
			return errCenterSessionReset
		}
		return fmt.Errorf("center protocol error: %s", payload.Code)
	default:
		return fmt.Errorf("unsupported message type %q", envelope.Type)
	}
}

func (c *Client) sendSnapshot(ctx context.Context, writer *mqttWriter, replyTo string) error {
	containers, err := c.containers.Snapshot(ctx)
	if err != nil {
		return err
	}
	payload := fleetprotocol.SnapshotPayload{
		SnapshotID: containers.SnapshotID,
		ObservedAt: containers.ObservedAt,
		Host:       hostinfo.Collect(),
		Containers: containers,
	}
	return writer.write(fleetprotocol.MessageTypeSnapshot, uuid.NewString(), replyTo, payload)
}

func (c *Client) executeCommand(parent context.Context, writer *mqttWriter, replyTo string, command fleetprotocol.CommandPayload) {
	accepted := command.OperationID != "" && command.Target.ContainerID != ""
	if !accepted {
		_ = writer.write(fleetprotocol.MessageTypeCommandAck, uuid.NewString(), replyTo, fleetprotocol.CommandAckPayload{
			OperationID: command.OperationID,
			Accepted:    false,
			ErrorCode:   "FLEET_COMMAND_INVALID",
		})
		return
	}
	if err := writer.write(fleetprotocol.MessageTypeCommandAck, uuid.NewString(), replyTo, fleetprotocol.CommandAckPayload{
		OperationID: command.OperationID,
		Accepted:    true,
	}); err != nil {
		return
	}

	timeout := fleetprotocol.CommandTimeout
	if command.Deadline > 0 {
		remaining := time.Until(time.UnixMilli(command.Deadline))
		if remaining <= 0 {
			c.sendCommandError(writer, replyTo, command.OperationID, "FLEET_OPERATION_TIMEOUT")
			return
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	var result any
	var err error
	switch command.OperationType {
	case fleetprotocol.OperationContainerDetail:
		result, err = c.containers.Detail(ctx, command.Target.ContainerID)
	case fleetprotocol.OperationContainerLogs:
		var request fleetcontainer.LogsRequest
		if len(command.Parameters) > 0 {
			err = json.Unmarshal(command.Parameters, &request)
		}
		if err == nil {
			result, err = c.containers.Logs(ctx, command.Target.ContainerID, request)
		}
	case fleetprotocol.OperationContainerRestart:
		result, err = c.containers.Restart(ctx, command.Target.ContainerID, fleetcontainer.RestartRequest{OperationID: command.OperationID})
	default:
		err = errors.New("unsupported operation type")
	}
	if err != nil {
		c.sendCommandError(writer, replyTo, command.OperationID, commandErrorCode(err))
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		c.sendCommandError(writer, replyTo, command.OperationID, "FLEET_RESULT_INVALID")
		return
	}
	_ = writer.write(fleetprotocol.MessageTypeCommandResult, uuid.NewString(), replyTo, fleetprotocol.CommandResultPayload{
		OperationID: command.OperationID,
		Status:      "succeeded",
		Result:      raw,
		FinishedAt:  time.Now().UTC().UnixMilli(),
	})
	if command.OperationType == fleetprotocol.OperationContainerRestart {
		if err := c.sendSnapshot(parent, writer, ""); err != nil {
			c.logger.Warn("post-restart fleet snapshot failed", "operationID", command.OperationID, "error", sanitizeError(err))
		}
	}
}

func (c *Client) sendCommandError(writer *mqttWriter, replyTo, operationID, code string) {
	_ = writer.write(fleetprotocol.MessageTypeCommandResult, uuid.NewString(), replyTo, fleetprotocol.CommandResultPayload{
		OperationID: operationID,
		Status:      "failed",
		ErrorCode:   code,
		FinishedAt:  time.Now().UTC().UnixMilli(),
	})
}

type mqttWriter struct {
	mu           sync.Mutex
	client       mqtt.Client
	authID       string
	connectionID string
	sessionID    string
}

func (w *mqttWriter) setSession(sessionID string) {
	w.mu.Lock()
	w.sessionID = strings.TrimSpace(sessionID)
	w.mu.Unlock()
}

func (w *mqttWriter) matches(connectionID, sessionID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.TrimSpace(connectionID) == w.connectionID && strings.TrimSpace(sessionID) == w.sessionID
}

func (w *mqttWriter) write(messageType fleetprotocol.MessageType, messageID, replyTo string, payload any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	payload = w.withSessionIdentity(payload)
	kind := topicKindForMessage(messageType)
	topic := fleetprotocol.MustManagementTopic(w.authID, kind)
	if topic == "" {
		return errors.New("invalid Fleet MQTT publish topic")
	}
	raw, err := fleetprotocol.MarshalEnvelope(messageType, messageID, replyTo, payload)
	if err != nil {
		return err
	}
	if len(raw) > fleetprotocol.MaxMessageBytes {
		return errors.New("Fleet MQTT message is too large")
	}
	token := w.client.Publish(topic, 1, false, raw)
	if !token.WaitTimeout(fleetprotocol.WriteTimeout) {
		return errors.New("publish Fleet MQTT message timeout")
	}
	return token.Error()
}

func (w *mqttWriter) withSessionIdentity(payload any) any {
	switch value := payload.(type) {
	case fleetprotocol.SnapshotPayload:
		value.ConnectionID = w.connectionID
		value.SessionID = w.sessionID
		return value
	case fleetprotocol.CommandAckPayload:
		value.ConnectionID = w.connectionID
		value.SessionID = w.sessionID
		return value
	case fleetprotocol.CommandResultPayload:
		value.ConnectionID = w.connectionID
		value.SessionID = w.sessionID
		return value
	default:
		return payload
	}
}

func topicKindForMessage(messageType fleetprotocol.MessageType) string {
	switch messageType {
	case fleetprotocol.MessageTypeHello:
		return fleetprotocol.TopicHello
	case fleetprotocol.MessageTypeHeartbeat:
		return fleetprotocol.TopicHeartbeat
	case fleetprotocol.MessageTypeSnapshot:
		return fleetprotocol.TopicSnapshot
	case fleetprotocol.MessageTypeCommandAck:
		return fleetprotocol.TopicCommandAck
	case fleetprotocol.MessageTypeCommandResult:
		return fleetprotocol.TopicCommandResult
	case fleetprotocol.MessageTypeDisconnect:
		return fleetprotocol.TopicDisconnect
	default:
		return ""
	}
}

func durationSeconds(value int, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return time.Duration(value) * time.Second
}

func withJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return time.Second
	}
	return delay + time.Duration(rand.Int64N(int64(delay/4)+1))
}

func commandErrorCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "FLEET_OPERATION_TIMEOUT"
	case errors.Is(err, fleetcontainer.ErrNotFound):
		return "FLEET_CONTAINER_NOT_FOUND"
	case errors.Is(err, fleetcontainer.ErrNotManaged):
		return "FLEET_CONTAINER_NOT_MANAGED"
	case errors.Is(err, fleetcontainer.ErrRestartDenied):
		return "FLEET_CONTAINER_RESTART_FORBIDDEN"
	case errors.Is(err, fleetcontainer.ErrConflict):
		return "FLEET_CONTAINER_OPERATION_CONFLICT"
	case errors.Is(err, fleetcontainer.ErrInvalid):
		return "FLEET_COMMAND_INVALID"
	default:
		return "FLEET_AGENT_COMMAND_FAILED"
	}
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if strings.TrimSpace(message) == "" {
		return "fleet connection error"
	}
	return message
}
