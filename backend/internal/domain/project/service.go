package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"backend/internal/appgateway"
	"backend/internal/config"
	"backend/internal/domain/appstatus"
	domainiam "backend/internal/domain/iam"
	"backend/internal/domain/license"
	"backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/service/tenantpg"

	"github.com/zeromicro/go-zero/core/logx"
)

type Service struct {
	projects   *repo.ProjectRepo
	uns        *repo.UnsRepo
	flows      *repo.FlowRepo
	iam        *domainiam.Service
	license    *license.Service
	lifecycle  *tenantpg.Lifecycle
	baseDSN    string
	roleSecret string
	// appGateway 供读路径按请求 Host 重建生成型入口地址；bootstrap 构造后注入，
	// 零值（测试/未注入）时重建为安全 no-op。
	appGateway config.AppGatewayConfig
}

// SetAppGatewayConfig 注入应用网关配置（bootstrap 在构造后补注入）。
func (s *Service) SetAppGatewayConfig(c config.AppGatewayConfig) {
	if s != nil {
		s.appGateway = c
	}
}

type SaveCommand struct {
	ID          int64
	Name        string
	Description string
	UserID      int64
}

type AppImportCommand struct {
	ProjectID int64
	AppName   string
	Version   string
	Manifest  map[string]any
	FileID    int64
	UserID    int64
	Status    string
}

type ManualAppCommand struct {
	ProjectID      int64
	Name           string
	Description    string
	AccessMode     string
	ContainerName  string
	ContainerPort  int
	TargetPath     string
	EntryURL       string
	OpenInPlatform bool
	IconAssetID    int64
	UserID         int64
}

type AppMetadataCommand struct {
	ProjectID    int64
	AppID        int64
	Name         string
	Description  string
	IconAssetID  int64
	CoverAssetID int64
	UserID       int64
}

type MemberSaveCommand struct {
	ProjectID      int64
	MemberID       int64
	UserID         int64
	RoleIDs        []int64
	OperatorUserID int64
}

type MemberGroupSaveCommand struct {
	ProjectID      int64
	ProjectGroupID int64
	GroupID        int64
	RoleIDs        []int64
	OperatorUserID int64
}

type ResourceRoots struct {
	UnsRoot        repo.UnsNode
	SourceFlowRoot repo.Flow
	EventFlowRoot  repo.Flow
}

func New(ctx context.Context, lifecycle *tenantpg.Lifecycle, baseDSN, roleSecret string, iamSvc *domainiam.Service) *Service {
	return &Service{
		projects:   repo.NewProjectRepo(ctx),
		uns:        repo.NewUnsRepo(ctx),
		flows:      repo.NewFlowRepo(ctx),
		iam:        iamSvc,
		license:    license.Default(),
		lifecycle:  lifecycle,
		baseDSN:    baseDSN,
		roleSecret: roleSecret,
	}
}

func projectDatabaseName(projectID int64) string {
	return fmt.Sprintf("project_%d", projectID)
}

func appSchemaName(appID int64) string {
	return fmt.Sprintf("app_%d", appID)
}

func (s *Service) isAdmin(ctx context.Context, userID int64) bool {
	if s.iam == nil {
		return false
	}
	return s.iam.IsAdmin(ctx, userID)
}

func (s *Service) List(ctx context.Context, keyword string, userID int64) (map[string]any, error) {
	items, err := s.projects.ListProjects(ctx, keyword, userID, s.isAdmin(ctx, userID))
	if err != nil {
		return nil, err
	}
	list := make([]any, 0, len(items))
	for _, item := range items {
		list = append(list, map[string]any{
			"id":          item.ID,
			"projectKey":  item.ProjectKey,
			"name":        item.Name,
			"description": item.Description,
			"status":      item.Status,
			"ownerUserId": item.OwnerUserID,
			"appNums":     item.AppNums,
			"createdBy":   formatProjectCreator(item.CreatorNickName, item.CreatorUserName),
			"createdTime": item.CreatedTime,
			"updatedTime": item.UpdatedTime,
			"createdAt":   item.CreatedTime,
			"updatedAt":   item.UpdatedTime,
		})
	}
	return map[string]any{"list": list, "total": len(list)}, nil
}

func (s *Service) CountApps(ctx context.Context) (int64, error) {
	return s.projects.CountProjectApps(ctx)
}

func (s *Service) Detail(ctx context.Context, id, userID int64) (map[string]any, error) {
	item, err := s.projects.GetProject(ctx, id)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if err := s.requireProjectAccess(ctx, item, userID); err != nil {
		return nil, err
	}
	return map[string]any{"project": item}, nil
}

func (s *Service) Create(ctx context.Context, cmd SaveCommand) (map[string]any, error) {
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		return nil, ErrInvalid
	}
	req := repo.Project{
		Name:        name,
		Description: cmd.Description,
		OwnerUserID: cmd.UserID,
	}
	req.CreatedBy = cmd.UserID
	item, err := s.projects.CreateProject(ctx, req)
	if err != nil {
		return nil, normalizeProjectSaveError(err)
	}

	// 创建 project 级 database
	databaseName := projectDatabaseName(item.ID)
	if s.lifecycle != nil {
		if err := s.lifecycle.CreateDatabase(ctx, databaseName); err != nil {
			// 回滚项目记录
			if delErr := s.projects.DeleteProject(context.WithoutCancel(ctx), item.ID, cmd.UserID); delErr != nil {
				return nil, fmt.Errorf("create tenant database failed: %w; rollback project also failed: %v", err, delErr)
			}
			return nil, fmt.Errorf("create tenant database: %w", err)
		}
		if err := s.projects.UpdateProjectDatabaseName(ctx, item.ID, databaseName); err != nil {
			return nil, fmt.Errorf("update project database name: %w", err)
		}
	}

	// 创建者默认成为 project 成员
	if _, err := s.projects.UpsertProjectMember(ctx, repo.ProjectMemberSave{
		ProjectID: item.ID,
		UserID:    cmd.UserID,
		RoleIDs:   []int64{},
	}); err != nil {
		return nil, fmt.Errorf("add creator as member: %w", err)
	}

	item.DatabaseName = databaseName
	return map[string]any{"project": item}, nil
}

