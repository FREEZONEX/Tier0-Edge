package launchpad

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"backend/internal/config"
	"backend/internal/domain/appstatus"
	"backend/internal/httpx"
	"backend/internal/repo"
)

const (
	recentAuditLimit = 50
	recentMaxCount   = 8
)

type Service struct {
	projects  *repo.ProjectRepo
	favorites *repo.UserFavoriteRepo
	audit     *repo.AuditRepo
	// appGateway 供读路径按请求 Host 重建生成型入口地址；bootstrap 构造后注入，
	// 零值（测试/未注入）时重建为安全 no-op。
	appGateway config.AppGatewayConfig
	// shortcutIconSecret 给「添加到主屏幕」的免登录取图地址签名；见 shortcuticon.go。
	shortcutIconSecret string
}

// SetAppGatewayConfig 注入应用网关配置（bootstrap 在构造后补注入）。
func (s *Service) SetAppGatewayConfig(c config.AppGatewayConfig) {
	if s != nil {
		s.appGateway = c
	}
}

type App struct {
	AppID       int64  `json:"appId"`
	AppName     string `json:"appName"`
	Display     string `json:"displayName"`
	Description string `json:"description"`
	// 部署状态:success / deploying / failed / stopped / pending,口径与项目域一致
	DeployStatus       string `json:"deployStatus"`
	IconURL            string `json:"iconUrl"`
	CoverURL           string `json:"coverUrl"`
	URL                string `json:"url"`
	SiteType           string `json:"siteType"`
	AppType            string `json:"appType"`
	Manual             bool   `json:"manual"`
	OpenInPlatform     bool   `json:"openInPlatform"`
	CreatedBy          string `json:"createdBy"`
	CreatedAt          int64  `json:"createdAt"`
	UpdatedAt          int64  `json:"updatedAt"`
	IsFavorite         bool   `json:"isFavorite"`
	ProjectName        string `json:"projectName"`
	ProjectDisplayName string `json:"projectDisplayName"`
}

type Project struct {
	ProjectID   int64  `json:"projectId"`
	ProjectName string `json:"projectName"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	CanEdit     bool   `json:"canEdit"`
	Apps        []App  `json:"apps"`
	CreatedBy   string `json:"createdBy"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type MembersQuery struct {
	ProjectName    string
	RoleKey        string
	Roles          []string
	UpdatedAtStart string
	UpdatedAtEnd   string
	Page           int64
	Size           int64
}

type RoleApp struct {
	AppID   string `json:"appId"`
	AppName string `json:"appName"`
}

type MemberRole struct {
	RoleID      string    `json:"roleId"`
	RoleKey     string    `json:"roleKey"`
	RoleName    string    `json:"roleName,omitempty"`
	Description string    `json:"description,omitempty"`
	MemberCount int64     `json:"memberCount"`
	Apps        []RoleApp `json:"apps,omitempty"`
}

type Member struct {
	UserID    string       `json:"userId"`
	UserName  string       `json:"userName,omitempty"`
	Email     string       `json:"email,omitempty"`
	Roles     []MemberRole `json:"roles"`
	UpdatedAt string       `json:"updatedAt,omitempty"`
}

type MembersPage struct {
	List  []Member `json:"list"`
	Total int64    `json:"total"`
	Page  int64    `json:"page"`
	Size  int64    `json:"size"`
}

func New(ctx context.Context, store *repo.Store) *Service {
	if store != nil {
		return &Service{
			projects:  repo.NewProjectRepo(store),
			favorites: repo.NewUserFavoriteRepo(store),
			audit:     repo.NewAuditRepo(store),
		}
	}
	return &Service{
		projects:  repo.NewProjectRepo(ctx),
		favorites: repo.NewUserFavoriteRepo(ctx),
		audit:     repo.NewAuditRepo(ctx),
	}
}

func (s *Service) Projects(ctx context.Context, userID int64, keyword string) ([]Project, error) {
	items, err := s.projects.ListProjects(ctx, keyword, userID, false)
	if err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(items))
	for _, item := range items {
		apps, err := s.apps(ctx, item, userID, "")
		if err != nil {
			return nil, err
		}
		out = append(out, projectResp(item, apps, userID))
	}
	return out, nil
}

