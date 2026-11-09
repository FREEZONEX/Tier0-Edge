package unsv2

import (
	"backend/internal/contextx"
	"backend/internal/domain/sourcestream"
	"backend/internal/repo"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Service) sharedRuntimeQuery(ctx context.Context, name string, fields map[string]json.RawMessage) (any, error) {
	id := resourceString(fields, "id")
	if name == "sourceRuntimeGet" {
		source, err := meta(ctx, "source", id, false)
		if err != nil {
			return nil, err
		}
		state, err := meta(ctx, "source_state", id, false)
		if err != nil {
			return nil, err
		}
		var data SourceData
		if json.Unmarshal(source.Data, &data) != nil || data.SchemaVersion != 1 {
			return nil, invalid("id", "shared Source required; existing runtime Sources retain their lifecycle API")
		}
		// Only the declared runtime projection crosses the API boundary. Legacy
		// resource metadata, input payloads and private lease tokens stay private.
		var snapshot struct {
			Health     string                      `json:"health"`
			LastDataAt int64                       `json:"lastDataAt"`
			CheckedAt  int64                       `json:"checkedAt"`
			Streams    map[string]acquisitionState `json:"streams"`
		}
		_ = json.Unmarshal(state.Data, &snapshot)
		streams := map[string]acquisitionState{}
		for bindingID, binding := range data.Config.Streams {
			observed, exists := snapshot.Streams[bindingID]
			observed.LeaseToken = ""
			if observed.Cursor != "" {
				observed.Cursor = "[REDACTED]"
			}
			if !binding.Enabled {
				observed.Status = "disabled"
			} else if !exists {
				observed.Status = "starting"
				observed.Revision = source.Revision
			} else if observed.Revision != source.Revision {
				observed.Status = "reconfiguring"
			}
			streams[bindingID] = observed
		}
		value := map[string]any{"health": snapshot.Health, "lastDataAt": snapshot.LastDataAt, "checkedAt": snapshot.CheckedAt, "streams": streams}
		return map[string]any{"sourceId": id, "revision": source.Revision, "state": value}, nil
	}
	row, err := meta(ctx, "stream", id, false)
	if err != nil {
		return nil, err
	}
	var data StreamData
	if json.Unmarshal(row.Data, &data) != nil || data.Draft == nil {
		return nil, invalid("id", "shared Stream required; programs use program/versions/list")
	}
	if name == "streamReleasesGet" {
		var release int
		_ = json.Unmarshal(fields["release"], &release)
		v, ok := data.Releases[strconv.Itoa(release)]
		if !ok {
			return nil, problem("NOT_FOUND", "release not found")
		}
		v.Definition.Processor.Code = ""
		return map[string]any{"streamId": id, "release": release, "definition": v.Definition, "publishedAt": v.PublishedAt}, nil
	}
	var p Pagination
	delete(fields, "id")
	raw, _ := json.Marshal(fields)
	_ = json.Unmarshal(raw, &p)
	page, size, err := bounds(p, 200)
	if err != nil {
		return nil, err
	}
	numbers := []int{}
	for k := range data.Releases {
		n, _ := strconv.Atoi(k)
		numbers = append(numbers, n)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(numbers)))
	list := []any{}
	for i := (page - 1) * size; i < min(len(numbers), page*size); i++ {
		n := numbers[i]
		v := data.Releases[strconv.Itoa(n)]
		v.Definition.Processor.Code = ""
		list = append(list, map[string]any{"streamId": id, "release": n, "publishedAt": v.PublishedAt, "definition": v.Definition})
	}
	return pageResult(list, int64(len(numbers)), page, size), nil
}
func int64Number(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// Runtime lifecycle adapters call the colleague's existing domain implementation,
// not internal HTTP handlers. They preserve revision, Source-impact and deployed
// dependency checks. Secrets/OAuth authorizations stay on the credential APIs.
func (s *Service) runtimeResource(ctx context.Context, op ResourceOperation, fields map[string]json.RawMessage) (any, error) {
	credentialIntent := false
	if op.Name == "sourceDocumentCreate" {
		credentialIntent = len(fields["credentials"]) > 0
		var items []map[string]json.RawMessage
		_ = json.Unmarshal(fields["items"], &items)
		for _, item := range items {
			credentialIntent = credentialIntent || len(item["credentials"]) > 0
		}
	}
	credentialIntent = credentialIntent || op.Name == "sourceNativeCredentialsUpdate" || op.Name == "sourceCredentialsSave"
	if credentialIntent {
		if err := require(ctx, "uns.source.credentials", "uns.source.manage"); err != nil {
			return nil, err
		}
	}
	var dryRun bool
	_ = json.Unmarshal(fields["dryRun"], &dryRun)
	if op.Mutation == "" || dryRun || resourceString(fields, "idempotencyKey") == "" || op.Name == "sourceProbe" || op.Name == "streamProgramTest" || op.Name == "streamProgramInvoke" {
		return s.executeRuntimeResource(ctx, op, fields)
	}
	identity, err := requestIdentity(ctx, resourceString(fields, "idempotencyKey"))
	if err != nil {
		return nil, err
	}
	// Keep receipt digests compatible with requests that explicitly sent false.
	fields["dryRun"] = json.RawMessage("false")
	raw, _ := json.Marshal(map[string]any{"operation": op.Name, "fields": fields})
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if credentialIntent {
		if s.sourceRuntime == nil {
			return nil, problem("UNAVAILABLE", "credential runtime unavailable")
		}
		digest, err = s.sourceRuntime.CredentialIntentDigest(raw)
		if err != nil {
			return nil, problem("SOURCE_CREDENTIAL_UNAVAILABLE", "credential encryption is not configured")
		}
	}
	if databaseRuntimeMutation(op.Name) {
		deferred, refresh := ctx, func() {}
		if s.sourceRuntime != nil {
			deferred, refresh = s.sourceRuntime.DeferSourceRefresh(ctx)
		}
		var value any
		readyToCommit := false
		err := repo.GetUnsConn(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var operationErr error
			value, operationErr = s.runtimeResourceReceipt(repo.WithUnsTransaction(deferred, tx), op, fields, identity, digest, true)
			readyToCommit = operationErr == nil
			return operationErr
		})
		if err != nil {
			if readyToCommit {
				return nil, &Fault{Code: "RUNTIME_RESULT_UNAVAILABLE", Message: "Lifecycle transaction outcome is unavailable", Reason: "Lifecycle transaction outcome is unavailable", Repair: "Retry the identical request with the same idempotencyKey to recover its original receipt; do not create a new key"}
			}
			return nil, err
		}
		refresh()
		return value, nil
	}
	return s.runtimeResourceReceipt(ctx, op, fields, identity, digest, false)
}

