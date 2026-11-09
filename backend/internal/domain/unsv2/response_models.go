package unsv2

// These response descriptions belong to the shared operation contract. Dynamic
// Record/Sample data is validated by the selected Collection schemas.
func ResponseModel(operation string) string {
	switch operation {
	case "apply", "transactionExecute", "metaBatchExecute", "recordBatchExecute", "recordTransactionExecute", "sampleAppendBatch":
		return "Outcome"
	case "transactionValidate", "metaBatchValidate", "recordBatchValidate", "recordTransactionValidate":
		return "TransactionValidation"
	case "transactionStatus", "metaBatchStatus", "recordBatchStatus", "sampleAppendBatchStatus":
		return "TransactionStatusResult"
	case "recordSearch":
		return "RecordSearchResult"
	case "recordAggregate":
		return "RecordAggregateResult"
	case "recordQueryValidate":
		return "RecordQueryValidation"
	case "dataDiscover":
		return "DiscoveryResult"
	case "collectionDescribe":
		return "CollectionDescription"
	case "appContext":
		return "AppContext"
	case "appKeyCreate":
		return "AppKeyCreated"
	case "appKeyRevoke":
		return "AppKeyRevoked"
	case "catalog":
		return "UnsCatalogData"
	case "jobs":
		return "JobQueryResponse"
	default:
		return "UnsQueryResponse"
	}
}

