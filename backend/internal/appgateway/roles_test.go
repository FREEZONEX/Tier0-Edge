package appgateway

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"backend/internal/config"
	"backend/internal/repo"
)

type fakeProjectRoleStore struct {
	member    repo.ProjectMember
	memberErr error
	app       repo.ProjectApp
	appErr    error
	bound     []repo.ProjectRoleApp
	boundErr  error
	calls     int
}

func (f *fakeProjectRoleStore) GetProjectMemberWithRoles(ctx context.Context, projectID, userID int64) (repo.ProjectMember, error) {
	f.calls++
	return f.member, f.memberErr
}

func (f *fakeProjectRoleStore) GetProjectAppByDeployAppID(ctx context.Context, projectID int64, deployAppID string) (repo.ProjectApp, error) {
	return f.app, f.appErr
}

func (f *fakeProjectRoleStore) ListProjectRoleAppIDsByAppIDs(ctx context.Context, projectID int64, appIDs []int64) ([]repo.ProjectRoleApp, error) {
	return f.bound, f.boundErr
}

func newTestResolver(store projectRoleStore) *roleResolver {
	rr := newRoleResolver()
	rr.newStore = func(ctx context.Context) projectRoleStore { return store }
	return rr
}

func testRoute() *repo.AppGatewayRoute {
	return &repo.AppGatewayRoute{AppID: "deploy-uuid-1", ProjectID: 15}
}

func memberWithRoles(roles ...repo.ProjectRole) repo.ProjectMember {
	return repo.ProjectMember{ID: 7, ProjectID: 15, UserID: 1, Status: "active", Roles: roles}
}

func TestResolve_NotProjectMember(t *testing.T) {
	store := &fakeProjectRoleStore{memberErr: repo.ErrNotFound}
	rr := newTestResolver(store)

	_, err := rr.resolve(context.Background(), testRoute(), 42)
	if !errors.Is(err, errNotProjectMember) {
		t.Fatalf("resolve() err = %v, want errNotProjectMember", err)
	}
}

func TestResolve_AppNotFound(t *testing.T) {
	store := &fakeProjectRoleStore{
		member: memberWithRoles(),
		appErr: repo.ErrNotFound,
	}
	rr := newTestResolver(store)

	_, err := rr.resolve(context.Background(), testRoute(), 1)
	if !errors.Is(err, errAppNotFound) {
		t.Fatalf("resolve() err = %v, want errAppNotFound", err)
	}
}

func TestResolve_OpenAppNoBoundRoles(t *testing.T) {
	store := &fakeProjectRoleStore{
		member: memberWithRoles(repo.ProjectRole{ID: 49, RoleKey: "equipment_manager"}),
		app:    repo.ProjectApp{ID: 100, ProjectID: 15},
		bound:  nil,
	}
	rr := newTestResolver(store)

	roles, err := rr.resolve(context.Background(), testRoute(), 1)
	if err != nil {
		t.Fatalf("resolve() err = %v, want nil", err)
	}
	if len(roles) != 0 {
		t.Fatalf("resolve() roles = %v, want empty (open app, no role headers)", roles)
	}
}

func TestResolve_NoIntersection(t *testing.T) {
	store := &fakeProjectRoleStore{
		member: memberWithRoles(repo.ProjectRole{ID: 49, RoleKey: "equipment_manager"}),
		app:    repo.ProjectApp{ID: 100, ProjectID: 15},
		bound:  []repo.ProjectRoleApp{{ProjectID: 15, RoleID: 74, AppID: 100}},
	}
	rr := newTestResolver(store)

	_, err := rr.resolve(context.Background(), testRoute(), 1)
	if !errors.Is(err, errNoValidAppRole) {
		t.Fatalf("resolve() err = %v, want errNoValidAppRole", err)
	}
}

func TestResolve_IntersectionSortedByRoleKey(t *testing.T) {
	store := &fakeProjectRoleStore{
		member: memberWithRoles(
			repo.ProjectRole{ID: 50, RoleKey: "equipment_viewer"},
			repo.ProjectRole{ID: 49, RoleKey: "equipment_manager"},
			repo.ProjectRole{ID: 51, RoleKey: "unrelated_role"},
		),
		app: repo.ProjectApp{ID: 100, ProjectID: 15},
		bound: []repo.ProjectRoleApp{
			{ProjectID: 15, RoleID: 49, AppID: 100},
			{ProjectID: 15, RoleID: 50, AppID: 100},
		},
	}
	rr := newTestResolver(store)

	roles, err := rr.resolve(context.Background(), testRoute(), 1)
	if err != nil {
		t.Fatalf("resolve() err = %v, want nil", err)
	}
	want := []string{"equipment_manager", "equipment_viewer"}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("resolve() roles = %v, want %v (role_key 字母序，第一个为默认激活)", roles, want)
	}
}

