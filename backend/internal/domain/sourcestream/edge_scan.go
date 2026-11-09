package sourcestream

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/streamexec"
)

const sapEntityPrefix = "vendor:sap-odata:entity:"

func sapDiscoveredEntities(row repo.Source) []NativeResource {
	var discovered []NativeResource
	if json.Unmarshal([]byte(row.Resources), &discovered) != nil {
		return nil
	}
	entities := []NativeResource{}
	for _, item := range discovered {
		entity, ok := strings.CutPrefix(item.ID, sapEntityPrefix)
		if ok && entity != "" && !strings.ContainsAny(entity, "/\\:?#%\x00\r\n") && entity != "." && entity != ".." &&
			item.Metadata["readOnly"] == true && item.Metadata["sourceType"] == "sap-odata" && item.Metadata["nativeIdentity"] == entity {
			entities = append(entities, item)
		}
	}
	return entities
}

// Vendor nextLinks stay on this Edge. Authentication binds the cursor to the
// Source revision, concrete entity, caller and requested native page size.
func scanScope(ctx context.Context, id, revision int64, ref string, limit, maxItems int) []byte {
	subject, _ := contextx.SubjectFrom(ctx)
	var peer, grant string
	if subject.FleetTask != nil {
		peer, grant = subject.FleetTask.NodeID, subject.FleetTask.GrantID
	}
	aad, _ := json.Marshal([]any{"edge-resource-scan/1", id, revision, ref, limit, maxItems, peer, grant, subject.UserID, subject.APIKeyID, subject.MCPGrantID})
	return aad
}

type sapContinuation struct {
	Link      string `json:"link"`
	Remaining int    `json:"remaining"`
}

func (s *Service) nextScanCursor(next string, remaining int, scope []byte) (string, error) {
	if next == "" {
		return "", nil
	}
	if s.cipher == nil || s.cipher.aead == nil {
		return "", ErrCredentialKey
	}
	aead := s.cipher.aead
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	payload, _ := json.Marshal(sapContinuation{Link: next, Remaining: remaining})
	token := base64.RawURLEncoding.EncodeToString(aead.Seal(nonce, nonce, payload, scope))
	if len(token) > 4096 {
		return "", ErrVendorResponse
	}
	return token, nil
}

func (s *Service) openScanCursor(cursor string, scope []byte) (sapContinuation, error) {
	var page sapContinuation
	if s.cipher == nil || s.cipher.aead == nil {
		return page, ErrCredentialKey
	}
	if len(cursor) > 4096 {
		return page, invalid("cursor", "invalidOrStale")
	}
	aead := s.cipher.aead
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(raw) < aead.NonceSize()+aead.Overhead() {
		return page, invalid("cursor", "invalidOrStale")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], scope)
	if err != nil || json.Unmarshal(plain, &page) != nil || page.Link == "" || page.Remaining < 0 {
		return page, invalid("cursor", "invalidOrStale")
	}
	return page, nil
}

func sapNextLink(endpoint, entity, raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	base, err := url.Parse(strings.TrimRight(endpoint, "/") + "/" + url.PathEscape(entity))
	if err != nil {
		return "", ErrVendorResponse
	}
	link, err := base.Parse(raw)
	if err != nil || link.Scheme != base.Scheme || link.Host != base.Host || link.User != nil || link.Fragment != "" ||
		link.EscapedPath() != base.EscapedPath() || link.RawQuery == "" || len(link.String()) > 2940 {
		return "", ErrVendorResponse
	}
	return link.String(), nil
}

