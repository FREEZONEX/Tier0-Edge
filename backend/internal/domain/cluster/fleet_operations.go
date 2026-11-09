package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/fleetprotocol"
	"backend/internal/repo"

	"github.com/zeromicro/go-zero/core/logx"
)

func (s *Service) FleetOperation(ctx context.Context, fleetNodeID, operationID string) (map[string]any, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	operationID = strings.TrimSpace(operationID)
	if fleetNodeID == "" || operationID == "" {
		return nil, ErrInvalid
	}
	if _, err := s.fleetRepo.GetNode(ctx, fleetNodeID, false); err != nil {
		return nil, err
	}
	record, err := s.fleetRepo.GetOperation(ctx, operationID)
	if err != nil {
		return nil, err
	}
	if record.FleetNodeID != fleetNodeID {
		return nil, repo.ErrNotFound
	}
	return fleetOperationResponse(record), nil
}

func (s *Service) FleetOperations(ctx context.Context, fleetNodeID string, page, size int64) (map[string]any, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	if fleetNodeID == "" {
		return nil, ErrInvalid
	}
	if _, err := s.fleetRepo.GetNode(ctx, fleetNodeID, false); err != nil {
		return nil, err
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	records, total, err := s.fleetRepo.ListOperations(ctx, fleetNodeID, page, size)
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(records))
	for _, record := range records {
		list = append(list, fleetOperationResponse(record))
	}
	return map[string]any{"list": list, "total": total, "page": page, "size": size}, nil
}

func (s *Service) createFleetOperation(ctx context.Context, input repo.CreateFleetOperationInput) (repo.FleetOperation, bool, error) {
	record, created, err := s.fleetRepo.CreateOperation(ctx, input)
	if errors.Is(err, repo.ErrConflict) || errors.Is(err, repo.ErrDuplicate) {
		return repo.FleetOperation{}, false, &FleetAgentResponseError{StatusCode: http.StatusConflict, Code: "FLEET_CONTAINER_OPERATION_CONFLICT"}
	}
	if err != nil {
		return repo.FleetOperation{}, false, err
	}
	if !created && (record.FleetNodeID != strings.TrimSpace(input.FleetNodeID) || record.ContainerID != strings.TrimSpace(input.ContainerID) || record.OperationType != strings.TrimSpace(input.OperationType)) {
		return repo.FleetOperation{}, false, &FleetAgentResponseError{StatusCode: http.StatusConflict, Code: "FLEET_CONTAINER_OPERATION_CONFLICT"}
	}
	return record, created, nil
}

func (s *Service) executeFleetRestart(operation repo.FleetOperation) {
	startedAt := time.Now().UTC().UnixMilli()
	if err := s.fleetRepo.MarkOperationRunning(context.Background(), operation.OperationID, startedAt); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fleetprotocol.CommandTimeout+5*time.Second)
	defer cancel()
	var err error
	if s.hasFleetInfrastructure(ctx, operation.FleetNodeID) || s.fleetTunnelStatus(operation.FleetNodeID).Status == "online" {
		var result fleetprotocol.ContainerRestartResult
		err = s.callFleetInfrastructure(ctx, operation.FleetNodeID, fleetTunnelRequest{
			Operation: fleetTunnelOperationRestart, ContainerID: operation.ContainerID,
			OperationID: operation.OperationID,
		}, &result)
		if err == nil {
			var resultJSON json.RawMessage
			resultJSON, err = json.Marshal(result)
			if err == nil {
				if completeErr := s.fleetRepo.CompleteOperation(context.Background(), operation.OperationID, repo.FleetOperationStatusSucceeded, "", resultJSON, time.Now().UTC().UnixMilli()); completeErr != nil {
					logx.Errorf("complete fleet restart operation operationID=%s error=%v", operation.OperationID, completeErr)
				}
				return
			}
		}
	} else if session, sessionErr := s.fleet.requireSession(operation.FleetNodeID); sessionErr == nil {
		var raw json.RawMessage
		raw, err = session.execute(ctx, fleetprotocol.OperationContainerRestart, operation.ContainerID, operation.OperationID, struct{}{}, fleetprotocol.CommandTimeout)
		if err == nil {
			var result fleetprotocol.ContainerRestartResult
			if decodeErr := unmarshalFleetResult(raw, &result); decodeErr != nil {
				err = decodeErr
			} else {
				resultJSON, marshalErr := json.Marshal(result)
				if marshalErr != nil {
					err = marshalErr
				} else if completeErr := s.fleetRepo.CompleteOperation(context.Background(), operation.OperationID, repo.FleetOperationStatusSucceeded, "", resultJSON, time.Now().UTC().UnixMilli()); completeErr != nil {
					logx.Errorf("complete fleet restart operation operationID=%s error=%v", operation.OperationID, completeErr)
				}
				return
			}
		}
	} else {
		err = sessionErr
	}
	status, code := fleetOperationFailure(err)
	if completeErr := s.fleetRepo.CompleteOperation(context.Background(), operation.OperationID, status, code, nil, time.Now().UTC().UnixMilli()); completeErr != nil {
		logx.Errorf("fail fleet restart operation operationID=%s error=%v", operation.OperationID, completeErr)
	}
}

func fleetOperationFailure(err error) (string, string) {
	if err == nil {
		return repo.FleetOperationStatusFailed, "FLEET_AGENT_COMMAND_FAILED"
	}
	if errors.Is(err, context.Canceled) {
		return repo.FleetOperationStatusCanceled, "FLEET_OPERATION_CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return repo.FleetOperationStatusTimedOut, "FLEET_OPERATION_TIMEOUT"
	}
	var responseErr *FleetAgentResponseError
	if errors.As(err, &responseErr) {
		if responseErr.Code == "FLEET_OPERATION_TIMEOUT" {
			return repo.FleetOperationStatusTimedOut, responseErr.Code
		}
		return repo.FleetOperationStatusFailed, strings.TrimSpace(responseErr.Code)
	}
	return repo.FleetOperationStatusFailed, "FLEET_AGENT_COMMAND_FAILED"
}

func fleetOperationResponse(record repo.FleetOperation) map[string]any {
	response := map[string]any{
		"operationID":   record.OperationID,
		"fleetNodeID":   record.FleetNodeID,
		"containerID":   record.ContainerID,
		"operationType": record.OperationType,
		"status":        record.Status,
		"actorID":       strconv.FormatInt(record.ActorID, 10),
		"startedAt":     record.StartedAt,
		"finishedAt":    record.FinishedAt,
		"createdAt":     record.CreatedTime.UnixMilli(),
		"updatedAt":     record.UpdatedTime.UnixMilli(),
	}
	if len(record.RequestJSON) > 0 {
		response["request"] = record.RequestJSON
	}
	if len(record.ResultJSON) > 0 {
		response["result"] = record.ResultJSON
	}
	if strings.TrimSpace(record.ErrorCode) != "" {
		response["errorCode"] = record.ErrorCode
	}
	return response
}