func (s *Service) Update(ctx context.Context, cmd SaveCommand) (map[string]any, error) {
	if strings.TrimSpace(cmd.Name) == "" {
		return nil, ErrInvalid
	}
	project, err := s.projects.GetProject(ctx, cmd.ID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, cmd.UserID); err != nil {
		return nil, err
	}
	req := repo.Project{Name: strings.TrimSpace(cmd.Name), Description: cmd.Description}
	req.UpdatedBy = cmd.UserID
	item, err := s.projects.UpdateProject(ctx, cmd.ID, req)
	if err != nil {
		return nil, normalizeProjectSaveError(err)
	}
	return map[string]any{"project": item}, nil
}

func (s *Service) Delete(ctx context.Context, id, userID int64) (map[string]any, error) {
	project, err := s.projects.GetProject(ctx, id)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, userID); err != nil {
		return nil, err
	}

	// 项目下仍有应用时不允许删除
	apps, err := s.projects.ListProjectApps(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list project apps: %w", err)
	}
	if len(apps) > 0 {
		return nil, ErrProjectHasApps
	}

	// 删除 project database
	if project.DatabaseName != "" && s.lifecycle != nil {
		if err := s.lifecycle.DropDatabase(ctx, project.DatabaseName); err != nil {
			return nil, fmt.Errorf("drop tenant database: %w", err)
		}
	}

	if err := s.projects.DeleteProject(ctx, id, userID); err != nil {
		return nil, normalizeNotFound(err)
	}
	return map[string]any{"deleted": true, "name": project.Name}, nil
}

func (s *Service) ListApps(ctx context.Context, projectID int64) ([]map[string]any, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, normalizeNotFound(err)
	}
	items, err := s.projects.ListProjectApps(ctx, projectID)
	if err != nil {
		return nil, err
	}
	appRoles := s.buildAppRoles(ctx, projectID, items)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, projectAppResp(ctx, s.appGateway, item, appRoles[item.ID]))
	}
	return out, nil
}

func (s *Service) buildAppRoles(ctx context.Context, projectID int64, apps []repo.ProjectApp) map[int64][]string {
	result := make(map[int64][]string, len(apps))
	if len(apps) == 0 {
		return result
	}
	appIDs := make([]int64, 0, len(apps))
	for _, app := range apps {
		appIDs = append(appIDs, app.ID)
	}
	roles, err := s.projects.ListProjectRoles(ctx, projectID)
	if err != nil {
		return result
	}
	roleNameMap := make(map[int64]string, len(roles))
	for _, role := range roles {
		name := strings.TrimSpace(role.Name)
		if name == "" {
			name = role.RoleKey
		}
		roleNameMap[role.ID] = name
	}
	roleApps, err := s.projects.ListProjectRoleAppIDsByAppIDs(ctx, projectID, appIDs)
	if err != nil {
		return result
	}
	for _, ra := range roleApps {
		name, ok := roleNameMap[ra.RoleID]
		if !ok || name == "" {
			continue
		}
		seen := false
		for _, existing := range result[ra.AppID] {
			if existing == name {
				seen = true
				break
			}
		}
		if !seen {
			result[ra.AppID] = append(result[ra.AppID], name)
		}
	}
	return result
}

func (s *Service) ListUns(ctx context.Context, projectID int64) ([]repo.UnsNode, error) {
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	root, err := s.ensureProjectUnsRoot(ctx, project, 0)
	if err != nil {
		return nil, err
	}
	return s.uns.ListUnsSubtree(ctx, root.ID)
}

func (s *Service) ListFlows(ctx context.Context, projectID int64) ([]repo.Flow, error) {
	if _, err := s.EnsureResourceRoots(ctx, projectID, 0); err != nil {
		return nil, normalizeNotFound(err)
	}
	apps, err := s.projects.ListProjectApps(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ids := collectProjectResourceIDs(apps, "flowIds")
	return s.flows.GetFlowsByIDs(ctx, ids)
}

func (s *Service) EnsureResourceRoots(ctx context.Context, projectID, userID int64) (ResourceRoots, error) {
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return ResourceRoots{}, normalizeNotFound(err)
	}
	return s.ensureResourceRootsForProject(ctx, project, userID)
}

// EnsureFlowRoots 仅确保项目的 source/event flow 根节点存在（UnsRoot 留零值）。
// App 导入的 UNS 统一落全局根，使用本方法避免顺带创建项目同名 UNS 根目录。
func (s *Service) EnsureFlowRoots(ctx context.Context, projectID, userID int64) (ResourceRoots, error) {
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return ResourceRoots{}, normalizeNotFound(err)
	}
	sourceRoot, err := s.ensureProjectFlowRoot(ctx, project, 1, userID)
	if err != nil {
		return ResourceRoots{}, err
	}
	eventRoot, err := s.ensureProjectFlowRoot(ctx, project, 2, userID)
	if err != nil {
		return ResourceRoots{}, err
	}
	return ResourceRoots{SourceFlowRoot: sourceRoot, EventFlowRoot: eventRoot}, nil
}

// EnsureFlowRoot 仅确保项目指定 flowType（1=source，2=event）的 flow 根节点存在。
// 供导入链路延迟调用：全部 Flow 命中更新、本地零新建时不产生任何根记录副作用。
func (s *Service) EnsureFlowRoot(ctx context.Context, projectID int64, flowType int, userID int64) (repo.Flow, error) {
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return repo.Flow{}, normalizeNotFound(err)
	}
	return s.ensureProjectFlowRoot(ctx, project, flowType, userID)
}

func (s *Service) ensureResourceRootsForProject(ctx context.Context, project repo.Project, userID int64) (ResourceRoots, error) {
	unsRoot, err := s.ensureProjectUnsRoot(ctx, project, userID)
	if err != nil {
		return ResourceRoots{}, err
	}
	sourceRoot, err := s.ensureProjectFlowRoot(ctx, project, 1, userID)
	if err != nil {
		return ResourceRoots{}, err
	}
	eventRoot, err := s.ensureProjectFlowRoot(ctx, project, 2, userID)
	if err != nil {
		return ResourceRoots{}, err
	}
	return ResourceRoots{UnsRoot: unsRoot, SourceFlowRoot: sourceRoot, EventFlowRoot: eventRoot}, nil
}

