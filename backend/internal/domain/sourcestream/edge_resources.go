package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/streamexec"
)

const plexJobsRef = "vendor:plex-mes:jobs"

type edgeResourceReadContext struct{}

var ErrEdgeConnectionRefused = errors.New("upstream Source connection refused")

// This fence is checked before contacting the upstream device.
var ErrEdgeReferenceRejected = fmt.Errorf("edge source reference rejected: %w", repo.ErrConflict)

// EdgeHTTPResponseError carries only an observed upstream response status.
type EdgeHTTPResponseError int

func (e EdgeHTTPResponseError) Error() string { return "SOURCE_HTTP_" + strconv.Itoa(int(e)) }

// These contracts describe the resources this particular native configuration
// can serve. They deliberately carry no endpoint, vendor parameters or secrets.
type edgeResource struct {
	ResourceRef         string         `json:"resourceRef"`
	Name                string         `json:"name"`
	Modes               []string       `json:"modes"`
	Permission          string         `json:"permission"`
	RequiredPermissions []string       `json:"requiredPermissions"`
	ConfigRevision      int64          `json:"configRevision"`
	InputSchema         map[string]any `json:"inputSchema"`
	OutputSchema        map[string]any `json:"outputSchema"`
	Metadata            map[string]any `json:"metadata,omitempty"`
}

func resourceObject(properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
}

func declaredResources(def SourceDefinition, row repo.Source) []edgeResource {
	revision := row.ConfigRevision
	resources := []edgeResource{}
	if def.ConnectionRole() != ClientRole {
		return resources
	}
	if def.SourceType == "plex-mes" && def.Protocol == HTTP {
		operation, ok := vendorOperation(def.SourceType, "jobs")
		if ok && operation.ReadOnly {
			resources = append(resources, edgeResource{ResourceRef: plexJobsRef, Name: operation.Name, Modes: []string{"read"},
				Permission: "uns.source", ConfigRevision: revision,
				InputSchema: resourceObject(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 10},
					"createdDateTimeBegin": map[string]string{"type": "string", "format": "date-time"}, "createdDateTimeEnd": map[string]string{"type": "string", "format": "date-time"}}),
				OutputSchema: resourceObject(map[string]any{"items": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, "returned": map[string]string{"type": "integer"}, "truncated": map[string]string{"type": "boolean"}}),
				Metadata:     map[string]any{"identityField": "id", "pagination": "none", "timeWindow": "inclusive", "readLimitScope": "preview"}})
		}
	}
	if def.Protocol == ModbusTCP && def.Modbus != nil {
		for _, item := range def.Modbus.Ranges {
			unitID := def.Modbus.UnitID
			if item.UnitID != nil {
				unitID = *item.UnitID
			}
			dataType := "integer"
			if item.Table == "coil" || item.Table == "discrete-input" {
				dataType = "boolean"
			}
			resources = append(resources, edgeResource{ResourceRef: "modbus:" + item.ID, Name: item.ID, Modes: []string{"read", "receive"},
				Permission: "uns.source", ConfigRevision: revision, InputSchema: resourceObject(map[string]any{}),
				OutputSchema: resourceObject(map[string]any{"values": map[string]any{"type": "array", "items": map[string]string{"type": dataType}, "minItems": item.Quantity, "maxItems": item.Quantity},
					"unitId": map[string]string{"type": "integer"}, "table": map[string]string{"type": "string"}, "address": map[string]string{"type": "integer"}}),
				Metadata: map[string]any{"unitId": unitID, "table": item.Table, "address": item.Address, "quantity": item.Quantity}})
		}
	}
	if def.SourceType == "sap-odata" && def.Protocol == HTTP {
		for _, entity := range sapDiscoveredEntities(row) {
			resources = append(resources, edgeResource{ResourceRef: entity.ID, Name: entity.Name, Modes: []string{"read", "scan"}, Permission: "uns.source", ConfigRevision: revision,
				InputSchema:  resourceObject(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, "maxItems": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000, "description": "Optional scan result budget; a positive value is sent as $top on the initial request. Zero means no total-item limit."}}),
				OutputSchema: resourceObject(map[string]any{"items": map[string]any{"type": "array", "items": map[string]string{"type": "object"}}, "nextCursor": map[string]string{"type": "string"}, "hasMore": map[string]string{"type": "boolean"}}),
				Metadata:     map[string]any{"pagination": "nativeNextLink", "cursorScope": "sourceRevisionEntityCallerAndLimits"}})
		}
	}
	resources = append(resources, fileResources(def, row)...)
	resources = append(resources, openProtocolResources(def, revision)...)
	resources = append(resources, teamcenterFileResources(def, row)...)
	resources = append(resources, salesforceDirectory(def, row)...)
	resources = append(resources, edgeSQLResources(def, revision)...)
	resources = append(resources, piDeclaredResources(def, revision)...)
	resources = append(resources, declaredDeviceResources(def, revision)...)
	resources = append(resources, boundedReadResources(def, row)...)
	resources = plexSAPDescriptors(resources, def, revision)
	resources = append(resources, businessDirectory(def, row)...)
	for i := range resources {
		resources[i].RequiredPermissions = []string{"uns.source", "uns.read"}
		properties, _ := resources[i].InputSchema["properties"].(map[string]any)
		if budget, ok := properties["maxItems"].(map[string]any); ok {
			budget["minimum"] = 0
			budget["description"] = "Zero omits the total result budget; each page remains bounded by limit."
		}
	}
	return resources
}

