package appgateway

import (
	"context"
	"net"
	"strconv"
	"testing"

	"backend/internal/config"
	"backend/internal/repo"
)

func TestIsPortAvailable(t *testing.T) {
	// 找一个当前可用的端口
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("无法获取可用端口: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	if !IsPortAvailable(port) {
		t.Fatalf("端口 %d 关闭后应可用", port)
	}

	// 重新占用该端口
	occupy, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("无法占用端口 %d: %v", port, err)
	}
	defer occupy.Close()

	if IsPortAvailable(port) {
		t.Fatalf("端口 %d 被占用时应不可用", port)
	}
}

func TestAllocatePort_SkipIfNoDB(t *testing.T) {
	// 没有初始化数据库时跳过集成测试
	if repo.GetCommonConn(context.Background()) == nil {
		t.Skip("common DB not initialized")
	}

	conf := config.Config{
		AppGateway: config.AppGatewayConfig{
			PortRangeStart: 30000,
			PortRangeEnd:   30002,
		},
	}

	port, err := AllocatePort(context.Background(), conf, nil)
	if err != nil {
		t.Fatalf("AllocatePort failed: %v", err)
	}
	if port < conf.AppGateway.PortRangeStart || port > conf.AppGateway.PortRangeEnd {
		t.Fatalf("allocated port %d out of range", port)
	}
}
