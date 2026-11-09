package repo

import (
	"context"
	"encoding/json"
	"time"

	"backend/internal/config"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GatewayRoute struct {
	ID            int64    `gorm:"column:id;primaryKey" json:"id"`
	RouteKey      string   `gorm:"column:route_key" json:"routeKey"`
	Name          string   `gorm:"column:name" json:"name"`
	Description   string   `gorm:"column:description" json:"description"`
	TargetType    string   `gorm:"column:target_type" json:"targetType"`
	MatchType     string   `gorm:"column:match_type" json:"matchType"`
	PathPattern   string   `gorm:"column:path_pattern" json:"pathPattern"`
	Methods       []string `gorm:"column:methods;type:jsonb;serializer:json" json:"methods"`
	TargetUrl     string   `gorm:"column:target_url" json:"targetUrl"`
	TargetPath    string   `gorm:"column:target_path" json:"targetPath"`
	StripPrefix   bool     `gorm:"column:strip_prefix" json:"stripPrefix"`
	RewritePath   string   `gorm:"column:rewrite_path" json:"rewritePath"`
	AuthPolicy    string   `gorm:"column:auth_policy" json:"authPolicy"`
	ResourceKey   string   `gorm:"column:resource_key" json:"resourceKey"`
	TimeoutMs     int      `gorm:"column:timeout_ms" json:"timeoutMs"`
	Priority      int      `gorm:"column:priority" json:"priority"`
	Enabled       bool     `gorm:"column:enabled" json:"enabled"`
	SystemBuiltin bool     `gorm:"column:system_builtin" json:"systemBuiltin"`
	SoftNoDelByTime
}

func (GatewayRoute) TableName() string { return "sys_gateway_route" }

func (r *GatewayRepo) ListGatewayRoutes(ctx context.Context) ([]GatewayRoute, error) {
	var out []GatewayRoute
	err := r.db.WithContext(ctx).
		Where("deleted_time = 0").
		Order("priority, id").
		Find(&out).Error
	return out, err
}

func (r *GatewayRepo) GetGatewayRoute(ctx context.Context, routeKey string) (GatewayRoute, error) {
	var out GatewayRoute
	err := r.db.WithContext(ctx).
		Where("route_key = ? AND deleted_time = 0", routeKey).
		First(&out).Error
	return out, err
}

func (r *GatewayRepo) UpsertGatewayRoute(ctx context.Context, route GatewayRoute) error {
	return upsertGatewayRoute(r.db.WithContext(ctx), route, time.Now().UTC().UnixMilli())
}

func (r *GatewayRepo) DeleteGatewayRoute(ctx context.Context, routeKey string, userID int64) error {
	res := r.db.WithContext(ctx).Model(&GatewayRoute{}).
		Where("route_key = ? AND deleted_time = 0 AND system_builtin = false", routeKey).
		Updates(softDeleteNoDelByValues(userID, 0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// upsertGatewayRoute inserts or updates a route keyed by (route_key, deleted_time).
// A map create keeps created_time/updated_time explicit while reusing the same
// conflict clause for both the public API and the seed.
func upsertGatewayRoute(db *gorm.DB, route GatewayRoute, now int64) error {
	ts := repoTimeFromMilli(now)
	methods, _ := json.Marshal(route.Methods)
	if len(route.Methods) == 0 {
		methods = []byte("[]")
	}
	if route.ID == 0 && !route.SystemBuiltin {
		id, err := nextPersonalID(db, "sys_gateway_route", "id")
		if err != nil {
			return err
		}
		route.ID = id
	}
	actorID := actorIDFromDB(db)
	updates := []string{
		"name", "description", "target_type", "match_type", "path_pattern", "methods",
		"target_url", "target_path", "strip_prefix", "rewrite_path", "auth_policy",
		"resource_key", "timeout_ms", "priority", "enabled", "updated_time",
	}
	createValues := map[string]any{
		"route_key":      route.RouteKey,
		"name":           route.Name,
		"description":    route.Description,
		"target_type":    route.TargetType,
		"match_type":     route.MatchType,
		"path_pattern":   route.PathPattern,
		"methods":        string(methods),
		"target_url":     route.TargetUrl,
		"target_path":    route.TargetPath,
		"strip_prefix":   route.StripPrefix,
		"rewrite_path":   route.RewritePath,
		"auth_policy":    route.AuthPolicy,
		"resource_key":   route.ResourceKey,
		"timeout_ms":     route.TimeoutMs,
		"priority":       route.Priority,
		"enabled":        route.Enabled,
		"system_builtin": route.SystemBuiltin,
		"created_time":   ts,
		"updated_time":   ts,
	}
	if route.ID != 0 {
		createValues["id"] = route.ID
	}
	if actorID != 0 {
		updates = append(updates, "updated_by")
		createValues["created_by"] = actorID
		createValues["updated_by"] = actorID
	}
	err := db.Table("sys_gateway_route").
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "route_key"}, {Name: "deleted_time"}},
			DoUpdates: clause.AssignmentColumns(updates),
		}).
		Create(createValues).Error
	return normalizeDBError(err)
}

func builtinRoutes(c config.GatewayConf) []GatewayRoute {
	const systemGatewayRouteIDBase int64 = 10000
	return []GatewayRoute{
		{ID: systemGatewayRouteIDBase + 1, RouteKey: "static.assets", Name: "Static Assets", TargetType: "static", MatchType: "prefix", PathPattern: "/assets/**", Methods: []string{"GET", "HEAD"}, AuthPolicy: "public", Priority: 10, Enabled: true, SystemBuiltin: true},
		{ID: systemGatewayRouteIDBase + 11, RouteKey: "static.spa", Name: "SPA", TargetType: "static", MatchType: "prefix", PathPattern: "/**", Methods: []string{"GET", "HEAD"}, AuthPolicy: "public", Priority: 9999, Enabled: true, SystemBuiltin: true},
	}
}

type GatewayRepo struct{ db *gorm.DB }

func NewGatewayRepo(in any) *GatewayRepo { return &GatewayRepo{db: GetCommonConn(in)} }
