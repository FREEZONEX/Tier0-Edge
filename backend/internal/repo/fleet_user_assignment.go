package repo

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	fleetAccountAssigned   = "assigned"
	fleetAccountUnassigned = "unassigned"
)

var ErrFleetUserNotAssignable = errors.New("fleet user is not assignable")

type FleetAccountAssignment struct {
	ID                 int64  `gorm:"column:id;primaryKey" json:"id"`
	FleetNodeID        string `gorm:"column:fleet_node_id" json:"fleetNodeID"`
	CenterUserID       int64  `gorm:"column:center_user_id" json:"centerUserID"`
	Status             string `gorm:"column:status" json:"status"`
	AssignmentRevision int64  `gorm:"column:assignment_revision" json:"assignmentRevision"`
	AssignedBy         int64  `gorm:"column:assigned_by" json:"assignedBy"`
	AssignedAt         int64  `gorm:"column:assigned_at" json:"assignedAt"`
	UnassignedAt       int64  `gorm:"column:unassigned_at" json:"unassignedAt"`
	NoDelTime
}

func (FleetAccountAssignment) TableName() string { return "fleet_account_assignment" }

type FleetAssignedAccount struct {
	CenterUserID       int64  `gorm:"column:center_user_id" json:"centerUserID"`
	UserName           string `gorm:"column:user_name" json:"userName"`
	NickName           string `gorm:"column:nick_name" json:"nickName"`
	Email              string `gorm:"column:email" json:"email"`
	Phone              string `gorm:"column:phone" json:"phone"`
	UserStatus         int64  `gorm:"column:user_status" json:"userStatus"`
	RoleCode           string `gorm:"column:role_code" json:"roleCode"`
	AssignmentRevision int64  `gorm:"column:assignment_revision" json:"assignmentRevision"`
	AssignedBy         int64  `gorm:"column:assigned_by" json:"assignedBy"`
	AssignedAt         int64  `gorm:"column:assigned_at" json:"assignedAt"`
}

type FleetAccountAssignmentSnapshot struct {
	NodeID      string                 `json:"nodeID"`
	FleetNodeID string                 `json:"fleetNodeID"`
	Revision    int64                  `json:"revision"`
	Accounts    []FleetAssignedAccount `json:"accounts"`
}

type FleetAssignedUserSnapshot struct {
	FleetNodeID string                `json:"fleetNodeId"`
	Revision    int64                 `json:"revision"`
	Users       []ClusterUserSyncItem `json:"users"`
}

func (r *ClusterRepo) GetFleetNodeAccountAssignments(ctx context.Context, fleetNodeID string) (FleetAccountAssignmentSnapshot, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	if fleetNodeID == "" {
		return FleetAccountAssignmentSnapshot{}, ErrInvalidArgument
	}
	var node FleetNode
	if err := r.db.WithContext(ctx).Where("fleet_node_id = ? AND status <> ?", fleetNodeID, FleetNodeStatusDeleted).Take(&node).Error; err != nil {
		return FleetAccountAssignmentSnapshot{}, normalizeDBError(err)
	}
	return fleetAccountAssignmentSnapshot(r.db.WithContext(ctx), node)
}

