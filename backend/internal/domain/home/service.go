package home

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	domainlicense "backend/internal/domain/license"
	"backend/internal/repo"
)

type Service struct {
	audit    *repo.AuditRepo
	iam      *repo.IAMRepo
	flow     *repo.FlowRepo
	projects *repo.ProjectRepo
	uns      *repo.UnsRepo
}

type RecentItem struct {
	Key          string `json:"key"`
	ResourceKey  string `json:"resourceKey"`
	ResourceName string `json:"resourceName"`
	ResourceType string `json:"resourceType"`
	BusinessType string `json:"businessType"`
	RoutePath    string `json:"routePath"`
	Icon         string `json:"icon"`
	OperatedAt   int64  `json:"operatedAt"`
}

const (
	recentResourceKeyUNS        = "uns.page"
	recentResourceKeySourceFlow = "flow.collection.page"
	recentResourceKeyEventFlow  = "flow.event.page"
	recentResourceKeyNotebook   = "notebook.view"
	recentResourceKeyProject    = "project.view"
	recentResourceKeyProjectApp = "launchpad.view"
)

var recentResourceKeys = map[string]struct{}{
	recentResourceKeyUNS:        {},
	recentResourceKeySourceFlow: {},
	recentResourceKeyEventFlow:  {},
	recentResourceKeyProject:    {},
	recentResourceKeyProjectApp: {},
}

func New(ctx context.Context) *Service {
	return &Service{
		audit:    repo.NewAuditRepo(ctx),
		iam:      repo.NewIAMRepo(ctx),
		flow:     repo.NewFlowRepo(ctx),
		projects: repo.NewProjectRepo(ctx),
		uns:      repo.NewUnsRepo(ctx),
	}
}

func (s *Service) RecentItems(ctx context.Context, userID int64, limit int, isShowInRecent int64) ([]RecentItem, error) {
	if userID <= 0 {
		return nil, repo.ErrInvalidArgument
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	allowedResourceKeys, err := s.allowedResourceKeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	const auditPageSize = 100
	out := make([]RecentItem, 0, limit)
	seenEntries := map[string]struct{}{}
	var before *repo.RecentAuditCursor
	for len(out) < limit {
		logs, err := s.audit.ListRecentAuditLogs(ctx, userID, auditPageSize, isShowInRecent, before)
		if err != nil {
			return nil, err
		}
		if len(logs) == 0 {
			break
		}
		keys := make([]string, 0, len(logs))
		resolved := make([]resolvedLog, 0, len(logs))
		seenResourceKeys := map[string]struct{}{}
		for _, item := range logs {
			key := resourceKeyForAudit(item)
			if key == "" || !isRecentResourceKey(key) {
				continue
			}
			key = s.flowResourceKeyForAudit(ctx, key, item)
			menuResourceKey := menuResourceKeyForRecent(key)
			if _, ok := allowedResourceKeys[menuResourceKey]; !ok {
				continue
			}
			if shouldHideRecentAudit(key, item) {
				continue
			}
			resolved = append(resolved, resolvedLog{item: item, key: key})
			if _, ok := seenResourceKeys[menuResourceKey]; ok {
				continue
			}
			seenResourceKeys[menuResourceKey] = struct{}{}
			keys = append(keys, menuResourceKey)
		}
		projects, err := s.accessibleProjects(ctx, userID, resolved)
		if err != nil {
			return nil, err
		}
		resources, err := s.iam.ResourceMapByKeys(ctx, keys)
		if err != nil {
			return nil, err
		}
		for _, entry := range resolved {
			resource, ok := resources[menuResourceKeyForRecent(entry.key)]
			if !ok || strings.TrimSpace(resource.RoutePath) == "" {
				continue
			}
			dedupeKey := recentResourceIdentity(entry.key, entry.item)
			if _, ok := seenEntries[dedupeKey]; ok {
				continue
			}
			resourceName, routePath, available, err := s.currentRecentResource(ctx, userID, entry.key, entry.item, projects)
			if err != nil {
				return nil, err
			}
			if !available || strings.TrimSpace(resourceName) == "" || !isValidRecentRoute(routePath) {
				continue
			}
			resourceType := resourceTypeForRecent(entry.key, resource.Name)
			businessType := strings.TrimSpace(entry.item.BusinessType)
			if entry.key == recentResourceKeyProject && strings.EqualFold(businessType, "Delete") {
				// The target project was confirmed to exist. A child deletion must
				// not disable its Recent link; the original audit remains unchanged.
				businessType = "Update"
			}
			seenEntries[dedupeKey] = struct{}{}
			out = append(out, RecentItem{
				Key:          dedupeKey,
				ResourceKey:  resource.ResourceKey,
				ResourceName: resourceName,
				ResourceType: resourceType,
				BusinessType: businessType,
				RoutePath:    routePath,
				Icon:         resource.Icon,
				OperatedAt:   entry.item.CreatedAt,
			})
			if len(out) >= limit {
				break
			}
		}
		if len(logs) < auditPageSize {
			break
		}
		last := logs[len(logs)-1]
		before = &repo.RecentAuditCursor{CreatedTime: last.CreatedTime, ID: last.ID}
	}
	return out, nil
}

func (s *Service) allowedResourceKeys(ctx context.Context, userID int64) (map[string]struct{}, error) {
	keys, err := s.iam.ResourceKeysForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if licenseSvc := domainlicense.Default(); licenseSvc != nil {
		keys = licenseSvc.FilterResourceKeys(keys)
	}
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			allowed[key] = struct{}{}
		}
	}
	return allowed, nil
}

