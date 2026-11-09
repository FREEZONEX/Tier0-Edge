package uns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	domainaudit "backend/internal/domain/audit"
	domaincommon "backend/internal/domain/common"
	"backend/internal/events"
	"backend/internal/repo"
	"gorm.io/gorm"
)

// ImportValidationError identifies a permanent payload failure; storage errors remain retryable.
type ImportValidationError struct{ Err error }

func (e *ImportValidationError) Error() string { return e.Err.Error() }
func (e *ImportValidationError) Unwrap() error { return e.Err }
func IsImportValidationError(err error) bool {
	var validation *ImportValidationError
	return errors.As(err, &validation) || errors.Is(err, ErrInvalidImportFileType) || errors.Is(err, ErrInvalidJSON) || errors.Is(err, ErrInvalidImportFormat) || errors.Is(err, ErrInvalid)
}

const importHistoryBatchSize = 100

type importBatchState struct {
	tree           *createTreeState
	existing       []repo.UnsNode
	labelIDs       map[string]int64
	labels         map[int64][]int64
	flows          map[int64]SaveCommand
	original       map[int64]bool
	seen           map[int64]bool
	updated        []repo.UnsNode
	writes         []repo.ImportedUnsNodeUpdate
	originalLabels map[int64][]int64
	unchanged      int
	history        []repo.UnsNode
}

// Prepare against one snapshot, then persist bounded SQL batches atomically.
// Reuse the OpenAPI planner but keep the file import's strict alias and tree rules.
func (s *Service) importBatch(ctx context.Context, payload map[string]any, userID int64, jobKey string, emit func(ImportStatus)) (map[string]any, error) {
	var result map[string]any
	var created, updated []repo.UnsNode
	err := repo.GetCommonConn(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, unsCreateBatchAdvisoryKey).Error; err != nil {
			return err
		}
		jobs := repo.NewAsyncJobRepo(tx)
		if jobKey != "" {
			job, err := jobs.GetAsyncJob(ctx, jobKey)
			if err != nil {
				return err
			}
			if job.Status == repo.AsyncJobStatusSucceeded || job.Status == repo.AsyncJobStatusFailed || job.Status == repo.AsyncJobStatusCanceled {
				return json.Unmarshal(job.ResultJSON, &result)
			}
			var previous map[string]any
			if err := json.Unmarshal(job.ResultJSON, &previous); err != nil {
				return err
			}
			if previous["phase"] == "history" {
				result = previous
				return nil
			}
			// Progress uses a separate connection so polling can see it before commit.
			if err := s.jobs.MarkAsyncJobRunning(ctx, jobKey); err != nil {
				return err
			}
		}
		txService := *s
		txService.uns = repo.NewUnsRepo(tx)
		existing, err := txService.uns.ListUnsNodesForImport(ctx)
		if err != nil {
			return err
		}
		batch := &importBatchState{tree: &createTreeState{service: s, ctx: ctx}, existing: existing, labels: map[int64][]int64{}, flows: map[int64]SaveCommand{}, original: map[int64]bool{}, seen: map[int64]bool{}}
		for _, node := range existing {
			batch.original[node.ID] = true
		}
		batch.originalLabels, err = txService.uns.ListUnsNodeLabelBindings(ctx)
		if err != nil {
			return err
		}
		batch.tree.initialize(existing)
		imported, skipped, err := txService.importLabels(ctx, payload, userID, emit)
		if err != nil {
			return err
		}
		batch.labelIDs, err = txService.importLabelIDs(ctx)
		if err != nil {
			return err
		}
		count, skip, err := txService.importNamespacePayload(ctx, payload, userID, emit, batch)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &ImportValidationError{Err: err}
		}
		// Implicit folders can later occur explicitly in the file.
		for i, node := range batch.tree.created {
			batch.tree.created[i] = batch.tree.byID[node.ID]
		}
		if err := txService.uns.UpdateImportedUnsNodes(ctx, batch.writes); err != nil {
			return err
		}
		if err := txService.uns.InsertUnsNodes(ctx, batch.tree.created); err != nil {
			return err
		}
		if err := txService.uns.ReplaceImportedUnsNodeLabels(ctx, batch.labels); err != nil {
			return err
		}
		physical := append([]repo.UnsNode{}, batch.history...)
		for _, node := range batch.tree.created {
			if node.Type == 2 && node.EnableHistory == 1 {
				physical = append(physical, node)
			}
		}
		historyBatches := (len(physical) + importHistoryBatchSize - 1) / importHistoryBatchSize
		for start := 0; start < len(physical); start += importHistoryBatchSize {
			nodes := physical[start:min(start+importHistoryBatchSize, len(physical))]
			if err := s.outbox.EnqueueOnTx(tx, "uns.physical.ensure", "uns", strconv.FormatInt(nodes[0].ID, 10), map[string]any{"nodes": nodes, "importJobKey": jobKey}); err != nil {
				return err
			}
		}
		result = map[string]any{"imported": imported + count - len(batch.updated) - batch.unchanged, "updated": len(batch.updated), "skipped": skipped + skip + batch.unchanged}
		if jobKey != "" {
			if historyBatches > 0 {
				result["phase"] = "history"
				result["historyBatches"] = historyBatches
				result["historyCompletedBatches"] = []string{}
				if err := jobs.UpdateAsyncJobResult(ctx, jobKey, 70, result); err != nil {
					return err
				}
			} else if err := CompleteImportJob(ctx, jobs, jobKey, repo.AsyncJobStatusSucceeded, "", result); err != nil {
				return err
			}
		}
		created = batch.tree.created
		updated = batch.updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(created) > 0 && s.bus != nil {
		// Reliable side effects were enqueued in the transaction above.
		_ = s.bus.Publish(ctx, events.PhaseAfterCommit, events.UnsNodesCreated{Nodes: created})
	}
	if len(updated) > 0 && s.bus != nil {
		_ = s.bus.Publish(ctx, events.PhaseAfterCommit, events.UnsNodesUpdated{Nodes: updated})
	}
	return result, nil
}