func (s *Service) EdgeResourceScan(ctx context.Context, id, revision int64, ref, cursor string, limit, maxItems, timeoutMs int, begin, end string, filters EdgePlexSAPFilters) (any, error) {
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
	if !filters.Valid(ref) {
		return nil, invalid("params", "unsupported")
	}
	if def.ConnectionRole() == ClientRole && salesforcePagedResource(def, ref) {
		if begin != "" || end != "" || filters != (EdgePlexSAPFilters{}) {
			return nil, invalid("params", "unsupported")
		}
		return s.salesforceScan(ctx, row, def, ref, cursor, limit, maxItems)
	}
	if def.ConnectionRole() == ClientRole && def.SourceType == "teamcenter" && strings.HasPrefix(ref, "vendor:teamcenter:file:") {
		if begin != "" || end != "" || filters != (EdgePlexSAPFilters{}) {
			return nil, invalid("params", "unsupported")
		}
		return s.teamcenterFilePage(ctx, row, def, ref, cursor, limit, maxItems, timeoutMs, false)
	}
	if def.ConnectionRole() == ClientRole && vendorPages(def) {
		return s.businessPage(ctx, row, def, ref, cursor, limit, maxItems, timeoutMs, begin, end, "", false)
	}
	if begin != "" || end != "" {
		return nil, invalid("params", "unsupported")
	}
	if def.ConnectionRole() == ClientRole && supportsExtendedPage(def) {
		return s.extendedPage(ctx, row, def, ref, cursor, limit, maxItems, timeoutMs, false)
	}
	if def.ConnectionRole() != ClientRole || def.Protocol != HTTP || def.SourceType != "sap-odata" {
		return nil, invalid("resourceRef", "nativeScanUnsupported")
	}
	return s.sapResourcePage(ctx, row, def, ref, cursor, limit, maxItems, timeoutMs, false, filters)
}

func supportsExtendedPage(def SourceDefinition) bool {
	return def.Protocol == OpenProtocol || def.Protocol == EdgeSQL || def.Protocol == FileRead || (def.Protocol == HTTP && def.SourceType == "pi-web-api" && vendorParameter(def, "pointWebId") != "")
}

