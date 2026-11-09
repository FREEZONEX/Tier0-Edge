//go:build !windows

package runtimeenv

import "os/exec"

func hideIdentityQuery(_ *exec.Cmd) {}
