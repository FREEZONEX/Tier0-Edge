package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
)

type ctxKey string

const stateKey ctxKey = "audit.state"
const auditResultMsgKey = "_resultMsg"

const (
	ScopeTypePlatform = 1
	ScopeTypeProject  = 2
)

const (
	ResTypeAuth           = "Auth"
	ResTypeUserManagement = "UserManagement"
	ResTypeRole           = "Role"
	ResTypeProject        = "Project"
	ResTypeProjectMember  = "ProjectMember"
	ResTypeProjectApp     = "ProjectApp"
	ResTypeUNS            = "UNS"
	ResTypeResource       = "Resource"
	ResTypeLicense        = "License"
	ResTypeApp            = "App"
	ResTypeTemplate       = "Template"
	ResTypeLabel          = "Label"
	ResTypeGroup          = "Group"
	ResTypeDashboard      = "Dashboard"
	ResTypeAppKey         = "AppKey"
	ResTypeSourceFlow     = "SourceFlow"
	ResTypeEventFlow      = "EventFlow"
	ResTypeNotebook       = "Notebook"
	ResTypeGateway        = "Gateway"
	ResTypeOAuthClient    = "OAuthClient"
	ResTypeMQTTAuth       = "MQTTAuth"
	ResTypeCluster        = "Cluster"
	ResTypeAsset          = "Asset"
	ResTypePlatform       = "Platform"
	ResTypeFleet          = "Fleet"
	ResTypeVisionEvent    = "VisionEvent"
	ResTypeCamera         = "Camera"
	ResTypeAnchor         = "Anchor"
)

const (
	BizCreate        = "Create"
	BizUpdate        = "Update"
	BizDelete        = "Delete"
	BizEnable        = "Enable"
	BizDisable       = "Disable"
	BizImport        = "Import"
	BizExport        = "Export"
	BizStart         = "Start"
	BizStop          = "Stop"
	BizLogin         = "Login"
	BizLogout        = "Logout"
	BizResetPassword = "ResetPassword"
	BizAssignRole    = "AssignRole"
	BizReplace       = "Replace"
	BizActivate      = "Activate"
	BizInstall       = "Install"
	BizUninstall     = "Uninstall"
	BizSave          = "Save"
	BizCopy          = "Copy"
	BizDeploy        = "Deploy"
	BizMark          = "Mark"
	BizUnmark        = "Unmark"
	BizBindUNS       = "BindUNS"
	BizConfirm       = "Confirm"
	BizRestore       = "Restore"
	BizView          = "View"
)

const (
	ShowInRecentYes int64 = 1
	ShowInRecentNo  int64 = 2
)

type RecordInput struct {
	ScopeType      int
	ScopeID        int64
	ScopeName      string
	ResType        string
	ResID          string
	ResName        string
	BusinessType   string
	IsShowInRecent int64
	// Code 仅作用于 RecordDirect(脱离 HTTP 响应上下文)的记录;
	// 0 视为 200。走 request state 的记录仍以响应状态码为准。
	Code int
	// Result can retain a non-success business outcome on a successful HTTP response.
	Result string
	Detail map[string]any
}

type Operator struct {
	ID    string
	Name  string
	Email string
}

type State struct {
	Record         *RecordInput
	Operator       *Operator
	RequestPayload map[string]any
	Suppressed     bool
	ClientIP       string
	UserAgent      string
}

type Service struct {
	repo              *repo.AuditRepo
	jobs              *repo.AsyncJobRepo
	outbox            *repo.OutboxRepo
	trustProxyHeaders bool
}

type PageQuery struct {
	PageNo          int
	PageSize        int
	ScopeType       int
	ScopeID         int64
	ResType         string
	BusinessType    string
	OperatorKeyword string
	Code            *int
	StartTime       string
	EndTime         string
	ScopeName       string
	OperatorID      int64
	ResourceKeyword string
	ResID           string
	RequestID       string
	IP              string
	Result          string
}

