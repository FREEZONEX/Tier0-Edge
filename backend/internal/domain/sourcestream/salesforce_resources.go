package sourcestream

import (
	"backend/internal/repo"
	"backend/internal/streamexec"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"

	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type SalesforceQueryDeclaration struct {
	ID       string   `json:"id"`
	Object   string   `json:"object"`
	Fields   []string `json:"fields"`
	Start    string   `json:"start"`
	End      string   `json:"end"`
	QueryAll bool     `json:"queryAll,omitempty"`
	CDC      bool     `json:"cdc,omitempty"`
}
type salesforceQuery = SalesforceQueryDeclaration

func salesforceQueries(def SourceDefinition) ([]salesforceQuery, error) {
	if def.SourceType != "salesforce" || def.Protocol != OAuth || def.HTTP == nil {
		return nil, nil
	}
	value, ok := def.HTTP.Parameters["salesforceQueries"]
	if !ok {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var queries []salesforceQuery
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&queries) != nil || len(queries) > 16 {
		return nil, invalid("http.parameters.salesforceQueries", "invalid")
	}
	seen := map[string]bool{}
	cdcCount := 0
	for i := range queries {
		q := &queries[i]
		if !sfIdentifier.MatchString(q.ID) || !sfIdentifier.MatchString(q.Object) || seen[q.ID] || len(q.Fields) < 1 || len(q.Fields) > 100 {
			return nil, invalid("salesforceQueries", "invalidDeclaration")
		}
		seen[q.ID] = true
		fields := map[string]bool{}
		for _, f := range q.Fields {
			if !sfIdentifier.MatchString(f) || fields[f] {
				return nil, invalid("salesforceQueries.fields", "invalid")
			}
			fields[f] = true
		}
		if !fields["Id"] || !fields["SystemModstamp"] || q.QueryAll && !fields["IsDeleted"] {
			return nil, invalid("salesforceQueries.fields", "identityWatermarkDeletionRequired")
		}
		q.Start, q.End, err = timeBounds(q.Start, q.End, true)
		if err != nil {
			return nil, err
		}
		if q.CDC {
			cdcCount++
		}
		if cdcCount > 1 {
			return nil, invalid("salesforceQueries.cdc", "oneTopicPerSourceRequired")
		}
		if q.CDC && q.Object == "User" {
			return nil, invalid("salesforceQueries.cdc", "unsupportedObject")
		}
	}
	if cdcCount > 0 {
		host, port, e := net.SplitHostPort(vendorParameter(def, "salesforcePubSubEndpoint"))
		origin, oe := url.Parse(def.Endpoint)
		p, pe := strconv.Atoi(port)
		tenant := vendorParameter(def, "salesforceTenantId")
		if e != nil || oe != nil || pe != nil || p < 1 || p > 65535 || !strings.EqualFold(host, origin.Hostname()) || len(tenant) < 1 || len(tenant) > 128 || strings.ContainsAny(tenant, "\r\n\x00") {
			return nil, invalid("http.parameters.salesforcePubSubEndpoint", "sameOriginTLSAndTenantRequired")
		}
	}
	return queries, nil
}

func salesforceDirectory(def SourceDefinition, row repo.Source) []edgeResource {
	queries, err := salesforceQueries(def)
	if err != nil {
		return nil
	}
	out := []edgeResource{}
	for _, q := range queries {
		for _, mode := range []string{"bulk", "deleted", "cdc"} {
			if mode == "cdc" && !q.CDC {
				continue
			}
			modes := []string{"read", "scan"}
			if mode == "cdc" {
				modes = []string{"receive"}
			}
			out = append(out, edgeResource{ResourceRef: "vendor:salesforce:" + mode + ":" + q.ID, Name: q.ID + " " + mode, Modes: modes, Permission: "uns.source", RequiredPermissions: []string{"uns.source", "uns.read"}, ConfigRevision: row.ConfigRevision, InputSchema: resourceObject(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, "maxItems": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000}}), OutputSchema: resourceObject(map[string]any{"items": map[string]any{"type": "array"}}), Metadata: map[string]any{"object": q.Object, "fields": q.Fields, "acknowledgement": "durableCommit", "identityField": "Id", "incrementalField": "SystemModstamp", "fixedWindow": true, "deletionSemantics": "deleted emits IsDeleted=true and SystemModstamp=source deletedDate; queryAll preserves IsDeleted=false on restored records; upsert by Id", "cdcUpdateSemantics": "apply changedFields and nulledFields; unchanged null is not a clear"}})
		}
	}
	return out
}