// These document mutations only change the local database. OAuth exchanges,
// deployment, probes and execution retain their durable admission boundary:
// a database rollback cannot prove that an external action did not happen.
func databaseRuntimeMutation(operation string) bool {
	switch operation {
	case "sourceDocumentCreate", "sourceDocumentUpdate", "sourceDocumentDelete", "sourceDocumentStateUpdate", "sourceNativeCredentialsUpdate",
		"streamDocumentCreate", "streamDocumentUpdate", "streamDocumentDisable", "streamProgramDelete":
		return true
	}
	return false
}

func (s *Service) runtimeResourceReceipt(ctx context.Context, op ResourceOperation, fields map[string]json.RawMessage, identity, digest string, atomic bool) (any, error) {
	id := "api-runtime-" + identity
	now := time.Now().UnixMilli()
	pending, _ := json.Marshal(map[string]any{"type": "apiRuntimeReceipt", "digest": digest, "status": "running"})
	created := repo.GetUnsConn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&repo.UnsMeta{Kind: "task", ID: id, Data: pending, Revision: 1, CreatedAt: now, UpdatedAt: now})
	if created.Error != nil {
		return nil, created.Error
	}
	if created.RowsAffected == 0 {
		row, e := repo.NewUnsMetaRepo(ctx).Get(ctx, id, false, "task")
		if e != nil {
			return nil, e
		}
		var receipt struct {
			Digest  string `json:"digest"`
			Status  string `json:"status"`
			Value   any    `json:"value"`
			Failure *Fault `json:"failure,omitempty"`
		}
		if decodeNumberJSON(row.Data, &receipt) != nil {
			return nil, problem("INTERNAL_ERROR", "invalid runtime receipt")
		}
		if receipt.Digest != digest {
			return nil, problem("IDEMPOTENCY_CONFLICT", "same key used for a different runtime request")
		}
		if receipt.Status == "completed" {
			return receipt.Value, nil
		}
		if receipt.Status == "failed" && receipt.Failure != nil {
			receipt.Failure.Message = receipt.Failure.Reason
			return nil, receipt.Failure
		}
		return nil, &Fault{Code: "RUNTIME_RESULT_UNAVAILABLE", Message: "Runtime request was already admitted; it will not be repeated", Repair: "Read the Source/Stream state and versions to reconcile the original operation; a new intended action needs the latest revision and a new key"}
	}
	value, operationErr := s.executeRuntimeResource(ctx, op, fields)
	status := "completed"
	var failure *Fault
	if operationErr != nil {
		status = "failed"
		if errors.As(operationErr, &failure) {
			copy := *failure
			if copy.Reason == "" {
				copy.Reason = operationErr.Error()
			}
			failure = &copy
		}
	}
	completed, _ := json.Marshal(map[string]any{"type": "apiRuntimeReceipt", "digest": digest, "status": status, "value": value, "failure": failure})
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if e := repo.GetUnsConn(saveCtx).Model(&repo.UnsMeta{}).Where("kind='task' AND id=?", id).Updates(map[string]any{"data": completed, "revision": 2, "updated_at": time.Now().UnixMilli()}).Error; e != nil {
		if atomic {
			return nil, &Fault{Code: "TEMPORARILY_UNAVAILABLE", Message: "Lifecycle receipt could not be saved", Reason: "Lifecycle receipt could not be saved", RolledBack: true, Retryable: true, Repair: "Retry the identical request with the same idempotencyKey; its database changes were rolled back"}
		}
		return nil, problem("RUNTIME_RESULT_UNAVAILABLE", "operation admitted; reconcile runtime state before another action")
	}
	return value, operationErr
}

