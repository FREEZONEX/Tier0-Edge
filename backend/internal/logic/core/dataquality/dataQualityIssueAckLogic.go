package dataquality

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type DataQualityIssueAckLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 确认数据质量 Issue
func NewDataQualityIssueAckLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DataQualityIssueAckLogic {
	return &DataQualityIssueAckLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DataQualityIssueAckLogic) DataQualityIssueAck(req *types.DataQualityIssueIDReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.DataQuality.AcknowledgeIssue(l.ctx, req.Id, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