func (b *importBatchState) prepare(cmd SaveCommand) (repo.UnsNode, error) {
	if cmd.AddFlow || cmd.MockData {
		return repo.UnsNode{}, &ImportValidationError{Err: ErrInvalid}
	}
	if err := b.tree.ctx.Err(); err != nil {
		return repo.UnsNode{}, err
	}
	// Preserve slash-path imports, including implicit folders and topic containers.
	path := strings.Trim(cmd.Name, "/")
	if strings.Contains(path, "/") {
		parts := splitNamespaceParts(path)
		nodeType, err := nodeTypeValue(cmd.NodeType)
		if err != nil {
			return repo.UnsNode{}, err
		}
		if err := validateCreatePathParts(path, parts, nodeType); err != nil {
			return repo.UnsNode{}, err
		}
		folders := parts[:len(parts)-1]
		if nodeType == 2 {
			if len(parts) < 2 {
				return repo.UnsNode{}, ErrInvalid
			}
			inferred := systemTopicTypeByName(parts[len(parts)-2])
			topicType, err := topicTypeValue(cmd.TopicType)
			if err != nil {
				return repo.UnsNode{}, err
			}
			if inferred == 0 || (topicType != 0 && topicType != inferred) {
				return repo.UnsNode{}, ErrInvalid
			}
			cmd.TopicType = topicTypeName(inferred)
			folders = parts[:len(parts)-2]
		}
		for _, name := range folders {
			folder, ok := b.tree.childByName(cmd.ParentID, name)
			if ok {
				if folder.Type != 1 || folder.TopicType != 0 {
					return repo.UnsNode{}, ErrInvalid
				}
			} else {
				folder, err = b.tree.prepareNode(SaveCommand{ParentID: cmd.ParentID, Name: name, DisplayName: name, NodeType: "folder", UserID: cmd.UserID})
				if err != nil {
					return repo.UnsNode{}, err
				}
			}
			cmd.ParentID = folder.ID
		}
		cmd.Name = parts[len(parts)-1]
	}
	if domaincommon.ExceedNameLimit(strings.TrimSpace(cmd.Name)) || domaincommon.ExceedMaxLength(strings.TrimSpace(cmd.DisplayName), domaincommon.DisplayNameMaxLen) || domaincommon.ExceedDescLimit(cmd.Description) {
		return repo.UnsNode{}, ErrInvalid
	}
	if strings.TrimSpace(cmd.Name) == "" {
		return repo.UnsNode{}, ErrInvalid
	}
	nodeType, err := nodeTypeValue(cmd.NodeType)
	if err != nil {
		return repo.UnsNode{}, err
	}
	if nodeType == 2 {
		cmd, err = b.tree.prepareFileParent(cmd)
		if err != nil {
			return repo.UnsNode{}, err
		}
	}
	current, exists := b.tree.childByName(cmd.ParentID, cmd.Name)
	var node repo.UnsNode
	if exists {
		if b.seen[current.ID] {
			return repo.UnsNode{}, fmt.Errorf("duplicate import path: %s", current.Namespace)
		}
		topicType, err := topicTypeValue(cmd.TopicType)
		if err != nil {
			return repo.UnsNode{}, err
		}
		if current.Type != nodeType || current.TopicType != topicType {
			return repo.UnsNode{}, ErrInvalid
		}
		alias := normalizeAliasCandidate(cmd.Alias)
		if alias != "" && alias != current.Alias {
			return repo.UnsNode{}, fmt.Errorf("alias does not match existing node: namespace=%s alias=%s", current.Namespace, alias)
		}
		// Cloud import only owns description/schema/extension properties, plus
		// history and labels on topics. Preserve display name, mock settings,
		// data quality policy, template, identity and unrelated metadata.
		cmd.ID, cmd.Alias, cmd.Name = current.ID, current.Alias, current.Name
		cmd.DataQualityPolicy = nil
		candidate, err := b.tree.commandToNode(cmd, current.Namespace, current.ID)
		if err != nil {
			return repo.UnsNode{}, err
		}
		node = current
		columns := []string{}
		schemaChanged := !importJSONEqual(current.Schema, candidate.Schema)
		if current.Description != candidate.Description {
			node.Description = candidate.Description
			columns = append(columns, "description")
		}
		if schemaChanged {
			if err := validateImportSchemaChange(current, candidate.Schema); err != nil {
				return repo.UnsNode{}, err
			}
			node.Schema = candidate.Schema
			columns = append(columns, "schema")
		}
		if !importJSONEqual(current.ExtendProperties, candidate.ExtendProperties) {
			node.ExtendProperties = candidate.ExtendProperties
			columns = append(columns, "extendproperties")
		}
		labelsChanged := node.Type == 2 && !importLabelIDsEqual(b.originalLabels[node.ID], cmd.LabelIDs)
		if node.Type == 2 && current.EnableHistory != candidate.EnableHistory {
			node.EnableHistory = candidate.EnableHistory
			columns = append(columns, "enable_history")
		}
		b.seen[node.ID] = true
		if len(columns) == 0 && !labelsChanged {
			if b.original[node.ID] {
				b.unchanged++
			}
			return node, nil
		}
		if len(columns) > 0 {
			node.UpdatedBy, node.UpdatedTime = cmd.UserID, time.Now().UTC()
			if err := b.tree.service.publishInTx(b.tree.ctx, events.UnsNodeSaving{Action: "update", Node: node, UserID: cmd.UserID}); err != nil {
				return repo.UnsNode{}, err
			}
		}
		if labelsChanged {
			b.labels[node.ID] = cmd.LabelIDs
		}
		if b.original[node.ID] {
			b.updated = append(b.updated, node)
			if len(columns) > 0 {
				b.writes = append(b.writes, repo.ImportedUnsNodeUpdate{Node: node, Columns: columns})
			}
			if node.Type == 2 && node.EnableHistory == 1 && (current.EnableHistory != 1 || schemaChanged) {
				b.history = append(b.history, node)
			}
		}
	} else {
		node, err = b.tree.prepareNode(cmd)
		if err != nil {
			return repo.UnsNode{}, err
		}
		node.DataQualityPolicy = normalizeFirstVersionDataQualityPolicy(cmd.DataQualityPolicy, node.Schema)
		b.labels[node.ID] = cmd.LabelIDs
		if node.Type == 2 && cmd.AddFlow {
			b.flows[node.ID] = cmd
		}
	}
	b.tree.indexNode(node)
	b.seen[node.ID] = true

	return node, nil
}

