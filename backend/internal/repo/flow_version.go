package repo

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	FlowVersionTypeManual = 1
	FlowVersionTypeAuto   = 2
)

type FlowVersion struct {
	ID           int64  `gorm:"column:id;primaryKey;autoIncrement:false" json:"id"`
	FlowID       int64  `gorm:"column:flow_id;index:idx_uns_flow_version_flow_created,sort:desc;uniqueIndex:idx_uns_flow_version_flow_name" json:"flowId"`
	VersionName  string `gorm:"column:version_name;size:63;uniqueIndex:idx_uns_flow_version_flow_name" json:"versionName"`
	VersionType  int    `gorm:"column:version_type;default:1;index:idx_uns_flow_version_type" json:"versionType"`
	Description  string `gorm:"column:description;size:512" json:"description"`
	SnapshotData string `gorm:"column:snapshot_data;type:text" json:"snapshotData"`
	Checksum     string `gorm:"column:checksum;size:64" json:"checksum"`
	SourceRev    string `gorm:"column:source_rev;size:64" json:"sourceRev"`
	NoDelTime
}

func (FlowVersion) TableName() string { return "uns_nodered_flow_version" }

// 复合主键 (parent_id, node_id)：同一节点可同时被 source 与 event flow 各关联一条
type flowNode struct {
	ParentID int64 `gorm:"column:parent_id;primaryKey;index:idx_uns_flow_node_parent"`
	NodeID   int64 `gorm:"column:node_id;primaryKey"`
	OnlyTime
}

func (flowNode) TableName() string { return "uns_nodered_flow_node" }

var ErrFlowFolderCannotDeploy = errors.New("flow folder cannot deploy")

func (r *FlowRepo) FlowVersions(ctx context.Context, flowID int64, pageNo, pageSize int) ([]FlowVersion, int64, error) {
	if pageNo <= 0 {
		pageNo = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var out []FlowVersion
	q := r.db.WithContext(ctx).Model(&FlowVersion{}).Where("flow_id = ?", flowID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_time DESC, id DESC").
		Offset((pageNo - 1) * pageSize).Limit(pageSize).Find(&out).Error
	return out, total, err
}

func (r *FlowRepo) CreateFlowVersion(ctx context.Context, flowID, userID int64, name, description, snapshotData, sourceRev string) (FlowVersion, error) {
	flow, err := r.GetFlow(ctx, flowID)
	if err != nil {
		return FlowVersion{}, err
	}
	if flow.NodeType == 1 {
		return FlowVersion{}, ErrFlowFolderCannotDeploy
	}
	if strings.TrimSpace(snapshotData) == "" {
		snapshotData = flow.FlowData
	}
	now := time.Now().UTC().UnixMilli()
	var out FlowVersion
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		exists, err := flowVersionNameExists(tx, flowID, name)
		if err != nil {
			return err
		}
		if exists {
			return ErrDuplicate
		}
		version, err := createFlowVersion(tx, flowID, name, FlowVersionTypeManual, description, snapshotData, sourceRev, userID, now)
		if err != nil {
			return err
		}
		status := "draft"
		if strings.EqualFold(strings.TrimSpace(flow.Status), "disabled") {
			status = "disabled"
		} else if strings.TrimSpace(flow.RuntimeFlowID) != "" {
			status = "pending"
		}
		res := tx.Model(&Flow{}).Where("id = ? AND deleted_time = 0", flow.ID).
			Updates(touchByValues(map[string]any{
				"flow_data":          snapshotData,
				"flow_status":        status,
				"current_version_id": version.ID,
			}, userID, now))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		out = version
		return nil
	})
	if err != nil {
		return FlowVersion{}, normalizeDBError(err)
	}
	return out, nil
}

func (r *FlowRepo) GetFlowVersion(ctx context.Context, flowID, versionID int64) (FlowVersion, error) {
	var version FlowVersion
	if err := r.db.WithContext(ctx).
		Where("id = ? AND flow_id = ?", versionID, flowID).Take(&version).Error; err != nil {
		return FlowVersion{}, err
	}
	return version, nil
}

