// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package dataquality

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type DataQualityRunCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 请求 UNS 节点立即进入深度质量检测
func NewDataQualityRunCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DataQualityRunCreateLogic {
	return &DataQualityRunCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DataQualityRunCreateLogic) DataQualityRunCreate(req *types.DataQualityNodeReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.DataQuality.RequestDeepRun(req.UnsId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
