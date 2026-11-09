package repo

import "gorm.io/gorm"

// 仅在旧版 191 确实代表登录日志时迁移，避免给原本只有导出权限的角色新增权限。
func migrateLoginLogGrants(tx *gorm.DB) error {
	return tx.Exec(`INSERT INTO sys_role_resource
		(role_id, resource_id, created_by, updated_by, created_time, updated_time, deleted_time)
		SELECT role_id, 192, created_by, updated_by, created_time, updated_time, 0
		FROM sys_role_resource
		WHERE resource_id = 191 AND deleted_time = 0
		AND EXISTS (SELECT 1 FROM sys_resource_info WHERE id = 191 AND resource_key = 'system.login.log' AND deleted_time = 0)
		ON CONFLICT (role_id, resource_id, deleted_time) DO NOTHING`).Error
}
