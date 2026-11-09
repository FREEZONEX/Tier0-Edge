package iam

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"backend/internal/repo"
)

const (
	platformWorkspaceID        = int64(1)
	platformMembersDefaultPage = int64(1)
	platformMembersDefaultSize = int64(20)
	platformMembersMaxSize     = int64(100)
)

type PlatformMembersQuery struct {
	Keyword        string
	RoleKey        string
	Roles          []string
	Statuses       []string
	UpdatedAtStart string
	UpdatedAtEnd   string
	Page           int64
	Size           int64
}

type PlatformMemberRole struct {
	RoleID      string `json:"roleId"`
	RoleKey     string `json:"roleKey"`
	RoleName    string `json:"roleName,omitempty"`
	Description string `json:"description,omitempty"`
}

type PlatformMember struct {
	UserID    string               `json:"userId"`
	UserName  string               `json:"userName,omitempty"`
	NickName  string               `json:"nickName,omitempty"`
	Email     string               `json:"email,omitempty"`
	Status    string               `json:"status"`
	Roles     []PlatformMemberRole `json:"roles"`
	UpdatedAt string               `json:"updatedAt"`
}

type PlatformMembersPage struct {
	List  []PlatformMember `json:"list"`
	Total int64            `json:"total"`
	Page  int64            `json:"page"`
	Size  int64            `json:"size"`
}

type platformMemberAggregate struct {
	userID             int64
	userName           string
	nickName           string
	email              string
	status             string
	roles              []PlatformMemberRole
	roleKeys           map[string]struct{}
	roleIDs            map[int64]struct{}
	effectiveUpdatedAt time.Time
}

