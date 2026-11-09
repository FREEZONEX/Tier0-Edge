package dataquality

import (
	"context"

	domaindataquality "backend/internal/domain/dataquality"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type DataQualityIssueListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 查询数据质量 Issue
func NewDataQualityIssueListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DataQualityIssueListLogic {
	return &DataQualityIssueListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DataQualityIssueListLogic) DataQualityIssueList(req *types.DataQualityIssueListReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.DataQuality.ListIssues(l.ctx, domaindataquality.IssueQuery{
		UnsID: req.UnsId, State: req.State, RuleCode: req.RuleCode, Severity: req.Severity,
		Page: req.Page, Size: req.Size,
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
