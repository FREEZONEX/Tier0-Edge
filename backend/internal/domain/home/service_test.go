package home

import (
	"testing"

	"backend/internal/repo"

	"gorm.io/gorm"
)

func TestResourceKeyFromPath_Projects(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/core/projects", recentResourceKeyProject},
		{"/api/core/projects/123", recentResourceKeyProject},
		{"/api/core/projects/123/members", recentResourceKeyProject},
		{"/api/core/uns", recentResourceKeyUNS},
		{"/api/core/flows", recentResourceKeySourceFlow},
		{"/api/core/notebooks", recentResourceKeyNotebook},
		{"/api/core/other", ""},
	}
	for _, c := range cases {
		got := resourceKeyFromPath(c.path)
		if got != c.want {
			t.Errorf("resourceKeyFromPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestResourceKeyForAudit_ProjectResType(t *testing.T) {
	cases := []struct {
		resType string
		want    string
	}{
		{"Project", recentResourceKeyProject},
		{"project", recentResourceKeyProject},
		{"ProjectMember", recentResourceKeyProject},
		{"ProjectApp", recentResourceKeyProjectApp},
		{"UNS", recentResourceKeyUNS},
		{"SourceFlow", recentResourceKeySourceFlow},
		{"EventFlow", recentResourceKeyEventFlow},
		{"Notebook", recentResourceKeyNotebook},
		{"Unknown", ""},
	}
	for _, c := range cases {
		item := repo.AuditLog{ResType: c.resType}
		got := resourceKeyForAudit(item)
		if got != c.want {
			t.Errorf("resourceKeyForAudit(ResType=%q) = %q, want %q", c.resType, got, c.want)
		}
	}
}

func TestResourceKeyForAudit_PathFallback(t *testing.T) {
	item := repo.AuditLog{
		ResType:    "Other",
		DetailJSON: `{"path":"/api/core/projects/123"}`,
	}
	got := resourceKeyForAudit(item)
	if got != recentResourceKeyProject {
		t.Errorf("resourceKeyForAudit with project path = %q, want %q", got, recentResourceKeyProject)
	}
}

func TestProjectIDFromAudit(t *testing.T) {
	cases := []struct {
		name       string
		resID      string
		detailJSON string
		want       int64
	}{
		{name: "resource id", resID: "123", want: 123},
		{name: "detail project id", resID: "456", detailJSON: `{"projectId":123}`, want: 123},
		{name: "member path", resID: "456", detailJSON: `{"path":"/api/core/projects/123/members/456"}`, want: 123},
		{name: "invalid", resID: "abc", want: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			item := repo.AuditLog{ResID: c.resID, DetailJSON: c.detailJSON}
			if got := projectIDFromAudit(item); got != c.want {
				t.Errorf("projectIDFromAudit() = %d, want %d", got, c.want)
			}
		})
	}
}

func TestResourceTypeForRecent_Project(t *testing.T) {
	got := resourceTypeForRecent(recentResourceKeyProject, "fallback")
	if got != "Project" {
		t.Errorf("resourceTypeForRecent(project.view) = %q, want Project", got)
	}
}

func TestResourceTypeForRecent_ProjectApp(t *testing.T) {
	got := resourceTypeForRecent(recentResourceKeyProjectApp, "fallback")
	if got != "App" {
		t.Errorf("resourceTypeForRecent(launchpad.view) = %q, want App", got)
	}
}

func TestMenuResourceKeyForRecent_ProjectApp(t *testing.T) {
	if got := menuResourceKeyForRecent(recentResourceKeyProjectApp); got != recentResourceKeyProject {
		t.Errorf("menuResourceKeyForRecent(launchpad.view) = %q, want %q", got, recentResourceKeyProject)
	}
	if got := menuResourceKeyForRecent(recentResourceKeyNotebook); got != recentResourceKeyNotebook {
		t.Errorf("menuResourceKeyForRecent(notebook.view) = %q, want %q", got, recentResourceKeyNotebook)
	}
}

func TestProjectAppRecentUsesProjectManagementRoute(t *testing.T) {
	if got := projectManagementRoutePath(123); got != "/project/123" {
		t.Errorf("projectManagementRoutePath(123) = %q, want /project/123", got)
	}
	if got := projectManagementRoutePath(0); got != "" {
		t.Errorf("projectManagementRoutePath(0) = %q, want empty", got)
	}
}

func TestProjectAppRecentTargetUsesCurrentProject(t *testing.T) {
	name, routePath, available := projectAppRecentTarget(
		repo.ProjectApp{ID: 456, ProjectID: 123, Name: "Example App", Status: "active"},
		map[int64]repo.Project{123: {ID: 123, Name: "Example Project"}},
	)
	if !available || name != "Example App" || routePath != "/project/123" {
		t.Fatalf(
			"projectAppRecentTarget() = (%q, %q, %v), want (%q, %q, true)",
			name,
			routePath,
			available,
			"Example App",
			"/project/123",
		)
	}
}

func TestProjectAppRecentTargetRejectsInaccessibleProject(t *testing.T) {
	_, _, available := projectAppRecentTarget(
		repo.ProjectApp{ID: 456, ProjectID: 123, Name: "Example App", Status: "active"},
		map[int64]repo.Project{},
	)
	if available {
		t.Fatal("projectAppRecentTarget() available = true, want false")
	}
}

func TestProjectAppRecentTargetRejectsInactiveApp(t *testing.T) {
	_, _, available := projectAppRecentTarget(
		repo.ProjectApp{ID: 456, ProjectID: 123, Name: "Example App", Status: "disabled"},
		map[int64]repo.Project{123: {ID: 123, Name: "Example Project"}},
	)
	if available {
		t.Fatal("projectAppRecentTarget() available = true, want false")
	}
}

func TestProjectAppRoleVisible(t *testing.T) {
	member := repo.ProjectMember{Roles: []repo.ProjectRole{{ID: 7}}}
	tests := []struct {
		name     string
		roleApps []repo.ProjectRoleApp
		want     bool
	}{
		{name: "unrestricted app", want: true},
		{name: "matching role", roleApps: []repo.ProjectRoleApp{{AppID: 456, RoleID: 7}}, want: true},
		{name: "missing role", roleApps: []repo.ProjectRoleApp{{AppID: 456, RoleID: 8}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := projectAppRoleVisible(member, tt.roleApps, 456); got != tt.want {
				t.Fatalf("projectAppRoleVisible() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecentLookupResultSkipsGormNotFound(t *testing.T) {
	_, _, available, err := recentLookupResult(gorm.ErrRecordNotFound)
	if err != nil || available {
		t.Fatalf("recentLookupResult(gorm.ErrRecordNotFound) = available %v, err %v; want false, nil", available, err)
	}
}

func TestIsRecentResourceKey(t *testing.T) {
	if !isRecentResourceKey(recentResourceKeyProject) {
		t.Error("expected project.view to be a recent resource key")
	}
	if isRecentResourceKey("unknown.key") {
		t.Error("expected unknown.key not to be a recent resource key")
	}
}
