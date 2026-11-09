package runtimeenv

import (
	"backend/internal/product"
	"runtime"
)

// BuildVersion and BuildRevision identify the source used by the package build.
var BuildVersion = "dev"
var BuildRevision = "dev"

func Edition() string { return "edge" }
func IsEdge() bool    { return Edition() == "edge" }

// ProductName is shared by APIs, launchers and installers. Edition is independent
// of the installation channel (native/docker) and the licensed capability tier.
func ProductName() string {
	return product.ProfileFor(Edition()).Name
}

func Identity() product.Identity {
	return product.Identity{Edition: Edition(), Platform: runtime.GOOS + "/" + runtime.GOARCH, Deployment: Mode()}
}
func ActivationRequired() bool { return product.ProfileFor(Edition()).ActivationRequired }