func (r *FlowRepo) RestoreFlowVersion(ctx context.Context, flowID, versionID, userID int64, snapshotData, runtimeFlowID, status string) (Flow, error) {
	now := time.Now().UTC().UnixMilli()
	status = strings.TrimSpace(status)
	if status == "" {
		status = "deployed"
	}
	var flow Flow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var version FlowVersion
		if err := tx.Where("id = ? AND flow_id = ?", versionID, flowID).Take(&version).Error; err != nil {
			return err
		}
		if strings.TrimSpace(snapshotData) == "" {
			snapshotData = version.SnapshotData
		}
		updates := map[string]any{
			"flow_data":          snapshotData,
			"flow_status":        status,
			"current_version_id": version.ID,
		}
		if strings.TrimSpace(runtimeFlowID) != "" {
			updates["flow_id"] = strings.TrimSpace(runtimeFlowID)
		}
		res := tx.Model(&flow).Clauses(returningAll()).
			Where("id = ? AND deleted_time = 0", flowID).
			Updates(touchByValues(updates, userID, now))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return Flow{}, err
	}
	flow.UnsNodeIDs, _ = r.FlowNodeIDs(ctx, flow.ID)
	return flow, nil
}

func (r *FlowRepo) FlowNodeIDs(ctx context.Context, flowID int64) ([]int64, error) {
	var out []int64
	err := r.db.WithContext(ctx).Table("uns_nodered_flow_node AS m").
		Select("m.node_id").
		Joins("JOIN uns_nodered_flow AS f ON f.id = m.parent_id").
		Where("m.parent_id = ?", flowID).
		Where("f.deleted_time = 0").
		Where("f.flow_type IN (1, 2) AND f.node_type = 2").
		Order("m.node_id").Pluck("m.node_id", &out).Error
	return out, err
}

// BindFlowIDMapsByNodeIDs 返回节点绑定的 flow id，按类型拆分为 source/event 两个 map，各取最新一条关联
func (r *FlowRepo) BindFlowIDMapsByNodeIDs(ctx context.Context, nodeIDs []int64) (map[int64]int64, map[int64]int64, error) {
	sourceOut := make(map[int64]int64, len(nodeIDs))
	eventOut := make(map[int64]int64, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return sourceOut, eventOut, nil
	}
	cleanNodeIDs := make([]int64, 0, len(nodeIDs))
	seen := make(map[int64]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if nodeID <= 0 {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		cleanNodeIDs = append(cleanNodeIDs, nodeID)
	}
	if len(cleanNodeIDs) == 0 {
		return sourceOut, eventOut, nil
	}
	var rows []struct {
		NodeID   int64 `gorm:"column:node_id"`
		FlowID   int64 `gorm:"column:flow_id"`
		FlowType int   `gorm:"column:flow_type"`
	}
	err := r.db.WithContext(ctx).
		Table("uns_nodered_flow_node AS m").
		Select("m.node_id, m.parent_id AS flow_id, f.flow_type").
		Joins("JOIN uns_nodered_flow AS f ON f.id = m.parent_id").
		Where("m.node_id IN ?", cleanNodeIDs).
		Where("f.deleted_time = 0").
		Where("f.flow_type IN (1, 2) AND f.node_type = 2").
		Order("m.node_id ASC, m.created_time DESC, m.parent_id DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		if row.NodeID == 0 || row.FlowID == 0 {
			continue
		}
		out := sourceOut
		if row.FlowType == 2 {
			out = eventOut
		}
		if _, ok := out[row.NodeID]; ok {
			continue
		}
		out[row.NodeID] = row.FlowID
	}
	return sourceOut, eventOut, nil
}

