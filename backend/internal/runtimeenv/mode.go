// Package runtimeenv describes how this Enterprise installation is hosted.
package runtimeenv

import (
	"os"
	"strings"
)

const Native = "native"
const Docker = "docker"

// BuildDeployment is set for release artifacts. Runtime env is only a development fallback.
var BuildDeployment string

// Mode defaults to the existing Docker deployment when the launcher has not
// explicitly selected native mode. It is independent of OS and license tier.
func Mode() string {
	if BuildDeployment != "" {
		if BuildDeployment == Native {
			return Native
		}
		return Docker
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TIER0_RUNTIME_MODE")), Native) {
		return Native
	}
	return Docker
}

func IsNative() bool { return Mode() == Native }
