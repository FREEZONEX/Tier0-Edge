package unsv2

import (
	"encoding/json"
	"strings"
)

// MatchResourcePath treats action segments (records:find) as literals and only
// whole :parameter segments as identities. URL.Path is already decoded by net/http.
func MatchResourcePath(pattern, path string) (map[string]string, bool) {
	want, got := strings.Split(pattern, "/"), strings.Split(path, "/")
	if len(want) != len(got) {
		return nil, false
	}
	params := map[string]string{}
	for i, part := range want {
		if strings.HasPrefix(part, ":") {
			if got[i] == "" {
				return nil, false
			}
			params[part[1:]] = got[i]
		} else if part != got[i] {
			return nil, false
		}
	}
	return params, true
}

// BindResourcePath never lets a request body override the Collection/Record URL.
func BindResourcePath(raw []byte, params map[string]string) ([]byte, error) {
	if len(params) == 0 {
		return raw, nil
	}
	var body map[string]json.RawMessage
	if err := Decode(raw, &body); err != nil {
		return nil, err
	}
	if body == nil {
		return nil, invalid("body", "JSON object required")
	}
	for key, value := range params {
		if previous, ok := body[key]; ok {
			var supplied string
			if json.Unmarshal(previous, &supplied) != nil || supplied != value {
				return nil, invalid(key, "body identity must match the URL")
			}
		}
		body[key], _ = json.Marshal(value)
	}
	return json.Marshal(body)
}
