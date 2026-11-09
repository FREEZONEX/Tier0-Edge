package sourceprotocol

import (
	"os/exec"
	"syscall"
)

func configureHIDProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
