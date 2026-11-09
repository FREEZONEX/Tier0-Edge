package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	domaincommon "backend/internal/domain/common"
	"backend/internal/repo"

	"github.com/xuri/excelize/v2"
)

const (
	UserImportJobType   = "iam.user.import"
	UserExportJobType   = "iam.user.export"
	UserImportEventType = "iam.user.import.job"
	UserExportEventType = "iam.user.export.job"

	UserExportScopeSelected = "selected"
	UserExportScopeFiltered = "filtered"
	UserExportScopeGroups   = "groups"
	UserExportScopeAll      = "all"

	UserExportSensitiveMasked = "masked"
	UserExportSensitiveFull   = "full"

	SensitiveExportResourceKey = "iam.user.export.sensitive"
)

type UserExportRequest struct {
	Scope         string   `json:"scope"`
	UserIDs       []int64  `json:"userIds,omitempty"`
	GroupIDs      []int64  `json:"groupIds,omitempty"`
	Keyword       string   `json:"keyword,omitempty"`
	Statuses      []string `json:"statuses,omitempty"`
	RoleIDs       []int64  `json:"roleIds,omitempty"`
	SensitiveMode string   `json:"sensitiveMode,omitempty"`
}

type UserGroupSaveCommand struct {
	GroupID     int64
	GroupName   string
	Description string
	UserID      int64
}

type ImportWorkbookResult struct {
	Result        map[string]any
	ErrorWorkbook []byte
}

type importIssue struct {
	Sheet  string
	Row    int
	Values []string
	Field  string
	Reason string
}

type importUserRow struct {
	Row             int
	Values          []string
	Username        string
	DisplayName     string
	Email           string
	Phone           string
	Enabled         bool
	InitialPassword string
	GroupName       string
}

type importWorkbook struct {
	Users   []importUserRow
	Headers []string
	Issues  []importIssue
}

func (s *Service) ListUserGroups(ctx context.Context, keyword string, page, size int) (map[string]any, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	items, total, err := s.groups.List(ctx, keyword, page, size)
	if err != nil {
		return nil, err
	}
	return map[string]any{"list": items, "total": total, "page": page, "size": size}, nil
}

func (s *Service) CreateUserGroup(ctx context.Context, cmd UserGroupSaveCommand) (repo.UserGroup, error) {
	if err := validateUserGroupFields(cmd.GroupName, cmd.Description); err != nil {
		return repo.UserGroup{}, err
	}
	return s.groups.Create(ctx, cmd.GroupName, cmd.Description, cmd.UserID)
}

func (s *Service) UpdateUserGroup(ctx context.Context, cmd UserGroupSaveCommand) (repo.UserGroup, error) {
	if err := validateUserGroupFields(cmd.GroupName, cmd.Description); err != nil {
		return repo.UserGroup{}, err
	}
	return s.groups.Update(ctx, cmd.GroupID, cmd.GroupName, cmd.Description, cmd.UserID)
}

func validateUserGroupFields(groupName, description string) error {
	if strings.TrimSpace(groupName) == "" ||
		domaincommon.ExceedNameLimit(strings.TrimSpace(groupName)) ||
		domaincommon.ExceedDescLimit(strings.TrimSpace(description)) {
		return repo.ErrInvalidArgument
	}
	return nil
}

func (s *Service) DeleteUserGroup(ctx context.Context, groupID, userID int64) error {
	return s.groups.Delete(ctx, groupID, userID)
}

func (s *Service) ListUserGroupMembers(ctx context.Context, groupID int64) (map[string]any, error) {
	items, err := s.groups.ListMembers(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"list": items, "total": len(items)}, nil
}

func (s *Service) ReplaceUserGroupMembers(ctx context.Context, groupID int64, userIDs []int64, userID int64) error {
	return s.groups.ReplaceMembers(ctx, groupID, userIDs, userID)
}

func (s *Service) CreateUserImportJob(ctx context.Context, sourceFileID, userID int64) (map[string]any, error) {
	if sourceFileID <= 0 {
		return nil, ErrInvalidUser
	}
	job, err := s.jobs.CreateAsyncJob(ctx, UserImportJobType, map[string]any{"sourceFileId": sourceFileID}, userID)
	if err != nil {
		return nil, err
	}
	if err := s.outbox.Enqueue(ctx, UserImportEventType, "asyncJob", job.JobKey, job); err != nil {
		_ = s.jobs.MarkAsyncJobFailed(ctx, job.JobKey, err.Error(), map[string]any{"error": err.Error()})
		return nil, err
	}
	return map[string]any{"jobKey": job.JobKey}, nil
}