func edgeResourceSource(ctx context.Context, id, revision int64) (repo.Source, SourceDefinition, error) {
	var def SourceDefinition
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(subject, "uns.source") || !contextx.HasResource(subject, "uns.read") {
		return repo.Source{}, def, ErrForbidden
	}
	current, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
	if err != nil {
		return current, def, err
	}
	if current.DesiredState == "disabled" || revision > 0 && current.ConfigRevision != revision {
		return current, def, ErrEdgeReferenceRejected
	}
	if err = json.Unmarshal([]byte(current.Document), &def); err != nil {
		return current, def, err
	}
	return current, def, nil
}

func (s *Service) EdgeResourceCatalog(ctx context.Context, id, revision int64) (any, error) {
	row, def, err := edgeResourceSource(ctx, id, revision)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": strconv.FormatInt(id, 10), "configRevision": row.ConfigRevision, "protocol": def.Protocol, "sourceType": def.SourceType, "resources": declaredResources(def, row)}, nil
}

type EdgeResourceReadParams struct {
	ItemID string `json:"itemId,omitempty"`
	EdgePlexSAPFilters
	Limit                *int   `json:"limit,omitempty"`
	CreatedDateTimeBegin string `json:"createdDateTimeBegin,omitempty"`
	CreatedDateTimeEnd   string `json:"createdDateTimeEnd,omitempty"`
	StartTime            string `json:"startTime,omitempty"`
	EndTime              string `json:"endTime,omitempty"`
	Matnr                string `json:"matnr,omitempty"`
	Since                string `json:"since,omitempty"`
}

