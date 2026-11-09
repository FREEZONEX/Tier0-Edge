package mcpserver

import "strings"

func sensitivePath(path string) bool {
	for _, segment := range strings.Split(path, ".") {
		if sensitive(segment) {
			return true
		}
	}
	return false
}

// A schema's enum/default/description can contain the credential itself. Drop
// protected properties in full, including nested object/array declarations.
func redactFieldSchema(value any) any {
	node, ok := value.(map[string]any)
	if !ok {
		return value
	}
	out := map[string]any{}
	for key, child := range node {
		switch key {
		case "properties":
			properties := map[string]any{}
			for name, schema := range object(child) {
				if !sensitive(name) {
					properties[name] = redactFieldSchema(schema)
				}
			}
			out[key] = properties
		case "required":
			names := []any{}
			for _, name := range array(child) {
				if !sensitive(stringValue(name)) {
					names = append(names, name)
				}
			}
			out[key] = names
		case "items", "additionalProperties":
			out[key] = redactFieldSchema(child)
		default:
			out[key] = redactData(child)
		}
	}
	return out
}

// Handles both the flat discovery response and legacy Collection/Stream data.
func redactMetadataSchemas(value any) any {
	switch node := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range node {
			switch key {
			case "recordSchema", "sampleSchema", "parametersSchema":
				out[key] = redactFieldSchema(child)
			case "fields", "relationships":
				visible := []any{}
				for _, item := range array(child) {
					field := object(item)
					if sensitivePath(stringValue(field["path"])) || sensitivePath(stringValue(field["sourceField"])) || sensitivePath(stringValue(field["targetField"])) {
						continue
					}
					visible = append(visible, redactMetadataSchemas(item))
				}
				out[key] = visible
			default:
				out[key] = redactMetadataSchemas(child)
			}
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, item := range node {
			out[i] = redactMetadataSchemas(item)
		}
		return out
	default:
		return value
	}
}
