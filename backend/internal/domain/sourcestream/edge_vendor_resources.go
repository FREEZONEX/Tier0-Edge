package sourcestream

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backend/internal/repo"
	"backend/internal/sourceresource"
	"backend/internal/streamexec"

	"github.com/google/uuid"
)

var sfIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,79}$`)
var tcItemID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var sfLocator = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,256}$`)

const crmObjectPrefix = sourceresource.SalesforceObjectPrefix
const tcQueryPrefix = "vendor:teamcenter:query:"
const accessEvents = "vendor:hikvision-isapi:events"

func vendorPages(def SourceDefinition) bool {
	return def.Protocol == OAuth && def.SourceType == "salesforce" || def.Protocol == HTTP && (def.SourceType == "hikvision-isapi" || def.SourceType == "teamcenter")
}

func businessDirectory(def SourceDefinition, row repo.Source) []edgeResource {
	if !vendorPages(def) {
		return nil
	}
	var entries []NativeResource
	_ = json.Unmarshal([]byte(row.Resources), &entries)
	out := []edgeResource{}
	for _, entry := range entries {
		ref := entry.ID
		if entry.Metadata["readOnly"] != true || entry.Metadata["sourceType"] != def.SourceType {
			continue
		}
		input := map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}
		modes := []string{"read", "scan"}
		metadata := map[string]any{}
		switch def.SourceType {
		case "salesforce":
			ref = sourceresource.SalesforceObjectRef(entry)
			if ref == "" {
				continue
			}
			metadata = map[string]any{"identityField": "Id", "pagination": "nextRecordsUrl", "scanLimit": 200, "incrementalField": "SystemModstamp when filterable"}
			input["limit"] = map[string]any{"type": "integer", "description": "Use 1..100 for read and 200 for scan."}
		case "teamcenter":
			query, ok := strings.CutPrefix(ref, tcQueryPrefix)
			if !ok || !safeObjectUID(query) || entry.Metadata["nativeIdentity"] != query {
				continue
			}
			metadata = map[string]any{"identityField": "uid", "pagination": "none", "truncationFlag": "truncated"}
			modes = []string{"read"}
			input["itemId"] = map[string]any{"type": "string", "pattern": tcItemID.String()}
		case "hikvision-isapi":
			metadata = map[string]any{"identityFields": []string{"serialNumber", "serialNo"}, "pagination": "searchResultPosition", "timeWindow": "inclusive"}
			if ref != accessEvents {
				continue
			}
		}
		if def.SourceType != "teamcenter" {
			input["maxItems"] = map[string]any{"type": "integer", "minimum": 0, "maximum": 10000, "description": "Total scan result budget; zero means no total-item limit."}
			for _, key := range []string{"createdDateTimeBegin", "createdDateTimeEnd"} {
				input[key] = map[string]string{"type": "string", "format": "date-time"}
			}
		}
		inputSchema := resourceObject(input)
		if def.SourceType == "hikvision-isapi" {
			inputSchema["required"] = []string{"createdDateTimeBegin", "createdDateTimeEnd"}
		}
		out = append(out, edgeResource{ResourceRef: ref, Name: entry.Name, Modes: modes, Permission: "uns.source", RequiredPermissions: []string{"uns.source", "uns.read"}, ConfigRevision: row.ConfigRevision, Metadata: metadata, InputSchema: inputSchema, OutputSchema: resourceObject(map[string]any{"items": map[string]any{"type": "array", "items": map[string]string{"type": "object"}}})})
	}
	return out
}