func (s *Service) Project(ctx context.Context, userID, projectID int64, keyword string) (map[string]any, error) {
	project, _, err := s.requireProjectAccess(ctx, projectID, userID)
	if err != nil {
		return nil, err
	}
	apps, err := s.apps(ctx, project, userID, keyword)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"project": projectItemResp(project, len(apps)),
		"apps":    apps,
		"canEdit": canEditProject(project, userID),
	}, nil
}

func (s *Service) App(ctx context.Context, userID, projectID, appID int64) (map[string]any, error) {
	project, _, err := s.requireProjectAccess(ctx, projectID, userID)
	if err != nil {
		return nil, err
	}
	apps, err := s.apps(ctx, project, userID, "")
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		if app.AppID == appID {
			return map[string]any{"project": projectResp(project, apps, userID), "app": app}, nil
		}
	}
	// 未在「角色可见」列表中命中：用户是项目成员，需进一步区分
	//   - App 存在且 Active（仅被角色过滤掉）→ 缺角色 → ErrAppRoleRequired（App access required）
	//   - 否则（不存在 / 已删除 / 未 Active）→ ErrNotFound（App not found，不披露存在性）
	allApps, err := s.projects.ListProjectApps(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	for _, item := range allApps {
		if item.ID == appID && item.Status == "active" {
			return nil, ErrAppRoleRequired
		}
	}
	return nil, ErrNotFound
}

func (s *Service) requireProjectAccess(ctx context.Context, projectID, userID int64) (repo.Project, repo.ProjectMember, error) {
	project, err := s.projects.GetProject(ctx, projectID)
	if errors.Is(err, repo.ErrNotFound) {
		return repo.Project{}, repo.ProjectMember{}, ErrNotFound
	}
	if err != nil {
		return repo.Project{}, repo.ProjectMember{}, err
	}
	// 仅项目成员可访问 launchpad 项目/应用详情，owner 非成员同样不放行
	member, err := s.projects.GetEffectiveProjectMember(ctx, projectID, userID)
	if errors.Is(err, repo.ErrNotFound) {
		return repo.Project{}, repo.ProjectMember{}, ErrNotFound
	}
	if err != nil {
		return repo.Project{}, repo.ProjectMember{}, err
	}
	return project, member, nil
}

func (s *Service) Members(ctx context.Context, query MembersQuery) (MembersPage, error) {
	projectName := strings.TrimSpace(query.ProjectName)
	if projectName == "" {
		return MembersPage{}, fmt.Errorf("%w: project.name.required", repo.ErrInvalidArgument)
	}
	project, err := s.projects.GetProjectByNameOrID(ctx, projectName)
	if errors.Is(err, repo.ErrNotFound) {
		return MembersPage{}, ErrNotFound
	}
	if err != nil {
		return MembersPage{}, err
	}

	page, size := normalizeLaunchpadMembersPage(query.Page, query.Size)
	updatedAtStart, err := parseLaunchpadTime(query.UpdatedAtStart, "updatedAtStart.invalid")
	if err != nil {
		return MembersPage{}, err
	}
	updatedAtEnd, updatedAtEndExclusive, err := parseLaunchpadUpdatedAtEnd(query.UpdatedAtEnd)
	if err != nil {
		return MembersPage{}, err
	}

	members, err := s.projects.ListEffectiveProjectMembers(ctx, project.ID)
	if err != nil {
		return MembersPage{}, err
	}

	roleKeys := normalizeLaunchpadMemberRoleKeys(query.RoleKey, query.Roles)
	filtered := make([]repo.ProjectMember, 0, len(members))
	for _, member := range members {
		if !launchpadMemberRoleMatches(member.Roles, roleKeys) {
			continue
		}
		if !launchpadMemberUpdatedAtMatches(member.UpdatedTime, updatedAtStart, updatedAtEnd, updatedAtEndExclusive) {
			continue
		}
		filtered = append(filtered, member)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].UpdatedTime == filtered[j].UpdatedTime {
			return filtered[i].ID < filtered[j].ID
		}
		return filtered[i].UpdatedTime > filtered[j].UpdatedTime
	})

	total := int64(len(filtered))
	start := total
	if total > 0 && page <= (total-1)/size+1 {
		start = (page - 1) * size
	}
	end := start
	if size < total-start {
		end += size
	} else {
		end = total
	}

	list := make([]Member, 0, end-start)
	roleDetails := map[int64]MemberRole{}
	if start < end {
		roleDetails, err = s.launchpadMemberRoleDetails(ctx, project.ID)
		if err != nil {
			return MembersPage{}, err
		}
	}
	for _, member := range filtered[start:end] {
		list = append(list, launchpadMemberResp(member, roleDetails))
	}
	return MembersPage{
		List:  list,
		Total: total,
		Page:  page,
		Size:  size,
	}, nil
}

