package unsv2

import (
	"encoding/json"
	"strconv"
	"strings"

	"backend/internal/domain/unsmeta"
)

func bindingPath(path string) ([]string, bool) {
	parts, err := unsmeta.ParseFieldPath(path)
	if err != nil || len(parts) < 2 || parts[0] != "data" {
		return nil, false
	}
	return parts[1:], true
}

func bindingIndex(path, kind string, size int) (int, bool) {
	parts := strings.Split(path, ".")
	if len(parts) != 3 || parts[0] != kind || parts[2] != "value" {
		return 0, false
	}
	i, err := strconv.Atoi(parts[1])
	return i, err == nil && i >= 0 && i < size && strconv.Itoa(i) == parts[1]
}

func validateBindings(c Change) error {
	seen := map[string]bool{}
	for _, b := range c.Bindings {
		if !unsmeta.ValidID(b.Ref) || seen[b.Path] {
			return invalid("bindings", "valid refs and distinct target paths required")
		}
		seen[b.Path] = true
		_, data := bindingPath(b.Path)
		_, operation := bindingIndex(b.Path, "operations", len(c.Operations))
		_, condition := bindingIndex(b.Path, "conditions", len(c.Conditions))
		filter := strings.HasPrefix(b.Path, "filter/")
		if filter {
			if _, err := bindFilterValue(c.Filter, b.Path, nil, false); err != nil {
				return err
			}
		}
		target := b.Path == "id" && contains([]string{"record.update", "record.modify", "record.replace", "record.delete", "record.read", "record.check"}, c.Action)
		if target && (c.ID.ID != "" || c.ID.Ref != "") {
			return invalid("id", "omit id when a binding supplies the target")
		}
		if !(target || filter && contains([]string{"record.check", "record.read", "record.update", "record.modify", "record.replace", "record.delete"}, c.Action) || data && contains([]string{"record.create", "record.replace", "sample.append"}, c.Action) || operation && (c.Action == "record.modify" || c.Action == "record.update") || condition && (c.Action == "record.check" || (c.Action == "record.modify" || c.Action == "record.update"))) {
			return invalid("bindings/path", "target must be a data field, field-operation value or existing filter scalar")
		}
		_, resultData := bindingPath(b.ResultPath)
		_, queryField, query := queryResultPath(b.ResultPath)
		if target && (!query || queryField != "id") {
			return invalid("bindings/resultPath", "dynamic target requires query.records.N.id")
		}
		if !query && b.ResultPath != "query.total" && !resultData && !contains([]string{"id", "revision", "collectionId"}, b.ResultPath) {
			return invalid("bindings/resultPath", "result identity, revision or record.read data field required")
		}
	}
	return nil
}

