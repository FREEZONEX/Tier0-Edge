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

type MqttAuthLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMqttAuthLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MqttAuthLogic {
	return &MqttAuthLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MqttAuthLogic) MqttAuth(req *types.MqttAuthReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.MQTT.Auth(l.ctx, req.Username, req.Password, req.Clientid, req.Peerhost)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
