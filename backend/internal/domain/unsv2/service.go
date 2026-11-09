package unsv2

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type Service struct {
	recordChangesKey []byte
	changeMu         sync.Mutex
	changeSignal     chan struct{}
	recordWaiters    int
	sessionAuth      AppSessionVerifier
	metadata         *unsmeta.Service
	refresh          func(context.Context, contextx.Subject) (contextx.Subject, error)
	stop             context.CancelFunc
	done             chan struct{}
	dataWork         chan struct{}
	sourceRuntime    *sourcestream.Service
	queryCursorKey   []byte
}

func (s *Service) SetSourceRuntime(runtime *sourcestream.Service) {
	s.sourceRuntime = runtime
	if runtime == nil {
		return
	}
	runtime.SetRequestAuthorizer(s.authorizeAppRequest, s.refresh)
	runtime.SetControlAuthorizer(func(ctx context.Context, permission string) error {
		err := CheckAppPlatformPermission(ctx, permission)
		var fault *Fault
		if errors.As(err, &fault) && (fault.Code == "FORBIDDEN" || fault.Code == "UNAUTHENTICATED") {
			return sourcestream.ErrForbidden
		}
		return err
	})
}

func New(metadata *unsmeta.Service, refresh func(context.Context, contextx.Subject) (contextx.Subject, error), sessionAuth ...AppSessionVerifier) *Service {
	// The shared SQL pool is bounded at ten connections. Heavy data requests
	// leave two available for authentication and control/background work.
	service := &Service{metadata: metadata, refresh: refresh, dataWork: make(chan struct{}, 8)}
	// Isolated service instances still sign cursors. Production replaces this
	// ephemeral key with the durable deployment key during bootstrap.
	service.queryCursorKey = make([]byte, 32)
	if _, err := rand.Read(service.queryCursorKey); err != nil {
		panic(err)
	}
	if len(sessionAuth) > 0 {
		service.sessionAuth = sessionAuth[0]
	}
	return service
}

