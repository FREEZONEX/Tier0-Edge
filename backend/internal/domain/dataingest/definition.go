package dataingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"backend/internal/repo"
)

type Definition struct {
	ID             int64
	Namespace      string
	Alias          string
	TopicType      int16
	DataType       int
	Fields         []Field
	SaveToDB       bool
	TimestampField string
	QualityField   string
}

type Field struct {
	Name        string
	Type        string
	SystemField bool
	LowerLimit  *float64 `json:"lowerLimit,omitempty"`
	UpperLimit  *float64 `json:"upperLimit,omitempty"`
}

type schemaDoc struct {
	DataType int     `json:"dataType"`
	Fields   []Field `json:"fields"`
}

type DefinitionResolver struct {
	repo    *repo.UnsRepo
	mu      sync.RWMutex
	byID    map[int64]*Definition
	byTopic map[string]*Definition
}

func NewDefinitionResolver(repo *repo.UnsRepo) *DefinitionResolver {
	return &DefinitionResolver{
		repo:    repo,
		byID:    map[int64]*Definition{},
		byTopic: map[string]*Definition{},
	}
}

func (r *DefinitionResolver) Resolve(ctx context.Context, topic string) (*Definition, error) {
	topic = strings.Trim(strings.TrimSpace(topic), "/")
	if topic == "" {
		return nil, ErrDefinitionNotFound
	}
	if id, err := strconv.ParseInt(topic, 10, 64); err == nil {
		if def, err := r.resolveByID(ctx, id); err == nil {
			return def, nil
		}
	}
	if def := r.cachedTopic(topic); def != nil {
		return def, nil
	}
	if def, err := r.resolveByNamespace(ctx, topic); err == nil {
		return def, nil
	}
	return nil, ErrDefinitionNotFound
}

func (r *DefinitionResolver) InvalidateNode(node repo.UnsNode) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, node.ID)
	delete(r.byTopic, strings.Trim(node.Namespace, "/"))
}

func (r *DefinitionResolver) InvalidateNodes(nodes []repo.UnsNode) {
	if r == nil || len(nodes) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, node := range nodes {
		delete(r.byID, node.ID)
		delete(r.byTopic, strings.Trim(node.Namespace, "/"))
	}
}

func (r *DefinitionResolver) resolveByID(ctx context.Context, id int64) (*Definition, error) {
	r.mu.RLock()
	def := r.byID[id]
	r.mu.RUnlock()
	if def != nil {
		return def, nil
	}
	node, err := r.repo.GetUnsNode(ctx, id, false)
	if err != nil || node.Type != 2 {
		return nil, ErrDefinitionNotFound
	}
	return r.remember(definitionFromNode(node)), nil
}

func (r *DefinitionResolver) resolveByNamespace(ctx context.Context, namespace string) (*Definition, error) {
	namespace = strings.Trim(namespace, "/")
	if def := r.cachedTopic(namespace); def != nil {
		return def, nil
	}
	node, err := r.repo.GetUnsNodeByNamespace(ctx, namespace)
	if err != nil || node.Type != 2 {
		return nil, ErrDefinitionNotFound
	}
	return r.remember(definitionFromNode(node)), nil
}

func (r *DefinitionResolver) cachedTopic(topic string) *Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byTopic[strings.Trim(topic, "/")]
}

func (r *DefinitionResolver) remember(def *Definition) *Definition {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[def.ID] = def
	if def.Namespace != "" {
		r.byTopic[def.Namespace] = def
	}
	return def
}

func definitionFromNode(node repo.UnsNode) *Definition {
	doc := parseSchema(node.Schema)
	dataType := doc.DataType
	if dataType == 0 {
		dataType = dataTypeFromTopicType(node.TopicType)
	}
	timestampField := "timeStamp"
	qualityField := "quality"
	if node.TopicType == metricTopicType {
		timestampField = metricTimeColumn
		qualityField = metricQualityColumn
	}
	def := &Definition{
		ID:             node.ID,
		Namespace:      strings.Trim(node.Namespace, "/"),
		Alias:          strings.Trim(node.Alias, "/"),
		TopicType:      node.TopicType,
		DataType:       dataType,
		Fields:         normalizeFields(doc.Fields),
		SaveToDB:       node.EnableHistory == 1,
		TimestampField: timestampField,
		QualityField:   qualityField,
	}
	if def.Alias == "" {
		def.Alias = def.Namespace
	}
	for _, field := range def.Fields {
		if isTimestampSystemField(field.Name, def.TopicType) {
			def.TimestampField = field.Name
		}
		if isQualitySystemField(field.Name, def.TopicType) {
			def.QualityField = field.Name
		}
	}
	return def
}

func isTimestampSystemField(name string, topicType int16) bool {
	if topicType == metricTopicType {
		return strings.EqualFold(name, metricTimeColumn) || strings.EqualFold(name, "_ct") || strings.EqualFold(name, "created_time")
	}
	return strings.EqualFold(name, "timeStamp") || strings.EqualFold(name, "timestamp") || strings.EqualFold(name, "_ct") || strings.EqualFold(name, "created_time")
}

func isQualitySystemField(name string, topicType int16) bool {
	if topicType == metricTopicType {
		return strings.EqualFold(name, metricQualityColumn) || strings.EqualFold(name, "_qos")
	}
	return strings.EqualFold(name, "quality") || strings.EqualFold(name, "_qos")
}

func parseSchema(raw json.RawMessage) schemaDoc {
	if len(raw) == 0 {
		return schemaDoc{}
	}
	var fields []Field
	if err := json.Unmarshal(raw, &fields); err == nil {
		return schemaDoc{Fields: fields}
	}
	var doc schemaDoc
	_ = json.Unmarshal(raw, &doc)
	return doc
}

func dataTypeFromTopicType(topicType int16) int {
	switch topicType {
	case 3:
		return 1
	case 1, 2:
		return 8
	default:
		return 0
	}
}

func normalizeFields(fields []Field) []Field {
	out := make([]Field, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		field.Name = strings.TrimSpace(field.Name)
		if field.Name == "" {
			continue
		}
		key := strings.ToLower(field.Name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		field.Type = normalizeFieldType(field.Type)
		out = append(out, field)
	}
	return out
}

func ValidateSchema(raw json.RawMessage) error {
	doc := parseSchema(raw)
	seen := map[string]struct{}{}
	for _, field := range doc.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			return errors.New("schema field name is empty")
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate schema field: %s", name)
		}
		seen[key] = struct{}{}
		if normalizeFieldType(field.Type) == "" {
			return fmt.Errorf("unsupported schema field type: %s", field.Type)
		}
	}
	return nil
}

var ErrDefinitionNotFound = errors.New("uns definition not found")
