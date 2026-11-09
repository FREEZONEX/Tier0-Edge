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
	DefaultHomePage         = "/home"
	DefaultOperatorHomePage = "/launchpad"
)

func normalizeUserHomePage(value string) string {
	value, ok := cleanHomePage(value)
	if !ok {
		return DefaultHomePage
	}
	return value
}

func cleanHomePage(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "", false
	}
	return value, true
}

func normalizeRoleHomePage(value string, allowedHomePages []string) string {
	allowed := map[string]string{}
	ordered := make([]string, 0, len(allowedHomePages))
	for _, page := range allowedHomePages {
		page, ok := cleanHomePage(page)
		if !ok {
			continue
		}
		key := strings.ToLower(page)
		if _, exists := allowed[key]; exists {
			continue
		}
		allowed[key] = page
		ordered = append(ordered, page)
	}
	if value, ok := cleanHomePage(value); ok {
		if page, ok := allowed[strings.ToLower(value)]; ok {
			return page
		}
	}
	if page, ok := allowed[strings.ToLower(DefaultHomePage)]; ok {
		return page
	}
	if page, ok := allowed[strings.ToLower(DefaultOperatorHomePage)]; ok {
		return page
	}
	if len(ordered) > 0 {
		return ordered[0]
	}
	return DefaultOperatorHomePage
}

type UserConfig struct {
	UserID       int64     `gorm:"column:user_id;primaryKey" json:"userId"`
	HomePage     string    `gorm:"column:home_page" json:"homePage"`
	MainLanguage string    `gorm:"column:main_language" json:"mainLanguage"`
	CreatedTime  time.Time `gorm:"column:created_time" json:"createdTime"`
	UpdatedTime  time.Time `gorm:"column:updated_time" json:"updatedTime"`
}

func (UserConfig) TableName() string { return "sys_user_config" }

type UserHomePageUpdate struct {
	UserID   int64
	HomePage string
}

type UserConfigRepo struct{ db *gorm.DB }

func NewUserConfigRepo(in any) *UserConfigRepo { return &UserConfigRepo{db: GetCommonConn(in)} }

func (r *UserConfigRepo) GetUserConfig(ctx context.Context, userID int64) (UserConfig, error) {
	var item UserConfig
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Take(&item).Error
	if err == nil {
		item.HomePage = normalizeUserHomePage(item.HomePage)
		return item, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return UserConfig{UserID: userID, HomePage: DefaultHomePage}, nil
	}
	return UserConfig{}, err
}

func (r *UserConfigRepo) UpdateUserConfig(ctx context.Context, userID int64, homePage, mainLanguage *string) (UserConfig, error) {
	now := time.Now().UTC()
	row := UserConfig{
		UserID:      userID,
		HomePage:    DefaultHomePage,
		CreatedTime: now,
		UpdatedTime: now,
	}
	updates := map[string]any{"updated_time": now}
	if homePage != nil {
		value := normalizeUserHomePage(*homePage)
		row.HomePage = value
		updates["home_page"] = value
	}
	if mainLanguage != nil {
		value := strings.TrimSpace(*mainLanguage)
		row.MainLanguage = value
		updates["main_language"] = value
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&row).Error; err != nil {
		return UserConfig{}, err
	}
	return r.GetUserConfig(ctx, userID)
}

func (r *UserConfigRepo) UpdateUserHomePages(ctx context.Context, actorID int64, updates []UserHomePageUpdate) error {
	if actorID <= 0 || len(updates) == 0 {
		return ErrInvalidArgument
	}
	now := time.Now().UTC()
	userIDs := make([]int64, 0, len(updates))
	rows := make([]UserConfig, 0, len(updates))
	for _, update := range updates {
		if update.UserID <= 0 {
			return ErrInvalidArgument
		}
		userIDs = append(userIDs, update.UserID)
		rows = append(rows, UserConfig{
			UserID:      update.UserID,
			HomePage:    normalizeUserHomePage(update.HomePage),
			CreatedTime: now,
			UpdatedTime: now,
		})
	}

	return normalizeDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var users []sysUserInfo
		if err := tx.Select("user_id", "user_name").
			Where("user_id IN ? AND deleted_time = 0", userIDs).
			Find(&users).Error; err != nil {
			return err
		}
		if len(users) != len(userIDs) {
			return gorm.ErrRecordNotFound
		}
		for _, user := range users {
			if strings.EqualFold(strings.TrimSpace(user.UserName), "tier0") && user.ID != actorID {
				return ErrSystemReadonly
			}
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"home_page", "updated_time"}),
		}).Create(&rows).Error
	}))
}