func (s *Service) accessibleProjects(ctx context.Context, userID int64, entries []resolvedLog) (map[int64]repo.Project, error) {
	projects := map[int64]repo.Project{}
	hasProject := false
	for _, entry := range entries {
		if entry.key == recentResourceKeyProject || entry.key == recentResourceKeyProjectApp {
			hasProject = true
			break
		}
	}
	if !hasProject {
		return projects, nil
	}
	includeAll, err := s.iam.IsAdmin(ctx, userID)
	if err != nil {
		return nil, err
	}
	items, err := s.projects.ListProjects(ctx, "", userID, includeAll)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		projects[item.ID] = item
	}
	return projects, nil
}

type resolvedLog struct {
	item repo.AuditLog
	key  string
}

func (s *Service) currentRecentResource(
	ctx context.Context,
	userID int64,
	resourceKey string,
	item repo.AuditLog,
	projects map[int64]repo.Project,
) (string, string, bool, error) {
	switch resourceKey {
	case recentResourceKeySourceFlow, recentResourceKeyEventFlow:
		id := flowIDFromAudit(item)
		if id <= 0 {
			return "", "", false, nil
		}
		flow, err := s.flow.GetFlow(ctx, id)
		if err != nil {
			return recentLookupResult(err)
		}
		if flow.NodeType == 1 {
			return "", "", false, nil
		}
		return strings.TrimSpace(flow.Name), routePathForFlow(flow), true, nil
	case recentResourceKeyUNS:
		if !strings.EqualFold(strings.TrimSpace(item.ResType), "UNS") {
			return "", "", false, nil
		}
		id, err := strconv.ParseInt(strings.TrimSpace(item.ResID), 10, 64)
		if err != nil || id <= 0 {
			return "", "", false, nil
		}
		node, err := s.uns.GetUnsNode(ctx, id, false)
		if err != nil {
			return recentLookupResult(err)
		}
		name := firstNonEmpty(node.DisplayName, node.Name, node.Namespace)
		values := url.Values{}
		values.Set("namespaceId", strconv.FormatInt(id, 10))
		return name, "/uns?" + values.Encode(), true, nil
	case recentResourceKeyProject:
		id := projectIDFromAudit(item)
		project, ok := projects[id]
		if !ok || id <= 0 {
			return "", "", false, nil
		}
		return strings.TrimSpace(project.Name), projectManagementRoutePath(id), true, nil
	case recentResourceKeyProjectApp:
		appID, appErr := strconv.ParseInt(strings.TrimSpace(item.ResID), 10, 64)
		if appErr != nil || appID <= 0 {
			return "", "", false, nil
		}
		app, err := s.projects.GetProjectAppByID(ctx, appID)
		if err != nil {
			return recentLookupResult(err)
		}
		name, routePath, available := projectAppRecentTarget(app, projects)
		if !available {
			return "", "", false, nil
		}
		visible, err := s.projectAppVisibleToUser(ctx, app.ProjectID, userID, app.ID)
		if err != nil {
			return "", "", false, err
		}
		if !visible {
			return "", "", false, nil
		}
		return name, routePath, true, nil
	}
	return "", "", false, nil
}

func projectAppRecentTarget(app repo.ProjectApp, projects map[int64]repo.Project) (string, string, bool) {
	if app.Status != "active" {
		return "", "", false
	}
	projectID := app.ProjectID
	if projectID <= 0 {
		return "", "", false
	}
	if _, ok := projects[projectID]; !ok {
		return "", "", false
	}
	name := strings.TrimSpace(app.Name)
	if name == "" {
		return "", "", false
	}
	return name, projectManagementRoutePath(projectID), true
}

