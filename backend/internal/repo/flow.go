package repo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Flow struct {
	ID               int64   `gorm:"column:id;primaryKey;autoIncrement:false" json:"id"`
	RuntimeFlowID    string  `gorm:"column:flow_id;size:64;index" json:"runtimeFlowId"`
	ConnectClientID  int64   `gorm:"column:connect_client_id;index" json:"connectClientId"`
	Name             string  `gorm:"column:flow_name;type:varchar(512);uniqueIndex:idx_uns_flow_parent_name_del,priority:3,where:deleted_time = 0" json:"name"`
	CurrentVersionID int64   `gorm:"column:current_version_id;default:0" json:"currentVersionId"`
	FlowData         string  `gorm:"column:flow_data;type:text" json:"flowData"`
	Status           string  `gorm:"column:flow_status;size:32" json:"status"`
	Template         string  `gorm:"column:template;size:64" json:"template"`
	Description      string  `gorm:"column:description;size:512" json:"description"`
	FlowType         int     `gorm:"column:flow_type;uniqueIndex:idx_uns_flow_parent_name_del,priority:1,where:deleted_time = 0" json:"flowType"`
	NodeType         int     `gorm:"column:node_type;default:1" json:"nodeType"`
	ParentID         int64   `gorm:"column:parent_id;default:0;index:idx_uns_flow_parent;uniqueIndex:idx_uns_flow_parent_name_del,priority:2,where:deleted_time = 0" json:"parentId"`
	SortKey          int64   `gorm:"column:sort_key;default:0;index:idx_uns_flow_sort" json:"sortKey"`
	IsFavorite       int16   `gorm:"column:is_favorite;default:2" json:"isFavorite"`
	UnsNodeIDs       []int64 `gorm:"-" json:"unsNodeIds,omitempty"`
	SoftNoDelByTime
}

func (Flow) TableName() string { return "uns_nodered_flow" }

type FlowFilter struct {
	FlowType int
	ParentID int64
	Keyword  string
}

type FlowRepo struct{ db *gorm.DB }

func NewFlowRepo(in any) *FlowRepo { return &FlowRepo{db: GetCommonConn(in)} }

func (r *FlowRepo) UserNamesByIDs(ctx context.Context, ids []int64) (map[int64]string, error) {
	cleanIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleanIDs = append(cleanIDs, id)
	}
	if len(cleanIDs) == 0 {
		return map[int64]string{}, nil
	}
	var rows []struct {
		UserID   int64  `gorm:"column:user_id"`
		UserName string `gorm:"column:user_name"`
	}
	if err := r.db.WithContext(ctx).
		Table("sys_user_info").
		Select("user_id, user_name").
		Where("user_id IN ? AND deleted_time = 0", cleanIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(rows))
	for _, row := range rows {
		out[row.UserID] = strings.TrimSpace(row.UserName)
	}
	return out, nil
}

func (r *FlowRepo) CreateFlow(ctx context.Context, flow Flow) (Flow, error) {
	if len(flow.UnsNodeIDs) > 0 && !flowCanBindUns(flow) {
		return Flow{}, ErrInvalidArgument
	}
	now := time.Now().UTC().UnixMilli()
	ensureID(&flow.ID)
	if flow.Status == "" {
		flow.Status = "draft"
	}
	flow.CurrentVersionID = 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&flow).Error; err != nil {
			return err
		}
		return replaceFlowNodes(tx, flow.ID, flow.FlowType, flow.UnsNodeIDs, now)
	})
	if err != nil {
		return Flow{}, normalizeDBError(err)
	}
	return flow, nil
}

func (r *FlowRepo) UpdateFlow(ctx context.Context, flow Flow) (Flow, error) {
	now := time.Now().UTC().UnixMilli()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Flow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "flow_type", "node_type").
			Where("id = ? AND deleted_time = 0", flow.ID).
			Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if current.FlowType != flow.FlowType || current.NodeType != flow.NodeType {
			return ErrInvalidArgument
		}
		if len(flow.UnsNodeIDs) > 0 && !flowCanBindUns(current) {
			return ErrInvalidArgument
		}
		res := tx.Model(&Flow{}).Where("id = ? AND deleted_time = 0", flow.ID).
			Updates(touchByValues(map[string]any{
				"flow_name":   flow.Name,
				"template":    flow.Template,
				"description": flow.Description,
				"parent_id":   flow.ParentID,
			}, flow.UpdatedBy, now))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return replaceFlowNodes(tx, flow.ID, flow.FlowType, flow.UnsNodeIDs, now)
	})
	if err != nil {
		return Flow{}, normalizeDBError(err)
	}
	return r.GetFlow(ctx, flow.ID)
}

// source/event flow 均可绑定 UNS 节点，folder 拒绝
func flowCanBindUns(flow Flow) bool {
	return (flow.FlowType == 1 || flow.FlowType == 2) && flow.NodeType == 2
}

