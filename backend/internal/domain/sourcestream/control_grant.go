package sourcestream

import (
	"context"
	"encoding/json"
	"strings"

	"backend/internal/contextx"
	"backend/internal/mcpaccess"
	"backend/internal/operationcontract"
	"backend/internal/repo"
	"backend/internal/unsref"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ControlGrantRequest struct {
	AllowAutomatic    bool            `json:"allowAutomatic"`
	CenterID          string          `json:"centerId"`
	ControlCode       string          `json:"controlCode"`
	ExpectedRevision  int64           `json:"expectedRevision"`
	CapabilityVersion string          `json:"capabilityVersion"`
	InputSchema       json.RawMessage `json:"inputSchema"`
	MaxTTLMillis      int64           `json:"maxTtlMs"`
	MinIntervalMillis int64           `json:"minIntervalMs"`
	Enabled           bool            `json:"enabled"`
}

func controlGrantView(grant repo.ControlGrant) map[string]any {
	return map[string]any{"centerId": strings.TrimPrefix(grant.Principal, "center:"), "controlCode": grant.ControlCode, "grantId": grant.GrantID, "revision": grant.Revision, "capabilityVersion": grant.CapabilityVersion,
		"allowAutomatic": grant.AllowAutomatic, "inputSchema": json.RawMessage(grant.InputSchema), "maxTtlMs": grant.MaxTTLMillis, "minIntervalMs": grant.MinIntervalMillis, "enabled": grant.Enabled, "lastInvokedAt": grant.LastInvokedAt, "updatedAt": grant.UpdatedAt}
}

func (s *Service) taskOperationResources(ctx context.Context, search, status string, page, size int) (map[string]any, error) {
	task, err := controlTask(ctx, "node", "controls.discover")
	if err != nil || task == nil {
		return nil, ErrForbidden
	}
	query := repo.GetUnsConn(ctx).Model(&repo.ControlGrant{}).Where("principal=? AND enabled=true", task.NodeID)
	if search != "" {
		query = query.Where("control_code ILIKE ?", "%"+search+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var grants []repo.ControlGrant
	if err := query.Order("control_code").Offset((page - 1) * size).Limit(size).Find(&grants).Error; err != nil {
		return nil, err
	}
	list := []map[string]any{}
	for _, grant := range grants {
		row, definition, err := repo.NewSourceStreamRepo(ctx).Operation(ctx, grant.ControlCode)
		if err != nil {
			return nil, err
		}
		if definition.CapabilityVersion() != grant.CapabilityVersion || definition.Status != "active" || status != "" && status != definition.Status {
			continue
		}
		view := operationView(row, definition)
		view["authorization"] = controlGrantView(grant)
		list = append(list, view)
	}
	return map[string]any{"list": list, "total": total, "page": page, "size": size}, nil
}

// Only a local user can authorize a pinned-center principal. A remote task has
// no user ID and can neither grant itself a capability nor change restrictions.
func (s *Service) ControlGrant(ctx context.Context, request ControlGrantRequest, save bool) (map[string]any, error) {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.UserID <= 0 || subject.FleetTask != nil || subject.APIKeyAppID != "" || !contextx.HasResource(subject, "fleet.connection.manage") {
		return nil, ErrForbidden
	}
	if subject.MCPRestricted && !mcpaccess.Has(subject, "fleet.control.grant") {
		return nil, ErrForbidden
	}
	if !contextx.HasResource(subject, "control.read") || !contextx.HasResource(subject, "control.write") || !contextx.HasResource(subject, "control.manage") {
		return nil, ErrForbidden
	}
	if !unsref.ValidMetadataID(request.CenterID) || !unsref.ValidMetadataID(request.ControlCode) || request.ExpectedRevision < 0 {
		return nil, repo.ErrInvalidArgument
	}
	principal := "center:" + request.CenterID
	r := repo.NewSourceStreamRepo(ctx)
	if !save {
		row, err := r.ControlGrant(ctx, principal, request.ControlCode)
		if err != nil {
			return nil, err
		}
		return controlGrantView(row), nil
	}
	if request.AllowAutomatic && !contextx.HasResource(subject, "control.automate") {
		return nil, ErrForbidden
	}
	if len(request.CapabilityVersion) != 64 || operationcontract.ValidateParameterSchema(request.InputSchema) != nil || request.MaxTTLMillis < 1000 || request.MaxTTLMillis > 300000 || request.MinIntervalMillis < 1000 || request.MinIntervalMillis > 86400000 {
		return nil, repo.ErrInvalidArgument
	}
	row, err := r.SaveControlGrant(ctx, repo.ControlGrant{AllowAutomatic: request.AllowAutomatic, Principal: principal, ControlCode: request.ControlCode, GrantID: uuid.NewString(), CapabilityVersion: request.CapabilityVersion, InputSchema: string(request.InputSchema), MaxTTLMillis: request.MaxTTLMillis, MinIntervalMillis: request.MinIntervalMillis, Enabled: request.Enabled, UpdatedBy: subject.UserID}, request.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	return controlGrantView(row), nil
}

func controlTask(ctx context.Context, id, scope string) (*contextx.FleetTaskSubject, error) {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok {
		return nil, ErrForbidden
	}
	task := subject.FleetTask
	if task == nil {
		return nil, nil
	}
	if subject.UserID != 0 || subject.APIKeyID != 0 || !strings.HasPrefix(task.NodeID, "center:") || task.ResourceID != id || task.Scope != scope {
		return nil, ErrForbidden
	}
	return task, nil
}

func checkControlTask(ctx context.Context, tx *gorm.DB, task *contextx.FleetTaskSubject, id, capability string, expires int64, raw json.RawMessage, admit bool) error {
	grant, err := repo.NewSourceStreamRepo(tx).CheckControlGrant(ctx, task.NodeID, id, task.GrantID, task.GrantRevision, capability, expires, admit)
	if err != nil {
		return err
	}
	if task.Automatic && !grant.AllowAutomatic {
		return repo.ErrControlGrant
	}
	if raw != nil && operationcontract.ValidateParameters(json.RawMessage(grant.InputSchema), raw) != nil {
		return ErrControlInvalidInput
	}
	return nil
}

type controlExecutionGuardKey struct{}

func checkControlExecution(ctx context.Context) error {
	if err := checkRequestExecution(ctx); err != nil {
		return err
	}
	guard, _ := ctx.Value(controlExecutionGuardKey{}).(func(context.Context) error)
	if guard != nil {
		return guard(ctx)
	}
	return nil
}
