package repo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Project struct {
	ID              int64  `gorm:"column:id;primaryKey" json:"id"`
	ProjectKey      string `gorm:"column:project_key" json:"projectKey"`
	Name            string `gorm:"column:name" json:"name"`
	Description     string `gorm:"column:description" json:"description"`
	Status          string `gorm:"column:status" json:"status"`
	OwnerUserID     int64  `gorm:"column:owner_user_id" json:"ownerUserId"`
	DatabaseName    string `gorm:"column:database_name" json:"databaseName"`
	AppNums         int64  `gorm:"-" json:"-"`
	CreatorNickName string `gorm:"-" json:"-"`
	CreatorUserName string `gorm:"-" json:"-"`
	SoftTime
}

func (Project) TableName() string { return "project" }

type ProjectApp struct {
	ID              int64           `gorm:"column:id;primaryKey" json:"id"`
	AppPrincipalID  string          `gorm:"column:app_principal_id;type:uuid;default:gen_random_uuid();<-:create" json:"appPrincipalId"`
	ProjectID       int64           `gorm:"column:project_id" json:"projectId"`
	AppKey          string          `gorm:"column:app_key" json:"appKey"`
	Name            string          `gorm:"column:name" json:"name"`
	SiteType        string          `gorm:"column:site_type" json:"siteType"`
	EntryPath       string          `gorm:"column:entry_path" json:"entryPath"`
	EntryURL        string          `gorm:"column:entry_url" json:"entryUrl"`
	RoutePrefix     string          `gorm:"column:route_prefix" json:"routePrefix"`
	GatewayRouteKey string          `gorm:"column:gateway_route_key" json:"gatewayRouteKey"`
	OAuthClientID   string          `gorm:"column:oauth_client_id" json:"oauthClientId"`
	IconAssetID     int64           `gorm:"column:icon_asset_id" json:"iconAssetId"`
	StaticAssetID   int64           `gorm:"column:static_asset_id" json:"staticAssetId"`
	ManifestJSON    json.RawMessage `gorm:"column:manifest_json;type:jsonb" json:"manifestJson"`
	SchemaName      string          `gorm:"column:schema_name" json:"schemaName"`
	Status          string          `gorm:"column:status" json:"status"`
	CreatorNickName string          `gorm:"->;column:creator_nick_name" json:"creatorNickName"`
	CreatorUserName string          `gorm:"->;column:creator_user_name" json:"creatorUserName"`
	SoftNoDelByTime
}

func (ProjectApp) TableName() string { return "project_app" }

type ProjectRole struct {
	ID          int64                `gorm:"column:id;primaryKey" json:"roleId"`
	ProjectID   int64                `gorm:"column:project_id" json:"projectId"`
	RoleKey     string               `gorm:"column:role_key" json:"roleKey"`
	Name        string               `gorm:"column:name" json:"roleName"`
	Description string               `gorm:"column:description" json:"description"`
	MemberCount int64                `gorm:"-" json:"memberCount"`
	Apps        []ProjectRoleAppItem `gorm:"-" json:"apps,omitempty"`
	OnlyTime
}

func (ProjectRole) TableName() string { return "project_role" }

type ProjectRoleApp struct {
	ProjectID   int64 `gorm:"column:project_id" json:"projectId"`
	RoleID      int64 `gorm:"column:role_id;primaryKey" json:"roleId"`
	AppID       int64 `gorm:"column:app_id;primaryKey" json:"appId"`
	CreatedTime int64 `gorm:"column:created_time" json:"createdTime"`
}

func (ProjectRoleApp) TableName() string { return "project_role_app" }

type ProjectRoleAppItem struct {
	AppID   int64  `json:"appId"`
	AppName string `json:"appName"`
}

type ProjectMember struct {
	ID          int64         `json:"memberId"`
	ProjectID   int64         `json:"projectId"`
	UserID      int64         `json:"userId"`
	UserName    string        `json:"userName"`
	Email       string        `json:"email"`
	Status      string        `json:"status"`
	Roles       []ProjectRole `json:"roles"`
	CreatedTime int64         `json:"createdTime"`
	UpdatedTime int64         `json:"updatedTime"`
}

type ProjectMemberSave struct {
	ProjectID int64
	MemberID  int64
	UserID    int64
	RoleIDs   []int64
}

func (r *ProjectRepo) CreateProject(ctx context.Context, item Project) (Project, error) {
	if item.ProjectKey == "" {
		item.ProjectKey = "prj_" + randomHex(12)
	}
	if item.Status == "" {
		item.Status = "active"
	}
	err := r.db.WithContext(ctx).Create(&item).Error
	return item, normalizeDBError(err)
}