func TestResolve_PositiveCacheHit(t *testing.T) {
	store := &fakeProjectRoleStore{
		member: memberWithRoles(repo.ProjectRole{ID: 49, RoleKey: "equipment_manager"}),
		app:    repo.ProjectApp{ID: 100, ProjectID: 15},
		bound:  []repo.ProjectRoleApp{{ProjectID: 15, RoleID: 49, AppID: 100}},
	}
	rr := newTestResolver(store)

	if _, err := rr.resolve(context.Background(), testRoute(), 1); err != nil {
		t.Fatalf("first resolve() err = %v", err)
	}
	callsAfterFirst := store.calls
	if _, err := rr.resolve(context.Background(), testRoute(), 1); err != nil {
		t.Fatalf("second resolve() err = %v", err)
	}
	if store.calls != callsAfterFirst {
		t.Fatalf("second resolve() should hit cache, store calls = %d, want %d", store.calls, callsAfterFirst)
	}
}

func TestResolve_NegativeCacheHit(t *testing.T) {
	store := &fakeProjectRoleStore{memberErr: repo.ErrNotFound}
	rr := newTestResolver(store)

	_, err1 := rr.resolve(context.Background(), testRoute(), 42)
	_, err2 := rr.resolve(context.Background(), testRoute(), 42)
	if !errors.Is(err1, errNotProjectMember) || !errors.Is(err2, errNotProjectMember) {
		t.Fatalf("resolve() errs = %v/%v, want errNotProjectMember", err1, err2)
	}
	if store.calls != 1 {
		t.Fatalf("negative result should be cached, store calls = %d, want 1", store.calls)
	}
}

func TestResolve_TransientErrorNotCached(t *testing.T) {
	store := &fakeProjectRoleStore{memberErr: errors.New("connection reset")}
	rr := newTestResolver(store)

	_, _ = rr.resolve(context.Background(), testRoute(), 42)
	_, err := rr.resolve(context.Background(), testRoute(), 42)
	if err == nil || errors.Is(err, errNotProjectMember) {
		t.Fatalf("resolve() err = %v, want raw transient error", err)
	}
	if store.calls != 2 {
		t.Fatalf("transient error should not be cached, store calls = %d, want 2", store.calls)
	}
}

func TestResolve_InvalidateAllDropsCache(t *testing.T) {
	store := &fakeProjectRoleStore{
		member: memberWithRoles(repo.ProjectRole{ID: 49, RoleKey: "equipment_manager"}),
		app:    repo.ProjectApp{ID: 100, ProjectID: 15},
		bound:  []repo.ProjectRoleApp{{ProjectID: 15, RoleID: 49, AppID: 100}},
	}
	rr := newTestResolver(store)

	if _, err := rr.resolve(context.Background(), testRoute(), 1); err != nil {
		t.Fatalf("first resolve() err = %v", err)
	}
	rr.invalidateAll()
	if _, err := rr.resolve(context.Background(), testRoute(), 1); err != nil {
		t.Fatalf("resolve() after invalidate err = %v", err)
	}
	if store.calls != 2 {
		t.Fatalf("resolve() after invalidateAll should refetch, store calls = %d, want 2", store.calls)
	}
}

func TestStripInjectedHeaders(t *testing.T) {
	r := &http.Request{Header: http.Header{}}
	r.Header.Set("X-Tier0-Active-Role", "admin")
	r.Header.Set("X-Tier0-Business-Roles", "admin")
	r.Header.Set("X-App-User-Role", "admin")
	r.Header.Set("user", `{"userID":"1","role":"admin"}`)
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Cookie", "edge_session=x")

	StripInjectedHeaders(r)

	for _, h := range []string{"X-Tier0-Active-Role", "X-Tier0-Business-Roles", "X-App-User-Role", "user"} {
		if got := r.Header.Get(h); got != "" {
			t.Errorf("header %q should be stripped, got %q", h, got)
		}
	}
	if got := r.Header.Get("Authorization"); got != "Bearer token" {
		t.Errorf("Authorization should be kept, got %q", got)
	}
	if got := r.Header.Get("Cookie"); got == "" {
		t.Errorf("Cookie should be kept")
	}
}

