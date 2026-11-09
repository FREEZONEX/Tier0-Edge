// Package product owns product policy, independently of OS and service health.
package product

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

//go:embed catalog.json
var source embed.FS

type Profile struct {
	Name               string            `json:"name"`
	ActivationRequired bool              `json:"activationRequired"`
	Capabilities       []string          `json:"capabilities"`
	Components         []string          `json:"components"`
	Adapters           map[string]string `json:"adapters"`
}
type Definition struct {
	LicenseFeature string   `json:"licenseFeature"`
	Resources      []string `json:"resources"`
	Pages          []string `json:"pages"`
}
type Component struct {
	Dependencies []string `json:"dependencies"`
	Protection   string   `json:"protection,omitempty"`
}
type Target struct {
	Editions       []string          `json:"editions"`
	PackageBackend string            `json:"packageBackend"`
	Unavailable    map[string]string `json:"unavailable"`
}
type Catalog struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Base          Profile               `json:"base"`
	Editions      map[string]Profile    `json:"editions"`
	Capabilities  map[string]Definition `json:"capabilities"`
	Components    map[string]Component  `json:"components"`
	Targets       map[string]Target     `json:"targets"`
}

var catalog = func() Catalog {
	raw, err := source.ReadFile("catalog.json")
	if err != nil {
		panic(err)
	}
	var c Catalog
	if err := json.Unmarshal(raw, &c); err != nil {
		panic(err)
	}
	return c
}()

type Identity struct {
	Edition    string `json:"edition"`
	Platform   string `json:"platform"`
	Deployment string `json:"deployment"`
}
type Capability struct {
	ProductAllowed        bool     `json:"productAllowed"`
	LicenseAllowed        bool     `json:"licenseAllowed"`
	InstallationSupported bool     `json:"installationSupported"`
	Enabled               bool     `json:"enabled"`
	ReasonCode            string   `json:"reasonCode,omitempty"`
	Pages                 []string `json:"pages"`
}

func ProfileFor(edition string) Profile {
	p, ok := catalog.Editions[edition]
	if !ok {
		p = catalog.Editions["edge"]
	}
	p.Capabilities = append(append([]string{}, catalog.Base.Capabilities...), p.Capabilities...)
	p.Components = append(append([]string{}, catalog.Base.Components...), p.Components...)
	overrides := p.Adapters
	p.Adapters = map[string]string{}
	for k, v := range catalog.Base.Adapters {
		p.Adapters[k] = v
	}
	for k, v := range overrides {
		p.Adapters[k] = v
	}
	return p
}

// Resolve only evaluates policy/support. A stopped process never changes this result.
// The caller supplies verified License grants; it cannot widen the product set.
func Resolve(id Identity, activated bool, allowFeature func(string) bool) map[string]Capability {
	p := ProfileFor(id.Edition)
	allowed := map[string]bool{}
	for _, k := range p.Capabilities {
		allowed[k] = true
	}
	target, supported := catalog.Targets[id.Platform+"/"+id.Deployment]
	supported = supported && slices.Contains(target.Editions, id.Edition)
	result := make(map[string]Capability, len(catalog.Capabilities))
	for key, def := range catalog.Capabilities {
		c := Capability{ProductAllowed: allowed[key], LicenseAllowed: !p.ActivationRequired || activated, InstallationSupported: supported, Pages: append([]string{}, def.Pages...)}
		if def.LicenseFeature != "" {
			c.LicenseAllowed = c.LicenseAllowed && allowFeature != nil && allowFeature(def.LicenseFeature)
		}
		reason := target.Unavailable[key]
		if reason != "" {
			c.InstallationSupported = false
		}
		switch {
		case !c.ProductAllowed:
			c.ReasonCode = "PRODUCT_CAPABILITY_DISABLED"
		case !supported:
			c.ReasonCode = "INSTALLATION_TARGET_UNSUPPORTED"
		case !c.InstallationSupported:
			c.ReasonCode = reason
		case !c.LicenseAllowed:
			c.ReasonCode = "LICENSE_CAPABILITY_REQUIRED"
		default:
			c.Enabled = true
		}
		result[key] = c
	}
	return result
}

// ResourceCapability uses the most specific prefix so uns.source isn't mistaken for uns.
func ResourceCapability(resource string) string {
	resource = strings.ToLower(strings.TrimSpace(resource))
	key, length := "", 0
	for k, d := range catalog.Capabilities {
		for _, prefix := range d.Resources {
			if (resource == strings.TrimSuffix(prefix, ".") || strings.HasPrefix(resource, prefix)) && len(prefix) > length {
				key, length = k, len(prefix)
			}
		}
	}
	return key
}
func Service(id string) Component {
	c := catalog.Components[id]
	c.Dependencies = append([]string{}, c.Dependencies...)
	return c
}
func Components(edition string) ([]string, error) {
	visited, pending := map[string]bool{}, map[string]bool{}
	out := []string{}
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		if pending[id] {
			return fmt.Errorf("component dependency cycle: %s", id)
		}
		c, ok := catalog.Components[id]
		if !ok {
			return fmt.Errorf("unknown component: %s", id)
		}
		pending[id] = true
		for _, d := range c.Dependencies {
			if err := visit(d); err != nil {
				return err
			}
		}
		delete(pending, id)
		visited[id] = true
		out = append(out, id)
		return nil
	}
	for _, id := range ProfileFor(edition).Components {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}