func (s *Service) ensureProjectUnsRoot(ctx context.Context, project repo.Project, userID int64) (repo.UnsNode, error) {
	if project.ID == 0 || strings.TrimSpace(project.Name) == "" {
		return repo.UnsNode{}, ErrInvalid
	}
	alias := projectUnsRootAlias(project.ID)
	if node, err := s.uns.GetUnsNodeByAlias(ctx, alias); err == nil {
		if node.Type != 1 || node.ParentID != 0 || node.TopicType != 0 {
			return repo.UnsNode{}, ErrInvalid
		}
		return node, nil
	} else if !errors.Is(err, repo.ErrNotFound) {
		return repo.UnsNode{}, err
	}
	name := strings.TrimSpace(project.Name)
	if node, err := s.uns.GetUnsNodeByNamespace(ctx, name); err == nil {
		if node.Type != 1 || node.ParentID != 0 || node.TopicType != 0 {
			return repo.UnsNode{}, ErrInvalid
		}
		return node, nil
	} else if !errors.Is(err, repo.ErrNotFound) {
		return repo.UnsNode{}, err
	}
	extendRaw, _ := json.Marshal(map[string]any{
		"projectId":   project.ID,
		"projectRoot": true,
	})
	nodeReq := repo.UnsNode{
		Name:             name,
		DisplayName:      name,
		Description:      project.Description,
		Namespace:        name,
		Alias:            alias,
		Type:             1,
		Schema:           json.RawMessage(`[]`),
		ExtendProperties: extendRaw,
	}
	nodeReq.CreatedBy = userID
	return s.uns.CreateUnsNode(ctx, nodeReq, nil, nil)
}

func (s *Service) ensureProjectFlowRoot(ctx context.Context, project repo.Project, flowType int, userID int64) (repo.Flow, error) {
	name := strings.TrimSpace(project.Name)
	if project.ID == 0 || name == "" || (flowType != 1 && flowType != 2) {
		return repo.Flow{}, ErrInvalid
	}
	existing, err := s.flows.GetFlowByName(ctx, flowType, 0, name)
	if err == nil {
		if existing.NodeType != 1 {
			return repo.Flow{}, ErrInvalid
		}
		return existing, nil
	}
	if !errors.Is(err, repo.ErrNotFound) {
		return repo.Flow{}, err
	}
	existingFolder, err := s.flows.GetFlowFolderByName(ctx, 0, name)
	if err == nil {
		return existingFolder, nil
	}
	if !errors.Is(err, repo.ErrNotFound) {
		return repo.Flow{}, err
	}
	flowReq := repo.Flow{
		Name:        name,
		Status:      "draft",
		Template:    "project",
		Description: project.Description,
		FlowType:    flowType,
		NodeType:    1,
		ParentID:    0,
	}
	flowReq.CreatedBy = userID
	flowReq.UpdatedBy = userID
	return s.flows.CreateFlow(ctx, flowReq)
}

func (s *Service) ListMembers(ctx context.Context, projectID int64) ([]repo.ProjectMember, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, normalizeNotFound(err)
	}
	return s.projects.ListProjectMembers(ctx, projectID)
}

func (s *Service) AddMember(ctx context.Context, cmd MemberSaveCommand) (repo.ProjectMember, error) {
	project, err := s.projects.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return repo.ProjectMember{}, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, cmd.OperatorUserID); err != nil {
		return repo.ProjectMember{}, err
	}
	if cmd.UserID == 0 {
		return repo.ProjectMember{}, ErrInvalid
	}
	item, err := s.projects.UpsertProjectMember(ctx, repo.ProjectMemberSave{
		ProjectID: cmd.ProjectID,
		UserID:    cmd.UserID,
		RoleIDs:   cmd.RoleIDs,
	})
	if err != nil {
		return repo.ProjectMember{}, normalizeNotFound(err)
	}
	// 成员角色变更，清空网关角色缓存，避免旧角色在 TTL 内继续生效
	appgateway.InvalidateRoleCache()
	return item, nil
}

func (s *Service) UpdateMember(ctx context.Context, cmd MemberSaveCommand) (repo.ProjectMember, error) {
	project, err := s.projects.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return repo.ProjectMember{}, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, cmd.OperatorUserID); err != nil {
		return repo.ProjectMember{}, err
	}
	if cmd.MemberID == 0 {
		return repo.ProjectMember{}, ErrInvalid
	}
	item, err := s.projects.UpdateProjectMember(ctx, repo.ProjectMemberSave{
		ProjectID: cmd.ProjectID,
		MemberID:  cmd.MemberID,
		RoleIDs:   cmd.RoleIDs,
	})
	if err != nil {
		return repo.ProjectMember{}, normalizeNotFound(err)
	}
	// 成员角色变更，清空网关角色缓存，避免旧角色在 TTL 内继续生效
	appgateway.InvalidateRoleCache()
	return item, nil
}

func (s *Service) DeleteMember(ctx context.Context, projectID, memberID, operatorUserID int64) error {
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, operatorUserID); err != nil {
		return err
	}
	if err := normalizeNotFound(s.projects.DeleteProjectMember(ctx, projectID, memberID)); err != nil {
		return err
	}
	// 成员删除，清空网关角色缓存，避免被移除成员在 TTL 内继续访问
	appgateway.InvalidateRoleCache()
	return nil
}

func (s *Service) ListMemberGroups(ctx context.Context, projectID int64) ([]repo.ProjectUserGroup, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, normalizeNotFound(err)
	}
	return s.projects.ListProjectUserGroups(ctx, projectID)
}

func (s *Service) AddMemberGroup(ctx context.Context, cmd MemberGroupSaveCommand) (repo.ProjectUserGroup, error) {
	project, err := s.projects.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return repo.ProjectUserGroup{}, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, cmd.OperatorUserID); err != nil {
		return repo.ProjectUserGroup{}, err
	}
	item, err := s.projects.UpsertProjectUserGroup(ctx, repo.ProjectUserGroupSave{ProjectID: cmd.ProjectID, GroupID: cmd.GroupID, RoleIDs: cmd.RoleIDs, UserID: cmd.OperatorUserID})
	if err != nil {
		return repo.ProjectUserGroup{}, normalizeNotFound(err)
	}
	appgateway.InvalidateRoleCache()
	return item, nil
}

