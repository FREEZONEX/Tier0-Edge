package unsv2

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/mcpaccess"
	"backend/internal/repo"
	"github.com/google/jsonschema-go/jsonschema"
)

type ResourceOperation struct {
	MetadataAction string   `json:"metadataAction,omitempty"`
	AppAllowed     bool     `json:"appAllowed,omitempty"`
	NovaAction     string   `json:"novaAction,omitempty"`
	Name           string   `json:"name"`
	PathParams     []string `json:"pathParams,omitempty"`
	Handler        string   `json:"handler"`
	Suffix         string   `json:"suffix"`
	Request        string   `json:"request"`
	Resource       string   `json:"resource"`
	Mutation       string   `json:"mutation"`
	Permission     string   `json:"permission"`
	Summary        string   `json:"summary"`
	MCP            *bool    `json:"mcp,omitempty"`
	Destructive    bool     `json:"destructive,omitempty"`
	Runtime        bool     `json:"runtime,omitempty"`
}

var resourceOnce sync.Once
var resourceOperations map[string]ResourceOperation
var appAllowedOperations map[string]bool
var resourceSchema *jsonschema.Resolved
var resourceShapes map[string]resourceShape
var resourceSchemaError error

type resourceShape struct {
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required"`
}

func loadResourceOperations() {
	var contract struct {
		Operations []ResourceOperation `json:"operations"`
		Schemas    map[string]any      `json:"schemas"`
	}
	resourceSchemaError = json.Unmarshal([]byte(ContractJSON), &contract)
	resourceOperations = map[string]ResourceOperation{}
	appAllowedOperations = map[string]bool{}
	resourceShapes = map[string]resourceShape{}
	properties := map[string]any{}
	for _, operation := range contract.Operations {
		appAllowedOperations[operation.Name] = operation.AppAllowed
		if operation.Resource == "" {
			continue
		}
		resourceOperations[operation.Name] = operation
		shapeRaw, _ := json.Marshal(contract.Schemas[operation.Request])
		var shape resourceShape
		if err := json.Unmarshal(shapeRaw, &shape); err != nil {
			resourceSchemaError = err
			return
		}
		resourceShapes[operation.Name] = shape
		for field, schema := range shape.Properties {
			properties[operation.Name+"/"+field] = schema
		}
		properties[operation.Name] = map[string]any{"$ref": "#/$defs/" + operation.Request}
	}
	root := map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "$defs": contract.Schemas}
	raw, err := json.Marshal(root)
	if err != nil {
		resourceSchemaError = err
		return
	}
	var converted any
	if err = json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "#/components/schemas/", "#/$defs/")), &converted); err != nil {
		resourceSchemaError = err
		return
	}
	resourceNullable(converted)
	raw, _ = json.Marshal(converted)
	var schema jsonschema.Schema
	if err = json.Unmarshal(raw, &schema); err != nil {
		resourceSchemaError = err
		return
	}
	resourceSchema, resourceSchemaError = schema.Resolve(nil)
}

func resourceNullable(value any) {
	switch node := value.(type) {
	case map[string]any:
		for _, child := range node {
			resourceNullable(child)
		}
		if nullable, _ := node["nullable"].(bool); nullable {
			delete(node, "nullable")
			inner := map[string]any{}
			for key, child := range node {
				inner[key] = child
				delete(node, key)
			}
			node["anyOf"] = []any{inner, map[string]any{"type": "null"}}
		}
	case []any:
		for _, child := range node {
			resourceNullable(child)
		}
	}
}

func ResourceOperations() []ResourceOperation {
	resourceOnce.Do(loadResourceOperations)
	operations := make([]ResourceOperation, 0, len(resourceOperations))
	for _, operation := range resourceOperations {
		operations = append(operations, operation)
	}
	return operations
}

func FindResourceOperation(name string) (ResourceOperation, bool) {
	resourceOnce.Do(loadResourceOperations)
	operation, ok := resourceOperations[name]
	return operation, ok
}

// App keys are admitted only by an explicit declaration in the shared contract.
func IsAppOperationAllowed(name string) bool {
	resourceOnce.Do(loadResourceOperations)
	return appAllowedOperations[name]
}

