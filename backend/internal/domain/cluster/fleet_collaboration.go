package cluster

import (
	"backend/internal/contextx"
	"backend/internal/domain/license"
	"backend/internal/repo"
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"
)

// CollaborationNodes currently exposes registration candidates. Registration,
// overlay reachability and Backend health are deliberately not inferred from
// the legacy Fleet Agent session or MQTT connection status.
func (s *Service) CreateCollaborationNode(ctx context.Context, name, description string) (map[string]any, error) {
	if !license.Default().IsHost() {
		return nil, ErrUnauthorized
	}
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if !validFleetNodeNameAndDescription(name, description) {
		return nil, ErrInvalid
	}
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok || actor.UserID <= 0 {
		return nil, ErrUnauthorized
	}
	node, err := s.fleetRepo.CreateCollaborationNode(ctx, repo.CreateFleetNodeInput{FleetNodeID: "fn_" + strings.ReplaceAll(uuid.NewString(), "-", ""), Name: name, Description: description, ActorID: actor.UserID, BranchLimit: license.Default().BranchLimit()})
	if err != nil {
		return nil, err
	}
	return s.CollaborationNode(ctx, node.FleetNodeID)
}
func (s *Service) CollaborationNode(ctx context.Context, id string) (map[string]any, error) {
	if !license.Default().IsHost() {
		return nil, ErrUnauthorized
	}
	node, err := s.fleetRepo.CollaborationNode(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		candidate, lookupErr := s.fleetRepo.GetNode(ctx, id, false)
		if lookupErr != nil {
			return nil, lookupErr
		}
		node = repo.FleetCollaborationNodeView{
			FleetCollaborationNode: repo.FleetCollaborationNode{NodeID: id, RegistrationStatus: "unregistered", NetworkStatus: "direct", BusinessStatus: "unchecked"},
			Name:                   candidate.Name, Description: candidate.Description,
		}
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"node": node}, nil
}
func (s *Service) CollaborationNodes(ctx context.Context, page, size int) (map[string]any, error) {
	if !license.Default().IsHost() {
		return nil, ErrUnauthorized
	}
	nodes, total, err := s.fleetRepo.CollaborationNodes(ctx, page, size)
	if err != nil {
		return nil, err
	}
	if nodes == nil {
		nodes = []repo.FleetCollaborationNodeView{}
	}
	return map[string]any{"items": nodes, "page": page, "size": size, "total": total}, nil
}

func (s *Service) CollaborationEnabled() bool { return s != nil && s.cfg.CollaborationEnabled }
