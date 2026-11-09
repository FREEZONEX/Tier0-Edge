package platform

// 合同降级、权限过滤、真实注册和 nil 服务的纯投影回归，不连接设备或数据库。
import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"backend/internal/bootstrap"
	"backend/internal/contextx"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsv2"
	"backend/internal/repo"
	"backend/internal/svc"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
)

func discoveryFixture() (*svc.ServiceContext, context.Context, map[string]bool, sourcestream.CapabilityReadiness) {
	routes := map[string]bool{"POST /openapi/v2/uns/discovery/search": true}
	for _, spec := range [][]discoveryOperation{sourceDiscoveryOperations, streamDiscoveryOperations} {
		for _, op := range spec {
			for _, name := range op.routes {
				registered, _ := unsv2.FindResourceOperation(name)
				routes["POST /openapi/v2"+registered.Suffix] = true
			}
		}
	}
	keys := map[string]struct{}{}
	for _, key := range []string{"uns.read", "uns.source", "uns.source.manage", "uns.stream", "uns.stream.manage", "uns.stream.deploy", "uns.stream.execute"} {
		keys[key] = struct{}{}
	}
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: keys})
	now := time.Now()
	return &svc.ServiceContext{PublicRoutes: routes, App: &bootstrap.App{SourceStream: sourcestream.New(nil, nil, "")}}, ctx, map[string]bool{"namespace.v2": true, "source": true, "stream": true}, sourcestream.CapabilityReadiness{SourceReady: true, SourceCheckedAt: now.Add(-time.Second), SourceValidUntil: now.Add(time.Second), Credentials: true, ProtocolSupported: true, ProgramExecution: true, AsyncWorker: true, ExecutorSupported: true, ExecutorReady: true, CheckedAt: now.Add(-time.Second), ValidUntil: now.Add(time.Second)}
}
func TestDiscoveryStateAndExpiry(t *testing.T) {
	now := time.Now()
	for _, state := range []string{"disabled", "not_supported", "unavailable", "readiness_unknown", "ready"} {
		a := assessment(state, "DEPENDENCIES_NOT_READY")
		require.Equal(t, state == "ready", a.Enabled)
		require.Equal(t, state != "disabled", a.ReleaseEnabled)
	}
	_, _, _, r := discoveryFixture()
	for _, test := range []struct {
		name   string
		change func(*sourcestream.CapabilityReadiness)
		reason string
	}{
		{"expired", func(r *sourcestream.CapabilityReadiness) { r.ValidUntil = now.Add(-time.Millisecond) }, "READINESS_EXPIRED"},
		{"failed", func(r *sourcestream.CapabilityReadiness) { r.ExecutorReady = false }, "RUNTIME_UNREACHABLE"},
		{"unsupported", func(r *sourcestream.CapabilityReadiness) { r.ExecutorSupported = false }, "READINESS_NOT_SUPPORTED"},
		{"future", func(r *sourcestream.CapabilityReadiness) { r.CheckedAt = now.Add(time.Second) }, "DEPENDENCIES_NOT_READY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := r
			test.change(&copy)
			a := executorAssessment(copy, now)
			require.False(t, a.Enabled)
			require.Empty(t, a.ValidUntil)
			require.Equal(t, test.reason, a.ReasonCode)
		})
	}
	a := executorAssessment(r, now)
	require.True(t, a.Enabled)
	require.Equal(t, r.ValidUntil.UTC().Format(time.RFC3339Nano), a.ValidUntil)
	r.ProgramExecution = false
	require.False(t, operationAssessment("stream", streamDiscoveryOperations[3], r, now).Enabled)
	require.True(t, operationAssessment("stream", streamDiscoveryOperations[1], r, now).Enabled, "registration does not require execution owner")
}
func TestDiscoveryRuntimeIsNotMetadata(t *testing.T) {
	service, ctx, schemas, r := discoveryFixture()
	r.AsyncWorker = false
	result := projectCapabilities(ctx, service, true, false, schemas, r, time.Now())
	for _, cap := range result.Capabilities {
		if cap.Code == "namespace.v2" {
			continue
		}
		require.False(t, cap.Enabled)
		require.Equal(t, "WORKER_NOT_REGISTERED", cap.ReasonCode)
		for _, op := range cap.Operations {
			if op.Operation == "read" || op.Operation == "disable" {
				require.True(t, op.Enabled)
			}
			if op.Operation == "test" || op.Operation == "discover" || op.Operation == "enable" || op.Operation == "execute" || op.Operation == "deploy" {
				require.True(t, op.RequiresResourceCheck)
				require.True(t, op.Enabled, "target synchronous checks must not require NATS")
			}
		}
	}
	r.AsyncWorker = true
	r.Credentials = false
	result = projectCapabilities(ctx, service, true, false, schemas, r, time.Now())
	require.Equal(t, "CREDENTIAL_SERVICE_UNAVAILABLE", result.Capabilities[1].ReasonCode)
	require.True(t, result.Capabilities[1].Operations[1].Enabled)
}
func TestDiscoveryPermissionsSchemaRoutesAndNil(t *testing.T) {
	service, ctx, schemas, r := discoveryFixture()
	delete(service.PublicRoutes, "POST /openapi/v2/uns/streams/program/deploy")
	result := projectCapabilities(ctx, service, true, false, schemas, r, time.Now())
	require.Equal(t, "ready", result.Capabilities[2].State)
	require.Equal(t, "not_supported", result.Capabilities[2].Operations[3].State)
	require.Empty(t, result.Capabilities[2].Operations[3].ValidUntil)
	schemas["source"] = false
	result = projectCapabilities(ctx, service, true, false, schemas, r, time.Now())
	require.Equal(t, "SCHEMA_NOT_READY", result.Capabilities[1].ReasonCode)
	require.False(t, result.Capabilities[1].Operations[0].Enabled)
	service.App.SourceStream = nil
	schemas["source"] = true
	result = projectCapabilities(ctx, service, true, false, schemas, r, time.Now())
	require.Equal(t, "CAPABILITY_DISABLED", result.Capabilities[1].ReasonCode)
	service, ctx, schemas, r = discoveryFixture()
	subject, _ := contextx.SubjectFrom(ctx)
	subject.AuthType = "apiKey"
	subject.APIKeyPermission = "read_only"
	subject.APIKeyID = 1
	ctx = contextx.WithSubject(ctx, subject)
	result = projectCapabilities(ctx, service, true, false, schemas, r, time.Now())
	require.Len(t, result.Capabilities[1].Operations, 1)
	require.Equal(t, "read", result.Capabilities[1].Operations[0].Operation)
	// Validate uses the actual non-mutation permission, unlike configure/deploy.
	require.Len(t, result.Capabilities[2].Operations, 2)
	subject.MCPRestricted = true
	subject.MCPScopes = nil
	subject.APIKeyPermission = "full_access"
	result = projectCapabilities(contextx.WithSubject(ctx, subject), service, true, false, schemas, r, time.Now())
	require.Len(t, result.Capabilities[1].Operations, 1)
	subject.APIKeyUsageType = repo.APIKeyUsageAppData
	result = projectCapabilities(contextx.WithSubject(ctx, subject), service, true, true, schemas, r, time.Now())
	require.Len(t, result.Capabilities, 1)
	require.Equal(t, "namespace.v2", result.Capabilities[0].Code)
}
func TestDiscoveryRequestAndIdentityFailClosed(t *testing.T) {
	service, ctx, _, _ := discoveryFixture()
	for _, raw := range []string{"null", "[]", "{}{}", `{"scope":"all"}`, "", string(make([]byte, 4097))} {
		_, err := discoverCapabilities(ctx, service, []byte(raw))
		require.Error(t, err)
	}
	for _, subject := range []contextx.Subject{{UserID: 1, AuthType: "sessionAPIKey"}, {UserID: 1, AuthType: "apiKey"}, {UserID: 1, AuthType: "session", FleetTask: &contextx.FleetTaskSubject{}}, {UserID: 1, AuthType: "session", APIKeyAppID: "legacy"}} {
		_, err := discoverCapabilities(contextx.WithSubject(context.Background(), subject), service, []byte("{}"))
		require.Error(t, err)
	}
	_, err := discoverCapabilities(ctx, &svc.ServiceContext{}, []byte("{}"))
	require.Error(t, err)
	b, err := json.Marshal(projectCapabilities(ctx, service, true, false, map[string]bool{}, sourcestream.CapabilityReadiness{}, time.Now()))
	require.NoError(t, err)
	require.NotContains(t, string(b), "validUntil")
}

