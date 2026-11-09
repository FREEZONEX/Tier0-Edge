package repo

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UnsNode struct {
	ID                int64              `gorm:"column:id;primaryKey;autoIncrement:false" json:"id"`
	IDPath            string             `gorm:"column:id_path;type:varchar(1024);not null;default:'';index" json:"idPath"`
	ParentID          int64              `gorm:"column:parent_id" json:"parentId"`
	Name              string             `gorm:"column:name" json:"name"`
	DisplayName       string             `gorm:"column:display_name" json:"displayName"`
	Description       string             `gorm:"column:description" json:"description"`
	Namespace         string             `gorm:"column:namespace" json:"namespace"`
	Alias             string             `gorm:"column:alias" json:"alias"`
	Type              int16              `gorm:"column:type" json:"type"`
	TopicType         int16              `gorm:"column:topic_type" json:"topicType"`
	SortKey           int64              `gorm:"column:sort_key;default:0;index" json:"sortKey"`
	Schema            json.RawMessage    `gorm:"column:schema;type:jsonb" json:"schema"`
	ExtendProperties  json.RawMessage    `gorm:"column:extendproperties;type:jsonb" json:"extendProperties"`
	DataQualityPolicy *DataQualityPolicy `gorm:"column:data_quality_policy;type:jsonb;serializer:json" json:"dataQualityPolicy,omitempty"`
	EnableHistory     int16              `gorm:"column:enable_history;default:2" json:"enableHistory"`
	MockData          int16              `gorm:"column:mock_data;default:2" json:"mockData"`
	TemplateID        *int64             `gorm:"column:template_id;default:null" json:"templateId,omitempty"`
	IsFavorite        int16              `gorm:"column:is_favorite;default:2" json:"isFavorite"`
	Status            string             `gorm:"column:status" json:"status"`
	RecycleIsDel      int16              `gorm:"column:recycle_is_del" json:"recycleIsDel"`
	HasChildren       bool               `gorm:"-" json:"hasChildren"`
	CountChildren     int64              `gorm:"-" json:"countChildren"`
	SoftTime
}

const (
	UnsNodeStatusActive  = "active"
	UnsNodeStatusDeleted = "deleted"
	UnsNodeStatusFinal   = "finalDeleted"

	UnsNodeRecycleHidden  = int16(1)
	UnsNodeRecycleVisible = int16(2)
)

func (UnsNode) TableName() string { return "uns_namespace_node_info" }

type UnsNodeFilter struct {
	ParentID       int64
	ParentIDSet    bool
	Keyword        string
	IncludeRecycle bool
	RecycleOnly    bool
}

type UnsRepo struct{ db *gorm.DB }

func NewUnsRepo(in any) *UnsRepo { return &UnsRepo{db: GetCommonConn(in)} }

// ListActiveUnsNodesRaw returns the active UNS metadata without child-count
// annotations. Batch create callers already build their own in-memory indexes,
// so the additional aggregate query in ListUnsNodes would be wasted work.
func (r *UnsRepo) ListActiveUnsNodesRaw(ctx context.Context) ([]UnsNode, error) {
	var out []UnsNode
	err := r.db.WithContext(ctx).Model(&UnsNode{}).
		Select("id, id_path, parent_id, name, namespace, alias, type, topic_type, sort_key").
		Where("deleted_time = 0").
		Order("parent_id, sort_key, id").
		Find(&out).Error
	return out, err
}

// InsertUnsNodes inserts prevalidated nodes in batches. The caller owns the
// transaction so it can keep the metadata snapshot and all inserts atomic.
func (r *UnsRepo) InsertUnsNodes(ctx context.Context, nodes []UnsNode) error {
	if len(nodes) == 0 {
		return nil
	}
	for i := range nodes {
		ensureID(&nodes[i].ID)
		if len(nodes[i].Schema) == 0 {
			nodes[i].Schema = json.RawMessage(`{}`)
		}
		if len(nodes[i].ExtendProperties) == 0 {
			nodes[i].ExtendProperties = json.RawMessage(`{}`)
		}
		normalizeUnsNodeDefaults(&nodes[i])
		nodes[i].Status = "active"
		nodes[i].RecycleIsDel = 0
	}
	return normalizeDBError(r.db.WithContext(ctx).CreateInBatches(&nodes, 500).Error)
}