func (s *Service) UpdateMemberGroup(ctx context.Context, cmd MemberGroupSaveCommand) (repo.ProjectUserGroup, error) {
	project, err := s.projects.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return repo.ProjectUserGroup{}, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, cmd.OperatorUserID); err != nil {
		return repo.ProjectUserGroup{}, err
	}
	item, err := s.projects.UpdateProjectUserGroup(ctx, repo.ProjectUserGroupSave{ProjectID: cmd.ProjectID, ProjectGroupID: cmd.ProjectGroupID, RoleIDs: cmd.RoleIDs, UserID: cmd.OperatorUserID})
	if err != nil {
		return repo.ProjectUserGroup{}, normalizeNotFound(err)
	}
	appgateway.InvalidateRoleCache()
	return item, nil
}

func (s *Service) DeleteMemberGroup(ctx context.Context, projectID, projectGroupID, operatorUserID int64) error {
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, operatorUserID); err != nil {
		return err
	}
	if err := s.projects.DeleteProjectUserGroup(ctx, projectID, projectGroupID); err != nil {
		return normalizeNotFound(err)
	}
	appgateway.InvalidateRoleCache()
	return nil
}

func (s *Service) ListRoles(ctx context.Context, projectID int64) ([]repo.ProjectRole, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, normalizeNotFound(err)
	}
	roles, err := s.projects.ListProjectRoles(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(roles) == 0 {
		return roles, nil
	}
	roleIDs := make([]int64, 0, len(roles))
	for _, r := range roles {
		roleIDs = append(roleIDs, r.ID)
	}
	apps, err := s.projects.ListProjectApps(ctx, projectID)
	if err != nil {
		return nil, err
	}
	appNameMap := make(map[int64]string, len(apps))
	for _, app := range apps {
		appNameMap[app.ID] = app.Name
	}
	roleApps, err := s.projects.ListProjectRoleAppsByRoleIDs(ctx, projectID, roleIDs)
	if err != nil {
		return nil, err
	}
	for i := range roles {
		roles[i].Apps = buildRoleApps(roleApps, appNameMap, roles[i].ID)
	}
	return roles, nil
}

func buildRoleApps(roleApps []repo.ProjectRoleApp, appNameMap map[int64]string, roleID int64) []repo.ProjectRoleAppItem {
	var items []repo.ProjectRoleAppItem
	seen := map[int64]struct{}{}
	for _, ra := range roleApps {
		if ra.RoleID != roleID {
			continue
		}
		name := strings.TrimSpace(appNameMap[ra.AppID])
		if name == "" {
			continue
		}
		if _, ok := seen[ra.AppID]; ok {
			continue
		}
		seen[ra.AppID] = struct{}{}
		items = append(items, repo.ProjectRoleAppItem{AppID: ra.AppID, AppName: name})
	}
	return items
}

func (s *Service) ImportApp(ctx context.Context, cmd AppImportCommand) (map[string]any, bool, error) {
	appName := strings.TrimSpace(cmd.AppName)
	if cmd.ProjectID == 0 || appName == "" {
		return nil, false, ErrInvalid
	}
	project, err := s.projects.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, false, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, cmd.UserID); err != nil {
		return nil, false, err
	}

	manifest, err := json.Marshal(cmd.Manifest)
	if err != nil {
		return nil, false, ErrInvalid
	}
	status := strings.TrimSpace(cmd.Status)
	if status == "" {
		status = "active"
	}
	item := repo.ProjectApp{
		ProjectID:     cmd.ProjectID,
		Name:          appName,
		SiteType:      "sameOriginStatic",
		StaticAssetID: cmd.FileID,
		ManifestJSON:  manifest,
		Status:        status,
	}
	item.CreatedBy = cmd.UserID
	item.UpdatedBy = cmd.UserID
	isNew := false
	// 按名称精确复用：同名 static 应用重新导入时更新原记录，避免 slugKey 冲突或不同名映射到同一 slug。
	// 非 static 应用（manual/dynamic）命中时禁止覆盖，返回名称重复错误。
	existing, err := s.projects.GetProjectAppByName(ctx, cmd.ProjectID, appName)
	if err == nil {
		if existing.SiteType != "sameOriginStatic" {
			return nil, false, ErrAppNameDuplicate
		}
		item.AppKey = existing.AppKey
		item, err = s.projects.UpdateProjectApp(ctx, existing.ID, item)
	} else if errors.Is(err, repo.ErrNotFound) {
		isNew = true
		item.AppKey = "app_" + repo.RandomHexForApp(8)
		if s.license != nil {
			count, countErr := s.projects.CountProjectApps(ctx)
			if countErr != nil {
				return nil, false, countErr
			}
			if quotaErr := s.license.CheckQuota("max_apps", count); quotaErr != nil {
				return nil, false, quotaErr
			}
		}
		item, err = s.projects.CreateProjectApp(ctx, item)
	}
	if err != nil {
		if errors.Is(err, repo.ErrDuplicate) {
			return nil, false, ErrAppNameDuplicate
		}
		return nil, false, err
	}
	return projectAppResp(ctx, s.appGateway, item, nil), isNew, nil
}

