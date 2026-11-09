package iam

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	auditdomain "backend/internal/domain/audit"
	domainlaunchpad "backend/internal/domain/launchpad"
	domainlicense "backend/internal/domain/license"
	"backend/internal/infra/outbox"
	"backend/internal/repo"

	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	repo      *repo.IAMRepo
	groups    *repo.UserGroupRepo
	jobs      *repo.AsyncJobRepo
	outbox    *outbox.Service
	launchpad *domainlaunchpad.Service
	audit     *auditdomain.Service
}

// BindAudit 注入审计服务,用于用户启停用等状态翻转的审计记录。
func (s *Service) BindAudit(auditSvc *auditdomain.Service) {
	s.audit = auditSvc
}

type UserRoleCommand struct {
	RoleID   int64
	RoleName string
}

type UserSaveCommand struct {
	Username    string
	Password    string
	FirstName   string
	Email       string
	Phone       string
	Enabled     bool
	RoleList    []UserRoleCommand
	UserID      int64
	IsRandomPwd bool
}

type UserUpdateCommand struct {
	ID        int64
	Username  string
	FirstName string
	Email     string
	Phone     string
	Enabled   bool
	RoleList  []UserRoleCommand
	UserID    int64
}

type UserBatchUpdateCommand struct {
	UserIDs  []int64
	HomePage *string
	RoleList []UserRoleCommand
	ActorID  int64
}

type UserBatchUpdateResult struct {
	UserID   int64
	HomePage string
	RoleID   int64
}

type UserPasswordResetCommand struct {
	UserID   int64
	Password string
	ActorID  int64
}

type CurrentUserPasswordCommand struct {
	UserID      int64
	OldPassword string
	NewPassword string
}

type ResourceSaveCommand struct {
	ResourceID  int64
	ParentID    int64
	ResourceKey string
	Name        string
	Type        int64
	RoutePath   string
	UrlType     int64
	OpenType    int64
	Icon        string
	Sort        int64
	Enabled     int64
}

func New(ctx context.Context) *Service {
	return &Service{
		repo:      repo.NewIAMRepo(ctx),
		groups:    repo.NewUserGroupRepo(ctx),
		jobs:      repo.NewAsyncJobRepo(ctx),
		outbox:    outbox.New(),
		launchpad: domainlaunchpad.New(ctx, nil),
	}
}

func (s *Service) IsAdmin(ctx context.Context, userID int64) bool {
	if s == nil || s.repo == nil {
		return false
	}
	ok, err := s.repo.IsAdmin(ctx, userID)
	return err == nil && ok
}

func (s *Service) UserRoleCode(ctx context.Context, userID int64) string {
	if s == nil || s.repo == nil {
		return ""
	}
	code, _ := s.repo.UserRoleCode(ctx, userID)
	return code
}

func (s *Service) Users(ctx context.Context) ([]map[string]any, error) {
	users, err := s.repo.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0, len(users))
	for _, user := range users {
		if userID, ok := user["userId"].(int64); ok && userID > 0 {
			userIDs = append(userIDs, userID)
		}
	}
	groupsByUser, err := s.groups.UserGroupsByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		userID, _ := user["userId"].(int64)
		user["groupList"] = groupsByUser[userID]
		preferred, _ := user["homePage"].(string)
		resolved, matched, err := s.resolveLaunchpadAppHomePage(ctx, userID, preferred)
		if err != nil {
			return nil, err
		}
		if matched {
			user["homePage"] = resolved
		}
	}
	return users, nil
}

