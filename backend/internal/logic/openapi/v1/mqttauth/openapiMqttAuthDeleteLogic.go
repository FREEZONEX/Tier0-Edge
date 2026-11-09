// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package mqttauth

import (
	"context"
	"strconv"

	auditdomain "backend/internal/domain/audit"
	domainmqtt "backend/internal/domain/mqtt"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiMqttAuthDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 删除 MQTT Auth
func NewOpenapiMqttAuthDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiMqttAuthDeleteLogic {
	return &OpenapiMqttAuthDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiMqttAuthDeleteLogic) OpenapiMqttAuthDelete(req *types.OpenapiMqttAuthDeleteReq) (resp *types.Envelope, err error) {
	if req == nil || req.Id <= 0 {
		return nil, logicx.Error(domainmqtt.ErrInvalid)
	}
	success, err := l.svcCtx.App.MQTT.DeleteDeviceClient(l.ctx, req.Id, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordMQTTAuthAudit(l.ctx, l.svcCtx, strconv.FormatInt(req.Id, 10), "", auditdomain.BizDelete, map[string]any{
		"id":      req.Id,
		"success": success,
	})
	return respx.Envelope(types.OpenapiMqttAuthDeleteResp{Success: success}), nil
}
