package unsv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"backend/internal/contextx"
)

// Fleet transports the existing document/lifecycle operations. It never adds a
// second configuration schema or execution engine and never impersonates a user.
type FleetRuntimeRequest struct {
	NodeID    string          `json:"nodeId"`
	Operation string          `json:"operation"`
	Data      json.RawMessage `json:"data"`
}
type FleetRuntimePacket struct {
	Operation     string          `json:"operation"`
	Data          json.RawMessage `json:"data"`
	Permissions   []string        `json:"permissions"`
	Actor         string          `json:"actor"`
	ReadOnly      bool            `json:"readOnly"`
	MCPRestricted bool            `json:"mcpRestricted"`
	MCPScopes     []string        `json:"mcpScopes"`
}

func fleetNativePermissions() []string {
	// Use the native resource contract, so adding a business resource does not
	// require a second Fleet permission list. Include secondary write guards.
	keys := map[string]bool{"uns.read": true, "uns.write": true, "uns.delete": true, "uns.manage": true}
	for _, operation := range ResourceOperations() {
		if operation.Permission != "" {
			keys[operation.Permission] = true
		}
	}
	permissions := make([]string, 0, len(keys))
	for key := range keys {
		permissions = append(permissions, key)
	}
	sort.Strings(permissions)
	return permissions
}

func fleetNativeOperation(name string) (ResourceOperation, bool) {
	if name == "sourceSubscriptionExchange" {
		return ResourceOperation{Name: name, Permission: "uns.source"}, true
	}
	// Catalog has no resource discriminator, but uses the same read permission
	// and audience filtering as the local UNS schema discovery entry point.
	if name == "catalog" || name == "transactionStatus" {
		return ResourceOperation{Name: name, Permission: "uns.read"}, true
	}
	if name == "recordTransactionExecute" {
		return ResourceOperation{Name: name, Permission: "uns.write", Mutation: "transaction"}, true
	}
	return FindResourceOperation(name)
}

func PrepareFleetRuntime(ctx context.Context, req FleetRuntimeRequest) (FleetRuntimePacket, error) {
	var packet FleetRuntimePacket
	op, ok := fleetNativeOperation(req.Operation)
	if !ok || !ResourceAllowed(ctx, op) {
		return packet, problem("FORBIDDEN", "native runtime operation not allowed")
	}
	if op.Request != "" {
		if _, err := validateResourceRequest(op, req.Data); err != nil {
			return packet, err
		}
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.UserID < 1 {
		return packet, problem("UNAUTHENTICATED", "user required")
	}
	packet = FleetRuntimePacket{Operation: req.Operation, Data: req.Data, Actor: fmt.Sprintf("%d/%d/%s", subject.UserID, subject.APIKeyID, subject.MCPGrantID), ReadOnly: subject.AuthType == "apiKey" && subject.APIKeyPermission == "read_only", MCPRestricted: subject.MCPRestricted}
	permissions := fleetNativePermissions()
	for _, permission := range permissions {
		if contextx.HasResource(subject, permission) {
			packet.Permissions = append(packet.Permissions, permission)
		}
	}
	// Preserve consent boundaries, but never send unrelated or administrator scopes.
	for _, scope := range subject.MCPScopes {
		if contains(permissions, scope) || scope == "uns.model.manage" {
			packet.MCPScopes = append(packet.MCPScopes, scope)
		}
	}
	return packet, nil
}
func FleetRuntimeTarget(packet FleetRuntimePacket) string {
	raw, _ := json.Marshal(packet)
	sum := sha256.Sum256(raw)
	return packet.Operation + ":" + hex.EncodeToString(sum[:])
}
func (s *Service) ExecuteFleetRuntime(ctx context.Context, peer, target string, packet FleetRuntimePacket) (any, error) {
	if peer == "" || target != FleetRuntimeTarget(packet) || len(packet.Actor) == 0 || len(packet.Actor) > 256 {
		return nil, problem("FORBIDDEN", "runtime request does not match signed capability")
	}
	if _, ok := fleetNativeOperation(packet.Operation); !ok {
		return nil, invalid("operation", "native operation required")
	}
	subject := contextx.Subject{AuthType: "fleetTask", FleetTask: &contextx.FleetTaskSubject{NodeID: peer, Scope: "runtime.execute", ResourceID: target, GrantID: packet.Actor}, ResourceKeys: map[string]struct{}{}, MCPRestricted: packet.MCPRestricted, MCPScopes: packet.MCPScopes}
	permissions := fleetNativePermissions()
	for _, permission := range packet.Permissions {
		if !contains(permissions, permission) {
			return nil, problem("FORBIDDEN", "invalid runtime permission")
		}
		subject.ResourceKeys[permission] = struct{}{}
	}
	if packet.ReadOnly {
		subject.AuthType = "apiKey"
		subject.APIKeyPermission = "read_only"
	}
	{
		// The center authenticates and constrains this caller. Its user/API key
		// is not an Edge IAM identity. Recheck the signed permission snapshot
		// within this bounded synchronous call, retaining MCP consent, redaction
		// and per-mutation permission checks. Never substitute a local user.
		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		execution := *s
		execution.refresh = func(check context.Context, actor contextx.Subject) (contextx.Subject, error) {
			if err := check.Err(); err != nil {
				return contextx.Subject{}, err
			}
			if actor.FleetTask == nil || actor.FleetTask.Scope != "runtime.execute" || actor.FleetTask.ResourceID != target || actor.FleetTask.NodeID != peer {
				return contextx.Subject{}, problem("FORBIDDEN", "invalid remote authorization snapshot")
			}
			return actor, nil
		}
		if packet.Operation == "sourceSubscriptionExchange" {
			return execution.exchangeSourceSubscription(contextx.WithSubject(bounded, subject), packet.Data)
		}
		return execution.Execute(contextx.WithSubject(bounded, subject), packet.Operation, packet.Data)
	}
}
