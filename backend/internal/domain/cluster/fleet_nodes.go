package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	domaincommon "backend/internal/domain/common"
	domainlicense "backend/internal/domain/license"
	"backend/internal/fleetprotocol"
	"backend/internal/repo"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

type CreateFleetNodeCommand struct {
	Name        string
	Description string
	UserID      int64
}

type ListFleetNodesQuery struct {
	Keyword      string
	Status       string
	OS           string
	AgentVersion string
	Page         int
	Size         int
}

type UpdateFleetNodeCommand struct {
	FleetNodeID string
	Name        string
	Description string
	UserID      int64
}

func (s *Service) CreateFleetNode(ctx context.Context, cmd CreateFleetNodeCommand) (map[string]any, error) {
	name := strings.TrimSpace(cmd.Name)
	description := strings.TrimSpace(cmd.Description)
	if !validFleetNodeNameAndDescription(name, description) {
		return nil, ErrInvalid
	}
	fleetNodeID := "fn_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	var created repo.FleetNodeConnection
	err := s.fleetRepo.WithTransaction(ctx, func(tx *repo.FleetRepo) error {
		var err error
		created, err = tx.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
			FleetNodeID: fleetNodeID,
			Name:        name,
			Description: description,
			CenterType:  "enterprise",
			ActorID:     cmd.UserID,
			BranchLimit: domainlicense.Default().BranchLimit(),
		})
		if err != nil {
			return err
		}
		return err
	})
	if err != nil {
		if errors.Is(err, repo.ErrFleetNodeQuotaExceeded) {
			return nil, ErrFleetNodeQuotaExceeded
		}
		return nil, err
	}
	response := fleetNodeResponse(repo.FleetNodeView{
		FleetNode:        created.Node,
		CredentialID:     created.Client.ClientID,
		CredentialStatus: "active",
	})
	response["accessToken"] = s.edgeToken(created.Client, created.Node)
	response["tunnel"] = s.fleetTunnelStatus(fleetNodeID)
	return response, nil
}

func (s *Service) FleetNodes(ctx context.Context, query ListFleetNodesQuery) (map[string]any, error) {
	status := strings.TrimSpace(strings.ToLower(query.Status))
	if status != "" && !validFleetNodeStatus(status) {
		return nil, ErrInvalid
	}
	items, total, err := s.fleetRepo.ListNodeViews(ctx, repo.FleetNodeListFilter{
		Keyword:      query.Keyword,
		Status:       status,
		OS:           query.OS,
		AgentVersion: query.AgentVersion,
		Page:         query.Page,
		Size:         query.Size,
	})
	if err != nil {
		return nil, err
	}
	counts, err := s.fleetRepo.CountNodesByStatus(ctx)
	if err != nil {
		return nil, err
	}
	snapshotRecords, err := s.fleetRepo.ListContainerSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	snapshots := fleetNodeSnapshotSummaries(snapshotRecords)
	list := make([]map[string]any, 0, len(items))
	onlineWithAbnormalContainers := 0
	for _, record := range snapshotRecords {
		snapshot, ok := snapshots[record.FleetNodeID]
		if record.NodeStatus == repo.FleetNodeStatusOnline && ok && snapshot.unhealthyContainerCount > 0 {
			onlineWithAbnormalContainers++
		}
	}
	for _, item := range items {
		response := fleetNodeResponse(item)
		response["tunnel"] = s.fleetTunnelStatus(item.FleetNodeID)
		if snapshot, ok := snapshots[item.FleetNodeID]; ok {
			response["lastSyncAt"] = snapshot.observedAt
			response["containerCount"] = snapshot.containerCount
			response["unhealthyContainerCount"] = snapshot.unhealthyContainerCount
		}
		list = append(list, response)
	}
	page, size := normalizeFleetPagination(query.Page, query.Size)
	active := counts[repo.FleetNodeStatusUnjoined] + counts[repo.FleetNodeStatusOnline] + counts[repo.FleetNodeStatusOffline]
	return map[string]any{
		"list":  list,
		"total": total,
		"page":  page,
		"size":  size,
		"summary": map[string]any{
			"active":                       active,
			"limit":                        domainlicense.Default().BranchLimit(),
			"online":                       counts[repo.FleetNodeStatusOnline],
			"offline":                      counts[repo.FleetNodeStatusOffline],
			"unjoined":                     counts[repo.FleetNodeStatusUnjoined],
			"deleted":                      counts[repo.FleetNodeStatusDeleted],
			"onlineWithAbnormalContainers": onlineWithAbnormalContainers,
		},
	}, nil
}

