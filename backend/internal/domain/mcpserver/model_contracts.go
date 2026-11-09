package mcpserver

import (
	"backend/internal/domain/unsv2"
	"context"
	"encoding/json"
	"strings"
)

func (s *Service) modelContracts(ctx context.Context, input ContractInput) (any, error) {
	if !permitted(ctx, "uns.read") {
		return nil, &unsv2.Fault{Code: "FORBIDDEN", Reason: "UNS reading permission required"}
	}
	if len(input.Operations) > 20 || len(input.Kinds) > 20 {
		return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Path: "operations/kinds", Reason: "At most 20 selections allowed"}
	}
	kinds := append([]string{}, input.Kinds...)
	if input.Kind != "" {
		kinds = append(kinds, input.Kind)
	}
	wanted := map[string]bool{}
	for _, kind := range kinds {
		if _, ok := unsv2.Model(kind); !ok {
			return nil, s.kindFault(ctx, kind)
		}
		wanted[kind] = true
	}
	c := catalog()
	operationSelection := len(input.Operations) > 0 && len(kinds) == 0
	if operationSelection {
		for _, name := range input.Operations {
			if o, ok := findOperation(c, name); ok {
				if resource, ok := unsv2.FindResourceOperation(o.Name); ok {
					wanted[resource.Resource] = true
				}
			}
		}
	}
	models := []map[string]any{}
	defs := map[string]any{}
	names := map[string]bool{}
	for _, model := range unsv2.Models() {
		if operationSelection && !wanted[model.Kind] {
			continue
		}
		if len(wanted) > 0 && !wanted[model.Kind] {
			continue
		}
		if !modelAllowed(ctx, model) {
			if len(wanted) > 0 {
				return nil, &unsv2.Fault{Code: "FORBIDDEN", Path: "kinds", Reason: "This model requires its current read or management permission", Repair: "Check the model permissions returned by api_operations_list"}
			}
			continue
		}
		for _, name := range model.Operations {
			names[name] = true
		}
		for _, operation := range unsv2.ResourceOperations() {
			if operation.Resource == model.Kind {
				names[operation.Name] = true
			}
		}
		for key, value := range schemaClosure(c.Schemas, model.Schema) {
			defs[key] = value
		}
		entry := map[string]any{"kind": model.Kind, "schemaRef": "#/components/schemas/" + model.Schema, "rules": model.Rules, "readOnlyFields": model.ReadOnlyFields, "boundary": model.Boundary, "identity": map[string]string{"record": "(collectionId,id)", "sample": "(collectionId,recordId,id)", "release": "(streamId,release)"}[model.Kind], "updatePolicy": "Use the current resource update/replace contract; updates preserve unmentioned fields, replacements require complete writable data; never replace with a redacted read projection"}
		entry["schemaAvailable"] = model.Schema != ""
		if model.Schema == "" {
			delete(entry, "schemaRef")
		}
		actions := []map[string]any{}
		for _, action := range model.Actions {
			includeMutationOperations(names, model.Kind, action)
			requiresRevision := false
			actions = append(actions, map[string]any{"action": action, "operation": mutationOperation(model.Kind, action), "mcpExecutable": modelExecutable(ctx, model, action), "mcpTool": mutationTool(model.Kind, action), "requiresRevision": requiresRevision})
		}
		entry["actions"] = actions
		models = append(models, entry)
	}
	selected := input.Operations
	if len(selected) == 0 {
		for _, o := range c.Operations {
			if names[o.Name] && operationDocumentationAllowed(ctx, o.Name) {
				selected = append(selected, o.Name)
			}
		}
	}
	out := []map[string]any{}
	for _, name := range selected {
		o, ok := findOperation(c, name)
		if !ok {
			return nil, s.operationFault(ctx, name)
		}
		if retiredDataQueryOperation(o.Name) {
			return nil, &unsv2.Fault{Code: "UNSUPPORTED_OPERATION", Path: "operations", Message: "Use the current collection-scoped Record or Sample resource contract"}
		}
		if !operationDocumentationAllowed(ctx, o.Name) {
			return nil, &unsv2.Fault{Code: "FORBIDDEN", Reason: "App model permission required"}
		}
		entry := operationView(ctx, o)
		entry["requestSchema"] = map[string]any{"$ref": "#/components/schemas/" + o.Request}
		response := unsv2.ResponseModel(o.Name)
		entry["responseSchema"] = map[string]any{"$ref": "#/components/schemas/" + response}
		entry["dataSchema"] = entry["responseSchema"]
		entry["responseTransport"] = "responseSchema describes the HTTP envelope; MCP returns dataSchema in structuredContent, or a structured Fault on error"
		envelopeName := "Response_" + o.Name
		defs[envelopeName] = map[string]any{"type": "object", "required": []string{"code", "msg", "requestId", "data"}, "properties": map[string]any{"code": map[string]any{"type": "integer"}, "msg": map[string]any{"type": "string"}, "requestId": map[string]any{"type": "string"}, "data": entry["dataSchema"], "error": map[string]any{"$ref": "#/components/schemas/Fault"}}}
		entry["responseSchema"] = map[string]any{"$ref": "#/components/schemas/" + envelopeName}
		if o.Response != "" {
			entry["responseSchema"] = map[string]any{"$ref": "#/components/schemas/" + o.Response}
			entry["dataSchema"] = object(object(c.Schemas[o.Response])["properties"])["data"]
			for key, value := range schemaClosure(c.Schemas, o.Response) {
				defs[key] = value
			}
		}
		for key, value := range schemaClosure(c.Schemas, "Fault") {
			defs[key] = value
		}
		for key, value := range schemaClosure(c.Schemas, o.Request) {
			defs[key] = value
		}
		for key, value := range schemaClosure(c.Schemas, response) {
			defs[key] = value
		}
		out = append(out, entry)
	}
	// 不通过通用 apply 请求暴露调用者无权读取的配置结构。
	for _, changeName := range []string{"UnsV2Change", "UnsV2MetaChange"} {
		change, ok := defs[changeName].(map[string]any)
		if !ok {
			continue
		}
		// The bundled catalog is immutable and shared. Permission filtering must
		// never alter another caller's schema variants.
		raw, _ := json.Marshal(change)
		var cloned map[string]any
		_ = json.Unmarshal(raw, &cloned)
		change = cloned
		defs[changeName] = change
		variants := []any{}
		for _, v := range array(change["oneOf"]) {
			props := object(object(v)["properties"])
			ks := array(object(props["kind"])["enum"])
			if len(ks) == 0 {
				variants = append(variants, v)
				continue
			}
			allowed := []any{}
			for _, k := range ks {
				m, ok := unsv2.Model(stringValue(k))
				if ok && modelAllowed(ctx, m) {
					allowed = append(allowed, k)
				}
			}
			if len(allowed) > 0 {
				object(props["kind"])["enum"] = allowed
				variants = append(variants, v)
			}
		}
		change["oneOf"] = variants
	}
	for _, model := range unsv2.Models() {
		if !modelAllowed(ctx, model) {
			for key := range schemaClosure(c.Schemas, model.Schema) {
				if key == model.Schema {
					delete(defs, key)
				}
			}
		}
	}
	reachable := map[string]any{}
	for _, entry := range out {
		for _, key := range []string{"requestSchema", "responseSchema"} {
			root := stringValue(object(entry[key])["$ref"])
			root = strings.TrimPrefix(root, "#/components/schemas/")
			for name, value := range schemaClosure(defs, root) {
				reachable[name] = value
			}
		}
		// Schema definitions are de-duplicated at the response root.
	}
	for _, entry := range models {
		root := strings.TrimPrefix(stringValue(entry["schemaRef"]), "#/components/schemas/")
		for name, value := range schemaClosure(defs, root) {
			reachable[name] = value
		}
	}
	relations := []map[string]any{}
	for _, relation := range unsv2.Relations() {
		if m, ok := unsv2.Model(stringValue(relation["from"])); ok && modelAllowed(ctx, m) {
			relations = append(relations, relation)
		}
	}
	endpoints := []string{}
	for _, operation := range c.Operations {
		if unsv2.BatchScope(operation.Name) != "" && operationDocumentationAllowed(ctx, operation.Name) {
			endpoints = append(endpoints, operation.Suffix)
		}
	}
	return map[string]any{"productVersion": s.version, "contractVersion": unsv2.Catalog()["contractVersion"], "models": models, "operations": out, "schemas": reachable, "relations": relations, "metadataBatch": map[string]any{"maxOperations": 200, "execution": "synchronous", "atomic": "transact is atomic; bulkWrite defaults to atomic=true and alone supports atomic=false", "preview": "dryRun=true validates with rollback; omitted dryRun is false", "limits": "Use each selected operation request schema for exact limits"}, "batch": map[string]any{"scope": "Record transactions", "atomic": true, "maxItems": unsv2.MaxItems, "syncItems": unsv2.SyncItems, "mode": "auto", "references": "{ref:opId} points to a creation; dependsOn orders transaction operations", "endpoints": endpoints, "crossScopeAtomicity": false, "partialArrayWrites": true, "transactionQueries": true, "preview": "Record transactions use dryRun=true on the same records:transact interface. Metadata writes use dryRun=true on the same interface; omitted dryRun defaults to false. Resource actions support previews only when dryRun is declared in their request schema. Reuse the original idempotencyKey with an unchanged request after an uncertain outcome; inspect business state/job before resubmitting without a key.", "concurrency": "Revision guards are optional where declared; supplied revisions must match. Single-record update/replace may explicitly upsert by _id or a complete ready unique-index key. Restore rechecks current schema and uniqueness atomically."}}, nil
}
