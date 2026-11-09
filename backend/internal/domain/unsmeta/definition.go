// Package unsmeta manages Namespace, Collection and field references independently
// of Source/Stream execution. Protocol workers can consume the same data service.
package unsmeta

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"backend/internal/domain/common"
	"backend/internal/unsref"
)

var (
	ErrInvalid     = errors.New("UnsMeta.invalid")
	ErrSchema      = errors.New("UnsMeta.invalidSchema")
	ErrReferenced  = errors.New("UnsMeta.referenced")
	ErrCycle       = errors.New("UnsMeta.cycle")
	ErrData        = errors.New("UnsMeta.schemaMismatch")
	ErrSchemaInUse = errors.New("UnsMeta.schemaInUse")
)

// SchemaMismatchError 仅返回相对 data 的 JSON Pointer 和校验规则，不包含记录值。
type SchemaMismatchError struct {
	Path   string
	Reason string
}

func (e *SchemaMismatchError) Error() string        { return "schema mismatch at " + e.Path + ": " + e.Reason }
func (e *SchemaMismatchError) Is(target error) bool { return target == ErrData }

type FieldBinding struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName,omitempty"`
	Kind         string `json:"kind"`
	CollectionID string `json:"collectionId"`
	RecordID     string `json:"recordId"`
	FieldPath    string `json:"fieldPath"`
}

// Schema deliberately supports a bounded, explicit JSON Schema subset. Unknown
// keywords are rejected rather than silently promising unenforced constraints.
type Schema struct {
	Type                 string            `json:"type"`
	Format               string            `json:"format,omitempty"`
	Title                string            `json:"title,omitempty"`
	Description          string            `json:"description,omitempty"`
	Unit                 string            `json:"unit,omitempty"`
	Properties           map[string]Schema `json:"properties,omitempty"`
	Required             []string          `json:"required,omitempty"`
	AdditionalProperties *bool             `json:"additionalProperties,omitempty"`
	Items                *Schema           `json:"items,omitempty"`
	Enum                 []any             `json:"enum,omitempty"`
	Minimum              *float64          `json:"minimum,omitempty"`
	Maximum              *float64          `json:"maximum,omitempty"`
}
type Document struct {
	DirectoryTrashID string               `json:"directoryTrashId,omitempty"`
	PurgeJobID       string               `json:"purgeJobId,omitempty"`
	SchemaVersion    int                  `json:"schemaVersion,omitempty"`
	ViewMode         string               `json:"viewMode,omitempty"`
	NodeType         string               `json:"nodeType,omitempty"`
	Name             string               `json:"name"`
	Description      string               `json:"description,omitempty"`
	ParentID         string               `json:"parentId,omitempty"`
	CollectionID     string               `json:"collectionId,omitempty"`
	Fields           []FieldBinding       `json:"fields,omitempty"`
	RecordSchema     *Schema              `json:"recordSchema,omitempty"`
	SampleSchema     *Schema              `json:"sampleSchema,omitempty"`
	Indexes          []IndexSpec          `json:"indexes,omitempty"`
	Semantics        *CollectionSemantics `json:"semantics,omitempty"`
}

