package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"backend/internal/repo"
)

var ErrPreviewNoSample = errors.New("device preview ended without a sample")
var ErrPreviewWatch = errors.New("device preview watch failed")
var ErrPreviewNodeRead = errors.New("device node read failed")
var ErrDeviceConnect = errors.New("device connection failed")
var ErrDeviceSession = errors.New("device session creation failed")

func declaredDeviceResources(def SourceDefinition, revision int64) []edgeResource {
	refs := map[string]string{}
	if def.Protocol == MQTT && def.MQTT != nil {
		for _, topic := range def.MQTT.TopicFilters {
			refs["mqtt:"+topic] = topic
		}
		for topic := range def.MQTT.Subscriptions {
			refs["mqtt:"+topic] = topic
		}
	}
	if def.Protocol == OPCUA && def.OPCUA != nil {
		for _, node := range def.OPCUA.NodeIDs {
			refs["opcua:"+node] = node
		}
	}
	keys := make([]string, 0, len(refs))
	for ref := range refs {
		keys = append(keys, ref)
	}
	sort.Strings(keys)
	out := make([]edgeResource, 0, len(keys))
	fields := map[string]any{
		"nodeId":          map[string]string{"type": "string"},
		"value":           map[string]any{},
		"statusCode":      map[string]string{"type": "string"},
		"good":            map[string]string{"type": "boolean"},
		"sourceTimestamp": map[string]string{"format": "date-time"},
		"serverTimestamp": map[string]string{"format": "date-time"},
	}
	if def.Protocol == MQTT {
		fields = map[string]any{
			"topic":         map[string]string{"type": "string"},
			"payload":       map[string]any{},
			"payloadBase64": map[string]string{"type": "string", "contentEncoding": "base64"},
			"receivedAt":    map[string]string{"type": "integer"},
			"qos":           map[string]string{"type": "integer"},
			"retain":        map[string]string{"type": "boolean"},
			"duplicate":     map[string]string{"type": "boolean"},
		}
	}
	itemSchema := map[string]any{"type": "object", "properties": fields}
	for _, ref := range keys {
		metadata := map[string]any{}
		if def.Protocol == MQTT {
			metadata["qos"] = def.MQTT.QoS
			if qos, ok := def.MQTT.Subscriptions[refs[ref]]; ok {
				metadata["qos"] = int(qos)
			}
		}
		if def.Protocol == OPCUA {
			metadata["samplingIntervalMs"] = def.OPCUA.SamplingIntervalMs
		}
		out = append(out, edgeResource{
			Metadata:    metadata,
			ResourceRef: ref, Name: refs[ref], Modes: []string{"read", "receive"}, Permission: "uns.source", ConfigRevision: revision,
			InputSchema:  resourceObject(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 1}}),
			OutputSchema: resourceObject(map[string]any{"items": map[string]any{"type": "array", "maxItems": 1, "items": itemSchema}, "returned": map[string]string{"type": "integer"}, "truncated": map[string]string{"type": "boolean"}}),
		})
	}
	return out
}

func (s *Service) edgeDevicePreview(ctx context.Context, row repo.Source, def SourceDefinition, ref string) (any, error) {
	ctx = context.WithValue(ctx, watchConfigRevisionKey{}, row.ConfigRevision)
	declared := false
	for _, item := range declaredDeviceResources(def, row.ConfigRevision) {
		if item.ResourceRef == ref {
			declared = true
			break
		}
	}
	if !declared {
		return nil, invalid("resourceRef", "undeclared")
	}
	if s.runtime == nil {
		return nil, ErrUnavailable
	}
	connection, credential, rev, err := s.connection(ctx, row.ID, nil)
	if err != nil {
		return nil, err
	}
	if rev != row.Revision {
		return nil, repo.ErrConflict
	}
	latest, err := repo.NewSourceStreamRepo(ctx).Source(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	if latest.ConfigRevision != row.ConfigRevision {
		return nil, repo.ErrConflict
	}
	if def.Protocol == OPCUA {
		value, err := s.runtime.Invoke(ctx, row.ID, connection, credential, ref, map[string]any{"operation": "read"})
		if err != nil {
			return nil, errors.Join(ErrPreviewNodeRead, err, ctx.Err())
		}
		raw, err := json.Marshal(value)
		if err != nil || len(raw) > 256*1024 {
			return nil, ErrVendorResponse
		}
		var item map[string]any
		if json.Unmarshal(raw, &item) != nil || item == nil {
			return nil, ErrVendorResponse
		}
		item["nodeId"] = strings.TrimPrefix(ref, "opcua:")
		return item, nil
	}
	watcher, ok := s.runtime.(interface {
		Watch(context.Context, int64, RuntimeSource, Credential, func(SourceInput) error) error
	})
	if !ok {
		return nil, ErrUnavailable
	}
	topic := strings.TrimPrefix(ref, "mqtt:")
	connection.MQTT.TopicFilters = []string{topic}
	if qos, ok := def.MQTT.Subscriptions[topic]; ok {
		connection.MQTT.QoS = int(qos)
	}
	connection.MQTT.Subscriptions = nil
	connection.MQTT.Ephemeral = true
	connection.MQTT.PacketEnvelope = true
	sample := make(chan any, 1)
	preview, cancel := context.WithCancel(ctx)
	defer cancel()
	err = watcher.Watch(preview, row.ID, connection, credential, func(input SourceInput) error {
		select {
		case sample <- input.Data:
			cancel()
		default:
		}
		return nil
	})
	select {
	case value := <-sample:
		raw, e := json.Marshal(value)
		if e != nil || len(raw) > 256*1024 {
			return nil, ErrVendorResponse
		}
		return value, nil
	default:
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, errors.Join(ErrPreviewNoSample, err)
		}
		return nil, errors.Join(ErrPreviewWatch, err)
	}
	return nil, ErrPreviewNoSample
}
