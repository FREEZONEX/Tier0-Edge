// Package mcpaccess 定义 MCP 操作范围，不通过角色名称推断权限。
package mcpaccess

import "backend/internal/contextx"

type Definition struct {
	Scope     string
	Resources []string
	Writable  bool
}

var Definitions = []Definition{
	{"fleet.control.automate", []string{"control.automate"}, true},
	{"fleet.control.read", []string{"cluster.edge.manage", "control.read"}, false},
	{"fleet.control.write", []string{"cluster.edge.manage", "control.read", "control.execute", "control.write"}, true},
	{"fleet.control.grant", []string{"fleet.connection.manage", "control.read", "control.write", "control.manage"}, true},
	{"fleet.manage", []string{"cluster.edge.manage", "uns.source.manage", "uns.stream.manage", "uns.write"}, true},
	{"fleet.ssh", []string{"fleet.ssh.manage"}, true},
	{"fleet.local", []string{"fleet.container.view", "fleet.connection.manage", "uns.source.manage"}, true},
	{"uns.read", []string{"uns.read"}, false},
	{"uns.source", []string{"uns.source"}, false},
	{"uns.stream", []string{"uns.stream"}, false},
	{"uns.app.read", []string{"uns.app.read"}, false},
	{"uns.model.manage", []string{"uns.manage", "uns.write"}, true},
	{"uns.write", []string{"uns.write"}, true},
	{"uns.source.manage", []string{"uns.source.manage"}, true},
	{"uns.source.credentials", []string{"uns.source.credentials"}, true},
	{"uns.stream.manage", []string{"uns.stream.manage"}, true},
	{"uns.stream.deploy", []string{"uns.stream.deploy"}, true},
	{"uns.stream.execute", []string{"uns.stream.execute"}, true},
	{"uns.app.manage", []string{"uns.app.manage", "uns.app.read"}, true},
	{"uns.delete", []string{"uns.delete"}, true},
	{"offline_access", nil, false},
}

func Scopes() []string {
	out := []string{}
	for _, d := range Definitions {
		out = append(out, d.Scope)
	}
	return out
}
func Has(subject contextx.Subject, scope string) bool {
	for _, v := range subject.MCPScopes {
		if v == scope {
			return true
		}
	}
	return false
}
func Available(subject contextx.Subject) []string {
	out := []string{}
	for _, d := range Definitions {
		allowed := true
		// App data keys derive writes from explicit Collection grants/resources,
		// not the legacy personal-key permission tier (see UNS ResourceAllowed).
		if d.Writable && subject.AuthType == "apiKey" && subject.APIKeyPermission == "read_only" && subject.APIKeyAppID == "" {
			allowed = false
		}
		for _, key := range d.Resources {
			if !contextx.HasResource(subject, key) {
				allowed = false
			}
		}
		if allowed {
			out = append(out, d.Scope)
		}
	}
	return out
}
func Constrain(subject contextx.Subject, requested []string) contextx.Subject {
	available := Available(subject)
	effective := []string{}
	resources := map[string]struct{}{}
	for _, d := range Definitions {
		selected := false
		for _, v := range requested {
			if v == d.Scope {
				selected = true
			}
		}
		allowed := false
		for _, v := range available {
			if v == d.Scope {
				allowed = true
			}
		}
		if !selected || !allowed {
			continue
		}
		effective = append(effective, d.Scope)
		for _, key := range d.Resources {
			resources[key] = struct{}{}
		}
	}
	subject.MCPRestricted = true
	subject.MCPScopes = effective
	subject.ResourceKeys = resources
	return subject
}
func MutationScope(action, kind string) string {
	if action == "stream.publish" {
		return "uns.stream.manage"
	}
	if action == "record.check" || action == "record.read" || action == "record.query" {
		return "uns.read"
	}
	if action == "record.create" || action == "record.replace" || (action == "record.modify" || action == "record.update") || action == "record.delete" || action == "record.restore" || action == "sample.append" {
		return "uns.write"
	}
	switch kind {
	case "directory", "topic", "collection":
		return "uns.model.manage"
	case "source":
		return "uns.source.manage"
	case "stream":
		return "uns.stream.manage"
	case "app":
		return "uns.app.manage"
	}
	return ""
}
func AllowedMutation(subject contextx.Subject, action, kind string, managed bool) bool {
	if !subject.MCPRestricted {
		return true
	}
	if !Has(subject, MutationScope(action, kind)) {
		return false
	}
	if (action == "record.modify" || action == "record.update") && !Has(subject, "uns.read") {
		return false
	}
	if (action == "meta.archive" || action == "record.delete") && !Has(subject, "uns.delete") {
		return false
	}
	if managed && !Has(subject, "uns.source.manage") {
		return false
	}
	return true
}
func CanWrite(subject contextx.Subject) bool {
	for _, d := range Definitions {
		if d.Writable && d.Scope != "uns.delete" && Has(subject, d.Scope) {
			return true
		}
	}
	return false
}