func salesforceSelection(def SourceDefinition, ref string) (salesforceQuery, string, error) {
	qs, err := salesforceQueries(def)
	if err != nil {
		return salesforceQuery{}, "", err
	}
	for _, q := range qs {
		for _, mode := range []string{"bulk", "deleted", "cdc"} {
			if ref == "vendor:salesforce:"+mode+":"+q.ID && (mode != "cdc" || q.CDC) {
				return q, mode, nil
			}
		}
	}
	return salesforceQuery{}, "", invalid("resourceRef", "undeclared")
}

func (s *Service) salesforceHTTP(ctx context.Context, row repo.Source, def SourceDefinition, method, path string, query map[string]string, body any) (HTTPResponse, error) {
	var out HTTPResponse
	rawBody, _ := json.Marshal(body)
	if body == nil {
		rawBody = nil
	}
	version := vendorParameter(def, "apiVersion")
	if version == "" {
		version = "v67.0"
	}
	if !strings.HasPrefix(version, "v") || strings.ContainsAny(version, "/\\?#") {
		return out, ErrVendorResponse
	}
	options, _ := json.Marshal(HTTPRequestOptions{Method: method, Path: "services/data/" + version + "/" + path, Query: query, JSON: rawBody, Headers: map[string]string{"Accept": "application/json, text/csv"}})
	raw, err := s.HTTPProgramCall(context.WithValue(ctx, vendorReadOnlyContext{}, true), row, def, streamexec.ProgramCall{Kind: "source.request", Options: options})
	if err != nil {
		return out, err
	}
	if json.Unmarshal(raw, &out) != nil {
		return out, ErrVendorResponse
	}
	if out.Status < 200 || out.Status >= 300 {
		return out, EdgeHTTPResponseError(out.Status)
	}
	return out, nil
}

type salesforceBulkState struct {
	JobID      string            `json:"jobId"`
	State      string            `json:"state"`
	Locator    string            `json:"locator"`
	CreatedAt  int64             `json:"createdAt"`
	FieldTypes map[string]string `json:"fieldTypes,omitempty"`
}
type salesforcePage struct {
	JobID   string `json:"jobId"`
	Locator string `json:"locator"`
	Offset  int    `json:"offset,omitempty"`
}

