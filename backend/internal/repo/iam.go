package repo

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ---- public DTOs ----

type User struct {
	ID          int64  `gorm:"column:user_id;primaryKey"`
	UserName    string `gorm:"column:user_name"`
	NickName    string `gorm:"column:nick_name"`
	Email       string `gorm:"column:email"`
	Phone       string `gorm:"column:phone"`
	Password    string `gorm:"column:password"`
	Status      int64  `gorm:"column:status"`
	IsRandomPwd bool   `gorm:"column:is_random_pwd"`
}

func (User) TableName() string { return "sys_user_info" }

type UserCreate struct {
	UserName    string
	NickName    string
	Email       string
	Phone       string
	Password    string
	Status      int64
	IsRandomPwd bool
	RoleID      int64
	UserID      int64
}

type UserUpdate struct {
	UserID   int64
	UserName string
	NickName string
	Email    string
	Phone    string
	Status   int64
	RoleID   int64
	HasRole  bool
	ActorID  int64
}

type UserBatchUpdate struct {
	UserIDs   []int64
	HomePages []UserHomePageUpdate
	RoleID    int64
	HasRole   bool
	ActorID   int64
}

type Role struct {
	ID               int64               `json:"id"`
	Name             string              `json:"name"`
	Code             string              `json:"code"`
	Status           int64               `json:"status"`
	DefaultHomePage  string              `json:"defaultHomePage"`
	ResourceList     []map[string]string `json:"resourceList,omitempty"`
	DenyResourceList []map[string]string `json:"denyResourceList,omitempty"`
}

type Resource struct {
	ID              int64            `json:"id"`
	ParentID        int64            `json:"parentId"`
	ResourceKey     string           `json:"resourceKey"`
	Name            string           `json:"name"`
	Type            int64            `json:"type"`
	RoutePath       string           `json:"routePath"`
	UrlType         int64            `json:"urlType"`
	OpenType        int64            `json:"openType"`
	Icon            string           `json:"icon"`
	Sort            int64            `json:"sort"`
	SystemGenerated int64            `json:"systemGenerated"`
	Actions         []ResourceAction `json:"actions,omitempty"`
	ActionCount     int64            `json:"actionCount"`
	Children        []Resource       `json:"children,omitempty"`
}

type ResourceAction struct {
	ID              int64  `gorm:"column:id" json:"id"`
	ResourceID      int64  `gorm:"column:resource_id" json:"resourceId"`
	ResourceKey     string `gorm:"column:resource_key" json:"resourceKey,omitempty"`
	ActionType      string `gorm:"column:action_type" json:"actionType"`
	ActionValue     string `gorm:"column:action_value" json:"actionValue"`
	Methods         string `gorm:"column:methods" json:"methods"`
	Enabled         int64  `gorm:"column:enabled" json:"enabled"`
	SystemGenerated int64  `gorm:"column:system_generated" json:"systemGenerated"`
}

// ---- internal gorm models for the sys_* tables (shared with seed/apikey) ----

type sysUserInfo struct {
	ID          int64  `gorm:"column:user_id;primaryKey"`
	UserName    string `gorm:"column:user_name"`
	NickName    string `gorm:"column:nick_name"`
	Password    string `gorm:"column:password"`
	Email       string `gorm:"column:email"`
	Phone       string `gorm:"column:phone"`
	Status      int64  `gorm:"column:status"`
	IsRandomPwd bool   `gorm:"column:is_random_pwd"`
	SoftNoDelByTime
}

func (sysUserInfo) TableName() string { return "sys_user_info" }

type sysRoleInfo struct {
	ID              int64  `gorm:"column:id;primaryKey"`
	Name            string `gorm:"column:name"`
	Code            string `gorm:"column:code"`
	Description     string `gorm:"column:desc;type:varchar(200);not null;default:''"`
	Type            int64  `gorm:"column:type"`
	Status          int64  `gorm:"column:status"`
	DefaultHomePage string `gorm:"column:default_home_page;default:/home"`
	SoftNoDelByTime
}

func (sysRoleInfo) TableName() string { return "sys_role_info" }

type sysResourceInfo struct {
	ID              int64  `gorm:"column:id;primaryKey"`
	ParentID        int64  `gorm:"column:parent_id"`
	ResourceKey     string `gorm:"column:resource_key"`
	Name            string `gorm:"column:name"`
	Type            int64  `gorm:"column:type"`
	RoutePath       string `gorm:"column:route_path"`
	UrlType         int64  `gorm:"column:url_type"`
	OpenType        int64  `gorm:"column:open_type"`
	Icon            string `gorm:"column:icon"`
	Sort            int64  `gorm:"column:sort"`
	Enabled         int64  `gorm:"column:enabled"`
	SystemGenerated int64  `gorm:"column:system_generated"`
	SoftNoDelByTime
}

func (sysResourceInfo) TableName() string { return "sys_resource_info" }

type sysResourceAction struct {
	ID              int64  `gorm:"column:id;primaryKey"`
	ResourceID      int64  `gorm:"column:resource_id"`
	ActionType      string `gorm:"column:action_type"`
	ActionValue     string `gorm:"column:action_value"`
	Methods         string `gorm:"column:methods"`
	Enabled         int64  `gorm:"column:enabled"`
	SystemGenerated int64  `gorm:"column:system_generated"`
	SoftNoDelByTime
}

func (sysResourceAction) TableName() string { return "sys_resource_action" }

type sysRoleResource struct {
	ID         int64 `gorm:"column:id;primaryKey"`
	RoleID     int64 `gorm:"column:role_id"`
	ResourceID int64 `gorm:"column:resource_id"`
	SoftNoDelByTime
}

func (sysRoleResource) TableName() string { return "sys_role_resource" }

type sysWorkspaceUser struct {
	ID          int64  `gorm:"column:id;primaryKey"`
	WorkspaceID int64  `gorm:"column:workspace_id"`
	UserID      int64  `gorm:"column:user_id"`
	RoleCode    string `gorm:"column:role_code"`
	RoleID      int64  `gorm:"column:role_id"`
	SoftNoDelByTime
}

func (sysWorkspaceUser) TableName() string { return "sys_workspace_user" }

// ---- users ----

func (r *IAMRepo) GetUserByUsername(ctx context.Context, username string) (User, error) {
	var user User
	err := r.db.WithContext(ctx).Where("user_name = ? AND deleted_time = 0", username).Take(&user).Error
	if err == nil {
		user.Email, user.Phone, err = decryptUserContacts(user.ID, user.Email, user.Phone)
	}
	return user, err
}

func (r *IAMRepo) GetUserByID(ctx context.Context, userID int64) (User, error) {
	var user User
	err := r.db.WithContext(ctx).Where("user_id = ? AND deleted_time = 0", userID).Take(&user).Error
	if err == nil {
		user.Email, user.Phone, err = decryptUserContacts(user.ID, user.Email, user.Phone)
	}
	return user, err
}

func (r *IAMRepo) MarkLogin(ctx context.Context, userID int64) error {
	now := time.Now().UTC().UnixMilli()
	return r.db.WithContext(ctx).Model(&User{}).Where("user_id = ?", userID).Updates(map[string]any{
		"last_login":   now,
		"first_login":  gorm.Expr("CASE WHEN first_login=0 THEN ? ELSE first_login END", now),
		"updated_time": repoTimeFromMilli(now),
	}).Error
}

func (r *IAMRepo) ListUsers(ctx context.Context) ([]map[string]any, error) {
	type userRow struct {
		UserID      int64     `gorm:"column:user_id"`
		UserName    string    `gorm:"column:user_name"`
		NickName    string    `gorm:"column:nick_name"`
		Email       string    `gorm:"column:email"`
		Phone       string    `gorm:"column:phone"`
		Status      int64     `gorm:"column:status"`
		LastLogin   int64     `gorm:"column:last_login"`
		IsRandomPwd bool      `gorm:"column:is_random_pwd"`
		HomePage    string    `gorm:"column:home_page"`
		CreatedTime time.Time `gorm:"column:created_time"`
	}
	var rows []userRow
	if err := r.db.WithContext(ctx).Table("sys_user_info u").
		Select("u.user_id, u.user_name, u.nick_name, u.email, u.phone, u.status, u.last_login, u.is_random_pwd, COALESCE(uc.home_page, '') AS home_page, u.created_time").
		Joins("LEFT JOIN sys_user_config uc ON uc.user_id = u.user_id").
		Where("u.deleted_time = 0").Order("u.user_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		userIDs = append(userIDs, row.UserID)
	}
	roleMap, err := r.userRoleLists(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		email, phone, err := decryptUserContacts(r.UserID, r.Email, r.Phone)
		if err != nil {
			return nil, err
		}
		createdTime := int64(0)
		if !r.CreatedTime.IsZero() {
			createdTime = r.CreatedTime.UTC().UnixMilli()
		}
		out = append(out, map[string]any{
			"userId":      r.UserID,
			"userName":    r.UserName,
			"nickName":    r.NickName,
			"email":       email,
			"phone":       phone,
			"status":      r.Status,
			"lastLogin":   r.LastLogin,
			"createdTime": createdTime,
			"isRandomPwd": r.IsRandomPwd,
			"homePage":    normalizeUserHomePage(r.HomePage),
			"roleList":    roleMap[r.UserID],
		})
	}
	return out, nil
}

