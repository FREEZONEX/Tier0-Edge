package repo

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

type UserLoginLog struct {
	ID            int64     `gorm:"column:id;type:BIGSERIAL;primaryKey;autoIncrement" json:"id"`
	UserID        int64     `gorm:"column:user_id;type:BIGINT;not null" json:"userId"`
	UserName      string    `gorm:"column:user_name;type:VARCHAR(100)" json:"userName"`
	IpAddr        string    `gorm:"column:ip_addr;type:VARCHAR(64)" json:"ipAddr"`
	LoginLocation string    `gorm:"column:login_location;type:VARCHAR(100)" json:"loginLocation"`
	Browser       string    `gorm:"column:browser;type:VARCHAR(50)" json:"browser"`
	Os            string    `gorm:"column:os;type:VARCHAR(50)" json:"os"`
	EventType     string    `gorm:"column:event_type;type:VARCHAR(20);not null;default:'login'" json:"eventType"`
	Code          int64     `gorm:"column:code;type:BIGINT;not null;default:200" json:"code"`
	Msg           string    `gorm:"column:msg;type:VARCHAR(255)" json:"msg"`
	CreatedTime   time.Time `gorm:"column:created_time;type:TIMESTAMPTZ;index;not null;default:CURRENT_TIMESTAMP" json:"-"`

	CreatedAt int64 `gorm:"-" json:"createdAt"`
}

func (UserLoginLog) TableName() string { return "sys_user_login_log" }

const (
	UserLoginEventLogin  = "login"
	UserLoginEventLogout = "logout"
)

type UserLoginLogFilter struct {
	PageNo    int
	PageSize  int
	Keyword   string
	UserName  string
	IpAddr    string
	EventType string
	Code      *int64
	StartTime int64
	EndTime   int64
}

const userLoginLogSelect = "id, user_id, user_name, ip_addr, login_location, browser, os, event_type, code, msg, created_time"

func (r *UserLoginLogRepo) ListUserLoginLogs(ctx context.Context, filter UserLoginLogFilter) ([]UserLoginLog, int64, error) {
	pageNo := filter.PageNo
	if pageNo <= 0 {
		pageNo = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	var total int64
	if err := applyUserLoginLogFilter(r.db.WithContext(ctx).Model(&UserLoginLog{}), filter).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var out []UserLoginLog
	q := applyUserLoginLogFilter(r.db.WithContext(ctx).Model(&UserLoginLog{}).Select(userLoginLogSelect), filter)
	err := q.Order("created_time DESC, id DESC").Limit(pageSize).Offset((pageNo - 1) * pageSize).Find(&out).Error
	if err != nil {
		return nil, 0, err
	}
	normalizeUserLoginLogs(out)
	return out, total, nil
}

func (r *UserLoginLogRepo) InsertUserLoginLog(ctx context.Context, item UserLoginLog) error {
	if item.CreatedTime.IsZero() {
		item.CreatedTime = time.Now().UTC()
	}
	if item.Code == 0 {
		item.Code = 200
	}
	if item.EventType == "" {
		item.EventType = "login"
	}
	return r.db.WithContext(ctx).Table(UserLoginLog{}.TableName()).Create(map[string]any{
		"user_id":        item.UserID,
		"user_name":      truncateUserLoginField(item.UserName, 100),
		"ip_addr":        truncateUserLoginField(item.IpAddr, 64),
		"login_location": truncateUserLoginField(item.LoginLocation, 100),
		"browser":        truncateUserLoginField(item.Browser, 50),
		"os":             truncateUserLoginField(item.Os, 50),
		"event_type":     item.EventType,
		"code":           item.Code,
		"msg":            truncateUserLoginField(item.Msg, 255),
		"created_time":   item.CreatedTime,
	}).Error
}

func applyUserLoginLogFilter(db *gorm.DB, f UserLoginLogFilter) *gorm.DB {
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + escapeSQLLike(kw) + "%"
		db = db.Where("(user_name ILIKE ? OR ip_addr ILIKE ?)", like, like)
	}
	if kw := strings.TrimSpace(f.UserName); kw != "" {
		db = db.Where("user_name ILIKE ?", "%"+escapeSQLLike(kw)+"%")
	}
	if kw := strings.TrimSpace(f.IpAddr); kw != "" {
		db = db.Where("ip_addr ILIKE ?", "%"+escapeSQLLike(kw)+"%")
	}
	if et := strings.TrimSpace(f.EventType); et != "" {
		db = db.Where("event_type = ?", et)
	}
	if f.Code != nil {
		db = db.Where("code = ?", *f.Code)
	}
	if f.StartTime > 0 {
		db = db.Where("created_time >= ?", time.UnixMilli(f.StartTime))
	}
	if f.EndTime > 0 {
		db = db.Where("created_time <= ?", time.UnixMilli(f.EndTime))
	}
	return db
}

func normalizeUserLoginLogs(items []UserLoginLog) {
	for i := range items {
		normalizeUserLoginLog(&items[i])
	}
}

func normalizeUserLoginLog(item *UserLoginLog) {
	if item == nil {
		return
	}
	if item.CreatedAt == 0 && !item.CreatedTime.IsZero() {
		item.CreatedAt = item.CreatedTime.UTC().UnixMilli()
	}
}

// truncateUserLoginField 按 Unicode 字符（rune）截断并去首尾空白，与 PostgreSQL
// VARCHAR(n) 的字符数语义一致；按字节截断会把多字节账号切出非法 UTF-8 导致落库失败。
func truncateUserLoginField(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

type UserLoginLogRepo struct{ db *gorm.DB }

func NewUserLoginLogRepo(in any) *UserLoginLogRepo { return &UserLoginLogRepo{db: GetCommonConn(in)} }
