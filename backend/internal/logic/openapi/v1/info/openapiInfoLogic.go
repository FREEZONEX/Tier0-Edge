// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package info

import (
	"context"
	"os"
	"strings"

	respx "backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiInfoLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiInfoLogic {
	return &OpenapiInfoLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiInfoLogic) OpenapiInfo() (resp *types.Envelope, err error) {
	return respx.Envelope(types.InfoResp{
		Name:    "edge-enterprise",
		Version: openapiProductVersion(l.svcCtx),
		Capabilities: []string{
			"uns.read",
			"uns.write",
			"flow.read",
			"flow.manage",
			"asset.read",
			"mqtt",
		},
		MqttBroker: openapiMqttBrokerHost(l.svcCtx),
	}), nil
}

func openapiProductVersion(svcCtx *svc.ServiceContext) string {
	if svcCtx != nil {
		if version := strings.TrimSpace(svcCtx.Config.ProductVersion); version != "" {
			return version
		}
	}
	if version := strings.TrimSpace(os.Getenv("PRODUCT_VERSION")); version != "" {
		return version
	}
	return "dev"
}

func openapiMqttBrokerHost(svcCtx *svc.ServiceContext) string {
	if svcCtx == nil {
		return "emqx"
	}
	broker := strings.TrimSpace(svcCtx.Config.Cluster.ExternalMqttBrokerHost)
	if broker == "" {
		return "emqx"
	}
	return broker
}
