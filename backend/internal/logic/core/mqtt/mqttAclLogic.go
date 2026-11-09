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

type MqttAclLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMqttAclLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MqttAclLogic {
	return &MqttAclLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MqttAclLogic) MqttAcl(req *types.MqttAclReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.MQTT.ACL(l.ctx, req.Username, req.Clientid, req.Topic, req.Action)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
