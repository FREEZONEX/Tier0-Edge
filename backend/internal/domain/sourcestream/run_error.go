package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"backend/internal/domain/unsmeta"
	"backend/internal/infra/edgesql"
	"backend/internal/repo"
	"backend/internal/streamexec"
)

// RunError 与 Cloud 共用公开形状，只从服务端已知类别重建固定文案。
type RunError struct {
	Code          string `json:"code"`
	Stage         string `json:"stage"`
	Message       string `json:"message"`
	Repair        string `json:"repair"`
	SourceRequest string `json:"sourceRequest,omitempty"`
	Reason        string `json:"reason,omitempty"`
	Path          string `json:"path,omitempty"`
}

func projectRunError(code string) *RunError {
	stage, message, repair := "", "", ""
	switch code {
	case "HOST_CALL_FAILED":
		stage, message, repair = "host_call", "A host operation failed during this run.", "Inspect the declared operation and its service availability; do not repeat prior external effects automatically."
	case "EXECUTION_FAILED":
		stage, message, repair = "execution", "The Stream execution failed.", "Inspect the script and any observed host error. A caught host error may not be the final cause; inspect prior effects before starting another run."
	case "RESULT_UNKNOWN":
		stage, message, repair = "unknown", "The run outcome could not be confirmed.", "Inspect the upstream system and saved receipts before starting another run; earlier effects may have completed."
	case "SCRIPT_TIMEOUT":
		stage, message, repair = "execution", "The Stream execution exceeded its deadline.", "Inspect host calls and script work within the configured budget. Do not replay prior external effects automatically."
	case "SCRIPT_INVALID":
		stage, message, repair = "validate_script", "The Stream script or execution input is invalid.", "Validate the script and entry input against the frozen contract before redeploying."
	case "OUTPUT_INVALID_output":
		stage, message, repair = "validate_output", "The Stream output does not match its contract.", "Read the Collection schema and Stream write contract, correct the output, then redeploy."
	case "COMPUTED_CONSTRAINT_FAILED":
		stage, message, repair = "uns_commit", "The stored output failed a data, binding or checkpoint constraint.", "Inspect the Collection schema, binding revision and checkpoint. Preserve the run and its output receipts; do not repeat Source calls."
	case "SINK_COMMIT_FAILED":
		stage, message, repair = "uns_commit", "The output commit could not be confirmed.", "Inspect storage availability and the saved output receipts. Pending output recovery must not repeat Source calls."
	case "SOURCE_SQL_FAILED":
		stage, message, repair = "source_query", "The SQL Source operation failed.", "Inspect the connection, configured query and database diagnostics before deciding whether to retry. No retry safety is implied."
	case "SOURCE_SQL_INVALID":
		stage, message, repair = "source_query", "The configured SQL statement is invalid.", "Correct the named query for the database dialect and redeploy; repeated calls cannot repair the statement."
	case "SOURCE_SQL_OBJECT_NOT_FOUND":
		stage, message, repair = "source_query", "A database object required by the SQL query was not found.", "Check the configured database, schema, table and column names before retrying."
	case "SOURCE_SQL_PERMISSION_DENIED":
		stage, message, repair = "source_auth", "The database denied the SQL operation.", "Check the authorized database account permissions without automatically expanding access."
	case "SOURCE_SQL_AUTH_FAILED":
		stage, message, repair = "source_auth", "The database rejected authentication.", "Check the Source credential binding and authorized database account before retrying."
	case "SOURCE_READ_TIMEOUT":
		stage, message, repair = "source_read", "A Source read exceeded its deadline.", "Inspect the supported read window, connection and resource progress before another bounded attempt."
	case "SOURCE_DNS_FAILED":
		stage, message, repair = "source_dns", "The Source hostname could not be resolved.", "Check the configured hostname and DNS resolution from the runtime."
	case "SOURCE_TLS_FAILED":
		stage, message, repair = "source_tls", "The Source TLS certificate, hostname or trust verification failed.", "Check the configured hostname, certificate validity and trusted CA without disabling verification."
	case "SOURCE_CONNECTION_REFUSED", "SOURCE_DEVICE_CONNECT_FAILED":
		stage, message, repair = "source_connection", "The Source connection could not be established.", "Check the configured endpoint, listener and secure connection from this Edge."
	case "SOURCE_DEVICE_SESSION_FAILED":
		stage, message, repair = "source_session", "The device session could not be established.", "Check authentication, certificate trust and the configured security policy."
	case "SOURCE_CREDENTIAL_UNAVAILABLE":
		stage, message, repair = "source_auth", "The Source credential is unavailable.", "Check the credential binding on the selected Edge."
	case "SOURCE_NODE_READ_FAILED":
		stage, message, repair = "source_read", "The OPC UA node read failed.", "Check the declared node and device session diagnostics."
	case "SOURCE_PREVIEW_NO_SAMPLE":
		stage, message, repair = "source_preview", "The preview ended without a device sample.", "Inspect the subscription and wait for a real event; do not synthesize a sample."
	case "SOURCE_PREVIEW_WATCH_FAILED":
		stage, message, repair = "source_subscription", "The Source watch failed during preview.", "Inspect the subscription state and protocol diagnostics."
	case "SOURCE_RESPONSE_INVALID":
		stage, message, repair = "source_response", "The Source response could not be used.", "Check the selected resource, response shape and requested limits."
	case "SOURCE_CURSOR_INVALID":
		stage, message, repair = "source_checkpoint", "The continuation no longer matches this resource.", "Inspect the Source revision and restart a bounded scan before advancing its checkpoint."
	case "SOURCE_FILE_CHANGED":
		stage, message, repair = "source_version", "The Source file changed during transfer.", "Read a new consistent snapshot before replacing the saved data."
	case "SOURCE_SNAPSHOT_CORRUPT":
		stage, message, repair = "source_snapshot", "The source snapshot failed its integrity check.", "Verify the published snapshot before accepting records; do not use partial data."
	case "SOURCE_TABLE_MISSING":
		stage, message, repair = "source_snapshot", "The configured snapshot table is unavailable.", "Check the declared table against the published snapshot schema."
	case "SOURCE_KEY_INVALID":
		stage, message, repair = "source_snapshot", "The snapshot does not provide an unambiguous business key.", "Check the declared key and source records before advancing the checkpoint."
	case "SOURCE_FILE_INCOMPLETE":
		stage, message, repair = "source_snapshot", "The Source file is not a complete readable snapshot.", "Wait for a consistent snapshot; do not accept partial data."
	case "SOURCE_RESOURCE_LIMIT":
		stage, message, repair = "source_budget", "The resource exceeded its bounded read limit.", "Reduce the approved selection or page size without skipping uncommitted records."
	case "SOURCE_FILE_AUTH_REJECTED":
		stage, message, repair = "source_auth", "The source file service rejected authorization.", "Check the authorized credential or download ticket before requesting a new snapshot."
	case "SOURCE_FILE_VERSION_UNAVAILABLE":
		stage, message, repair = "source_version", "The source file has no supported stable version validator.", "Use a resource with a supported strong version validator before resuming the transfer."
	case "SOURCE_RESPONSE_PARTIAL":
		stage, message, repair = "source_response", "The Source reported a partial response.", "Inspect the declared selection and provider result before accepting it as complete."
	case "SOURCE_JOB_FAILED":
		stage, message, repair = "source_job", "The Source resource job failed.", "Inspect the saved job state and requested resource before starting another job."
	case "SOURCE_JOB_OUTCOME_UNKNOWN":
		stage, message, repair = "source_job", "The Source resource job outcome could not be confirmed.", "Inspect the saved job and upstream system before starting another job; do not assume the first operation was not dispatched. Do not automatically resend the job."
	case "SOURCE_CALL_FORBIDDEN":
		stage, message, repair = "validate_binding", "The Source call was not permitted by its contract.", "Check declared resources, supported options and the caller permissions. Do not expand authorization automatically."
	case "EDGE_SOURCE_REFERENCE_REJECTED":
		stage, message, repair = "validate_binding", "The Source reference is inactive or changed.", "Refresh the Source configuration and redeploy the Stream. Earlier calls may have completed."
	default:
		if len(code) > len("SOURCE_HTTP_") && code[:len("SOURCE_HTTP_")] == "SOURCE_HTTP_" {
			n, err := strconv.Atoi(code[len("SOURCE_HTTP_"):])
			if err == nil && n >= 300 && n < 600 && code == "SOURCE_HTTP_"+strconv.Itoa(n) {
				stage, message, repair = "source_response", "A Source request returned HTTP "+strconv.Itoa(n)+".", "Check the authorized upstream response handling and rate limits before retrying. Earlier effects may have completed."
			}
		}
	}
	if stage == "" {
		return nil
	}
	return &RunError{Code: code, Stage: stage, Message: message, Repair: repair}
}

