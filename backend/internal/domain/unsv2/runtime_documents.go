package unsv2

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/sourcestream"
	"backend/internal/mcpaccess"
	"backend/internal/repo"
	"gorm.io/gorm"
)

func nativeDocumentOperation(name string) bool {
	switch name {
	case "streamRunGet", "sourceNativeCredentialsUpdate", "sourceNativeOAuthStart", "sourceNativeOAuthComplete", "sourceDocumentGet", "sourceDocumentList", "sourceDocumentCreate", "sourceDocumentUpdate", "sourceDocumentDelete", "sourceDocumentStateUpdate", "sourceImpact", "sourceObservationsGet", "streamDocumentGet", "streamDocumentCreate", "streamDocumentUpdate", "streamDocumentValidate", "streamDocumentDisable", "streamProgramList", "streamProgramDelete", "streamObservationGet", "streamRunsList":
		return true
	}
	return false
}

func fullRuntimeDocument(ctx context.Context, kind string) error {
	permission := "uns." + kind + ".manage"
	if err := require(ctx, permission); err != nil {
		return err
	}
	subject, _ := contextx.SubjectFrom(ctx)
	if subject.AuthType == "apiKey" && subject.APIKeyPermission == "read_only" || subject.MCPRestricted && !mcpaccess.Has(subject, permission) {
		return problem("FORBIDDEN", "Full document requires current management consent and a full-access API key")
	}
	return nil
}
func runtimeSummary(view map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"id", "name", "description", "protocol", "sourceId", "model", "revision", "configRevision", "desiredState", "runtimeState", "health", "healthMessage", "connectionCheck", "dataCheck", "lastDataAt", "checkedAt", "activeVersion", "activeRelease", "pendingVersion", "status", "executionState", "deploymentState", "createdAt", "updatedAt"} {
		if value, ok := view[key]; ok {
			out[key] = value
		}
	}
	return out
}
func nativeID(fields map[string]json.RawMessage, key string) (int64, error) {
	id, err := strconv.ParseInt(resourceString(fields, key), 10, 64)
	if err != nil || id < 1 {
		return 0, invalid(key, "positive decimal string required")
	}
	return id, nil
}
func nativeRevision(fields map[string]json.RawMessage) int64 {
	var n int64
	_ = json.Unmarshal(fields["expectedRevision"], &n)
	return n
}

var previewRuntimeRollback = errors.New("rollback runtime configuration preview")

func (s *Service) runtimeDocument(ctx context.Context, op ResourceOperation, fields map[string]json.RawMessage) (any, error) {
	if s.sourceRuntime == nil {
		return nil, problem("UNAVAILABLE", "Source runtime unavailable")
	}
	r := s.sourceRuntime
	var dryRun bool
	_ = json.Unmarshal(fields["dryRun"], &dryRun)
	if op.Mutation != "" && op.Name != "sourceNativeOAuthStart" && op.Name != "sourceNativeOAuthComplete" {
		deferred, refresh := r.DeferSourceRefresh(ctx)
		var result any
		err := repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
			nested := repo.WithUnsTransaction(deferred, tx)
			var e error
			result, e = s.executeRuntimeDocument(nested, op, fields)
			if e != nil {
				return e
			}
			if dryRun {
				return previewRuntimeRollback
			}
			return nil
		})
		if errors.Is(err, previewRuntimeRollback) {
			return map[string]any{"validated": true, "sideEffects": false}, nil
		}
		if err == nil {
			refresh()
		}
		return result, err
	}
	return s.executeRuntimeDocument(ctx, op, fields)
}