// CompleteImportHistory checkpoints each DDL batch exactly once. Metadata and
// DDL can use different databases, so failed DDL is retried without reimporting nodes.
func (s *Service) CompleteImportHistory(ctx context.Context, jobKey, eventID string, failure error) error {
	if jobKey == "" {
		return nil
	}
	return repo.GetCommonConn(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		jobs := repo.NewAsyncJobRepo(tx)
		job, err := jobs.GetAsyncJobForUpdate(ctx, jobKey)
		if err != nil {
			return err
		}
		if job.Status == repo.AsyncJobStatusSucceeded || job.Status == repo.AsyncJobStatusFailed || job.Status == repo.AsyncJobStatusCanceled {
			return nil
		}
		var result map[string]any
		if err := json.Unmarshal(job.ResultJSON, &result); err != nil {
			return err
		}
		if result["phase"] != "history" {
			return ErrInvalid
		}
		if failure != nil {
			result["phase"] = "historyFailed"
			return CompleteImportJob(ctx, jobs, jobKey, repo.AsyncJobStatusFailed, failure.Error(), result)
		}
		completed, _ := result["historyCompletedBatches"].([]any)
		for _, id := range completed {
			if id == eventID {
				return nil
			}
		}
		completed = append(completed, eventID)
		result["historyCompletedBatches"] = completed
		total := int(toInt64(result["historyBatches"]))
		if total <= 0 {
			return ErrInvalid
		}
		if len(completed) >= total {
			result["phase"] = "complete"
			return CompleteImportJob(ctx, jobs, jobKey, repo.AsyncJobStatusSucceeded, "", result)
		}
		return jobs.UpdateAsyncJobResult(ctx, jobKey, 70+30*len(completed)/total, result)
	})
}

