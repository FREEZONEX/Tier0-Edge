package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"backend/internal/domain/unsv2"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

type unsV2Context struct{}
type requestMetadata struct {
	ID, Operation      string
	FleetNodeID        string
	FleetTargetInvalid bool
	Raw                []byte
	PathParams         map[string]string
}

var contractOnce sync.Once
var operations map[string]string

func IsUnsV2(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/core/v2/uns/") || strings.HasPrefix(r.URL.Path, "/openapi/v2/uns/")
}
func PrepareUnsV2(r *http.Request) {
	if _, ok := r.Context().Value(unsV2Context{}).(requestMetadata); ok {
		return
	}
	contractOnce.Do(func() {
		var contract struct {
			Operations []struct {
				Name   string `json:"name"`
				Suffix string `json:"suffix"`
			} `json:"operations"`
		}
		_ = json.Unmarshal([]byte(unsv2.ContractJSON), &contract)
		operations = map[string]string{}
		for _, op := range contract.Operations {
			operations[op.Suffix] = op.Name
		}
	})
	suffix := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/api/core/v2"), "/openapi/v2")
	metadata := requestMetadata{ID: uuid.NewString(), Operation: operations[suffix]}
	if values := r.Header.Values("X-Fleet-Node-Id"); len(values) > 0 {
		metadata.FleetNodeID = values[0]
		metadata.FleetTargetInvalid = len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 128 || strings.ContainsRune("_.:-", rune(values[0][0])) || strings.ContainsFunc(values[0], func(c rune) bool {
			return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_.:-", c))
		})
	}
	if metadata.Operation == "" {
		for pattern, name := range operations {
			if params, ok := unsv2.MatchResourcePath(pattern, suffix); ok {
				metadata.Operation = name
				metadata.PathParams = params
				break
			}
		}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), unsV2Context{}, metadata))
}
func UnsV2RequestID(ctx context.Context) string {
	if metadata, ok := ctx.Value(unsV2Context{}).(requestMetadata); ok {
		return metadata.ID
	}
	return uuid.NewString()
}
func UnsV2Operation(ctx context.Context) (string, bool) {
	metadata, ok := ctx.Value(unsV2Context{}).(requestMetadata)
	return metadata.Operation, ok
}
func UnsV2RawRequest(ctx context.Context) []byte {
	metadata, _ := ctx.Value(unsV2Context{}).(requestMetadata)
	return metadata.Raw
}
func UnsV2FleetNode(ctx context.Context) string {
	metadata, _ := ctx.Value(unsV2Context{}).(requestMetadata)
	return metadata.FleetNodeID
}
func ParseUnsV2Request(r *http.Request, target any) error {
	PrepareUnsV2(r)
	if metadata, _ := r.Context().Value(unsV2Context{}).(requestMetadata); metadata.FleetTargetInvalid {
		return &unsv2.Fault{Code: "INVALID_REQUEST", Message: "invalid Fleet target"}
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return &unsv2.Fault{Code: "INVALID_JSON", Message: "application/json required"}
	}
	if r.Body == nil {
		return &unsv2.Fault{Code: "INVALID_JSON", Message: "JSON object required"}
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, unsv2.MaxBodyBytes+1))
	if err != nil {
		return err
	}
	metadata, _ := r.Context().Value(unsV2Context{}).(requestMetadata)
	raw, err = unsv2.BindResourcePath(raw, metadata.PathParams)
	if err != nil {
		return err
	}
	if _, resource := unsv2.FindResourceOperation(metadata.Operation); resource || unsv2.BatchScope(metadata.Operation) != "" {
		// These dispatchers validate their shared strict schema after adapting
		// legacy request shapes; never discard fields through generated DTOs.
		var object map[string]json.RawMessage
		if err := unsv2.Decode(raw, &object); err != nil {
			return err
		}
		if object == nil {
			return &unsv2.Fault{Code: "INVALID_REQUEST", Message: "JSON object required"}
		}
	} else if err := unsv2.Decode(raw, target); err != nil {
		return err
	}
	metadata.Raw = raw
	*r = *r.WithContext(context.WithValue(r.Context(), unsV2Context{}, metadata))
	return nil
}
func WriteUnsV2Error(w http.ResponseWriter, ctx context.Context, err error) {
	status := http.StatusInternalServerError
	code := "INTERNAL_ERROR"
	message := "UNS request failed"
	retryable := false
	details := []map[string]any{}
	var f *unsv2.Fault
	if errors.As(err, &f) {
		code = f.Code
		message = f.Message
		retryable = f.Retryable
		switch code {
		case "UNAUTHENTICATED":
			status = 401
		case "FORBIDDEN":
			status = 403
		case "NOT_FOUND":
			status = 404
		case "VERSION_CONFLICT", "IDEMPOTENCY_CONFLICT", "DUPLICATE", "REFERENCED", "ORIGIN_CONFLICT", "CONDITION_FAILED", "DUPLICATE_KEY", "LOCK_CONFLICT":
			status = 409
		case "REQUEST_TOO_LARGE", "RESPONSE_TOO_LARGE":
			status = 413
		case "TEMPORARILY_UNAVAILABLE", "APP_NOT_READY":
			status = 503
		case "QUERY_TIMEOUT":
			status = 504
		case "INTERNAL_ERROR":
			status = 500
		default:
			status = 422
		}
		if f.OpID != "" || f.Path != "" {
			details = append(details, map[string]any{"opId": f.OpID, "path": f.Path})
		}
	} else {
		logx.WithContext(ctx).Errorf("UNS v2 request failed: %v", err)
	}
	errorBody := map[string]any{"code": code, "retryable": retryable, "details": details}
	errorBody["retryAdvice"] = unsv2.AdviceForFault(f)
	if f != nil {
		if f.RolledBack {
			errorBody["rolledBack"] = true
		}
		if f.ConditionIndex != nil {
			errorBody["conditionIndex"] = *f.ConditionIndex
		}
		if f.OpID != "" {
			errorBody["opId"] = f.OpID
		}
		if f.Path != "" {
			errorBody["path"] = f.Path
		}
		if len(f.Candidates) > 0 {
			errorBody["candidates"] = f.Candidates
		}
		if f.Reason != "" {
			errorBody["reason"] = f.Reason
		}
		if f.Repair != "" {
			errorBody["repair"] = f.Repair
		}
		if f.ObjectKind != "" {
			errorBody["objectKind"] = f.ObjectKind
		}
		if f.ObjectID != "" {
			errorBody["objectId"] = f.ObjectID
		}
	}
	var data any
	if f != nil {
		data = f.Data
	}
	gozerohttpx.WriteJson(w, status, map[string]any{"code": status, "msg": message, "requestId": UnsV2RequestID(ctx), "data": data, "error": errorBody})
}
func WriteUnsV2AuthError(w http.ResponseWriter, r *http.Request, code string) bool {
	if IsCapabilityDiscovery(r) {
		status, publicCode, message := http.StatusServiceUnavailable, "DISCOVERY_UNAVAILABLE", "Capability publication is unavailable"
		switch code {
		case "UNAUTHENTICATED":
			status, publicCode, message = http.StatusUnauthorized, code, "An authenticated identity is required"
		case "FORBIDDEN":
			status, publicCode, message = http.StatusForbidden, code, "The current identity is not authorized"
		}
		return WriteCapabilityDiscoveryError(w, r, status, publicCode, message)
	}
	if !IsUnsV2(r) && !strings.HasPrefix(r.URL.Path, "/openapi/v2/") {
		return false
	}
	PrepareUnsV2(r)
	WriteUnsV2Error(w, r.Context(), &unsv2.Fault{Code: code, Message: strings.ToLower(code)})
	return true
}
