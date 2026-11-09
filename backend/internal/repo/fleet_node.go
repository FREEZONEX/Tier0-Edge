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
	FleetNodeStatusUnjoined = "unjoined"
	FleetNodeStatusOnline   = "online"
	FleetNodeStatusOffline  = "offline"
	FleetNodeStatusDeleted  = "deleted"

	FleetNodeQuotaUnlimited int64 = -1
)

var ErrFleetNodeQuotaExceeded = errors.New("fleet node quota exceeded")

type FleetNode struct {
	ID                 int64  `gorm:"column:id;primaryKey" json:"-"`
	FleetNodeID        string `gorm:"column:fleet_node_id" json:"fleetNodeID"`
	Name               string `gorm:"column:name" json:"name"`
	Description        string `gorm:"column:description" json:"description"`
	CenterType         string `gorm:"column:center_type" json:"centerType"`
	Status             string `gorm:"column:status" json:"status"`
	ActiveSessionID    string `gorm:"column:active_session_id" json:"activeSessionID,omitempty"`
	FirstJoinedAt      int64  `gorm:"column:first_joined_at" json:"firstJoinedAt"`
	LastOnlineAt       int64  `gorm:"column:last_online_at" json:"lastOnlineAt"`
	LastOfflineAt      int64  `gorm:"column:last_offline_at" json:"lastOfflineAt"`
	AgentVersion       string `gorm:"column:agent_version" json:"agentVersion"`
	ProtocolVersion    string `gorm:"column:protocol_version" json:"protocolVersion"`
	OS                 string `gorm:"column:os" json:"os"`
	Arch               string `gorm:"column:arch" json:"arch"`
	ConnectClientID    int64  `gorm:"column:connect_client_id" json:"connectClientID"`
	AssignmentRevision int64  `gorm:"column:assignment_revision" json:"assignmentRevision"`
	SoftNoDelByTime
}

func (FleetNode) TableName() string { return "fleet_node" }

type CreateFleetNodeInput struct {
	FleetNodeID string
	Name        string
	Description string
	CenterType  string
	ActorID     int64
	BranchLimit int64
}

type FleetNodeConnection struct {
	Node   FleetNode
	Client ConnectClient
}

type FleetNodeView struct {
	FleetNode
	CredentialID         string `gorm:"column:credential_id" json:"credentialID"`
	CredentialStatus     string `gorm:"column:credential_status" json:"credentialStatus"`
	CredentialExpiresAt  int64  `gorm:"column:credential_expires_at" json:"credentialExpiresAt"`
	CredentialLastUsedAt int64  `gorm:"column:credential_last_used_at" json:"credentialLastUsedAt"`
}

type FleetNodeListFilter struct {
	Keyword      string
	Status       string
	OS           string
	AgentVersion string
	Page         int
	Size         int
}

type FleetRepo struct{ db *gorm.DB }

func NewFleetRepo(in any) *FleetRepo { return &FleetRepo{db: GetCommonConn(in)} }

func (r *FleetRepo) CreateNodeWithConnection(ctx context.Context, input CreateFleetNodeInput) (FleetNodeConnection, error) {
	input.FleetNodeID = strings.TrimSpace(input.FleetNodeID)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.CenterType = strings.TrimSpace(input.CenterType)
	if input.CenterType == "" {
		input.CenterType = "enterprise"
	}
	if input.FleetNodeID == "" || input.Name == "" || input.BranchLimit < FleetNodeQuotaUnlimited {
		return FleetNodeConnection{}, ErrInvalidArgument
	}

	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	node := FleetNode{
		FleetNodeID: input.FleetNodeID,
		Name:        input.Name,
		Description: input.Description,
		CenterType:  input.CenterType,
		Status:      FleetNodeStatusUnjoined,
	}
	node.CreatedBy = input.ActorID
	node.UpdatedBy = input.ActorID
	node.CreatedTime = ts
	node.UpdatedTime = ts
	var client ConnectClient

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockFleetQuota(tx); err != nil {
			return err
		}
		var currentCount int64
		if err := tx.Model(&FleetNode{}).
			Where("status IN ?", []string{FleetNodeStatusUnjoined, FleetNodeStatusOnline, FleetNodeStatusOffline}).
			Count(&currentCount).Error; err != nil {
			return normalizeDBError(err)
		}
		if input.BranchLimit != FleetNodeQuotaUnlimited && currentCount >= input.BranchLimit {
			return ErrFleetNodeQuotaExceeded
		}
		var err error
		clientInput := ConnectClient{
			Name:        input.Name,
			Description: input.Description,
			Type:        ConnectClientTypeEdge,
		}
		clientInput.CreatedBy = input.ActorID
		client, err = NewConnectClientRepo(tx).CreateConnectClient(ctx, clientInput)
		if err != nil {
			return err
		}
		node.ConnectClientID = client.ID
		if err := tx.Create(&node).Error; err != nil {
			return normalizeDBError(err)
		}
		return nil
	})
	if err != nil {
		return FleetNodeConnection{}, err
	}
	return FleetNodeConnection{Node: node, Client: client}, nil
}

