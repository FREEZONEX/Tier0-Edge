package uns

import (
	"context"

	"backend/internal/contextx"
	"backend/internal/domain/iam"
	domainuns "backend/internal/domain/uns"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/types"
)

func requestDataQualityPolicy(input *types.UnsDataQualityPolicyReq) *repo.DataQualityPolicy {
	if input == nil {
		return nil
	}
	return &repo.DataQualityPolicy{
		NotifyOnIssue:      false,
		ExpectedIntervalMs: input.ExpectedIntervalMs,
		MissingGraceMs:     input.MissingGraceMs,
	}
}

// Embedded policies have the same write permission as the dedicated policy API.
func authorizeDataQualityPolicy(ctx context.Context, req *types.UnsNodeSaveReq) error {
	if req.DataQualityPolicy == nil {
		return nil
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(subject, "uns.dataQuality.manage") {
		return logicx.Error(iam.ErrForbidden)
	}
	return nil
}

func BuildUnsNodeSaveCommand(ctx context.Context, req *types.UnsNodeSaveReq) domainuns.SaveCommand {
	return domainuns.SaveCommand{
		ID:                req.NodeId,
		ParentID:          req.ParentId,
		Name:              req.Name,
		DisplayName:       req.DisplayName,
		Description:       req.Description,
		Alias:             req.Alias,
		Namespace:         req.Namespace,
		NodeType:          req.NodeType,
		TopicType:         req.TopicType,
		Schema:            req.Schema,
		ExtendProperties:  req.ExtendProperties,
		DataQualityPolicy: requestDataQualityPolicy(req.DataQualityPolicy),
		LabelIDs:          req.LabelIds,
		AssetFileIDs:      req.AssetFileIds,
		EnableHistory:     req.Persistence,
		AddFlow:           req.WithFlow || req.AddFlow || req.MockData,
		MockData:          req.MockData || req.AddFlow || req.WithFlow,
		UserID:            logicx.UserID(ctx),
	}
}
