package repo

import (
	"context"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserGroup struct {
	ID          int64  `gorm:"column:id;primaryKey" json:"groupId"`
	GroupName   string `gorm:"column:group_name;size:63" json:"groupName"`
	Description string `gorm:"column:description;size:512" json:"description"`
	MemberCount int64  `gorm:"column:member_count;->" json:"memberCount"`
	CreatedBy   int64  `gorm:"column:created_by" json:"createdBy"`
	UpdatedBy   int64  `gorm:"column:updated_by" json:"updatedBy"`
	CreatedTime int64  `gorm:"column:created_time" json:"createdTime"`
	UpdatedTime int64  `gorm:"column:updated_time" json:"updatedTime"`
	DeletedTime int64  `gorm:"column:deleted_time" json:"-"`
}

func (UserGroup) TableName() string { return "sys_user_group" }

type UserGroupMember struct {
	GroupID     int64  `gorm:"column:group_id;primaryKey" json:"groupId"`
	UserID      int64  `gorm:"column:user_id;primaryKey" json:"userId"`
	UserName    string `gorm:"-" json:"userName"`
	DisplayName string `gorm:"-" json:"displayName"`
	Email       string `gorm:"-" json:"email"`
	Enabled     bool   `gorm:"-" json:"enabled"`
	CreatedBy   int64  `gorm:"column:created_by" json:"createdBy"`
	CreatedTime int64  `gorm:"column:created_time" json:"createdTime"`
}

func (UserGroupMember) TableName() string { return "sys_user_group_member" }

type UserGroupRepo struct{ db *gorm.DB }

func NewUserGroupRepo(in any) *UserGroupRepo { return &UserGroupRepo{db: GetCommonConn(in)} }

func (r *UserGroupRepo) List(ctx context.Context, keyword string, page, size int) ([]UserGroup, int64, error) {
	q := r.db.WithContext(ctx).Table("sys_user_group g").Where("g.deleted_time = 0")
	if value := strings.TrimSpace(keyword); value != "" {
		q = q.Where("g.group_name ILIKE ? OR g.description ILIKE ?", "%"+value+"%", "%"+value+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	var groups []UserGroup
	err := q.Select("g.*, COUNT(gm.user_id) AS member_count").
		Joins("LEFT JOIN sys_user_group_member gm ON gm.group_id = g.id").
		Group("g.id").Order("g.id DESC").Offset((page - 1) * size).Limit(size).Scan(&groups).Error
	return groups, total, err
}

func (r *UserGroupRepo) Get(ctx context.Context, groupID int64) (UserGroup, error) {
	var group UserGroup
	err := r.db.WithContext(ctx).Table("sys_user_group g").
		Select("g.*, COUNT(gm.user_id) AS member_count").
		Joins("LEFT JOIN sys_user_group_member gm ON gm.group_id = g.id").
		Where("g.id = ? AND g.deleted_time = 0", groupID).
		Group("g.id").Take(&group).Error
	return group, err
}

func (r *UserGroupRepo) GetByName(ctx context.Context, groupName string) (UserGroup, error) {
	var group UserGroup
	err := r.db.WithContext(ctx).Where("LOWER(group_name) = LOWER(?) AND deleted_time = 0", strings.TrimSpace(groupName)).Take(&group).Error
	return group, err
}

func (r *UserGroupRepo) Create(ctx context.Context, groupName, description string, userID int64) (UserGroup, error) {
	name := strings.TrimSpace(groupName)
	if name == "" {
		return UserGroup{}, ErrInvalidArgument
	}
	now := repoNowMilli()
	group := UserGroup{GroupName: name, Description: strings.TrimSpace(description), CreatedBy: userID, UpdatedBy: userID, CreatedTime: now, UpdatedTime: now}
	if err := r.db.WithContext(ctx).Create(&group).Error; err != nil {
		return UserGroup{}, normalizeDBError(err)
	}
	return group, nil
}

func (r *UserGroupRepo) Update(ctx context.Context, groupID int64, groupName, description string, userID int64) (UserGroup, error) {
	name := strings.TrimSpace(groupName)
	if groupID <= 0 || name == "" {
		return UserGroup{}, ErrInvalidArgument
	}
	now := repoNowMilli()
	res := r.db.WithContext(ctx).Model(&UserGroup{}).Where("id = ? AND deleted_time = 0", groupID).
		Updates(map[string]any{"group_name": name, "description": strings.TrimSpace(description), "updated_by": userID, "updated_time": now})
	if res.Error != nil {
		return UserGroup{}, normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return UserGroup{}, ErrNotFound
	}
	return r.Get(ctx, groupID)
}

func (r *UserGroupRepo) Delete(ctx context.Context, groupID, userID int64) error {
	if groupID <= 0 {
		return ErrInvalidArgument
	}
	now := repoNowMilli()
	return normalizeDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var projectGroupIDs []int64
		if err := tx.Table("project_user_group").Where("group_id = ?", groupID).Pluck("id", &projectGroupIDs).Error; err != nil {
			return err
		}
		if len(projectGroupIDs) > 0 {
			if err := tx.Exec("DELETE FROM project_user_group_role WHERE project_user_group_id IN ?", projectGroupIDs).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec("DELETE FROM project_user_group WHERE group_id = ?", groupID).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM sys_user_group_member WHERE group_id = ?", groupID).Error; err != nil {
			return err
		}
		res := tx.Model(&UserGroup{}).Where("id = ? AND deleted_time = 0", groupID).
			Updates(map[string]any{"updated_by": userID, "updated_time": now, "deleted_time": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	}))
}

func (r *UserGroupRepo) ListMembers(ctx context.Context, groupID int64) ([]UserGroupMember, error) {
	type row struct {
		GroupID     int64  `gorm:"column:group_id"`
		UserID      int64  `gorm:"column:user_id"`
		UserName    string `gorm:"column:user_name"`
		DisplayName string `gorm:"column:display_name"`
		Email       string `gorm:"column:email"`
		Status      int64  `gorm:"column:status"`
		CreatedBy   int64  `gorm:"column:created_by"`
		CreatedTime int64  `gorm:"column:created_time"`
	}
	var rows []row
	err := r.db.WithContext(ctx).Table("sys_user_group_member gm").
		Select("gm.group_id, gm.user_id, u.user_name, u.nick_name AS display_name, u.email, u.status, gm.created_by, gm.created_time").
		Joins("JOIN sys_user_info u ON u.user_id = gm.user_id AND u.deleted_time = 0").
		Where("gm.group_id = ?", groupID).Order("u.user_name, u.user_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]UserGroupMember, 0, len(rows))
	for _, item := range rows {
		email, _, err := decryptUserContacts(item.UserID, item.Email, "")
		if err != nil {
			return nil, err
		}
		out = append(out, UserGroupMember{GroupID: item.GroupID, UserID: item.UserID, UserName: item.UserName, DisplayName: item.DisplayName, Email: email, Enabled: item.Status == 1, CreatedBy: item.CreatedBy, CreatedTime: item.CreatedTime})
	}
	return out, nil
}

func (r *UserGroupRepo) ReplaceMembers(ctx context.Context, groupID int64, userIDs []int64, userID int64) error {
	if groupID <= 0 {
		return ErrInvalidArgument
	}
	unique := make([]int64, 0, len(userIDs))
	seen := map[int64]struct{}{}
	for _, id := range userIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return normalizeDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var groupCount int64
		if err := tx.Model(&UserGroup{}).Where("id = ? AND deleted_time = 0", groupID).Count(&groupCount).Error; err != nil {
			return err
		}
		if groupCount == 0 {
			return ErrNotFound
		}
		if len(unique) > 0 {
			var userCount int64
			if err := tx.Model(&sysUserInfo{}).Where("user_id IN ? AND deleted_time = 0", unique).Count(&userCount).Error; err != nil {
				return err
			}
			if userCount != int64(len(unique)) {
				return ErrNotFound
			}
		}
		if err := tx.Exec("DELETE FROM sys_user_group_member WHERE group_id = ?", groupID).Error; err != nil {
			return err
		}
		now := repoNowMilli()
		for _, id := range unique {
			if err := tx.Create(&UserGroupMember{GroupID: groupID, UserID: id, CreatedBy: userID, CreatedTime: now}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&UserGroup{}).Where("id = ?", groupID).Updates(map[string]any{"updated_by": userID, "updated_time": now}).Error
	}))
}

func (r *UserGroupRepo) AddMember(ctx context.Context, groupID, memberUserID, actorID int64) error {
	if groupID <= 0 || memberUserID <= 0 {
		return ErrInvalidArgument
	}
	now := repoNowMilli()
	return normalizeDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var group UserGroup
		if err := tx.Where("id = ? AND deleted_time = 0", groupID).Take(&group).Error; err != nil {
			return err
		}
		if err := ensureProjectUserExists(tx, memberUserID); err != nil {
			return err
		}
		member := UserGroupMember{GroupID: groupID, UserID: memberUserID, CreatedBy: actorID, CreatedTime: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&member).Error; err != nil {
			return err
		}
		return tx.Model(&UserGroup{}).Where("id = ?", groupID).Updates(map[string]any{"updated_by": actorID, "updated_time": now}).Error
	}))
}

