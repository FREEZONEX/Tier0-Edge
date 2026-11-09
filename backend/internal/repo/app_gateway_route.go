package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// AppGatewayRoute 应用网关路由表
type AppGatewayRoute struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	AppID         string    `gorm:"column:app_id;size:36;not null;uniqueIndex:idx_app_gateway_routes_app_id" json:"appId"`
	ProjectID     int64     `gorm:"column:project_id;not null;index:idx_app_gateway_routes_project_id" json:"projectId"`
	AccessMode    string    `gorm:"column:access_mode;size:20;not null;default:'port'" json:"accessMode"` // port | subdomain
	Port          int       `gorm:"column:port;uniqueIndex:idx_app_gateway_routes_port" json:"port"`
	Domain        string    `gorm:"column:domain;size:255;index:idx_app_gateway_routes_domain" json:"domain"`
	ContainerHost string    `gorm:"column:container_host;size:50;not null;default:'172.18.0.1'" json:"containerHost"`
	ContainerPort int       `gorm:"column:container_port;not null" json:"containerPort"`
	CreatedTime   time.Time `gorm:"column:created_time;autoCreateTime" json:"createdTime"`
	UpdatedTime   time.Time `gorm:"column:updated_time;autoUpdateTime" json:"updatedTime"`
	DeletedTime   int64     `gorm:"column:deleted_time;default:0" json:"deletedTime"`
}

func (AppGatewayRoute) TableName() string { return "app_gateway_routes" }

type AppGatewayRouteRepo struct{ db *gorm.DB }

func NewAppGatewayRouteRepo(in any) *AppGatewayRouteRepo {
	return &AppGatewayRouteRepo{db: GetCommonConn(in)}
}

type AppGatewayRouteFilter struct {
	AppID      string
	ProjectID  int64
	AccessMode string
	Port       int
	Domain     string
}

func (r *AppGatewayRouteRepo) fmtFilter(ctx context.Context, f AppGatewayRouteFilter) *gorm.DB {
	db := r.db.WithContext(ctx).Model(&AppGatewayRoute{})
	if f.AppID != "" {
		db = db.Where("app_id = ?", f.AppID)
	}
	if f.ProjectID > 0 {
		db = db.Where("project_id = ?", f.ProjectID)
	}
	if f.AccessMode != "" {
		db = db.Where("access_mode = ?", f.AccessMode)
	}
	if f.Port > 0 {
		db = db.Where("port = ?", f.Port)
	}
	if f.Domain != "" {
		db = db.Where("domain = ?", f.Domain)
	}
	return db
}

func (r *AppGatewayRouteRepo) Insert(ctx context.Context, data *AppGatewayRoute) error {
	return normalizeDBError(r.db.WithContext(ctx).Create(data).Error)
}

func (r *AppGatewayRouteRepo) FindOneByFilter(ctx context.Context, f AppGatewayRouteFilter) (*AppGatewayRoute, error) {
	var result AppGatewayRoute
	err := r.fmtFilter(ctx, f).First(&result).Error
	if err != nil {
		return nil, normalizeDBError(err)
	}
	return &result, nil
}

func (r *AppGatewayRouteRepo) FindByAppID(ctx context.Context, appID string) (*AppGatewayRoute, error) {
	return r.FindOneByFilter(ctx, AppGatewayRouteFilter{AppID: appID})
}

func (r *AppGatewayRouteRepo) ListAll(ctx context.Context) ([]*AppGatewayRoute, error) {
	var results []*AppGatewayRoute
	err := r.db.WithContext(ctx).Find(&results).Error
	return results, err
}

func (r *AppGatewayRouteRepo) Update(ctx context.Context, data *AppGatewayRoute) error {
	return normalizeDBError(r.db.WithContext(ctx).Where("id = ?", data.ID).Save(data).Error)
}

func (r *AppGatewayRouteRepo) Delete(ctx context.Context, id int64) error {
	return normalizeDBError(r.db.WithContext(ctx).Where("id = ?", id).Delete(&AppGatewayRoute{}).Error)
}

func (r *AppGatewayRouteRepo) DeleteByAppID(ctx context.Context, appID string) error {
	// 先清零 port，释放 unique 约束，避免 soft-delete 后插入同端口冲突
	if err := r.db.WithContext(ctx).Model(&AppGatewayRoute{}).Where("app_id = ?", appID).
		Updates(map[string]any{"port": 0}).Error; err != nil {
		return normalizeDBError(err)
	}
	return normalizeDBError(r.db.WithContext(ctx).Where("app_id = ?", appID).Delete(&AppGatewayRoute{}).Error)
}
