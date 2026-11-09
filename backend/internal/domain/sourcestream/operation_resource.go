package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/common"
	"backend/internal/mcpaccess"
	"backend/internal/operationcontract"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *Service) operationAccess(ctx context.Context, permissions ...string) error {
	if isStreamRequestContext(ctx) {
		return s.streamRequestAccess(ctx, permissions...)
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok {
		return ErrForbidden
	}
	if subject.MCPRestricted {
		for _, permission := range permissions {
			allowed := mcpaccess.Has(subject, "fleet.control.write")
			if permission == "control.read" {
				allowed = allowed || mcpaccess.Has(subject, "fleet.control.read") || mcpaccess.Has(subject, "fleet.control.grant")
			}
			if !allowed {
				return ErrForbidden
			}
		}
	}
	if subject.FleetTask != nil {
		// Exact action/resource and live local authorization are checked by each
		// operation below. No role or ResourceKeys are synthesized for tasks.
		for _, permission := range permissions {
			if permission != "control.read" && permission != "control.execute" && permission != "control.write" {
				return ErrForbidden
			}
		}
		return nil
	}
	for _, permission := range permissions {
		if !contextx.HasResource(subject, permission) {
			return ErrForbidden
		}
		if subject.APIKeyUsageType == repo.APIKeyUsageAppData {
			// App 失效、撤权和缺少装配都拒绝，不能只相信签发时的 Key 范围。
			if s.controlAccess == nil {
				return ErrForbidden
			}
			if err := s.controlAccess(ctx, permission); err != nil {
				return err
			}
		}
	}
	return nil
}

// OperationResources returns public contracts, never scripts, source credentials
// or the private Stream/version binding.
func (s *Service) OperationResources(ctx context.Context, search, status string, page, size int) (map[string]any, error) {
	if err := s.operationAccess(ctx, "control.read"); err != nil {
		return nil, err
	}
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 50
	}
	if page < 1 || page > 100000 || size < 1 || size > 100 || common.ExceedDescLimit(search) || status != "" && status != "active" && status != "disabled" {
		return nil, repo.ErrInvalidArgument
	}
	if subject, _ := contextx.SubjectFrom(ctx); subject.FleetTask != nil {
		return s.taskOperationResources(ctx, search, status, page, size)
	}
	r := repo.NewSourceStreamRepo(ctx)
	actor, _ := contextx.SubjectFrom(ctx)
	rows, total, err := r.Operations(ctx, search, status, page, size, actor.APIKeyUsageType == repo.APIKeyUsageAppData)
	if err != nil {
		return nil, err
	}
	list := []map[string]any{}
	for _, row := range rows {
		_, definition, err := r.Operation(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		list = append(list, operationView(row, definition))
	}
	return map[string]any{"list": list, "total": total, "page": page, "size": size}, nil
}

func (s *Service) OperationResource(ctx context.Context, id string) (map[string]any, error) {
	if err := s.operationAccess(ctx, "control.read"); err != nil {
		return nil, err
	}
	row, definition, err := repo.NewSourceStreamRepo(ctx).Operation(ctx, id)
	if err != nil {
		return nil, err
	}
	if definition.DeclarationKind == "request" {
		if err := s.checkAppRequestRead(ctx, definition); err != nil {
			return nil, err
		}
	}
	task, err := controlTask(ctx, id, "controls.describe")
	if err != nil {
		return nil, err
	}
	if task != nil {
		if err := checkControlTask(ctx, repo.GetUnsConn(ctx), task, id, definition.CapabilityVersion(), 0, nil, false); err != nil {
			return nil, err
		}
	}
	view := operationView(row, definition)
	if task != nil {
		grant, err := repo.NewSourceStreamRepo(ctx).ControlGrant(ctx, task.NodeID, id)
		if err != nil {
			return nil, err
		}
		view["authorization"] = controlGrantView(grant)
	}
	return view, nil
}

func operationView(row repo.UnsMeta, definition repo.OperationDefinition) map[string]any {
	return map[string]any{"code": row.ID, "kind": "control", "model": "request-reply", "name": definition.Name,
		"description": definition.Description, "contractVersion": definition.ContractVersion, "status": definition.Status, "revision": row.Revision, "capabilityVersion": definition.CapabilityVersion(),
		"contract": map[string]any{"inputSchema": definition.InputSchema, "outputSchema": definition.OutputSchema,
			"sideEffect": definition.SideEffect, "idempotency": definition.Idempotency, "timeoutMs": definition.TimeoutMs}}
}

// InvokeOperationResource freezes the first resolution. Replays only read a
// completed frozen result; they never run user code or device operations again.
func (s *Service) InvokeOperationResource(ctx context.Context, id, key string, input any) (map[string]any, error) {
	return s.InvokeOperationResourceWithOptions(ctx, id, key, input, OperationInvokeOptions{})
}

func (s *Service) InvokeOperationResourceWithOptions(ctx context.Context, id, key string, input any, options OperationInvokeOptions) (map[string]any, error) {
	if err := s.operationAccess(ctx, "control.read", "control.execute"); err != nil {
		return nil, err
	}
	subject, _ := contextx.SubjectFrom(ctx)
	if subject.FleetTask != nil {
		if err := s.checkRemoteControlAccess(); err != nil {
			return nil, err
		}
	}
	if s.executor == nil {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(key) != key || len(key) > 128 || options.ExpiresAt < 0 || options.ExpiresAt > time.Now().Add(5*time.Minute).UnixMilli() || options.CapabilityVersion != "" && len(options.CapabilityVersion) != 64 {
		return nil, repo.ErrInvalidArgument
	}
	r := repo.NewSourceStreamRepo(ctx)
	suppliedKey := key != ""
	if key == "" {
		if subject.FleetTask != nil {
			return nil, repo.ErrInvalidArgument
		}
		// 无调用方幂等身份的只读请求仍有独立运行回执，不合并两次调用。
		key = uuid.NewString()
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > operationcontract.MaxValueBytes {
		return nil, ErrControlInvalidInput
	}
	digest := sha256.Sum256(raw)
	task, err := controlTask(ctx, id, "controls.invoke")
	if err != nil {
		return nil, err
	}
	claim := repo.OperationInvocationOptions{CapabilityVersion: options.CapabilityVersion, ExpiresAt: options.ExpiresAt}
	// The contract is selected under the deployment lock. Never authorize a
	// newer binding using a contract read before a concurrent publication.
	claim.Authorize = func(tx *gorm.DB, definition repo.OperationDefinition) error {
		if isStreamRequestContext(ctx) && definition.DeclarationKind != "request" {
			return repo.ErrNotFound
		}
		if definition.DeclarationKind == "request" {
			checked := repo.WithUnsTransaction(ctx, tx)
			if err := s.checkRequestBindingPublisher(checked, repo.NewSourceStreamRepo(tx), definition.Binding); err != nil {
				return err
			}
			streamID, _ := strconv.ParseInt(definition.Binding.StreamID, 10, 64)
			versionID, _ := strconv.ParseInt(definition.Binding.DeploymentVersion, 10, 64)
			version, err := repo.NewSourceStreamRepo(tx).Version(checked, streamID, versionID)
			if err != nil {
				return err
			}
			actions := []string{"stream.requests.invoke"}
			if definition.SideEffect == "write" {
				actions = append(actions, "stream.requests.write")
			}
			if err := s.checkAppRequestGrant(checked, version, definition.Binding.EntryID, actions...); err != nil {
				return err
			}
		}
		if definition.SideEffect == "write" {
			if err := s.operationAccess(ctx, "control.write"); err != nil {
				return err
			}
		}
		if !suppliedKey && definition.Idempotency == "required" {
			return repo.ErrInvalidArgument
		}
		if definition.Resource.ValidateInput(raw) != nil {
			return ErrControlInvalidInput
		}
		if task != nil {
			if err := s.checkRemoteControlAccess(); err != nil {
				return err
			}
			return checkControlTask(ctx, tx, task, id, options.CapabilityVersion, options.ExpiresAt, raw, true)
		}
		return nil
	}
	if task != nil {
		if options.ExpiresAt == 0 || options.CapabilityVersion == "" {
			return nil, repo.ErrInvalidArgument
		}
		// Live grant checks also precede idempotent replay. Revocation never
		// turns an invoke route into an unscoped result-reading capability.
		if err := checkControlTask(ctx, repo.GetUnsConn(ctx), task, id, options.CapabilityVersion, options.ExpiresAt, raw, false); err != nil {
			return nil, err
		}
		claim.Principal = task.NodeID
	}
	invocation, created, err := r.ClaimOperationInvocationWithOptions(ctx, id, key, hex.EncodeToString(digest[:]), subject.UserID, subject.APIKeyID, claim)
	if err != nil {
		if errors.Is(err, repo.ErrControlIdempotencyConflict) {
			return nil, ErrIdempotencyConflict
		}
		if errors.Is(err, repo.ErrControlInactive) {
			return nil, ErrControlInactive
		}
		return nil, err
	}
	definition, err := s.operationInvocationContract(ctx, invocation)
	if err != nil {
		return nil, err
	}
	{
		version, err := r.Version(ctx, invocation.StreamID, invocation.VersionID)
		if err != nil {
			return nil, err
		}
		ctx, err = s.withRequestPublisher(ctx, version, invocation.EntryID)
		if err != nil {
			return nil, err
		}
	}
	// Completed receipts keep their original schema and side-effect permission,
	// even when the current release has changed or the operation is inactive.
	if definition.SideEffect == "write" {
		if err := s.operationAccess(ctx, "control.write"); err != nil {
			return nil, err
		}
	}
	runID := repo.ProgramRunIdentity(invocation.MessageID, invocation.StreamID, invocation.VersionID, invocation.EntryID)
	if !created {
		return s.operationInvocationResult(ctx, invocation, true, false)
	}

	stream, err := r.Stream(ctx, invocation.StreamID)
	if err != nil {
		return nil, err
	}
	if stream.Status != "enabled" || stream.ActiveVersion != invocation.VersionID {
		return nil, ErrControlInactive
	}
	version, err := r.Version(ctx, invocation.StreamID, invocation.VersionID)
	if err != nil {
		return nil, err
	}
	if invocation.ExpiresAt != 0 && invocation.ExpiresAt <= time.Now().UnixMilli() {
		return nil, repo.ErrControlExpired
	}
	deadline := time.Now().Add(time.Duration(definition.TimeoutMs) * time.Millisecond)
	if invocation.ExpiresAt != 0 && time.UnixMilli(invocation.ExpiresAt).Before(deadline) {
		deadline = time.UnixMilli(invocation.ExpiresAt)
	}
	operationCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if task != nil {
		guard := func(callCtx context.Context) error {
			return checkControlTask(callCtx, repo.GetUnsConn(callCtx), task, id, invocation.CapabilityVersion, invocation.ExpiresAt, raw, false)
		}
		if err := guard(operationCtx); err != nil {
			return nil, err
		}
		operationCtx = context.WithValue(operationCtx, controlExecutionGuardKey{}, guard)
	}
	// The public control grant is the authorization boundary. The shared Stream
	// host still checks its legacy internal execution capability, so add only
	// that capability to an execution-local copy of the authenticated subject.
	executionSubject := subject
	executionSubject.ResourceKeys = make(map[string]struct{}, len(subject.ResourceKeys)+1)
	for key := range subject.ResourceKeys {
		executionSubject.ResourceKeys[key] = struct{}{}
	}
	executionSubject.ResourceKeys["uns.stream.execute"] = struct{}{}
	operationCtx = contextx.WithSubject(operationCtx, executionSubject)
	view, err := s.invokeProgramV2(operationCtx, stream, version, invocation.EntryID, invocation.Identity, input, definition.TimeoutMs)
	if err != nil {
		// A lost terminal or an already dispatched effect takes precedence over
		// an accompanying output error. The frozen receipt still prevents replay.
		if isStreamRequestContext(ctx) && (operationCtx.Err() != nil || errors.Is(err, ErrUnknownResult)) {
			return nil, ErrUnknownResult
		}
		if errors.Is(err, streamexec.ErrOutput) {
			return nil, ErrControlOutputInvalid
		}
		return nil, err
	}
	result, ok := view["result"].(map[string]any)
	if !ok {
		return nil, repo.ErrInvalidArgument
	}
	run, err := r.Run(ctx, runID)
	if err != nil {
		return nil, err
	}
	if err := checkRequestExecution(ctx); err != nil {
		return nil, err
	}
	return operationResult(invocation, run, result["data"], false), nil
}

func operationResult(invocation repo.OperationInvocation, run repo.StreamRun, data any, replayed bool) map[string]any {
	return map[string]any{"invocationId": invocation.Identity, "controlCode": invocation.ResourceID, "contractVersion": invocation.ContractVersion, "capabilityVersion": invocation.CapabilityVersion, "expiresAt": invocation.ExpiresAt, "businessCompletion": "unknown", "result": data,
		"execution": map[string]any{"runId": run.ID, "status": run.Status, "replayed": replayed, "usesNATS": false, "operationRepeated": false}}
}
