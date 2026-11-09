package platform

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"backend/internal/contextx"
	domainlicense "backend/internal/domain/license"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsv2"
	"backend/internal/repo"
	"backend/internal/runtimeenv"
	"backend/internal/svc"
)

type readinessAssessment struct {
	ReleaseEnabled bool   `json:"releaseEnabled"`
	Enabled        bool   `json:"enabled"`
	Allowed        bool   `json:"allowed"`
	State          string `json:"state"`
	Availability   string `json:"availability"`
	ReasonCode     string `json:"reasonCode,omitempty"`
	CheckedAt      string `json:"checkedAt,omitempty"`
	ValidUntil     string `json:"validUntil,omitempty"`
}
type publicOperation struct {
	readinessAssessment
	Operation             string `json:"operation"`
	RequiresResourceCheck bool   `json:"requiresResourceCheck"`
}
type publicCapability struct {
	readinessAssessment
	Code             string            `json:"code"`
	ContractVersions []string          `json:"contractVersions"`
	Operations       []publicOperation `json:"operations,omitempty"`
}
type capabilityDiscovery struct {
	Product             string             `json:"product"`
	ProductVersion      string             `json:"productVersion,omitempty"`
	ContractVersion     string             `json:"contractVersion"`
	APIContractVersions []string           `json:"apiContractVersions"`
	SDKContractVersions []string           `json:"sdkContractVersions"`
	Capabilities        []publicCapability `json:"capabilities"`
	Deprecations        []any              `json:"deprecations"`
}

func assessment(state, reason string) readinessAssessment {
	availability := "unavailable"
	if state == "ready" {
		availability = "available"
	}
	if state == "readiness_unknown" {
		availability = "unknown"
	}
	return readinessAssessment{ReleaseEnabled: state != "disabled", Enabled: state == "ready", Allowed: true, State: state, Availability: availability, ReasonCode: reason}
}

type discoveryOperation struct {
	name     string
	routes   []string
	resource bool
}

var sourceDiscoveryOperations = []discoveryOperation{
	{"read", []string{"sourceDocumentGet", "sourceDocumentList"}, false},
	{"configure", []string{"sourceDocumentCreate", "sourceDocumentUpdate"}, false},
	{"test", []string{"sourceProbe"}, true},
	{"discover", []string{"sourceDiscover"}, true},
	{"enable", []string{"sourceDocumentStateUpdate"}, true},
	{"disable", []string{"sourceDocumentStateUpdate"}, false},
}
var streamDiscoveryOperations = []discoveryOperation{
	{"read", []string{"streamDocumentGet", "streamProgramList"}, false},
	{"configure", []string{"streamDocumentCreate", "streamDocumentUpdate"}, false},
	{"validate", []string{"streamDocumentValidate"}, false},
	{"deploy", []string{"streamProgramDeploy"}, true},
	{"execute", []string{"streamProgramInvoke"}, true},
	{"disable", []string{"streamDocumentDisable"}, false},
}

func discoveryOperationAllowed(ctx context.Context, op discoveryOperation) bool {
	for _, name := range op.routes {
		registered, ok := unsv2.FindResourceOperation(name)
		if !ok || !unsv2.ResourceAllowed(ctx, registered) || !licenseAllows(registered.Permission) {
			return false
		}
		// The disable handler additionally requires deployment authorization after
		// its registered manage check. Discovery must honor both prerequisites.
		if name == "streamDocumentDisable" {
			subject, ok := contextx.SubjectFrom(ctx)
			if !ok || !contextx.HasResource(subject, "uns.stream.deploy") || !licenseAllows("uns.stream.deploy") {
				return false
			}
		}
	}
	return true
}
func discoveryOperationRegistered(routes map[string]bool, op discoveryOperation) bool {
	for _, name := range op.routes {
		registered, ok := unsv2.FindResourceOperation(name)
		if !ok || !routes["POST /openapi/v2"+registered.Suffix] {
			return false
		}
	}
	return true
}
func licenseAllows(key string) bool {
	license := domainlicense.Default()
	return license.AllowResource(key)
}