func (s *Service) launchpadMemberRoleDetails(ctx context.Context, projectID int64) (map[int64]MemberRole, error) {
	roles, err := s.projects.ListProjectRoles(ctx, projectID)
	if err != nil {
		return nil, err
	}
	details := make(map[int64]MemberRole, len(roles))
	if len(roles) == 0 {
		return details, nil
	}

	roleIDs := make([]int64, 0, len(roles))
	for _, role := range roles {
		roleIDs = append(roleIDs, role.ID)
	}
	roleApps, err := s.projects.ListProjectRoleAppsByRoleIDs(ctx, projectID, roleIDs)
	if err != nil {
		return nil, err
	}
	apps, err := s.projects.ListProjectApps(ctx, projectID)
	if err != nil {
		return nil, err
	}
	appNames := make(map[int64]string, len(apps))
	for _, app := range apps {
		appNames[app.ID] = app.Name
	}
	appsByRole := make(map[int64][]RoleApp, len(roles))
	for _, binding := range roleApps {
		appName := strings.TrimSpace(appNames[binding.AppID])
		if appName == "" {
			continue
		}
		appsByRole[binding.RoleID] = append(appsByRole[binding.RoleID], RoleApp{
			AppID:   strconv.FormatInt(binding.AppID, 10),
			AppName: appName,
		})
	}
	for _, role := range roles {
		roleApps := appsByRole[role.ID]
		sort.Slice(roleApps, func(i, j int) bool {
			if roleApps[i].AppName == roleApps[j].AppName {
				return roleApps[i].AppID < roleApps[j].AppID
			}
			return roleApps[i].AppName < roleApps[j].AppName
		})
		details[role.ID] = MemberRole{
			RoleID:      strconv.FormatInt(role.ID, 10),
			RoleKey:     role.RoleKey,
			RoleName:    role.Name,
			Description: role.Description,
			MemberCount: role.MemberCount,
			Apps:        roleApps,
		}
	}
	return details, nil
}

func (s *Service) apps(ctx context.Context, project repo.Project, userID int64, keyword string) ([]App, error) {
	items, err := s.projects.ListProjectApps(ctx, project.ID)
	if err != nil {
		return nil, err
	}

	// 应用可见性只看项目成员与项目角色：非项目成员（含 owner 但非成员）在 launchpad 看不到任何应用
	member, err := s.projects.GetEffectiveProjectMember(ctx, project.ID, userID)
	if errors.Is(err, repo.ErrNotFound) {
		return []App{}, nil
	}
	if err != nil {
		return nil, err
	}
	userRoleIDs := make(map[int64]struct{})
	for _, r := range member.Roles {
		userRoleIDs[r.ID] = struct{}{}
	}

	appIDs := make([]int64, 0, len(items))
	for _, item := range items {
		appIDs = append(appIDs, item.ID)
	}
	roleApps, err := s.projects.ListProjectRoleAppIDsByAppIDs(ctx, project.ID, appIDs)
	if err != nil {
		return nil, err
	}
	appRoleMap := make(map[int64][]int64, len(roleApps))
	for _, ra := range roleApps {
		appRoleMap[ra.AppID] = append(appRoleMap[ra.AppID], ra.RoleID)
	}

	keyword = strings.ToLower(strings.TrimSpace(keyword))
	out := make([]App, 0, len(items))
	for _, item := range items {
		if item.Status != "active" {
			continue
		}
		boundRoleIDs := appRoleMap[item.ID]
		if len(boundRoleIDs) > 0 && !hasAnyRole(userRoleIDs, boundRoleIDs) {
			continue
		}
		app := appResp(ctx, s.appGateway, item, project)
		if keyword == "" || strings.Contains(strings.ToLower(app.AppName), keyword) || strings.Contains(strings.ToLower(app.Display), keyword) {
			out = append(out, app)
		}
	}
	return out, nil
}