func (r *IAMRepo) CreateUser(ctx context.Context, input UserCreate) (map[string]any, error) {
	username := strings.TrimSpace(input.UserName)
	nickName := strings.TrimSpace(input.NickName)
	email := strings.TrimSpace(input.Email)
	phone := strings.TrimSpace(input.Phone)
	if nickName == "" {
		nickName = username
	}
	if username == "" || strings.TrimSpace(input.Password) == "" || input.RoleID <= 0 {
		return nil, ErrInvalidArgument
	}
	var created User
	var role sysRoleInfo
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		created, role, err = createUserInTx(tx, input)
		return err
	})
	if err != nil {
		return nil, normalizeDBError(err)
	}
	roleList := []map[string]any{{
		"roleId":   role.ID,
		"roleName": role.Name,
		"roleCode": role.Code,
	}}
	return map[string]any{
		"userId":      created.ID,
		"userName":    username,
		"nickName":    nickName,
		"email":       email,
		"phone":       phone,
		"status":      created.Status,
		"lastLogin":   int64(0),
		"isRandomPwd": input.IsRandomPwd,
		"roleList":    roleList,
	}, nil
}

func createUserInTx(tx *gorm.DB, input UserCreate) (User, sysRoleInfo, error) {
	username := strings.TrimSpace(input.UserName)
	nickName := strings.TrimSpace(input.NickName)
	email := strings.TrimSpace(input.Email)
	phone := strings.TrimSpace(input.Phone)
	if nickName == "" {
		nickName = username
	}
	if username == "" || strings.TrimSpace(input.Password) == "" || input.RoleID <= 0 {
		return User{}, sysRoleInfo{}, ErrInvalidArgument
	}
	status := int64(0)
	if input.Status != 0 {
		status = 1
	}
	var role sysRoleInfo
	if err := tx.Where("id = ? AND deleted_time = 0", input.RoleID).Take(&role).Error; err != nil {
		return User{}, sysRoleInfo{}, err
	}
	if err := lockUsernameInTx(tx, username); err != nil {
		return User{}, sysRoleInfo{}, err
	}
	var count int64
	if err := tx.Model(&sysUserInfo{}).Where("LOWER(user_name) = LOWER(?) AND deleted_time = 0", username).Count(&count).Error; err != nil {
		return User{}, sysRoleInfo{}, err
	}
	if count > 0 {
		return User{}, sysRoleInfo{}, ErrUserAccountDuplicate
	}
	encryptedEmail, encryptedPhone, err := encryptUserContactPair(email, phone)
	if err != nil {
		return User{}, sysRoleInfo{}, err
	}
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	userID, err := nextPersonalID(tx, "sys_user_info", "user_id")
	if err != nil {
		return User{}, sysRoleInfo{}, err
	}
	row := sysUserInfo{
		ID: userID, UserName: username, NickName: nickName, Password: input.Password,
		Email: encryptedEmail, Phone: encryptedPhone, Status: status, IsRandomPwd: input.IsRandomPwd,
	}
	row.CreatedBy, row.UpdatedBy = input.UserID, input.UserID
	row.CreatedTime, row.UpdatedTime = ts, ts
	if err := tx.Create(&row).Error; err != nil {
		return User{}, sysRoleInfo{}, err
	}
	workspaceUserID, err := nextPersonalID(tx, "sys_workspace_user", "id")
	if err != nil {
		return User{}, sysRoleInfo{}, err
	}
	workspaceUser := sysWorkspaceUser{ID: workspaceUserID, WorkspaceID: 1, UserID: userID, RoleCode: role.Code, RoleID: role.ID}
	workspaceUser.CreatedBy, workspaceUser.UpdatedBy = input.UserID, input.UserID
	workspaceUser.CreatedTime, workspaceUser.UpdatedTime = ts, ts
	if err := tx.Create(&workspaceUser).Error; err != nil {
		return User{}, sysRoleInfo{}, err
	}
	homePage, err := roleDefaultHomePage(tx, role)
	if err != nil {
		return User{}, sysRoleInfo{}, err
	}
	userConfig := UserConfig{UserID: userID, HomePage: homePage, CreatedTime: ts, UpdatedTime: ts}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{"home_page": homePage, "updated_time": ts}),
	}).Create(&userConfig).Error; err != nil {
		return User{}, sysRoleInfo{}, err
	}
	return User{
		ID: userID, UserName: username, NickName: nickName, Email: email, Phone: phone,
		Password: input.Password, Status: status, IsRandomPwd: input.IsRandomPwd,
	}, role, nil
}

func (r *IAMRepo) UpdateUser(ctx context.Context, input UserUpdate) (map[string]any, error) {
	if input.UserID <= 0 {
		return nil, ErrInvalidArgument
	}
	username := strings.TrimSpace(input.UserName)
	nickName := strings.TrimSpace(input.NickName)
	email := strings.TrimSpace(input.Email)
	phone := strings.TrimSpace(input.Phone)
	status := int64(0)
	if input.Status != 0 {
		status = 1
	}
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current sysUserInfo
		if err := tx.Where("user_id = ? AND deleted_time = 0", input.UserID).Take(&current).Error; err != nil {
			return err
		}
		plaintextEmail, plaintextPhone, decryptErr := decryptUserContacts(current.ID, current.Email, current.Phone)
		if decryptErr != nil {
			return decryptErr
		}
		current.Email = plaintextEmail
		current.Phone = plaintextPhone
		isSystemUser := strings.EqualFold(strings.TrimSpace(current.UserName), "tier0")
		isSelfProfileUpdate := input.ActorID == input.UserID && !input.HasRole
		if isSystemUser && !isSelfProfileUpdate {
			return ErrSystemReadonly
		}
		if isSystemUser && username != "" && username != current.UserName {
			return ErrSystemReadonly
		}
		if isSystemUser && isSelfProfileUpdate {
			status = current.Status
		}
		if username == "" {
			username = current.UserName
		}
		if nickName == "" {
			nickName = current.NickName
		}
		if email == "" {
			email = current.Email
		}
		if phone == "" {
			phone = current.Phone
		}
		if username == "" {
			return ErrInvalidArgument
		}
		var count int64
		if username != current.UserName {
			if err := lockUsernameInTx(tx, username); err != nil {
				return err
			}
			if err := tx.Model(&sysUserInfo{}).Where("LOWER(user_name) = LOWER(?) AND user_id <> ? AND deleted_time = 0", username, input.UserID).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrUserAccountDuplicate
			}
		}
		encryptedEmail, encryptedPhone, err := encryptUserContactPair(email, phone)
		if err != nil {
			return err
		}
		if err := tx.Model(&sysUserInfo{}).Where("user_id = ? AND deleted_time = 0", input.UserID).
			Updates(touchByValues(map[string]any{
				"user_name": username,
				"nick_name": nickName,
				"email":     encryptedEmail,
				"phone":     encryptedPhone,
				"status":    status,
			}, input.ActorID, now)).Error; err != nil {
			return err
		}
		if !input.HasRole {
			return touchFleetAssignmentsForCenterUser(ctx, tx, input.UserID, input.ActorID, now)
		}
		if input.RoleID <= 0 {
			return ErrInvalidArgument
		}
		var role sysRoleInfo
		if err := tx.Where("id = ? AND deleted_time = 0", input.RoleID).Take(&role).Error; err != nil {
			return err
		}
		var workspaceRows []sysWorkspaceUser
		if err := tx.Where("workspace_id = ? AND user_id = ? AND deleted_time = 0", int64(1), input.UserID).
			Find(&workspaceRows).Error; err != nil {
			return err
		}
		if err := softDeleteWorkspaceUserRows(tx, workspaceRows, input.ActorID, now); err != nil {
			return err
		}
		workspaceUserID, err := nextPersonalID(tx, "sys_workspace_user", "id")
		if err != nil {
			return err
		}
		workspaceUser := sysWorkspaceUser{
			ID:          workspaceUserID,
			WorkspaceID: 1,
			UserID:      input.UserID,
			RoleCode:    role.Code,
			RoleID:      role.ID,
		}
		workspaceUser.CreatedBy = input.ActorID
		workspaceUser.UpdatedBy = input.ActorID
		workspaceUser.CreatedTime = ts
		workspaceUser.UpdatedTime = ts
		if err := tx.Create(&workspaceUser).Error; err != nil {
			return err
		}
		return touchFleetAssignmentsForCenterUser(ctx, tx, input.UserID, input.ActorID, now)
	})
	if err != nil {
		return nil, normalizeDBError(err)
	}
	return r.GetUserInfoMap(ctx, input.UserID)
}

