package iam

import (
	domainlicense "backend/internal/domain/license"
	"backend/internal/repo"
)

func filterResourcesByLicense(resources []repo.Resource) []repo.Resource {
	licenseSvc := domainlicense.Default()
	if licenseSvc == nil {
		return resources
	}
	out := make([]repo.Resource, 0, len(resources))
	for _, item := range resources {
		children := filterResourcesByLicense(item.Children)
		allowed := licenseSvc.AllowResource(item.ResourceKey)
		if !allowed && len(children) == 0 {
			continue
		}
		item.Children = children
		out = append(out, item)
	}
	return out
}
