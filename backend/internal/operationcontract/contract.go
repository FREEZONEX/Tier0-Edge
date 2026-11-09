// Package operationcontract defines the control contract shared by Stream
// registration and App invocation. It contains no runtime binding.
package operationcontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"backend/internal/domain/common"
	"backend/internal/unsref"
	"github.com/google/jsonschema-go/jsonschema"
)

const (
	MaxSchemaBytes      = 16 * 1024
	MaxSchemaDepth      = 8
	MaxSchemaNodes      = 1024
	MaxTimeoutMs        = 5000
	MaxRequestTimeoutMs = 30000
	MaxValueBytes       = 1024 * 1024
)

var requestIdentity = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)

func ValidRequestCode(code string) bool { return len(code) <= 128 && requestIdentity.MatchString(code) }

func IsStreamRequestRoute(method, path string) bool {
	if method != "POST" {
		return false
	}
	for _, prefix := range []string{"/openapi/v2/stream-requests/", "/api/core/v2/stream-requests/"} {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(path, prefix)
		if suffix == "discover" {
			return true
		}
		parts := strings.Split(suffix, "/")
		return len(parts) == 2 && ValidRequestCode(parts[0]) && (parts[1] == "describe" || parts[1] == "invoke")
	}
	return false
}

var (
	ErrContract = errors.New("invalid operation contract")
	ErrInput    = errors.New("operation input does not match contract")
	ErrOutput   = errors.New("operation output does not match contract")
)

// 正式 App 仅能进入这三个公开端点，不能借此访问其他 V1 或管理接口。
func IsAppRoute(method, path string) bool {
	if method != "POST" {
		return false
	}
	suffix, ok := strings.CutPrefix(path, "/openapi/v1/controls/")
	if !ok {
		return false
	}
	if suffix == "discover" {
		return true
	}
	code, action, ok := strings.Cut(suffix, "/")
	return ok && unsref.ValidMetadataID(code) && (action == "describe" || action == "invoke")
}

// Resource is declared by the script. Deployment assigns the private binding;
// scripts cannot choose another Stream, version or arbitrary execution target.
type Resource struct {
	Code            string          `json:"code"`
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	ContractVersion string          `json:"contractVersion"`
	InputSchema     json.RawMessage `json:"inputSchema"`
	OutputSchema    json.RawMessage `json:"outputSchema"`
	SideEffect      string          `json:"sideEffect"`
	Idempotency     string          `json:"idempotency"`
	TimeoutMs       int             `json:"timeoutMs"`
}

func (r Resource) Validate() error {
	if !unsref.ValidMetadataID(r.Code) || strings.TrimSpace(r.Name) != r.Name || r.Name == "" ||
		common.ExceedNameLimit(r.Name) || common.ExceedDescLimit(r.Description) ||
		!unsref.ValidMetadataID(r.ContractVersion) ||
		(r.SideEffect != "none" && r.SideEffect != "write") || r.Idempotency != "required" ||
		r.TimeoutMs < 100 || r.TimeoutMs > MaxTimeoutMs {
		return ErrContract
	}
	if _, err := resolve(r.InputSchema); err != nil {
		return err
	}
	_, err := resolve(r.OutputSchema)
	return err
}

func (r Resource) ValidateInput(value json.RawMessage) error {
	return validateValue(r.InputSchema, value, ErrInput)
}

// ValidateRequest 使用 Cloud 请求合同；旧 control 的边界仍由 Validate 保留。
func (r Resource) ValidateRequest() error {
	if !ValidRequestCode(r.Code) || strings.TrimSpace(r.Name) != r.Name || r.Name == "" ||
		common.ExceedNameLimit(r.Name) || common.ExceedDescLimit(r.Description) || r.ContractVersion == "" || len(r.ContractVersion) > 128 ||
		(r.SideEffect != "read" && r.SideEffect != "write") ||
		(r.Idempotency != "none" && r.Idempotency != "optional" && r.Idempotency != "required") ||
		(r.SideEffect == "write" && r.Idempotency != "required") || r.TimeoutMs < 1 || r.TimeoutMs > MaxRequestTimeoutMs {
		return ErrContract
	}
	if _, err := resolve(r.InputSchema); err != nil {
		return err
	}
	_, err := resolve(r.OutputSchema)
	return err
}