func (r *ClusterRepo) ReplaceFleetNodeAccountAssignments(
	ctx context.Context,
	fleetNodeID string,
	centerUserIDs []int64,
	expectedRevision int64,
	actorID int64,
) (FleetAccountAssignmentSnapshot, bool, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	desiredIDs, err := canonicalFleetAccountUserIDs(centerUserIDs)
	if err != nil || fleetNodeID == "" || expectedRevision < 0 {
		return FleetAccountAssignmentSnapshot{}, false, ErrInvalidArgument
	}
	var snapshot FleetAccountAssignmentSnapshot
	changed := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node FleetNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("fleet_node_id = ? AND status <> ?", fleetNodeID, FleetNodeStatusDeleted).Take(&node).Error; err != nil {
			return normalizeDBError(err)
		}
		if node.AssignmentRevision != expectedRevision {
			return ErrConflict
		}
		if err := validateFleetAssignableUsers(tx, desiredIDs); err != nil {
			return err
		}

		var current []FleetAccountAssignment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("fleet_node_id = ?", fleetNodeID).Order("center_user_id").Find(&current).Error; err != nil {
			return normalizeDBError(err)
		}
		currentAssigned := make([]int64, 0, len(current))
		byUserID := make(map[int64]FleetAccountAssignment, len(current))
		for _, item := range current {
			byUserID[item.CenterUserID] = item
			if item.Status == fleetAccountAssigned {
				currentAssigned = append(currentAssigned, item.CenterUserID)
			}
		}
		sort.Slice(currentAssigned, func(i, j int) bool { return currentAssigned[i] < currentAssigned[j] })
		if equalFleetAccountUserIDs(currentAssigned, desiredIDs) {
			var snapshotErr error
			snapshot, snapshotErr = fleetAccountAssignmentSnapshot(tx, node)
			return snapshotErr
		}

		changed = true
		now := time.Now().UTC().UnixMilli()
		ts := repoTimeFromMilli(now)
		nextRevision := node.AssignmentRevision + 1
		desiredSet := make(map[int64]struct{}, len(desiredIDs))
		for _, userID := range desiredIDs {
			desiredSet[userID] = struct{}{}
			if currentItem, exists := byUserID[userID]; exists {
				updates := map[string]any{"status": fleetAccountAssigned, "assignment_revision": nextRevision, "updated_by": actorID, "updated_time": ts}
				if currentItem.Status != fleetAccountAssigned {
					updates["assigned_by"] = actorID
					updates["assigned_at"] = now
					updates["unassigned_at"] = int64(0)
				}
				if err := tx.Model(&FleetAccountAssignment{}).Where("id = ?", currentItem.ID).Updates(updates).Error; err != nil {
					return normalizeDBError(err)
				}
				continue
			}
			item := FleetAccountAssignment{FleetNodeID: fleetNodeID, CenterUserID: userID, Status: fleetAccountAssigned, AssignmentRevision: nextRevision, AssignedBy: actorID, AssignedAt: now}
			item.CreatedBy = actorID
			item.UpdatedBy = actorID
			item.CreatedTime = ts
			item.UpdatedTime = ts
			if err := tx.Create(&item).Error; err != nil {
				return normalizeDBError(err)
			}
		}
		for _, currentItem := range current {
			if currentItem.Status != fleetAccountAssigned {
				continue
			}
			if _, keep := desiredSet[currentItem.CenterUserID]; keep {
				continue
			}
			updates := map[string]any{"status": fleetAccountUnassigned, "assignment_revision": nextRevision, "unassigned_at": now, "updated_by": actorID, "updated_time": ts}
			if err := tx.Model(&FleetAccountAssignment{}).Where("id = ?", currentItem.ID).Updates(updates).Error; err != nil {
				return normalizeDBError(err)
			}
		}
		if err := tx.Model(&FleetNode{}).Where("id = ? AND status <> ?", node.ID, FleetNodeStatusDeleted).Updates(map[string]any{"assignment_revision": nextRevision, "updated_by": actorID, "updated_time": ts}).Error; err != nil {
			return normalizeDBError(err)
		}
		node.AssignmentRevision = nextRevision
		if err := enqueueFleetUserSyncRequested(ctx, tx, node, actorID, now); err != nil {
			return err
		}
		var snapshotErr error
		snapshot, snapshotErr = fleetAccountAssignmentSnapshot(tx, node)
		return snapshotErr
	})
	if err != nil {
		return FleetAccountAssignmentSnapshot{}, false, err
	}
	return snapshot, changed, nil
}

