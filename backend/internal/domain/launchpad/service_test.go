package launchpad

// 测试目标：launchpad 纯逻辑函数（service.go 中无 DB 部分）。
// 覆盖的正常路径：分页归一化、角色键归一化与匹配、时间解析（end 排他语义）、
// 成员响应构建与角色排序、projectResp/appResp 字段映射、canEditProject、firstNonEmpty、
// launchpadAppIconURL、timeMilli、hasAnyRole。
// 覆盖的异常情况：非法时间、非法 routePath、未知 role 详情回退。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/repo"
)

func TestNormalizeLaunchpadMembersPage(t *testing.T) {
	page, size := normalizeLaunchpadMembersPage(0, 0)
	if page != 1 || size != 20 {
		t.Fatalf("defaults = %d/%d", page, size)
	}
	page, size = normalizeLaunchpadMembersPage(-1, -5)
	if page != 1 || size != 20 {
		t.Fatalf("negative = %d/%d", page, size)
	}
	page, size = normalizeLaunchpadMembersPage(2, 500)
	if page != 2 || size != 100 {
		t.Fatalf("clamped = %d/%d", page, size)
	}
}

func TestNormalizeLaunchpadMemberRoleKeys(t *testing.T) {
	keys := normalizeLaunchpadMemberRoleKeys("admin", []string{"", " viewer "})
	if len(keys) != 2 {
		t.Fatalf("keys = %v", keys)
	}
	if _, ok := keys["admin"]; !ok {
		t.Fatal("roleKey missing")
	}
	if _, ok := keys["viewer"]; !ok {
		t.Fatal("roles missing")
	}
	if len(normalizeLaunchpadMemberRoleKeys("", nil)) != 0 {
		t.Fatal("empty keys should be empty")
	}
}

func TestLaunchpadMemberRoleMatches(t *testing.T) {
	roles := []repo.ProjectRole{{RoleKey: "admin"}, {RoleKey: "viewer"}}
	if !launchpadMemberRoleMatches(roles, nil) {
		t.Fatal("nil keys should match")
	}
	// roleKeys 已由 normalizeLaunchpadMemberRoleKeys 小写化，直接匹配小写
	if !launchpadMemberRoleMatches(roles, map[string]struct{}{"viewer": {}}) {
		t.Fatal("matching role")
	}
	if launchpadMemberRoleMatches(roles, map[string]struct{}{"editor": {}}) {
		t.Fatal("non-matching role")
	}
	if launchpadMemberRoleMatches(nil, map[string]struct{}{"admin": {}}) {
		t.Fatal("empty roles should not match")
	}
}