func (r *FlowRepo) FlowIDsByNodeIDs(ctx context.Context, nodeIDs []int64, flowType int) ([]int64, error) {
	if len(nodeIDs) == 0 {
		return []int64{}, nil
	}
	cleanNodeIDs := make([]int64, 0, len(nodeIDs))
	seen := make(map[int64]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if nodeID <= 0 {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		cleanNodeIDs = append(cleanNodeIDs, nodeID)
	}
	if len(cleanNodeIDs) == 0 {
		return []int64{}, nil
	}
	q := r.db.WithContext(ctx).
		Table("uns_nodered_flow_node AS m").
		Joins("JOIN uns_nodered_flow AS f ON f.id = m.parent_id").
		Where("m.node_id IN ?", cleanNodeIDs).
		Where("f.deleted_time = 0").
		Where("f.node_type = 2")
	if flowType != 0 {
		q = q.Where("f.flow_type = ?", flowType)
	}
	var out []int64
	err := q.Distinct("f.id").Order("f.id").Pluck("f.id", &out).Error
	return out, err
}

func replaceFlowNodes(db *gorm.DB, flowID int64, flowType int, nodeIDs []int64, now int64) error {
	cleanNodeIDs := make([]int64, 0, len(nodeIDs))
	seen := make(map[int64]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if nodeID == 0 {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		cleanNodeIDs = append(cleanNodeIDs, nodeID)
	}
	if err := db.Where("parent_id = ?", flowID).Delete(&flowNode{}).Error; err != nil {
		return err
	}
	if len(cleanNodeIDs) > 0 {
		// 同类型内抢占：flowType 非 0 时只把节点从同类型的其他 flow 上摘掉，
		// 允许同一节点在 source 与 event 各保留一条绑定
		evict := db.Where("node_id IN ?", cleanNodeIDs)
		if flowType != 0 {
			sub := db.Session(&gorm.Session{NewDB: true}).Model(&Flow{}).Select("id").Where("flow_type = ? AND deleted_time = 0", flowType)
			evict = evict.Where("parent_id IN (?)", sub)
		}
		if err := evict.Delete(&flowNode{}).Error; err != nil {
			return err
		}
	}
	for _, nodeID := range cleanNodeIDs {
		row := flowNode{ParentID: flowID, NodeID: nodeID}
		ts := repoTimeFromMilli(now)
		row.CreatedTime = ts
		row.UpdatedTime = ts
		if err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "parent_id"}, {Name: "node_id"}},
			DoUpdates: clause.Assignments(map[string]any{"updated_time": ts}),
		}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func createFlowVersion(db *gorm.DB, flowID int64, name string, versionType int, description, data, sourceRev string, userID, now int64) (FlowVersion, error) {
	checksum, err := flowSnapshotChecksum(data)
	if err != nil {
		return FlowVersion{}, err
	}
	v := FlowVersion{
		FlowID:       flowID,
		VersionName:  strings.TrimSpace(name),
		VersionType:  versionType,
		Description:  strings.TrimSpace(description),
		SnapshotData: data,
		Checksum:     checksum,
		SourceRev:    strings.TrimSpace(sourceRev),
	}
	ensureID(&v.ID)
	v.CreatedBy = userID
	v.UpdatedBy = userID
	ts := repoTimeFromMilli(now)
	v.CreatedTime = ts
	v.UpdatedTime = ts
	err = db.Create(&v).Error
	return v, normalizeDBError(err)
}

func flowVersionNameExists(db *gorm.DB, flowID int64, name string) (bool, error) {
	var count int64
	err := db.Model(&FlowVersion{}).
		Where("flow_id = ? AND version_name = ?", flowID, strings.TrimSpace(name)).
		Count(&count).Error
	return count > 0, err
}

func flowSnapshotChecksum(snapshot string) (string, error) {
	if strings.TrimSpace(snapshot) == "" {
		return "", nil
	}
	var normalized any
	if err := json.Unmarshal([]byte(snapshot), &normalized); err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:]), nil
}

func (r *FlowRepo) PruneAutoFlowVersions(ctx context.Context, flowID int64, keep int) error {
	if flowID == 0 || keep <= 0 {
		return nil
	}
	return pruneAutoFlowVersions(r.db.WithContext(ctx), flowID, keep)
}

func pruneAutoFlowVersions(db *gorm.DB, flowID int64, keep int) error {
	if flowID == 0 || keep <= 0 {
		return nil
	}
	sub := db.Model(&FlowVersion{}).
		Select("id").
		Where("flow_id = ? AND version_type = ?", flowID, FlowVersionTypeAuto).
		Order("created_time DESC, id DESC").
		Limit(keep)
	return db.
		Where("flow_id = ? AND version_type = ? AND id NOT IN (?)", flowID, FlowVersionTypeAuto, sub).
		Delete(&FlowVersion{}).Error
}
