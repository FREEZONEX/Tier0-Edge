package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/common"
	"backend/internal/domain/unsv2"
	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var businessKey = regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)

func sessionPermission(ctx context.Context, key string) bool {
	actor, ok := contextx.SubjectFrom(ctx)
	return ok && actor.AuthType == "session" && !actor.MCPRestricted && contextx.HasResource(actor, key)
}

func (m *Management) Catalog(ctx context.Context, archived, launchable bool) (any, error) {
	actor, _ := contextx.SubjectFrom(ctx)
	manage := !launchable && sessionPermission(ctx, "uns.app.manage")
	member, err := repo.IsActiveWorkspaceMember(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	if actor.AuthType != "session" || actor.MCPRestricted || !member || archived && !manage {
		return nil, forbiddenRequest("App access is forbidden")
	}
	db := repo.GetUnsConn(ctx).Where(repo.NativeAppDefinitions)
	if archived {
		db = db.Where("deleted_at<>0")
	} else {
		db = db.Where("deleted_at=0")
	}
	var definitions []repo.UnsMeta
	if err := db.Order("updated_at DESC,id").Find(&definitions).Error; err != nil {
		return nil, err
	}
	rows, err := repo.NewAppInstallationRepo(ctx).List(ctx)
	if err != nil {
		return nil, err
	}
	installed := make(map[string]repo.AppInstallation, len(rows))
	for _, r := range rows {
		installed[r.AppID] = r
	}
	workspace, err := repo.UserBusinessRoles(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(definitions))
	for _, row := range definitions {
		var app unsv2.AppData
		if err := json.Unmarshal(row.Data, &app); err != nil {
			return nil, err
		}
		_, allowed := unsv2.NativeAppRoles(app, workspace)
		runtime, ok := installed[row.ID]
		if !manage && (!allowed || !ok) {
			continue
		}
		status := runtime.Status
		if !ok {
			status = "notInstalled"
		} else if status == "running" {
			if _, err := m.runtime.Resolve(row.ID); err != nil {
				status = "starting"
			}
		}
		if launchable && (!allowed || status != "running") {
			continue
		}
		item := map[string]any{"appId": row.ID, "name": app.Name, "description": app.Description, "version": runtime.Version, "status": status, "entryPath": "/apps/" + row.ID + "/", "canOpen": allowed && status == "running" && !archived}
		if manage {
			item["revision"] = runtime.Revision
			item["definitionRevision"] = row.Revision
			item["roles"] = app.Roles
			item["lastError"] = runtime.LastError
			item["stage"] = runtime.Stage
			item["desiredState"] = runtime.DesiredState
		}
		items = append(items, item)
	}
	return map[string]any{"items": items, "total": len(items)}, nil
}

func (m *Management) SetTrashed(ctx context.Context, id string, revision int64, trash bool) error {
	if !sessionPermission(ctx, "uns.app.manage") {
		return forbiddenRequest("App management permission required")
	}
	if revision <= 0 {
		return invalidRequest("App revision required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return invalidRequest("Invalid App identity")
	}
	lock := m.appLock(id)
	lock.Lock()
	defer lock.Unlock()
	return metadataTransaction(ctx, func(ctx context.Context) error {
		tx := repo.GetUnsConn(ctx)
		var row repo.UnsMeta
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(repo.NativeAppDefinitions).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		if row.Revision != revision || trash != (row.DeletedAt == 0) {
			return repo.ErrConflict
		}
		var deletedAt int64
		if trash {
			runtime, err := repo.NewAppInstallationRepo(ctx).Get(ctx, id)
			if err != nil && !errors.Is(err, repo.ErrNotFound) {
				return err
			}
			if err == nil && runtime.Stage != "uninstalled" {
				if err = m.runtime.Stop(ctx, id); err != nil {
					return err
				}
			}
			deletedAt = time.Now().UnixMilli()
		}
		return tx.Model(&row).Updates(map[string]any{"deleted_at": deletedAt, "updated_at": time.Now().UnixMilli(), "revision": revision + 1}).Error
	})
}

func BusinessRoles(ctx context.Context, directory bool) (any, error) {
	key := "uns.app.manage"
	if !sessionPermission(ctx, key) {
		return nil, forbiddenRequest("Role directory access is forbidden")
	}
	roles := []repo.BusinessRole{}
	q := repo.GetUnsConn(ctx).Order("name,role_key")
	if !directory {
		q = q.Select("role_key,name,description")
	}
	if err := q.Find(&roles).Error; err != nil {
		return nil, err
	}
	result := map[string]any{"roles": roles}
	if directory {
		users, groups, err := repo.BusinessRoleDirectory(ctx)
		if err != nil {
			return nil, err
		}
		result["users"], result["groups"] = users, groups
		type binding struct {
			Key     string `json:"key"`
			ID      string `json:"id"`
			Name    string `json:"name"`
			Trashed bool   `json:"trashed"`
		}
		bindings := []binding{}
		err = repo.GetUnsConn(ctx).Raw(`SELECT r->>'id' AS key,id,data->>'name' AS name,(deleted_at<>0) AS trashed FROM uns_meta CROSS JOIN LATERAL jsonb_array_elements(COALESCE(NULLIF(data->'roles','null'),'[]')) r WHERE ` + repo.NativeAppDefinitions + ` ORDER BY name,id`).Scan(&bindings).Error
		if err != nil {
			return nil, err
		}
		result["bindings"] = bindings
		used := map[string]bool{}
		for _, b := range bindings {
			used[b.Key] = true
		}
		for i := range roles {
			roles[i].CanDelete = !used[roles[i].Key]
		}
		result["roles"] = roles
	}
	return result, nil
}

func SaveBusinessRole(ctx context.Context, r repo.BusinessRole) error {
	permission := "uns.app.manage"
	if !sessionPermission(ctx, permission) {
		return forbiddenRequest("Business role management permission required")
	}
	r.Name = strings.TrimSpace(r.Name)
	if !businessKey.MatchString(r.Key) || len(r.Key) > 63 || r.Name == "" || common.ExceedNameLimit(r.Name) || common.ExceedDescLimit(r.Description) || r.Revision < 0 {
		return invalidRequest("Invalid business role")
	}
	if r.UserIDs == nil {
		r.UserIDs = []string{}
	}
	if r.GroupIDs == nil {
		r.GroupIDs = []string{}
	}
	return repo.SaveBusinessRole(ctx, r)
}
func DeleteBusinessRole(ctx context.Context, key string, revision int64) error {
	if !sessionPermission(ctx, "uns.app.manage") {
		return forbiddenRequest("Business role deletion permission required")
	}
	return repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := repo.LockBusinessRoles(tx); err != nil {
			return err
		}
		var role repo.BusinessRole
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("role_key=?", key).Take(&role).Error; err != nil {
			return err
		}
		if role.Revision != revision {
			return repo.ErrConflict
		}
		probe, _ := json.Marshal([]map[string]string{{"id": key}})
		var count int64
		if err := tx.Model(&repo.UnsMeta{}).Where(repo.NativeAppDefinitions).Where("data->'roles' @> ?::jsonb", string(probe)).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return &requestError{status: 409, message: "Business role is still used by an App"}
		}
		return tx.Delete(&role).Error
	})
}

func ReplaceBusinessRoleAssignments(ctx context.Context, kind, id string, keys, expected []string) error {
	if !sessionPermission(ctx, "uns.app.manage") {
		return forbiddenRequest("App role management permission required")
	}
	if (kind != "user" && kind != "group") || id == "" || len(keys) > 2000 || len(expected) > 2000 {
		return invalidRequest("Invalid business role assignment")
	}
	return repo.ReplaceBusinessRoleAssignments(ctx, kind, id, keys, expected)
}
