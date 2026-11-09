package unsv2

import "encoding/json"

// RFC 7396 is shared by inspection and mutation, so the intent token confirms
// exactly the same candidate, including nested removals and exact JSON numbers.
func sourceImpactCandidate(before string, raw json.RawMessage) (any, error) {
	var document, patch map[string]any
	if decodeNumberJSON([]byte(before), &document) != nil || decodeNumberJSON(raw, &patch) != nil || patch == nil {
		return nil, invalid("patch", "object required")
	}
	var merge func(map[string]any, map[string]any) map[string]any
	merge = func(target, patch map[string]any) map[string]any {
		if target == nil {
			target = map[string]any{}
		}
		for key, value := range patch {
			if value == nil {
				delete(target, key)
				continue
			}
			if nested, ok := value.(map[string]any); ok {
				existing, _ := target[key].(map[string]any)
				target[key] = merge(existing, nested)
			} else {
				target[key] = value
			}
		}
		return target
	}
	return merge(document, patch), nil
}
