// Package fleetv2 adapts the same domain capabilities for Session and APIKey callers.
package fleetv2

import (
	"backend/internal/domain/cluster"
	"backend/internal/domain/fleetapi"
	domainlicense "backend/internal/domain/license"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
	"backend/internal/fleetprotocol"
	"backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/runtimeenv"
	"backend/internal/svc"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	zerohttp "github.com/zeromicro/go-zero/rest/httpx"
	"gorm.io/gorm"
	"net/http"
	"strings"
)

var ErrUnavailable = fleetapi.ErrUnavailable

var ErrInvalid = fleetapi.ErrInvalid

func Execute(ctx context.Context, svcCtx *svc.ServiceContext, op string, raw []byte) (any, error) {
	return (&fleetapi.Service{Cluster: svcCtx.App.Cluster, Uns: svcCtx.App.UnsV2, SourceStream: svcCtx.App.SourceStream}).Execute(ctx, op, raw)
}
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg := 500, "INTERNAL_ERROR", "Fleet operation failed"
	var revisionConflict *cluster.FleetRevisionConflict
	var fault *unsv2.Fault
	var databaseError *pgconn.PgError
	var agentError *cluster.FleetAgentResponseError
	var sshError *cluster.FleetSSHConnectError
	switch {
	case errors.Is(err, cluster.ErrFleetCenterType):
		status, code, msg = 422, "CENTER_TYPE_MISMATCH", err.Error()
	case errors.Is(err, cluster.ErrHostCannotConnect):
		status, code, msg = 403, "CENTER_CANNOT_CONNECT_UPSTREAM", "A center node cannot connect to or be managed by another center"
	case errors.Is(err, cluster.ErrFleetControlNotAllowed):
		status, code, msg = 403, "EDGE_CONTROL_DISABLED", "Control execution is disabled by the Edge owner"
	case errors.Is(err, cluster.ErrFleetSSHNotAllowed):
		status, code, msg = 403, "EDGE_SSH_DISABLED", "SSH is disabled by the Edge owner"
	case errors.Is(err, cluster.ErrFleetMetadataNotAllowed):
		status, code, msg = 403, "EDGE_METADATA_READ_ONLY", "Metadata changes are disabled by the Edge owner"
	case errors.As(err, &sshError):
		status, code, msg = 422, sshError.Code, "fleet.ssh.reason."+sshError.Code
	case errors.Is(err, cluster.ErrFleetSSHConnection):
		status, code, msg = 422, "SSH_CONNECTION_FAILED", "fleet.ssh.candidateFailed"
	case errors.Is(err, cluster.ErrFleetSSHNetwork):
		status, code, msg = 503, "SSH_NETWORK_UNAVAILABLE", "fleet.ssh.reason.SSH_NETWORK_UNAVAILABLE"
	case errors.Is(err, cluster.ErrFleetSSHIdentity):
		status, code, msg = 409, "SSH_HOST_KEY_CHANGED", "fleet.ssh.reason.SSH_HOST_KEY_CHANGED"
	case errors.As(err, &agentError):
		status, code, msg = 503, "FLEET_AGENT_UNAVAILABLE", "Fleet Agent or container is unavailable"
		switch agentError.StatusCode {
		case http.StatusNotImplemented:
			status, code, msg = agentError.StatusCode, agentError.Code, "This operation is not supported by the installation"
		case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict:
			status, code, msg = agentError.StatusCode, agentError.Code, "Container access was rejected; check its installation, running state and permissions"
		}
	case errors.Is(err, cluster.ErrFleetAuthorizationRevoked):
		status, code, msg = 403, "AUTHORIZATION_REVOKED", "Fleet task grant was revoked; retain pending data until explicit reauthorization"
	case errors.Is(err, unsmeta.ErrSchema), errors.Is(err, unsmeta.ErrSchemaInUse):
		status, code, msg = 409, "SCHEMA_MISMATCH", "Collection definition is incompatible with retained data"
	case errors.Is(err, unsmeta.ErrInvalid):
		status, code, msg = 400, "INVALID_REQUEST", "Invalid Collection definition"
	case errors.Is(err, repo.ErrStorageOperation):
		status, code, msg = 400, "UNSUPPORTED_OPERATION", "This operation cannot preserve the Collection storage policy"
	case errors.Is(err, repo.ErrStorageDefinitionPending):
		status, code, msg = 409, "COLLECTION_DEFINITION_PENDING", "Collection definition must synchronize with center before remote writes"
	case errors.Is(err, repo.ErrStorageUnavailable):
		status, code, msg = 503, "CENTER_UNAVAILABLE", "Center query failed; local history is not a fallback"
	case errors.Is(err, sourcestream.ErrForbidden), errors.Is(err, repo.ErrControlGrant):
		status, code, msg = 403, "FORBIDDEN", "Control authorization required"
	case errors.Is(err, repo.ErrControlCapability):
		status, code, msg = 409, "CONTROL_CAPABILITY_CHANGED", "Refresh the published Control capability before authorizing it"
	case errors.Is(err, repo.ErrControlIdempotencyConflict):
		status, code, msg = 409, "IDEMPOTENCY_CONFLICT", "The Control idempotency key already identifies a different request"
	case errors.Is(err, repo.ErrControlExpired):
		status, code, msg = 410, "CONTROL_EXPIRED", "The Control request expired before execution"
	case errors.Is(err, cluster.ErrFleetPeerUnavailable):
		status, code, msg = 503, "PEER_UNAVAILABLE", "The registered peer is unavailable; retained drafts and active snapshots are unchanged"
	case errors.Is(err, ErrUnavailable):
		status, code, msg = 503, "COLLABORATION_DISABLED", "Fleet collaboration is disabled on this installation"
	case errors.Is(err, ErrInvalid), errors.Is(err, cluster.ErrInvalid), errors.Is(err, repo.ErrInvalidArgument):
		status, code, msg = 400, "INVALID_REQUEST", "Invalid Fleet request"
	case errors.Is(err, cluster.ErrUnauthorized):
		status, code, msg = 403, "FORBIDDEN", "Fleet permission and Cluster capability required"
	case errors.As(err, &revisionConflict):
		status, code, msg = 409, "VERSION_CONFLICT", "The authoritative draft changed; refresh and merge your changes explicitly"
	case errors.Is(err, repo.ErrConflict), errors.As(err, &databaseError) && databaseError.Code == "23505":
		status, code, msg = 409, "CONFLICT", "Fleet identity or name already exists"
	case errors.Is(err, repo.ErrFleetStorageFull):
		status, code, msg = 429, "STORAGE_CAPACITY_REACHED", "Physical storage admission limit reached; preserve the batch and release capacity before retrying"
	case errors.Is(err, repo.ErrFleetStorageUnobserved):
		status, code, msg = 503, "STORAGE_MONITORING_UNAVAILABLE", "Physical storage monitoring requires valid limits and PostgreSQL monitoring permissions; preserve the batch"
	case errors.Is(err, repo.ErrFleetQueueFull):
		status, code, msg = 429, "QUEUE_FULL", "Durable queue capacity reached; preserve the batch and retry after capacity is available"
	case errors.Is(err, repo.ErrFleetNodeQuotaExceeded):
		status, code, msg = 409, "QUOTA_EXCEEDED", "Fleet node quota exceeded"
	case errors.Is(err, repo.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		status, code, msg = 404, "NOT_FOUND", "Fleet node not found"
	case errors.As(err, &fault):
		status, code, msg = 400, fault.Code, "Invalid Fleet configuration"
		if fault.Code == "FORBIDDEN" || fault.Code == "UNAUTHENTICATED" {
			status = 403
			msg = "Fleet capability not authorized"
		}
		if fault.Code == "VERSION_CONFLICT" || fault.Code == "EDGE_SOURCE_REFERENCE_REJECTED" {
			status = 409
		}
		if fault.Code == "NOT_FOUND" {
			status = 404
		}
		switch fault.Code {
		case "SOURCE_SQL_FAILED", "SOURCE_SQL_INVALID", "SOURCE_SQL_OBJECT_NOT_FOUND", "SOURCE_SQL_PERMISSION_DENIED", "SOURCE_SQL_AUTH_FAILED", "SOURCE_RESOURCE_LIMIT", "SOURCE_CREDENTIAL_UNAVAILABLE", "SOURCE_PREVIEW_NO_SAMPLE", "SOURCE_PREVIEW_WATCH_FAILED", "SOURCE_READ_TIMEOUT", "SOURCE_NODE_READ_FAILED", "SOURCE_RESPONSE_INVALID", "SOURCE_DEVICE_CONNECT_FAILED", "SOURCE_DEVICE_SESSION_FAILED":
			status, msg = 422, fault.Code
		case "SOURCE_CONNECTION_REFUSED":
			status, msg = 422, "The upstream Source connection was refused"
		case "SOURCE_HTTP_401", "SOURCE_HTTP_403", "SOURCE_HTTP_429":
			status, msg = 422, "The upstream Source rejected the resource read"
		}
		if fault.Code == "RUNTIME_OPERATION_FAILED" {
			status, msg = 422, "fleet.runtime.operationFailed"
		}
	}
	var detail any
	if errors.As(err, &revisionConflict) {
		detail = map[string]any{"currentRevision": revisionConflict.CurrentRevision}
	}
	failure := map[string]any{"code": code}
	if fault != nil && fault.Code == code {
		if diagnostic := sourcestream.SourceReadDiagnosticCode(code); diagnostic != nil {
			// 仅重建已知 Source 诊断，不透传 Fault 中的上游正文或任意字段。
			status, msg = http.StatusUnprocessableEntity, diagnostic.Message
			failure["stage"], failure["message"], failure["repair"] = diagnostic.Stage, diagnostic.Message, diagnostic.Repair
			validation := sourcestream.SourceReadDiagnostic(&sourcestream.ValidationError{Field: fault.Path, Code: fault.Reason})
			if validation != nil && validation.Code == code && validation.Path != "" {
				failure["path"], failure["reason"] = validation.Path, validation.Reason
			}
		}
	}
	if code == "INVALID_REQUEST" && fault != nil {
		// Never forward raw validator errors: they may include submitted secrets.
		msg = "Request parameters do not match the Edge operation schema"
		reason := msg
		switch fault.Reason {
		case "required field is missing", "field is not allowed for this operation", "invalid JSON field", "JSON object required", "one JSON object required":
			reason = fault.Reason
		case "field does not match the operation schema; inspect api_contract_get":
			reason = "field does not match the operation schema"
		}
		failure["stage"], failure["reason"] = "requestValidation", reason
		failure["path"], failure["field"] = fault.Path, fault.Path
		failure["repair"] = "Inspect the target Edge operation schema, correct the indicated field, and validate again with dryRun=true"
		failure["details"] = []map[string]string{{"path": fault.Path}}
	}
	zerohttp.WriteJson(w, status, map[string]any{"code": status, "msg": msg, "requestId": httpx.UnsV2RequestID(r.Context()), "data": detail, "error": failure})
}

