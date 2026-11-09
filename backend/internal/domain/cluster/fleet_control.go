package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/license"
	"backend/internal/mcpaccess"
	"backend/internal/operationcontract"
	"backend/internal/repo"
	"backend/internal/unsref"
	"gorm.io/gorm"
)

type FleetControlRequest struct {
	NodeID            string          `json:"nodeId"`
	ControlCode       string          `json:"controlCode"`
	CapabilityVersion string          `json:"capabilityVersion"`
	GrantID           string          `json:"grantId"`
	GrantRevision     int64           `json:"grantRevision"`
	DeviceParameter   string          `json:"deviceParameter"`
	DeviceID          string          `json:"deviceId"`
	Input             json.RawMessage `json:"input"`
	ExpiresAt         int64           `json:"expiresAt"`
	IdempotencyKey    string          `json:"idempotencyKey"`
	InvocationID      string          `json:"invocationId"`
	ExpectedRevision  int64           `json:"expectedRevision"`
	Page              int             `json:"page"`
	Size              int             `json:"size"`
}

type FleetControlCapability struct {
	Code              string `json:"code"`
	CapabilityVersion string `json:"capabilityVersion"`
	Status            string `json:"status"`
	Contract          struct {
		InputSchema json.RawMessage `json:"inputSchema"`
		SideEffect  string          `json:"sideEffect"`
	} `json:"contract"`
	Authorization struct {
		GrantID           string          `json:"grantId"`
		Revision          int64           `json:"revision"`
		CapabilityVersion string          `json:"capabilityVersion"`
		InputSchema       json.RawMessage `json:"inputSchema"`
		MaxTTLMillis      int64           `json:"maxTtlMs"`
		Enabled           bool            `json:"enabled"`
		AllowAutomatic    bool            `json:"allowAutomatic"`
	} `json:"authorization"`
}

func (s *Service) SetFleetControlSubject(refresh func(context.Context, contextx.Subject) (contextx.Subject, error)) {
	s.fleetControlSubject = refresh
}

func fleetControlAccess(subject contextx.Subject, write bool) bool {
	if subject.UserID <= 0 || subject.FleetTask != nil || subject.APIKeyAppID != "" {
		return false
	}
	if subject.MCPRestricted && !mcpaccess.Has(subject, "fleet.control.write") && (write || !mcpaccess.Has(subject, "fleet.control.read")) {
		return false
	}
	keys := []string{"cluster.edge.manage", "control.read"}
	if write {
		keys = append(keys, "control.execute", "control.write")
	}
	for _, key := range keys {
		if !contextx.HasResource(subject, key) {
			return false
		}
	}
	return true
}

func fleetControlActor(subject contextx.Subject) json.RawMessage {
	// Store identifiers needed to recheck authority, never tokens or credentials.
	raw, _ := json.Marshal(contextx.Subject{AuthType: subject.AuthType, UserID: subject.UserID, APIKeyID: subject.APIKeyID, MCPRestricted: subject.MCPRestricted, MCPScopes: subject.MCPScopes, MCPGrantID: subject.MCPGrantID})
	return raw
}

func (s *Service) controlCapability(ctx context.Context, request FleetControlRequest) (FleetControlCapability, error) {
	var result FleetControlCapability
	if !unsref.ValidMetadataID(request.NodeID) || !unsref.ValidMetadataID(request.ControlCode) || !unsref.ValidMetadataID(request.GrantID) || request.GrantRevision <= 0 || len(request.CapabilityVersion) != 64 {
		return result, ErrInvalid
	}
	raw, err := s.CallFleetControl(ctx, request.NodeID, request.ControlCode, "describe", request.GrantID, request.GrantRevision, map[string]any{}, nil)
	if err != nil {
		return result, err
	}
	if json.Unmarshal(raw, &result) != nil || result.Code != request.ControlCode || result.Status != "active" || result.CapabilityVersion != request.CapabilityVersion || !result.Authorization.Enabled || result.Authorization.GrantID != request.GrantID || result.Authorization.Revision != request.GrantRevision || result.Authorization.CapabilityVersion != request.CapabilityVersion {
		return result, repo.ErrControlCapability
	}
	return result, nil
}