func ResourceAllowed(ctx context.Context, operation ResourceOperation) bool {
	subject, _ := contextx.SubjectFrom(ctx)
	if subject.APIKeyUsageType == repo.APIKeyUsageAppData && !operation.AppAllowed {
		return false
	}
	if require(ctx, operation.Permission) != nil {
		return false
	}
	if operation.Mutation == "" {
		return true
	}
	if subject.AuthType == "apiKey" && subject.APIKeyPermission == "read_only" && appIdentity(ctx) == "" {
		return false
	}
	if operation.Runtime {
		return !subject.MCPRestricted || mcpaccess.Has(subject, operation.Permission)
	}
	kind := operation.Resource
	if kind == "record" || kind == "sample" {
		kind = ""
	}
	if subject.MCPRestricted && operation.MCP != nil && !*operation.MCP {
		return false
	}
	if operation.Resource == "metadata" || operation.Name == "namespaceCopy" {
		return !subject.MCPRestricted || mcpaccess.Has(subject, "uns.model.manage")
	}
	if subject.MCPRestricted && operation.Destructive && !mcpaccess.Has(subject, "uns.delete") {
		return false
	}
	return mcpaccess.AllowedMutation(subject, operation.Mutation, kind, false)
}

func validateResourceRequest(operation ResourceOperation, raw []byte) (map[string]json.RawMessage, error) {
	resourceOnce.Do(loadResourceOperations)
	if resourceSchemaError != nil {
		return nil, problem("INTERNAL_ERROR", "resource contract is unavailable")
	}
	var fields map[string]json.RawMessage
	if err := Decode(raw, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, invalid("", "JSON object required")
	}
	if operation.Name == "sourceDocumentCreate" || operation.Name == "sourceDocumentUpdate" {
		if document, exists := fields["document"]; exists {
			fields["document"] = normalizeSourceUnitID(document)
		}
		var items []map[string]json.RawMessage
		if json.Unmarshal(fields["items"], &items) == nil {
			for _, item := range items {
				if document, exists := item["document"]; exists {
					item["document"] = normalizeSourceUnitID(document)
				}
			}
			fields["items"], _ = json.Marshal(items)
		}
	}
	shape := resourceShapes[operation.Name]
	for _, field := range shape.Required {
		if _, exists := fields[field]; !exists {
			return nil, invalid(field, "required field is missing")
		}
	}
	for field, rawField := range fields {
		if _, allowed := shape.Properties[field]; !allowed {
			return nil, invalid(field, "field is not allowed for this operation")
		}
		var value any
		if err := json.Unmarshal(rawField, &value); err != nil {
			return nil, invalid(field, "invalid JSON field")
		}
		if err := resourceSchema.Validate(map[string]any{operation.Name + "/" + field: value}); err != nil {
			return nil, invalid(field, "field does not match the operation schema; inspect api_contract_get")
		}
	}
	var value any
	raw, _ = json.Marshal(fields)
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, invalid("", "JSON object required")
	}
	if err := resourceSchema.Validate(map[string]any{operation.Name: value}); err != nil {
		return nil, invalid("request", "request does not match "+operation.Request+"; inspect api_contract_get for required fields, types and allowed properties")
	}
	return fields, nil
}

// Only Modbus unitId has this legacy representation. Keep object IDs and all
// other fields intact, and leave invalid values for the normal schema validator.
func normalizeSourceUnitID(raw json.RawMessage) json.RawMessage {
	var document, modbus map[string]json.RawMessage
	if json.Unmarshal(raw, &document) != nil || json.Unmarshal(document["modbus"], &modbus) != nil {
		return raw
	}
	var text string
	if json.Unmarshal(modbus["unitId"], &text) != nil || text == "" || strings.TrimLeft(text, "0123456789") != "" {
		return raw
	}
	unitID, err := strconv.ParseUint(text, 10, 8)
	if err != nil {
		return raw
	}
	modbus["unitId"] = json.RawMessage(strconv.FormatUint(unitID, 10))
	document["modbus"], _ = json.Marshal(modbus)
	normalized, _ := json.Marshal(document)
	return normalized
}

func resourceString(fields map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(fields[key], &value)
	return value
}

