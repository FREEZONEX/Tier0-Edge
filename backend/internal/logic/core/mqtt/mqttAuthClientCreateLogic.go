// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package mqtt

import (
	"context"

	domainmqtt "backend/internal/domain/mqtt"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MqttAuthClientCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMqttAuthClientCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MqttAuthClientCreateLogic {
	return &MqttAuthClientCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MqttAuthClientCreateLogic) MqttAuthClientCreate(req *types.MqttAuthClientCreateReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.MQTT.GenerateClient(l.ctx, domainmqtt.ClientCommand{
		Type:                        req.ClientType,
		Name:                        req.Name,
		Description:                 req.Description,
		UserID:                      logicx.UserID(l.ctx),
		ClientIDRandomSuffixEnabled: req.ClientIdRandomSuffixEnabled,
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
