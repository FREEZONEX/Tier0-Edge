package runtimeenv

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"syscall"
)

func hideIdentityQuery(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