func (s *Service) CreateUser(ctx context.Context, cmd UserSaveCommand) (map[string]any, error) {
	username := strings.TrimSpace(cmd.Username)
	password := strings.TrimSpace(cmd.Password)
	roleID := int64(0)
	if len(cmd.RoleList) > 0 {
		roleID = cmd.RoleList[0].RoleID
	}
	if username == "" || password == "" || roleID <= 0 {
		return nil, ErrInvalidUser
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	status := int64(0)
	if cmd.Enabled {
		status = 1
		if err := s.checkUserQuota(ctx); err != nil {
			return nil, err
		}
	}
	return s.repo.CreateUser(ctx, repo.UserCreate{
		UserName:    username,
		NickName:    strings.TrimSpace(cmd.FirstName),
		Email:       strings.TrimSpace(cmd.Email),
		Phone:       strings.TrimSpace(cmd.Phone),
		Password:    string(hash),
		Status:      status,
		IsRandomPwd: cmd.IsRandomPwd,
		RoleID:      roleID,
		UserID:      cmd.UserID,
	})
}

func (s *Service) UpdateUser(ctx context.Context, cmd UserUpdateCommand) (map[string]any, error) {
	roleID := int64(0)
	hasRole := len(cmd.RoleList) > 0
	if hasRole {
		roleID = cmd.RoleList[0].RoleID
	}
	status := int64(0)
	if cmd.Enabled {
		status = 1
	}
	current, err := s.repo.GetUserByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}
	if status == 1 && current.Status != 1 {
		if err := s.checkUserQuota(ctx); err != nil {
			return nil, err
		}
	}
	data, err := s.repo.UpdateUser(ctx, repo.UserUpdate{
		UserID:   cmd.ID,
		UserName: strings.TrimSpace(cmd.Username),
		NickName: strings.TrimSpace(cmd.FirstName),
		Email:    strings.TrimSpace(cmd.Email),
		Phone:    strings.TrimSpace(cmd.Phone),
		Status:   status,
		RoleID:   roleID,
		HasRole:  hasRole,
		ActorID:  cmd.UserID,
	})
	if err != nil {
		return nil, err
	}
	if current.Status != status {
		s.recordUserStatusAudit(ctx, cmd.UserID, cmd.ID, current.Status, status)
	}
	return data, nil
}

// recordUserStatusAudit 用户启停用状态翻转时记一条 Enable/Disable 审计。
func (s *Service) recordUserStatusAudit(ctx context.Context, actorID, userID, from, to int64) {
	if s.audit == nil {
		return
	}
	operator := &auditdomain.Operator{ID: strconv.FormatInt(actorID, 10)}
	if actor, err := s.repo.GetUserByID(ctx, actorID); err == nil {
		operator.Name = actor.UserName
	}
	businessType := auditdomain.BizDisable
	if to == 1 {
		businessType = auditdomain.BizEnable
	}
	input := auditdomain.RecordInput{
		ScopeType:    auditdomain.ScopeTypePlatform,
		ResType:      auditdomain.ResTypeUserManagement,
		ResID:        strconv.FormatInt(userID, 10),
		BusinessType: businessType,
		Detail:       map[string]any{"userId": userID, "from": from, "to": to},
	}
	s.audit.RecordDirect(operator, input)
	if auditdomain.StateFromContext(ctx) != nil {
		s.audit.Record(ctx, input)
		s.audit.Suppress(ctx)
	}
}

func (s *Service) DeleteUser(ctx context.Context, userID, actorID int64) error {
	return s.repo.DeleteUser(ctx, userID, actorID)
}

func (s *Service) ResetUserPassword(ctx context.Context, cmd UserPasswordResetCommand) error {
	password := strings.TrimSpace(cmd.Password)
	if cmd.UserID <= 0 || password == "" {
		return ErrInvalidUser
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.repo.ResetUserPassword(ctx, cmd.UserID, string(hash), cmd.ActorID)
}

func (s *Service) ChangeCurrentUserPassword(ctx context.Context, cmd CurrentUserPasswordCommand) error {
	oldPassword := strings.TrimSpace(cmd.OldPassword)
	newPassword := strings.TrimSpace(cmd.NewPassword)
	if cmd.UserID <= 0 || oldPassword == "" || newPassword == "" {
		return ErrInvalidUser
	}
	user, err := s.repo.GetUserByID(ctx, cmd.UserID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword)); err != nil {
		return ErrInvalidPassword
	}
	if newPassword == oldPassword {
		return ErrInvalidPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.repo.ChangeUserPassword(ctx, cmd.UserID, string(hash))
}

func (s *Service) CountEnabledUsers(ctx context.Context) (int64, error) {
	return s.repo.CountEnabledUsers(ctx)
}

func (s *Service) checkUserQuota(ctx context.Context) error {
	licenseSvc := domainlicense.Default()
	if licenseSvc == nil {
		return nil
	}
	count, err := s.repo.CountEnabledUsers(ctx)
	if err != nil {
		return err
	}
	return licenseSvc.CheckQuota("max_users", count)
}

func (s *Service) Roles(ctx context.Context) ([]repo.Role, error) {
	roles, err := s.repo.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	return s.filterRolesByLicense(roles), nil
}

func (s *Service) CreateRole(ctx context.Context, name string, userID int64) (repo.Role, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return repo.Role{}, ErrInvalidRole
	}
	return s.repo.CreateRole(ctx, name, "", userID)
}

