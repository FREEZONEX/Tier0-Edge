package repo

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// CreateCollaborationNode shares the Fleet quota and identity without issuing
// a legacy MQTT credential or implicitly creating a UNS mirror subscription.
func (r *FleetRepo) CreateCollaborationNode(ctx context.Context, input CreateFleetNodeInput) (FleetNode, error) {
	if input.FleetNodeID == "" || input.Name == "" || input.BranchLimit < FleetNodeQuotaUnlimited {
		return FleetNode{}, ErrInvalidArgument
	}
	node := FleetNode{FleetNodeID: input.FleetNodeID, Name: input.Name, Description: input.Description, CenterType: "enterprise", Status: FleetNodeStatusUnjoined}
	node.CreatedBy = input.ActorID
	node.UpdatedBy = input.ActorID
	node.CreatedTime = repoTimeFromMilli(time.Now().UnixMilli())
	node.UpdatedTime = node.CreatedTime
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockFleetQuota(tx); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&FleetNode{}).Where("status <> ?", FleetNodeStatusDeleted).Count(&count).Error; err != nil {
			return err
		}
		if input.BranchLimit != FleetNodeQuotaUnlimited && count >= input.BranchLimit {
			return ErrFleetNodeQuotaExceeded
		}
		if err := tx.Create(&node).Error; err != nil {
			return err
		}
		return tx.Create(&FleetCollaborationNode{NodeID: node.FleetNodeID, RegistrationStatus: "unregistered", NetworkStatus: "direct", BusinessStatus: "unchecked"}).Error
	})
	return node, normalizeDBError(err)
}

// FleetCollaborationNode stores registration independently of Agent sessions.
type FleetCollaborationNode struct {
	TLSFingerprint     string  `json:"-"`
	InstallationID     string  `json:"installationId"`
	NodeID             string  `gorm:"column:node_id;primaryKey" json:"nodeId"`
	RegistrationStatus string  `json:"registrationStatus"`
	PublicKey          string  `json:"-"`
	KeyID              string  `json:"keyId"`
	Endpoint           string  `json:"endpoint"`
	NetworkNodeID      *string `json:"networkNodeId,omitempty"`
	RegisteredAt       int64   `json:"registeredAt"`
	CheckedAt          int64   `json:"checkedAt"`
	LastConnectedAt    int64   `json:"lastConnectedAt"`
	NetworkStatus      string  `json:"networkStatus"`
	BusinessStatus     string  `json:"businessStatus"`
}

func (FleetCollaborationNode) TableName() string { return "fleet_collaboration_node" }