type ResponseMeta struct {
	StatusCode int
	Status     string
	JobKey     string
	JobStatus  string
}

type Option func(*Service)

func WithTrustProxyHeaders(trust bool) Option {
	return func(s *Service) { s.trustProxyHeaders = trust }
}

func New(ctx context.Context, options ...Option) *Service {
	auditRepo := repo.NewAuditRepo(ctx)
	svc := &Service{
		repo:   auditRepo,
		jobs:   repo.NewAsyncJobRepo(ctx),
		outbox: repo.NewOutboxRepo(ctx),
	}
	for _, option := range options {
		option(svc)
	}
	return svc
}

func (s *Service) Page(ctx context.Context, query PageQuery) (map[string]any, error) {
	filter, err := buildFilter(query)
	if err != nil {
		return nil, err
	}
	items, total, err := s.repo.ListAuditLogs(ctx, filter)
	if err != nil {
		return nil, err
	}
	pageNo := query.PageNo
	if pageNo <= 0 {
		pageNo = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return map[string]any{
		"pageNo":   pageNo,
		"pageSize": pageSize,
		"total":    total,
		"data":     items,
	}, nil
}

func (s *Service) Detail(ctx context.Context, id int64) (repo.AuditLog, error) {
	item, err := s.repo.GetAuditLog(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		return repo.AuditLog{}, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	// Preserve the submitted audit event, but expose the current task snapshot in its details.
	var detail map[string]any
	if s.jobs != nil && item.ResType == ResTypeUNS && item.BusinessType == BizImport && json.Unmarshal([]byte(item.DetailJSON), &detail) == nil {
		if jobKey, ok := detail["jobKey"].(string); ok && jobKey != "" {
			job, jobErr := s.jobs.GetAsyncJob(ctx, jobKey)
			if jobErr == nil && job.JobType == "uns.import" {
				var request struct {
					SourceFileID int64 `json:"sourceFileId"`
				}
				if json.Unmarshal(job.RequestJSON, &request) == nil && request.SourceFileID > 0 {
					detail["sourceFileId"] = request.SourceFileID
				}
				detail["job"] = map[string]any{"jobKey": job.JobKey, "status": job.Status, "progress": job.Progress, "resultJson": job.ResultJSON, "errorMessage": job.ErrorMessage}
				item.DetailJSON = marshalDetail(detail)
			} else if jobErr != nil && !errors.Is(jobErr, repo.ErrNotFound) {
				return item, jobErr
			}
		}
	}
	return item, nil
}

func (s *Service) WithState(ctx context.Context) context.Context {
	if StateFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, stateKey, &State{})
}

func StateFromContext(ctx context.Context) *State {
	if ctx == nil {
		return nil
	}
	if state, ok := ctx.Value(stateKey).(*State); ok {
		return state
	}
	return nil
}

func (s *Service) Record(ctx context.Context, input RecordInput) {
	state := StateFromContext(ctx)
	if state == nil {
		return
	}
	copied := normalizeRecord(input)
	state.Record = &copied
}

func (s *Service) Suppress(ctx context.Context) {
	state := StateFromContext(ctx)
	if state != nil {
		state.Suppressed = true
	}
}

// RecordDirect records an audit entry immediately, without an HTTP request
// context. It is used by async jobs and internal flows where the middleware
// never created an audit state; operator may be nil to record as "system".
func (s *Service) RecordDirect(operator *Operator, input RecordInput) {
	if s == nil {
		return
	}
	record := normalizeRecord(input)
	if strings.TrimSpace(record.ResType) == "" || strings.TrimSpace(record.BusinessType) == "" {
		return
	}
	detail := sanitizeDetail(record.Detail)
	if len(detail) == 0 {
		detail = map[string]any{}
	}
	code := record.Code
	if code <= 0 {
		code = http.StatusOK
	}
	item := &repo.AuditLog{
		UserID:           operatorUserID(operator),
		ScopeType:        record.ScopeType,
		ScopeID:          record.ScopeID,
		ScopeName:        truncateString(record.ScopeName, 200),
		ResType:          truncateString(record.ResType, 50),
		ResID:            truncateString(record.ResID, 50),
		ResName:          truncateString(record.ResName, 256),
		BusinessType:     truncateString(record.BusinessType, 64),
		BusinessTypeCode: repo.AuditBusinessTypeCode(record.BusinessType),
		IsShowInRecent:   showInRecentForRecord(&record),
		Code:             int64(code),
		DetailJSON:       marshalDetail(detail),
		CreatedTime:      time.Now().UTC(),
	}
	if operator != nil {
		item.OperatorName = truncateString(operator.Name, 200)
		item.OperatorEmail = truncateString(operator.Email, 255)
	}
	if strings.TrimSpace(item.OperatorName) == "" {
		item.OperatorName = "system"
	}
	s.persist(context.Background(), *item)
}

func (s *Service) BindSubject(ctx context.Context, subject contextx.Subject) {
	state := StateFromContext(ctx)
	if state == nil {
		return
	}
	name := strings.TrimSpace(subject.UserName)
	if subject.AuthType == "apiKey" && strings.TrimSpace(subject.APIKeyName) != "" {
		name = strings.TrimSpace(subject.APIKeyName)
	}
	state.Operator = &Operator{
		ID:    strconv.FormatInt(subject.UserID, 10),
		Name:  truncateString(name, 200),
		Email: truncateString(subject.Email, 255),
	}
}

func (s *Service) BindRequestPayload(ctx context.Context, payload map[string]any) {
	state := StateFromContext(ctx)
	if state == nil || len(payload) == 0 {
		return
	}
	state.RequestPayload = payload
}

// BindRequestInfo stores the client IP and User-Agent of the current request in
// the audit state so business logic (e.g. login logging) can read them from the
// request context without touching the http.Request directly.
func (s *Service) BindRequestInfo(r *http.Request) {
	if r == nil {
		return
	}
	state := StateFromContext(r.Context())
	if state == nil {
		return
	}
	state.ClientIP = clientIP(r, s.trustProxyHeaders)
	state.UserAgent = truncateString(r.UserAgent(), 256)
}

// RequestInfoFrom returns the client IP and User-Agent bound to the request
// context by the audit middleware.
func RequestInfoFrom(ctx context.Context) (ip, userAgent string, ok bool) {
	state := StateFromContext(ctx)
	if state == nil {
		return "", "", false
	}
	return state.ClientIP, state.UserAgent, true
}

func (s *Service) FlushRequest(r *http.Request, meta ResponseMeta) {
	item := s.BuildAuditLog(r, meta)
	if item == nil {
		return
	}
	s.persist(r.Context(), *item)
}

func (s *Service) BuildAuditLog(r *http.Request, meta ResponseMeta) *repo.AuditLog {
	if r == nil {
		return nil
	}
	state := StateFromContext(r.Context())
	var record *RecordInput
	var operator *Operator
	explicit := false
	if state != nil {
		if state.Suppressed {
			return nil
		}
		record = state.Record
		explicit = record != nil
		operator = state.Operator
	}
	if record == nil {
		record = autoRecord(r)
	}
	if record != nil {
		normalized := normalizeRecord(*record)
		record = &normalized
	}
	if !shouldRecordAudit(r.Method, record, explicit) {
		return nil
	}
	requestPayload := requestPayloadFromContext(r.Context())
	if strings.TrimSpace(record.ResName) == "" {
		record.ResName = resourceNameFromPayload(requestPayload)
	}
	if strings.TrimSpace(record.ResName) == "" {
		record.ResName = record.ResID
	}

	code := meta.StatusCode
	if code <= 0 {
		code = http.StatusOK
	}
	result := repo.AuditResult(int64(code))
	if code >= 200 && code < 300 && (record.Result == "accepted" || record.Result == "failed") {
		result = record.Result
	}
	if code >= 200 && code < 300 && meta.JobKey != "" {
		switch meta.JobStatus {
		case repo.AsyncJobStatusPending, repo.AsyncJobStatusRunning:
			result = "accepted"
		case repo.AsyncJobStatusFailed, repo.AsyncJobStatusCanceled:
			result = "failed"
		}
	}
	detail := sanitizeDetail(record.Detail)
	if len(detail) == 0 {
		detail = map[string]any{}
	}
	if _, ok := detail["path"]; !ok {
		detail["path"] = r.URL.Path
	}
	if _, ok := detail["method"]; !ok {
		detail["method"] = r.Method
	}
	if r.URL.RawQuery != "" {
		detail["query"] = sanitizeDetailValue(mapFromQuery(r.URL.Query()))
	}
	if meta.JobKey != "" {
		detail["jobKey"] = meta.JobKey
		detail["jobStatus"] = meta.JobStatus
	}
	if status := strings.TrimSpace(meta.Status); status != "" {
		detail[auditResultMsgKey] = status
	} else {
		detail[auditResultMsgKey] = http.StatusText(code)
	}

	item := &repo.AuditLog{
		UserID:           operatorUserID(operator),
		ScopeType:        record.ScopeType,
		ScopeID:          record.ScopeID,
		ScopeName:        truncateString(record.ScopeName, 200),
		ResType:          truncateString(record.ResType, 50),
		ResID:            truncateString(record.ResID, 50),
		ResName:          truncateString(record.ResName, 256),
		BusinessType:     truncateString(record.BusinessType, 64),
		BusinessTypeCode: repo.AuditBusinessTypeCode(record.BusinessType),
		IsShowInRecent:   showInRecentForRecord(record),
		Code:             int64(code),
		Result:           result,
		RequestID:        requestID(r), IP: clientIP(r, s.trustProxyHeaders), UserAgent: truncateString(r.UserAgent(), 2048),
		ResultMsg: truncateString(meta.Status, 2048), ResourcePath: resourcePath(detail),
		DetailJSON:  marshalDetail(detail),
		CreatedTime: time.Now().UTC(),
	}
	if operator != nil {
		item.OperatorName = truncateString(operator.Name, 200)
		item.OperatorEmail = truncateString(operator.Email, 255)
	}
	return item
}

func operatorUserID(operator *Operator) int64 {
	if operator == nil {
		return 0
	}
	userID, err := strconv.ParseInt(strings.TrimSpace(operator.ID), 10, 64)
	if err != nil || userID < 0 {
		return 0
	}
	return userID
}

func autoRecord(r *http.Request) *RecordInput {
	if r == nil || !shouldAutoAudit(r) {
		return nil
	}
	if isAuditExport(r) {
		return &RecordInput{ScopeType: ScopeTypePlatform, ResType: "AuditLog", ResName: "AuditLog", BusinessType: BizExport, IsShowInRecent: ShowInRecentNo, Detail: map[string]any{"auto": true}}
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	core := stripAPIPrefix(parts)
	if len(core) == 0 {
		return nil
	}
	resType := resourceTypeFromPath(core)
	bizType := businessTypeFromPath(r.Method, core)
	resID := resourceIDFromPath(core)
	if strings.EqualFold(core[0], "iam") {
		// IAM 集合路由的 users/roles 不是对象 ID；失败创建没有已存在的对象。
		resID = ""
		if len(core) > 2 {
			if id, err := strconv.ParseInt(core[2], 10, 64); err == nil && id > 0 {
				resID = core[2]
			}
		}
	}
	if resType == "" || bizType == "" {
		return nil
	}
	return &RecordInput{
		ScopeType:    ScopeTypePlatform,
		ResType:      resType,
		ResID:        resID,
		ResName:      resourceNameFromPayload(requestPayloadFromContext(r.Context())),
		BusinessType: bizType,
		Detail:       map[string]any{"auto": true},
	}
}

func requestPayloadFromContext(ctx context.Context) map[string]any {
	state := StateFromContext(ctx)
	if state == nil {
		return nil
	}
	return state.RequestPayload
}

func shouldAutoAudit(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	path := strings.ToLower(strings.TrimSpace(r.URL.Path))
	if path == "" {
		return false
	}
	if isAuditExport(r) {
		return true
	}
	skips := []string{
		"/api/core/audit-logs",
		"/api/core/login-logs",
		// 登录/登出事件走登录日志（login-logs）或显式审计，登录接口本身不再自动审计。
		"/api/core/auth/login",
		"/api/core/auth/logout",
		"/api/core/auth/sso/",
		"/api/core/auth/mini-entry",
		"/api/core/mqtt/auth",
		"/api/core/mqtt/acl",
		"/api/core/oauth/token",
		"/api/core/oauth/authorize",
		"/api/core/uns/newmsg",
		"/api/core/system/",
		"/api/core/assets",
		"/api/core/asset-bindings",
		"/api/openapi/",
		"/openapi/",
		"/healthz",
		"/readyz",
		"/metrics",
	}
	for _, skip := range skips {
		if strings.HasPrefix(path, skip) {
			return false
		}
	}
	// Vision worker 面的机器调用(注册/结果上报/心跳)按秒级频率触发,
	// 不是用户操作,不进审计;heartbeat 带路径参数,只能按后缀识别。
	if strings.HasPrefix(path, "/api/core/vision/worker/") ||
		strings.HasPrefix(path, "/api/core/vision/ingest/") ||
		(strings.HasPrefix(path, "/api/core/vision/tasks/") && strings.HasSuffix(path, "/heartbeat")) {
		return false
	}
	return strings.HasPrefix(path, "/api/core/")
}

func isAuditExport(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/core/audit-logs/export"
}

func stripAPIPrefix(parts []string) []string {
	if len(parts) >= 3 && parts[0] == "api" && parts[1] == "core" {
		return parts[2:]
	}
	return parts
}

func resourceTypeFromPath(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	first := strings.ToLower(parts[0])
	second := ""
	if len(parts) > 1 {
		second = strings.ToLower(parts[1])
	}
	switch first {
	case "iam":
		// 成功操作由业务显式记录；失败操作按真实 IAM 路由归入相同模块。
		if len(parts) > 1 {
			return resourceTypeFromPath(parts[1:])
		}
		return "Iam"
	case "auth":
		return ResTypeAuth
	case "users", "user":
		return ResTypeUserManagement
	case "roles", "role":
		return ResTypeRole
	case "resources", "resource", "menus", "menu":
		return ResTypeResource
	case "projects", "project":
		if len(parts) > 2 && strings.EqualFold(parts[2], "apps") {
			return ResTypeProjectApp
		}
		if second == "members" {
			return ResTypeProjectMember
		}
		return ResTypeProject
	case "uns":
		switch second {
		case "templates":
			return ResTypeTemplate
		case "labels":
			return ResTypeLabel
		default:
			return ResTypeUNS
		}
	case "flows", "flow":
		if len(parts) > 1 && (parts[1] == "event" || parts[1] == "event-flow") {
			return ResTypeEventFlow
		}
		return ResTypeSourceFlow
	case "dashboards", "dashboard":
		return ResTypeDashboard
	case "notebooks", "notebook":
		return ResTypeNotebook
	case "apikeys", "api-keys", "apikey":
		return ResTypeAppKey
	case "gateway":
		return ResTypeGateway
	case "oauth":
		return ResTypeOAuthClient
	case "mqtt":
		return ResTypeMQTTAuth
	case "cluster":
		return ResTypeCluster
	case "assets", "asset":
		return ResTypeAsset
	default:
		return toPascal(first)
	}
}

func businessTypeFromPath(method string, parts []string) string {
	joined := strings.ToLower(strings.Join(parts, "/"))
	switch {
	case strings.Contains(joined, "login"):
		return BizLogin
	case strings.Contains(joined, "logout"):
		return BizLogout
	case strings.Contains(joined, "reset"):
		return BizResetPassword
	case strings.Contains(joined, "enable"):
		return BizEnable
	case strings.Contains(joined, "disable"):
		return BizDisable
	case strings.Contains(joined, "import"):
		return BizImport
	case strings.Contains(joined, "export"):
		return BizExport
	case strings.Contains(joined, "deploy"):
		return BizDeploy
	case strings.Contains(joined, "restore") || strings.Contains(joined, "revert"):
		return BizRestore
	case strings.Contains(joined, "copy") || strings.Contains(joined, "clone"):
		return BizCopy
	case strings.Contains(joined, "mark"):
		return BizMark
	case strings.Contains(joined, "bind"):
		return BizBindUNS
	case strings.Contains(joined, "save"):
		return BizSave
	case strings.Contains(joined, "install"):
		return BizInstall
	case strings.Contains(joined, "uninstall"):
		return BizUninstall
	}
	switch method {
	case http.MethodPost:
		return BizCreate
	case http.MethodPut, http.MethodPatch:
		return BizUpdate
	case http.MethodDelete:
		return BizDelete
	default:
		return ""
	}
}

func resourceIDFromPath(parts []string) string {
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		if lower == "create" || lower == "update" || lower == "delete" || lower == "enable" || lower == "disable" ||
			lower == "import" || lower == "export" || lower == "deploy" || lower == "save" || lower == "reset" ||
			lower == "list" || lower == "page" || lower == "version" || lower == "data" || lower == "status" ||
			lower == "content" || lower == "snapshots" || lower == "start" || lower == "stop" ||
			lower == "revert" || lower == "password" || lower == "test" || lower == "template" ||
			lower == "preview" || lower == "flowdata" || lower == "unsbindflow" || lower == "heartbeat" {
			continue
		}
		return truncateString(part, 128)
	}
	return ""
}

func resourceNameFromPath(parts []string) string {
	id := resourceIDFromPath(parts)
	if id == "" {
		return strings.Join(parts, "/")
	}
	return id
}

func resourceNameFromPayload(payload map[string]any) string {
	if len(payload) == 0 {
		return ""
	}
	keys := []string{
		"name",
		"flowName",
		"userName",
		"username",
		"nickName",
		"clientName",
		"apiKeyName",
		"tokenName",
		"nodeName",
		"userId",
		"resourceKey",
		"routeKey",
		"fileName",
		"originalName",
		"attachmentName",
		"versionName",
		"newName",
		"keyName",
		"title",
		"label",
		"namespace",
		"alias",
		"path",
	}
	for _, key := range keys {
		if value := payloadStringValue(payload, key); value != "" {
			return truncateString(value, 256)
		}
	}
	return ""
}

func payloadStringValue(payload map[string]any, key string) string {
	for currentKey, value := range payload {
		if !strings.EqualFold(strings.TrimSpace(currentKey), key) {
			continue
		}
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed)
		case json.Number:
			return strings.TrimSpace(typed.String())
		case float64:
			if typed == float64(int64(typed)) {
				return strconv.FormatInt(int64(typed), 10)
			}
			return strconv.FormatFloat(typed, 'f', -1, 64)
		case int64:
			return strconv.FormatInt(typed, 10)
		case int:
			return strconv.Itoa(typed)
		}
	}
	return ""
}