func (s *Service) BuildUserImportTemplate() ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	_ = f.SetSheetName(f.GetSheetName(0), "Users")
	usersHeader := []any{"username", "displayName", "email", "phone", "enabled", "initialPassword", "groupName"}
	_ = f.SetSheetRow("Users", "A1", &usersHeader)
	configureUserSheet(f, "Users", []float64{26, 18, 24, 14, 12, 24, 36}, 1)
	buffer, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Service) PreviewUserExport(ctx context.Context, request UserExportRequest, userID int64) (map[string]any, error) {
	normalized, err := normalizeUserExportRequest(request)
	if err != nil {
		return nil, err
	}
	users, _, _, err := s.selectUserExportData(ctx, normalized)
	if err != nil {
		return nil, err
	}
	sensitiveAllowed, err := s.CanExportSensitive(ctx, userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"estimatedCount":   len(users),
		"sensitiveAllowed": sensitiveAllowed,
		"scope":            normalized.Scope,
	}, nil
}

func (s *Service) CreateUserExportJob(ctx context.Context, request UserExportRequest, userID int64) (map[string]any, error) {
	normalized, err := normalizeUserExportRequest(request)
	if err != nil {
		return nil, err
	}
	if normalized.SensitiveMode == UserExportSensitiveFull {
		allowed, err := s.CanExportSensitive(ctx, userID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrForbidden
		}
	}
	job, err := s.jobs.CreateAsyncJob(ctx, UserExportJobType, normalized, userID)
	if err != nil {
		return nil, err
	}
	if err := s.outbox.Enqueue(ctx, UserExportEventType, "asyncJob", job.JobKey, job); err != nil {
		_ = s.jobs.MarkAsyncJobFailed(ctx, job.JobKey, err.Error(), map[string]any{"error": err.Error()})
		return nil, err
	}
	return map[string]any{"jobKey": job.JobKey}, nil
}

