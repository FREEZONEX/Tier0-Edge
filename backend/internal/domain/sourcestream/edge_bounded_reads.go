package sourcestream

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
)

var materialNumber = regexp.MustCompile(`^[A-Za-z0-9]{1,18}$`)

// These bounded read results carry upstream JSON directly to the declared
// output schema. Keep decimal measurements as JSON numbers through decoding.
type preserveVendorJSONNumbers struct{}

func supportsBoundedReads(def SourceDefinition) bool {
	return def.Protocol == HTTP && (def.SourceType == "pi-web-api" || def.SourceType == "wms-rest" || def.SourceType == "plant0-simctl")
}

func boundedReadResources(def SourceDefinition, row repo.Source) []edgeResource {
	if !supportsBoundedReads(def) {
		return nil
	}
	var verified []NativeResource
	if json.Unmarshal([]byte(row.Resources), &verified) != nil {
		return nil
	}
	var resources []edgeResource
	for _, item := range verified {
		if item.Metadata["sourceType"] != def.SourceType || item.Metadata["readOnly"] != true || item.Metadata["metadataVerifiedAt"] == nil {
			continue
		}
		props := map[string]any{}
		limit := map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 10}
		date := map[string]string{"type": "string", "format": "date-time"}
		id, _ := item.Metadata["nativeIdentity"].(string)
		metadata := map[string]any{}
		var required []string
		switch item.ID {
		case "vendor:wms-rest:inventory", "vendor:wms-rest:movements":
			props["matnr"] = map[string]string{"type": "string", "pattern": materialNumber.String()}
			props["limit"] = limit
			if strings.HasSuffix(item.ID, ":movements") {
				props["since"] = date
				metadata["identityField"] = "move_id"
			} else {
				metadata["identityField"] = "matnr"
			}
		case "vendor:plant0-simctl:board":
			metadata["windowMinutes"] = 30
		default:
			path := vendorParameter(def, "pointPath")
			if def.SourceType != "pi-web-api" || path == "" || item.Metadata["pointPath"] != path || id == "" {
				continue
			}
			switch item.ID {
			case "vendor:pi-web-api:value:" + id:
			case "vendor:pi-web-api:recorded:" + id:
				props["startTime"], props["endTime"], props["limit"] = date, date, limit
				required = []string{"startTime", "endTime"}
				metadata["maxWindowHours"] = 24
			default:
				continue
			}
			metadata["webId"], metadata["pointPath"] = id, path
		}
		input := resourceObject(props)
		if len(required) > 0 {
			input["required"] = required
		}
		resources = append(resources, edgeResource{ResourceRef: item.ID, Name: item.Name, Modes: []string{"read"}, Permission: "uns.source", ConfigRevision: row.ConfigRevision, InputSchema: input, OutputSchema: resourceObject(map[string]any{
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, "returned": map[string]string{"type": "integer"}, "truncated": map[string]string{"type": "boolean"},
		}), Metadata: metadata})
	}
	return resources
}

func validUTC(value string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, value)
	return t, err == nil && strings.HasSuffix(value, "Z")
}

func boundedQuery(ref string, p EdgeResourceReadParams) (map[string]string, int, error) {
	limit := 10
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 1 || limit > 100 || p.CreatedDateTimeBegin != "" || p.CreatedDateTimeEnd != "" {
		return nil, 0, invalid("params", "unsupported")
	}
	query := map[string]string{}
	switch {
	case ref == "vendor:plant0-simctl:board" || strings.HasPrefix(ref, "vendor:pi-web-api:value:"):
		if p != (EdgeResourceReadParams{}) {
			return nil, 0, invalid("params", "unsupported")
		}
		if ref == "vendor:plant0-simctl:board" {
			query["minutes"] = "30"
		}
		limit = 1
	case strings.HasPrefix(ref, "vendor:pi-web-api:recorded:"):
		begin, ok := validUTC(p.StartTime)
		end, valid := validUTC(p.EndTime)
		if !ok || !valid || begin.After(end) || end.Sub(begin) > 24*time.Hour || p.Matnr != "" || p.Since != "" {
			return nil, 0, invalid("params", "boundedUTCWindowRequired")
		}
		query["startTime"], query["endTime"], query["boundaryType"], query["maxCount"] = p.StartTime, p.EndTime, "Inside", strconv.Itoa(limit+1)
	case ref == "vendor:wms-rest:inventory" || ref == "vendor:wms-rest:movements":
		if p.StartTime != "" || p.EndTime != "" || p.Matnr != "" && !materialNumber.MatchString(p.Matnr) {
			return nil, 0, invalid("params", "unsupported")
		}
		if p.Matnr != "" {
			query["matnr"] = p.Matnr
		}
		if p.Since != "" {
			if _, ok := validUTC(p.Since); !ok || ref != "vendor:wms-rest:movements" {
				return nil, 0, invalid("params.since", "unsupported")
			}
			query["since"] = p.Since
		}
		query["limit"] = strconv.Itoa(limit + 1)
	default:
		return nil, 0, invalid("resourceRef", "undeclared")
	}
	return query, limit, nil
}