func (r *IAMRepo) BatchUpdateUsers(ctx context.Context, input UserBatchUpdate) error {
	if len(input.UserIDs) == 0 || input.ActorID <= 0 || (!input.HasRole && len(input.HomePages) == 0) {
		return ErrInvalidArgument
	}
	if input.HasRole && input.RoleID <= 0 {
		return ErrInvalidArgument
	}
	userIDSet := make(map[int64]struct{}, len(input.UserIDs))
	for _, userID := range input.UserIDs {
		if userID <= 0 {
			return ErrInvalidArgument
		}
		userIDSet[userID] = struct{}{}
	}
	if len(userIDSet) != len(input.UserIDs) {
		return ErrInvalidArgument
	}
	for _, update := range input.HomePages {
		if _, ok := userIDSet[update.UserID]; !ok {
			return ErrInvalidArgument
		}
	}

	now := time.Now().UTC()
	nowMilli := now.UnixMilli()
	return normalizeDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var users []sysUserInfo
		if err := tx.Select("user_id", "user_name").
			Where("user_id IN ? AND deleted_time = 0", input.UserIDs).
			Find(&users).Error; err != nil {
			return err
		}
		if len(users) != len(input.UserIDs) {
			return ErrNotFound
		}
		for _, user := range users {
			if strings.EqualFold(strings.TrimSpace(user.UserName), "tier0") && (input.HasRole || user.ID != input.ActorID) {
				return ErrSystemReadonly
			}
		}

		var role sysRoleInfo
		if input.HasRole {
			if err := tx.Where("id = ? AND deleted_time = 0", input.RoleID).Take(&role).Error; err != nil {
				return err
			}
			for _, userID := range input.UserIDs {
				var workspaceRows []sysWorkspaceUser
				if err := tx.Where("workspace_id = ? AND user_id = ? AND deleted_time = 0", int64(1), userID).
					Find(&workspaceRows).Error; err != nil {
					return err
				}
				if err := softDeleteWorkspaceUserRows(tx, workspaceRows, input.ActorID, nowMilli); err != nil {
					return err
				}
				workspaceUserID, err := nextPersonalID(tx, "sys_workspace_user", "id")
				if err != nil {
					return err
				}
				workspaceUser := sysWorkspaceUser{
					ID:          workspaceUserID,
					WorkspaceID: 1,
					UserID:      userID,
					RoleCode:    role.Code,
					RoleID:      role.ID,
				}
				workspaceUser.CreatedBy = input.ActorID
				workspaceUser.UpdatedBy = input.ActorID
				workspaceUser.CreatedTime = now
				workspaceUser.UpdatedTime = now
				if err := tx.Create(&workspaceUser).Error; err != nil {
					return err
				}
				if err := touchFleetAssignmentsForCenterUser(ctx, tx, userID, input.ActorID, nowMilli); err != nil {
					return err
				}
			}
		}

		if len(input.HomePages) > 0 {
			rows := make([]UserConfig, 0, len(input.HomePages))
			for _, update := range input.HomePages {
				rows = append(rows, UserConfig{
					UserID:      update.UserID,
					HomePage:    normalizeUserHomePage(update.HomePage),
					CreatedTime: now,
					UpdatedTime: now,
				})
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "user_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"home_page", "updated_time"}),
			}).Create(&rows).Error; err != nil {
				return err
			}
		}
		return nil
	}))
}

func lockUsernameInTx(tx *gorm.DB, username string) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?), hashtext(LOWER(?)))", "tier0:user_name", strings.TrimSpace(username)).Error
}

func (r *IAMRepo) DeleteUser(ctx context.Context, userID, actorID int64) error {
	if userID <= 0 {
		return ErrInvalidArgument
	}
	now := time.Now().UTC().UnixMilli()
	return normalizeDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current sysUserInfo
		if err := tx.Where("user_id = ? AND deleted_time = 0", userID).Take(&current).Error; err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(current.UserName), "tier0") {
			return ErrSystemReadonly
		}
		if err := unassignDeletedFleetCenterUser(ctx, tx, userID, actorID, now); err != nil {
			return err
		}
		// Transfer owned projects to the system owner (tier0) before deleting the user.
		var owner sysUserInfo
		if err := tx.Where("user_name = ? AND deleted_time = 0", "tier0").
			First(&owner).Error; err != nil {
			return ErrSystemOwnerNotFound
		}
		if err := tx.Model(&Project{}).
			Where("owner_user_id = ? AND deleted_time = 0", userID).
			Updates(map[string]any{
				"owner_user_id": owner.ID,
				"updated_by":    actorID,
				"updated_time":  repoTimeFromMilli(now),
			}).Error; err != nil {
			return err
		}
		// Clean up the user's project memberships and their role bindings.
		var memberIDs []int64
		if err := tx.Model(&projectMember{}).
			Where("user_id = ?", userID).
			Pluck("id", &memberIDs).Error; err != nil {
			return err
		}
		if len(memberIDs) > 0 {
			if err := tx.Where("member_id IN ?", memberIDs).
				Delete(&projectMemberRole{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("user_id = ?", userID).
			Delete(&projectMember{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM sys_user_group_member WHERE user_id = ?", userID).Error; err != nil {
			return err
		}
		if err := tx.Model(&sysUserInfo{}).Where("user_id = ? AND deleted_time = 0", userID).
			Updates(softDeleteNoDelByValues(actorID, now+1000)).Error; err != nil {
			return err
		}
		var workspaceRows []sysWorkspaceUser
		if err := tx.Where("user_id = ? AND deleted_time = 0", userID).Find(&workspaceRows).Error; err != nil {
			return err
		}
		if err := softDeleteWorkspaceUserRows(tx, workspaceRows, actorID, now+1000); err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&ExternalIdentity{}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", userID).Delete(&UserConfig{}).Error
	}))
}

func (r *IAMRepo) ResetUserPassword(ctx context.Context, userID int64, passwordHash string, actorID int64) error {
	if userID <= 0 || strings.TrimSpace(passwordHash) == "" {
		return ErrInvalidArgument
	}
	now := time.Now().UTC().UnixMilli()
	return normalizeDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current sysUserInfo
		if err := tx.Where("user_id = ? AND deleted_time = 0", userID).Take(&current).Error; err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(current.UserName), "tier0") {
			return ErrSystemReadonly
		}
		return tx.Model(&sysUserInfo{}).Where("user_id = ? AND deleted_time = 0", userID).
			Updates(touchByValues(map[string]any{
				"password":      passwordHash,
				"is_random_pwd": false,
			}, actorID, now)).Error
	}))
}

func (r *IAMRepo) ChangeUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	if userID <= 0 || strings.TrimSpace(passwordHash) == "" {
		return ErrInvalidArgument
	}
	now := time.Now().UTC().UnixMilli()
	return normalizeDBError(r.db.WithContext(ctx).Model(&sysUserInfo{}).
		Where("user_id = ? AND deleted_time = 0", userID).
		Updates(touchByValues(map[string]any{
			"password":      passwordHash,
			"is_random_pwd": false,
		}, userID, now)).Error)
}

