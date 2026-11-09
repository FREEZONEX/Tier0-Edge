//go:build !windows

package mcpserver

import "os/exec"

func startSandboxCommand(cmd *exec.Cmd, _ uint64) (func(), error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return func() { _ = cmd.Process.Kill() }, nil
}
