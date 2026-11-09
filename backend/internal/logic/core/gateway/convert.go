package gateway

import (
	"backend/internal/repo"
	"backend/internal/types"
)

func toRepoRoute(req *types.GatewayRouteReq) repo.GatewayRoute {
	return repo.GatewayRoute{
		RouteKey:    req.RouteKey,
		Name:        req.Name,
		Description: req.Description,
		TargetType:  req.TargetType,
		MatchType:   req.MatchType,
		PathPattern: req.PathPattern,
		Methods:     req.Methods,
		TargetUrl:   req.TargetUrl,
		TargetPath:  req.TargetPath,
		StripPrefix: req.StripPrefix,
		RewritePath: req.RewritePath,
		AuthPolicy:  req.AuthPolicy,
		ResourceKey: req.ResourceKey,
		TimeoutMs:   req.TimeoutMs,
		Priority:    req.Priority,
		Enabled:     req.Enabled,
	}
}
