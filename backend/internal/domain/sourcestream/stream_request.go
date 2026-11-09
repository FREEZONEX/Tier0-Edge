package sourcestream

import (
	"context"
	"encoding/json"
	"strings"

	"backend/internal/contextx"
	"backend/internal/operationcontract"
	"backend/internal/repo"
)

type streamRequestContextKey struct{}

func isStreamRequestContext(ctx context.Context) bool {
	value, _ := ctx.Value(streamRequestContextKey{}).(bool)
	return value
}

// These entry points select request permissions without manufacturing legacy
// Control or Fleet authority on the authenticated subject.
func streamRequestContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, streamRequestContextKey{}, true)
}

func (s *Service) streamRequestAccess(ctx context.Context, permissions ...string) error {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.FleetTask != nil || subject.MCPRestricted {
		return ErrForbidden
	}
	invoking := false
	for _, permission := range permissions {
		invoking = invoking || permission == "control.execute"
	}
	for _, permission := range permissions {
		key := ""
		switch permission {
		case "control.read":
			if invoking {
				continue
			}
			key = "stream.requests.read"
			if !contextx.HasResource(subject, key) && contextx.HasResource(subject, "stream.requests.manage") {
				key = "stream.requests.manage"
			}
		case "control.execute":
			key = "stream.requests.invoke"
		case "control.write":
			key = "stream.requests.write"
		default:
			return ErrForbidden
		}
		if !contextx.HasResource(subject, key) {
			return ErrForbidden
		}
		if subject.APIKeyUsageType == repo.APIKeyUsageAppData {
			if s.controlAccess == nil {
				return ErrForbidden
			}
			if err := s.controlAccess(ctx, key); err != nil {
				return err
			}
		}
	}
	return nil
}

type PublicStreamRequest struct {
	Code            string         `json:"code"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	ContractVersion string         `json:"contractVersion"`
	InputSchema     map[string]any `json:"inputSchema"`
	OutputSchema    map[string]any `json:"outputSchema"`
	SideEffect      string         `json:"sideEffect"`
	Idempotency     string         `json:"idempotency"`
	TimeoutMs       int            `json:"timeoutMs"`
	Status          string         `json:"status"`
}

type StreamRequestPage struct {
	Total int64                 `json:"total"`
	List  []PublicStreamRequest `json:"list"`
}

type StreamRequestResult struct {
	Code            string `json:"code"`
	ContractVersion string `json:"contractVersion"`
	Result          any    `json:"result"`
	RunID           string `json:"runID"`
	Status          string `json:"status"`
	Replayed        bool   `json:"replayed"`
}

func publicRequest(definition repo.OperationDefinition) (PublicStreamRequest, error) {
	if definition.DeclarationKind != "request" || definition.Resource.ValidateRequest() != nil {
		return PublicStreamRequest{}, repo.ErrNotFound
	}
	result := PublicStreamRequest{Code: definition.Code, Name: definition.Name, Description: definition.Description,
		ContractVersion: definition.ContractVersion, SideEffect: definition.SideEffect, Idempotency: definition.Idempotency,
		TimeoutMs: definition.TimeoutMs, Status: definition.Status}
	if result.Status == "disabled" {
		result.Status = "inactive"
	}
	if json.Unmarshal(definition.InputSchema, &result.InputSchema) != nil || json.Unmarshal(definition.OutputSchema, &result.OutputSchema) != nil {
		return PublicStreamRequest{}, ErrUnavailable
	}
	return result, nil
}

func (s *Service) DiscoverStreamRequests(ctx context.Context, search string, page, size int64) (StreamRequestPage, error) {
	result := StreamRequestPage{List: []PublicStreamRequest{}}
	if err := s.streamRequestAccess(ctx, "control.read"); err != nil {
		return result, err
	}
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if page < 1 || page > 100000 || size < 1 || size > 200 || len(search) > 256 {
		return result, repo.ErrInvalidArgument
	}
	r := repo.NewSourceStreamRepo(ctx)
	if subject, _ := contextx.SubjectFrom(ctx); subject.APIKeyUsageType == repo.APIKeyUsageAppData {
		// Filter before pagination/count so denied codes are not observable.
		for candidatePage := 1; ; candidatePage++ {
			rows, total, err := r.StreamRequests(ctx, search, candidatePage, 200, subject.APIKeyAppID)
			if err != nil {
				return result, err
			}
			for _, row := range rows {
				var definition repo.OperationDefinition
				if json.Unmarshal(row.Data, &definition) != nil {
					return result, ErrUnavailable
				}
				if err := s.checkAppRequestRead(ctx, definition); err != nil {
					if requestGrantDenied(err) {
						continue
					}
					return result, err
				}
				item, err := publicRequest(definition)
				if err != nil {
					return result, err
				}
				result.Total++
				if result.Total > (page-1)*size && result.Total <= page*size {
					result.List = append(result.List, item)
				}
			}
			if int64(candidatePage*200) >= total {
				return result, nil
			}
		}
	}
	rows, total, err := r.StreamRequests(ctx, search, int(page), int(size))
	if err != nil {
		return result, err
	}
	for _, row := range rows {
		var definition repo.OperationDefinition
		if json.Unmarshal(row.Data, &definition) != nil {
			return result, ErrUnavailable
		}
		item, err := publicRequest(definition)
		if err != nil {
			return result, err
		}
		result.List = append(result.List, item)
	}
	result.Total = total
	return result, nil
}

func (s *Service) DescribeStreamRequest(ctx context.Context, code string) (PublicStreamRequest, error) {
	if err := s.streamRequestAccess(ctx, "control.read"); err != nil {
		return PublicStreamRequest{}, err
	}
	if !operationcontract.ValidRequestCode(code) {
		return PublicStreamRequest{}, repo.ErrInvalidArgument
	}
	_, definition, err := repo.NewSourceStreamRepo(ctx).Operation(ctx, code)
	if err != nil {
		return PublicStreamRequest{}, err
	}
	if definition.Status != "active" {
		return PublicStreamRequest{}, repo.ErrNotFound
	}
	if err := s.checkAppRequestRead(ctx, definition); err != nil {
		return PublicStreamRequest{}, err
	}
	return publicRequest(definition)
}

func (s *Service) InvokeStreamRequest(ctx context.Context, code, key string, input any) (StreamRequestResult, error) {
	if !operationcontract.ValidRequestCode(code) || strings.TrimSpace(key) != key || len(key) > 128 {
		return StreamRequestResult{}, repo.ErrInvalidArgument
	}
	view, err := s.InvokeOperationResource(streamRequestContext(ctx), code, key, input)
	if err != nil {
		return StreamRequestResult{}, err
	}
	execution, ok := view["execution"].(map[string]any)
	if !ok {
		return StreamRequestResult{}, ErrUnknownResult
	}
	result := StreamRequestResult{Code: code, Result: view["result"]}
	result.ContractVersion, _ = view["contractVersion"].(string)
	result.RunID, _ = execution["runId"].(string)
	result.Status, _ = execution["status"].(string)
	result.Replayed, _ = execution["replayed"].(bool)
	if result.ContractVersion == "" || result.RunID == "" || result.Status != "succeeded" {
		return StreamRequestResult{}, ErrUnknownResult
	}
	return result, nil
}