func ValidID(id string) bool { return unsref.ValidMetadataID(id) }
func Parse(input any, target any) error {
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 256*1024 {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return ErrInvalid
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return ErrInvalid
	}
	return nil
}
func (d Document) Validate(kind string) error {
	if kind != "collection" && (len(d.Indexes) != 0 || d.Semantics != nil) {
		return ErrSchema
	}
	if strings.TrimSpace(d.Name) != d.Name || d.Name == "" || common.ExceedNameLimit(d.Name) || common.ExceedDescLimit(d.Description) {
		return ErrInvalid
	}
	if strings.ContainsAny(d.Name, "/\x00") {
		return ErrInvalid
	}
	if d.ParentID != "" && !ValidID(d.ParentID) {
		return ErrInvalid
	}
	switch kind {
	case "folder":
		if d.CollectionID != "" || len(d.Fields) > 0 || d.RecordSchema != nil || d.SampleSchema != nil {
			return ErrInvalid
		}
	case "topic":
		if d.ViewMode != "" && d.ViewMode != "records" && d.ViewMode != "fields" {
			return ErrInvalid
		}
		if d.ViewMode == "records" && (d.CollectionID == "" || len(d.Fields) > 0) {
			return ErrInvalid
		}
		if d.ViewMode == "fields" && (d.CollectionID != "" || len(d.Fields) == 0) {
			return ErrInvalid
		}
		if d.RecordSchema != nil || d.SampleSchema != nil || (d.CollectionID != "" && !ValidID(d.CollectionID)) || len(d.Fields) > 128 {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, f := range d.Fields {
			if !ValidID(f.Name) || seen[f.Name] || common.ExceedNameLimit(f.DisplayName) || !ValidID(f.CollectionID) || !ValidID(f.RecordID) || (f.Kind != "record" && f.Kind != "sample") || !validFieldPath(f.FieldPath) {
				return ErrInvalid
			}
			seen[f.Name] = true
		}
	case "collection":
		for _, schema := range []*Schema{d.RecordSchema, d.SampleSchema} {
			if schema != nil {
				for _, key := range []string{"_id", "_meta"} {
					if _, reserved := schema.Properties[key]; reserved {
						return ErrSchema
					}
				}
			}
		}
		if d.ViewMode != "" || d.CollectionID != "" || len(d.Fields) > 0 || d.RecordSchema == nil || d.RecordSchema.Type != "object" {
			return ErrInvalid
		}
		if err := d.RecordSchema.validate(0); err != nil {
			return err
		}
		if err := ValidateIndexes(d); err != nil {
			return err
		}
		if err := d.ValidateSemantics(); err != nil {
			return err
		}
		if d.SampleSchema != nil {
			if d.SampleSchema.Type != "object" {
				return ErrSchema
			}
			return d.SampleSchema.validate(0)
		}
	default:
		return ErrInvalid
	}
	return nil
}
func validFieldPath(path string) bool {
	_, err := ParseFieldPath(path)
	return err == nil
}

// Batch configuration and data writes use the same bounded Schema validator.
func (s Schema) Accepts(value any) bool { return s.accepts(value) }
func (s Schema) Validate() error        { return s.validate(0) }
func (s Schema) validate(depth int) error {
	if depth > 8 || len(s.Properties) > 128 || len(s.Enum) > 100 || len(s.Title) > 512 || len(s.Description) > 2048 || utf8.RuneCountInString(s.Unit) > 32 {
		return ErrSchema
	}
	switch s.Type {
	case "object", "array", "string", "number", "integer", "boolean", "null":
	default:
		return ErrSchema
	}
	if !validSchemaFormat(s.Type, s.Format) {
		return ErrSchema
	}
	if s.Type != "object" && (len(s.Properties) > 0 || len(s.Required) > 0 || s.AdditionalProperties != nil) {
		return ErrSchema
	}
	if (s.Type == "array") != (s.Items != nil) {
		return ErrSchema
	}
	if (s.Minimum != nil || s.Maximum != nil) && s.Type != "number" && s.Type != "integer" {
		return ErrSchema
	}
	if s.Minimum != nil && s.Maximum != nil && *s.Minimum > *s.Maximum {
		return ErrSchema
	}
	seen := map[string]bool{}
	for _, field := range s.Required {
		if _, ok := s.Properties[field]; !ok || seen[field] {
			return ErrSchema
		}
		seen[field] = true
	}
	for key, property := range s.Properties {
		if !validFieldPath(FormatFieldPath([]string{key})) {
			return ErrSchema
		}
		if err := property.validate(depth + 1); err != nil {
			return err
		}
	}
	if s.Items != nil {
		return s.Items.validate(depth + 1)
	}
	return nil
}
func (s Schema) accepts(value any) bool {
	if len(s.Enum) > 0 {
		matched := false
		for _, option := range s.Enum {
			matched = matched || equalJSONValues(value, option)
		}
		if !matched {
			return false
		}
	}
	switch s.Type {
	case "object":
		object, ok := value.(map[string]any)
		if !ok || object == nil {
			return false
		}
		for _, key := range s.Required {
			if _, ok := object[key]; !ok {
				return false
			}
		}
		for key, value := range object {
			property, found := s.Properties[key]
			if found {
				if !property.accepts(value) {
					return false
				}
			} else if s.AdditionalProperties != nil && !*s.AdditionalProperties {
				return false
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok || s.Items == nil {
			return false
		}
		for _, value := range array {
			if !s.Items.accepts(value) {
				return false
			}
		}
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "number", "integer":
		number, ok := exactNumber(value)
		if !ok || (s.Type == "integer" && !number.IsInt()) {
			return false
		}
		if s.Minimum != nil {
			minimum, valid := exactNumber(*s.Minimum)
			if !valid || number.Cmp(minimum) < 0 {
				return false
			}
		}
		if s.Maximum != nil {
			maximum, valid := exactNumber(*s.Maximum)
			if !valid || number.Cmp(maximum) > 0 {
				return false
			}
		}
		return true
	default:
		return false
	}
	return true
}
func schemaField(s *Schema, path string) bool {
	parts, err := ParseFieldPath(path)
	if err != nil {
		return false
	}
	_, ok := SchemaField(s, parts)
	return ok
}