func (r *ClusterRepo) GetFleetAssignedUserSnapshot(ctx context.Context, fleetNodeID string) (FleetAssignedUserSnapshot, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	if fleetNodeID == "" {
		return FleetAssignedUserSnapshot{}, ErrInvalidArgument
	}
	var snapshot FleetAssignedUserSnapshot
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node FleetNode
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("fleet_node_id = ? AND status <> ?", fleetNodeID, FleetNodeStatusDeleted).
			Take(&node).Error; err != nil {
			return normalizeDBError(err)
		}
		var assignedUserIDs []int64
		if err := tx.Model(&FleetAccountAssignment{}).
			Where("fleet_node_id = ? AND status = ?", fleetNodeID, fleetAccountAssigned).
			Order("center_user_id").
			Pluck("center_user_id", &assignedUserIDs).Error; err != nil {
			return normalizeDBError(err)
		}
		users, err := NewIAMRepo(tx).ListClusterUserSnapshotByIDs(ctx, assignedUserIDs, false)
		if err != nil {
			return err
		}
		snapshot = FleetAssignedUserSnapshot{FleetNodeID: node.FleetNodeID, Revision: node.AssignmentRevision, Users: users}
		return nil
	})
	return snapshot, err
}

func fleetAccountAssignmentSnapshot(db *gorm.DB, node FleetNode) (FleetAccountAssignmentSnapshot, error) {
	accounts := make([]FleetAssignedAccount, 0)
	err := db.Table("fleet_account_assignment a").
		Select("a.center_user_id, u.user_name, u.nick_name, u.email, u.phone, u.status AS user_status, COALESCE(wu.role_code, '') AS role_code, a.assignment_revision, a.assigned_by, a.assigned_at").
		Joins("JOIN sys_user_info u ON u.user_id = a.center_user_id").
		Joins("LEFT JOIN sys_workspace_user wu ON wu.workspace_id = 1 AND wu.user_id = a.center_user_id AND wu.deleted_time = 0").
		Where("a.fleet_node_id = ? AND a.status = ?", node.FleetNodeID, fleetAccountAssigned).
		Order("a.center_user_id").Scan(&accounts).Error
	if err != nil {
		return FleetAccountAssignmentSnapshot{}, normalizeDBError(err)
	}
	for index := range accounts {
		email, phone, err := decryptUserContacts(accounts[index].CenterUserID, accounts[index].Email, accounts[index].Phone)
		if err != nil {
			return FleetAccountAssignmentSnapshot{}, err
		}
		accounts[index].Email = email
		accounts[index].Phone = phone
	}
	return FleetAccountAssignmentSnapshot{NodeID: node.FleetNodeID, FleetNodeID: node.FleetNodeID, Revision: node.AssignmentRevision, Accounts: accounts}, nil
}

func validateFleetAssignableUsers(db *gorm.DB, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	type userRoleRow struct {
		UserID   int64  `gorm:"column:user_id"`
		UserName string `gorm:"column:user_name"`
		RoleCode string `gorm:"column:role_code"`
	}
	var rows []userRoleRow
	if err := db.Table("sys_user_info u").Select("u.user_id, u.user_name, wu.role_code").Joins("JOIN sys_workspace_user wu ON wu.workspace_id = 1 AND wu.user_id = u.user_id AND wu.deleted_time = 0").Where("u.deleted_time = 0 AND u.user_id IN ?", userIDs).Scan(&rows).Error; err != nil {
		return normalizeDBError(err)
	}
	found := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		found[row.UserID] = struct{}{}
		roleCode := strings.ToLower(strings.TrimSpace(row.RoleCode))
		if row.UserID < PersonalIDStart || strings.EqualFold(strings.TrimSpace(row.UserName), "tier0") || roleCode == "owner" {
			return ErrFleetUserNotAssignable
		}
	}
	if len(found) != len(userIDs) {
		return ErrFleetUserNotAssignable
	}
	return nil
}

func canonicalFleetAccountUserIDs(values []int64) ([]int64, error) {
	seen := make(map[int64]struct{}, len(values))
	canonical := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			return nil, ErrInvalidArgument
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		canonical = append(canonical, value)
	}
	sort.Slice(canonical, func(i, j int) bool { return canonical[i] < canonical[j] })
	return canonical, nil
}