func hasAnyRole(userRoles map[int64]struct{}, roleIDs []int64) bool {
	for _, id := range roleIDs {
		if _, ok := userRoles[id]; ok {
			return true
		}
	}
	return false
}

func projectResp(item repo.Project, apps []App, userID int64) Project {
	return Project{
		ProjectID:   item.ID,
		ProjectName: strconv.FormatInt(item.ID, 10),
		DisplayName: item.Name,
		Description: item.Description,
		CanEdit:     canEditProject(item, userID),
		Apps:        apps,
		CreatedBy:   strconv.FormatInt(item.CreatedBy, 10),
		CreatedAt:   timeMilli(item.CreatedTime),
		UpdatedAt:   timeMilli(item.UpdatedTime),
	}
}

func canEditProject(item repo.Project, userID int64) bool {
	return item.OwnerUserID == userID
}

func projectItemResp(item repo.Project, appNums int) map[string]any {
	return map[string]any{
		"id":          item.ID,
		"name":        item.Name,
		"displayName": item.Name,
		"description": item.Description,
		"status":      item.Status,
		"appNums":     appNums,
		"createdBy":   strconv.FormatInt(item.CreatedBy, 10),
		"createdAt":   timeMilli(item.CreatedTime),
		"updatedAt":   timeMilli(item.UpdatedTime),
	}
}

func appResp(ctx context.Context, gw config.AppGatewayConfig, item repo.ProjectApp, project repo.Project) App {
	manifest := map[string]any{}
	if len(item.ManifestJSON) > 0 {
		_ = json.Unmarshal(item.ManifestJSON, &manifest)
	}
	appType, _ := manifest["app_type"].(string)
	description, _ := manifest["description"].(string)
	deploying, _ := manifest["deploying"].(bool)
	deployError, _ := manifest["deploy_error"].(string)
	openInPlatform, _ := manifest["openInPlatform"].(bool)
	coverAssetID := coverAssetIDOf(item)
	manual := item.SiteType == "manualExternal" || appType == "manual"
	// 入口地址按当前请求 Host 动态重建：仅处理生成型 URL（换 IP / 双网卡后仍指向
	// 当前入口）；相对路径与手工外部地址（manual）可能落在网关端口段的任意
	// 域名上，必须原样透传
	url := firstNonEmpty(item.EntryURL, item.EntryPath, item.RoutePrefix)
	if !manual {
		url = httpx.RebuildGeneratedEntryURL(ctx, gw, url)
	}
	// 手工外部地址不经过平台部署,恒为 success(与项目域列表同口径)
	deployStatus := appstatus.DeployStatus(item.Status, deploying, deployError)
	if manual {
		deployStatus = "success"
	}
	return App{
		AppID:              item.ID,
		AppName:            strconv.FormatInt(item.ID, 10),
		Display:            item.Name,
		Description:        strings.TrimSpace(description),
		DeployStatus:       deployStatus,
		IconURL:            launchpadAppMediaURL(item.ID, MediaKindIcon, item.IconAssetID),
		CoverURL:           launchpadAppMediaURL(item.ID, MediaKindCover, coverAssetID),
		URL:                url,
		SiteType:           item.SiteType,
		AppType:            appType,
		Manual:             manual,
		OpenInPlatform:     !manual || openInPlatform,
		CreatedBy:          strconv.FormatInt(item.CreatedBy, 10),
		CreatedAt:          timeMilli(item.CreatedTime),
		UpdatedAt:          timeMilli(item.UpdatedTime),
		ProjectName:        strconv.FormatInt(project.ID, 10),
		ProjectDisplayName: project.Name,
	}
}

// MediaKind 是 Launchpad 允许按 App 取的媒体类型。
type MediaKind string

const (
	MediaKindIcon  MediaKind = "icon"
	MediaKindCover MediaKind = "cover"
)

