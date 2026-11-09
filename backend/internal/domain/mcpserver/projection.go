package mcpserver

import "net/url"

// 连接信息仅展示协议与去掉 userinfo/query/fragment 的地址。HTTP headers/body、
// TLS 私钥、credentialRef 等均不进入 MCP 投影。
func sourceProjection(data map[string]any) map[string]any {
	out := pick(data, "schemaVersion", "name", "description", "adapter", "configRevision")
	config := object(data["config"])
	connection := object(config["connection"])
	publicConnection := pick(connection, "protocol", "role", "runtimeLocation")
	if endpoint := stringValue(connection["endpoint"]); endpoint != "" {
		if u, err := url.Parse(endpoint); err == nil && u.Host != "" && u.Scheme != "" {
			u.User = nil
			u.RawQuery = ""
			u.Fragment = ""
			publicConnection["endpointOrigin"] = u.Scheme + "://" + u.Host
		}
	}
	instances := map[string]any{}
	for id, value := range object(config["streams"]) {
		binding := object(value)
		item := pick(binding, "name", "streamId", "streamRelease", "enabled", "topicId", "model", "activeVersion", "pendingVersion", "status")
		if parameters, ok := binding["parameters"]; ok {
			item["parameters"] = redactData(parameters)
		}
		if target, ok := binding["target"]; ok {
			item["target"] = pick(object(target), "collectionId")
		}
		if mapping, ok := binding["mapping"]; ok {
			item["mapping"] = pick(object(mapping), "recordId", "recordIdPath", "recordPath", "sampleEnabled", "samplePath", "timePath", "qualityPath")
		}
		if target, ok := binding["metadataTarget"]; ok {
			item["metadataTarget"] = pick(object(target), "collectionId", "topicId", "recordId", "recordIdPath", "recordPath", "sampleEnabled", "samplePath", "timePath", "qualityPath")
		}
		instances[id] = item
	}
	out["config"] = map[string]any{"connection": publicConnection, "streams": instances}
	return out
}

func stateProjection(data map[string]any) map[string]any {
	out := pick(data, "schemaVersion", "health", "checkedAt", "lastReceivedAt", "lastSampleAt", "configRevision", "observedRevision", "imported", "ackSeq")
	streams := map[string]any{}
	for id, value := range object(data["streams"]) {
		state := object(value)
		public := pick(state, "status", "health", "connected", "ackSeq", "seq", "epoch", "lastReceivedAt", "lastSampleAt", "checkedAt", "lastErrorCode", "errorCode", "attempts", "retryAt")
		if _, ok := state["cursor"]; ok {
			public["hasCursor"] = state["cursor"] != nil
		}
		streams[id] = public
	}
	out["streams"] = streams
	return out
}

func streamDefinitionProjection(value any) map[string]any {
	data := object(value)
	out := pick(data, "supportedProtocols", "parametersSchema", "output")
	if processor, ok := data["processor"]; ok {
		out["processor"] = pick(object(processor), "language", "timeoutMs")
		out["hasProcessorCode"] = stringValue(object(processor)["code"]) != ""
	}
	return out
}

func streamProjection(data map[string]any) map[string]any {
	out := pick(data, "schemaVersion", "name", "description", "executionMode", "model", "sourceId", "activeVersion", "pendingVersion", "status")
	if target, ok := data["metadataTarget"]; ok {
		out["metadataTarget"] = pick(object(target), "collectionId", "topicId", "recordId", "recordIdPath", "recordPath", "sampleEnabled", "samplePath", "timePath", "qualityPath")
	}
	if draft, ok := data["draft"]; ok {
		out["draft"] = streamDefinitionProjection(draft)
	}
	if releases, ok := data["releases"]; ok {
		public := map[string]any{}
		for id, value := range object(releases) {
			release := object(value)
			public[id] = map[string]any{"publishedAt": release["publishedAt"], "definition": streamDefinitionProjection(release["definition"])}
		}
		out["releases"] = public
	}
	for _, key := range []string{"draftManifest", "programManifest"} {
		if value, ok := data[key]; ok {
			manifest := object(value)
			public := map[string]any{}
			for _, listKey := range []string{"sources", "topics"} {
				items := []any{}
				for _, item := range array(manifest[listKey]) {
					items = append(items, pick(object(item), "id", "sourceId", "topicId", "name", "protocol", "direction"))
				}
				public[listKey] = items
			}
			out[key] = public
		}
	}
	return out
}

func array(value any) []any {
	v, _ := value.([]any)
	if v == nil {
		return []any{}
	}
	return v
}
