package sourcestream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"backend/internal/contextx"
	"backend/internal/operationcontract"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"gorm.io/gorm"
)

// Optional for legacy local callers; mandatory for remotely authorized tasks.
// Expiry is an absolute UTC millisecond deadline, never refreshed on retries.
type OperationInvokeOptions struct {
	CapabilityVersion string
	ExpiresAt         int64
}

// Query reads only the caller's original reservation. No resolution against a
// newly deployed capability and no execution, retry or device readback occurs.
func (s *Service) OperationInvocation(ctx context.Context, id, key string) (map[string]any, error) {
	if err := s.operationAccess(ctx, "control.read"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(key) != key {
		return nil, repo.ErrInvalidArgument
	}
	subject, _ := contextx.SubjectFrom(ctx)
	task, err := controlTask(ctx, id, "controls.get")
	if err != nil {
		return nil, err
	}
	principal := ""
	if task != nil {
		principal = task.NodeID
	}
	invocation, err := repo.NewSourceStreamRepo(ctx).OperationInvocation(ctx, id, key, subject.UserID, subject.APIKeyID, principal)
	if err != nil {
		return nil, err
	}
	version, err := repo.NewSourceStreamRepo(ctx).Version(ctx, invocation.StreamID, invocation.VersionID)
	if err != nil {
		return nil, err
	}
	ctx, err = s.withRequestPublisher(ctx, version, invocation.EntryID)
	if err != nil {
		return nil, err
	}
	return s.operationInvocationResult(ctx, invocation, true, true)
}

func (s *Service) operationInvocationResult(ctx context.Context, invocation repo.OperationInvocation, replayed, query bool) (map[string]any, error) {
	if err := checkRequestExecution(ctx); err != nil {
		return nil, err
	}
	r := repo.NewSourceStreamRepo(ctx)
	runID := repo.ProgramRunIdentity(invocation.MessageID, invocation.StreamID, invocation.VersionID, invocation.EntryID)
	task, err := r.Task(ctx, runID)
	if err != nil && !errors.Is(err, repo.ErrNotFound) && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err != nil || task.Status != "succeeded" {
		if !query {
			return nil, ErrUnknownResult
		}
		// Admission without a completed result may already have operated a device.
		// Only the existing executor's durable terminal failure is a known failure.
		status := "unknown"
		run, runErr := r.Run(ctx, runID)
		if runErr != nil && !errors.Is(runErr, repo.ErrNotFound) && !errors.Is(runErr, gorm.ErrRecordNotFound) {
			return nil, runErr
		}
		if runErr == nil && run.Status == "failed" {
			status = "failed"
		}
		return map[string]any{"invocationId": invocation.Identity, "controlCode": invocation.ResourceID, "contractVersion": invocation.ContractVersion, "capabilityVersion": invocation.CapabilityVersion, "expiresAt": invocation.ExpiresAt, "businessCompletion": "unknown", "resultAvailable": false,
			"execution": map[string]any{"runId": runID, "status": status, "replayed": true, "usesNATS": false, "operationRepeated": false}}, nil
	}
	if task.StreamID != invocation.StreamID || task.VersionID != invocation.VersionID || task.EntryID != invocation.EntryID {
		return nil, repo.ErrConflict
	}
	contract, err := s.operationInvocationContract(ctx, invocation)
	if err != nil {
		return nil, err
	}
	var frozen streamexec.ProgramResult
	if json.Unmarshal([]byte(task.Output), &frozen) != nil || contract.ValidateOutput(frozen.Data) != nil {
		return nil, ErrControlOutputInvalid
	}
	var data any
	decoder := json.NewDecoder(bytes.NewReader(frozen.Data))
	decoder.UseNumber()
	if decoder.Decode(&data) != nil {
		return nil, repo.ErrInvalidArgument
	}
	run, err := r.Run(ctx, runID)
	if err != nil {
		return nil, err
	}
	view := operationResult(invocation, run, data, replayed)
	if err := checkRequestExecution(ctx); err != nil {
		return nil, err
	}
	view["resultAvailable"] = true
	return view, nil
}

func (s *Service) operationInvocationContract(ctx context.Context, invocation repo.OperationInvocation) (operationcontract.Resource, error) {
	var contract operationcontract.Resource
	if json.Unmarshal([]byte(invocation.Contract), &contract) != nil || contract.Code == "" {
		// Pre-migration reservations already pinned an immutable program version.
		version, err := repo.NewSourceStreamRepo(ctx).Version(ctx, invocation.StreamID, invocation.VersionID)
		if err != nil {
			return contract, err
		}
		manifest, err := version.ProgramV2Manifest()
		if err != nil {
			return contract, err
		}
		for _, entry := range manifest.Entries {
			if entry.ID == invocation.EntryID && entry.Operation() != nil {
				contract = *entry.Operation()
				break
			}
		}
	}
	if contract.Code != invocation.ResourceID || contract.ContractVersion != invocation.ContractVersion || contract.ValidateStored() != nil {
		return contract, repo.ErrConflict
	}
	return contract, nil
}
