package sourceprotocol

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/internal/domain/sourcestream"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/google/uuid"
)

var ErrProtocol = errors.New("SourceStream.protocolFailed")

type Input = sourcestream.SourceInput

// 驱动是执行层内部装配项，不建立用户管理实体或安装流程。
type Driver interface {
	Probe(context.Context, int64, sourcestream.RuntimeSource, sourcestream.Credential) error
	Discover(context.Context, int64, sourcestream.RuntimeSource, sourcestream.Credential, string) ([]sourcestream.NativeResource, error)
	Invoke(context.Context, int64, sourcestream.RuntimeSource, sourcestream.Credential, string, any) (any, error)
}
type WatchDriver interface {
	Watch(context.Context, int64, sourcestream.RuntimeSource, sourcestream.Credential, func(Input) error) error
}
type Runtime struct {
	hidPreviewMu  sync.Mutex
	drivers       map[sourcestream.Protocol]Driver
	serverDrivers map[sourcestream.Protocol]Driver
	connections   *sourceConnectionPool
	watchMu       sync.Mutex
	watches       map[string]*sourceWatch
	sharedMu      sync.Mutex
	shared        map[string]*sharedWatch
	closed        bool
}

func New() *Runtime {
	return &Runtime{drivers: make(map[sourcestream.Protocol]Driver), serverDrivers: make(map[sourcestream.Protocol]Driver), connections: newSourceConnectionPool(), watches: make(map[string]*sourceWatch)}
}
func (r *Runtime) RegisterServer(protocol sourcestream.Protocol, driver Driver) {
	if pooled, ok := driver.(interface {
		withConnectionPool(*sourceConnectionPool) Driver
	}); ok {
		driver = pooled.withConnectionPool(newSourceConnectionPool())
	}
	r.serverDrivers[protocol] = driver
}
func (r *Runtime) Register(protocol sourcestream.Protocol, driver Driver) {
	if pooled, ok := driver.(interface {
		withConnectionPool(*sourceConnectionPool) Driver
	}); ok {
		driver = pooled.withConnectionPool(newSourceConnectionPool())
	}
	r.drivers[protocol] = driver
}

type sourceWatch struct {
	id            int64
	connectionKey string
	key           string
	cancel        context.CancelFunc
}

func (r *Runtime) watchContext(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential) (context.Context, func(), error) {
	key, err := sourceConnectionKey(ctx, id, def, credential)
	if err != nil {
		return nil, nil, err
	}
	scopeDef := def.Connection()
	scopeDef.Name, scopeDef.Description = "", ""
	if scopeDef.MQTT != nil {
		scopeDef.MQTT.Subscriptions = mqttSubscriptions(def)
		scopeDef.MQTT.TopicFilters = nil
		scopeDef.MQTT.QoS = 0
	}
	if scopeDef.OPCUA != nil {
		scopeDef.OPCUA.NodeIDs = append([]string(nil), def.OPCUA.NodeIDs...)
		scopeDef.OPCUA.SamplingIntervalMs = def.OPCUA.SamplingIntervalMs
		sort.Strings(scopeDef.OPCUA.NodeIDs)
	}
	raw, err := json.Marshal(scopeDef)
	if err != nil {
		return nil, nil, ErrProtocol
	}
	scopeKey := key + ":" + fmt.Sprintf("%x", sha256.Sum256(raw))
	if def.MQTT != nil && def.MQTT.Ephemeral {
		scopeKey += ":" + uuid.NewString()
	}
	r.watchMu.Lock()
	if r.closed {
		r.watchMu.Unlock()
		return nil, nil, sourcestream.ErrUnavailable
	}
	for ownerKey, old := range r.watches {
		if old.id == id && old.connectionKey != key {
			old.cancel()
			delete(r.watches, ownerKey)
		}
	}
	if r.watches[scopeKey] != nil || len(r.watches) >= sourcePoolLimit {
		r.watchMu.Unlock()
		return nil, nil, ErrProtocol
	}
	life, cancel := context.WithCancel(ctx)
	owner := &sourceWatch{id: id, key: scopeKey, connectionKey: key, cancel: cancel}
	r.watches[scopeKey] = owner
	r.watchMu.Unlock()
	return life, func() {
		cancel()
		r.watchMu.Lock()
		if r.watches[scopeKey] == owner {
			delete(r.watches, scopeKey)
		}
		r.watchMu.Unlock()
	}, nil
}

