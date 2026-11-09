package unsmeta

import (
	"sort"
	"strconv"
	"strings"
)

// Explain is diagnostic only: Accepts remains the authoritative validator.
// It returns a stable path and rule, never the rejected value.
func (s Schema) Explain(value any) (string, string) {
	if s.Accepts(value) {
		return "", ""
	}
	if object, ok := value.(map[string]any); ok && s.Type == "object" {
		for _, key := range s.Required {
			if _, exists := object[key]; !exists {
				return "/" + pointerPart(key), "Required field is missing"
			}
		}
		keys := []string{}
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if property, found := s.Properties[key]; found {
				if path, reason := property.Explain(object[key]); reason != "" {
					return "/" + pointerPart(key) + path, reason
				}
			} else if s.AdditionalProperties != nil && !*s.AdditionalProperties {
				return "/" + pointerPart(key), "Field is not declared and additionalProperties=false"
			}
		}
	}
	if array, ok := value.([]any); ok && s.Type == "array" && s.Items != nil {
		for index, v := range array {
			if path, reason := s.Items.Explain(v); reason != "" {
				return "/" + strconv.Itoa(index) + path, reason
			}
		}
	}
	return "", "Expected " + s.Type + " conforming to the declared enum and numeric bounds"
}
func pointerPart(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
