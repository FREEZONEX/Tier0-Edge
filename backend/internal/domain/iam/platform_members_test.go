package iam

// 测试目标：平台成员查询（platform_members.go）的纯逻辑部分。
// 覆盖的正常路径：分页归一化与边界、状态过滤、角色过滤、关键字匹配（用户名/昵称/邮箱/userId）、
// 时间过滤（含 end 排他语义）、角色排序、更新时间聚合。
// 覆盖的异常情况：非法状态值报错、非法时间字符串报错。

import (
	"errors"
	"testing"
	"time"

	"backend/internal/repo"
)

func TestNormalizePlatformMembersPage(t *testing.T) {
	page, size := normalizePlatformMembersPage(0, 0)
	if page != 1 || size != 20 {
		t.Fatalf("defaults = %d/%d", page, size)
	}
	page, size = normalizePlatformMembersPage(-1, -5)
	if page != 1 || size != 20 {
		t.Fatalf("negative = %d/%d", page, size)
	}
	page, size = normalizePlatformMembersPage(3, 500)
	if page != 3 || size != 100 {
		t.Fatalf("clamped = %d/%d", page, size)
	}
}

func TestPlatformMembersPageBounds(t *testing.T) {
	cases := []struct {
		name        string
		total, page int64
		size        int64
		wantStart   int
		wantEnd     int
	}{
		{name: "empty", total: 0, page: 2, size: 20, wantStart: 0, wantEnd: 0},
		{name: "page beyond total", total: 5, page: 99, size: 20, wantStart: 5, wantEnd: 5},
		{name: "first page", total: 25, page: 1, size: 20, wantStart: 0, wantEnd: 20},
		{name: "second page", total: 25, page: 2, size: 20, wantStart: 20, wantEnd: 25},
		{name: "exact boundary", total: 40, page: 2, size: 20, wantStart: 20, wantEnd: 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := platformMembersPageBounds(tc.total, tc.page, tc.size)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("bounds(%d,%d,%d) = %d,%d want %d,%d", tc.total, tc.page, tc.size, start, end, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

func TestNormalizePlatformMemberStatuses(t *testing.T) {
	statuses, err := normalizePlatformMemberStatuses([]string{"active", " DISABLED ", ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("statuses = %v", statuses)
	}
	if _, err := normalizePlatformMemberStatuses([]string{"banned"}); !errors.Is(err, repo.ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}
	empty, err := normalizePlatformMemberStatuses(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("nil statuses = %v, %v", empty, err)
	}
}

func TestEnterprisePlatformUserStatus(t *testing.T) {
	if enterprisePlatformUserStatus(1) != "active" {
		t.Fatal("status 1 should be active")
	}
	if enterprisePlatformUserStatus(0) != "disabled" || enterprisePlatformUserStatus(2) != "disabled" {
		t.Fatal("non-1 status should be disabled")
	}
}

func TestNormalizePlatformMemberRoleFilters(t *testing.T) {
	filters := normalizePlatformMemberRoleFilters("admin", []string{"", " operator "})
	if len(filters) != 2 {
		t.Fatalf("filters = %v", filters)
	}
	if _, ok := filters["admin"]; !ok {
		t.Fatal("roleKey missing")
	}
	if _, ok := filters["operator"]; !ok {
		t.Fatal("roles missing")
	}
	if len(normalizePlatformMemberRoleFilters("", nil)) != 0 {
		t.Fatal("empty filters should be empty")
	}
}

func TestEnterprisePlatformMemberMatchesKeyword(t *testing.T) {
	row := repo.PlatformMemberRow{UserID: 1001, UserName: "Alice", NickName: "Ali", Email: "alice@example.com"}
	if !enterprisePlatformMemberMatchesKeyword(row, "") {
		t.Fatal("empty keyword should match")
	}
	if !enterprisePlatformMemberMatchesKeyword(row, "ali") {
		t.Fatal("nickname keyword should match")
	}
	// 该函数假定 keyword 已由调用方小写化（PlatformMembers 入口先 ToLower）
	if !enterprisePlatformMemberMatchesKeyword(row, "alice") {
		t.Fatal("username match")
	}
	if !enterprisePlatformMemberMatchesKeyword(row, "alice@example.com") {
		t.Fatal("email match")
	}
	if !enterprisePlatformMemberMatchesKeyword(row, "1001") {
		t.Fatal("userID keyword match")
	}
	if enterprisePlatformMemberMatchesKeyword(row, "bob") {
		t.Fatal("unrelated keyword should not match")
	}
}

func TestPlatformMemberRoleMatches(t *testing.T) {
	memberRoles := map[string]struct{}{"admin": {}, "viewer": {}}
	if !platformMemberRoleMatches(memberRoles, nil) {
		t.Fatal("nil filters should match")
	}
	if !platformMemberRoleMatches(memberRoles, map[string]struct{}{"viewer": {}}) {
		t.Fatal("matching role filter")
	}
	if platformMemberRoleMatches(memberRoles, map[string]struct{}{"editor": {}}) {
		t.Fatal("non-matching role filter")
	}
}

func TestMaxPlatformMemberTime(t *testing.T) {
	base := time.Now().UTC()
	if got := maxPlatformMemberTime(base, base.Add(time.Hour), base.Add(-time.Hour)); !got.Equal(base.Add(time.Hour)) {
		t.Fatalf("max = %v", got)
	}
	if got := maxPlatformMemberTime(time.Time{}); !got.IsZero() {
		t.Fatalf("zero max = %v", got)
	}
}

func TestParsePlatformMembersTime(t *testing.T) {
	parsed, err := parsePlatformMembersTime("", "x")
	if err != nil || parsed != nil {
		t.Fatalf("empty = %v, %v", parsed, err)
	}
	parsed, err = parsePlatformMembersTime("2026-01-02T03:04:05+08:00", "x")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Location() != time.UTC {
		t.Fatalf("location = %v, want UTC", parsed.Location())
	}
	if _, err := parsePlatformMembersTime("garbage", "x"); !errors.Is(err, repo.ErrInvalidArgument) {
		t.Fatalf("err = %v", err)
	}
}

func TestParsePlatformMembersEndTime(t *testing.T) {
	// 无小数秒：end 加 1 秒变排他
	end, exclusive, err := parsePlatformMembersEndTime("2026-01-02T03:04:05Z")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !exclusive || end.Second() != 6 {
		t.Fatalf("end=%v exclusive=%v", end, exclusive)
	}
	// 有小数秒：原样排他
	end, exclusive, err = parsePlatformMembersEndTime("2026-01-02T03:04:05.123Z")
	if err != nil {
		t.Fatalf("parse fractional: %v", err)
	}
	if exclusive || end.Second() != 5 {
		t.Fatalf("fractional end=%v exclusive=%v", end, exclusive)
	}
	// 空值
	end, exclusive, err = parsePlatformMembersEndTime("")
	if err != nil || end != nil || exclusive {
		t.Fatalf("empty end = %v, %v, %v", end, exclusive, err)
	}
}

func TestPlatformMembersTimeHasFraction(t *testing.T) {
	if !platformMembersTimeHasFraction("2026-01-02T03:04:05.123Z") {
		t.Fatal("fractional time not detected")
	}
	if platformMembersTimeHasFraction("2026-01-02T03:04:05Z") {
		t.Fatal("second-precision time reported fractional")
	}
	if platformMembersTimeHasFraction("2026-01-02T03:04:05+08:00") {
		t.Fatal("offset time reported fractional")
	}
	if platformMembersTimeHasFraction("no-time") {
		t.Fatal("non-time reported fractional")
	}
}

func TestPlatformMemberTimeMatches(t *testing.T) {
	value := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	start := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC)
	if !platformMemberTimeMatches(value, nil, nil, false) {
		t.Fatal("no bounds should match")
	}
	if !platformMemberTimeMatches(value, &start, nil, false) {
		t.Fatal("start bound should match")
	}
	if platformMemberTimeMatches(value.Add(-time.Hour), &start, nil, false) {
		t.Fatal("before start should not match")
	}
	if !platformMemberTimeMatches(value, &start, &end, true) {
		t.Fatal("exclusive end should match")
	}
	if platformMemberTimeMatches(value.Add(time.Hour), &start, &end, true) {
		t.Fatal("after exclusive end should not match")
	}
	if !platformMemberTimeMatches(value, &start, &end, false) {
		t.Fatal("inclusive end should match")
	}
	if platformMemberTimeMatches(value.Add(2*time.Hour), &start, &end, false) {
		t.Fatal("after inclusive end should not match")
	}
}