func (s *Service) admitDataWork(ctx context.Context) (func(), error) {
	if s.dataWork == nil {
		return func() {}, nil
	}
	select {
	case s.dataWork <- struct{}{}:
		return func() { <-s.dataWork }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func requestHash(batch Batch) string {
	batch.Mode = ""
	raw, _ := json.Marshal(batch)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func requestIdentity(ctx context.Context, key string) (string, error) {
	// Resource writes intentionally share this principal-wide namespace. Do not
	// insert a route here: doing so would bypass already persisted retry receipts.
	// Scoped batch adapters explicitly derive their own keys before calling us.
	subject, ok := contextx.SubjectFrom(ctx)
	if ok && subject.FleetTask != nil && subject.FleetTask.Scope == "runtime.execute" {
		if strings.TrimSpace(key) != key || key == "" || len(key) > 128 {
			return "", invalid("idempotencyKey", "1 to 128 characters required")
		}
		sum := sha256.Sum256([]byte("fleet/" + subject.FleetTask.NodeID + "/" + subject.FleetTask.GrantID + "/" + key))
		return hex.EncodeToString(sum[:]), nil
	}
	if !ok || subject.UserID <= 0 {
		return "", problem("UNAUTHENTICATED", "authenticated user required")
	}
	if strings.TrimSpace(key) != key || key == "" || len(key) > 128 {
		return "", invalid("idempotencyKey", "1 to 128 characters required")
	}
	identity := fmt.Sprintf("%d/%d/%s", subject.UserID, subject.APIKeyID, key)
	if subject.MCPGrantID != "" {
		identity += "/mcp/" + subject.MCPGrantID
	}
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:]), nil
}
func (s *Service) Apply(ctx context.Context, batch Batch) (Outcome, error) {
	out, err := s.apply(ctx, batch)
	if batch.IdempotencyKey == "" {
		err = keylessWriteError(err)
	}
	return out, err
}

func keylessWriteError(err error) error {
	var fault *Fault
	if errors.As(err, &fault) {
		copy := *fault
		copy.Retryable = false
		copy.RetryAdvice = &RetryAdvice{RecommendedAction: "readBusinessStateOrJobBeforeRetry"}
		if copy.RolledBack {
			copy.RetryAdvice.RecommendedAction = "inspectErrorAndCorrectRequest"
		}
		return &copy
	}
	return err
}

func (s *Service) apply(ctx context.Context, batch Batch) (Outcome, error) {
	checked, refreshErr := s.refreshAppStreamPolicyBatch(ctx, batch.Items)
	if refreshErr != nil {
		return Outcome{}, refreshErr
	}
	ctx = checked
	if batch.Mode == "" {
		batch.Mode = "auto"
	}
	if len(batch.Items) > MaxItems || len(batch.Items) == 0 {
		return Outcome{}, invalid("items", "1 to 10000 operations required")
	}
	if batch.Mode != "auto" && batch.Mode != "sync" && batch.Mode != "async" {
		return Outcome{}, invalid("mode", "expected auto, sync or async")
	}
	for index, item := range batch.Items {
		if err := item.validate(); err != nil {
			return Outcome{}, itemFault(index, item, err)
		}
		if err := mutationPermissions(ctx, item); err != nil {
			return Outcome{}, itemFault(index, item, err)
		}
	}
	key := batch.IdempotencyKey
	if key == "" {
		key = uuid.NewString() // Internal execution identity, never a client retry key.
	}
	identity, err := requestIdentity(ctx, key)
	if err != nil {
		return Outcome{}, err
	}
	digest := requestHash(batch)
	if batch.IdempotencyKey != "" {
		if existing, found, err := s.existing(ctx, identity, digest); found || err != nil {
			return existing, err
		}
	}
	p, err := newPlan(batch)
	if err != nil {
		return Outcome{}, err
	}
	if !batch.DryRun && (batch.Mode == "async" || batch.Mode == "auto" && (len(p.Items) > SyncItems || p.Cost > SyncItems)) {
		return s.enqueue(ctx, batch, p, identity, digest)
	}
	if batch.Mode == "sync" && len(p.Items) > SyncItems {
		return Outcome{}, invalid("mode", "sync supports at most 200 items; use auto or async")
	}
	budget := 10 * time.Second
	if batch.Mode == "auto" {
		budget = 3 * time.Second
	}
	if batch.DryRun {
		budget = 30 * time.Second
	}
	bounded, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	out, err := s.execute(bounded, batch, p, identity, digest, false)
	hasUpsert := false
	for _, item := range p.Items {
		hasUpsert = hasUpsert || item.Upsert
	}
	for attempt := 0; hasUpsert && attempt < 3 && err != nil && out.Status == "rolledBack" && !batch.DryRun && bounded.Err() == nil; attempt++ {
		var pg *pgconn.PgError
		var fault *Fault
		retry := errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01" || pg.Code == "23505" && strings.HasPrefix(pg.ConstraintName, "ux_"))
		retry = retry || errors.As(err, &fault) && (fault.Code == "DUPLICATE_KEY" || fault.Code == "TEMPORARILY_UNAVAILABLE")
		if !retry {
			break
		}
		out, err = s.execute(bounded, batch, p, identity, digest, false)
	}

	if batch.Mode == "auto" && !batch.DryRun && !hasRecordQuery(p.Items) && out.Status == "rolledBack" && err != nil && ctx.Err() == nil && (errors.Is(err, context.DeadlineExceeded) || bounded.Err() == context.DeadlineExceeded) {
		return s.enqueue(ctx, batch, p, identity, digest)
	}
	return out, classify(err)
}

type receipt struct {
	Permissions []Change `json:"permissions,omitempty"`
	Type        string   `json:"type"`
	Digest      string   `json:"digest"`
	Outcome     Outcome  `json:"outcome"`
}

func (s *Service) existing(ctx context.Context, identity, digest string) (Outcome, bool, error) {
	var row repo.UnsMeta
	err := repo.GetUnsConn(ctx).Where("kind='task' AND id=?", "api-receipt-"+identity).Take(&row).Error
	if err == nil {
		var r receipt
		if json.Unmarshal(row.Data, &r) != nil {
			return Outcome{}, true, problem("INTERNAL_ERROR", "invalid saved receipt")
		}
		if r.Digest != digest {
			return Outcome{}, true, problem("IDEMPOTENCY_CONFLICT", "same key with different request")
		}
		r.Outcome.Replayed = true
		return r.Outcome, true, nil
	}
	if !errors.Is(err, repo.ErrNotFound) {
		return Outcome{}, false, err
	}
	var job repo.UnsMeta
	err = repo.GetUnsConn(ctx).Where("kind='task' AND id=?", "api-job-"+identity).Take(&job).Error
	if errors.Is(err, repo.ErrNotFound) {
		return Outcome{}, false, nil
	}
	if err != nil {
		return Outcome{}, false, err
	}
	var data jobData
	if json.Unmarshal(job.Data, &data) != nil {
		return Outcome{}, true, problem("INTERNAL_ERROR", "invalid saved job")
	}
	if data.Digest != digest {
		return Outcome{}, true, problem("IDEMPOTENCY_CONFLICT", "same key with different request")
	}
	return Outcome{Status: "accepted", JobID: job.ID, Replayed: true}, true, nil
}
func (s *Service) execute(ctx context.Context, batch Batch, p *plan, identity, digest string, worker bool) (Outcome, error) {
	requestCtx := ctx
	dataOnly := true
	queryPlan := false
	filterPlan := false
	for _, item := range p.Items {
		queryPlan = queryPlan || item.Action == "record.query"
		filterPlan = filterPlan || len(item.Filter) > 0
		switch item.Action {
		case "record.create", "record.replace", "record.delete", "record.restore", "record.check", "record.read", "record.query", "record.modify", "record.update", "sample.append":
		default:
			dataOnly = false
		}
	}
	if queryPlan {
		for _, item := range p.Items {
			if !strings.HasPrefix(item.Action, "record.") {
				return Outcome{}, invalid("items", "record.query may only participate in Record transactions")
			}
		}
	}
	for index, item := range p.Items {
		if err := mutationPermissions(ctx, item); err != nil {
			return Outcome{}, itemFault(index, item, err)
		}
	}
	if dataOnly && len(p.Items) > 1 {
		release, err := s.admitDataWork(ctx)
		if err != nil {
			return Outcome{Status: "rolledBack"}, &Fault{Code: "TEMPORARILY_UNAVAILABLE", Message: "Batch budget expired or request canceled before execution", Retryable: true, RolledBack: true}
		}
		defer release()
	}
	out := Outcome{Status: "committed", Results: make([]Result, len(p.Items))}
	if batch.DryRun {
		out.Status = "validated"
	}
	// A commit transport error can arrive after PostgreSQL committed. Only
	// callback failures prove that this transaction did not reach commit.
	transactionBodyCompleted := false
	var options *sql.TxOptions
	if queryPlan || filterPlan {
		options = &sql.TxOptions{Isolation: sql.LevelSerializable}
	}
	err := repo.MetadataEventTransaction(ctx, repo.GetUnsConn(ctx), func(tx *gorm.DB) error {
		checks := []appStreamPolicyCheck{}
		ctx = context.WithValue(ctx, appStreamPolicyChecksKey{}, &checks)
		writer := tx
		if dataOnly {
			// Every data-operation failure exits this callback and rolls back the
			// entire batch. Nested writer savepoints cannot provide partial success.
			writer = tx.Session(&gorm.Session{DisableNestedTransaction: true})
		}
		ctx = repo.WithUnsTransaction(ctx, writer)
		// Match the existing writer lock: all schema/ref changes use an exclusive
		// metadata lock; ordinary record ingestion retains shared access.
		retainReceipt := batch.IdempotencyKey != "" || worker
		if retainReceipt {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,812300001))", identity).Error; err != nil {
				return err
			}
			checked, refreshErr := s.refreshAppStreamPolicyBatch(ctx, batch.Items)
			if refreshErr != nil {
				return refreshErr
			}
			ctx = checked
			var previous repo.UnsMeta
			err := tx.Where("kind='task' AND id=?", "api-receipt-"+identity).Take(&previous).Error
			if err == nil {
				var r receipt
				if json.Unmarshal(previous.Data, &r) != nil || r.Digest != digest {
					return problem("IDEMPOTENCY_CONFLICT", "same key with different request")
				}
				out = r.Outcome
				out.Replayed = true
				transactionBodyCompleted = true
				return nil
			}
			if !errors.Is(err, repo.ErrNotFound) {
				return err
			}
			if !worker && !batch.DryRun {
				if existing, found, err := s.existing(ctx, identity, digest); found || err != nil {
					out = existing
					transactionBodyCompleted = err == nil
					return err
				}
			}
		}
		locked, err := lockPlan(tx, p)
		if err != nil {
			return err
		}
		if len(p.AssertLocks) > 0 {
			for _, item := range p.Items {
				if item.CollectionID.ID != "" {
					if err := repo.RequireLocalCollection(tx, item.CollectionID.ID); err != nil {
						return classify(err)
					}
				}
			}
		}
		if err := checkRecordLockAssertions(ctx, tx, p.AssertLocks); err != nil {
			return err
		}
		if dataOnly {
			ctx = unsmeta.WithPrelockedRecords(ctx, writer, locked)
		}
		completed := map[string]Result{}
		written := map[repo.UnsRecordLock]bool{}
		for key := range priorRecordWriteTargets(ctx) {
			written[key] = true
		}
		resultBytes := 0
		resultLimit := MaxQueryResponseBytes
		if actor, ok := contextx.SubjectFrom(ctx); ok && actor.MCPRestricted {
			// Leave room for MCP's 256 KiB envelope and redacted projections.
			// Reject in the transaction, before any write can commit.
			resultLimit = 120 << 10
		}
		if remaining, ok := ctx.Value(recordResultBudgetKey{}).(int); ok && remaining < resultLimit {
			resultLimit = remaining
		}
		for _, index := range p.Order {
			item := p.Items[index]
			if err := mutationPermissions(ctx, item); err != nil {
				return itemFault(index, item, err)
			}
			itemContext, deliveries := repo.CaptureCollectionDelivery(ctx)
			var result Result
			var err error
			item, err = resolveBindings(item, completed)
			if err == nil && len(item.Filter) > 0 {
				item, err = resolveRecordFilter(ctx, tx, item)
			}
			if err == nil && item.noMatch {
				out.Results[index] = Result{OpID: item.OpID, Action: item.Action, CollectionID: item.CollectionID.ID, Status: "notMatched"}
				completed[item.OpID] = out.Results[index]
				continue
			}
			if item.LockToken == "" {
				item.LockToken = item.LockTokens[item.ID.ID]
			}
			if err == nil && strings.HasPrefix(item.Action, "record.") && item.Action != "record.query" {
				key := repo.UnsRecordLock{CollectionID: item.CollectionID.ID, RecordID: item.ID.ID}
				if item.Action != "record.read" && item.Action != "record.check" {
					if written[key] {
						err = invalid("id", "multiple writes resolve to the same Record in this transaction")
					}
					written[key] = true
				}
			}
			if err != nil {
				// The same diagnostic/rollback path applies to reference failures.
			} else if item.Action == "record.query" {
				result, err = s.recordTransactionQuery(ctx, tx, item)
			} else if transactionAction(item.Action) {
				result, err = s.recordTransaction(ctx, tx, item)
			} else if strings.HasPrefix(item.Action, "meta.") || item.Action == "stream.publish" {
				result, err = s.metadataChange(ctx, tx, item)
			} else {
				result, err = s.dataChange(itemContext, tx, item)
			}
			if err != nil {
				return itemFault(index, item, err)
			}
			if len(*deliveries) > 0 && !batch.DryRun {
				result.Delivery = &WriteDelivery{State: "pendingUpload", LocalCommitted: true, Pending: len(*deliveries)}
				for _, delivery := range *deliveries {
					result.Delivery.EventIDs = append(result.Delivery.EventIDs, delivery.EventID)
					result.Delivery.LocalCommitted = result.Delivery.LocalCommitted && delivery.Policy.Mode == "both"
				}
			} else if !batch.DryRun && result.CollectionID != "" && !contains([]string{"record.read", "record.check", "record.query"}, item.Action) {
				result.Delivery = &WriteDelivery{State: "localCompleted", LocalCommitted: true}
			}
			out.Results[index] = result
			if result.Status == "pendingUpload" && !batch.DryRun {
				out.Status = "accepted"
			}
			completed[item.OpID] = result
			rawResult, _ := json.Marshal(result)
			resultBytes += len(rawResult)
			if resultBytes > resultLimit {
				return itemFault(index, item, problem("RESPONSE_TOO_LARGE", "transaction results exceed the transport budget; reduce returned documents or split independent items"))
			}
		}
		if err := s.recheckAppStreamPolicyWrites(ctx); err != nil {
			return err
		}
		if actor, ok := contextx.SubjectFrom(ctx); ok && actor.MCPRestricted {
			// 大批次提交前再次检查授权，撤销/降权发生在执行期间时整体回滚。
			current, err := s.refresh(ctx, actor)
			if err != nil {
				return problem("FORBIDDEN", "MCP authorization is no longer valid")
			}
			checked := contextx.WithSubject(ctx, current)
			for index, item := range p.Items {
				if err := mutationPermissions(checked, item); err != nil {
					return itemFault(index, item, err)
				}
				if item.Kind == "app" && (item.Action == "meta.create" || item.Action == "meta.replace" || item.Action == "meta.patch") {
					var data AppData
					appRow, err := meta(checked, "app", item.ID.ID, false)
					if err != nil {
						return err
					}
					if err := json.Unmarshal(appRow.Data, &data); err != nil {
						return err
					}
					if err := validateApp(checked, data); err != nil {
						return err
					}
				}
				if item.Kind == "app" && item.Action == "meta.resourcePatch" {
					row, err := meta(checked, "app", item.ID.ID, false)
					if err != nil {
						return err
					}
					var data AppData
					if err = json.Unmarshal(row.Data, &data); err != nil {
						return err
					}
					if err = validateApp(checked, data); err != nil {
						return err
					}
				}
			}
		}
		if batch.DryRun {
			return dryRunRollback
		}
		if retainReceipt {
			raw, _ := json.Marshal(receipt{Type: "apiReceipt", Digest: digest, Outcome: out, Permissions: permissionDescriptors(p.Items)})
			now := time.Now().UnixMilli()
			if err := tx.Create(&repo.UnsMeta{Kind: "task", ID: "api-receipt-" + identity, Data: raw, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
				return err
			}
		}
		transactionBodyCompleted = true
		return nil
	}, options)
	// PostgreSQL explicitly rejected the transaction at commit. Unlike a lost
	// connection these errors prove rollback, even after the callback succeeded.
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01") {
		transactionBodyCompleted = false
	}
	if errors.Is(err, dryRunRollback) {
		err = nil
	}
	if err != nil && (queryPlan || filterPlan) && !transactionBodyCompleted && !batch.DryRun && batch.IdempotencyKey != "" {
		// A serializable snapshot taken before waiting on the receipt lock may
		// not see the concurrent winner. After rollback, replay its durable result
		// using a fresh connection; never re-execute the business transaction here.
		if saved, found, lookupErr := s.existing(requestCtx, identity, digest); found && lookupErr == nil && saved.Status != "accepted" {
			return saved, nil
		}
	}
	if err != nil {
		out = Outcome{Status: "rolledBack"}
		if transactionBodyCompleted {
			out.Status = "unknown"
		}
		err = classify(err)
		var fault *Fault
		if errors.As(err, &fault) {
			copy := *fault
			copy.RolledBack = !transactionBodyCompleted
			if transactionBodyCompleted {
				copy.RetryAdvice = &RetryAdvice{RecommendedAction: "queryTransactionStatusBeforeRetry", CanRetrySameRequest: true, KeepIdempotencyKey: true}
			}
			if batch.IdempotencyKey == "" {
				copy.RetryAdvice = &RetryAdvice{RecommendedAction: "readBusinessStateOrJobBeforeRetry"}
			}
			err = &copy
		}
	}
	return out, err
}