func (r *ProjectRepo) UpdateProjectAppManifest(ctx context.Context, projectID, appID int64, patch map[string]any, userID int64) error {
	now := repoNowMilli()
	var row struct {
		ManifestJSON json.RawMessage `gorm:"column:manifest_json"`
	}
	if err := r.db.WithContext(ctx).Table("project_app").Select("manifest_json").
		Where("id = ? AND project_id = ? AND deleted_time = 0", appID, projectID).Take(&row).Error; err != nil {
		return err
	}
	manifest := map[string]any{}
	if len(row.ManifestJSON) > 0 {
		_ = json.Unmarshal(row.ManifestJSON, &manifest)
	}
	for key, value := range patch {
		manifest[key] = value
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	res := r.db.WithContext(ctx).Model(&ProjectApp{}).
		Where("id = ? AND project_id = ? AND deleted_time = 0", appID, projectID).
		Updates(touchByValues(map[string]any{"manifest_json": json.RawMessage(raw)}, userID, now))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) ListProjectApps(ctx context.Context, projectID int64) ([]ProjectApp, error) {
	var out []ProjectApp
	err := r.db.WithContext(ctx).
		Table("project_app pa").
		Select(`pa.*, COALESCE(u.nick_name, '') AS creator_nick_name, COALESCE(u.user_name, '') AS creator_user_name`).
		Joins("LEFT JOIN sys_user_info u ON u.user_id = pa.created_by AND u.deleted_time = 0").
		Where("pa.project_id = ? AND pa.deleted_time = 0", projectID).
		Order("pa.id DESC").
		Scan(&out).Error
	return out, err
}

// ListAllProjectAppManifests 返回全部存活应用的 id/project_id/manifest_json（跨 Project）。
// 删除 App 清理其导入的 Flow/UNS 时，据此判断资源是否仍被其他存活 App 的 manifest 引用，
// 避免误删共享资源（Flow 顶层 id 全局命中后可能被其他 Project 的 App 引用）。
func (r *ProjectRepo) ListAllProjectAppManifests(ctx context.Context) ([]ProjectApp, error) {
	var out []ProjectApp
	err := r.db.WithContext(ctx).
		Select("id, project_id, manifest_json").
		Where("deleted_time = 0").
		Find(&out).Error
	return out, err
}

func (r *ProjectRepo) ListContainerApps(ctx context.Context) ([]ProjectApp, error) {
	var out []ProjectApp
	err := r.db.WithContext(ctx).
		Where("project_id = 0 AND site_type = ? AND deleted_time = 0", "externalContainer").
		Order("id DESC").
		Find(&out).Error
	return out, err
}

func (r *ProjectRepo) GetContainerApp(ctx context.Context, appID int64) (ProjectApp, error) {
	var item ProjectApp
	err := r.db.WithContext(ctx).
		Where("id = ? AND project_id = 0 AND site_type = ? AND deleted_time = 0", appID, "externalContainer").
		Take(&item).Error
	return item, err
}

func (r *ProjectRepo) GetProjectAppByKey(ctx context.Context, projectID int64, appKey string) (ProjectApp, error) {
	var item ProjectApp
	err := r.db.WithContext(ctx).Where("project_id = ? AND app_key = ? AND deleted_time = 0", projectID, appKey).Take(&item).Error
	return item, err
}

// GetProjectAppByDeployAppID 按部署产物 UUID（manifest_json.app_id）查询应用记录。
// 应用网关做项目角色门控时，需要把路由表里的部署 appID 映射回 project_app 主键。
func (r *ProjectRepo) GetProjectAppByDeployAppID(ctx context.Context, projectID int64, deployAppID string) (ProjectApp, error) {
	var item ProjectApp
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND deleted_time = 0 AND manifest_json->>'app_id' = ?", projectID, deployAppID).
		Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProjectApp{}, ErrNotFound
	}
	return item, err
}

// GetProjectAppBySourceAppID 按导出包来源应用 ID（manifest_json.sourceAppId）查询应用记录。
// 同名 artifact 包更新导入优先按来源身份定位目标，应用重命名后仍能命中原记录。
func (r *ProjectRepo) GetProjectAppBySourceAppID(ctx context.Context, projectID int64, sourceAppID string) (ProjectApp, error) {
	var item ProjectApp
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND deleted_time = 0 AND manifest_json->>'sourceAppId' = ?", projectID, sourceAppID).
		Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProjectApp{}, ErrNotFound
	}
	return item, err
}

// GetProjectAppBySourceAppIDGlobal 按包内来源 appId（manifest_json.sourceAppId）全局查询应用记录。
// 导入为新 App 时据此校验包内来源 appId 是否已在平台其他/当前 Project 中存在。
// 同一来源 appId 可能被多个 Project 导入，按 id 升序取最早导入的记录，保证阻断文案中的 Project 名可预期。
func (r *ProjectRepo) GetProjectAppBySourceAppIDGlobal(ctx context.Context, sourceAppID string) (ProjectApp, error) {
	var item ProjectApp
	err := r.db.WithContext(ctx).
		Where("deleted_time = 0 AND manifest_json->>'sourceAppId' = ?", sourceAppID).
		Order("id").
		Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProjectApp{}, ErrNotFound
	}
	return item, err
}

// GetProjectMemberWithRoles 按 (project_id, user_id) 定向查询成员并加载其角色。
// 供应用网关等热路径使用，避免 GetProjectMember 的全量成员枚举 + 逐成员角色加载。
func (r *ProjectRepo) GetProjectMemberWithRoles(ctx context.Context, projectID, userID int64) (ProjectMember, error) {
	return r.GetEffectiveProjectMember(ctx, projectID, userID)
}

func (r *ProjectRepo) GetProjectAppByName(ctx context.Context, projectID int64, name string) (ProjectApp, error) {
	var item ProjectApp
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND LOWER(name) = LOWER(?) AND deleted_time = 0", projectID, name).
		Take(&item).Error
	return item, err
}