func (r *UnsRepo) CreateUnsNode(ctx context.Context, node UnsNode, labelIDs []int64, assetIDs []int64) (UnsNode, error) {
	now := time.Now().UTC().UnixMilli()
	ensureID(&node.ID)
	if len(node.Schema) == 0 {
		node.Schema = json.RawMessage(`{}`)
	}
	if len(node.ExtendProperties) == 0 {
		node.ExtendProperties = json.RawMessage(`{}`)
	}
	normalizeUnsNodeDefaults(&node)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if node.Type == 2 {
			if node.TopicType == 0 {
				return ErrInvalidUnsNode
			}
			if err := ensureUnsFileTopicParent(tx, &node, now); err != nil {
				return err
			}
		} else if node.ParentID != 0 {
			if err := validateUnsFolderParent(tx, node.ParentID); err != nil {
				return err
			}
		}
		if err := ensureUnsSiblingNameAvailable(tx, node.ParentID, node.Name, 0); err != nil {
			return err
		}
		namespace := strings.Trim(node.Namespace, "/")
		if namespace == "" {
			built, err := buildUnsNamespace(tx, node.ParentID, node.Name)
			if err != nil {
				return err
			}
			namespace = built
		}
		node.Namespace = namespace
		if err := fillUnsNodePathFields(tx, &node); err != nil {
			return err
		}
		node.Status = "active"
		node.RecycleIsDel = 0
		if err := tx.Create(&node).Error; err != nil {
			return err
		}
		if err := replaceUnsNodeLabels(tx, node.ID, labelIDs, now); err != nil {
			return err
		}
		return bindAssetIDs(tx, assetIDs, "unsNode", node.ID, node.CreatedBy, now)
	})
	if err != nil {
		return UnsNode{}, normalizeDBError(err)
	}
	return node, nil
}

