package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	domainasset "backend/internal/domain/asset"
	domainaudit "backend/internal/domain/audit"
	"backend/internal/domain/cluster"
	domainiam "backend/internal/domain/iam"
	domainnotify "backend/internal/domain/notify"
	domainuns "backend/internal/domain/uns"
	"backend/internal/infra/eventstream"
	"backend/internal/infra/outbox"
	"backend/internal/repo"

	"github.com/zeromicro/go-zero/core/logx"
)

type unsImportJobRequest struct {
	DryRun       bool  `json:"dryRun"`
	SourceFileID int64 `json:"sourceFileId"`
}

type unsExportJobRequest struct {
	RootNodeID int64 `json:"rootNodeId"`
}

type userImportJobRequest struct {
	SourceFileID int64 `json:"sourceFileId"`
}

type unsPhysicalEnsureRequest struct {
	Nodes        []repo.UnsNode `json:"nodes"`
	ImportJobKey string         `json:"importJobKey,omitempty"`
}

const unsPhysicalEnsureEventType = "uns.physical.ensure"

func newOutboxWorker(app *App) *outbox.Worker {
	worker := outbox.NewWorker(context.Background())
	if app == nil {
		return worker
	}
	worker.Register("uns.import.job", app.handleUnsImportJob)
	worker.Register("uns.export.job", app.handleUnsExportJob)
	worker.Register(domainiam.UserImportEventType, app.withUserJobTerminalFailure(domainiam.UserImportJobType, domainaudit.BizImport, app.handleUserImportJob))
	worker.Register(domainiam.UserExportEventType, app.withUserJobTerminalFailure(domainiam.UserExportJobType, domainaudit.BizExport, app.handleUserExportJob))
	// 消息通知（TASK-030）：OpenAPI 建站内信投递
	if app.Notify != nil {
		worker.Register(domainnotify.OutboxCreateInboxEvent, func(ctx context.Context, event repo.OutboxEvent) error {
			var payload domainnotify.CreateInboxPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return err
			}
			return app.Notify.HandleCreateInbox(ctx, payload)
		})
	}
	if app.Cluster != nil {
		worker.Register(cluster.FleetUserSyncRequestedOutboxEventType, app.Cluster.PublishFleetUserSyncRequestedOutbox)
	}
	return worker
}

// 审计使用独立消费者，避免大量操作日志阻塞导入、同步和通知。
func newAuditOutboxWorker(app *App) *outbox.Worker {
	worker := outbox.NewWorker(context.Background(), outbox.WithCompletedRetention(24*time.Hour))
	if app != nil && app.Audit != nil {
		worker.Register(domainaudit.OutboxEventType, app.Audit.Consume)
	}
	return worker
}

func (a *App) withUserJobTerminalFailure(expectedType, businessType string, handler outbox.EventHandler) outbox.EventHandler {
	return func(ctx context.Context, event repo.OutboxEvent) error {
		err := handler(ctx, event)
		if err == nil || event.Attempts < 4 {
			return err
		}
		jobs := repo.NewAsyncJobRepo(ctx)
		job, ok, loadErr := loadAsyncJob(ctx, jobs, event, expectedType)
		if loadErr != nil {
			return errors.Join(err, loadErr)
		}
		if !ok {
			return nil
		}
		return a.completeUserJob(ctx, jobs, job, repo.AsyncJobStatusFailed, businessType, err.Error(), map[string]any{
			"attempts": event.Attempts + 1,
		})
	}
}

