package repo

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	UserSyncPasswordPolicyDefault = "default"
	UserSyncPasswordPolicySource  = "source"
)

type ClusterUserSyncRole struct {
	RoleID   int64  `json:"roleId"`
	RoleName string `json:"roleName"`
	RoleCode string `json:"roleCode"`
}

type ClusterUserSyncItem struct {
	UserID       int64                 `json:"userId"`
	UserName     string                `json:"userName"`
	NickName     string                `json:"nickName"`
	Email        string                `json:"email"`
	Phone        string                `json:"phone"`
	PasswordHash string                `json:"passwordHash,omitempty"`
	Status       int64                 `json:"status"`
	IsRandomPwd  bool                  `json:"isRandomPwd"`
	DeletedTime  int64                 `json:"deletedTime"`
	RoleList     []ClusterUserSyncRole `json:"roleList"`
}

type ClusterUserSyncApplyInput struct {
	Users               []ClusterUserSyncItem
	PasswordPolicy      string
	DefaultPasswordHash string
	ActorID             int64
	CenterIdentity      string
	CenterType          string
	FleetNodeID         string
	SourceRevision      int64
	Complete            bool
}

type ClusterUserSyncApplyResult struct {
	Stats         map[string]int   `json:"stats"`
	CreatedUsers  []map[string]any `json:"createdUsers"`
	UpdatedUsers  []map[string]any `json:"updatedUsers"`
	DisabledUsers []map[string]any `json:"disabledUsers"`
	DeletedUsers  []map[string]any `json:"deletedUsers"`
	SkippedUsers  []map[string]any `json:"skippedUsers"`
	Conflicts     []map[string]any `json:"conflicts"`
	RoleMappings  []map[string]any `json:"roleMappings"`
}

func (r *IAMRepo) ListClusterUserSnapshot(ctx context.Context, includePasswordHash bool) ([]ClusterUserSyncItem, error) {
	return r.listClusterUserSnapshot(ctx, nil, includePasswordHash)
}

func (r *IAMRepo) ListClusterUserSnapshotByIDs(ctx context.Context, userIDs []int64, includePasswordHash bool) ([]ClusterUserSyncItem, error) {
	if len(userIDs) == 0 {
		return []ClusterUserSyncItem{}, nil
	}
	return r.listClusterUserSnapshot(ctx, userIDs, includePasswordHash)
}