func (r *UnsRepo) UpdateUnsNode(ctx context.Context, node UnsNode, labelIDs []int64, assetIDs []int64) (UnsNode, error) {
	now := time.Now().UTC().UnixMilli()
	if len(node.Schema) == 0 {
		node.Schema = json.RawMessage(`{}`)
	}
	if len(node.ExtendProperties) == 0 {
		node.ExtendProperties = json.RawMessage(`{}`)
	}
	normalizeUnsNodeDefaults(&node)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := loadUnsNode(tx, node.ID, true)
		if err != nil {
			return err
		}
		if current.DeletedTime != 0 {
			return ErrNotFound
		}
		if err := ensureUnsSiblingNameAvailable(tx, node.ParentID, node.Name, node.ID); err != nil {
			return err
		}
		namespace := strings.Trim(node.Namespace, "/")
		if namespace == "" {
			built, err := buildUnsNamespace(tx, node.ParentID, node.Name)
			if err != nil {
				return err
			}
			namespace = built
		}
		node.Namespace = namespace
		if err := fillUnsNodePathFields(tx, &node); err != nil {
			return err
		}
		values := touchByValues(map[string]any{
			"id_path":          node.IDPath,
			"parent_id":        node.ParentID,
			"name":             node.Name,
			"display_name":     node.DisplayName,
			"description":      node.Description,
			"namespace":        node.Namespace,
			"alias":            node.Alias,
			"type":             node.Type,
			"topic_type":       node.TopicType,
			"sort_key":         node.SortKey,
			"schema":           node.Schema,
			"extendproperties": node.ExtendProperties,
			"enable_history":   node.EnableHistory,
			"mock_data":        node.MockData,
			"is_favorite":      node.IsFavorite,
		}, node.UpdatedBy, now)
		if node.TemplateID != nil {
			values["template_id"] = node.TemplateID
		}
		res := tx.Model(&node).Clauses(returningAll()).
			Where("id = ? AND deleted_time = 0", node.ID).
			Updates(values)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		if current.Namespace != node.Namespace {
			oldPrefix := current.Namespace + "/"
			newPrefix := node.Namespace + "/"
			if err := tx.Exec(`
UPDATE uns_namespace_node_info
SET namespace=? || substring(namespace from ?::int), updated_by=?, updated_time=?
WHERE deleted_time=0 AND namespace LIKE ?`,
				newPrefix, len(oldPrefix)+1, node.UpdatedBy, repoTimeFromMilli(now), oldPrefix+"%").Error; err != nil {
				return err
			}
		}
		if current.IDPath != "" && current.IDPath != node.IDPath {
			if err := updateUnsIDPathPrefix(tx, current.IDPath, node.IDPath, node.UpdatedBy, now); err != nil {
				return err
			}
		}
		if err := replaceUnsNodeLabels(tx, node.ID, labelIDs, now); err != nil {
			return err
		}
		if len(assetIDs) > 0 {
			if err := bindAssetIDs(tx, assetIDs, "unsNode", node.ID, node.UpdatedBy, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return UnsNode{}, normalizeDBError(err)
	}
	return node, nil
}

func (r *UnsRepo) UpdateUnsNodeHistory(ctx context.Context, id int64, enableHistory int16, userID int64) (UnsNode, error) {
	now := time.Now().UTC().UnixMilli()
	var node UnsNode
	res := r.db.WithContext(ctx).Model(&node).Clauses(returningAll()).
		Where("id = ? AND deleted_time = 0", id).
		Updates(touchByValues(map[string]any{"enable_history": enableHistory}, userID, now))
	if res.Error != nil {
		return UnsNode{}, normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return UnsNode{}, ErrNotFound
	}
	return node, nil
}

func (r *UnsRepo) ListUnsNodes(ctx context.Context, filter UnsNodeFilter) ([]UnsNode, error) {
	if filter.RecycleOnly {
		out, err := r.listUnsRecycleTreeNodes(ctx, filter)
		if err != nil {
			return nil, err
		}
		annotateUnsRecycleTreeChildren(out)
		return out, nil
	}

	q := r.db.WithContext(ctx).Model(&UnsNode{})
	if filter.IncludeRecycle {
		q = q.Unscoped().
			Where("deleted_time = 0 OR (deleted_time > 0 AND recycle_is_del = ?)", UnsNodeRecycleVisible)
	} else {
		q = q.Where("deleted_time = 0")
	}
	if filter.ParentIDSet {
		q = q.Where("parent_id = ?", filter.ParentID)
	}
	if kw := strings.TrimSpace(filter.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("(name ILIKE ? OR display_name ILIKE ? OR namespace ILIKE ?)", like, like, like)
	}
	var out []UnsNode
	if err := q.Order("parent_id, sort_key, id").Find(&out).Error; err != nil {
		return nil, err
	}
	if strings.TrimSpace(filter.Keyword) != "" && !filter.ParentIDSet && !filter.RecycleOnly {
		var err error
		out, err = r.includeUnsSearchAncestors(ctx, out, filter.IncludeRecycle)
		if err != nil {
			return nil, err
		}
	}
	if err := r.annotateUnsNodeChildren(ctx, out, filter.IncludeRecycle); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *UnsRepo) CountActiveUnsNodes(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&UnsNode{}).Where("deleted_time = 0").Count(&count).Error
	return count, err
}