// ValidateStored 只供经过清单校验后冻结的绑定与历史回执读取。
func (r Resource) ValidateStored() error {
	if r.Validate() == nil {
		return nil
	}
	return r.ValidateRequest()
}

func (r Resource) ValidateOutput(value json.RawMessage) error {
	return validateValue(r.OutputSchema, value, ErrOutput)
}

// Authorization constraints use the same bounded Schema dialect as Controls.
// Both the published input schema and this additional restriction must pass.
func ValidateParameterSchema(schema json.RawMessage) error {
	_, err := resolve(schema)
	return err
}
func ValidateParameters(schema, value json.RawMessage) error {
	return validateValue(schema, value, ErrInput)
}

func validateValue(schema, value json.RawMessage, mismatch error) error {
	resolved, err := resolve(schema)
	if err != nil {
		return err
	}
	if len(value) == 0 || len(value) > MaxValueBytes {
		return mismatch
	}
	var input any
	if json.Unmarshal(value, &input) != nil || resolved.Validate(input) != nil {
		return mismatch
	}
	return nil
}

// A small explicit Schema subset prevents remote references and unsupported
// keywords from silently becoming an unenforced public promise.
func resolve(raw json.RawMessage) (*jsonschema.Resolved, error) {
	if len(raw) == 0 || len(raw) > MaxSchemaBytes {
		return nil, ErrContract
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || root == nil {
		return nil, ErrContract
	}
	nodes := 0
	if !validSchema(root, 0, &nodes) {
		return nil, ErrContract
	}
	var schema jsonschema.Schema
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(&schema) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrContract
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, ErrContract
	}
	return resolved, nil
}

func validSchema(schema map[string]json.RawMessage, depth int, nodes *int) bool {
	*nodes++
	if depth > MaxSchemaDepth || *nodes > MaxSchemaNodes {
		return false
	}
	// 仅空的根 Schema 表示任意 JSON；嵌套节点继续要求显式类型。
	if depth == 0 && len(schema) == 0 {
		return true
	}
	var kind string
	if json.Unmarshal(schema["type"], &kind) != nil {
		return false
	}
	switch kind {
	case "object", "array", "string", "number", "integer", "boolean", "null":
	default:
		return false
	}
	for key, value := range schema {
		switch key {
		case "type":
		case "title", "description":
			var text string
			if json.Unmarshal(value, &text) != nil || len(text) > 2048 {
				return false
			}
		case "properties":
			var properties map[string]map[string]json.RawMessage
			if kind != "object" || json.Unmarshal(value, &properties) != nil || properties == nil || len(properties) > 128 {
				return false
			}
			for name, property := range properties {
				if name == "" || len(name) > 128 || name == "__proto__" || name == "constructor" || name == "prototype" || !validSchema(property, depth+1, nodes) {
					return false
				}
			}
		case "items":
			var item map[string]json.RawMessage
			if kind != "array" || json.Unmarshal(value, &item) != nil || !validSchema(item, depth+1, nodes) {
				return false
			}
		case "required":
			var fields []string
			var properties map[string]json.RawMessage
			_ = json.Unmarshal(schema["properties"], &properties)
			if kind != "object" || json.Unmarshal(value, &fields) != nil || fields == nil || len(fields) > 128 {
				return false
			}
			seen := map[string]bool{}
			for _, field := range fields {
				if _, ok := properties[field]; !ok || seen[field] {
					return false
				}
				seen[field] = true
			}
		case "additionalProperties":
			if kind != "object" || string(value) != "true" && string(value) != "false" {
				return false
			}
		case "minimum", "maximum":
			var number float64
			if kind != "number" && kind != "integer" || string(value) == "null" || json.Unmarshal(value, &number) != nil {
				return false
			}
		case "enum":
			var values []any
			if json.Unmarshal(value, &values) != nil || len(values) == 0 || len(values) > 100 {
				return false
			}
		default:
			return false
		}
	}
	_, items := schema["items"]
	return kind != "array" || items
}
