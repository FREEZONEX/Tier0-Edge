package system

type openAPIRequestFieldDoc struct {
	Description string
	Enum        []any
	ItemsEnum   []any
}

var (
	openAPINodeTypeEnum         = []any{"PATH", "TOPIC"}
	openAPITopicTypeEnum        = []any{"STATE", "ACTION", "METRIC"}
	openAPIFlowTypeEnum         = []any{"SourceFlow", "EventFlow"}
	openAPIHistoryFunctionEnum  = []any{"avg", "min", "max", "sum", "count", "first", "last"}
	openAPIHistoryCountModeEnum = []any{"exact", "none"}
	openAPISchemaFieldEnum      = []any{"INTEGER", "LONG", "DOUBLE", "FLOAT", "STRING", "BOOLEAN", "DATETIME"}
	openAPIQoSEnum              = []any{0, 1, 2}
	openAPIFavoriteEnum         = []any{1, 2}
	openAPIUpdateMaskEnum       = []any{"name", "description", "displayName", "extendProperties", "fields", "alias", "enableHistory"}
)

var openAPIRequestSchemaDocs = map[string]map[string]openAPIRequestFieldDoc{
	"AssetUploadReq": {
		"fileName":    {Description: "Original file name used when creating the upload URL."},
		"contentType": {Description: "MIME content type of the file, for example application/octet-stream."},
		"size":        {Description: "File size in bytes."},
		"sha256":      {Description: "Optional SHA-256 checksum of the file content."},
		"storageKey":  {Description: "Optional object storage key. Omit it to let the server generate one."},
		"isPublic":    {Description: "Whether the uploaded object should be readable through a public file URL."},
		"business":    {Description: "Business domain or feature that owns the uploaded file."},
		"useBy":       {Description: "Usage scenario or owner category for the uploaded file."},
	},
	"ReadReq": {
		"topics":             {Description: "UNS topic paths to read."},
		"include_metadata":   {Description: "Whether each result should include UNS node metadata."},
		"include_leaf_value": {Description: "Whether leaf nodes should include their latest payload value."},
	},
	"WriteReq": {
		"writes": {Description: "Write operations to apply in this request."},
		"qos":    {Description: "MQTT QoS level for the write operation. Allowed values: 0, 1, 2.", Enum: openAPIQoSEnum},
		"retain": {Description: "Whether the MQTT message should be retained."},
	},
	"WriteItem": {
		"topic":     {Description: "UNS topic path to write."},
		"value":     {Description: "Payload value to write to the topic."},
		"timeStamp": {Description: "Optional payload timestamp in Unix milliseconds. Omit to use server time."},
	},
	"BrowseReq": {
		"path":               {Description: "Root UNS path to browse. Empty means the workspace root."},
		"max_depth":          {Description: "Maximum tree depth to return. Omit or use 0 for the default depth."},
		"include_metadata":   {Description: "Whether returned nodes should include metadata."},
		"include_leaf_value": {Description: "Whether returned leaf nodes should include latest payload values."},
	},
	"SearchReq": {
		"keyword":            {Description: "Keyword matched against UNS node names, aliases, and paths."},
		"topicType":          {Description: "Topic data category filter. Allowed values: STATE, ACTION, METRIC.", Enum: openAPITopicTypeEnum},
		"path_prefix":        {Description: "Only return nodes under this UNS path prefix."},
		"include_metadata":   {Description: "Whether returned nodes should include metadata."},
		"include_leaf_value": {Description: "Whether returned leaf nodes should include latest payload values."},
		"page":               {Description: "Page number, starting from 1."},
		"size":               {Description: "Page size."},
	},
	"HistoryReq": {
		"topics":      {Description: "UNS topic paths whose history should be queried."},
		"start_time":  {Description: "History query start time in RFC3339 format, for example 2026-06-22T00:00:00Z."},
		"end_time":    {Description: "History query end time in RFC3339 format, for example 2026-06-22T01:00:00Z."},
		"aggregation": {Description: "Optional aggregation settings for bucketed history results."},
		"page":        {Description: "Page number, starting from 1."},
		"size":        {Description: "Page size."},
		"countMode":   {Description: "Total count mode. Defaults to exact for backward compatibility; use none to skip COUNT and rely on totalExact and hasMore.", Enum: openAPIHistoryCountModeEnum},
	},
	"HistoryAggregation": {
		"interval": {Description: "Positive aggregation window. Supports Go duration strings such as 1m, 5m, 1h, and day strings such as 1d."},
		"function": {Description: "Legacy single-field aggregation function. Allowed values: avg, min, max, sum, count, first, last.", Enum: openAPIHistoryFunctionEnum},
		"field":    {Description: "Legacy single-field payload name to aggregate, for example value."},
		"fields":   {Description: "Optional multi-field aggregation definitions. Each payload field can appear once per request."},
	},
	"HistoryAggregationField": {
		"name":     {Description: "First-level payload field name to aggregate."},
		"function": {Description: "Aggregation function. Defaults to avg for numeric fields and last for other fields.", Enum: openAPIHistoryFunctionEnum},
	},
	"NodeCreateReq": {
		"namespace": {Description: "UNS nodes to create. Children can be nested to create a subtree."},
	},
	"NamespaceNode": {
		"name":             {Description: "Node name. For nested create, this is the segment under its parent."},
		"type":             {Description: "Node type. Allowed values: PATH, TOPIC.", Enum: openAPINodeTypeEnum},
		"children":         {Description: "Child nodes to create under this node."},
		"topicType":        {Description: "Topic data category for TOPIC nodes. Allowed values: STATE, ACTION, METRIC.", Enum: openAPITopicTypeEnum},
		"fields":           {Description: "Payload schema fields for TOPIC nodes."},
		"description":      {Description: "Human-readable node description."},
		"displayName":      {Description: "Display name shown in clients."},
		"extendProperties": {Description: "Custom metadata key-value pairs for the node."},
		"alias":            {Description: "Optional topic alias."},
		"enableHistory":    {Description: "Whether history storage is enabled for this topic."},
	},
	"SchemaField": {
		"name": {Description: "Payload field name."},
		"type": {Description: "Payload field type. Allowed values: INTEGER, LONG, DOUBLE, FLOAT, STRING, BOOLEAN, DATETIME.", Enum: openAPISchemaFieldEnum},
		"unit": {Description: "Optional engineering unit for the field."},
	},
	"NodeUpdateReq": {
		"path":             {Description: "Existing UNS topic or path node to update."},
		"name":             {Description: "New node name."},
		"description":      {Description: "Updated node description."},
		"displayName":      {Description: "Updated display name."},
		"extendProperties": {Description: "Updated custom metadata key-value pairs."},
		"fields":           {Description: "Updated payload schema fields."},
		"alias":            {Description: "Updated topic alias."},
		"enableHistory":    {Description: "Whether history storage is enabled for this topic."},
		"updateMask":       {Description: "Cloud-compatible partial update field list. Allowed values: name, description, displayName, extendProperties, fields, alias, enableHistory.", ItemsEnum: openAPIUpdateMaskEnum},
	},
	"NodeDeleteReq": {
		"topics":      {Description: "UNS topic or path node paths to delete."},
		"hard_delete": {Description: "Whether to permanently delete nodes instead of moving them to recycle state."},
	},
	"NodeRestoreReq": {
		"path": {Description: "UNS topic or path node to restore from recycle state."},
	},
	"UnsBindFlowReq": {
		"unsId":  {Description: "UNS node ID to bind."},
		"flowId": {Description: "Flow ID to bind to the UNS node."},
	},
	"OpenapiFlowLegacyListReq": {
		"keyword":  {Description: "Keyword matched against flow names and descriptions."},
		"flowType": {Description: "Flow category filter. Canonical values: SourceFlow, EventFlow. Enterprise also accepts source, event, 1, and 2 aliases.", Enum: openAPIFlowTypeEnum},
	},
	"OpenapiFlowLegacyGetReq": {
		"id": {Description: "Flow ID."},
	},
	"OpenapiFlowLegacyCreateReq": {
		"flowName":    {Description: "Flow name."},
		"flowType":    {Description: "Flow category. Canonical values: SourceFlow, EventFlow. Enterprise also accepts source, event, 1, and 2 aliases.", Enum: openAPIFlowTypeEnum},
		"description": {Description: "Flow description."},
		"template":    {Description: "Optional flow template JSON."},
	},
	"OpenapiFlowLegacyUpdateReq": {
		"id":          {Description: "Flow ID to update."},
		"flowName":    {Description: "Updated flow name."},
		"description": {Description: "Updated flow description."},
		"template":    {Description: "Updated flow template JSON."},
		"isFavorite":  {Description: "Favorite state. Allowed values: 1 favorite, 2 not favorite.", Enum: openAPIFavoriteEnum},
	},
	"OpenapiFlowLegacyDeleteReq": {
		"ids": {Description: "Flow IDs to delete."},
	},
	"OpenapiFlowLegacyDeployReq": {
		"id":        {Description: "Flow ID to deploy."},
		"flowsJson": {Description: "Node-RED flows JSON to deploy."},
	},
	"OpenapiFlowLegacyNodesReq": {
		"flowType": {Description: "Flow category whose Node-RED node set should be returned. Canonical values: SourceFlow, EventFlow. Enterprise also accepts source, event, 1, and 2 aliases.", Enum: openAPIFlowTypeEnum},
	},
}

