package project

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	respx "backend/internal/httpx"
)

// DNS 单个标签的协议上限，不使用业务对象 alias/name 的长度限制。
const appNetworkAliasMaxLen = 63

var appNetworkAliasPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// 保留平台服务及未来生成的容器名前缀，避免别名覆盖内部依赖。
var reservedAppNetworkAliases = map[string]bool{
	"localhost": true, "backend": true, "zlm": true, "vision-worker": true,
	"opcua-server": true, "tsdb": true, "db-bootstrap": true, "redis": true,
	"emqx": true, "sourceflow": true, "nodered": true, "eventflow": true,
	"marimo": true, "uns-agent-api": true, "uns-agent-worker": true,
	"rustfs": true, "rustfs-init": true, "host": true,
}

func validateAppNetworkAlias(alias string) error {
	if alias == "" {
		return nil
	}
	if len(alias) > appNetworkAliasMaxLen || !appNetworkAliasPattern.MatchString(alias) || !strings.ContainsAny(alias, "abcdefghijklmnopqrstuvwxyz") {
		return respx.NewHTTPError(400, "project.networkAliasInvalid")
	}
	if reservedAppNetworkAliases[alias] || strings.HasPrefix(alias, "app-") || strings.HasPrefix(alias, "tier0-") || strings.HasPrefix(alias, "enterprise-") {
		return respx.NewHTTPError(400, "project.networkAliasReserved")
	}
	return nil
}

// 数据库预占负责平台 App 并发；Docker 检查额外覆盖手工接入 runtime 网络的容器。
// 检查失败即停止部署，且在停止旧容器前执行。
func checkDockerNetworkAlias(ctx context.Context, alias, ownContainer string) error {
	if alias == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	network := getRuntimeNetworkName()
	out, err := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "network="+network).Output()
	if err != nil {
		return fmt.Errorf("check network alias: %w", err)
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"inspect"}, ids...)
	out, err = exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return fmt.Errorf("inspect network aliases: %w", err)
	}
	var containers []struct {
		Name            string
		NetworkSettings struct {
			Networks map[string]struct {
				Aliases  []string
				DNSNames []string
			}
		}
	}
	if err := json.Unmarshal(out, &containers); err != nil {
		return err
	}
	for _, c := range containers {
		name := strings.TrimPrefix(c.Name, "/")
		if name == ownContainer {
			continue
		}
		endpoint, ok := c.NetworkSettings.Networks[network]
		if !ok {
			continue
		}
		names := append([]string{name}, endpoint.Aliases...)
		names = append(names, endpoint.DNSNames...)
		for _, candidate := range names {
			if strings.EqualFold(candidate, alias) {
				return respx.NewHTTPError(409, "project.networkAliasTaken")
			}
		}
	}
	return nil
}