func (r *IAMRepo) GetUserInfoMap(ctx context.Context, userID int64) (map[string]any, error) {
	type userRow struct {
		UserID      int64  `gorm:"column:user_id"`
		UserName    string `gorm:"column:user_name"`
		NickName    string `gorm:"column:nick_name"`
		Email       string `gorm:"column:email"`
		Phone       string `gorm:"column:phone"`
		Status      int64  `gorm:"column:status"`
		LastLogin   int64  `gorm:"column:last_login"`
		IsRandomPwd bool   `gorm:"column:is_random_pwd"`
	}
	var row userRow
	if err := r.db.WithContext(ctx).Table("sys_user_info").
		Select("user_id, user_name, nick_name, email, phone, status, last_login, is_random_pwd").
		Where("user_id = ? AND deleted_time = 0", userID).Take(&row).Error; err != nil {
		return nil, err
	}
	plaintextEmail, plaintextPhone, err := decryptUserContacts(row.UserID, row.Email, row.Phone)
	if err != nil {
		return nil, err
	}
	row.Email = plaintextEmail
	row.Phone = plaintextPhone
	roleMap, err := r.userRoleLists(ctx, []int64{userID})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"userId":      row.UserID,
		"userName":    row.UserName,
		"nickName":    row.NickName,
		"email":       row.Email,
		"phone":       row.Phone,
		"status":      row.Status,
		"lastLogin":   row.LastLogin,
		"isRandomPwd": row.IsRandomPwd,
		"roleList":    roleMap[row.UserID],
	}, nil
}

func (r *IAMRepo) CountEnabledUsers(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Table("sys_user_info").Where("deleted_time = 0 AND status = 1").Count(&count).Error
	return count, err
}

func (r *IAMRepo) userRoleLists(ctx context.Context, userIDs []int64) (map[int64][]map[string]any, error) {
	out := make(map[int64][]map[string]any, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	type roleRow struct {
		UserID   int64  `gorm:"column:user_id"`
		RoleID   int64  `gorm:"column:role_id"`
		RoleName string `gorm:"column:role_name"`
		RoleCode string `gorm:"column:role_code"`
	}
	var rows []roleRow
	if err := r.db.WithContext(ctx).Table("sys_workspace_user wu").
		Select("wu.user_id, wu.role_id, ri.name AS role_name, ri.code AS role_code").
		Joins("JOIN sys_role_info ri ON ri.id = wu.role_id AND ri.deleted_time = 0").
		Where("wu.deleted_time = 0 AND wu.user_id IN ?", userIDs).
		Order("wu.user_id, wu.id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.UserID] = append(out[row.UserID], map[string]any{
			"roleId":   row.RoleID,
			"roleName": row.RoleName,
			"roleCode": row.RoleCode,
		})
	}
	return out, nil
}

func (r *IAMRepo) ResourceKeysForUser(ctx context.Context, userID int64) ([]string, error) {
	var out []string
	err := r.db.WithContext(ctx).Table("sys_workspace_user wu").
		Joins("JOIN sys_role_resource rr ON rr.role_id=wu.role_id AND rr.deleted_time=0").
		Joins("JOIN sys_resource_info ri ON ri.id=rr.resource_id AND ri.deleted_time=0 AND ri.enabled=1").
		Where("wu.user_id = ? AND wu.deleted_time = 0", userID).
		Distinct().Pluck("ri.resource_key", &out).Error
	return out, err
}

func (r *IAMRepo) UIActionsForUser(ctx context.Context, userID int64) ([]string, error) {
	var out []string
	err := r.db.WithContext(ctx).Table("sys_workspace_user wu").
		Joins("JOIN sys_role_resource rr ON rr.role_id=wu.role_id AND rr.deleted_time=0").
		Joins("JOIN sys_resource_action ra ON ra.resource_id=rr.resource_id AND ra.deleted_time=0 AND ra.enabled=1").
		Where("wu.user_id = ? AND wu.deleted_time = 0 AND ra.action_type = 'ui'", userID).
		Distinct().Pluck("ra.action_value", &out).Error
	return out, err
}

func (r *IAMRepo) UIActionsForResourceKeys(ctx context.Context, resourceKeys []string) ([]string, error) {
	keys := uniqueStrings(resourceKeys)
	if len(keys) == 0 {
		return []string{}, nil
	}
	var out []string
	err := r.db.WithContext(ctx).Table("sys_resource_action ra").
		Joins("JOIN sys_resource_info ri ON ri.id=ra.resource_id AND ri.deleted_time=0 AND ri.enabled=1").
		Where("ri.resource_key IN ? AND ra.deleted_time = 0 AND ra.enabled = 1 AND ra.action_type = 'ui'", keys).
		Distinct().Pluck("ra.action_value", &out).Error
	return out, err
}

// ---- roles ----

func (r *IAMRepo) ListRoles(ctx context.Context) ([]Role, error) {
	var rows []sysRoleInfo
	if err := r.db.WithContext(ctx).Where("deleted_time = 0").Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	var out []Role
	for _, row := range rows {
		defaultHomePage, err := roleDefaultHomePage(r.db.WithContext(ctx), row)
		if err != nil {
			return nil, err
		}
		role := Role{ID: row.ID, Name: row.Name, Code: row.Code, Status: row.Status, DefaultHomePage: defaultHomePage}
		resourceList, err := r.RoleResourceURIs(ctx, role.ID)
		if err != nil {
			return nil, err
		}
		if isBuilderRoleCode(role.Code) {
			resourceList = append(resourceList, map[string]string{"uri": "button:*"})
		}
		role.ResourceList = resourceList
		role.DenyResourceList = []map[string]string{}
		out = append(out, role)
	}
	return out, nil
}

func (r *IAMRepo) CreateRole(ctx context.Context, name, code string, userID int64) (Role, error) {
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	code = strings.TrimSpace(code)
	if code == "" {
		code = "role_" + strconv.FormatInt(now, 10)
	}
	var row sysRoleInfo
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row = sysRoleInfo{Name: strings.TrimSpace(name), Code: code, Description: "", Type: 1, Status: 1, DefaultHomePage: DefaultOperatorHomePage}
		id, err := nextPersonalID(tx, "sys_role_info", "id")
		if err != nil {
			return err
		}
		row.ID = id
		row.CreatedBy = userID
		row.UpdatedBy = userID
		row.CreatedTime = ts
		row.UpdatedTime = ts
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return syncRoleResources(tx, row.ID, []int64{mandatoryLaunchpadResourceID}, userID, now)
	})
	if err != nil {
		return Role{}, normalizeDBError(err)
	}
	resourceList, err := r.RoleResourceURIs(ctx, row.ID)
	if err != nil {
		return Role{}, err
	}
	return Role{ID: row.ID, Name: row.Name, Code: row.Code, Status: row.Status, DefaultHomePage: row.DefaultHomePage, ResourceList: resourceList}, nil
}

func (r *IAMRepo) UpdateRole(ctx context.Context, roleID int64, name string, allowURIs []string, defaultHomePage string, userID int64) error {
	now := time.Now().UTC().UnixMilli()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var role sysRoleInfo
		if err := tx.Where("id = ? AND deleted_time = 0", roleID).Take(&role).Error; err != nil {
			return err
		}
		if isSystemRoleCode(role.Code) {
			return ErrSystemReadonly
		}
		resourceIDs, err := roleResourceIDsByURIs(tx, allowURIs)
		if err != nil {
			return err
		}
		resourceIDs = withMandatoryRoleResources(resourceIDs)
		homePages, err := homePagesByResourceIDs(tx, resourceIDs)
		if err != nil {
			return err
		}
		updates := map[string]any{"default_home_page": normalizeRoleHomePage(defaultHomePage, homePages)}
		if strings.TrimSpace(name) != "" {
			updates["name"] = strings.TrimSpace(name)
		}
		if err := tx.Model(&sysRoleInfo{}).Where("id = ? AND deleted_time = 0", roleID).
			Updates(touchByValues(updates, userID, now)).Error; err != nil {
			return err
		}
		if err := syncRoleResources(tx, roleID, resourceIDs, userID, now); err != nil {
			return err
		}
		return nil
	})
	return normalizeDBError(err)
}

func (r *IAMRepo) DeleteRole(ctx context.Context, roleID, userID int64) error {
	now := time.Now().UTC().UnixMilli()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var role sysRoleInfo
		if err := tx.Where("id = ? AND deleted_time = 0", roleID).Take(&role).Error; err != nil {
			return err
		}
		if isSystemRoleCode(role.Code) {
			return ErrSystemReadonly
		}
		if err := tx.Model(&sysRoleInfo{}).Where("id = ? AND deleted_time = 0", roleID).
			Updates(softDeleteNoDelByValues(userID, now)).Error; err != nil {
			return err
		}
		var rows []sysRoleResource
		if err := tx.Where("role_id = ? AND deleted_time = 0", roleID).Find(&rows).Error; err != nil {
			return err
		}
		return softDeleteRoleResourceRows(tx, rows, userID, now)
	})
}