func shouldRecordAudit(method string, record *RecordInput, explicit bool) bool {
	if record == nil {
		return false
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		// 显式埋点(如敏感下载的 View)允许记录;自动审计只对登录放行 GET。
		return explicit || (strings.TrimSpace(record.ResType) == ResTypeAuth && strings.TrimSpace(record.BusinessType) == BizLogin)
	}
	return strings.TrimSpace(record.ResType) != "" && strings.TrimSpace(record.BusinessType) != ""
}

func normalizeRecord(input RecordInput) RecordInput {
	if input.ScopeType == 0 {
		input.ScopeType = ScopeTypePlatform
	}
	input.ResType = strings.TrimSpace(input.ResType)
	input.ResID = strings.TrimSpace(input.ResID)
	input.ResName = strings.TrimSpace(input.ResName)
	input.BusinessType = strings.TrimSpace(input.BusinessType)
	if input.Detail == nil {
		input.Detail = map[string]any{}
	}
	return input
}

func showInRecentForRecord(record *RecordInput) int64 {
	if record == nil {
		return ShowInRecentNo
	}
	switch record.IsShowInRecent {
	case ShowInRecentYes, ShowInRecentNo:
		return record.IsShowInRecent
	}
	if hiddenRecentResourceType(record.ResType) {
		return ShowInRecentNo
	}
	return ShowInRecentYes
}

