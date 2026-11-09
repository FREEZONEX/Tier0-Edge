package unsmeta

import "backend/internal/fieldpath"

func ParseFieldPath(path string) ([]string, error)         { return fieldpath.Parse(path) }
func FormatFieldPath(parts []string) string                { return fieldpath.Format(parts) }
func fieldPathValue(value any, parts []string) (any, bool) { return fieldpath.Value(value, parts) }

// SchemaField resolves object keys and explicit array indices without implicit
// array traversal. Object properties named "0" remain ordinary properties.
func SchemaField(schema *Schema, parts []string) (*Schema, bool) {
	current := schema
	for _, part := range parts {
		if current == nil {
			return nil, false
		}
		switch current.Type {
		case "object":
			next, ok := current.Properties[part]
			if !ok {
				return nil, false
			}
			current = &next
		case "array":
			if _, ok := fieldpath.Index(part); !ok || current.Items == nil {
				return nil, false
			}
			current = current.Items
		default:
			return nil, false
		}
	}
	return current, current != nil
}