// SourceReadDiagnosticCode 供认证后的接口出口按已知类别重建诊断，不信任外部文案。
func SourceReadDiagnosticCode(code string) *RunError {
	diagnostic := projectRunError(code)
	if diagnostic != nil && strings.HasPrefix(diagnostic.Stage, "source_") {
		return diagnostic
	}
	return nil
}

// SourceReadDiagnostic 仅接受真实宿主错误的类型，不解析脚本、驱动或响应正文。
func SourceReadDiagnostic(err error) *RunError {
	if err == nil {
		return nil
	}
	code := edgesql.DiagnosticCode(err)
	var validation *ValidationError
	validationMapped := false
	if errors.As(err, &validation) {
		code = ""
		switch validation.Field + "." + validation.Code {
		case "cursor.invalidOrStale":
			code = "SOURCE_CURSOR_INVALID"
		case "file.fileVersionChanged", "cursor.fileVersionChanged", "cursor.directoryChanged":
			code = "SOURCE_FILE_CHANGED"
		case "file.corruptSnapshot":
			code = "SOURCE_SNAPSHOT_CORRUPT"
		case "file.ordinaryTableRequired":
			code = "SOURCE_TABLE_MISSING"
		case "file.ambiguousBusinessKey":
			code = "SOURCE_KEY_INVALID"
		case "file.writeInProgress", "file.sizeMismatch", "file.standaloneSQLiteRequired":
			code = "SOURCE_FILE_INCOMPLETE"
		case "file.tooLarge", "directory.tooManyEntries", "limit.upstreamNativePageExceedsByteLimit":
			code = "SOURCE_RESOURCE_LIMIT"
		case "file.teamcenterTicketRejected":
			code = "SOURCE_FILE_AUTH_REJECTED"
		case "resourceRef.teamcenterPartialErrors":
			code = "SOURCE_RESPONSE_PARTIAL"
		case "file.strongVersionValidatorRequired":
			code = "SOURCE_FILE_VERSION_UNAVAILABLE"
		case "job.bulkJobFailed":
			code = "SOURCE_JOB_FAILED"
		case "job.creationOutcomeUnknown":
			code = "SOURCE_JOB_OUTCOME_UNKNOWN"
		case "source.upstreamRateLimited":
			code = "SOURCE_HTTP_429"
		}
		validationMapped = code != ""
	}
	var network net.Error
	var httpError EdgeHTTPResponseError
	if code == "" {
		switch {
		case errors.Is(err, ErrPreviewNoSample):
			code = "SOURCE_PREVIEW_NO_SAMPLE"
		case errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
			code = "SOURCE_READ_TIMEOUT"
		case errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, ErrEdgeConnectionRefused):
			code = "SOURCE_CONNECTION_REFUSED"
		case errors.Is(err, ErrDeviceConnect):
			code = "SOURCE_DEVICE_CONNECT_FAILED"
		case errors.Is(err, ErrDeviceSession):
			code = "SOURCE_DEVICE_SESSION_FAILED"
		case errors.Is(err, ErrPreviewNodeRead):
			code = "SOURCE_NODE_READ_FAILED"
		case errors.Is(err, ErrPreviewWatch):
			code = "SOURCE_PREVIEW_WATCH_FAILED"
		case errors.Is(err, ErrCredential), errors.Is(err, ErrCredentialKey), errors.Is(err, ErrOAuthRequired):
			code = "SOURCE_CREDENTIAL_UNAVAILABLE"
		case errors.As(err, &httpError):
			code = httpError.Error()
		case errors.Is(err, ErrVendorResponse):
			code = "SOURCE_RESPONSE_INVALID"
		}
	}
	diagnostic := projectRunError(code)
	if diagnostic != nil && validationMapped {
		diagnostic.Path, diagnostic.Reason = validation.Field, validation.Code
	}
	return diagnostic
}