func (s *Service) CanExportSensitive(ctx context.Context, userID int64) (bool, error) {
	keys, err := s.repo.ResourceKeysForUser(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, key := range keys {
		if key == SensitiveExportResourceKey {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) GetUserJob(ctx context.Context, jobKey string, userID int64) (map[string]any, error) {
	job, err := s.jobs.GetAsyncJob(ctx, strings.TrimSpace(jobKey))
	if err != nil {
		return nil, err
	}
	if job.JobType != UserImportJobType && job.JobType != UserExportJobType {
		return nil, repo.ErrNotFound
	}
	if job.CreatedBy != userID {
		roleCode, roleErr := s.repo.UserRoleCode(ctx, userID)
		if roleErr != nil {
			return nil, roleErr
		}
		if roleCode != repo.BuiltinRoleAdmin {
			return nil, ErrForbidden
		}
	}
	result := map[string]any{}
	if len(job.ResultJSON) > 0 {
		_ = json.Unmarshal(job.ResultJSON, &result)
	}
	return map[string]any{
		"jobKey":       job.JobKey,
		"jobType":      job.JobType,
		"status":       job.Status,
		"progress":     job.Progress,
		"resultJson":   result,
		"errorMessage": job.ErrorMessage,
		"startedTime":  job.StartedTime,
		"finishedTime": job.FinishedTime,
	}, nil
}

func (s *Service) ImportUsersWorkbook(ctx context.Context, fileName string, raw []byte, actorID int64) (ImportWorkbookResult, error) {
	parsed, err := parseImportWorkbook(fileName, raw)
	if err != nil {
		return ImportWorkbookResult{}, err
	}
	result := map[string]any{
		"partial":     false,
		"users":       map[string]int{"success": 0, "failed": 0, "skipped": 0},
		"groups":      map[string]int{"success": 0, "failed": 0, "skipped": 0},
		"memberships": map[string]int{"success": 0, "failed": 0, "skipped": 0},
	}
	issues := append([]importIssue(nil), parsed.Issues...)
	for range parsed.Issues {
		incrementImportCount(result, "users", "failed")
	}
	existingMemberships := map[string]struct{}{}
	allMemberships, err := s.groups.ListAllMembers(ctx)
	if err != nil {
		return ImportWorkbookResult{}, err
	}
	for _, membership := range allMemberships {
		existingMemberships[fmt.Sprintf("%d:%d", membership.GroupID, membership.UserID)] = struct{}{}
	}
	groupByName := map[string]repo.UserGroup{}
	for _, row := range parsed.Users {
		user, getErr := s.repo.GetUserByUsername(ctx, row.Username)
		if getErr == nil {
			incrementImportCount(result, "users", "skipped")
		} else if !errors.Is(getErr, repo.ErrNotFound) {
			issues = append(issues, importIssue{Sheet: "Users", Row: row.Row, Values: row.Values, Field: "username", Reason: getErr.Error()})
			incrementImportCount(result, "users", "failed")
			continue
		} else {
			password := strings.TrimSpace(row.InitialPassword)
			if password == "" {
				password = "tier0"
			}
			created, createErr := s.CreateUser(ctx, UserSaveCommand{
				Username: row.Username, Password: password, FirstName: row.DisplayName,
				Email: row.Email, Phone: row.Phone, Enabled: row.Enabled,
				RoleList: []UserRoleCommand{{RoleID: repo.SystemRoleOperatorID, RoleName: repo.BuiltinRoleOperator}},
				UserID:   actorID, IsRandomPwd: true,
			})
			if createErr != nil {
				issues = append(issues, importIssue{Sheet: "Users", Row: row.Row, Values: row.Values, Field: "username", Reason: createErr.Error()})
				incrementImportCount(result, "users", "failed")
				continue
			}
			user.ID = anyInt64(created["userId"])
			incrementImportCount(result, "users", "success")
		}

		if row.GroupName == "" {
			continue
		}
		groupKey := strings.ToLower(row.GroupName)
		group, ok := groupByName[groupKey]
		if !ok {
			group, getErr = s.groups.GetByName(ctx, row.GroupName)
			if getErr == nil {
				incrementImportCount(result, "groups", "skipped")
			} else if !errors.Is(getErr, repo.ErrNotFound) {
				issues = append(issues, importIssue{Sheet: "Users", Row: row.Row, Values: row.Values, Field: "groupName", Reason: getErr.Error()})
				incrementImportCount(result, "groups", "failed")
				incrementImportCount(result, "memberships", "failed")
				continue
			} else {
				group, getErr = s.groups.Create(ctx, row.GroupName, "", actorID)
				if getErr != nil {
					issues = append(issues, importIssue{Sheet: "Users", Row: row.Row, Values: row.Values, Field: "groupName", Reason: getErr.Error()})
					incrementImportCount(result, "groups", "failed")
					incrementImportCount(result, "memberships", "failed")
					continue
				}
				incrementImportCount(result, "groups", "success")
			}
			groupByName[groupKey] = group
		}

		key := fmt.Sprintf("%d:%d", group.ID, user.ID)
		if _, exists := existingMemberships[key]; exists {
			incrementImportCount(result, "memberships", "skipped")
			continue
		}
		if addErr := s.groups.AddMember(ctx, group.ID, user.ID, actorID); addErr != nil {
			issues = append(issues, importIssue{Sheet: "Users", Row: row.Row, Values: row.Values, Field: "groupName", Reason: addErr.Error()})
			incrementImportCount(result, "memberships", "failed")
			continue
		}
		existingMemberships[key] = struct{}{}
		incrementImportCount(result, "memberships", "success")
	}

	if len(issues) > 0 {
		result["partial"] = true
		errorWorkbook, buildErr := buildImportErrorWorkbook(parsed.Headers, issues)
		if buildErr != nil {
			return ImportWorkbookResult{}, buildErr
		}
		return ImportWorkbookResult{Result: result, ErrorWorkbook: errorWorkbook}, nil
	}
	return ImportWorkbookResult{Result: result}, nil
}

func (s *Service) ExportUsersWorkbook(ctx context.Context, request UserExportRequest) ([]byte, map[string]any, error) {
	normalized, err := normalizeUserExportRequest(request)
	if err != nil {
		return nil, nil, err
	}
	users, groups, memberships, err := s.selectUserExportData(ctx, normalized)
	if err != nil {
		return nil, nil, err
	}
	groupNames := make(map[int64]string, len(groups))
	for _, group := range groups {
		groupNames[group.ID] = group.GroupName
	}
	groupNamesByUser := make(map[int64][]string)
	for _, membership := range memberships {
		if groupName := groupNames[membership.GroupID]; groupName != "" {
			groupNamesByUser[membership.UserID] = append(groupNamesByUser[membership.UserID], groupName)
		}
	}
	for userID := range groupNamesByUser {
		sort.Strings(groupNamesByUser[userID])
	}
	generatedAt := time.Now()
	exportedAt := generatedAt.UTC().Format(time.RFC3339)
	f := excelize.NewFile()
	defer f.Close()
	_ = f.SetSheetName(f.GetSheetName(0), "Users")
	usersHeader := []any{
		"userId", "username", "displayName", "email", "phone", "status",
		"roleCode", "roleName", "userGroups", "lastLoginAt", "createdAt", "exportedAt",
	}
	_ = f.SetSheetRow("Users", "A1", &usersHeader)
	for i, user := range users {
		email := anyString(user["email"])
		phone := anyString(user["phone"])
		if normalized.SensitiveMode != UserExportSensitiveFull {
			email = maskEmail(email)
			phone = maskPhone(phone)
		}
		roleCodes, roleNames := userRoleTexts(user)
		row := []any{
			anyInt64(user["userId"]),
			excelSafeString(anyString(user["userName"])),
			excelSafeString(anyString(user["nickName"])),
			excelOptionalString(email),
			excelOptionalString(phone),
			map[bool]string{true: "enabled", false: "disabled"}[anyInt64(user["status"]) == 1],
			excelOptionalString(strings.Join(roleCodes, ", ")),
			excelOptionalString(strings.Join(roleNames, ", ")),
			excelOptionalString(strings.Join(groupNamesByUser[anyInt64(user["userId"])], ", ")),
			excelOptionalString(formatExportTimestamp(anyInt64(user["lastLogin"]))),
			excelOptionalString(formatExportTimestamp(anyInt64(user["createdTime"]))),
			exportedAt,
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		_ = f.SetSheetRow("Users", cell, &row)
	}
	configureUserSheet(f, "Users", []float64{14, 32, 28, 36, 20, 12, 20, 22, 64, 26, 26, 26}, len(users)+1)
	buffer, err := f.WriteToBuffer()
	if err != nil {
		return nil, nil, err
	}
	result := map[string]any{
		"users": len(users), "groups": len(groups), "memberships": len(memberships),
		"scope": normalized.Scope, "sensitiveMode": normalized.SensitiveMode,
		"fileName": fmt.Sprintf("users-%s.xlsx", generatedAt.Format("20060102-1504")),
	}
	return buffer.Bytes(), result, nil
}

func configureUserSheet(f *excelize.File, sheet string, widths []float64, lastRow int) {
	for index, width := range widths {
		column, err := excelize.ColumnNumberToName(index + 1)
		if err == nil {
			_ = f.SetColWidth(sheet, column, column, width)
		}
	}
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	if len(widths) > 0 {
		lastColumn, err := excelize.ColumnNumberToName(len(widths))
		if err == nil {
			_ = f.AutoFilter(sheet, fmt.Sprintf("A1:%s%d", lastColumn, max(1, lastRow)), nil)
		}
	}
}

func userRoleTexts(user map[string]any) ([]string, []string) {
	roleCodes := make([]string, 0)
	roleNames := make([]string, 0)
	appendRole := func(role map[string]any) {
		if value := strings.TrimSpace(anyString(role["roleCode"])); value != "" {
			roleCodes = append(roleCodes, value)
		}
		if value := strings.TrimSpace(anyString(role["roleName"])); value != "" {
			roleNames = append(roleNames, value)
		}
	}
	switch roles := user["roleList"].(type) {
	case []map[string]any:
		for _, role := range roles {
			appendRole(role)
		}
	case []any:
		for _, item := range roles {
			if role, ok := item.(map[string]any); ok {
				appendRole(role)
			}
		}
	}
	return roleCodes, roleNames
}

func formatExportTimestamp(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.UnixMilli(value).UTC().Format(time.RFC3339)
}

func excelOptionalString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return excelSafeString(value)
}

func normalizeUserExportRequest(request UserExportRequest) (UserExportRequest, error) {
	request.Scope = strings.ToLower(strings.TrimSpace(request.Scope))
	if request.Scope == "" {
		request.Scope = UserExportScopeFiltered
	}
	request.SensitiveMode = strings.ToLower(strings.TrimSpace(request.SensitiveMode))
	if request.SensitiveMode == "" {
		request.SensitiveMode = UserExportSensitiveMasked
	}
	request.Keyword = strings.TrimSpace(request.Keyword)
	request.UserIDs = uniquePositiveInt64s(request.UserIDs)
	request.GroupIDs = uniquePositiveInt64s(request.GroupIDs)
	request.RoleIDs = uniquePositiveInt64s(request.RoleIDs)
	for _, status := range request.Statuses {
		status = strings.ToLower(strings.TrimSpace(status))
		if status != "enabled" && status != "disabled" {
			return UserExportRequest{}, repo.ErrInvalidArgument
		}
	}
	request.Statuses = uniqueExportStatuses(request.Statuses)
	switch request.Scope {
	case UserExportScopeSelected:
		if len(request.UserIDs) == 0 {
			return UserExportRequest{}, repo.ErrInvalidArgument
		}
	case UserExportScopeGroups:
		if len(request.GroupIDs) == 0 {
			return UserExportRequest{}, repo.ErrInvalidArgument
		}
	case UserExportScopeFiltered, UserExportScopeAll:
	default:
		return UserExportRequest{}, repo.ErrInvalidArgument
	}
	if request.SensitiveMode != UserExportSensitiveMasked && request.SensitiveMode != UserExportSensitiveFull {
		return UserExportRequest{}, repo.ErrInvalidArgument
	}
	return request, nil
}

func (s *Service) selectUserExportData(ctx context.Context, request UserExportRequest) ([]map[string]any, []repo.UserGroup, []repo.UserGroupMember, error) {
	allUsers, err := s.repo.ListUsers(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	allGroups, _, err := s.groups.List(ctx, "", 1, 1_000_000_000)
	if err != nil {
		return nil, nil, nil, err
	}
	allMemberships, err := s.groups.ListAllMembers(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	groupsByUser := make(map[int64]map[int64]struct{})
	for _, membership := range allMemberships {
		if groupsByUser[membership.UserID] == nil {
			groupsByUser[membership.UserID] = map[int64]struct{}{}
		}
		groupsByUser[membership.UserID][membership.GroupID] = struct{}{}
	}
	selectedIDs := map[int64]struct{}{}
	switch request.Scope {
	case UserExportScopeSelected:
		for _, userID := range request.UserIDs {
			selectedIDs[userID] = struct{}{}
		}
	case UserExportScopeGroups:
		groupSet := int64Set(request.GroupIDs)
		for _, membership := range allMemberships {
			if _, ok := groupSet[membership.GroupID]; ok {
				selectedIDs[membership.UserID] = struct{}{}
			}
		}
	case UserExportScopeAll:
		for _, user := range allUsers {
			if !strings.EqualFold(anyString(user["userName"]), "tier0") {
				selectedIDs[anyInt64(user["userId"])] = struct{}{}
			}
		}
	case UserExportScopeFiltered:
		for _, user := range allUsers {
			if matchesUserExportFilters(user, groupsByUser[anyInt64(user["userId"])], request) {
				selectedIDs[anyInt64(user["userId"])] = struct{}{}
			}
		}
	}
	users := make([]map[string]any, 0, len(selectedIDs))
	for _, user := range allUsers {
		if _, ok := selectedIDs[anyInt64(user["userId"])]; ok {
			users = append(users, user)
		}
	}
	memberships := make([]repo.UserGroupMember, 0)
	relevantGroupIDs := map[int64]struct{}{}
	if request.Scope == UserExportScopeGroups {
		for _, groupID := range request.GroupIDs {
			relevantGroupIDs[groupID] = struct{}{}
		}
	}
	for _, membership := range allMemberships {
		if _, ok := selectedIDs[membership.UserID]; !ok {
			continue
		}
		memberships = append(memberships, membership)
		relevantGroupIDs[membership.GroupID] = struct{}{}
	}
	groups := make([]repo.UserGroup, 0, len(relevantGroupIDs))
	for _, group := range allGroups {
		if _, ok := relevantGroupIDs[group.ID]; ok {
			groups = append(groups, group)
		}
	}
	return users, groups, memberships, nil
}

func matchesUserExportFilters(user map[string]any, groupIDs map[int64]struct{}, request UserExportRequest) bool {
	if keyword := strings.ToLower(request.Keyword); keyword != "" {
		matched := false
		for _, field := range []string{"userName", "nickName"} {
			if strings.Contains(strings.ToLower(anyString(user[field])), keyword) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(request.Statuses) == 1 {
		if request.Statuses[0] == "enabled" && anyInt64(user["status"]) != 1 {
			return false
		}
		if request.Statuses[0] == "disabled" && anyInt64(user["status"]) == 1 {
			return false
		}
	}
	if len(request.RoleIDs) > 0 && !userHasAnyRole(user, request.RoleIDs) {
		return false
	}
	if len(request.GroupIDs) > 0 {
		matched := false
		for _, groupID := range request.GroupIDs {
			if _, ok := groupIDs[groupID]; ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func userHasRole(user map[string]any, roleID int64) bool {
	switch roles := user["roleList"].(type) {
	case []map[string]any:
		for _, role := range roles {
			if anyInt64(role["roleId"]) == roleID {
				return true
			}
		}
	case []any:
		for _, item := range roles {
			if role, ok := item.(map[string]any); ok && anyInt64(role["roleId"]) == roleID {
				return true
			}
		}
	}
	return false
}

func userHasAnyRole(user map[string]any, roleIDs []int64) bool {
	for _, roleID := range roleIDs {
		if userHasRole(user, roleID) {
			return true
		}
	}
	return false
}

func uniqueExportStatuses(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "enabled" && value != "disabled" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uniquePositiveInt64s(values []int64) []int64 {
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func int64Set(values []int64) map[int64]struct{} {
	out := make(map[int64]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func maskEmail(value string) string {
	value = strings.TrimSpace(value)
	parts := strings.SplitN(value, "@", 2)
	if len(parts) != 2 || parts[0] == "" {
		return maskText(value)
	}
	local := []rune(parts[0])
	return string(local[:1]) + "***@" + parts[1]
}

func maskPhone(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) < 7 {
		return maskText(value)
	}
	return string(runes[:3]) + "****" + string(runes[len(runes)-4:])
}

func maskText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) == 1 {
		return "*"
	}
	return string(runes[:1]) + "***"
}

func parseImportWorkbook(fileName string, raw []byte) (importWorkbook, error) {
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(fileName)), ".xlsx") {
		return importWorkbook{}, fmt.Errorf("only .xlsx files are supported")
	}
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return importWorkbook{}, fmt.Errorf("open workbook: %w", err)
	}
	defer f.Close()
	parsed := importWorkbook{}
	index, indexErr := f.GetSheetIndex("Users")
	if indexErr != nil || index == -1 {
		return importWorkbook{}, errors.New("missing required sheet Users")
	}
	users, headers, err := workbookRows(f, "Users", []string{"username", "displayName", "email", "phone", "enabled", "initialPassword", "groupName"})
	if err != nil {
		return importWorkbook{}, err
	}
	parsed.Headers = headers
	for _, item := range users {
		username := strings.TrimSpace(item.value("username"))
		if username == "" {
			parsed.Issues = append(parsed.Issues, importIssue{Sheet: "Users", Row: item.row, Values: item.values, Field: "username", Reason: "username is required"})
			continue
		}
		enabled, err := parseImportBool(item.value("enabled"), true)
		if err != nil {
			parsed.Issues = append(parsed.Issues, importIssue{Sheet: "Users", Row: item.row, Values: item.values, Field: "enabled", Reason: "enabled must be true or false"})
			continue
		}
		displayName := strings.TrimSpace(item.value("displayName"))
		if displayName == "" {
			displayName = username
		}
		groupName := strings.TrimSpace(item.value("groupName"))
		if domaincommon.ExceedNameLimit(groupName) {
			parsed.Issues = append(parsed.Issues, importIssue{Sheet: "Users", Row: item.row, Values: item.values, Field: "groupName", Reason: fmt.Sprintf("groupName must not exceed %d characters", domaincommon.NameMaxLen)})
			continue
		}
		parsed.Users = append(parsed.Users, importUserRow{
			Row: item.row, Values: item.values, Username: username, DisplayName: displayName,
			Email: strings.TrimSpace(item.value("email")), Phone: strings.TrimSpace(item.value("phone")),
			Enabled: enabled, InitialPassword: item.value("initialPassword"), GroupName: groupName,
		})
	}
	return parsed, nil
}

type workbookRow struct {
	row     int
	values  []string
	headers map[string]int
}

func (r workbookRow) value(name string) string {
	index, ok := r.headers[name]
	if !ok || index >= len(r.values) {
		return ""
	}
	return r.values[index]
}

func workbookRows(f *excelize.File, sheet string, expectedHeaders []string) ([]workbookRow, []string, error) {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("%s sheet is empty", sheet)
	}
	headers := make(map[string]int, len(rows[0]))
	for index, header := range rows[0] {
		headers[strings.TrimSpace(header)] = index
	}
	for _, header := range expectedHeaders {
		if _, ok := headers[header]; !ok {
			return nil, nil, fmt.Errorf("%s sheet missing column %s", sheet, header)
		}
	}
	out := make([]workbookRow, 0, len(rows)-1)
	for index, values := range rows[1:] {
		blank := true
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				blank = false
				break
			}
		}
		if blank {
			continue
		}
		padded := append([]string(nil), values...)
		for len(padded) < len(rows[0]) {
			padded = append(padded, "")
		}
		out = append(out, workbookRow{row: index + 2, values: padded, headers: headers})
	}
	return out, append([]string(nil), rows[0]...), nil
}

func parseImportBool(value string, defaultValue bool) (bool, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return defaultValue, nil
	}
	switch value {
	case "true", "1", "yes", "y", "是", "启用":
		return true, nil
	case "false", "0", "no", "n", "否", "禁用":
		return false, nil
	default:
		return false, errors.New("invalid boolean")
	}
}

func buildImportErrorWorkbook(headers []string, issues []importIssue) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	_ = f.SetSheetName(f.GetSheetName(0), "Users")
	header := append(append([]string(nil), headers...), "errorReason")
	headerValues := make([]any, len(header))
	for i := range header {
		headerValues[i] = header[i]
	}
	_ = f.SetSheetRow("Users", "A1", &headerValues)
	passwordIndex := -1
	headerIndexes := make(map[string]int, len(headers))
	for index, name := range headers {
		headerIndexes[strings.TrimSpace(name)] = index
		if name == "initialPassword" {
			passwordIndex = index
		}
	}
	errorStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Color: "#B42318"},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#FEE4E2"}},
	})
	if err != nil {
		return nil, err
	}
	errorReasonColumn := len(headers) + 1
	rowIndex := 2
	for _, issue := range issues {
		values := make([]any, 0, len(issue.Values)+1)
		for index, value := range issue.Values {
			if index == passwordIndex && strings.TrimSpace(value) != "" {
				value = "********"
			}
			values = append(values, excelOptionalString(value))
		}
		values = append(values, fmt.Sprintf("row %d: %s", issue.Row, issue.Reason))
		cell, _ := excelize.CoordinatesToCellName(1, rowIndex)
		_ = f.SetSheetRow("Users", cell, &values)
		if fieldIndex, ok := headerIndexes[issue.Field]; ok {
			fieldCell, coordinateErr := excelize.CoordinatesToCellName(fieldIndex+1, rowIndex)
			if coordinateErr != nil {
				return nil, coordinateErr
			}
			if styleErr := f.SetCellStyle("Users", fieldCell, fieldCell, errorStyle); styleErr != nil {
				return nil, styleErr
			}
		}
		rowIndex++
	}
	errorReasonStart, err := excelize.CoordinatesToCellName(errorReasonColumn, 1)
	if err != nil {
		return nil, err
	}
	errorReasonEnd, err := excelize.CoordinatesToCellName(errorReasonColumn, rowIndex-1)
	if err != nil {
		return nil, err
	}
	if err := f.SetCellStyle("Users", errorReasonStart, errorReasonEnd, errorStyle); err != nil {
		return nil, err
	}
	configureUserSheet(f, "Users", []float64{26, 18, 24, 14, 12, 24, 36, 48}, rowIndex-1)
	buffer, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func incrementImportCount(result map[string]any, section, field string) {
	counts, _ := result[section].(map[string]int)
	counts[field]++
}

func excelSafeString(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}

func anyString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func anyInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	default:
		return 0
	}
}