func (s *Service) projectAppVisibleToUser(ctx context.Context, projectID, userID, appID int64) (bool, error) {
	member, err := s.projects.GetEffectiveProjectMember(ctx, projectID, userID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	roleApps, err := s.projects.ListProjectRoleAppIDsByAppIDs(ctx, projectID, []int64{appID})
	if err != nil {
		return false, err
	}
	return projectAppRoleVisible(member, roleApps, appID), nil
}

func projectAppRoleVisible(member repo.ProjectMember, roleApps []repo.ProjectRoleApp, appID int64) bool {
	if len(roleApps) == 0 {
		return true
	}
	userRoleIDs := make(map[int64]struct{}, len(member.Roles))
	for _, role := range member.Roles {
		userRoleIDs[role.ID] = struct{}{}
	}
	for _, roleApp := range roleApps {
		if roleApp.AppID != appID {
			continue
		}
		if _, ok := userRoleIDs[roleApp.RoleID]; ok {
			return true
		}
	}
	return false
}

func recentLookupResult(err error) (string, string, bool, error) {
	if errors.Is(err, repo.ErrNotFound) {
		return "", "", false, nil
	}
	return "", "", false, err
}

func projectManagementRoutePath(projectID int64) string {
	if projectID <= 0 {
		return ""
	}
	return "/project/" + strconv.FormatInt(projectID, 10)
}

func routePathForFlow(flow repo.Flow) string {
	basePath := "/collection-flow"
	if flow.FlowType == 2 {
		basePath = "/event-flow"
	}
	runtimeFlowID := strings.TrimSpace(flow.RuntimeFlowID)
	if runtimeFlowID == "" {
		runtimeFlowID = strconv.FormatInt(flow.ID, 10)
	}
	values := url.Values{}
	values.Set("id", strconv.FormatInt(flow.ID, 10))
	values.Set("name", flow.Name)
	values.Set("status", flow.Status)
	values.Set("flowId", runtimeFlowID)
	values.Set("copyEnable", "false")
	values.Set("from", basePath)
	return basePath + "/flow-editor?" + values.Encode()
}

func isValidRecentRoute(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func notebookIDForRoute(item repo.AuditLog) int64 {
	id := notebookIDFromAudit(item)
	if id > 0 {
		return id
	}
	return 0
}

func isNotebookFolderAudit(item repo.AuditLog) bool {
	return strings.Contains(strings.ToLower(detailPath(item.DetailJSON)), "/notebook-folders")
}

func (s *Service) flowForAudit(ctx context.Context, resourceKey string, item repo.AuditLog) repo.Flow {
	if s == nil || s.flow == nil {
		return repo.Flow{}
	}
	id := flowIDFromAudit(item)
	if id > 0 {
		flow, err := s.flow.GetFlow(ctx, id)
		if err == nil {
			return flow
		}
	}
	return repo.Flow{}
}

func notebookIDFromAudit(item repo.AuditLog) int64 {
	if id, err := strconv.ParseInt(strings.TrimSpace(item.ResID), 10, 64); err == nil && id > 0 {
		return id
	}
	path := detailPath(item.DetailJSON)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "notebooks", "notebook":
			if i+1 >= len(parts) {
				return 0
			}
			id, err := strconv.ParseInt(strings.TrimSpace(parts[i+1]), 10, 64)
			if err == nil && id > 0 {
				return id
			}
			return 0
		}
	}
	return 0
}

// Use the same target ID as currentRecentResource. A project member audit's
// ResID identifies the member, while its Recent entry opens the parent project.
// Names, audit IDs and action types must not create extra entries for that target.
func recentResourceIdentity(resourceKey string, item repo.AuditLog) string {
	var id int64
	switch resourceKey {
	case recentResourceKeyNotebook:
		id = notebookIDForRoute(item)
	case recentResourceKeySourceFlow, recentResourceKeyEventFlow:
		id = flowIDFromAudit(item)
	case recentResourceKeyProject:
		id = projectIDFromAudit(item)
	default: // UNS and ProjectApp use their own resource ID, not the parent route.
		id, _ = strconv.ParseInt(strings.TrimSpace(item.ResID), 10, 64)
	}
	return resourceKey + ":" + strconv.FormatInt(id, 10)
}

func flowIDFromAudit(item repo.AuditLog) int64 {
	if id, err := strconv.ParseInt(strings.TrimSpace(item.ResID), 10, 64); err == nil && id > 0 {
		return id
	}
	path := detailPath(item.DetailJSON)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "flows", "flow":
			for j := i + 1; j < len(parts); j++ {
				if id, err := strconv.ParseInt(strings.TrimSpace(parts[j]), 10, 64); err == nil && id > 0 {
					return id
				}
			}
			return 0
		}
	}
	return 0
}