type programHostDiagnostics struct {
	mu   sync.Mutex
	code string
}

// 宿主错误只表示运行中观察到的失败；脚本可以捕获它，不能据此替换最终执行结果。
func (d *programHostDiagnostics) wrap(host streamexec.ProgramHost) streamexec.ProgramHost {
	return func(ctx context.Context, call streamexec.ProgramCall) (json.RawMessage, error) {
		data, err := host(ctx, call)
		if err == nil && call.Kind == "source.request" {
			var response struct {
				Status int `json:"status"`
			}
			if json.Unmarshal(data, &response) == nil {
				if diagnostic := projectRunError("SOURCE_HTTP_" + strconv.Itoa(response.Status)); diagnostic != nil {
					d.mu.Lock()
					if d.code == "" {
						d.code = diagnostic.Code
					}
					d.mu.Unlock()
				}
			}
		}
		if err != nil {
			diagnostic := projectRunError("HOST_CALL_FAILED")
			if len(call.Kind) >= 7 && call.Kind[:7] == "source." {
				switch {
				case errors.Is(err, ErrEdgeReferenceRejected), errors.Is(err, errInactivePlan), errors.Is(err, repo.ErrConflict):
					diagnostic = projectRunError("EDGE_SOURCE_REFERENCE_REJECTED")
				case errors.Is(err, ErrForbidden), errors.Is(err, streamexec.ErrSourceDenied):
					diagnostic = projectRunError("SOURCE_CALL_FORBIDDEN")
				default:
					if source := SourceReadDiagnostic(err); source != nil {
						diagnostic = source
					}
				}
			}
			if diagnostic != nil {
				d.mu.Lock()
				if d.code == "" {
					d.code = diagnostic.Code
				}
				d.mu.Unlock()
			}
		}
		return data, err
	}
}

