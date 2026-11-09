package repo

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// Native App definitions use a local entry; external App registrations retain their own access model.
const NativeAppDefinitions = "kind='app' AND mount_id IS NULL AND data->'entry'->>'basePath'=('/apps/'||id) AND COALESCE(data->'entry'->>'environment','')=''"

func IsActiveWorkspaceMember(ctx context.Context, userID int64) (bool, error) {
	var active bool
	err := GetCommonConn(ctx).Raw(`SELECT EXISTS(SELECT 1 FROM sys_workspace_user wu JOIN sys_user_info u ON u.user_id=wu.user_id WHERE wu.workspace_id=1 AND wu.user_id=? AND wu.deleted_time=0 AND u.deleted_time=0 AND u.status=1)`, userID).Scan(&active).Error
	return active, err
}

type BusinessRole struct {
	Key         string   `gorm:"column:role_key;primaryKey" json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	UserIDs     []string `gorm:"column:user_ids;serializer:json" json:"userIds,omitempty"`
	GroupIDs    []string `gorm:"column:group_ids;serializer:json" json:"groupIds,omitempty"`
	Revision    int64    `json:"revision"`
	CanDelete   bool     `gorm:"-" json:"canDelete"`
}

func (BusinessRole) TableName() string { return "workspace_business_role" }

// App bindings live in UNS metadata. Hold the role rows until that metadata
// commits so deleting an unused role cannot race a new binding.
func LockBusinessRoles(tx *gorm.DB) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(814206109)").Error
}

func RegisterBusinessRoles(tx *gorm.DB, roles []BusinessRole) error {
	if len(roles) == 0 {
		return nil
	}
	if err := LockBusinessRoles(tx); err != nil {
		return err
	}
	for _, role := range roles {
		if err := tx.Exec(`INSERT INTO workspace_business_role (role_key,name,description) VALUES (?,?,?)
     ON CONFLICT (role_key) DO NOTHING`, role.Key, role.Name, role.Description).Error; err != nil {
			return err
		}
	}
	return nil
}

func UserBusinessRoles(ctx context.Context, userID int64) (map[string]string, error) {
	var groups []string
	var roles []BusinessRole
	err := GetCommonConn(ctx).Raw(`SELECT g.id::text FROM sys_user_group_member gm JOIN sys_user_group g ON g.id=gm.group_id WHERE gm.user_id=? AND g.deleted_time=0`, userID).Scan(&groups).Error
	if err != nil {
		return nil, err
	}
	err = GetUnsConn(ctx).Raw(`SELECT role_key,name FROM workspace_business_role WHERE jsonb_exists(user_ids,?) OR jsonb_exists_any(group_ids,string_to_array(?,','))`, strconv.FormatInt(userID, 10), strings.Join(groups, ",")).Scan(&roles).Error
	result := make(map[string]string, len(roles))
	for _, role := range roles {
		result[role.Key] = role.Name
	}
	return result, err
}

type BusinessRoleOption struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	MemberIDs []string `gorm:"column:member_ids;serializer:json" json:"memberIds,omitempty"`
}

func BusinessRoleDirectory(ctx context.Context) (users, groups []BusinessRoleOption, err error) {
	users, groups = []BusinessRoleOption{}, []BusinessRoleOption{}
	db := GetCommonConn(ctx)
	err = db.Raw("SELECT user_id::text AS id, COALESCE(NULLIF(nick_name,''),user_name) AS name FROM sys_user_info WHERE deleted_time=0 AND status=1 AND EXISTS(SELECT 1 FROM sys_workspace_user w WHERE w.workspace_id=1 AND w.user_id=sys_user_info.user_id AND w.deleted_time=0) ORDER BY name,user_id").Scan(&users).Error
	if err == nil {
		err = db.Raw(`SELECT g.id::text AS id,g.group_name AS name,COALESCE((SELECT jsonb_agg(gm.user_id::text ORDER BY gm.user_id) FROM sys_user_group_member gm JOIN sys_user_info u ON u.user_id=gm.user_id WHERE gm.group_id=g.id AND u.status=1 AND u.deleted_time=0 AND EXISTS(SELECT 1 FROM sys_workspace_user wu WHERE wu.workspace_id=1 AND wu.user_id=u.user_id AND wu.deleted_time=0)), '[]') AS member_ids FROM sys_user_group g WHERE g.deleted_time=0 ORDER BY g.group_name,g.id`).Scan(&groups).Error
	}
	return
}

func SaveBusinessRole(ctx context.Context, r BusinessRole) error {
	return GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockBusinessRoles(tx); err != nil {
			return err
		}
		for _, entry := range []struct {
			ids                  []string
			table, id, condition string
		}{
			{r.UserIDs, "sys_user_info", "user_id", "deleted_time=0 AND status=1 AND EXISTS(SELECT 1 FROM sys_workspace_user w WHERE w.workspace_id=1 AND w.user_id=sys_user_info.user_id AND w.deleted_time=0)"},
			{r.GroupIDs, "sys_user_group", "id", "deleted_time=0"},
		} {
			if len(entry.ids) == 0 {
				continue
			}
			ids := make([]int64, 0, len(entry.ids))
			for _, s := range entry.ids {
				i, err := strconv.ParseInt(s, 10, 64)
				if err != nil || i <= 0 || strconv.FormatInt(i, 10) != s {
					return ErrInvalidArgument
				}
				ids = append(ids, i)
			}
			var count int64
			if err := GetCommonConn(ctx).Table(entry.table).Where(entry.condition).Where(entry.id+" IN ?", ids).Count(&count).Error; err != nil {
				return err
			}
			if count != int64(len(ids)) {
				return ErrInvalidArgument
			}
		}
		users, _ := json.Marshal(r.UserIDs)
		groups, _ := json.Marshal(r.GroupIDs)
		var q *gorm.DB
		if r.Revision == 0 {
			q = tx.Exec(`INSERT INTO workspace_business_role (role_key,name,description,user_ids,group_ids) VALUES (?,?,?,?::jsonb,?::jsonb) ON CONFLICT DO NOTHING`, r.Key, r.Name, r.Description, string(users), string(groups))
		} else {
			q = tx.Exec(`UPDATE workspace_business_role SET name=?,description=?,user_ids=?::jsonb,group_ids=?::jsonb,revision=revision+1 WHERE role_key=? AND revision=?`, r.Name, r.Description, string(users), string(groups), r.Key, r.Revision)
		}
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// Replace one member's or group's direct grants atomically. Other principals and
// inherited grants are untouched. Compare the observed set to reject stale forms.
func ReplaceBusinessRoleAssignments(ctx context.Context, kind, id string, keys, expected []string) error {
	field := "user_ids"
	if kind == "group" {
		field = "group_ids"
	}
	return GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockBusinessRoles(tx); err != nil {
			return err
		}
		var roles []BusinessRole
		if err := tx.Order("role_key").Find(&roles).Error; err != nil {
			return err
		}
		current := []string{}
		available := map[string]bool{}
		for _, role := range roles {
			available[role.Key] = true
			ids := role.UserIDs
			if kind == "group" {
				ids = role.GroupIDs
			}
			if slices.Contains(ids, id) {
				current = append(current, role.Key)
			}
		}
		wanted := map[string]bool{}
		for _, key := range keys {
			if !available[key] || wanted[key] {
				return ErrInvalidArgument
			}
			wanted[key] = true
		}
		observed := append([]string{}, expected...)
		slices.Sort(observed)
		if !slices.Equal(current, observed) {
			return ErrConflict
		}
		// Validate identities under the same assignment lock used by role deletion.
		users, groups, err := BusinessRoleDirectory(ctx)
		if err != nil {
			return err
		}
		targets := users
		if kind == "group" {
			targets = groups
		}
		if !slices.ContainsFunc(targets, func(t BusinessRoleOption) bool { return t.ID == id }) {
			return ErrNotFound
		}
		for _, role := range roles {
			ids := role.UserIDs
			if kind == "group" {
				ids = role.GroupIDs
			}
			has := slices.Contains(ids, id)
			if has == wanted[role.Key] {
				continue
			}
			next := slices.DeleteFunc(append([]string{}, ids...), func(v string) bool { return v == id })
			if wanted[role.Key] {
				next = append(next, id)
			}
			raw, err := json.Marshal(next)
			if err != nil {
				return err
			}
			if err := tx.Model(&BusinessRole{}).Where("role_key=?", role.Key).Updates(map[string]any{field: string(raw), "revision": gorm.Expr("revision+1")}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
