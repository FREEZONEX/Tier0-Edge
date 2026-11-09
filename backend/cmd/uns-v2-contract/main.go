// Emit the domain configuration schemas consumed by the API generator.
package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"

	"backend/internal/domain/common"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
)

type generator struct{ schemas map[string]any }

func (g *generator) schema(t reflect.Type) any {
	if t.Kind() == reflect.Pointer {
		return g.schema(t.Elem())
	}
	if t == reflect.TypeOf(json.RawMessage{}) {
		return map[string]any{}
	}
	if t == reflect.TypeOf(unsv2.Identity{}) {
		return map[string]any{"$ref": "#/components/schemas/UnsV2Identity"}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Uint, reflect.Uint8:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": g.schema(t.Elem())}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": g.schema(t.Elem())}
	}
	if t.Kind() != reflect.Struct {
		return map[string]any{}
	}
	name := t.Name()
	if name != "" {
		if _, exists := g.schemas[name]; exists {
			return map[string]any{"$ref": "#/components/schemas/" + name}
		}
		g.schemas[name] = map[string]any{}
	}
	properties := map[string]any{}
	required := []string{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			g.schema(f.Type)
			embedded := g.schemas[f.Type.Name()].(map[string]any)
			for key, value := range embedded["properties"].(map[string]any) {
				properties[key] = value
			}
			required = append(required, embedded["required"].([]string)...)
			continue
		}
		tag := strings.Split(f.Tag.Get("json"), ",")
		if tag[0] == "-" || tag[0] == "" {
			continue
		}
		properties[tag[0]] = g.schema(f.Type)
		if len(tag) == 1 {
			required = append(required, tag[0])
		}
	}
	definition := map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
	if name == "" {
		return definition
	}
	g.schemas[name] = definition
	return map[string]any{"$ref": "#/components/schemas/" + name}
}
func (g *generator) restrict(name string, keep []string, required []string) {
	schema := g.schemas[name].(map[string]any)
	props := schema["properties"].(map[string]any)
	for key := range props {
		found := false
		for _, k := range keep {
			if key == k {
				found = true
			}
		}
		if !found {
			delete(props, key)
		}
	}
	schema["required"] = required
}
func main() {
	g := &generator{schemas: map[string]any{}}
	g.schema(reflect.TypeOf(unsv2.Acquisition{}))
	// Namespace the colleague's executable program schemas separately from
	// shared StreamDefinition; both are discoverable through the same catalog.
	g.schemas["RuntimeStreamDefinition"] = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"name", "script"}, "properties": map[string]any{
		"name": map[string]any{"type": "string", "minLength": 1, "maxLength": common.NameMaxLen}, "description": map[string]any{"type": "string", "maxLength": common.DescMaxLen}, "script": map[string]any{"type": "string", "minLength": 1, "maxLength": sourcestream.MaxScriptBytes, "writeOnly": true, "description": "Complete module.exports=api=>{...} program; uses api.source.use/api.uns.use/api.stream registration, not a transform function body"}, "timeoutMs": map[string]any{"type": "integer", "minimum": 1, "maximum": 60000, "default": 1000}, "extensions": map[string]any{"type": "object", "additionalProperties": true},
	}}
	g.schemas["RuntimeProgramTestInput"] = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"data"}, "description": "Isolated preview envelope; data is passed to the selected entry. sourceSamples maps Source alias to operation key to JSON result. topicSamples maps UNS alias to JSON value. checkpointSample is at most 16 KiB of JSON.", "properties": map[string]any{
		"data": map[string]any{}, "sourceSamples": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "object", "additionalProperties": true}}, "topicSamples": map[string]any{"type": "object", "additionalProperties": true}, "checkpointSample": map[string]any{}, "previewWrites": map[string]any{"type": "boolean", "description": "Compare proposed UNS writes with current data; requires uns.read and does not commit"},
	}}
	for _, value := range []any{unsmeta.Document{}, unsv2.StreamData{}, unsv2.SourceData{}, unsv2.AppData{}, unsv2.Result{}, unsv2.Outcome{}, unsv2.Fault{}, unsv2.AppContext{}, unsv2.AppSessionIdentity{}, unsv2.AppKeyCreated{}, unsv2.AppKeyRevoked{}, unsv2.TransactionValidation{}, unsv2.TransactionStatusResult{}, sourcestream.SourceDefinition{}} {
		g.schema(reflect.TypeOf(value))
	}
	g.schema(reflect.TypeOf(sourcestream.TeamcenterFileSelection{}))
	g.schema(reflect.TypeOf(sourcestream.SalesforceQueryDeclaration{}))
	outcome := g.schemas["Outcome"].(map[string]any)["properties"].(map[string]any)
	outcome["status"] = map[string]any{"type": "string", "enum": []string{"committed", "validated", "accepted", "partiallyCommitted", "failed", "unknown", "validationFailed"}, "description": "Partial array writes expose summary and each result.error; accepted requires jobs/query. A failed partial job may contain committed items."}
	outcome["summary"].(map[string]any)["description"] = "Present for allowPartial results; total equals succeeded + failed + notExecuted + unknown. Preview successes are validated, never committed."
	g.schemas["Result"].(map[string]any)["properties"].(map[string]any)["status"].(map[string]any)["description"] = "Partial arrays use committed/validated/failed/notExecuted/unknown; atomic writes retain their action-specific status. Match opId=item-N to input order and inspect error for failures."
	g.restrict("SourceData", []string{"schemaVersion", "name", "description", "adapter", "config"}, []string{"schemaVersion", "name", "adapter", "config"})
	g.restrict("StreamData", []string{"schemaVersion", "name", "description", "executionMode", "draft"}, []string{"schemaVersion", "name", "draft"})
	g.schemas["AppData"].(map[string]any)["required"] = []string{"schemaVersion", "name", "collections", "navigation"}
	g.schemas["SourceData"].(map[string]any)["properties"].(map[string]any)["config"].(map[string]any)["required"] = []string{"connection"}
	g.restrict("Mapping", []string{"recordId", "recordIdPath", "recordPath", "sampleEnabled", "samplePath", "timePath", "qualityPath"}, []string{})
	g.restrict("SourceDefinition", []string{"name", "description", "protocol", "sourceType", "role", "endpoint", "credentialRef", "runtimeLocation", "http", "mqtt", "opcua", "modbus", "file", "sql", "openProtocol", "extensions"}, []string{"endpoint"})
	source := g.schemas["SourceData"].(map[string]any)["properties"].(map[string]any)
	config := source["config"].(map[string]any)["properties"].(map[string]any)
	delete(config, "streams") // Acquisition belongs to immutable Stream releases.
	config["connection"] = map[string]any{"$ref": "#/components/schemas/SourceDefinition"}
	g.schemas["Binding"].(map[string]any)["properties"].(map[string]any)["target"].(map[string]any)["properties"].(map[string]any)["collectionId"] = map[string]any{"$ref": "#/components/schemas/UnsV2Identity"}
	g.schemas["Processor"].(map[string]any)["properties"].(map[string]any)["language"] = map[string]any{"type": "string", "enum": []string{"javascript"}}
	base := g.schemas["Document"].(map[string]any)["properties"].(map[string]any)
	makeDoc := func(name string, fields, required []string) {
		props := map[string]any{}
		for _, field := range fields {
			props[field] = base[field]
		}
		g.schemas[name] = map[string]any{"type": "object", "additionalProperties": false, "properties": props, "required": required}
	}
	makeDoc("DirectoryData", []string{"schemaVersion", "name", "description", "parentId"}, []string{"schemaVersion", "name"})
	makeDoc("CollectionData", []string{"schemaVersion", "name", "description", "recordSchema", "sampleSchema", "indexes", "semantics"}, []string{"schemaVersion", "name", "recordSchema"})

	g.schema(reflect.TypeOf(unsmeta.IndexState{}))
	index := g.schemas["IndexSpec"].(map[string]any)
	propsIndex := index["properties"].(map[string]any)
	propsIndex["kind"] = map[string]any{"type": "string", "enum": []string{"btree", "gin"}, "default": "btree"}
	propsIndex["target"] = map[string]any{"type": "string", "enum": []string{"records", "samples"}, "default": "records"}
	propsIndex["path"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 512, "description": "Bare business field path; _id and _meta.* cannot be declared as business indexes."}
	propsIndex["paths"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 3, "uniqueItems": true, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 512}}
	index["oneOf"] = []any{map[string]any{"required": []string{"path"}, "not": map[string]any{"required": []string{"paths"}}}, map[string]any{"required": []string{"paths"}, "not": map[string]any{"required": []string{"path"}}}}
	index["description"] = "Nova index declaration. Unique is supported only for Record B-tree indexes; GIN takes one path. Unique values retain JSON types, exclude soft-deleted Records, allow missing paths, and treat explicit JSON null as a value. Constraints become effective only when indexStatus.state=ready."
	g.schemas["IndexState"].(map[string]any)["properties"].(map[string]any)["state"] = map[string]any{"type": "string", "enum": []string{"pending", "building", "ready", "failed", "removing", "managedAtSource"}}
	g.schemas["CollectionData"].(map[string]any)["properties"].(map[string]any)["indexes"].(map[string]any)["maxItems"] = 16
	makeDoc("TopicData", []string{"schemaVersion", "name", "description", "parentId", "viewMode", "collectionId", "fields"}, []string{"schemaVersion", "name", "viewMode"})
	topic := g.schemas["TopicData"].(map[string]any)
	props := topic["properties"].(map[string]any)
	props["viewMode"] = map[string]any{"type": "string", "enum": []string{"records", "fields"}}
	props["collectionId"] = map[string]any{"$ref": "#/components/schemas/UnsV2Identity"}
	for _, name := range []string{"DirectoryData", "TopicData"} {
		g.schemas[name].(map[string]any)["properties"].(map[string]any)["parentId"] = map[string]any{"nullable": true, "allOf": []any{map[string]any{"$ref": "#/components/schemas/UnsV2Identity"}}}
	}
	for _, pair := range [][2]string{{"Binding", "streamId"}, {"Binding", "topicId"}, {"Grant", "collectionId"}, {"FieldBinding", "collectionId"}, {"FieldBinding", "recordId"}} {
		g.schemas[pair[0]].(map[string]any)["properties"].(map[string]any)[pair[1]] = map[string]any{"$ref": "#/components/schemas/UnsV2Identity"}
	}
	grant := g.schemas["Grant"].(map[string]any)["properties"].(map[string]any)
	grant["permissions"] = map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"record:read", "record:write", "record:delete", "sample:read", "sample:write"}}}
	g.schemas["AppData"].(map[string]any)["properties"].(map[string]any)["navigation"].(map[string]any)["properties"].(map[string]any)["topicIds"] = map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/UnsV2Identity"}}
	for name, schema := range unsv2.ResponseSchemas() {
		g.schemas[name] = schema
	}
	for _, model := range unsv2.Models() {
		if schema, ok := g.schemas[model.Schema].(map[string]any); ok {
			schema["description"] = strings.Join(model.Rules, "; ")
			schema["x-readOnlyFields"] = model.ReadOnlyFields
			props := schema["properties"].(map[string]any)
			if p, ok := props["schemaVersion"].(map[string]any); ok {
				p["enum"] = []int{1}
				p["default"] = 1
			}
			if p, ok := props["name"].(map[string]any); ok {
				p["minLength"] = 1
				p["maxLength"] = common.NameMaxLen
				p["description"] = "Unique display name; no surrounding whitespace"
			}
			if p, ok := props["description"].(map[string]any); ok {
				p["maxLength"] = common.DescMaxLen
			}
		}
	}
	protocols := []string{string(sourcestream.HTTP), string(sourcestream.OAuth), string(sourcestream.MQTT), string(sourcestream.OPCUA), string(sourcestream.ModbusTCP)}
	header := g.schemas["Header"].(map[string]any)["properties"].(map[string]any)
	header["name"] = map[string]any{"type": "string", "minLength": 1, "maxLength": common.NameMaxLen}
	header["description"] = map[string]any{"type": "string", "maxLength": common.DescMaxLen}
	g.schemas["StreamDefinition"].(map[string]any)["properties"].(map[string]any)["supportedProtocols"] = map[string]any{"type": "array", "minItems": 0, "description": "Supported field protocols.", "items": map[string]any{"type": "string", "enum": protocols}}
	g.schemas["SourceDefinition"].(map[string]any)["description"] = "Select the matching protocol configuration. file-read uses file.resources, edge-sql uses exactly one of sql.query (preset) or sql.queries (named), and PI uses http.parameters. Target protocol catalogs declare supported execution and TLS capabilities."
	binding := g.schemas["Binding"].(map[string]any)["properties"].(map[string]any)
	binding["parameters"] = map[string]any{"type": "object", "additionalProperties": true, "description": "Must satisfy parametersSchema of the selected immutable streamRelease; fetch the Stream release before constructing this object"}
	binding["acquisition"] = map[string]any{"oneOf": []any{map[string]any{"$ref": "#/components/schemas/Acquisition"}, map[string]any{"type": "object", "maxProperties": 0, "description": "Inactive draft only; cannot enable until acquisition is configured"}}}
	binding["streamRelease"] = map[string]any{"type": "integer", "minimum": 1}
	g.schemas["SourceData"].(map[string]any)["properties"].(map[string]any)["adapter"] = map[string]any{"type": "string", "enum": protocols, "description": "Select exactly this protocol; http-api is not http"}
	connection := g.schemas["SourceDefinition"].(map[string]any)["properties"].(map[string]any)
	connection["protocol"] = map[string]any{"type": "string", "enum": protocols, "readOnly": true, "description": "Normalized from Source.adapter"}
	connection["runtimeLocation"] = map[string]any{"type": "string", "enum": []string{"local"}, "default": "local"}
	connection["role"] = map[string]any{"type": "string", "enum": []string{"client", "server"}, "default": "client"}
	connection["endpoint"] = map[string]any{"type": "string", "description": "Protocol URL; no userinfo, query or fragment. Server role uses managed endpoint: http://managed, mqtt://managed or opc.tcp://managed"}
	connection["credentialRef"] = map[string]any{"type": "string", "writeOnly": true, "description": "Existing credential reference only; this API cannot mint or rotate credentials"}
	p := g.schemas["Processor"].(map[string]any)["properties"].(map[string]any)
	p["code"] = map[string]any{"type": "string", "minLength": 1, "maxLength": sourcestream.MaxScriptBytes, "writeOnly": true, "description": "JavaScript function body for process(input,params); configuration only, not execution"}
	p["timeoutMs"] = map[string]any{"type": "integer", "minimum": 1, "maximum": sourcestream.MaxScriptTimeoutMs}
	g.schemas["Binding"].(map[string]any)["properties"].(map[string]any)["enabled"] = map[string]any{"type": "boolean", "default": false, "description": "Create/update disabled, then use sources/bindings/state/update with current parent Source revision"}
	acq := g.schemas["Acquisition"].(map[string]any)
	ap := acq["properties"].(map[string]any)
	ap["mode"] = map[string]any{"type": "string", "enum": []string{"poll", "watch"}, "description": "HTTP/OAuth use poll; native protocols use watch with nativeResourceRefs and collection"}
	ap["pollIntervalMs"] = map[string]any{"type": "integer", "minimum": 1000, "maximum": 86400000, "description": "Delay between completed full scans; not an overlapping timer"}
	ap["timeoutMs"] = map[string]any{"type": "integer", "minimum": 100, "maximum": 30000, "description": "Whole HTTP scan budget; native watches are persistent and cancellable"}
	acq["oneOf"] = []any{map[string]any{"properties": map[string]any{"mode": map[string]any{"enum": []string{"poll"}}}, "required": []string{"http", "pollIntervalMs"}}, map[string]any{"properties": map[string]any{"mode": map[string]any{"enum": []string{"watch"}}}, "required": []string{"nativeResourceRefs", "collection"}}}
	rp := g.schemas["AcquisitionRetry"].(map[string]any)["properties"].(map[string]any)
	rp["maxAttempts"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 5, "description": "Total HTTP GET attempts per page, including the first attempt"}
	rp["backoffMs"] = map[string]any{"type": "integer", "minimum": 100, "maximum": 30000}
	pp := g.schemas["HTTPPagination"].(map[string]any)["properties"].(map[string]any)
	pp["mode"] = map[string]any{"type": "string", "enum": []string{"none", "page", "cursor"}}
	pp["maxPages"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "description": "Guard per scan; hitting it while more data remains fails visibly rather than claiming a complete scan"}
	pp["pageSize"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}
	g.schemas["AppData"].(map[string]any)["properties"].(map[string]any)["status"] = map[string]any{"type": "string", "enum": []string{"active", "disabled"}, "default": "active"}
	platformPermissions := map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": unsv2.AppPlatformPermissions()}, "uniqueItems": true, "maxItems": len(unsv2.AppPlatformPermissions()), "description": "Explicit platform capabilities; default none; current App grants intersect with key and issuer resources"}
	g.schemas["AppData"].(map[string]any)["properties"].(map[string]any)["platformPermissions"] = platformPermissions
	streamPolicy := g.schemas["AppStreamPolicy"].(map[string]any)
	streamProperties := streamPolicy["properties"].(map[string]any)
	streamProperties["version"] = map[string]any{"type": "string", "enum": []string{"cloud-hosted-app-stream/v1"}}
	streamProperties["status"] = map[string]any{"type": "string", "enum": []string{"active", "disabled"}}
	streamProperties["platformPermissions"] = map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": unsv2.AppStreamPermissions()}, "uniqueItems": true, "maxItems": len(unsv2.AppStreamPermissions())}
	streamProperties["messageTopics"] = map[string]any{"type": "array", "maxItems": 32, "items": map[string]any{"$ref": "#/components/schemas/AppStreamMessageTopicPermission"}}
	topicProperties := g.schemas["AppStreamMessageTopicPermission"].(map[string]any)["properties"].(map[string]any)
	topicProperties["topic"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 128, "pattern": "^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,127}$"}
	g.schemas["AppData"].(map[string]any)["properties"].(map[string]any)["streamInteraction"] = map[string]any{"$ref": "#/components/schemas/AppStreamPolicy", "readOnly": true, "description": "Server-managed native App policy; generic App writes preserve it and cannot grant or revoke Stream authority."}
	g.schemas["AppContext"].(map[string]any)["properties"].(map[string]any)["platformPermissions"] = platformPermissions
	json.NewEncoder(os.Stdout).Encode(g.schemas)
}