func (r *UserGroupRepo) ListAllMembers(ctx context.Context) ([]UserGroupMember, error) {
	type row struct {
		GroupID     int64  `gorm:"column:group_id"`
		UserID      int64  `gorm:"column:user_id"`
		UserName    string `gorm:"column:user_name"`
		CreatedBy   int64  `gorm:"column:created_by"`
		CreatedTime int64  `gorm:"column:created_time"`
	}
	var rows []row
	err := r.db.WithContext(ctx).Table("sys_user_group_member gm").
		Select("gm.group_id, gm.user_id, u.user_name, gm.created_by, gm.created_time").
		Joins("JOIN sys_user_info u ON u.user_id = gm.user_id AND u.deleted_time = 0").
		Joins("JOIN sys_user_group g ON g.id = gm.group_id AND g.deleted_time = 0").
		Order("gm.group_id, u.user_name, gm.user_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]UserGroupMember, 0, len(rows))
	for _, item := range rows {
		out = append(out, UserGroupMember{GroupID: item.GroupID, UserID: item.UserID, UserName: item.UserName, CreatedBy: item.CreatedBy, CreatedTime: item.CreatedTime})
	}
	return out, nil
}

func (r *UserGroupRepo) UserGroupsByUserIDs(ctx context.Context, userIDs []int64) (map[int64][]UserGroup, error) {
	result := make(map[int64][]UserGroup, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	type row struct {
		UserID      int64  `gorm:"column:user_id"`
		GroupID     int64  `gorm:"column:group_id"`
		GroupName   string `gorm:"column:group_name"`
		Description string `gorm:"column:description"`
	}
	var rows []row
	err := r.db.WithContext(ctx).Table("sys_user_group_member gm").
		Select("gm.user_id, g.id AS group_id, g.group_name, g.description").
		Joins("JOIN sys_user_group g ON g.id = gm.group_id AND g.deleted_time = 0").
		Where("gm.user_id IN ?", userIDs).
		Order("g.group_name, g.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, item := range rows {
		result[item.UserID] = append(result[item.UserID], UserGroup{
			ID: item.GroupID, GroupName: item.GroupName, Description: item.Description,
		})
	}
	return result, nil
}

type ProjectUserGroup struct {
	ID          int64         `gorm:"column:id;primaryKey" json:"projectGroupId"`
	ProjectID   int64         `gorm:"column:project_id" json:"projectId"`
	GroupID     int64         `gorm:"column:group_id" json:"groupId"`
	GroupName   string        `gorm:"column:group_name;->" json:"groupName"`
	Description string        `gorm:"column:description;->" json:"description"`
	MemberCount int64         `gorm:"column:member_count;->" json:"memberCount"`
	Roles       []ProjectRole `gorm:"-" json:"roles"`
	CreatedBy   int64         `gorm:"column:created_by" json:"createdBy"`
	UpdatedBy   int64         `gorm:"column:updated_by" json:"updatedBy"`
	CreatedTime int64         `gorm:"column:created_time" json:"createdTime"`
	UpdatedTime int64         `gorm:"column:updated_time" json:"updatedTime"`
}

func (ProjectUserGroup) TableName() string { return "project_user_group" }

type ProjectUserGroupSave struct {
	ProjectID      int64
	ProjectGroupID int64
	GroupID        int64
	RoleIDs        []int64
	UserID         int64
}

func (r *ProjectRepo) ListProjectUserGroups(ctx context.Context, projectID int64) ([]ProjectUserGroup, error) {
	var groups []ProjectUserGroup
	err := r.db.WithContext(ctx).Table("project_user_group pg").
		Select("pg.*, g.group_name, g.description, COUNT(DISTINCT gm.user_id) AS member_count").
		Joins("JOIN sys_user_group g ON g.id = pg.group_id AND g.deleted_time = 0").
		Joins("LEFT JOIN sys_user_group_member gm ON gm.group_id = g.id").
		Where("pg.project_id = ?", projectID).Group("pg.id, g.group_name, g.description").Order("pg.id").Scan(&groups).Error
	if err != nil {
		return nil, err
	}
	for i := range groups {
		groups[i].Roles, err = r.projectUserGroupRoles(ctx, projectID, groups[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return groups, nil
}

func (r *ProjectRepo) UpsertProjectUserGroup(ctx context.Context, input ProjectUserGroupSave) (ProjectUserGroup, error) {
	if input.ProjectID <= 0 || input.GroupID <= 0 {
		return ProjectUserGroup{}, ErrInvalidArgument
	}
	now := repoNowMilli()
	var projectGroupID int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&UserGroup{}).Where("id = ? AND deleted_time = 0", input.GroupID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
		row := ProjectUserGroup{ProjectID: input.ProjectID, GroupID: input.GroupID, CreatedBy: input.UserID, UpdatedBy: input.UserID, CreatedTime: now, UpdatedTime: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "project_id"}, {Name: "group_id"}}, DoUpdates: clause.Assignments(map[string]any{"updated_by": input.UserID, "updated_time": now})}).Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Table("project_user_group").Select("id").Where("project_id = ? AND group_id = ?", input.ProjectID, input.GroupID).Scan(&projectGroupID).Error; err != nil {
			return err
		}
		return replaceProjectUserGroupRoles(tx, input.ProjectID, projectGroupID, input.RoleIDs, now)
	})
	if err != nil {
		return ProjectUserGroup{}, normalizeDBError(err)
	}
	return r.getProjectUserGroup(ctx, input.ProjectID, projectGroupID)
}

