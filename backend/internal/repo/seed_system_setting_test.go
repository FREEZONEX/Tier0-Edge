package repo

import "testing"

// 系统配置页的登录有效期 API 必须有匹配的 sys_resource_action seed,
// 否则 SessionAuth+Permission 路由对所有角色(含 admin)一律 403。
// 该路由同时受 permissionMiddleware.isAdminOnlyInfrastructureAPI 的 admin 硬门禁保护:
// 即使自定义角色被授予 system.session.manage,非 admin 依然 403(需求要求仅超管可改)。
// operatorResourceIDs 不含该资源,operator 天然排除。两个方向都钉住。

const (
	systemSessionManageResourceID = int64(185)
	systemSessionManageResource   = "system.session.manage"
	loginSessionDurationAction    = "/api/core/system/login-session-duration"
)

func TestSystemSessionManageGrantsLoginSessionDurationAPI(t *testing.T) {
	foundResource := false
	for _, r := range seedResources() {
		if r.ID != systemSessionManageResourceID {
			continue
		}
		foundResource = true
		if r.ResourceKey != systemSessionManageResource {
			t.Fatalf("resource %d key = %q, want %q", r.ID, r.ResourceKey, systemSessionManageResource)
		}
		if r.ParentID != 100 {
			t.Fatalf("resource %d parent = %d, want 100(system)", r.ID, r.ParentID)
		}
	}
	if !foundResource {
		t.Fatalf("seedResources 缺少 %q(id=%d)", systemSessionManageResource, systemSessionManageResourceID)
	}

	foundAction := false
	for _, action := range seedActions() {
		if action.ActionType != "api" || action.ActionValue != loginSessionDurationAction {
			continue
		}
		if action.ResourceID != systemSessionManageResourceID {
			t.Fatalf("%s bound to resource %d, want %d(system.session.manage)",
				loginSessionDurationAction, action.ResourceID, systemSessionManageResourceID)
		}
		if action.Methods != "GET,PUT" {
			t.Fatalf("%s methods = %q, want GET,PUT", loginSessionDurationAction, action.Methods)
		}
		foundAction = true
	}
	if !foundAction {
		t.Fatalf("seedActions 缺少 api action %q;无 seed action 时该路由对含 admin 在内的所有角色 403",
			loginSessionDurationAction)
	}
}

func TestOperatorRoleDoesNotGetSystemSessionManage(t *testing.T) {
	for _, id := range operatorResourceIDs() {
		if id == systemSessionManageResourceID {
			t.Fatalf("operatorResourceIDs 不能包含 %d(system.session.manage),否则 operator 可修改全局登录有效期",
				id)
		}
	}
}

func TestSeedAuditAndLoginResourcesAreIsolated(t *testing.T) {
	ids := map[int64]string{}
	keys := map[string]int64{}
	for _, r := range seedResources() {
		if previous, ok := ids[r.ID]; ok {
			t.Fatalf("resource ID %d reused by %s and %s", r.ID, previous, r.ResourceKey)
		}
		ids[r.ID] = r.ResourceKey
		keys[r.ResourceKey] = r.ID
	}
	if keys["system.audit.export"] == keys["system.login.log"] {
		t.Fatal("audit export and login log share permission")
	}
	for _, a := range seedActions() {
		if a.ActionValue == "/api/core/login-logs/page" && a.ResourceID != keys["system.login.log"] {
			t.Fatal("login API bound to wrong resource")
		}
		if a.ActionValue == "/api/core/audit-logs/export" && a.ResourceID != keys["system.audit.export"] {
			t.Fatal("export API bound to wrong resource")
		}
	}
}