func (r *ProjectRepo) GetProjectApp(ctx context.Context, projectID, appID int64) (ProjectApp, error) {
	var item ProjectApp
	err := r.db.WithContext(ctx).Where("id = ? AND project_id = ? AND deleted_time = 0", appID, projectID).Take(&item).Error
	return item, err
}

// GetProjectAppByID 按应用 ID 查询，不限制项目 ID，用于最近应用等跨项目场景。
func (r *ProjectRepo) GetProjectAppByID(ctx context.Context, appID int64) (ProjectApp, error) {
	var item ProjectApp
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", appID).Take(&item).Error
	return item, err
}

// 授权主体由本地安装生成，不使用导入来源或可更新的部署 UUID。
// 这里只解析身份；应用/项目状态和调用者权限仍由领域服务实时检查。
func (r *ProjectRepo) GetProjectAppByPrincipalID(ctx context.Context, principalID string) (ProjectApp, error) {
	parsed, err := uuid.Parse(principalID)
	if err != nil || parsed == uuid.Nil || parsed.String() != principalID {
		return ProjectApp{}, ErrInvalidArgument
	}
	var item ProjectApp
	err = r.db.WithContext(ctx).Where("app_principal_id = ? AND deleted_time = 0", principalID).Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProjectApp{}, ErrNotFound
	}
	return item, err
}

// 已删除应用的主体也保持预留，防止旧凭据被同名外部登记重新激活。
func (r *ProjectRepo) ProjectAppPrincipalReserved(ctx context.Context, id string) (bool, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil {
		return false, nil
	}
	var count int64
	err = r.db.WithContext(ctx).Unscoped().Model(&ProjectApp{}).
		Where("app_principal_id = ?", parsed.String()).Count(&count).Error
	return count > 0, err
}

// GetProjectAppByDockerAppID 按平台注入容器的 docker app UUID 反查应用。
//
// 部署时 APP_ID 被注入容器（见 logic/core/project/deploy.go 的 generateExtraEnvs），
// SDK 用它作 sender.id 发通知；该 UUID 落在 manifest_json.app_id 里，是平台侧唯一
// 能把"发通知的那个容器"对回 Launchpad 应用的线索。
func (r *ProjectRepo) GetProjectAppByDockerAppID(ctx context.Context, dockerAppID string) (ProjectApp, error) {
	var item ProjectApp
	if strings.TrimSpace(dockerAppID) == "" {
		return item, ErrNotFound
	}
	err := r.db.WithContext(ctx).
		Where("manifest_json->>'app_id' = ? AND deleted_time = 0", dockerAppID).
		Take(&item).Error
	return item, err
}

func (r *ProjectRepo) CreateProjectApp(ctx context.Context, item ProjectApp) (ProjectApp, error) {
	// 创建（包括复制/重复导入）始终由数据库分配新身份，不能认领包或调用方提供的身份。
	item.AppPrincipalID = ""
	if item.SiteType == "" {
		item.SiteType = "sameOriginStatic"
	}
	if item.Status == "" {
		item.Status = "active"
	}
	if len(item.ManifestJSON) == 0 {
		item.ManifestJSON = json.RawMessage(`{}`)
	}
	err := r.db.WithContext(ctx).Create(&item).Error
	return item, normalizeDBError(err)
}

func (r *ProjectRepo) CountProjectApps(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&ProjectApp{}).
		Where("deleted_time = 0 AND project_id > 0 AND status IN ?", []string{"active", "deploying"}).
		Count(&count).Error
	return count, err
}

func (r *ProjectRepo) UpdateProjectApp(ctx context.Context, id int64, item ProjectApp) (ProjectApp, error) {
	now := repoNowMilli()
	if len(item.ManifestJSON) == 0 {
		item.ManifestJSON = json.RawMessage(`{}`)
	}
	var out ProjectApp
	res := r.db.WithContext(ctx).Model(&out).Clauses(returningAll()).
		Where("id = ? AND deleted_time = 0", id).
		Updates(touchByValues(map[string]any{
			"name":              item.Name,
			"site_type":         item.SiteType,
			"entry_path":        item.EntryPath,
			"entry_url":         item.EntryURL,
			"route_prefix":      item.RoutePrefix,
			"gateway_route_key": item.GatewayRouteKey,
			"oauth_client_id":   item.OAuthClientID,
			"icon_asset_id":     item.IconAssetID,
			"static_asset_id":   item.StaticAssetID,
			"manifest_json":     item.ManifestJSON,
			"status":            item.Status,
		}, item.UpdatedBy, now))
	if res.Error != nil {
		return ProjectApp{}, normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return ProjectApp{}, ErrNotFound
	}
	return out, nil
}

// UpdateGeneratedAppEntryURLByRuntimeID updates only installer-generated
// dynamic Apps. Manual App URLs do not carry the runtime app_id marker and are
// intentionally left untouched.
func (r *ProjectRepo) UpdateGeneratedAppEntryURLByRuntimeID(ctx context.Context, runtimeAppID, entryURL string) error {
	return normalizeDBError(r.db.WithContext(ctx).Model(&ProjectApp{}).
		Where("deleted_time = 0 AND manifest_json ->> 'app_type' = ? AND manifest_json ->> 'app_id' = ? AND entry_url IS DISTINCT FROM ?", "dynamic", runtimeAppID, entryURL).
		Updates(map[string]any{
			"entry_url":    entryURL,
			"updated_time": time.Now(),
		}).Error)
}