func (r *FleetRepo) CountQuotaNodes(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&FleetNode{}).
		Where("status IN ?", []string{FleetNodeStatusUnjoined, FleetNodeStatusOnline, FleetNodeStatusOffline}).
		Count(&count).Error
	return count, normalizeDBError(err)
}

func (r *FleetRepo) CountNodesByStatus(ctx context.Context) (map[string]int64, error) {
	type row struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:count"`
	}
	var rows []row
	err := r.db.WithContext(ctx).Unscoped().Model(&FleetNode{}).
		Select("status, COUNT(*) AS count").
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, normalizeDBError(err)
	}
	counts := map[string]int64{
		FleetNodeStatusUnjoined: 0,
		FleetNodeStatusOnline:   0,
		FleetNodeStatusOffline:  0,
		FleetNodeStatusDeleted:  0,
	}
	for _, item := range rows {
		counts[item.Status] = item.Count
	}
	return counts, nil
}

func (r *FleetRepo) GetNode(ctx context.Context, fleetNodeID string, includeDeleted bool) (FleetNode, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	if fleetNodeID == "" {
		return FleetNode{}, ErrInvalidArgument
	}
	query := r.db.WithContext(ctx).Where("fleet_node_id = ?", fleetNodeID)
	if includeDeleted {
		query = query.Unscoped()
	} else {
		query = query.Where("status <> ?", FleetNodeStatusDeleted)
	}
	var node FleetNode
	err := query.Take(&node).Error
	return node, normalizeDBError(err)
}

func (r *FleetRepo) GetNodeView(ctx context.Context, fleetNodeID string, includeDeleted bool) (FleetNodeView, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	if fleetNodeID == "" {
		return FleetNodeView{}, ErrInvalidArgument
	}
	query := fleetNodeViewQuery(r.db.WithContext(ctx)).Where("n.fleet_node_id = ?", fleetNodeID)
	if !includeDeleted {
		query = query.Where("n.status <> ?", FleetNodeStatusDeleted)
	}
	var view FleetNodeView
	err := query.Take(&view).Error
	return view, normalizeDBError(err)
}

