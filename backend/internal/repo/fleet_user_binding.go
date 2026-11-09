package repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	FleetUserBindingStatusActive     = "active"
	FleetUserBindingStatusUnassigned = "unassigned"
	FleetUserBindingStatusConflict   = "conflict"

	FleetUserSyncStatusSuccess = "success"
	FleetUserSyncStatusPartial = "partial"
)

type FleetUserBinding struct {
	ID                 int64     `gorm:"column:id;primaryKey" json:"id"`
	CenterIdentity     string    `gorm:"column:center_identity" json:"centerIdentity"`
	CenterType         string    `gorm:"column:center_type" json:"centerType"`
	FleetNodeID        string    `gorm:"column:fleet_node_id" json:"fleetNodeID"`
	SourceUserID       int64     `gorm:"column:source_user_id" json:"sourceUserID"`
	LocalUserID        int64     `gorm:"column:local_user_id" json:"localUserID"`
	Status             string    `gorm:"column:status" json:"status"`
	LastSourceRevision int64     `gorm:"column:last_source_revision" json:"lastSourceRevision"`
	LastAppliedAt      int64     `gorm:"column:last_applied_at" json:"lastAppliedAt"`
	LastErrorCode      string    `gorm:"column:last_error_code" json:"lastErrorCode"`
	LastErrorMessage   string    `gorm:"column:last_error_message" json:"lastErrorMessage"`
	CreatedTime        time.Time `gorm:"column:created_time;autoCreateTime" json:"createdTime"`
	UpdatedTime        time.Time `gorm:"column:updated_time;autoUpdateTime" json:"updatedTime"`
}

func (FleetUserBinding) TableName() string { return "fleet_user_binding" }

type FleetUserSyncState struct {
	CenterIdentity      string    `gorm:"column:center_identity;primaryKey" json:"centerIdentity"`
	FleetNodeID         string    `gorm:"column:fleet_node_id;primaryKey" json:"fleetNodeID"`
	CenterType          string    `gorm:"column:center_type" json:"centerType"`
	LastAttemptRevision int64     `gorm:"column:last_attempt_revision" json:"lastAttemptRevision"`
	LastAppliedRevision int64     `gorm:"column:last_applied_revision" json:"lastAppliedRevision"`
	Status              string    `gorm:"column:status" json:"status"`
	LastErrorCode       string    `gorm:"column:last_error_code" json:"lastErrorCode"`
	UpdatedTime         time.Time `gorm:"column:updated_time;autoUpdateTime" json:"updatedTime"`
	CreatedTime         time.Time `gorm:"column:created_time;autoCreateTime" json:"createdTime"`
}

func (FleetUserSyncState) TableName() string { return "fleet_user_sync_state" }

func (r *IAMRepo) FleetSyncedUserIDs(ctx context.Context, centerID, nodeID string) ([]int64, error) {
	ids := []int64{}
	err := r.db.WithContext(ctx).Model(&FleetUserBinding{}).
		Where("center_identity = ? AND fleet_node_id = ? AND local_user_id > 0 AND status IN ?", centerID, nodeID, []string{FleetUserBindingStatusActive, FleetUserBindingStatusUnassigned}).
		Distinct().Pluck("local_user_id", &ids).Error
	return ids, err
}

