package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
)

const teamcenterFilePrefix = "vendor:teamcenter:file:"

// The Edge administrator approves each complete relation chain. Callers select
// only its resource reference; they cannot supply a UID, path or download ticket.
type TeamcenterFileSelection struct {
	ID          string `json:"id"`
	QueryUID    string `json:"queryUid"`
	ItemID      string `json:"itemId"`
	ItemUID     string `json:"itemUid"`
	RevisionUID string `json:"revisionUid"`
	DatasetUID  string `json:"datasetUid"`
	FileUID     string `json:"fileUid"`
}

type teamcenterFileIdentity struct {
	ItemUID          string `json:"itemUid"`
	RevisionUID      string `json:"revisionUid"`
	Revision         string `json:"revision"`
	DatasetUID       string `json:"datasetUid"`
	FileUID          string `json:"fileUid"`
	FileName         string `json:"fileName"`
	Size             int64  `json:"fileSize"`
	SourceDocumentID string `json:"sourceDocumentId"`
}

type teamcenterFilePosition struct {
	Identity teamcenterFileIdentity `json:"identity"`
	SHA256   string                 `json:"sha256"`
	Offset   int                    `json:"offset"`
}

func teamcenterFileSelections(def SourceDefinition) ([]TeamcenterFileSelection, error) {
	if def.HTTP == nil || def.HTTP.Parameters["fileSelections"] == nil {
		return nil, nil
	}
	raw, err := json.Marshal(def.HTTP.Parameters["fileSelections"])
	if err != nil || len(raw) > 48*1024 {
		return nil, invalid("http.parameters.fileSelections", "invalid")
	}
	var selections []TeamcenterFileSelection
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&selections) != nil || len(selections) == 0 || len(selections) > 32 {
		return nil, invalid("http.parameters.fileSelections", "invalid")
	}
	u, err := url.Parse(def.Endpoint)
	if err != nil || u.Scheme != "https" || def.Protocol != HTTP || def.SourceType != "teamcenter" || def.ConnectionRole() != ClientRole {
		return nil, invalid("http.parameters.fileSelections", "httpsTeamcenterSourceRequired")
	}
	seen := map[string]bool{}
	for _, selection := range selections {
		if !fileResourceID.MatchString(selection.ID) || seen[selection.ID] || !tcItemID.MatchString(selection.ItemID) {
			return nil, invalid("http.parameters.fileSelections", "invalidSelection")
		}
		seen[selection.ID] = true
		for _, uid := range []string{selection.QueryUID, selection.ItemUID, selection.RevisionUID, selection.DatasetUID, selection.FileUID} {
			if !safeObjectUID(uid) || len(uid) > 128 {
				return nil, invalid("http.parameters.fileSelections", "invalidUID")
			}
		}
	}
	return selections, nil
}

func validateTeamcenterFiles(def SourceDefinition) error {
	_, err := teamcenterFileSelections(def)
	return err
}

func teamcenterFileResources(def SourceDefinition, row repo.Source) []edgeResource {
	selections, err := teamcenterFileSelections(def)
	if err != nil {
		return nil
	}
	queries := map[string]bool{}
	for _, resource := range businessDirectory(def, row) {
		queries[resource.ResourceRef] = true
	}
	resources := []edgeResource{}
	chunkFields := map[string]any{}
	for _, field := range []string{"businessKey", "itemUid", "revisionUid", "revision", "datasetUid", "fileUid", "fileName", "sourceDocumentId", "fileSha256"} {
		chunkFields[field] = map[string]string{"type": "string"}
	}
	for _, field := range []string{"fileSize", "chunkIndex", "chunkCount"} {
		chunkFields[field] = map[string]string{"type": "integer"}
	}
	chunkFields["contentBase64"] = map[string]string{"type": "string", "contentEncoding": "base64"}
	for _, selection := range selections {
		if !queries[tcQueryPrefix+selection.QueryUID] {
			continue
		}
		resources = append(resources, edgeResource{
			ResourceRef: teamcenterFilePrefix + selection.ID, Name: selection.ID,
			Modes: []string{"read", "scan"}, Permission: "uns.source",
			RequiredPermissions: []string{"uns.source", "uns.read"}, ConfigRevision: row.ConfigRevision,
			InputSchema: resourceObject(map[string]any{
				"limit":    map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				"maxItems": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000},
			}),
			OutputSchema: resourceObject(map[string]any{
				"items":      map[string]any{"type": "array", "items": resourceObject(chunkFields)},
				"nextCursor": map[string]string{"type": "string"}, "hasMore": map[string]string{"type": "boolean"},
			}),
			Metadata: map[string]any{
				"identityField": "businessKey", "fileUid": selection.FileUID, "revisionUid": selection.RevisionUID,
				"versionField": "fileSha256", "contentEncoding": "base64", "chunkBytes": 1024, "maxFileBytes": maxSourceFileBytes,
			},
		})
	}
	return resources
}