func TestGateByProjectRoles_InjectsHeaders(t *testing.T) {
	store := &fakeProjectRoleStore{
		member: memberWithRoles(
			repo.ProjectRole{ID: 50, RoleKey: "equipment_viewer"},
			repo.ProjectRole{ID: 49, RoleKey: "equipment_manager"},
		),
		app:   repo.ProjectApp{ID: 100, ProjectID: 15},
		bound: []repo.ProjectRoleApp{{ProjectID: 15, RoleID: 49, AppID: 100}, {ProjectID: 15, RoleID: 50, AppID: 100}},
	}
	a := NewAuth(config.Config{})
	a.resolver = newTestResolver(store)

	r := &http.Request{Header: http.Header{}}
	w := &recordingResponseWriter{}
	userInfo := map[string]string{"userID": "1", "userName": "admin", "role": "admin"}

	if !a.gateByProjectRoles(w, r, testRoute(), userInfo) {
		t.Fatalf("gateByProjectRoles() = false, want true")
	}
	if got := r.Header.Get(headerXTier0BusinessRoles); got != "equipment_manager,equipment_viewer" {
		t.Errorf("%s = %q, want equipment_manager,equipment_viewer", headerXTier0BusinessRoles, got)
	}
	if got := r.Header.Get(headerXTier0ActiveRole); got != "equipment_manager" {
		t.Errorf("%s = %q, want equipment_manager (role_key 字母序第一个)", headerXTier0ActiveRole, got)
	}
	if got := r.Header.Get(headerXTier0Runtime); got != runtimeValueDeployed {
		t.Errorf("%s = %q, want %s", headerXTier0Runtime, got, runtimeValueDeployed)
	}
	if got := userInfo["role"]; got != "equipment_manager" {
		t.Errorf("userInfo[role] = %q, want equipment_manager (默认激活角色覆盖平台角色)", got)
	}
}

func TestGateByProjectRoles_Forbidden(t *testing.T) {
	store := &fakeProjectRoleStore{memberErr: repo.ErrNotFound}
	a := NewAuth(config.Config{})
	a.resolver = newTestResolver(store)

	r := &http.Request{Header: http.Header{}}
	w := &recordingResponseWriter{}

	if a.gateByProjectRoles(w, r, testRoute(), map[string]string{"userID": "42"}) {
		t.Fatalf("gateByProjectRoles() = true, want false")
	}
	if w.code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.code)
	}
}

func TestGateByProjectRoles_OpenAppPassesWithoutHeaders(t *testing.T) {
	store := &fakeProjectRoleStore{
		member: memberWithRoles(repo.ProjectRole{ID: 49, RoleKey: "equipment_manager"}),
		app:    repo.ProjectApp{ID: 100, ProjectID: 15},
		bound:  nil,
	}
	a := NewAuth(config.Config{})
	a.resolver = newTestResolver(store)

	r := &http.Request{Header: http.Header{}}
	w := &recordingResponseWriter{}
	userInfo := map[string]string{"userID": "1", "role": "admin"}

	if !a.gateByProjectRoles(w, r, testRoute(), userInfo) {
		t.Fatalf("gateByProjectRoles() = false, want true (open app)")
	}
	if got := r.Header.Get(headerXTier0ActiveRole); got != "" {
		t.Errorf("%s should not be injected for open app, got %q", headerXTier0ActiveRole, got)
	}
	if got := userInfo["role"]; got != "" {
		t.Errorf("userInfo[role] = %q, want empty (open app 不注入平台角色)", got)
	}
	if got := r.Header.Get(headerXTier0Runtime); got != runtimeValueDeployed {
		t.Errorf("%s = %q, want %q (open app 需注入运行时头)", headerXTier0Runtime, got, runtimeValueDeployed)
	}
	if got := r.Header.Get(headerXTier0BusinessRoles); got != "" {
		t.Errorf("%s = %q, want empty (open app 注入空角色列表)", headerXTier0BusinessRoles, got)
	}
}

func TestGateByProjectRoles_PlatformAdminCannotBypassMembership(t *testing.T) {
	store := &fakeProjectRoleStore{memberErr: repo.ErrNotFound}
	a := NewAuth(config.Config{})
	a.resolver = newTestResolver(store)

	r := &http.Request{Header: http.Header{}}
	w := &recordingResponseWriter{}

	if a.gateByProjectRoles(w, r, testRoute(), map[string]string{"userID": "42", "role": "admin"}) {
		t.Fatalf("gateByProjectRoles() = true, want false (平台管理员不能绕过项目成员校验)")
	}
	if w.code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.code)
	}
	if store.calls != 1 {
		t.Errorf("项目成员校验次数 = %d, want 1", store.calls)
	}
}

func TestGateByProjectRoles_MissingContextFailsClosed(t *testing.T) {
	tests := []struct {
		name  string
		route *repo.AppGatewayRoute
		setup func(*Auth)
	}{
		{name: "missing route", setup: func(*Auth) {}},
		{name: "missing resolver", route: testRoute(), setup: func(a *Auth) { a.resolver = nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAuth(config.Config{})
			tt.setup(a)
			w := &recordingResponseWriter{}
			r := &http.Request{Header: http.Header{}}

			if a.gateByProjectRoles(w, r, tt.route, map[string]string{"userID": "1"}) {
				t.Fatal("gateByProjectRoles() = true, want false")
			}
			if w.code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", w.code)
			}
		})
	}
}
