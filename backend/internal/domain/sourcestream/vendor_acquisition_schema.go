package sourcestream

// These schemas let installation-time agents discover the object-array
// declarations used by the bounded adapters. Execution validates them again.
func vendorAcquisitionParameters(sourceType string) map[string]any {
	stringProperty := map[string]any{"type": "string"}
	switch sourceType {
	case "teamcenter":
		properties := map[string]any{}
		fields := []string{"id", "queryUid", "itemId", "itemUid", "revisionUid", "datasetUid", "fileUid"}
		for _, field := range fields {
			properties[field] = stringProperty
		}
		return map[string]any{"fileSelections": map[string]any{"type": "array", "minItems": 1, "maxItems": 32,
			"items": map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": fields}}}
	case "salesforce":
		properties := map[string]any{"id": stringProperty, "object": stringProperty,
			"fields": map[string]any{"type": "array", "minItems": 2, "maxItems": 100, "uniqueItems": true, "items": stringProperty},
			"start":  map[string]any{"type": "string", "format": "date-time"}, "end": map[string]any{"type": "string", "format": "date-time"},
			"queryAll": map[string]any{"type": "boolean"}, "cdc": map[string]any{"type": "boolean"}}
		return map[string]any{
			"salesforceQueries": map[string]any{"type": "array", "minItems": 1, "maxItems": 32,
				"items": map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": []string{"id", "object", "fields", "start", "end"}}},
			"salesforcePubSubEndpoint": stringProperty, "salesforceTenantId": stringProperty,
		}
	}
	return nil
}
