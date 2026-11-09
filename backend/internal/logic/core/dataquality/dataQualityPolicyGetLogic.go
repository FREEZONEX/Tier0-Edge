package dataquality

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type DataQualityPolicyGetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 查询 UNS 节点数据质量策略
func NewDataQualityPolicyGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DataQualityPolicyGetLogic {
	return &DataQualityPolicyGetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DataQualityPolicyGetLogic) DataQualityPolicyGet(req *types.DataQualityNodeReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.DataQuality.GetPolicy(l.ctx, req.UnsId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
