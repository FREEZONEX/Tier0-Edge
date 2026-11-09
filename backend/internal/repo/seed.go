package repo

import (
	"context"
	"os"
	"strings"
	"time"

	"backend/internal/config"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) Seed(ctx context.Context, security config.SecurityConf, gateway config.GatewayConf) error {
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	builderPassword := security.InitialAdminPassword
	if builderPassword == "" {
		builderPassword = "tier0"
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(builderPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	return s.CommonDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := migrateLegacyBuilderRole(tx, ts); err != nil {
			return err
		}

		builderRole := sysRoleInfo{ID: SystemRoleAdminID, Name: "Admin", Code: BuiltinRoleAdmin, Description: "built-in admin", Type: 1, Status: 1, DefaultHomePage: DefaultHomePage}
		builderRole.CreatedTime = ts
		builderRole.UpdatedTime = ts
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "code"}, {Name: "deleted_time"}},
			DoUpdates: clause.Assignments(map[string]any{"default_home_page": DefaultHomePage, "updated_time": ts}),
		}).Create(&builderRole).Error; err != nil {
			return err
		}
		builderRoleID := builderRole.ID

		builderEmail, err := encryptUserContact(userContactFieldEmail, "tier0@example.com")
		if err != nil {
			return err
		}
		builderUser := sysUserInfo{UserName: "tier0", NickName: "Admin", Password: string(passwordHash), Email: builderEmail, Status: 1, IsRandomPwd: false}
		builderUser.CreatedTime = ts
		builderUser.UpdatedTime = ts
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_name"}, {Name: "deleted_time"}},
			DoUpdates: clause.Assignments(map[string]any{"updated_time": ts}),
		}).Create(&builderUser).Error; err != nil {
			return err
		}
		builderUserID := builderUser.ID

		builderWorkspaceUser := sysWorkspaceUser{WorkspaceID: 1, UserID: builderUserID, RoleCode: BuiltinRoleAdmin, RoleID: builderRoleID}
		builderWorkspaceUser.CreatedTime = ts
		builderWorkspaceUser.UpdatedTime = ts
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "user_id"}, {Name: "deleted_time"}},
			DoUpdates: clause.Assignments(map[string]any{"role_code": BuiltinRoleAdmin, "role_id": builderRoleID, "updated_time": ts}),
		}).Create(&builderWorkspaceUser).Error; err != nil {
			return err
		}

		operatorRole := sysRoleInfo{ID: SystemRoleOperatorID, Name: "Operator", Code: BuiltinRoleOperator, Description: "built-in operator", Type: 1, Status: 1, DefaultHomePage: DefaultOperatorHomePage}
		operatorRole.CreatedTime = ts
		operatorRole.UpdatedTime = ts
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "code"}, {Name: "deleted_time"}},
			DoUpdates: clause.Assignments(map[string]any{"default_home_page": DefaultOperatorHomePage, "updated_time": ts}),
		}).Create(&operatorRole).Error; err != nil {
			return err
		}
		operatorRoleID := operatorRole.ID

		if err := migrateLoginLogGrants(tx); err != nil {
			return err
		}
		resources := seedResources()
		for _, r := range resources {
			row := sysResourceInfo{ID: r.ID, ParentID: r.ParentID, ResourceKey: r.ResourceKey, Name: r.Name, Type: r.Type, RoutePath: r.RoutePath, UrlType: r.UrlType, OpenType: r.OpenType, Icon: r.Icon, Sort: r.Sort, Enabled: 1, SystemGenerated: 1}
			row.CreatedTime = ts
			row.UpdatedTime = ts
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{"parent_id", "resource_key", "name", "type", "route_path", "url_type", "open_type", "icon", "sort", "enabled", "system_generated", "updated_time"}),
			}).Create(&row).Error; err != nil {
				return err
			}
			builderRoleResource := roleResourceWithTime(builderRoleID, r.ID, now)
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "role_id"}, {Name: "resource_id"}, {Name: "deleted_time"}},
				DoNothing: true,
			}).Create(builderRoleResource).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&sysResourceInfo{}).Where("resource_key = ?", "system.localization").
			Updates(map[string]any{"enabled": 0, "updated_time": ts}).Error; err != nil {
			return err
		}
		if err := tx.Model(&sysResourceAction{}).Where("action_value LIKE ?", "Localization.%").
			Updates(map[string]any{"enabled": 0, "updated_time": ts}).Error; err != nil {
			return err
		}
		operatorResources := operatorResourceIDs()
		if err := tx.Where("role_id = ? AND deleted_time = 0 AND resource_id NOT IN ?", operatorRoleID, operatorResources).
			Delete(&sysRoleResource{}).Error; err != nil {
			return err
		}
		for _, resourceID := range operatorResources {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "role_id"}, {Name: "resource_id"}, {Name: "deleted_time"}},
				DoNothing: true,
			}).Create(roleResourceWithTime(operatorRoleID, resourceID, now)).Error; err != nil {
				return err
			}
		}

		for _, a := range seedActions() {
			row := sysResourceAction{ResourceID: a.ResourceID, ActionType: a.ActionType, ActionValue: a.ActionValue, Methods: a.Methods, Enabled: 1, SystemGenerated: 1}
			row.CreatedTime = ts
			row.UpdatedTime = ts
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "action_type"}, {Name: "action_value"}, {Name: "methods"}, {Name: "deleted_time"}},
				DoUpdates: clause.AssignmentColumns([]string{"resource_id", "enabled", "system_generated", "updated_time"}),
			}).Create(&row).Error; err != nil {
				return err
			}
		}
		if err := alignClusterConnectionActions(tx, ts); err != nil {
			return err
		}
		if err := disableRetiredSeedEntries(tx, now); err != nil {
			return err
		}
		if strings.TrimSpace(os.Getenv("SOURCE_STREAM_NATS_URL")) != "" {
			// Keep stored legacy flows and their history, but retire their menus
			// and actions when this deployment uses the Source/Stream runtime.
			legacyIDs := []int64{298, 299, 300, 310, 319}
			if err := tx.Model(&sysResourceInfo{}).Where("id IN ?", legacyIDs).
				Updates(map[string]any{"enabled": 0, "updated_time": ts}).Error; err != nil {
				return err
			}
			if err := tx.Model(&sysResourceAction{}).Where("resource_id IN ?", legacyIDs).
				Updates(map[string]any{"enabled": 0, "updated_time": ts}).Error; err != nil {
				return err
			}
		}
		for _, route := range builtinRoutes(gateway) {
			if err := upsertGatewayRoute(tx, route, now); err != nil {
				return err
			}
		}

		if strings.TrimSpace(security.PluginAPIKey) != "" {
			apiKeyRepo := NewAPIKeyRepo(tx)
			if err := apiKeyRepo.RegisterAPIKey(ctx, security.PluginAPIKey,
				"edge-opcua-server", "", builderUserID,
				"service", "read_only", "internal",
				APIKeyResourceKeysForPermission("read_only")); err != nil {
				return err
			}
		}

		return nil
	})
}

