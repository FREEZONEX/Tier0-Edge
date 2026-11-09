package unsv2_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/apikey"
	"backend/internal/domain/appinstall"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
	corehandlers "backend/internal/handler/core/unsv2"
	requesthandlers "backend/internal/handler/openapi/v2/streamrequests"
	handlers "backend/internal/handler/openapi/v2/unsv2"
	"backend/internal/logic/fleetv2"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/testkit"
	"backend/internal/testkit/scripttest"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
	zerohandler "github.com/zeromicro/go-zero/rest/handler"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"gorm.io/gorm"
)

type object = map[string]any

type receiptProtocolRuntime struct {
	sourcestream.ProtocolRuntime
	releases atomic.Int64
}

func (r *receiptProtocolRuntime) ReleaseSource(int64) { r.releases.Add(1) }

func TestNativeAtomicSourceTLSCreationAndReplay(t *testing.T) {
	nativeEdgeTestProduct(t)
	store, _ := testkit.NewTestDB(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	protocol := &receiptProtocolRuntime{}
	runtime := sourcestream.New(protocol, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer runtime.Close()
	service := unsv2.New(unsmeta.New(), nil)
	service.SetSourceRuntime(runtime)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.source.manage": {}, "uns.source.credentials": {}, "uns.read": {}, "uns.write": {}}})
	request := object{"document": object{"name": "Atomic TLS Source", "protocol": "http-api", "role": "client", "runtimeLocation": "local", "endpoint": server.URL, "http": object{"authentication": "none"}}, "credentials": object{"kind": "custom", "values": object{"caCertificatePem": certificate}}, "idempotencyKey": "atomic-tls-source"}
	invoke := func() (any, error) {
		raw, e := json.Marshal(request)
		require.NoError(t, e)
		return service.Execute(ctx, "sourceDocumentCreate", raw)
	}
	count := func() int64 {
		var n int64
		require.NoError(t, store.UnsDB().Model(&repo.Source{}).Count(&n).Error)
		return n
	}
	request["dryRun"] = true
	_, err := invoke()
	require.NoError(t, err)
	require.Zero(t, count(), "preview must not leave a Source or owned TLS slot")
	request["dryRun"] = false
	// A receipt failure, including a deferred failure at COMMIT, must not leave
	// either the Source or a permanently running admission record behind.
	require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION reject_runtime_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN
	  IF NEW.kind='task' AND NEW.data->>'type'='apiRuntimeReceipt' AND NEW.data->>'status'='completed' THEN
	    RAISE EXCEPTION 'injected receipt failure';
	  END IF;
	  RETURN NEW;
	END $$`).Error)
	for _, trigger := range []string{
		`CREATE TRIGGER reject_runtime_receipt BEFORE UPDATE ON public.uns_meta FOR EACH ROW EXECUTE FUNCTION reject_runtime_receipt()`,
		`CREATE CONSTRAINT TRIGGER reject_runtime_receipt AFTER UPDATE ON public.uns_meta DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_runtime_receipt()`,
	} {
		require.NoError(t, store.UnsDB().Exec(trigger).Error)
		_, err := invoke()
		require.Error(t, err)
		require.Zero(t, count(), "Source and credentials must roll back with their receipt")
		require.Zero(t, protocol.releases.Load(), "rolled-back writes must not invalidate live Source sessions")
		var receipts int64
		require.NoError(t, store.UnsDB().Model(&repo.UnsMeta{}).Where("kind='task' AND data->>'type'='apiRuntimeReceipt'").Count(&receipts).Error)
		require.Zero(t, receipts, "the same request must remain recoverable after rollback")
		require.NoError(t, store.UnsDB().Exec(`DROP TRIGGER reject_runtime_receipt ON public.uns_meta`).Error)
	}
	require.NoError(t, store.UnsDB().Exec(`DROP FUNCTION reject_runtime_receipt()`).Error)

	// Concurrent callers use the same existing task identity. Only one may create
	// the Source; the others wait for and return that transaction's frozen reply.
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)
	type reply struct {
		value any
		err   error
	}
	replies := make(chan reply, 4)
	for i := 0; i < cap(replies); i++ {
		go func() {
			value, err := service.Execute(ctx, "sourceDocumentCreate", requestJSON)
			replies <- reply{value, err}
		}()
	}
	first := <-replies
	require.NoError(t, first.err)
	created := first.value
	for i := 1; i < cap(replies); i++ {
		result := <-replies
		require.NoError(t, result.err)
		require.Equal(t, created, result.value)
	}
	require.EqualValues(t, 1, count())
	require.EqualValues(t, 1, protocol.releases.Load(), "only the committed creation refreshes Source sessions")
	again, err := invoke()
	require.NoError(t, err)
	require.Equal(t, created, again)
	require.EqualValues(t, 1, count(), "a single atomic request must replay without creating another Source")
	raw, err := json.Marshal(created)
	require.NoError(t, err)
	require.Contains(t, string(raw), "caRef")
	require.NotContains(t, string(raw), "BEGIN CERTIFICATE")
	request["idempotencyKey"] = "invalid-tls-source"
	request["document"].(object)["name"] = "Invalid TLS Source"
	request["credentials"].(object)["values"] = object{"clientCertificatePem": certificate}
	_, err = invoke()
	require.Error(t, err)
	require.EqualValues(t, 1, count(), "invalid TLS material must roll back the new Source in the same API call")
}

type documentTestExecutor struct {
	*scripttest.TrustedScriptExecutor
}

func (documentTestExecutor) ExecuteProgram(ctx context.Context, request streamexec.ProgramRequest, host streamexec.ProgramHost) (streamexec.ProgramResult, error) {
	return streamexec.ExecuteProgram(ctx, request, host)
}

func TestFleetValidationErrorDetails(t *testing.T) {
	w := httptest.NewRecorder()
	fleetv2.WriteError(w, httptest.NewRequest("POST", "/", nil), &unsv2.Fault{
		Code: "INVALID_REQUEST", Path: "document.modbus.unitId", Reason: "required field is missing", Message: "private-password-value",
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "document.modbus.unitId") || !strings.Contains(w.Body.String(), "requestValidation") || strings.Contains(w.Body.String(), "private-password-value") {
		t.Fatalf("unexpected validation envelope: %s", w.Body.String())
	}
}

func nativeEdgeTestProduct(t *testing.T) {
	t.Helper()
	// Exercise shared authorization without a host-bound Enterprise license.
	t.Setenv("TIER0_RUNTIME_MODE", "native")
}

func TestNativeDocumentDryRunDefault(t *testing.T) {
	nativeEdgeTestProduct(t)
	store, _ := testkit.NewTestDB(t)
	service := unsv2.New(unsmeta.New(), nil)
	service.SetSourceRuntime(sourcestream.New(nil, documentTestExecutor{scripttest.New(t)}, ""))
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.source.manage": {}, "uns.stream.manage": {}, "uns.read": {}, "uns.write": {}}})
	for _, tc := range []struct {
		operation string
		document  object
		model     any
	}{
		{"sourceDocumentCreate", object{"name": "Test MQTT", "protocol": "mqtt", "endpoint": "mqtt://broker:1883", "runtimeLocation": "local", "mqtt": object{"clientId": "test"}}, &repo.Source{}},
		{"streamDocumentCreate", object{"name": "Test Stream", "script": object{"code": "module.exports=api=>api.stream.onRequest({id:'read'},ctx=>ctx.input);", "timeoutMs": 3000}}, &repo.Stream{}},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			count := func() int64 {
				var n int64
				require.NoError(t, store.UnsDB().Model(tc.model).Count(&n).Error)
				return n
			}
			initial := count()
			for i, explicit := range []bool{false, true} {
				tc.document["name"] = fmt.Sprintf("Dry run %d", i)
				request := object{"document": tc.document, "idempotencyKey": fmt.Sprintf("%s-%d", tc.operation, i), "dryRun": true}
				execute := func() any {
					raw, err := json.Marshal(request)
					require.NoError(t, err)
					value, err := service.Execute(ctx, tc.operation, raw)
					require.NoError(t, err)
					return value
				}
				require.Equal(t, object{"validated": true, "sideEffects": false}, execute())
				require.Equal(t, initial+int64(i), count())
				if explicit {
					request["dryRun"] = false
				} else {
					delete(request, "dryRun")
				}
				if tc.operation == "streamDocumentCreate" && !explicit {
					require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION reject_stream_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
					BEGIN
					  IF NEW.kind='task' AND NEW.data->>'type'='apiRuntimeReceipt' AND NEW.data->>'status'='completed' THEN
					    RAISE EXCEPTION 'injected Stream receipt failure';
					  END IF;
					  RETURN NEW;
					END $$`).Error)
					require.NoError(t, store.UnsDB().Exec(`CREATE TRIGGER reject_stream_receipt BEFORE UPDATE ON public.uns_meta FOR EACH ROW EXECUTE FUNCTION reject_stream_receipt()`).Error)
					raw, err := json.Marshal(request)
					require.NoError(t, err)
					_, err = service.Execute(ctx, tc.operation, raw)
					require.Error(t, err)
					require.Equal(t, initial, count(), "Stream creation must roll back with its receipt")
					require.NoError(t, store.UnsDB().Exec(`DROP TRIGGER reject_stream_receipt ON public.uns_meta`).Error)
					require.NoError(t, store.UnsDB().Exec(`DROP FUNCTION reject_stream_receipt()`).Error)
				}
				created := execute()
				require.NotEmpty(t, created.(object)["id"])
				if explicit {
					delete(request, "dryRun")
				} else {
					request["dryRun"] = false
				}
				require.Equal(t, created, execute())
				require.Equal(t, initial+int64(i)+1, count())
			}
		})
	}
}

func TestFleetNativeResourcePermissions(t *testing.T) {
	nativeEdgeTestProduct(t)
	subject := contextx.Subject{UserID: 1, AuthType: "apiKey", APIKeyPermission: "read_write", MCPRestricted: true,
		MCPScopes:    []string{"uns.app.read", "fleet.ssh"},
		ResourceKeys: map[string]struct{}{"uns.app.read": {}, "fleet.ssh.manage": {}},
	}
	request := unsv2.FleetRuntimeRequest{NodeID: "registered-edge", Operation: "appList", Data: json.RawMessage(`{}`)}
	packet, err := unsv2.PrepareFleetRuntime(contextx.WithSubject(context.Background(), subject), request)
	require.NoError(t, err, "native App reads must not need a Fleet resource allowlist entry")
	require.Equal(t, []string{"uns.app.read"}, packet.Permissions)
	require.Equal(t, []string{"uns.app.read"}, packet.MCPScopes)

	subject.ResourceKeys = map[string]struct{}{"uns.read": {}}
	_, err = unsv2.PrepareFleetRuntime(contextx.WithSubject(context.Background(), subject), request)
	require.Error(t, err, "UNS data reading does not grant App reading")

	subject.ResourceKeys = map[string]struct{}{"uns.app.manage": {}}
	subject.MCPScopes = []string{"uns.app.manage"}
	subject.APIKeyPermission = "read_only"
	request.Operation = "appCreate"
	_, err = unsv2.PrepareFleetRuntime(contextx.WithSubject(context.Background(), subject), request)
	require.Error(t, err, "remote writes must retain the read-only key boundary")

	request.Operation = "http://unregistered-target/admin"
	_, err = unsv2.PrepareFleetRuntime(contextx.WithSubject(context.Background(), subject), request)
	require.Error(t, err, "operation names must still exist in the native contract")

	target := unsv2.FleetRuntimeTarget(packet)
	packet.Data = json.RawMessage(`{"page":2}`)
	_, err = new(unsv2.Service).ExecuteFleetRuntime(context.Background(), "center", target, packet)
	require.Error(t, err, "a signed operation cannot be changed before execution")
}

func TestFleetRuntimeCatalog(t *testing.T) {
	nativeEdgeTestProduct(t)
	// Exercise the real Fleet execution adapter and shared catalog without a DB:
	// schema discovery must be read-only and retain the native audience filtering.
	service := new(unsv2.Service)
	request := unsv2.FleetRuntimeRequest{NodeID: "registered-edge", Operation: "catalog", Data: json.RawMessage(`{}`)}
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("readOnly=%t", readOnly), func(t *testing.T) {
			subject := contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: map[string]struct{}{"uns.read": {}, "fleet.ssh.manage": {}}}
			if readOnly {
				subject.AuthType, subject.APIKeyPermission = "apiKey", "read_only"
				subject.MCPRestricted = true
				subject.MCPScopes = []string{"uns.read", "fleet.ssh"}
			}
			packet, err := unsv2.PrepareFleetRuntime(contextx.WithSubject(context.Background(), subject), request)
			require.NoError(t, err)
			require.Equal(t, []string{"uns.read"}, packet.Permissions)
			require.Equal(t, readOnly, packet.ReadOnly)
			if readOnly {
				require.Equal(t, []string{"uns.read"}, packet.MCPScopes)
			}
			result, err := service.ExecuteFleetRuntime(context.Background(), "center", unsv2.FleetRuntimeTarget(packet), packet)
			require.NoError(t, err)
			catalog := result.(map[string]any)
			native := unsv2.Catalog()
			require.Equal(t, native["contractVersion"], catalog["contractVersion"])
			require.Equal(t, native["schemas"], catalog["schemas"], "must serve this Edge's actual schemas")
			stream, ok := unsv2.FindResourceOperation("streamDocumentCreate")
			require.True(t, ok)
			require.Contains(t, catalog["schemas"], stream.Request)
			operations := catalog["operations"].([]any)
			require.NotEmpty(t, operations)
			for _, entry := range operations {
				op := entry.(map[string]any)
				if readOnly {
					require.NotEqual(t, true, op["coreOnly"])
					require.NotEqual(t, true, op["deprecated"])
				} else {
					require.NotEqual(t, true, op["openapiOnly"])
				}
			}
		})
	}
}