func (r *IAMRepo) ApplyFleetUserSnapshot(ctx context.Context, input ClusterUserSyncApplyInput) (ClusterUserSyncApplyResult, error) {
	result := ClusterUserSyncApplyResult{Stats: map[string]int{}}
	input.CenterIdentity = strings.TrimSpace(input.CenterIdentity)
	input.CenterType = strings.TrimSpace(input.CenterType)
	input.FleetNodeID = strings.TrimSpace(input.FleetNodeID)
	if input.CenterIdentity == "" || input.FleetNodeID == "" || input.SourceRevision <= 0 || !input.Complete || strings.TrimSpace(input.DefaultPasswordHash) == "" {
		return result, ErrInvalidArgument
	}
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	total := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockUserEmailWrites(tx); err != nil {
			return err
		}
		if err := tx.Exec("LOCK TABLE fleet_user_sync_state IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		var state FleetUserSyncState
		stateErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("center_identity = ? AND fleet_node_id = ?", input.CenterIdentity, input.FleetNodeID).
			Take(&state).Error
		if stateErr != nil && !errors.Is(stateErr, gorm.ErrRecordNotFound) {
			return stateErr
		}
		if stateErr == nil && input.SourceRevision < state.LastAttemptRevision {
			return ErrConflict
		}
		if stateErr == nil && input.SourceRevision == state.LastAppliedRevision && state.Status == FleetUserSyncStatusSuccess {
			return nil
		}

		roles, err := activeRolesForClusterSync(tx)
		if err != nil {
			return err
		}
		usersByID, activeNames, activeEmails, err := clusterSyncUsersByID(tx)
		if err != nil {
			return err
		}
		var bindings []FleetUserBinding
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("center_identity = ? AND fleet_node_id = ?", input.CenterIdentity, input.FleetNodeID).
			Find(&bindings).Error; err != nil {
			return err
		}
		bindingBySource := make(map[int64]FleetUserBinding, len(bindings))
		for _, binding := range bindings {
			bindingBySource[binding.SourceUserID] = binding
		}
		desiredSourceIDs := make(map[int64]struct{}, len(input.Users))
		seenSourceIDs := make(map[int64]struct{}, len(input.Users))
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
			if _, duplicate := seenSourceIDs[source.UserID]; duplicate {
				result.SkippedUsers = append(result.SkippedUsers, userSyncIssue(source, "duplicateUser", "duplicate source userId"))
				continue
			}
			seenSourceIDs[source.UserID] = struct{}{}
			if isClusterSyncSystemUser(source) || source.DeletedTime > 0 {
				continue
			}
			total++
			desiredSourceIDs[source.UserID] = struct{}{}
			targetRole, mapped := mapClusterSyncRole(source.RoleList, roles)
			fallback := false
			if !mapped {
				operatorRole, ok := fallbackClusterSyncOperatorRole(roles)
				if !ok {
					result.RoleMappings = appendClusterRoleMapping(result.RoleMappings, source.RoleList, targetRole, false, false)
					result.SkippedUsers = append(result.SkippedUsers, userSyncIssue(source, "roleMappingMissing", "no matching target role"))
					binding := bindingBySource[source.UserID]
					if err := upsertFleetUserBinding(tx, input, binding, source.UserID, binding.LocalUserID, FleetUserBindingStatusConflict, now, "roleMappingMissing", "no matching target role"); err != nil {
						return err
					}
					continue
				}
				targetRole = operatorRole
				fallback = true
			}
			result.RoleMappings = appendClusterRoleMapping(result.RoleMappings, source.RoleList, targetRole, mapped, fallback)

			binding, hasBinding := bindingBySource[source.UserID]
			localUserID := binding.LocalUserID
			current, hasLocalUser := usersByID[localUserID]
			if hasBinding && localUserID > 0 && !hasLocalUser {
				result.Conflicts = append(result.Conflicts, userSyncIssue(source, "bindingTargetMissing", "bound local user does not exist"))
				if err := upsertFleetUserBinding(tx, input, binding, source.UserID, localUserID, FleetUserBindingStatusConflict, now, "bindingTargetMissing", "bound local user does not exist"); err != nil {
					return err
				}
				continue
			}
			if conflictID, ok := activeNames[strings.ToLower(source.UserName)]; ok && (!hasLocalUser || conflictID != localUserID) {
				result.Conflicts = append(result.Conflicts, userSyncIssue(source, "usernameConflict", "username is already used by an unbound local user"))
				if err := upsertFleetUserBinding(tx, input, binding, source.UserID, localUserID, FleetUserBindingStatusConflict, now, "usernameConflict", "username is already used by an unbound local user"); err != nil {
					return err
				}
				continue
			}
			if source.Email != "" {
				if conflictID, ok := activeEmails[strings.ToLower(source.Email)]; ok && (!hasLocalUser || conflictID != localUserID) {
					result.Conflicts = append(result.Conflicts, userSyncIssue(source, "emailConflict", "email is already used by an unbound local user"))
					if err := upsertFleetUserBinding(tx, input, binding, source.UserID, localUserID, FleetUserBindingStatusConflict, now, "emailConflict", "email is already used by an unbound local user"); err != nil {
						return err
					}
					continue
				}
			}

			if !hasBinding || localUserID <= 0 {
				localUserID, err = createFleetSyncUser(tx, source, targetRole, input.DefaultPasswordHash, input.ActorID, ts)
				if err != nil {
					return err
				}
				result.CreatedUsers = append(result.CreatedUsers, userSyncUserSummary(source))
			} else if current.DeletedTime > 0 {
				passwordHash := strings.TrimSpace(current.Password)
				if passwordHash == "" {
					passwordHash = input.DefaultPasswordHash
				}
				if err := restoreClusterSyncUser(tx, localUserID, source, targetRole, passwordHash, current.IsRandomPwd, input.ActorID, now); err != nil {
					return err
				}
				result.CreatedUsers = append(result.CreatedUsers, userSyncUserSummary(source))
			} else {
				if err := updateClusterSyncUser(tx, localUserID, source, targetRole, "", UserSyncPasswordPolicyDefault, current.IsRandomPwd, input.ActorID, now); err != nil {
					return err
				}
				if source.Status == 1 {
					result.UpdatedUsers = append(result.UpdatedUsers, userSyncUserSummary(source))
				} else {
					result.DisabledUsers = append(result.DisabledUsers, userSyncUserSummary(source))
				}
				delete(activeNames, strings.ToLower(current.UserName))
				if strings.TrimSpace(current.Email) != "" {
					delete(activeEmails, strings.ToLower(current.Email))
				}
			}
			activeNames[strings.ToLower(source.UserName)] = localUserID
			if source.Email != "" {
				activeEmails[strings.ToLower(source.Email)] = localUserID
			}
			if err := upsertFleetUserBinding(tx, input, binding, source.UserID, localUserID, FleetUserBindingStatusActive, now, "", ""); err != nil {
				return err
			}
		}

		for _, binding := range bindings {
			if _, keep := desiredSourceIDs[binding.SourceUserID]; keep {
				continue
			}
			if binding.Status == FleetUserBindingStatusActive && binding.LocalUserID > 0 {
				if current, exists := usersByID[binding.LocalUserID]; exists && current.DeletedTime == 0 {
					if err := softDeleteClusterUser(tx, current, input.ActorID, now); err != nil {
						return err
					}
					result.DeletedUsers = append(result.DeletedUsers, map[string]any{"userId": binding.SourceUserID, "userName": current.UserName, "status": current.Status})
				}
			}
			if err := upsertFleetUserBinding(tx, input, binding, binding.SourceUserID, binding.LocalUserID, FleetUserBindingStatusUnassigned, now, "", ""); err != nil {
				return err
			}
		}

		state.CenterIdentity = input.CenterIdentity
		state.FleetNodeID = input.FleetNodeID
		state.CenterType = input.CenterType
		state.LastAttemptRevision = input.SourceRevision
		state.Status = FleetUserSyncStatusSuccess
		state.LastErrorCode = ""
		if len(result.Conflicts) > 0 || len(result.SkippedUsers) > 0 {
			state.Status = FleetUserSyncStatusPartial
			state.LastErrorCode = "FLEET_USER_SYNC_PARTIAL"
		} else {
			state.LastAppliedRevision = input.SourceRevision
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "center_identity"}, {Name: "fleet_node_id"}},
			UpdateAll: true,
		}).Create(&state).Error
	})
	result.Stats["created"] = len(result.CreatedUsers)
	result.Stats["updated"] = len(result.UpdatedUsers)
	result.Stats["disabled"] = len(result.DisabledUsers)
	result.Stats["deleted"] = len(result.DeletedUsers)
	result.Stats["skipped"] = len(result.SkippedUsers)
	result.Stats["conflicts"] = len(result.Conflicts)
	result.Stats["total"] = total
	return result, normalizeDBError(err)
}

