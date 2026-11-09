package unsv2

import (
	"backend/internal/contextx"
	"bytes"
	"context"
	"encoding/json"
)

// RFC 7396 semantics inside the existing batch transaction: objects merge,
// arrays/scalars replace, null removes. Full validators still own all rules.
func (s *Service) mergePatch(ctx context.Context, c Change) (Change, error) {
	row, err := meta(ctx, c.Kind, c.ID.ID, false)
	if err != nil {
		return c, err
	}
	if row.Revision != c.ExpectedRevision {
		return c, problem("VERSION_CONFLICT", "object revision changed")
	}
	var current, patch map[string]any
	decodeObject := func(raw []byte, target *map[string]any) error {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		return decoder.Decode(target)
	}
	if decodeObject(row.Data, &current) != nil || current["schemaVersion"] != json.Number("1") {
		return c, invalid("id", "patch requires a versioned configuration object")
	}
	if decodeObject(c.Data, &patch) != nil || patch == nil {
		return c, invalid("data", "patch must be a JSON object")
	}
	model, _ := Model(c.Kind)
	for _, key := range model.ReadOnlyFields {
		if _, ok := patch[key]; ok {
			return c, invalid("data/"+key, "read-only field cannot be patched")
		}
	}
	if actor, ok := contextx.SubjectFrom(ctx); ok && actor.MCPRestricted && c.Kind == "app" {
		for _, key := range []string{"members", "roles"} {
			if _, exists := patch[key]; exists {
				return c, problem("FORBIDDEN", "MCP does not manage App members or roles")
			}
		}
	}
	mergeObject(current, patch)
	for _, key := range model.ReadOnlyFields {
		delete(current, key)
	}
	c.Data, _ = json.Marshal(current)
	c.Action = "meta.replace"
	return c, nil
}

func mergeObject(target, patch map[string]any) {
	for key, value := range patch {
		if value == nil {
			delete(target, key)
			continue
		}
		if object, ok := value.(map[string]any); ok {
			nested, _ := target[key].(map[string]any)
			if nested == nil {
				nested = map[string]any{}
			}
			mergeObject(nested, object)
			target[key] = nested
		} else {
			target[key] = value
		}
	}
}

func RepairHint(code string) string {
	switch code {
	case "VERSION_CONFLICT":
		return "Query the current object revision, rebase the intended field changes, then preview with a new idempotency key"
	case "DUPLICATE":
		return "Query existing identities/names; select a new identity or patch the intended existing object"
	case "DUPLICATE_KEY":
		return "Choose a non-conflicting key or resolve retained duplicates before changing Collection constraints; data is never removed automatically"
	case "CONDITION_FAILED":
		return "Read the authorized current data and reconsider the whole transaction before retrying"
	case "REFERENCED":
		return "Query explicit references; update authorized bindings before retrying; retained data is not silently deleted"
	case "SCHEMA_MISMATCH":
		return "Compare the payload with Collection recordSchema/sampleSchema and Stream output; retained data may block schema changes"
	case "NOT_FOUND":
		return "Query the explicit kind and identity; create dependencies before consumers or use a creation opId reference"
	case "CYCLIC_DEPENDENCY":
		return "Remove parent/dependency cycles and check creation order and opId reference kinds"
	case "FORBIDDEN":
		return "Inspect current permissions, OAuth scopes and the advertised operation boundary; do not retry by changing appId"
	case "IDEMPOTENCY_CONFLICT":
		return "Keep the exact original payload for retries; use a new key for a changed request"
	case "TEMPORARILY_UNAVAILABLE":
		return "Retry the identical request with the same idempotency key"
	}
	return "Inspect the current contract, correct the indicated object/path and preview before submitting"
}