// UpdateFlowNameDescription 仅更新 Flow 的 flow_name 与 description（含 touch 字段），
// 不触碰 flow_type/parent_id/flow-node 关联。用于 App 导入顶层 id 命中覆盖时按包内值更新名称与描述。
func (r *FlowRepo) UpdateFlowNameDescription(ctx context.Context, id int64, name, description string, userID int64) (Flow, error) {
	now := time.Now().UTC().UnixMilli()
	var flow Flow
	res := r.db.WithContext(ctx).Model(&flow).Clauses(returningAll()).
		Where("id = ? AND deleted_time = 0", id).
		Updates(touchByValues(map[string]any{
			"flow_name":   name,
			"description": description,
		}, userID, now))
	if res.Error != nil {
		return Flow{}, normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return Flow{}, ErrNotFound
	}
	return flow, nil
}

// UpdateFlowConnectClient 回写 Flow 的 connect_client_id（导入 flow 补建本地 MQTT 凭证时使用）。
func (r *FlowRepo) UpdateFlowConnectClient(ctx context.Context, id, connectClientID int64, userID int64) error {
	now := time.Now().UTC().UnixMilli()
	res := r.db.WithContext(ctx).Model(&Flow{}).
		Where("id = ? AND deleted_time = 0", id).
		Updates(touchByValues(map[string]any{"connect_client_id": connectClientID}, userID, now))
	if res.Error != nil {
		return normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *FlowRepo) MarkFlow(ctx context.Context, id int64, pinned bool, userID int64) (Flow, error) {
	now := time.Now().UTC().UnixMilli()
	sortKey := int64(0)
	isFavorite := int16(2)
	if pinned {
		sortKey = now
		isFavorite = 1
	}
	var out Flow
	res := r.db.WithContext(ctx).Model(&out).Clauses(returningAll()).
		Where("id = ? AND deleted_time = 0", id).
		Updates(touchByValues(map[string]any{
			"sort_key":    sortKey,
			"is_favorite": isFavorite,
		}, userID, now))
	if res.Error != nil {
		return Flow{}, normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return Flow{}, ErrNotFound
	}
	out.UnsNodeIDs, _ = r.FlowNodeIDs(ctx, out.ID)
	return out, nil
}

func (r *FlowRepo) ListFlows(ctx context.Context, filter FlowFilter) ([]Flow, error) {
	q := r.db.WithContext(ctx).Where("deleted_time = 0")
	if filter.FlowType != 0 {
		q = q.Where(`(
			(node_type <> 1 AND flow_type = ?)
			OR
			(node_type = 1 AND EXISTS (
				SELECT 1
				FROM uns_nodered_flow AS flow_child
				WHERE flow_child.deleted_time = 0
					AND flow_child.parent_id = uns_nodered_flow.id
					AND flow_child.node_type <> 1
					AND flow_child.flow_type = ?
			))
		)`, filter.FlowType, filter.FlowType)
	}
	q = q.Where("parent_id = ?", filter.ParentID)
	if kw := strings.TrimSpace(filter.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("(flow_name ILIKE ? OR description ILIKE ?)", like, like)
	}
	var out []Flow
	if err := q.Order("flow_type, parent_id, node_type, sort_key DESC, id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	for i := range out {
		out[i].UnsNodeIDs, _ = r.FlowNodeIDs(ctx, out[i].ID)
	}
	return out, nil
}

func (r *FlowRepo) ListEditorContextFlows(ctx context.Context, flowType int, excludeID int64) ([]Flow, error) {
	q := r.db.WithContext(ctx).
		Where("deleted_time = 0").
		Where("node_type <> 1")
	if flowType != 0 {
		q = q.Where("flow_type = ?", flowType)
	}
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	var out []Flow
	if err := q.Order("flow_type, parent_id, sort_key DESC, id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *FlowRepo) GetFlow(ctx context.Context, id int64) (Flow, error) {
	var flow Flow
	if err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&flow).Error; err != nil {
		return Flow{}, err
	}
	flow.UnsNodeIDs, _ = r.FlowNodeIDs(ctx, id)
	return flow, nil
}

func (r *FlowRepo) GetFlowByName(ctx context.Context, flowType int, parentID int64, name string) (Flow, error) {
	var flow Flow
	if err := r.db.WithContext(ctx).
		Where("flow_type = ? AND parent_id = ? AND flow_name = ? AND deleted_time = 0", flowType, parentID, strings.TrimSpace(name)).
		Order("id").Take(&flow).Error; err != nil {
		return Flow{}, err
	}
	flow.UnsNodeIDs, _ = r.FlowNodeIDs(ctx, flow.ID)
	return flow, nil
}

// GetFlowByRuntimeFlowID 按 runtime Flow ID（flow_id 列，普通索引非唯一）查询 flow。
// 命中结果需由调用方校验归属（如 ParentID），防止跨 project 误命中；未找到返回 ErrNotFound。
func (r *FlowRepo) GetFlowByRuntimeFlowID(ctx context.Context, flowType int, runtimeFlowID string) (Flow, error) {
	var flow Flow
	if err := r.db.WithContext(ctx).
		Where("flow_type = ? AND flow_id = ? AND deleted_time = 0", flowType, strings.TrimSpace(runtimeFlowID)).
		Order("id").Take(&flow).Error; err != nil {
		return Flow{}, err
	}
	flow.UnsNodeIDs, _ = r.FlowNodeIDs(ctx, flow.ID)
	return flow, nil
}

func (r *FlowRepo) AvailableFlowName(ctx context.Context, flowType int, parentID int64, base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", ErrInvalidArgument
	}
	var rows []struct {
		Name string `gorm:"column:flow_name"`
	}
	like := escapeSQLLike(base) + "-%"
	if err := r.db.WithContext(ctx).
		Model(&Flow{}).
		Select("flow_name").
		Where("flow_type = ? AND parent_id = ? AND deleted_time = 0 AND (flow_name = ? OR flow_name LIKE ? ESCAPE '\\')", flowType, parentID, base, like).
		Find(&rows).Error; err != nil {
		return "", err
	}
	existsBase := false
	var maxSuffix int64
	prefix := base + "-"
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == base {
			existsBase = true
			continue
		}
		if suffix, ok := strings.CutPrefix(name, prefix); ok {
			if n, ok := parseFlowNameSuffix(suffix); ok && n > maxSuffix {
				maxSuffix = n
			}
		}
	}
	if !existsBase {
		return base, nil
	}
	return fmt.Sprintf("%s-%d", base, maxSuffix+1), nil
}