func (s *Service) PlatformMembers(ctx context.Context, query PlatformMembersQuery) (PlatformMembersPage, error) {
	page, size := normalizePlatformMembersPage(query.Page, query.Size)
	updatedAtStart, err := parsePlatformMembersTime(query.UpdatedAtStart, "updatedAtStart.invalid")
	if err != nil {
		return PlatformMembersPage{}, err
	}
	updatedAtEnd, updatedAtEndExclusive, err := parsePlatformMembersEndTime(query.UpdatedAtEnd)
	if err != nil {
		return PlatformMembersPage{}, err
	}
	statusFilter, err := normalizePlatformMemberStatuses(query.Statuses)
	if err != nil {
		return PlatformMembersPage{}, err
	}
	roleFilter := normalizePlatformMemberRoleFilters(query.RoleKey, query.Roles)

	rows, err := s.repo.ListPlatformMemberRows(ctx, platformWorkspaceID)
	if err != nil {
		return PlatformMembersPage{}, err
	}

	keyword := strings.ToLower(strings.TrimSpace(query.Keyword))
	membersByID := make(map[int64]*platformMemberAggregate, len(rows))
	for _, row := range rows {
		status := enterprisePlatformUserStatus(row.Status)
		if len(statusFilter) > 0 {
			if _, ok := statusFilter[status]; !ok {
				continue
			}
		}
		if !enterprisePlatformMemberMatchesKeyword(row, keyword) {
			continue
		}

		member := membersByID[row.UserID]
		if member == nil {
			member = &platformMemberAggregate{
				userID:   row.UserID,
				userName: row.UserName,
				nickName: row.NickName,
				email:    row.Email,
				status:   status,
				roles:    make([]PlatformMemberRole, 0, 1),
				roleKeys: make(map[string]struct{}),
				roleIDs:  make(map[int64]struct{}),
			}
			membersByID[row.UserID] = member
		}

		roleKey := strings.TrimSpace(row.RoleCode)
		member.roleKeys[strings.ToLower(roleKey)] = struct{}{}
		if _, exists := member.roleIDs[row.RoleID]; !exists {
			member.roleIDs[row.RoleID] = struct{}{}
			member.roles = append(member.roles, PlatformMemberRole{
				RoleID:      strconv.FormatInt(row.RoleID, 10),
				RoleKey:     roleKey,
				RoleName:    row.RoleName,
				Description: row.RoleDescription,
			})
		}
		member.effectiveUpdatedAt = maxPlatformMemberTime(
			member.effectiveUpdatedAt,
			row.UserUpdatedTime,
			row.MemberUpdatedTime,
			row.RoleUpdatedTime,
		)
	}

	filtered := make([]*platformMemberAggregate, 0, len(membersByID))
	for _, member := range membersByID {
		if !platformMemberRoleMatches(member.roleKeys, roleFilter) {
			continue
		}
		if !platformMemberTimeMatches(member.effectiveUpdatedAt, updatedAtStart, updatedAtEnd, updatedAtEndExclusive) {
			continue
		}
		sort.Slice(member.roles, func(i, j int) bool {
			left := strings.ToLower(member.roles[i].RoleKey)
			right := strings.ToLower(member.roles[j].RoleKey)
			if left == right {
				return member.roles[i].RoleID < member.roles[j].RoleID
			}
			return left < right
		})
		filtered = append(filtered, member)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].effectiveUpdatedAt.Equal(filtered[j].effectiveUpdatedAt) {
			return filtered[i].userID < filtered[j].userID
		}
		return filtered[i].effectiveUpdatedAt.After(filtered[j].effectiveUpdatedAt)
	})

	total := int64(len(filtered))
	start, end := platformMembersPageBounds(total, page, size)
	list := make([]PlatformMember, 0, end-start)
	for _, member := range filtered[start:end] {
		list = append(list, PlatformMember{
			UserID:    strconv.FormatInt(member.userID, 10),
			UserName:  member.userName,
			NickName:  member.nickName,
			Email:     member.email,
			Status:    member.status,
			Roles:     member.roles,
			UpdatedAt: member.effectiveUpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return PlatformMembersPage{List: list, Total: total, Page: page, Size: size}, nil
}

func normalizePlatformMembersPage(page, size int64) (int64, int64) {
	if page <= 0 {
		page = platformMembersDefaultPage
	}
	if size <= 0 {
		size = platformMembersDefaultSize
	}
	if size > platformMembersMaxSize {
		size = platformMembersMaxSize
	}
	return page, size
}

func platformMembersPageBounds(total, page, size int64) (int, int) {
	start := total
	if total > 0 && page <= (total-1)/size+1 {
		start = (page - 1) * size
	}
	end := start + size
	if end > total {
		end = total
	}
	return int(start), int(end)
}

func normalizePlatformMemberStatuses(values []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		switch value {
		case "":
			continue
		case "active", "disabled":
			result[value] = struct{}{}
		default:
			return nil, fmt.Errorf("%w: statuses.invalid", repo.ErrInvalidArgument)
		}
	}
	return result, nil
}

func enterprisePlatformUserStatus(status int64) string {
	if status == 1 {
		return "active"
	}
	return "disabled"
}

func normalizePlatformMemberRoleFilters(roleKey string, roles []string) map[string]struct{} {
	result := make(map[string]struct{}, len(roles)+1)
	for _, value := range append([]string{roleKey}, roles...) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func enterprisePlatformMemberMatchesKeyword(row repo.PlatformMemberRow, keyword string) bool {
	if keyword == "" {
		return true
	}
	if userID, err := strconv.ParseInt(keyword, 10, 64); err == nil && row.UserID == userID {
		return true
	}
	return strings.Contains(strings.ToLower(row.UserName), keyword) ||
		strings.Contains(strings.ToLower(row.NickName), keyword) ||
		strings.Contains(strings.ToLower(row.Email), keyword)
}

func platformMemberRoleMatches(memberRoles, filters map[string]struct{}) bool {
	if len(filters) == 0 {
		return true
	}
	for roleKey := range memberRoles {
		if _, ok := filters[roleKey]; ok {
			return true
		}
	}
	return false
}

func maxPlatformMemberTime(values ...time.Time) time.Time {
	var result time.Time
	for _, value := range values {
		if value.After(result) {
			result = value
		}
	}
	return result
}

func parsePlatformMembersTime(value, message string) (*time.Time, error) {
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

func parsePlatformMembersEndTime(value string) (*time.Time, bool, error) {
	parsed, err := parsePlatformMembersTime(value, "updatedAtEnd.invalid")
	if err != nil || parsed == nil {
		return parsed, false, err
	}
	if !platformMembersTimeHasFraction(value) {
		exclusive := parsed.Add(time.Second)
		return &exclusive, true, nil
	}
	return parsed, false, nil
}

func platformMembersTimeHasFraction(value string) bool {
	timeStart := strings.IndexByte(value, 'T')
	if timeStart < 0 {
		return false
	}
	for i := timeStart + 1; i < len(value); i++ {
		switch value[i] {
		case '.':
			return true
		case 'Z', '+', '-':
			return false
		}
	}
	return false
}

func platformMemberTimeMatches(value time.Time, start, end *time.Time, endExclusive bool) bool {
	if start != nil && value.Before(*start) {
		return false
	}
	if end == nil {
		return true
	}
	if endExclusive {
		return value.Before(*end)
	}
	return !value.After(*end)
}