var dryRunRollback = errors.New("UNS dry run rollback")

func (s *Service) Execute(ctx context.Context, operation string, raw []byte) (value any, err error) {
	if retiredMetadataOperation(operation) {
		return nil, metadataRejected(problem("UNSUPPORTED_OPERATION", "use the metadata document or dedicated resource operation"))
	}
	if subject, _ := contextx.SubjectFrom(ctx); subject.APIKeyUsageType == repo.APIKeyUsageAppData && !IsAppOperationAllowed(operation) {
		return nil, problem("FORBIDDEN", "this operation is not available to App keys")
	}
	// Resource runtime/validation errors can occur before Apply. They must not
	// advertise receipt-based retries for requests without a caller key either.
	if resource, ok := FindResourceOperation(operation); ok && resource.Mutation != "" {
		var request struct {
			IdempotencyKey string `json:"idempotencyKey"`
		}
		_ = json.Unmarshal(raw, &request)
		if request.IdempotencyKey == "" {
			defer func() { err = keylessWriteError(err) }()
		}
	}
	if result, handled, err := s.executeAI(ctx, operation, raw); handled {
		return result, err
	}
	if resource, ok := FindResourceOperation(operation); ok {
		return s.resourceExecute(ctx, resource, raw)
	}
	if operation == "appContext" {
		return s.appContext(ctx)
	}
	if operation == "appKeyCreate" || operation == "appKeyRevoke" {
		var request AppKeyRequest
		if err := Decode(raw, &request); err != nil {
			return nil, err
		}
		return s.appKey(ctx, operation, request)
	}
	if operation == "catalog" {
		if err := require(ctx, "uns.read"); err != nil {
			return nil, err
		}
		catalog := Catalog()
		subject, _ := contextx.SubjectFrom(ctx)
		external := subject.AuthType == "apiKey" || subject.AuthType == "mcpOAuth" || subject.MCPRestricted
		operations, _ := catalog["operations"].([]any)
		visible := make([]any, 0, len(operations))
		for _, entry := range operations {
			op, ok := entry.(map[string]any)
			if !ok || external && (op["coreOnly"] == true || op["deprecated"] == true) || !external && op["openapiOnly"] == true {
				continue
			}
			visible = append(visible, entry)
		}
		catalog["operations"] = visible
		return catalog, nil
	}
	if operation == "apply" {
		var request Batch
		if err := Decode(raw, &request); err != nil {
			return nil, err
		}
		return s.Apply(ctx, request)
	}
	if operation == "samples" || operation == "records" && !internalLegacyDataQuery(ctx, raw) {
		var request DataQueries
		if err := Decode(raw, &request); err != nil {
			return nil, err
		}
		return s.DataQuery(ctx, operation, request)
	}
	var request Queries
	if err := Decode(raw, &request); err != nil {
		return nil, err
	}
	return s.Query(ctx, operation, request)
}