func ResponseSchemas() map[string]any {
	str := map[string]any{"type": "string"}
	integer := map[string]any{"type": "integer"}
	boolean := map[string]any{"type": "boolean"}
	strings := map[string]any{"type": "array", "items": str}
	anyObj := map[string]any{"type": "object", "additionalProperties": true}
	ref := func(name string) any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	list := func(name string) any { return map[string]any{"type": "array", "items": ref(name)} }
	obj := func(props map[string]any, required ...string) any {
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": true}
	}
	pageProperties := map[string]any{
		"total": map[string]any{"type": "integer", "nullable": true, "description": "Matching row count; null when includeTotal=false."},
		"page":  integer, "size": integer,
		"hasMore":       map[string]any{"type": "boolean", "description": "Continue with nextCursor in cursor mode, otherwise nextPage."},
		"complete":      map[string]any{"type": "boolean", "description": "True only when this response contains the entire query result; a final page alone is not complete."},
		"truncated":     map[string]any{"type": "boolean", "enum": []bool{false}},
		"snapshotScope": map[string]any{"type": "string", "enum": []string{"thisRequestOnly", "boundedLive"}, "description": "thisRequestOnly uses independent request snapshots; boundedLive limits cursor traversal by asOf but does not preserve row versions across requests."},
		"nextPage":      map[string]any{"type": "integer", "minimum": 2},
		"nextCursor":    str, "asOf": map[string]any{"type": "integer", "description": "Cursor traversal cutoff in Unix milliseconds; not a retained database snapshot."}, "totalKnown": boolean,
	}
	pageRequired := []string{"total", "page", "size", "hasMore", "complete", "truncated", "snapshotScope"}
	schemas := map[string]any{
		"PaginationResult":      obj(pageProperties, pageRequired...),
		"QueryWarning":          obj(map[string]any{"code": str, "severity": str, "path": str, "advice": str}, "code", "severity", "path", "advice"),
		"DiscoveryCollection":   obj(map[string]any{"collectionId": str, "name": str, "description": str, "revision": integer, "recordGrain": str, "sampleGrain": str, "hasSamples": boolean}, "collectionId", "name", "description", "revision"),
		"DiscoveryResult":       obj(map[string]any{"list": list("DiscoveryCollection"), "total": integer, "page": integer, "size": integer}, "list", "total", "page", "size"),
		"DiscoveredField":       obj(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"record", "sample"}}, "path": str, "type": str, "format": str, "required": boolean, "description": str, "unit": str, "enum": map[string]any{"type": "array", "items": map[string]any{}}, "aggregationNote": str}, "kind", "path", "type", "required", "description", "unit"),
		"CollectionDescription": obj(map[string]any{"collectionId": str, "name": str, "description": str, "revision": integer, "recordSchema": ref("Schema"), "sampleSchema": ref("Schema"), "indexes": list("IndexSpec"), "indexStatus": list("IndexState"), "metadataNotice": str, "recordGrain": str, "sampleGrain": str, "fields": list("DiscoveredField"), "relationships": list("CollectionRelationship")}, "collectionId", "name", "description", "revision", "recordSchema", "indexes", "indexStatus", "metadataNotice", "fields", "relationships"),
		"QueryColumn":           obj(map[string]any{"name": str, "type": str, "nullable": boolean, "computed": boolean, "sourceField": str, "aggregate": str, "description": str, "unit": str}, "name", "type", "nullable", "sourceField", "aggregate"),
		"RecordSearchResult":    obj(map[string]any{"list": list("RecordRow"), "page": integer, "size": integer, "total": integer, "latestSamples": list("LatestSample"), "rowShape": map[string]any{"type": "string", "enum": []string{"record"}}, "nullMeaning": str}, "list", "page", "size", "total", "rowShape", "nullMeaning"),
		"RecordAggregateResult": obj(map[string]any{"list": list("ProjectedRecordRow"), "page": integer, "size": integer, "total": integer, "columns": list("QueryColumn"), "warnings": strings, "queryWarnings": list("QueryWarning"), "rowShape": map[string]any{"type": "string", "enum": []string{"projection"}}, "nullMeaning": str}, "list", "page", "size", "total", "columns", "warnings", "rowShape", "nullMeaning"),
		"RecordQueryValidation": obj(map[string]any{"status": map[string]any{"type": "string", "enum": []string{"validated"}}, "executed": map[string]any{"type": "boolean", "enum": []bool{false}}, "collectionIds": strings, "columns": list("QueryColumn"), "page": integer, "size": integer, "warnings": strings, "queryWarnings": list("QueryWarning"), "limits": obj(map[string]any{"queryTimeoutMs": integer, "maxPageSize": integer}, "queryTimeoutMs", "maxPageSize"), "nextAction": str}, "status", "executed", "collectionIds", "columns", "page", "size", "warnings", "limits", "nextAction"),
		"ProjectedRecordRow":    map[string]any{"type": "object", "description": "Relational query row keyed by select aliases; values are scalar or null, including aggregate results", "additionalProperties": map[string]any{"anyOf": []any{map[string]any{"type": "string", "nullable": true}, map[string]any{"type": "number", "nullable": true}, map[string]any{"type": "boolean", "nullable": true}}}},
		"JobRow":                obj(map[string]any{"jobId": str, "status": map[string]any{"type": "string", "enum": []string{"queued", "running", "succeeded", "failed"}}, "progress": obj(map[string]any{"total": integer, "completed": integer}, "total", "completed"), "createdAt": integer, "startedAt": integer, "finishedAt": integer, "attempts": integer, "result": map[string]any{"nullable": true, "allOf": []any{ref("Outcome")}}, "error": map[string]any{"nullable": true, "allOf": []any{ref("Fault")}}}, "jobId", "status", "progress"),
		"JobQueryResult":        obj(map[string]any{"queryId": str, "list": list("JobRow")}, "queryId", "list"),
		"JobQueryResponse":      obj(map[string]any{"results": list("JobQueryResult")}, "results"),
		"UnsCatalogData":        obj(map[string]any{"contractVersion": str, "models": map[string]any{"type": "array", "items": anyObj}, "relations": map[string]any{"type": "array", "items": anyObj}, "capabilities": anyObj, "limits": anyObj, "operations": map[string]any{"type": "array", "items": anyObj}, "schemas": anyObj}, "contractVersion", "capabilities"),
		"RecordRow":             obj(map[string]any{"id": str, "collectionId": str, "sourceId": map[string]any{"type": "string", "nullable": true}, "revision": integer, "createdAt": integer, "updatedAt": integer, "data": map[string]any{"type": "object", "description": "Must conform to Collection.recordSchema; no inferred associations"}}, "id", "collectionId", "revision", "data"),
		"SampleRow":             obj(map[string]any{"id": str, "collectionId": str, "recordId": str, "time": map[string]any{"type": "integer", "description": "Business time, Unix milliseconds"}, "receivedAt": integer, "quality": str, "sourceId": map[string]any{"type": "string", "nullable": true}, "data": map[string]any{"type": "object", "description": "Must conform to Collection.sampleSchema"}}, "id", "collectionId", "recordId", "time", "data"),
		"MetadataRow":           obj(map[string]any{"kind": str, "id": str, "revision": integer, "data": anyObj, "launch": obj(map[string]any{"status": str, "url": str}, "status"), "createdAt": integer, "updatedAt": integer, "deletedAt": integer}, "kind", "id", "revision", "data"),
		"LatestSample":          obj(map[string]any{"recordId": str, "sample": map[string]any{"nullable": true, "allOf": []any{ref("SampleRow")}}}, "recordId", "sample"),
		"SampleTrendPoint":      obj(map[string]any{"time": integer, "bucket": integer, "value": map[string]any{"type": "number", "nullable": true}, "min": map[string]any{"type": "number", "nullable": true}, "max": map[string]any{"type": "number", "nullable": true}, "count": integer, "validCount": integer}, "time", "bucket", "value", "min", "max", "count", "validCount"),
		"QueryResult":           obj(map[string]any{"queryId": str, "list": map[string]any{"type": "array", "items": map[string]any{"anyOf": []any{ref("MetadataRow"), ref("RecordRow"), ref("SampleRow"), ref("ProjectedRecordRow")}}}, "page": integer, "size": integer, "total": integer, "collectionId": str, "recordId": str, "fieldPath": map[string]any{"type": "array", "items": str}, "timeRange": obj(map[string]any{"from": integer, "to": integer}, "from", "to"), "bucketMs": integer, "aggregation": map[string]any{"type": "string", "enum": []string{"mean-min-max"}}, "points": list("SampleTrendPoint"), "definition": ref("MetadataRow"), "ancestors": list("MetadataRow"), "latestSamples": list("LatestSample"), "records": anyObj, "selectedRecord": anyObj, "fields": map[string]any{"type": "array", "items": anyObj}, "targets": map[string]any{"type": "array", "items": anyObj}}, "queryId"),
		"UnsQueryResponse":      obj(map[string]any{"results": list("QueryResult")}, "results"),
	}
	for _, name := range []string{"DiscoveryResult", "RecordSearchResult", "RecordAggregateResult", "QueryResult"} {
		schema := schemas[name].(map[string]any)
		properties := schema["properties"].(map[string]any)
		for key, value := range pageProperties {
			properties[key] = value
		}
		// QueryResult also describes unpaged legacy query variants.
		if name != "QueryResult" {
			schema["required"] = append(schema["required"].([]string), "hasMore", "complete", "truncated", "snapshotScope")
		}
	}
	job := schemas["JobRow"].(map[string]any)
	jobProperties := job["properties"].(map[string]any)
	jobProperties["executionStatus"] = map[string]any{"type": "string", "enum": []string{"queued", "running", "finished"}}
	jobProperties["outcome"] = map[string]any{"type": "string", "enum": []string{"pending", "committed", "partiallyCommitted", "rolledBack", "unknown"}, "description": "Data commit outcome independent of legacy status; failed does not imply rollback."}
	job["required"] = append(job["required"].([]string), "executionStatus", "outcome")
	return schemas
}