type teamcenterProperty struct {
	Values []string `json:"dbValues"`
	Nulls  []bool   `json:"isNulls"`
}

type teamcenterModel struct {
	UID       string                        `json:"uid"`
	ClassName string                        `json:"className"`
	Props     map[string]teamcenterProperty `json:"props"`
}

// A null or unreturned property never proves an empty relation.
func (object teamcenterModel) values(name string) ([]string, error) {
	property, ok := object.Props[name]
	if !ok || len(property.Values) != len(property.Nulls) || slices.Contains(property.Nulls, true) {
		return nil, invalid("resourceRef", "teamcenterPropertyMissing")
	}
	return property.Values, nil
}

func (object teamcenterModel) scalar(name string) (string, error) {
	values, err := object.values(name)
	if err != nil || len(values) != 1 {
		return "", invalid("resourceRef", "teamcenterPropertyMissing")
	}
	return values[0], nil
}

func teamcenterFilePolicy() map[string]any {
	attributes := map[string][]string{
		"Item":         {"item_id", "revision_list"},
		"ItemRevision": {"item_revision_id", "items_tag", "IMAN_specification"},
		"Dataset":      {"ref_list"},
		"ImanFile":     {"original_file_name", "file_size", "plant0_source_document_id"},
	}
	types := make([]map[string]any, 0, len(attributes))
	for _, name := range []string{"Item", "ItemRevision", "Dataset", "ImanFile"} {
		properties := make([]map[string]string, 0, len(attributes[name]))
		for _, attribute := range attributes[name] {
			properties = append(properties, map[string]string{"name": attribute})
		}
		types = append(types, map[string]any{"name": name, "properties": properties})
	}
	return map[string]any{"types": types}
}

// Preserve Teamcenter's partial-failure distinction without exporting vendor
// messages, session material or incomplete object graphs as successful results.
func (s *Service) teamcenterFileCall(ctx context.Context, row repo.Source, def SourceDefinition, operation string, options map[string]any, target any) error {
	encoded, err := json.Marshal(options)
	if err != nil {
		return err
	}
	raw, err := s.vendorNativeCall(ctx, row, def, streamexec.ProgramCall{Kind: "source.read", ResourceRef: "vendor:teamcenter:" + operation, Options: encoded}, "", "")
	if err != nil {
		return err
	}
	var response struct {
		Status int
		Data   json.RawMessage
		Error  *struct{ Code string }
	}
	if json.Unmarshal(raw, &response) != nil {
		return ErrVendorResponse
	}
	if response.Status == http.StatusTooManyRequests {
		return invalid("source", "upstreamRateLimited")
	}
	if response.Status >= 500 {
		return ErrUnavailable
	}
	if response.Error != nil && response.Error.Code == "vendorPartialFailure" {
		return invalid("resourceRef", "teamcenterPartialErrors")
	}
	if response.Status != http.StatusOK || response.Error != nil || json.Unmarshal(response.Data, target) != nil {
		return ErrVendorResponse
	}
	return nil
}