func createFleetSyncUser(tx *gorm.DB, source ClusterUserSyncItem, role sysRoleInfo, passwordHash string, actorID int64, ts time.Time) (int64, error) {
	localUserID, err := nextPersonalID(tx, "sys_user_info", "user_id")
	if err != nil {
		return 0, err
	}
	local := source
	local.UserID = localUserID
	if err := createClusterSyncUser(tx, local, role, passwordHash, true, actorID, ts); err != nil {
		return 0, err
	}
	return localUserID, nil
}

func upsertFleetUserBinding(tx *gorm.DB, input ClusterUserSyncApplyInput, current FleetUserBinding, sourceUserID, localUserID int64, status string, appliedAt int64, errorCode, errorMessage string) error {
	row := FleetUserBinding{
		ID:                 current.ID,
		CenterIdentity:     input.CenterIdentity,
		CenterType:         input.CenterType,
		FleetNodeID:        input.FleetNodeID,
		SourceUserID:       sourceUserID,
		LocalUserID:        localUserID,
		Status:             status,
		LastSourceRevision: input.SourceRevision,
		LastAppliedAt:      appliedAt,
		LastErrorCode:      errorCode,
		LastErrorMessage:   errorMessage,
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "center_identity"}, {Name: "fleet_node_id"}, {Name: "source_user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"center_type", "local_user_id", "status", "last_source_revision", "last_applied_at", "last_error_code", "last_error_message", "updated_time",
		}),
	}).Create(&row).Error
}
