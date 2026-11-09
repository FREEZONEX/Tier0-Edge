package unsmeta

import (
	"backend/internal/domain/common"
	"strings"
)

// CollectionSemantics is descriptive metadata, never an instruction to a caller.
// Relationships do not enforce foreign keys or cardinality constraints.
type CollectionSemantics struct {
	RecordGrain   string                   `json:"recordGrain,omitempty"`
	SampleGrain   string                   `json:"sampleGrain,omitempty"`
	Fields        []FieldSemantics         `json:"fields,omitempty"`
	Relationships []CollectionRelationship `json:"relationships,omitempty"`
}
type FieldSemantics struct {
	Kind            string `json:"kind"`
	Path            string `json:"path"`
	AggregationNote string `json:"aggregationNote"`
}
type CollectionRelationship struct {
	Name               string `json:"name"`
	SourceField        string `json:"sourceField"`
	TargetCollectionID string `json:"targetCollectionId"`
	TargetField        string `json:"targetField"`
	Cardinality        string `json:"cardinality"`
	Description        string `json:"description,omitempty"`
}

// SemanticField resolves only declared paths, never arbitrary JSON properties.
func SemanticField(schema *Schema, path string) (Schema, bool) {
	if path == "_id" {
		return Schema{Type: "string"}, true
	}
	if schema == nil || strings.HasPrefix(path, "_meta.") || !validFieldPath(path) {
		return Schema{}, false
	}
	parts, _ := ParseFieldPath(path)
	current, ok := SchemaField(schema, parts)
	if !ok {
		return Schema{}, false
	}
	return *current, true
}
func (d Document) ValidateSemantics() error {
	s := d.Semantics
	if s == nil {
		return nil
	}
	if common.ExceedDescLimit(s.RecordGrain) || common.ExceedDescLimit(s.SampleGrain) || len(s.Fields) > 128 || len(s.Relationships) > 32 || s.SampleGrain != "" && d.SampleSchema == nil {
		return ErrSchema
	}
	seen := map[string]bool{}
	for _, f := range s.Fields {
		schema := d.RecordSchema
		if f.Kind == "sample" {
			schema = d.SampleSchema
		} else if f.Kind != "record" {
			return ErrSchema
		}
		if schema == nil || strings.HasPrefix(f.Path, "_meta.") || seen[f.Kind+":"+f.Path] || strings.TrimSpace(f.AggregationNote) == "" || common.ExceedDescLimit(f.AggregationNote) {
			return ErrSchema
		}
		if _, ok := SemanticField(schema, f.Path); !ok {
			return ErrSchema
		}
		seen[f.Kind+":"+f.Path] = true
	}
	seen = map[string]bool{}
	for _, r := range s.Relationships {
		if !ValidID(r.Name) || common.ExceedNameLimit(r.Name) || seen[r.Name] || !ValidID(r.TargetCollectionID) || common.ExceedDescLimit(r.Description) {
			return ErrSchema
		}
		seen[r.Name] = true
		switch r.Cardinality {
		case "oneToOne", "manyToOne", "oneToMany", "manyToMany":
		default:
			return ErrSchema
		}
		field, ok := SemanticField(d.RecordSchema, r.SourceField)
		if !ok || field.Type == "object" || field.Type == "array" || field.Type == "null" {
			return ErrSchema
		}
		if r.TargetField != "_id" && (strings.HasPrefix(r.TargetField, "_meta.") || !validFieldPath(r.TargetField)) {
			return ErrSchema
		}
	}
	return nil
}