func (r *IAMRepo) RoleResourceURIs(ctx context.Context, roleID int64) ([]map[string]string, error) {
	var rows []struct {
		ResourceKey string `gorm:"column:resource_key"`
		RoutePath   string `gorm:"column:route_path"`
	}
	err := r.db.WithContext(ctx).Table("sys_role_resource rr").
		Select("ri.resource_key, ri.route_path").
		Joins("JOIN sys_resource_info ri ON ri.id=rr.resource_id AND ri.deleted_time=0 AND ri.enabled=1").
		Where("rr.role_id = ? AND rr.deleted_time = 0", roleID).
		Distinct().Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ResourceKey) == "" {
			continue
		}
		out = append(out, map[string]string{
			"uri":         "resource:" + row.ResourceKey,
			"resourceKey": row.ResourceKey,
			"routePath":   row.RoutePath,
		})
	}
	return out, nil
}

func roleResourceIDsByURIs(db *gorm.DB, uris []string) ([]int64, error) {
	seen := map[string]struct{}{}
	resourceKeys := make([]string, 0, len(uris))
	routes := make([]string, 0, len(uris))
	actions := make([]string, 0, len(uris))
	for _, uri := range uris {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			continue
		}
		if _, ok := seen[uri]; ok {
			continue
		}
		seen[uri] = struct{}{}
		switch {
		case strings.HasPrefix(uri, "resource:"):
			resourceKeys = append(resourceKeys, strings.TrimPrefix(uri, "resource:"))
		case strings.HasPrefix(uri, "button:"):
			actions = append(actions, uri)
		case strings.Contains(uri, ".") && !strings.HasPrefix(uri, "/"):
			resourceKeys = append(resourceKeys, uri)
		default:
			routes = append(routes, uri)
		}
	}
	if len(resourceKeys) == 0 && len(routes) == 0 && len(actions) == 0 {
		return nil, nil
	}
	idSet := map[int64]struct{}{}
	addIDs := func(ids []int64) {
		for _, id := range ids {
			idSet[id] = struct{}{}
		}
	}
	if len(resourceKeys) > 0 {
		var ids []int64
		if err := db.Model(&sysResourceInfo{}).
			Where("deleted_time = 0 AND enabled = 1 AND resource_key IN ?", resourceKeys).
			Distinct().Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		addIDs(ids)
	}
	if len(routes) > 0 {
		var ids []int64
		if err := db.Model(&sysResourceInfo{}).
			Where("deleted_time = 0 AND enabled = 1 AND route_path IN ?", routes).
			Distinct().Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		addIDs(ids)
	}
	if len(actions) > 0 {
		var ids []int64
		if err := db.Table("sys_resource_action").
			Where("deleted_time = 0 AND enabled = 1 AND action_type = 'ui' AND action_value IN ?", actions).
			Distinct().Pluck("resource_id", &ids).Error; err != nil {
			return nil, err
		}
		addIDs(ids)
	}
	out := make([]int64, 0, len(idSet))
	for id := range idSet {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (r *IAMRepo) ResourceKeysByURIs(ctx context.Context, uris []string) (map[string]string, error) {
	out := map[string]string{}
	seen := map[string]struct{}{}
	resourceKeys := make([]string, 0, len(uris))
	routes := make([]string, 0, len(uris))
	actions := make([]string, 0, len(uris))
	for _, uri := range uris {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			continue
		}
		if _, ok := seen[uri]; ok {
			continue
		}
		seen[uri] = struct{}{}
		switch {
		case strings.HasPrefix(uri, "resource:"):
			key := strings.TrimSpace(strings.TrimPrefix(uri, "resource:"))
			if key != "" {
				out[uri] = key
				resourceKeys = append(resourceKeys, key)
			}
		case strings.HasPrefix(uri, "button:"):
			actions = append(actions, uri)
		case strings.Contains(uri, ".") && !strings.HasPrefix(uri, "/"):
			out[uri] = uri
			resourceKeys = append(resourceKeys, uri)
		default:
			routes = append(routes, uri)
		}
	}
	if len(resourceKeys) > 0 {
		var rows []struct {
			ResourceKey string `gorm:"column:resource_key"`
		}
		if err := r.db.WithContext(ctx).Model(&sysResourceInfo{}).
			Select("resource_key").
			Where("deleted_time = 0 AND enabled = 1 AND resource_key IN ?", resourceKeys).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		exists := map[string]struct{}{}
		for _, row := range rows {
			exists[row.ResourceKey] = struct{}{}
		}
		for uri, key := range out {
			if _, ok := exists[key]; !ok {
				delete(out, uri)
			}
		}
	}
	if len(routes) > 0 {
		var rows []struct {
			RoutePath   string `gorm:"column:route_path"`
			ResourceKey string `gorm:"column:resource_key"`
		}
		if err := r.db.WithContext(ctx).Model(&sysResourceInfo{}).
			Select("route_path, resource_key").
			Where("deleted_time = 0 AND enabled = 1 AND route_path IN ?", routes).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			out[row.RoutePath] = row.ResourceKey
		}
	}
	if len(actions) > 0 {
		var rows []struct {
			ActionValue string `gorm:"column:action_value"`
			ResourceKey string `gorm:"column:resource_key"`
		}
		if err := r.db.WithContext(ctx).Table("sys_resource_action ra").
			Select("ra.action_value, ri.resource_key").
			Joins("JOIN sys_resource_info ri ON ri.id=ra.resource_id AND ri.deleted_time=0 AND ri.enabled=1").
			Where("ra.deleted_time = 0 AND ra.enabled = 1 AND ra.action_type = 'ui' AND ra.action_value IN ?", actions).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			out[row.ActionValue] = row.ResourceKey
		}
	}
	return out, nil
}

const (
	mandatoryHomeResourceID      int64 = 197
	mandatoryLaunchpadResourceID int64 = 620
)