func (r *ProjectRepo) UpdateProjectAppMetadata(
	ctx context.Context,
	projectID, appID int64,
	name, description string,
	iconAssetID, coverAssetID int64,
	userID int64,
) (ProjectApp, error) {
	var out ProjectApp
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND project_id = ? AND deleted_time = 0", appID, projectID).
			Take(&out).Error; err != nil {
			return err
		}

		manifest := map[string]any{}
		if len(out.ManifestJSON) > 0 {
			_ = json.Unmarshal(out.ManifestJSON, &manifest)
		}
		if iconAssetID <= 0 {
			iconAssetID = out.IconAssetID
		}
		if coverAssetID <= 0 {
			if value, ok := manifest["coverAssetId"].(float64); ok {
				coverAssetID = int64(value)
			}
		}
		manifest["name"] = name
		manifest["displayName"] = name
		manifest["description"] = description
		manifest["coverAssetId"] = coverAssetID
		raw, err := json.Marshal(manifest)
		if err != nil {
			return err
		}

		res := tx.Model(&out).Clauses(returningAll()).
			Where("id = ? AND project_id = ? AND deleted_time = 0", appID, projectID).
			Updates(touchByValues(map[string]any{
				"name":          name,
				"icon_asset_id": iconAssetID,
				"manifest_json": json.RawMessage(raw),
			}, userID, repoNowMilli()))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
	return out, normalizeDBError(err)
}

