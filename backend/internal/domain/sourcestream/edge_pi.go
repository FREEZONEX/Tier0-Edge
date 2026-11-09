package sourcestream

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"golang.org/x/time/rate"
)

var piWebID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`)

// PI collection rows preserve the JSON type of the original TimedValue, including
// numeric decimals. The generic JavaScript vendor interface stringifies them.

// Concurrent archive bindings on the Edge share a small upstream request budget.
// Snapshot reads remain independent so a backfill cannot starve live collection.
var piArchiveRate = rate.NewLimiter(rate.Every(500*time.Millisecond), 1)

const piRefPrefix = "vendor:pi-web-api:"

func validatePIBinding(d SourceDefinition) error {
	for _, name := range []string{"pointWebId", "backfillStart", "backfillEnd"} {
		if value, present := d.HTTP.Parameters[name]; present {
			if _, ok := value.(string); !ok {
				return invalid("http.parameters."+name, "invalid")
			}
		}
	}
	id, start, end := vendorParameter(d, "pointWebId"), vendorParameter(d, "backfillStart"), vendorParameter(d, "backfillEnd")
	// Discovery by path and a fixed WebID can yield the same resource ref with
	// different archive limits. Each Source must choose one binding.
	if id != "" && vendorParameter(d, "pointPath") != "" {
		return invalid("http.parameters.pointWebId", "conflictingPointBinding")
	}
	if id == "" && start == "" && end == "" {
		return nil
	}
	if !piWebID.MatchString(id) || d.Protocol != HTTP || !strings.HasPrefix(d.Endpoint, "https://") || d.HTTP.Authentication != "basic" {
		return invalid("http.parameters.pointWebId", "verifiedPIConnectionRequired")
	}
	if start == "" && end == "" {
		return nil
	}
	a, err := time.Parse(time.RFC3339Nano, start)
	b, err2 := time.Parse(time.RFC3339Nano, end)
	_, off1 := a.Zone()
	_, off2 := b.Zone()
	if err != nil || err2 != nil || off1 != 0 || off2 != 0 || a.After(b) {
		return invalid("http.parameters.backfillStart", "orderedUTCWindowRequired")
	}
	return nil
}

func piDeclaredResources(def SourceDefinition, revision int64) []edgeResource {
	id := vendorParameter(def, "pointWebId")
	if def.SourceType != "pi-web-api" || def.HTTP == nil || id == "" || validatePIBinding(def) != nil {
		return nil
	}
	var resources []edgeResource
	for _, op := range []string{"value", "recorded"} {
		modes := []string{"read"}
		input := map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}
		if op == "recorded" {
			if vendorParameter(def, "backfillStart") == "" {
				continue
			}
			modes = append(modes, "scan")
			input["maxItems"] = map[string]any{"type": "integer", "minimum": 0, "maximum": 10000}
		}
		fields := map[string]any{
			"pointWebId":        map[string]string{"type": "string"},
			"Timestamp":         map[string]string{"type": "string", "format": "date-time"},
			"Value":             map[string]string{"description": "Original PI value, including digital state objects and null."},
			"UnitsAbbreviation": map[string]string{"type": "string"},
		}
		for _, flag := range []string{"Good", "Questionable", "Substituted", "Annotated"} {
			fields[flag] = map[string]string{"type": "boolean"}
		}
		resource := edgeResource{
			ResourceRef:    piRefPrefix + op + ":" + id,
			Name:           op + " " + id,
			Modes:          modes,
			Permission:     "uns.source",
			ConfigRevision: revision,
			InputSchema:    resourceObject(input),
			OutputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"items": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": fields}},
			}},
			Metadata: map[string]any{"pointWebId": id, "identityFields": []string{"pointWebId", "Timestamp"}},
		}
		if op == "recorded" {
			resource.Metadata["startTime"] = vendorParameter(def, "backfillStart")
			resource.Metadata["endTime"] = vendorParameter(def, "backfillEnd")
		}
		resources = append(resources, resource)
	}
	return resources
}

func (s *Service) piResourcePage(ctx context.Context, row repo.Source, def SourceDefinition, ref, cursor string, limit, maxItems, timeoutMs int, preview bool) (any, error) {
	id := vendorParameter(def, "pointWebId")
	if id == "" || def.SourceType != "pi-web-api" || def.HTTP == nil || validatePIBinding(def) != nil {
		return nil, invalid("resourceRef", "undeclared")
	}
	archive := ref == piRefPrefix+"recorded:"+id && vendorParameter(def, "backfillStart") != ""
	if !archive && (!preview || ref != piRefPrefix+"value:"+id) {
		return nil, invalid("resourceRef", "undeclared")
	}
	scope := scanScope(ctx, row.ID, row.ConfigRevision, ref, limit, maxItems)
	start, end := vendorParameter(def, "backfillStart"), vendorParameter(def, "backfillEnd")
	budget := maxItems
	if cursor != "" {
		page, err := s.openScanCursor(cursor, scope)
		if err != nil {
			return nil, err
		}
		start, budget = page.Link, page.Remaining
	}
	size := limit
	if budget > 0 {
		size = min(size, budget)
	}
	query := map[string]string{}
	if archive {
		if err := piArchiveRate.Wait(ctx); err != nil {
			return nil, err
		}
		query = map[string]string{"startTime": start, "endTime": end, "maxCount": strconv.Itoa(size + 1), "boundaryType": "Inside"}
	}
	if deadline, ok := s.piRetryAfter.Load(row.ID); ok && time.Now().Before(deadline.(time.Time)) {
		return nil, invalid("source", "upstreamRateLimited")
	}
	opts, _ := json.Marshal(map[string]any{"timeoutMs": timeoutMs, "query": query})
	raw, err := s.vendorNativeCall(context.WithValue(ctx, preserveVendorJSONNumbers{}, true), row, def, streamexec.ProgramCall{Kind: "source.read", ResourceRef: ref, Options: opts}, "", "")
	if err != nil {
		return nil, err
	}
	var response struct {
		Status  int
		Headers http.Header
		Data    json.RawMessage
		Error   json.RawMessage
	}
	if json.Unmarshal(raw, &response) != nil {
		return nil, ErrVendorResponse
	}
	if response.Status == http.StatusTooManyRequests {
		deadline := time.Now().Add(time.Second)
		retry := response.Headers.Get("Retry-After")
		if at, err := http.ParseTime(retry); err == nil && at.After(deadline) {
			deadline = at
		} else if seconds, err := strconv.ParseUint(retry, 10, 32); err == nil && seconds > 0 {
			deadline = time.Now().Add(time.Duration(seconds) * time.Second)
		}
		s.piRetryAfter.Store(row.ID, deadline)
		time.AfterFunc(time.Until(deadline), func() { s.piRetryAfter.CompareAndDelete(row.ID, deadline) })
		return nil, invalid("source", "upstreamRateLimited")
	}
	if response.Status != http.StatusOK || len(response.Error) != 0 {
		return nil, ErrVendorResponse
	}
	items := []json.RawMessage{response.Data}
	if archive {
		var data struct{ Items []json.RawMessage }
		if json.Unmarshal(response.Data, &data) != nil || data.Items == nil || len(data.Items) > size+1 {
			return nil, ErrVendorResponse
		}
		items = data.Items
	}
	if cursor != "" && len(items) == 0 {
		return nil, invalid("cursor", "archiveBoundaryChanged")
	}
	out := make([]map[string]json.RawMessage, 0, min(size, len(items)))
	since, _ := time.Parse(time.RFC3339Nano, start)
	until, _ := time.Parse(time.RFC3339Nano, end)
	var last time.Time
	next := ""
	for i, rawItem := range items {
		var sample map[string]json.RawMessage
		if json.Unmarshal(rawItem, &sample) != nil || sample["Value"] == nil {
			return nil, ErrVendorResponse
		}
		var stamp string
		if json.Unmarshal(sample["Timestamp"], &stamp) != nil {
			return nil, ErrVendorResponse
		}
		at, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil || archive && (at.Before(since) || at.After(until) || i > 0 && !at.After(last)) {
			return nil, ErrVendorResponse
		}
		// The first unread sample was observed as lookahead on the previous
		// page. Missing that boundary means the vendor archive changed.
		if cursor != "" && i == 0 && !at.Equal(since) {
			return nil, invalid("cursor", "archiveBoundaryChanged")
		}
		for _, key := range []string{"Good", "Questionable", "Substituted", "Annotated"} {
			if string(sample[key]) != "true" && string(sample[key]) != "false" {
				return nil, ErrVendorResponse
			}
		}
		last = at
		stamp = at.UTC().Format(time.RFC3339Nano)
		if i == size {
			next = stamp
			break
		}
		sample["Timestamp"], _ = json.Marshal(stamp)
		sample["pointWebId"], _ = json.Marshal(vendorParameter(def, "pointWebId"))
		out = append(out, sample)
	}
	if encoded, _ := json.Marshal(out); len(encoded) > 256*1024 {
		return nil, invalid("limit", "upstreamNativePageExceedsByteLimit")
	}
	result := map[string]any{"resourceRef": ref, "configRevision": row.ConfigRevision, "collectedAt": time.Now().UnixMilli(), "items": out}
	if preview {
		result["returned"], result["truncated"] = len(out), next != ""
		return result, nil
	}
	if budget > 0 {
		budget -= len(out)
		if budget == 0 {
			result["truncated"] = next != ""
			next = ""
		}
	}
	token, err := s.nextScanCursor(next, budget, scope)
	if err != nil {
		return nil, err
	}
	result["nextCursor"], result["hasMore"] = token, token != ""
	return result, nil
}
