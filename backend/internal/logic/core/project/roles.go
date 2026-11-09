package project

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	domainproject "backend/internal/domain/project"
)

const (
	projectPackageRoleMaxNameLen = 128
	projectPackageRoleMaxDescLen = 512
)

type projectPackageRole struct {
	Role        string
	DisplayName string
	Description string
}

type rolePayload struct {
	RoleKey      string `json:"roleKey"`
	RoleKeySnake string `json:"role_key"`
	RoleName     string `json:"roleName"`
	Key          string `json:"key"`
	Name         string `json:"name"`
	Desc         string `json:"desc"`
	Role         string `json:"role"`
	DisplayName  string `json:"displayName"`
	Description  string `json:"description"`
}

func (p rolePayload) normalize() (projectPackageRole, error) {
	role := firstNonEmptyString(p.RoleKey, p.RoleKeySnake, p.Key, p.Role)
	name := firstNonEmptyString(p.RoleName, p.Name, p.DisplayName, role)
	desc := firstNonEmptyString(p.Description, p.Desc)

	if strings.TrimSpace(role) == "" {
		return projectPackageRole{}, domainproject.ErrUnsupportedPackageFormat
	}

	if utf8.RuneCountInString(name) > projectPackageRoleMaxNameLen {
		return projectPackageRole{}, domainproject.ErrUnsupportedPackageFormat
	}
	if utf8.RuneCountInString(desc) > projectPackageRoleMaxDescLen {
		return projectPackageRole{}, domainproject.ErrUnsupportedPackageFormat
	}

	return projectPackageRole{
		Role:        strings.TrimSpace(role),
		DisplayName: strings.TrimSpace(name),
		Description: strings.TrimSpace(desc),
	}, nil
}

func parseProjectAppRoles(raw []byte) ([]projectPackageRole, error) {
	raw = stripUTF8BOM(raw)
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}

	// 1. 数组对象格式：[{"roleKey":"...",...}]
	var objects []rolePayload
	if err := json.Unmarshal(raw, &objects); err == nil {
		return normalizeRolePayloads(objects)
	}

	// 2. 字符串数组格式：["role1","role2"]
	var keys []string
	if err := json.Unmarshal(raw, &keys); err == nil {
		roles := make([]projectPackageRole, 0, len(keys))
		for _, k := range keys {
			if key := strings.TrimSpace(k); key != "" {
				roles = append(roles, projectPackageRole{Role: key, DisplayName: key})
			}
		}
		return roles, nil
	}

	// 3. 对象格式：需要区分 {"roles":[...]} 与 {"roleKey":"name",...}
	var objectMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objectMap); err == nil {
		if rolesRaw, ok := objectMap["roles"]; ok {
			var wrapped []rolePayload
			if err := json.Unmarshal(rolesRaw, &wrapped); err == nil {
				return normalizeRolePayloads(wrapped)
			}
		}
		return normalizeRoleObjectMap(objectMap)
	}

	return nil, domainproject.ErrUnsupportedPackageFormat
}

func normalizeRoleObjectMap(objectMap map[string]json.RawMessage) ([]projectPackageRole, error) {
	keys := make([]string, 0, len(objectMap))
	for k := range objectMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	roles := make([]projectPackageRole, 0, len(keys))
	for _, k := range keys {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		name := key
		desc := ""
		var s string
		if err := json.Unmarshal(objectMap[k], &s); err == nil {
			name = firstNonEmptyString(strings.TrimSpace(s), key)
		} else {
			var obj struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				Description string `json:"description"`
				Desc        string `json:"desc"`
			}
			if err := json.Unmarshal(objectMap[k], &obj); err == nil {
				name = firstNonEmptyString(obj.Name, obj.DisplayName, key)
				desc = firstNonEmptyString(obj.Description, obj.Desc)
			}
		}
		role := projectPackageRole{
			Role:        key,
			DisplayName: strings.TrimSpace(name),
			Description: strings.TrimSpace(desc),
		}
		if utf8.RuneCountInString(role.DisplayName) > projectPackageRoleMaxNameLen {
			return nil, domainproject.ErrUnsupportedPackageFormat
		}
		if utf8.RuneCountInString(role.Description) > projectPackageRoleMaxDescLen {
			return nil, domainproject.ErrUnsupportedPackageFormat
		}
		roles = append(roles, role)
	}
	return roles, nil
}

func normalizeRolePayloads(payloads []rolePayload) ([]projectPackageRole, error) {
	roles := make([]projectPackageRole, 0, len(payloads))
	for _, p := range payloads {
		r, err := p.normalize()
		if err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, nil
}

func stripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