func (r *ProjectRepo) UpdateProjectAppStatus(ctx context.Context, projectID, appID int64, status string, userID int64) error {
	res := r.db.WithContext(ctx).Model(&ProjectApp{}).
		Where("id = ? AND project_id = ? AND deleted_time = 0", appID, projectID).
		Updates(touchByValues(map[string]any{"status": status}, userID, 0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ErrAppDeployInProgress 应用正在部署中（CAS 标记 deploying 未命中），
// 由调用方映射为可读的并发冲突错误。
var ErrAppDeployInProgress = errors.New("app deploy in progress")

// UpdateProjectAppStatusDeployingCAS 以 CAS 语义把应用标记为 deploying：
// 仅当当前 status 不是 deploying 时更新成功；0 行命中（应用不存在或已在部署中）返回 ErrAppDeployInProgress。
// 用于覆盖导入/更新导入的并发互斥，防止同一应用被并发重建。
func (r *ProjectRepo) UpdateProjectAppStatusDeployingCAS(ctx context.Context, projectID, appID int64, userID int64) error {
	res := r.db.WithContext(ctx).Model(&ProjectApp{}).
		Where("id = ? AND project_id = ? AND status <> 'deploying' AND deleted_time = 0", appID, projectID).
		Updates(touchByValues(map[string]any{"status": "deploying"}, userID, 0))
	if res.Error != nil {
		return res.Error
	}
	return casDeployStatusError(res.RowsAffected)
}

// casDeployStatusError 把 CAS 更新的影响行数转换为结果：0 行命中视为并发冲突
// （应用不存在或已在部署中）。纯函数，便于单测。
func casDeployStatusError(rowsAffected int64) error {
	if rowsAffected == 0 {
		return ErrAppDeployInProgress
	}
	return nil
}

func (r *ProjectRepo) UpdateContainerAppStatus(ctx context.Context, appID int64, status string, userID int64) error {
	res := r.db.WithContext(ctx).Model(&ProjectApp{}).
		Where("id = ? AND project_id = 0 AND site_type = ? AND deleted_time = 0", appID, "externalContainer").
		Updates(touchByValues(map[string]any{"status": status}, userID, 0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) DeleteProjectApp(ctx context.Context, projectID, appID, userID int64) error {
	res := r.db.WithContext(ctx).Model(&ProjectApp{}).
		Where("id = ? AND project_id = ? AND deleted_time = 0", appID, projectID).
		Updates(softDeleteNoDelByValues(userID, 0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) DeleteContainerApp(ctx context.Context, appID, userID int64) error {
	res := r.db.WithContext(ctx).Model(&ProjectApp{}).
		Where("id = ? AND project_id = 0 AND site_type = ? AND deleted_time = 0", appID, "externalContainer").
		Updates(softDeleteNoDelByValues(userID, 0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) UpsertProjectRole(ctx context.Context, item ProjectRole) (ProjectRole, error) {
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	if item.RoleKey == "" {
		item.RoleKey = "role_" + randomHex(8)
	}
	var existing ProjectRole
	err := r.db.WithContext(ctx).Where("project_id = ? AND role_key = ?", item.ProjectID, item.RoleKey).Order("id").Take(&existing).Error
	if err == nil {
		var out ProjectRole
		if err := r.db.WithContext(ctx).Model(&out).Clauses(returningAll()).
			Where("id = ?", existing.ID).
			Updates(map[string]any{"name": item.Name, "description": item.Description, "updated_time": ts}).Error; err != nil {
			return ProjectRole{}, normalizeDBError(err)
		}
		return out, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return ProjectRole{}, err
	}
	row := ProjectRole{ProjectID: item.ProjectID, RoleKey: item.RoleKey, Name: item.Name, Description: item.Description}
	row.CreatedTime = ts
	row.UpdatedTime = ts
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return ProjectRole{}, normalizeDBError(err)
	}
	return row, nil
}

func (r *ProjectRepo) InsertProjectRoleApps(ctx context.Context, projectID, roleID int64, appIDs []int64) error {
	if len(appIDs) == 0 {
		return nil
	}
	now := time.Now().UTC().UnixMilli()
	records := make([]ProjectRoleApp, 0, len(appIDs))
	for _, appID := range appIDs {
		if appID == 0 {
			continue
		}
		records = append(records, ProjectRoleApp{
			ProjectID:   projectID,
			RoleID:      roleID,
			AppID:       appID,
			CreatedTime: now,
		})
	}
	if len(records) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "role_id"}, {Name: "app_id"}},
		DoNothing: true,
	}).Create(&records).Error
}

func (r *ProjectRepo) ListProjectRoleAppsByRoleIDs(ctx context.Context, projectID int64, roleIDs []int64) ([]ProjectRoleApp, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	var out []ProjectRoleApp
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND role_id IN ?", projectID, roleIDs).
		Find(&out).Error
	return out, err
}

func (r *ProjectRepo) ListProjectRoleAppIDsByAppIDs(ctx context.Context, projectID int64, appIDs []int64) ([]ProjectRoleApp, error) {
	if len(appIDs) == 0 {
		return nil, nil
	}
	var out []ProjectRoleApp
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND app_id IN ?", projectID, appIDs).
		Find(&out).Error
	return out, err
}

func (r *ProjectRepo) DeleteProjectRoleAppsByAppID(ctx context.Context, projectID, appID int64) error {
	return r.db.WithContext(ctx).
		Where("project_id = ? AND app_id = ?", projectID, appID).
		Delete(&ProjectRoleApp{}).Error
}

func (r *ProjectRepo) DeleteProjectRoleAppsByRoleID(ctx context.Context, projectID, roleID int64) error {
	return r.db.WithContext(ctx).
		Where("project_id = ? AND role_id = ?", projectID, roleID).
		Delete(&ProjectRoleApp{}).Error
}

func (r *ProjectRepo) DeleteProjectMemberRolesByRoleID(ctx context.Context, projectID, roleID int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM project_member_role WHERE project_id = ? AND role_id = ?", projectID, roleID).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM project_user_group_role WHERE project_id = ? AND role_id = ?", projectID, roleID).Error
	})
}

// DeleteProjectRolesIfNoApps deletes roles (and their member bindings) that have no app associations.
// It returns the IDs of the roles that were deleted.
func (r *ProjectRepo) DeleteProjectRolesIfNoApps(ctx context.Context, projectID int64, roleIDs []int64) ([]int64, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	var stillBound []int64
	err := r.db.WithContext(ctx).
		Model(&ProjectRoleApp{}).
		Where("project_id = ? AND role_id IN ?", projectID, roleIDs).
		Distinct("role_id").
		Pluck("role_id", &stillBound).Error
	if err != nil {
		return nil, err
	}
	stillBoundSet := make(map[int64]struct{}, len(stillBound))
	for _, id := range stillBound {
		stillBoundSet[id] = struct{}{}
	}
	var toDelete []int64
	for _, id := range roleIDs {
		if _, ok := stillBoundSet[id]; !ok {
			toDelete = append(toDelete, id)
		}
	}
	if len(toDelete) == 0 {
		return nil, nil
	}
	return toDelete, r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM project_member_role WHERE project_id = ? AND role_id IN ?", projectID, toDelete).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM project_user_group_role WHERE project_id = ? AND role_id IN ?", projectID, toDelete).Error; err != nil {
			return err
		}
		return tx.Where("project_id = ? AND id IN ?", projectID, toDelete).Delete(&ProjectRole{}).Error
	})
}