func (s *Service) executeRuntimeDocument(ctx context.Context, op ResourceOperation, fields map[string]json.RawMessage) (any, error) {
	r := s.sourceRuntime
	if len(fields["items"]) > 0 && (op.Name == "sourceDocumentCreate" || op.Name == "sourceDocumentUpdate") {
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(fields["items"], &items); err != nil {
			return nil, err
		}
		results := make([]any, 0, len(items))
		for _, item := range items {
			value, err := s.executeRuntimeDocument(ctx, op, item)
			if err != nil {
				return nil, err
			}
			results = append(results, value)
		}
		return map[string]any{"items": results}, nil
	}
	var id int64
	var err error
	if len(fields["id"]) > 0 {
		id, err = nativeID(fields, "id")
		if err != nil {
			return nil, err
		}
	}
	revision := nativeRevision(fields)
	// 省略确认令牌与显式提供空值不同；无依赖也必须校验调用方提交的令牌。
	if raw, supplied := fields["confirmedImpactRevision"]; supplied {
		var token string
		if json.Unmarshal(raw, &token) != nil || strings.TrimSpace(token) == "" {
			return nil, invalid("confirmedImpactRevision", "nonempty impact revision required")
		}
	}
	var pagination Pagination
	raw, _ := json.Marshal(fields)
	_ = json.Unmarshal(raw, &pagination)
	page, size, err := bounds(pagination, 200)
	if err != nil {
		return nil, err
	}
	var document any
	_ = json.Unmarshal(fields["document"], &document)
	if document == nil {
		_ = json.Unmarshal(fields["program"], &document)
	}
	var sourceRevision int64
	_ = json.Unmarshal(fields["expectedSourceRevision"], &sourceRevision)
	switch op.Name {
	case "sourceDocumentGet", "streamDocumentGet":
		full := resourceString(fields, "view") != "summary"
		if full {
			if err := fullRuntimeDocument(ctx, op.Resource); err != nil {
				return nil, err
			}
		}
		var value map[string]any
		if op.Resource == "source" {
			value, err = r.Source(ctx, id)
		} else {
			value, err = r.Stream(ctx, id)
		}
		if err != nil {
			return nil, err
		}
		if !full {
			value = runtimeSummary(value)
		}
		return value, nil
	case "sourceDocumentList", "streamProgramList":
		var full bool
		_ = json.Unmarshal(fields["includeDocument"], &full)
		if full {
			if err := fullRuntimeDocument(ctx, op.Resource); err != nil {
				return nil, err
			}
		}
		var value map[string]any
		if op.Resource == "source" {
			value, err = r.Sources(ctx, repo.SourceFilter{Page: page, Size: size, Search: resourceString(fields, "search"), Protocol: resourceString(fields, "protocol"), Health: resourceString(fields, "health"), DesiredState: resourceString(fields, "desiredState")})
		} else {
			var sourceID int64
			if len(fields["sourceId"]) > 0 {
				sourceID, err = nativeID(fields, "sourceId")
				if err != nil {
					return nil, err
				}
			}
			value, err = r.Streams(ctx, repo.StreamFilter{Page: page, Size: size, Search: resourceString(fields, "search"), SourceID: sourceID, Model: resourceString(fields, "model"), Status: resourceString(fields, "status")})
		}
		if err != nil {
			return nil, err
		}
		if !full {
			rows, _ := value["list"].([]map[string]any)
			for i, row := range rows {
				rows[i] = runtimeSummary(row)
			}
			value["list"] = rows
		}
		return value, nil
	case "sourceNativeCredentialsUpdate", "sourceNativeOAuthStart", "sourceNativeOAuthComplete":
		if err := require(ctx, "uns.source.manage", "uns.source.credentials"); err != nil {
			return nil, err
		}
		if op.Name == "sourceNativeOAuthStart" {
			return r.OAuthStart(ctx, id, resourceString(fields, "redirectUri"))
		}
		if op.Name == "sourceNativeOAuthComplete" {
			return r.OAuthComplete(ctx, id, resourceString(fields, "state"), resourceString(fields, "code"))
		}
		var credential sourcestream.Credential
		if err := decodeRuntimeCredential(fields["credentials"], &credential); err != nil {
			return nil, err
		}
		if credential.Kind == "oauth2" {
			credential.OAuth = &sourcestream.OAuthCredential{}
		}
		ctx, err = r.BindSourceImpact(ctx, id, revision, "credentials", nil)
		if err != nil {
			return nil, err
		}
		return r.SaveCredential(ctx, id, revision, credential, resourceString(fields, "confirmedImpactRevision"))
	case "sourceImpact":
		current, e := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
		if e != nil {
			return nil, e
		}
		if current.Revision != revision {
			return nil, repo.ErrConflict
		}
		if len(fields["patch"]) > 0 {
			document, err = sourceImpactCandidate(current.Document, fields["patch"])
			if err != nil {
				return nil, err
			}
		}
		var offset, size int
		_ = json.Unmarshal(fields["offset"], &offset)
		_ = json.Unmarshal(fields["size"], &size)
		return r.InspectSourceImpact(ctx, id, revision, resourceString(fields, "action"), document, offset, size, resourceString(fields, "impactRevision"))
	case "sourceDocumentCreate":
		if len(fields["credentials"]) == 0 {
			return r.CreateSource(ctx, document, 0)
		}
		if err := require(ctx, "uns.source.credentials"); err != nil {
			return nil, err
		}
		var credential sourcestream.Credential
		if err := decodeRuntimeCredential(fields["credentials"], &credential); err != nil {
			return nil, err
		}
		if credential.Kind == "oauth2" {
			credential.OAuth = &sourcestream.OAuthCredential{}
		}
		value, err := r.CreateSource(ctx, document, 0)
		if err != nil {
			return nil, err
		}
		return r.SaveCredential(ctx, int64Number(value["id"]), int64Number(value["revision"]), credential, "")
	case "sourceDocumentUpdate":
		if len(fields["patch"]) > 0 {
			current, e := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
			if e != nil {
				return nil, e
			}
			if current.Revision != revision {
				return nil, repo.ErrConflict
			}
			document, err = sourceImpactCandidate(current.Document, fields["patch"])
			if err != nil {
				return nil, err
			}
		}
		ctx, err = r.BindSourceImpact(ctx, id, revision, "update", document)
		if err != nil {
			return nil, err
		}
		return r.UpdateSource(ctx, id, revision, document, resourceString(fields, "confirmedImpactRevision"))
	case "sourceDocumentStateUpdate":
		return r.SetSourceState(ctx, id, revision, resourceString(fields, "desiredState"), resourceString(fields, "confirmedImpactRevision"))
	case "sourceDocumentDelete":
		ctx, err = r.BindSourceImpact(ctx, id, revision, "delete", nil)
		if err != nil {
			return nil, err
		}
		return r.DeleteSource(ctx, id, revision, resourceString(fields, "confirmedImpactRevision"))
	case "sourceObservationsGet":
		var items []map[string]json.RawMessage
		_ = json.Unmarshal(fields["items"], &items)
		rows := make([]any, 0, len(items))
		for _, item := range items {
			sourceID, e := nativeID(item, "id")
			if e != nil {
				return nil, e
			}
			source, e := r.Source(ctx, sourceID)
			if e != nil {
				return nil, e
			}
			if int64Number(source["revision"]) != nativeRevision(item) {
				return nil, repo.ErrConflict
			}
			rows = append(rows, runtimeSummary(source))
		}
		return map[string]any{"items": rows}, nil
	case "streamDocumentCreate", "streamDocumentUpdate":
		return r.SaveStream(ctx, id, revision, document)
	case "streamDocumentValidate":
		return r.ValidationView(ctx, document, sourceRevision)
	case "streamDocumentDisable", "streamProgramDelete":
		if err := require(ctx, "uns.stream.deploy"); err != nil {
			return nil, err
		}
		if op.Name == "streamDocumentDisable" {
			expected, e := strconv.ParseInt(resourceString(fields, "expectedActiveVersion"), 10, 64)
			if e != nil || expected < 0 {
				return nil, invalid("expectedActiveVersion", "nonnegative decimal string required")
			}
			current, e := repo.NewSourceStreamRepo(ctx).Stream(ctx, id)
			if e != nil {
				return nil, e
			}
			if current.ActiveVersion != expected || current.Revision != revision {
				return nil, repo.ErrConflict
			}
		}
		return r.Disable(ctx, id, revision, op.Name == "streamProgramDelete")
	case "streamObservationGet":
		value, e := r.Stream(ctx, id)
		if e != nil {
			return nil, e
		}
		return runtimeSummary(value), nil
	case "streamRunGet":
		return r.Run(ctx, resourceString(fields, "runId"))
	case "streamRunsList":
		var sourceID, topicID int64
		if len(fields["sourceId"]) > 0 {
			sourceID, err = nativeID(fields, "sourceId")
			if err != nil {
				return nil, err
			}
		}
		if len(fields["topicId"]) > 0 {
			topicID, err = nativeID(fields, "topicId")
			if err != nil {
				return nil, err
			}
		}
		return r.Runs(ctx, repo.StreamRunFilter{StreamID: id, SourceID: sourceID, TopicID: topicID, Page: page, Size: size, Status: resourceString(fields, "status"), Model: resourceString(fields, "model")})
	}
	return nil, invalid("operation", "unsupported native document operation: "+strings.TrimSpace(op.Name))
}