func (r *ProjectRepo) UpdateProjectUserGroup(ctx context.Context, input ProjectUserGroupSave) (ProjectUserGroup, error) {
	if input.ProjectID <= 0 || input.ProjectGroupID <= 0 {
		return ProjectUserGroup{}, ErrInvalidArgument
	}
	now := repoNowMilli()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&ProjectUserGroup{}).Where("project_id = ? AND id = ?", input.ProjectID, input.ProjectGroupID).
			Updates(map[string]any{"updated_by": input.UserID, "updated_time": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return replaceProjectUserGroupRoles(tx, input.ProjectID, input.ProjectGroupID, input.RoleIDs, now)
	})
	if err != nil {
		return ProjectUserGroup{}, normalizeDBError(err)
	}
	return r.getProjectUserGroup(ctx, input.ProjectID, input.ProjectGroupID)
}

func (r *ProjectRepo) DeleteProjectUserGroup(ctx context.Context, projectID, projectGroupID int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM project_user_group_role WHERE project_id = ? AND project_user_group_id = ?", projectID, projectGroupID).Error; err != nil {
			return err
		}
		res := tx.Exec("DELETE FROM project_user_group WHERE project_id = ? AND id = ?", projectID, projectGroupID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *ProjectRepo) getProjectUserGroup(ctx context.Context, projectID, projectGroupID int64) (ProjectUserGroup, error) {
	items, err := r.ListProjectUserGroups(ctx, projectID)
	if err != nil {
		return ProjectUserGroup{}, err
	}
	for _, item := range items {
		if item.ID == projectGroupID {
			return item, nil
		}
	}
	return ProjectUserGroup{}, ErrNotFound
}

