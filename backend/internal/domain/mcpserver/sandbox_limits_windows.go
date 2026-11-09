package mcpserver

import (
	"backend/internal/appruntime"
	"golang.org/x/sys/windows"
	"os/exec"
	"sync"
	"unsafe"
)

// Kernel memory/process limits are applied before the suspended child runs.
// Closing the parent-owned Job terminates the child, including parent crashes.
func startSandboxCommand(cmd *exec.Cmd, memory uint64) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	closeJob := func() { once.Do(func() { _ = windows.CloseHandle(job) }) }
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY | windows.JOB_OBJECT_LIMIT_JOB_MEMORY
	info.BasicLimitInformation.ActiveProcessLimit = 1
	info.ProcessMemoryLimit = uintptr(memory)
	info.JobMemoryLimit = uintptr(memory)
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		closeJob()
		return nil, err
	}
	if err = appruntime.StartCommandInJob(cmd, job); err != nil {
		closeJob()
		return nil, err
	}
	return closeJob, nil
}
func applySandboxLimits() error     { return nil }      // Parent applies the Job limits before execution.
func processRSS(int) (uint64, bool) { return 0, false } // Job memory limits are enforced by Windows.
