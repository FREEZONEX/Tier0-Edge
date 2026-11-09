package sourcestream

// 覆盖可信宿主诊断、脚本捕获后的成功/另抛异常、历史回执、提交恢复与真实持久账本。
import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"backend/internal/domain/unsmeta"
	"backend/internal/infra/edgesql"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestProgramRunErrorProjection(t *testing.T) {
	legacy := runView(repo.StreamRun{Status: "failed", Summary: `["legacy"]`})
	require.Equal(t, []any{"legacy"}, legacy["summary"])
	legacyUnknown := runView(repo.StreamRun{Status: "result-unknown", Summary: `{"error":"SourceStream.resultUnknown"}`})
	require.Equal(t, "RESULT_UNKNOWN", legacyUnknown["error"].(*RunError).Code)
	legacyCommit := runView(repo.StreamRun{Status: "partial-failure", Summary: `{"phase":"uns-commit"}`})
	require.Equal(t, "SINK_COMMIT_FAILED", legacyCommit["error"].(*RunError).Code)

	for _, status := range []string{"succeeded", "processing", "accepted"} {
		view := runView(repo.StreamRun{Status: status, Summary: `{"errorCode":"SCRIPT_TIMEOUT","hostErrorCode":"SOURCE_READ_TIMEOUT"}`})
		require.NotContains(t, view, "error")
		require.NotContains(t, view, "diagnostics")
	}
	view := runView(repo.StreamRun{Status: "failed", Summary: `{"error":"SourceStream.executionFailed","errorCode":"private secret","hostErrorCode":"SOURCE_SQL_INVALID password"}`})
	require.Equal(t, "EXECUTION_FAILED", view["error"].(*RunError).Code)
	require.NotContains(t, view, "diagnostics")
	view = runView(repo.StreamRun{Status: "unknown", Summary: `{"errorCode":"SCRIPT_TIMEOUT","hostErrorCode":"SOURCE_READ_TIMEOUT"}`})
	require.Equal(t, "RESULT_UNKNOWN", view["error"].(*RunError).Code)
	observed := view["diagnostics"].(map[string]any)["hostError"].(*RunError)
	require.Equal(t, "SOURCE_READ_TIMEOUT", observed.Code)
	require.Empty(t, observed.SourceRequest)
	require.Contains(t, observed.Repair, "may have been caught")
}

func TestProgramHostErrorCaughtThenSuccessfulOrScriptFailure(t *testing.T) {
	for _, tail := range []string{"return 42;", `throw new Error("private script text SOURCE_SQL_AUTH_FAILED");`} {
		code := `module.exports=api=>{const s=api.sources.use("db");api.stream.onRequest({id:"run"},async()=>{try{await s.read("sql:orders");}catch(e){} ` + tail + `});}`
		request := streamexec.ProgramRequest{Code: code, TimeoutMs: 1000, CheckOnly: true, SourceAliases: map[string]streamexec.ProgramSource{"db": {ID: "11", Resources: []string{"sql:orders"}}}}
		registered, err := streamexec.ExecuteProgram(context.Background(), request, nil)
		require.NoError(t, err)
		request.CheckOnly = false
		request.EntryID = "run"
		request.Input = json.RawMessage(`{}`)
		request.Context = json.RawMessage(`{}`)
		request.ExpectedManifest = &registered.Manifest
		observed := &programHostDiagnostics{}
		_, err = streamexec.ExecuteProgram(context.Background(), request, observed.wrap(func(context.Context, streamexec.ProgramCall) (json.RawMessage, error) {
			return nil, edgesql.SafeError(context.Background(), &pgconn.PgError{Code: "42601", Message: "private query"})
		}))
		if tail == "return 42;" {
			require.NoError(t, err)
			require.NoError(t, observed.failure(err))
			continue
		}
		require.Error(t, err)
		failure := observed.failure(err)
		require.Equal(t, "EXECUTION_FAILED", executionFailureCode(failure))
		var host observedProgramFailure
		require.ErrorAs(t, failure, &host)
		require.Equal(t, "SOURCE_SQL_INVALID", host.hostCode)
	}
}