func (r *FleetRepo) ListNodeViews(ctx context.Context, filter FleetNodeListFilter) ([]FleetNodeView, int64, error) {
	filter.Keyword = strings.TrimSpace(strings.ToLower(filter.Keyword))
	filter.Status = strings.TrimSpace(strings.ToLower(filter.Status))
	filter.OS = strings.TrimSpace(strings.ToLower(filter.OS))
	filter.AgentVersion = strings.TrimSpace(filter.AgentVersion)
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Size <= 0 {
		filter.Size = 20
	}
	if filter.Size > 100 {
		filter.Size = 100
	}

	base := r.db.WithContext(ctx).Table("fleet_node n")
	if filter.Status == "" {
		base = base.Where("n.status <> ?", FleetNodeStatusDeleted)
	} else {
		base = base.Where("n.status = ?", filter.Status)
	}
	if filter.Keyword != "" {
		keyword := "%" + filter.Keyword + "%"
		base = base.Where("LOWER(n.name) LIKE ? OR LOWER(n.fleet_node_id) LIKE ?", keyword, keyword)
	}
	if filter.OS != "" {
		base = base.Where("LOWER(n.os) = ?", filter.OS)
	}
	if filter.AgentVersion != "" {
		base = base.Where("n.agent_version = ?", filter.AgentVersion)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	items := make([]FleetNodeView, 0)
	query := fleetNodeViewQuery(base).
		Order("n.id DESC").
		Offset((filter.Page - 1) * filter.Size).
		Limit(filter.Size)
	if err := query.Scan(&items).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	return items, total, nil
}

func (r *FleetRepo) UpdateNode(ctx context.Context, fleetNodeID, name, description string, actorID int64) (FleetNodeView, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if fleetNodeID == "" || name == "" {
		return FleetNodeView{}, ErrInvalidArgument
	}
	now := time.Now().UTC().UnixMilli()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node FleetNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("fleet_node_id = ? AND status <> ?", fleetNodeID, FleetNodeStatusDeleted).
			Take(&node).Error; err != nil {
			return normalizeDBError(err)
		}
		if node.Name == name && node.Description == description {
			return nil
		}
		if err := tx.Model(&FleetNode{}).Where("id = ?", node.ID).Updates(map[string]any{
			"name":         name,
			"description":  description,
			"updated_by":   actorID,
			"updated_time": repoTimeFromMilli(now),
		}).Error; err != nil {
			return normalizeDBError(err)
		}
		if err := tx.Model(&ConnectClient{}).Where("id = ? AND deleted_time = 0", node.ConnectClientID).Updates(map[string]any{
			"name":         name,
			"description":  description,
			"updated_by":   actorID,
			"updated_time": repoTimeFromMilli(now),
		}).Error; err != nil {
			return normalizeDBError(err)
		}
		return nil
	})
	if err != nil {
		return FleetNodeView{}, err
	}
	return r.GetNodeView(ctx, fleetNodeID, false)
}

func (r *FleetRepo) DeleteNode(ctx context.Context, fleetNodeID string, actorID int64) error {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	if fleetNodeID == "" {
		return ErrInvalidArgument
	}
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node FleetNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("fleet_node_id = ? AND status <> ?", fleetNodeID, FleetNodeStatusDeleted).
			Take(&node).Error; err != nil {
			return normalizeDBError(err)
		}
		if err := tx.Model(&FleetNode{}).Where("id = ?", node.ID).Updates(map[string]any{
			"status":            FleetNodeStatusDeleted,
			"active_session_id": "",
			"updated_by":        actorID,
			"updated_time":      ts,
			"deleted_time":      now / 1000,
		}).Error; err != nil {
			return normalizeDBError(err)
		}
		if err := tx.Model(&FleetOperation{}).
			Where("fleet_node_id = ? AND status IN ?", fleetNodeID, []string{FleetOperationStatusQueued, FleetOperationStatusRunning}).
			Updates(map[string]any{
				"status":       FleetOperationStatusCanceled,
				"error_code":   "FLEET_NODE_DELETED",
				"finished_at":  now,
				"updated_time": ts,
			}).Error; err != nil {
			return normalizeDBError(err)
		}
		if err := tx.Model(&ConnectClient{}).
			Where("id = ? AND deleted_time = 0", node.ConnectClientID).
			Updates(map[string]any{
				"auth_status":    AuthStatusDisabled,
				"connect_status": ConnectStatusOffline,
				"updated_by":     actorID,
				"updated_time":   ts,
			}).Error; err != nil {
			return normalizeDBError(err)
		}
		return nil
	})
	return err
}