func (s *Service) UpdateRole(ctx context.Context, roleID int64, name string, allowURIs []string, defaultHomePage string, userID int64) error {
	if roleID <= 0 {
		return ErrInvalidRole
	}
	filteredURIs, err := s.filterRoleURIsByLicense(ctx, allowURIs)
	if err != nil {
		return err
	}
	return s.repo.UpdateRole(ctx, roleID, name, filteredURIs, defaultHomePage, userID)
}

func (s *Service) DeleteRole(ctx context.Context, roleID, userID int64) error {
	if roleID <= 0 {
		return ErrInvalidRole
	}
	return s.repo.DeleteRole(ctx, roleID, userID)
}

func (s *Service) Resources(ctx context.Context) ([]repo.Resource, error) {
	return s.repo.ListResources(ctx)
}

func (s *Service) Menus(ctx context.Context) ([]repo.Resource, error) {
	return s.repo.ListMenuResources(ctx)
}

func (s *Service) SaveResource(ctx context.Context, cmd ResourceSaveCommand, userID int64) (repo.Resource, error) {
	if strings.TrimSpace(cmd.ResourceKey) == "" || strings.TrimSpace(cmd.Name) == "" || cmd.Type <= 0 {
		return repo.Resource{}, ErrInvalidRole
	}
	return s.repo.SaveResource(ctx, repo.Resource{
		ID:          cmd.ResourceID,
		ParentID:    cmd.ParentID,
		ResourceKey: strings.TrimSpace(cmd.ResourceKey),
		Name:        strings.TrimSpace(cmd.Name),
		Type:        cmd.Type,
		RoutePath:   strings.TrimSpace(cmd.RoutePath),
		UrlType:     cmd.UrlType,
		OpenType:    cmd.OpenType,
		Icon:        strings.TrimSpace(cmd.Icon),
		Sort:        cmd.Sort,
	}, userID, cmd.Enabled)
}

func (s *Service) ResourceActions(ctx context.Context, resourceID int64) ([]repo.ResourceAction, error) {
	if resourceID <= 0 {
		return nil, ErrInvalidRole
	}
	return s.repo.ListResourceActions(ctx, resourceID)
}

func (s *Service) ResolveUserHomePage(ctx context.Context, userID int64, preferred string) (string, error) {
	if resolved, matched, err := s.resolveLaunchpadAppHomePage(ctx, userID, preferred); matched || err != nil {
		return resolved, err
	}
	keys, err := s.repo.ResourceKeysForUser(ctx, userID)
	if err != nil {
		return "", err
	}
	licenseSvc := domainlicense.Default()
	if licenseSvc != nil {
		keys = licenseSvc.FilterResourceKeys(keys)
	}
	resourceMap, err := s.repo.ResourceMapByKeys(ctx, keys)
	if err != nil {
		return "", err
	}
	allowedPages := map[string]string{}
	for _, item := range resourceMap {
		page := strings.TrimSpace(item.RoutePath)
		if page == "" || !strings.HasPrefix(page, "/") || strings.HasPrefix(page, "//") {
			continue
		}
		allowedPages[strings.ToLower(page)] = page
	}
	orderedPages, err := s.repo.HomePagesForUser(ctx, userID)
	if err != nil {
		return "", err
	}
	orderedAllowedPages := orderedAllowedHomePages(allowedPages, orderedPages)
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		if page, ok := allowedPages[strings.ToLower(preferred)]; ok {
			return page, nil
		}
	}
	defaultPage, err := s.repo.DefaultHomePageForUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if page, ok := allowedPages[strings.ToLower(strings.TrimSpace(defaultPage))]; ok {
		return page, nil
	}
	return defaultAllowedHomePageInOrder(allowedPages, orderedAllowedPages), nil
}

func (s *Service) UpdateUserHomePage(ctx context.Context, userID int64, preferred string) (string, error) {
	homePage, err := s.ResolveUserHomePage(ctx, userID, preferred)
	if err != nil {
		return "", err
	}
	if _, err := repo.NewUserConfigRepo(ctx).UpdateUserConfig(ctx, userID, &homePage, nil); err != nil {
		return "", err
	}
	return homePage, nil
}