func withMandatoryRoleResources(resourceIDs []int64) []int64 {
	idSet := map[int64]struct{}{
		mandatoryHomeResourceID:      {},
		mandatoryLaunchpadResourceID: {},
	}
	for _, id := range resourceIDs {
		if id > 0 {
			idSet[id] = struct{}{}
		}
	}
	out := make([]int64, 0, len(idSet))
	for id := range idSet {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func syncRoleResources(db *gorm.DB, roleID int64, resourceIDs []int64, userID int64, now int64) error {
	resourceIDs = withMandatoryRoleResources(resourceIDs)
	target := make(map[int64]struct{}, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		target[resourceID] = struct{}{}
	}
	missing := make(map[int64]struct{}, len(target))
	for resourceID := range target {
		missing[resourceID] = struct{}{}
	}
	var activeRows []sysRoleResource
	if err := db.Where("role_id = ? AND deleted_time = 0", roleID).Find(&activeRows).Error; err != nil {
		return err
	}
	var removeRows []sysRoleResource
	for _, row := range activeRows {
		if _, ok := target[row.ResourceID]; ok {
			delete(missing, row.ResourceID)
			continue
		}
		removeRows = append(removeRows, row)
	}
	if err := softDeleteRoleResourceRows(db, removeRows, userID, now); err != nil {
		return err
	}
	insertIDs := make([]int64, 0, len(missing))
	for resourceID := range missing {
		insertIDs = append(insertIDs, resourceID)
	}
	sort.Slice(insertIDs, func(i, j int) bool { return insertIDs[i] < insertIDs[j] })
	ts := repoTimeFromMilli(now)
	for _, resourceID := range insertIDs {
		row := sysRoleResource{RoleID: roleID, ResourceID: resourceID}
		id, err := nextPersonalID(db, "sys_role_resource", "id")
		if err != nil {
			return err
		}
		row.ID = id
		row.CreatedBy = userID
		row.UpdatedBy = userID
		row.CreatedTime = ts
		row.UpdatedTime = ts
		if err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "role_id"}, {Name: "resource_id"}, {Name: "deleted_time"}},
			DoNothing: true,
		}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func softDeleteRoleResourceRows(db *gorm.DB, rows []sysRoleResource, userID int64, now int64) error {
	for i, row := range rows {
		deleteNow := now + int64(i+1)*1000
		if err := db.Model(&sysRoleResource{}).Where("id = ? AND deleted_time = 0", row.ID).
			Updates(softDeleteNoDelByValues(userID, deleteNow)).Error; err != nil {
			return err
		}
	}
	return nil
}

func softDeleteWorkspaceUserRows(db *gorm.DB, rows []sysWorkspaceUser, userID int64, now int64) error {
	for i, row := range rows {
		deleteNow := now + int64(i+1)*1000
		if err := db.Model(&sysWorkspaceUser{}).Where("id = ? AND deleted_time = 0", row.ID).
			Updates(softDeleteNoDelByValues(userID, deleteNow)).Error; err != nil {
			return err
		}
	}
	return nil
}

func homePagesByResourceIDs(db *gorm.DB, resourceIDs []int64) ([]string, error) {
	resourceIDs = withMandatoryRoleResources(resourceIDs)
	var pages []string
	if err := db.Model(&sysResourceInfo{}).
		Where("id IN ? AND deleted_time = 0 AND enabled = 1 AND route_path <> '' AND type IN ?", resourceIDs, []int64{2, 4, 5}).
		Order("sort, id").
		Pluck("route_path", &pages).Error; err != nil {
		return nil, err
	}
	return uniqueHomePages(pages), nil
}

func roleDefaultHomePage(db *gorm.DB, role sysRoleInfo) (string, error) {
	if isBuilderRoleCode(role.Code) && strings.TrimSpace(role.DefaultHomePage) == "" {
		return DefaultHomePage, nil
	}
	var ids []int64
	if err := db.Model(&sysRoleResource{}).
		Where("role_id = ? AND deleted_time = 0", role.ID).
		Distinct().
		Pluck("resource_id", &ids).Error; err != nil {
		return "", err
	}
	pages, err := homePagesByResourceIDs(db, ids)
	if err != nil {
		return "", err
	}
	defaultPage := role.DefaultHomePage
	if strings.TrimSpace(defaultPage) == "" {
		if isBuilderRoleCode(role.Code) {
			defaultPage = DefaultHomePage
		} else {
			defaultPage = DefaultOperatorHomePage
		}
	}
	return normalizeRoleHomePage(defaultPage, pages), nil
}

func matchAllowedHomePage(value string, allowedHomePages []string) (string, bool) {
	value, ok := cleanHomePage(value)
	if !ok {
		return "", false
	}
	for _, page := range allowedHomePages {
		page, ok := cleanHomePage(page)
		if !ok {
			continue
		}
		if strings.EqualFold(page, value) {
			return page, true
		}
	}
	return "", false
}

func (r *IAMRepo) HomePagesForUser(ctx context.Context, userID int64) ([]string, error) {
	var pages []string
	err := r.db.WithContext(ctx).Table("sys_workspace_user wu").
		Joins("JOIN sys_role_resource rr ON rr.role_id=wu.role_id AND rr.deleted_time=0").
		Joins("JOIN sys_resource_info ri ON ri.id=rr.resource_id AND ri.deleted_time=0 AND ri.enabled=1").
		Where("wu.user_id = ? AND wu.deleted_time = 0 AND ri.route_path <> '' AND ri.type IN ?", userID, []int64{2, 4, 5}).
		Order("ri.sort, ri.id").
		Pluck("ri.route_path", &pages).Error
	if err != nil {
		return nil, err
	}
	return uniqueHomePages(pages), nil
}

func uniqueHomePages(pages []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(pages))
	for _, page := range pages {
		page = strings.TrimSpace(page)
		if page == "" {
			continue
		}
		key := strings.ToLower(page)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, page)
	}
	return out
}

func (r *IAMRepo) DefaultHomePageForUser(ctx context.Context, userID int64) (string, error) {
	var roles []sysRoleInfo
	if err := r.db.WithContext(ctx).Table("sys_workspace_user wu").
		Select("ri.*").
		Joins("JOIN sys_role_info ri ON ri.id=wu.role_id AND ri.deleted_time=0").
		Where("wu.user_id = ? AND wu.deleted_time = 0", userID).
		Order("wu.id").
		Scan(&roles).Error; err != nil {
		return "", err
	}
	if len(roles) == 0 {
		return DefaultOperatorHomePage, nil
	}
	allowedPages, err := r.HomePagesForUser(ctx, userID)
	if err != nil {
		return "", err
	}
	for _, role := range roles {
		defaultPage := role.DefaultHomePage
		if strings.TrimSpace(defaultPage) == "" {
			if isBuilderRoleCode(role.Code) {
				defaultPage = DefaultHomePage
			} else {
				defaultPage = DefaultOperatorHomePage
			}
		}
		if page, ok := matchAllowedHomePage(defaultPage, allowedPages); ok {
			return page, nil
		}
	}
	return normalizeRoleHomePage("", allowedPages), nil
}

func (r *IAMRepo) ResolveUserHomePage(ctx context.Context, userID int64, preferred string) (string, error) {
	allowedPages, err := r.HomePagesForUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if page, ok := matchAllowedHomePage(preferred, allowedPages); ok {
		return page, nil
	}
	return r.DefaultHomePageForUser(ctx, userID)
}

// ---- resources & actions ----

func (r *IAMRepo) ListResources(ctx context.Context) ([]Resource, error) {
	return r.listResources(ctx, nil, true, false)
}

func (r *IAMRepo) ListMenuResources(ctx context.Context) ([]Resource, error) {
	return r.listResources(ctx, []int64{1, 2, 4, 5}, false, true)
}

func (r *IAMRepo) ResourceMapByKeys(ctx context.Context, keys []string) (map[string]Resource, error) {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	out := make(map[string]Resource, len(normalized))
	if len(normalized) == 0 {
		return out, nil
	}
	var rows []sysResourceInfo
	if err := r.db.WithContext(ctx).
		Where("resource_key IN ? AND deleted_time = 0 AND enabled = 1", normalized).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		normalizeMenuResourceRow(&row)
		out[row.ResourceKey] = Resource{
			ID:              row.ID,
			ParentID:        row.ParentID,
			ResourceKey:     row.ResourceKey,
			Name:            row.Name,
			Type:            row.Type,
			RoutePath:       row.RoutePath,
			UrlType:         row.UrlType,
			OpenType:        row.OpenType,
			Icon:            row.Icon,
			Sort:            row.Sort,
			SystemGenerated: row.SystemGenerated,
		}
	}
	return out, nil
}