// launchpadAppMediaURL 生成按 App 取图的地址：/api/core/launchpad/apps/{appID}/{icon|cover}。
// 刻意不再直接暴露 /api/core/assets/{assetID}/download —— 那条路径没有归属校验，
// 授权只看路由级权限，给 Launchpad 开放就等于允许按 id 猜测下载任意资产。
// 这里按 App 取图，服务端解析 assetID 并校验 App 对当前用户可见（见 AppMediaAssetID）。
func launchpadAppMediaURL(appID int64, kind MediaKind, assetID int64) string {
	if appID <= 0 || assetID <= 0 {
		return ""
	}
	return fmt.Sprintf("/api/core/launchpad/apps/%d/%s", appID, kind)
}

// coverAssetIDOf 从 App manifest 中解析封面资产 id，没有则返回 0。
func coverAssetIDOf(item repo.ProjectApp) int64 {
	if len(item.ManifestJSON) == 0 {
		return 0
	}
	manifest := map[string]any{}
	if err := json.Unmarshal(item.ManifestJSON, &manifest); err != nil {
		return 0
	}
	value, ok := manifest["coverAssetId"].(float64)
	if !ok {
		return 0
	}
	return int64(value)
}

// AppMediaAssetID 解析 App 的图标/封面资产 id，并校验该 App 对调用者可见。
// Launchpad 的图片端点只靠这一层做归属校验，operator 拿不到与自己无关的资产。
func (s *Service) AppMediaAssetID(ctx context.Context, userID, appID int64, kind MediaKind) (int64, error) {
	if userID <= 0 || appID <= 0 {
		return 0, repo.ErrInvalidArgument
	}
	app, err := s.projects.GetProjectAppByID(ctx, appID)
	if err != nil {
		return 0, err
	}
	if err := s.requireAppVisible(ctx, app.ProjectID, userID, app.ID); err != nil {
		return 0, err
	}
	var assetID int64
	switch kind {
	case MediaKindIcon:
		assetID = app.IconAssetID
	case MediaKindCover:
		assetID = coverAssetIDOf(app)
	default:
		return 0, repo.ErrInvalidArgument
	}
	if assetID <= 0 {
		return 0, ErrNotFound
	}
	return assetID, nil
}

func timeMilli(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixMilli()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeLaunchpadMembersPage(page, size int64) (int64, int64) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func normalizeLaunchpadMemberRoleKeys(roleKey string, roles []string) map[string]struct{} {
	result := make(map[string]struct{}, len(roles)+1)
	for _, value := range append([]string{roleKey}, roles...) {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func launchpadMemberRoleMatches(roles []repo.ProjectRole, roleKeys map[string]struct{}) bool {
	if len(roleKeys) == 0 {
		return true
	}
	for _, role := range roles {
		if _, ok := roleKeys[strings.ToLower(strings.TrimSpace(role.RoleKey))]; ok {
			return true
		}
	}
	return false
}

func parseLaunchpadTime(value, message string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", repo.ErrInvalidArgument, message)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func parseLaunchpadUpdatedAtEnd(value string) (*time.Time, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, false, fmt.Errorf("%w: updatedAtEnd.invalid", repo.ErrInvalidArgument)
	}
	parsed = parsed.UTC()
	if !rfc3339HasFractionalSecond(value) {
		parsed = parsed.Add(time.Second)
		return &parsed, true, nil
	}
	return &parsed, false, nil
}

func rfc3339HasFractionalSecond(value string) bool {
	tIndex := strings.IndexByte(value, 'T')
	if tIndex < 0 {
		return false
	}
	timePart := value[tIndex+1:]
	if zIndex := strings.IndexByte(timePart, 'Z'); zIndex >= 0 {
		timePart = timePart[:zIndex]
	} else {
		for i := len(timePart) - 1; i >= 0; i-- {
			if timePart[i] == '+' || timePart[i] == '-' {
				timePart = timePart[:i]
				break
			}
		}
	}
	return strings.Contains(timePart, ".")
}

func launchpadMemberUpdatedAtMatches(value int64, start, end *time.Time, endExclusive bool) bool {
	if start == nil && end == nil {
		return true
	}
	updatedAt := time.UnixMilli(value).UTC()
	if start != nil && updatedAt.Before(*start) {
		return false
	}
	if end != nil {
		if endExclusive {
			return updatedAt.Before(*end)
		}
		return !updatedAt.After(*end)
	}
	return true
}

func launchpadMemberResp(member repo.ProjectMember, roleDetails map[int64]MemberRole) Member {
	resp := Member{
		UserID:   strconv.FormatInt(member.UserID, 10),
		UserName: member.UserName,
		Email:    member.Email,
		Roles:    make([]MemberRole, 0, len(member.Roles)),
	}
	roles := append([]repo.ProjectRole(nil), member.Roles...)
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].RoleKey == roles[j].RoleKey {
			return roles[i].ID < roles[j].ID
		}
		return roles[i].RoleKey < roles[j].RoleKey
	})
	for _, role := range roles {
		detail, ok := roleDetails[role.ID]
		if !ok {
			detail = MemberRole{
				RoleID:      strconv.FormatInt(role.ID, 10),
				RoleKey:     role.RoleKey,
				RoleName:    role.Name,
				Description: role.Description,
			}
		}
		resp.Roles = append(resp.Roles, detail)
	}
	if member.UpdatedTime > 0 {
		resp.UpdatedAt = time.UnixMilli(member.UpdatedTime).UTC().Format(time.RFC3339)
	}
	return resp
}

