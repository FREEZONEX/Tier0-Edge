package sourcestream

func plexSAPDescriptors(resources []edgeResource, def SourceDefinition, revision int64) []edgeResource {
	for _, item := range resources {
		if item.ResourceRef == "vendor:sap-odata:entity:Materials" {
			props := item.InputSchema["properties"].(map[string]any)
			props["material"] = map[string]string{"type": "string", "pattern": "^[0-9]{18}$"}
			props["baseUnit"] = map[string]string{"type": "string", "pattern": "^[A-Z0-9]{1,8}$"}
			props["order"] = map[string]any{"type": "string", "enum": []string{"materialAsc", "materialDesc"}}
			props["selectFields"] = map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"Material", "Description", "BaseUnit"}}, "minItems": 1, "maxItems": 3, "uniqueItems": true, "description": "Include Material, the stable identity."}
		}
		if item.ResourceRef != plexJobsRef {
			continue
		}
		for _, op := range []string{"operations", "production"} {
			operation, ok := vendorOperation(def.SourceType, op)
			if !ok || !operation.ReadOnly {
				continue
			}
			props := map[string]any{"jobId": map[string]string{"type": "string", "format": "uuid"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}
			schema := resourceObject(props)
			metadata := map[string]any{"pagination": "none", "readLimitScope": "preview"}
			if op == "operations" {
				schema["required"] = []string{"jobId"}
				metadata["identityField"] = "jobOperationId"
			} else {
				for _, k := range []string{"beginDate", "endDate"} {
					props[k] = map[string]string{"type": "string", "format": "date-time"}
				}
				schema["required"] = []string{"beginDate", "endDate"}
				metadata["timeWindow"] = "inclusive"
				metadata["maxWindowSeconds"] = 86400
			}
			resources = append(resources, edgeResource{ResourceRef: "vendor:plex-mes:" + op, Name: operation.Name, Modes: []string{"read"}, Permission: item.Permission, ConfigRevision: revision, InputSchema: schema, OutputSchema: item.OutputSchema, Metadata: metadata})
		}
	}
	return resources
}