func (r *ProjectRepo) ListProjectRoles(ctx context.Context, projectID int64) ([]ProjectRole, error) {
	type roleRow struct {
		ID          int64     `gorm:"column:id"`
		ProjectID   int64     `gorm:"column:project_id"`
		RoleKey     string    `gorm:"column:role_key"`
		Name        string    `gorm:"column:name"`
		Description string    `gorm:"column:description"`
		MemberCount int64     `gorm:"column:member_count"`
		CreatedTime time.Time `gorm:"column:created_time"`
		UpdatedTime time.Time `gorm:"column:updated_time"`
	}
	var rows []roleRow
	err := r.db.WithContext(ctx).Table("project_role r").
		Select("r.id, r.project_id, r.role_key, r.name, r.description, r.created_time, r.updated_time").
		Where("r.project_id = ?", projectID).
		Order("r.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts, err := r.effectiveProjectRoleCounts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectRole, 0, len(rows))
	for _, r := range rows {
		item := ProjectRole{ID: r.ID, ProjectID: r.ProjectID, RoleKey: r.RoleKey, Name: r.Name, Description: r.Description, MemberCount: counts[r.ID]}
		item.CreatedTime = r.CreatedTime
		item.UpdatedTime = r.UpdatedTime
		out = append(out, item)
	}
	return out, nil
}

func (r *ProjectRepo) ListProjectMembers(ctx context.Context, projectID int64) ([]ProjectMember, error) {
	type memberRow struct {
		ID          int64  `gorm:"column:id"`
		ProjectID   int64  `gorm:"column:project_id"`
		UserID      int64  `gorm:"column:user_id"`
		UserName    string `gorm:"column:user_name"`
		Email       string `gorm:"column:email"`
		Status      string `gorm:"column:status"`
		CreatedTime int64  `gorm:"column:created_time"`
		UpdatedTime int64  `gorm:"column:updated_time"`
	}
	var rows []memberRow
	err := r.db.WithContext(ctx).Table("project_member m").
		Select("m.id, m.project_id, m.user_id, coalesce(u.user_name,'') AS user_name, coalesce(u.email,'') AS email, m.status, m.created_time, m.updated_time").
		Joins("LEFT JOIN sys_user_info u ON u.user_id=m.user_id AND u.deleted_time=0").
		Where("m.project_id = ?", projectID).
		Order("m.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ProjectMember, 0, len(rows))
	for _, row := range rows {
		email, _, err := decryptUserContacts(row.UserID, row.Email, "")
		if err != nil {
			return nil, err
		}
		member := ProjectMember{ID: row.ID, ProjectID: row.ProjectID, UserID: row.UserID, UserName: row.UserName, Email: email, Status: row.Status, CreatedTime: row.CreatedTime, UpdatedTime: row.UpdatedTime}
		member.Roles, err = r.ProjectMemberRoles(ctx, projectID, member.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, member)
	}
	return out, nil
}

func (r *ProjectRepo) UpsertProjectMember(ctx context.Context, item ProjectMemberSave) (ProjectMember, error) {
	now := time.Now().UTC().UnixMilli()
	if item.ProjectID == 0 || item.UserID == 0 {
		return ProjectMember{}, ErrInvalidArgument
	}
	const memberStatus = "active"
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureProjectUserExists(tx, item.UserID); err != nil {
			return err
		}
		row := map[string]any{
			"project_id":   item.ProjectID,
			"user_id":      item.UserID,
			"status":       memberStatus,
			"created_time": now,
			"updated_time": now,
		}
		if err := tx.Table("project_member").Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"status":       memberStatus,
				"updated_time": now,
			}),
		}).Create(row).Error; err != nil {
			return err
		}
		memberID, err := projectMemberID(tx, item.ProjectID, item.UserID)
		if err != nil {
			return err
		}
		return replaceProjectMemberRoles(tx, item.ProjectID, memberID, item.RoleIDs, now)
	})
	if err != nil {
		return ProjectMember{}, normalizeDBError(err)
	}
	return r.GetProjectMember(ctx, item.ProjectID, 0, item.UserID)
}

func (r *ProjectRepo) UpdateProjectMember(ctx context.Context, item ProjectMemberSave) (ProjectMember, error) {
	now := time.Now().UTC().UnixMilli()
	if item.ProjectID == 0 || item.MemberID == 0 {
		return ProjectMember{}, ErrInvalidArgument
	}
	const memberStatus = "active"
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Table("project_member").
			Where("project_id = ? AND id = ?", item.ProjectID, item.MemberID).
			Updates(map[string]any{"status": memberStatus, "updated_time": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return replaceProjectMemberRoles(tx, item.ProjectID, item.MemberID, item.RoleIDs, now)
	})
	if err != nil {
		return ProjectMember{}, normalizeDBError(err)
	}
	return r.GetProjectMember(ctx, item.ProjectID, item.MemberID, 0)
}

