package appgateway

import (
	"testing"

	"backend/internal/repo"
)

func TestRouter_AddAndFind(t *testing.T) {
	router := NewRouter()

	router.AddRoute(&repo.AppGatewayRoute{
		AppID:         "app-001",
		AccessMode:    "port",
		Port:          30001,
		ContainerHost: "172.18.0.1",
		ContainerPort: 8080,
	})

	router.AddRoute(&repo.AppGatewayRoute{
		AppID:         "app-002",
		AccessMode:    "subdomain",
		Domain:        "app-002.localhost",
		ContainerHost: "172.18.0.1",
		ContainerPort: 8081,
	})

	if route := router.FindByPort(30001); route == nil || route.AppID != "app-001" {
		t.Errorf("FindByPort(30001) = %+v, want app-001", route)
	}

	if route := router.FindByDomain("app-002.localhost"); route == nil || route.AppID != "app-002" {
		t.Errorf("FindByDomain(app-002.localhost) = %+v, want app-002", route)
	}

	if route := router.FindByPort(99999); route != nil {
		t.Errorf("FindByPort(99999) should be nil")
	}

	if route := router.FindByDomain("notexist.localhost"); route != nil {
		t.Errorf("FindByDomain(notexist.localhost) should be nil")
	}
}

func TestRouter_RemoveRoute(t *testing.T) {
	router := NewRouter()

	router.AddRoute(&repo.AppGatewayRoute{
		AppID:      "app-001",
		AccessMode: "port",
		Port:       30001,
	})

	if router.FindByPort(30001) == nil {
		t.Fatal("expected route on port 30001")
	}

	router.RemoveRoute("app-001")
	if router.FindByPort(30001) != nil {
		t.Errorf("RemoveRoute did not remove port route")
	}
}

func TestRouter_ConcurrentAccess(t *testing.T) {
	router := NewRouter()

	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 100; i++ {
			router.AddRoute(&repo.AppGatewayRoute{
				AppID:      "app-" + string(rune('0'+i%10)),
				AccessMode: "port",
				Port:       30000 + i,
			})
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = router.FindByPort(30000 + i)
		}
		done <- true
	}()

	<-done
	<-done

	for i := 0; i < 100; i++ {
		if router.FindByPort(30000+i) == nil {
			t.Errorf("port %d should have route", 30000+i)
		}
	}
}

func TestRouter_WildcardSubdomainMatch(t *testing.T) {
	router := NewRouterWithSuffix("apps.example.com")
	router.AddRoute(&repo.AppGatewayRoute{
		AppID:         "app-003",
		AccessMode:    "subdomain",
		Domain:        "a1b2c3d4e5f6.apps.example.com",
		ContainerHost: "172.18.0.1",
		ContainerPort: 8081,
	})

	// 新格式：<shortID>.<suffix> 通配命中
	if r := router.FindByDomain("a1b2c3d4e5f6.apps.example.com"); r == nil || r.AppID != "app-003" {
		t.Errorf("FindByDomain(wildcard) = %+v, want app-003", r)
	}

	// 带端口
	if r := router.FindByDomain("a1b2c3d4e5f6.apps.example.com:8080"); r == nil || r.AppID != "app-003" {
		t.Errorf("FindByDomain(wildcard with port) = %+v, want app-003", r)
	}

	// 多级子域名不命中
	if r := router.FindByDomain("a.b.apps.example.com"); r != nil {
		t.Errorf("FindByDomain(multi-level) should be nil, got %+v", r)
	}

	// 其他后缀不命中
	if r := router.FindByDomain("a1b2c3d4e5f6.other.com"); r != nil {
		t.Errorf("FindByDomain(other suffix) should be nil, got %+v", r)
	}

	// 裸父域不命中
	if r := router.FindByDomain("apps.example.com"); r != nil {
		t.Errorf("FindByDomain(bare parent) should be nil, got %+v", r)
	}
}

func TestRouter_WildcardWithExactFallback(t *testing.T) {
	router := NewRouterWithSuffix("apps.example.com")
	// 旧 app-<shortID> 格式（精确匹配兼容）
	router.AddRoute(&repo.AppGatewayRoute{
		AppID:         "app-004",
		AccessMode:    "subdomain",
		Domain:        "app-a1b2c3d4e5f6.apps.example.com",
		ContainerHost: "172.18.0.1",
		ContainerPort: 8081,
	})

	if r := router.FindByDomain("app-a1b2c3d4e5f6.apps.example.com"); r == nil || r.AppID != "app-004" {
		t.Errorf("FindByDomain(exact old format) = %+v, want app-004", r)
	}
	// 旧格式也作为单级子域 key 索引（app-a1b2c3d4e5f6 是单级）
	if r := router.FindByDomain("app-a1b2c3d4e5f6.apps.example.com:8088"); r == nil || r.AppID != "app-004" {
		t.Errorf("FindByDomain(old format with port) = %+v, want app-004", r)
	}
}

func TestRouter_WildcardDisabledWithoutSuffix(t *testing.T) {
	router := NewRouterWithSuffix("")
	router.AddRoute(&repo.AppGatewayRoute{
		AppID:         "app-005",
		AccessMode:    "subdomain",
		Domain:        "a1b2c3d4e5f6.apps.example.com",
		ContainerHost: "172.18.0.1",
		ContainerPort: 8081,
	})

	// 无 suffix：仅精确匹配命中，通配子域名 key 未索引
	if r := router.FindByDomain("a1b2c3d4e5f6.apps.example.com"); r == nil || r.AppID != "app-005" {
		t.Errorf("FindByDomain(exact) = %+v, want app-005 (exact always matches)", r)
	}
	if r := router.FindByDomain("a1b2c3d4e5f6.apps.example.com:8088"); r == nil || r.AppID != "app-005" {
		t.Errorf("FindByDomain(exact with port) = %+v, want app-005 (exact stripped-port match)", r)
	}
}

func TestRouter_RemoveRouteClearsSubdomainIndex(t *testing.T) {
	router := NewRouterWithSuffix("apps.example.com")
	router.AddRoute(&repo.AppGatewayRoute{
		AppID:         "app-006",
		AccessMode:    "subdomain",
		Domain:        "a1b2c3d4e5f6.apps.example.com",
		ContainerHost: "172.18.0.1",
		ContainerPort: 8081,
	})

	router.RemoveRoute("app-006")

	if r := router.FindByDomain("a1b2c3d4e5f6.apps.example.com"); r != nil {
		t.Errorf("RemoveRoute did not clear wildcard subdomain index, got %+v", r)
	}
}

func TestRouter_FindByDomainStripsPort(t *testing.T) {
	router := NewRouter()
	router.AddRoute(&repo.AppGatewayRoute{
		AppID:         "app-007",
		AccessMode:    "subdomain",
		Domain:        "exact.localhost",
		ContainerHost: "172.18.0.1",
		ContainerPort: 8081,
	})

	if r := router.FindByDomain("exact.localhost:8088"); r == nil || r.AppID != "app-007" {
		t.Errorf("FindByDomain(exact with port) = %+v, want app-007", r)
	}
	if r := router.FindByDomain("exact.localhost"); r == nil || r.AppID != "app-007" {
		t.Errorf("FindByDomain(exact) = %+v, want app-007", r)
	}
}