type fleetNodeSnapshotSummary struct {
	observedAt              int64
	containerCount          int
	unhealthyContainerCount int
}

func fleetNodeSnapshotSummaries(records []repo.FleetContainerSnapshot) map[string]fleetNodeSnapshotSummary {
	summaries := make(map[string]fleetNodeSnapshotSummary, len(records))
	for _, record := range records {
		var snapshot fleetprotocol.SnapshotPayload
		if json.Unmarshal(record.Payload, &snapshot) != nil {
			continue
		}
		counts := fleetprotocol.SummarizeContainers(snapshot.Containers.Containers)
		summaries[record.FleetNodeID] = fleetNodeSnapshotSummary{
			observedAt:              record.ObservedAt,
			containerCount:          counts.Total,
			unhealthyContainerCount: counts.Unhealthy,
		}
	}
	return summaries
}

func (s *Service) FleetNode(ctx context.Context, fleetNodeID string, includeDeleted bool) (map[string]any, error) {
	view, err := s.fleetRepo.GetNodeView(ctx, fleetNodeID, includeDeleted)
	if err != nil {
		return nil, err
	}
	response := fleetNodeResponse(view)
	response["tunnel"] = s.fleetTunnelStatus(fleetNodeID)
	return response, nil
}

func (s *Service) UpdateFleetNode(ctx context.Context, cmd UpdateFleetNodeCommand) (map[string]any, error) {
	cmd.FleetNodeID = strings.TrimSpace(cmd.FleetNodeID)
	cmd.Name = strings.TrimSpace(cmd.Name)
	cmd.Description = strings.TrimSpace(cmd.Description)
	if cmd.FleetNodeID == "" || !validFleetNodeNameAndDescription(cmd.Name, cmd.Description) {
		return nil, ErrInvalid
	}
	var view repo.FleetNodeView
	err := s.fleetRepo.WithTransaction(ctx, func(tx *repo.FleetRepo) error {
		var err error
		view, err = tx.UpdateNode(ctx, cmd.FleetNodeID, cmd.Name, cmd.Description, cmd.UserID)
		if err != nil {
			return err
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	response := fleetNodeResponse(view)
	response["tunnel"] = s.fleetTunnelStatus(cmd.FleetNodeID)
	return response, nil
}

func (s *Service) DeleteFleetNode(ctx context.Context, fleetNodeID string, userID int64) (map[string]any, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	if fleetNodeID == "" {
		return nil, ErrInvalid
	}
	err := s.fleetRepo.DeleteNode(ctx, fleetNodeID, userID)
	if err != nil {
		return nil, err
	}
	if s.tunnel != nil {
		s.tunnel.DisconnectNode(fleetNodeID)
	}
	s.fleet.disconnectAndForget(fleetNodeID)
	return map[string]any{"fleetNodeID": fleetNodeID, "deleted": true}, nil
}

func (s *Service) RestoreFleetNode(ctx context.Context, fleetNodeID string, userID int64) (map[string]any, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	if fleetNodeID == "" {
		return nil, ErrInvalid
	}
	restored, err := s.fleetRepo.RestoreNodeWithConnection(
		ctx,
		fleetNodeID,
		userID,
		domainlicense.Default().BranchLimit(),
	)
	if err != nil {
		if errors.Is(err, repo.ErrFleetNodeQuotaExceeded) {
			return nil, ErrFleetNodeQuotaExceeded
		}
		return nil, err
	}
	logx.WithContext(ctx).Infof("fleet node restored with fresh credential fleetNodeID=%s credentialID=%s", fleetNodeID, restored.Client.ClientID)
	response := fleetNodeResponse(repo.FleetNodeView{
		FleetNode:        restored.Node,
		CredentialID:     restored.Client.ClientID,
		CredentialStatus: "active",
	})
	response["accessToken"] = s.edgeToken(restored.Client, restored.Node)
	response["tunnel"] = s.fleetTunnelStatus(fleetNodeID)
	return response, nil
}

func fleetNodeResponse(view repo.FleetNodeView) map[string]any {
	return map[string]any{
		"fleetNodeID":          view.FleetNodeID,
		"mountId":              view.FleetNodeID,
		"mountPointId":         "",
		"name":                 view.Name,
		"description":          view.Description,
		"centerType":           view.CenterType,
		"status":               view.Status,
		"activeSessionID":      view.ActiveSessionID,
		"firstJoinedAt":        view.FirstJoinedAt,
		"lastOnlineAt":         view.LastOnlineAt,
		"lastOfflineAt":        view.LastOfflineAt,
		"agentVersion":         view.AgentVersion,
		"protocolVersion":      view.ProtocolVersion,
		"os":                   view.OS,
		"arch":                 view.Arch,
		"assignmentRevision":   view.AssignmentRevision,
		"credentialID":         view.CredentialID,
		"credentialStatus":     view.CredentialStatus,
		"credentialExpiresAt":  view.CredentialExpiresAt,
		"credentialLastUsedAt": view.CredentialLastUsedAt,
		"createdBy":            strconv.FormatInt(view.CreatedBy, 10),
		"updatedBy":            strconv.FormatInt(view.UpdatedBy, 10),
		"createdAt":            view.CreatedTime.UnixMilli(),
		"updatedAt":            view.UpdatedTime.UnixMilli(),
		"deletedAt":            fleetDeletedAtMilli(int64(view.DeletedTime)),
	}
}

func fleetDeletedAtMilli(deletedTime int64) int64 {
	if deletedTime <= 0 {
		return 0
	}
	return deletedTime * 1000
}

func validFleetNodeNameAndDescription(name, description string) bool {
	return name != "" &&
		!domaincommon.ExceedMaxLength(name, domaincommon.ConnectionNameMaxLen) &&
		!domaincommon.ExceedDescLimit(description)
}

func validFleetNodeStatus(status string) bool {
	switch status {
	case repo.FleetNodeStatusUnjoined, repo.FleetNodeStatusOnline, repo.FleetNodeStatusOffline, repo.FleetNodeStatusDeleted:
		return true
	default:
		return false
	}
}

func normalizeFleetPagination(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

// ReissueFleetToken does not enroll a business node or alter UNS/account assignments.
func (s *Service) ReissueFleetToken(ctx context.Context, nodeID string, actorID int64) (map[string]any, error) {
	if !domainlicense.Default().IsHost() {
		return nil, ErrUnauthorized
	}
	rotated, err := s.fleetRepo.ReissueNodeCredential(ctx, nodeID, actorID)
	if err != nil {
		return nil, err
	}
	if s.tunnel != nil {
		s.tunnel.DisconnectNode(nodeID)
	}
	if s.fleet != nil {
		s.fleet.disconnectAndForget(nodeID)
	}
	s.recordFleetNodeSessionAudit(nodeID, rotated.Node.Name, "fleetTokenReissue", "infrastructure credential rotated")
	response := fleetNodeResponse(repo.FleetNodeView{FleetNode: rotated.Node, CredentialID: rotated.Client.ClientID, CredentialStatus: "active"})
	response["accessToken"] = s.edgeToken(rotated.Client, rotated.Node)
	return response, nil
}
