package repo

import (
	"slices"
	"testing"
)

func TestAPIKeyStreamOperationRequiresFullPermission(t *testing.T) {
	for _, permission := range []string{"", "unknown", APIKeyPermissionRead, APIKeyPermissionWrite} {
		for _, scope := range []string{"uns.stream.execute", "control.read", "control.execute", "control.write", "control.manage", "stream.requests.read", "stream.requests.invoke", "stream.requests.write", "stream.requests.manage"} {
			if slices.Contains(APIKeyResourceKeysForPermission(permission), scope) {
				t.Fatalf("%q must not permit device controls through %q", permission, scope)
			}
		}
	}
	full := APIKeyResourceKeysForPermission(APIKeyPermissionFull)
	for _, required := range []string{"uns.stream.execute", "uns.read", "uns.write", "control.read", "control.execute", "control.write", "control.manage", "stream.requests.read", "stream.requests.invoke", "stream.requests.write", "stream.requests.manage"} {
		if !slices.Contains(full, required) {
			t.Fatalf("full access is missing %q", required)
		}
	}
}

func TestAgentOrchestrationScopesRemainOptIn(t *testing.T) {
	for _, permission := range []string{APIKeyPermissionRead, APIKeyPermissionWrite, APIKeyPermissionFull} {
		for _, scope := range []string{"uns.source", "uns.source.manage", "uns.source.credentials", "uns.stream", "uns.stream.manage", "uns.stream.deploy"} {
			if slices.Contains(APIKeyResourceKeysForPermission(permission), scope) {
				t.Fatalf("%s implicitly grants %s", permission, scope)
			}
		}
	}
}