func TestFleetRuntimeCatalogRejectsInvalidAuthorization(t *testing.T) {
	nativeEdgeTestProduct(t)
	service := new(unsv2.Service)
	packet := unsv2.FleetRuntimePacket{Operation: "catalog", Data: json.RawMessage(`{}`), Actor: "1/0/", Permissions: []string{"uns.read"}, ReadOnly: true}
	target := unsv2.FleetRuntimeTarget(packet)
	for _, tc := range []struct {
		name, code string
		mutate     func(*unsv2.FleetRuntimePacket)
		keepTarget bool
	}{
		{"missing read permission", "FORBIDDEN", func(p *unsv2.FleetRuntimePacket) { p.Permissions = nil }, false},
		{"write is not read", "FORBIDDEN", func(p *unsv2.FleetRuntimePacket) { p.Permissions = []string{"uns.write"} }, false},
		{"unrelated permission", "FORBIDDEN", func(p *unsv2.FleetRuntimePacket) { p.Permissions = []string{"uns.read", "fleet.ssh.manage"} }, false},
		{"missing actor", "FORBIDDEN", func(p *unsv2.FleetRuntimePacket) { p.Actor = "" }, false},
		{"changed actor", "FORBIDDEN", func(p *unsv2.FleetRuntimePacket) { p.Actor = "2/0/" }, true},
		{"changed data", "FORBIDDEN", func(p *unsv2.FleetRuntimePacket) { p.Data = json.RawMessage(`{"url":"http://other-host/admin"}`) }, true},
		{"arbitrary HTTP", "INVALID_REQUEST", func(p *unsv2.FleetRuntimePacket) { p.Operation = "http://other-host/admin" }, false},
		{"other unlisted operation", "INVALID_REQUEST", func(p *unsv2.FleetRuntimePacket) { p.Operation = "appKeyCreate" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := packet
			tc.mutate(&candidate)
			signedTarget := target
			if !tc.keepTarget {
				signedTarget = unsv2.FleetRuntimeTarget(candidate)
			}
			_, err := service.ExecuteFleetRuntime(context.Background(), "center", signedTarget, candidate)
			var fault *unsv2.Fault
			require.ErrorAs(t, err, &fault)
			require.Equal(t, tc.code, fault.Code)
		})
	}
	_, err := service.ExecuteFleetRuntime(context.Background(), "", target, packet)
	require.Error(t, err, "authenticated peer is required")
	_, err = service.ExecuteFleetRuntime(context.Background(), "center", "other-target", packet)
	require.Error(t, err, "target binding is required")

	for _, subject := range []contextx.Subject{
		{UserID: 1, ResourceKeys: map[string]struct{}{"uns.write": {}}},
		{ResourceKeys: map[string]struct{}{"uns.read": {}}},
		{UserID: 1, APIKeyUsageType: repo.APIKeyUsageAppData, ResourceKeys: map[string]struct{}{"uns.read": {}}},
	} {
		_, err := unsv2.PrepareFleetRuntime(contextx.WithSubject(context.Background(), subject), unsv2.FleetRuntimeRequest{NodeID: "registered-edge", Operation: "catalog", Data: json.RawMessage(`{}`)})
		require.Error(t, err, "invalid callers must not receive a Fleet catalog packet")
	}
}

type appAPI struct {
	t                  *testing.T
	server             *httptest.Server
	svc                *svc.ServiceContext
	admin              string
	next               int
	validator          *jsonschema.Resolved
	responseOperations []appResponseOperation
}

type appResponseOperation struct {
	Name     string `json:"name"`
	Suffix   string `json:"suffix"`
	Response string `json:"response"`
}

func responseSchemaNullable(v any) {
	switch n := v.(type) {
	case map[string]any:
		for _, child := range n {
			responseSchemaNullable(child)
		}
		if n["nullable"] == true {
			delete(n, "nullable")
			inner := map[string]any{}
			for key, child := range n {
				inner[key] = child
				delete(n, key)
			}
			n["anyOf"] = []any{inner, map[string]any{"type": "null"}}
		}
	case []any:
		for _, child := range n {
			responseSchemaNullable(child)
		}
	}
}

func (a *appAPI) validateResponse(path string, envelope object) {
	a.t.Helper()
	actual := strings.Split(path, "/")
	for _, operation := range a.responseOperations {
		pattern := strings.Split(strings.TrimPrefix(operation.Suffix, "/uns/"), "/")
		if len(actual) != len(pattern) {
			continue
		}
		matched := true
		for i, part := range pattern {
			if !strings.HasPrefix(part, ":") && part != actual[i] {
				matched = false
				break
			}
		}
		if matched {
			require.NoError(a.t, a.validator.Validate(object{operation.Name: envelope}), "%s response violates generated %s", path, operation.Response)
			return
		}
	}
	a.t.Fatalf("response schema operation missing for %s", path)
}

func appResourcePath(path string) string {
	actions := map[string]string{"find": "find", "count": "count", "distinct": "distinct", "explain": "explain", "aggregate-pipeline": "aggregate", "insert-one": "", "insert-many": "insertMany", "update-one": "updateOne", "update-many": "updateMany", "replace-one": "replaceOne", "delete-one": "deleteOne", "delete-many": "deleteMany", "find-one-and-update": "findOneAndUpdate", "find-one-and-replace": "findOneAndReplace", "find-one-and-delete": "findOneAndDelete", "bulk-write": "bulkWrite"}
	if strings.HasPrefix(path, "records/") {
		if action, ok := actions[strings.TrimPrefix(path, "records/")]; ok {
			suffix := ""
			if action != "" {
				suffix = ":" + action
			}
			return "collections/:collectionId/records" + suffix
		}
	}
	switch path {
	case "samples/query-series":
		return "collections/:collectionId/samples:find"
	case "samples/aggregate":
		return "collections/:collectionId/samples:aggregate"
	case "samples/latest-values":
		return "collections/:collectionId/samples:latest"
	case "samples/delete-range":
		return "collections/:collectionId/samples:delete"
	case "samples/append":
		return "collections/:collectionId/records/:recordId/samples"
	case "records/query":
		return "collections/:collectionId/records:aggregate"
	}
	return path
}

func newAppAPI(t *testing.T) *appAPI {
	t.Helper()
	nativeEdgeTestProduct(t)
	s := svctest.NewTestServiceContext(t)
	s.App.UnsMeta = unsmeta.New()
	s.App.UnsV2 = unsv2.New(s.App.UnsMeta, s.App.Auth.RefreshSubject)
	mux := http.NewServeMux()
	for path, handler := range map[string]http.HandlerFunc{
		"metadata":            handlers.UnsV2MetadataInsertOneHandler(s),
		"metadata:find":       handlers.UnsV2MetadataFindHandler(s),
		"metadata:count":      handlers.UnsV2MetadataCountHandler(s),
		"metadata:aggregate":  handlers.UnsV2MetadataAggregateHandler(s),
		"metadata:updateOne":  handlers.UnsV2MetadataUpdateOneHandler(s),
		"metadata:updateMany": handlers.UnsV2MetadataUpdateManyHandler(s),
		"metadata:replaceOne": handlers.UnsV2MetadataReplaceOneHandler(s),
		"metadata:deleteOne":  handlers.UnsV2MetadataDeleteOneHandler(s),
		"metadata:deleteMany": handlers.UnsV2MetadataDeleteManyHandler(s),
		"metadata:restore":    handlers.UnsV2MetadataRestoreHandler(s),
		"metadata:purge":      handlers.UnsV2MetadataPurgeHandler(s),
		"metadata:bulkWrite":  handlers.UnsV2MetadataBulkWriteHandler(s),
		"metadata:transact":   handlers.UnsV2MetadataTransactHandler(s),
		"namespace:copy":      handlers.UnsV2NamespaceCopyHandler(s),
		"sources:replace":     handlers.UnsV2SourceReplaceHandler(s),
		"streams:replace":     handlers.UnsV2StreamReplaceHandler(s),
		"apps:replace":        handlers.UnsV2AppReplaceHandler(s),
		"apps/create":         handlers.UnsV2AppCreateHandler(s),

		"records/find":                              handlers.UnsV2RecordFindHandler(s),
		"collections/:collectionId/records:get":     handlers.UnsV2RecordGetHandler(s),
		"records/count":                             handlers.UnsV2RecordCountHandler(s),
		"records/insert-one":                        handlers.UnsV2RecordInsertOneHandler(s),
		"records/update-one":                        handlers.UnsV2RecordUpdateOneHandler(s),
		"records/replace-one":                       handlers.UnsV2RecordReplaceOneHandler(s),
		"records/delete-one":                        handlers.UnsV2RecordDeleteOneHandler(s),
		"records/update-many":                       handlers.UnsV2RecordUpdateManyHandler(s),
		"records/delete-many":                       handlers.UnsV2RecordDeleteManyHandler(s),
		"records/bulk-write":                        handlers.UnsV2RecordBulkWriteHandler(s),
		"samples/query-series":                      handlers.UnsV2SampleFindHandler(s),
		"samples/aggregate":                         handlers.UnsV2SampleAggregateHandler(s),
		"samples/latest-values":                     handlers.UnsV2SampleLatestValuesHandler(s),
		"samples/delete-range":                      handlers.UnsV2SampleDeleteRangeHandler(s),
		"collections/:collectionId/records:lock":    handlers.UnsV2RecordLockHandler(s),
		"collections/:collectionId/records:renew":   handlers.UnsV2RecordRenewHandler(s),
		"collections/:collectionId/records:unlock":  handlers.UnsV2RecordUnlockHandler(s),
		"collections/:collectionId/records:restore": handlers.UnsV2RecordRestoreHandler(s),
		"records:transact":                          handlers.UnsV2RecordTransactionExecuteHandler(s),
		"samples/append":                            handlers.UnsV2SampleAppendHandler(s),
		"apps/context":                              handlers.UnsV2AppContextHandler(s),
		"apps/keys/create":                          handlers.UnsV2AppKeyCreateHandler(s),
		"apps/keys/revoke":                          handlers.UnsV2AppKeyRevokeHandler(s),
		"app-stream-policy/get":                     handlers.UnsV2AppStreamPolicyGetHandler(s),
		"app-stream-policy/update":                  handlers.UnsV2AppStreamPolicyUpdateHandler(s),
		"stream-requests/grants/get":                handlers.UnsV2StreamRequestGrantGetHandler(s),
		"app-stream-access/list":                    handlers.UnsV2AppStreamAccessListHandler(s),
		"stream-requests/grants/set":                handlers.UnsV2StreamRequestGrantSetHandler(s),
		"records/query":                             handlers.UnsV2RecordAggregatePipelineHandler(s),
		"records:queryBatch":                        handlers.UnsV2RecordQueryBatchHandler(s),
		"topics/view":                               handlers.UnsV2TopicsHandler(s),
		"jobs/query":                                handlers.UnsV2JobsHandler(s),
	} {
		pattern := strings.ReplaceAll(strings.ReplaceAll(appResourcePath(path), ":collectionId", "{collectionId}"), ":recordId", "{recordId}")
		mux.HandleFunc("POST /openapi/v2/uns/"+pattern, s.ApiKeyAuth(handler))
	}
	mux.HandleFunc("POST /openapi/v1/uns/write", s.ApiKeyAuth(func(w http.ResponseWriter, r *http.Request) { t.Error("App key reached legacy handler") }))
	a := &appAPI{t: t, svc: s, server: httptest.NewServer(mux)}
	var contract struct {
		Operations []appResponseOperation `json:"operations"`
		Schemas    map[string]any         `json:"schemas"`
	}
	require.NoError(t, json.Unmarshal([]byte(unsv2.ContractJSON), &contract))
	properties := map[string]any{}
	for _, operation := range contract.Operations {
		if operation.Response != "" {
			properties[operation.Name] = object{"$ref": "#/$defs/" + operation.Response}
		} else {
			properties[operation.Name] = object{"type": "object", "properties": object{"data": object{"$ref": "#/$defs/" + unsv2.ResponseModel(operation.Name)}}}
		}
	}
	root := object{"type": "object", "additionalProperties": false, "properties": properties, "$defs": contract.Schemas}
	encoded, err := json.Marshal(root)
	require.NoError(t, err)
	var converted any
	require.NoError(t, json.Unmarshal([]byte(strings.ReplaceAll(string(encoded), "#/components/schemas/", "#/$defs/")), &converted))
	responseSchemaNullable(converted)
	encoded, err = json.Marshal(converted)
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(encoded, &schema))
	a.validator, err = schema.Resolve(nil)
	require.NoError(t, err)
	a.responseOperations = contract.Operations
	t.Cleanup(a.server.Close)
	_, a.admin, _ = repo.NewAPIKeyRepo(context.Background()).CreateAPIKey(context.Background(), "agent-provisioner", 1, 1, "full_access", "external", "personal", []string{"uns.read", "uns.write", "uns.delete", "uns.app.manage", "uns.app.read", "uns.app.credentials"})
	require.NotEmpty(t, a.admin)
	return a
}

func (a *appAPI) post(key, path string, body any, status int) object {
	path = appResourcePath(path)
	if value, ok := body.(object); ok {
		for _, field := range []string{"collectionId", "recordId"} {
			if strings.Contains(path, ":"+field) {
				path = strings.ReplaceAll(path, ":"+field, fmt.Sprint(value[field]))
			}
		}
	}
	a.t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(a.t, err)
	request, err := http.NewRequest(http.MethodPost, a.server.URL+"/openapi/v2/uns/"+path, bytes.NewReader(raw))
	require.NoError(a.t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", key)
	response, err := a.server.Client().Do(request)
	require.NoError(a.t, err)
	defer response.Body.Close()
	var envelope object
	require.NoError(a.t, json.NewDecoder(response.Body).Decode(&envelope))
	// Do not include responses containing a credential in assertion failures.
	require.Equal(a.t, status, response.StatusCode, "%s: %v", path, envelope["msg"])
	if status != http.StatusOK {
		return envelope
	}
	a.validateResponse(path, envelope)
	return envelope["data"].(map[string]any)
}

func (a *appAPI) apply(key string, status int, items ...object) object {
	a.next++
	path := "metadata:transact"
	if len(items) == 1 && items[0]["kind"] == "app" {
		item := items[0]
		if item["action"] == "meta.create" {
			// App 授权对象是这些数据 API 测试的初始夹具，创建后管理更新走真实专用 HTTP 入口。
			require.Equal(a.t, a.admin, key)
			ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}, "uns.delete": {}, "uns.app.manage": {}}})
			raw, _ := json.Marshal(object{"mode": "sync", "items": items})
			var batch unsv2.Batch
			require.NoError(a.t, json.Unmarshal(raw, &batch))
			out, err := a.svc.App.UnsV2.Apply(ctx, batch)
			require.NoError(a.t, err)
			return object{"status": out.Status}
		}
		body := object{"id": item["id"], "replacement": item["data"], "idempotencyKey": fmt.Sprintf("test-%d", a.next)}
		if revision, ok := item["expectedRevision"]; ok {
			body["expectedRevision"] = revision
		}
		return a.post(key, "apps:replace", body, status)
	}
	if len(items) > 0 {
		if strings.HasPrefix(fmt.Sprint(items[0]["action"]), "record.") {
			path = "records:transact"
		} else if items[0]["action"] == "sample.append" {
			item := items[0]
			sample := object{"time": item["time"], "data": item["data"]}
			if id, ok := item["id"]; ok {
				sample["id"] = id
			}
			return a.post(key, "samples/append", object{"collectionId": item["collectionId"], "recordId": item["recordId"], "dryRun": false, "items": []object{sample}}, status)
		}
	}
	if path == "metadata:transact" {
		operations := []object{}
		for _, item := range items {
			op := object{}
			for key, value := range item {
				op[key] = value
			}
			switch item["action"] {
			case "meta.create":
				op["action"] = "insertOne"
			case "meta.replace":
				op["action"] = "replaceOne"
				op["replacement"] = op["data"]
				delete(op, "data")
			case "meta.patch":
				op["action"] = "updateOne"
				op["update"] = object{"$set": op["data"]}
				delete(op, "data")
			case "meta.archive":
				op["action"] = "deleteOne"
			case "meta.restore":
				op["action"] = "restoreOne"
			}
			operations = append(operations, op)
		}
		return a.post(key, path, object{"idempotencyKey": fmt.Sprintf("test-%d", a.next), "operations": operations}, status)
	}
	return a.post(key, path, object{"idempotencyKey": fmt.Sprintf("test-%d", a.next), "mode": "sync", "items": items}, status)
}