func (s *Service) CreateManualApp(ctx context.Context, cmd ManualAppCommand) (map[string]any, error) {
	name := strings.TrimSpace(cmd.Name)
	accessMode := normalizeManualAppAccessMode(cmd.AccessMode)
	if cmd.ProjectID == 0 || name == "" {
		return nil, ErrInvalid
	}
	entryURL := strings.TrimSpace(cmd.EntryURL)
	proxyTarget, proxyPort, err := manualAppProxyTarget(cmd)
	if err != nil {
		return nil, err
	}
	targetPath := normalizeManualAppTargetPath(cmd.TargetPath)
	if accessMode == "external" {
		if !validManualAppEntryURL(entryURL) {
			return nil, ErrInvalid
		}
	} else {
		entryURL = ""
	}
	project, err := s.projects.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, cmd.UserID); err != nil {
		return nil, err
	}
	if _, err := s.projects.GetProjectAppByName(ctx, cmd.ProjectID, name); err == nil {
		return nil, ErrAppNameDuplicate
	} else if !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	if s.license != nil {
		count, countErr := s.projects.CountProjectApps(ctx)
		if countErr != nil {
			return nil, countErr
		}
		if quotaErr := s.license.CheckQuota("max_apps", count); quotaErr != nil {
			return nil, quotaErr
		}
	}
	manifestMap := map[string]any{
		"app_type":       "manual",
		"description":    strings.TrimSpace(cmd.Description),
		"openInPlatform": cmd.OpenInPlatform,
		"accessMode":     accessMode,
	}
	if accessMode == "proxy" {
		manifestMap["containerName"] = strings.TrimSpace(cmd.ContainerName)
		manifestMap["containerPort"] = proxyPort
		manifestMap["targetPath"] = targetPath
	}
	manifest, err := json.Marshal(manifestMap)
	if err != nil {
		return nil, ErrInvalid
	}
	item := repo.ProjectApp{
		ProjectID:    cmd.ProjectID,
		AppKey:       "manual_" + repo.RandomHexForApp(8),
		Name:         name,
		SiteType:     "manualExternal",
		EntryURL:     entryURL,
		IconAssetID:  cmd.IconAssetID,
		ManifestJSON: json.RawMessage(manifest),
		Status:       "active",
	}
	item.CreatedBy = cmd.UserID
	item.UpdatedBy = cmd.UserID
	item, err = s.projects.CreateProjectApp(ctx, item)
	if err != nil {
		if errors.Is(err, repo.ErrDuplicate) {
			return nil, ErrAppNameDuplicate
		}
		return nil, err
	}
	if accessMode == "proxy" {
		routePrefix := manualAppProxyPrefix(item.ProjectID, item.ID)
		routeKey := manualAppGatewayRouteKey(item.ProjectID, item.ID)
		routeRepo := repo.NewGatewayRepo(ctx)
		if err := routeRepo.UpsertGatewayRoute(ctx, repo.GatewayRoute{
			RouteKey:    routeKey,
			Name:        "Manual App " + item.Name,
			Description: strings.TrimSpace(cmd.Description),
			TargetType:  "reverseProxy",
			MatchType:   "prefix",
			PathPattern: routePrefix + "/**",
			Methods:     []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
			TargetUrl:   proxyTarget,
			TargetPath:  targetPath,
			StripPrefix: true,
			AuthPolicy:  "login",
			ResourceKey: "launchpad.view",
			TimeoutMs:   10000,
			Priority:    120,
			Enabled:     true,
		}); err != nil {
			_ = s.projects.DeleteProjectApp(ctx, item.ProjectID, item.ID, cmd.UserID)
			return nil, err
		}
		item.EntryURL = routePrefix + "/"
		item.RoutePrefix = routePrefix
		item.GatewayRouteKey = routeKey
		item.UpdatedBy = cmd.UserID
		item, err = s.projects.UpdateProjectApp(ctx, item.ID, item)
		if err != nil {
			return nil, err
		}
	}
	return projectAppResp(ctx, s.appGateway, item, nil), nil
}

func (s *Service) UpdateAppMetadata(ctx context.Context, cmd AppMetadataCommand) (map[string]any, error) {
	name := strings.TrimSpace(cmd.Name)
	if cmd.ProjectID == 0 || cmd.AppID == 0 || name == "" {
		return nil, ErrInvalid
	}
	project, err := s.projects.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, cmd.UserID); err != nil {
		return nil, err
	}
	item, err := s.projects.UpdateProjectAppMetadata(
		ctx,
		cmd.ProjectID,
		cmd.AppID,
		name,
		strings.TrimSpace(cmd.Description),
		cmd.IconAssetID,
		cmd.CoverAssetID,
		cmd.UserID,
	)
	if err != nil {
		if errors.Is(err, repo.ErrDuplicate) {
			return nil, ErrAppNameDuplicate
		}
		return nil, normalizeNotFound(err)
	}
	return projectAppResp(ctx, s.appGateway, item, nil), nil
}

func (s *Service) UpdateAppStatus(ctx context.Context, projectID, appID int64, status string, userID int64) (map[string]any, error) {
	normalized := NormalizeAppStatus(status)
	if normalized == "" {
		return nil, ErrInvalid
	}
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, userID); err != nil {
		return nil, err
	}
	if normalized == "active" && s.license != nil {
		currentApp, appErr := s.projects.GetProjectApp(ctx, projectID, appID)
		if appErr != nil {
			return nil, normalizeNotFound(appErr)
		}
		// 若当前应用已经是 active 或 deploying，说明它已被计入配额，允许状态流转；
		// 若从 stopped/disabled/failed 恢复为 active，则需要重新校验配额。
		if currentApp.Status != "active" && currentApp.Status != "deploying" {
			count, countErr := s.projects.CountProjectApps(ctx)
			if countErr != nil {
				return nil, countErr
			}
			if quotaErr := s.license.CheckQuota("max_apps", count); quotaErr != nil {
				return nil, quotaErr
			}
		}
	}
	if err := s.projects.UpdateProjectAppStatus(ctx, projectID, appID, normalized, userID); err != nil {
		return nil, normalizeNotFound(err)
	}
	return map[string]any{"updated": true}, nil
}