func (r *FlowRepo) FindFlowByName(ctx context.Context, flowType int, name string) (Flow, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Flow{}, ErrInvalidArgument
	}
	q := r.db.WithContext(ctx).
		Where("flow_name = ? AND node_type <> 1 AND deleted_time = 0", name)
	if flowType != 0 {
		q = q.Where("flow_type = ?", flowType)
	}
	var flow Flow
	if err := q.Order("id DESC").Take(&flow).Error; err != nil {
		return Flow{}, err
	}
	flow.UnsNodeIDs, _ = r.FlowNodeIDs(ctx, flow.ID)
	return flow, nil
}

func parseFlowNameSuffix(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	return n, err == nil
}

func escapeSQLLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

func (r *FlowRepo) GetFlowFolderByName(ctx context.Context, parentID int64, name string) (Flow, error) {
	var flow Flow
	if err := r.db.WithContext(ctx).
		Where("parent_id = ? AND flow_name = ? AND node_type = 1 AND deleted_time = 0", parentID, strings.TrimSpace(name)).
		Order("id").Take(&flow).Error; err != nil {
		return Flow{}, err
	}
	flow.UnsNodeIDs, _ = r.FlowNodeIDs(ctx, flow.ID)
	return flow, nil
}

func (r *FlowRepo) GetFlowsByIDs(ctx context.Context, ids []int64) ([]Flow, error) {
	if len(ids) == 0 {
		return []Flow{}, nil
	}
	var out []Flow
	if err := r.db.WithContext(ctx).
		Where("id IN ? AND deleted_time = 0", ids).
		Order("flow_type, parent_id, sort_key, id").Find(&out).Error; err != nil {
		return nil, err
	}
	for i := range out {
		out[i].UnsNodeIDs, _ = r.FlowNodeIDs(ctx, out[i].ID)
	}
	return out, nil
}

func (r *FlowRepo) DeleteFlow(ctx context.Context, id, userID int64) error {
	now := time.Now().UTC().UnixMilli()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Flow
		if err := tx.Where("id = ? AND deleted_time = 0", id).Take(&current).Error; err != nil {
			return err
		}
		if current.NodeType == 1 {
			if err := tx.Model(&Flow{}).Where("parent_id = ? AND deleted_time = 0", id).
				Updates(touchByValues(map[string]any{"parent_id": 0}, userID, now)).Error; err != nil {
				return err
			}
		}
		res := tx.Model(&Flow{}).Where("id = ? AND deleted_time = 0", id).
			Updates(softDeleteNoDelByValues(userID, now))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := tx.Where("parent_id = ?", id).Delete(&flowNode{}).Error; err != nil {
			return err
		}
		return tx.Where("flow_id = ?", id).Delete(&FlowVersion{}).Error
	})
}