func (s *Service) executeRuntimeResource(ctx context.Context, op ResourceOperation, fields map[string]json.RawMessage) (any, error) {
	if nativeDocumentOperation(op.Name) {
		value, err := s.runtimeDocument(ctx, op, fields)
		return runtimeOperationResult(value, err)
	}
	if op.Name == "sourceProtocolsList" {
		return sourceProtocolCatalog(), nil
	}
	if s.sourceRuntime == nil {
		return nil, problem("UNAVAILABLE", "Source runtime unavailable")
	}
	if op.Name == "sourceDevicesList" {
		value, err := s.sourceRuntime.Devices(ctx, resourceString(fields, "kind"))
		return runtimeOperationResult(value, err)
	}
	id := resourceString(fields, "id")
	if op.Name == "sourceCredentialsList" {
		value, err := s.sourceRuntime.SharedCredentialReferences(ctx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, problem("NOT_FOUND", "Source not found")
		}
		if errors.Is(err, repo.ErrInvalidArgument) {
			return nil, invalid("id", "shared Source identity required")
		}
		if errors.Is(err, sourcestream.ErrCredential) || errors.Is(err, sourcestream.ErrCredentialKey) {
			return nil, problem("UNAVAILABLE", "Source credential references could not be read on this installation")
		}
		return value, err
	}
	if op.Name == "sourceCredentialsSave" {
		if err := require(ctx, "uns.source.credentials", "uns.source.manage"); err != nil {
			return nil, err
		}
		var revision, expected int64
		var dryRun bool
		var values map[string]string
		_ = json.Unmarshal(fields["sourceRevision"], &revision)
		_ = json.Unmarshal(fields["expectedCredentialRevision"], &expected)
		_ = json.Unmarshal(fields["dryRun"], &dryRun)
		_ = json.Unmarshal(fields["values"], &values)
		value, err := s.sourceRuntime.SaveSharedCredential(ctx, id, revision, resourceString(fields, "credentialRef"), expected, sourcestream.Credential{Kind: resourceString(fields, "kind"), Username: resourceString(fields, "username"), Secret: resourceString(fields, "secret"), Values: values}, dryRun)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, problem("NOT_FOUND", "Source not found")
		}
		if errors.Is(err, sourcestream.ErrCredentialKey) {
			return nil, problem("UNAVAILABLE", "Source credential encryption is not configured on this installation")
		}
		if errors.Is(err, sourcestream.ErrCredential) {
			return nil, invalid("credential", "invalid credential or unsupported Source protocol")
		}
		if errors.Is(err, repo.ErrConflict) {
			return nil, problem("VERSION_CONFLICT", "Source or credential revision changed; refresh references before retrying")
		}
		if errors.Is(err, repo.ErrInvalidArgument) {
			return nil, invalid("credential", "valid Source identity and credential revisions required")
		}
		return value, err
	}
	var revision, sourceRevision int64
	_ = json.Unmarshal(fields["expectedRevision"], &revision)
	_ = json.Unmarshal(fields["expectedSourceRevision"], &sourceRevision)
	numeric := int64(0)
	if id != "" {
		var err error
		numeric, err = strconv.ParseInt(id, 10, 64)
		if err != nil || numeric <= 0 {
			return nil, invalid("id", "runtime lifecycle requires the positive decimal string ID returned by its create operation")
		}
	}
	var document, input any
	_ = json.Unmarshal(fields["document"], &document)
	_ = json.Unmarshal(fields["input"], &input)
	var dryRun bool
	_ = json.Unmarshal(fields["dryRun"], &dryRun)
	var timeout int
	_ = json.Unmarshal(fields["timeoutMs"], &timeout)
	// Omitted caller guards use a server snapshot. Runtime save/deploy still
	// rechecks that snapshot and dependency versions before changing live state.
	if op.Resource == "stream" && op.Mutation != "" && numeric > 0 && op.Name != "streamProgramInvoke" {
		current, e := repo.NewSourceStreamRepo(ctx).Stream(ctx, numeric)
		if e != nil {
			return nil, e
		}
		if revision > 0 && revision != current.Revision {
			return nil, problem("VERSION_CONFLICT", "Stream revision changed")
		}
		if revision == 0 {
			revision = current.Revision
		}
		if sourceRevision == 0 && current.SourceID > 0 {
			source, e := repo.NewSourceStreamRepo(ctx).Source(ctx, current.SourceID)
			if e != nil {
				return nil, e
			}
			sourceRevision = source.ConfigRevision
		}
	}
	r := s.sourceRuntime
	var value any
	var err error
	switch op.Name {
	case "sourceResourceCatalog", "sourceResourceRead", "sourceResourceScan":
		if err := require(ctx, "uns.source", "uns.read"); err != nil {
			return nil, err
		}
		var configRevision int64
		_ = json.Unmarshal(fields["configRevision"], &configRevision)
		if op.Name == "sourceResourceCatalog" {
			value, err = r.EdgeResourceCatalog(ctx, numeric, configRevision)
		} else if op.Name == "sourceResourceScan" {
			var limit int
			_ = json.Unmarshal(fields["limit"], &limit)
			var params struct {
				sourcestream.EdgePlexSAPFilters
				MaxItems             int    `json:"maxItems"`
				CreatedDateTimeBegin string `json:"createdDateTimeBegin"`
				CreatedDateTimeEnd   string `json:"createdDateTimeEnd"`
			}
			if raw, ok := fields["params"]; ok {
				if err = Decode(raw, &params); err != nil {
					return nil, err
				}
			}
			value, err = r.EdgeResourceScan(ctx, numeric, configRevision, resourceString(fields, "resourceRef"), resourceString(fields, "cursor"), limit, params.MaxItems, timeout, params.CreatedDateTimeBegin, params.CreatedDateTimeEnd, params.EdgePlexSAPFilters)
		} else {
			var params sourcestream.EdgeResourceReadParams
			if raw, ok := fields["params"]; ok {
				if err = Decode(raw, &params); err != nil {
					return nil, err
				}
			}
			value, err = r.EdgeResourceRead(ctx, numeric, configRevision, resourceString(fields, "resourceRef"), params, timeout)
		}
		if err != nil {
			subject, _ := contextx.SubjectFrom(ctx)
			if fault := sourceReadFault(err); fault != nil {
				return nil, fault
			}
			if subject.FleetTask != nil && op.Name != "sourceResourceCatalog" && errors.Is(err, sourcestream.ErrEdgeReferenceRejected) {
				return nil, problem("EDGE_SOURCE_REFERENCE_REJECTED", "The Edge rejected the configured Source reference before the resource read")
			}
			if errors.Is(err, sourcestream.ErrEdgeConnectionRefused) {
				return nil, problem("SOURCE_CONNECTION_REFUSED", "The upstream Source connection was refused")
			}
			var upstream sourcestream.EdgeHTTPResponseError
			if errors.As(err, &upstream) {
				return nil, problem(upstream.Error(), "The upstream Source rejected the resource read")
			}
			switch {
			case errors.Is(err, sourcestream.ErrCredential), errors.Is(err, sourcestream.ErrCredentialKey), errors.Is(err, sourcestream.ErrOAuthRequired):
				return nil, problem("SOURCE_CREDENTIAL_UNAVAILABLE", "Edge could not load the Source credential")
			case errors.Is(err, sourcestream.ErrPreviewNoSample):
				return nil, problem("SOURCE_PREVIEW_NO_SAMPLE", "The preview ended before a device sample arrived")
			case errors.Is(err, sourcestream.ErrPreviewWatch):
				return nil, problem("SOURCE_PREVIEW_WATCH_FAILED", "The Edge watch failed before delivering a preview sample")
			case errors.Is(err, context.DeadlineExceeded):
				return nil, problem("SOURCE_READ_TIMEOUT", "The Edge resource operation exceeded its deadline")
			case errors.Is(err, sourcestream.ErrVendorResponse):
				return nil, problem("SOURCE_RESPONSE_INVALID", "The Source response could not be used for this resource")
			}
			return runtimeOperationResult(nil, err)
		}
		// Do not coerce vendor record identifiers into Enterprise lifecycle IDs.
		return value, nil
	case "sourceRuntimeRead":
		var selected sourcestream.ModbusRange
		var configRevision int64
		if err = Decode(fields["range"], &selected); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(fields["configRevision"], &configRevision)
		value, err = r.ReadReference(ctx, numeric, configRevision, selected, timeout)
	case "sourceValidate":
		var d sourcestream.SourceDefinition
		if err = Decode(fields["document"], &d); err == nil {
			if d.RuntimeLocation == "" {
				d.RuntimeLocation = "local"
			}
			err = r.ValidateSource(d)
		}
		value = map[string]any{"valid": err == nil, "sideEffects": false}
	case "sourceProbe":
		if resourceString(fields, "mode") == "data" {
			if e := require(ctx, "uns.source", "uns.read"); e != nil {
				return nil, e
			}
		}
		if dryRun {
			if _, e := meta(ctx, "source", id, false); e != nil {
				return nil, e
			}
			return map[string]any{"validated": true, "sideEffects": false}, nil
		}
		current, e := repo.NewSourceStreamRepo(ctx).Source(ctx, numeric)
		if e != nil {
			return nil, e
		}
		result, e := r.CheckSource(ctx, numeric, resourceString(fields, "mode"), timeout)
		err = e
		if e == nil {
			value = map[string]any{"sourceId": id, "protocol": current.Protocol, "mode": result.Mode, "stage": result.Stage, "health": result.Health, "reachable": result.Health == "healthy", "latencyMs": result.LatencyMs, "errorCode": result.ErrorCode, "message": result.Message, "checkedAt": result.CheckedAt, "testedAt": result.CheckedAt}
		}
	case "sourceDiscover":
		current, e := repo.NewSourceStreamRepo(ctx).Source(ctx, numeric)
		if e != nil {
			return nil, e
		}
		if revision > 0 && revision != current.Revision {
			return nil, problem("VERSION_CONFLICT", "Source revision changed")
		}
		revision = current.Revision
		if dryRun {
			return map[string]any{"validated": true, "sideEffects": false}, nil
		}
		value, err = r.Discover(ctx, numeric, revision, resourceString(fields, "parentResourceRef"))
	case "streamProgramValidate":
		value, err = r.ValidationView(ctx, document, sourceRevision)
	case "streamProgramInvoke":
		if e := require(ctx, "uns.write"); e != nil {
			return nil, e
		}
		version, e := strconv.ParseInt(resourceString(fields, "expectedActiveVersion"), 10, 64)
		if e != nil || version < 1 {
			return nil, invalid("expectedActiveVersion", "positive decimal string required")
		}
		if dryRun {
			current, e := repo.NewSourceStreamRepo(ctx).Stream(ctx, numeric)
			if e != nil {
				return nil, e
			}
			if current.Status != "enabled" || current.ActiveVersion != version {
				return nil, problem("VERSION_CONFLICT", "active program version changed or disabled")
			}
			return map[string]any{"validated": true, "sideEffects": false}, nil
		}
		key := resourceString(fields, "idempotencyKey")
		if key == "" {
			key = uuid.NewString()
		}
		value, err = r.InvokeEntry(ctx, numeric, version, resourceString(fields, "entryId"), key, input, timeout)
	case "streamProgramTest":
		if resourceString(fields, "phase") == "fetch" {
			if e := require(ctx, "uns.stream.execute", "uns.source", "uns.read"); e != nil {
				return nil, e
			}
		}
		value, err = r.TestEntry(ctx, document, input, sourceRevision, resourceString(fields, "phase"), resourceString(fields, "entryId"))
	case "streamProgramCreate", "streamProgramUpdate":
		if dryRun {
			value, err = r.ValidationView(ctx, document, sourceRevision)
		} else {
			value, err = r.SaveStream(ctx, numeric, revision, document)
		}
	case "streamProgramGet":
		if err := fullRuntimeDocument(ctx, "stream"); err != nil {
			return nil, err
		}
		value, err = r.Stream(ctx, numeric)
	case "streamProgramVersionsList":
		var p Pagination
		raw, _ := json.Marshal(fields)
		_ = json.Unmarshal(raw, &p)
		page, size, e := bounds(p, 200)
		if e != nil {
			return nil, e
		}
		value, err = r.Versions(ctx, numeric, page, size)
		if err == nil && fullRuntimeDocument(ctx, "stream") != nil {
			listing := value.(map[string]any)
			rows, _ := listing["list"].([]map[string]any)
			for _, row := range rows {
				delete(row, "document")
				delete(row, "manifest")
			}
		}
	case "streamProgramDeploy", "streamProgramRollback", "streamProgramDisable":
		if e := require(ctx, "uns.stream.manage"); e != nil {
			return nil, e
		}
		current, e := r.Stream(ctx, numeric)
		if e != nil {
			return nil, e
		}
		if int64Number(current["revision"]) != revision {
			return nil, problem("VERSION_CONFLICT", "Stream revision changed")
		}
		if raw, provided := fields["expectedActiveVersion"]; provided {
			var expected string
			var value int64
			if json.Unmarshal(raw, &expected) != nil {
				return nil, invalid("expectedActiveVersion", "decimal string required")
			}
			value, e = strconv.ParseInt(expected, 10, 64)
			if e != nil || value < 0 {
				return nil, invalid("expectedActiveVersion", "nonnegative decimal string required")
			}
			if int64Number(current["activeVersion"]) != value {
				return nil, problem("VERSION_CONFLICT", "active Stream version changed")
			}
		}
		if resourceString(fields, "expectedReleaseState") == "inactive" && int64Number(current["activeVersion"]) != 0 {
			return nil, problem("VERSION_CONFLICT", "Stream is active")
		}
		if dryRun {
			if op.Name != "streamProgramDisable" {
				stream, e := repo.NewSourceStreamRepo(ctx).Stream(ctx, numeric)
				if e != nil {
					return nil, e
				}
				doc := stream.Document
				if op.Name == "streamProgramRollback" {
					version, e := strconv.ParseInt(resourceString(fields, "versionId"), 10, 64)
					if e != nil || version < 1 {
						return nil, invalid("versionId", "positive decimal string required")
					}
					saved, e := repo.NewSourceStreamRepo(ctx).Version(ctx, numeric, version)
					if e != nil {
						return nil, e
					}
					doc = saved.Document
				}
				var definition any
				if e = json.Unmarshal([]byte(doc), &definition); e != nil {
					return nil, e
				}
				if _, _, e = r.Validate(ctx, definition, sourceRevision, true); e != nil {
					return nil, invalid("document", "deployment dependencies or definition invalid")
				}
			}
			return map[string]any{"validated": true, "sideEffects": false, "revision": revision}, nil
		}
		if op.Name == "streamProgramDisable" {
			value, err = r.Disable(ctx, numeric, revision, false)
		} else {
			var version int64
			if op.Name == "streamProgramRollback" {
				version, err = strconv.ParseInt(resourceString(fields, "versionId"), 10, 64)
				if err != nil || version < 1 {
					return nil, invalid("versionId", "positive decimal string required")
				}
			}
			value, err = r.Deploy(ctx, numeric, revision, sourceRevision, version)
		}
	case "streamProgramRunsList":
		var p Pagination
		raw, _ := json.Marshal(fields)
		_ = json.Unmarshal(raw, &p)
		page, size, e := bounds(p, 200)
		if e != nil {
			return nil, e
		}
		value, err = r.Runs(ctx, repo.StreamRunFilter{StreamID: numeric, Status: resourceString(fields, "status"), Page: page, Size: size})
	case "streamProgramRunsGet":
		value, err = r.Run(ctx, resourceString(fields, "runId"))
		if err == nil {
			v := value.(map[string]any)
			if n, ok := v["streamId"].(int64); !ok || n != numeric {
				return nil, problem("NOT_FOUND", "run not found for this Stream")
			}
		}
	default:
		return nil, invalid("operation", "unsupported runtime operation")
	}
	return runtimeOperationResult(value, err)
}