func isRecentResourceKey(key string) bool {
	_, ok := recentResourceKeys[strings.TrimSpace(key)]
	return ok
}

func (s *Service) flowResourceKeyForAudit(ctx context.Context, resourceKey string, item repo.AuditLog) string {
	if resourceKey != recentResourceKeySourceFlow && resourceKey != recentResourceKeyEventFlow {
		return resourceKey
	}
	flow := s.flowForAudit(ctx, resourceKey, item)
	switch flow.FlowType {
	case 1:
		return recentResourceKeySourceFlow
	case 2:
		return recentResourceKeyEventFlow
	default:
		return resourceKey
	}
}

func shouldHideRecentAudit(resourceKey string, item repo.AuditLog) bool {
	if resourceKey != recentResourceKeyUNS {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.BusinessType)) {
	case "import", "export":
		return true
	default:
		return false
	}
}

func flowTypeForRecent(resourceKey string, item repo.AuditLog) int {
	switch resourceKey {
	case recentResourceKeySourceFlow:
		return 1
	case recentResourceKeyEventFlow:
		return 2
	}
	switch strings.ToLower(strings.TrimSpace(item.ResType)) {
	case "eventflow":
		return 2
	case "sourceflow", "flow":
		return 1
	default:
		return 0
	}
}

func resourceTypeForRecent(resourceKey, fallback string) string {
	switch resourceKey {
	case recentResourceKeyUNS:
		return "UNS"
	case recentResourceKeySourceFlow, recentResourceKeyEventFlow:
		return "Flow"
	case recentResourceKeyNotebook:
		return "Notebook"
	case recentResourceKeyProject:
		return "Project"
	case recentResourceKeyProjectApp:
		return "App"
	default:
		return strings.TrimSpace(fallback)
	}
}

func menuResourceKeyForRecent(resourceKey string) string {
	if resourceKey == recentResourceKeyProjectApp {
		return recentResourceKeyProject
	}
	return resourceKey
}

func resourceKeyForAudit(item repo.AuditLog) string {
	if strings.EqualFold(strings.TrimSpace(item.ResType), "ProjectApp") {
		return recentResourceKeyProjectApp
	}
	if key := resourceKeyFromPath(detailPath(item.DetailJSON)); key != "" {
		return key
	}
	switch strings.ToLower(strings.TrimSpace(item.ResType)) {
	case "uns", "template", "label", "group", "dashboard":
		return recentResourceKeyUNS
	case "sourceflow", "flow":
		return recentResourceKeySourceFlow
	case "eventflow":
		return recentResourceKeyEventFlow
	case "notebook":
		return recentResourceKeyNotebook
	case "project", "projectmember":
		return recentResourceKeyProject
	default:
		return ""
	}
}

func projectIDFromAudit(item repo.AuditLog) int64 {
	if id := detailInt64(item.DetailJSON, "projectId"); id > 0 {
		return id
	}
	parts := strings.Split(strings.Trim(detailPath(item.DetailJSON), "/"), "/")
	for i, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "projects", "project":
			if i+1 < len(parts) {
				if id, err := strconv.ParseInt(strings.TrimSpace(parts[i+1]), 10, 64); err == nil && id > 0 {
					return id
				}
			}
		}
	}
	id, _ := strconv.ParseInt(strings.TrimSpace(item.ResID), 10, 64)
	return id
}

func detailInt64(detailJSON, key string) int64 {
	if strings.TrimSpace(detailJSON) == "" || strings.TrimSpace(key) == "" {
		return 0
	}
	var detail map[string]any
	if err := json.Unmarshal([]byte(detailJSON), &detail); err != nil {
		return 0
	}
	switch value := detail[key].(type) {
	case float64:
		return int64(value)
	case string:
		result, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return result
	default:
		return 0
	}
}

func detailPath(detailJSON string) string {
	if strings.TrimSpace(detailJSON) == "" {
		return ""
	}
	var detail map[string]any
	if err := json.Unmarshal([]byte(detailJSON), &detail); err != nil {
		return ""
	}
	if value, ok := detail["path"].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func resourceKeyFromPath(path string) string {
	path = strings.ToLower(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	switch {
	case strings.Contains(path, "/api/core/uns"):
		return recentResourceKeyUNS
	case strings.Contains(path, "/api/core/flows"):
		return recentResourceKeySourceFlow
	case strings.Contains(path, "/api/core/notebooks"), strings.Contains(path, "/api/core/notebook-folders"):
		return recentResourceKeyNotebook
	case strings.Contains(path, "/api/core/projects"):
		return recentResourceKeyProject
	default:
		return ""
	}
}
