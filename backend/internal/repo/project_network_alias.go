package repo

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
)

var ErrAppNetworkAliasTaken = errors.New("project.networkAliasTaken")

// 所有 App 共用 runtime 网络，跨项目预占；事务锁防止并发导入抢占同名。
// pending 名与当前名同时保留，覆盖部署失败恢复 manifest 后自动释放新别名。
func (r *ProjectRepo) lockAppNetworkAlias(ctx context.Context, alias string, appID int64) error {
	if alias == "" {
		return nil
	}
	if err := r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "app-network-alias:"+alias).Error; err != nil {
		return err
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&ProjectApp{}).
		Where("deleted_time = 0 AND id <> ?", appID).
		Where("manifest_json->>'network_alias' = ? OR manifest_json->>'network_alias_pending' = ? OR manifest_json->>'container_name' = ?", alias, alias, alias).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrAppNetworkAliasTaken
	}
	return nil
}

func (r *ProjectRepo) CreateProjectAppWithNetworkAlias(ctx context.Context, item ProjectApp, alias string) (ProjectApp, error) {
	if alias == "" {
		return r.CreateProjectApp(ctx, item)
	}
	var created ProjectApp
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := NewProjectRepo(tx)
		if err := repo.lockAppNetworkAlias(ctx, alias, 0); err != nil {
			return err
		}
		manifest := map[string]any{}
		if len(item.ManifestJSON) > 0 {
			if err := json.Unmarshal(item.ManifestJSON, &manifest); err != nil {
				return err
			}
		}
		manifest["network_alias"] = alias
		raw, err := json.Marshal(manifest)
		if err != nil {
			return err
		}
		item.ManifestJSON = raw
		created, err = repo.CreateProjectApp(ctx, item)
		return err
	})
	return created, err
}

func (r *ProjectRepo) MarkAppDeployingWithNetworkAlias(ctx context.Context, projectID, appID, userID int64, alias string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := NewProjectRepo(tx)
		if err := repo.lockAppNetworkAlias(ctx, alias, appID); err != nil {
			return err
		}
		if err := repo.UpdateProjectAppStatusDeployingCAS(ctx, projectID, appID, userID); err != nil {
			return err
		}
		return repo.UpdateProjectAppManifest(ctx, projectID, appID, map[string]any{
			"deploying": true, "deploy_error": "", "network_alias_pending": alias,
		}, userID)
	})
}
