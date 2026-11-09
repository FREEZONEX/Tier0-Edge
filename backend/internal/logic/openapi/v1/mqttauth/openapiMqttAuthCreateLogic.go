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

type OpenapiMqttAuthCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 创建 MQTT Auth
func NewOpenapiMqttAuthCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiMqttAuthCreateLogic {
	return &OpenapiMqttAuthCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiMqttAuthCreateLogic) OpenapiMqttAuthCreate(req *types.OpenapiMqttAuthCreateReq) (resp *types.Envelope, err error) {
	if req == nil {
		return nil, logicx.Error(domainmqtt.ErrInvalid)
	}
	data, err := l.svcCtx.App.MQTT.GenerateDeviceClient(l.ctx, domainmqtt.ClientCommand{
		Name:                        req.Name,
		Description:                 req.Description,
		UserID:                      logicx.UserID(l.ctx),
		ClientIDRandomSuffixEnabled: req.ClientIDRandomSuffixEnabled,
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordMQTTAuthAudit(l.ctx, l.svcCtx, strconv.FormatInt(data.ID, 10), data.Name, auditdomain.BizCreate, map[string]any{
		"name":     data.Name,
		"clientID": data.ClientID,
		"username": data.Username,
	})
	return respx.Envelope(types.OpenapiMqttAuthCreateResp{
		Id:                          data.ID,
		Name:                        data.Name,
		ClientID:                    data.ClientID,
		Username:                    data.Username,
		Password:                    data.Password,
		ClientIDRandomSuffixEnabled: data.ClientIDRandomSuffixEnabled,
	}), nil
}