func (a *App) handleUnsPhysicalEnsure(ctx context.Context, event repo.OutboxEvent) (runErr error) {
	if a == nil {
		return errors.New("physical ensure dependencies are not ready")
	}
	var req unsPhysicalEnsureRequest
	if err := json.Unmarshal(event.Payload, &req); err != nil {
		return fmt.Errorf("invalid physical ensure payload: %w", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			runErr = fmt.Errorf("physical ensure panic: %v", recovered)
		}
		if runErr != nil && req.ImportJobKey != "" && a.UNS != nil && event.Attempts >= 4 && ctx.Err() == nil {
			runErr = errors.Join(runErr, a.UNS.CompleteImportHistory(ctx, req.ImportJobKey, event.EventID, runErr))
		}
	}()
	if a.DataIngest == nil {
		return errors.New("physical ensure dependencies are not ready")
	}
	if req.ImportJobKey != "" {
		// A newer import may have updated these nodes before this DDL batch runs.
		// Never apply the old event snapshot over the latest configuration.
		ids := make([]int64, 0, len(req.Nodes))
		for _, node := range req.Nodes {
			ids = append(ids, node.ID)
		}
		nodes, err := repo.NewUnsRepo(ctx).GetUnsNodesByIDs(ctx, ids)
		if err != nil {
			return err
		}
		req.Nodes = nodes
	}
	if len(req.Nodes) == 0 {
		if req.ImportJobKey != "" && a.UNS != nil {
			return a.UNS.CompleteImportHistory(ctx, req.ImportJobKey, event.EventID, nil)
		}
		return nil
	}
	started := time.Now()
	metricCount := 0
	aliases := map[string]struct{}{}
	for _, node := range req.Nodes {
		if node.TopicType == 3 {
			metricCount++
		}
		alias := strings.Trim(strings.TrimSpace(node.Alias), "/")
		if alias == "" {
			alias = strings.Trim(strings.TrimSpace(node.Namespace), "/")
		}
		if alias != "" {
			aliases[alias] = struct{}{}
		}
	}
	if err := a.DataIngest.EnsurePhysicalForNodes(ctx, req.Nodes); err != nil {
		return fmt.Errorf("uns physical ensure batch failed nodes=%d metrics=%d aliases=%d duration=%s: %w", len(req.Nodes), metricCount, len(aliases), time.Since(started), err)
	}
	logx.WithContext(ctx).Infof("uns physical ensure batch completed nodes=%d metrics=%d aliases=%d duration=%s", len(req.Nodes), metricCount, len(aliases), time.Since(started))
	if req.ImportJobKey != "" {
		if a.UNS == nil {
			return errors.New("UNS import service is not ready")
		}
		return a.UNS.CompleteImportHistory(ctx, req.ImportJobKey, event.EventID, nil)
	}
	return nil
}

