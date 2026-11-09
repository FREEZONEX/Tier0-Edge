package cluster

import (
	"context"
	"strconv"

	"backend/internal/contextx"
	auditdomain "backend/internal/domain/audit"
	"backend/internal/repo"
)

type FleetAuditRequest struct {
	NodeID  string `json:"nodeId"`
	AuditID int64  `json:"auditId,omitempty"`
	Page    int    `json:"page,omitempty"`
	Size    int    `json:"size,omitempty"`
}

// A node's activity view reads audit events, not container command jobs.
func (s *Service) FleetAudit(ctx context.Context, op string, req FleetAuditRequest) (any, error) {
	if _, err := s.fleetRepo.GetNode(ctx, req.NodeID, false); err != nil {
		return nil, err
	}
	if s.audit == nil {
		return nil, ErrInvalid
	}
	if op == "nodesAuditGet" {
		item, err := s.audit.Detail(ctx, req.AuditID)
		if err != nil || item.ResType != auditdomain.ResTypeFleet || item.ResID != req.NodeID {
			return nil, repo.ErrNotFound
		}
		return item, nil
	}
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 || req.Size > 100 {
		req.Size = 20
	}
	return s.audit.Page(ctx, auditdomain.PageQuery{PageNo: req.Page, PageSize: req.Size, ResType: auditdomain.ResTypeFleet, ResID: req.NodeID})
}

// Never record raw Source credentials, Stream scripts, metadata bodies or output.
func (s *Service) RecordFleetAction(ctx context.Context, nodeID, action string, detail map[string]any, err error) {
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok || s.audit == nil {
		return
	}
	code := 200
	if err != nil {
		code = 400
	}
	if detail == nil {
		detail = map[string]any{}
	}
	detail["action"] = action
	name := actor.UserName
	if name == "" {
		name = strconv.FormatInt(actor.UserID, 10)
	}
	s.audit.RecordDirect(&auditdomain.Operator{ID: strconv.FormatInt(actor.UserID, 10), Name: name}, auditdomain.RecordInput{
		ScopeType: auditdomain.ScopeTypePlatform, ResType: auditdomain.ResTypeFleet,
		ResID: nodeID, BusinessType: auditdomain.BizUpdate, Code: code, Detail: detail,
	})
}
