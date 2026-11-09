// TASK-028 audit.go 单测。
// 目标：覆盖 recordUNSAudit / firstUNSAuditString / unsAuditString 三个辅助函数。
// 正常路径：string/int64/int/int32/float64 值转审计字符串；name 按 namespace/name/displayName
// 优先级取首个非空值；有 id/name 时调用 App.Audit.Record（ctx state，不落库，属中间件链路）。
// 异常情况：nil svcCtx / nil App / nil Audit 服务空指针安全；数据缺 id 且缺 name 时提前返回。
package uns

import (
	"context"
	"testing"

	"backend/internal/bootstrap"
	"backend/internal/domain/audit"
	"backend/internal/svc"

	"github.com/stretchr/testify/require"
)

func TestUnsAuditString_StringTrimmed(t *testing.T) {
	require.Equal(t, "abc", unsAuditString(map[string]any{"id": "  abc  "}, "id"))
}

func TestUnsAuditString_MissingKey(t *testing.T) {
	require.Equal(t, "", unsAuditString(map[string]any{"name": "x"}, "id"))
}

func TestUnsAuditString_IntTypes(t *testing.T) {
	require.Equal(t, "42", unsAuditString(map[string]any{"v": int64(42)}, "v"))
	require.Equal(t, "42", unsAuditString(map[string]any{"v": 42}, "v"))
	require.Equal(t, "42", unsAuditString(map[string]any{"v": int32(42)}, "v"))
}

func TestUnsAuditString_Float64(t *testing.T) {
	require.Equal(t, "42", unsAuditString(map[string]any{"v": float64(42)}, "v"))
	require.Equal(t, "3.14", unsAuditString(map[string]any{"v": 3.14}, "v"))
}

func TestUnsAuditString_UnsupportedType(t *testing.T) {
	require.Equal(t, "", unsAuditString(map[string]any{"v": true}, "v"))
	require.Equal(t, "", unsAuditString(map[string]any{"v": []string{"a"}}, "v"))
}

func TestFirstUNSAuditString_NamespacePriority(t *testing.T) {
	data := map[string]any{"namespace": "sensor/01", "name": "fallback"}
	require.Equal(t, "sensor/01", firstUNSAuditString(data, "namespace", "name", "displayName"))
}

func TestFirstUNSAuditString_FallsBackToName(t *testing.T) {
	data := map[string]any{"namespace": "", "displayName": ""}
	require.Equal(t, "", firstUNSAuditString(data, "namespace", "name", "displayName"))
	data["name"] = "sensor/01"
	require.Equal(t, "sensor/01", firstUNSAuditString(data, "namespace", "name", "displayName"))
}

func TestRecordUNSAudit_NilSvcCtx(t *testing.T) {
	require.NotPanics(t, func() {
		recordUNSAudit(context.Background(), nil, map[string]any{"id": int64(1), "name": "x"}, audit.BizCreate)
	})
}

func TestRecordUNSAudit_NilApp(t *testing.T) {
	require.NotPanics(t, func() {
		recordUNSAudit(context.Background(), &svc.ServiceContext{}, map[string]any{"id": int64(1)}, audit.BizCreate)
	})
}

func TestRecordUNSAudit_NilAuditService(t *testing.T) {
	require.NotPanics(t, func() {
		recordUNSAudit(context.Background(), &svc.ServiceContext{App: &bootstrap.App{}}, map[string]any{"id": int64(1)}, audit.BizCreate)
	})
}

func TestRecordUNSAudit_NoIDNoName(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	// 数据缺 id 且缺 name 时提前返回，不触发 Record
	require.NotPanics(t, func() {
		recordUNSAudit(ctx, svcCtx, map[string]any{"foo": "bar"}, audit.BizCreate)
	})
}

func TestRecordUNSAudit_WithRealSvcCtx(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	// 完整链路：Record 写入 ctx state（审计落库由 HTTP 中间件负责，单测不覆盖），
	// 仅验证不 panic、不返回错误。
	require.NotPanics(t, func() {
		recordUNSAudit(ctx, svcCtx, map[string]any{"id": int64(42), "name": "sensor/01"}, audit.BizCreate)
	})
}