func runtimeOperationResult(value any, err error) (any, error) {
	if err != nil {
		var fault *Fault
		if errors.As(err, &fault) {
			return nil, fault
		}
		if errors.Is(err, sourcestream.ErrForbidden) {
			return nil, problem("FORBIDDEN", "runtime permission required")
		}
		var v *sourcestream.ValidationError
		if errors.As(err, &v) {
			if v.Code == "unsupportedCapability" {
				return nil, problem("UNSUPPORTED_CAPABILITY", "The selected target does not implement "+v.Field)
			}
			return nil, invalid(v.Field, v.Code)
		}
		if errors.Is(err, repo.ErrConflict) {
			return nil, problem("VERSION_CONFLICT", "runtime revision or dependency changed")
		}
		if errors.Is(err, repo.ErrNotFound) {
			return nil, problem("NOT_FOUND", "runtime object not found")
		}
		switch {
		case errors.Is(err, sourcestream.ErrDeviceConnect):
			return nil, problem("SOURCE_DEVICE_CONNECT_FAILED", "Edge could not establish the device connection")
		case errors.Is(err, sourcestream.ErrDeviceSession):
			return nil, problem("SOURCE_DEVICE_SESSION_FAILED", "Edge could not create the device session")
		case errors.Is(err, sourcestream.ErrPreviewNodeRead):
			return nil, problem("SOURCE_NODE_READ_FAILED", "Edge could not read the requested OPC UA node")
		}
		return nil, problem("RUNTIME_OPERATION_FAILED", "Source/Stream runtime operation failed; inspect configuration, health and dependency revisions")
	}
	// v1 uses int64 IDs. v2 returns their exact decimal strings for JS/AI clients.
	raw, _ := json.Marshal(value)
	var normalized any
	_ = decodeNumberJSON(raw, &normalized)
	return runtimeIdentityStrings(normalized), nil
}

func runtimeIdentityStrings(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			if n, ok := x.(json.Number); ok && (k == "id" || (strings.HasSuffix(k, "Id") && k != "unitId") || k == "activeVersion" || k == "pendingVersion") {
				v[k] = n.String()
			} else {
				v[k] = runtimeIdentityStrings(x)
			}
		}
	case []any:
		for i, x := range v {
			v[i] = runtimeIdentityStrings(x)
		}
	}
	return value
}
