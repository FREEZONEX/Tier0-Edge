package repo

import "strings"

const (
	// APIKeyAppIDMaxLen matches the existing sys_api_key.app_id storage limit.
	APIKeyAppIDMaxLen     = 64
	APIKeyUsageAppData    = "app-data"
	APIKeyPermissionRead  = "read_only"
	APIKeyPermissionWrite = "data_writer"
	APIKeyPermissionFull  = "full_access"
)

func NormalizeAPIKeyPermission(permission string) string {
	switch strings.TrimSpace(strings.ToLower(permission)) {
	case "", "read", "read_only", "readonly":
		return APIKeyPermissionRead
	case "write", "data_writer", "writer":
		return APIKeyPermissionWrite
	case "full", "full_access", "fullaccess", "manage":
		return APIKeyPermissionFull
	default:
		return APIKeyPermissionRead
	}
}

// Platform user/project directories and Agent Source/Stream management,
// credential and deployment scopes are opt-in
// through explicit resourceKeys. Never add them to existing default key profiles.
func APIKeyResourceKeysForPermission(permission string) []string {
	read := []string{
		"uns.read",
		"flow.read",
		"asset.file.read",
		"asset.file.download",
		"launchpad.view",
		"openapi.base",
		// 消息通知（TASK-030）：读档可查询，写档起可发送（对齐云端 notifications:read/send 语义）
		"notifications.read",
	}
	write := append([]string{}, read...)
	write = append(write,
		"uns.write",
		"flow.manage",
		"asset.file.upload",
		"notifications.send",
	)
	full := append([]string{}, write...)
	full = append(full,
		// Device operations can have side effects; read/write-data keys alone
		// must never acquire permission to invoke a Stream operation.
		"uns.stream.execute",
		"control.read",
		"control.execute",
		"control.write",
		"control.manage",
		"stream.requests.read",
		"stream.requests.invoke",
		"stream.requests.write",
		"stream.requests.manage",
		"uns.manage",
		"uns.delete",
		"project.view",
		"project.manage",
		"asset.file.delete",
		"gateway.route.manage",
		"gateway.route.create",
		"gateway.route.update",
		"gateway.route.delete",
		"gateway.route.test",
		"mqtt.auth.manage",
		"mqtt.auth.write",
		"mqtt.auth.delete",
	)
	switch NormalizeAPIKeyPermission(permission) {
	case APIKeyPermissionWrite:
		return uniqueStrings(write)
	case APIKeyPermissionFull:
		return uniqueStrings(full)
	default:
		return uniqueStrings(read)
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