func (s *Service) readBoundedResource(ctx context.Context, row repo.Source, def SourceDefinition, ref string, params EdgeResourceReadParams, timeoutMs int, result map[string]any) error {
	declared := false
	for _, item := range boundedReadResources(def, row) {
		if item.ResourceRef == ref {
			declared = true
			break
		}
	}
	if !declared {
		return invalid("resourceRef", "undeclared")
	}
	query, limit, err := boundedQuery(ref, params)
	if err != nil {
		return err
	}
	options, _ := json.Marshal(map[string]any{"timeoutMs": timeoutMs, "query": query})
	raw, err := s.vendorNativeCall(context.WithValue(ctx, preserveVendorJSONNumbers{}, true), row, def, streamexec.ProgramCall{Kind: "source.read", ResourceRef: ref, Options: options}, "", "")
	if err != nil {
		return err
	}
	var response struct {
		Status      int
		Data, Error json.RawMessage
	}
	if json.Unmarshal(raw, &response) != nil || response.Status < 200 || response.Status >= 300 || len(response.Error) > 0 {
		return ErrVendorResponse
	}
	var rows []map[string]json.RawMessage
	switch {
	case ref == "vendor:plant0-simctl:board":
		var source struct {
			TS    string   `json:"ts"`
			Run   string   `json:"run"`
			Mode  string   `json:"mode"`
			Speed *float64 `json:"speed"`
			Today struct{ Good, Scrap *int64 }
			WIP   struct {
				Open *int64 `json:"open_wos"`
			}
			Utilities struct{ KW *float64 }
		}
		if json.Unmarshal(response.Data, &source) != nil || source.TS == "" || source.Run == "" || source.Mode == "" || source.Today.Good == nil || source.Today.Scrap == nil || source.WIP.Open == nil || source.Utilities.KW == nil || source.Speed == nil {
			return ErrVendorResponse
		}
		result["items"] = []any{map[string]any{"ts": source.TS, "run": source.Run, "mode": source.Mode, "speed": *source.Speed, "goodToday": *source.Today.Good, "scrapToday": *source.Today.Scrap, "openWorkOrders": *source.WIP.Open, "powerKw": *source.Utilities.KW}}
		result["returned"], result["truncated"] = 1, false
		return nil
	case strings.HasPrefix(ref, "vendor:pi-web-api:value:"):
		var value map[string]json.RawMessage
		if json.Unmarshal(response.Data, &value) != nil || value["Timestamp"] == nil || value["Value"] == nil {
			return ErrVendorResponse
		}
		rows = []map[string]json.RawMessage{value}
	case strings.HasPrefix(ref, "vendor:pi-web-api:recorded:"):
		var body struct{ Items []map[string]json.RawMessage }
		if json.Unmarshal(response.Data, &body) != nil {
			return ErrVendorResponse
		}
		rows = body.Items
	default:
		if json.Unmarshal(response.Data, &rows) != nil {
			return ErrVendorResponse
		}
	}
	if rows == nil {
		return ErrVendorResponse
	}
	count := min(len(rows), limit)
	encoded, err := json.Marshal(rows[:count])
	if err != nil || len(encoded) > 256*1024 {
		return ErrVendorResponse
	}
	result["items"], result["returned"], result["truncated"] = rows[:count], count, len(rows) > count
	return nil
}