func (s *Service) boundedBusinessCall(ctx context.Context, row repo.Source, def SourceDefinition, operation string, opts map[string]any, dst any) error {
	encoded, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	raw, err := s.vendorNativeCall(ctx, row, def, streamexec.ProgramCall{Kind: "source.read", ResourceRef: "vendor:" + def.SourceType + ":" + operation, Options: encoded}, "", "")
	if err != nil {
		return err
	}
	var envelope struct {
		Status int
		Error  json.RawMessage
		Data   json.RawMessage
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Status < 200 || envelope.Status >= 300 || (len(envelope.Error) > 0 && string(envelope.Error) != "null") || json.Unmarshal(envelope.Data, dst) != nil {
		return ErrVendorResponse
	}
	return nil
}

func timeBounds(begin, end string, required bool) (string, string, error) {
	if begin == "" && end == "" && !required {
		return "", "", nil
	}
	a, e1 := time.Parse(time.RFC3339Nano, begin)
	b, e2 := time.Parse(time.RFC3339Nano, end)
	if e1 != nil || e2 != nil || a.After(b) {
		return "", "", invalid("params", "orderedDateTimeWindowRequired")
	}
	return a.UTC().Format(time.RFC3339Nano), b.UTC().Format(time.RFC3339Nano), nil
}

func (s *Service) crmRead(ctx context.Context, row repo.Source, def SourceDefinition, ref, next, begin, end string, fetch, remaining, timeout int) ([]map[string]any, string, error) {
	object := strings.TrimPrefix(ref, crmObjectPrefix)
	if !sfIdentifier.MatchString(object) {
		return nil, "", ErrVendorResponse
	}
	version := vendorParameter(def, "apiVersion")
	if version == "" {
		version = "v67.0"
	}
	// Discover the fields allowed on this object before projecting values or filtering by update time.
	var desc struct {
		Fields []struct {
			Name       string
			Filterable bool
		}
	}
	err := s.boundedBusinessCall(ctx, row, def, "describe:"+object, map[string]any{"timeoutMs": timeout}, &desc)
	if err != nil {
		return nil, "", err
	}
	fields := []string{"Id"}
	hasStamp := false
	for _, field := range desc.Fields {
		switch field.Name {
		case "Name", "LastName", "SystemModstamp":
			fields = append(fields, field.Name)
			if field.Name == "SystemModstamp" && field.Filterable {
				hasStamp = true
			}
		}
	}
	q := "SELECT " + strings.Join(fields, ",") + " FROM " + object
	if begin != "" {
		if !hasStamp {
			return nil, "", invalid("params", "objectHasNoIncrementalField")
		}
		q += " WHERE SystemModstamp >= " + begin + " AND SystemModstamp <= " + end
	}
	if hasStamp {
		q += " ORDER BY SystemModstamp,Id"
	} else {
		q += " ORDER BY Id"
	}
	if remaining > 0 {
		q += " LIMIT " + strconv.Itoa(remaining)
	}
	opts := map[string]any{"query": map[string]string{"q": q}, "timeoutMs": timeout, "headers": map[string]string{"Sforce-Query-Options": "batchSize=200"}}
	if next != "" {
		delete(opts, "query")
		opts["next"] = next
	}
	var page struct {
		Done           *bool
		Records        []map[string]any
		NextRecordsURL string
	}
	if err = s.boundedBusinessCall(ctx, row, def, "query", opts, &page); err != nil {
		return nil, "", err
	}
	if page.Done == nil || page.Records == nil || len(page.Records) > fetch || *page.Done != (page.NextRecordsURL == "") {
		return nil, "", ErrVendorResponse
	}
	for _, r := range page.Records {
		if vendorString(r["Id"]) == "" {
			return nil, "", ErrVendorResponse
		}
		delete(r, "attributes")
	}
	if page.NextRecordsURL != "" {
		path := "/services/data/" + version + "/query/"
		locator, ok := strings.CutPrefix(page.NextRecordsURL, path)
		if !ok || !sfLocator.MatchString(locator) || page.NextRecordsURL == next || len(page.Records) == 0 {
			return nil, "", ErrVendorResponse
		}
	}
	return page.Records, page.NextRecordsURL, nil
}

func (s *Service) accessRead(ctx context.Context, row repo.Source, def SourceDefinition, next, begin, end string, fetch, timeout int) ([]map[string]any, string, error) {
	searchID := uuid.NewString()
	pos := 0
	if next != "" {
		var err error
		var rawPos string
		var found bool
		searchID, rawPos, found = strings.Cut(next, "|")
		if _, parseErr := uuid.Parse(searchID); !found || parseErr != nil {
			return nil, "", ErrVendorResponse
		}
		pos, err = strconv.Atoi(rawPos)
		if err != nil || pos < 1 {
			return nil, "", ErrVendorResponse
		}
	}
	var deviceXML string
	if err := s.boundedBusinessCall(ctx, row, def, "device", map[string]any{"timeoutMs": timeout}, &deviceXML); err != nil {
		return nil, "", err
	}
	var device struct {
		SerialNumber string `xml:"serialNumber"`
	}
	if err := xml.Unmarshal([]byte(deviceXML), &device); err != nil || device.SerialNumber == "" {
		return nil, "", ErrVendorResponse
	}
	cond := map[string]any{"searchID": searchID, "searchResultPosition": pos, "maxResults": fetch, "startTime": begin, "endTime": end, "major": 5, "minor": 1}
	opts := map[string]any{"data": map[string]any{"AcsEventCond": cond}, "query": map[string]string{"format": "json"}, "timeoutMs": timeout}
	var response struct {
		A csPage `json:"AcsEvent"`
	}
	if err := s.boundedBusinessCall(ctx, row, def, "events", opts, &response); err != nil {
		return nil, "", err
	}
	a := response.A
	if a.SearchID != searchID || a.Count != len(a.List) || len(a.List) > fetch {
		return nil, "", ErrVendorResponse
	}
	result := make([]map[string]any, 0, a.Count)
	for _, item := range a.List {
		if item["serialNo"] == nil || item["time"] == nil {
			return nil, "", ErrVendorResponse
		}
		// Avoid card numbers, photos, names or unrelated personal data.
		r := map[string]any{"serialNumber": device.SerialNumber}
		for _, key := range []string{"serialNo", "time", "major", "minor", "doorNo", "attendanceStatus"} {
			if v, ok := item[key]; ok {
				r[key] = v
			}
		}
		result = append(result, r)
	}
	switch a.State {
	case "MORE":
		if a.Count < 1 {
			return nil, "", ErrVendorResponse
		}
		return result, searchID + "|" + strconv.Itoa(pos+a.Count), nil
	case "OK", "NO MATCH":
		return result, "", nil
	}
	return nil, "", ErrVendorResponse
}

type csPage struct {
	SearchID string           `json:"searchID"`
	Count    int              `json:"numOfMatches"`
	State    string           `json:"responseStatusStrg"`
	List     []map[string]any `json:"InfoList"`
}

func safeObjectUID(uid string) bool {
	return uid != "" && len(uid) <= 240 && !strings.ContainsAny(uid, "/?#%:\\\x00\r\n")
}

func (s *Service) tcRead(ctx context.Context, row repo.Source, def SourceDefinition, ref, itemID string, limit, timeout int) ([]map[string]any, bool, error) {
	uid := strings.TrimPrefix(ref, tcQueryPrefix)
	if !safeObjectUID(uid) {
		return nil, false, ErrVendorResponse
	}
	opts := func(body any) map[string]any {
		return map[string]any{"timeoutMs": timeout, "data": map[string]any{"header": map[string]any{}, "body": body}}
	}
	var rr struct {
		ArrayOfResults []struct {
			ObjectUIDS []string `json:"objectUIDS"`
		}
	}
	entry, value := "Name", "*"
	if itemID != "" {
		entry, value = "Item ID", itemID
	}
	err := s.boundedBusinessCall(ctx, row, def, "query", opts(map[string]any{"queries": []any{map[string]any{"query": uid, "clientId": "edge", "entries": []string{entry}, "values": []string{value}, "maxNumToReturn": limit + 1}}}), &rr)
	if err != nil {
		return nil, false, err
	}
	if len(rr.ArrayOfResults) != 1 {
		return nil, false, ErrVendorResponse
	}
	uids := rr.ArrayOfResults[0].ObjectUIDS
	truncated := len(uids) > limit
	if truncated {
		uids = uids[:limit]
	}
	out := make([]map[string]any, 0, len(uids))
	if len(uids) == 0 {
		return out, false, nil
	}
	for _, id := range uids {
		if !safeObjectUID(id) {
			return nil, false, ErrVendorResponse
		}
	}
	// Teamcenter rejects properties from a different model type in a shared getProperties call.
	policyType := func(name string, attributes ...string) map[string]any {
		properties := make([]map[string]string, 0, len(attributes))
		for _, attr := range attributes {
			properties = append(properties, map[string]string{"name": attr})
		}
		return map[string]any{"name": name, "properties": properties}
	}
	types := []any{policyType("Item", "object_name", "object_type", "item_id", "revision_list"), policyType("ItemRevision", "object_name", "object_type", "item_id", "item_revision_id", "items_tag")}
	var props struct {
		ModelObjects map[string]map[string]any
		ServiceData  struct{ ModelObjects map[string]map[string]any }
	}
	options := map[string]any{"timeoutMs": timeout, "data": map[string]any{"header": map[string]any{"policy": map[string]any{"types": types}}, "body": map[string]any{"uids": uids}}}
	if err = s.boundedBusinessCall(ctx, row, def, "objects", options, &props); err != nil {
		return nil, false, err
	}
	if props.ModelObjects == nil {
		props.ModelObjects = props.ServiceData.ModelObjects
	}
	for _, id := range uids {
		item := props.ModelObjects[id]
		if item == nil || vendorString(item["uid"]) != id {
			return nil, false, ErrVendorResponse
		}
		out = append(out, item)
	}
	return out, truncated, nil
}

func (s *Service) businessPage(ctx context.Context, row repo.Source, def SourceDefinition, ref, cursor string, limit, maxItems, timeout int, begin, end, itemID string, preview bool) (any, error) {
	allowed := false
	for _, resource := range businessDirectory(def, row) {
		if ref == resource.ResourceRef {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, invalid("resourceRef", "undeclared")
	}
	if limit < 1 || (limit > 100 && !(def.SourceType == "salesforce" && limit == 200 && !preview)) || maxItems < 0 || maxItems > 10000 {
		return nil, invalid("limit", "outOfRange")
	}
	begin, end, err := timeBounds(begin, end, def.SourceType == "hikvision-isapi")
	if err != nil {
		return nil, err
	}
	scope := append(scanScope(ctx, row.ID, row.ConfigRevision, ref, limit, maxItems), []byte("|"+begin+"|"+end)...)
	page := sapContinuation{Remaining: maxItems}
	if cursor != "" {
		page, err = s.openScanCursor(cursor, scope)
		if err != nil {
			return nil, err
		}
	}
	fetch := limit
	if page.Remaining > 0 {
		fetch = min(fetch, page.Remaining)
	}
	var items []map[string]any
	var next string
	var truncated bool
	switch def.SourceType {
	case "salesforce":
		if !preview && limit != 200 {
			return nil, invalid("limit", "salesforceNativeBatchSize200Required")
		}
		n := page.Remaining
		if preview {
			n = limit + 1
			fetch = n
		}
		items, next, err = s.crmRead(ctx, row, def, ref, page.Link, begin, end, fetch, n, timeout)
	case "hikvision-isapi":
		items, next, err = s.accessRead(ctx, row, def, page.Link, begin, end, fetch, timeout)
	case "teamcenter":
		if !preview || begin != "" {
			return nil, invalid("params", "nativeScanUnsupported")
		}
		items, truncated, err = s.tcRead(ctx, row, def, ref, itemID, limit, timeout)
	default:
		return nil, invalid("resourceRef", "undeclared")
	}
	if err != nil {
		return nil, err
	}
	if preview && len(items) > limit {
		truncated = true
		items = items[:limit]
	}
	size, err := json.Marshal(items)
	if err != nil || len(size) > 256*1024 {
		return nil, invalid("limit", "upstreamNativePageExceedsByteLimit")
	}
	latest, err := repo.NewSourceStreamRepo(ctx).Source(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	if latest.ConfigRevision != row.ConfigRevision {
		return nil, repo.ErrConflict
	}
	result := map[string]any{"collectedAt": time.Now().UnixMilli(), "resourceRef": ref, "configRevision": row.ConfigRevision, "items": items}
	if preview {
		result["truncated"], result["returned"] = truncated || next != "", len(items)
		return result, nil
	}
	if maxItems > 0 {
		page.Remaining -= len(items)
		if page.Remaining < 0 {
			return nil, ErrVendorResponse
		}
		if page.Remaining == 0 {
			next = ""
		}
	}
	token, err := s.nextScanCursor(next, page.Remaining, scope)
	if err != nil {
		return nil, err
	}
	result["hasMore"], result["nextCursor"] = token != "", token
	return result, nil
}