func (s *Service) BatchUpdateUsers(ctx context.Context, cmd UserBatchUpdateCommand) ([]UserBatchUpdateResult, error) {
	hasHomePage := cmd.HomePage != nil && strings.TrimSpace(*cmd.HomePage) != ""
	hasRole := len(cmd.RoleList) > 0
	if len(cmd.UserIDs) == 0 || cmd.ActorID <= 0 || (!hasHomePage && !hasRole) || len(cmd.RoleList) > 1 {
		return nil, ErrInvalidUser
	}
	roleID := int64(0)
	if hasRole {
		roleID = cmd.RoleList[0].RoleID
		if roleID <= 0 {
			return nil, ErrInvalidUser
		}
	}

	var targetRole *repo.Role
	if hasHomePage && hasRole {
		roles, err := s.Roles(ctx)
		if err != nil {
			return nil, err
		}
		for i := range roles {
			if roles[i].ID == roleID {
				targetRole = &roles[i]
				break
			}
		}
		if targetRole == nil {
			return nil, repo.ErrNotFound
		}
	}

	seen := make(map[int64]struct{}, len(cmd.UserIDs))
	results := make([]UserBatchUpdateResult, 0, len(cmd.UserIDs))
	repoUpdates := make([]repo.UserHomePageUpdate, 0, len(cmd.UserIDs))
	for _, userID := range cmd.UserIDs {
		if userID <= 0 {
			return nil, ErrInvalidUser
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}

		result := UserBatchUpdateResult{UserID: userID, RoleID: roleID}
		if hasHomePage {
			var (
				homePage string
				err      error
			)
			if targetRole != nil {
				homePage, err = s.resolveUserHomePageForRole(ctx, userID, *cmd.HomePage, *targetRole)
			} else {
				homePage, err = s.ResolveUserHomePage(ctx, userID, *cmd.HomePage)
			}
			if err != nil {
				return nil, err
			}
			result.HomePage = homePage
			repoUpdates = append(repoUpdates, repo.UserHomePageUpdate{UserID: userID, HomePage: homePage})
		}
		results = append(results, result)
	}
	if err := s.repo.BatchUpdateUsers(ctx, repo.UserBatchUpdate{
		UserIDs:   userIDsFromBatchResults(results),
		HomePages: repoUpdates,
		RoleID:    roleID,
		HasRole:   hasRole,
		ActorID:   cmd.ActorID,
	}); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Service) resolveUserHomePageForRole(ctx context.Context, userID int64, preferred string, role repo.Role) (string, error) {
	if resolved, matched, err := s.resolveLaunchpadAppHomePage(ctx, userID, preferred); matched || err != nil {
		return resolved, err
	}
	allowed, ordered := allowedRoleHomePages(role.ResourceList)
	if page, ok := allowed[strings.ToLower(strings.TrimSpace(preferred))]; ok {
		return page, nil
	}
	if page, ok := allowed[strings.ToLower(strings.TrimSpace(role.DefaultHomePage))]; ok {
		return page, nil
	}
	return defaultAllowedHomePageInOrder(allowed, ordered), nil
}

func userIDsFromBatchResults(results []UserBatchUpdateResult) []int64 {
	userIDs := make([]int64, 0, len(results))
	for _, result := range results {
		userIDs = append(userIDs, result.UserID)
	}
	return userIDs
}

func (s *Service) resolveLaunchpadAppHomePage(ctx context.Context, userID int64, preferred string) (string, bool, error) {
	value := strings.TrimSpace(preferred)
	if !strings.HasPrefix(strings.ToLower(value), repo.DefaultOperatorHomePage+"/") {
		return "", false, nil
	}

	path := strings.TrimSuffix(strings.SplitN(value, "?", 2)[0], "/")
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 3 || !strings.EqualFold(parts[0], "launchpad") {
		return repo.DefaultOperatorHomePage, true, nil
	}
	projectID, projectErr := strconv.ParseInt(parts[1], 10, 64)
	appID, appErr := strconv.ParseInt(parts[2], 10, 64)
	if projectErr != nil || appErr != nil || projectID <= 0 || appID <= 0 {
		return repo.DefaultOperatorHomePage, true, nil
	}

	if _, err := s.launchpad.App(ctx, userID, projectID, appID); err != nil {
		if errors.Is(err, domainlaunchpad.ErrNotFound) {
			return repo.DefaultOperatorHomePage, true, nil
		}
		return "", true, err
	}
	return fmt.Sprintf("%s/%d/%d", repo.DefaultOperatorHomePage, projectID, appID), true, nil
}