func (a *appAPI) query(key, path string, q object, status int) object {
	a.t.Helper()
	if path == "records/query" {
		return a.post(key, path, q, status)
	}
	q["queryId"] = "q"
	result := a.post(key, path, object{"queries": []object{q}}, status)
	if status != http.StatusOK {
		return result
	}
	return result["results"].([]any)[0].(map[string]any)
}

func appDocument(permissions []string) object {
	return object{"schemaVersion": 1, "name": "Maintenance", "roles": []any{}, "members": []any{}, "collections": []object{{"alias": "orders", "collectionId": "orders", "permissions": permissions}}, "navigation": object{"topicIds": []any{}}, "status": "active"}
}

func TestAppRegistrationCannotClaimProjectPrincipal(t *testing.T) {
	a := newAppAPI(t)
	ctx := svctest.CtxWithUser(1)
	r := repo.NewProjectRepo(a.svc.App.Store)
	project, err := r.CreateProject(ctx, repo.Project{Name: "Hosted principal", Status: "active", OwnerUserID: 1})
	require.NoError(t, err)
	app, err := r.CreateProjectApp(ctx, repo.ProjectApp{ProjectID: project.ID, AppKey: "hosted", Name: "Hosted"})
	require.NoError(t, err)
	for _, phase := range []string{"live", "retired"} {
		if phase == "retired" {
			require.NoError(t, r.DeleteProjectApp(ctx, project.ID, app.ID, 1))
		}
		a.post(a.admin, "apps/create", object{"idempotencyKey": "claim-" + phase, "dryRun": false,
			"items": []object{{"id": app.AppPrincipalID, "name": "Impersonator"}}}, 403)
		reserved, err := r.ProjectAppPrincipalReserved(ctx, app.AppPrincipalID)
		require.NoError(t, err)
		require.True(t, reserved)
		var count int64
		require.NoError(t, a.svc.App.Store.UnsDB().Model(&repo.UnsMeta{}).Where("kind='app' AND id=?", app.AppPrincipalID).Count(&count).Error)
		require.Zero(t, count)
		// Simulate stale/imported metadata outside the public write gate. It
		// must not grant runtime access or allow issuing a new external App key.
		forged := repo.UnsMeta{Kind: "app", ID: app.AppPrincipalID, Revision: 1,
			Data: json.RawMessage(`{"schemaVersion":1,"name":"Forged","status":"active","roles":[],"members":[],"collections":[],"navigation":{"topicIds":[]}}`)}
		require.NoError(t, a.svc.App.Store.UnsDB().Create(&forged).Error)
		a.post(a.admin, "apps/keys/create", object{"appId": app.AppPrincipalID, "name": "must-not-issue"}, 403)
		_, key, err := repo.NewAPIKeyRepo(a.svc.App.Store).CreateAppDataKey(ctx, "stale-key-"+phase, app.AppPrincipalID, 1, 1, []string{"uns.read"})
		require.NoError(t, err)
		a.post(key, "apps/context", object{}, 403)
		require.NoError(t, a.svc.App.Store.UnsDB().Where("kind='app' AND id=?", app.AppPrincipalID).Delete(&repo.UnsMeta{}).Error)
	}
	a.post(a.admin, "apps/create", object{"idempotencyKey": "independent-external", "dryRun": false,
		"items": []object{{"id": "independent-external", "name": "External"}}}, 200)
}

func TestHostedAppStreamAuthorityUsesLiveFacts(t *testing.T) {
	s := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	r := repo.NewProjectRepo(s.App.Store)
	project, err := r.CreateProject(ctx, repo.Project{Name: "Stream authority", Status: "active", OwnerUserID: 1})
	require.NoError(t, err)
	app, err := r.CreateProjectApp(ctx, repo.ProjectApp{ProjectID: project.ID, AppKey: "stream-authority", Name: "Stream authority", EntryURL: "/apps/fixture"})
	require.NoError(t, err)
	profile := object{"schemaVersion": 1, "name": app.AppPrincipalID, "dataProfileVersion": "cloud-hosted-app-data/v1", "status": "disabled",
		"collections": []any{}, "roles": []any{}, "members": []any{}, "navigation": object{"topicIds": []any{}},
		"streamInteraction": object{"version": "cloud-hosted-app-stream/v1", "status": "active", "platformPermissions": []string{"stream.requests.invoke", "stream.requests.write"}}}
	raw, err := json.Marshal(profile)
	require.NoError(t, err)
	require.NoError(t, s.App.Store.UnsDB().Create(&repo.UnsMeta{Kind: "app", ID: app.AppPrincipalID, Data: raw, Revision: 1}).Error)
	subject := contextx.Subject{UserID: 1, APIKeyUsageType: repo.APIKeyUsageAppData, APIKeyAppID: app.AppPrincipalID,
		ResourceKeys: map[string]struct{}{"stream.requests.invoke": {}, "stream.requests.write": {}, "control.execute": {}}}
	check := func(permission string) error {
		return unsv2.CheckAppPlatformPermission(contextx.WithSubject(ctx, subject), permission)
	}
	require.NoError(t, check("stream.requests.invoke"), "disabled data policy does not disable active Stream policy")
	require.Error(t, check("control.execute"), "new Stream grants never imply legacy Control authority")
	delete(subject.ResourceKeys, "stream.requests.write")
	require.Error(t, check("stream.requests.write"), "policy cannot expand a key's effective scopes")
	write := func() {
		raw, err := json.Marshal(profile)
		require.NoError(t, err)
		require.NoError(t, s.App.Store.UnsDB().Model(&repo.UnsMeta{}).Where("kind='app' AND id=?", app.AppPrincipalID).Update("data", raw).Error)
	}
	policy := profile["streamInteraction"].(object)
	policy["status"], policy["platformPermissions"] = "disabled", []string{}
	write()
	require.Error(t, check("stream.requests.invoke"), "revocation is effective without reissuing the key")
	policy["status"], policy["platformPermissions"] = "active", []string{"stream.requests.invoke"}
	write()
	require.NoError(t, check("stream.requests.invoke"))
	for _, permissions := range [][]string{{"stream.requests.write"}, {"stream.requests.invoke", "stream.requests.invoke"}, {"stream.requests.manage"}, {"stream.messages.publish"}} {
		policy["platformPermissions"] = permissions
		write()
		require.Error(t, check("stream.requests.invoke"), "invalid or unsupported policy must fail closed")
	}
	policy["platformPermissions"] = []string{"stream.requests.invoke"}
	profile["PlatformPermissions"] = []string{"stream.requests.invoke"}
	write()
	require.Error(t, check("stream.requests.invoke"), "case variants cannot smuggle top-level authority")
	delete(profile, "PlatformPermissions")
	write()
	for _, status := range []string{"stopped", "disabled", "deploying", "failed"} {
		require.NoError(t, r.UpdateProjectAppStatus(ctx, project.ID, app.ID, status, 1))
		require.Error(t, check("stream.requests.invoke"), status)
	}
	require.NoError(t, r.UpdateProjectAppStatus(ctx, project.ID, app.ID, "active", 1))
	require.NoError(t, r.UpdateProjectAppManifest(ctx, project.ID, app.ID, map[string]any{"deploying": true}, 1))
	require.Error(t, check("stream.requests.invoke"))
	require.NoError(t, r.UpdateProjectAppManifest(ctx, project.ID, app.ID, map[string]any{"deploying": false}, 1))
	require.NoError(t, check("stream.requests.invoke"))
	require.NoError(t, s.App.Store.CommonDB().Model(&repo.Project{}).Where("id=?", project.ID).Update("status", "disabled").Error)
	require.Error(t, check("stream.requests.invoke"), "project state is checked live")
	require.NoError(t, s.App.Store.CommonDB().Model(&repo.Project{}).Where("id=?", project.ID).Update("status", "active").Error)
	require.NoError(t, r.DeleteProjectApp(ctx, project.ID, app.ID, 1))
	require.Error(t, check("stream.requests.invoke"), "deleted identities cannot use retained policy")
	// Unbound hosted-looking rows cannot fall back to the external App path.
	subject.APIKeyAppID = "9d2f8399-02af-4280-a345-970bbdd8fd5a"
	profile["name"], profile["status"] = subject.APIKeyAppID, "active"
	profile["platformPermissions"] = []string{"stream.requests.invoke"}
	raw, err = json.Marshal(profile)
	require.NoError(t, err)
	require.NoError(t, s.App.Store.UnsDB().Create(&repo.UnsMeta{Kind: "app", ID: subject.APIKeyAppID, Data: raw, Revision: 1}).Error)
	require.Error(t, check("stream.requests.invoke"), "a profile marker never confers identity or top-level authority")
}

func TestNativeAppStreamAuthorityAndCredentialPreparation(t *testing.T) {
	a := newAppAPI(t)
	ctx := svctest.CtxWithUser(1)
	id := "1d393c53-648f-4444-98de-1f45e5a15eb1"
	doc := object{"schemaVersion": 1, "name": "Native authority", "status": "active", "roles": []any{}, "members": []any{}, "collections": []any{}, "navigation": object{"topicIds": []any{}}}
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, a.svc.App.Store.UnsDB().Create(&repo.UnsMeta{Kind: "app", ID: id, Revision: 1, Data: raw}).Error)
	installation := repo.AppInstallation{AppID: id, Manifest: json.RawMessage(`{}`), Stage: "starting", Status: "starting", DesiredState: "running"}
	require.NoError(t, a.svc.App.Store.CommonDB().Create(&installation).Error)
	// The real credential handler remains usable before process health passes.
	issued := a.post(a.admin, "apps/keys/create", object{"appId": id, "name": "candidate-bootstrap"}, 200)
	a.post(issued["apiKey"].(string), "apps/context", object{}, 200)
	doc["platformPermissions"] = []string{"stream.requests.read", "stream.requests.invoke", "control.execute"}
	raw, err = json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, a.svc.App.Store.UnsDB().Model(&repo.UnsMeta{}).Where("kind='app' AND id=?", id).Update("data", raw).Error)
	// Exercise the production middleware and generated Request handler with
	// issued credentials, including the immutable scope of the bootstrap key.
	a.svc.App.SourceStream = sourcestream.New(nil, scripttest.New(t), "")
	a.svc.App.UnsV2.SetSourceRuntime(a.svc.App.SourceStream)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /openapi/v2/stream-requests/discover", a.svc.ApiKeyAuth(requesthandlers.StreamRequestDiscoverHandler(a.svc)))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	discover := func(key string, status int) {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, server.URL+"/openapi/v2/stream-requests/discover", strings.NewReader(`{}`))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-API-Key", key)
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		defer response.Body.Close()
		require.Equal(t, status, response.StatusCode)
		var value object
		require.NoError(t, json.NewDecoder(response.Body).Decode(&value))
		if status == http.StatusOK {
			require.EqualValues(t, 0, value["total"])
			require.Equal(t, []any{}, value["list"])
		}
	}
	_, manager, err := repo.NewAPIKeyRepo(ctx).CreateAPIKey(ctx, "stream-provisioner", 1, 1, "full_access", "external", "personal", []string{"uns.read", "uns.app.manage", "uns.app.credentials", "stream.requests.read", "stream.requests.invoke", "control.execute"})
	require.NoError(t, err)
	streamKey := a.post(manager, "apps/keys/create", object{"appId": id, "name": "candidate-stream"}, 200)["apiKey"].(string)
	discover(streamKey, http.StatusForbidden)
	subject := contextx.Subject{UserID: 1, AuthType: "apiKey", APIKeyUsageType: repo.APIKeyUsageAppData, APIKeyAppID: id,
		ResourceKeys: map[string]struct{}{"stream.requests.invoke": {}, "control.execute": {}}}
	check := func(permission string) error {
		return unsv2.CheckAppPlatformPermission(contextx.WithSubject(ctx, subject), permission)
	}
	set := func(values object) {
		t.Helper()
		require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Updates(values).Error)
	}
	require.Error(t, check("stream.requests.invoke"), "a candidate without a serving release has no Stream authority")
	set(object{"release_id": "63ad6f88-4102-4827-994d-d33f12f4eca0", "status": "running", "stage": "ready"})
	require.NoError(t, check("stream.requests.invoke"))
	require.NoError(t, check("control.execute"))
	discover(streamKey, http.StatusOK)
	discover(issued["apiKey"].(string), http.StatusForbidden)
	set(object{"stage": "failed", "last_error": "candidate failed"})
	require.NoError(t, check("stream.requests.invoke"), "failed replacement preserves the still-running release")
	discover(streamKey, http.StatusOK)
	set(object{"stage": "starting", "operation_id": "replacement"})
	require.NoError(t, check("stream.requests.invoke"), "building a replacement does not revoke the serving release")
	for _, state := range []object{
		{"desired_state": "stopped", "status": "running", "stage": "ready"},
		{"desired_state": "running", "status": "stopped", "stage": "ready"},
		{"desired_state": "running", "status": "starting", "stage": "ready"},
		{"desired_state": "running", "status": "failed", "stage": "ready"},
		{"desired_state": "running", "status": "running", "stage": "uninstalled"},
	} {
		set(state)
		require.Error(t, check("stream.requests.invoke"), "current installation state must revoke old keys immediately")
		require.Error(t, check("control.execute"), "legacy Control has the same installation boundary")
		discover(streamKey, http.StatusForbidden)
	}
	set(object{"desired_state": "running", "status": "running", "stage": "ready"})
	require.NoError(t, check("stream.requests.invoke"))
	discover(streamKey, http.StatusOK)
	doc["platformPermissions"] = []string{"stream.requests.invoke", "control.execute"}
	raw, err = json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, a.svc.App.Store.UnsDB().Model(&repo.UnsMeta{}).Where("kind='app' AND id=?", id).Update("data", raw).Error)
	discover(streamKey, http.StatusForbidden)
	delete(subject.ResourceKeys, "stream.requests.invoke")
	require.Error(t, check("stream.requests.invoke"), "installation readiness never expands Key authority")
	subject.ResourceKeys["stream.requests.invoke"] = struct{}{}
	// Another external identity cannot inherit a running installation's grants.
	subject.APIKeyAppID = "external-app"
	require.Error(t, check("stream.requests.invoke"))
}