func (s *Service) resourceExecute(ctx context.Context, operation ResourceOperation, raw []byte) (any, error) {
	if operation.Name == "appStreamAccessList" {
		return s.executeAppStreamAccess(ctx, operation, raw)
	}
	if operation.Name == "streamRequestGrantGet" || operation.Name == "streamRequestGrantSet" {
		return s.executeAppRequestGrant(ctx, operation, raw)
	}
	if operation.Name == "appStreamPolicyGet" || operation.Name == "appStreamPolicyUpdate" {
		return s.executeAppStreamPolicy(ctx, operation, raw)
	}
	if operation.Name == "metadataPurge" {
		return s.purgeCollections(ctx, raw)
	}
	if operation.MetadataAction != "" {
		return s.metadataOperation(ctx, operation.Name, raw)
	}
	if contains([]string{"namespaceCopy", "sourceReplace", "streamReplace", "appReplace"}, operation.Name) {
		return s.metadataSpecializedWrite(ctx, operation, raw)
	}
	if !ResourceAllowed(ctx, operation) {
		return nil, problem("FORBIDDEN", "resource operation permission is required")
	}
	if operation.Name == "recordQueryBatch" {
		return s.recordQueryBatch(ctx, raw)
	}
	if operation.NovaAction != "" {
		if _, err := validateResourceRequest(operation, raw); err != nil {
			return nil, err
		}
		return s.novaRecordOperation(ctx, operation.NovaAction, raw)
	}
	raw, legacySingle, legacyKey, err := normalizeResourceWrite(operation, raw)
	if err != nil {
		return nil, err
	}
	fields, err := validateResourceRequest(operation, raw)
	if err != nil {
		return nil, err
	}
	if legacyKey != "" {
		fields["idempotencyKey"], _ = json.Marshal(legacyKey)
	}
	if arrayResource(operation) {
		return s.arrayResourceWrite(ctx, operation, fields, legacySingle)
	}
	if operation.Runtime {
		return s.runtimeResource(ctx, operation, fields)
	}
	if operation.Name == "appSessionInspect" {
		return s.appSessionInspect(ctx, resourceString(fields, "appId"), resourceString(fields, "token"))
	}
	if operation.Mutation == "" {
		return s.resourceQuery(ctx, operation, fields)
	}
	batch := Batch{IdempotencyKey: resourceString(fields, "idempotencyKey")}
	_ = json.Unmarshal(fields["dryRun"], &batch.DryRun)
	change, err := resourceChange(operation, fields)
	if err != nil {
		return nil, err
	}
	batch.Items = []Change{change}
	out, err := s.Apply(ctx, batch)
	if err != nil {
		return nil, err
	}
	response := map[string]any{"status": out.Status, "replayed": out.Replayed}
	if out.JobID != "" {
		response["jobId"] = out.JobID
	}
	if len(out.Results) == 1 {
		response["result"] = out.Results[0]
	}
	return response, nil
}

func (s *Service) resourceQuery(ctx context.Context, operation ResourceOperation, fields map[string]json.RawMessage) (any, error) {
	if operation.Name == "sourceRuntimeGet" || operation.Name == "streamReleasesList" || operation.Name == "streamReleasesGet" {
		return s.sharedRuntimeQuery(ctx, operation.Name, fields)
	}
	query := Query{QueryID: operation.Name}
	queryOperation := "metadata"
	if archived, ok := fields["archived"]; ok {
		fields["includeArchived"] = archived
		delete(fields, "archived")
	}
	id := resourceString(fields, "id")
	bindingID := resourceString(fields, "bindingId")
	delete(fields, "id")
	delete(fields, "bindingId")
	raw, _ := json.Marshal(fields)
	if err := Decode(raw, &query); err != nil {
		return nil, err
	}
	query.QueryID = operation.Name
	query.Kinds = []string{operation.Resource}
	if id != "" {
		query.IDs = []string{id}
		query.Size = 1
	}
	if operation.Resource == "collection" && id != "" {
		queryOperation = "collections"
		query.CollectionID, query.Mode, query.Include = id, "detail", []string{"definition"}
	}
	if operation.Resource == "record" {
		queryOperation = "records"
		query.IDs = nil
		if id != "" {
			query.RecordIDs = []string{id}
		}
	}
	if operation.Resource == "sample" {
		queryOperation = "samples"
		query.Mode = "history"
		if operation.Name == "sampleLatest" {
			query.Mode = "latest"
		}
	}
	value, err := s.Query(ctx, queryOperation, Queries{Queries: []Query{query}})
	if err != nil {
		return nil, err
	}
	result := value.(map[string]any)["results"].([]map[string]any)[0]
	delete(result, "queryId")
	if queryOperation == "collections" {
		return result["definition"], nil
	}
	if id == "" {
		return result, nil
	}
	encoded, _ := json.Marshal(result["list"])
	var rows []map[string]any
	if err := decodeNumberJSON(encoded, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, problem("NOT_FOUND", "resource not found")
	}
	row := rows[0]
	if strings.Contains(operation.Suffix, "/bindings/") {
		data, _ := row["data"].(map[string]any)
		config, _ := data["config"].(map[string]any)
		bindings, _ := config["streams"].(map[string]any)
		if bindingID != "" {
			binding, ok := bindings[bindingID]
			if !ok {
				return nil, problem("NOT_FOUND", "binding not found")
			}
			return map[string]any{"sourceId": id, "revision": row["revision"], "bindingId": bindingID, "binding": binding}, nil
		}
		if bindings == nil {
			bindings = map[string]any{}
		}
		return map[string]any{"sourceId": id, "revision": row["revision"], "bindings": bindings}, nil
	}
	return row, nil
}

