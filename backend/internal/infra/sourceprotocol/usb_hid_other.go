//go:build !windows

package sourceprotocol

import "os/exec"

func configureHIDProcess(*exec.Cmd) {}
