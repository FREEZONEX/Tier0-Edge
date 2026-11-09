// Package license evaluates the public Edge capabilities used by shared services.
package license

import (
	"strings"

	"backend/internal/product"
	"backend/internal/runtimeenv"
)

type Service struct{}

var edge Service

func Default() *Service { return &edge }

func (*Service) IsHost() bool                  { return false }
func (*Service) CenterManagementEnabled() bool { return false }
func (*Service) BranchLimit() int64            { return 0 }

// Edge does not impose commercial user or App quotas. RBAC is evaluated separately.
func (*Service) CheckQuota(string, int64) error { return nil }

func (*Service) Capabilities() map[string]product.Capability {
	return product.Resolve(runtimeenv.Identity(), true, func(feature string) bool {
		return feature == "audit" || feature == "launchpad"
	})
}

func (s *Service) CapabilityAllowed(name string) bool {
	return s.Capabilities()[name].Enabled
}

func (s *Service) AllowResource(resource string) bool {
	resource = strings.TrimSpace(strings.ToLower(resource))
	switch {
	case strings.HasPrefix(resource, "siem."), strings.HasPrefix(resource, "appbuilder."), strings.HasPrefix(resource, "app.builder."):
		return false
	}
	key := product.ResourceCapability(resource)
	return key == "" || s.CapabilityAllowed(key)
}

func (s *Service) FilterResourceKeys(keys []string) []string {
	filtered := make([]string, 0, len(keys))
	for _, key := range keys {
		if s.AllowResource(key) {
			filtered = append(filtered, key)
		}
	}
	return filtered
}
