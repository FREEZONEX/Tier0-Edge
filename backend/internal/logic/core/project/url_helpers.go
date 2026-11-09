package project

import (
	"backend/internal/config"
	"backend/internal/repo"
	"context"
	"fmt"
	"net/url"
	"os"
)

// getExternalHost 获取外部访问主机名
func getExternalHost(conf config.Config) string {
	if conf.AppGateway.ExternalHost != "" {
		return conf.AppGateway.ExternalHost
	}
	return getPublicIP()
}

// getPublicIP 获取公网 IP
func getPublicIP() string {
	if ip := os.Getenv("PUBLIC_IP"); ip != "" {
		return ip
	}
	return getHostIP()
}

// buildStaticAppURL 构建应用访问 URL
func buildStaticAppURL(ctx context.Context, conf config.Config, appID, accessMode string) string {
	host := getExternalHost(conf)
	scheme := appGatewayScheme(conf)

	var baseURL string
	if accessMode == "port" {
		routeRepo := repo.NewAppGatewayRouteRepo(ctx)
		route, err := routeRepo.FindByAppID(ctx, appID)
		if err == nil && route != nil && route.Port > 0 {
			baseURL = fmt.Sprintf("%s://%s:%d/", scheme, host, route.Port)
		}
	} else if accessMode == "subdomain" {
		routeRepo := repo.NewAppGatewayRouteRepo(ctx)
		route, err := routeRepo.FindByAppID(ctx, appID)
		if err == nil && route != nil && route.Domain != "" {
			authority := config.AppGatewayPublicAuthority(conf.AppGateway, route.Domain)
			baseURL = fmt.Sprintf("%s://%s/", scheme, authority)
		}
	}

	return baseURL
}

// buildStaticAppURLWithToken 构建带 token 的应用访问 URL
func buildStaticAppURLWithToken(ctx context.Context, conf config.Config, appID, accessMode, token string) string {
	baseURL := buildStaticAppURL(ctx, conf, appID, accessMode)
	if baseURL == "" {
		return ""
	}
	if token != "" {
		baseURL += "?_token=" + url.QueryEscape(token)
	}
	return baseURL
}

// buildStaticAppIconURL 构建应用图标 URL
func buildStaticAppIconURL(ctx context.Context, conf config.Config, appID, accessMode string) string {
	host := getExternalHost(conf)
	scheme := appGatewayScheme(conf)

	if accessMode == "port" {
		routeRepo := repo.NewAppGatewayRouteRepo(ctx)
		route, err := routeRepo.FindByAppID(ctx, appID)
		if err == nil && route != nil && route.Port > 0 {
			return fmt.Sprintf("%s://%s:%d/icon.png", scheme, host, route.Port)
		}
	} else if accessMode == "subdomain" {
		routeRepo := repo.NewAppGatewayRouteRepo(ctx)
		route, err := routeRepo.FindByAppID(ctx, appID)
		if err == nil && route != nil && route.Domain != "" {
			authority := config.AppGatewayPublicAuthority(conf.AppGateway, route.Domain)
			return fmt.Sprintf("%s://%s/icon.png", scheme, authority)
		}
	}

	return fmt.Sprintf("%s://%s/icon.png", scheme, host)
}

func appGatewayScheme(conf config.Config) string {
	return config.NormalizeAppGatewayScheme(conf.AppGateway.Scheme)
}