func (s *Service) FleetControl(ctx context.Context, op string, request FleetControlRequest) (any, error) {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !s.CollaborationEnabled() || !license.Default().IsHost() || !fleetControlAccess(subject, op == "controlIntentsCreate" || op == "controlIntentsConfirm") {
		return nil, ErrUnauthorized
	}
	switch op {
	case "controlsCatalog":
		if !unsref.ValidMetadataID(request.NodeID) {
			return nil, ErrInvalid
		}
		page, size := request.Page, request.Size
		if page == 0 {
			page = 1
		}
		if size == 0 {
			size = 50
		}
		if page < 1 || page > 100000 || size < 1 || size > 100 {
			return nil, ErrInvalid
		}
		return s.CallFleetControl(ctx, request.NodeID, "node", "discover", "", 0, map[string]any{"page": page, "size": size}, nil)
	case "controlIntentsList":
		page, size := request.Page, request.Size
		if page == 0 {
			page = 1
		}
		if size == 0 {
			size = 50
		}
		if page < 1 || page > 100000 || size < 1 || size > 100 {
			return nil, ErrInvalid
		}
		q := repo.GetUnsConn(ctx).Model(&repo.FleetControlIntent{})
		if request.NodeID != "" {
			q = q.Where("node_id=?", request.NodeID)
		}
		var total int64
		if err := q.Count(&total).Error; err != nil {
			return nil, err
		}
		rows := []repo.FleetControlIntent{}
		if err := q.Order("created_at DESC,invocation_id").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
			return nil, err
		}
		return map[string]any{"items": rows, "total": total, "page": page, "size": size}, nil
	case "controlIntentsGet", "controlIntentsQuery":
		if !unsref.ValidMetadataID(request.InvocationID) {
			return nil, ErrInvalid
		}
		row, err := repo.GetFleetControlIntent(ctx, request.InvocationID)
		if err != nil {
			return nil, err
		}
		if op == "controlIntentsQuery" && (row.State == "dispatched" || row.State == "unknown") {
			s.queryControlIntent(ctx, row)
			return repo.GetFleetControlIntent(ctx, request.InvocationID)
		}
		return row, nil
	case "controlIntentsConfirm":
		if !unsref.ValidMetadataID(request.InvocationID) || request.ExpectedRevision <= 0 {
			return nil, ErrInvalid
		}
		row, err := repo.GetFleetControlIntent(ctx, request.InvocationID)
		if err != nil {
			return nil, err
		}
		if err := s.requireRemoteControl(ctx, row.NodeID); err != nil {
			return nil, err
		}
		return repo.ConfirmFleetControlIntent(ctx, request.InvocationID, request.ExpectedRevision, subject.UserID, fleetControlActor(subject))
	case "controlIntentsCreate":
		if request.IdempotencyKey == "" || len(request.IdempotencyKey) > 128 || strings.TrimSpace(request.IdempotencyKey) != request.IdempotencyKey || !unsref.ValidMetadataID(request.DeviceParameter) || !unsref.ValidMetadataID(request.DeviceID) || len(request.Input) > 65536 || request.ExpiresAt > time.Now().Add(5*time.Minute).UnixMilli() {
			return nil, ErrInvalid
		}
		var input map[string]any
		if json.Unmarshal(request.Input, &input) != nil || input == nil || input[request.DeviceParameter] != request.DeviceID {
			return nil, ErrInvalid
		}
		inputHash, err := repo.FleetContentHash(json.RawMessage(request.Input))
		if err != nil {
			return nil, ErrInvalid
		}
		principal := "user:" + strconv.FormatInt(subject.UserID, 10) + ":key:" + strconv.FormatInt(subject.APIKeyID, 10)
		identity, _ := repo.FleetContentHash([]string{"fleet-control-intent-v1", principal, request.IdempotencyKey})
		// Ignore paging/lookup fields: they are not invocation parameters.
		row := repo.FleetControlIntent{InvocationID: identity, Principal: principal, IdempotencyKey: request.IdempotencyKey, NodeID: request.NodeID, ControlCode: request.ControlCode, CapabilityVersion: request.CapabilityVersion, GrantID: request.GrantID, GrantRevision: request.GrantRevision, DeviceParameter: request.DeviceParameter, DeviceID: request.DeviceID, Input: request.Input, InputHash: inputHash, ExpiresAt: request.ExpiresAt, State: "pendingConfirmation", CreatedBy: subject.UserID, Actor: fleetControlActor(subject)}
		hash, err := repo.FleetContentHash(row)
		if err != nil {
			return nil, err
		}
		row.RequestHash = hash
		// A lost create response must remain queryable after the capability or TTL
		// changes; inspect only this actor's exact immutable request before I/O.
		old, err := repo.GetFleetControlIntent(ctx, identity)
		if err == nil {
			if old.RequestHash != hash {
				return nil, repo.ErrControlIdempotencyConflict
			}
			return map[string]any{"intent": old, "replayed": true}, nil
		}
		if !errors.Is(err, repo.ErrNotFound) {
			return nil, err
		}
		if err := s.requireRemoteControl(ctx, request.NodeID); err != nil {
			return nil, err
		}
		capability, err := s.controlCapability(ctx, request)
		if err != nil {
			return nil, err
		}
		if request.ExpiresAt <= time.Now().UnixMilli() {
			return nil, repo.ErrControlExpired
		}
		if request.ExpiresAt > time.Now().UnixMilli()+capability.Authorization.MaxTTLMillis || operationcontract.ValidateParameters(capability.Contract.InputSchema, request.Input) != nil || operationcontract.ValidateParameters(capability.Authorization.InputSchema, request.Input) != nil {
			return nil, ErrInvalid
		}
		replayed := false
		err = repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
			var err error
			row, replayed, err = repo.PutFleetControlIntent(ctx, tx, row)
			return err
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"intent": row, "replayed": replayed}, nil
	}
	return nil, ErrInvalid
}

