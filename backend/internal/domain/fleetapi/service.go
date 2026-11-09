// Package fleetapi is the shared internal UI and MCP operation adapter.
package fleetapi

import (
	"backend/internal/contextx"
	"backend/internal/domain/cluster"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsv2"
	"backend/internal/mcpaccess"
	"backend/internal/repo"
	"bytes"
	"context"

	"errors"
	"strings"
)

var ErrInvalid = errors.New("invalid Fleet request")
var ErrUnavailable = errors.New("Fleet collaboration disabled")

type Service struct {
	Cluster      *cluster.Service
	Uns          *unsv2.Service
	SourceStream *sourcestream.Service
}

func (s *Service) Execute(ctx context.Context, op string, raw []byte) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, ErrInvalid
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.UserID <= 0 {
		return nil, cluster.ErrUnauthorized
	}
	permission, scope := OperationAccess(op, subject)
	if !contextx.HasResource(subject, permission) || subject.APIKeyAppID != "" {
		return nil, cluster.ErrUnauthorized
	}
	if subject.MCPRestricted && !mcpaccess.Has(subject, scope) {
		return nil, cluster.ErrUnauthorized
	}

	if op != "currentGet" && !strings.HasPrefix(op, "localStorage") && !s.Cluster.CollaborationEnabled() {
		return nil, ErrUnavailable
	}

	if strings.HasPrefix(op, "localStorage") {
		var req cluster.FleetStorageRequest
		if unsv2.Decode(raw, &req) != nil {
			return nil, ErrInvalid
		}
		action := strings.ToLower(strings.TrimPrefix(op, "localStorage"))
		if !contextx.HasResource(subject, "uns.read") || action == "save" && !contextx.HasResource(subject, "uns.write") {
			return nil, cluster.ErrUnauthorized
		}
		if req.NodeID != "" {
			return nil, ErrInvalid
		}
		return s.Cluster.FleetStorage(ctx, action, req)
	}
	if strings.HasPrefix(op, "collectionSync") {
		var req cluster.FleetCollectionSyncRequest
		if unsv2.Decode(raw, &req) != nil {
			return nil, ErrInvalid
		}
		if !contextx.HasResource(subject, "uns.read") || op == "collectionSyncSave" && !contextx.HasResource(subject, "uns.write") {
			return nil, cluster.ErrUnauthorized
		}
		return s.Cluster.FleetCollectionSync(ctx, strings.ToLower(strings.TrimPrefix(op, "collectionSync")), req)
	}
	switch op {
	case "controlGrantsCatalog":
		if s.SourceStream == nil {
			return nil, ErrUnavailable
		}
		catalog, err := s.SourceStream.OperationResources(ctx, "", "active", 1, 100)
		if err != nil {
			return nil, err
		}
		current, err := s.Cluster.CollaborationCurrent(ctx)
		if err != nil {
			return nil, err
		}
		if view, ok := current.(map[string]any); ok {
			if connection, ok := view["connection"].(map[string]any); ok {
				catalog["centerId"] = connection["centerId"]
			}
		}
		return catalog, nil
	case "controlGrantsGet", "controlGrantsSave":
		if s.SourceStream == nil {
			return nil, ErrUnavailable
		}
		var req sourcestream.ControlGrantRequest
		if unsv2.Decode(raw, &req) != nil {
			return nil, ErrInvalid
		}
		return s.SourceStream.ControlGrant(ctx, req, op == "controlGrantsSave")
	case "currentGet":
		if err := unsv2.Decode(raw, &struct{}{}); err != nil {
			return nil, ErrInvalid
		}
		return s.Cluster.CollaborationCurrent(ctx)
	case "connectionCollectionsSave":
		var req cluster.FleetCollectionScopeRequest
		if unsv2.Decode(raw, &req) != nil {
			return nil, ErrInvalid
		}
		return s.Cluster.SaveCollectionSyncScope(ctx, req)
	case "connectionAccessGet":
		if unsv2.Decode(raw, &struct{}{}) != nil {
			return nil, ErrInvalid
		}
		return s.Cluster.FleetAccess()
	case "connectionAccessSave":
		var req cluster.FleetAccessRequest
		if unsv2.Decode(raw, &req) != nil {
			return nil, ErrInvalid
		}
		return s.Cluster.SaveFleetAccess(ctx, req)
	case "connectionUsers":
		if err := unsv2.Decode(raw, &struct{}{}); err != nil {
			return nil, ErrInvalid
		}
		return s.Cluster.SyncedFleetUsers(ctx)
	case "connectionConfigure":
		var req struct {
			CenterURL      string `json:"centerUrl"`
			NodeID         string `json:"nodeId"`
			EnrollmentCode string `json:"enrollmentCode"`
			NetworkNodeID  string `json:"networkNodeId"`
		}
		if err := unsv2.Decode(raw, &req); err != nil {
			return nil, ErrInvalid
		}
		return s.Cluster.ConfigureCollaboration(ctx, req.CenterURL, req.NodeID, req.EnrollmentCode, req.NetworkNodeID)

	}
	return nil, repo.ErrNotFound
}
