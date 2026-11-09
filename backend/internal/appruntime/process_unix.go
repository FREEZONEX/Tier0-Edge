//go:build linux || darwin

package appruntime

import (
	"os/exec"
	"sync"
	"syscall"
)

func startOwned(cmd *exec.Cmd) (func() error, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var once sync.Once
	var err error
	return func() error {
		once.Do(func() {
			err = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			if err == syscall.ESRCH {
				err = nil
			}
		})
		return err
	}, nil
}
