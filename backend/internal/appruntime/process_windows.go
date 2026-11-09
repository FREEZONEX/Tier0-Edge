package appruntime

import (
	"errors"
	"golang.org/x/sys/windows"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

func startOwned(cmd *exec.Cmd) (func() error, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	var closeOnce sync.Once
	var closeErr error
	closeJob := func() error { closeOnce.Do(func() { closeErr = windows.CloseHandle(job) }); return closeErr }
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		closeJob()
		return nil, err
	}
	if err = StartCommandInJob(cmd, job); err != nil {
		closeJob()
		return nil, err
	}
	return closeJob, nil
}

// StartCommandInJob 在执行任何子进程代码之前完成 Job 归属，供 App 和本机服务共用。
func StartCommandInJob(cmd *exec.Cmd, job windows.Handle, childJobs ...windows.Handle) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW | windows.CREATE_SUSPENDED
	if err := cmd.Start(); err != nil {
		return err
	}
	fail := func(e error) error {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return e
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fail(err)
	}
	err = windows.AssignProcessToJobObject(job, process)
	if err == nil {
		for _, childJob := range childJobs {
			if err = windows.AssignProcessToJobObject(childJob, process); err != nil {
				break
			}
		}
	}
	windows.CloseHandle(process)
	if err != nil {
		return fail(err)
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fail(err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(cmd.Process.Pid) {
			continue
		}
		thread, e := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if e != nil {
			return fail(e)
		}
		_, e = windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		if e != nil {
			return fail(e)
		}
		return nil
	}
	return fail(errors.New("native App startup thread unavailable"))
}