// Source lifecycle owns all managed connections, including a running watch.
func (r *Runtime) ReleaseSource(id int64) {
	r.watchMu.Lock()
	for key, watch := range r.watches {
		if watch.id == id {
			delete(r.watches, key)
			watch.cancel()
		}
	}
	r.watchMu.Unlock()
	r.connections.releaseSource(id)
	for _, driver := range r.lifecycleDrivers() {
		if lifecycle, ok := driver.(interface{ ReleaseSource(int64) }); ok {
			lifecycle.ReleaseSource(id)
		}
	}
}

func (r *Runtime) Close() {
	r.watchMu.Lock()
	if r.closed {
		r.watchMu.Unlock()
		return
	}
	r.closed = true
	for id, watch := range r.watches {
		delete(r.watches, id)
		watch.cancel()
	}
	r.watchMu.Unlock()
	r.connections.close()
	for _, driver := range r.lifecycleDrivers() {
		if lifecycle, ok := driver.(interface{ Close() }); ok {
			lifecycle.Close()
		}
	}
}

// The OPC UA bridge serves both roles. Calling its lifecycle twice could
// retire a replacement session created between the two cleanup requests.
func (r *Runtime) lifecycleDrivers() []Driver {
	seen := make(map[Driver]bool)
	result := []Driver{}
	for _, drivers := range []map[sourcestream.Protocol]Driver{r.drivers, r.serverDrivers} {
		for _, driver := range drivers {
			if driver == nil {
				continue
			}
			if reflect.TypeOf(driver).Comparable() {
				if seen[driver] {
					continue
				}
				seen[driver] = true
			}
			result = append(result, driver)
		}
	}
	return result
}

func (r *Runtime) isClosed() bool {
	r.watchMu.Lock()
	defer r.watchMu.Unlock()
	return r.closed
}

func (r *Runtime) withMQTT(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, action func(context.Context, mqtt.Client) (any, error)) (any, error) {
	entry, err := r.connections.entry(ctx, id, def, credential)
	if err != nil {
		return nil, err
	}
	connect := func(ctx context.Context) (any, func(), error) {
		client, err := mqttConnection(ctx, id, def, credential)
		if err != nil {
			return nil, nil, err
		}
		return client, func() { client.Disconnect(100) }, nil
	}
	return r.connections.withEntry(ctx, entry, connect, func(ctx context.Context, value any) (any, error) {
		client := value.(mqtt.Client)
		if !client.IsConnectionOpen() {
			// A known-closed transport can be replaced before any dispatch.
			// Never retry action: losing a publish/reply may mean the device
			// already acted. Keep Source generation and cancellation ownership.
			next, err := r.connections.replaceValue(ctx, entry, connect)
			if err != nil {
				return nil, err
			}
			client = next.(mqtt.Client)
		}
		return action(ctx, client)
	})
}

func waitMQTT(ctx context.Context, token mqtt.Token) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-token.Done():
		if errors.Is(token.Error(), packets.ErrorRefusedBadUsernameOrPassword) || errors.Is(token.Error(), packets.ErrorRefusedNotAuthorised) {
			return sourcestream.ErrCredential
		}
		if token.Error() != nil {
			return ErrProtocol
		}
		return nil
	}
}
func mqttConnection(ctx context.Context, id int64, def sourcestream.RuntimeSource, credential sourcestream.Credential) (mqtt.Client, error) {
	return connectMQTT(ctx, id, def, credential, false, nil)
}

// Watch groups share Source ownership, not MQTT client identities. Display
// fields and credentials never participate in this subscription-scope suffix.
func mqttSubscriptions(def sourcestream.SourceDefinition) map[string]byte {
	subscriptions := map[string]byte{}
	for _, filter := range def.MQTT.TopicFilters {
		subscriptions[filter] = byte(def.MQTT.QoS)
	}
	for filter, qos := range def.MQTT.Subscriptions {
		subscriptions[filter] = qos
	}
	return subscriptions
}