// Read/query data is available only after its Collection permission check.
// Bindings never resolve arbitrary database objects or permission fields.
func resolveBindings(c Change, results map[string]Result) (Change, error) {
	if len(c.Bindings) == 0 {
		return c, nil
	}
	c.Operations = append([]FieldOperation(nil), c.Operations...)
	c.Conditions = append([]RecordFilter(nil), c.Conditions...)
	for i, binding := range c.Bindings {
		fail := func(message string) (Change, error) { return c, invalid("bindings/"+strconv.Itoa(i), message) }
		result, ok := results[binding.Ref]
		if !ok {
			return fail("result dependency has not completed")
		}
		var value any
		path := binding.ResultPath
		source := result
		if n, field, query := queryResultPath(path); query {
			if result.Action != "record.query" || result.Query == nil || n >= len(result.Query.Records) {
				return fail("query result index is absent; assert count or select an existing result")
			}
			row := result.Query.Records[n]
			source = Result{Action: "record.read", ID: row.ID, CollectionID: row.CollectionID, Revision: row.Revision, Data: row.Data}
			path = field
		}
		switch path {
		case "query.total":
			if result.Query == nil || result.Action != "record.query" {
				return fail("query.total requires a completed query")
			}
			value = result.Query.Total
		case "id":
			value = source.ID
		case "collectionId":
			if source.CollectionID == "" {
				return fail("result has no Collection identity")
			}
			value = source.CollectionID
		case "revision":
			if source.Revision <= 0 {
				return fail("result has no revision")
			}
			value = source.Revision
		default:
			if source.Action != "record.read" || decodeNumberJSON(source.Data, &value) != nil {
				return fail("data binding requires a completed record.read")
			}
			parts, _ := bindingPath(path)
			for _, part := range parts {
				object, ok := value.(map[string]any)
				if !ok {
					return fail("result field is not an object")
				}
				value, ok = object[part]
				if !ok {
					return fail("result field does not exist")
				}
			}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return c, err
		}
		if binding.Path == "id" {
			c.ID, err = validateDynamicTarget(c, result, path, value)
			if err != nil {
				return c, err
			}
		} else if n, ok := bindingIndex(binding.Path, "operations", len(c.Operations)); ok {
			c.Operations[n].Value = raw
		} else if n, ok := bindingIndex(binding.Path, "conditions", len(c.Conditions)); ok {
			c.Conditions[n].Value = raw
		} else if strings.HasPrefix(binding.Path, "filter/") {
			c.Filter, err = bindFilterValue(c.Filter, binding.Path, value, true)
			if err != nil {
				return c, err
			}
		} else {
			parts, _ := bindingPath(binding.Path)
			var data map[string]any
			if decodeNumberJSON(c.Data, &data) != nil || data == nil {
				return fail("data binding target must be an object")
			}
			parent := data
			for _, part := range parts[:len(parts)-1] {
				next, ok := parent[part].(map[string]any)
				if !ok {
					return fail("data binding parent must be an existing object")
				}
				parent = next
			}
			// Decode a fresh value for each binding: callers cannot alias or mutate
			// the immutable snapshot returned by a previous read operation.
			if err := decodeNumberJSON(raw, &value); err != nil {
				return c, err
			}
			parent[parts[len(parts)-1]] = value
			c.Data, err = json.Marshal(data)
			if err != nil {
				return c, err
			}
		}
	}
	return c, nil
}

// Mongo field names contain dots, so predicate bindings use JSON Pointer
// segments (filter/data.available/$gte), not the data-field dot notation.
// Only existing scalar placeholders can change; no binding can inject an
// operator, new field, or predicate subtree.
func bindFilterValue(raw json.RawMessage, path string, value any, write bool) (json.RawMessage, error) {
	fail := func() (json.RawMessage, error) {
		return nil, invalid("bindings/path", "filter binding must address an existing scalar using filter/ JSON Pointer segments")
	}
	var root map[string]any
	if !strings.HasPrefix(path, "filter/") || decodeNumberJSON(raw, &root) != nil || root == nil {
		return fail()
	}
	parts := strings.Split(strings.TrimPrefix(path, "filter/"), "/")
	if len(parts) > 24 {
		return fail()
	}
	var current any = root
	for i, encoded := range parts {
		key := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		if strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1") != encoded {
			return fail()
		}
		var child any
		var set func(any)
		switch container := current.(type) {
		case map[string]any:
			var ok bool
			child, ok = container[key]
			if !ok {
				return fail()
			}
			set = func(v any) { container[key] = v }
		case []any:
			n, err := strconv.Atoi(key)
			if err != nil || n < 0 || n >= len(container) || strconv.Itoa(n) != key {
				return fail()
			}
			child = container[n]
			set = func(v any) { container[n] = v }
		default:
			return fail()
		}
		if i == len(parts)-1 {
			switch child.(type) {
			case map[string]any, []any:
				return fail()
			}
			if write {
				switch value.(type) {
				case map[string]any, []any:
					return fail()
				}
				set(value)
			}
		}
		current = child
	}
	return json.Marshal(root)
}