func (r *IAMRepo) listClusterUserSnapshot(ctx context.Context, userIDs []int64, includePasswordHash bool) ([]ClusterUserSyncItem, error) {
	type userRow struct {
		UserID       int64  `gorm:"column:user_id"`
		UserName     string `gorm:"column:user_name"`
		NickName     string `gorm:"column:nick_name"`
		Email        string `gorm:"column:email"`
		Phone        string `gorm:"column:phone"`
		PasswordHash string `gorm:"column:password"`
		Status       int64  `gorm:"column:status"`
		IsRandomPwd  bool   `gorm:"column:is_random_pwd"`
		DeletedTime  int64  `gorm:"column:deleted_time"`
	}
	var users []userRow
	userQuery := r.db.WithContext(ctx).Table("sys_user_info").
		Select("user_id, user_name, nick_name, email, phone, password, status, is_random_pwd, deleted_time")
	if userIDs != nil {
		userQuery = userQuery.Where("user_id IN ?", userIDs)
	}
	if err := userQuery.Order("user_id").Scan(&users).Error; err != nil {
		return nil, err
	}
	type roleRow struct {
		UserID   int64  `gorm:"column:user_id"`
		RoleID   int64  `gorm:"column:role_id"`
		RoleCode string `gorm:"column:role_code"`
		RoleName string `gorm:"column:role_name"`
	}
	var roleRows []roleRow
	roleQuery := r.db.WithContext(ctx).Table("sys_workspace_user wu").
		Select("wu.user_id, wu.role_id, r.code AS role_code, r.name AS role_name").
		Joins("JOIN sys_role_info r ON r.id = wu.role_id AND r.deleted_time = 0").
		Where("wu.deleted_time = 0")
	if userIDs != nil {
		roleQuery = roleQuery.Where("wu.user_id IN ?", userIDs)
	}
	if err := roleQuery.Order("wu.user_id, wu.id").Scan(&roleRows).Error; err != nil {
		return nil, err
	}
	roleMap := make(map[int64][]ClusterUserSyncRole, len(roleRows))
	for _, row := range roleRows {
		roleMap[row.UserID] = append(roleMap[row.UserID], ClusterUserSyncRole{
			RoleID:   row.RoleID,
			RoleName: row.RoleName,
			RoleCode: row.RoleCode,
		})
	}
	out := make([]ClusterUserSyncItem, 0, len(users))
	for _, row := range users {
		email, phone, err := decryptUserContacts(row.UserID, row.Email, row.Phone)
		if err != nil {
			return nil, err
		}
		item := ClusterUserSyncItem{
			UserID:      row.UserID,
			UserName:    row.UserName,
			NickName:    row.NickName,
			Email:       email,
			Phone:       phone,
			Status:      row.Status,
			IsRandomPwd: row.IsRandomPwd,
			DeletedTime: row.DeletedTime,
			RoleList:    roleMap[row.UserID],
		}
		if includePasswordHash {
			item.PasswordHash = row.PasswordHash
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *IAMRepo) ApplyClusterUserSnapshot(ctx context.Context, input ClusterUserSyncApplyInput) (ClusterUserSyncApplyResult, error) {
	result := ClusterUserSyncApplyResult{Stats: map[string]int{}}
	policy := strings.TrimSpace(input.PasswordPolicy)
	if policy == "" {
		policy = UserSyncPasswordPolicyDefault
	}
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	total := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockUserEmailWrites(tx); err != nil {
			return err
		}
		roles, err := activeRolesForClusterSync(tx)
		if err != nil {
			return err
		}
		usersByID, activeNames, activeEmails, err := clusterSyncUsersByID(tx)
		if err != nil {
			return err
		}
		for _, source := range input.Users {
			source.UserName = strings.TrimSpace(source.UserName)
			source.NickName = strings.TrimSpace(source.NickName)
			source.Email = strings.TrimSpace(source.Email)
			source.Phone = strings.TrimSpace(source.Phone)
			if source.UserID <= 0 || source.UserName == "" {
				result.SkippedUsers = append(result.SkippedUsers, userSyncIssue(source, "invalidUser", "missing userId or userName"))
				total++
				continue
			}
			if isClusterSyncSystemUser(source) {
				continue
			}
			total++
			current, exists := usersByID[source.UserID]
			if source.DeletedTime > 0 {
				if !exists || current.DeletedTime > 0 {
					result.SkippedUsers = append(result.SkippedUsers, userSyncIssue(source, "alreadyDeleted", "target user is already absent"))
					continue
				}
				if err := softDeleteClusterUser(tx, current, input.ActorID, now); err != nil {
					return err
				}
				delete(activeNames, strings.ToLower(current.UserName))
				if strings.TrimSpace(current.Email) != "" {
					delete(activeEmails, strings.ToLower(current.Email))
				}
				result.DeletedUsers = append(result.DeletedUsers, userSyncUserSummary(source))
				continue
			}
			if conflictID, ok := activeNames[strings.ToLower(source.UserName)]; ok && conflictID != source.UserID {
				result.Conflicts = append(result.Conflicts, userSyncIssue(source, "usernameConflict", "username is already used by another user"))
				continue
			}
			if source.Email != "" {
				if conflictID, ok := activeEmails[strings.ToLower(source.Email)]; ok && conflictID != source.UserID {
					result.Conflicts = append(result.Conflicts, userSyncIssue(source, "emailConflict", "email is already used by another user"))
					continue
				}
			}
			targetRole, mapped := mapClusterSyncRole(source.RoleList, roles)
			fallback := false
			if !mapped {
				operatorRole, ok := fallbackClusterSyncOperatorRole(roles)
				if !ok {
					result.RoleMappings = appendClusterRoleMapping(result.RoleMappings, source.RoleList, targetRole, false, false)
					result.SkippedUsers = append(result.SkippedUsers, userSyncIssue(source, "roleMappingMissing", "no matching target role"))
					continue
				}
				targetRole = operatorRole
				fallback = true
			}
			result.RoleMappings = appendClusterRoleMapping(result.RoleMappings, source.RoleList, targetRole, mapped, fallback)
			passwordHash := strings.TrimSpace(input.DefaultPasswordHash)
			isRandomPwd := false
			if policy == UserSyncPasswordPolicySource {
				passwordHash = strings.TrimSpace(source.PasswordHash)
				isRandomPwd = source.IsRandomPwd
			}
			if passwordHash == "" && (!exists || current.DeletedTime > 0 || policy == UserSyncPasswordPolicySource) {
				result.SkippedUsers = append(result.SkippedUsers, userSyncIssue(source, "passwordUnavailable", "source password is unavailable"))
				continue
			}
			if !exists {
				if err := createClusterSyncUser(tx, source, targetRole, passwordHash, isRandomPwd, input.ActorID, ts); err != nil {
					return err
				}
				result.CreatedUsers = append(result.CreatedUsers, userSyncUserSummary(source))
				activeNames[strings.ToLower(source.UserName)] = source.UserID
				if source.Email != "" {
					activeEmails[strings.ToLower(source.Email)] = source.UserID
				}
				continue
			}
			if current.DeletedTime > 0 {
				if err := restoreClusterSyncUser(tx, source.UserID, source, targetRole, passwordHash, isRandomPwd, input.ActorID, now); err != nil {
					return err
				}
				result.CreatedUsers = append(result.CreatedUsers, userSyncUserSummary(source))
				activeNames[strings.ToLower(source.UserName)] = source.UserID
				if source.Email != "" {
					activeEmails[strings.ToLower(source.Email)] = source.UserID
				}
				continue
			}
			if err := updateClusterSyncUser(tx, source.UserID, source, targetRole, passwordHash, policy, isRandomPwd, input.ActorID, now); err != nil {
				return err
			}
			summary := userSyncUserSummary(source)
			if source.Status != 1 {
				result.DisabledUsers = append(result.DisabledUsers, summary)
			} else {
				result.UpdatedUsers = append(result.UpdatedUsers, summary)
			}
		}
		return nil
	})
	if err != nil {
		return result, normalizeDBError(err)
	}
	result.Stats["created"] = len(result.CreatedUsers)
	result.Stats["updated"] = len(result.UpdatedUsers)
	result.Stats["disabled"] = len(result.DisabledUsers)
	result.Stats["deleted"] = len(result.DeletedUsers)
	result.Stats["skipped"] = len(result.SkippedUsers)
	result.Stats["conflicts"] = len(result.Conflicts)
	result.Stats["total"] = total
	return result, nil
}