func hiddenRecentResourceType(resType string) bool {
	switch strings.TrimSpace(resType) {
	case ResTypeUNS,
		ResTypeTemplate,
		ResTypeLabel,
		ResTypeGroup,
		ResTypeDashboard,
		ResTypeSourceFlow,
		ResTypeEventFlow,
		ResTypeNotebook,
		ResTypeProject:
		return false
	default:
		return true
	}
}

func requestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	for _, key := range []string{"X-Request-Id", "X-Request-ID", "Traceparent"} {
		if value := strings.TrimSpace(r.Header.Get(key)); value != "" {
			return truncateString(value, 64)
		}
	}
	return ""
}

// clientIP 解析客户端 IP。trustProxyHeaders 为 false 时忽略 X-Forwarded-For/
// X-Real-IP 等可被调用方伪造的转发头，直接记录 socket 地址，避免未认证客户端
// 伪造来源污染审计/登录日志；仅在 Backend 部署于可信反向代理之后时开启。
func clientIP(r *http.Request, trustProxyHeaders bool) string {
	if r == nil {
		return ""
	}
	if trustProxyHeaders {
		// 外部代理必须覆盖这些头；默认直连部署仍只信任 TCP 对端。
		// X-Real-IP 由最后一跳代理覆写、不可伪造；X-Forwarded-For 为追加语义
		// （如 nginx $proxy_add_x_forwarded_for），取最后一段（最近的可信代理
		// 写入的地址），避免客户端自带伪造段被采信。
		if value := strings.TrimSpace(r.Header.Get("X-Real-IP")); value != "" {
			if ip := net.ParseIP(value); ip != nil {
				return ip.String()
			}
		}
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return truncateString(host, 64)
	}
	return truncateString(strings.TrimSpace(r.RemoteAddr), 64)
}

