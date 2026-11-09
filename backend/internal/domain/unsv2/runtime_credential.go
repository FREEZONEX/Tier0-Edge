package unsv2

import (
	"backend/internal/domain/sourcestream"
	"encoding/json"
	"strconv"
)

// Public asset IDs are exact decimal strings; only the internal encrypted
// credential record uses int64. Never round IDs through a float64 map.
func decodeRuntimeCredential(raw json.RawMessage, target *sourcestream.Credential) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, key := range []string{"certificateFileId", "privateKeyFileId", "caFileId"} {
		value, ok := fields[key]
		if !ok {
			continue
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			return invalid("credentials."+key, "decimal string required")
		}
		id, err := strconv.ParseInt(text, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != text {
			return invalid("credentials."+key, "positive decimal string required")
		}
		fields[key] = json.RawMessage(text)
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return Decode(encoded, target)
}
