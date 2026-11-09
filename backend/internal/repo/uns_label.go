package repo

import (
	"context"
	"strings"
)

type UnsLabel struct {
	ID          int64  `gorm:"column:id;primaryKey;autoIncrement:false" json:"id"`
	LabelKey    string `gorm:"column:label_key" json:"labelKey"`
	Name        string `gorm:"column:name" json:"name"`
	Color       string `gorm:"column:color" json:"color"`
	Description string `gorm:"column:description" json:"description"`
	SoftOnlyTime
}

func (UnsLabel) TableName() string { return "uns_namespace_label_info" }

func (r *UnsRepo) ListUnsLabels(ctx context.Context) ([]UnsLabel, error) {
	var out []UnsLabel
	err := r.db.WithContext(ctx).
		Select("id, label_key, name, color, description, created_time, updated_time, deleted_time").
		Where("deleted_time = 0").Order("id").Find(&out).Error
	return out, err
}

func (r *UnsRepo) GetUnsLabel(ctx context.Context, id int64) (UnsLabel, error) {
	var item UnsLabel
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&item).Error
	return item, err
}

func (r *UnsRepo) CreateUnsLabel(ctx context.Context, item UnsLabel) (UnsLabel, error) {
	ensureID(&item.ID)
	if strings.TrimSpace(item.LabelKey) == "" {
		item.LabelKey = "label_" + randomHex(8)
	}
	err := r.db.WithContext(ctx).Create(&item).Error
	return item, normalizeDBError(err)
}

func (r *UnsRepo) UpdateUnsLabel(ctx context.Context, id int64, item UnsLabel) (UnsLabel, error) {
	var out UnsLabel
	res := r.db.WithContext(ctx).Model(&out).Clauses(returningAll()).
		Where("id = ? AND deleted_time = 0", id).
		Updates(touchValues(map[string]any{"name": item.Name, "color": item.Color, "description": item.Description}, 0))
	if res.Error != nil {
		return UnsLabel{}, normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return UnsLabel{}, ErrNotFound
	}
	return out, nil
}

func (r *UnsRepo) DeleteUnsLabel(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Model(&UnsLabel{}).
		Where("id = ? AND deleted_time = 0", id).
		Updates(softDeleteNoDelByValues(0, 0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// InsertUnsNodeLabels binds labels for newly inserted nodes in the caller's transaction.
func (r *UnsRepo) InsertUnsNodeLabels(ctx context.Context, bindings map[int64][]int64) error {
	rows := make([]unsLabelNode, 0)
	for nodeID, labelIDs := range bindings {
		seen := make(map[int64]bool, len(labelIDs))
		for _, labelID := range labelIDs {
			if labelID <= 0 || seen[labelID] {
				continue
			}
			seen[labelID] = true
			rows = append(rows, unsLabelNode{LabelID: labelID, NodeID: nodeID})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return normalizeDBError(r.db.WithContext(ctx).CreateInBatches(&rows, 500).Error)
}

// ReplaceImportedUnsNodeLabels resets only imported nodes, in the metadata transaction.
func (r *UnsRepo) ReplaceImportedUnsNodeLabels(ctx context.Context, bindings map[int64][]int64) error {
	ids := make([]int64, 0, len(bindings))
	for id := range bindings {
		ids = append(ids, id)
	}
	for start := 0; start < len(ids); start += 500 {
		if err := r.db.WithContext(ctx).Where("node_id IN ?", ids[start:min(start+500, len(ids))]).Delete(&unsLabelNode{}).Error; err != nil {
			return err
		}
	}
	return r.InsertUnsNodeLabels(ctx, bindings)
}

// ListUnsNodeLabelBindings loads label sets once for import change detection.
func (r *UnsRepo) ListUnsNodeLabelBindings(ctx context.Context) (map[int64][]int64, error) {
	var rows []unsLabelNode
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := map[int64][]int64{}
	for _, row := range rows {
		result[row.NodeID] = append(result[row.NodeID], row.LabelID)
	}
	return result, nil
}