func (r *UnsRepo) listUnsRecycleTreeNodes(ctx context.Context, filter UnsNodeFilter) ([]UnsNode, error) {
	q := r.db.WithContext(ctx).Unscoped().Model(&UnsNode{}).
		Where("deleted_time > 0 AND recycle_is_del = 2")
	if filter.ParentIDSet {
		q = q.Where("parent_id = ?", filter.ParentID)
	}
	if kw := strings.TrimSpace(filter.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("(name ILIKE ? OR display_name ILIKE ? OR namespace ILIKE ?)", like, like, like)
	}
	var out []UnsNode
	if err := q.Order("parent_id, sort_key, id").Find(&out).Error; err != nil {
		return nil, err
	}
	return r.includeUnsRecycleAncestors(ctx, out)
}

func (r *UnsRepo) includeUnsRecycleAncestors(ctx context.Context, nodes []UnsNode) ([]UnsNode, error) {
	if len(nodes) == 0 {
		return nodes, nil
	}
	seen := make(map[int64]UnsNode, len(nodes))
	nextParents := make(map[int64]struct{})
	for _, node := range nodes {
		seen[node.ID] = node
		if node.ParentID != 0 {
			nextParents[node.ParentID] = struct{}{}
		}
	}
	for len(nextParents) > 0 {
		ids := make([]int64, 0, len(nextParents))
		for id := range nextParents {
			if _, ok := seen[id]; !ok {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			break
		}
		var parents []UnsNode
		if err := r.db.WithContext(ctx).Unscoped().Model(&UnsNode{}).
			Where("id IN ?", ids).
			Where("deleted_time = 0 OR (deleted_time > 0 AND recycle_is_del = 2)").
			Find(&parents).Error; err != nil {
			return nil, err
		}
		nextParents = map[int64]struct{}{}
		for _, parent := range parents {
			if _, ok := seen[parent.ID]; ok {
				continue
			}
			seen[parent.ID] = parent
			if parent.ParentID != 0 {
				nextParents[parent.ParentID] = struct{}{}
			}
		}
	}
	out := make([]UnsNode, 0, len(seen))
	for _, node := range seen {
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ParentID != out[j].ParentID {
			return out[i].ParentID < out[j].ParentID
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func annotateUnsRecycleTreeChildren(nodes []UnsNode) {
	if len(nodes) == 0 {
		return
	}
	indexByID := make(map[int64]int, len(nodes))
	for i := range nodes {
		indexByID[nodes[i].ID] = i
	}
	childrenByParent := make(map[int64][]int)
	for i := range nodes {
		if _, ok := indexByID[nodes[i].ParentID]; ok {
			childrenByParent[nodes[i].ParentID] = append(childrenByParent[nodes[i].ParentID], i)
		}
	}
	memo := make(map[int64]int64, len(nodes))
	var countRecycleFiles func(id int64) int64
	countRecycleFiles = func(id int64) int64 {
		if count, ok := memo[id]; ok {
			return count
		}
		var total int64
		for _, childIndex := range childrenByParent[id] {
			child := nodes[childIndex]
			if child.Type == 2 && child.DeletedTime > 0 && child.RecycleIsDel == 2 {
				total++
			}
			total += countRecycleFiles(child.ID)
		}
		memo[id] = total
		return total
	}
	for i := range nodes {
		nodes[i].HasChildren = len(childrenByParent[nodes[i].ID]) > 0
		nodes[i].CountChildren = countRecycleFiles(nodes[i].ID)
	}
}

func (r *UnsRepo) includeUnsSearchAncestors(ctx context.Context, nodes []UnsNode, includeRecycle bool) ([]UnsNode, error) {
	if len(nodes) == 0 {
		return nodes, nil
	}
	seen := make(map[int64]UnsNode, len(nodes))
	nextParents := make(map[int64]struct{})
	for _, node := range nodes {
		seen[node.ID] = node
		if node.ParentID != 0 {
			nextParents[node.ParentID] = struct{}{}
		}
	}
	for len(nextParents) > 0 {
		ids := make([]int64, 0, len(nextParents))
		for id := range nextParents {
			if _, ok := seen[id]; !ok {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			break
		}
		q := r.db.WithContext(ctx).Model(&UnsNode{}).Where("id IN ?", ids)
		if includeRecycle {
			q = q.Unscoped().
				Where("deleted_time = 0 OR (deleted_time > 0 AND recycle_is_del = ?)", UnsNodeRecycleVisible)
		} else {
			q = q.Where("deleted_time = 0")
		}
		var parents []UnsNode
		if err := q.Find(&parents).Error; err != nil {
			return nil, err
		}
		nextParents = map[int64]struct{}{}
		for _, parent := range parents {
			if _, ok := seen[parent.ID]; ok {
				continue
			}
			seen[parent.ID] = parent
			if parent.ParentID != 0 {
				nextParents[parent.ParentID] = struct{}{}
			}
		}
	}
	out := make([]UnsNode, 0, len(seen))
	for _, node := range seen {
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ParentID != out[j].ParentID {
			return out[i].ParentID < out[j].ParentID
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (r *UnsRepo) annotateUnsNodeChildren(ctx context.Context, nodes []UnsNode, includeRecycle bool) error {
	if len(nodes) == 0 {
		return nil
	}
	q := r.db.WithContext(ctx).Table("uns_namespace_node_info").Select("id, parent_id, type")
	if includeRecycle {
		q = q.Where("deleted_time = 0 OR (deleted_time > 0 AND recycle_is_del = ?)", UnsNodeRecycleVisible)
	} else {
		q = q.Where("deleted_time = 0")
	}
	type childInfo struct {
		ID       int64 `gorm:"column:id"`
		ParentID int64 `gorm:"column:parent_id"`
		Type     int16 `gorm:"column:type"`
	}
	var rows []childInfo
	if err := q.Scan(&rows).Error; err != nil {
		return err
	}
	directCounts := make(map[int64]int64, len(rows))
	childrenByParent := make(map[int64][]childInfo, len(rows))
	for _, row := range rows {
		directCounts[row.ParentID]++
		childrenByParent[row.ParentID] = append(childrenByParent[row.ParentID], row)
	}
	memo := make(map[int64]int64, len(rows))
	var countFiles func(id int64) int64
	countFiles = func(id int64) int64 {
		if count, ok := memo[id]; ok {
			return count
		}
		var total int64
		for _, child := range childrenByParent[id] {
			if child.Type == 2 {
				total++
			}
			total += countFiles(child.ID)
		}
		memo[id] = total
		return total
	}
	for i := range nodes {
		nodes[i].CountChildren = countFiles(nodes[i].ID)
		nodes[i].HasChildren = directCounts[nodes[i].ID] > 0
	}
	return nil
}

func (r *UnsRepo) GetUnsNode(ctx context.Context, id int64, includeDeleted bool) (UnsNode, error) {
	return loadUnsNode(r.db.WithContext(ctx), id, includeDeleted)
}

// GetUnsNodeForUpdate serializes background flow creation for the same UNS.
// The caller must supply a transaction when constructing the repo.
func (r *UnsRepo) GetUnsNodeForUpdate(ctx context.Context, id int64) (UnsNode, error) {
	return loadUnsNode(r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}), id, false)
}

func (r *UnsRepo) GetUnsNodeByNamespace(ctx context.Context, namespace string) (UnsNode, error) {
	var node UnsNode
	err := r.db.WithContext(ctx).Where("namespace = ? AND deleted_time = 0", strings.Trim(namespace, "/")).Take(&node).Error
	return node, err
}

func (r *UnsRepo) GetUnsNodeByNamespaceIncludeDeleted(ctx context.Context, namespace string) (UnsNode, error) {
	var node UnsNode
	err := r.db.WithContext(ctx).Unscoped().
		Where("namespace = ?", strings.Trim(namespace, "/")).
		Order("deleted_time = 0 DESC, id").
		Take(&node).Error
	return node, err
}

func (r *UnsRepo) GetUnsNodeByAlias(ctx context.Context, alias string) (UnsNode, error) {
	var node UnsNode
	err := r.db.WithContext(ctx).Where("alias = ? AND deleted_time = 0", strings.Trim(alias, "/")).Take(&node).Error
	return node, err
}

func (r *UnsRepo) GetUnsNodeByAliasIncludeDeleted(ctx context.Context, alias string) (UnsNode, error) {
	var node UnsNode
	err := r.db.WithContext(ctx).Unscoped().
		Where("alias = ?", strings.Trim(alias, "/")).
		Order("deleted_time = 0 DESC, id").
		Take(&node).Error
	return node, err
}

func (r *UnsRepo) GetUnsNodesByIDs(ctx context.Context, ids []int64) ([]UnsNode, error) {
	if len(ids) == 0 {
		return []UnsNode{}, nil
	}
	var out []UnsNode
	if err := r.db.WithContext(ctx).
		Where("id IN ? AND deleted_time = 0", ids).
		Order("parent_id, sort_key, id").Find(&out).Error; err != nil {
		return nil, err
	}
	if err := r.annotateUnsNodeChildren(ctx, out, false); err != nil {
		return nil, err
	}
	return out, nil
}

// GetActiveUnsNodesByRefs resolves OpenAPI topic references in one query.
// Callers preserve the public lookup priority (id, namespace, then alias).
func (r *UnsRepo) GetActiveUnsNodesByRefs(ctx context.Context, ids []int64, namespaces, aliases []string) ([]UnsNode, error) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if len(ids) > 0 {
		clauses = append(clauses, "id IN ?")
		args = append(args, ids)
	}
	if len(namespaces) > 0 {
		clauses = append(clauses, "namespace IN ?")
		args = append(args, namespaces)
	}
	if len(aliases) > 0 {
		clauses = append(clauses, "alias IN ?")
		args = append(args, aliases)
	}
	if len(clauses) == 0 {
		return []UnsNode{}, nil
	}
	var out []UnsNode
	err := r.db.WithContext(ctx).
		Where("deleted_time = 0 AND ("+strings.Join(clauses, " OR ")+")", args...).
		Find(&out).Error
	return out, err
}

func (r *UnsRepo) ListUnsSubtree(ctx context.Context, rootID int64) ([]UnsNode, error) {
	root, err := r.GetUnsNode(ctx, rootID, false)
	if err != nil {
		return nil, err
	}
	out, err := listUnsSubtree(r.db.WithContext(ctx), root.Namespace, false)
	if err != nil {
		return nil, err
	}
	if err := r.annotateUnsNodeChildren(ctx, out, false); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *UnsRepo) DeleteUnsNode(ctx context.Context, id, userID int64) ([]UnsNode, error) {
	now := time.Now().UTC().UnixMilli()
	var targets []UnsNode
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		root, err := loadUnsNode(tx, id, false)
		if err != nil {
			return err
		}
		targets, err = listUnsSubtree(tx, root.Namespace, false)
		if err != nil {
			return err
		}
		return tx.Exec(`
UPDATE uns_namespace_node_info
SET deleted_time=?, deleted_by=?, updated_by=?, updated_time=?, status='deleted', recycle_is_del=2
WHERE deleted_time=0 AND (id=? OR namespace LIKE ?)`, now, userID, userID, repoTimeFromMilli(now), id, root.Namespace+"/%").Error
	})
	if err != nil {
		return nil, normalizeDBError(err)
	}
	return targets, nil
}

func (r *UnsRepo) MarkUnsNodesDeletedOutsideRecycle(ctx context.Context, ids []int64, userID int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().UnixMilli()
	return r.db.WithContext(ctx).
		Model(&UnsNode{}).
		Where("id IN ? AND deleted_time = 0", ids).
		Updates(touchByValues(map[string]any{
			"deleted_time":   repoDeleteTimeFromMilli(now),
			"recycle_is_del": UnsNodeRecycleHidden,
			"status":         UnsNodeStatusFinal,
		}, userID, now)).Error
}

func cleanUnsNamespace(value string) string {
	return strings.Trim(strings.TrimSpace(value), "/")
}

func (r *UnsRepo) RestoreUnsNode(ctx context.Context, id, userID int64, confirm bool) ([]UnsNode, error) {
	now := time.Now().UTC().UnixMilli()
	var targets []UnsNode
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		root, err := loadUnsNode(tx, id, true)
		if err != nil {
			return err
		}
		stepIDs, err := recycleRestorePlan(tx, root)
		if err != nil {
			return err
		}
		for _, nodeID := range stepIDs {
			restored, err := restoreUnsRecycleNode(tx, nodeID, userID, now, confirm)
			if err != nil {
				return err
			}
			targets = append(targets, restored...)
		}
		return nil
	})
	if err != nil {
		return nil, normalizeDBError(err)
	}
	return targets, nil
}

func restoreUnsRecycleNode(tx *gorm.DB, id, userID, now int64, confirm bool) ([]UnsNode, error) {
	node, err := loadUnsNode(tx, id, true)
	if err != nil {
		return nil, err
	}
	if node.DeletedTime == 0 || node.RecycleIsDel != 2 {
		return nil, nil
	}
	if node.Type == 2 {
		if err := ensureRestoreFileTopicParent(tx, &node, userID, now); err != nil {
			return nil, err
		}
		node, err = loadUnsNode(tx, id, true)
		if err != nil {
			return nil, err
		}
		if node.DeletedTime == 0 || node.RecycleIsDel != 2 {
			return nil, nil
		}
	}
	namespace, err := buildUnsNamespaceForParent(tx, node.ParentID, node.Name)
	if err != nil {
		return nil, err
	}
	idPath, err := buildUnsIDPath(tx, node.ParentID, node.ID)
	if err != nil {
		return nil, err
	}
	active, err := findActiveUnsNodeByNamespace(tx, namespace)
	if err != nil {
		return nil, err
	}
	if active != nil {
		if isReservedUnsEnumFolder(node) {
			if err := mergeUnsReservedEnumFolder(tx, node, *active, userID, now); err != nil {
				return nil, err
			}
			return []UnsNode{*active}, nil
		}
		if !confirm {
			return nil, ErrUnsRestoreConflict
		}
		oldName := node.Name
		namespace = availableNamespace(tx, namespace)
		node.Name = lastNamespacePart(namespace)
		if strings.TrimSpace(node.DisplayName) == "" || node.DisplayName == oldName {
			node.DisplayName = node.Name
		}
	}
	values := touchByValues(map[string]any{
		"parent_id":      node.ParentID,
		"id_path":        idPath,
		"name":           node.Name,
		"namespace":      namespace,
		"deleted_time":   0,
		"deleted_by":     0,
		"status":         "active",
		"recycle_is_del": 0,
	}, userID, now)
	if strings.TrimSpace(node.DisplayName) == "" {
		values["display_name"] = node.Name
	} else {
		values["display_name"] = node.DisplayName
	}
	if err := tx.Unscoped().Model(&UnsNode{}).
		Where("id = ? AND deleted_time > 0 AND recycle_is_del = 2", node.ID).
		Updates(values).Error; err != nil {
		return nil, err
	}
	node.Namespace = namespace
	node.IDPath = idPath
	node.DeletedTime = 0
	node.DeletedBy = 0
	node.Status = "active"
	node.RecycleIsDel = 0
	node.UpdatedBy = userID
	node.UpdatedTime = repoTimeFromMilli(now)
	if strings.TrimSpace(node.DisplayName) == "" {
		node.DisplayName = node.Name
	}
	return []UnsNode{node}, nil
}

func findActiveUnsNodeByNamespace(db *gorm.DB, namespace string) (*UnsNode, error) {
	var node UnsNode
	err := db.Where("namespace = ? AND deleted_time = 0", namespace).Order("id").Take(&node).Error
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &node, nil
}

func mergeUnsReservedEnumFolder(tx *gorm.DB, deleted, active UnsNode, userID, now int64) error {
	if deleted.IDPath != "" && active.IDPath != "" {
		if err := updateUnsIDPathPrefix(tx, deleted.IDPath, active.IDPath, userID, now); err != nil {
			return err
		}
	}
	if err := tx.Unscoped().Model(&UnsNode{}).
		Where("parent_id = ? AND deleted_time > 0 AND recycle_is_del = 2", deleted.ID).
		Updates(touchByValues(map[string]any{"parent_id": active.ID}, userID, now)).Error; err != nil {
		return err
	}
	return tx.Unscoped().Model(&UnsNode{}).
		Where("id = ? AND deleted_time > 0 AND recycle_is_del = 2", deleted.ID).
		Updates(touchByValues(map[string]any{"status": "finalDeleted", "recycle_is_del": 1}, userID, now)).Error
}

func (r *UnsRepo) ForceDeleteUnsNode(ctx context.Context, id, userID int64) ([]UnsNode, error) {
	now := time.Now().UTC().UnixMilli()
	var targets []UnsNode
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		root, err := loadUnsNode(tx, id, true)
		if err != nil {
			return err
		}
		if root.DeletedTime == 0 {
			return ErrUnsNotInRecycle
		}
		if root.RecycleIsDel != 2 {
			return ErrUnsNotInRecycle
		}
		targets, err = listUnsRecycleSubtreeByParent(tx, root)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return ErrUnsNotInRecycle
		}
		ids := make([]int64, 0, len(targets))
		for _, target := range targets {
			ids = append(ids, target.ID)
		}
		if err := tx.Unscoped().Model(&UnsNode{}).Where("id IN ?", ids).
			Updates(touchByValues(map[string]any{
				"status":         "finalDeleted",
				"recycle_is_del": 1,
				"deleted_by":     userID,
			}, userID, now)).Error; err != nil {
			return err
		}
		if err := tx.Where("node_id IN ?", ids).Delete(&unsLabelNode{}).Error; err != nil {
			return err
		}
		return tx.Model(&AssetBinding{}).
			Where("owner_type = 'unsNode' AND owner_id IN ? AND deleted_time = 0", ids).
			Update("deleted_time", now).Error
	})
	if err != nil {
		return nil, err
	}
	return targets, nil
}