func (s *Service) ReconcileFleetControlIntents(ctx context.Context) {
	if !s.CollaborationEnabled() || !license.Default().IsHost() {
		return
	}
	for range 8 {
		row, claimed, err := repo.ClaimFleetControlIntent(ctx)
		if err != nil || !claimed {
			break
		}
		s.dispatchControlIntent(ctx, row)
	}
	var pending []repo.FleetControlIntent
	if repo.GetUnsConn(ctx).Where("state IN ? AND query_after<=? AND query_count<60", []string{"dispatched", "unknown"}, time.Now().UnixMilli()).Order("query_after").Limit(8).Find(&pending).Error != nil {
		return
	}
	for _, row := range pending {
		s.queryControlIntent(ctx, row)
	}
}

func (s *Service) dispatchControlIntent(ctx context.Context, row repo.FleetControlIntent) {
	finish := func(state, reason string, raw json.RawMessage) {
		_ = repo.FinishFleetControlIntent(ctx, row.InvocationID, state, reason, raw)
	}
	var previous contextx.Subject
	if json.Unmarshal(row.Actor, &previous) != nil || s.fleetControlSubject == nil {
		finish("failed", "CONTROL_AUTHORIZATION_UNAVAILABLE", nil)
		return
	}
	subject, err := s.fleetControlSubject(ctx, previous)
	if err != nil {
		finish("failed", "CONTROL_AUTHORIZATION_UNAVAILABLE", nil)
		return
	}
	if !fleetControlAccess(subject, true) || row.Automatic && !contextx.HasResource(subject, "control.automate") {
		finish("failed", "CONTROL_AUTHORIZATION_REVOKED", nil)
		return
	}
	if row.ExpiresAt <= time.Now().UnixMilli() {
		finish("expired", "CONTROL_EXPIRED", nil)
		return
	}
	raw, err := s.callFleetControl(ctx, row.NodeID, row.ControlCode, "invoke", row.GrantID, row.GrantRevision, json.RawMessage(row.Input), map[string]string{"Idempotency-Key": row.InvocationID, "X-Control-Capability-Version": row.CapabilityVersion, "X-Control-Expires-At": strconv.FormatInt(row.ExpiresAt, 10)}, row.Automatic)
	if err != nil {
		if errors.Is(err, ErrFleetControlNotAllowed) {
			finish("failed", "EDGE_CONTROL_DISABLED", nil)
			return
		}
		// Only explicit pre-admission denials are known not to have acted. A lost
		// response, 5xx or idempotency conflict always requires original-result query.
		var peer *fleetPeerError
		if errors.As(err, &peer) {
			if peer.Status == 410 {
				finish("expired", "CONTROL_EXPIRED", nil)
				return
			}
			if peer.Status == 400 || peer.Status == 403 || peer.Status == 404 || peer.Status == 429 {
				finish("failed", "CONTROL_REJECTED", nil)
				return
			}
		}
		finish("unknown", "CONTROL_RESULT_UNKNOWN", nil)
		return
	}
	s.finishControlResult(ctx, row, raw)
}

func (s *Service) finishControlResult(ctx context.Context, row repo.FleetControlIntent, raw json.RawMessage) {
	var result struct {
		ControlCode       string `json:"controlCode"`
		CapabilityVersion string `json:"capabilityVersion"`
		Execution         struct {
			Status string `json:"status"`
		} `json:"execution"`
	}
	state := "unknown"
	if json.Unmarshal(raw, &result) == nil && result.ControlCode == row.ControlCode && result.CapabilityVersion == row.CapabilityVersion && (result.Execution.Status == "succeeded" || result.Execution.Status == "failed") {
		state = result.Execution.Status
	}
	reason := ""
	if state == "unknown" {
		reason = "CONTROL_RESULT_UNKNOWN"
	}
	_ = repo.FinishFleetControlIntent(ctx, row.InvocationID, state, reason, raw)
}

func (s *Service) queryControlIntent(ctx context.Context, row repo.FleetControlIntent) {
	// Compare the prior schedule to avoid duplicate polling by multiple workers.
	q := repo.GetUnsConn(ctx).Model(&repo.FleetControlIntent{}).Where("invocation_id=? AND query_after=? AND state IN ?", row.InvocationID, row.QueryAfter, []string{"dispatched", "unknown"}).Updates(map[string]any{"query_after": time.Now().Add(time.Minute).UnixMilli(), "query_count": gorm.Expr("query_count+1")})
	if q.Error != nil || q.RowsAffected != 1 {
		return
	}
	raw, err := s.CallFleetControl(ctx, row.NodeID, row.ControlCode, "get", "", 0, map[string]any{"idempotencyKey": row.InvocationID}, nil)
	if err != nil {
		_ = repo.FinishFleetControlIntent(ctx, row.InvocationID, "unknown", "CONTROL_RESULT_UNKNOWN", nil)
		return
	}
	s.finishControlResult(ctx, row, raw)
}