// UserFavoriteVO 收藏对外展示结构。
type UserFavoriteVO struct {
	ResType     string `json:"resType"`
	ResID       string `json:"resId"`
	ResName     string `json:"resName"`
	CreatedTime int64  `json:"createdTime"`
}

// Favorites 返回当前用户收藏列表，可按 resType 过滤。
func (s *Service) Favorites(ctx context.Context, userID int64, resType string) ([]UserFavoriteVO, error) {
	if userID <= 0 {
		return nil, repo.ErrInvalidArgument
	}
	rows, err := s.favorites.List(ctx, repo.UserFavoriteFilter{UserID: userID, ResType: resType})
	if err != nil {
		return nil, err
	}
	out := make([]UserFavoriteVO, 0, len(rows))
	for _, r := range rows {
		out = append(out, UserFavoriteVO{
			ResType:     r.ResType,
			ResID:       r.ResID,
			ResName:     r.ResName,
			CreatedTime: r.CreatedTime,
		})
	}
	return out, nil
}

// BatchAddFavorites 批量新增用户收藏。
func (s *Service) BatchAddFavorites(ctx context.Context, userID int64, items []repo.UserFavorite) error {
	if userID <= 0 {
		return repo.ErrInvalidArgument
	}
	return s.favorites.BatchInsert(ctx, userID, items)
}

// BatchDeleteFavorites 批量删除用户收藏。
func (s *Service) BatchDeleteFavorites(ctx context.Context, userID int64, items []repo.UserFavorite) error {
	if userID <= 0 {
		return repo.ErrInvalidArgument
	}
	return s.favorites.BatchDelete(ctx, userID, items)
}