// Compare decoded JSON so jsonb key ordering and whitespace do not create writes.
func importJSONEqual(left, right json.RawMessage) bool {
	decode := func(raw json.RawMessage) (any, error) {
		if len(raw) == 0 {
			return nil, nil
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		err := decoder.Decode(&value)
		switch v := value.(type) {
		case []any:
			if len(v) == 0 {
				value = nil
			}
		case map[string]any:
			if len(v) == 0 {
				value = nil
			}
		}
		return value, err
	}
	a, errA := decode(left)
	b, errB := decode(right)
	return errA == nil && errB == nil && reflect.DeepEqual(a, b)
}

func importLabelIDsEqual(left, right []int64) bool {
	set := func(ids []int64) map[int64]bool {
		out := map[int64]bool{}
		for _, id := range ids {
			if id > 0 {
				out[id] = true
			}
		}
		return out
	}
	return reflect.DeepEqual(set(left), set(right))
}

// Match the cloud import schema boundary: field types stay fixed and Metric
// fields cannot be deleted. Additions and order/metadata changes are allowed.
func validateImportSchemaChange(current repo.UnsNode, next json.RawMessage) error {
	if current.Type != 2 {
		return nil
	}
	fields := func(schema json.RawMessage) map[string]string {
		out := map[string]string{}
		for _, raw := range schemaFieldsFromRaw(schema) {
			if field, ok := raw.(map[string]any); ok {
				out[strings.TrimSpace(fmt.Sprint(field["name"]))] = strings.ToUpper(strings.TrimSpace(fmt.Sprint(field["type"])))
			}
		}
		return out
	}
	oldFields, newFields := fields(current.Schema), fields(next)
	for name, oldType := range oldFields {
		newType, exists := newFields[name]
		if exists && oldType != newType {
			return fmt.Errorf("field type change is not allowed: %s", name)
		}
		if !exists && current.TopicType == 3 {
			return fmt.Errorf("metric schema field deletion is not allowed: %s", name)
		}
	}
	return nil
}

// CompleteImportJob commits the final job state and audit result together, once.
func CompleteImportJob(ctx context.Context, jobs *repo.AsyncJobRepo, jobKey, status, message string, result map[string]any) error {
	job, err := jobs.GetAsyncJob(ctx, jobKey)
	if err != nil {
		return err
	}
	detail := map[string]any{"jobKey": jobKey, "jobType": "uns.import", "status": status, "error": message}
	var request struct {
		SourceFileID int64 `json:"sourceFileId"`
	}
	if json.Unmarshal(job.RequestJSON, &request) == nil && request.SourceFileID > 0 {
		detail["sourceFileId"] = request.SourceFileID
	}
	for key, value := range result {
		detail[key] = value
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	code := int64(200)
	if status == repo.AsyncJobStatusFailed {
		code = 500
	}
	return jobs.CompleteAsyncJobWithAudit(ctx, jobKey, status, message, result, repo.AuditLog{
		UserID: job.CreatedBy, ScopeType: domainaudit.ScopeTypePlatform,
		ResType: domainaudit.ResTypeUNS, ResName: "UNS import", BusinessType: domainaudit.BizImport,
		DetailJSON: string(raw), Code: code, IsShowInRecent: domainaudit.ShowInRecentNo,
	})
}
