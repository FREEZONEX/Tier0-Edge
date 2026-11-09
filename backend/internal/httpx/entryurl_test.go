// 测试目标：httpx.RebuildGeneratedEntryURL 按请求入口重建生成型应用地址（entryurl.go）。
// 覆盖的正常路径：port 模式 host 替换（原端口与 path/query 保留、无端口沿用请求入口）、
// ExternalHost 命中与端口范围命中两条路径、subdomain 模式重建到当前配置 suffix（label
// 含旧格式 app-<shortID> 与现格式 <shortID>；请求显式端口保留，无端口走 ExternalPort
// 归一化）、请求 scheme 采纳、IPv6 无端口请求 host。
// 覆盖的异常情况：manualExternal 手工地址/相对路径/解析失败原样返回、无 ctx authority
// 原样返回、配置零值不命中、subdomain suffix 不匹配不误改。

package httpx

import (
	"context"
	"testing"

	"backend/internal/config"
)

var rebuildCfg = config.AppGatewayConfig{
	ExternalHost:          "10.0.0.1",
	Scheme:                "http",
	PortRangeStart:        30000,
	PortRangeEnd:          30099,
	AppHostPortRangeStart: 30100,
	AppHostPortRangeEnd:   30199,
	SubdomainSuffix:       "apps.example.com",
}

func authorityCtx(scheme, host string) context.Context {
	return WithAuthority(context.Background(), scheme, host)
}

func TestRebuildGeneratedEntryURL_PortMode(t *testing.T) {
	ctx := authorityCtx("http", "192.168.7.9:8080")

	// host 等于配置 ExternalHost：换 host、保留端口与 path/query
	got := RebuildGeneratedEntryURL(ctx, rebuildCfg, "http://10.0.0.1:30001/app/?x=1")
	if got != "http://192.168.7.9:30001/app/?x=1" {
		t.Fatalf("external host match = %q", got)
	}

	// 端口落在网关端口范围内（host 已失效不等 ExternalHost）同样命中
	got = RebuildGeneratedEntryURL(ctx, rebuildCfg, "http://old-entrance.internal:30042/")
	if got != "http://192.168.7.9:30042/" {
		t.Fatalf("gateway port match = %q", got)
	}

	// 宿主机应用端口范围（AppHostPortRange）同样命中
	got = RebuildGeneratedEntryURL(ctx, rebuildCfg, "http://10.9.9.9:30100/")
	if got != "http://192.168.7.9:30100/" {
		t.Fatalf("app host port match = %q", got)
	}

	// raw 无端口（host 等于 ExternalHost）：沿用请求入口（含其端口），scheme 取请求侧
	got = RebuildGeneratedEntryURL(ctx, rebuildCfg, "https://10.0.0.1/login")
	if got != "http://192.168.7.9:8080/login" {
		t.Fatalf("no port rebuild = %q", got)
	}

	// 端口超出网关范围且 host 不等 ExternalHost：不命中，原样返回
	got = RebuildGeneratedEntryURL(ctx, rebuildCfg, "http://other.example.com:19999/")
	if got != "http://other.example.com:19999/" {
		t.Fatalf("out-of-range port = %q", got)
	}
}