func (a *App) handleUnsImportJob(ctx context.Context, event repo.OutboxEvent) (runErr error) {
	jobs := repo.NewAsyncJobRepo(ctx)
	job, ok, err := loadAsyncJob(ctx, jobs, event, "uns.import")
	if err != nil || !ok {
		return err
	}
	// Preserve retry behavior for infrastructure failures, with a terminal job result
	// when outbox reaches its final attempt (including panics).
	defer func() {
		if recovered := recover(); recovered != nil {
			runErr = fmt.Errorf("import panic: %v", recovered)
		}
		if runErr != nil && event.Attempts >= 4 && ctx.Err() == nil {
			runErr = markAsyncJobFailed(ctx, jobs, job, runErr.Error(), nil)
		}
	}()
	var previous map[string]any
	if err := json.Unmarshal(job.ResultJSON, &previous); err != nil {
		return err
	}
	// The metadata transaction already enqueued every history batch.
	if previous["phase"] == "history" {
		return nil
	}
	var req unsImportJobRequest
	if err := json.Unmarshal(job.RequestJSON, &req); err != nil {
		return markAsyncJobFailed(ctx, jobs, job, "invalid import job request: "+err.Error(), nil)
	}
	if req.SourceFileID <= 0 {
		return markAsyncJobFailed(ctx, jobs, job, "sourceFileId is required", nil)
	}
	if a == nil || a.Asset == nil || a.UNS == nil {
		return errors.New("async import dependencies are not ready")
	}
	if req.DryRun {
		if err := jobs.MarkAsyncJobRunning(ctx, job.JobKey); err != nil {
			return err
		}
	}
	file, reader, _, err := a.Asset.Open(ctx, req.SourceFileID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return markAsyncJobFailed(ctx, jobs, job, err.Error(), map[string]any{"sourceFileId": req.SourceFileID})
		}
		return err
	}
	defer reader.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return markAsyncJobFailed(ctx, jobs, job, err.Error(), map[string]any{"sourceFileId": req.SourceFileID})
		}
		return err
	}
	lastProgress := -1
	progress := func(status domainuns.ImportStatus) {
		if !req.DryRun && status.Task == "Start Import" {
			return
		}
		value := min(int(status.Progress), 99)
		if !req.DryRun {
			value = min(int(status.Progress*0.7), 69)
		}
		if value >= 0 && value != lastProgress && !status.Finished {
			lastProgress = value
			if err := jobs.ActiveOnly().UpdateAsyncJobProgress(ctx, job.JobKey, value); err != nil {
				logx.WithContext(ctx).Errorf("async import progress update failed jobKey=%s err=%v", job.JobKey, err)
			}
		}
	}
	var result map[string]any
	if req.DryRun {
		result, err = a.UNS.ValidateImportData(ctx, file.OriginalName, raw, progress)
	} else {
		result, err = a.UNS.ImportJob(ctx, file.OriginalName, raw, job.CreatedBy, job.JobKey, progress)
	}
	if err != nil {
		if domainuns.IsImportValidationError(err) {
			return markAsyncJobFailed(ctx, jobs, job, err.Error(), result)
		}
		return err
	}
	if req.DryRun {
		return domainuns.CompleteImportJob(ctx, jobs, job.JobKey, repo.AsyncJobStatusSucceeded, "", result)
	}
	return nil
}

func (a *App) handleUnsExportJob(ctx context.Context, event repo.OutboxEvent) error {
	jobs := repo.NewAsyncJobRepo(ctx)
	job, ok, err := loadAsyncJob(ctx, jobs, event, "uns.export")
	if err != nil || !ok {
		return err
	}
	var req unsExportJobRequest
	if err := json.Unmarshal(job.RequestJSON, &req); err != nil {
		return markAsyncJobFailed(ctx, jobs, job, "invalid export job request: "+err.Error(), nil)
	}
	if a == nil || a.UNS == nil {
		return errors.New("async export dependencies are not ready")
	}
	if err := jobs.MarkAsyncJobRunning(ctx, job.JobKey); err != nil {
		return err
	}
	cmd := domainuns.ExportCommand{ExportType: "ALL"}
	if req.RootNodeID > 0 {
		cmd.ExportType = "PART"
		cmd.Folders = []int64{req.RootNodeID}
	}
	result, err := a.UNS.ExportData(ctx, cmd)
	if err != nil {
		return markAsyncJobFailed(ctx, jobs, job, err.Error(), nil)
	}
	return jobs.MarkAsyncJobSucceeded(ctx, job.JobKey, result)
}