func unsTopicTypeName(value int16) string {
	switch value {
	case 1:
		return "State"
	case 2:
		return "Action"
	case 3:
		return "Metric"
	default:
		return ""
	}
}

// ListUnsNodesForImport locks the full metadata snapshot until the import commits.
// Updates must preserve node identity and metadata not represented in import files.
func (r *UnsRepo) ListUnsNodesForImport(ctx context.Context) ([]UnsNode, error) {
	var nodes []UnsNode
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("deleted_time = 0").Order("id").Find(&nodes).Error
	return nodes, err
}

// ImportedUnsNodeUpdate contains only changed, import-owned configuration fields.
type ImportedUnsNodeUpdate struct {
	Node    UnsNode
	Columns []string
}

// UpdateImportedUnsNodes groups equal column sets for bounded batch updates.
// The caller holds row locks from ListUnsNodesForImport and owns the transaction.
func (r *UnsRepo) UpdateImportedUnsNodes(ctx context.Context, updates []ImportedUnsNodeUpdate) error {
	groups := map[string][]UnsNode{}
	allowed := map[string]bool{"description": true, "schema": true, "extendproperties": true, "enable_history": true}
	for _, update := range updates {
		if len(update.Columns) == 0 {
			continue
		}
		columns := append([]string{}, update.Columns...)
		sort.Strings(columns)
		for _, column := range columns {
			if !allowed[column] {
				return ErrInvalidArgument
			}
		}
		key := strings.Join(columns, ",")
		groups[key] = append(groups[key], update.Node)
	}
	for key, nodes := range groups {
		columns := append(strings.Split(key, ","), "updated_by", "updated_time")
		if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns(columns),
		}).CreateInBatches(&nodes, 500).Error; err != nil {
			return normalizeDBError(err)
		}
	}
	return nil
}
