package repo

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserFavorite 用户收藏表模型，对应 sys_user_favorite。
// 用于 Launchpad 中 App/Project 等资源的跨端收藏持久化。
type UserFavorite struct {
	ID          int64  `gorm:"column:id;type:BIGSERIAL;primaryKey;autoIncrement" json:"id"`
	UserID      int64  `gorm:"column:user_id;type:BIGINT;not null" json:"userId"`
	ResType     string `gorm:"column:res_type;type:VARCHAR(50);not null" json:"resType"`
	ResID       string `gorm:"column:res_id;type:VARCHAR(50);not null" json:"resId"`
	ResName     string `gorm:"column:res_name;type:VARCHAR(256);not null;default:''" json:"resName"`
	CreatedTime int64  `gorm:"column:created_time;type:BIGINT;not null" json:"createdTime"`
}

func (UserFavorite) TableName() string { return "sys_user_favorite" }

// UserFavoriteFilter 收藏查询过滤条件。
type UserFavoriteFilter struct {
	UserID  int64
	ResType string
}

// UserFavoriteRepo 用户收藏数据访问层。
type UserFavoriteRepo struct{ db *gorm.DB }

// NewUserFavoriteRepo 创建 UserFavoriteRepo 实例，in 可为 *Store、*gorm.DB 或 context.Context。
func NewUserFavoriteRepo(in any) *UserFavoriteRepo {
	return &UserFavoriteRepo{db: GetCommonConn(in)}
}

// List 按用户与资源类型查询收藏列表。
func (r *UserFavoriteRepo) List(ctx context.Context, f UserFavoriteFilter) ([]UserFavorite, error) {
	db := r.db.WithContext(ctx).Model(&UserFavorite{})
	if f.UserID > 0 {
		db = db.Where("user_id = ?", f.UserID)
	}
	if f.ResType != "" {
		db = db.Where("res_type = ?", f.ResType)
	}
	var out []UserFavorite
	err := db.Order("created_time DESC, id DESC").Find(&out).Error
	return out, err
}

// BatchInsert 批量新增收藏，已存在时静默忽略（由唯一索引保证）。
func (r *UserFavoriteRepo) BatchInsert(ctx context.Context, userID int64, items []UserFavorite) error {
	if userID <= 0 || len(items) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	rows := make([]map[string]any, 0, len(items))
	for _, it := range items {
		resType := strings.TrimSpace(it.ResType)
		resID := strings.TrimSpace(it.ResID)
		if resType == "" || resID == "" {
			continue
		}
		rows = append(rows, map[string]any{
			"user_id":      userID,
			"res_type":     resType,
			"res_id":       resID,
			"res_name":     strings.TrimSpace(it.ResName),
			"created_time": now,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Table(UserFavorite{}.TableName()).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "res_type"}, {Name: "res_id"}},
			DoNothing: true,
		}).
		Create(rows).Error
}

// BatchDelete 批量删除收藏。
func (r *UserFavoriteRepo) BatchDelete(ctx context.Context, userID int64, items []UserFavorite) error {
	if userID <= 0 || len(items) == 0 {
		return nil
	}
	resTypes := make([]string, 0, len(items))
	resIDs := make([]string, 0, len(items))
	for _, it := range items {
		resType := strings.TrimSpace(it.ResType)
		resID := strings.TrimSpace(it.ResID)
		if resType == "" || resID == "" {
			continue
		}
		resTypes = append(resTypes, resType)
		resIDs = append(resIDs, resID)
	}
	if len(resIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Where("res_type IN ?", resTypes).
		Where("res_id IN ?", resIDs).
		Delete(&UserFavorite{}).Error
}

// ExistsMap 批量判断指定资源是否已被收藏，返回 resId -> true 的映射。
func (r *UserFavoriteRepo) ExistsMap(ctx context.Context, userID int64, resType string, resIDs []string) (map[string]bool, error) {
	out := make(map[string]bool)
	if userID <= 0 || resType == "" || len(resIDs) == 0 {
		return out, nil
	}
	var rows []UserFavorite
	err := r.db.WithContext(ctx).Model(&UserFavorite{}).
		Where("user_id = ? AND res_type = ? AND res_id IN ?", userID, resType, resIDs).
		Find(&rows).Error
	for _, row := range rows {
		out[row.ResID] = true
	}
	return out, err
}