func (s *Service) filterRolesByLicense(roles []repo.Role) []repo.Role {
	licenseSvc := domainlicense.Default()
	if licenseSvc == nil {
		return roles
	}
	out := make([]repo.Role, 0, len(roles))
	for _, role := range roles {
		role.ResourceList = filterRoleResourceList(role.ResourceList, licenseSvc)
		role.DefaultHomePage = normalizeRoleHomePage(role.DefaultHomePage, role.ResourceList)
		out = append(out, role)
	}
	return out
}

func (s *Service) filterRoleURIsByLicense(ctx context.Context, uris []string) ([]string, error) {
	licenseSvc := domainlicense.Default()
	if licenseSvc == nil {
		return uris, nil
	}
	resourceKeysByURI, err := s.repo.ResourceKeysByURIs(ctx, uris)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(uris))
	for _, uri := range uris {
		trimmed := strings.TrimSpace(uri)
		if trimmed == "" {
			continue
		}
		resourceKey := strings.TrimSpace(resourceKeysByURI[trimmed])
		if resourceKey != "" && !licenseSvc.AllowResource(resourceKey) {
			return nil, ErrLicenseResourceDenied
		}
		out = append(out, trimmed)
	}
	return out, nil
}

func filterRoleResourceList(resourceList []map[string]string, licenseSvc *domainlicense.Service) []map[string]string {
	out := make([]map[string]string, 0, len(resourceList))
	for _, item := range resourceList {
		resourceKey := strings.TrimSpace(item["resourceKey"])
		if resourceKey == "" {
			continue
		}
		if licenseSvc.AllowResource(resourceKey) {
			out = append(out, item)
		}
	}
	return out
}

func normalizeRoleHomePage(value string, resourceList []map[string]string) string {
	allowed, ordered := allowedRoleHomePages(resourceList)
	if page, ok := allowed[strings.ToLower(strings.TrimSpace(value))]; ok {
		return page
	}
	return defaultAllowedHomePageInOrder(allowed, ordered)
}

func allowedRoleHomePages(resourceList []map[string]string) (map[string]string, []string) {
	allowed := map[string]string{}
	ordered := make([]string, 0, len(resourceList))
	for _, item := range resourceList {
		page := strings.TrimSpace(item["routePath"])
		if page == "" || !strings.HasPrefix(page, "/") || strings.HasPrefix(page, "//") {
			continue
		}
		key := strings.ToLower(page)
		if _, exists := allowed[key]; exists {
			continue
		}
		allowed[key] = page
		ordered = append(ordered, page)
	}
	return allowed, ordered
}

func orderedAllowedHomePages(allowed map[string]string, ordered []string) []string {
	out := make([]string, 0, len(ordered))
	seen := map[string]struct{}{}
	for _, page := range ordered {
		key := strings.ToLower(strings.TrimSpace(page))
		if key == "" {
			continue
		}
		value, ok := allowed[key]
		if !ok {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func defaultAllowedHomePageInOrder(allowed map[string]string, ordered []string) string {
	if page, ok := allowed[strings.ToLower(repo.DefaultHomePage)]; ok {
		return page
	}
	if page, ok := allowed[strings.ToLower(repo.DefaultOperatorHomePage)]; ok {
		return page
	}
	if len(ordered) > 0 {
		return ordered[0]
	}
	return repo.DefaultOperatorHomePage
}

func (s *Service) SaveResourceAction(ctx context.Context, action repo.ResourceAction, userID int64) (repo.ResourceAction, error) {
	if action.ResourceID <= 0 {
		return repo.ResourceAction{}, ErrInvalidRole
	}
	action.ActionType = strings.TrimSpace(strings.ToLower(action.ActionType))
	action.ActionValue = strings.TrimSpace(action.ActionValue)
	action.Methods = strings.TrimSpace(strings.ToUpper(action.Methods))
	if action.ActionType == "" || action.ActionValue == "" {
		return repo.ResourceAction{}, ErrInvalidRole
	}
	return s.repo.SaveResourceAction(ctx, action, userID)
}

func (s *Service) DeleteResourceAction(ctx context.Context, actionID, userID int64) error {
	if actionID <= 0 {
		return ErrInvalidRole
	}
	return s.repo.DeleteResourceAction(ctx, actionID, userID)
}

var ErrInvalidRole = errors.New("invalid role")
var ErrInvalidUser = errors.New("invalid user")
var ErrForbidden = errors.New("iam.forbidden")
var ErrInvalidPassword = errors.New("invalid password")
var ErrLicenseResourceDenied = errors.New("license resource is not allowed")
