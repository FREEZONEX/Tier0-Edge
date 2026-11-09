package nativessh

import "syscall"

func terminalProcessAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true, Setctty: true}
}