func (a *App) handleUserImportJob(ctx context.Context, event repo.OutboxEvent) error {
	jobs := repo.NewAsyncJobRepo(ctx)
	job, ok, err := loadAsyncJob(ctx, jobs, event, domainiam.UserImportJobType)
	if err != nil || !ok {
		return err
	}
	if a == nil || a.IAM == nil || a.Asset == nil {
		return errors.New("user import dependencies are not ready")
	}
	var req userImportJobRequest
	if err := json.Unmarshal(job.RequestJSON, &req); err != nil || req.SourceFileID <= 0 {
		if err == nil {
			err = errors.New("sourceFileId is required")
		}
		return a.completeUserJob(ctx, jobs, job, repo.AsyncJobStatusFailed, domainaudit.BizImport, err.Error(), map[string]any{"sourceFileId": req.SourceFileID})
	}
	if err := jobs.MarkAsyncJobRunning(ctx, job.JobKey); err != nil {
		return err
	}
	file, reader, _, err := a.Asset.Open(ctx, req.SourceFileID)
	if err != nil {
		return a.completeUserJob(ctx, jobs, job, repo.AsyncJobStatusFailed, domainaudit.BizImport, err.Error(), map[string]any{"sourceFileId": req.SourceFileID})
	}
	defer reader.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	processed, err := a.IAM.ImportUsersWorkbook(ctx, file.OriginalName, raw, job.CreatedBy)
	if err != nil {
		return a.completeUserJob(ctx, jobs, job, repo.AsyncJobStatusFailed, domainaudit.BizImport, err.Error(), map[string]any{"sourceFileId": req.SourceFileID})
	}
	result := processed.Result
	result["sourceFileId"] = req.SourceFileID
	if len(processed.ErrorWorkbook) > 0 {
		fileID, err := a.uploadUserJobWorkbook(ctx, job, "user-import-errors.xlsx", processed.ErrorWorkbook)
		if err != nil {
			return err
		}
		result["errorFileId"] = fileID
	}
	return a.completeUserJob(ctx, jobs, job, repo.AsyncJobStatusSucceeded, domainaudit.BizImport, "", result)
}

func (a *App) handleUserExportJob(ctx context.Context, event repo.OutboxEvent) error {
	jobs := repo.NewAsyncJobRepo(ctx)
	job, ok, err := loadAsyncJob(ctx, jobs, event, domainiam.UserExportJobType)
	if err != nil || !ok {
		return err
	}
	if a == nil || a.IAM == nil || a.Asset == nil {
		return errors.New("user export dependencies are not ready")
	}
	if err := jobs.MarkAsyncJobRunning(ctx, job.JobKey); err != nil {
		return err
	}
	var request domainiam.UserExportRequest
	if len(job.RequestJSON) > 0 {
		if err := json.Unmarshal(job.RequestJSON, &request); err != nil {
			return a.completeUserJob(ctx, jobs, job, repo.AsyncJobStatusFailed, domainaudit.BizExport, err.Error(), nil)
		}
	}
	raw, result, err := a.IAM.ExportUsersWorkbook(ctx, request)
	if err != nil {
		return a.completeUserJob(ctx, jobs, job, repo.AsyncJobStatusFailed, domainaudit.BizExport, err.Error(), nil)
	}
	result["scope"] = request.Scope
	result["sensitiveMode"] = request.SensitiveMode
	fileName, _ := result["fileName"].(string)
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		fileName = "users.xlsx"
	}
	fileID, err := a.uploadUserJobWorkbook(ctx, job, fileName, raw)
	if err != nil {
		return err
	}
	result["resultFileId"] = fileID
	return a.completeUserJob(ctx, jobs, job, repo.AsyncJobStatusSucceeded, domainaudit.BizExport, "", result)
}

func (a *App) uploadUserJobWorkbook(ctx context.Context, job repo.AsyncJob, fileName string, raw []byte) (int64, error) {
	created, err := a.Asset.UploadContent(ctx, domainasset.UploadContentCommand{FileName: fileName, ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Size: int64(len(raw)), Reader: bytes.NewReader(raw), UserID: job.CreatedBy})
	if err != nil {
		return 0, err
	}
	fileID := anyJobInt64(created["fileId"])
	if fileID <= 0 {
		return 0, errors.New("uploaded workbook missing fileId")
	}
	if _, err := a.Asset.Bind(ctx, domainasset.BindCommand{FileID: fileID, OwnerType: "asyncJob", OwnerID: job.ID, UserID: job.CreatedBy}); err != nil {
		return 0, err
	}
	return fileID, nil
}