var openAPIRequestParameterDocs = map[string]openAPIRequestFieldDoc{
	"unsId":          {Description: "UNS node ID."},
	"fileName":       {Description: "Attachment file name."},
	"sha256":         {Description: "Optional SHA-256 checksum of the attachment content."},
	"pageNo":         {Description: "Page number, starting from 1."},
	"pageSize":       {Description: "Page size."},
	"includeFileUrl": {Description: "Whether attachment list items should include temporary file URLs."},
}

func applyOpenAPIRequestDescriptions(spec map[string]any) {
	components, ok := spec["components"].(map[string]any)
	if ok {
		if schemas, ok := components["schemas"].(map[string]any); ok {
			applyOpenAPISchemaDocs(schemas)
		}
	}
	// goctl currently emits Swagger 2 definitions. Keep the OpenAPI 3 path
	// above for forward compatibility, while documenting the schema shape that
	// is actually served today.
	if definitions, ok := spec["definitions"].(map[string]any); ok {
		applyOpenAPISchemaDocs(definitions)
	}
	applyOpenAPIParameterDocs(spec["paths"])
}

func applyOpenAPISchemaDocs(schemas map[string]any) {
	for schemaName, docs := range openAPIRequestSchemaDocs {
		applyOpenAPISchemaFieldDocs(schemas[schemaName], docs)
	}
}