func TestRebuildGeneratedEntryURL_SubdomainMode(t *testing.T) {
	ctx := authorityCtx("https", "a1b2c3d4e5f6.apps.example.com")

	// 现格式 <shortID>.<suffix>：重建到当前配置 suffix（网关只分发配置 suffix，
	// 不随请求 Host 推导不可路由的父域）
	got := RebuildGeneratedEntryURL(ctx, rebuildCfg, "http://a1b2c3d4e5f6.apps.example.com/")
	if got != "https://a1b2c3d4e5f6.apps.example.com/" {
		t.Fatalf("subdomain shortID rebuild = %q", got)
	}

	// 旧格式 app-<shortID>.<suffix>：label 原样保留
	got = RebuildGeneratedEntryURL(ctx, rebuildCfg, "http://app-a1b2c3d4e5f6.apps.example.com/")
	if got != "https://app-a1b2c3d4e5f6.apps.example.com/" {
		t.Fatalf("subdomain legacy label = %q", got)
	}

	// DB 中 suffix 已过期（suffix 迁移后未跑 change-entrance 补偿）：读时无法
	// 猜测旧 suffix，不命中任何模式、原样返回；该残留由 change-entrance 的
	// 数据库补偿负责
	staleCtx := authorityCtx("https", "a1b2c3d4e5f6.apps.example.com")
	got = RebuildGeneratedEntryURL(staleCtx, rebuildCfg, "http://a1b2c3d4e5f6.apps.old.com/")
	if got != "http://a1b2c3d4e5f6.apps.old.com/" {
		t.Fatalf("stale suffix should stay untouched = %q", got)
	}

	// 请求经备用域名访问：同样重建到配置 suffix（不推导请求父域）
	altCtx := authorityCtx("https", "a1b2c3d4e5f6.alt.example.com")
	got = RebuildGeneratedEntryURL(altCtx, rebuildCfg, "http://a1b2c3d4e5f6.apps.example.com/")
	if got != "https://a1b2c3d4e5f6.apps.example.com/" {
		t.Fatalf("alt parent request should use configured suffix = %q", got)
	}

	// 请求 host 为裸 IP：用配置 suffix 现算；显式端口与域名形态一致地保留
	ipCtx := authorityCtx("http", "192.168.7.9:8080")
	got = RebuildGeneratedEntryURL(ipCtx, rebuildCfg, "http://a1b2c3d4e5f6.apps.example.com/")
	if got != "http://a1b2c3d4e5f6.apps.example.com:8080/" {
		t.Fatalf("subdomain via IP request = %q", got)
	}

	// 请求显式携带非标端口：保留该端口（浏览器正经该端口访问）
	portedCtx := authorityCtx("https", "a1b2c3d4e5f6.apps.example.com:8443")
	got = RebuildGeneratedEntryURL(portedCtx, rebuildCfg, "http://a1b2c3d4e5f6.apps.example.com/")
	if got != "https://a1b2c3d4e5f6.apps.example.com:8443/" {
		t.Fatalf("subdomain explicit request port = %q", got)
	}

	// 无请求端口但配置了非标准 ExternalPort：按配置补端口
	extCfg := rebuildCfg
	extCfg.ExternalPort = 8443
	got = RebuildGeneratedEntryURL(ctx, extCfg, "http://a1b2c3d4e5f6.apps.example.com/")
	if got != "https://a1b2c3d4e5f6.apps.example.com:8443/" {
		t.Fatalf("subdomain configured external port = %q", got)
	}

	// 请求 host 即 suffix 本身（浏览平台入口域）：应用子域继续挂在 suffix 下
	apexCtx := authorityCtx("https", "apps.example.com")
	got = RebuildGeneratedEntryURL(apexCtx, rebuildCfg, "http://a1b2c3d4e5f6.apps.example.com/")
	if got != "https://a1b2c3d4e5f6.apps.example.com/" {
		t.Fatalf("subdomain apex request = %q", got)
	}

	// raw host 不是配置 suffix 的单标签子域：非生成地址，原样返回
	got = RebuildGeneratedEntryURL(ctx, rebuildCfg, "http://deep.a1b2.apps.example.com/")
	if got != "http://deep.a1b2.apps.example.com/" {
		t.Fatalf("multi-label host should stay = %q", got)
	}
	got = RebuildGeneratedEntryURL(ctx, rebuildCfg, "https://other.example.com/")
	if got != "https://other.example.com/" {
		t.Fatalf("unrelated domain should stay = %q", got)
	}
}

func TestRebuildGeneratedEntryURL_IPv6PortlessRequest(t *testing.T) {
	// 标准端口 IPv6 直访：请求 host 为无端口的方括号字面量
	ctx := authorityCtx("http", "[2001:db8::2]")
	got := RebuildGeneratedEntryURL(ctx, rebuildCfg, "http://old-entrance.internal:30001/")
	if got != "http://[2001:db8::2]:30001/" {
		t.Fatalf("ipv6 portless request = %q", got)
	}
	// IPv6 带端口形态同样正确
	ctx6 := authorityCtx("http", "[2001:db8::2]:8080")
	got = RebuildGeneratedEntryURL(ctx6, rebuildCfg, "http://10.0.0.1:30001/")
	if got != "http://[2001:db8::2]:30001/" {
		t.Fatalf("ipv6 with port request = %q", got)
	}
}

func TestRebuildGeneratedEntryURL_UnmatchedStaysUntouched(t *testing.T) {
	ctx := authorityCtx("http", "192.168.7.9:8080")

	cases := []string{
		// manualExternal 手工外部地址
		"https://third-party.example.com/dashboard",
		// 相对路径（EntryPath / RoutePrefix）
		"/path/prefix/",
		// 解析失败 / 非常量形式
		"://bad-url",
		"ftp://10.0.0.1:30001/file",
		"",
		"   ",
	}
	for _, raw := range cases {
		if got := RebuildGeneratedEntryURL(ctx, rebuildCfg, raw); got != raw {
			t.Fatalf("raw %q rebuilt to %q, want untouched", raw, got)
		}
	}
}

func TestRebuildGeneratedEntryURL_NoCtxReturnsRaw(t *testing.T) {
	cases := []string{
		"http://10.0.0.1:30001/",
		"http://a1b2c3d4e5f6.apps.example.com/",
		"https://third-party.example.com/dashboard",
	}
	for _, raw := range cases {
		if got := RebuildGeneratedEntryURL(context.Background(), rebuildCfg, raw); got != raw {
			t.Fatalf("no ctx: raw %q rebuilt to %q", raw, got)
		}
	}
	// 零值配置：不命中任何模式
	ctx := authorityCtx("http", "192.168.7.9:8080")
	raw := "http://10.0.0.1:30001/"
	if got := RebuildGeneratedEntryURL(ctx, config.AppGatewayConfig{}, raw); got != raw {
		t.Fatalf("zero cfg rebuilt %q to %q", raw, got)
	}
}