func (s *Service) DeleteApp(ctx context.Context, projectID, appID, userID int64) (map[string]any, error) {
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, userID); err != nil {
		return nil, err
	}
	// 删除前先取应用记录：删除后需按 manifest flowIds/unsNodeIds 清理其导入的 Flow/UNS
	app, err := s.projects.GetProjectApp(ctx, projectID, appID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	roleApps, err := s.projects.ListProjectRoleAppIDsByAppIDs(ctx, projectID, []int64{appID})
	if err != nil {
		return nil, err
	}
	if err := s.projects.DeleteProjectRoleAppsByAppID(ctx, projectID, appID); err != nil {
		return nil, err
	}
	if err := s.projects.DeleteProjectApp(ctx, projectID, appID, userID); err != nil {
		return nil, normalizeNotFound(err)
	}
	if len(roleApps) > 0 {
		roleIDs := make([]int64, 0, len(roleApps))
		for _, ra := range roleApps {
			roleIDs = append(roleIDs, ra.RoleID)
		}
		if _, err := s.projects.DeleteProjectRolesIfNoApps(ctx, projectID, roleIDs); err != nil {
			return nil, err
		}
	}
	// 应用角色绑定已删除，清空网关角色缓存
	appgateway.InvalidateRoleCache()
	// 清理该 App 导入的 Flow/UNS 节点；仍被其他存活 App 引用的共享资源不删
	if err := s.CleanupAppImportedResources(ctx, app, userID); err != nil {
		logx.WithContext(ctx).Errorf("cleanup imported resources after app delete: projectID=%d appID=%d err=%v", projectID, appID, err)
	}
	return map[string]any{"deleted": true}, nil
}

// CleanupAppImportedResources 按 App manifest 记录清理其导入的 Flow 与 UNS 节点。
// 防误删：
//   - 新 manifest（带 createdFlowIds/createdUnsNodeIds）优先只删本 App 实际新建的资源：
//     被顶层 id 命中而更新的既有 Flow、覆盖更新的既有 UNS 节点（可能是其他 Project 手工创建/既有）
//     不在 created 集合内，绝不误删；
//   - 仍被其他存活 App（含其他 Project）manifest 引用的 Flow/UNS 节点不删；项目 UNS 根节点不删；
//   - 旧 manifest（无 created 字段）回退到全量 flowIds/unsNodeIds 引用集合（历史行为，存在误删
//     命中匹配外部资源的风险；新导入的 manifest 都会写入 created 集合）。
//
// 单个资源删除失败只记录日志，不阻断其余清理。
func (s *Service) CleanupAppImportedResources(ctx context.Context, app repo.ProjectApp, userID int64) error {
	manifest := map[string]any{}
	if len(app.ManifestJSON) > 0 {
		_ = json.Unmarshal(app.ManifestJSON, &manifest)
	}
	flowIDs := cleanupPreferCreated(jsonInt64Slice(manifest["createdFlowIds"]), jsonInt64Slice(manifest["flowIds"]))
	unsNodeIDs := cleanupPreferCreated(jsonInt64Slice(manifest["createdUnsNodeIds"]), jsonInt64Slice(manifest["unsNodeIds"]))
	if len(flowIDs) == 0 && len(unsNodeIDs) == 0 {
		return nil
	}
	others, err := s.projects.ListAllProjectAppManifests(ctx)
	if err != nil {
		return err
	}
	sharedFlowIDs, sharedUnsIDs := referencedResourceIDs(others, app.ID)
	logger := logx.WithContext(ctx)
	for _, id := range filterUnsharedResourceIDs(flowIDs, sharedFlowIDs) {
		if err := s.flows.DeleteFlow(ctx, id, userID); err != nil && !errors.Is(err, repo.ErrNotFound) {
			logger.Errorf("cleanup app imported flow %d failed: %v", id, err)
		}
	}
	// 项目 UNS 根节点常驻，不随 App 删除（只读查询，删除路径不触发根节点创建）
	var keepUnsIDs []int64
	if root, err := s.uns.GetUnsNodeByAlias(ctx, projectUnsRootAlias(app.ProjectID)); err == nil {
		keepUnsIDs = append(keepUnsIDs, root.ID)
	}
	for _, id := range filterUnsharedResourceIDs(unsNodeIDs, sharedUnsIDs, keepUnsIDs...) {
		if _, err := s.uns.DeleteUnsNode(ctx, id, userID); err != nil && !errors.Is(err, repo.ErrNotFound) {
			logger.Errorf("cleanup app imported uns node %d failed: %v", id, err)
		}
	}
	return nil
}

// referencedResourceIDs 统计其他存活 App（排除 excludeAppID）manifest 中引用的 flow/UNS 资源 ID 集合。
func referencedResourceIDs(apps []repo.ProjectApp, excludeAppID int64) (flowIDs, unsNodeIDs map[int64]struct{}) {
	flowIDs = map[int64]struct{}{}
	unsNodeIDs = map[int64]struct{}{}
	for _, app := range apps {
		if app.ID == excludeAppID {
			continue
		}
		manifest := map[string]any{}
		if len(app.ManifestJSON) > 0 {
			_ = json.Unmarshal(app.ManifestJSON, &manifest)
		}
		for _, id := range jsonInt64Slice(manifest["flowIds"]) {
			flowIDs[id] = struct{}{}
		}
		for _, id := range jsonInt64Slice(manifest["unsNodeIds"]) {
			unsNodeIDs[id] = struct{}{}
		}
	}
	return flowIDs, unsNodeIDs
}

// filterUnsharedResourceIDs 从 ids 中剔除仍被其他 App 引用的 ID 与指定保留 ID（如项目 UNS 根），
// 返回允许删除的子集（保持原顺序）。
func filterUnsharedResourceIDs(ids []int64, shared map[int64]struct{}, keepIDs ...int64) []int64 {
	keep := make(map[int64]struct{}, len(keepIDs))
	for _, id := range keepIDs {
		keep[id] = struct{}{}
	}
	var out []int64
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := shared[id]; ok {
			continue
		}
		if _, ok := keep[id]; ok {
			continue
		}
		out = append(out, id)
	}
	return out
}

// cleanupPreferCreated 删 App 清理资源时选择实际删除的 ID 集合：
// created 字段存在（含空数组，表示本 App 未新建任何资源，命中匹配的外部资源不误删）时只删
// created；仅当字段缺失（旧 manifest 无 created 字段）才回退全量引用集合（历史行为）。
// 纯函数，便于单测。
func cleanupPreferCreated(created, all []int64) []int64 {
	if created != nil {
		return created
	}
	return all
}