func (s *Service) extendedPage(ctx context.Context, row repo.Source, def SourceDefinition, ref, cursor string, limit, maxItems, timeoutMs int, preview bool) (any, error) {
	if limit < 1 || limit > 100 || maxItems < 0 || maxItems > 10000 {
		return nil, invalid("params", "outOfRange")
	}
	var result any
	var err error
	switch def.Protocol {
	case OpenProtocol:
		result, err = s.openProtocolPage(ctx, row, def, ref, cursor, limit, maxItems, preview)
	case EdgeSQL:
		result, err = s.edgeSQLPage(ctx, row, def, ref, cursor, limit, maxItems, preview)
	case FileRead:
		result, err = s.fileResourcePage(ctx, row, def, ref, cursor, limit, maxItems, preview)
	default:
		result, err = s.piResourcePage(ctx, row, def, ref, cursor, limit, maxItems, timeoutMs, preview)
	}
	if err != nil {
		return nil, err
	}
	latest, err := repo.NewSourceStreamRepo(ctx).Source(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	if latest.ConfigRevision != row.ConfigRevision {
		return nil, repo.ErrConflict
	}
	return result, nil
}

func (s *Service) sapResourcePage(ctx context.Context, row repo.Source, def SourceDefinition, ref, cursor string, limit, maxItems, timeoutMs int, preview bool, filters EdgePlexSAPFilters) (any, error) {
	if limit < 1 || limit > 100 {
		return nil, invalid("limit", "outOfRange")
	}
	if maxItems < 0 || maxItems > 10000 {
		return nil, invalid("params.maxItems", "outOfRange")
	}
	entity := ""
	for _, item := range sapDiscoveredEntities(row) {
		if item.ID == ref {
			entity = strings.TrimPrefix(item.ID, sapEntityPrefix)
			break
		}
	}
	if entity == "" {
		return nil, invalid("resourceRef", "undeclaredEntity")
	}
	if !filters.Valid(ref) {
		return nil, invalid("params", "unsupported")
	}
	scope := scanScope(ctx, row.ID, row.ConfigRevision, ref, limit, maxItems)
	if filters != (EdgePlexSAPFilters{}) {
		encoded, _ := json.Marshal(filters)
		scope = append(scope, encoded...)
	}
	page := sapContinuation{Remaining: maxItems}
	if cursor != "" {
		var err error
		page, err = s.openScanCursor(cursor, scope)
		if err != nil {
			return nil, err
		}
	}
	next := page.Link
	query := map[string]string{}
	if next == "" {
		if maxItems > 0 {
			query["$top"] = strconv.Itoa(maxItems)
		}
		if entity == "Materials" {
			query["$orderby"] = "Material asc"
			if filters.Order == "materialDesc" {
				query["$orderby"] = "Material desc"
			}
			if filters.SelectFields != nil {
				query["$select"] = strings.Join(*filters.SelectFields, ",")
			}
			predicates := []string{}
			if filters.Material != "" {
				predicates = append(predicates, "Material eq '"+filters.Material+"'")
			}
			if filters.BaseUnit != "" {
				predicates = append(predicates, "BaseUnit eq '"+filters.BaseUnit+"'")
			}
			if len(predicates) > 0 {
				query["$filter"] = strings.Join(predicates, " and ")
			}
		}
	}
	options, _ := json.Marshal(map[string]any{"timeoutMs": timeoutMs, "next": next, "query": query, "headers": map[string]string{"Prefer": "odata.maxpagesize=" + strconv.Itoa(limit)}})
	raw, err := s.vendorNativeCall(ctx, row, def, streamexec.ProgramCall{Kind: "source.read", ResourceRef: ref, Options: options}, "", "")
	if err != nil {
		return nil, err
	}
	var response struct {
		Status int
		Data   struct {
			Value []json.RawMessage `json:"value"`
			Next  string            `json:"@odata.nextLink"`
		}
		Error json.RawMessage
	}
	if json.Unmarshal(raw, &response) != nil || response.Status < 200 || response.Status >= 300 || response.Data.Value == nil || len(response.Error) > 0 {
		return nil, ErrVendorResponse
	}
	if maxItems > 0 && len(response.Data.Value) > page.Remaining {
		return nil, invalid("params.maxItems", "upstreamNativePageExceedsBudget")
	}
	truncated := preview && len(response.Data.Value) > limit
	if truncated {
		response.Data.Value = response.Data.Value[:limit]
	}
	if len(response.Data.Value) > limit {
		return nil, invalid("limit", "upstreamNativePageExceedsLimit")
	}
	size := 0
	for _, item := range response.Data.Value {
		if len(item) == 0 || item[0] != '{' {
			return nil, ErrVendorResponse
		}
		size += len(item)
	}
	if size > 256*1024 {
		return nil, invalid("limit", "upstreamNativePageExceedsByteLimit")
	}
	nextLink, err := sapNextLink(def.Endpoint, entity, response.Data.Next)
	if err != nil {
		return nil, err
	}
	if next != "" && nextLink == next {
		return nil, ErrVendorResponse
	}
	latest, err := repo.NewSourceStreamRepo(ctx).Source(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	if latest.ConfigRevision != row.ConfigRevision {
		return nil, repo.ErrConflict
	}
	result := map[string]any{"resourceRef": ref, "configRevision": row.ConfigRevision, "items": response.Data.Value, "collectedAt": time.Now().UnixMilli()}
	if preview {
		result["returned"] = len(response.Data.Value)
		result["truncated"] = truncated || nextLink != ""
		return result, nil
	}
	if maxItems > 0 {
		page.Remaining -= len(response.Data.Value)
		if page.Remaining == 0 {
			nextLink = ""
		}
	}
	token, err := s.nextScanCursor(nextLink, page.Remaining, scope)
	if err != nil {
		return nil, err
	}
	result["nextCursor"], result["hasMore"] = token, nextLink != ""
	return result, nil
}