func (r *FlowRepo) SaveFlowData(ctx context.Context, id int64, data string, userID int64) (Flow, error) {
	now := time.Now().UTC().UnixMilli()
	current, err := r.GetFlow(ctx, id)
	if err != nil {
		return Flow{}, err
	}
	status := "draft"
	if strings.EqualFold(strings.TrimSpace(current.Status), "disabled") {
		status = "disabled"
	}
	var flow Flow
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&flow).Clauses(returningAll()).
			Where("id = ? AND deleted_time = 0", id).
			Updates(touchByValues(map[string]any{"flow_data": data, "flow_status": status}, userID, now))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		version, err := createFlowVersion(tx, flow.ID, fmt.Sprintf("auto-save-%d", now), FlowVersionTypeAuto, "auto save", data, "", userID, now)
		if err != nil {
			return err
		}
		if err := tx.Model(&Flow{}).Where("id = ?", flow.ID).Update("current_version_id", version.ID).Error; err != nil {
			return err
		}
		if err := pruneAutoFlowVersions(tx, flow.ID, 10); err != nil {
			return err
		}
		flow.CurrentVersionID = version.ID
		return nil
	})
	if err != nil {
		return Flow{}, normalizeDBError(err)
	}
	return flow, nil
}

func (r *FlowRepo) DeployFlow(ctx context.Context, id, userID int64) (Flow, error) {
	now := time.Now().UTC().UnixMilli()
	flow, err := r.GetFlow(ctx, id)
	if err != nil {
		return Flow{}, err
	}
	if flow.NodeType == 1 {
		return Flow{}, ErrFlowFolderCannotDeploy
	}
	var updated Flow
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := createFlowVersion(tx, flow.ID, fmt.Sprintf("auto-deploy-%d", now), FlowVersionTypeAuto, "auto deploy", flow.FlowData, "", userID, now)
		if err != nil {
			return err
		}
		res := tx.Model(&updated).Clauses(returningAll()).
			Where("id = ? AND deleted_time = 0", id).
			Updates(touchByValues(map[string]any{"flow_status": "deployed", "current_version_id": version.ID}, userID, now))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return pruneAutoFlowVersions(tx, flow.ID, 10)
	})
	if err != nil {
		return Flow{}, normalizeDBError(err)
	}
	updated.UnsNodeIDs, _ = r.FlowNodeIDs(ctx, updated.ID)
	return updated, nil
}

func (r *FlowRepo) MarkFlowDeployed(ctx context.Context, id int64, runtimeFlowID string, deployedFlowData string, status string, userID int64) (Flow, error) {
	now := time.Now().UTC().UnixMilli()
	flow, err := r.GetFlow(ctx, id)
	if err != nil {
		return Flow{}, err
	}
	if flow.NodeType == 1 {
		return Flow{}, ErrFlowFolderCannotDeploy
	}
	if strings.TrimSpace(runtimeFlowID) == "" {
		runtimeFlowID = flow.RuntimeFlowID
	}
	snapshotData := strings.TrimSpace(deployedFlowData)
	if snapshotData == "" {
		snapshotData = flow.FlowData
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "deployed"
	}
	var updated Flow
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := createFlowVersion(tx, flow.ID, fmt.Sprintf("auto-deploy-%d", now), FlowVersionTypeAuto, "auto deploy", snapshotData, "", userID, now)
		if err != nil {
			return err
		}
		values := map[string]any{
			"flow_id":            runtimeFlowID,
			"flow_status":        status,
			"current_version_id": version.ID,
		}
		if snapshotData != flow.FlowData {
			values["flow_data"] = snapshotData
		}
		res := tx.Model(&updated).Clauses(returningAll()).
			Where("id = ? AND deleted_time = 0", id).
			Updates(touchByValues(values, userID, now))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return pruneAutoFlowVersions(tx, flow.ID, 10)
	})
	if err != nil {
		return Flow{}, normalizeDBError(err)
	}
	updated.UnsNodeIDs, _ = r.FlowNodeIDs(ctx, updated.ID)
	return updated, nil
}

func (r *FlowRepo) UpdateFlowStatus(ctx context.Context, id int64, status string, userID int64) (Flow, error) {
	now := time.Now().UTC().UnixMilli()
	var updated Flow
	res := r.db.WithContext(ctx).Model(&updated).Clauses(returningAll()).
		Where("id = ? AND deleted_time = 0", id).
		Updates(touchByValues(map[string]any{"flow_status": strings.TrimSpace(status)}, userID, now))
	if res.Error != nil {
		return Flow{}, normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return Flow{}, ErrNotFound
	}
	updated.UnsNodeIDs, _ = r.FlowNodeIDs(ctx, updated.ID)
	return updated, nil
}

// RememberFlowRuntimeID preserves an allocated tab without marking deployment successful.
func (r *FlowRepo) RememberFlowRuntimeID(ctx context.Context, id int64, runtimeID string) error {
	result := r.db.WithContext(ctx).Model(&Flow{}).Where("id = ? AND deleted_time = 0", id).Update("flow_id", runtimeID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