func (s *Service) UpsertRole(ctx context.Context, projectID int64, roleKey, name, description string, userID int64) (repo.ProjectRole, error) {
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return repo.ProjectRole{}, normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, userID); err != nil {
		return repo.ProjectRole{}, err
	}
	if strings.TrimSpace(roleKey) == "" {
		return repo.ProjectRole{}, ErrInvalid
	}
	if strings.TrimSpace(name) == "" {
		name = roleKey
	}
	return s.projects.UpsertProjectRole(ctx, repo.ProjectRole{
		ProjectID:   projectID,
		RoleKey:     strings.TrimSpace(roleKey),
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
	})
}

func (s *Service) PatchAppManifest(ctx context.Context, projectID, appID int64, patch map[string]any, userID int64) error {
	if projectID == 0 || appID == 0 {
		return ErrInvalid
	}
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return normalizeNotFound(err)
	}
	if err := s.requireProjectManage(ctx, project, userID); err != nil {
		return err
	}
	return s.projects.UpdateProjectAppManifest(ctx, projectID, appID, patch, userID)
}

func projectAppResp(ctx context.Context, gw config.AppGatewayConfig, item repo.ProjectApp, accessibleRoles []string) map[string]any {
	if accessibleRoles == nil {
		accessibleRoles = []string{}
	}
	manifest := map[string]any{}
	if len(item.ManifestJSON) > 0 {
		_ = json.Unmarshal(item.ManifestJSON, &manifest)
	}
	version := ""
	if value, ok := manifest["version"]; ok {
		version = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(jsonScalar(value)), "\""), "\""))
	}
	description := manifestString(manifest, "description", "")
	appType := manifestString(manifest, "app_type", "")
	openInPlatform, _ := manifest["openInPlatform"].(bool)
	manual := item.SiteType == "manualExternal" || appType == "manual"
	deploying, _ := manifest["deploying"].(bool)
	deployError, _ := manifest["deploy_error"].(string)
	deployStatus := deployStatusForList(item.Status, deploying, deployError)
	if manual {
		deployStatus = "success"
	}
	// 入口地址按当前请求 Host 动态重建：仅处理生成型 URL；手工外部地址可能
	// 落在网关端口段的任意域名上，必须原样透传
	entryURL := item.EntryURL
	if !manual {
		entryURL = httpx.RebuildGeneratedEntryURL(ctx, gw, item.EntryURL)
	}
	return map[string]any{
		"appId":             item.ID,
		"appPrincipalId":    item.AppPrincipalID,
		"projectId":         item.ProjectID,
		"appKey":            item.AppKey,
		"name":              item.Name,
		"displayName":       manifestString(manifest, "displayName", item.Name),
		"description":       description,
		"version":           version,
		"status":            AppStatusForFrontend(item.Status),
		"siteType":          item.SiteType,
		"appType":           appType,
		"manual":            manual,
		"openInPlatform":    openInPlatform,
		"url":               entryURL,
		"deployStatus":      deployStatus,
		"deployError":       deployError,
		"sourceAppId":       manifestString(manifest, "sourceAppId", ""),
		"sourceProjectId":   manifestString(manifest, "sourceProjectId", ""),
		"sourceWorkspaceId": manifestString(manifest, "sourceWorkspaceId", ""),
		"schemaName":        item.SchemaName,
		"networkAlias":      manifestString(manifest, "network_alias", ""),
		"internalUrl":       AppInternalURL(manifest),
		"accessibleRoles":   accessibleRoles,
		"staticUrl":         entryURL,
		"iconAssetId":       item.IconAssetID,
		"iconUrl":           projectAppIconURL(item.IconAssetID),
		"coverAssetId":      manifestInt64(manifest, "coverAssetId"),
		"coverUrl":          projectAppIconURL(manifestInt64(manifest, "coverAssetId")),
		"createdBy":         formatProjectCreator(item.CreatorNickName, item.CreatorUserName),
		"createdAt":         item.CreatedTime,
		"updatedAt":         item.UpdatedTime,
	}
}

func projectAppIconURL(iconAssetID int64) string {
	if iconAssetID <= 0 {
		return ""
	}
	return fmt.Sprintf("/api/core/assets/%d/download", iconAssetID)
}

// 部署状态与状态归一化的实现收在 domain/appstatus,launchpad 域也要用同一份口径
// (那边导不进 project:launchpad → project → iam → launchpad 会成环)。
func deployStatusForList(status string, deploying bool, deployError string) string {
	return appstatus.DeployStatus(status, deploying, deployError)
}