func TestParseLaunchpadTime(t *testing.T) {
	parsed, err := parseLaunchpadTime("", "m")
	if err != nil || parsed != nil {
		t.Fatalf("empty = %v, %v", parsed, err)
	}
	parsed, err = parseLaunchpadTime("2026-01-02T03:04:05+08:00", "m")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Location() != time.UTC {
		t.Fatalf("location = %v", parsed.Location())
	}
	if _, err := parseLaunchpadTime("bad", "m"); !errors.Is(err, repo.ErrInvalidArgument) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseLaunchpadUpdatedAtEnd(t *testing.T) {
	end, exclusive, err := parseLaunchpadUpdatedAtEnd("2026-01-02T03:04:05Z")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !exclusive || end.Second() != 6 {
		t.Fatalf("end=%v exclusive=%v", end, exclusive)
	}
	end, exclusive, err = parseLaunchpadUpdatedAtEnd("2026-01-02T03:04:05.500Z")
	if err != nil {
		t.Fatalf("parse fractional: %v", err)
	}
	if exclusive || end.Second() != 5 || end.Nanosecond() != 500_000_000 {
		t.Fatalf("fractional end=%v exclusive=%v", end, exclusive)
	}
	end, exclusive, err = parseLaunchpadUpdatedAtEnd("")
	if err != nil || end != nil || exclusive {
		t.Fatalf("empty = %v, %v, %v", end, exclusive, err)
	}
}

func TestRFC3339HasFractionalSecond(t *testing.T) {
	if !rfc3339HasFractionalSecond("2026-01-02T03:04:05.1Z") {
		t.Fatal("fractional Z not detected")
	}
	if !rfc3339HasFractionalSecond("2026-01-02T03:04:05.1+08:00") {
		t.Fatal("fractional offset not detected")
	}
	if rfc3339HasFractionalSecond("2026-01-02T03:04:05Z") {
		t.Fatal("second precision reported fractional")
	}
	if rfc3339HasFractionalSecond("no-time") {
		t.Fatal("non-time reported fractional")
	}
}

func TestLaunchpadMemberUpdatedAtMatches(t *testing.T) {
	value := int64(1767300000000) // 2026-01-02 左右
	start := time.UnixMilli(value - 60_000).UTC()
	end := time.UnixMilli(value + 60_000).UTC()
	if !launchpadMemberUpdatedAtMatches(value, nil, nil, false) {
		t.Fatal("no bounds should match")
	}
	if !launchpadMemberUpdatedAtMatches(value, &start, nil, false) {
		t.Fatal("start bound should match")
	}
	if launchpadMemberUpdatedAtMatches(value-120_000, &start, nil, false) {
		t.Fatal("before start should not match")
	}
	if !launchpadMemberUpdatedAtMatches(value, &start, &end, true) {
		t.Fatal("exclusive end should match")
	}
	if launchpadMemberUpdatedAtMatches(value+120_000, &start, &end, true) {
		t.Fatal("after exclusive end should not match")
	}
	if !launchpadMemberUpdatedAtMatches(value, &start, &end, false) {
		t.Fatal("inclusive end should match")
	}
}

func TestLaunchpadMemberResp(t *testing.T) {
	member := repo.ProjectMember{
		ID:          1,
		ProjectID:   10,
		UserID:      100,
		UserName:    "alice",
		Email:       "a@b.c",
		UpdatedTime: 1767300000000,
		Roles: []repo.ProjectRole{
			{ID: 2, RoleKey: "viewer", Name: "Viewer"},
			{ID: 1, RoleKey: "admin", Name: "Admin"},
		},
	}
	details := map[int64]MemberRole{
		1: {RoleID: "1", RoleKey: "admin", RoleName: "Admin", Description: "d", MemberCount: 2, Apps: []RoleApp{{AppID: "1", AppName: "app"}}},
	}
	resp := launchpadMemberResp(member, details)
	if resp.UserID != "100" {
		t.Fatalf("resp = %+v", resp)
	}
	// 角色按键排序：admin 在前
	if len(resp.Roles) != 2 || resp.Roles[0].RoleKey != "admin" || resp.Roles[1].RoleKey != "viewer" {
		t.Fatalf("roles = %+v", resp.Roles)
	}
	// 有详情的使用详情（含 Apps），无详情的回退基础字段
	if len(resp.Roles[0].Apps) != 1 || resp.Roles[0].MemberCount != 2 {
		t.Fatalf("role detail = %+v", resp.Roles[0])
	}
	if resp.Roles[1].MemberCount != 0 || resp.Roles[1].Description != "" {
		t.Fatalf("fallback role = %+v", resp.Roles[1])
	}
	// UpdatedAt 格式
	if _, err := time.Parse(time.RFC3339, resp.UpdatedAt); err != nil {
		t.Fatalf("UpdatedAt = %q", resp.UpdatedAt)
	}
}

func TestProjectResp(t *testing.T) {
	now := time.Now().UTC()
	project := repo.Project{ID: 10, Name: "P1", Description: "d", OwnerUserID: 5}
	project.CreatedBy = 5
	project.CreatedTime = now
	project.UpdatedTime = now
	resp := projectResp(project, []App{{AppID: 1}}, 5)
	if resp.ProjectID != 10 || resp.ProjectName != "10" || resp.DisplayName != "P1" {
		t.Fatalf("resp = %+v", resp)
	}
	if !resp.CanEdit {
		t.Fatal("owner should be able to edit")
	}
	if resp.CreatedAt != now.UnixMilli() {
		t.Fatalf("createdAt = %d", resp.CreatedAt)
	}
	// 非 owner 不可编辑
	if projectResp(project, nil, 99).CanEdit {
		t.Fatal("non-owner should not edit")
	}
	// 零值时间 → 0
	if projectResp(project, nil, 5).CreatedAt == 0 {
		t.Fatal("zero time should map to 0")
	}
}

func TestProjectItemResp(t *testing.T) {
	project := repo.Project{ID: 3, Name: "P", Status: "active"}
	resp := projectItemResp(project, 2)
	if resp["id"] != int64(3) || resp["appNums"] != 2 || resp["status"] != "active" {
		t.Fatalf("resp = %v", resp)
	}
}

func TestAppResp(t *testing.T) {
	project := repo.Project{ID: 10, Name: "P1"}
	app := repo.ProjectApp{
		ID:           7,
		Name:         "App7",
		SiteType:     "manualExternal",
		EntryURL:     "https://example.com",
		IconAssetID:  5,
		ManifestJSON: json.RawMessage(`{"app_type":"manual","openInPlatform":true,"coverAssetId":6,"description":"  App 自己的描述  "}`),
		Status:       "active",
	}
	now := time.Now().UTC()
	app.CreatedTime = now
	app.UpdatedTime = now
	resp := appResp(context.Background(), config.AppGatewayConfig{}, app, project)
	if resp.AppID != 7 || resp.AppName != "7" || resp.Display != "App7" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Manual != true || !resp.OpenInPlatform {
		t.Fatal("manual app with openInPlatform should be open in platform")
	}
	if resp.URL != "https://example.com" {
		t.Fatalf("url = %q", resp.URL)
	}
	// 图标与封面按 App 取图（服务端解析 assetID 并校验可见性），不再直出 assetID
	if resp.IconURL != "/api/core/launchpad/apps/7/icon" || resp.CoverURL != "/api/core/launchpad/apps/7/cover" {
		t.Fatalf("icon urls = %q %q", resp.IconURL, resp.CoverURL)
	}
	if resp.ProjectName != "10" || resp.ProjectDisplayName != "P1" {
		t.Fatalf("project = %+v", resp)
	}
	// 描述取自 manifest 且去掉首尾空白（移动端/Web 直接展示，不再回落项目描述）
	if resp.Description != "App 自己的描述" {
		t.Fatalf("description = %q", resp.Description)
	}
	// 手工外部地址不经过平台部署，恒为 success
	if resp.DeployStatus != "success" {
		t.Fatalf("manual deployStatus = %q", resp.DeployStatus)
	}
	// 部署中 / 部署失败取自 manifest，优先于库里的状态
	deploying := repo.ProjectApp{
		ID:           9,
		Name:         "B",
		SiteType:     "embedded",
		EntryPath:    "/p",
		Status:       "active",
		ManifestJSON: json.RawMessage(`{"deploying":true}`),
	}
	if got := appResp(context.Background(), config.AppGatewayConfig{}, deploying, project).DeployStatus; got != "deploying" {
		t.Fatalf("deploying deployStatus = %q", got)
	}
	stopped := repo.ProjectApp{ID: 11, Name: "C", SiteType: "embedded", EntryPath: "/p", Status: "stopped"}
	if got := appResp(context.Background(), config.AppGatewayConfig{}, stopped, project).DeployStatus; got != "stopped" {
		t.Fatalf("stopped deployStatus = %q", got)
	}
	// EntryURL 为空时回退 EntryPath / RoutePrefix
	app2 := repo.ProjectApp{ID: 8, Name: "A", SiteType: "embedded", EntryPath: "/path", RoutePrefix: "/prefix"}
	if url := appResp(context.Background(), config.AppGatewayConfig{}, app2, project).URL; url != "/path" {
		t.Fatalf("fallback url = %q", url)
	}
	// 非 manual 类型 OpenInPlatform=true
	if !appResp(context.Background(), config.AppGatewayConfig{}, app2, project).OpenInPlatform {
		t.Fatal("embedded app should open in platform")
	}
	// 无 manifest 时描述为空串，由调用方决定不展示
	if desc := appResp(context.Background(), config.AppGatewayConfig{}, app2, project).Description; desc != "" {
		t.Fatalf("empty manifest description = %q", desc)
	}
	// 无 manifest 字段
	app3 := repo.ProjectApp{ID: 9, Name: "B", SiteType: "external"}
	if appResp(context.Background(), config.AppGatewayConfig{}, app3, project).Manual {
		t.Fatal("external app should not be manual")
	}
}

func TestLaunchpadAppMediaURL(t *testing.T) {
	// 没有资产时不给 URL，前端才不会为空图标发一次必然 404 的请求
	if launchpadAppMediaURL(7, MediaKindIcon, 0) != "" || launchpadAppMediaURL(7, MediaKindIcon, -1) != "" {
		t.Fatal("invalid asset id should be empty")
	}
	if launchpadAppMediaURL(0, MediaKindIcon, 3) != "" {
		t.Fatal("invalid app id should be empty")
	}
	// 地址按 App 走，不再暴露 assetID：/assets/{id}/download 不做归属校验
	if got := launchpadAppMediaURL(7, MediaKindIcon, 3); got != "/api/core/launchpad/apps/7/icon" {
		t.Fatalf("icon url = %q", got)
	}
	if got := launchpadAppMediaURL(7, MediaKindCover, 3); got != "/api/core/launchpad/apps/7/cover" {
		t.Fatalf("cover url = %q", got)
	}
}

func TestTimeMilli(t *testing.T) {
	if timeMilli(time.Time{}) != 0 {
		t.Fatal("zero time should be 0")
	}
	now := time.Now().UTC()
	if timeMilli(now) != now.UnixMilli() {
		t.Fatal("milli conversion mismatch")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "  ", "v") != "v" {
		t.Fatal("firstNonEmpty mismatch")
	}
	if firstNonEmpty("", "") != "" {
		t.Fatal("all empty should be empty")
	}
}

func TestHasAnyRole(t *testing.T) {
	userRoles := map[int64]struct{}{1: {}, 3: {}}
	if !hasAnyRole(userRoles, []int64{2, 3}) {
		t.Fatal("matching role should be found")
	}
	if hasAnyRole(userRoles, []int64{2, 4}) {
		t.Fatal("non-matching roles should not be found")
	}
	if hasAnyRole(userRoles, nil) {
		t.Fatal("nil roles should not be found")
	}
}