type observedProgramFailure struct {
	error
	hostCode string
}

func (e observedProgramFailure) Unwrap() error { return e.error }
func (d *programHostDiagnostics) failure(err error) error {
	if err == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return observedProgramFailure{err, d.code}
}

func executionFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrUnknownResult):
		return "RESULT_UNKNOWN"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, streamexec.ErrTimeout):
		return "SCRIPT_TIMEOUT"
	case errors.Is(err, streamexec.ErrSyntax), errors.Is(err, streamexec.ErrInput):
		return "SCRIPT_INVALID"
	case errors.Is(err, streamexec.ErrOutput):
		return "OUTPUT_INVALID_output"
	default:
		return "EXECUTION_FAILED"
	}
}

func recordCommitError(summary map[string]any, err error) {
	delete(summary, "errorCode")
	if err == nil {
		return
	}
	code := "SINK_COMMIT_FAILED"
	if errors.Is(err, unsmeta.ErrSchema) || errors.Is(err, unsmeta.ErrData) || errors.Is(err, unsmeta.ErrInvalid) || errors.Is(err, repo.ErrInvalidArgument) || errors.Is(err, repo.ErrConflict) {
		code = "COMPUTED_CONSTRAINT_FAILED"
	}
	summary["errorCode"] = code
}

func publicRunError(status string, summary map[string]any) *RunError {
	if status != "failed" && status != "unknown" && status != "result-unknown" && status != "partial-failure" {
		return nil
	}
	if status == "unknown" || status == "result-unknown" {
		return projectRunError("RESULT_UNKNOWN")
	}
	code, _ := summary["errorCode"].(string)
	if diagnostic := projectRunError(code); diagnostic != nil {
		return diagnostic
	}
	if status == "partial-failure" && summary["phase"] == "uns-commit" {
		return projectRunError("SINK_COMMIT_FAILED")
	}
	return projectRunError("EXECUTION_FAILED")
}

// SQL 驱动正文可能含参数和连接信息，进入领域前只保留固定类别及取消信号。
func safeSQLReadError(ctx context.Context, err error) error { return edgesql.SafeError(ctx, err) }