func TestNativeAppIndependentStreamPolicy(t *testing.T) {
	a := newAppAPI(t)
	a.provision()
	ctx := svctest.CtxWithUser(1)
	id := "663d566a-65e7-4996-928c-fa91f4f24fa6"
	doc := appDocument([]string{"record:read", "record:write"})
	policy := object{"version": "cloud-hosted-app-stream/v1", "status": "active", "platformPermissions": []string{"stream.requests.read", "stream.requests.invoke"}}
	doc["streamInteraction"] = policy
	// Persisted policy is a fixture until the dedicated policy management API is
	// connected. Credential creation and all business reads use real handlers.
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, a.svc.App.Store.UnsDB().Create(&repo.UnsMeta{Kind: "app", ID: id, Revision: 1, Data: raw}).Error)
	require.NoError(t, a.svc.App.Store.CommonDB().Create(&repo.AppInstallation{AppID: id, ReleaseID: "release", Manifest: json.RawMessage(`{}`), Stage: "ready", Status: "running", DesiredState: "running"}).Error)
	credentialRoot := t.TempDir()
	if runtime.GOOS == "windows" {
		// Match the native installer's private parent ACL. Windows ignores the
		// POSIX mode passed to MkdirAll, so an ordinary temp directory is public.
		owner, err := user.Current()
		require.NoError(t, err)
		require.NoError(t, exec.Command("icacls.exe", credentialRoot, "/inheritance:r", "/grant:r", "*"+owner.Uid+":(OI)(CI)F", "*S-1-5-18:(OI)(CI)F").Run())
	}
	credentials, err := appinstall.NewCredentials(credentialRoot, "native-policy-test-credential-secret", "http://127.0.0.1", a.svc.App.UnsV2)
	require.NoError(t, err)
	// Use the actual native installation credential store, not just its HTTP
	// counterpart. A candidate can prepare its encrypted environment first.
	installer := contextx.WithSubject(ctx, contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}, "uns.app.manage": {}, "uns.app.credentials": {}, "stream.requests.read": {}, "stream.requests.invoke": {}}})
	require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Update("status", "starting").Error)
	require.NoError(t, credentials.Ensure(installer, id))
	environment, err := credentials.Environment(installer, id)
	require.NoError(t, err)
	require.True(t, environment["TIER0_API_KEY"] != "")
	require.Equal(t, id, environment["TIER0_UNS_APP_ID"])
	require.NoError(t, credentials.Ensure(installer, id), "replacement reuses the persisted credential")
	require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Update("status", "running").Error)
	_, manager, err := repo.NewAPIKeyRepo(ctx).CreateAPIKey(ctx, "policy-issuer", 1, 1, "full_access", "external", "personal", []string{"uns.read", "uns.write", "uns.app.manage", "uns.app.credentials", "stream.requests.read", "stream.requests.invoke"})
	require.NoError(t, err)
	a.post(a.admin, "apps/keys/create", object{"appId": id, "name": "insufficient"}, 403)
	created := a.post(manager, "apps/keys/create", object{"appId": id, "name": "independent-policy"}, 200)
	key := created["apiKey"].(string)
	require.ElementsMatch(t, []any{"uns.read", "uns.write", "stream.requests.read", "stream.requests.invoke"}, created["resourceKeys"])
	value := a.post(key, "apps/context", object{}, 200)
	require.Len(t, value["collections"], 1)
	require.ElementsMatch(t, []any{"stream.requests.read", "stream.requests.invoke"}, value["platformPermissions"])
	a.post(key, "records/find", object{"collectionId": "orders"}, 200)
	a.svc.App.SourceStream = sourcestream.New(nil, scripttest.New(t), "")
	a.svc.App.UnsV2.SetSourceRuntime(a.svc.App.SourceStream)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /openapi/v2/stream-requests/discover", a.svc.ApiKeyAuth(requesthandlers.StreamRequestDiscoverHandler(a.svc)))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	discover := func(status int) {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, server.URL+"/openapi/v2/stream-requests/discover", strings.NewReader(`{}`))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-API-Key", key)
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		defer response.Body.Close()
		require.Equal(t, status, response.StatusCode)
	}
	discover(200)
	// An ordinary replacement omitting the independent policy preserves it,
	// even when the data manager lacks all Stream permissions.
	delete(doc, "streamInteraction")
	doc["status"] = "disabled"
	a.apply(a.admin, 200, object{"opId": "data-disable", "action": "meta.replace", "kind": "app", "id": id, "expectedRevision": 1, "data": doc})
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, id, false, "app")
	require.NoError(t, err)
	var stored unsv2.AppData
	require.NoError(t, json.Unmarshal(row.Data, &stored))
	require.NotNil(t, stored.StreamInteraction)
	require.Equal(t, "active", stored.StreamInteraction.Status)
	a.post(key, "records/find", object{"collectionId": "orders"}, 403)
	discover(200)
	value = a.post(key, "apps/context", object{}, 200)
	require.Empty(t, value["collections"])
	require.Len(t, value["platformPermissions"], 2)
	streamOnly := a.post(manager, "apps/keys/create", object{"appId": id, "name": "stream-only"}, 200)
	require.NotContains(t, streamOnly["resourceKeys"], "uns.write")
	doc["streamInteraction"] = object{"version": "cloud-hosted-app-stream/v1", "status": "disabled", "platformPermissions": []string{}}
	a.apply(a.admin, 403, object{"opId": "illegal-revoke", "action": "meta.replace", "kind": "app", "id": id, "expectedRevision": 2, "data": doc})
	// Stream revocation leaves Collection authorization intact; top-level
	// legacy capabilities cannot override the explicit independent policy.
	doc["status"] = "active"
	doc["platformPermissions"] = []string{"stream.requests.read"}
	writeFixture := func() {
		t.Helper()
		raw, err := json.Marshal(doc)
		require.NoError(t, err)
		require.NoError(t, a.svc.App.Store.UnsDB().Model(&repo.UnsMeta{}).Where("kind='app' AND id=?", id).Update("data", raw).Error)
	}
	writeFixture()
	discover(403)
	a.post(key, "records/find", object{"collectionId": "orders"}, 200)
	value = a.post(key, "apps/context", object{}, 200)
	require.Empty(t, value["platformPermissions"])
	for _, invalid := range []any{
		nil,
		object{"version": "cloud-hosted-app-stream/v1", "status": "active", "platformPermissions": []string{"stream.requests.write"}},
		object{"version": "cloud-hosted-app-stream/v1", "status": "active", "platformPermissions": []string{"stream.requests.read", "stream.requests.read"}},
		object{"version": "cloud-hosted-app-stream/v1", "status": "disabled", "platformPermissions": []string{"stream.requests.read"}},
		object{"version": "cloud-hosted-app-stream/v1", "status": "active", "PlatformPermissions": []string{"stream.requests.read"}},
		object{"version": "cloud-hosted-app-stream/v1", "status": "active", "platformPermissions": []string{"control.execute"}},
	} {
		doc["streamInteraction"] = invalid
		writeFixture()
		discover(403)
		a.post(manager, "apps/keys/create", object{"appId": id, "name": "invalid-policy"}, 403)
	}
	doc["streamInteraction"] = policy
	writeFixture()
	// An external App cannot copy a native profile to obtain its authority.
	raw, err = json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, a.svc.App.Store.UnsDB().Model(&repo.UnsMeta{}).Where("kind='app' AND id='maintenance'").Update("data", raw).Error)
	a.post(manager, "apps/keys/create", object{"appId": "maintenance", "name": "forged-profile"}, 403)
	require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Update("stage", "uninstalled").Error)
	a.post(manager, "apps/keys/create", object{"appId": id, "name": "uninstalled"}, 403)
	require.NoError(t, credentials.Revoke(installer, id))
	_, err = credentials.Environment(installer, id)
	require.Error(t, err)
}

func TestNativeAppCredentialRefresh(t *testing.T) {
	a := newAppAPI(t)
	a.provision()
	ctx := svctest.CtxWithUser(1)
	id := "ff37589e-b27c-4d9a-932a-8153d7209dd5"
	doc := appDocument([]string{"record:read", "record:write"})
	doc["streamInteraction"] = object{"version": "cloud-hosted-app-stream/v1", "status": "active", "platformPermissions": []string{"stream.requests.read"}}
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, a.svc.App.Store.UnsDB().Create(&repo.UnsMeta{Kind: "app", ID: id, Revision: 1, Data: raw}).Error)
	require.NoError(t, a.svc.App.Store.CommonDB().Create(&repo.AppInstallation{AppID: id, ReleaseID: "release", Manifest: json.RawMessage(`{}`), Stage: "ready", Status: "running", DesiredState: "running"}).Error)
	root := t.TempDir()
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Chmod(root, 0o700))
	}
	if runtime.GOOS == "windows" {
		owner, err := user.Current()
		require.NoError(t, err)
		require.NoError(t, exec.Command("icacls.exe", root, "/inheritance:r", "/grant:r", "*"+owner.Uid+":(OI)(CI)F", "*S-1-5-18:(OI)(CI)F").Run())
	}
	credentials, err := appinstall.NewCredentials(root, "native-refresh-test-credential-secret", "http://127.0.0.1", a.svc.App.UnsV2)
	require.NoError(t, err)
	actor, err := a.svc.App.Auth.RefreshSubject(ctx, contextx.Subject{UserID: 1, AuthType: "session"})
	require.NoError(t, err)
	manager := contextx.WithSubject(ctx, actor)
	require.NoError(t, credentials.Ensure(manager, id))
	environment, err := credentials.Environment(manager, id)
	require.NoError(t, err)
	oldKey := environment["TIER0_API_KEY"]
	keys := repo.NewAPIKeyRepo(ctx)
	old, err := keys.ValidateAPIKeyDetail(ctx, oldKey)
	require.NoError(t, err)
	require.Contains(t, old.ResourceKeys, "stream.requests.read")
	require.NotContains(t, old.ResourceKeys, "stream.requests.invoke")
	service, err := appinstall.New(ctx, appinstall.Options{Root: root, Repository: repo.NewAppInstallationRepo(ctx), Environment: credentials.Environment, Validate: func(ctx context.Context, appID string) error {
		_, err := repo.NewUnsMetaRepo(ctx).Get(ctx, appID, false, "app")
		return err
	}})
	require.NoError(t, err)
	t.Cleanup(service.Close)
	management, err := appinstall.NewManagement(ctx, service, credentials, a.svc.App.UnsV2, a.svc.App.Asset, root)
	require.NoError(t, err)
	t.Cleanup(management.Close)
	activeKeys := func() int64 {
		var count int64
		require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.APIKey{}).Where("app_id=? AND deleted_time=0", id).Count(&count).Error)
		return count
	}
	request, _ := json.Marshal(object{"appId": id, "expectedRevision": 1, "status": "active", "platformPermissions": []string{"stream.requests.read", "stream.requests.invoke", "stream.requests.write"}, "idempotencyKey": "refresh-policy", "dryRun": false})
	_, err = a.svc.App.UnsV2.Execute(manager, "appStreamPolicyUpdate", request)
	require.NoError(t, err)
	require.NoError(t, credentials.Ensure(manager, id))
	environment, err = credentials.Environment(manager, id)
	require.NoError(t, err)
	require.True(t, oldKey == environment["TIER0_API_KEY"], "ordinary Ensure must not rotate or expand the saved credential")
	require.Error(t, management.Action(manager, id, "refreshCredentials"), "running App cannot rotate")
	require.EqualValues(t, 1, activeKeys())
	require.NoError(t, management.Action(manager, id, "stop"))
	require.NoError(t, a.svc.App.Store.CommonDB().Exec("UPDATE sys_user_info SET status=0 WHERE user_id=1").Error)
	err = management.Action(manager, id, "refreshCredentials")
	require.NoError(t, a.svc.App.Store.CommonDB().Exec("UPDATE sys_user_info SET status=1 WHERE user_id=1").Error)
	require.Error(t, err, "cached manager subject must not outlive the actual account authority")
	require.EqualValues(t, 1, activeKeys())
	for _, permissions := range [][]string{{"uns.app.manage"}, {"uns.app.manage", "uns.app.credentials", "uns.read", "uns.write", "stream.requests.read"}} {
		scopes := map[string]struct{}{}
		for _, permission := range permissions {
			scopes[permission] = struct{}{}
		}
		limited := contextx.WithSubject(ctx, contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: scopes})
		require.Error(t, management.Action(limited, id, "refreshCredentials"))
		_, err := keys.ValidateAPIKeyDetail(ctx, oldKey)
		require.NoError(t, err, "insufficient issuance authority must preserve the old key")
		require.EqualValues(t, 1, activeKeys())
	}
	for _, change := range []object{{"operation_id": "installation"}, {"stage": "uninstalled"}, {"desired_state": "running"}} {
		require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Updates(change).Error)
		require.Error(t, management.Action(manager, id, "refreshCredentials"))
		require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Updates(object{"operation_id": "", "stage": "ready", "desired_state": "stopped"}).Error)
	}
	require.NoError(t, management.Action(manager, id, "refreshCredentials"))
	_, err = keys.ValidateAPIKeyDetail(ctx, oldKey)
	require.Error(t, err)
	environment, err = credentials.Environment(manager, id)
	require.NoError(t, err)
	currentKey := environment["TIER0_API_KEY"]
	require.True(t, oldKey != currentKey)
	current, err := keys.ValidateAPIKeyDetail(ctx, currentKey)
	require.NoError(t, err)
	require.Contains(t, current.ResourceKeys, "stream.requests.write")
	require.Contains(t, current.ResourceKeys, "uns.write", "data scopes survive rotation")
	require.EqualValues(t, 1, activeKeys())
	a.post(currentKey, "records/find", object{"collectionId": "orders"}, 200)
	// A real filesystem failure after database revocation must not retain the
	// candidate Key. Restoring storage and explicitly retrying creates a new key.
	credentialPath := filepath.Join(root, id, "credential.enc")
	backupPath := filepath.Join(root, id, "previous.enc")
	injected := false
	callback := "test:native_credential_replace_failure"
	require.NoError(t, a.svc.App.Store.CommonDB().Callback().Update().After("gorm:commit_or_rollback_transaction").Register(callback, func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]interface{})
		if injected || tx.Statement.Table != "sys_api_key" || !ok || values["deleted_time"] == nil || tx.Error != nil {
			return
		}
		injected = true
		require.NoError(t, os.Rename(credentialPath, backupPath))
		require.NoError(t, os.Mkdir(credentialPath, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(credentialPath, "blocked"), []byte("test"), 0600))
	}))
	err = management.Action(manager, id, "refreshCredentials")
	require.NoError(t, a.svc.App.Store.CommonDB().Callback().Update().Remove(callback))
	require.Error(t, err)
	require.True(t, injected)
	require.Zero(t, activeKeys())
	_, err = credentials.Environment(manager, id)
	require.Error(t, err)
	row, err := repo.NewAppInstallationRepo(ctx).Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "stopped", row.Status)
	require.NoError(t, os.Remove(filepath.Join(credentialPath, "blocked")))
	require.NoError(t, os.Remove(credentialPath))
	require.NoError(t, os.Rename(backupPath, credentialPath))
	require.NoError(t, management.Action(manager, id, "refreshCredentials"))
	require.EqualValues(t, 1, activeKeys())
	_, err = credentials.Environment(manager, id)
	require.NoError(t, err)
	_, err = keys.ValidateAPIKeyDetail(ctx, currentKey)
	require.Error(t, err)
}