func (r *ProjectRepo) DeleteProjectMember(ctx context.Context, projectID, memberID int64) error {
	if projectID == 0 || memberID == 0 {
		return ErrInvalidArgument
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM project_member_role WHERE project_id = ? AND member_id = ?", projectID, memberID).Error; err != nil {
			return err
		}
		res := tx.Exec("DELETE FROM project_member WHERE project_id = ? AND id = ?", projectID, memberID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *ProjectRepo) GetProjectMember(ctx context.Context, projectID, memberID, userID int64) (ProjectMember, error) {
	members, err := r.ListProjectMembers(ctx, projectID)
	if err != nil {
		return ProjectMember{}, err
	}
	for _, member := range members {
		if memberID != 0 && member.ID == memberID {
			return member, nil
		}
		if userID != 0 && member.UserID == userID {
			return member, nil
		}
	}
	return ProjectMember{}, ErrNotFound
}

func ensureProjectUserExists(tx *gorm.DB, userID int64) error {
	var count int64
	if err := tx.Table("sys_user_info").
		Where("user_id = ? AND deleted_time = 0", userID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func projectMemberID(tx *gorm.DB, projectID, userID int64) (int64, error) {
	var row struct {
		ID int64 `gorm:"column:id"`
	}
	if err := tx.Table("project_member").
		Select("id").
		Where("project_id = ? AND user_id = ?", projectID, userID).
		Take(&row).Error; err != nil {
		return 0, err
	}
	return row.ID, nil
}

func replaceProjectMemberRoles(tx *gorm.DB, projectID, memberID int64, roleIDs []int64, now int64) error {
	if err := tx.Exec("DELETE FROM project_member_role WHERE project_id = ? AND member_id = ?", projectID, memberID).Error; err != nil {
		return err
	}
	seen := map[int64]struct{}{}
	for _, roleID := range roleIDs {
		if roleID == 0 {
			continue
		}
		if _, ok := seen[roleID]; ok {
			continue
		}
		seen[roleID] = struct{}{}
		var count int64
		if err := tx.Table("project_role").
			Where("project_id = ? AND id = ?", projectID, roleID).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			continue
		}
		if err := tx.Table("project_member_role").Create(map[string]any{
			"project_id":   projectID,
			"member_id":    memberID,
			"role_id":      roleID,
			"created_time": now,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *ProjectRepo) ProjectMemberRoles(ctx context.Context, projectID, memberID int64) ([]ProjectRole, error) {
	type roleRow struct {
		ID          int64     `gorm:"column:id"`
		ProjectID   int64     `gorm:"column:project_id"`
		RoleKey     string    `gorm:"column:role_key"`
		Name        string    `gorm:"column:name"`
		Description string    `gorm:"column:description"`
		CreatedTime time.Time `gorm:"column:created_time"`
		UpdatedTime time.Time `gorm:"column:updated_time"`
	}
	var rows []roleRow
	err := r.db.WithContext(ctx).Table("project_member_role mr").
		Select("r.id, r.project_id, r.role_key, r.name, r.description, r.created_time, r.updated_time").
		Joins("JOIN project_role r ON r.project_id=mr.project_id AND r.id=mr.role_id").
		Where("mr.project_id = ? AND mr.member_id = ?", projectID, memberID).
		Order("r.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ProjectRole, 0, len(rows))
	for _, r := range rows {
		item := ProjectRole{ID: r.ID, ProjectID: r.ProjectID, RoleKey: r.RoleKey, Name: r.Name, Description: r.Description}
		item.CreatedTime = r.CreatedTime
		item.UpdatedTime = r.UpdatedTime
		out = append(out, item)
	}
	return out, nil
}

func (r *ProjectRepo) ListProjects(ctx context.Context, keyword string, userID int64, includeAll bool) ([]Project, error) {
	type row struct {
		ID              int64     `gorm:"column:id"`
		ProjectKey      string    `gorm:"column:project_key"`
		Name            string    `gorm:"column:name"`
		Description     string    `gorm:"column:description"`
		Status          string    `gorm:"column:status"`
		OwnerUserID     int64     `gorm:"column:owner_user_id"`
		CreatedBy       int64     `gorm:"column:created_by"`
		UpdatedBy       int64     `gorm:"column:updated_by"`
		CreatedTime     time.Time `gorm:"column:created_time"`
		UpdatedTime     time.Time `gorm:"column:updated_time"`
		DeletedBy       int64     `gorm:"column:deleted_by"`
		DeletedTime     int64     `gorm:"column:deleted_time"`
		AppNums         int64     `gorm:"column:app_nums"`
		CreatorNickName string    `gorm:"column:creator_nick_name"`
		CreatorUserName string    `gorm:"column:creator_user_name"`
	}

	q := r.db.WithContext(ctx).
		Table("project p").
		Select(`p.id, p.project_key, p.name, p.description, p.status, p.owner_user_id, p.created_by, p.updated_by,
				p.created_time, p.updated_time, p.deleted_by, p.deleted_time,
				COALESCE(app_counts.app_count, 0) AS app_nums,
				COALESCE(u.nick_name, '') AS creator_nick_name,
				COALESCE(u.user_name, '') AS creator_user_name`).
		Joins(`LEFT JOIN (
			SELECT project_id, COUNT(*) AS app_count
			FROM project_app
			WHERE deleted_time = 0
			GROUP BY project_id
		) app_counts ON app_counts.project_id = p.id`).
		Joins("LEFT JOIN sys_user_info u ON u.user_id = p.created_by AND u.deleted_time = 0").
		Where("p.deleted_time = 0")

	if !includeAll {
		q = q.Where(`(p.owner_user_id = ?
			OR EXISTS (SELECT 1 FROM project_member pm WHERE pm.project_id = p.id AND pm.user_id = ?)
			OR EXISTS (
				SELECT 1 FROM project_user_group pg
				JOIN sys_user_group g ON g.id = pg.group_id AND g.deleted_time = 0
				JOIN sys_user_group_member gm ON gm.group_id = g.id
				WHERE pg.project_id = p.id AND gm.user_id = ?
			))`, userID, userID, userID)
	}

	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("(p.name ILIKE ? OR p.project_key ILIKE ? OR p.description ILIKE ?)", like, like, like)
	}

	var rows []row
	if err := q.Order("p.id DESC").Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]Project, 0, len(rows))
	for _, r := range rows {
		out = append(out, Project{
			ID:              r.ID,
			ProjectKey:      r.ProjectKey,
			Name:            r.Name,
			Description:     r.Description,
			Status:          r.Status,
			OwnerUserID:     r.OwnerUserID,
			AppNums:         r.AppNums,
			CreatorNickName: r.CreatorNickName,
			CreatorUserName: r.CreatorUserName,
			SoftTime: SoftTime{
				NoDelTime: NoDelTime{
					CreatedBy: r.CreatedBy,
					UpdatedBy: r.UpdatedBy,
					OnlyTime: OnlyTime{
						CreateTime:  CreateTime{CreatedTime: r.CreatedTime},
						UpdatedTime: r.UpdatedTime,
					},
				},
				DeletedBy:        r.DeletedBy,
				DeletedTimeField: DeletedTimeField{DeletedTime: DeletedTime(r.DeletedTime)},
			},
		})
	}
	return out, nil
}

// ResolveUserProjectID returns the lowest project ID the user owns or belongs
// to. It returns 0 when the user has no project, which callers treat as the
// deployment-level scope. OpenAPI requests carry no project context, so this
// deterministic scope is used to namespace asset storage keys per project.
func (r *ProjectRepo) ResolveUserProjectID(ctx context.Context, userID int64) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	var ids []int64
	err := r.db.WithContext(ctx).
		Table("project p").
		Where(`p.deleted_time = 0 AND (p.owner_user_id = ?
			OR EXISTS (SELECT 1 FROM project_member pm WHERE pm.project_id = p.id AND pm.user_id = ?)
			OR EXISTS (
				SELECT 1 FROM project_user_group pg
				JOIN sys_user_group g ON g.id = pg.group_id AND g.deleted_time = 0
				JOIN sys_user_group_member gm ON gm.group_id = g.id
				WHERE pg.project_id = p.id AND gm.user_id = ?
			))`, userID, userID, userID).
		Order("p.id ASC").
		Limit(1).
		Pluck("p.id", &ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}

// UserHasProjectAccess reports whether the user owns the project or is a
// member of it. Operations on existing asset files use this per-file check
// instead of comparing against the caller's current lowest project (see
// ResolveUserProjectID), so membership changes never strand previously
// uploaded files.
func (r *ProjectRepo) UserHasProjectAccess(ctx context.Context, userID, projectID int64) (bool, error) {
	if userID <= 0 || projectID <= 0 {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).
		Table("project p").
		Where(`p.deleted_time = 0 AND p.id = ? AND (p.owner_user_id = ?
			OR EXISTS (SELECT 1 FROM project_member pm WHERE pm.project_id = p.id AND pm.user_id = ?)
			OR EXISTS (
				SELECT 1 FROM project_user_group pg
				JOIN sys_user_group g ON g.id = pg.group_id AND g.deleted_time = 0
				JOIN sys_user_group_member gm ON gm.group_id = g.id
				WHERE pg.project_id = p.id AND gm.user_id = ?
			))`, projectID, userID, userID, userID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *ProjectRepo) GetProject(ctx context.Context, id int64) (Project, error) {
	var item Project
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&item).Error
	return item, err
}

func (r *ProjectRepo) GetProjectByNameOrID(ctx context.Context, nameOrID string) (Project, error) {
	value := strings.TrimSpace(nameOrID)
	if value == "" {
		return Project{}, ErrInvalidArgument
	}
	if id, err := strconv.ParseInt(value, 10, 64); err == nil {
		item, err := r.GetProject(ctx, id)
		if err == nil {
			return item, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Project{}, err
		}
	}
	var item Project
	err := r.db.WithContext(ctx).
		Where("deleted_time = 0 AND (project_key = ? OR name = ?)", value, value).
		Order("id").
		Take(&item).Error
	return item, err
}

func (r *ProjectRepo) UpdateProject(ctx context.Context, id int64, item Project) (Project, error) {
	var out Project
	res := r.db.WithContext(ctx).Model(&out).Clauses(returningAll()).
		Where("id = ? AND deleted_time = 0", id).
		Updates(touchByValues(map[string]any{"name": item.Name, "description": item.Description}, item.UpdatedBy, 0))
	if res.Error != nil {
		return Project{}, normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return Project{}, ErrNotFound
	}
	return out, nil
}

func (r *ProjectRepo) UpdateProjectDatabaseName(ctx context.Context, id int64, databaseName string) error {
	res := r.db.WithContext(ctx).Model(&Project{}).
		Where("id = ? AND deleted_time = 0", id).
		Update("database_name", databaseName)
	if res.Error != nil {
		return normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) UpdateProjectAppSchemaName(ctx context.Context, id int64, schemaName string) error {
	res := r.db.WithContext(ctx).Model(&ProjectApp{}).
		Where("id = ? AND deleted_time = 0", id).
		Update("schema_name", schemaName)
	if res.Error != nil {
		return normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateProjectAppIconAsset 单独更新应用图标 asset ID，避免整体覆盖 manifest 等其他字段
func (r *ProjectRepo) UpdateProjectAppIconAsset(ctx context.Context, projectID, appID, iconAssetID int64, userID int64) error {
	res := r.db.WithContext(ctx).Model(&ProjectApp{}).
		Where("id = ? AND project_id = ? AND deleted_time = 0", appID, projectID).
		Updates(touchByValues(map[string]any{"icon_asset_id": iconAssetID}, userID, repoNowMilli()))
	if res.Error != nil {
		return normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) DeleteProject(ctx context.Context, id, userID int64) error {
	res := r.db.WithContext(ctx).Model(&Project{}).
		Where("id = ? AND deleted_time = 0", id).
		Updates(softDeleteValues(userID, 0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

type ProjectRepo struct{ db *gorm.DB }

func NewProjectRepo(in any) *ProjectRepo { return &ProjectRepo{db: GetCommonConn(in)} }