func (a *App) completeUserJob(ctx context.Context, jobs *repo.AsyncJobRepo, job repo.AsyncJob, status, businessType, message string, result map[string]any) error {
	if result == nil {
		result = map[string]any{}
	}
	if message != "" {
		result["error"] = message
	}
	result["jobKey"] = job.JobKey
	result["status"] = status
	detail, err := json.Marshal(result)
	if err != nil {
		return err
	}
	operatorName := ""
	if user, userErr := repo.NewIAMRepo(ctx).GetUserByID(ctx, job.CreatedBy); userErr == nil {
		operatorName = user.UserName
	}
	code := int64(200)
	if status == repo.AsyncJobStatusFailed {
		code = 500
	}
	if err := jobs.CompleteAsyncJobWithAudit(ctx, job.JobKey, status, message, result, repo.AuditLog{
		UserID: job.CreatedBy, ResType: domainaudit.ResTypeUserManagement, ResName: "User import/export",
		BusinessType: businessType, DetailJSON: string(detail), Code: code, IsShowInRecent: domainaudit.ShowInRecentNo,
		OperatorName: operatorName,
	}); err != nil {
		return err
	}
	eventstream.Publish(eventstream.Event{
		Type: "iam.user.job",
		Keys: []string{"job:" + job.JobKey},
		Data: map[string]any{
			"jobKey": job.JobKey, "jobType": job.JobType, "status": status, "progress": 100,
			"resultJson": result, "errorMessage": message, "finishedTime": time.Now().UTC().UnixMilli(),
		},
	})
	return nil
}

func anyJobInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	default:
		return 0
	}
}

func loadAsyncJob(ctx context.Context, jobs *repo.AsyncJobRepo, event repo.OutboxEvent, expectedType string) (repo.AsyncJob, bool, error) {
	if strings.TrimSpace(event.AggregateType) != "asyncJob" {
		return repo.AsyncJob{}, false, fmt.Errorf("invalid aggregate type %q for async job event", event.AggregateType)
	}
	jobKey := strings.TrimSpace(event.AggregateID)
	if jobKey == "" && len(event.Payload) > 0 {
		var payload repo.AsyncJob
		if err := json.Unmarshal(event.Payload, &payload); err == nil {
			jobKey = strings.TrimSpace(payload.JobKey)
		}
	}
	if jobKey == "" {
		return repo.AsyncJob{}, false, errors.New("async job event missing aggregate id")
	}
	job, err := jobs.GetAsyncJob(ctx, jobKey)
	if err != nil {
		return repo.AsyncJob{}, false, err
	}
	if expectedType != "" && job.JobType != expectedType {
		return repo.AsyncJob{}, false, fmt.Errorf("unexpected job type %q, want %q", job.JobType, expectedType)
	}
	if isTerminalAsyncJobStatus(job.Status) {
		return job, false, nil
	}
	return job, true, nil
}

func isTerminalAsyncJobStatus(status string) bool {
	switch status {
	case repo.AsyncJobStatusSucceeded, repo.AsyncJobStatusFailed, repo.AsyncJobStatusCanceled:
		return true
	default:
		return false
	}
}

func markAsyncJobFailed(ctx context.Context, jobs *repo.AsyncJobRepo, job repo.AsyncJob, message string, result map[string]any) error {
	if result == nil {
		result = map[string]any{}
	}
	if strings.TrimSpace(message) != "" {
		result["error"] = message
	}
	if job.JobType == "uns.import" {
		return domainuns.CompleteImportJob(ctx, jobs, job.JobKey, repo.AsyncJobStatusFailed, message, result)
	}
	return jobs.MarkAsyncJobFailed(ctx, job.JobKey, message, result)
}

// DDL must not hold up new import jobs, notifications, or metadata sync.
func newUnsPhysicalOutboxWorker(app *App) *outbox.Worker {
	worker := outbox.NewWorker(context.Background(), outbox.WithBatchSize(1))
	if app != nil {
		worker.Register(unsPhysicalEnsureEventType, app.handleUnsPhysicalEnsure)
	}
	return worker
}