func TestNativeAppStreamPolicyManagement(t *testing.T) {
	a := newAppAPI(t)
	a.provision()
	ctx := svctest.CtxWithUser(1)
	id := "abbd3425-502c-47b4-9b2f-cfda1bb215eb"
	doc := appDocument([]string{"record:read", "record:write", "record:delete"})
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, a.svc.App.Store.UnsDB().Create(&repo.UnsMeta{Kind: "app", ID: id, Revision: 1, Data: raw}).Error)
	require.NoError(t, a.svc.App.Store.CommonDB().Create(&repo.AppInstallation{AppID: id, Name: "Policy App", ReleaseID: "release", Manifest: json.RawMessage(`{}`), Stage: "ready", Status: "running", DesiredState: "running"}).Error)
	newKey := func(name string, resources []string) (repo.APIKey, string) {
		t.Helper()
		row, key, err := repo.NewAPIKeyRepo(ctx).CreateAPIKey(ctx, name, 1, 1, "full_access", "external", "personal", resources)
		require.NoError(t, err)
		return row, key
	}
	base := []string{"uns.write", "stream.requests.manage"}
	requestScopes := append(append([]string{}, base...), "stream.requests.read", "stream.requests.invoke")
	managerRow, manager := newKey("policy-manager", append(append([]string{}, requestScopes...), "stream.messages.publish", "stream.messages.subscribe"))
	_, requestManager := newKey("request-manager", requestScopes)
	_, revoker := newKey("policy-revoker", base)
	get := func(key string) object { return a.post(key, "app-stream-policy/get", object{"appId": id}, 200) }
	request := func(revision int, status, key string, permissions ...string) object {
		return object{"appId": id, "expectedRevision": revision, "status": status, "platformPermissions": permissions, "idempotencyKey": key, "dryRun": false}
	}
	a.post(a.admin, "app-stream-policy/get", object{"appId": id}, 403)
	view := get(manager)
	require.Equal(t, "disabled", view["status"])
	require.Equal(t, "available", view["authorityStatus"])
	require.EqualValues(t, 1, view["revision"])
	a.post(manager, "app-stream-policy/update", request(1, "active", "invalid-write", "stream.requests.write"), 422)
	invalidTopic := request(1, "active", "invalid-topic", "stream.messages.publish")
	invalidTopic["messageTopics"] = []object{{"topic": "orders/#", "publish": true, "subscribe": false}}
	a.post(manager, "app-stream-policy/update", invalidTopic, 422)
	first := request(1, "active", "first-policy", "stream.requests.read")
	first["dryRun"] = true
	preview := a.post(manager, "app-stream-policy/update", first, 200)
	require.Equal(t, "validated", preview["status"])
	require.EqualValues(t, 1, get(manager)["revision"])
	first["dryRun"] = false
	committed := a.post(manager, "app-stream-policy/update", first, 200)
	require.Equal(t, "committed", committed["status"])
	require.EqualValues(t, 2, get(manager)["revision"])
	// Another policy/metadata update must not alter the first request's digest.
	doc["name"] = "Renamed App"
	a.apply(a.admin, 200, object{"opId": "rename", "action": "meta.replace", "kind": "app", "id": id, "expectedRevision": 2, "data": doc})
	replayed := a.post(manager, "app-stream-policy/update", first, 200)
	require.Equal(t, true, replayed["replayed"])
	require.Equal(t, committed["results"], replayed["results"])
	require.EqualValues(t, 3, get(manager)["revision"])
	conflict := request(1, "active", "first-policy", "stream.requests.invoke")
	a.post(manager, "app-stream-policy/update", conflict, 409)
	a.post(manager, "app-stream-policy/update", request(1, "active", "stale", "stream.requests.read"), 409)
	a.post(revoker, "app-stream-policy/update", request(3, "active", "expansion", "stream.requests.read", "stream.requests.invoke"), 403)
	require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Update("status", "stopped").Error)
	a.post(revoker, "app-stream-policy/update", request(3, "disabled", "offline-revoke", []string{}...), 200)
	a.post(manager, "app-stream-policy/update", request(4, "active", "offline-reactivate", "stream.requests.read"), 503)
	require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Update("status", "running").Error)
	message := request(4, "active", "message-policy", "stream.requests.read", "stream.messages.publish")
	message["messageTopics"] = []object{{"topic": "orders/changed", "publish": true, "subscribe": false}}
	a.post(manager, "app-stream-policy/update", message, 200)
	keepTopics := request(5, "active", "request-only-change", "stream.requests.read", "stream.requests.invoke", "stream.messages.publish")
	a.post(requestManager, "app-stream-policy/update", keepTopics, 200)
	require.Equal(t, []any{object{"topic": "orders/changed", "publish": true, "subscribe": false}}, get(manager)["messageTopics"])
	clearTopics := request(6, "active", "clear-topics", "stream.requests.read", "stream.requests.invoke", "stream.messages.publish")
	clearTopics["messageTopics"] = []object{}
	a.post(requestManager, "app-stream-policy/update", clearTopics, 403)
	a.post(manager, "app-stream-policy/update", clearTopics, 200)
	require.Empty(t, get(manager)["messageTopics"])
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, id, false, "app")
	require.NoError(t, err)
	var stored unsv2.AppData
	require.NoError(t, json.Unmarshal(row.Data, &stored))
	require.Equal(t, "Renamed App", stored.Name)
	require.Equal(t, []string{"record:read", "record:write", "record:delete"}, stored.Collections[0].Permissions)
	// Replay must refresh the actual key record, not trust its old scopes.
	_, err = repo.NewAPIKeyRepo(ctx).UpdateAPIKey(ctx, managerRow.ID, 1, "policy-manager", "full_access", []string{"uns.write"})
	require.NoError(t, err)
	a.post(manager, "app-stream-policy/update", first, 403)
	// Message scopes are opt-in and do not silently expand default key profiles.
	require.NotContains(t, repo.APIKeyResourceKeysForPermission("full_access"), "stream.messages.publish")
	// A real native installation may attach its first policy at revision zero;
	// callers cannot use zero as an unconditional overwrite of an existing row.
	newID := "c119a0cc-3920-483a-8bce-f761b666e447"
	require.NoError(t, a.svc.App.Store.CommonDB().Create(&repo.AppInstallation{AppID: newID, Name: "New policy", ReleaseID: "release", Manifest: json.RawMessage(`{}`), Status: "running", DesiredState: "running", Stage: "ready"}).Error)
	view = a.post(requestManager, "app-stream-policy/get", object{"appId": newID}, 200)
	require.EqualValues(t, 0, view["revision"])
	attach := request(0, "active", "attach", "stream.requests.read")
	attach["appId"] = newID
	a.post(requestManager, "app-stream-policy/update", attach, 200)
	attach["idempotencyKey"] = "overwrite"
	a.post(requestManager, "app-stream-policy/update", attach, 409)
	// Loss of the runtime fact must not prevent removal of persisted grants.
	require.NoError(t, a.svc.App.Store.CommonDB().Where("app_id=?", newID).Delete(&repo.AppInstallation{}).Error)
	view = a.post(requestManager, "app-stream-policy/get", object{"appId": newID}, 200)
	require.Equal(t, "notFound", view["authorityStatus"])
	remove := request(1, "disabled", "remove-missing", []string{}...)
	remove["appId"] = newID
	a.post(requestManager, "app-stream-policy/update", remove, 200)
}

func TestNativeAppStreamPolicyRechecksBeforeCommit(t *testing.T) {
	for _, boundary := range []string{"key-scope", "installation"} {
		t.Run(boundary, func(t *testing.T) {
			a := newAppAPI(t)
			ctx := svctest.CtxWithUser(1)
			id := "c1821079-62de-496b-b02d-815f0363db1f"
			doc := object{"schemaVersion": 1, "name": "Commit boundary", "status": "active", "roles": []any{}, "members": []any{}, "collections": []any{}, "navigation": object{"topicIds": []any{}}}
			raw, err := json.Marshal(doc)
			require.NoError(t, err)
			require.NoError(t, a.svc.App.Store.UnsDB().Create(&repo.UnsMeta{Kind: "app", ID: id, Revision: 1, Data: raw}).Error)
			require.NoError(t, a.svc.App.Store.CommonDB().Create(&repo.AppInstallation{AppID: id, Name: "Commit boundary", ReleaseID: "release", Manifest: json.RawMessage(`{}`), Status: "running", DesiredState: "running", Stage: "ready"}).Error)
			scopes := []string{"uns.write", "stream.requests.manage", "stream.requests.read"}
			manager, key, err := repo.NewAPIKeyRepo(ctx).CreateAPIKey(ctx, "commit-manager", 1, 1, "full_access", "external", "personal", scopes)
			require.NoError(t, err)
			revoked := false
			// Revoke actual persisted authority once the uncommitted new policy is
			// visible. All authentication still runs through the real auth service.
			a.svc.App.UnsV2 = unsv2.New(a.svc.App.UnsMeta, func(check context.Context, actor contextx.Subject) (contextx.Subject, error) {
				row, err := repo.NewUnsMetaRepo(check).Get(check, id, false, "app")
				if err != nil {
					return contextx.Subject{}, err
				}
				if row.Revision == 2 && !revoked {
					revoked = true
					if boundary == "key-scope" {
						_, err = repo.NewAPIKeyRepo(context.Background()).UpdateAPIKey(context.Background(), manager.ID, 1, "commit-manager", "full_access", scopes[:2])
					} else {
						err = a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Update("status", "stopped").Error
					}
					if err != nil {
						return contextx.Subject{}, err
					}
				}
				return a.svc.App.Auth.RefreshSubject(check, actor)
			})
			request := object{"appId": id, "expectedRevision": 1, "status": "active", "platformPermissions": []string{"stream.requests.read"}, "idempotencyKey": "commit-check", "dryRun": false}
			expectedStatus := http.StatusForbidden
			if boundary == "installation" {
				expectedStatus = http.StatusServiceUnavailable
			}
			a.post(key, "app-stream-policy/update", request, expectedStatus)
			require.True(t, revoked, "the test must reach the post-write authorization check")
			row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, id, false, "app")
			require.NoError(t, err)
			require.EqualValues(t, 1, row.Revision, "failed commit checks roll back policy and receipt together")
			_, err = repo.NewAPIKeyRepo(ctx).UpdateAPIKey(ctx, manager.ID, 1, "commit-manager", "full_access", scopes)
			require.NoError(t, err)
			require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Update("status", "running").Error)
			result := a.post(key, "app-stream-policy/update", request, 200)
			require.Equal(t, false, result["replayed"])
		})
	}
}

func (a *appAPI) provision() (string, string) {
	recordSchema := object{"type": "object", "required": []string{"title", "priority", "done"}, "additionalProperties": false, "properties": object{
		"title": object{"type": "string"}, "priority": object{"type": "number"}, "quantity": object{"type": "integer"}, "done": object{"type": "boolean"}, "note": object{"type": "string"}, "site": object{"type": "object", "properties": object{"name": object{"type": "string"}}},
	}}
	for _, id := range []string{"orders", "private"} {
		a.apply(a.admin, 200, object{"opId": id, "action": "meta.create", "kind": "collection", "id": id, "data": object{"schemaVersion": 1, "name": id, "recordSchema": recordSchema, "sampleSchema": object{"type": "object", "required": []string{"temperature"}, "properties": object{"temperature": object{"type": "number"}}}}})
	}
	a.apply(a.admin, 200, object{"opId": "app", "action": "meta.create", "kind": "app", "id": "maintenance", "data": appDocument([]string{"record:read", "record:write", "record:delete", "sample:read", "sample:write"})})
	key := a.post(a.admin, "apps/keys/create", object{"appId": "maintenance", "name": "maintenance-server"}, 200)
	return key["apiKey"].(string), key["keyId"].(string)
}

func order(id string, priority int, done bool) object {
	return object{"opId": id, "action": "record.create", "id": id, "collectionId": "orders", "data": object{"title": "Pump " + id, "priority": priority, "done": done, "site": object{"name": "Factory A"}}}
}

func TestCloudCanonicalWriteReceipts(t *testing.T) {
	a := newAppAPI(t)
	key, _ := a.provision()
	create := object{"collectionId": "orders", "id": "receipt", "idempotencyKey": "cloud-insert", "data": object{"title": "Original", "priority": 1, "done": false}, "returnDocument": "after", "projection": object{"title": 1}}
	created := a.post(key, "records/insert-one", create, 200)
	require.Equal(t, "receipt", created["insertedId"])
	require.Equal(t, object{"title": "Original"}, created["value"].(map[string]any)["data"])
	change := object{"collectionId": "orders", "filter": object{"_id": "receipt"}, "update": object{"$set": object{"title": "Changed"}}, "returnDocument": "before", "idempotencyKey": "cloud-update"}
	updated := a.post(key, "records/update-one", change, 200)
	require.EqualValues(t, 1, updated["matchedCount"])
	require.Equal(t, "Original", updated["value"].(map[string]any)["data"].(map[string]any)["title"])
	replayed := a.post(key, "records/insert-one", create, 200)
	require.Equal(t, true, replayed["replayed"])
	require.Equal(t, created["value"], replayed["value"], "insert replay retains original projection and snapshot")
	replayed = a.post(key, "records/update-one", change, 200)
	require.Equal(t, updated["value"], replayed["value"])
	missing := a.post(key, "records/delete-one", object{"collectionId": "orders", "filter": object{"_id": "absent"}, "returnDocument": "before"}, 200)
	require.Contains(t, missing, "value")
	require.Nil(t, missing["value"])
	require.EqualValues(t, 0, missing["deletedCount"])
	a.post(key, "records/delete-one", object{"collectionId": "orders", "filter": object{"_id": "receipt"}, "returnDocument": "after"}, 422)
	a.post(key, "records/update-one", object{"collectionId": "orders", "filter": object{"_id": "receipt"}, "update": object{"$set": object{"priority": 99}}, "projection": object{"title": 1}}, 422)
	preview := a.post(key, "records/update-one", object{"collectionId": "orders", "filter": object{"_id": "receipt"}, "update": object{"$set": object{"title": "Preview"}}, "returnDocument": "after", "dryRun": true}, 200)
	require.Equal(t, "validated", preview["status"])
	require.Equal(t, "Preview", preview["value"].(map[string]any)["data"].(map[string]any)["title"])
	removed := a.post(key, "records/delete-one", object{"collectionId": "orders", "filter": object{"_id": "receipt"}, "returnDocument": "before"}, 200)
	require.Equal(t, "Changed", removed["value"].(map[string]any)["data"].(map[string]any)["title"])
	require.EqualValues(t, 1, removed["deletedCount"])
	// Write-only App grants must not gain read permission through write receipts.
	a.apply(a.admin, 200, object{"opId": "restrict", "action": "meta.replace", "kind": "app", "id": "maintenance", "expectedRevision": 1, "data": appDocument([]string{"record:write"})})
	a.post(key, "records/insert-one", object{"collectionId": "orders", "id": "private-receipt", "data": object{"title": "Denied", "priority": 1, "done": false}, "returnDocument": "after"}, 403)
	a.post(key, "records/insert-one", object{"collectionId": "orders", "id": "private-receipt", "data": object{"title": "Allowed", "priority": 1, "done": false}}, 200)
}