func (s *Service) resolveTeamcenterFile(ctx context.Context, row repo.Source, def SourceDefinition, selection TeamcenterFileSelection, timeout int) (teamcenterFileIdentity, error) {
	var identity teamcenterFileIdentity
	// Exact Item ID and UID matching prevent a broad or truncated query from
	// silently authorizing an attachment on another object.
	var query struct {
		ArrayOfResults []struct {
			ObjectUIDS []string `json:"objectUIDS"`
		}
	}
	queryOptions := map[string]any{"timeoutMs": timeout, "data": map[string]any{
		"header": map[string]any{"state": map[string]any{}, "policy": map[string]any{}},
		"body": map[string]any{"queries": []any{map[string]any{
			"query": selection.QueryUID, "clientId": "edge-fms", "entries": []string{"Item ID"}, "values": []string{selection.ItemID}, "maxNumToReturn": 2,
		}}},
	}}
	if err := s.teamcenterFileCall(ctx, row, def, "query", queryOptions, &query); err != nil {
		return identity, err
	}
	if len(query.ArrayOfResults) != 1 || len(query.ArrayOfResults[0].ObjectUIDS) != 1 || query.ArrayOfResults[0].ObjectUIDS[0] != selection.ItemUID {
		return identity, invalid("resourceRef", "teamcenterQueryScopeMismatch")
	}
	var response struct {
		ModelObjects map[string]teamcenterModel
		ServiceData  struct{ ModelObjects map[string]teamcenterModel }
	}
	uids := []string{selection.ItemUID, selection.RevisionUID, selection.DatasetUID, selection.FileUID}
	options := map[string]any{"timeoutMs": timeout, "data": map[string]any{
		"header": map[string]any{"state": map[string]any{}, "policy": teamcenterFilePolicy()},
		"body":   map[string]any{"uids": uids},
	}}
	if err := s.teamcenterFileCall(ctx, row, def, "objects", options, &response); err != nil {
		return identity, err
	}
	models := response.ModelObjects
	if models == nil {
		models = response.ServiceData.ModelObjects
	}
	classes := []string{"Item", "ItemRevision", "Dataset", "ImanFile"}
	for index, uid := range uids {
		if models[uid].UID != uid || models[uid].ClassName != classes[index] {
			return identity, invalid("resourceRef", "teamcenterRelationMismatch")
		}
	}
	item, revision, dataset, file := models[uids[0]], models[uids[1]], models[uids[2]], models[uids[3]]
	itemID, err := item.scalar("item_id")
	if err != nil || itemID != selection.ItemID {
		return identity, invalid("resourceRef", "teamcenterRelationMismatch")
	}
	for _, relation := range []struct {
		model              teamcenterModel
		property, expected string
	}{
		{item, "revision_list", selection.RevisionUID},
		{revision, "items_tag", selection.ItemUID},
		{revision, "IMAN_specification", selection.DatasetUID},
		{dataset, "ref_list", selection.FileUID},
	} {
		values, err := relation.model.values(relation.property)
		if err != nil || !slices.Contains(values, relation.expected) {
			return identity, invalid("resourceRef", "teamcenterRelationMismatch")
		}
	}
	identity = teamcenterFileIdentity{ItemUID: selection.ItemUID, RevisionUID: selection.RevisionUID, DatasetUID: selection.DatasetUID, FileUID: selection.FileUID}
	if identity.Revision, err = revision.scalar("item_revision_id"); err != nil || identity.Revision == "" || len(identity.Revision) > 128 {
		return identity, ErrVendorResponse
	}
	if identity.FileName, err = file.scalar("original_file_name"); err != nil || identity.FileName == "" || len(identity.FileName) > 255 || strings.ContainsAny(identity.FileName, "/\\\x00\r\n") || identity.FileName == "." || identity.FileName == ".." {
		return identity, ErrVendorResponse
	}
	size, err := file.scalar("file_size")
	if err != nil {
		return identity, err
	}
	if identity.Size, err = strconv.ParseInt(size, 10, 64); err != nil || identity.Size < 0 || identity.Size > maxSourceFileBytes {
		return identity, invalid("file", "sizeOutOfRange")
	}
	if identity.SourceDocumentID, err = file.scalar("plant0_source_document_id"); err != nil || len(identity.SourceDocumentID) > 512 {
		return identity, ErrVendorResponse
	}
	return identity, nil
}

func (identity teamcenterFileIdentity) cacheKey() string {
	raw, _ := json.Marshal(identity)
	sum := sha256.Sum256(raw)
	return "teamcenter:" + hex.EncodeToString(sum[:])
}

