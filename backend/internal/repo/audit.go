package repo

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	AuditBizCreate           int64 = 1
	AuditBizUpdate           int64 = 2
	AuditBizDelete           int64 = 3
	AuditBizImport           int64 = 4
	AuditBizExport           int64 = 5
	AuditBizLogin            int64 = 6
	AuditBizLogout           int64 = 7
	AuditBizDeploy           int64 = 8
	AuditBizStop             int64 = 9
	AuditBizRollback         int64 = 10
	AuditBizSaveAs           int64 = 11
	AuditBizMoveToRecycleBin int64 = 12
	AuditBizRestore          int64 = 13
	AuditBizInvite           int64 = 14
	AuditBizRemoveMember     int64 = 15
	AuditBizChangeRole       int64 = 16
	AuditBizEnable           int64 = 17
	AuditBizDisable          int64 = 18
	AuditBizResetPassword    int64 = 19
	AuditBizAssignRole       int64 = 20
	AuditBizReplace          int64 = 21
	AuditBizActivate         int64 = 22
	AuditBizInstall          int64 = 23
	AuditBizUninstall        int64 = 24
	AuditBizSave             int64 = 25
	AuditBizCopy             int64 = 26
	AuditBizMark             int64 = 27
	AuditBizUnmark           int64 = 28
	AuditBizBindUNS          int64 = 29
	AuditBizConfirm          int64 = 30
	AuditBizStart            int64 = 31
	AuditBizView             int64 = 32
)

var auditBusinessTypeByName = map[string]int64{
	"create":           AuditBizCreate,
	"update":           AuditBizUpdate,
	"delete":           AuditBizDelete,
	"import":           AuditBizImport,
	"export":           AuditBizExport,
	"login":            AuditBizLogin,
	"logout":           AuditBizLogout,
	"deploy":           AuditBizDeploy,
	"stop":             AuditBizStop,
	"rollback":         AuditBizRollback,
	"saveas":           AuditBizSaveAs,
	"movetorecyclebin": AuditBizMoveToRecycleBin,
	"restore":          AuditBizRestore,
	"invite":           AuditBizInvite,
	"removemember":     AuditBizRemoveMember,
	"changerole":       AuditBizChangeRole,
	"enable":           AuditBizEnable,
	"disable":          AuditBizDisable,
	"resetpassword":    AuditBizResetPassword,
	"assignrole":       AuditBizAssignRole,
	"replace":          AuditBizReplace,
	"activate":         AuditBizActivate,
	"install":          AuditBizInstall,
	"uninstall":        AuditBizUninstall,
	"save":             AuditBizSave,
	"copy":             AuditBizCopy,
	"mark":             AuditBizMark,
	"unmark":           AuditBizUnmark,
	"binduns":          AuditBizBindUNS,
	"confirm":          AuditBizConfirm,
	"start":            AuditBizStart,
	"view":             AuditBizView,
}

var auditBusinessNameByType = map[int64]string{
	AuditBizCreate:           "Create",
	AuditBizUpdate:           "Update",
	AuditBizDelete:           "Delete",
	AuditBizImport:           "Import",
	AuditBizExport:           "Export",
	AuditBizLogin:            "Login",
	AuditBizLogout:           "Logout",
	AuditBizDeploy:           "Deploy",
	AuditBizStop:             "Stop",
	AuditBizRollback:         "Rollback",
	AuditBizSaveAs:           "SaveAs",
	AuditBizMoveToRecycleBin: "MoveToRecycleBin",
	AuditBizRestore:          "Restore",
	AuditBizInvite:           "Invite",
	AuditBizRemoveMember:     "RemoveMember",
	AuditBizChangeRole:       "ChangeRole",
	AuditBizEnable:           "Enable",
	AuditBizDisable:          "Disable",
	AuditBizResetPassword:    "ResetPassword",
	AuditBizAssignRole:       "AssignRole",
	AuditBizReplace:          "Replace",
	AuditBizActivate:         "Activate",
	AuditBizInstall:          "Install",
	AuditBizUninstall:        "Uninstall",
	AuditBizSave:             "Save",
	AuditBizCopy:             "Copy",
	AuditBizMark:             "Mark",
	AuditBizUnmark:           "Unmark",
	AuditBizBindUNS:          "BindUNS",
	AuditBizConfirm:          "Confirm",
	AuditBizStart:            "Start",
	AuditBizView:             "View",
}