func TestProgramCommitErrorRecoveryClearsDiagnostic(t *testing.T) {
	summary := map[string]any{}
	recordCommitError(summary, &unsmeta.SchemaMismatchError{Path: "private-field", Reason: "private-value"})
	require.Equal(t, "COMPUTED_CONSTRAINT_FAILED", summary["errorCode"])
	recordCommitError(summary, errors.New("private storage address"))
	require.Equal(t, "SINK_COMMIT_FAILED", summary["errorCode"])
	recordCommitError(summary, nil)
	require.NotContains(t, summary, "errorCode")
	for _, tc := range []struct {
		err                 error
		code, stage, detail string
	}{
		{invalid("file", "sizeMismatch"), "SOURCE_FILE_INCOMPLETE", "source_snapshot", "complete readable snapshot"},
		{invalid("cursor", "invalidOrStale"), "SOURCE_CURSOR_INVALID", "source_checkpoint", "continuation"},
		{ErrDeviceSession, "SOURCE_DEVICE_SESSION_FAILED", "source_session", "device session"},
		{ErrPreviewNoSample, "SOURCE_PREVIEW_NO_SAMPLE", "source_preview", "without a device sample"},
		{context.DeadlineExceeded, "SOURCE_READ_TIMEOUT", "source_read", "deadline"},
		{invalid("file", "corruptSnapshot"), "SOURCE_SNAPSHOT_CORRUPT", "source_snapshot", "integrity check"},
		{invalid("file", "ordinaryTableRequired"), "SOURCE_TABLE_MISSING", "source_snapshot", "snapshot table"},
		{invalid("file", "ambiguousBusinessKey"), "SOURCE_KEY_INVALID", "source_snapshot", "unambiguous business key"},
		{invalid("file", "teamcenterTicketRejected"), "SOURCE_FILE_AUTH_REJECTED", "source_auth", "rejected authorization"},
		{invalid("file", "strongVersionValidatorRequired"), "SOURCE_FILE_VERSION_UNAVAILABLE", "source_version", "stable version validator"},
		{invalid("resourceRef", "teamcenterPartialErrors"), "SOURCE_RESPONSE_PARTIAL", "source_response", "partial response"},
		{invalid("job", "bulkJobFailed"), "SOURCE_JOB_FAILED", "source_job", "job failed"},
		{invalid("job", "creationOutcomeUnknown"), "SOURCE_JOB_OUTCOME_UNKNOWN", "source_job", "outcome could not be confirmed"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			diagnostic := SourceReadDiagnostic(tc.err)
			require.NotNil(t, diagnostic)
			require.Equal(t, tc.code, diagnostic.Code)
			require.Equal(t, tc.stage, diagnostic.Stage)
			require.Contains(t, diagnostic.Message, tc.detail)
			require.NotEmpty(t, diagnostic.Repair)
			require.Empty(t, diagnostic.SourceRequest)
			fleet := SourceReadDiagnosticCode(tc.code)
			require.NotNil(t, fleet)
			require.Equal(t, diagnostic.Stage, fleet.Stage)
			require.Equal(t, diagnostic.Message, fleet.Message)
			require.Equal(t, diagnostic.Repair, fleet.Repair)
			if tc.code == "SOURCE_JOB_OUTCOME_UNKNOWN" {
				require.Contains(t, diagnostic.Repair, "Inspect the saved job and upstream system")
				require.Contains(t, diagnostic.Repair, "Do not automatically resend the job")
			}
		})
	}
}

func TestProgramObservedHTTPResponseIsNotRunOutcome(t *testing.T) {
	observed := &programHostDiagnostics{}
	host := observed.wrap(func(context.Context, streamexec.ProgramCall) (json.RawMessage, error) {
		return json.RawMessage(`{"status":403,"body":"private upstream body","headers":{"token":"secret"}}`), nil
	})
	_, err := host(context.Background(), streamexec.ProgramCall{Kind: "source.request"})
	require.NoError(t, err)
	require.NoError(t, observed.failure(nil))
	var failure observedProgramFailure
	require.ErrorAs(t, observed.failure(streamexec.ErrExecution), &failure)
	require.Equal(t, "SOURCE_HTTP_403", failure.hostCode)
	encoded, err := json.Marshal(projectRunError(failure.hostCode))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
	require.NotContains(t, string(encoded), "private upstream")
}

func TestProgramFailureDiagnosticDurable(t *testing.T) {
	_, _ = testkit.NewTestDB(t)
	ctx := context.Background()
	r := repo.NewSourceStreamRepo(ctx)
	run := repo.StreamRun{ID: "safe-diagnostic", StreamID: 7001, VersionID: 7002, EntryID: "run", Model: "program", Status: "processing", Summary: `{}`}
	created, err := r.ReserveRun(ctx, run)
	require.NoError(t, err)
	require.True(t, created)
	s := &Service{}
	err = s.failProgramV2Run(ctx, run, observedProgramFailure{streamexec.ErrExecution, "SOURCE_SQL_PERMISSION_DENIED"})
	require.ErrorIs(t, err, ErrExecutionFailed)
	stored, err := r.Run(ctx, run.ID)
	require.NoError(t, err)
	view := runView(stored)
	require.Equal(t, "failed", view["status"])
	require.Equal(t, "EXECUTION_FAILED", view["error"].(*RunError).Code)
	require.Equal(t, "SOURCE_SQL_PERMISSION_DENIED", view["diagnostics"].(map[string]any)["hostError"].(*RunError).Code)
	listed, _, err := r.Runs(ctx, repo.StreamRunFilter{StreamID: run.StreamID, Page: 1, Size: 10})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, view, runView(listed[0]))
}