type resourcePatch struct {
	Operation string                     `json:"operation"`
	Fields    map[string]json.RawMessage `json:"fields"`
}

type bindingLifecycleKey struct{}

func mergeResourcePatch(ctx context.Context, change Change) (Change, error) {
	var patch resourcePatch
	if err := Decode(change.Data, &patch); err != nil {
		return change, err
	}
	operation, ok := FindResourceOperation(patch.Operation)
	if !ok || operation.Mutation != "meta.resourcePatch" || operation.Resource != change.Kind {
		return change, invalid("data.operation", "resource patch operation required")
	}
	if !ResourceAllowed(ctx, operation) {
		return change, problem("FORBIDDEN", "resource patch permission is required")
	}
	request := map[string]any{"id": change.ID.ID, "dryRun": true}
	if change.ExpectedRevision > 0 {
		request["expectedRevision"] = change.ExpectedRevision
	}
	for key, value := range patch.Fields {
		if contains([]string{"id", "expectedRevision", "idempotencyKey", "dryRun"}, key) {
			return change, invalid("data.fields", "control fields are not patchable")
		}
		request[key] = value
	}
	delete(request, "dryRun")
	raw, _ := json.Marshal(map[string]any{"dryRun": true, "items": []any{request}})
	if _, err := validateResourceRequest(operation, raw); err != nil {
		return change, err
	}
	row, err := meta(ctx, change.Kind, change.ID.ID, false)
	if err != nil {
		return change, err
	}
	if row.Revision != change.ExpectedRevision {
		return change, problem("VERSION_CONFLICT", "resource revision changed")
	}
	var data map[string]json.RawMessage
	if err = json.Unmarshal(row.Data, &data); err != nil {
		return change, err
	}
	for _, key := range []string{"credentials", "nodeType", "releases", "configRevision", "desiredState"} {
		delete(data, key)
	}
	if change.Kind == "source" {
		var config map[string]json.RawMessage
		if err = json.Unmarshal(data["config"], &config); err != nil || config == nil {
			return change, invalid("id", "shared Source configuration required")
		}
		if connection, ok := patch.Fields["connection"]; ok {
			var previous, next map[string]json.RawMessage
			_ = json.Unmarshal(config["connection"], &previous)
			_ = json.Unmarshal(connection, &next)
			if credential, exists := previous["credentialRef"]; exists {
				if _, supplied := next["credentialRef"]; !supplied {
					next["credentialRef"] = credential
				}
			}
			config["connection"], _ = json.Marshal(next)
			delete(patch.Fields, "connection")
		}
		if strings.Contains(operation.Suffix, "/bindings/") {
			bindingID := resourceString(patch.Fields, "bindingId")
			if !unsmeta.ValidID(bindingID) {
				return change, invalid("bindingId", "valid binding identity required")
			}
			bindings := map[string]json.RawMessage{}
			_ = json.Unmarshal(config["streams"], &bindings)
			if bindings == nil {
				bindings = map[string]json.RawMessage{}
			}
			_, exists := bindings[bindingID]
			creation := strings.HasSuffix(operation.Suffix, "/create")
			if creation && exists {
				return change, problem("DUPLICATE", "binding already exists")
			}
			if !creation && !exists {
				return change, problem("NOT_FOUND", "binding not found")
			}
			if strings.HasSuffix(operation.Suffix, "/delete") {
				delete(bindings, bindingID)
			} else if operation.Name == "sourceBindingsStateUpdate" || operation.Name == "sourceBindingsReleaseUpdate" {
				var binding Binding
				if err = json.Unmarshal(bindings[bindingID], &binding); err != nil {
					return change, err
				}
				if operation.Name == "sourceBindingsStateUpdate" {
					if err = json.Unmarshal(patch.Fields["enabled"], &binding.Enabled); err != nil {
						return change, err
					}
				} else {
					_ = json.Unmarshal(patch.Fields["streamId"], &binding.StreamID)
					_ = json.Unmarshal(patch.Fields["streamRelease"], &binding.StreamRelease)
					delete(patch.Fields, "streamId")
					delete(patch.Fields, "streamRelease")
				}
				if binding.Enabled {
					if err = require(ctx, "uns.write", "uns.stream.manage"); err != nil {
						return change, err
					}
				}
				bindings[bindingID], _ = json.Marshal(binding)
				delete(patch.Fields, "enabled")
			} else {
				var binding Binding
				if err = Decode(patch.Fields["binding"], &binding); err != nil {
					return change, err
				}
				if binding.Enabled {
					return change, invalid("binding.enabled", "save disabled, then enable with sourceBindingsStateUpdate")
				}
				bindings[bindingID] = patch.Fields["binding"]
			}
			config["streams"], _ = json.Marshal(bindings)
			delete(patch.Fields, "bindingId")
			delete(patch.Fields, "binding")
		}
		data["config"], _ = json.Marshal(config)
	}
	if operation.Name == "streamReleasesRestore" {
		var release int
		_ = json.Unmarshal(patch.Fields["release"], &release)
		var stream StreamData
		_ = json.Unmarshal(row.Data, &stream)
		selected, exists := stream.Releases[strconv.Itoa(release)]
		if !exists {
			return change, problem("NOT_FOUND", "release not found")
		}
		patch.Fields["draft"], _ = json.Marshal(selected.Definition)
		delete(patch.Fields, "release")
	}
	if operation.Name == "topicViewUpdate" {
		delete(data, "collectionId")
		delete(data, "fields")
	}
	for key, value := range patch.Fields {
		data[key] = value
	}
	change.Action = "meta.replace"
	change.Data, err = json.Marshal(data)
	return change, err
}