func (r *FleetRepo) RestoreNodeWithConnection(
	ctx context.Context,
	fleetNodeID string,
	actorID int64,
	branchLimit int64,
) (FleetNodeConnection, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	if fleetNodeID == "" || branchLimit < FleetNodeQuotaUnlimited {
		return FleetNodeConnection{}, ErrInvalidArgument
	}
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	var restored FleetNodeConnection
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockFleetQuota(tx); err != nil {
			return err
		}
		var activeCount int64
		if err := tx.Model(&FleetNode{}).
			Where("status IN ?", []string{FleetNodeStatusUnjoined, FleetNodeStatusOnline, FleetNodeStatusOffline}).
			Count(&activeCount).Error; err != nil {
			return normalizeDBError(err)
		}
		if branchLimit != FleetNodeQuotaUnlimited && activeCount >= branchLimit {
			return ErrFleetNodeQuotaExceeded
		}
		var node FleetNode
		if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("fleet_node_id = ? AND status = ?", fleetNodeID, FleetNodeStatusDeleted).
			Take(&node).Error; err != nil {
			return normalizeDBError(err)
		}
		if err := tx.Model(&ConnectClient{}).
			Where("id = ? AND deleted_time = 0", node.ConnectClientID).
			Updates(map[string]any{
				"auth_status":    AuthStatusDisabled,
				"connect_status": ConnectStatusOffline,
				"updated_by":     actorID,
				"updated_time":   ts,
			}).Error; err != nil {
			return normalizeDBError(err)
		}

		clientInput := ConnectClient{
			Name:        node.Name,
			Description: node.Description,
			Type:        ConnectClientTypeEdge,
		}
		clientInput.CreatedBy = actorID
		client, err := NewConnectClientRepo(tx).CreateConnectClient(ctx, clientInput)
		if err != nil {
			return err
		}
		if err := tx.Unscoped().Model(&FleetNode{}).Where("id = ?", node.ID).Updates(map[string]any{
			"status":            FleetNodeStatusUnjoined,
			"active_session_id": "",
			"connect_client_id": client.ID,
			"updated_by":        actorID,
			"updated_time":      ts,
			"deleted_time":      int64(0),
		}).Error; err != nil {
			return normalizeDBError(err)
		}
		node.Status = FleetNodeStatusUnjoined
		node.ActiveSessionID = ""
		node.ConnectClientID = client.ID
		node.UpdatedBy = actorID
		node.UpdatedTime = ts
		node.DeletedTime = 0
		restored = FleetNodeConnection{Node: node, Client: client}
		return nil
	})
	return restored, err
}

func (r *FleetRepo) GetNodeByConnectClientID(ctx context.Context, connectClientID int64, includeDeleted bool) (FleetNode, error) {
	if connectClientID <= 0 {
		return FleetNode{}, ErrInvalidArgument
	}
	query := r.db.WithContext(ctx).Where("connect_client_id = ?", connectClientID)
	if includeDeleted {
		query = query.Unscoped()
	} else {
		query = query.Where("status <> ?", FleetNodeStatusDeleted)
	}
	var node FleetNode
	err := query.Take(&node).Error
	return node, normalizeDBError(err)
}

type ActivateFleetNodeSessionInput struct {
	FleetNodeID     string
	SessionID       string
	AgentVersion    string
	ProtocolVersion string
	OS              string
	Arch            string
	ConnectedAt     int64
}