func activeRolesForClusterSync(tx *gorm.DB) (map[string]sysRoleInfo, error) {
	var rows []sysRoleInfo
	if err := tx.Where("deleted_time = 0 AND status = 1").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]sysRoleInfo, len(rows)*2)
	var builderRole sysRoleInfo
	var hasBuilderRole bool
	var operatorRole sysRoleInfo
	var hasOperatorRole bool
	for _, row := range rows {
		if code := strings.ToLower(strings.TrimSpace(row.Code)); code != "" {
			out["code:"+code] = row
			if code == BuiltinRoleBuilder {
				builderRole = row
				hasBuilderRole = true
			}
			if code == BuiltinRoleOperator {
				operatorRole = row
				hasOperatorRole = true
			}
		}
		if name := strings.ToLower(strings.TrimSpace(row.Name)); name != "" {
			out["name:"+name] = row
		}
	}
	addAlias := func(key string, role sysRoleInfo) {
		if _, ok := out[key]; !ok {
			out[key] = role
		}
	}
	if hasBuilderRole {
		for _, alias := range []string{legacyBuiltinRoleBuilder, "owner", "admin"} {
			addAlias("code:"+alias, builderRole)
			addAlias("name:"+alias, builderRole)
		}
	}
	if hasOperatorRole {
		addAlias("code:member", operatorRole)
		addAlias("name:member", operatorRole)
	}
	return out, nil
}

func isClusterSyncSystemUser(source ClusterUserSyncItem) bool {
	return source.UserID == 1 || strings.EqualFold(strings.TrimSpace(source.UserName), "tier0")
}