func sanitizeDetail(detail map[string]any) map[string]any {
	if len(detail) == 0 {
		return map[string]any{}
	}
	// 快照可能含具体 struct 或 []T，先归一化后递归脱敏，防止类型绕过字段过滤。
	raw, err := json.Marshal(detail)
	if err != nil {
		return map[string]any{}
	}
	var normalized map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&normalized) != nil {
		return map[string]any{}
	}
	sanitized, ok := sanitizeDetailValue(normalized).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return sanitized
}

func sanitizeDetailValue(value any) any {
	switch item := value.(type) {
	case map[string]any:
		sanitized := make(map[string]any, len(item))
		for key, child := range item {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "credentials", "passphrase", "enrollmentcode", "privatekey", "signature", "password", "passwords", "newpassword", "client_secret", "clientsecret", "authorization", "token", "ticket", "cookie", "license", "licensetoken", "license_token", "email", "phone":
				continue
			default:
				sanitized[key] = sanitizeDetailValue(child)
			}
		}
		return sanitized
	case []any:
		sanitized := make([]any, len(item))
		for index, child := range item {
			sanitized[index] = sanitizeDetailValue(child)
		}
		return sanitized
	default:
		return value
	}
}

func marshalDetail(detail map[string]any) string {
	raw, err := json.Marshal(detail)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func truncateString(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func toPascal(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	seps := func(r rune) bool { return r == '-' || r == '_' || r == ' ' }
	parts := strings.FieldsFunc(value, seps)
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

func parseTime(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return n
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UnixMilli()
	}
	return 0
}

var ErrNotFound = errors.New("audit log not found")