// migrateLegacyBuilderRole upgrades the old built-in "builder" role to the new
// "admin" role code/name so that existing deployments continue to work after the
// built-in role rename. It also rewrites workspace role codes that still point to
// the legacy code, and normalizes legacy project_member.status values that used
// to hold access-level names (e.g. "Can Edit") to the active membership status.
func migrateLegacyBuilderRole(tx *gorm.DB, ts time.Time) error {
	result := tx.Model(&sysRoleInfo{}).
		Where("id = ? AND code = ? AND deleted_time = 0", SystemRoleAdminID, legacyBuiltinRoleBuilder).
		Updates(map[string]any{
			"name":              "Admin",
			"code":              BuiltinRoleAdmin,
			"default_home_page": DefaultHomePage,
			"updated_time":      ts,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		if err := tx.Model(&sysWorkspaceUser{}).
			Where("role_id = ? AND role_code = ? AND deleted_time = 0", SystemRoleAdminID, legacyBuiltinRoleBuilder).
			Updates(map[string]any{
				"role_code":    BuiltinRoleAdmin,
				"updated_time": ts,
			}).Error; err != nil {
			return err
		}
	}
	// Normalize legacy project_member.status values that stored access-level
	// names (e.g. "Admin"/"Can Edit"/"Can Access") to the active membership
	// status. The legacy access-level semantics are intentionally dropped:
	// project/app permissions are now expressed through project roles and
	// project_member_role. Administrators must re-assign roles to legacy
	// members after upgrading.
	if err := tx.Table("project_member").
		Where("status <> ?", "active").
		Updates(map[string]any{
			"status":       "active",
			"updated_time": time.Now().UTC().UnixMilli(),
		}).Error; err != nil {
		return err
	}
	return nil
}

func alignClusterConnectionActions(tx *gorm.DB, ts time.Time) error {
	connectionActions := []ResourceAction{
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/sample-history", Methods: "POST"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/config", Methods: "GET"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/connect", Methods: "POST"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/force-sync", Methods: "POST"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/user-sync", Methods: "POST"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/disconnect", Methods: "POST"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/test-connect", Methods: "POST"},
	}
	for _, action := range connectionActions {
		if err := tx.Model(&sysResourceAction{}).
			Where(
				"action_type = ? AND action_value = ? AND methods = ? AND deleted_time = 0",
				action.ActionType,
				action.ActionValue,
				action.Methods,
			).
			Updates(map[string]any{
				"resource_id":      action.ResourceID,
				"enabled":          1,
				"system_generated": 1,
				"updated_time":     ts,
			}).Error; err != nil {
			return err
		}
	}
	return nil
}

func roleResourceWithTime(roleID, resourceID, now int64) *sysRoleResource {
	ts := repoTimeFromMilli(now)
	row := sysRoleResource{RoleID: roleID, ResourceID: resourceID}
	row.ID = systemRoleResourceID(roleID, resourceID)
	row.CreatedTime = ts
	row.UpdatedTime = ts
	return &row
}

func systemRoleResourceID(roleID, resourceID int64) int64 {
	return roleID*100000 + resourceID
}

func resourceKeys(resources []Resource) []string {
	out := make([]string, 0, len(resources))
	for _, r := range resources {
		out = append(out, r.ResourceKey)
	}
	return out
}

// retiredSeedActionValues 是已停用的 action_value 清单：从 seedActions 里删掉不会回收存量库
// 已经种下的行，必须在这里显式停用。
func retiredSeedActionValues() []string {
	return []string{
		// PR #525 曾把这条挂到 launchpad.view 上给 operator 看图，随后改为按 App 取图的
		// 专用端点；存量库里已经种下的这行必须显式停用，否则宽授权会一直留着。
		"/api/core/assets/:fileId/download",
		"button:MenuConfiguration.addOperation",
		"button:MenuConfiguration.editOperation",
		"button:MenuConfiguration.deleteOperation",
		"/sourceflow/**",
		"/nodered/home/**",
		"/eventflow/home/**",
		"/emqx/**",
		"advancedUse.advancedUse",
		"menu.emqx",
	}
}

func disableRetiredSeedEntries(tx *gorm.DB, now int64) error {
	ts := repoTimeFromMilli(now)
	retiredResourceKeys := retiredSeedResourceKeys()
	retiredActionValues := retiredSeedActionValues()
	// Fleet remains an internal UI/MCP capability; retire prior public bindings.
	if err := tx.Model(&sysResourceAction{}).Where("action_value LIKE ?", "/openapi/v2/fleet/%").Updates(map[string]any{"enabled": 0, "updated_time": ts}).Error; err != nil {
		return err
	}

	var resourceIDs []int64
	if err := tx.Model(&sysResourceInfo{}).
		Where("resource_key IN ?", retiredResourceKeys).
		Pluck("id", &resourceIDs).Error; err != nil {
		return err
	}
	if len(resourceIDs) > 0 {
		if err := tx.Model(&sysResourceInfo{}).
			Where("id IN ?", resourceIDs).
			Updates(map[string]any{"enabled": 0, "updated_time": ts}).Error; err != nil {
			return err
		}
		if err := tx.Model(&sysResourceAction{}).
			Where("resource_id IN ?", resourceIDs).
			Updates(map[string]any{"enabled": 0, "updated_time": ts}).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&sysResourceAction{}).
		Where("action_value IN ?", retiredActionValues).
		Updates(map[string]any{"enabled": 0, "updated_time": ts}).Error; err != nil {
		return err
	}
	for _, action := range retiredSeedActionMethodPairs() {
		if err := tx.Model(&sysResourceAction{}).
			Where(
				"action_type = ? AND action_value = ? AND methods = ? AND deleted_time = 0",
				action.ActionType,
				action.ActionValue,
				action.Methods,
			).
			Updates(map[string]any{"enabled": 0, "updated_time": ts}).Error; err != nil {
			return err
		}
	}
	if err := tx.Table("sys_gateway_route").
		Where("route_key IN ? AND deleted_time = 0", retiredGatewayRouteKeys()).
		Updates(map[string]any{"enabled": false, "updated_time": ts}).Error; err != nil {
		return err
	}
	if err := tx.Table("sys_oauth_client").
		Where("client_id IN ? AND system_builtin = true", retiredOAuthClientIDs()).
		Updates(map[string]any{"enabled": false, "updated_time": ts}).Error; err != nil {
		return err
	}
	return nil
}

func retiredGatewayRouteKeys() []string {
	return []string{
		"proxy.unsagent",
		"proxy.marimo", "flow.source.payload", "flow.event.payload", "flow.source.home.payload", "flow.event.home.payload",
		"proxy.nodered.home.edge", "proxy.eventflow.home.edge", "proxy.nodered.edge", "proxy.eventflow.edge",
		"proxy.nodered.home",
		"proxy.eventflow.home",
		"proxy.nodered.tenant",
		"proxy.eventflow.tenant",
		"proxy.sourceflow",
		"proxy.eventflow",
		"proxy.grafana",
		"proxy.portainer.home",
		"proxy.portainer",
		"proxy.emqx.home",
		"proxy.emqx",
	}
}

func retiredOAuthClientIDs() []string {
	return []string{
		"portainer",
	}
}

func retiredSeedActionMethodPairs() []ResourceAction {
	return []ResourceAction{
		{ActionType: "api", ActionValue: "/api/core/gateway/routes", Methods: "GET,POST"},
		{ActionType: "api", ActionValue: "/api/core/gateway/routes/**", Methods: "PUT,DELETE,POST"},
		{ActionType: "api", ActionValue: "/api/core/mqtt/auth-clients", Methods: "GET,POST"},
		{ActionType: "api", ActionValue: "/api/core/mqtt/auth-clients/**", Methods: "POST,DELETE"},
		{ActionType: "api", ActionValue: "/api/core/common/api-keys", Methods: "GET,POST"},
		{ActionType: "api", ActionValue: "/api/core/common/api-keys/**", Methods: "PUT,DELETE"},
		{ActionType: "api", ActionValue: "/api/core/iam/resources/**", Methods: "GET,POST,PUT,DELETE"},
		{ActionType: "api", ActionValue: "/api/core/iam/resources/**", Methods: "POST,PUT"},
		{ActionType: "api", ActionValue: "/api/core/iam/roles", Methods: "GET,POST"},
		{ActionType: "api", ActionValue: "/api/core/iam/roles/**", Methods: "PUT,DELETE"},
		{ActionType: "openapi", ActionValue: "/openapi/v1/mock/ping", Methods: "GET"},
		{ActionType: "openapi", ActionValue: "/openapi/v1/uns/browse", Methods: "GET"},
		{ActionType: "openapi", ActionValue: "/openapi/v1/uns/nodes/**", Methods: "GET,POST,PUT,DELETE"},
		{ActionType: "openapi", ActionValue: "/openapi/v1/uns/labels", Methods: "GET,POST"},
		{ActionType: "openapi", ActionValue: "/openapi/v1/uns/labels/**", Methods: "GET,PUT,DELETE"},
		{ActionType: "openapi", ActionValue: "/openapi/v1/uns/**", Methods: "GET,POST"},
		{ActionType: "openapi", ActionValue: "/openapi/v1/flows", Methods: "GET"},
		{ActionType: "openapi", ActionValue: "/openapi/v1/flows/**", Methods: "GET"},
		{ActionType: "openapi", ActionValue: "/openapi/v1/assets", Methods: "GET"},
		{ActionType: "openapi", ActionValue: "/openapi/v1/assets/**", Methods: "GET"},
		{ActionType: "openapi", ActionValue: "/openapi/v1/uns/:unsId/attachments", Methods: "GET"},
	}
}

func retiredSeedResourceKeys() []string {
	return []string{
		"project.view", "project.manage",
		"unsagent.view", "ai.gateway.config.manage",
		"anchor.dir", "anchor.model", "anchor.scene",
		"vision.camera.page", "vision.camera.read", "vision.camera.manage",
		"vision.algorithm.read", "vision.algorithm.manage", "vision.task.read", "vision.task.manage",
		"notebook.view", "notebook.read", "notebook.manage",
		"flow.read", "flow.manage", "flow.collection.page", "flow.event.page", "flow.runtime.view", "flow.runtime.edit",
		"app.management",
		"plugin.management",
		"app.space",
		"app.space.view",
		"appspace.view",
		"app.store",
		"app.market",
		"uns.browse",
		"uns.search",
		"uns.create",
		"uns.update",
		"uns.restore",
		"uns.attachment.read",
		"uns.attachment.manage",
		"uns.label.manage",
		"flow",
		"flow.list",
		"flow.write",
		"flow.event.view",
		"flow.event.edit",
		"dashboard.view",
		"openapi.mock.ping",
		"mock.permission",
		"system.portainer.view",
		"dev.tools",
		"dev.advanced.use",
		"gateway.route.write",
		"iam.role.write",
		"apikey.write",
		"system.menu.write",
		"iam.resource.write",
		"app.container.view",
		"app.container.manage",
		"system.emqx.view",
		"anchor",
	}
}

func operatorResourceIDs() []int64 {
	return []int64{
		620,
	}
}

func seedResources() []Resource {
	return []Resource{
		{ID: 197, ParentID: 0, ResourceKey: "home.page", Name: "Home", Type: 2, RoutePath: "/home", Icon: "Home", Sort: 5, UrlType: 1, OpenType: 0},
		{ID: 198, ParentID: 0, ResourceKey: "uns", Name: "UNS", Type: 1, Icon: "menu.tag.uns", Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 221, ParentID: 198, ResourceKey: "uns.overview.page", Name: "Overview", Type: 2, RoutePath: "/uns/overview", Icon: "Namespace", Sort: 5, UrlType: 1, OpenType: 0},
		{ID: 199, ParentID: 198, ResourceKey: "uns.page", Name: "Namespace", Type: 2, RoutePath: "/uns", Icon: "Namespace", Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 219, ParentID: 198, ResourceKey: "uns.dataQuality.page", Name: "Data Quality Alarms", Type: 2, RoutePath: "/uns/data-quality-alarms", Icon: "Logs", Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 220, ParentID: 198, ResourceKey: "uns.collections.page", Name: "Collections", Type: 2, RoutePath: "/collections", Icon: "Namespace", Sort: 15, UrlType: 1, OpenType: 0},
		{ID: 212, ParentID: 199, ResourceKey: "uns.read", Name: "UNS 读取", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 214, ParentID: 199, ResourceKey: "uns.write", Name: "UNS 写入", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 215, ParentID: 199, ResourceKey: "uns.delete", Name: "UNS 删除", Type: 3, Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 217, ParentID: 219, ResourceKey: "uns.dataQuality.read", Name: "数据质量查看", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 218, ParentID: 219, ResourceKey: "uns.dataQuality.manage", Name: "数据质量管理", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 210, ParentID: 199, ResourceKey: "uns.manage", Name: "UNS 管理", Type: 3, Sort: 80, UrlType: 1, OpenType: 0},
		{ID: 216, ParentID: 199, ResourceKey: "unsagent.view", Name: "UNS Agent", Type: 3, Sort: 90, UrlType: 1, OpenType: 0},
		{ID: 3300, ParentID: 199, ResourceKey: "uns.source", Name: "Source Read", Type: 3, Sort: 100, UrlType: 1, OpenType: 0},
		{ID: 3301, ParentID: 3300, ResourceKey: "uns.source.manage", Name: "Source Manage", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 3302, ParentID: 3300, ResourceKey: "uns.source.credentials", Name: "Source Credentials", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 3310, ParentID: 199, ResourceKey: "uns.stream", Name: "Stream Read", Type: 3, Sort: 110, UrlType: 1, OpenType: 0},
		{ID: 3311, ParentID: 3310, ResourceKey: "uns.stream.manage", Name: "Stream Draft and Test", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 3312, ParentID: 3310, ResourceKey: "uns.stream.deploy", Name: "Stream Deploy", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 3313, ParentID: 3310, ResourceKey: "uns.stream.execute", Name: "Stream Operation", Type: 3, Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 3350, ParentID: 199, ResourceKey: "control.read", Name: "Control Read", Type: 3, Sort: 130, UrlType: 1, OpenType: 0},
		{ID: 3351, ParentID: 3350, ResourceKey: "control.execute", Name: "Control Execute", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 3352, ParentID: 3350, ResourceKey: "control.write", Name: "Control Device Write", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 3354, ParentID: 3350, ResourceKey: "control.automate", Name: "Control Automatic Execution", Type: 3, Sort: 40, UrlType: 1, OpenType: 0},
		{ID: 3353, ParentID: 3350, ResourceKey: "control.manage", Name: "Control Manage", Type: 3, Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 3380, ParentID: 199, ResourceKey: "stream.requests.read", Name: "Stream Request Read", Type: 3, Sort: 140, UrlType: 1, OpenType: 0},
		{ID: 3381, ParentID: 3380, ResourceKey: "stream.requests.invoke", Name: "Stream Request Invoke", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 3382, ParentID: 3380, ResourceKey: "stream.requests.write", Name: "Stream Request Write", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 3383, ParentID: 3380, ResourceKey: "stream.requests.manage", Name: "Stream Request Manage", Type: 3, Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 3390, ParentID: 199, ResourceKey: "stream.messages.publish", Name: "Stream Message Publish", Type: 3, Sort: 150, UrlType: 1, OpenType: 0},
		{ID: 3391, ParentID: 3390, ResourceKey: "stream.messages.subscribe", Name: "Stream Message Subscribe", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 3330, ParentID: 200, ResourceKey: "uns.app.read", Name: "App", Type: 2, RoutePath: "/app", Icon: "Project", Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 3331, ParentID: 3330, ResourceKey: "uns.app.manage", Name: "UNS App Grants", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 3332, ParentID: 3330, ResourceKey: "uns.app.credentials", Name: "UNS App Credentials", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 298, ParentID: 0, ResourceKey: "flow", Name: "Flow", Type: 1, Icon: "menu.tag.connections", Sort: 15, UrlType: 1, OpenType: 0},
		{ID: 200, ParentID: 0, ResourceKey: "app", Name: "App", Type: 1, Icon: "menu.tag.appspace", Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 600, ParentID: 200, ResourceKey: "project.view", Name: "Project", Type: 2, RoutePath: "/project", Icon: "Project", Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 620, ParentID: 200, ResourceKey: "launchpad.view", Name: "Launchpad", Type: 2, RoutePath: "/launchpad", Icon: "Launchpad", Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 800, ParentID: 0, ResourceKey: "anchor.dir", Name: "Anchor", Type: 1, Icon: "menu.tag.anchor", Sort: 25, UrlType: 1, OpenType: 0},
		{ID: 810, ParentID: 800, ResourceKey: "anchor.model", Name: "Model Library", Type: 2, RoutePath: "/anchor/model", Icon: "AnchorModel", Sort: 10, UrlType: 2, OpenType: 0},
		{ID: 820, ParentID: 800, ResourceKey: "anchor.scene", Name: "Scenes", Type: 2, RoutePath: "/anchor/scene", Icon: "AnchorScene", Sort: 20, UrlType: 2, OpenType: 0},
		// Vision 并入 Analysis 分组(原独立 "Vision" 分组 900 移出清单即自动禁用)。
		{ID: 910, ParentID: 3000, ResourceKey: "vision.camera.page", Name: "Vision", Type: 2, RoutePath: "/vision", Icon: "VisionCamera", Sort: 5, UrlType: 1, OpenType: 0},
		{ID: 911, ParentID: 910, ResourceKey: "vision.camera.read", Name: "Camera Read", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 912, ParentID: 910, ResourceKey: "vision.camera.manage", Name: "Camera Manage", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 915, ParentID: 910, ResourceKey: "vision.algorithm.read", Name: "Algorithm Read", Type: 3, Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 916, ParentID: 910, ResourceKey: "vision.algorithm.manage", Name: "Algorithm Manage", Type: 3, Sort: 40, UrlType: 1, OpenType: 0},
		{ID: 917, ParentID: 910, ResourceKey: "vision.task.read", Name: "Task Read", Type: 3, Sort: 50, UrlType: 1, OpenType: 0},
		{ID: 918, ParentID: 910, ResourceKey: "vision.task.manage", Name: "Task Manage", Type: 3, Sort: 60, UrlType: 1, OpenType: 0},
		{ID: 3000, ParentID: 0, ResourceKey: "analysis", Name: "Analysis", Type: 1, Icon: "menu.tag.connections", Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 170, ParentID: 440, ResourceKey: "cluster.edge.manage", Name: "Edge Node", Type: 3, Icon: "SystemCascade", Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 171, ParentID: 170, ResourceKey: "fleet.account.view", Name: "Fleet Account View", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 172, ParentID: 170, ResourceKey: "fleet.account.manage", Name: "Fleet Account Manage", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 173, ParentID: 170, ResourceKey: "fleet.container.view", Name: "Fleet Container View", Type: 3, Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 174, ParentID: 170, ResourceKey: "fleet.container.log", Name: "Fleet Container Log", Type: 3, Sort: 40, UrlType: 1, OpenType: 0},
		{ID: 175, ParentID: 170, ResourceKey: "fleet.container.restart", Name: "Fleet Container Restart", Type: 3, Sort: 50, UrlType: 1, OpenType: 0},
		{ID: 177, ParentID: 440, ResourceKey: "fleet.connection.manage", Name: "Fleet Connection Manage", Type: 3, Sort: 70, UrlType: 1, OpenType: 0},
		{ID: 178, ParentID: 170, ResourceKey: "fleet.ssh.manage", Name: "Fleet SSH Operations", Type: 3, Sort: 80, UrlType: 1, OpenType: 0},
		{ID: 176, ParentID: 170, ResourceKey: "fleet.operation.view", Name: "Fleet Operation View", Type: 3, Sort: 60, UrlType: 1, OpenType: 0},
		{ID: 100, ParentID: 0, ResourceKey: "system", Name: "System", Type: 1, Icon: "menu.tag.system", Sort: 60, UrlType: 1, OpenType: 0},
		{ID: 140, ParentID: 100, ResourceKey: "gateway.route.manage", Name: "Routing Management", Type: 2, RoutePath: "/routing-management", Icon: "RoutingManagement", Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 141, ParentID: 140, ResourceKey: "gateway.route.create", Name: "Gateway Route Create", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 144, ParentID: 140, ResourceKey: "gateway.route.update", Name: "Gateway Route Update", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 142, ParentID: 140, ResourceKey: "gateway.route.delete", Name: "Gateway Route Delete", Type: 3, Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 143, ParentID: 140, ResourceKey: "gateway.route.test", Name: "Gateway Route Test", Type: 3, Sort: 40, UrlType: 1, OpenType: 0},
		{ID: 110, ParentID: 100, ResourceKey: "iam.user.view", Name: "User Management", Type: 2, RoutePath: "/account-management", Icon: "UserManagement", Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 111, ParentID: 110, ResourceKey: "iam.user.export.sensitive", Name: "User Sensitive Export", Type: 3, Sort: 5, UrlType: 1, OpenType: 0},
		{ID: 120, ParentID: 110, ResourceKey: "iam.role.view", Name: "Role Settings", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 121, ParentID: 120, ResourceKey: "iam.role.create", Name: "Role Create", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 123, ParentID: 120, ResourceKey: "iam.role.update", Name: "Role Update", Type: 3, Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 122, ParentID: 120, ResourceKey: "iam.role.delete", Name: "Role Delete", Type: 3, Sort: 40, UrlType: 1, OpenType: 0},
		{ID: 150, ParentID: 100, ResourceKey: "apikey.manage", Name: "API Key", Type: 2, RoutePath: "/OpenData", Icon: "OpenData", Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 151, ParentID: 150, ResourceKey: "apikey.create", Name: "API Key Create", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 153, ParentID: 150, ResourceKey: "apikey.update", Name: "API Key Update", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 152, ParentID: 150, ResourceKey: "apikey.delete", Name: "API Key Delete", Type: 3, Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 154, ParentID: 150, ResourceKey: "ai.gateway.config.manage", Name: "AI Provider Config", Type: 3, Sort: 40, UrlType: 1, OpenType: 0},
		{ID: 180, ParentID: 100, ResourceKey: "system.menu.config", Name: "Menu Config", Type: 2, RoutePath: "/MenuConfiguration", Icon: "MenuConfiguration", Sort: 40, UrlType: 1, OpenType: 0},
		{ID: 181, ParentID: 180, ResourceKey: "system.menu.create", Name: "Menu Config Create", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 183, ParentID: 180, ResourceKey: "system.menu.update", Name: "Menu Config Update", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 182, ParentID: 180, ResourceKey: "system.menu.delete", Name: "Menu Config Delete", Type: 3, Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 191, ParentID: 190, ResourceKey: "system.audit.export", Name: "Export Audit Logs", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 190, ParentID: 100, ResourceKey: "system.audit.log", Name: "Audit Log", Type: 2, RoutePath: "/audit-log", Icon: "Logs", Sort: 50, UrlType: 1, OpenType: 0},
		{ID: 185, ParentID: 100, ResourceKey: "system.session.manage", Name: "System Configuration", Type: 3, Sort: 45, UrlType: 1, OpenType: 0},
		{ID: 192, ParentID: 100, ResourceKey: "system.login.log", Name: "Login Log", Type: 2, RoutePath: "/login-log", Icon: "Authentication", Sort: 51, UrlType: 1, OpenType: 0},
		{ID: 130, ParentID: 100, ResourceKey: "iam.resource.view", Name: "Permission Management", Type: 2, RoutePath: "/PermissionManagement", Icon: "Authentication", Sort: 60, UrlType: 1, OpenType: 0},
		{ID: 131, ParentID: 130, ResourceKey: "iam.resource.create", Name: "Permission Create", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 133, ParentID: 130, ResourceKey: "iam.resource.update", Name: "Permission Update", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 132, ParentID: 130, ResourceKey: "iam.resource.delete", Name: "Permission Delete", Type: 3, Sort: 30, UrlType: 1, OpenType: 0},
		{ID: 160, ParentID: 100, ResourceKey: "oauth.client.manage", Name: "OAuth Client", Type: 2, RoutePath: "/oauth-clients", Icon: "Authentication", Sort: 55, UrlType: 1, OpenType: 0},
		{ID: 440, ParentID: 198, ResourceKey: "mqtt.auth.manage", Name: "MQTT Auth", Type: 2, RoutePath: "/mqtt-auth", Icon: "MqttBroker", Sort: 50, UrlType: 1, OpenType: 0},
		{ID: 441, ParentID: 440, ResourceKey: "mqtt.auth.write", Name: "MQTT Auth Write", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 442, ParentID: 440, ResourceKey: "mqtt.auth.delete", Name: "MQTT Auth Delete", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 500, ParentID: 0, ResourceKey: "openapi.base", Name: "OpenAPI Base", Type: 3, Sort: 50, UrlType: 1, OpenType: 0},
		{ID: 3340, ParentID: 500, ResourceKey: "platform.users.read", Name: "OpenAPI 平台用户读取", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 3341, ParentID: 500, ResourceKey: "platform.projects.members.read", Name: "OpenAPI 项目成员读取", Type: 3, Sort: 20, UrlType: 1, OpenType: 0},
		{ID: 610, ParentID: 600, ResourceKey: "project.manage", Name: "Project Manage", Type: 3, Sort: 10, UrlType: 1, OpenType: 0},
		{ID: 700, ParentID: 0, ResourceKey: "asset.file.upload", Name: "文件上传", Type: 3, Sort: 70, UrlType: 1, OpenType: 0},
		{ID: 710, ParentID: 0, ResourceKey: "asset.file.read", Name: "文件查看", Type: 3, Sort: 71, UrlType: 1, OpenType: 0},
		{ID: 720, ParentID: 0, ResourceKey: "asset.file.delete", Name: "文件删除", Type: 3, Sort: 72, UrlType: 1, OpenType: 0},
		{ID: 730, ParentID: 0, ResourceKey: "asset.file.download", Name: "文件下载", Type: 3, Sort: 73, UrlType: 1, OpenType: 0},
		// 消息通知（TASK-030）：管理端「通知管理」+ 用户侧「消息中心」入口
		{ID: 3200, ParentID: 0, ResourceKey: "notify", Name: "Notification", Type: 1, Icon: "Bell", Sort: 35, UrlType: 1, OpenType: 0},
		{ID: 3210, ParentID: 3200, ResourceKey: "notify.manage", Name: "通知管理", Type: 2, RoutePath: "/notification-manage", Icon: "Notification", Sort: 10, UrlType: 1, OpenType: 0},
		// OpenAPI 通知能力资源键：AuthenticateAPIKey 会对 owner IAM 资源键与 Key 资源键求交集，
		// 因此 notifications.send/read 必须存在于资源树（admin 默认全量），否则任何 Key 都无法通过
		{ID: 3230, ParentID: 3210, ResourceKey: "notifications.send", Name: "OpenAPI 发送通知", Type: 3, Sort: 40, UrlType: 1, OpenType: 0},
		{ID: 3231, ParentID: 3210, ResourceKey: "notifications.read", Name: "OpenAPI 查询通知", Type: 3, Sort: 50, UrlType: 1, OpenType: 0},
		{ID: 3220, ParentID: 3200, ResourceKey: "notify.message.center", Name: "消息中心", Type: 2, RoutePath: "/message-center", Icon: "MessageCenter", Sort: 20, UrlType: 1, OpenType: 0},
	}
}

func seedActions() []ResourceAction {
	actions := append(legacySeedActions(), unsV2Actions()...)
	actions = append(actions, platformV2Actions()...)
	actions = append(actions, fleetV2Actions()...)
	return append(actions,
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/apps/:appId/trash", Methods: "POST"},
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/business-role-assignments/update", Methods: "POST"},
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/business-role-options", Methods: "GET"},
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/business-roles", Methods: "GET"},
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/business-roles/create", Methods: "POST"},
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/business-roles/update", Methods: "POST"},
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/business-roles/delete", Methods: "POST"},
		ResourceAction{ResourceID: 3330, ActionType: "api", ActionValue: "/api/core/app-runtime/apps", Methods: "GET"},
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/preview", Methods: "POST"},
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/install", Methods: "POST"},
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/apps/:appId/actions", Methods: "POST"},
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/apps/:appId/logs", Methods: "GET"},
		ResourceAction{ResourceID: 3331, ActionType: "api", ActionValue: "/api/core/app-runtime/jobs/:jobId", Methods: "GET"},
		ResourceAction{ResourceID: 3330, ActionType: "gateway", ActionValue: "/apps/**", Methods: "GET,HEAD,POST,PUT,DELETE,PATCH,OPTIONS"},
		ResourceAction{ResourceID: 212, ActionType: "openapi", ActionValue: "/mcp", Methods: "GET,POST,DELETE"},
		ResourceAction{ResourceID: 150, ActionType: "api", ActionValue: "/api/core/mcp/info", Methods: "GET"},
	)
}
func legacySeedActions() []ResourceAction {
	return []ResourceAction{
		{ResourceID: 212, ActionType: "api", ActionValue: "/api/core/uns/metadata", Methods: "GET"},
		{ResourceID: 214, ActionType: "api", ActionValue: "/api/core/uns/metadata", Methods: "POST"},
		{ResourceID: 214, ActionType: "api", ActionValue: "/api/core/uns/metadata/:id/copy", Methods: "POST"},
		{ResourceID: 214, ActionType: "api", ActionValue: "/api/core/uns/collections", Methods: "POST"},
		{ResourceID: 214, ActionType: "api", ActionValue: "/api/core/uns/metadata/:id/state", Methods: "POST"},
		{ResourceID: 212, ActionType: "api", ActionValue: "/api/core/uns/metadata/:id/view", Methods: "GET"},
		{ResourceID: 212, ActionType: "api", ActionValue: "/api/core/uns/collections/:collectionId/records", Methods: "GET"},
		{ResourceID: 214, ActionType: "api", ActionValue: "/api/core/uns/collections/:collectionId/records", Methods: "POST"},
		{ResourceID: 215, ActionType: "api", ActionValue: "/api/core/uns/collections/:collectionId/records/:recordId", Methods: "DELETE"},
		{ResourceID: 212, ActionType: "api", ActionValue: "/api/core/uns/collections/:collectionId/samples", Methods: "GET"},
		{ResourceID: 214, ActionType: "api", ActionValue: "/api/core/uns/collections/:collectionId/samples", Methods: "POST"},
		{ResourceID: 214, ActionType: "ui", ActionValue: "button:Namespace.metadataWrite"},
		{ResourceID: 221, ActionType: "ui", ActionValue: "UnsOverview.title"},
		{ResourceID: 215, ActionType: "ui", ActionValue: "button:Namespace.metadataDelete"},
		{ResourceID: 3300, ActionType: "ui", ActionValue: "button:Namespace.sources"},
		{ResourceID: 3301, ActionType: "ui", ActionValue: "button:Namespace.sourceManage"},
		{ResourceID: 3302, ActionType: "ui", ActionValue: "button:Namespace.sourceCredentials"},
		{ResourceID: 3310, ActionType: "ui", ActionValue: "button:Namespace.streams"},
		{ResourceID: 3311, ActionType: "ui", ActionValue: "button:Namespace.streamManage"},
		{ResourceID: 3331, ActionType: "ui", ActionValue: "button:Namespace.appManage"},
		{ResourceID: 3332, ActionType: "ui", ActionValue: "button:Namespace.appCredentials"},
		{ResourceID: 3383, ActionType: "ui", ActionValue: "button:StreamRequests.manage"},
		{ResourceID: 3312, ActionType: "ui", ActionValue: "button:Namespace.streamDeploy"},
		{ResourceID: 3313, ActionType: "ui", ActionValue: "button:Namespace.streamExecute"},
		{ResourceID: 3300, ActionType: "api", ActionValue: "/api/core/uns/source-protocols", Methods: "GET"},
		{ResourceID: 3300, ActionType: "api", ActionValue: "/api/core/uns/sources", Methods: "GET"},
		{ResourceID: 3300, ActionType: "api", ActionValue: "/api/core/uns/sources/:sourceId", Methods: "GET"},
		{ResourceID: 3300, ActionType: "api", ActionValue: "/api/core/uns/sources/:sourceId/impact", Methods: "GET"},
		{ResourceID: 3301, ActionType: "api", ActionValue: "/api/core/uns/sources", Methods: "POST"},
		{ResourceID: 3301, ActionType: "api", ActionValue: "/api/core/uns/sources/:sourceId", Methods: "PUT,DELETE"},
		{ResourceID: 3301, ActionType: "api", ActionValue: "/api/core/uns/sources/:sourceId/discover", Methods: "POST"},
		{ResourceID: 3301, ActionType: "api", ActionValue: "/api/core/uns/sources/probe", Methods: "POST"},
		{ResourceID: 3301, ActionType: "api", ActionValue: "/api/core/uns/sources/usb-hid/devices", Methods: "POST"},
		{ResourceID: 3301, ActionType: "api", ActionValue: "/api/core/uns/sources/usb-hid/preview", Methods: "POST"},
		{ResourceID: 3302, ActionType: "api", ActionValue: "/api/core/uns/sources/:sourceId/credentials", Methods: "PUT"},
		{ResourceID: 3302, ActionType: "api", ActionValue: "/api/core/uns/sources/:sourceId/oauth/start", Methods: "POST"},
		{ResourceID: 3302, ActionType: "api", ActionValue: "/api/core/uns/sources/:sourceId/oauth/complete", Methods: "POST"},

		{ResourceID: 3310, ActionType: "api", ActionValue: "/api/core/uns/streams", Methods: "GET"},
		{ResourceID: 3310, ActionType: "api", ActionValue: "/api/core/uns/streams/:streamId", Methods: "GET"},
		{ResourceID: 3310, ActionType: "api", ActionValue: "/api/core/uns/streams/:streamId/versions", Methods: "GET"},
		{ResourceID: 3310, ActionType: "api", ActionValue: "/api/core/uns/stream-runs", Methods: "GET"},
		{ResourceID: 3310, ActionType: "api", ActionValue: "/api/core/uns/stream-runs/:runId", Methods: "GET"},
		{ResourceID: 3310, ActionType: "api", ActionValue: "/api/core/uns/topics/:topicId/streams", Methods: "GET"},
		{ResourceID: 3311, ActionType: "api", ActionValue: "/api/core/uns/streams", Methods: "POST"},
		{ResourceID: 3311, ActionType: "api", ActionValue: "/api/core/uns/streams/:streamId", Methods: "PUT,DELETE"},
		{ResourceID: 3311, ActionType: "api", ActionValue: "/api/core/uns/streams/validate", Methods: "POST"},
		{ResourceID: 3311, ActionType: "api", ActionValue: "/api/core/uns/streams/test", Methods: "POST"},
		{ResourceID: 3312, ActionType: "api", ActionValue: "/api/core/uns/streams/:streamId/deploy", Methods: "POST"},
		{ResourceID: 3312, ActionType: "api", ActionValue: "/api/core/uns/streams/:streamId/disable", Methods: "POST"},
		{ResourceID: 3312, ActionType: "api", ActionValue: "/api/core/uns/streams/:streamId/versions/:versionId/activate", Methods: "POST"},
		{ResourceID: 3313, ActionType: "api", ActionValue: "/api/core/uns/streams/:streamId/invoke", Methods: "POST"},
		{ResourceID: 3350, ActionType: "openapi", ActionValue: "/openapi/v1/controls/discover", Methods: "POST"},
		{ResourceID: 3350, ActionType: "ui", ActionValue: "button:Control.read"},
		{ResourceID: 3351, ActionType: "ui", ActionValue: "button:Control.execute"},
		{ResourceID: 3352, ActionType: "ui", ActionValue: "button:Control.write"},
		{ResourceID: 3354, ActionType: "ui", ActionValue: "button:Control.automate"},
		{ResourceID: 3353, ActionType: "ui", ActionValue: "button:Control.manage"},
		{ResourceID: 3350, ActionType: "openapi", ActionValue: "/openapi/v1/controls/:controlCode/describe", Methods: "POST"},
		{ResourceID: 3350, ActionType: "openapi", ActionValue: "/openapi/v1/controls/:controlCode/invocations/get", Methods: "POST"},
		{ResourceID: 3351, ActionType: "openapi", ActionValue: "/openapi/v1/controls/:controlCode/invoke", Methods: "POST"},
		{ResourceID: 3380, ActionType: "openapi", ActionValue: "/openapi/v2/stream-requests/discover", Methods: "POST"},
		{ResourceID: 3380, ActionType: "openapi", ActionValue: "/openapi/v2/stream-requests/:requestCode/describe", Methods: "POST"},
		{ResourceID: 3381, ActionType: "openapi", ActionValue: "/openapi/v2/stream-requests/:requestCode/invoke", Methods: "POST"},
		{ResourceID: 3380, ActionType: "api", ActionValue: "/api/core/v2/stream-requests/discover", Methods: "POST"},
		{ResourceID: 3380, ActionType: "api", ActionValue: "/api/core/v2/stream-requests/:requestCode/describe", Methods: "POST"},
		{ResourceID: 3381, ActionType: "api", ActionValue: "/api/core/v2/stream-requests/:requestCode/invoke", Methods: "POST"},
		{ResourceID: 3300, ActionType: "openapi", ActionValue: "/openapi/v1/uns/source-protocols/list", Methods: "POST"},
		{ResourceID: 3300, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/list", Methods: "POST"},
		{ResourceID: 3300, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/:sourceId/detail", Methods: "POST"},
		{ResourceID: 3300, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/:sourceId/impact", Methods: "POST"},
		{ResourceID: 3301, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/create", Methods: "POST"},
		{ResourceID: 3301, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/:sourceId/update", Methods: "POST"},
		{ResourceID: 3301, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/:sourceId/delete", Methods: "POST"},
		{ResourceID: 3301, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/validate", Methods: "POST"},
		{ResourceID: 3301, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/probe", Methods: "POST"},
		{ResourceID: 3301, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/:sourceId/discover", Methods: "POST"},
		{ResourceID: 3302, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/:sourceId/credentials", Methods: "POST"},
		{ResourceID: 3302, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/:sourceId/oauth/start", Methods: "POST"},
		{ResourceID: 3302, ActionType: "openapi", ActionValue: "/openapi/v1/uns/sources/:sourceId/oauth/complete", Methods: "POST"},
		{ResourceID: 3310, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/list", Methods: "POST"},
		{ResourceID: 3310, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/:streamId/detail", Methods: "POST"},
		{ResourceID: 3310, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/:streamId/versions/list", Methods: "POST"},
		{ResourceID: 3310, ActionType: "openapi", ActionValue: "/openapi/v1/uns/stream-runs/list", Methods: "POST"},
		{ResourceID: 3310, ActionType: "openapi", ActionValue: "/openapi/v1/uns/stream-runs/:runId/detail", Methods: "POST"},
		{ResourceID: 3311, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/create", Methods: "POST"},
		{ResourceID: 3311, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/:streamId/update", Methods: "POST"},
		{ResourceID: 3311, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/:streamId/delete", Methods: "POST"},
		{ResourceID: 3311, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/validate", Methods: "POST"},
		{ResourceID: 3311, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/test", Methods: "POST"},
		{ResourceID: 3312, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/:streamId/deploy", Methods: "POST"},
		{ResourceID: 3312, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/:streamId/disable", Methods: "POST"},
		{ResourceID: 3312, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/:streamId/versions/:versionId/activate", Methods: "POST"},
		{ResourceID: 3313, ActionType: "openapi", ActionValue: "/openapi/v1/uns/streams/:streamId/invoke", Methods: "POST"},
		{ResourceID: 210, ActionType: "openapi", ActionValue: "/openapi/v1/uns/topics/create", Methods: "POST"},
		{ResourceID: 210, ActionType: "openapi", ActionValue: "/openapi/v1/uns/topics/:id/update", Methods: "POST"},
		{ResourceID: 210, ActionType: "openapi", ActionValue: "/openapi/v1/uns/topics/validate", Methods: "POST"},
		{ResourceID: 212, ActionType: "openapi", ActionValue: "/openapi/v1/uns/topics/list", Methods: "POST"},
		{ResourceID: 212, ActionType: "openapi", ActionValue: "/openapi/v1/uns/topics/:id/detail", Methods: "POST"},
		{ResourceID: 212, ActionType: "openapi", ActionValue: "/openapi/v1/uns/metadata/list", Methods: "POST"},
		{ResourceID: 210, ActionType: "openapi", ActionValue: "/openapi/v1/uns/metadata/save", Methods: "POST"},
		{ResourceID: 212, ActionType: "openapi", ActionValue: "/openapi/v1/uns/metadata/:id/view", Methods: "POST"},
		{ResourceID: 210, ActionType: "openapi", ActionValue: "/openapi/v1/uns/metadata/:id/state", Methods: "POST"},
		{ResourceID: 210, ActionType: "openapi", ActionValue: "/openapi/v1/uns/collections/create", Methods: "POST"},
		{ResourceID: 212, ActionType: "openapi", ActionValue: "/openapi/v1/uns/collections/:collectionId/records/list", Methods: "POST"},
		{ResourceID: 212, ActionType: "openapi", ActionValue: "/openapi/v1/uns/collections/:collectionId/samples/list", Methods: "POST"},
		{ResourceID: 3301, ActionType: "api", ActionValue: "/api/core/uns/sources/validate", Methods: "POST"},
		{ResourceID: 210, ActionType: "api", ActionValue: "/api/core/uns/topics/validate", Methods: "POST"},
		{ResourceID: 110, ActionType: "ui", ActionValue: "menu.system.users"},
		{ResourceID: 110, ActionType: "ui", ActionValue: "button:UserManagement.add"},
		{ResourceID: 110, ActionType: "ui", ActionValue: "button:UserManagement.edit"},
		{ResourceID: 110, ActionType: "ui", ActionValue: "button:UserManagement.enable"},
		{ResourceID: 110, ActionType: "ui", ActionValue: "button:UserManagement.disable"},
		{ResourceID: 110, ActionType: "ui", ActionValue: "button:UserManagement.resetPassword"},
		{ResourceID: 110, ActionType: "ui", ActionValue: "button:UserManagement.delete"},
		{ResourceID: 110, ActionType: "ui", ActionValue: "button:UserManagement.import"},
		{ResourceID: 110, ActionType: "ui", ActionValue: "button:UserManagement.export"},
		{ResourceID: 110, ActionType: "ui", ActionValue: "button:UserManagement.userGroups"},
		{ResourceID: 111, ActionType: "ui", ActionValue: "button:UserManagement.exportSensitive"},
		{ResourceID: 120, ActionType: "ui", ActionValue: "button:UserManagement.role_setting"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/users", Methods: "GET"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/users", Methods: "POST"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/users/batch", Methods: "PUT"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/users/**", Methods: "PUT"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/users/**", Methods: "POST"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/users/**", Methods: "DELETE"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/user-groups", Methods: "GET,POST"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/user-groups/**", Methods: "GET,PUT,DELETE"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/users/import", Methods: "POST"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/users/import-template", Methods: "POST"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/users/export", Methods: "POST"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/users/export/preview", Methods: "POST"},
		{ResourceID: 110, ActionType: "api", ActionValue: "/api/core/iam/user-jobs/:jobKey", Methods: "GET"},
		{ResourceID: 610, ActionType: "api", ActionValue: "/api/core/projects/:projectId/member-groups", Methods: "GET,POST"},
		{ResourceID: 610, ActionType: "api", ActionValue: "/api/core/projects/:projectId/member-groups/:projectGroupId", Methods: "PUT,DELETE"},
		{ResourceID: 120, ActionType: "ui", ActionValue: "menu.system.roles"},
		{ResourceID: 120, ActionType: "api", ActionValue: "/api/core/iam/roles", Methods: "GET"},
		{ResourceID: 121, ActionType: "api", ActionValue: "/api/core/iam/roles", Methods: "POST"},
		{ResourceID: 123, ActionType: "api", ActionValue: "/api/core/iam/roles/**", Methods: "PUT"},
		{ResourceID: 122, ActionType: "api", ActionValue: "/api/core/iam/roles/**", Methods: "DELETE"},
		{ResourceID: 130, ActionType: "ui", ActionValue: "menu.system.permissions"},
		{ResourceID: 130, ActionType: "api", ActionValue: "/api/core/iam/resources", Methods: "GET"},
		{ResourceID: 130, ActionType: "api", ActionValue: "/api/core/iam/menus", Methods: "GET"},
		{ResourceID: 140, ActionType: "ui", ActionValue: "menu.system.gateway"},
		{ResourceID: 140, ActionType: "api", ActionValue: "/api/core/gateway/routes", Methods: "GET"},
		{ResourceID: 141, ActionType: "ui", ActionValue: "button:RoutingManagement.add"},
		{ResourceID: 141, ActionType: "api", ActionValue: "/api/core/gateway/routes", Methods: "POST"},
		{ResourceID: 144, ActionType: "ui", ActionValue: "button:RoutingManagement.edit"},
		{ResourceID: 144, ActionType: "api", ActionValue: "/api/core/gateway/routes/**", Methods: "PUT"},
		{ResourceID: 142, ActionType: "ui", ActionValue: "button:RoutingManagement.delete"},
		{ResourceID: 142, ActionType: "api", ActionValue: "/api/core/gateway/routes/**", Methods: "DELETE"},
		{ResourceID: 143, ActionType: "ui", ActionValue: "button:RoutingManagement.test"},
		{ResourceID: 143, ActionType: "api", ActionValue: "/api/core/gateway/routes/**", Methods: "POST"},
		{ResourceID: 150, ActionType: "ui", ActionValue: "menu.system.apikey"},
		{ResourceID: 150, ActionType: "api", ActionValue: "/api/core/common/api-keys", Methods: "GET"},
		{ResourceID: 151, ActionType: "ui", ActionValue: "button:dataopen.addKey"},
		{ResourceID: 151, ActionType: "api", ActionValue: "/api/core/common/api-keys", Methods: "POST"},
		{ResourceID: 153, ActionType: "ui", ActionValue: "button:dataopen.enable"},
		{ResourceID: 153, ActionType: "ui", ActionValue: "button:dataopen.disable"},
		{ResourceID: 153, ActionType: "api", ActionValue: "/api/core/common/api-keys/**", Methods: "PUT"},
		{ResourceID: 152, ActionType: "ui", ActionValue: "button:dataopen.delete"},
		{ResourceID: 152, ActionType: "api", ActionValue: "/api/core/common/api-keys/**", Methods: "DELETE"},
		{ResourceID: 154, ActionType: "api", ActionValue: "/api/core/ai/configs", Methods: "GET,POST"},
		{ResourceID: 154, ActionType: "api", ActionValue: "/api/core/ai/configs/test", Methods: "POST"},
		{ResourceID: 154, ActionType: "api", ActionValue: "/api/core/ai/configs/test-embedding", Methods: "POST"},
		{ResourceID: 154, ActionType: "api", ActionValue: "/api/core/ai/configs/**", Methods: "PUT,DELETE"},
		{ResourceID: 154, ActionType: "api", ActionValue: "/api/core/ai/trusted-hosts", Methods: "GET,PUT"},
		{ResourceID: 154, ActionType: "ui", ActionValue: "OpenData.aiConfigAdd"},
		{ResourceID: 154, ActionType: "ui", ActionValue: "OpenData.aiConfigEdit"},
		{ResourceID: 154, ActionType: "ui", ActionValue: "OpenData.aiConfigDelete"},
		{ResourceID: 154, ActionType: "ui", ActionValue: "OpenData.aiTrustedHostsManage"},
		{ResourceID: 160, ActionType: "ui", ActionValue: "menu.system.oauth"},
		{ResourceID: 180, ActionType: "ui", ActionValue: "MenuConfiguration.menu"},
		{ResourceID: 181, ActionType: "ui", ActionValue: "button:MenuConfiguration.addMenu"},
		{ResourceID: 181, ActionType: "api", ActionValue: "/api/core/iam/resources", Methods: "POST"},
		{ResourceID: 183, ActionType: "ui", ActionValue: "button:MenuConfiguration.editMenu"},
		{ResourceID: 183, ActionType: "ui", ActionValue: "button:MenuConfiguration.enabledMenu"},
		{ResourceID: 183, ActionType: "api", ActionValue: "/api/core/iam/resources/**", Methods: "PUT"},
		{ResourceID: 182, ActionType: "ui", ActionValue: "button:MenuConfiguration.deleteMenu"},
		{ResourceID: 191, ActionType: "ui", ActionValue: "button:AuditLog.export"},
		{ResourceID: 191, ActionType: "api", ActionValue: "/api/core/audit-logs/export", Methods: "POST"},
		{ResourceID: 190, ActionType: "api", ActionValue: "/api/core/audit-logs/filter-options", Methods: "POST"},
		{ResourceID: 190, ActionType: "ui", ActionValue: "route.auditLog"},
		{ResourceID: 190, ActionType: "api", ActionValue: "/api/core/audit-logs/page", Methods: "POST"},
		{ResourceID: 190, ActionType: "api", ActionValue: "/api/core/audit-logs/**", Methods: "GET"},
		{ResourceID: 185, ActionType: "api", ActionValue: "/api/core/system/login-session-duration", Methods: "GET,PUT"},
		{ResourceID: 192, ActionType: "ui", ActionValue: "route.loginLog"},
		{ResourceID: 192, ActionType: "api", ActionValue: "/api/core/login-logs/page", Methods: "POST"},
		{ResourceID: 160, ActionType: "api", ActionValue: "/api/core/iam/oauth-clients", Methods: "GET,POST"},
		{ResourceID: 160, ActionType: "api", ActionValue: "/api/core/iam/oauth-clients/**", Methods: "PUT,DELETE"},
		{ResourceID: 170, ActionType: "ui", ActionValue: "menu.system.cluster"},
		{ResourceID: 178, ActionType: "ui", ActionValue: "button:FleetNode.ssh"},
		{ResourceID: 177, ActionType: "ui", ActionValue: "button:FleetConnection.configure"},
		{ResourceID: 170, ActionType: "ui", ActionValue: "button:FleetNode.add"},
		{ResourceID: 170, ActionType: "ui", ActionValue: "button:FleetNode.edit"},
		{ResourceID: 170, ActionType: "ui", ActionValue: "button:FleetNode.reissueToken"},
		{ResourceID: 170, ActionType: "ui", ActionValue: "button:FleetNode.delete"},
		{ResourceID: 170, ActionType: "ui", ActionValue: "button:FleetNode.restore"},
		{ResourceID: 170, ActionType: "api", ActionValue: "/api/core/cluster/nodes", Methods: "GET"},
		{ResourceID: 170, ActionType: "api", ActionValue: "/api/core/fleet/nodes", Methods: "GET,POST"},
		{ResourceID: 170, ActionType: "api", ActionValue: "/api/core/fleet/nodes/:nodeID", Methods: "GET,PUT,DELETE"},
		{ResourceID: 170, ActionType: "api", ActionValue: "/api/core/fleet/nodes/:nodeID/restore", Methods: "POST"},
		{ResourceID: 170, ActionType: "api", ActionValue: "/api/core/fleet/nodes/:nodeID/uns-scopes", Methods: "GET"},
		{ResourceID: 171, ActionType: "api", ActionValue: "/api/core/fleet/nodes/:nodeID/accounts", Methods: "GET"},
		{ResourceID: 172, ActionType: "api", ActionValue: "/api/core/fleet/nodes/:nodeID/accounts", Methods: "PUT"},
		{ResourceID: 172, ActionType: "ui", ActionValue: "button:FleetNode.accounts"},
		{ResourceID: 173, ActionType: "api", ActionValue: "/api/core/fleet/current", Methods: "GET"},
		{ResourceID: 173, ActionType: "api", ActionValue: "/api/core/fleet/current/connection", Methods: "PUT"},
		{ResourceID: 173, ActionType: "api", ActionValue: "/api/core/system/runtime-metrics", Methods: "GET"},
		{ResourceID: 173, ActionType: "api", ActionValue: "/api/core/fleet/nodes/:nodeID/containers", Methods: "GET"},
		{ResourceID: 173, ActionType: "api", ActionValue: "/api/core/fleet/nodes/:nodeID/containers/:containerID", Methods: "GET"},
		{ResourceID: 176, ActionType: "api", ActionValue: "/api/core/fleet/nodes/:nodeID/operations", Methods: "GET"},
		{ResourceID: 176, ActionType: "api", ActionValue: "/api/core/fleet/nodes/:nodeID/operations/:operationID", Methods: "GET"},
		{ResourceID: 174, ActionType: "api", ActionValue: "/api/core/fleet/nodes/:nodeID/containers/:containerID/logs", Methods: "POST"},
		{ResourceID: 175, ActionType: "api", ActionValue: "/api/core/fleet/nodes/:nodeID/containers/:containerID/restart", Methods: "POST"},
		{ResourceID: 173, ActionType: "ui", ActionValue: "button:FleetRuntime.view"},
		{ResourceID: 173, ActionType: "ui", ActionValue: "button:FleetRuntime.connect"},
		{ResourceID: 173, ActionType: "api", ActionValue: "/api/core/fleet/current/containers", Methods: "GET"},
		{ResourceID: 173, ActionType: "api", ActionValue: "/api/core/fleet/current/containers/:containerID", Methods: "GET"},
		{ResourceID: 174, ActionType: "api", ActionValue: "/api/core/fleet/current/containers/:containerID/logs", Methods: "POST"},
		{ResourceID: 174, ActionType: "ui", ActionValue: "button:FleetContainer.logs"},
		{ResourceID: 175, ActionType: "api", ActionValue: "/api/core/fleet/current/containers/:containerID/restart", Methods: "POST"},
		{ResourceID: 175, ActionType: "ui", ActionValue: "button:FleetContainer.restart"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/sample-history", Methods: "POST"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/config", Methods: "GET"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/connect", Methods: "POST"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/force-sync", Methods: "POST"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/user-sync", Methods: "POST"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/disconnect", Methods: "POST"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/cluster/test-connect", Methods: "POST"},
		{ResourceID: 170, ActionType: "api", ActionValue: "/api/core/cluster/tokens", Methods: "POST"},
		{ResourceID: 170, ActionType: "api", ActionValue: "/api/core/cluster/scopes", Methods: "GET"},
		{ResourceID: 170, ActionType: "api", ActionValue: "/api/core/cluster/outbox", Methods: "GET"},
		{ResourceID: 170, ActionType: "api", ActionValue: "/api/core/cluster/sync-logs", Methods: "GET"},
		{ResourceID: 130, ActionType: "api", ActionValue: "/api/core/iam/resources/**", Methods: "GET"},
		{ResourceID: 131, ActionType: "api", ActionValue: "/api/core/iam/resources/**", Methods: "POST"},
		{ResourceID: 133, ActionType: "api", ActionValue: "/api/core/iam/resources/**", Methods: "PUT"},
		{ResourceID: 132, ActionType: "api", ActionValue: "/api/core/iam/resources/**", Methods: "DELETE"},
		{ResourceID: 197, ActionType: "ui", ActionValue: "menu.home"},
		{ResourceID: 197, ActionType: "api", ActionValue: "/api/core/uns/newMsg", Methods: "GET"},
		{ResourceID: 197, ActionType: "api", ActionValue: "/api/core/uns/dashboard", Methods: "GET"},
		{ResourceID: 199, ActionType: "ui", ActionValue: "menu.uns"},
		{ResourceID: 199, ActionType: "api", ActionValue: "/api/core/uns/nodes", Methods: "GET"},
		{ResourceID: 199, ActionType: "api", ActionValue: "/api/core/uns/newMsg", Methods: "GET"},
		{ResourceID: 199, ActionType: "api", ActionValue: "/api/core/uns/dashboard", Methods: "GET"},
		{ResourceID: 210, ActionType: "ui", ActionValue: "button:Namespace.uns_import"},
		{ResourceID: 210, ActionType: "ui", ActionValue: "button:Namespace.uns_export"},
		{ResourceID: 210, ActionType: "api", ActionValue: "/api/core/uns/import", Methods: "POST"},
		{ResourceID: 210, ActionType: "api", ActionValue: "/api/core/uns/export", Methods: "POST"},
		{ResourceID: 210, ActionType: "api", ActionValue: "/api/core/uns/export/global", Methods: "POST"},
		{ResourceID: 210, ActionType: "api", ActionValue: "/api/core/uns/import-jobs", Methods: "POST"},
		{ResourceID: 210, ActionType: "api", ActionValue: "/api/core/uns/import-jobs/**", Methods: "GET"},
		{ResourceID: 210, ActionType: "api", ActionValue: "/api/core/uns/export-jobs", Methods: "POST"},
		{ResourceID: 210, ActionType: "api", ActionValue: "/api/core/uns/export-jobs/**", Methods: "GET"},
		{ResourceID: 212, ActionType: "openapi", ActionValue: "/openapi/v1/uns/read", Methods: "POST"},
		{ResourceID: 212, ActionType: "openapi", ActionValue: "/openapi/v1/uns/browse", Methods: "POST"},
		{ResourceID: 212, ActionType: "openapi", ActionValue: "/openapi/v1/uns/search", Methods: "POST"},
		{ResourceID: 212, ActionType: "openapi", ActionValue: "/openapi/v1/uns/history", Methods: "POST"},
		{ResourceID: 212, ActionType: "openapi", ActionValue: "/openapi/v1/uns/nodes", Methods: "POST"},
		{ResourceID: 210, ActionType: "ui", ActionValue: "button:Namespace.folder_detail"},
		{ResourceID: 210, ActionType: "ui", ActionValue: "button:Namespace.file_detail"},
		{ResourceID: 212, ActionType: "api", ActionValue: "/api/core/uns/nodes/**", Methods: "GET"},
		{ResourceID: 212, ActionType: "api", ActionValue: "/api/core/uns/recycle", Methods: "GET"},
		{ResourceID: 212, ActionType: "api", ActionValue: "/api/core/uns/labels", Methods: "GET"},
		{ResourceID: 212, ActionType: "api", ActionValue: "/api/core/uns/labels/**", Methods: "GET"},
		{ResourceID: 217, ActionType: "api", ActionValue: "/api/core/uns/data-quality/policies/:unsId", Methods: "GET"},
		{ResourceID: 217, ActionType: "api", ActionValue: "/api/core/uns/data-quality/status", Methods: "GET"},
		{ResourceID: 217, ActionType: "api", ActionValue: "/api/core/uns/data-quality/active-summary", Methods: "GET"},
		{ResourceID: 217, ActionType: "api", ActionValue: "/api/core/uns/data-quality/issues", Methods: "GET"},
		{ResourceID: 218, ActionType: "api", ActionValue: "/api/core/uns/data-quality/policies/:unsId", Methods: "PUT"},
		{ResourceID: 218, ActionType: "api", ActionValue: "/api/core/uns/data-quality/runs/:unsId", Methods: "POST"},
		{ResourceID: 218, ActionType: "api", ActionValue: "/api/core/uns/data-quality/issues/:id/ack", Methods: "POST"},
		{ResourceID: 214, ActionType: "openapi", ActionValue: "/openapi/v1/uns/write", Methods: "POST"},
		{ResourceID: 210, ActionType: "openapi", ActionValue: "/openapi/v1/uns/create", Methods: "POST"},
		{ResourceID: 210, ActionType: "openapi", ActionValue: "/openapi/v1/uns/update", Methods: "POST"},
		{ResourceID: 210, ActionType: "openapi", ActionValue: "/openapi/v1/uns/restore", Methods: "POST"},
		{ResourceID: 215, ActionType: "openapi", ActionValue: "/openapi/v1/uns/delete", Methods: "POST"},
		{ResourceID: 222, ActionType: "api", ActionValue: "/api/core/uns/labels", Methods: "POST"},
		{ResourceID: 222, ActionType: "api", ActionValue: "/api/core/uns/labels/**", Methods: "PUT,DELETE"},
		{ResourceID: 210, ActionType: "api", ActionValue: "/api/core/uns/nodes", Methods: "POST"},
		{ResourceID: 210, ActionType: "ui", ActionValue: "button:Namespace.folder_add"},
		{ResourceID: 210, ActionType: "ui", ActionValue: "button:Namespace.file_add"},
		{ResourceID: 210, ActionType: "api", ActionValue: "/api/core/uns/nodes/**", Methods: "PUT"},
		{ResourceID: 210, ActionType: "ui", ActionValue: "button:Namespace.folder_copy"},
		{ResourceID: 210, ActionType: "ui", ActionValue: "button:Namespace.folder_paste"},
		{ResourceID: 210, ActionType: "ui", ActionValue: "button:Namespace.file_copy"},
		{ResourceID: 210, ActionType: "ui", ActionValue: "button:Namespace.file_paste"},
		{ResourceID: 215, ActionType: "api", ActionValue: "/api/core/uns/nodes/**", Methods: "DELETE"},
		{ResourceID: 215, ActionType: "ui", ActionValue: "button:Namespace.folder_delete"},
		{ResourceID: 215, ActionType: "ui", ActionValue: "button:Namespace.file_delete"},
		{ResourceID: 210, ActionType: "api", ActionValue: "/api/core/uns/nodes/**", Methods: "POST"},
		{ResourceID: 216, ActionType: "ui", ActionValue: "button:Namespace.uns_agent_view"},
		{ResourceID: 216, ActionType: "gateway", ActionValue: "/agent-svc/**", Methods: "GET,POST,PUT,DELETE,PATCH"},
		{ResourceID: 440, ActionType: "ui", ActionValue: "menu.mqttAuth"},
		{ResourceID: 440, ActionType: "api", ActionValue: "/api/core/mqtt/auth-clients", Methods: "GET"},
		{ResourceID: 441, ActionType: "ui", ActionValue: "button:MqttAuth.add"},
		{ResourceID: 441, ActionType: "ui", ActionValue: "button:MqttAuth.reset"},
		{ResourceID: 441, ActionType: "ui", ActionValue: "button:MqttAuth.enable"},
		{ResourceID: 441, ActionType: "ui", ActionValue: "button:MqttAuth.disable"},
		{ResourceID: 441, ActionType: "api", ActionValue: "/api/core/mqtt/auth-clients", Methods: "POST"},
		{ResourceID: 441, ActionType: "api", ActionValue: "/api/core/mqtt/auth-clients/**", Methods: "POST"},
		{ResourceID: 441, ActionType: "openapi", ActionValue: "/openapi/v1/mqtt-auth/create", Methods: "POST"},
		{ResourceID: 442, ActionType: "ui", ActionValue: "button:MqttAuth.delete"},
		{ResourceID: 442, ActionType: "api", ActionValue: "/api/core/mqtt/auth-clients/**", Methods: "DELETE"},
		{ResourceID: 442, ActionType: "openapi", ActionValue: "/openapi/v1/mqtt-auth/delete", Methods: "POST"},
		{ResourceID: 500, ActionType: "openapi", ActionValue: "/openapi/v1/auth/whoami", Methods: "POST"},
		{ResourceID: 500, ActionType: "openapi", ActionValue: "/openapi/v1/info", Methods: "POST"},
		{ResourceID: 600, ActionType: "api", ActionValue: "/api/core/projects", Methods: "GET"},
		{ResourceID: 600, ActionType: "ui", ActionValue: "menu.projects"},
		{ResourceID: 600, ActionType: "api", ActionValue: "/api/core/projects/**", Methods: "GET"},
		{ResourceID: 610, ActionType: "api", ActionValue: "/api/core/projects", Methods: "POST"},
		{ResourceID: 610, ActionType: "api", ActionValue: "/api/core/projects/**", Methods: "POST,PUT,DELETE"},
		{ResourceID: 610, ActionType: "ui", ActionValue: "button:Project.add"},
		{ResourceID: 610, ActionType: "ui", ActionValue: "button:Project.edit"},
		{ResourceID: 610, ActionType: "ui", ActionValue: "button:Project.delete"},
		{ResourceID: 610, ActionType: "ui", ActionValue: "button:Project.start"},
		{ResourceID: 610, ActionType: "ui", ActionValue: "button:Project.stop"},
		{ResourceID: 620, ActionType: "ui", ActionValue: "Launchpad.title"},
		{ResourceID: 620, ActionType: "api", ActionValue: "/api/core/launchpad/projects", Methods: "GET"},
		{ResourceID: 620, ActionType: "api", ActionValue: "/api/core/launchpad/projects/**", Methods: "GET"},
		{ResourceID: 620, ActionType: "api", ActionValue: "/api/core/launchpad/:projectName/getMembers", Methods: "POST"},
		{ResourceID: 620, ActionType: "api", ActionValue: "/api/core/launchpad/favorites", Methods: "GET,POST,DELETE"},
		{ResourceID: 620, ActionType: "api", ActionValue: "/api/core/launchpad/recents", Methods: "GET"},
		{ResourceID: 620, ActionType: "api", ActionValue: "/api/core/launchpad/apps/:appId/view", Methods: "POST"},
		// Launchpad 卡片的 App 图标与封面按 App 取图（见 domain/launchpad service.go 的
		// launchpadAppMediaURL）。这两条挂在 launchpad.view 上：operator 只有 620 这一个资源，
		// 不给的话图片一律 403 破图。
		// 不能改用 /api/core/assets/:fileId/download —— 那条路径不做归属校验，授权只看路由级
		// 权限，开给 Launchpad 等于允许按 id 猜测下载任意资产（PR #525 的 Codex P1）。
		{ResourceID: 620, ActionType: "api", ActionValue: "/api/core/launchpad/apps/:appId/icon", Methods: "GET"},
		{ResourceID: 620, ActionType: "api", ActionValue: "/api/core/launchpad/apps/:appId/cover", Methods: "GET"},
		// 移动端「添加到主屏幕」换取免登录取图地址；同样挂 620，否则 operator 拿不到，
		// 桌面图标只能退回 Tier0 默认图。取图本身走免鉴权路由，不需要在这里授权。
		{ResourceID: 620, ActionType: "api", ActionValue: "/api/core/launchpad/apps/:appId/shortcut-icon-link", Methods: "GET"},
		{ResourceID: 620, ActionType: "gateway", ActionValue: "/app-proxy/**", Methods: "GET,HEAD,POST,PUT,DELETE,PATCH,OPTIONS"},
		{ResourceID: 620, ActionType: "openapi", ActionValue: "/openapi/v1/launchpad/:projectName/getMembers", Methods: "POST"},
		{ResourceID: 620, ActionType: "openapi", ActionValue: "/openapi/v1/platform/getMembers", Methods: "POST"},
		{ResourceID: 810, ActionType: "ui", ActionValue: "menu.anchorModel"},
		{ResourceID: 810, ActionType: "api", ActionValue: "/api/core/anchor/models", Methods: "GET"},
		{ResourceID: 810, ActionType: "api", ActionValue: "/api/core/anchor/models", Methods: "POST"},
		{ResourceID: 810, ActionType: "api", ActionValue: "/api/core/anchor/models/demo", Methods: "POST"},
		{ResourceID: 810, ActionType: "api", ActionValue: "/api/core/anchor/models/**", Methods: "GET,PUT,DELETE"},
		{ResourceID: 810, ActionType: "api", ActionValue: "/api/core/anchor/instances", Methods: "GET"},
		{ResourceID: 810, ActionType: "api", ActionValue: "/api/core/anchor/instances", Methods: "POST"},
		{ResourceID: 810, ActionType: "api", ActionValue: "/api/core/anchor/instances/**", Methods: "GET,PUT,DELETE"},
		{ResourceID: 810, ActionType: "api", ActionValue: "/api/core/anchor/mqtt-credentials", Methods: "POST"},
		{ResourceID: 820, ActionType: "ui", ActionValue: "menu.anchorScene"},
		{ResourceID: 820, ActionType: "api", ActionValue: "/api/core/anchor/scenes", Methods: "GET"},
		{ResourceID: 820, ActionType: "api", ActionValue: "/api/core/anchor/scenes", Methods: "POST"},
		{ResourceID: 820, ActionType: "api", ActionValue: "/api/core/anchor/scenes/**", Methods: "GET,PUT,DELETE"},
		{ResourceID: 910, ActionType: "ui", ActionValue: "menu.vision.camera"},
		{ResourceID: 911, ActionType: "api", ActionValue: "/api/core/video/cameras", Methods: "GET"},
		{ResourceID: 911, ActionType: "api", ActionValue: "/api/core/video/cameras/**", Methods: "GET"},
		{ResourceID: 911, ActionType: "api", ActionValue: "/api/core/video/channels", Methods: "GET"},
		{ResourceID: 911, ActionType: "api", ActionValue: "/api/core/video/cameras/:id/preview", Methods: "POST,DELETE"},
		{ResourceID: 912, ActionType: "ui", ActionValue: "button:Vision.camera.add"},
		{ResourceID: 912, ActionType: "ui", ActionValue: "button:Vision.camera.edit"},
		{ResourceID: 912, ActionType: "ui", ActionValue: "button:Vision.camera.delete"},
		{ResourceID: 912, ActionType: "ui", ActionValue: "button:Vision.camera.reconnect"},
		{ResourceID: 912, ActionType: "api", ActionValue: "/api/core/video/cameras", Methods: "POST"},
		{ResourceID: 912, ActionType: "api", ActionValue: "/api/core/video/cameras/probe", Methods: "POST"},
		{ResourceID: 912, ActionType: "api", ActionValue: "/api/core/video/cameras/:id/reconnect", Methods: "POST"},
		{ResourceID: 912, ActionType: "api", ActionValue: "/api/core/video/cameras/**", Methods: "PUT,DELETE"},
		{ResourceID: 915, ActionType: "api", ActionValue: "/api/core/vision/algorithms", Methods: "GET"},
		{ResourceID: 915, ActionType: "api", ActionValue: "/api/core/vision/algorithms/**", Methods: "GET"},
		{ResourceID: 916, ActionType: "ui", ActionValue: "button:Vision.algorithm.add"},
		{ResourceID: 916, ActionType: "ui", ActionValue: "button:Vision.algorithm.delete"},
		{ResourceID: 916, ActionType: "ui", ActionValue: "button:Vision.algorithm.uploadModel"},
		{ResourceID: 915, ActionType: "api", ActionValue: "/api/core/vision/models", Methods: "GET"},
		{ResourceID: 915, ActionType: "api", ActionValue: "/api/core/vision/models/**", Methods: "GET"},
		{ResourceID: 916, ActionType: "api", ActionValue: "/api/core/vision/models", Methods: "POST"},
		{ResourceID: 916, ActionType: "api", ActionValue: "/api/core/vision/models/**", Methods: "PUT,DELETE"},
		{ResourceID: 916, ActionType: "api", ActionValue: "/api/core/vision/algorithms", Methods: "POST"},
		{ResourceID: 916, ActionType: "api", ActionValue: "/api/core/vision/algorithms/import", Methods: "POST"},
		{ResourceID: 916, ActionType: "api", ActionValue: "/api/core/vision/algorithms/:id/model", Methods: "POST"},
		{ResourceID: 916, ActionType: "api", ActionValue: "/api/core/vision/algorithms/**", Methods: "PUT,DELETE"},
		{ResourceID: 917, ActionType: "api", ActionValue: "/api/core/vision/tasks", Methods: "GET"},
		{ResourceID: 917, ActionType: "api", ActionValue: "/api/core/vision/tasks/**", Methods: "GET"},
		{ResourceID: 917, ActionType: "api", ActionValue: "/api/core/vision/alarms", Methods: "GET"},
		{ResourceID: 917, ActionType: "api", ActionValue: "/api/core/vision/events", Methods: "GET"},
		{ResourceID: 917, ActionType: "api", ActionValue: "/api/core/vision/events/**", Methods: "GET"},
		{ResourceID: 918, ActionType: "api", ActionValue: "/api/core/vision/events/**", Methods: "POST,DELETE"},
		{ResourceID: 918, ActionType: "ui", ActionValue: "button:Vision.task.add"},
		{ResourceID: 918, ActionType: "ui", ActionValue: "button:Vision.task.delete"},
		{ResourceID: 918, ActionType: "ui", ActionValue: "button:Vision.task.run"},
		{ResourceID: 918, ActionType: "ui", ActionValue: "button:Vision.results.review"},
		{ResourceID: 918, ActionType: "api", ActionValue: "/api/core/vision/tasks", Methods: "POST"},
		{ResourceID: 918, ActionType: "api", ActionValue: "/api/core/vision/tasks/**", Methods: "POST,PUT,DELETE"},
		{ResourceID: 700, ActionType: "api", ActionValue: "/api/core/assets", Methods: "POST"},
		{ResourceID: 700, ActionType: "api", ActionValue: "/api/core/assets/multipart/init", Methods: "POST"},
		{ResourceID: 700, ActionType: "api", ActionValue: "/api/core/assets/multipart/part-urls", Methods: "POST"},
		{ResourceID: 700, ActionType: "api", ActionValue: "/api/core/assets/multipart/complete", Methods: "POST"},
		{ResourceID: 700, ActionType: "api", ActionValue: "/api/core/assets/multipart/abort", Methods: "POST"},
		{ResourceID: 700, ActionType: "api", ActionValue: "/api/core/asset-bindings", Methods: "POST"},
		{ResourceID: 700, ActionType: "openapi", ActionValue: "/openapi/v1/assets/upload", Methods: "POST"},
		{ResourceID: 700, ActionType: "openapi", ActionValue: "/openapi/v1/uns/attachments", Methods: "POST"},
		{ResourceID: 700, ActionType: "openapi", ActionValue: "/openapi/v1/assets/files", Methods: "POST"},
		{ResourceID: 700, ActionType: "openapi", ActionValue: "/openapi/v1/assets/files/multipart/init", Methods: "POST"},
		{ResourceID: 700, ActionType: "openapi", ActionValue: "/openapi/v1/assets/files/multipart/part-urls", Methods: "POST"},
		{ResourceID: 700, ActionType: "openapi", ActionValue: "/openapi/v1/assets/files/multipart/complete", Methods: "POST"},
		{ResourceID: 700, ActionType: "openapi", ActionValue: "/openapi/v1/assets/files/multipart/abort", Methods: "POST"},
		{ResourceID: 710, ActionType: "ui", ActionValue: "menu.assets"},
		{ResourceID: 710, ActionType: "api", ActionValue: "/api/core/assets", Methods: "GET"},
		{ResourceID: 710, ActionType: "api", ActionValue: "/api/core/assets/**", Methods: "GET"},
		{ResourceID: 710, ActionType: "openapi", ActionValue: "/openapi/v1/uns/attachments/list", Methods: "POST"},
		{ResourceID: 720, ActionType: "api", ActionValue: "/api/core/assets/**", Methods: "DELETE"},
		{ResourceID: 720, ActionType: "api", ActionValue: "/api/core/asset-bindings/**", Methods: "DELETE"},
		{ResourceID: 720, ActionType: "openapi", ActionValue: "/openapi/v1/assets/files/delete", Methods: "POST"},
		{ResourceID: 730, ActionType: "openapi", ActionValue: "/openapi/v1/assets/files/download", Methods: "GET"},
		{ResourceID: 730, ActionType: "openapi", ActionValue: "/openapi/v1/assets/files/url", Methods: "GET"},
		// 消息通知管理端（TASK-030）：接口权限挂 3210（admin 角色随 seedResources 自动全量，
		// 自定义角色经角色设置勾选）；用户端消息接口仅 SessionAuth，无需 api action
		{ResourceID: 3210, ActionType: "ui", ActionValue: "menu.notify.manage"},
		{ResourceID: 3210, ActionType: "api", ActionValue: "/api/core/notify/notification/save", Methods: "POST"},
		{ResourceID: 3210, ActionType: "api", ActionValue: "/api/core/notify/notification/send", Methods: "POST"},
		{ResourceID: 3210, ActionType: "api", ActionValue: "/api/core/notify/notification/revoke", Methods: "POST"},
		{ResourceID: 3210, ActionType: "api", ActionValue: "/api/core/notify/notification", Methods: "DELETE"},
		{ResourceID: 3210, ActionType: "api", ActionValue: "/api/core/notify/notification/copy", Methods: "POST"},
		{ResourceID: 3210, ActionType: "api", ActionValue: "/api/core/notify/notification/detail", Methods: "GET"},
		{ResourceID: 3210, ActionType: "api", ActionValue: "/api/core/notify/notification/list", Methods: "GET"},
		{ResourceID: 3210, ActionType: "api", ActionValue: "/api/core/notify/message-history/list", Methods: "GET"},
		{ResourceID: 3210, ActionType: "api", ActionValue: "/api/core/notify/recipient-options", Methods: "GET"},
		{ResourceID: 3210, ActionType: "api", ActionValue: "/api/core/notify/notification/estimate-users", Methods: "POST"},
		{ResourceID: 3220, ActionType: "ui", ActionValue: "menu.notify.messageCenter"},
		{ResourceID: 3230, ActionType: "openapi", ActionValue: "/openapi/v1/notifications/send", Methods: "POST"},
		{ResourceID: 3231, ActionType: "openapi", ActionValue: "/openapi/v1/notifications/get", Methods: "POST"},
	}
}