func Catalog() map[string]any {
	var catalog map[string]any
	_ = json.Unmarshal([]byte(ContractJSON), &catalog)
	for key, value := range map[string]any{"contractVersion": catalog["version"], "kinds": Kinds,
		"limits": map[string]any{"syncItems": SyncItems, "maxItems": MaxItems, "maxBodyBytes": MaxBodyBytes, "maxQueries": MaxQueries, "transactionQueryMaxRows": MaxTransactionQueryRows},
		"modes":  []string{"auto", "sync", "async"}, "jobStates": []string{"queued", "running", "succeeded", "failed"},
		"actions":      []string{"meta.create", "meta.replace", "meta.patch", "meta.resourcePatch", "meta.copy", "meta.archive", "meta.restore", "stream.publish", "record.create", "record.replace", "record.delete", "record.restore", "record.check", "record.read", "record.query", "record.modify", "record.update", "sample.append"},
		"capabilities": map[string]bool{"atomicBatch": true, "partialArrayWrites": true, "transactionQueries": true, "conditionalTransactions": true, "atomicFieldOperations": true, "transactionResultBindings": true, "collectionUniqueIndexes": true, "automaticAsync": true, "reusableStreamConfiguration": true, "sharedStreamExecution": false, "afterCommitCommands": false, "appScopedData": true, "recordFilters": true, "recordOrdering": true}} {
		catalog[key] = value
	}
	catalog["models"] = Models()
	capabilities := catalog["capabilities"].(map[string]bool)
	capabilities["scopedBatches"] = true
	capabilities["metadataBatch"] = false
	capabilities["metadataDocuments"] = true
	capabilities["metadataTransactions"] = true
	actions := []string{}
	for _, operation := range ResourceOperations() {
		if !contains(actions, operation.Name) {
			actions = append(actions, operation.Name)
		}
	}
	catalog["actions"] = actions
	capabilities["recordBatch"] = true
	capabilities["sampleAppendBatch"] = true
	// Shared acquisition and complete program deployment have distinct lifecycles.
	capabilities["sharedStreamExecution"] = true
	capabilities["sourceStreamRuntimeLifecycle"] = true
	capabilities["relationalQueries"] = true
	capabilities["recordJoins"] = true
	capabilities["recordAggregates"] = true
	capabilities["preaggregatedJoins"] = true
	capabilities["computedQueryColumns"] = true
	capabilities["recordScopedBatchLocks"] = true
	capabilities["booleanQueryPredicates"] = true
	capabilities["pipelineQueries"] = true
	capabilities["sampleFilters"] = true
	capabilities["queryFirstResult"] = true
	capabilities["sharedWriteFilters"] = true
	capabilities["signedKeysetPagination"] = true
	capabilities["escapedFieldPaths"] = true
	capabilities["arrayPredicates"] = true
	capabilities["arrayFieldOperations"] = true
	capabilities["collectionIndexes"] = true
	capabilities["guardedLookups"] = true
	capabilities["dateAndConditionalExpressions"] = true
	capabilities["atomicFirstUpdate"] = true
	capabilities["returnedRecordDocuments"] = true
	limits := catalog["limits"].(map[string]any)
	limits["queryCursorMaxBytes"] = pipelineCursorMaxBytes
	limits["expressionStringMaxBytes"] = maxExpressionStringBytes
	limits["lookupMaxOuterRows"] = maxLookupOuterRows
	limits["lookupMaxInputRows"] = maxLookupInputRows
	limits["lookupMaxOutputRows"] = maxLookupOutputRows
	limits["lookupMaxStageBytes"] = lookupByteBudget
	capabilities["authorizedDataDiscovery"] = true
	capabilities["collectionSemantics"] = true
	capabilities["queryValidation"] = true
	capabilities["transactionValidation"] = true
	capabilities["transactionStatus"] = true
	catalog["limits"].(map[string]any)["relationalQueries"] = map[string]int{"joins": 4, "physicalJoins": 4, "subqueries": 4, "expressionNodes": 64, "expressionDepth": 6, "joinConditions": 8, "select": 32, "groupBy": 8, "having": 16, "orderBy": 8, "predicateNodes": 64, "predicateDepth": 6, "inValues": 100, "pageSize": 200, "maxOffset": 1000000, "queryTimeoutMs": 5000, "batchTimeoutMs": 15000, "maxResponseBytes": MaxQueryResponseBytes}
	catalog["relations"] = Relations()
	return catalog
}