func (s *Service) EdgeResourceRead(ctx context.Context, id, revision int64, ref string, params EdgeResourceReadParams, timeoutMs int) (any, error) {
	if revision <= 0 {
		return nil, invalid("configRevision", "required")
	}
	if timeoutMs == 0 {
		timeoutMs = 5000
	}
	if timeoutMs < 100 || timeoutMs > 10000 {
		return nil, invalid("timeoutMs", "outOfRange")
	}
	ctx = context.WithValue(ctx, edgeResourceReadContext{}, true)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	row, def, err := edgeResourceSource(ctx, id, revision)
	if err != nil {
		return nil, err
	}
	if def.ConnectionRole() != ClientRole {
		return nil, invalid("resourceRef", "undeclared")
	}
	if params.ItemID != "" && (def.SourceType != "teamcenter" || !tcItemID.MatchString(params.ItemID)) {
		return nil, invalid("params.itemId", "invalidItemIdentity")
	}
	if !params.EdgePlexSAPFilters.Valid(ref) {
		return nil, invalid("params", "unsupported")
	}
	result := map[string]any{"resourceRef": ref, "configRevision": revision}
	if salesforcePagedResource(def, ref) {
		if params != (EdgeResourceReadParams{Limit: params.Limit}) {
			return nil, invalid("params", "unsupported")
		}
		limit := 10
		if params.Limit != nil {
			limit = *params.Limit
		}
		return s.salesforceScan(ctx, row, def, ref, "", limit, limit)
	}
	if def.SourceType == "teamcenter" && strings.HasPrefix(ref, "vendor:teamcenter:file:") {
		if params != (EdgeResourceReadParams{Limit: params.Limit}) {
			return nil, invalid("params", "unsupported")
		}
		limit := 10
		if params.Limit != nil {
			limit = *params.Limit
		}
		return s.teamcenterFilePage(ctx, row, def, ref, "", limit, 0, timeoutMs, true)
	}
	if !supportsBoundedReads(def) && (params.StartTime != "" || params.EndTime != "" || params.Matnr != "" || params.Since != "") {
		return nil, invalid("params", "unsupported")
	}
	if supportsExtendedPage(def) {
		if params != (EdgeResourceReadParams{Limit: params.Limit}) {
			return nil, invalid("params", "unsupported")
		}
		limit := 10
		if def.Protocol == OpenProtocol {
			limit = 1
		}
		if params.Limit != nil {
			limit = *params.Limit
		}
		return s.extendedPage(ctx, row, def, ref, "", limit, 0, timeoutMs, true)
	}
	switch {
	case vendorPages(def):
		limit := 10
		if params.Limit != nil {
			limit = *params.Limit
		}
		return s.businessPage(ctx, row, def, ref, "", limit, 0, timeoutMs, params.CreatedDateTimeBegin, params.CreatedDateTimeEnd, params.ItemID, true)
	case def.Protocol == MQTT || def.Protocol == OPCUA:
		if params.CreatedDateTimeBegin != "" || params.CreatedDateTimeEnd != "" || (params.Limit != nil && *params.Limit != 1) {
			return nil, invalid("params", "unsupported")
		}
		item, e := s.edgeDevicePreview(ctx, row, def, ref)
		if e != nil {
			return nil, e
		}
		result["items"], result["returned"], result["truncated"] = []any{item}, 1, false
	case supportsBoundedReads(def):
		if err = s.readBoundedResource(ctx, row, def, ref, params, timeoutMs, result); err != nil {
			return nil, err
		}
	case def.SourceType == "sap-odata" && def.Protocol == HTTP:
		if params.CreatedDateTimeBegin != "" || params.CreatedDateTimeEnd != "" {
			return nil, invalid("params", "unsupported")
		}
		limit := 10
		if params.Limit != nil {
			limit = *params.Limit
		}
		return s.sapResourcePage(ctx, row, def, ref, "", limit, limit+1, timeoutMs, true, params.EdgePlexSAPFilters)
	case def.SourceType == "plex-mes" && def.Protocol == HTTP && (ref == plexJobsRef || ref == "vendor:plex-mes:operations" || ref == "vendor:plex-mes:production"):
		limit := 10
		if params.Limit != nil {
			limit = *params.Limit
		}
		if limit < 1 || limit > 100 {
			return nil, invalid("params.limit", "outOfRange")
		}
		query := map[string]string{}
		if params.CreatedDateTimeBegin != "" || params.CreatedDateTimeEnd != "" {
			if ref != plexJobsRef {
				return nil, invalid("params", "unsupported")
			}
			begin, e := time.Parse(time.RFC3339Nano, params.CreatedDateTimeBegin)
			end, e2 := time.Parse(time.RFC3339Nano, params.CreatedDateTimeEnd)
			if e != nil || e2 != nil || begin.After(end) {
				return nil, invalid("params", "orderedDateTimeWindowRequired")
			}
			query["createdDateTimeBegin"] = begin.UTC().Format(time.RFC3339Nano)
			query["createdDateTimeEnd"] = end.UTC().Format(time.RFC3339Nano)
		}
		path := map[string]string{}
		if ref == "vendor:plex-mes:operations" {
			path["id"] = params.JobID
		}
		if ref == "vendor:plex-mes:production" {
			query["beginDate"] = params.BeginDate
			query["endDate"] = params.EndDate
			if params.JobID != "" {
				query["jobId"] = params.JobID
			}
		}
		options, _ := json.Marshal(map[string]any{"timeoutMs": timeoutMs, "query": query, "params": path})
		raw, err := s.vendorNativeCall(ctx, row, def, streamexec.ProgramCall{Kind: "source.read", ResourceRef: ref, Options: options}, "", "")
		if err != nil {
			return nil, err
		}
		var response struct {
			Status int
			Data   []json.RawMessage
			Error  json.RawMessage
		}
		if json.Unmarshal(raw, &response) != nil || response.Status < 200 || response.Status >= 300 || len(response.Error) > 0 || response.Data == nil {
			return nil, ErrVendorResponse
		}
		count := min(limit, len(response.Data))
		bytes := 0
		for i, item := range response.Data[:count] {
			if len(item) == 0 || item[0] != '{' {
				return nil, ErrVendorResponse
			}
			bytes += len(item)
			if bytes > 256*1024 {
				if i == 0 {
					return nil, ErrVendorResponse
				}
				count = i
				break
			}
		}
		result["items"], result["returned"], result["truncated"] = response.Data[:count], count, count < len(response.Data)
	case def.Protocol == ModbusTCP && def.Modbus != nil:
		if params != (EdgeResourceReadParams{}) {
			return nil, invalid("params", "unsupported")
		}
		for _, item := range def.Modbus.Ranges {
			if ref != "modbus:"+item.ID {
				continue
			}
			selected := item
			selected.UnitID, selected.Writable = nil, nil
			value, err := s.ReadReference(ctx, id, revision, selected, timeoutMs)
			if err != nil {
				return nil, err
			}
			data, ok := value.(map[string]any)
			if !ok || data["values"] == nil {
				return nil, ErrExecutionFailed
			}
			result["values"], result["unitId"], result["address"], result["table"] = data["values"], data["unitId"], item.Address, item.Table
			break
		}
		if _, ok := result["values"]; !ok {
			return nil, invalid("resourceRef", "undeclared")
		}
	default:
		return nil, invalid("resourceRef", "undeclared")
	}
	latest, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
	if err != nil {
		return nil, err
	}
	if latest.ConfigRevision != revision {
		return nil, repo.ErrConflict
	}
	result["collectedAt"] = time.Now().UnixMilli()
	return result, nil
}