func (r *FleetRepo) ActivateNodeSession(ctx context.Context, input ActivateFleetNodeSessionInput) error {
	input.FleetNodeID = strings.TrimSpace(input.FleetNodeID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	if input.FleetNodeID == "" || input.SessionID == "" || input.ConnectedAt <= 0 {
		return ErrInvalidArgument
	}
	result := r.db.WithContext(ctx).Model(&FleetNode{}).
		Where("fleet_node_id = ? AND status <> ?", input.FleetNodeID, FleetNodeStatusDeleted).
		Updates(map[string]any{
			"status":            FleetNodeStatusOnline,
			"active_session_id": input.SessionID,
			"first_joined_at":   gorm.Expr("CASE WHEN first_joined_at = 0 THEN ? ELSE first_joined_at END", input.ConnectedAt),
			"last_online_at":    input.ConnectedAt,
			"agent_version":     strings.TrimSpace(input.AgentVersion),
			"protocol_version":  strings.TrimSpace(input.ProtocolVersion),
			"os":                strings.TrimSpace(input.OS),
			"arch":              strings.TrimSpace(input.Arch),
			"updated_time":      repoTimeFromMilli(input.ConnectedAt),
		})
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *FleetRepo) TouchNodeSession(ctx context.Context, fleetNodeID, sessionID string, heartbeatAt int64) error {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	sessionID = strings.TrimSpace(sessionID)
	if fleetNodeID == "" || sessionID == "" || heartbeatAt <= 0 {
		return ErrInvalidArgument
	}
	result := r.db.WithContext(ctx).Model(&FleetNode{}).
		Where("fleet_node_id = ? AND active_session_id = ? AND status = ?", fleetNodeID, sessionID, FleetNodeStatusOnline).
		Updates(map[string]any{
			"last_online_at": heartbeatAt,
			"updated_time":   repoTimeFromMilli(heartbeatAt),
		})
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

func (r *FleetRepo) DeactivateNodeSession(ctx context.Context, fleetNodeID, sessionID string, disconnectedAt int64) error {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	sessionID = strings.TrimSpace(sessionID)
	if fleetNodeID == "" || sessionID == "" || disconnectedAt <= 0 {
		return ErrInvalidArgument
	}
	return normalizeDBError(r.db.WithContext(ctx).Model(&FleetNode{}).
		Where("fleet_node_id = ? AND active_session_id = ? AND status = ?", fleetNodeID, sessionID, FleetNodeStatusOnline).
		Updates(map[string]any{
			"status":            FleetNodeStatusOffline,
			"active_session_id": "",
			"last_offline_at":   disconnectedAt,
			"updated_time":      repoTimeFromMilli(disconnectedAt),
		}).Error)
}

func (r *FleetRepo) MarkOnlineNodesOffline(ctx context.Context, disconnectedAt int64) error {
	if disconnectedAt <= 0 {
		return ErrInvalidArgument
	}
	return normalizeDBError(r.db.WithContext(ctx).Model(&FleetNode{}).
		Where("status = ?", FleetNodeStatusOnline).
		Updates(map[string]any{
			"status":            FleetNodeStatusOffline,
			"active_session_id": "",
			"last_offline_at":   disconnectedAt,
			"updated_time":      repoTimeFromMilli(disconnectedAt),
		}).Error)
}

func lockFleetQuota(tx *gorm.DB) error {
	type fleetQuotaLock struct {
		ID int16 `gorm:"column:id"`
	}
	var lock fleetQuotaLock
	err := tx.Table("fleet_quota_lock").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = 1").Take(&lock).Error
	return normalizeDBError(err)
}

func fleetNodeViewQuery(db *gorm.DB) *gorm.DB {
	return db.
		Table("fleet_node n").
		Select(`n.*,
				COALESCE(c.client_id, '') AS credential_id,
				CASE WHEN c.auth_status = 1 THEN 'active' WHEN c.id IS NOT NULL THEN 'revoked' ELSE '' END AS credential_status,
				0 AS credential_expires_at,
				COALESCE(c.connected_time, 0) AS credential_last_used_at`).
		Joins("LEFT JOIN uns_connect_client c ON c.id = n.connect_client_id AND c.deleted_time = 0")
}

// ReissueNodeCredential rotates only infrastructure credentials, keeping node identity and business trust.
func (r *FleetRepo) ReissueNodeCredential(ctx context.Context, fleetNodeID string, actorID int64) (FleetNodeConnection, error) {
	now := time.Now().UTC().UnixMilli()
	ts := repoTimeFromMilli(now)
	var rotated FleetNodeConnection
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node FleetNode
		if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("fleet_node_id = ? AND status <> ?", fleetNodeID, FleetNodeStatusDeleted).
			Take(&node).Error; err != nil {
			return normalizeDBError(err)
		}
		if err := tx.Model(&ConnectClient{}).
			Where("id = ? AND deleted_time = 0", node.ConnectClientID).
			Updates(map[string]any{
				"auth_status":    AuthStatusDisabled,
				"connect_status": ConnectStatusOffline,
				"updated_by":     actorID,
				"updated_time":   ts,
			}).Error; err != nil {
			return normalizeDBError(err)
		}

		clientInput := ConnectClient{
			Name:        node.Name,
			Description: node.Description,
			Type:        ConnectClientTypeEdge,
		}
		clientInput.CreatedBy = actorID
		client, err := NewConnectClientRepo(tx).CreateConnectClient(ctx, clientInput)
		if err != nil {
			return err
		}
		if err := tx.Unscoped().Model(&FleetNode{}).Where("id = ?", node.ID).Updates(map[string]any{
			"status":            FleetNodeStatusUnjoined,
			"active_session_id": "",
			"connect_client_id": client.ID,
			"updated_by":        actorID,
			"updated_time":      ts,
		}).Error; err != nil {
			return normalizeDBError(err)
		}
		node.Status = FleetNodeStatusUnjoined
		node.ActiveSessionID = ""
		node.ConnectClientID = client.ID
		node.UpdatedBy = actorID
		node.UpdatedTime = ts
		rotated = FleetNodeConnection{Node: node, Client: client}
		return nil
	})
	return rotated, err
}

// WithTransaction groups Fleet node metadata and credential updates.
func (r *FleetRepo) WithTransaction(ctx context.Context, fn func(*FleetRepo) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(NewFleetRepo(tx)) })
}
