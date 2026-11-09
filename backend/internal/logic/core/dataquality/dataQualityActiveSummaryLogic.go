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

type DataQualityActiveSummaryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 查询 UNS 树活动数据质量异常摘要
func NewDataQualityActiveSummaryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DataQualityActiveSummaryLogic {
	return &DataQualityActiveSummaryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DataQualityActiveSummaryLogic) DataQualityActiveSummary() (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.DataQuality.ActiveSummary(l.ctx)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