func clusterSyncUsersByID(tx *gorm.DB) (map[int64]sysUserInfo, map[string]int64, map[string]int64, error) {
	var rows []sysUserInfo
	if err := tx.Unscoped().Find(&rows).Error; err != nil {
		return nil, nil, nil, err
	}
	byID := make(map[int64]sysUserInfo, len(rows))
	names := make(map[string]int64, len(rows))
	emails := make(map[string]int64, len(rows))
	for _, row := range rows {
		email, phone, err := decryptUserContacts(row.ID, row.Email, row.Phone)
		if err != nil {
			return nil, nil, nil, err
		}
		row.Email = email
		row.Phone = phone
		byID[row.ID] = row
		if row.DeletedTime == 0 {
			if name := strings.ToLower(strings.TrimSpace(row.UserName)); name != "" {
				names[name] = row.ID
			}
			if email := strings.ToLower(strings.TrimSpace(row.Email)); email != "" {
				emails[email] = row.ID
			}
		}
	}
	return byID, names, emails, nil
}

func mapClusterSyncRole(sourceRoles []ClusterUserSyncRole, roles map[string]sysRoleInfo) (sysRoleInfo, bool) {
	for _, source := range sourceRoles {
		if code := strings.ToLower(strings.TrimSpace(source.RoleCode)); code != "" {
			if role, ok := roles["code:"+code]; ok {
				return role, true
			}
		}
		if name := strings.ToLower(strings.TrimSpace(source.RoleName)); name != "" {
			if role, ok := roles["name:"+name]; ok {
				return role, true
			}
		}
	}
	return sysRoleInfo{}, false
}

func fallbackClusterSyncOperatorRole(roles map[string]sysRoleInfo) (sysRoleInfo, bool) {
	for _, key := range []string{"code:" + BuiltinRoleOperator, "name:" + BuiltinRoleOperator} {
		if role, ok := roles[key]; ok && role.ID > 0 {
			return role, true
		}
	}
	return sysRoleInfo{}, false
}

func appendClusterRoleMapping(mappings []map[string]any, sourceRoles []ClusterUserSyncRole, target sysRoleInfo, mapped bool, fallback bool) []map[string]any {
	if len(sourceRoles) == 0 {
		return mappings
	}
	source := sourceRoles[0]
	key := map[string]any{
		"sourceRoleId":   source.RoleID,
		"sourceRoleCode": source.RoleCode,
		"sourceRoleName": source.RoleName,
	}
	for _, existing := range mappings {
		if existing["sourceRoleId"] == key["sourceRoleId"] && existing["sourceRoleCode"] == key["sourceRoleCode"] {
			return mappings
		}
	}
	if mapped || fallback {
		key["targetRoleId"] = target.ID
		key["targetRoleCode"] = target.Code
		key["targetRoleName"] = target.Name
		key["status"] = "mapped"
		if fallback {
			key["fallback"] = BuiltinRoleOperator
		}
	} else {
		key["status"] = "missing"
	}
	return append(mappings, key)
}

func createClusterSyncUser(tx *gorm.DB, source ClusterUserSyncItem, role sysRoleInfo, passwordHash string, isRandomPwd bool, actorID int64, ts time.Time) error {
	nickName := source.NickName
	if nickName == "" {
		nickName = source.UserName
	}
	email, phone, err := encryptUserContactPair(source.Email, source.Phone)
	if err != nil {
		return err
	}
	row := sysUserInfo{
		ID:          source.UserID,
		UserName:    source.UserName,
		NickName:    nickName,
		Password:    passwordHash,
		Email:       email,
		Phone:       phone,
		Status:      normalizeUserSyncStatus(source.Status),
		IsRandomPwd: isRandomPwd,
	}
	row.CreatedBy = actorID
	row.UpdatedBy = actorID
	row.CreatedTime = ts
	row.UpdatedTime = ts
	if err := tx.Create(&row).Error; err != nil {
		return err
	}
	return createClusterSyncWorkspaceUser(tx, source.UserID, role, actorID, ts)
}

func restoreClusterSyncUser(tx *gorm.DB, localUserID int64, source ClusterUserSyncItem, role sysRoleInfo, passwordHash string, isRandomPwd bool, actorID, now int64) error {
	email, phone, err := encryptUserContactPair(source.Email, source.Phone)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"user_name":     source.UserName,
		"nick_name":     firstNonEmptyForRepo(source.NickName, source.UserName),
		"email":         email,
		"phone":         phone,
		"status":        normalizeUserSyncStatus(source.Status),
		"password":      passwordHash,
		"is_random_pwd": isRandomPwd,
		"deleted_time":  int64(0),
	}
	if err := tx.Unscoped().Model(&sysUserInfo{}).Where("user_id = ?", localUserID).
		Updates(touchByValues(updates, actorID, now)).Error; err != nil {
		return err
	}
	return replaceClusterSyncWorkspaceUser(tx, localUserID, role, actorID, now)
}