func (s *Service) teamcenterFilePage(ctx context.Context, row repo.Source, def SourceDefinition, ref, cursor string, limit, maxItems, timeout int, preview bool) (any, error) {
	if limit < 1 || limit > 100 || maxItems < 0 || maxItems > 10000 {
		return nil, invalid("params", "outOfRange")
	}
	allowed := false
	for _, resource := range teamcenterFileResources(def, row) {
		allowed = allowed || resource.ResourceRef == ref
	}
	if !allowed {
		return nil, invalid("resourceRef", "undeclared")
	}
	selections, _ := teamcenterFileSelections(def)
	var selection TeamcenterFileSelection
	for _, candidate := range selections {
		if teamcenterFilePrefix+candidate.ID == ref {
			selection = candidate
			break
		}
	}
	scope := scanScope(ctx, row.ID, row.ConfigRevision, ref, limit, maxItems)
	var position teamcenterFilePosition
	remaining := maxItems
	if cursor != "" {
		page, err := s.openScanCursor(cursor, scope)
		if err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(page.Link), &position) != nil || len(position.SHA256) != 64 || position.Offset < 0 {
			return nil, invalid("cursor", "invalidOrStale")
		}
		remaining = page.Remaining
	}
	snapshot, release, err := acquireFileSnapshot(ctx, fileSnapshotKey{row.ID, row.ConfigRevision, "teamcenter:" + selection.ID})
	if err != nil {
		return nil, err
	}
	defer release()
	identity := position.Identity
	cached := cursor != "" && snapshot.file != nil && snapshot.version == position.SHA256 && snapshot.checked[identity.cacheKey()]
	if !cached {
		identity, err = s.resolveTeamcenterFile(ctx, row, def, selection, timeout)
		if err != nil {
			return nil, err
		}
		if cursor != "" && identity != position.Identity {
			return nil, invalid("cursor", "fileVersionChanged")
		}
		if err := s.prepareTeamcenterFile(ctx, row, def, selection, identity, snapshot, position.SHA256, timeout); err != nil {
			return nil, err
		}
	}
	count := limit
	if maxItems > 0 {
		count = min(count, remaining)
	}
	items, err := fileSnapshotChunks(snapshot.file, snapshot.version, position.Offset, count+1)
	if err != nil {
		return nil, err
	}
	more := len(items) > count
	if more {
		items = items[:count]
	}
	prefix := strconv.FormatInt(row.ID, 10) + "/" + selection.ID + "/" + identity.RevisionUID + "/" + identity.FileUID + "/"
	for _, item := range items {
		item["businessKey"] = prefix + item["businessKey"].(string)
		item["itemUid"], item["revisionUid"], item["revision"] = identity.ItemUID, identity.RevisionUID, identity.Revision
		item["datasetUid"], item["fileUid"], item["fileName"] = identity.DatasetUID, identity.FileUID, identity.FileName
		item["sourceDocumentId"], item["fileSha256"] = identity.SourceDocumentID, snapshot.version
	}
	encoded, err := json.Marshal(items)
	if err != nil || len(encoded) > 256*1024 {
		return nil, invalid("limit", "upstreamNativePageExceedsByteLimit")
	}
	current, err := repo.NewSourceStreamRepo(ctx).Source(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	if current.ConfigRevision != row.ConfigRevision || current.DesiredState != "enabled" {
		return nil, repo.ErrConflict
	}
	reply := map[string]any{"resourceRef": ref, "configRevision": row.ConfigRevision, "collectedAt": time.Now().UnixMilli(), "items": items}
	if preview {
		reply["returned"], reply["truncated"] = len(items), more
		return reply, nil
	}
	if maxItems > 0 {
		remaining -= len(items)
		if remaining == 0 {
			reply["truncated"] = more
			more = false
		}
	}
	token := ""
	if more {
		position = teamcenterFilePosition{Identity: identity, SHA256: snapshot.version, Offset: position.Offset + len(items)}
		raw, _ := json.Marshal(position)
		token, err = s.nextScanCursor(string(raw), remaining, scope)
		if err != nil {
			return nil, err
		}
	}
	reply["nextCursor"], reply["hasMore"] = token, more
	return reply, nil
}