func discoverCapabilities(ctx context.Context, service *svc.ServiceContext, raw []byte) (any, error) {
	var input map[string]json.RawMessage
	if len(raw) > 4096 || decode(raw, &input) != nil || input == nil || len(input) != 0 {
		return nil, ErrDiscoveryRequest
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.UserID <= 0 || subject.FleetTask != nil || (subject.AuthType != "session" && subject.AuthType != "oauth" && subject.AuthType != "apiKey") || (subject.AuthType == "apiKey" && subject.APIKeyID <= 0) {
		return nil, &apiError{401, "UNAUTHENTICATED", "An authenticated identity is required", ""}
	}
	if subject.APIKeyAppID != "" && subject.APIKeyUsageType != repo.APIKeyUsageAppData {
		return nil, forbidden()
	}
	namespace, appKey, err := unsv2.DiscoveryAccess(ctx)
	if err != nil {
		return nil, err
	}
	if service == nil || service.App == nil || service.App.Store == nil || service.App.Store.CommonDB() == nil || len(service.PublicRoutes) == 0 {
		return nil, &apiError{503, "DISCOVERY_UNAVAILABLE", "No trustworthy capability publication is available", ""}
	}
	checkCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	db, err := service.App.Store.CommonDB().DB()
	if err != nil || db.PingContext(checkCtx) != nil {
		return nil, &apiError{503, "DISCOVERY_UNAVAILABLE", "Capability publication is unavailable", ""}
	}
	schemas := service.App.Store.CapabilitySchemas(checkCtx)
	return projectCapabilities(ctx, service, namespace, appKey, schemas, service.App.SourceStream.CapabilityReadiness(), time.Now()), nil
}

// Publication and per-operation authorization are separate from resource execution.
func projectCapabilities(ctx context.Context, service *svc.ServiceContext, namespace, appKey bool, schemas map[string]bool, ready sourcestream.CapabilityReadiness, now time.Time) capabilityDiscovery {
	result := capabilityDiscovery{Product: runtimeenv.Edition(), ProductVersion: service.Config.ProductVersion, ContractVersion: "1.0", APIContractVersions: []string{"openapi/v2"}, SDKContractVersions: []string{}, Capabilities: []publicCapability{}, Deprecations: []any{}}
	for route := range service.PublicRoutes {
		if strings.HasPrefix(route, "POST /openapi/v1/") {
			result.APIContractVersions = append(result.APIContractVersions, "openapi/v1")
			break
		}
	}
	if namespace && licenseAllows("uns.read") {
		a := assessment("ready", "READY")
		if !schemas["namespace.v2"] {
			a = assessment("unavailable", "SCHEMA_NOT_READY")
		}
		if !service.PublicRoutes["POST /openapi/v2/uns/discovery/search"] {
			a = assessment("not_supported", "ROUTE_NOT_REGISTERED")
		}
		result.Capabilities = append(result.Capabilities, publicCapability{readinessAssessment: a, Code: "namespace.v2", ContractVersions: []string{"openapi/v2"}})
	}
	if appKey {
		return result
	}
	for _, spec := range []struct {
		code string
		ops  []discoveryOperation
	}{{"source", sourceDiscoveryOperations}, {"stream", streamDiscoveryOperations}} {
		ops := []publicOperation{}
		for _, op := range spec.ops {
			if !discoveryOperationAllowed(ctx, op) {
				continue
			}
			a := operationAssessment(spec.code, op, ready, now)
			if !schemas[spec.code] {
				a = assessment("unavailable", "SCHEMA_NOT_READY")
			}
			if !discoveryOperationRegistered(service.PublicRoutes, op) {
				a = assessment("not_supported", "ROUTE_NOT_REGISTERED")
			}
			if service.App.SourceStream == nil {
				a = assessment("disabled", "CAPABILITY_DISABLED")
			}
			ops = append(ops, publicOperation{readinessAssessment: a, Operation: op.name, RequiresResourceCheck: op.resource})
		}
		if len(ops) == 0 {
			continue
		}
		// Aggregate reports runtime preconditions even for a metadata-only principal.
		a := runtimeAssessment(spec.code, ready, now)
		if !schemas[spec.code] {
			a = assessment("unavailable", "SCHEMA_NOT_READY")
		}
		if service.App.SourceStream == nil {
			a = assessment("disabled", "CAPABILITY_DISABLED")
		}
		result.Capabilities = append(result.Capabilities, publicCapability{readinessAssessment: a, Code: spec.code, ContractVersions: []string{"openapi/v2"}, Operations: ops})
	}
	return result
}

func operationAssessment(code string, op discoveryOperation, r sourcestream.CapabilityReadiness, now time.Time) readinessAssessment {
	if op.name == "read" || op.name == "disable" {
		return assessment("ready", "READY")
	}
	if code == "source" {
		if op.name != "configure" && !r.Credentials {
			return assessment("unavailable", "CREDENTIAL_SERVICE_UNAVAILABLE")
		}
		// Protocol/listener/connection checks remain on the authorized target endpoint.
		if op.name != "configure" && !r.ProtocolSupported {
			return assessment("readiness_unknown", "READINESS_NOT_SUPPORTED")
		}
		return assessment("ready", "READY")
	}
	// Saving the current program performs CheckOnly registration in the executor;
	// it does not require an async worker, a protocol host or an execution owner.
	a := executorAssessment(r, now)
	if (op.name == "execute" || op.name == "deploy") && !r.ProgramExecution {
		return assessment("unavailable", "RUNTIME_UNREACHABLE")
	}
	return a
}

func executorAssessment(r sourcestream.CapabilityReadiness, now time.Time) readinessAssessment {
	if !r.ExecutorSupported || r.CheckedAt.IsZero() {
		return assessment("readiness_unknown", "READINESS_NOT_SUPPORTED")
	}
	a := assessment("ready", "READY")
	if r.CheckedAt.After(now) || !r.ValidUntil.After(r.CheckedAt) {
		a = assessment("readiness_unknown", "DEPENDENCIES_NOT_READY")
	} else if !now.Before(r.ValidUntil) {
		a = assessment("unavailable", "READINESS_EXPIRED")
	} else if !r.ExecutorReady {
		a = assessment("unavailable", "RUNTIME_UNREACHABLE")
	}
	if !r.CheckedAt.After(now) {
		a.CheckedAt = r.CheckedAt.UTC().Format(time.RFC3339Nano)
	}
	if a.Enabled {
		a.ValidUntil = r.ValidUntil.UTC().Format(time.RFC3339Nano)
	}
	return a
}

func runtimeAssessment(code string, r sourcestream.CapabilityReadiness, now time.Time) readinessAssessment {
	if code == "source" {
		if !r.Credentials {
			return assessment("unavailable", "CREDENTIAL_SERVICE_UNAVAILABLE")
		}
		if !r.AsyncWorker {
			return assessment("unavailable", "WORKER_NOT_REGISTERED")
		}
		if !r.ProtocolSupported || r.SourceCheckedAt.IsZero() {
			return assessment("readiness_unknown", "READINESS_NOT_SUPPORTED")
		}
		// Reuse the same strict evidence projection, without conflating a worker
		// observation with a successful Source connection or resource execution.
		return executorAssessment(sourcestream.CapabilityReadiness{ExecutorSupported: true, ExecutorReady: r.SourceReady, CheckedAt: r.SourceCheckedAt, ValidUntil: r.SourceValidUntil}, now)

	}
	a := executorAssessment(r, now)
	if !r.ProgramExecution {
		return assessment("unavailable", "RUNTIME_UNREACHABLE")
	}
	if !r.AsyncWorker {
		return assessment("unavailable", "WORKER_NOT_REGISTERED")
	}
	return a
}