func (r *IAMRepo) listResources(ctx context.Context, types []int64, includeActions bool, normalizeMenu bool) ([]Resource, error) {
	var rows []sysResourceInfo
	query := r.db.WithContext(ctx).Where("deleted_time = 0 AND enabled = 1")
	if retired := retiredSeedResourceKeys(); len(retired) > 0 {
		query = query.Where("resource_key NOT IN ?", retired)
	}
	if len(types) > 0 {
		query = query.Where("type IN ?", types)
	}
	if err := query.Order("sort, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	actionsByResource := map[int64][]ResourceAction{}
	if includeActions && len(rows) > 0 {
		var resourceIDs []int64
		for _, row := range rows {
			resourceIDs = append(resourceIDs, row.ID)
		}
		var actions []ResourceAction
		if err := r.db.WithContext(ctx).Table("sys_resource_action ra").
			Select("ra.id, ra.resource_id, ri.resource_key, ra.action_type, ra.action_value, ra.methods, ra.enabled, ra.system_generated").
			Joins("JOIN sys_resource_info ri ON ri.id=ra.resource_id AND ri.deleted_time=0 AND ri.enabled=1").
			Where("ra.deleted_time = 0 AND ra.resource_id IN ?", resourceIDs).
			Order("ra.action_type, ra.action_value, ra.id").
			Scan(&actions).Error; err != nil {
			return nil, err
		}
		for _, action := range actions {
			actionsByResource[action.ResourceID] = append(actionsByResource[action.ResourceID], action)
		}
	}
	byID := map[int64]*Resource{}
	var ordered []*Resource
	for _, r := range rows {
		if normalizeMenu {
			normalizeMenuResourceRow(&r)
		}
		if len(types) > 0 && !containsInt64(types, r.Type) {
			continue
		}
		actions := actionsByResource[r.ID]
		res := Resource{ID: r.ID, ParentID: r.ParentID, ResourceKey: r.ResourceKey, Name: r.Name, Type: r.Type, RoutePath: r.RoutePath, Icon: r.Icon, Sort: r.Sort, SystemGenerated: r.SystemGenerated, Actions: actions, ActionCount: int64(len(actions))}
		res.UrlType = r.UrlType
		res.OpenType = r.OpenType
		byID[res.ID] = &res
		ordered = append(ordered, &res)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Sort == ordered[j].Sort {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Sort < ordered[j].Sort
	})
	childrenByParent := map[int64][]*Resource{}
	var rootNodes []*Resource
	for _, r := range ordered {
		if _, ok := byID[r.ParentID]; ok {
			childrenByParent[r.ParentID] = append(childrenByParent[r.ParentID], r)
			continue
		}
		rootNodes = append(rootNodes, r)
	}
	var buildTree func(*Resource) Resource
	buildTree = func(node *Resource) Resource {
		out := *node
		out.Children = nil
		for _, child := range childrenByParent[node.ID] {
			out.Children = append(out.Children, buildTree(child))
		}
		return out
	}
	var roots []Resource
	for _, r := range rootNodes {
		roots = append(roots, buildTree(r))
	}
	return roots, nil
}

func (r *IAMRepo) SaveResource(ctx context.Context, input Resource, userID int64, enabled int64) (Resource, error) {
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	input.ResourceKey = strings.TrimSpace(input.ResourceKey)
	input.Name = strings.TrimSpace(input.Name)
	input.RoutePath = strings.TrimSpace(input.RoutePath)
	input.Icon = strings.TrimSpace(input.Icon)
	if input.ResourceKey == "" || input.Name == "" || input.Type <= 0 {
		return Resource{}, ErrInvalidArgument
	}
	if input.UrlType == 0 {
		input.UrlType = 1
	}
	if input.Type != 2 && input.Type != 4 && input.Type != 5 {
		input.RoutePath = ""
		input.UrlType = 1
		input.OpenType = 0
	}
	var saved Resource
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row sysResourceInfo
		if input.ID > 0 {
			if err := tx.Where("id = ? AND deleted_time = 0", input.ID).Take(&row).Error; err != nil {
				return err
			}
			if row.SystemGenerated == 1 {
				return ErrSystemReadonly
			}
		}
		if input.ResourceKey != row.ResourceKey {
			var count int64
			if err := tx.Model(&sysResourceInfo{}).
				Where("resource_key = ? AND deleted_time = 0 AND id <> ?", input.ResourceKey, input.ID).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrInvalidArgument
			}
		}
		updates := map[string]any{
			"parent_id":        input.ParentID,
			"resource_key":     input.ResourceKey,
			"name":             input.Name,
			"type":             input.Type,
			"route_path":       input.RoutePath,
			"url_type":         input.UrlType,
			"open_type":        input.OpenType,
			"icon":             input.Icon,
			"sort":             input.Sort,
			"enabled":          enabled,
			"system_generated": 0,
		}
		if input.ID > 0 {
			if err := tx.Model(&sysResourceInfo{}).Where("id = ? AND deleted_time = 0", input.ID).
				Updates(touchByValues(updates, userID, now)).Error; err != nil {
				return err
			}
		} else {
			id, err := nextPersonalID(tx, "sys_resource_info", "id")
			if err != nil {
				return err
			}
			row = sysResourceInfo{
				ID:          id,
				ParentID:    input.ParentID,
				ResourceKey: input.ResourceKey,
				Name:        input.Name,
				Type:        input.Type,
				RoutePath:   input.RoutePath,
				UrlType:     input.UrlType,
				OpenType:    input.OpenType,
				Icon:        input.Icon,
				Sort:        input.Sort,
				Enabled:     enabled,
			}
			row.CreatedBy = userID
			row.UpdatedBy = userID
			row.CreatedTime = ts
			row.UpdatedTime = ts
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			input.ID = row.ID
		}
		var rows []sysResourceInfo
		if err := tx.Where("id = ? AND deleted_time = 0", input.ID).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrNotFound
		}
		saved = Resource{
			ID:              rows[0].ID,
			ParentID:        rows[0].ParentID,
			ResourceKey:     rows[0].ResourceKey,
			Name:            rows[0].Name,
			Type:            rows[0].Type,
			RoutePath:       rows[0].RoutePath,
			UrlType:         rows[0].UrlType,
			OpenType:        rows[0].OpenType,
			Icon:            rows[0].Icon,
			Sort:            rows[0].Sort,
			SystemGenerated: rows[0].SystemGenerated,
		}
		return nil
	})
	if err != nil {
		return Resource{}, normalizeDBError(err)
	}
	return saved, nil
}

func containsInt64(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type menuResourceOverride struct {
	parentID  int64
	name      string
	typeValue int64
	routePath string
	urlType   int64
	openType  int64
	icon      string
	sort      int64
}

func normalizeMenuResourceRow(row *sysResourceInfo) {
	override, ok := menuResourceOverrides[row.ResourceKey]
	if !ok {
		return
	}
	row.ParentID = override.parentID
	row.Name = override.name
	row.Type = override.typeValue
	row.RoutePath = override.routePath
	row.UrlType = override.urlType
	row.OpenType = override.openType
	row.Icon = override.icon
	row.Sort = override.sort
}

var menuResourceOverrides = map[string]menuResourceOverride{
	"uns":                  {parentID: 0, name: "UNS", typeValue: 1, icon: "menu.tag.uns", sort: 10, urlType: 1, openType: 0},
	"home.page":            {parentID: 0, name: "Home", typeValue: 2, routePath: "/home", icon: "Home", sort: 5, urlType: 1, openType: 0},
	"uns.page":             {parentID: 198, name: "Namespace", typeValue: 2, routePath: "/uns", icon: "Namespace", sort: 10, urlType: 1, openType: 0},
	"flow.collection.page": {parentID: 198, name: "Source Flow", typeValue: 2, routePath: "/collection-flow", icon: "SourceFlow", sort: 20, urlType: 1, openType: 0},
	"flow.event.page":      {parentID: 198, name: "Event Flow", typeValue: 2, routePath: "/event-flow", icon: "EventFlow", sort: 30, urlType: 1, openType: 0},
	"app":                  {parentID: 0, name: "App", typeValue: 1, icon: "menu.tag.appspace", sort: 20, urlType: 1, openType: 0},
	"project.view":         {parentID: 200, name: "Project", typeValue: 2, routePath: "/project", icon: "Project", sort: 10, urlType: 1, openType: 0},
	"launchpad.view":       {parentID: 200, name: "Launchpad", typeValue: 2, routePath: "/launchpad", icon: "Launchpad", sort: 20, urlType: 1, openType: 0},
	"analysis":             {parentID: 0, name: "Data Analysis", typeValue: 1, icon: "menu.tag.connections", sort: 30, urlType: 1, openType: 0},
	"cluster.edge.manage":  {parentID: 440, name: "Edge Node", typeValue: 3, icon: "SystemCascade", sort: 30, urlType: 1, openType: 0},
	"system":               {parentID: 0, name: "System", typeValue: 1, icon: "menu.tag.system", sort: 60, urlType: 1, openType: 0},
	"gateway.route.manage": {parentID: 100, name: "Routing Management", typeValue: 2, routePath: "/routing-management", icon: "RoutingManagement", sort: 10, urlType: 1, openType: 0},
	"gateway.route.create": {parentID: 140, name: "Gateway Route Create", typeValue: 3, sort: 10},
	"gateway.route.update": {parentID: 140, name: "Gateway Route Update", typeValue: 3, sort: 20},
	"gateway.route.delete": {parentID: 140, name: "Gateway Route Delete", typeValue: 3, sort: 30},
	"gateway.route.test":   {parentID: 140, name: "Gateway Route Test", typeValue: 3, sort: 40},
	"iam.user.view":        {parentID: 100, name: "User Management", typeValue: 2, routePath: "/account-management", icon: "UserManagement", sort: 20, urlType: 1, openType: 0},
	"apikey.manage":        {parentID: 100, name: "API Key", typeValue: 2, routePath: "/OpenData", icon: "OpenData", sort: 30, urlType: 1, openType: 0},
	"system.menu.config":   {parentID: 100, name: "Menu Config", typeValue: 2, routePath: "/MenuConfiguration", icon: "MenuConfiguration", sort: 40, urlType: 1, openType: 0},
	"system.audit.log":     {parentID: 100, name: "Audit Log", typeValue: 2, routePath: "/audit-log", icon: "Logs", sort: 50, urlType: 1, openType: 0},
	"iam.resource.view":    {parentID: 100, name: "Permission Management", typeValue: 2, routePath: "/PermissionManagement", icon: "Authentication", sort: 60, urlType: 1, openType: 0},
	"oauth.client.manage":  {parentID: 100, name: "OAuth Client", typeValue: 2, routePath: "/oauth-clients", icon: "Authentication", sort: 55, urlType: 1, openType: 0},
	"mqtt.auth.manage":     {parentID: 198, name: "MQTT Auth", typeValue: 2, routePath: "/mqtt-auth", icon: "MqttBroker", sort: 50, urlType: 1, openType: 0},
	"mqtt.auth.write":      {parentID: 440, name: "MQTT Auth Write", typeValue: 3, sort: 10, urlType: 1, openType: 0},
	"mqtt.auth.delete":     {parentID: 440, name: "MQTT Auth Delete", typeValue: 3, sort: 20, urlType: 1, openType: 0},
}

func (r *IAMRepo) ListResourceActions(ctx context.Context, resourceID int64) ([]ResourceAction, error) {
	var actions []ResourceAction
	err := r.db.WithContext(ctx).Table("sys_resource_action ra").
		Select("ra.id, ra.resource_id, ri.resource_key, ra.action_type, ra.action_value, ra.methods, ra.enabled, ra.system_generated").
		Joins("JOIN sys_resource_info ri ON ri.id=ra.resource_id AND ri.deleted_time=0 AND ri.enabled=1").
		Where("ra.resource_id = ? AND ra.deleted_time = 0", resourceID).
		Order("ra.action_type, ra.action_value, ra.id").
		Scan(&actions).Error
	return actions, err
}

func (r *IAMRepo) SaveResourceAction(ctx context.Context, action ResourceAction, userID int64) (ResourceAction, error) {
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	action.ActionType = strings.TrimSpace(strings.ToLower(action.ActionType))
	action.ActionValue = strings.TrimSpace(action.ActionValue)
	action.Methods = strings.TrimSpace(strings.ToUpper(action.Methods))
	if action.ID == 0 && action.Enabled == 0 {
		action.Enabled = 1
	}
	if action.ResourceID <= 0 || action.ActionType == "" || action.ActionValue == "" {
		return ResourceAction{}, ErrInvalidArgument
	}
	row := sysResourceAction{
		ID:              action.ID,
		ResourceID:      action.ResourceID,
		ActionType:      action.ActionType,
		ActionValue:     action.ActionValue,
		Methods:         action.Methods,
		Enabled:         action.Enabled,
		SystemGenerated: action.SystemGenerated,
	}
	row.UpdatedBy = userID
	row.UpdatedTime = ts
	row.SystemGenerated = 0
	if row.ID > 0 {
		var existing sysResourceAction
		if err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", row.ID).Take(&existing).Error; err != nil {
			return ResourceAction{}, err
		}
		if existing.SystemGenerated == 1 {
			return ResourceAction{}, ErrSystemReadonly
		}
		err := r.db.WithContext(ctx).Model(&sysResourceAction{}).
			Where("id = ? AND deleted_time = 0", row.ID).
			Updates(touchByValues(map[string]any{
				"resource_id":      row.ResourceID,
				"action_type":      row.ActionType,
				"action_value":     row.ActionValue,
				"methods":          row.Methods,
				"enabled":          row.Enabled,
				"system_generated": int64(0),
			}, userID, now)).Error
		if err != nil {
			return ResourceAction{}, normalizeDBError(err)
		}
	} else {
		id, err := nextPersonalID(r.db.WithContext(ctx), "sys_resource_action", "id")
		if err != nil {
			return ResourceAction{}, err
		}
		row.ID = id
		row.CreatedBy = userID
		row.SystemGenerated = 0
		row.CreatedTime = ts
		if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
			return ResourceAction{}, normalizeDBError(err)
		}
	}
	actions, err := r.ListResourceActions(ctx, row.ResourceID)
	if err != nil {
		return ResourceAction{}, err
	}
	for _, item := range actions {
		if item.ID == row.ID {
			return item, nil
		}
	}
	return ResourceAction{
		ID:              row.ID,
		ResourceID:      row.ResourceID,
		ActionType:      row.ActionType,
		ActionValue:     row.ActionValue,
		Methods:         row.Methods,
		Enabled:         row.Enabled,
		SystemGenerated: row.SystemGenerated,
	}, nil
}