type AuditLog struct {
	ID               int64     `gorm:"column:id;type:BIGSERIAL;primaryKey;autoIncrement" json:"id"`
	UserID           int64     `gorm:"column:user_id;type:BIGINT;not null" json:"userId"`
	ResType          string    `gorm:"column:res_type;type:VARCHAR(50)" json:"resType"`
	ResID            string    `gorm:"column:res_id;type:VARCHAR(50)" json:"resId"`
	ResName          string    `gorm:"column:res_name;type:VARCHAR(256)" json:"resName"`
	BusinessTypeCode int64     `gorm:"column:business_type;type:BIGINT;not null" json:"-"`
	DetailJSON       string    `gorm:"column:detail;type:TEXT" json:"detailJson"`
	Code             int64     `gorm:"column:code;type:BIGINT;not null;default:200" json:"code"`
	IsShowInRecent   int64     `gorm:"column:is_show_in_recent;type:BIGINT;not null;default:1" json:"isShowInRecent"`
	OperatorName     string    `gorm:"column:operator_name;type:VARCHAR(100)" json:"operatorName"`
	OperatorEmail    string    `gorm:"-" json:"operatorEmail"`
	CreatedTime      time.Time `gorm:"column:created_time;type:TIMESTAMPTZ;index;not null;default:CURRENT_TIMESTAMP" json:"-"`

	EventID      string `gorm:"column:event_id" json:"eventId,omitempty"`
	RequestID    string `gorm:"column:request_id" json:"requestId"`
	IP           string `gorm:"column:ip" json:"ip"`
	UserAgent    string `gorm:"column:user_agent" json:"userAgent"`
	ResultMsg    string `gorm:"column:result_msg" json:"resultMsg"`
	ResourcePath string `gorm:"column:resource_path" json:"resourcePath"`
	Result       string `gorm:"column:result" json:"result"`
	DetailValid  bool   `gorm:"-" json:"detailValid"`
	OperatorID   string `gorm:"-" json:"operatorId"`
	ScopeType    int    `gorm:"column:scope_type" json:"scopeType"`
	ScopeID      int64  `gorm:"column:scope_id" json:"scopeId"`
	ScopeName    string `gorm:"column:scope_name" json:"scopeName"`
	BusinessType string `gorm:"-" json:"businessType"`
	CreatedAt    int64  `gorm:"-" json:"createdAt"`
}

func (AuditLog) TableName() string { return "sys_user_oper_log" }

type AuditLogFilter struct {
	// FleetUserOperations hides background/refresh noise only in the Fleet view.
	FleetUserOperations bool
	PageNo              int
	PageSize            int
	ScopeType           int
	ScopeID             int64
	ResType             string
	ResID               string
	ScopeName           string
	OperatorID          int64
	ResourceKeyword     string
	RequestID           string
	IP                  string
	Result              string
	BusinessTypeCode    int64
	OperatorKeyword     string
	OperatorUserIDs     []int64
	Code                *int
	StartTime           int64
	EndTime             int64
}

const auditSelect = "id, user_id, res_type, res_id, res_name, business_type, detail, code, is_show_in_recent, operator_name, created_time, scope_type, scope_id, scope_name, request_id, ip, user_agent, result_msg, resource_path, event_id, result"

func AuditBusinessTypeCode(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return n
	}
	return auditBusinessTypeByName[strings.ToLower(value)]
}

func AuditBusinessTypeName(code int64) string {
	if name := auditBusinessNameByType[code]; name != "" {
		return name
	}
	if code == 0 {
		return ""
	}
	return strconv.FormatInt(code, 10)
}

