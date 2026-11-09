package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
	"backend/internal/mcpaccess"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mutationInput struct {
	AssertLocks    []unsv2.RecordLockAssertion `json:"assertLocks,omitempty"`
	IdempotencyKey string                      `json:"idempotencyKey"`
	Mode           string                      `json:"mode,omitempty"`
	Atomic         *bool                       `json:"atomic,omitempty"`
	DryRun         *bool                       `json:"dryRun"`
	Items          []unsv2.Change              `json:"items"`
}
type jobsInput struct {
	Queries      []unsv2.Query `json:"queries"`
	ResultOffset int           `json:"resultOffset,omitempty"`
	ResultSize   int           `json:"resultSize,omitempty"`
}

func operationTool(name, description string, destructive bool) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive, IdempotentHint: true, OpenWorldHint: new(bool)}}
}

// OpenAPI nullable 不是 JSON Schema 关键字；转换后根目录的 parentId=null 才能通过 MCP。
func jsonSchemaNullable(value any) {
	switch v := value.(type) {
	case map[string]any:
		for _, part := range v {
			jsonSchemaNullable(part)
		}
		if v["nullable"] == true {
			delete(v, "nullable")
			original := map[string]any{}
			for key, part := range v {
				original[key] = part
				delete(v, key)
			}
			v["anyOf"] = []any{original, map[string]any{"type": "null"}}
		}
	case []any:
		for _, part := range v {
			jsonSchemaNullable(part)
		}
	}
}
func (s *Service) addMutationTools(server *mcp.Server, subject contextx.Subject) {
	if !mcpaccess.CanWrite(subject) {
		return
	}
	s.addScopedBatchTools(server, subject)
	if _, exists := findOperation(catalog(), "jobs"); !exists {
		return
	}
	server.AddTool(jobsQueryTool("uns_jobs_query", "Query your own shared UNS batch jobs. Current permissions and consented operation scopes are checked for every job operation; other users' jobs are not exposed. resultOffset/resultSize page operation results (default 20, max 200); summaries clearly identify partial results. Never returns saved actor credentials or request bodies."), func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input jobsInput
		if err := unsv2.Decode(request.Params.Arguments, &input); err != nil {
			return result(nil, err)
		}
		if input.ResultOffset < 0 || input.ResultSize < 0 || input.ResultSize > 200 {
			return result(nil, &unsv2.Fault{Code: "INVALID_REQUEST", Message: "resultOffset must be non-negative; resultSize 1..200"})
		}
		value, err := s.uns.Query(contextx.WithSubject(ctx, subject), "jobs", unsv2.Queries{Queries: input.Queries})
		if err != nil {
			return result(nil, safeMutationError(err))
		}
		raw, _ := json.Marshal(value)
		var out any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&out); err != nil {
			return result(nil, safeMutationError(err))
		}
		pageJobResults(out, input.ResultOffset, input.ResultSize)
		return result(sanitize(out), nil)
	})
}
func (s *Service) mutateScoped(ctx context.Context, input mutationInput, operation string) (any, error) {
	ctx = dataQueryReadContext(ctx)
	if input.DryRun == nil {
		return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Message: "Explicit dryRun true/false is required; preview with true first"}
	}
	subject, _ := contextx.SubjectFrom(ctx)
	for i := range input.Items {
		item := &input.Items[i]
		if err := unsv2.ValidateChange(*item); err != nil {
			if f, ok := err.(*unsv2.Fault); ok {
				copy := *f
				copy.OpID = item.OpID
				copy.ObjectKind = item.Kind
				copy.ObjectID = item.ID.ID
				copy.Path = fmt.Sprintf("/items/%d/%s", i, strings.TrimPrefix(f.Path, "/"))
				return nil, &copy
			}
			return nil, err
		}
		if bytes.Contains(item.Data, []byte(`"[REDACTED]"`)) {
			return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Path: "data", Reason: "A redacted query projection is not a replacement document", Repair: "Use metadata patches with intended writable fields; a supplied expectedRevision must match the current revision"}
		}
		for _, operation := range item.Operations {
			if bytes.Contains(operation.Value, []byte(`"[REDACTED]"`)) {
				return nil, &unsv2.Fault{Code: "INVALID_REQUEST", OpID: item.OpID, Path: fmt.Sprintf("/items/%d/operations", i), Message: "Redacted projections cannot be used as field replacement values"}
			}
		}
		for _, binding := range item.Bindings {
			path := binding.ResultPath
			if strings.HasPrefix(path, "query.records.") {
				// The query envelope is not part of the public data field path's
				// depth budget. ValidateChange already checked its row index.
				parts := strings.SplitN(path, ".", 4)
				if len(parts) == 4 {
					path = parts[3]
				}
			}
			segments, err := unsmeta.ParseFieldPath(path)
			if err != nil {
				return nil, &unsv2.Fault{Code: "INVALID_REQUEST", OpID: item.OpID, Path: fmt.Sprintf("/items/%d/bindings", i), Message: "Invalid result field path"}
			}
			for _, segment := range segments {
				if sensitive(segment) {
					return nil, &unsv2.Fault{Code: "FORBIDDEN", OpID: item.OpID, Path: fmt.Sprintf("/items/%d/bindings", i), Message: "MCP result bindings cannot copy credential fields into another field"}
				}
			}
		}
		if (item.Action == "record.modify" || item.Action == "record.update") && !mcpaccess.Has(subject, "uns.read") {
			return nil, &unsv2.Fault{Code: "FORBIDDEN", OpID: item.OpID, Message: "Record field modifications require both read and write consent"}
		}
		if !mcpaccess.AllowedMutation(subject, item.Action, item.Kind, item.ManagedBy != nil) {
			return nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "The required operation scope was not consented"}
		}
		if item.Kind == "source" && credentialMaterial(item.Data) {
			return nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "MCP cannot manage credential values; use the dedicated existing credential channel"}
		}
	}
	batch := unsv2.Batch{AssertLocks: input.AssertLocks, IdempotencyKey: input.IdempotencyKey, Mode: input.Mode, Atomic: input.Atomic, DryRun: *input.DryRun, Items: input.Items}
	value, err := s.uns.ScopedBatch(ctx, operation, batch)
	if err != nil {
		return nil, err
	}
	// Transaction reads and their saved receipts obey the same credential-field
	// projection as ordinary MCP queries. Preserve JSON numbers while projecting.
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return sanitize(out), nil
}
func credentialMaterial(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	var contains func(any) bool
	contains = func(value any) bool {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if key != "credentialRef" && sensitive(key) {
					return true
				}
				if contains(item) {
					return true
				}
			}
		case []any:
			for _, item := range v {
				if contains(item) {
					return true
				}
			}
		}
		return false
	}
	return contains(value)
}
func safeMutationError(err error) error {
	if err == nil {
		return nil
	}
	if f, ok := err.(*unsv2.Fault); ok {
		copy := *f
		copy.Message = "The authorized batch could not be completed"
		if f.Reason != "" {
			copy.Message = f.Reason
		}
		if copy.Repair == "" {
			copy.Repair = unsv2.RepairHint(copy.Code)
		}
		return &copy
	}
	return &unsv2.Fault{Code: "QUERY_FAILED", Message: "The authorized batch could not be completed"}
}
func pageJobResults(value any, offset, size int) {
	if size == 0 {
		size = 20
	}
	switch v := value.(type) {
	case map[string]any:
		if results, ok := v["results"].([]any); ok && v["replayed"] != nil {
			start := offset
			if start > len(results) {
				start = len(results)
			}
			end := start + size
			if end > len(results) {
				end = len(results)
			}
			v["results"] = results[start:end]
			v["totalResults"] = len(results)
			v["resultOffset"] = start
			v["resultSize"] = size
			v["hasMoreResults"] = end < len(results)
		}
		for _, part := range v {
			pageJobResults(part, offset, size)
		}
	case []any:
		for _, part := range v {
			pageJobResults(part, offset, size)
		}
	}
}