func updateClusterSyncUser(tx *gorm.DB, localUserID int64, source ClusterUserSyncItem, role sysRoleInfo, passwordHash, policy string, isRandomPwd bool, actorID, now int64) error {
	email, phone, err := encryptUserContactPair(source.Email, source.Phone)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"user_name": source.UserName,
		"nick_name": firstNonEmptyForRepo(source.NickName, source.UserName),
		"email":     email,
		"phone":     phone,
		"status":    normalizeUserSyncStatus(source.Status),
	}
	if policy == UserSyncPasswordPolicySource && strings.TrimSpace(passwordHash) != "" {
		updates["password"] = passwordHash
		updates["is_random_pwd"] = isRandomPwd
	}
	if err := tx.Model(&sysUserInfo{}).Where("user_id = ? AND deleted_time = 0", localUserID).
		Updates(touchByValues(updates, actorID, now)).Error; err != nil {
		return err
	}
	return replaceClusterSyncWorkspaceUser(tx, localUserID, role, actorID, now)
}

func replaceClusterSyncWorkspaceUser(tx *gorm.DB, userID int64, role sysRoleInfo, actorID, now int64) error {
	var rows []sysWorkspaceUser
	if err := tx.Where("workspace_id = ? AND user_id = ? AND deleted_time = 0", int64(1), userID).
		Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 1 && rows[0].RoleID == role.ID && rows[0].RoleCode == role.Code {
		return nil
	}
	if err := softDeleteWorkspaceUserRows(tx, rows, actorID, now); err != nil {
		return err
	}
	return createClusterSyncWorkspaceUser(tx, userID, role, actorID, repoTimeFromMilli(now))
}

func createClusterSyncWorkspaceUser(tx *gorm.DB, userID int64, role sysRoleInfo, actorID int64, ts time.Time) error {
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
	workspaceUser.CreatedBy = actorID
	workspaceUser.UpdatedBy = actorID
	workspaceUser.CreatedTime = ts
	workspaceUser.UpdatedTime = ts
	if err := tx.Create(&workspaceUser).Error; err != nil {
		return err
	}
	homePage, err := roleDefaultHomePage(tx, role)
	if err != nil {
		return err
	}
	config := UserConfig{UserID: userID, HomePage: homePage, CreatedTime: ts, UpdatedTime: ts}
	return tx.Where("user_id = ?", userID).Assign(config).FirstOrCreate(&config).Error
}

func softDeleteClusterUser(tx *gorm.DB, current sysUserInfo, actorID, now int64) error {
	if strings.EqualFold(strings.TrimSpace(current.UserName), "tier0") {
		return ErrSystemReadonly
	}
	if err := tx.Model(&sysUserInfo{}).Where("user_id = ? AND deleted_time = 0", current.ID).
		Updates(softDeleteNoDelByValues(actorID, now)).Error; err != nil {
		return err
	}
	var workspaceRows []sysWorkspaceUser
	if err := tx.Where("user_id = ? AND deleted_time = 0", current.ID).Find(&workspaceRows).Error; err != nil {
		return err
	}
	if err := softDeleteWorkspaceUserRows(tx, workspaceRows, actorID, now+1000); err != nil {
		return err
	}
	return tx.Where("user_id = ?", current.ID).Delete(&UserConfig{}).Error
}

func normalizeUserSyncStatus(status int64) int64 {
	if status == 1 {
		return 1
	}
	return 0
}

func userSyncIssue(user ClusterUserSyncItem, reason, detail string) map[string]any {
	out := userSyncUserSummary(user)
	out["reason"] = reason
	out["detail"] = detail
	return out
}

func userSyncUserSummary(user ClusterUserSyncItem) map[string]any {
	return map[string]any{
		"userId":   user.UserID,
		"userName": user.UserName,
		"status":   user.Status,
	}
}

func firstNonEmptyForRepo(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
