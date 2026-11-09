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

type DataQualityPolicyPutLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 创建或更新 UNS 节点数据质量策略
func NewDataQualityPolicyPutLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DataQualityPolicyPutLogic {
	return &DataQualityPolicyPutLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DataQualityPolicyPutLogic) DataQualityPolicyPut(req *types.DataQualityPolicyReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.DataQuality.SavePolicy(l.ctx, domaindataquality.PolicyInput{
		UnsID: req.UnsId, Enabled: req.Enabled, NotifyOnIssue: req.NotifyOnIssue,
		ExpectedIntervalMs: req.ExpectedIntervalMs,
		SequenceField:      req.SequenceField, MissingGraceMs: req.MissingGraceMs,
		LateThresholdMs: req.LateThresholdMs, ClockSkewToleranceMs: req.ClockSkewToleranceMs,
		GapToleranceMs: req.GapToleranceMs, OutOfOrderToleranceMs: req.OutOfOrderToleranceMs,
		FrequencyTolerancePercent: req.FrequencyTolerancePercent, MinSamples: req.MinSamples,
		FlatMinSamples: req.FlatMinSamples, FlatMinDurationMs: req.FlatMinDurationMs,
		MandatoryFields: req.MandatoryFields, FlatFields: req.FlatFields, StaleFields: req.StaleFields,
		Rules: req.Rules, SeverityOverrides: req.SeverityOverrides, SamplingPriority: req.SamplingPriority,
		CooldownMs: req.CooldownMs, UserID: logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
