// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package mqtt

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MqttAuthClientListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMqttAuthClientListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MqttAuthClientListLogic {
	return &MqttAuthClientListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MqttAuthClientListLogic) MqttAuthClientList(req *types.MqttAuthClientListReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.MQTT.ListClients(l.ctx, req.ClientType, req.Keyword)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
