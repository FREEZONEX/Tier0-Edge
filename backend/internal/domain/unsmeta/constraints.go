package unsmeta

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// IndexSpec 使用 Nova 的业务字段路径；target 仅为 Sample 查询索引扩展。
// 省略 target 时作用于 Record，唯一索引只允许用于 Record。
type IndexSpec struct {
	Path   string   `json:"path,omitempty"`
	Paths  []string `json:"paths,omitempty"`
	Kind   string   `json:"kind,omitempty"`
	Unique bool     `json:"unique,omitempty"`
	Target string   `json:"target,omitempty"`
}

func (s IndexSpec) AllPaths() []string {
	if len(s.Paths) > 0 {
		return s.Paths
	}
	return []string{s.Path}
}
func (s IndexSpec) EffectiveKind() string {
	if s.Kind == "" {
		return "btree"
	}
	return s.Kind
}
func (s IndexSpec) TargetKind() string {
	if s.Target == "" {
		return "records"
	}
	return s.Target
}
func (s IndexSpec) normalized() IndexSpec {
	s.Paths = append([]string(nil), s.AllPaths()...)
	s.Path = ""
	for i, path := range s.Paths {
		parts, _ := ParseFieldPath(path)
		s.Paths[i] = FormatFieldPath(parts)
	}
	s.Kind, s.Target = s.EffectiveKind(), s.TargetKind()
	return s
}
func indexSpecKey(s IndexSpec) string {
	raw, _ := json.Marshal(s.normalized())
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

// IndexState 公开已授权 Collection 的规格及状态，不包含数据库诊断中的业务值。
type IndexState struct {
	Name  string    `json:"name"`
	Spec  IndexSpec `json:"spec"`
	State string    `json:"state"`
	Error string    `json:"error,omitempty"`
}

func ValidateIndexes(d Document) error {
	if len(d.Indexes) > 16 {
		return ErrSchema
	}
	seen := map[string]bool{}
	for _, spec := range d.Indexes {
		if spec.Path != "" && len(spec.Paths) > 0 || len(spec.AllPaths()) > 3 {
			return ErrSchema
		}
		kind, target := spec.EffectiveKind(), spec.TargetKind()
		if kind != "btree" && kind != "gin" || target != "records" && target != "samples" || spec.Unique && (kind != "btree" || target != "records") || kind == "gin" && len(spec.AllPaths()) != 1 {
			return ErrSchema
		}
		paths := map[string]bool{}
		for _, path := range spec.AllPaths() {
			parts, err := ParseFieldPath(path)
			if err != nil || len(parts) == 0 || parts[0] == "_id" || parts[0] == "_meta" {
				return ErrSchema
			}
			canonical := FormatFieldPath(parts)
			if paths[canonical] {
				return ErrSchema
			}
			paths[canonical] = true
			field, ok := SchemaField(queryIndexSchema(d, target), parts)
			if !ok || kind == "gin" && field.Type != "array" && field.Type != "object" {
				return ErrSchema
			}
		}
		key := indexSpecKey(spec)
		if seen[key] {
			return ErrSchema
		}
		seen[key] = true
	}
	return nil
}

// 内部任务复用索引编译结构，对外仅接受 indexes 声明。
func queryIndexes(d Document) []QueryIndex {
	out := make([]QueryIndex, 0, len(d.Indexes))
	for _, spec := range d.Indexes {
		index := QueryIndex{Name: indexSpecKey(spec), Kind: spec.TargetKind(), Type: spec.EffectiveKind(), Unique: spec.Unique, Spec: spec}
		for _, path := range spec.AllPaths() {
			index.Fields = append(index.Fields, QueryIndexField{Field: path, Direction: "asc"})
		}
		out = append(out, index)
	}
	return out
}
func IndexStates(states []QueryIndexState, d Document) []IndexState {
	out := make([]IndexState, 0, len(states))
	for _, state := range states {
		spec := state.Spec
		for _, declared := range d.Indexes {
			if indexSpecKey(declared) == state.Name {
				spec = declared
				break
			}
		}
		out = append(out, IndexState{Name: state.Name, Spec: spec, State: state.Status, Error: state.ErrorCode})
	}
	return out
}