type FleetCollaborationNodeView struct {
	FleetCollaborationNode
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (r *FleetRepo) CollaborationNode(ctx context.Context, id string) (FleetCollaborationNodeView, error) {
	var out FleetCollaborationNodeView
	err := r.db.WithContext(ctx).Table("fleet_collaboration_node c").Select("c.*, n.name, n.description").Joins("JOIN fleet_node n ON n.fleet_node_id=c.node_id AND n.status <> ?", FleetNodeStatusDeleted).Where("c.node_id = ?", id).Take(&out).Error
	return out, normalizeDBError(err)
}
func (r *FleetRepo) CollaborationNodes(ctx context.Context, page, size int) ([]FleetCollaborationNodeView, int64, error) {
	var out []FleetCollaborationNodeView
	var total int64
	query := r.db.WithContext(ctx).Table("fleet_collaboration_node c").Joins("JOIN fleet_node n ON n.fleet_node_id=c.node_id AND n.status <> ?", FleetNodeStatusDeleted)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	err := query.Select("c.*, n.name, n.description").Order("n.id DESC").Offset((page - 1) * size).Limit(size).Find(&out).Error
	return out, total, normalizeDBError(err)
}

type FleetCollaborationEnrollment struct {
	CodeHash   string `gorm:"primaryKey"`
	NodeID     string
	ExpiresAt  int64
	ConsumedAt int64
	ProofHash  string
	CreatedBy  int64
}

func (FleetCollaborationEnrollment) TableName() string { return "fleet_collaboration_enrollment" }
func (r *FleetRepo) CreateCollaborationEnrollment(ctx context.Context, row FleetCollaborationEnrollment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node FleetCollaborationNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND registration_status = ?", row.NodeID, "unregistered").Take(&node).Error; err != nil {
			return normalizeDBError(err)
		}
		// Minting a new code invalidates outstanding codes for this node.
		if err := tx.Model(&FleetCollaborationEnrollment{}).Where("node_id = ? AND consumed_at = 0", row.NodeID).Update("expires_at", int64(0)).Error; err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
}
func (r *FleetRepo) EnrollCollaborationNode(ctx context.Context, hash, proofHash string, node FleetCollaborationNode, now int64, proofTimestamp ...int64) (FleetCollaborationNode, error) {
	var result FleetCollaborationNode
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var active FleetNode
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("fleet_node_id = ? AND status <> ?", node.NodeID, FleetNodeStatusDeleted).Take(&active).Error; err != nil {
			return normalizeDBError(err)
		}
		var code FleetCollaborationEnrollment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code_hash = ? AND node_id = ?", hash, node.NodeID).Take(&code).Error; err != nil {
			return normalizeDBError(err)
		}
		if code.ConsumedAt > 0 {
			if code.ProofHash != proofHash {
				return ErrInvalidArgument
			}
			return tx.Where("node_id = ? AND registration_status = ?", node.NodeID, "registered").Take(&result).Error
		}
		// An already consumed, byte-identical signed proof is a read-only receipt.
		// Only first consumption requires a fresh proof and unexpired code.
		if code.ExpiresAt < now || (len(proofTimestamp) > 0 && proofTimestamp[0] < now-5*60*1000) {
			return ErrInvalidArgument
		}
		result = node
		result.RegistrationStatus = "registered"
		result.RegisteredAt = now
		update := tx.Model(&FleetCollaborationNode{}).Where("node_id = ? AND registration_status = ?", node.NodeID, "unregistered").Updates(map[string]any{"tls_fingerprint": node.TLSFingerprint, "installation_id": node.InstallationID, "public_key": node.PublicKey, "key_id": node.KeyID, "endpoint": node.Endpoint, "network_node_id": node.NetworkNodeID, "registration_status": "registered", "registered_at": now})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrInvalidArgument
		}
		return tx.Model(&code).Updates(map[string]any{"consumed_at": now, "proof_hash": proofHash}).Error
	})
	return result, normalizeDBError(err)
}

// RecordCollaborationHealth prevents late probes from overwriting newer results.
// Initialize on first confirmed success; thereafter only an observed reconnect
// advances the connection timestamp, not every successful refresh.
func (r *FleetRepo) RecordCollaborationHealth(ctx context.Context, nodeID, network, business string, checkedAt int64) (int64, error) {
	var latest FleetCollaborationNode
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&FleetCollaborationNode{}).Where("node_id=? AND checked_at <= ?", nodeID, checkedAt).Updates(map[string]any{
			"network_status": network, "business_status": business, "checked_at": checkedAt,
			"last_connected_at": gorm.Expr("CASE WHEN ? = 'available' AND (business_status <> 'available' OR last_connected_at = 0) THEN ? ELSE last_connected_at END", business, checkedAt),
		}).Error; err != nil {
			return err
		}
		return tx.Select("last_connected_at").Where("node_id=?", nodeID).Take(&latest).Error
	})
	return latest.LastConnectedAt, normalizeDBError(err)
}

// Network observation does not register a business identity or mark business online.
// Register the business projection independently of any VPN controller.
func (r *FleetRepo) EnsureFleetCollaborationNode(ctx context.Context, nodeID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node FleetNode
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("fleet_node_id=? AND status<>?", nodeID, FleetNodeStatusDeleted).Take(&node).Error; err != nil {
			return normalizeDBError(err)
		}
		row := FleetCollaborationNode{NodeID: nodeID, RegistrationStatus: "unregistered", NetworkStatus: "direct", BusinessStatus: "unchecked"}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "node_id"}}, DoNothing: true}).Create(&row).Error
	})
}

// The domain has authenticated both the configured token and installation key,
// and matched the callback to this node's installation identity. Updating its
// reachable address does not rotate business identity or expand grants.
func (r *FleetRepo) RefreshCollaborationEndpoint(ctx context.Context, nodeID, publicKey, endpoint, pin string) error {
	result := r.db.WithContext(ctx).Model(&FleetCollaborationNode{}).Where("node_id=? AND public_key=? AND registration_status='registered'", nodeID, publicKey).Updates(map[string]any{"endpoint": endpoint, "tls_fingerprint": pin, "business_status": "unchecked"})
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}