func (r *AuditRepo) ListAuditLogs(ctx context.Context, filter AuditLogFilter) ([]AuditLog, int64, error) {
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
	operatorUserIDs, err := r.userIDsMatchingEmail(ctx, filter.OperatorKeyword)
	if err != nil {
		return nil, 0, err
	}
	filter.OperatorUserIDs = operatorUserIDs

	var total int64
	if err := applyAuditFilter(r.db.WithContext(ctx).Model(&AuditLog{}), filter).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var out []AuditLog
	q := applyAuditFilter(r.db.WithContext(ctx).Model(&AuditLog{}).Select(auditSelect), filter)
	err = q.Order("created_time DESC, id DESC").Limit(pageSize).Offset((pageNo - 1) * pageSize).Find(&out).Error
	if err != nil {
		return nil, 0, err
	}
	if err := r.attachOperatorEmails(ctx, out); err != nil {
		return nil, 0, err
	}
	normalizeAuditLogs(out)
	return out, total, nil
}

func (r *AuditRepo) GetAuditLog(ctx context.Context, id int64) (AuditLog, error) {
	var item AuditLog
	err := r.db.WithContext(ctx).Model(&AuditLog{}).Select(auditSelect).Where("id = ?", id).Take(&item).Error
	if err == nil {
		items := []AuditLog{item}
		if attachErr := r.attachOperatorEmails(ctx, items); attachErr != nil {
			return AuditLog{}, attachErr
		}
		item = items[0]
		normalizeAuditLog(&item)
	}
	return item, err
}

type RecentAuditCursor struct {
	CreatedTime time.Time
	ID          int64
}