func (s *Service) salesforceScan(ctx context.Context, row repo.Source, def SourceDefinition, ref, cursor string, limit, maxItems int) (result any, resultErr error) {
	defer func() {
		if resultErr != nil {
			return
		}
		current, e := repo.NewSourceStreamRepo(ctx).Source(ctx, row.ID)
		if e != nil {
			result, resultErr = nil, e
			return
		}
		if current.ConfigRevision != row.ConfigRevision || current.DesiredState != "enabled" {
			result, resultErr = nil, repo.ErrConflict
			return
		}
		envelope, ok := result.(map[string]any)
		if !ok {
			result, resultErr = nil, ErrVendorResponse
			return
		}
		envelope["resourceRef"] = ref
		envelope["configRevision"] = row.ConfigRevision
		envelope["collectedAt"] = time.Now().UnixMilli()
	}()
	q, mode, err := salesforceSelection(def, ref)
	if err != nil {
		return nil, err
	}
	if mode == "cdc" {
		return nil, invalid("resourceRef", "scanUnsupported")
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || maxItems < 0 || maxItems > 10000 {
		return nil, invalid("limit", "invalid")
	}
	scope := scanScope(ctx, row.ID, row.ConfigRevision, ref, limit, maxItems)
	hash := sha256.Sum256(scope)
	key := hex.EncodeToString(hash[:])
	page := salesforcePage{}
	remaining := maxItems
	if cursor != "" {
		c, e := s.openScanCursor(cursor, scope)
		if e != nil {
			return nil, e
		}
		if json.Unmarshal([]byte(c.Link), &page) != nil {
			return nil, ErrVendorResponse
		}
		remaining = c.Remaining
	}
	if mode == "deleted" {
		return s.salesforceDeleted(ctx, row, def, q, page, remaining, limit, scope)
	}
	var state salesforceBulkState
	err = repo.NewSourceStreamRepo(ctx).SalesforceState(ctx, row.ID, row.ConfigRevision, func(states map[string]json.RawMessage) error {
		if len(states[key]) > 0 {
			return json.Unmarshal(states[key], &state)
		}
		if len(states) >= 128 {
			return invalid("job", "sourceJobBudgetExceeded")
		}
		state = salesforceBulkState{State: "creating", CreatedAt: time.Now().UnixMilli()}
		states[key], _ = json.Marshal(state)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if state.JobID == "" {
		var describe struct {
			Fields []struct {
				Name string
				Type string
			}
		}
		if e := s.boundedBusinessCall(ctx, row, def, "describe:"+q.Object, map[string]any{}, &describe); e != nil {
			return nil, e
		}
		state.FieldTypes = map[string]string{}
		for _, f := range describe.Fields {
			state.FieldTypes[f.Name] = f.Type
		}
		for _, f := range q.Fields {
			if state.FieldTypes[f] == "" {
				return nil, invalid("fields", "notInObjectDescribe")
			}
		}
		// A persisted creating marker makes an interrupted POST visible; never silently duplicate a job.
		// Only its creator may make the POST in this invocation.
		if state.CreatedAt == 0 {
			return nil, invalid("job", "creationOutcomeUnknown")
		}
		claimed := false
		err = repo.NewSourceStreamRepo(ctx).SalesforceState(ctx, row.ID, row.ConfigRevision, func(states map[string]json.RawMessage) error {
			var current salesforceBulkState
			_ = json.Unmarshal(states[key], &current)
			if current.State != "creating" {
				return nil
			}
			current.State = "submitting"
			states[key], _ = json.Marshal(current)
			claimed = true
			return nil
		})
		if err != nil {
			return nil, err
		}
		query := "SELECT " + strings.Join(q.Fields, ",") + " FROM " + q.Object + " WHERE SystemModstamp >= " + q.Start + " AND SystemModstamp <= " + q.End + " ORDER BY SystemModstamp,Id"
		operation := "query"
		if q.QueryAll {
			operation = "queryAll"
		}
		var response HTTPResponse
		var e error
		if claimed {
			response, e = s.salesforceHTTP(ctx, row, def, http.MethodPost, "jobs/query", nil, map[string]any{"operation": operation, "query": query, "contentType": "CSV", "columnDelimiter": "COMMA", "lineEnding": "LF"})
		} else {
			response, e = s.salesforceRecoverJob(ctx, row, def, query, operation, state.CreatedAt)
		}
		if e != nil {
			return nil, e
		}
		data, ok := response.Data.(map[string]any)
		if !ok {
			return nil, ErrVendorResponse
		}
		state.JobID = vendorString(data["id"])
		state.State = vendorString(data["state"])
		if !sfLocator.MatchString(state.JobID) {
			return nil, ErrVendorResponse
		}
		err = repo.NewSourceStreamRepo(ctx).SalesforceState(ctx, row.ID, row.ConfigRevision, func(states map[string]json.RawMessage) error { states[key], _ = json.Marshal(state); return nil })
		if err != nil {
			return nil, err
		}
	}
	if page.JobID != "" && page.JobID != state.JobID {
		return nil, invalid("cursor", "invalidOrStale")
	}
	page.JobID = state.JobID
	response, err := s.salesforceHTTP(ctx, row, def, "GET", "jobs/query/"+state.JobID, nil, nil)
	if err != nil {
		return nil, err
	}
	data, ok := response.Data.(map[string]any)
	if !ok {
		return nil, ErrVendorResponse
	}
	state.State = vendorString(data["state"])
	if state.State == "Failed" || state.State == "Aborted" {
		return nil, invalid("job", "bulkJobFailed")
	}
	var items []map[string]any
	hasMore := true
	if state.State == "JobComplete" {
		query := map[string]string{"maxRecords": strconv.Itoa(limit)}
		if page.Locator != "" {
			query["locator"] = page.Locator
		}
		response, err = s.salesforceHTTP(ctx, row, def, "GET", "jobs/query/"+state.JobID+"/results", query, nil)
		if err != nil {
			return nil, err
		}
		csvText, ok := response.Data.(string)
		if !ok {
			return nil, ErrVendorResponse
		}
		items, err = salesforceCSV(csvText, q.Fields, limit)
		if err != nil {
			return nil, err
		}
		if err = salesforceCSVTypes(items, state.FieldTypes); err != nil {
			return nil, err
		}
		locator := http.Header(response.Headers).Get("Sforce-Locator")
		if locator == "" {
			return nil, ErrVendorResponse
		}
		hasMore = locator != "null"
		page.Locator = locator
		if !hasMore {
			page.Locator = ""
		}
		state.Locator = page.Locator
	} else if state.State != "InProgress" && state.State != "UploadComplete" {
		return nil, ErrVendorResponse
	}
	if items == nil {
		items = []map[string]any{}
	}
	truncated := false
	if remaining > 0 {
		if len(items) > remaining {
			truncated = true
			items = items[:remaining]
		}
		remaining -= len(items)
		if remaining == 0 {
			truncated = truncated || hasMore
			hasMore = false
		}
	}
	err = repo.NewSourceStreamRepo(ctx).SalesforceState(ctx, row.ID, row.ConfigRevision, func(states map[string]json.RawMessage) error { states[key], _ = json.Marshal(state); return nil })
	if err != nil {
		return nil, err
	}
	next := ""
	if hasMore {
		raw, _ := json.Marshal(page)
		next, err = s.nextScanCursor(string(raw), remaining, scope)
	}
	return map[string]any{"items": items, "returned": len(items), "truncated": truncated, "nextCursor": next, "hasMore": hasMore, "jobId": state.JobID, "jobState": state.State}, err
}

// An interrupted POST is recovered from the current OAuth user's job catalogue,
// never retried. Ambiguous or incomplete catalogues require operator resolution.
func (s *Service) salesforceRecoverJob(ctx context.Context, row repo.Source, def SourceDefinition, query, operation string, createdAt int64) (HTTPResponse, error) {
	response, err := s.salesforceHTTP(ctx, row, def, "GET", "jobs/query", nil, nil)
	if err != nil {
		return response, err
	}
	raw, _ := json.Marshal(response.Data)
	var list struct {
		Records        []map[string]any
		NextRecordsURL string
		Done           *bool
	}
	if json.Unmarshal(raw, &list) != nil || list.Records == nil || list.NextRecordsURL != "" || list.Done != nil && !*list.Done || len(list.Records) > 100 {
		return response, invalid("job", "creationRecoveryNeedsCompleteCatalogue")
	}
	var found *HTTPResponse
	for _, candidate := range list.Records {
		id := vendorString(candidate["id"])
		if !sfLocator.MatchString(id) {
			return response, ErrVendorResponse
		}
		created := vendorString(candidate["createdDate"])
		stamp, e := time.Parse(time.RFC3339Nano, created)
		if e != nil {
			stamp, e = time.Parse("2006-01-02T15:04:05.999999999-0700", created)
		}
		if e != nil || stamp.UnixMilli() < createdAt-5000 || stamp.UnixMilli() > createdAt+30000 {
			continue
		}
		detail, e := s.salesforceHTTP(ctx, row, def, "GET", "jobs/query/"+id, nil, nil)
		if e != nil {
			return response, e
		}
		job, ok := detail.Data.(map[string]any)
		if !ok {
			return response, ErrVendorResponse
		}
		if vendorString(job["query"]) != query || vendorString(job["operation"]) != operation || vendorString(job["contentType"]) != "CSV" {
			continue
		}
		if found != nil {
			return response, invalid("job", "creationRecoveryAmbiguous")
		}
		copy := detail
		found = &copy
	}
	if found == nil {
		return response, invalid("job", "creationOutcomeUnknown")
	}
	return *found, nil
}

func salesforceCSV(text string, fields []string, limit int) ([]map[string]any, error) {
	r := csv.NewReader(strings.NewReader(text))
	header, err := r.Read()
	if err != nil || len(header) != len(fields) {
		return nil, ErrVendorResponse
	}
	allowed := map[string]bool{}
	for _, f := range fields {
		allowed[f] = true
	}
	for _, f := range header {
		if !allowed[f] {
			return nil, ErrVendorResponse
		}
		delete(allowed, f)
	}
	out := []map[string]any{}
	for {
		values, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil || len(out) >= limit {
			return nil, ErrVendorResponse
		}
		row := map[string]any{}
		for i, f := range header {
			if values[i] == "" {
				row[f] = nil
			} else if f == "IsDeleted" {
				v, e := strconv.ParseBool(values[i])
				if e != nil {
					return nil, ErrVendorResponse
				}
				row[f] = v
			} else {
				row[f] = values[i]
			}
		}
		if vendorString(row["Id"]) == "" {
			return nil, ErrVendorResponse
		}
		out = append(out, row)
	}
	return out, nil
}

func salesforceCSVTypes(rows []map[string]any, types map[string]string) error {
	for _, row := range rows {
		for field, value := range row {
			if value == nil {
				continue
			}
			text, ok := value.(string)
			if !ok {
				continue
			}
			switch types[field] {
			case "boolean":
				v, e := strconv.ParseBool(text)
				if e != nil {
					return ErrVendorResponse
				}
				row[field] = v
			case "int", "long":
				if _, e := strconv.ParseInt(text, 10, 64); e != nil {
					return ErrVendorResponse
				}
				row[field] = json.Number(text)
			case "double", "currency", "percent":
				if _, e := strconv.ParseFloat(text, 64); e != nil || !json.Valid([]byte(text)) {
					return ErrVendorResponse
				}
				row[field] = json.Number(text)
			}
		}
	}
	return nil
}

func (s *Service) salesforceDeleted(ctx context.Context, row repo.Source, def SourceDefinition, q salesforceQuery, page salesforcePage, remaining, limit int, scope []byte) (any, error) {
	start, _ := time.Parse(time.RFC3339Nano, q.Start)
	end, _ := time.Parse(time.RFC3339Nano, q.End)
	if end.Sub(start) > 30*24*time.Hour {
		return nil, invalid("start", "deletionWindowExpired")
	}
	hash := sha256.Sum256(scope)
	key := "deleted:" + hex.EncodeToString(hash[:])
	var raw json.RawMessage
	err := repo.NewSourceStreamRepo(ctx).SalesforceState(ctx, row.ID, row.ConfigRevision, func(states map[string]json.RawMessage) error {
		raw = append(json.RawMessage(nil), states[key]...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		if start.Before(time.Now().Add(-30 * 24 * time.Hour)) {
			return nil, invalid("start", "deletionWindowExpired")
		}
		response, e := s.salesforceHTTP(ctx, row, def, "GET", "sobjects/"+q.Object+"/deleted", map[string]string{"start": q.Start, "end": q.End}, nil)
		if e != nil {
			return nil, e
		}
		raw, _ = json.Marshal(response.Data)
		if len(raw) > 1024*1024 {
			return nil, ErrVendorResponse
		}
		err = repo.NewSourceStreamRepo(ctx).SalesforceState(ctx, row.ID, row.ConfigRevision, func(states map[string]json.RawMessage) error {
			if saved := states[key]; len(saved) > 0 {
				raw = append(json.RawMessage(nil), saved...)
			} else {
				if len(states) >= 128 {
					return invalid("job", "sourceJobBudgetExceeded")
				}
				states[key] = raw
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	var data struct {
		DeletedRecords []struct {
			ID          string `json:"id"`
			DeletedDate string `json:"deletedDate"`
		}
		LatestDateCovered string
	}
	if json.Unmarshal(raw, &data) != nil || data.LatestDateCovered == "" {
		return nil, ErrVendorResponse
	}
	if page.Offset < 0 || page.Offset > len(data.DeletedRecords) {
		return nil, ErrVendorResponse
	}
	items := []map[string]any{}
	for page.Offset < len(data.DeletedRecords) && len(items) < limit && (remaining == 0 || len(items) < remaining) {
		r := data.DeletedRecords[page.Offset]
		if r.ID == "" || r.DeletedDate == "" {
			return nil, ErrVendorResponse
		}
		deletedAt, e := time.Parse(time.RFC3339Nano, r.DeletedDate)
		if e != nil {
			deletedAt, e = time.Parse("2006-01-02T15:04:05.999999999-0700", r.DeletedDate)
		}
		if e != nil || deletedAt.Before(start) || deletedAt.After(end) {
			return nil, ErrVendorResponse
		}
		items = append(items, map[string]any{"Id": r.ID, "IsDeleted": true, "SystemModstamp": deletedAt.UTC().Format(time.RFC3339Nano)})
		page.Offset++
	}
	hasMore := page.Offset < len(data.DeletedRecords)
	truncated := false
	if remaining > 0 {
		remaining -= len(items)
		truncated = hasMore && remaining == 0
		hasMore = hasMore && remaining > 0
	}
	next := ""
	if hasMore {
		raw, _ = json.Marshal(page)
		next, err = s.nextScanCursor(string(raw), remaining, scope)
	}
	return map[string]any{"items": items, "returned": len(items), "truncated": truncated, "nextCursor": next, "hasMore": hasMore, "latestDateCovered": data.LatestDateCovered}, err
}