type transportTokenKey struct{}

func TransportContext(ctx context.Context, authorization string) context.Context {
	return context.WithValue(ctx, transportTokenKey{}, strings.TrimPrefix(authorization, "Bearer "))
}
func Dispatch(ctx context.Context, svcCtx *svc.ServiceContext, handler string, raw []byte) (any, error) {
	if strings.HasPrefix(handler, "FleetNode") {
		var req struct {
			ResourceID string          `json:"resourceId"`
			Payload    json.RawMessage `json:"payload"`
		}
		if err := unsv2.Decode(raw, &req); err != nil {
			return nil, ErrInvalid
		}
		scopes := map[string]string{"FleetNodeRuntimeExecute": "runtime.execute", "FleetNodeInfrastructureSnapshot": "infrastructure.snapshot", "FleetNodeInfrastructureDetail": "infrastructure.detail", "FleetNodeInfrastructureLogs": "infrastructure.logs", "FleetNodeInfrastructureRestart": "infrastructure.restart", "FleetNodeStorageGet": "storage.get", "FleetNodeStorageSave": "storage.save", "FleetNodeStorageDefinition": "storage.definition", "FleetNodeStorageTarget": "storage.target", "FleetNodeCollectionsCatalog": "collections.catalog", "FleetNodeUsersApply": "users.apply", "FleetNodeCapabilitiesGet": "capabilities.get"}
		scopes["FleetNodeInfrastructureRuntime"] = "infrastructure.runtime"
		scope, ok := scopes[handler]
		if !ok {
			return nil, ErrInvalid
		}
		if scope == "infrastructure.runtime" {
			var runtimeRequest fleetprotocol.RuntimeRequest
			if unsv2.Decode(req.Payload, &runtimeRequest) != nil {
				return nil, ErrInvalid
			}
			scope += "." + runtimeRequest.Action
			if runtimeRequest.Action == "control" {
				scope += "." + runtimeRequest.Verb
			}
		}
		token, _ := ctx.Value(transportTokenKey{}).(string)
		claims, err := svcCtx.App.Cluster.VerifyFleetServiceToken(ctx, token, scope, req.ResourceID)
		if err != nil {
			return nil, err
		}
		if scope == "runtime.execute" {
			var packet unsv2.FleetRuntimePacket
			if unsv2.Decode(req.Payload, &packet) != nil {
				return nil, ErrInvalid
			}
			if err := svcCtx.App.Cluster.CheckFleetMetadataAccess(packet.Operation); err != nil {
				return nil, err
			}
			return svcCtx.App.UnsV2.ExecuteFleetRuntime(ctx, claims.Subject, req.ResourceID, packet)
		}
		if strings.HasPrefix(scope, "infrastructure.") {
			return svcCtx.App.Cluster.FleetInfrastructure(ctx, scope, req.ResourceID, req.Payload)
		}
		if scope == "storage.get" || scope == "storage.save" {
			var payload cluster.FleetStorageRequest
			if unsv2.Decode(req.Payload, &payload) != nil || payload.NodeID != "" || req.ResourceID != "node" {
				return nil, ErrInvalid
			}
			return svcCtx.App.Cluster.FleetStorage(ctx, strings.TrimPrefix(scope, "storage."), payload)
		}
		if scope == "storage.definition" {
			var payload cluster.FleetStorageDefinition
			if unsv2.Decode(req.Payload, &payload) != nil {
				return nil, ErrInvalid
			}
			return svcCtx.App.Cluster.AcceptStorageDefinition(ctx, claims, payload)
		}
		if scope == "storage.target" {
			var payload cluster.FleetStorageTargetRequest
			if unsv2.Decode(req.Payload, &payload) != nil {
				return nil, ErrInvalid
			}
			return svcCtx.App.Cluster.AcceptCollectionStorage(ctx, claims, payload, svcCtx.App.UnsV2)
		}
		if scope == "collections.catalog" {
			if req.ResourceID != "node" {
				return nil, cluster.ErrUnauthorized
			}
			var payload struct {
				Page int `json:"page"`
			}
			if unsv2.Decode(req.Payload, &payload) != nil {
				return nil, ErrInvalid
			}
			return svcCtx.App.Cluster.FleetCollectionCatalog(ctx, payload.Page)
		}

		if scope == "users.apply" {
			var payload cluster.FleetUserSnapshotRequest
			if req.ResourceID != "node" || unsv2.Decode(req.Payload, &payload) != nil {
				return nil, ErrInvalid
			}
			return svcCtx.App.Cluster.ApplyFleetUserSnapshot(ctx, claims, payload)
		}
		if handler == "FleetNodeCapabilitiesGet" {
			access, err := svcCtx.App.Cluster.FleetAccess()
			if err != nil {
				return nil, err
			}
			identity := runtimeenv.Identity()
			return map[string]any{"protocolVersion": 1, "businessStatus": "available", "access": access, "runtimeMode": identity.Deployment, "managedRuntime": true, "edition": identity.Edition, "platform": identity.Platform, "deployment": identity.Deployment, "capabilities": domainlicense.Default().Capabilities()}, nil
		}
		return nil, ErrUnavailable
	}

	return Execute(ctx, svcCtx, OperationName(handler), raw)
}