func applyOpenAPISchemaFieldDocs(schema any, docs map[string]openAPIRequestFieldDoc) {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return
	}
	properties, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		return
	}
	for fieldName, doc := range docs {
		property, ok := properties[fieldName].(map[string]any)
		if !ok {
			continue
		}
		applyOpenAPIFieldDoc(property, doc)
	}
}

func applyOpenAPIParameterDocs(rawPaths any) {
	paths, ok := rawPaths.(map[string]any)
	if !ok {
		return
	}
	for _, item := range paths {
		pathItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, operation := range pathItem {
			operationMap, ok := operation.(map[string]any)
			if !ok {
				continue
			}
			parameters, ok := operationMap["parameters"].([]any)
			if !ok {
				continue
			}
			for _, parameter := range parameters {
				parameterMap, ok := parameter.(map[string]any)
				if !ok {
					continue
				}
				name, _ := parameterMap["name"].(string)
				doc, ok := openAPIRequestParameterDocs[name]
				if !ok {
					continue
				}
				applyOpenAPIFieldDoc(parameterMap, doc)
				if len(doc.Enum) > 0 {
					schema, ok := parameterMap["schema"].(map[string]any)
					if !ok {
						schema = map[string]any{}
						parameterMap["schema"] = schema
					}
					schema["enum"] = doc.Enum
				}
			}
		}
	}
}

func applyOpenAPIFieldDoc(target map[string]any, doc openAPIRequestFieldDoc) {
	if doc.Description != "" {
		target["description"] = doc.Description
	}
	if len(doc.Enum) > 0 {
		target["enum"] = doc.Enum
	}
	if len(doc.ItemsEnum) > 0 {
		items, ok := target["items"].(map[string]any)
		if !ok {
			items = map[string]any{}
			target["items"] = items
		}
		items["enum"] = doc.ItemsEnum
	}
}
