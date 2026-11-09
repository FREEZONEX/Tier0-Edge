package sourceprotocol

import (
	"context"
	"sort"
	"strings"

	"backend/internal/domain/sourcestream"
)

// One platform Broker, isolated Source namespaces. Internal credentials never
// enter the Source document, discovered resources, script, or device response.
type MQTTServerDriver struct {
	Endpoint   string
	Credential sourcestream.Credential
	runtime    *Runtime
}

func (d MQTTServerDriver) withConnectionPool(pool *sourceConnectionPool) Driver {
	d.runtime = New()
	d.runtime.connections = pool
	return d
}
func (d MQTTServerDriver) managedRuntime() (*Runtime, func()) {
	if d.runtime != nil {
		return d.runtime, func() {}
	}
	runtime := New()
	return runtime, runtime.Close
}
func (d MQTTServerDriver) ReleaseSource(id int64) {
	if d.runtime != nil {
		d.runtime.ReleaseSource(id)
	}
}
func (d MQTTServerDriver) Close() {
	if d.runtime != nil {
		d.runtime.Close()
	}
}

func (d MQTTServerDriver) connection(id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential) (sourcestream.SourceDefinition, error) {
	if id <= 0 || def.Validate() != nil || def.ConnectionRole() != sourcestream.ServerRole || def.MQTT == nil ||
		def.CredentialRef == "" || credential.Kind != "basic" || credential.Username == "" || credential.Secret == "" ||
		d.Endpoint == "" || d.Credential.Kind != "basic" || d.Credential.Username == "" || d.Credential.Secret == "" {
		return def, sourcestream.ErrUnavailable
	}
	prefix := sourcestream.MQTTServerPrefix(id, def.CredentialRef)
	options := *def.MQTT
	options.ClientID += "-" + def.CredentialRef
	options.TopicFilters = nil
	options.ServerSubscribeTopics = nil
	for _, filter := range def.MQTT.TopicFilters {
		options.TopicFilters = append(options.TopicFilters, prefix+filter)
	}
	if options.RequestTopic != "" {
		options.RequestTopic = prefix + options.RequestTopic
		options.ReplyTopic = prefix + options.ReplyTopic
	}
	def.Role, def.Endpoint, def.MQTT = sourcestream.ClientRole, d.Endpoint, &options
	if def.Validate() != nil {
		return def, sourcestream.ErrUnavailable
	}
	return def, nil
}

func (d MQTTServerDriver) Probe(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential) error {
	resolved, err := d.connection(id, def, credential)
	if err != nil {
		return err
	}
	runtime, done := d.managedRuntime()
	defer done()
	return runtime.Probe(ctx, id, resolved, d.Credential)
}

func (d MQTTServerDriver) Discover(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, _ string) ([]sourcestream.NativeResource, error) {
	if err := d.Probe(ctx, id, def, credential); err != nil {
		return nil, err
	}
	prefix := sourcestream.MQTTServerPrefix(id, def.CredentialRef)
	items := map[string]sourcestream.NativeResource{}
	for _, topic := range def.MQTT.TopicFilters {
		item := mqttResource(topic, false)
		item.Models = []sourcestream.AccessModel{sourcestream.Publish}
		item.Metadata["wireTopic"] = prefix + topic
		items[item.ID] = item
	}
	for _, topic := range def.MQTT.ServerSubscribeTopics {
		item := mqttResource(topic, false)
		item.Models = []sourcestream.AccessModel{sourcestream.Subscribe}
		if old, exists := items[item.ID]; exists {
			item.Models = append(old.Models, sourcestream.Subscribe)
		}
		item.Metadata["wireTopic"] = prefix + topic
		items[item.ID] = item
	}
	if def.MQTT.RequestTopic != "" {
		items["mqtt-request-reply"] = sourcestream.NativeResource{
			ID: "mqtt-request-reply", Name: def.MQTT.RequestTopic, Models: []sourcestream.AccessModel{sourcestream.RequestReply},
			Metadata: map[string]any{"requestTopic": prefix + def.MQTT.RequestTopic, "replyTopic": prefix + def.MQTT.ReplyTopic, "replyTopicField": def.MQTT.ReplyTopicField, "correlationField": def.MQTT.CorrelationField},
		}
	}
	result := make([]sourcestream.NativeResource, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (d MQTTServerDriver) Watch(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, emit func(Input) error) error {
	return d.WatchWithKnownSubscriptions(ctx, id, def, credential, nil, emit)
}

func (d MQTTServerDriver) WatchWithKnownSubscriptions(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, known []string, emit func(Input) error) error {
	resolved, err := d.connection(id, def, credential)
	if err != nil || emit == nil {
		if err != nil {
			return err
		}
		return ErrProtocol
	}
	prefix := sourcestream.MQTTServerPrefix(id, def.CredentialRef)
	wireKnown := make([]string, 0, len(known))
	for _, filter := range known {
		wireKnown = append(wireKnown, prefix+filter)
	}
	runtime, done := d.managedRuntime()
	defer done()
	return runtime.WatchWithKnownSubscriptions(ctx, id, resolved, d.Credential, wireKnown, func(input Input) error {
		wire := strings.TrimPrefix(input.ResourceRef, "mqtt:")
		if !strings.HasPrefix(wire, prefix) {
			return ErrProtocol
		}
		input.ResourceRef = "mqtt:" + strings.TrimPrefix(wire, prefix)
		return emit(input)
	})
}

func (d MQTTServerDriver) Invoke(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, ref string, input any) (any, error) {
	resolved, err := d.connection(id, def, credential)
	if err != nil {
		return nil, err
	}
	if ref != "mqtt-request-reply" {
		if !strings.HasPrefix(ref, "mqtt:") {
			return nil, ErrProtocol
		}
		topic := strings.TrimPrefix(ref, "mqtt:")
		allowed := false
		for _, declared := range def.MQTT.ServerSubscribeTopics {
			allowed = allowed || topic == declared
		}
		if !allowed || strings.ContainsAny(topic, "+#") {
			return nil, ErrProtocol
		}
		ref = "mqtt:" + sourcestream.MQTTServerPrefix(id, def.CredentialRef) + topic
	}
	runtime, done := d.managedRuntime()
	defer done()
	return runtime.Invoke(ctx, id, resolved, d.Credential, ref, input)
}
