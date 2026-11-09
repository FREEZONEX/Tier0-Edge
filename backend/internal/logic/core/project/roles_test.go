package project

import (
	"reflect"
	"testing"
)

func TestParseProjectAppRoles_ReferenceFormat(t *testing.T) {
	raw := []byte(`[
		{"roleKey":"test","name":"测试","description":"测试"},
		{"roleKey":"admin","name":"管理员","description":"超管"}
	]`)
	roles, err := parseProjectAppRoles(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []projectPackageRole{
		{Role: "test", DisplayName: "测试", Description: "测试"},
		{Role: "admin", DisplayName: "管理员", Description: "超管"},
	}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("got %+v, want %+v", roles, want)
	}
}

func TestParseProjectAppRoles_SnakeCaseRoleKey(t *testing.T) {
	raw := []byte(`[
		{"role_key":"head_teacher","name":"班主任","description":"负责班级整体任务、作业、人员与进度统筹"},
		{"role_key":"student","name":"普通学生","description":"负责接收个人任务、提交作业并跟踪个人完成情况"}
	]`)
	roles, err := parseProjectAppRoles(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []projectPackageRole{
		{Role: "head_teacher", DisplayName: "班主任", Description: "负责班级整体任务、作业、人员与进度统筹"},
		{Role: "student", DisplayName: "普通学生", Description: "负责接收个人任务、提交作业并跟踪个人完成情况"},
	}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("got %+v, want %+v", roles, want)
	}
}

func TestParseProjectAppRoles_Wrapper(t *testing.T) {
	raw := []byte(`{"roles":[{"role":"viewer","displayName":"观众","description":"只读"}]}`)
	roles, err := parseProjectAppRoles(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []projectPackageRole{
		{Role: "viewer", DisplayName: "观众", Description: "只读"},
	}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("got %+v, want %+v", roles, want)
	}
}

func TestParseProjectAppRoles_StringList(t *testing.T) {
	raw := []byte(`["role1","role2"]`)
	roles, err := parseProjectAppRoles(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []projectPackageRole{
		{Role: "role1", DisplayName: "role1"},
		{Role: "role2", DisplayName: "role2"},
	}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("got %+v, want %+v", roles, want)
	}
}

func TestParseProjectAppRoles_FieldAliasFallback(t *testing.T) {
	raw := []byte(`[{"key":"only-key","desc":"desc"}]`)
	roles, err := parseProjectAppRoles(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []projectPackageRole{
		{Role: "only-key", DisplayName: "only-key", Description: "desc"},
	}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("got %+v, want %+v", roles, want)
	}
}

func TestParseProjectAppRoles_UTF8BOM(t *testing.T) {
	raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`[{"roleKey":"bom","name":"BOM"}]`)...)
	roles, err := parseProjectAppRoles(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []projectPackageRole{
		{Role: "bom", DisplayName: "BOM"},
	}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("got %+v, want %+v", roles, want)
	}
}

func TestParseProjectAppRoles_Empty(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte(""), []byte("  "), []byte("[]"), []byte(`{"roles":[]}`)} {
		roles, err := parseProjectAppRoles(raw)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", raw, err)
		}
		if len(roles) != 0 {
			t.Fatalf("expected empty roles for %q, got %d", raw, len(roles))
		}
	}
}

func TestParseProjectAppRoles_NameTooLong(t *testing.T) {
	raw := []byte(`[{"roleKey":"x","name":"` + string(make([]byte, 129)) + `"}]`)
	_, err := parseProjectAppRoles(raw)
	if err == nil {
		t.Fatal("expected error for name too long")
	}
}

func TestParseProjectAppRoles_DescriptionTooLong(t *testing.T) {
	raw := []byte(`[{"roleKey":"x","name":"x","description":"` + string(make([]byte, 513)) + `"}]`)
	_, err := parseProjectAppRoles(raw)
	if err == nil {
		t.Fatal("expected error for description too long")
	}
}

func TestParseProjectAppRoles_InvalidJSON(t *testing.T) {
	raw := []byte(`{not json}`)
	_, err := parseProjectAppRoles(raw)
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestParseProjectAppRoles_ObjectMap_StringValues(t *testing.T) {
	raw := []byte(`{"admin":"管理员","viewer":"观众"}`)
	roles, err := parseProjectAppRoles(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []projectPackageRole{
		{Role: "admin", DisplayName: "管理员"},
		{Role: "viewer", DisplayName: "观众"},
	}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("got %+v, want %+v", roles, want)
	}
}

func TestParseProjectAppRoles_ObjectMap_ObjectValues(t *testing.T) {
	raw := []byte(`{"admin":{"name":"系统管理员","description":"全部权限"},"viewer":{"name":"只读用户"}}`)
	roles, err := parseProjectAppRoles(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []projectPackageRole{
		{Role: "admin", DisplayName: "系统管理员", Description: "全部权限"},
		{Role: "viewer", DisplayName: "只读用户"},
	}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("got %+v, want %+v", roles, want)
	}
}