func (r *IAMRepo) DeleteResourceAction(ctx context.Context, actionID, userID int64) error {
	var action sysResourceAction
	if err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", actionID).Take(&action).Error; err != nil {
		return err
	}
	if action.SystemGenerated == 1 {
		return ErrSystemReadonly
	}
	res := r.db.WithContext(ctx).Model(&sysResourceAction{}).
		Where("id = ? AND deleted_time = 0 AND system_generated = 0", actionID).
		Updates(softDeleteNoDelByValues(userID, 0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *IAMRepo) MatchAction(ctx context.Context, actionType, method, path string) ([]ResourceAction, error) {
	var candidates []ResourceAction
	err := r.db.WithContext(ctx).Table("sys_resource_action ra").
		Select("ra.id, ra.resource_id, ri.resource_key, ra.action_type, ra.action_value, ra.methods, ra.enabled, ra.system_generated").
		Joins("JOIN sys_resource_info ri ON ri.id=ra.resource_id AND ri.deleted_time=0 AND ri.enabled=1").
		Where("ra.action_type = ? AND ra.deleted_time = 0 AND ra.enabled = 1", actionType).
		// Keep every pattern that pathMatches can accept, including normalized
		// slash variants, :parameters and /** prefixes. The existing matcher is
		// still authoritative; unrelated literal routes need not leave the DB.
		Where("btrim(ra.action_value, '/') = ? OR strpos(ra.action_value, ':') > 0 OR right(ra.action_value, 3) = '/**'", strings.Trim(path, "/")).
		Scan(&candidates).Error
	if err != nil {
		return nil, err
	}
	var matches []ResourceAction
	for _, a := range candidates {
		if methodMatches(a.Methods, method) && pathMatches(a.ActionValue, path) {
			matches = append(matches, a)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		leftExact := !strings.HasSuffix(matches[i].ActionValue, "/**")
		rightExact := !strings.HasSuffix(matches[j].ActionValue, "/**")
		if leftExact != rightExact {
			return leftExact
		}
		return len(matches[i].ActionValue) > len(matches[j].ActionValue)
	})
	return matches, nil
}

type IAMRepo struct{ db *gorm.DB }

func NewIAMRepo(in any) *IAMRepo { return &IAMRepo{db: GetCommonConn(in)} }

const (
	PersonalIDStart      int64 = 1000000000
	SystemRoleBuilderID  int64 = 1
	SystemRoleAdminID    int64 = 1
	SystemRoleOperatorID int64 = 2

	BuiltinRoleBuilder  = "admin"
	BuiltinRoleAdmin    = "admin"
	BuiltinRoleOperator = "operator"

	legacyBuiltinRoleBuilder = "builder"
)

var ErrSystemReadonly = errors.New("system seed data is read-only")

var ErrSystemOwnerNotFound = errors.New("system owner not found")

// projectMember and projectMemberRole are lightweight GORM targets used only for
// hard-deleting membership rows without exposing full DTOs.
type projectMember struct{}

func (projectMember) TableName() string { return "project_member" }

type projectMemberRole struct{}

func (projectMemberRole) TableName() string { return "project_member_role" }

func nextPersonalID(db *gorm.DB, tableName, columnName string) (int64, error) {
	var maxID int64
	err := db.Table(tableName).
		Select("COALESCE(MAX("+columnName+"), ?)", PersonalIDStart-1).
		Where(columnName+" >= ?", PersonalIDStart).
		Scan(&maxID).Error
	if err != nil {
		return 0, err
	}
	return maxID + 1, nil
}

func isAdminRoleCode(code string) bool {
	code = strings.TrimSpace(strings.ToLower(code))
	return code == BuiltinRoleAdmin || code == legacyBuiltinRoleBuilder
}

func isBuilderRoleCode(code string) bool {
	return isAdminRoleCode(code)
}

func isOperatorRoleCode(code string) bool {
	code = strings.TrimSpace(strings.ToLower(code))
	return code == BuiltinRoleOperator
}

func isSystemRoleCode(code string) bool {
	code = strings.TrimSpace(strings.ToLower(code))
	return isAdminRoleCode(code) || code == BuiltinRoleOperator
}

// IsAdmin 判断用户是否拥有平台 admin 角色（兼容旧 builder 角色）。
func (r *IAMRepo) IsAdmin(ctx context.Context, userID int64) (bool, error) {
	code, err := r.UserRoleCode(ctx, userID)
	if err != nil {
		return false, err
	}
	return isAdminRoleCode(code), nil
}

// UserRoleCode 返回用户当前工作空间的角色 code。
func (r *IAMRepo) UserRoleCode(ctx context.Context, userID int64) (string, error) {
	var code string
	err := r.db.WithContext(ctx).Table("sys_workspace_user wu").
		Joins("JOIN sys_role_info ri ON ri.id = wu.role_id AND ri.deleted_time = 0").
		Where("wu.user_id = ? AND wu.deleted_time = 0", userID).
		Select("ri.code").
		Limit(1).
		Scan(&code).Error
	return code, err
}
