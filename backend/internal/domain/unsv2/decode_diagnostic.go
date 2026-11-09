package unsv2

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// Decoder errors omit map keys and array indices. Locate an unambiguous field
// without echoing its value; ambiguous occurrences stay at the request root.
func diagnosticFieldPath(raw []byte, field string) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	paths := []string{}
	var visit func(any, string)
	visit = func(value any, path string) {
		switch v := value.(type) {
		case map[string]any:
			keys := []string{}
			for key := range v {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				p := path + "/" + jsonPointerPart(key)
				if key == field {
					paths = append(paths, p)
				}
				visit(v[key], p)
			}
		case []any:
			for index, item := range v {
				visit(item, path+"/"+strconv.Itoa(index))
			}
		}
	}
	visit(value, "")
	if len(paths) == 1 {
		return paths[0]
	}
	return ""
}
func jsonPointerPart(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
func decodeChangeData(raw []byte, target any) error {
	err := Decode(raw, target)
	var fault *Fault
	if errors.As(err, &fault) {
		fault.Path = "data/" + strings.TrimPrefix(fault.Path, "/")
	}
	return err
}
