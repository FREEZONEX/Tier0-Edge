package cluster

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"backend/internal/repo"
)

func (s *Service) FleetNodeAccounts(ctx context.Context, fleetNodeID string) (map[string]any, error) {
	if strings.TrimSpace(fleetNodeID) == "" {
		return nil, ErrInvalid
	}
	snapshot, err := s.repo.GetFleetNodeAccountAssignments(ctx, fleetNodeID)
	if err != nil {
		return nil, err
	}
	return fleetAccountAssignmentResponse(snapshot, false), nil
}

func (s *Service) ReplaceFleetNodeAccounts(ctx context.Context, cmd FleetAccountAssignmentCommand) (map[string]any, error) {
	if strings.TrimSpace(cmd.FleetNodeID) == "" || cmd.ExpectedRevision < 0 {
		return nil, ErrInvalid
	}
	snapshot, changed, err := s.repo.ReplaceFleetNodeAccountAssignments(
		ctx,
		cmd.FleetNodeID,
		cmd.CenterUserIDs,
		cmd.ExpectedRevision,
		cmd.UserID,
	)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrConflict):
			return nil, ErrAccountRevisionConflict
		case errors.Is(err, repo.ErrFleetUserNotAssignable), errors.Is(err, repo.ErrInvalidArgument):
			return nil, ErrAccountNotAssignable
		default:
			return nil, err
		}
	}
	return fleetAccountAssignmentResponse(snapshot, changed), nil
}

func fleetAccountAssignmentResponse(snapshot repo.FleetAccountAssignmentSnapshot, changed bool) map[string]any {
	centerUserIDs := make([]string, 0, len(snapshot.Accounts))
	accounts := make([]map[string]any, 0, len(snapshot.Accounts))
	for _, account := range snapshot.Accounts {
		centerUserID := strconv.FormatInt(account.CenterUserID, 10)
		centerUserIDs = append(centerUserIDs, centerUserID)
		accounts = append(accounts, map[string]any{
			"centerUserID":       centerUserID,
			"userName":           account.UserName,
			"nickName":           account.NickName,
			"email":              account.Email,
			"phone":              account.Phone,
			"userStatus":         account.UserStatus,
			"roleCode":           account.RoleCode,
			"assignmentRevision": account.AssignmentRevision,
			"assignedBy":         strconv.FormatInt(account.AssignedBy, 10),
			"assignedAt":         account.AssignedAt,
		})
	}
	return map[string]any{
		"nodeID":        snapshot.NodeID,
		"fleetNodeID":   snapshot.FleetNodeID,
		"revision":      snapshot.Revision,
		"changed":       changed,
		"centerUserIDs": centerUserIDs,
		"accounts":      accounts,
	}
}
