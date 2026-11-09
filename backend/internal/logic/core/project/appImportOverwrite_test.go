package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// 覆盖导入字段级规则（2026-08-06 拍板，EIMP-034/050）的纯函数测试：
// schema 按字段 name 增量合并（同名替换/新增追加/缺失保留），type/topicType 映射保留原值。

func TestMergeUnsSchemaFields(t *testing.T) {
	field := func(name, typ string) map[string]any {
		return map[string]any{"name": name, "type": typ}
	}
	tests := []struct {
		name     string
		existing string
		pkg      []map[string]any
		want     []map[string]any
	}{
		{
			name:     "同名字段替换并保持目标原位置",
			existing: `[{"name":"a","type":"FLOAT"},{"name":"b","type":"FLOAT"}]`,
			pkg:      []map[string]any{field("a", "FLOAT"), field("b", "DOUBLE")},
			want:     []map[string]any{field("a", "FLOAT"), field("b", "DOUBLE")},
		},
		{
			name:     "包内新增字段追加在尾部",
			existing: `[{"name":"a","type":"FLOAT"}]`,
			pkg:      []map[string]any{field("a", "FLOAT"), field("c", "STRING")},
			want:     []map[string]any{field("a", "FLOAT"), field("c", "STRING")},
		},
		{
			name:     "目标有而包内没有的字段保留",
			existing: `[{"name":"a","type":"FLOAT"},{"name":"legacy","type":"FLOAT"}]`,
			pkg:      []map[string]any{field("a", "FLOAT")},
			want:     []map[string]any{field("a", "FLOAT"), field("legacy", "FLOAT")},
		},
		{
			name:     "替换保持原位置且新增按包内顺序追加",
			existing: `[{"name":"a","type":"FLOAT"},{"name":"b","type":"FLOAT"}]`,
			pkg:      []map[string]any{field("c", "STRING"), field("a", "DOUBLE")},
			want:     []map[string]any{field("a", "DOUBLE"), field("b", "FLOAT"), field("c", "STRING")},
		},
		{
			name:     "existing 为 fields 包装对象也能解析",
			existing: `{"fields":[{"name":"a","type":"FLOAT"}]}`,
			pkg:      []map[string]any{field("a", "DOUBLE")},
			want:     []map[string]any{field("a", "DOUBLE")},
		},
		{
			name:     "目标字段无 name 视为无法匹配但保留",
			existing: `[{"type":"FLOAT"},{"name":"a","type":"FLOAT"}]`,
			pkg:      []map[string]any{field("a", "FLOAT")},
			want:     []map[string]any{map[string]any{"type": "FLOAT"}, field("a", "FLOAT")},
		},
		{
			name:     "包内字段无 name 被忽略",
			existing: `[{"name":"a","type":"FLOAT"}]`,
			pkg:      []map[string]any{map[string]any{"type": "STRING"}, field("a", "FLOAT")},
			want:     []map[string]any{field("a", "FLOAT")},
		},
		{
			name:     "existing 为空时结果就是包内字段",
			existing: ``,
			pkg:      []map[string]any{field("a", "FLOAT")},
			want:     []map[string]any{field("a", "FLOAT")},
		},
		{
			name:     "existing 为非法 JSON 时按空处理",
			existing: `{bad`,
			pkg:      []map[string]any{field("a", "FLOAT")},
			want:     []map[string]any{field("a", "FLOAT")},
		},
		{
			// EIMP-034：update 包仅携带部分字段时，目标端既有字段（eimp_baseline_only）保留。
			name:     "EIMP-034 增量保留场景",
			existing: `[{"name":"timestamp","type":"STRING"},{"name":"pm25","type":"FLOAT"},{"name":"eimp_baseline_only","type":"FLOAT"},{"name":"eimp_shared","type":"FLOAT"}]`,
			pkg:      []map[string]any{field("eimp_shared", "DOUBLE"), field("eimp_update_only", "STRING")},
			want: []map[string]any{
				field("timestamp", "STRING"), field("pm25", "FLOAT"),
				field("eimp_baseline_only", "FLOAT"), field("eimp_shared", "DOUBLE"),
				field("eimp_update_only", "STRING"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeUnsSchemaFields(json.RawMessage(tt.existing), tt.pkg)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeUnsSchemaFields() = %v, want %v", got, tt.want)
			}
		})
	}
}

// 包内 fields 缺失或为空数组时保留目标 schema（node.Fields 缺失与空数组不可区分）。
func TestMergeUnsSchemaFields_EmptyPackage(t *testing.T) {
	existing := json.RawMessage(`[{"name":"a","type":"FLOAT"}]`)
	want := []map[string]any{map[string]any{"name": "a", "type": "FLOAT"}}
	if got := mergeUnsSchemaFields(existing, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("mergeUnsSchemaFields(nil pkg) = %v, want %v", got, want)
	}
	if got := mergeUnsSchemaFields(existing, []map[string]any{}); !reflect.DeepEqual(got, want) {
		t.Errorf("mergeUnsSchemaFields(empty pkg) = %v, want %v", got, want)
	}
}

func TestUnsImportNodeTypeName(t *testing.T) {
	tests := []struct {
		value int16
		want  string
	}{
		{1, "folder"},
		{2, "file"},
		{0, "folder"},
	}
	for _, tt := range tests {
		if got := unsImportNodeTypeName(tt.value); got != tt.want {
			t.Errorf("unsImportNodeTypeName(%d) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestUnsImportTopicTypeName(t *testing.T) {
	tests := []struct {
		value int16
		want  string
	}{
		{1, "State"},
		{2, "Action"},
		{3, "Metric"},
		{0, ""},
		{99, ""},
	}
	for _, tt := range tests {
		if got := unsImportTopicTypeName(tt.value); got != tt.want {
			t.Errorf("unsImportTopicTypeName(%d) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestParseProjectUnsEnableHistory(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    *bool
		wantErr bool
	}{
		{name: "missing"},
		{name: "null", raw: "null"},
		{name: "cloud true", raw: `"TRUE"`, want: boolValue(true)},
		{name: "cloud false", raw: `"FALSE"`, want: boolValue(false)},
		{name: "lowercase string", raw: `"true"`, want: boolValue(true)},
		{name: "boolean", raw: "true", want: boolValue(true)},
		{name: "numeric true", raw: "1", want: boolValue(true)},
		{name: "numeric false", raw: "2", want: boolValue(false)},
		{name: "invalid", raw: `"unexpected"`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProjectUnsEnableHistory(json.RawMessage(tt.raw))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("got %v, want nil", *got)
				}
				return
			}
			if got == nil || *got != *tt.want {
				t.Fatalf("got %v, want %v", got, *tt.want)
			}
		})
	}
}

func boolValue(value bool) *bool {
	return &value
}

// 静态包同 Project 重复命中必须转为阻断错误（Replace 仅接受 artifact 包），
// 非哨兵错误原样透传（Codex P2：静态重复不能转 replaceRequired 决策）。
func TestStaticDuplicateError(t *testing.T) {
	sentinel := &appReplaceRequiredError{AppID: 10, ProjectID: 74, Name: "demo"}
	err := staticDuplicateError(sentinel)
	if err == nil || errors.Is(err, sentinel) {
		t.Fatalf("sentinel should be converted to a blocking error, got %v", err)
	}
	if !strings.Contains(err.Error(), "静态应用包不支持覆盖导入") {
		t.Errorf("unexpected message: %v", err)
	}
	other := fmt.Errorf("other")
	if got := staticDuplicateError(other); !errors.Is(got, other) {
		t.Errorf("non-sentinel error should pass through, got %v", got)
	}
}