func (r *AuditRepo) ListRecentAuditLogs(ctx context.Context, userID int64, limit int, isShowInRecent int64, before *RecentAuditCursor) ([]AuditLog, error) {
	if userID <= 0 {
		return nil, ErrInvalidArgument
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var out []AuditLog
	query := r.db.WithContext(ctx).Model(&AuditLog{}).Select(auditSelect).
		Where("user_id = ? AND res_type <> '' AND code >= 200 AND code < 400", userID)
	if isShowInRecent > 0 {
		query = query.Where("is_show_in_recent = ?", isShowInRecent)
	}
	if before != nil {
		query = query.Where("(created_time, id) < (?, ?)", before.CreatedTime, before.ID)
	}
	err := query.Order("created_time DESC, id DESC").Limit(limit).Find(&out).Error
	if err != nil {
		return nil, err
	}
	if err := r.attachOperatorEmails(ctx, out); err != nil {
		return nil, err
	}
	normalizeAuditLogs(out)
	return out, nil
}

// ListRecentAppViews 查询用户最近的 ProjectApp View 记录，用于 Launchpad 最近应用。
func (r *AuditRepo) ListRecentAppViews(ctx context.Context, userID int64, limit int) ([]AuditLog, error) {
	if userID <= 0 {
		return nil, ErrInvalidArgument
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var out []AuditLog
	err := r.db.WithContext(ctx).Model(&AuditLog{}).Select(auditSelect).
		Where("user_id = ? AND res_type = 'ProjectApp' AND business_type = ? AND code >= 200 AND code < 400", userID, AuditBizView).
		Order("created_time DESC, id DESC").
		Limit(limit).
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	if err := r.attachOperatorEmails(ctx, out); err != nil {
		return nil, err
	}
	normalizeAuditLogs(out)
	return out, nil
}

func (r *AuditRepo) InsertAuditLog(ctx context.Context, item AuditLog) error {
	if item.CreatedTime.IsZero() {
		item.CreatedTime = time.Now().UTC()
	}
	if item.Code == 0 {
		item.Code = 200
	}
	if item.Result == "" {
		item.Result = AuditResult(item.Code)
	}
	if item.IsShowInRecent == 0 {
		item.IsShowInRecent = 1
	}
	if item.BusinessTypeCode == 0 {
		item.BusinessTypeCode = AuditBusinessTypeCode(item.BusinessType)
	}
	if strings.TrimSpace(item.DetailJSON) == "" {
		item.DetailJSON = "{}"
	}
	q := r.db.WithContext(ctx).Table(AuditLog{}.TableName())
	if item.EventID != "" {
		q = q.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_id"}}, TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "event_id <> ''"}}}, DoNothing: true})
	}
	return q.Create(map[string]any{
		"event_id":   item.EventID,
		"result":     item.Result,
		"scope_type": item.ScopeType, "scope_id": item.ScopeID, "scope_name": truncateAuditField(item.ScopeName, 200),
		"request_id": truncateAuditField(item.RequestID, 64), "ip": truncateAuditField(item.IP, 64),
		"user_agent": item.UserAgent, "result_msg": item.ResultMsg, "resource_path": item.ResourcePath,
		"user_id":           item.UserID,
		"res_type":          truncateAuditField(item.ResType, 50),
		"res_id":            truncateAuditField(item.ResID, 50),
		"res_name":          truncateAuditField(item.ResName, 256),
		"business_type":     item.BusinessTypeCode,
		"detail":            item.DetailJSON,
		"code":              item.Code,
		"is_show_in_recent": item.IsShowInRecent,
		"operator_name":     truncateAuditField(item.OperatorName, 100),
		"created_time":      item.CreatedTime,
	}).Error
}

func applyAuditFilter(db *gorm.DB, f AuditLogFilter) *gorm.DB {
	if f.FleetUserOperations {
		db = db.Where("user_id > 0 AND LOWER(TRIM(COALESCE(operator_name, ''))) <> ?", "system")
		// detail is legacy TEXT and may not be valid JSON; avoid casts that can
		// break the entire list. Match the action field, not arbitrary text.
		db = db.Where("COALESCE(detail, '') !~ ?", `"action"[[:space:]]*:[[:space:]]*"flow\.(catalog|snapshot)\.refresh"`)
	}
	if f.ScopeType != 0 {
		if f.ScopeType == -1 {
			db = db.Where("scope_type = 0")
		} else {
			db = db.Where("scope_type = ?", f.ScopeType)
		}
	}
	if f.ScopeID > 0 {
		db = db.Where("scope_id = ?", f.ScopeID)
	}
	if kw := strings.TrimSpace(f.ScopeName); kw != "" {
		db = db.Where("scope_name ILIKE ?", auditLike(kw))
	}
	if f.OperatorID > 0 {
		db = db.Where("user_id = ?", f.OperatorID)
	}
	if kw := strings.TrimSpace(f.ResourceKeyword); kw != "" {
		db = db.Where("(res_name ILIKE ? OR res_id = ? OR resource_path ILIKE ? OR (res_type = 'UNS' AND resource_path = '' AND detail ILIKE ?))", auditLike(kw), kw, auditLike(kw), auditLike(kw))
	}
	if f.RequestID != "" {
		db = db.Where("request_id = ?", strings.TrimSpace(f.RequestID))
	}
	if f.IP != "" {
		db = db.Where("ip = ?", strings.TrimSpace(f.IP))
	}
	switch f.Result {
	case "success":
		db = db.Where("(result = 'success' OR (result = '' AND code >= 200 AND code < 400 AND code <> 202))")
	case "accepted":
		db = db.Where("(result = 'accepted' OR (result = '' AND code = 202))")
	case "failed":
		db = db.Where("(result = 'failed' OR (result = '' AND (code < 200 OR code >= 400)))")
	}
	if rt := strings.TrimSpace(f.ResType); rt != "" {
		db = db.Where("res_type = ?", rt)
	}
	if resID := strings.TrimSpace(f.ResID); resID != "" {
		db = db.Where("res_id = ?", resID)
	}
	if f.BusinessTypeCode != 0 {
		db = db.Where("business_type = ?", f.BusinessTypeCode)
	}
	if kw := strings.TrimSpace(f.OperatorKeyword); kw != "" {
		like := auditLike(kw)
		if len(f.OperatorUserIDs) > 0 {
			db = db.Where("(operator_name ILIKE ? OR CAST(user_id AS TEXT) ILIKE ? OR user_id IN ?)", like, like, f.OperatorUserIDs)
		} else {
			db = db.Where("(operator_name ILIKE ? OR CAST(user_id AS TEXT) ILIKE ?)", like, like)
		}
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

func (r *AuditRepo) userIDsMatchingEmail(ctx context.Context, keyword string) ([]int64, error) {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if !strings.Contains(keyword, "@") {
		return nil, nil
	}
	type row struct {
		UserID int64  `gorm:"column:user_id"`
		Email  string `gorm:"column:email"`
	}
	var rows []row
	if err := r.db.WithContext(ctx).Table("sys_user_info").Select("user_id, email").Where("email <> ''").Scan(&rows).Error; err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0)
	for _, item := range rows {
		email, _, err := decryptUserContacts(item.UserID, item.Email, "")
		if err != nil {
			return nil, err
		}
		if strings.Contains(strings.ToLower(email), keyword) {
			userIDs = append(userIDs, item.UserID)
		}
	}
	return userIDs, nil
}

func (r *AuditRepo) attachOperatorEmails(ctx context.Context, items []AuditLog) error {
	if len(items) == 0 {
		return nil
	}
	userIDs := make([]int64, 0, len(items))
	seen := make(map[int64]struct{}, len(items))
	for _, item := range items {
		if item.UserID <= 0 {
			continue
		}
		if _, ok := seen[item.UserID]; ok {
			continue
		}
		seen[item.UserID] = struct{}{}
		userIDs = append(userIDs, item.UserID)
	}
	if len(userIDs) == 0 {
		return nil
	}
	type row struct {
		UserID int64  `gorm:"column:user_id"`
		Email  string `gorm:"column:email"`
	}
	var rows []row
	if err := r.db.WithContext(ctx).Table("sys_user_info").Select("user_id, email").Where("user_id IN ?", userIDs).Scan(&rows).Error; err != nil {
		return err
	}
	emails := make(map[int64]string, len(rows))
	for _, item := range rows {
		email, _, err := decryptUserContacts(item.UserID, item.Email, "")
		if err != nil {
			return err
		}
		emails[item.UserID] = email
	}
	for index := range items {
		items[index].OperatorEmail = emails[items[index].UserID]
	}
	return nil
}

func normalizeAuditLogs(items []AuditLog) {
	for i := range items {
		normalizeAuditLog(&items[i])
	}
}

func normalizeAuditLog(item *AuditLog) {
	if item == nil {
		return
	}
	item.OperatorID = strconv.FormatInt(item.UserID, 10)
	if item.Result == "" {
		item.Result = AuditResult(item.Code)
	}
	item.DetailValid = json.Valid([]byte(item.DetailJSON))
	if item.ResultMsg == "" && item.DetailValid {
		var detail map[string]any
		if json.Unmarshal([]byte(item.DetailJSON), &detail) == nil {
			item.ResultMsg, _ = detail["_resultMsg"].(string)
		}
	}
	item.BusinessType = AuditBusinessTypeName(item.BusinessTypeCode)
	if item.CreatedAt == 0 && !item.CreatedTime.IsZero() {
		item.CreatedAt = item.CreatedTime.UTC().UnixMilli()
	}
	if strings.TrimSpace(item.DetailJSON) == "" {
		item.DetailJSON = "{}"
	}
}

func truncateAuditField(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

type AuditRepo struct{ db *gorm.DB }

func NewAuditRepo(in any) *AuditRepo { return &AuditRepo{db: GetCommonConn(in)} }

func AuditResult(code int64) string {
	if code == 202 {
		return "accepted"
	}
	if code >= 200 && code < 400 {
		return "success"
	}
	return "failed"
}
