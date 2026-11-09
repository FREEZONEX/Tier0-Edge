package fleetapi

import (
	"backend/internal/contextx"
	"backend/internal/mcpaccess"
	"strings"
)

// OperationAccess is shared by HTTP/MCP execution and tool discovery. Detailed
// domain permissions are additionally checked by the operation itself.
func OperationAccess(op string, subject contextx.Subject) (permission, scope string) {
	permission = "cluster.edge.manage"
	if strings.HasPrefix(op, "ssh") {
		permission = "fleet.ssh.manage"
	}
	if op == "connectionUsers" {
		permission = "iam.user.view"
	}
	if strings.HasPrefix(op, "localStorage") || strings.HasPrefix(op, "controlGrants") || op == "connectionConfigure" || strings.HasPrefix(op, "connectionAccess") || op == "connectionCollectionsSave" {
		permission = "fleet.connection.manage"
	} else if op == "currentGet" {
		permission = "fleet.container.view"
	}
	if strings.HasPrefix(op, "localStorage") {
		permission = "uns.read"
		if op == "localStorageSave" {
			permission = "uns.write"
		}
	}
	scope = "fleet.manage"
	if permission != "cluster.edge.manage" {
		scope = "fleet.local"
	}
	if strings.HasPrefix(op, "ssh") {
		scope = "fleet.ssh"
	}
	if strings.HasPrefix(op, "controlGrants") {
		scope = "fleet.control.grant"
	}
	if op == "controlsCatalog" || strings.HasPrefix(op, "controlIntents") {
		scope = "fleet.control.read"
		if op == "controlIntentsCreate" || op == "controlIntentsConfirm" || mcpaccess.Has(subject, "fleet.control.write") {
			scope = "fleet.control.write"
		}
	}
	return permission, scope
}