func collectProjectResourceIDs(apps []repo.ProjectApp, key string) []int64 {
	seen := map[int64]struct{}{}
	var ids []int64
	for _, app := range apps {
		manifest := map[string]any{}
		if len(app.ManifestJSON) > 0 {
			_ = json.Unmarshal(app.ManifestJSON, &manifest)
		}
		for _, id := range jsonInt64Slice(manifest[key]) {
			if id == 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

func validManualAppEntryURL(value string) bool {
	if strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func normalizeManualAppAccessMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "external") {
		return "external"
	}
	return "proxy"
}

func manualAppProxyTarget(cmd ManualAppCommand) (string, int, error) {
	if normalizeManualAppAccessMode(cmd.AccessMode) != "proxy" {
		return "", 0, nil
	}
	containerName := strings.TrimSpace(cmd.ContainerName)
	port := cmd.ContainerPort
	if port == 0 {
		port = 80
	}
	if !validManualAppContainerName(containerName) || port <= 0 || port > 65535 {
		return "", 0, ErrInvalid
	}
	return fmt.Sprintf("http://%s:%d", containerName, port), port, nil
}

func validManualAppContainerName(value string) bool {
	if value == "" || strings.EqualFold(value, "localhost") {
		return false
	}
	return regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,252}$`).MatchString(value)
}

func normalizeManualAppTargetPath(value string) string {
	path := strings.TrimSpace(value)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.HasPrefix(path, "//") {
		return "/"
	}
	if path == "/" {
		return "/"
	}
	return strings.TrimRight(path, "/")
}

func manualAppProxyPrefix(projectID, appID int64) string {
	return fmt.Sprintf("/app-proxy/projects/%d/apps/%d", projectID, appID)
}

func manualAppGatewayRouteKey(projectID, appID int64) string {
	return fmt.Sprintf("project.manual.%d.%d", projectID, appID)
}

func jsonInt64Slice(value any) []int64 {
	switch v := value.(type) {
	case []int64:
		return v
	case []int:
		out := make([]int64, 0, len(v))
		for _, id := range v {
			out = append(out, int64(id))
		}
		return out
	case []any:
		out := make([]int64, 0, len(v))
		for _, item := range v {
			if id := int64Value(item); id != 0 {
				out = append(out, id)
			}
		}
		return out
	default:
		return nil
	}
}

func int64Value(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		id, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return id
	default:
		return 0
	}
}

func manifestInt64(manifest map[string]any, key string) int64 {
	return int64Value(manifest[key])
}

func manifestString(manifest map[string]any, key, fallback string) string {
	if value, ok := manifest[key]; ok {
		text := strings.TrimSpace(jsonScalar(value))
		if text != "" {
			return text
		}
	}
	return fallback
}

func jsonScalar(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strings.TrimRight(strings.TrimRight(strconvFormatFloat(v), "0"), ".")
	default:
		return strings.TrimSpace(strings.Trim(strings.TrimSpace(fmtSprint(v)), "\""))
	}
}

func strconvFormatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func fmtSprint(value any) string {
	return fmt.Sprint(value)
}

func NormalizeAppStatus(status string) string {
	return appstatus.Normalize(status)
}

func AppStatusForFrontend(status string) string {
	switch NormalizeAppStatus(status) {
	case "active":
		return "Active"
	case "deploying":
		return "Deploying"
	case "failed":
		return "Failed"
	case "stopped":
		return "Stopped"
	default:
		return "Disabled"
	}
}

func formatProjectCreator(nickName, userName string) string {
	nickName = strings.TrimSpace(nickName)
	userName = strings.TrimSpace(userName)
	if nickName != "" {
		return nickName
	}
	if userName != "" {
		return userName
	}
	return "-"
}

func projectUnsRootAlias(projectID int64) string {
	return fmt.Sprintf("project_%d_root", projectID)
}

func normalizeNotFound(err error) error {
	if errors.Is(err, repo.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

func normalizeProjectSaveError(err error) error {
	if errors.Is(err, repo.ErrDuplicate) {
		return ErrDuplicate
	}
	return normalizeNotFound(err)
}

const (
	// ProjectImportAssetOwnerType 前端 Import 弹窗上传导入包时 asset 绑定的 ownerType（ownerId=projectId）。
	// core multipart 分片上传 complete 后同样按此绑定，使导入归属校验（checkImportAssetOwned）可通过。
	ProjectImportAssetOwnerType = "projectApp"
)

var (
	ErrInvalid                  = errors.New("invalid project")
	ErrNotFound                 = errors.New("project not found")
	ErrForbidden                = errors.New("project forbidden")
	ErrDuplicate                = errors.New("project.nameDuplicate")
	ErrAppNameDuplicate         = errors.New("project.appNameDuplicate")
	ErrUnsupportedPackageFormat = errors.New("project.unsupportedPackageFormat")
	ErrProjectHasApps           = errors.New("project.hasApps")
	// ErrAppIdentityUnverifiable 覆盖导入时目标应用未记录 sourceAppId 或包内 meta.json 缺 appId，无法校验来源身份
	ErrAppIdentityUnverifiable = errors.New("project.appIdentityUnverifiable")
	// ErrAppIdentityNotMatch 覆盖导入时包内 appId 与目标应用来源 appId 不一致
	ErrAppIdentityNotMatch = errors.New("project.appIdentityNotMatch")
	// ErrAppReplaceInvalidTarget 覆盖导入的目标应用不是已部署的动态应用（或处于 failed 状态），无法原地重建
	ErrAppReplaceInvalidTarget = errors.New("project.appReplaceInvalidTarget")
	// ErrAppReplaceInProgress 目标应用正在部署中（含进行中的覆盖导入），拒绝并发的覆盖导入
	ErrAppReplaceInProgress = errors.New("project.appReplaceInProgress")
	// ErrAppAlreadyExists 导入为新 App 时包内来源 appId 已在当前 Project 存在，应走覆盖更新
	ErrAppAlreadyExists = errors.New("project.appAlreadyExists")
	// ErrAppNameTooLong 应用名称超过 64 字符（rune）上限
	ErrAppNameTooLong = errors.New("project.appNameTooLong")
	// ErrAppDescTooLong 应用描述超过 200 字符（rune）上限
	ErrAppDescTooLong = errors.New("project.appDescTooLong")
	// ErrImportAssetDenied 导入包 asset 不存在或不属于当前 Project，拒绝读取（403），
	// 不泄露文件名与来源 Project
	ErrImportAssetDenied = errors.New("project.importAssetDenied")
	// ErrAppNotFound 覆盖导入的目标应用不存在或已被删除（区别于 Project 缺失的 ErrNotFound）
	ErrAppNotFound = errors.New("project.appNotFound")
)

// requireProjectManage 校验用户是否有权管理 project：平台 admin 或 project 创建者。
func (s *Service) requireProjectManage(ctx context.Context, project repo.Project, userID int64) error {
	if s.isAdmin(ctx, userID) {
		return nil
	}
	if project.OwnerUserID == userID {
		return nil
	}
	return ErrNotFound
}

// CheckManage 校验用户是否有权管理指定 project：平台 admin 或 project 创建者。
func (s *Service) CheckManage(ctx context.Context, projectID, userID int64) error {
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return normalizeNotFound(err)
	}
	return s.requireProjectManage(ctx, project, userID)
}

// requireProjectAccess 校验用户是否有权查看/访问 project：平台 admin、project 创建者或 project 成员。
func (s *Service) requireProjectAccess(ctx context.Context, project repo.Project, userID int64) error {
	if s.isAdmin(ctx, userID) {
		return nil
	}
	if project.OwnerUserID == userID {
		return nil
	}
	_, err := s.projects.GetEffectiveProjectMember(ctx, project.ID, userID)
	if err == nil {
		return nil
	}
	if errors.Is(err, repo.ErrNotFound) {
		return ErrForbidden
	}
	return err
}