func mqttWatchScope(def sourcestream.SourceDefinition) string {
	subscriptions := mqttSubscriptions(def)
	raw, _ := json.Marshal(subscriptions)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

func connectMQTT(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, watch bool, lost func(mqtt.Client, error), handlers ...mqtt.MessageHandler) (mqtt.Client, error) {
	if def.Protocol != sourcestream.MQTT || def.MQTT == nil {
		return nil, ErrProtocol
	}
	endpoint, err := url.Parse(def.Endpoint)
	if err != nil || (endpoint.Scheme != "mqtt" && endpoint.Scheme != "mqtts") {
		return nil, ErrProtocol
	}
	endpoint.Scheme = map[string]string{"mqtt": "tcp", "mqtts": "ssl"}[endpoint.Scheme]
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	timeout := 5 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	if timeout <= 0 {
		return nil, context.DeadlineExceeded
	}
	options := mqtt.NewClientOptions().AddBroker(endpoint.String()).SetClientID(fmt.Sprintf("source-%d-%s-%s", id, def.MQTT.ClientID, uuid.NewString()[:8])).SetCleanSession(true).SetAutoReconnect(false).SetConnectTimeout(timeout).SetWriteTimeout(timeout).SetOrderMatters(false)
	if watch && !def.MQTT.Ephemeral {
		// Each watcher installs exactly the current plan set; never resume stale subscriptions.
		// Broker-side offline buffering is not retained across watcher reconnections.
		options.SetClientID(fmt.Sprintf("source-%d-%s-acquire-%s", id, def.MQTT.ClientID, mqttWatchScope(def))).SetCleanSession(true).SetAutoAckDisabled(true).SetOrderMatters(true)
		options.SetConnectionLostHandler(lost)
	}
	if len(handlers) > 0 {
		// Persistent sessions may deliver queued messages before SUBSCRIBE.
		options.SetDefaultPublishHandler(handlers[0])
	}
	config, err := sourcestream.MQTTClientTLS(def, credential)
	if err != nil {
		return nil, err
	}
	if credential.Kind == "basic" {
		options.SetUsername(credential.Username).SetPassword(credential.Secret)
	}
	if config != nil {
		options.SetTLSConfig(config)
	}
	client := mqtt.NewClient(options)
	if err = waitMQTT(ctx, client.Connect()); err != nil {
		client.Disconnect(0)
		return nil, err
	}
	return client, nil
}
func (r *Runtime) Probe(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential) error {
	if r.isClosed() {
		return sourcestream.ErrUnavailable
	}
	if def.Protocol == sourcestream.USBHID {
		return watchHID(ctx, def, nil)
	}
	if def.ConnectionRole() == sourcestream.ServerRole {
		if driver := r.serverDrivers[def.Protocol]; driver != nil {
			return driver.Probe(ctx, id, def, credential)
		}
		return sourcestream.ErrUnavailable
	}
	switch def.Protocol {
	case sourcestream.MQTT:
		_, err := r.withMQTT(ctx, id, def, credential, func(context.Context, mqtt.Client) (any, error) { return nil, nil })
		return err
	default:
		if driver := r.drivers[def.Protocol]; driver != nil {
			return driver.Probe(ctx, id, def, credential)
		}
		return sourcestream.ErrUnavailable
	}
}
func (r *Runtime) Discover(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, parent string) ([]sourcestream.NativeResource, error) {
	if r.isClosed() {
		return nil, sourcestream.ErrUnavailable
	}
	if def.Protocol == sourcestream.USBHID {
		if parent != "" {
			return []sourcestream.NativeResource{}, nil
		}
		if err := watchHID(ctx, def, nil); err != nil {
			return nil, err
		}
		return []sourcestream.NativeResource{{ID: sourcestream.HIDTextResource, Name: "Text", Models: []sourcestream.AccessModel{sourcestream.Publish}}}, nil
	}
	if def.ConnectionRole() == sourcestream.ServerRole {
		if driver := r.serverDrivers[def.Protocol]; driver != nil {
			return driver.Discover(ctx, id, def, credential, parent)
		}
		return nil, sourcestream.ErrUnavailable
	}
	switch def.Protocol {
	case sourcestream.MQTT:
		result, err := r.withMQTT(ctx, id, def, credential, func(ctx context.Context, client mqtt.Client) (any, error) {
			items, err := discoverMQTT(ctx, client, def)
			return items, err
		})
		if err != nil {
			return nil, err
		}
		return result.([]sourcestream.NativeResource), nil
	default:
		if driver := r.drivers[def.Protocol]; driver != nil {
			return driver.Discover(ctx, id, def, credential, parent)
		}
		return nil, sourcestream.ErrUnavailable
	}
}

func discoverMQTT(ctx context.Context, client mqtt.Client, def sourcestream.SourceDefinition) ([]sourcestream.NativeResource, error) {
	defer func() {
		if len(def.MQTT.TopicFilters) > 0 {
			client.Unsubscribe(def.MQTT.TopicFilters...).WaitTimeout(time.Second)
		}
	}()
	var err error
	seen := map[string]sourcestream.NativeResource{}
	var lock sync.Mutex
	for _, filter := range def.MQTT.TopicFilters {
		resource := mqttResource(filter, false)
		if strings.ContainsAny(filter, "+#") {
			// A configured input filter is selectable even before a device
			// publishes. It is not a valid destination for an MQTT publish.
			resource.Models = []sourcestream.AccessModel{sourcestream.Publish}
			resource.Metadata["topicFilter"] = true
		}
		lock.Lock()
		seen[filter] = resource
		lock.Unlock()
		if err = waitMQTT(ctx, client.Subscribe(filter, byte(def.MQTT.QoS), func(_ mqtt.Client, message mqtt.Message) {
			lock.Lock()
			if len(seen) < 1000 || seen[message.Topic()].ID != "" {
				seen[message.Topic()] = mqttResource(message.Topic(), true)
			}
			lock.Unlock()
		})); err != nil {
			return nil, err
		}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(1500 * time.Millisecond):
	}
	lock.Lock()
	defer lock.Unlock()
	items := make([]sourcestream.NativeResource, 0, len(seen)+1)
	for _, item := range seen {
		items = append(items, item)
	}
	if def.MQTT.RequestTopic != "" {
		items = append(items, sourcestream.NativeResource{ID: "mqtt-request-reply", Name: def.MQTT.RequestTopic, Models: []sourcestream.AccessModel{sourcestream.RequestReply}, Metadata: map[string]any{"requestTopic": def.MQTT.RequestTopic, "replyTopic": def.MQTT.ReplyTopic, "replyTopicField": def.MQTT.ReplyTopicField, "correlationField": def.MQTT.CorrelationField}})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// The supervisor owns reconnection. QoS 1/2 messages are acknowledged only
// after the caller has durably accepted them; QoS 0 remains best-effort.
func (r *Runtime) Watch(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, emit func(Input) error) error {
	return r.WatchWithKnownSubscriptions(ctx, id, def, credential, nil, emit)
}

// known contains only this Source's configured/discovered subscription filters.
// It is a reconciliation hint, never an authorization or additional input scope.
func (r *Runtime) WatchWithKnownSubscriptions(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, known []string, emit func(Input) error) error {
	if def.Protocol == sourcestream.ModbusTCP && def.ConnectionRole() == sourcestream.ClientRole {
		return r.sharedModbusRanges(ctx, id, def, credential, emit)
	}
	if def.Protocol == sourcestream.USBHID {
		return r.sharedSourceWatch(ctx, id, def, credential, known, emit)
	}
	if def.Protocol == sourcestream.MQTT && def.ConnectionRole() != sourcestream.ServerRole && def.MQTT != nil && !def.MQTT.Ephemeral {
		return r.sharedSourceWatch(ctx, id, def, credential, known, emit)
	}
	if def.Protocol == sourcestream.OPCUA && def.ConnectionRole() != sourcestream.ServerRole && def.OPCUA != nil {
		return r.sharedSourceWatch(ctx, id, def, credential, known, emit)
	}
	return r.watchWithKnownSubscriptions(ctx, id, def, credential, known, emit)
}

func (r *Runtime) watchWithKnownSubscriptions(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, known []string, emit func(Input) error) error {
	if def.Protocol == sourcestream.USBHID {
		return watchHID(ctx, def, emit)
	}
	ctx, done, err := r.watchContext(ctx, id, def, credential)
	if err != nil {
		return err
	}
	defer done()
	if def.ConnectionRole() == sourcestream.ServerRole {
		if driver, ok := r.serverDrivers[def.Protocol].(interface {
			WatchWithKnownSubscriptions(context.Context, int64, sourcestream.SourceDefinition, sourcestream.Credential, []string, func(Input) error) error
		}); ok {
			return driver.WatchWithKnownSubscriptions(ctx, id, def, credential, known, emit)
		}
		if driver, ok := r.serverDrivers[def.Protocol].(WatchDriver); ok {
			return driver.Watch(ctx, id, def, credential, emit)
		}
		return sourcestream.ErrUnavailable
	}
	if def.Protocol != sourcestream.MQTT {
		if driver, ok := r.drivers[def.Protocol].(WatchDriver); ok {
			return driver.Watch(ctx, id, def, credential, emit)
		}
		return sourcestream.ErrUnavailable
	}
	if def.MQTT != nil && len(def.MQTT.Subscriptions) > 0 {
		copyOptions := *def.MQTT
		def.MQTT = &copyOptions
		def.MQTT.TopicFilters = nil
		for filter := range def.MQTT.Subscriptions {
			def.MQTT.TopicFilters = append(def.MQTT.TopicFilters, filter)
		}
	}
	if def.MQTT == nil || len(def.MQTT.TopicFilters) == 0 || emit == nil {
		return ErrProtocol
	}
	current := make(map[string]bool, len(def.MQTT.TopicFilters))
	refs := make([]string, 0, len(def.MQTT.TopicFilters))
	for _, filter := range def.MQTT.TopicFilters {
		current[filter] = true
		refs = append(refs, "mqtt:"+filter)
	}
	scope := sourcestream.StreamDefinition{Model: sourcestream.Publish, NativeResourceRefs: refs}
	failed := make(chan error, 1)
	var halted atomic.Bool
	fail := func(err error) {
		halted.Store(true)
		sourcestream.ReportWatchReady(ctx, false)
		select {
		case failed <- err:
		default:
		}
	}
	handler := func(_ mqtt.Client, message mqtt.Message) {
		if ctx.Err() != nil || halted.Load() {
			return
		}
		if !scope.MatchesResource("mqtt:" + message.Topic()) {
			// Obsolete queued deliveries must not fill the inflight window or
			// enter UNS. Current-scope deliveries still require durable acceptance.
			message.Ack()
			return
		}
		if len(message.Payload()) > 1024*1024 {
			fail(ErrProtocol)
			return
		}
		var data any
		if json.Unmarshal(message.Payload(), &data) != nil {
			data = string(message.Payload())
		}
		receivedAt := time.Now().UnixMilli()
		if def.MQTT.PacketEnvelope {
			data = map[string]any{"topic": message.Topic(), "payload": data, "payloadBase64": base64.StdEncoding.EncodeToString(message.Payload()), "qos": message.Qos(), "retain": message.Retained(), "duplicate": message.Duplicate(), "receivedAt": receivedAt}
		}
		if err := emit(Input{ResourceRef: "mqtt:" + message.Topic(), Data: data, ReceivedAt: receivedAt}); err != nil {
			fail(err)
			return
		}
		message.Ack()
	}
	client, err := connectMQTT(ctx, id, def, credential, true, func(_ mqtt.Client, _ error) { fail(ErrProtocol) }, handler)
	if err != nil {
		return err
	}
	defer client.Disconnect(100)
	defer sourcestream.ReportWatchReady(ctx, false)
	stale := []string{}
	seen := map[string]bool{}
	for _, filter := range known {
		if filter != "" && !current[filter] && !seen[filter] {
			stale = append(stale, filter)
			seen[filter] = true
		}
	}
	if len(stale) > 0 {
		if err = waitMQTT(ctx, client.Unsubscribe(stale...)); err != nil {
			return err
		}
	}
	for _, filter := range def.MQTT.TopicFilters {
		// A single default callback also avoids duplicate emits for overlapping
		// filters; no callback registration gap exists after Connect.
		qos := byte(def.MQTT.QoS)
		if value, exists := def.MQTT.Subscriptions[filter]; exists {
			qos = value
		}
		token := client.Subscribe(filter, qos, nil)
		err = waitMQTT(ctx, token)
		if err != nil {
			return err
		}
		// Paho completes a SUBACK token without setting Error even when the
		// Broker returns 0x80. A rejected/downgraded subscription is not ready.
		result, ok := token.(*mqtt.SubscribeToken)
		if !ok {
			return ErrProtocol
		}
		granted, exists := result.Result()[filter]
		if !exists || granted != qos {
			return ErrProtocol
		}
	}
	if halted.Load() {
		// A persistent session may deliver pending input during SUBSCRIBE.
		// Preserve its durable-ingress error, rather than relabeling it as
		// a handshake failure when refusing readiness.
		select {
		case err := <-failed:
			return err
		default:
			return ErrProtocol
		}
	}
	if !client.IsConnectionOpen() {
		return ErrProtocol
	}
	sourcestream.ReportWatchReady(ctx, true)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-failed:
		return err
	}
}
func mqttResource(topic string, observed bool) sourcestream.NativeResource {
	return sourcestream.NativeResource{ID: "mqtt:" + topic, Name: topic, Models: []sourcestream.AccessModel{sourcestream.Publish, sourcestream.Subscribe}, Metadata: map[string]any{"topic": topic, "observed": observed}}
}
func (r *Runtime) Invoke(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, ref string, input any) (any, error) {
	if r.isClosed() {
		return nil, sourcestream.ErrUnavailable
	}
	if def.ConnectionRole() == sourcestream.ServerRole {
		if driver := r.serverDrivers[def.Protocol]; driver != nil {
			return driver.Invoke(ctx, id, def, credential, ref, input)
		}
		return nil, sourcestream.ErrUnavailable
	}
	switch def.Protocol {
	case sourcestream.MQTT:
		return r.withMQTT(ctx, id, def, credential, func(ctx context.Context, client mqtt.Client) (any, error) {
			return invokeMQTTClient(ctx, client, def, ref, input)
		})
	default:
		if driver := r.drivers[def.Protocol]; driver != nil {
			return driver.Invoke(ctx, id, def, credential, ref, input)
		}
		return nil, sourcestream.ErrUnavailable
	}
}
func invokeMQTT(ctx context.Context, id int64, def sourcestream.RuntimeSource, credential sourcestream.Credential, ref string, input any) (any, error) {
	client, err := mqttConnection(ctx, id, def, credential)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(100)
	return invokeMQTTClient(ctx, client, def, ref, input)
}
func invokeMQTTClient(ctx context.Context, client mqtt.Client, def sourcestream.SourceDefinition, ref string, input any) (any, error) {
	var err error
	if ref == "mqtt-request-reply" {
		if def.MQTT.RequestTopic == "" {
			return nil, ErrProtocol
		}
		raw, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		var data map[string]any
		if json.Unmarshal(raw, &data) != nil || data == nil {
			return nil, ErrProtocol
		}
		correlation := uuid.NewString()
		setCorrelation(data, def.MQTT.CorrelationField, correlation)
		replyTopic := strings.ReplaceAll(def.MQTT.ReplyTopic, "{correlationId}", correlation)
		if def.MQTT.ReplyTopicField != "" {
			setCorrelation(data, def.MQTT.ReplyTopicField, replyTopic)
		}
		responses := make(chan any, 1)
		defer func() { client.Unsubscribe(replyTopic).WaitTimeout(time.Second) }()
		if err = waitMQTT(ctx, client.Subscribe(replyTopic, byte(def.MQTT.QoS), func(_ mqtt.Client, msg mqtt.Message) {
			if len(msg.Payload()) > 1024*1024 {
				return
			}
			var response map[string]any
			if json.Unmarshal(msg.Payload(), &response) == nil && getCorrelation(response, def.MQTT.CorrelationField) == correlation {
				select {
				case responses <- response:
				default:
				}
			}
		})); err != nil {
			return nil, err
		}
		raw, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
		if err = waitMQTT(ctx, client.Publish(def.MQTT.RequestTopic, byte(def.MQTT.QoS), false, raw)); err != nil {
			return nil, sourcestream.ErrUnknownResult
		}
		select {
		case response := <-responses:
			return response, nil
		case <-ctx.Done():
			return nil, sourcestream.ErrUnknownResult
		}
	}
	if !strings.HasPrefix(ref, "mqtt:") || strings.ContainsAny(strings.TrimPrefix(ref, "mqtt:"), "+#") {
		return nil, ErrProtocol
	}
	topic := strings.TrimPrefix(ref, "mqtt:")
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if err = waitMQTT(ctx, client.Publish(topic, byte(def.MQTT.QoS), false, raw)); err != nil {
		return nil, sourcestream.ErrUnknownResult
	}
	return map[string]any{"receipt": "broker-accepted", "deviceExecuted": false, "topic": topic}, nil
}
func setCorrelation(data map[string]any, field, value string) {
	parts := strings.Split(field, ".")
	for _, part := range parts[:len(parts)-1] {
		child, ok := data[part].(map[string]any)
		if !ok {
			child = map[string]any{}
			data[part] = child
		}
		data = child
	}
	data[parts[len(parts)-1]] = value
}
func getCorrelation(data map[string]any, field string) string {
	parts := strings.Split(field, ".")
	for _, part := range parts[:len(parts)-1] {
		child, ok := data[part].(map[string]any)
		if !ok {
			return ""
		}
		data = child
	}
	result, _ := data[parts[len(parts)-1]].(string)
	return result
}
