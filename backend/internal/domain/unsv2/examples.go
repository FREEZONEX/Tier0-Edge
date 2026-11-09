package unsv2

// Cross-kind request examples live beside their shared contract, not in MCP.
func LinkedExample() map[string]any {
	ref := func(id string) any { return map[string]any{"ref": id} }
	record := map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}, "unit": map[string]any{"type": "string"}}, "required": []string{"name"}, "additionalProperties": false}
	sample := map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "number"}}, "required": []string{"value"}, "additionalProperties": false}
	meta := func(op, kind, id string, data any) any {
		return map[string]any{"opId": op, "action": "meta.create", "kind": kind, "id": id, "expectedRevision": 0, "data": data}
	}
	items := []any{
		meta("collection", "collection", "example-measurements", map[string]any{"schemaVersion": 1, "name": "Example measurements", "recordSchema": record, "sampleSchema": sample}),
		meta("factory", "directory", "example-factory", map[string]any{"schemaVersion": 1, "name": "Example factory", "parentId": nil}),
		meta("workshop", "directory", "example-workshop", map[string]any{"schemaVersion": 1, "name": "Example workshop", "parentId": ref("factory")}),
		meta("topic", "topic", "example-topic", map[string]any{"schemaVersion": 1, "name": "Example measurements list", "parentId": ref("workshop"), "viewMode": "records", "collectionId": ref("collection")}),
		meta("source", "source", "example-source", map[string]any{"schemaVersion": 1, "name": "Example MQTT source", "adapter": "mqtt", "config": map[string]any{"connection": map[string]any{"endpoint": "mqtt://example.local:1883", "mqtt": map[string]any{"clientId": "example-client"}}}}),
		meta("stream", "stream", "example-transform", map[string]any{"schemaVersion": 1, "name": "Example temperature transform", "executionMode": "transform", "draft": map[string]any{"supportedProtocols": []string{"mqtt", "http-api"}, "parametersSchema": map[string]any{"type": "object", "properties": map[string]any{"scale": map[string]any{"type": "number"}}, "required": []string{"scale"}, "additionalProperties": false}, "processor": map[string]any{"language": "javascript", "code": "return {record:{name:'Temperature',unit:'C'},sample:{value:Number(input.rawTemp)*params.scale},time:input.time};", "timeoutMs": 1000}, "output": map[string]any{"recordSchema": record, "sampleSchema": sample}}}),
		map[string]any{"opId": "release", "action": "stream.publish", "streamId": ref("stream"), "expectedRevision": 1},
		meta("app", "app", "example-dashboard", map[string]any{"schemaVersion": 1, "name": "Example dashboard", "collections": []any{map[string]any{"alias": "measurements", "collectionId": ref("collection"), "permissions": []string{"record:read", "sample:read"}}}, "navigation": map[string]any{"topicIds": []any{ref("topic")}}}),
		map[string]any{"opId": "point", "action": "record.create", "collectionId": ref("collection"), "id": "point-1", "expectedRevision": 0, "data": map[string]any{"name": "Temperature", "unit": "C"}},
		map[string]any{"opId": "sample", "action": "sample.append", "collectionId": ref("collection"), "recordId": ref("point"), "id": "sample-1", "time": 1735689600000, "quality": "Good", "data": map[string]any{"value": 82.6}},
	}
	return map[string]any{"idempotencyKey": "example-preview-key", "mode": "auto", "dryRun": true, "items": items}
}