func equalFleetAccountUserIDs(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func unassignDeletedFleetCenterUser(ctx context.Context, tx *gorm.DB, centerUserID, actorID, now int64) error {
	var assignments []FleetAccountAssignment
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("center_user_id = ? AND status = ?", centerUserID, fleetAccountAssigned).
		Order("fleet_node_id").Find(&assignments).Error; err != nil {
		return err
	}
	for _, assignment := range assignments {
		var node FleetNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("fleet_node_id = ?", assignment.FleetNodeID).
			Take(&node).Error; err != nil {
			return err
		}
		if node.Status == FleetNodeStatusDeleted {
			continue
		}
		nextRevision := node.AssignmentRevision + 1
		ts := repoTimeFromMilli(now)
		if err := tx.Model(&FleetAccountAssignment{}).Where("id = ?", assignment.ID).Updates(map[string]any{
			"status":              fleetAccountUnassigned,
			"assignment_revision": nextRevision,
			"unassigned_at":       now,
			"updated_by":          actorID,
			"updated_time":        ts,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&FleetNode{}).Where("id = ?", node.ID).Updates(map[string]any{
			"assignment_revision": nextRevision,
			"updated_by":          actorID,
			"updated_time":        ts,
		}).Error; err != nil {
			return err
		}
		node.AssignmentRevision = nextRevision
		if err := enqueueFleetUserSyncRequested(ctx, tx, node, actorID, now); err != nil {
			return err
		}
	}
	return nil
}

func touchFleetAssignmentsForCenterUser(ctx context.Context, tx *gorm.DB, centerUserID, actorID, now int64) error {
	var assignments []FleetAccountAssignment
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("center_user_id = ? AND status = ?", centerUserID, fleetAccountAssigned).
		Order("fleet_node_id").Find(&assignments).Error; err != nil {
		return err
	}
	for _, assignment := range assignments {
		var node FleetNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("fleet_node_id = ?", assignment.FleetNodeID).
			Take(&node).Error; err != nil {
			return err
		}
		if node.Status == FleetNodeStatusDeleted {
			continue
		}
		nextRevision := node.AssignmentRevision + 1
		ts := repoTimeFromMilli(now)
		if err := tx.Model(&FleetAccountAssignment{}).Where("id = ?", assignment.ID).Updates(map[string]any{
			"assignment_revision": nextRevision,
			"updated_by":          actorID,
			"updated_time":        ts,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&FleetNode{}).Where("id = ?", node.ID).Updates(map[string]any{
			"assignment_revision": nextRevision,
			"updated_by":          actorID,
			"updated_time":        ts,
		}).Error; err != nil {
			return err
		}
		node.AssignmentRevision = nextRevision
		if err := enqueueFleetUserSyncRequested(ctx, tx, node, actorID, now); err != nil {
			return err
		}
	}
	return nil
}

const fleetUserSyncRequestedEventType = "fleet.user.sync.requested"

type fleetUserSyncRequestedPayload struct {
	EventID     string `json:"eventID"`
	FleetNodeID string `json:"fleetNodeID"`
	MqttAuthID  string `json:"mqttAuthID"`
	Revision    int64  `json:"revision"`
	RequestedBy int64  `json:"requestedBy"`
	RequestedAt int64  `json:"requestedAt"`
}

func enqueueFleetUserSyncRequested(ctx context.Context, tx *gorm.DB, node FleetNode, actorID, now int64) error {
	if strings.TrimSpace(node.FleetNodeID) == "" || node.ConnectClientID <= 0 || node.AssignmentRevision <= 0 {
		return ErrInvalidArgument
	}
	eventID := fmt.Sprintf("fleet-user-sync:%s:%d", node.FleetNodeID, node.AssignmentRevision)
	if len(eventID) > 64 {
		digest := sha256.Sum256([]byte(eventID))
		eventID = fmt.Sprintf("%x", digest[:])
	}
	payload, err := json.Marshal(fleetUserSyncRequestedPayload{
		EventID:     eventID,
		FleetNodeID: node.FleetNodeID,
		MqttAuthID:  strconv.FormatInt(node.ConnectClientID, 10),
		Revision:    node.AssignmentRevision,
		RequestedBy: actorID,
		RequestedAt: now,
	})
	if err != nil {
		return err
	}
	return NewOutboxRepo(tx).EnqueueOutbox(ctx, OutboxEvent{
		EventID:       eventID,
		EventType:     fleetUserSyncRequestedEventType,
		AggregateType: "fleetNode",
		AggregateID:   node.FleetNodeID,
		Payload:       payload,
	})
}