func resourceChange(operation ResourceOperation, fields map[string]json.RawMessage) (Change, error) {
	change := Change{OpID: operation.Name, Action: operation.Mutation, Kind: operation.Resource, ID: Identity{ID: resourceString(fields, "id")}}
	_ = json.Unmarshal(fields["expectedRevision"], &change.ExpectedRevision)
	if operation.Resource == "record" || operation.Resource == "sample" {
		change.Kind = ""
		change.CollectionID.ID = resourceString(fields, "collectionId")
		change.RecordID.ID = resourceString(fields, "recordId")
		change.Data = fields["data"]
		change.Filter = fields["filter"]
		change.Selection = resourceString(fields, "selection")
		change.ReturnDocument = resourceString(fields, "returnDocument")
		_ = json.Unmarshal(fields["sort"], &change.Sort)
		change.Quality = resourceString(fields, "quality")
		_ = json.Unmarshal(fields["time"], &change.Time)
		_ = json.Unmarshal(fields["operations"], &change.Operations)
		_ = json.Unmarshal(fields["conditions"], &change.Conditions)
	} else if operation.Mutation == "stream.publish" {
		change.StreamID = change.ID
	} else {
		for _, key := range []string{"id", "expectedRevision", "idempotencyKey", "dryRun"} {
			delete(fields, key)
		}
		if operation.Mutation == "meta.resourcePatch" {
			if len(fields) == 0 {
				return change, invalid("request", "at least one update field is required")
			}
			change.Data, _ = json.Marshal(resourcePatch{Operation: operation.Name, Fields: fields})
		} else if operation.Mutation == "meta.create" {
			fields["schemaVersion"] = json.RawMessage(`1`)
			if change.Kind == "source" {
				fields["config"], _ = json.Marshal(map[string]any{"connection": fields["connection"], "streams": map[string]any{}})
				delete(fields, "connection")
			}
			if change.Kind == "app" {
				for _, key := range []string{"roles", "members", "collections"} {
					fields[key] = json.RawMessage(`[]`)
				}
				fields["navigation"] = json.RawMessage(`{"topicIds":[]}`)
			}
			change.Data, _ = json.Marshal(fields)
		}
	}
	return change, nil
}