func TestAppHTTPDataLifecycle(t *testing.T) {
	a := newAppAPI(t)
	key, keyID := a.provision()
	ctx := a.post(key, "apps/context", object{}, 200)
	require.Equal(t, "maintenance", ctx["appId"])
	require.NotContains(t, ctx, "apiKey")
	batch := object{"idempotencyKey": "first-orders", "mode": "sync", "items": []object{order("b", 10, false), order("a", 2, true), order("c", 0, false)}}
	a.post(key, "records:transact", batch, 200)
	require.Equal(t, true, a.post(key, "records:transact", batch, 200)["replayed"])
	find := a.post(key, "records/find", object{"collectionId": "orders", "filter": object{"priority": object{"$gte": 2}, "_meta.createdAt": object{"$gt": 0}}, "sort": []any{[]any{"priority", 1}}}, 200)
	items := find["items"].([]any)
	require.Len(t, items, 2)
	require.Equal(t, "a", items[0].(map[string]any)["id"])
	scoped := a.post(key, "collections/orders/records:find", object{"filter": object{"_id": "a"}}, 200)
	require.Len(t, scoped["items"], 1)
	a.post(key, "collections/orders/records:find", object{"collectionId": "private"}, 422)
	a.post(key, "records/find", object{"collectionId": "orders", "filter": object{"data.priority": 2}}, 422)
	projected := object{"collectionId": "orders", "projection": object{"title": 1}, "limit": 1}
	page := a.post(key, "records/find", projected, 200)
	require.NotContains(t, page["items"].([]any)[0].(map[string]any)["data"], "priority")
	projected["cursor"] = page["pageInfo"].(map[string]any)["nextCursor"]
	require.NotEmpty(t, projected["cursor"])
	a.post(key, "records/find", projected, 200)
	projected["projection"] = object{"priority": 1}
	a.post(key, "records/find", projected, 422)
	write := object{"collectionId": "orders", "filter": object{"_id": "d"}, "update": object{"$setOnInsert": object{"title": "Pump d", "priority": 3, "done": false}}, "upsert": true, "idempotencyKey": "upsert-d"}
	created := a.post(key, "records/update-one", write, 200)
	require.NotContains(t, created, "upsertedCount")
	require.Equal(t, "d", created["upsertedId"])
	require.Equal(t, true, a.post(key, "records/update-one", write, 200)["replayed"])
	delete(write, "idempotencyKey")
	unchanged := a.post(key, "records/update-one", write, 200)
	require.EqualValues(t, 1, unchanged["matchedCount"])
	require.EqualValues(t, 0, unchanged["modifiedCount"])
	edited := a.post(key, "records/update-one", object{"collectionId": "orders", "filter": object{"_id": "b"}, "update": object{"$inc": object{"priority": 1}}, "returnDocument": "after"}, 200)
	require.EqualValues(t, 11, edited["value"].(map[string]any)["data"].(map[string]any)["priority"])
	lease := a.post(key, "collections/orders/records:lock", object{"ids": []string{"b"}, "label": "claim-order"}, 200)["locks"].([]any)[0].(map[string]any)
	require.Equal(t, "b", lease["record"].(map[string]any)["id"])
	require.NotContains(t, lease["record"].(map[string]any)["lock"], "token")
	token := lease["token"].(string)
	a.post(key, "records/update-one", object{"collectionId": "orders", "filter": object{"_id": "b"}, "update": object{"$inc": object{"priority": 1}}}, 409)
	a.post(key, "collections/orders/records:renew", object{"locks": []object{{"id": "b", "token": "wrong"}}}, 409)
	a.post(key, "collections/orders/records:renew", object{"locks": []object{{"id": "b", "token": token}}, "ttlMs": 60000}, 200)
	read := a.post(key, "records:transact", object{"assertLocks": []object{{"collectionId": "orders", "id": "b", "token": token}}, "items": []object{{"opId": "read", "action": "record.read", "collectionId": "orders", "id": "b"}}}, 200)["results"].([]any)[0].(map[string]any)
	require.Equal(t, "b", read["document"].(map[string]any)["id"])
	a.post(key, "collections/orders/records:unlock", object{"ids": []string{"b"}, "force": true}, 403)
	guarded := a.post(key, "records/update-one", object{"collectionId": "orders", "filter": object{"_id": "b"}, "update": object{"$inc": object{"priority": 1}}, "lockToken": token, "release": true, "returnDocument": "after"}, 200)
	require.NotContains(t, guarded["value"].(map[string]any), "lock")
	lease = a.post(key, "collections/orders/records:lock", object{"ids": []string{"b"}}, 200)["locks"].([]any)[0].(map[string]any)
	require.EqualValues(t, 1, a.post(key, "collections/orders/records:unlock", object{"locks": []object{{"id": "b", "token": lease["token"]}}}, 200)["unlockedCount"])
	lease = a.post(key, "collections/orders/records:lock", object{"ids": []string{"b"}}, 200)["locks"].([]any)[0].(map[string]any)
	permissions := map[string]struct{}{"uns.read": {}, "uns.write": {}, "uns.app.manage": {}}
	forceRaw := []byte(`{"collectionId":"orders","ids":["b"],"force":true}`)
	_, forceErr := a.svc.App.UnsV2.Execute(contextx.WithSubject(context.Background(), contextx.Subject{UserID: 2, AuthType: "session", ResourceKeys: permissions}), "recordUnlock", forceRaw)
	require.Error(t, forceErr, "a custom permission grant must not imply administrator")
	var forceFault *unsv2.Fault
	require.ErrorAs(t, forceErr, &forceFault)
	require.Equal(t, "FORBIDDEN", forceFault.Code)
	_, forceErr = a.svc.App.UnsV2.Execute(contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: permissions}), "recordUnlock", forceRaw)
	require.NoError(t, forceErr)
	firstDistinct := a.post(key, "records/aggregate-pipeline", object{"collectionId": "orders", "pipeline": []object{{"$group": object{"_id": "$priority"}}, {"$sort": []any{[]any{"_id", 1}}}}, "page": object{"size": 1, "page": 1}}, 200)
	secondDistinct := a.post(key, "records/aggregate-pipeline", object{"collectionId": "orders", "pipeline": []object{{"$group": object{"_id": "$priority"}}, {"$sort": []any{[]any{"_id", 1}}}}, "page": object{"size": 1, "page": 2}}, 200)
	require.NotEqual(t, firstDistinct["items"], secondDistinct["items"])
	a.post(key, "records/insert-one", object{"collectionId": "orders", "id": "snapshot", "data": object{"title": "Snapshot before", "priority": 1, "done": false}}, 200)
	replaced := a.post(key, "records/replace-one", object{"collectionId": "orders", "filter": object{"_id": "snapshot"}, "replacement": object{"title": "Snapshot after", "priority": 2, "done": false}, "returnDocument": "before"}, 200)
	require.Equal(t, "Snapshot before", replaced["value"].(map[string]any)["data"].(map[string]any)["title"])
	removed := a.post(key, "records/delete-one", object{"collectionId": "orders", "filter": object{"_id": "snapshot"}, "returnDocument": "before"}, 200)
	require.Equal(t, "Snapshot after", removed["value"].(map[string]any)["data"].(map[string]any)["title"])
	upsertBefore := a.post(key, "records/update-one", object{"collectionId": "orders", "filter": object{"_id": "upsert-before"}, "upsert": true, "returnDocument": "before", "update": object{"$setOnInsert": object{"title": "Upsert before", "priority": 1, "done": false}}}, 200)
	require.Nil(t, upsertBefore["value"])
	require.Equal(t, "upsert-before", upsertBefore["upsertedId"])
	absent := a.post(key, "records/delete-one", object{"collectionId": "orders", "filter": object{"_id": "missing"}}, 200)
	require.EqualValues(t, 0, absent["deletedCount"])
	now := time.Now().UnixMilli()
	a.apply(key, 200, object{"opId": "reading", "action": "sample.append", "collectionId": "orders", "recordId": "b", "time": now, "data": object{"temperature": 24.5}})
	latest := a.post(key, "samples/latest-values", object{"collectionId": "orders", "recordIds": []string{"b", "c"}}, 200)
	series := latest["series"].([]any)
	require.NotNil(t, series[0].(map[string]any)["sample"])
	require.Nil(t, series[1].(map[string]any)["sample"])
	query := object{"collectionId": "orders", "recordIds": []string{"b"}, "from": now - 1000, "to": now + 1000}
	raw := a.post(key, "samples/query-series", query, 200)
	require.Len(t, raw["items"], 1)
	require.Equal(t, series[0].(map[string]any)["sample"], raw["items"].([]any)[0])
	a.apply(key, 200, object{"opId": "reading-c", "action": "sample.append", "collectionId": "orders", "recordId": "c", "time": now, "data": object{"temperature": 26}})
	multi := a.post(key, "samples/query-series", object{"collectionId": "orders", "recordIds": []string{"b", "c"}, "from": now - 1000, "to": now + 1000, "filter": object{"$or": []object{{"temperature": object{"$gt": 25}}, {"_meta.recordId": "b"}}}}, 200)
	require.Len(t, multi["items"], 2)
	aggregate := object{"collectionId": "orders", "recordIds": []string{"b", "c"}, "from": now - 1000, "to": now + 1000, "pipeline": []object{{"$match": object{"temperature": object{"$gt": 20}}}, {"$group": object{"_id": "$_meta.recordId", "maxTemperature": object{"$max": "$temperature"}}}}}
	buckets := a.post(key, "samples/aggregate", aggregate, 200)
	require.Len(t, buckets["items"], 2)
	a.apply(a.admin, 200, object{"opId": "unique", "action": "meta.patch", "kind": "collection", "id": "orders", "data": object{"indexes": []object{{"path": "title", "unique": true}}}})
	require.NoError(t, unsmeta.ReconcileQueryIndex(context.Background()))
	a.post(key, "records/insert-one", object{"collectionId": "orders", "id": "duplicate-title", "data": object{"title": "Pump a", "priority": 1, "done": false}}, 409)
	a.post(key, "records/update-one", object{"collectionId": "orders", "filter": object{"done": true}, "update": object{"$set": object{"priority": 2}}, "upsert": true}, 422)
	keyed := object{"collectionId": "orders", "filter": object{"title": "Unique title"}, "update": object{"$set": object{"priority": 4, "done": false}}, "upsert": true}
	require.NotEmpty(t, a.post(key, "records/update-one", keyed, 200)["upsertedId"])
	require.EqualValues(t, 1, a.post(key, "records/update-one", keyed, 200)["matchedCount"])
	concurrent := make(chan error, 4)
	for i := range 4 {
		go func(i int) {
			raw, err := json.Marshal(object{"collectionId": "orders", "filter": object{"title": "Concurrent unique title"}, "update": object{"$set": object{"priority": 4, "done": false}}, "upsert": true, "idempotencyKey": fmt.Sprintf("concurrent-unique-%d", i)})
			if err != nil {
				concurrent <- err
				return
			}
			request, err := http.NewRequest(http.MethodPost, a.server.URL+"/openapi/v2/uns/collections/orders/records:updateOne", bytes.NewReader(raw))
			if err != nil {
				concurrent <- err
				return
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-API-Key", key)
			response, err := a.server.Client().Do(request)
			if err != nil {
				concurrent <- err
				return
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				concurrent <- fmt.Errorf("concurrent upsert status %d", response.StatusCode)
				return
			}
			concurrent <- nil
		}(i)
	}
	for range 4 {
		require.NoError(t, <-concurrent)
	}
	require.EqualValues(t, 1, a.post(key, "records/count", object{"collectionId": "orders", "filter": object{"title": "Concurrent unique title"}}, 200)["count"])
	deleted := a.post(key, "records/delete-one", object{"collectionId": "orders", "filter": object{"_id": "b"}}, 200)
	require.EqualValues(t, 1, deleted["deletedCount"])
	count := a.post(key, "records/count", object{"collectionId": "orders", "filter": object{"_id": "b"}}, 200)
	require.EqualValues(t, 0, count["count"])
	delete(query, "bucket")
	delete(query, "fields")
	history := a.post(key, "samples/query-series", query, 200)
	require.Len(t, history["items"], 1)
	a.post(key, "records/insert-one", object{"collectionId": "orders", "id": "e", "data": object{"title": "Pump b", "priority": 0, "done": false}}, 200)
	a.post(key, "records/insert-one", object{"collectionId": "orders", "id": "b", "data": object{"title": "Revived b", "priority": 0, "done": false}}, 409)
	a.post(key, "collections/orders/records:restore", object{"ids": []string{"b", "a"}}, 409)
	a.post(key, "records/delete-one", object{"collectionId": "orders", "filter": object{"_id": "e"}}, 200)
	restored := a.post(key, "collections/orders/records:restore", object{"ids": []string{"b", "a"}}, 200)
	require.EqualValues(t, 1, restored["restoredCount"])
	bulk := a.post(key, "records/bulk-write", object{"collectionId": "orders", "ordered": false, "operations": []object{{"insertOne": object{"id": "a", "data": object{"title": "duplicate ID", "priority": 0, "done": false}}}, {"insertOne": object{"id": "f", "data": object{"title": "Pump f", "priority": 0, "done": false}}}}}, 200)
	require.EqualValues(t, 1, bulk["insertedCount"])
	require.Len(t, bulk["writeErrors"], 1)
	require.Equal(t, "f", bulk["insertedIds"].(map[string]any)["1"])
	bulkUpsert := object{"collectionId": "orders", "idempotencyKey": "bulk-upsert", "operations": []object{{"updateOne": object{"filter": object{"_id": "g"}, "upsert": true, "update": object{"$setOnInsert": object{"title": "Pump g", "priority": 0, "done": false}}}}}}
	upserted := a.post(key, "records/bulk-write", bulkUpsert, 200)
	require.EqualValues(t, 1, upserted["upsertedCount"])
	require.EqualValues(t, 0, upserted["insertedCount"])
	require.Equal(t, "g", upserted["upsertedIds"].(map[string]any)["0"])
	require.Equal(t, true, a.post(key, "records/bulk-write", bulkUpsert, 200)["replayed"])
	many := object{"collectionId": "orders", "filter": object{"done": false}, "update": object{"$set": object{"done": true}}, "idempotencyKey": "finish-orders"}
	changed := a.post(key, "records/update-many", many, 200)
	require.Greater(t, changed["modifiedCount"].(float64), float64(0))
	replay := a.post(key, "records/update-many", many, 200)
	require.Equal(t, changed["modifiedCount"], replay["modifiedCount"])
	require.Equal(t, true, replay["replayed"])
	deleteSamples := object{"collectionId": "orders", "recordIds": []string{"b", "c"}, "from": now - 1000, "to": now + 1000, "dryRun": true}
	require.EqualValues(t, 2, a.post(key, "samples/delete-range", deleteSamples, 200)["deletedCount"])
	deleteSamples["dryRun"] = false
	require.EqualValues(t, 2, a.post(key, "samples/delete-range", deleteSamples, 200)["deletedCount"])
	require.Empty(t, a.post(key, "samples/query-series", query, 200)["items"])
	a.post(a.admin, "apps/keys/revoke", object{"appId": "maintenance", "keyId": keyID}, 200)
	a.post(key, "apps/context", object{}, 401)
}