func (r *ProjectRepo) projectUserGroupRoles(ctx context.Context, projectID, projectGroupID int64) ([]ProjectRole, error) {
	var roles []ProjectRole
	err := r.db.WithContext(ctx).Table("project_user_group_role gr").Select("r.*").
		Joins("JOIN project_role r ON r.project_id = gr.project_id AND r.id = gr.role_id").
		Where("gr.project_id = ? AND gr.project_user_group_id = ?", projectID, projectGroupID).Order("r.id").Scan(&roles).Error
	return roles, err
}

func replaceProjectUserGroupRoles(tx *gorm.DB, projectID, projectGroupID int64, roleIDs []int64, now int64) error {
	if err := tx.Exec("DELETE FROM project_user_group_role WHERE project_id = ? AND project_user_group_id = ?", projectID, projectGroupID).Error; err != nil {
		return err
	}
	unique := map[int64]struct{}{}
	for _, roleID := range roleIDs {
		if roleID <= 0 {
			continue
		}
		if _, ok := unique[roleID]; ok {
			continue
		}
		unique[roleID] = struct{}{}
		var count int64
		if err := tx.Model(&ProjectRole{}).Where("project_id = ? AND id = ?", projectID, roleID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
		if err := tx.Table("project_user_group_role").Create(map[string]any{"project_id": projectID, "project_user_group_id": projectGroupID, "role_id": roleID, "created_time": now}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *ProjectRepo) ListEffectiveProjectMembers(ctx context.Context, projectID int64) ([]ProjectMember, error) {
	return r.effectiveProjectMembers(ctx, projectID, 0)
}

func (r *ProjectRepo) GetEffectiveProjectMember(ctx context.Context, projectID, userID int64) (ProjectMember, error) {
	members, err := r.effectiveProjectMembers(ctx, projectID, userID)
	if err != nil {
		return ProjectMember{}, err
	}
	if len(members) == 0 {
		return ProjectMember{}, ErrNotFound
	}
	return members[0], nil
}

func (r *ProjectRepo) effectiveProjectMembers(ctx context.Context, projectID, userID int64) ([]ProjectMember, error) {
	type row struct {
		MemberID        int64  `gorm:"column:member_id"`
		UserID          int64  `gorm:"column:user_id"`
		UserName        string `gorm:"column:user_name"`
		Email           string `gorm:"column:email"`
		CreatedTime     int64  `gorm:"column:created_time"`
		UpdatedTime     int64  `gorm:"column:updated_time"`
		RoleID          int64  `gorm:"column:role_id"`
		RoleKey         string `gorm:"column:role_key"`
		RoleName        string `gorm:"column:role_name"`
		RoleDescription string `gorm:"column:role_description"`
	}
	query := `
		SELECT m.id AS member_id, m.user_id, u.user_name, u.email, m.created_time, m.updated_time,
		       COALESCE(r.id, 0) AS role_id, COALESCE(r.role_key, '') AS role_key, COALESCE(r.name, '') AS role_name, COALESCE(r.description, '') AS role_description
		FROM project_member m
		JOIN sys_user_info u ON u.user_id = m.user_id AND u.deleted_time = 0 AND u.status = 1
		LEFT JOIN project_member_role mr ON mr.project_id = m.project_id AND mr.member_id = m.id
		LEFT JOIN project_role r ON r.project_id = mr.project_id AND r.id = mr.role_id
		WHERE m.project_id = ? AND m.status = 'active' AND (? = 0 OR m.user_id = ?)
		UNION ALL
		SELECT 0 AS member_id, gm.user_id, u.user_name, u.email,
		       GREATEST(pg.created_time, gm.created_time) AS created_time,
		       GREATEST(pg.updated_time, g.updated_time, gm.created_time) AS updated_time,
		       COALESCE(r.id, 0) AS role_id, COALESCE(r.role_key, '') AS role_key, COALESCE(r.name, '') AS role_name, COALESCE(r.description, '') AS role_description
		FROM project_user_group pg
		JOIN sys_user_group g ON g.id = pg.group_id AND g.deleted_time = 0
		JOIN sys_user_group_member gm ON gm.group_id = g.id
		JOIN sys_user_info u ON u.user_id = gm.user_id AND u.deleted_time = 0 AND u.status = 1
		LEFT JOIN project_user_group_role gr ON gr.project_id = pg.project_id AND gr.project_user_group_id = pg.id
		LEFT JOIN project_role r ON r.project_id = gr.project_id AND r.id = gr.role_id
		WHERE pg.project_id = ? AND (? = 0 OR gm.user_id = ?)`
	var rows []row
	if err := r.db.WithContext(ctx).Raw(query, projectID, userID, userID, projectID, userID, userID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	byUser := map[int64]*ProjectMember{}
	roleSeen := map[int64]map[int64]struct{}{}
	for _, item := range rows {
		member := byUser[item.UserID]
		if member == nil {
			email, _, err := decryptUserContacts(item.UserID, item.Email, "")
			if err != nil {
				return nil, err
			}
			member = &ProjectMember{ID: item.MemberID, ProjectID: projectID, UserID: item.UserID, UserName: item.UserName, Email: email, Status: "active", Roles: []ProjectRole{}, CreatedTime: item.CreatedTime, UpdatedTime: item.UpdatedTime}
			byUser[item.UserID] = member
			roleSeen[item.UserID] = map[int64]struct{}{}
		} else {
			if item.MemberID > 0 {
				member.ID = item.MemberID
			}
			if item.CreatedTime > 0 && (member.CreatedTime == 0 || item.CreatedTime < member.CreatedTime) {
				member.CreatedTime = item.CreatedTime
			}
			if item.UpdatedTime > member.UpdatedTime {
				member.UpdatedTime = item.UpdatedTime
			}
		}
		if item.RoleID > 0 {
			if _, exists := roleSeen[item.UserID][item.RoleID]; !exists {
				roleSeen[item.UserID][item.RoleID] = struct{}{}
				member.Roles = append(member.Roles, ProjectRole{ID: item.RoleID, ProjectID: projectID, RoleKey: item.RoleKey, Name: item.RoleName, Description: item.RoleDescription})
			}
		}
	}
	out := make([]ProjectMember, 0, len(byUser))
	for _, member := range byUser {
		sort.Slice(member.Roles, func(i, j int) bool { return member.Roles[i].ID < member.Roles[j].ID })
		out = append(out, *member)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out, nil
}

func (r *ProjectRepo) effectiveProjectRoleCounts(ctx context.Context, projectID int64) (map[int64]int64, error) {
	type row struct {
		RoleID int64 `gorm:"column:role_id"`
		Count  int64 `gorm:"column:member_count"`
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(`
		SELECT role_id, COUNT(DISTINCT user_id) AS member_count FROM (
			SELECT mr.role_id, m.user_id
			FROM project_member_role mr
			JOIN project_member m ON m.project_id = mr.project_id AND m.id = mr.member_id
			JOIN sys_user_info u ON u.user_id = m.user_id AND u.deleted_time = 0 AND u.status = 1
			WHERE mr.project_id = ? AND m.status = 'active'
			UNION ALL
			SELECT gr.role_id, gm.user_id
			FROM project_user_group_role gr
			JOIN project_user_group pg ON pg.project_id = gr.project_id AND pg.id = gr.project_user_group_id
			JOIN sys_user_group g ON g.id = pg.group_id AND g.deleted_time = 0
			JOIN sys_user_group_member gm ON gm.group_id = g.id
			JOIN sys_user_info u ON u.user_id = gm.user_id AND u.deleted_time = 0 AND u.status = 1
			WHERE gr.project_id = ?
		) effective_roles GROUP BY role_id`, projectID, projectID).Scan(&rows).Error
	counts := make(map[int64]int64, len(rows))
	for _, item := range rows {
		counts[item.RoleID] = item.Count
	}
	return counts, err
}
