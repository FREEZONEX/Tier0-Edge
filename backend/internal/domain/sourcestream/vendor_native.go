package sourcestream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
)

type vendorNativeInput struct {
	Params    map[string]string          `json:"params,omitempty"`
	Query     map[string]json.RawMessage `json:"query,omitempty"`
	Data      json.RawMessage            `json:"data,omitempty"`
	Headers   map[string]string          `json:"headers,omitempty"`
	Next      string                     `json:"next,omitempty"`
	TimeoutMs int                        `json:"timeoutMs,omitempty"`
}

// Only the code-owned native read contract may enable a rejected-session retry;
// user scripts cannot label an arbitrary POST as idempotent through options.
type vendorReadOnlyContext struct{}

func vendorNativeRequest(d SourceDefinition, call streamexec.ProgramCall) (HTTPRequestOptions, error) {
	parts := strings.SplitN(call.ResourceRef, ":", 4)
	if len(parts) < 3 || parts[0] != "vendor" || parts[1] != d.SourceType {
		return HTTPRequestOptions{}, streamexec.ErrSourceDenied
	}
	operation, ok := vendorOperation(d.SourceType, parts[2])
	if !ok || call.Kind != "source.read" && call.Kind != "source.invoke" || call.Kind == "source.read" && !operation.ReadOnly {
		return HTTPRequestOptions{}, streamexec.ErrSourceDenied
	}
	raw := call.Options
	if call.Kind == "source.invoke" {
		raw = call.Input
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	input, _, err := parseDocument[vendorNativeInput](raw)
	if err != nil {
		return HTTPRequestOptions{}, streamexec.ErrSourceDenied
	}
	if input.TimeoutMs != 0 && (input.TimeoutMs < 100 || input.TimeoutMs > streamexec.MaxProgramSourceTimeoutMs) {
		return HTTPRequestOptions{}, streamexec.ErrSourceDenied
	}
	if len(input.Params) > 32 || len(input.Query) > 100 || len(input.Headers) > 64 {
		return HTTPRequestOptions{}, streamexec.ErrSourceDenied
	}
	if input.Params == nil {
		input.Params = map[string]string{}
	}
	if d.SourceType == "salesforce" && input.Params["version"] == "" {
		input.Params["version"] = vendorParameter(d, "apiVersion")
		if input.Params["version"] == "" {
			input.Params["version"] = "v67.0"
		}
	}
	if len(parts) == 4 {
		switch d.SourceType {
		case "pi-web-api":
			input.Params["webId"] = parts[3]
		case "sap-odata":
			input.Params["entity"] = parts[3]
		case "salesforce":
			input.Params["object"] = parts[3]
		}
	}
	options := HTTPRequestOptions{TimeoutMs: input.TimeoutMs, Method: operation.Method, Path: operation.Path, JSON: input.Data, Headers: input.Headers, QueryValues: map[string][]string{}}
	for key, value := range input.Params {
		if len(key) > 128 || len(value) > 2048 || strings.ContainsAny(value, "\\\x00\r\n/?#") || value == "." || value == ".." {
			return HTTPRequestOptions{}, streamexec.ErrSourceDenied
		}
		options.Path = strings.ReplaceAll(options.Path, "{"+key+"}", url.PathEscape(value))
	}
	if strings.ContainsAny(options.Path, "{}") {
		return HTTPRequestOptions{}, streamexec.ErrSourceDenied
	}
	for key, raw := range input.Query {
		var single string
		var multiple []string
		if json.Unmarshal(raw, &single) == nil {
			options.QueryValues[key] = []string{single}
		} else if json.Unmarshal(raw, &multiple) == nil {
			options.QueryValues[key] = multiple
		} else {
			return HTTPRequestOptions{}, streamexec.ErrSourceDenied
		}
	}
	if input.Next != "" {
		if !operation.ReadOnly || operation.Method != http.MethodGet {
			return HTTPRequestOptions{}, streamexec.ErrSourceDenied
		}
		next, err := url.Parse(input.Next)
		base, _ := url.Parse(strings.TrimRight(d.Endpoint, "/") + "/")
		if err != nil || next.User != nil || next.Fragment != "" || len(input.Next) > 8192 {
			return HTTPRequestOptions{}, streamexec.ErrSourceDenied
		}
		if !next.IsAbs() {
			next = base.ResolveReference(next)
		}
		if next.Scheme != base.Scheme || next.Host != base.Host || !strings.HasPrefix(next.Path, base.Path) {
			return HTTPRequestOptions{}, streamexec.ErrSourceDenied
		}
		options.Path = strings.TrimPrefix(next.EscapedPath(), base.EscapedPath())
		options.QueryValues = next.Query()
		options.Query = nil
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return HTTPRequestOptions{}, err
	}
	return parseHTTPRequest(encoded)
}

// Native calls resolve a registered operation; scripts never assemble login,
// cookies or signing requests. The ordinary HTTP effect journal still controls
// writes; read-only POST contracts are explicitly safe for session recovery.
func (s *Service) vendorNativeCall(ctx context.Context, row repo.Source, d SourceDefinition, call streamexec.ProgramCall, runID, entryID string) (json.RawMessage, error) {
	options, err := vendorNativeRequest(d, call)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	httpCall := call
	httpCall.Kind = "source.request"
	httpCall.Options = encoded
	httpCall.Input = nil
	var raw json.RawMessage
	if call.Kind == "source.read" {
		raw, err = s.HTTPProgramCall(context.WithValue(ctx, vendorReadOnlyContext{}, true), row, d, httpCall)
	} else {
		raw, err = s.programV2HTTPCall(ctx, row, d, httpCall, runID, entryID)
	}
	if err != nil {
		return nil, err
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(raw, &result) != nil {
		return nil, ErrVendorResponse
	}
	var data any
	decoder := json.NewDecoder(strings.NewReader(string(result["data"])))
	decoder.UseNumber()
	if decoder.Decode(&data) != nil {
		return nil, ErrVendorResponse
	}
	if pagination := vendorPagination(data); pagination != nil {
		result["pagination"], err = json.Marshal(pagination)
		if err != nil {
			return nil, err
		}
	}
	if business := vendorBusinessError(d.SourceType, data); business != "" {
		result["error"], err = json.Marshal(map[string]string{"code": business, "message": "厂商接口返回业务错误；请检查 data 中的原生状态。"})
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(result)
}

func vendorPagination(value any) map[string]any {
	object := vendorObject(value)
	if object == nil {
		return nil
	}
	for _, key := range []string{"@odata.nextLink", "nextRecordsUrl"} {
		if next, ok := object[key].(string); ok && next != "" {
			return map[string]any{"next": next}
		}
	}
	if links := vendorObject(object["Links"]); links != nil {
		if next, ok := links["Next"].(string); ok && next != "" {
			return map[string]any{"next": next}
		}
	}
	return nil
}
func vendorBusinessError(sourceType string, value any) string {
	// ExecuteBillQuery wraps a single business rejection as [[{Result:...}]].
	// Unwrap only this vendor's exact envelope; ordinary rows and COUNT remain data.
	if sourceType == "kingdee-k3cloud" {
		if rows, ok := value.([]any); ok && len(rows) == 1 {
			if cells, ok := rows[0].([]any); ok && len(cells) == 1 {
				value = cells[0]
			}
		}
	}
	object := vendorObject(value)
	if object == nil {
		return ""
	}
	if object["error"] != nil {
		return "vendorError"
	}
	switch sourceType {
	case "yonyou-yonbip":
		if code := vendorString(object["code"]); code != "" && code != "200" && code != "0" && code != "00000" {
			return "vendorCode"
		}
	case "kingdee-jdy":
		if code := vendorString(object["errcode"]); code != "" && code != "0" {
			return "vendorCode"
		}
	case "ngp":
		if code := vendorString(object["code"]); code != "" && code != "0" && code != "200" {
			return "vendorCode"
		}
	case "kingdee-k3cloud":
		result := vendorObject(object["Result"])
		status := vendorObject(result["ResponseStatus"])
		if success, ok := status["IsSuccess"].(bool); ok && !success {
			if vendorString(status["MsgCode"]) == "1" {
				return "vendorSessionExpired"
			}
			return "vendorBusinessRejected"
		}
	case "teamcenter":
		qname := vendorString(object[".QName"])
		if strings.HasPrefix(qname, "http://teamcenter.com/Schemas/Soa/") && strings.Contains(qname, "/Exceptions.") {
			if strings.HasSuffix(qname, "/Exceptions.InvalidUserException") {
				return "vendorSessionExpired"
			}
			return "vendorException"
		}
		if object["exception"] != nil {
			return "vendorException"
		}
		if partial, ok := object["partialErrors"].([]any); ok && len(partial) > 0 {
			return "vendorPartialFailure"
		}
		service := vendorObject(object["ServiceData"])
		if partial, ok := service["partialErrors"].([]any); ok && len(partial) > 0 {
			return "vendorPartialFailure"
		}
	}
	return ""
}

func vendorProbeRequest(d SourceDefinition) (HTTPRequestOptions, error) {
	op := ""
	input := vendorNativeInput{Query: map[string]json.RawMessage{}}
	putQuery := func(key, value string) { encoded, _ := json.Marshal(value); input.Query[key] = encoded }
	switch d.SourceType {
	case "plant0-simctl":
		op = "board"
		putQuery("minutes", "30")
	case "pi-web-api":
		op = "servers"
		if path := vendorParameter(d, "pointPath"); path != "" {
			op = "point"
			putQuery("path", path)
			break
		}
		if name := vendorParameter(d, "serverName"); name != "" {
			putQuery("name", name)
		}
	case "plex-mes":
		// The jobs feed is not paginated and grows over time. Probe the smaller
		// workcenter catalog independently of the bounded full-feed read.
		op = "workcenters"
	case "wms-rest":
		op = "inventory"
		putQuery("limit", "1")
	case "sap-odata":
		op = "service"
	case "yonyou-yonbip":
		op = "materials"
		input.Data, _ = json.Marshal(map[string]any{"pageIndex": 1, "pageSize": 1, "orgId": vendorParameter(d, "orgId")})
	case "kingdee-jdy":
		op = "materials"
		putQuery("page", "1")
		putQuery("page_size", "1")
		putQuery("show_units", "true")
	case "ngp":
		op = "materials"
		input.Data = json.RawMessage(`{"pageIndex":1,"pageSize":1}`)
	case "kingdee-k3cloud":
		op = "query"
		input.Data = json.RawMessage(`{"data":{"FormId":"BD_MATERIAL","FieldKeys":"FMaterialId,FNumber,FName,FModifyDate,FForbidStatus","StartRow":0,"Limit":1}}`)
	case "salesforce":
		op = "objects"
	case "teamcenter":
		op = "queries"
		input.Data = json.RawMessage(`{"header":{},"body":{}}`)
	case "hikvision-isapi":
		op = "device"
	default:
		return HTTPRequestOptions{}, ErrVendorResponse
	}
	raw, _ := json.Marshal(input)
	return vendorNativeRequest(d, streamexec.ProgramCall{Kind: "source.read", ResourceRef: "vendor:" + d.SourceType + ":" + op, Options: raw})
}

func (s *Service) vendorMetadata(ctx context.Context, id int64, d SourceDefinition) (any, error) {
	row, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
	if err != nil {
		return nil, err
	}
	options, err := vendorProbeRequest(d)
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(options)
	raw, err := s.HTTPProgramCall(context.WithValue(ctx, vendorReadOnlyContext{}, true), row, d, streamexec.ProgramCall{Kind: "source.request", Options: encoded})
	if err != nil {
		return nil, err
	}
	var result HTTPResponse
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&result) != nil {
		return nil, ErrVendorResponse
	}
	if result.Status == http.StatusUnauthorized || result.Status == http.StatusForbidden {
		return nil, ErrCredential
	}
	if result.Status < 200 || result.Status >= 300 || vendorBusinessError(d.SourceType, result.Data) != "" {
		return nil, ErrVendorResponse
	}
	return result.Data, nil
}

func (s *Service) vendorDiscover(ctx context.Context, id int64, d SourceDefinition) ([]NativeResource, error) {
	metadata, err := s.vendorMetadata(ctx, id, d)
	if err != nil {
		return nil, err
	}
	resources := vendorResources(d.SourceType)
	for index := range resources {
		resources[index].Metadata["metadataVerifiedAt"] = time.Now().UnixMilli()
	}
	object := vendorObject(metadata)
	var entities []any
	var operation, identity, label string
	switch d.SourceType {
	case "sap-odata":
		entities, _ = object["value"].([]any)
		operation = "entity"
		identity = "name"
		label = "name"
	case "teamcenter":
		entities, _ = object["queries"].([]any)
		operation, identity, label = "query", "query", "name"
	case "salesforce":
		entities, _ = object["sobjects"].([]any)
		operation = "describe"
		identity = "name"
		label = "label"
	case "pi-web-api":
		if path := vendorParameter(d, "pointPath"); path != "" {
			id := vendorString(object["WebId"])
			if id == "" || len(id) > 256 || strings.ContainsAny(id, "/\\:?#%\x00\r\n") || vendorString(object["Path"]) != path {
				return nil, ErrVendorResponse
			}
			for _, op := range []string{"value", "recorded"} {
				resources = append(resources, NativeResource{ID: "vendor:pi-web-api:" + op + ":" + id, Name: vendorString(object["Name"]), Models: []AccessModel{RequestReply}, Metadata: map[string]any{"sourceType": d.SourceType, "readOnly": true, "pointPath": path, "nativeIdentity": id, "metadataVerifiedAt": time.Now().UnixMilli()}})
			}
			return resources, nil
		}
		entities, _ = object["Items"].([]any)
		operation = "points"
		identity = "WebId"
		label = "Name"
	}
	for _, entity := range entities {
		item := vendorObject(entity)
		key := vendorString(item[identity])
		if key == "" || len(key) > 2048 || strings.ContainsAny(key, "\x00\r\n:/\\") {
			continue
		}
		name := vendorString(item[label])
		if name == "" {
			name = key
		}
		resources = append(resources, NativeResource{ID: "vendor:" + d.SourceType + ":" + operation + ":" + key, Name: name, Models: []AccessModel{Publish, RequestReply}, Metadata: map[string]any{"sourceType": d.SourceType, "operation": operation, "nativeIdentity": key, "metadataVerifiedAt": time.Now().UnixMilli(), "readOnly": true}})
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	if len(resources) > 2000 {
		return nil, fmt.Errorf("%w: too many resources", ErrVendorResponse)
	}
	return resources, nil
}