func TestAppHTTPAuthorization(t *testing.T) {
	a := newAppAPI(t)
	key, _ := a.provision()
	a.query(key, "records/query", object{"collectionId": "private", "pipeline": []object{}}, 403)
	a.post(key, "metadata:find", object{"kind": "directory"}, 403)
	a.post(key, "metadata:find", object{"kind": "collection", "state": "deleted"}, 403)
	visible := a.post(key, "metadata:find", object{"kind": "collection"}, 200)["items"].([]any)
	require.Len(t, visible, 1)
	require.Equal(t, "orders", visible[0].(map[string]any)["id"])
	require.Empty(t, a.post(key, "metadata:find", object{"kind": "collection", "filter": object{"_id": "private"}, "limit": 1}, 200)["items"])
	foreign := order("foreign", 1, false)
	foreign["collectionId"] = "private"
	a.apply(key, 403, foreign)
	a.apply(key, 403, object{"opId": "escalate", "action": "meta.create", "kind": "collection", "data": object{}})
	a.post(key, "apps/keys/create", object{"appId": "maintenance", "name": "escalate"}, 403)
	a.post(key, "apps/context", object{"appId": "another"}, 422)
	_, err := a.svc.App.Auth.AuthenticateMQTTAPIKey(context.Background(), key)
	require.Error(t, err)
	request, _ := http.NewRequest("POST", a.server.URL+"/openapi/v1/uns/write", nil)
	request.Header.Set("X-API-Key", key)
	response, err := a.server.Client().Do(request)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, 403, response.StatusCode)
	// Same key, reduced live grant: cached credentials must not preserve write access.
	doc := appDocument([]string{"record:read"})
	a.apply(a.admin, 200, object{"opId": "restrict", "action": "meta.replace", "kind": "app", "id": "maintenance", "expectedRevision": 1, "data": doc})
	metadata := a.post(key, "metadata:find", object{"kind": "collection", "filter": object{"_id": "orders"}, "limit": 1}, 200)["items"].([]any)[0].(map[string]any)
	require.NotContains(t, metadata["data"].(map[string]any), "sampleSchema")
	a.post(key, "metadata:find", object{"kind": "collection", "filter": object{"sampleSchema.type": "object"}}, 422)
	a.query(key, "records/query", object{"collectionId": "orders", "pipeline": []object{}}, 200)
	a.apply(key, 403, order("denied", 1, false))
	a.query(key, "records/query", object{"collectionId": "orders", "pipeline": []object{{"$lookup": object{"collectionId": "orders", "kind": "samples", "recordId": "b", "localField": "_id", "foreignField": "_meta.recordId", "as": "readings"}}}}, 403)
	now := time.Now().UnixMilli()
	a.post(key, "samples/query-series", object{"collectionId": "orders", "recordIds": []string{"b"}, "from": now - 1000, "to": now + 1000}, 403)
	doc["status"] = "disabled"
	a.apply(a.admin, 200, object{"opId": "disable", "action": "meta.replace", "kind": "app", "id": "maintenance", "expectedRevision": 2, "data": doc})
	a.post(key, "apps/context", object{}, 403)
	a.query(key, "records/query", object{"collectionId": "orders", "pipeline": []object{}}, 403)
}

func TestAppClientHTTP(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node >=22.18 required for the cross-language client smoke")
	}
	a := newAppAPI(t)
	key, _ := a.provision()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	script, err := filepath.Abs("../../../../packages/app-data-client/integration.mjs")
	require.NoError(t, err)
	command := exec.CommandContext(ctx, node, script)
	command.Env = append(os.Environ(), "APP_API_BASE="+a.server.URL+"/openapi/v2", "APP_API_KEY="+key)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	t.Log(string(output))
}

func TestAppHTTPScopesAndJobs(t *testing.T) {
	a := newAppAPI(t)
	key, _ := a.provision()
	ctx := context.Background()
	keys := repo.NewAPIKeyRepo(ctx)
	// Existing Launchpad credentials use app_id as metadata, not UNS App grants.
	legacy, legacyKey, err := keys.CreateAPIKey(ctx, "existing-launchpad", 1, 1, "read_only", "external", "app", []string{"uns.read"})
	require.NoError(t, err)
	require.NoError(t, repo.GetCommonConn(ctx).Table("sys_api_key").Where("id = ?", legacy.ID).Update("app_id", "123").Error)
	a.query(legacyKey, "records/query", object{"collectionId": "private", "pipeline": []object{}}, 200)
	a.post(legacyKey, "apps/context", object{}, 403)
	a.post(a.admin, "apps/keys/revoke", object{"appId": "123", "keyId": fmt.Sprint(legacy.ID)}, 404)
	_, err = keys.ValidateAPIKeyDetail(ctx, legacyKey)
	require.NoError(t, err, "App revoke must preserve existing Launchpad credentials")
	for _, profile := range []string{"read_only", "data_writer", "full_access"} {
		_, ordinary, err := keys.CreateAPIKey(ctx, profile, 1, 1, profile, "external", "personal", repo.APIKeyResourceKeysForPermission(profile))
		require.NoError(t, err)
		a.post(ordinary, "apps/keys/create", object{"appId": "maintenance", "name": "must-not-issue"}, 403)
	}
	_, _, err = keys.CreateAPIKey(ctx, "unbound-data-key", 1, 1, "full_access", repo.APIKeyUsageAppData, "app", []string{"uns.read", "uns.write"})
	require.Error(t, err)
	second := a.post(a.admin, "apps/keys/create", object{"appId": "maintenance", "name": "second-runtime"}, 200)["apiKey"].(string)
	job := a.post(key, "records:transact", object{"idempotencyKey": "queued-order", "mode": "async", "items": []object{order("queued", 1, false)}}, 200)
	require.Equal(t, "accepted", job["status"])
	jobQuery := object{"jobIds": []any{job["jobId"]}}
	a.query(second, "jobs/query", jobQuery, 404)
	a.query(key, "jobs/query", jobQuery, 200)
	// Revoke writes before the worker starts. Persisted jobs cannot retain the old grant.
	a.apply(a.admin, 200, object{"opId": "restrict", "action": "meta.replace", "kind": "app", "id": "maintenance", "expectedRevision": 1, "data": appDocument([]string{"record:read"})})
	a.svc.App.UnsV2.Start(ctx)
	t.Cleanup(a.svc.App.UnsV2.Close)
	require.Eventually(t, func() bool {
		state := a.query(a.admin, "jobs/query", jobQuery, 200)["list"].([]any)[0].(map[string]any)
		return state["status"] == "failed"
	}, 5*time.Second, 100*time.Millisecond)
	page := a.query(key, "records/query", object{"collectionId": "orders", "pipeline": []object{}}, 200)
	require.Empty(t, page["items"])
	a.apply(a.admin, 200, order("obsolete", 1, false))
	a.apply(a.admin, 200, object{"opId": "delete-only", "action": "meta.replace", "kind": "app", "id": "maintenance", "expectedRevision": 2, "data": appDocument([]string{"record:delete"})})
	deleteKey := a.post(a.admin, "apps/keys/create", object{"appId": "maintenance", "name": "delete-only"}, 200)["apiKey"].(string)
	a.apply(deleteKey, 200, object{"opId": "remove", "action": "record.delete", "filter": object{"_id": "obsolete"}, "collectionId": "orders", "expectedRevision": 1})
	a.apply(deleteKey, 403, order("not-writable", 1, false))
	a.query(deleteKey, "records/query", object{"collectionId": "orders", "pipeline": []object{}}, 403)
	// Issuer permissions are also live, not just the key's saved scope.
	require.NoError(t, repo.GetCommonConn(ctx).Exec("DELETE FROM sys_role_resource WHERE resource_id = ?", 3332).Error)
	a.post(a.admin, "apps/keys/create", object{"appId": "maintenance", "name": "revoked-issuer"}, 403)
}

func TestAppProvisioningExample(t *testing.T) {
	a := newAppAPI(t)
	raw, err := os.ReadFile("../../../../doc/uns-v2/examples/create-app.json")
	require.NoError(t, err)
	var request object
	require.NoError(t, json.Unmarshal(raw, &request))
	for _, entry := range request["steps"].([]any) {
		step := entry.(map[string]any)
		result := a.post(a.admin, strings.TrimPrefix(step["path"].(string), "/uns/"), step["body"], 200)
		require.Equal(t, "committed", result["status"])
	}
	key := a.post(a.admin, "apps/keys/create", object{"appId": "maintenance-app", "name": "example-key"}, 200)["apiKey"].(string)
	info := a.post(key, "apps/context", object{}, 200)
	grant := info["collections"].([]any)[0].(map[string]any)
	require.Equal(t, "orders", grant["alias"])
	require.Equal(t, "maintenance-orders", grant["collectionId"])
	a.post(key, "records/insert-one", object{"collectionId": "maintenance-orders", "id": "example-order", "data": object{"title": "Check pump", "priority": 1, "done": false}}, 200)
	records := a.post(key, "collections/maintenance-orders/records:get", object{"ids": []string{"example-order"}}, 200)
	require.Len(t, records["items"], 1)
}

func TestAppCredentialIsolation(t *testing.T) {
	a := newAppAPI(t)
	a.provision()
	ctx := context.Background()
	a.apply(a.admin, 200, object{"opId": "read-only", "action": "meta.replace", "kind": "app", "id": "maintenance", "expectedRevision": 1, "data": appDocument([]string{"record:read"})})
	issued := a.post(a.admin, "apps/keys/create", object{"appId": "maintenance", "name": "limited-app"}, 200)
	raw := issued["apiKey"].(string)
	keys := repo.NewAPIKeyRepo(ctx)
	before, err := keys.ValidateAPIKeyDetail(ctx, raw)
	require.NoError(t, err)
	_, err = apikey.New(ctx).Delete(ctx, before.ID, 1)
	require.ErrorIs(t, err, apikey.ErrNotFound)
	require.ErrorIs(t, keys.DeleteAppDataKey(ctx, before.ID, 1, "other-app"), repo.ErrNotFound)
	require.ErrorIs(t, keys.DeleteAppDataKey(ctx, before.ID, 2, "maintenance"), repo.ErrNotFound)
	require.ErrorIs(t, keys.DeleteAppDataKey(ctx, before.ID, 1, ""), repo.ErrNotFound)
	ordinary, ordinaryRaw, err := keys.CreateAPIKey(ctx, "ordinary", 1, 1, "read_only", "external", "personal", []string{"uns.read"})
	require.NoError(t, err)
	_, err = apikey.New(ctx).Delete(ctx, ordinary.ID, 1)
	require.NoError(t, err)
	_, err = keys.ValidateAPIKeyDetail(ctx, ordinaryRaw)
	require.Error(t, err)
	for _, ownerType := range []string{"", "app"} {
		listed, err := keys.ListAPIKeys(ctx, 1, ownerType, "")
		require.NoError(t, err)
		for _, key := range listed {
			require.NotEqual(t, repo.APIKeyUsageAppData, key.UsageType)
		}
	}
	// New Collection grants must not let the generic key editor expand the issued scope.
	a.apply(a.admin, 200, object{"opId": "write-grant", "action": "meta.replace", "kind": "app", "id": "maintenance", "expectedRevision": 2, "data": appDocument([]string{"record:read", "record:write", "record:delete"})})
	for _, profile := range []string{"read_only", "data_writer", "full_access"} {
		_, err := apikey.New(ctx).Update(ctx, before.ID, 1, "must-not-change", profile)
		require.ErrorIs(t, err, apikey.ErrNotFound)
	}
	after, err := keys.ValidateAPIKeyDetail(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, before.Name, after.Name)
	require.Equal(t, before.ResourceKeys, after.ResourceKeys)
	a.apply(raw, 403, order("blocked-write", 1, false))
	rotated := a.post(a.admin, "apps/keys/create", object{"appId": "maintenance", "name": "rotated-app"}, 200)["apiKey"].(string)
	a.apply(rotated, 200, order("allowed-write", 1, false))
	a.post(a.admin, "apps/keys/revoke", object{"appId": "maintenance", "keyId": issued["keyId"]}, 200)
	_, err = keys.ValidateAPIKeyDetail(ctx, raw)
	require.Error(t, err)
	var internalContextActions int64
	require.NoError(t, repo.GetCommonConn(ctx).Table("sys_resource_action").Where("action_value = ?", "/api/core/v2/uns/apps/context").Count(&internalContextActions).Error)
	require.Zero(t, internalContextActions)
}

