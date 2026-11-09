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

type MqttAuthClientResetPasswordLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMqttAuthClientResetPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MqttAuthClientResetPasswordLogic {
	return &MqttAuthClientResetPasswordLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MqttAuthClientResetPasswordLogic) MqttAuthClientResetPassword(req *types.IdReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.MQTT.ResetPassword(l.ctx, req.Id, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