// RecordAppView 校验应用访问权限，供 handler 在记录 View 审计前调用。
func (s *Service) RecordAppView(ctx context.Context, userID, appID int64) error {
	if userID <= 0 || appID <= 0 {
		return repo.ErrInvalidArgument
	}
	app, err := s.projects.GetProjectAppByID(ctx, appID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if app.Status != "active" {
		return ErrNotFound
	}
	if _, _, err := s.requireProjectAccess(ctx, app.ProjectID, userID); err != nil {
		return err
	}
	// 还需要校验角色绑定（复用 apps 中的权限逻辑）
	if err := s.requireAppVisible(ctx, app.ProjectID, userID, app.ID); err != nil {
		return err
	}
	return nil
}

// requireAppVisible 校验用户是否能看到指定应用（项目成员 + 角色绑定）。
func (s *Service) requireAppVisible(ctx context.Context, projectID, userID, appID int64) error {
	member, err := s.projects.GetEffectiveProjectMember(ctx, projectID, userID)
	if err != nil {
		return err
	}
	userRoleIDs := make(map[int64]struct{})
	for _, r := range member.Roles {
		userRoleIDs[r.ID] = struct{}{}
	}
	roleApps, err := s.projects.ListProjectRoleAppIDsByAppIDs(ctx, projectID, []int64{appID})
	if err != nil {
		return err
	}
	if len(roleApps) == 0 {
		return nil
	}
	for _, ra := range roleApps {
		if ra.AppID == appID {
			if _, ok := userRoleIDs[ra.RoleID]; ok {
				return nil
			}
		}
	}
	return ErrNotFound
}

// RecentApps 返回用户最近打开的应用，最多 8 条，按“收藏优先 + 查看时间倒序”排列。
func (s *Service) RecentApps(ctx context.Context, userID int64) ([]App, error) {
	if userID <= 0 {
		return nil, repo.ErrInvalidArgument
	}

	// 置顶优先:先取用户置顶的 App(即使从未打开过),再用最近打开记录补齐,总数上限 recentMaxCount
	pinned, err := s.favorites.List(ctx, repo.UserFavoriteFilter{UserID: userID, ResType: PinnedResTypeApp})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(pinned, func(i, j int) bool { return pinned[i].CreatedTime > pinned[j].CreatedTime })

	logs, err := s.audit.ListRecentAppViews(ctx, userID, recentAuditLimit)
	if err != nil {
		return nil, err
	}
	viewedAt := make(map[int64]time.Time, len(logs))
	viewOrder := make([]int64, 0, len(logs))
	for _, log := range logs {
		id, err := strconv.ParseInt(strings.TrimSpace(log.ResID), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := viewedAt[id]; !ok {
			viewedAt[id] = log.CreatedTime
			viewOrder = append(viewOrder, id)
		}
	}

	type candidate struct {
		appID  int64
		pinned bool
		// 置顶应用取置顶时间与最近打开时间的较大值,仅最近打开的取打开时间
		sortTime int64
	}
	candidates := make([]candidate, 0, len(pinned)+len(viewOrder))
	seen := make(map[int64]struct{}, len(pinned)+len(viewOrder))
	for _, fav := range pinned {
		id, err := strconv.ParseInt(strings.TrimSpace(fav.ResID), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ts := fav.CreatedTime
		if t, ok := viewedAt[id]; ok {
			if vt := t.UTC().UnixMilli(); vt > ts {
				ts = vt
			}
		}
		candidates = append(candidates, candidate{appID: id, pinned: true, sortTime: ts})
	}
	for _, id := range viewOrder {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		candidates = append(candidates, candidate{appID: id, pinned: false, sortTime: viewedAt[id].UTC().UnixMilli()})
	}

	out := make([]App, 0, recentMaxCount)
	for _, it := range candidates {
		if len(out) >= recentMaxCount {
			break
		}
		app, err := s.projects.GetProjectAppByID(ctx, it.appID)
		if err != nil {
			continue
		}
		if app.Status != "active" {
			continue
		}
		if err := s.requireAppVisible(ctx, app.ProjectID, userID, app.ID); err != nil {
			continue
		}
		project, err := s.projects.GetProject(ctx, app.ProjectID)
		if err != nil {
			continue
		}
		vo := appResp(ctx, s.appGateway, app, project)
		vo.UpdatedAt = it.sortTime
		vo.IsFavorite = it.pinned
		out = append(out, vo)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsFavorite != out[j].IsFavorite {
			return out[i].IsFavorite
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

// Launchpad 置顶（Pinned）的 resType，与前端 pages/launchpad/pinned-res-type.ts、移动端
// 以及云端 saas 保持同一套取值：三端共用 sys_user_favorite 存储，只靠 res_type 区分。
// 存量的小写 app / project 记录由 V2.2.4_002__backfill_launchpad_pinned.sql 复制过来；
// 改名必须同步前端、移动端与该迁移脚本，否则用户置顶会静默丢失。
const (
	PinnedResTypeApp     = "LaunchpadPinnedApp"
	PinnedResTypeProject = "LaunchpadPinnedProject"
)

var ErrNotFound = errors.New("launchpad item not found")

// ErrAppRoleRequired 表示 App 存在且 Active、用户是项目成员，但缺少 App 绑定的 Business Role。
// 与 ErrNotFound 区分：前者对应「App access required」页，后者对应「App not found」（不披露存在性）。
var ErrAppRoleRequired = errors.New("launchpad app role required")