func TestDiscoveryStreamDisableRequiresManageAndDeploy(t *testing.T) {
	for _, auth := range []string{"session", "apiKey"} {
		for _, manage := range []bool{false, true} {
			for _, deploy := range []bool{false, true} {
				service, ctx, schemas, r := discoveryFixture()
				subject, _ := contextx.SubjectFrom(ctx)
				subject.AuthType, subject.APIKeyID, subject.APIKeyPermission = auth, 1, "full_access"
				if !manage {
					delete(subject.ResourceKeys, "uns.stream.manage")
				}
				if !deploy {
					delete(subject.ResourceKeys, "uns.stream.deploy")
				}
				result := projectCapabilities(contextx.WithSubject(ctx, subject), service, true, false, schemas, r, time.Now())
				found := false
				for _, capability := range result.Capabilities {
					if capability.Code != "stream" {
						continue
					}
					for _, operation := range capability.Operations {
						if operation.Operation == "disable" {
							found = true
							require.True(t, operation.Enabled)
							require.False(t, operation.RequiresResourceCheck)
						}
					}
				}
				require.Equal(t, manage && deploy, found, "auth=%s manage=%t deploy=%t", auth, manage, deploy)
			}
		}
	}
}

func TestDiscoveryWireMatchesSharedSchema(t *testing.T) {
	raw, err := os.ReadFile("../../../../../../packages/app-data-client/contracts/capability-discovery/response.schema.json")
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(raw, &schema))
	compiled, err := schema.Resolve(nil)
	require.NoError(t, err)
	service, ctx, schemas, evidence := discoveryFixture()
	for _, change := range []func(){func() {}, func() { evidence.AsyncWorker = false }, func() { evidence.ValidUntil = time.Now().Add(-time.Second) }, func() { schemas["source"] = false }, func() { delete(service.PublicRoutes, "POST /openapi/v2/uns/streams/program/invoke") }} {
		change()
		result := projectCapabilities(ctx, service, true, false, schemas, evidence, time.Now())
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		var value any
		require.NoError(t, json.Unmarshal(encoded, &value))
		require.NoError(t, compiled.Validate(value), string(encoded))
	}
}