func TestNativeAppRequestGrantBoundReleaseAndRevocation(t *testing.T) {
	a := newAppAPI(t)
	a.provision()
	ctx := svctest.CtxWithUser(1)
	id := "eb3137e2-516c-4a27-8d8e-6a1b6bc4d6b5"
	permissions := []string{"stream.requests.read", "stream.requests.invoke"}
	doc := appDocument([]string{"record:read", "sample:read"})
	doc["streamInteraction"] = object{"version": "cloud-hosted-app-stream/v1", "status": "active", "platformPermissions": permissions}
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, a.svc.App.Store.UnsDB().Create(&repo.UnsMeta{Kind: "app", ID: id, Revision: 1, Data: raw}).Error)
	require.NoError(t, a.svc.App.Store.CommonDB().Create(&repo.AppInstallation{AppID: id, Name: "Request App", ReleaseID: "release", Manifest: json.RawMessage(`{}`), Stage: "ready", Status: "running", DesiredState: "running"}).Error)
	resources := []string{"uns.read", "uns.write", "uns.app.manage", "uns.app.credentials", "uns.stream.manage", "stream.requests.manage", "stream.requests.read", "stream.requests.invoke", "stream.requests.write"}
	_, manager, err := repo.NewAPIKeyRepo(ctx).CreateAPIKey(ctx, "request-grant-manager", 1, 1, "full_access", "external", "personal", resources)
	require.NoError(t, err)
	appKey := a.post(manager, "apps/keys/create", object{"appId": id, "name": "request-client"}, 200)["apiKey"].(string)
	publisher := contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: map[string]struct{}{}}
	for _, permission := range resources {
		publisher.ResourceKeys[permission] = struct{}{}
	}
	publishCtx := contextx.WithSubject(ctx, publisher)
	runtime := sourcestream.New(nil, documentTestExecutor{scripttest.New(t)}, "")
	t.Cleanup(runtime.Close)
	runtime.SetProgramAuthorityResolver(a.svc.App.Auth.ResolveProgramAuthority)
	a.svc.App.SourceStream = runtime
	a.svc.App.UnsV2.SetSourceRuntime(runtime)
	require.NoError(t, runtime.StartProgramExecutions(publishCtx))
	script := `module.exports=api=>{api.stream.onRequest({id:"run",request:{code:"app.echo",name:"Echo",contractVersion:"1",inputSchema:{},outputSchema:{},sideEffect:"read",idempotency:"optional",timeoutMs:3000}},ctx=>({echo:ctx.input}));};`
	stream, err := runtime.SaveStream(publishCtx, 0, 0, object{"name": "App request", "script": object{"code": script, "timeoutMs": 3000}})
	require.NoError(t, err)
	stream, err = runtime.Deploy(publishCtx, stream["id"].(int64), stream["revision"].(int64), 0, 0)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /openapi/v2/stream-requests/discover", a.svc.ApiKeyAuth(requesthandlers.StreamRequestDiscoverHandler(a.svc)))
	mux.HandleFunc("POST /openapi/v2/stream-requests/{requestCode}/invoke", a.svc.ApiKeyAuth(func(w http.ResponseWriter, r *http.Request) {
		requesthandlers.StreamRequestInvokeHandler(a.svc)(w, pathvar.WithVars(r, map[string]string{"requestCode": r.PathValue("requestCode")}))
	}))
	mux.HandleFunc("POST /openapi/v2/stream-requests/{requestCode}/describe", a.svc.ApiKeyAuth(func(w http.ResponseWriter, r *http.Request) {
		requesthandlers.StreamRequestDescribeHandler(a.svc)(w, pathvar.WithVars(r, map[string]string{"requestCode": r.PathValue("requestCode")}))
	}))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	call := func(path string, body object, status int) object {
		t.Helper()
		payload := object{}
		for key, value := range body {
			if key != "code" {
				payload[key] = value
			}
		}
		if code, ok := body["code"].(string); ok {
			path = "/" + code + path
		}
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		request, err := http.NewRequest(http.MethodPost, server.URL+"/openapi/v2/stream-requests"+path, bytes.NewReader(raw))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-API-Key", appKey)
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		defer response.Body.Close()
		var value object
		require.NoError(t, json.NewDecoder(response.Body).Decode(&value))
		require.Equal(t, status, response.StatusCode, "%v", value)
		return value
	}
	require.EqualValues(t, 0, call("/discover", object{}, 200)["total"])
	call("/invoke", object{"code": "app.echo", "idempotencyKey": "original", "input": object{"value": 1}}, 403)
	principal := object{"code": "app.echo", "principalKind": "app", "principalID": id}
	get := func() object { return a.post(manager, "stream-requests/grants/get", principal, 200) }
	view := get()
	require.EqualValues(t, 1, view["activationSupported"])
	require.EqualValues(t, 2, view["registered"])
	set := func(token string, active int, actions []string, status int) object {
		return a.post(manager, "stream-requests/grants/set", object{"code": "app.echo", "principalKind": "app", "principalID": id, "active": active, "actions": actions, "expectedSnapshotToken": token}, status)
	}
	firstToken := view["snapshotToken"].(string)
	view = set(firstToken, 1, permissions, 200)
	set(firstToken, 1, permissions, 409)
	// Concurrent changes compare the same observed state under the grant lock.
	concurrentBody, _ := json.Marshal(object{"code": "app.echo", "principalKind": "app", "principalID": id, "active": 1, "actions": permissions, "expectedSnapshotToken": view["snapshotToken"]})
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			req, err := http.NewRequest(http.MethodPost, a.server.URL+"/openapi/v2/uns/stream-requests/grants/set", bytes.NewReader(concurrentBody))
			if err != nil {
				statuses <- 0
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", manager)
			response, err := a.server.Client().Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			defer response.Body.Close()
			statuses <- response.StatusCode
		}()
	}
	firstStatus, secondStatus := <-statuses, <-statuses
	require.ElementsMatch(t, []int{200, 409}, []int{firstStatus, secondStatus})
	access := a.post(manager, "app-stream-access/list", object{"appId": id, "includeInactive": 1}, 200)
	require.EqualValues(t, 1, access["total"])
	require.Equal(t, "available", access["items"].([]any)[0].(map[string]any)["state"])

	require.EqualValues(t, 1, call("/discover", object{}, 200)["total"])
	require.Equal(t, "app.echo", call("/describe", object{"code": "app.echo"}, 200)["code"])
	invocation := object{"code": "app.echo", "idempotencyKey": "original", "input": object{"value": 1}}
	first := call("/invoke", invocation, 200)
	require.Equal(t, false, first["replayed"])
	require.Equal(t, true, call("/invoke", invocation, 200)["replayed"])
	// Neither an App key nor a client-forged principal may manage grants.
	a.post(appKey, "stream-requests/grants/get", principal, 403)
	a.post(manager, "stream-requests/grants/get", object{"code": "app.echo", "principalKind": "user", "principalID": "1"}, 422)
	// A changed release requires explicit review; the old key cannot execute a
	// different publication merely because its public code stayed the same.
	stream, err = runtime.SaveStream(publishCtx, stream["id"].(int64), stream["revision"].(int64), object{"name": "App request", "script": object{"code": script + "\n// reviewed second release", "timeoutMs": 3000}})
	require.NoError(t, err)
	_, err = runtime.Deploy(publishCtx, stream["id"].(int64), stream["revision"].(int64), 0, 0)
	require.NoError(t, err)
	call("/invoke", object{"code": "app.echo", "idempotencyKey": "new-release", "input": object{}}, 403)
	view = get()
	view = set(view["snapshotToken"].(string), 1, permissions, 200)
	call("/invoke", object{"code": "app.echo", "idempotencyKey": "new-release", "input": object{}}, 200)
	call("/invoke", invocation, 403)
	// Data grants are independent from code grants and are refreshed before
	// both a new call and replay, even while the API key still has uns.read.
	dataScript := `module.exports=api=>{const orders=api.uns.use({collectionId:"orders",access:"read"});api.stream.onRequest({id:"orders",request:{code:"app.orders",name:"Orders",contractVersion:"1",inputSchema:{},outputSchema:{},sideEffect:"read",idempotency:"optional",timeoutMs:3000}},()=>orders.read({limit:1}));};`
	dataStream, err := runtime.SaveStream(publishCtx, 0, 0, object{"name": "App data request", "script": object{"code": dataScript, "timeoutMs": 3000}})
	require.NoError(t, err)
	_, err = runtime.Deploy(publishCtx, dataStream["id"].(int64), dataStream["revision"].(int64), 0, 0)
	require.NoError(t, err)
	dataView := a.post(manager, "stream-requests/grants/get", object{"code": "app.orders", "principalKind": "app", "principalID": id}, 200)
	a.post(manager, "stream-requests/grants/set", object{"code": "app.orders", "principalKind": "app", "principalID": id, "active": 1, "actions": permissions, "expectedSnapshotToken": dataView["snapshotToken"]}, 200)
	call("/invoke", object{"code": "app.orders", "idempotencyKey": "data-read", "input": object{}}, 200)
	doc["collections"] = []object{}
	a.apply(a.admin, 200, object{"opId": "remove-data", "action": "meta.replace", "kind": "app", "id": id, "expectedRevision": 1, "data": doc})
	call("/invoke", object{"code": "app.orders", "idempotencyKey": "data-read", "input": object{}}, 403)
	call("/invoke", object{"code": "app.orders", "idempotencyKey": "data-read-new", "input": object{}}, 403)
	// Revocation works while the App is stopped and takes effect for completed
	// receipts as well as new execution; no new execution key is manufactured.
	require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Update("status", "stopped").Error)
	view = get()
	require.EqualValues(t, 2, view["recipientEligible"])
	set(view["snapshotToken"].(string), 2, []string{}, 200)
	require.NoError(t, a.svc.App.Store.CommonDB().Model(&repo.AppInstallation{}).Where("app_id=?", id).Update("status", "running").Error)
	call("/invoke", object{"code": "app.echo", "idempotencyKey": "new-release", "input": object{}}, 403)
	require.EqualValues(t, 0, call("/discover", object{}, 200)["total"])
}

func TestRecordSubscribeHTTPStreamingAuthenticationAndResume(t *testing.T) {
	a := newAppAPI(t)
	key, _ := a.provision()
	a.svc.App.UnsV2.SetRecordChangesCursorSecret("isolated-http-subscription-secret")
	require.NoError(t, repo.ConfigureRecordChangeNotifier(a.svc.App.Store.UnsDB(), a.svc.App.UnsV2.NotifyRecordChanges))
	login, err := a.svc.App.Auth.Login(context.Background(), "tier0", "tier0")
	require.NoError(t, err)
	var active atomic.Int64
	var shortDeadline atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/core/v2/uns/records:subscribe", a.svc.SessionAuth(a.svc.Permission(corehandlers.UnsV2RecordSubscribeHandler(a.svc))))
	mux.HandleFunc("GET /openapi/v2/uns/records:subscribe", a.svc.ApiKeyAuth(handlers.UnsV2RecordSubscribeHandler(a.svc)))
	// These are the actual go-zero wrappers used by the server. An SSE request
	// must bypass its ordinary request timeout and retain deadline/flush access.
	wrapped := zerohandler.LogHandler(zerohandler.TimeoutHandler(500 * time.Millisecond)(mux))
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		if shortDeadline.Load() {
			ctx, cancel := context.WithTimeout(r.Context(), 3200*time.Millisecond)
			defer cancel()
			r = r.WithContext(ctx)
		}
		wrapped.ServeHTTP(w, r)
	}))
	server.Config.ReadTimeout = time.Second
	server.Config.WriteTimeout = time.Second
	server.Start()
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 6 * time.Second
	type frame struct{ event, id, data string }
	readFrame := func(reader *bufio.Scanner) frame {
		t.Helper()
		out := frame{}
		for reader.Scan() {
			line := reader.Text()
			if line == "" {
				return out
			}
			if name, value, ok := strings.Cut(line, ": "); ok {
				switch name {
				case "event":
					out.event = value
				case "id":
					out.id = value
				case "data":
					out.data = value
				}
			}
		}
		require.NoError(t, reader.Err())
		t.Fatal("subscription ended before a frame")
		return out
	}
	connect := func(ctx context.Context, prefix, cursor string, headers map[string]string) *http.Response {
		t.Helper()
		r, e := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+prefix+"/uns/records:subscribe?collectionIds=orders", nil)
		require.NoError(t, e)
		r.Header.Set("Accept", "text/event-stream")
		if prefix == "/api/core/v2" {
			r.Header.Set("Authorization", "Bearer "+login.Token)
		} else {
			r.Header.Set("X-API-Key", key)
		}
		if cursor != "" {
			r.Header.Set("Last-Event-ID", cursor)
		}
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		response, e := client.Do(r)
		require.NoError(t, e)
		return response
	}
	for _, prefix := range []string{"/api/core/v2", "/openapi/v2"} {
		t.Run(prefix, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := time.Now()
			response := connect(ctx, prefix, "", nil)
			require.Equal(t, 200, response.StatusCode)
			require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))
			scanner := bufio.NewScanner(response.Body)
			head := readFrame(scanner)
			require.Equal(t, "heartbeat", head.event)
			require.NotEmpty(t, head.id)
			require.Less(t, time.Since(started), time.Second, "initial frame must be flushed without buffering")
			// Exceed the configured HTTP network deadline, then exercise the
			// committed DB notification and a second flushed frame.
			time.Sleep(1100 * time.Millisecond)
			id := "sse-" + strings.ReplaceAll(prefix, "/", "-")
			a.apply(a.admin, 200, order(id, 1, false))
			change := readFrame(scanner)
			require.Equal(t, "change", change.event)
			require.NotEmpty(t, change.id)
			var event unsv2.RecordChangeEvent
			require.NoError(t, json.Unmarshal([]byte(change.data), &event))
			require.Equal(t, id, event.RecordID)
			require.Equal(t, event.EventID, change.id)
			require.NotContains(t, change.data, "Factory A")
			cancel()
			response.Body.Close()
			require.Eventually(t, func() bool { return active.Load() == 0 }, time.Second, 10*time.Millisecond)
			a.apply(a.admin, 200, order(id+"-later", 2, false))
			resumedCtx, stop := context.WithCancel(context.Background())
			resumed := connect(resumedCtx, prefix, change.id, nil)
			require.Equal(t, 200, resumed.StatusCode)
			read := readFrame(bufio.NewScanner(resumed.Body))
			require.Equal(t, "change", read.event)
			require.Contains(t, read.data, id+"-later")
			stop()
			resumed.Body.Close()
			require.Eventually(t, func() bool { return active.Load() == 0 }, time.Second, 10*time.Millisecond)
		})
	}
	for _, headers := range []map[string]string{{"X-Fleet-Node-Id": "remote-node"}, {"Accept": "application/json"}} {
		response := connect(context.Background(), "/openapi/v2", "", headers)
		require.NotEqual(t, 200, response.StatusCode)
		response.Body.Close()
	}
	invalid := connect(context.Background(), "/openapi/v2", "invalid-cursor", nil)
	require.NotEqual(t, 200, invalid.StatusCode)
	invalid.Body.Close()
	// The reader reserves 3 seconds at the end of the caller's budget. The
	// transport must close there rather than spin sending empty heartbeats.
	shortDeadline.Store(true)
	started := time.Now()
	bounded := connect(context.Background(), "/openapi/v2", "", nil)
	require.Equal(t, 200, bounded.StatusCode)
	body, err := io.ReadAll(bounded.Body)
	bounded.Body.Close()
	require.NoError(t, err)
	require.Less(t, time.Since(started), 2*time.Second)
	require.LessOrEqual(t, strings.Count(string(body), "event: heartbeat"), 2)
	require.Eventually(t, func() bool { return active.Load() == 0 }, time.Second, 10*time.Millisecond)
	shortDeadline.Store(false)
	// A live App grant change invalidates the cursor scope on the next poll.
	revoked := connect(context.Background(), "/openapi/v2", "", nil)
	require.Equal(t, 200, revoked.StatusCode)
	scanner := bufio.NewScanner(revoked.Body)
	require.Equal(t, "heartbeat", readFrame(scanner).event)
	a.apply(a.admin, 200, object{"opId": "remove-read", "action": "meta.replace", "kind": "app", "id": "maintenance", "expectedRevision": 1, "data": appDocument([]string{"record:write"})})
	reset := readFrame(scanner)
	require.Equal(t, "reset", reset.event)
	require.Empty(t, reset.id)
	require.Contains(t, reset.data, "SCOPE_CHANGED")
	revoked.Body.Close()
	require.Eventually(t, func() bool { return active.Load() == 0 }, time.Second, 10*time.Millisecond)
}
