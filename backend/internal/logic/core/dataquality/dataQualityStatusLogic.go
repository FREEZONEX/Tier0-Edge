package dataquality

import (
	"context"

	"backend/internal/domain/dataquality"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type DataQualityStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 查询数据质量实时状态
func NewDataQualityStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DataQualityStatusLogic {
	return &DataQualityStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DataQualityStatusLogic) DataQualityStatus(req *types.DataQualityStatusReq) (resp *types.Envelope, err error) {
	if !l.svcCtx.App.DataQuality.Enabled() {
		return nil, logicx.Error(dataquality.ErrFeatureDisabled)
	}
	data, err := l.svcCtx.App.DataQuality.StatusView(l.ctx, req.UnsId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
