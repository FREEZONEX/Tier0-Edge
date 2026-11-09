//go:build linux

package mcpserver

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// sandboxAddressSpaceBytes 子进程虚拟地址空间上限（Go 运行时会预留较大的虚拟空间，RSS 另由父进程盯）。
const sandboxAddressSpaceBytes = 4 << 30

// applySandboxLimits 在子进程里收紧内核限额：不能写文件、不留 core、少量 fd、CPU 秒数封顶、no_new_privs。
func applySandboxLimits() error {
	limits := []struct {
		resource int
		value    uint64
	}{
		{unix.RLIMIT_AS, sandboxAddressSpaceBytes},
		// CPU 秒数只兜死循环之外的失控（墙钟超时由父进程掐），给足余量。
		{unix.RLIMIT_CPU, 120},
		{unix.RLIMIT_FSIZE, 0},
		{unix.RLIMIT_CORE, 0},
		{unix.RLIMIT_NOFILE, 32},
	}
	for _, limit := range limits {
		var current unix.Rlimit
		if err := unix.Getrlimit(limit.resource, &current); err != nil {
			return err
		}
		value := limit.value
		if current.Max < value {
			value = current.Max
		}
		if err := unix.Setrlimit(limit.resource, &unix.Rlimit{Cur: value, Max: value}); err != nil {
			return fmt.Errorf("set sandbox resource limit %d: %w", limit.resource, err)
		}
	}
	// no_new_privs 是线程属性，生产镜像（CGO_ENABLED=0）同步到全部 Go 线程；链入 cgo 时（本地测试二进制）
	// AllThreadsSyscall 返回 ENOTSUP，退回只设当前线程 —— goja 本身没有 exec 能力，这一层只是纵深防御。
	_, _, errno := syscall.AllThreadsSyscall6(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0, 0)
	switch errno {
	case 0:
		return nil
	case syscall.ENOTSUP:
		return unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)
	default:
		return errno
	}
}

// processRSS 读 /proc/<pid>/statm 的常驻集大小。
func processRSS(pid int) (uint64, bool) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return pages * uint64(os.Getpagesize()), true
}
