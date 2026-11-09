package main

import (
	"backend/internal/appruntime"
	"backend/internal/runtimeenv"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type processJob = windows.Handle

func prepareInstallation(root, programRoot string) (func() error, func(error), error) {
	return func() error { return nil }, func(error) {}, nil
}

const nativeHTTPListenHost = "127.0.0.1"

func productVersion(_ string) string { return runtimeenv.BuildVersion }

func binaryName(s string) string             { return s + ".exe" }
func terminationSignals() []os.Signal        { return []os.Signal{os.Interrupt} }
func opcuaSocket(_ string, id string) string { return `\\.\pipe\Tier0-` + id + "-opcua" }
func configureCommand(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
func startInJob(c *exec.Cmd, j processJob, children ...processJob) error {
	return appruntime.StartCommandInJob(c, j, children...)
}
func closeJob(j processJob) error              { return windows.CloseHandle(j) }
func terminateService(s *managedService) error { return windows.TerminateJobObject(s.job, 0) }
func openBrowser(base string) error {
	v, _ := windows.UTF16PtrFromString("open")
	u, _ := windows.UTF16PtrFromString(base)
	return windows.ShellExecute(0, v, u, nil, nil, windows.SW_SHOWNORMAL)
}
func lockInstallation(root, id string) (func(), error) {
	name, _ := windows.UTF16PtrFromString(`Local\Tier0Native-` + id)
	mutex, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if mutex != 0 {
			windows.CloseHandle(mutex)
		}
		return nil, fmt.Errorf("this installation is already running")
	}
	if err != nil {
		return nil, err
	}
	return func() { windows.CloseHandle(mutex) }, nil

}
func protectState(root string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	acl := exec.Command("icacls.exe", filepath.Join(root, "state"), "/inheritance:r", "/grant:r", "*"+u.Uid+":(OI)(CI)F", "*S-1-5-18:(OI)(CI)F")
	acl.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := acl.CombinedOutput(); err != nil {
		return fmt.Errorf("protect private state: %s", out)
	}

	return nil
}
func hostIdentity() (string, error) {
	machineKey, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return "", fmt.Errorf("read Windows installation identity: %w", err)
	}
	machineID, _, identityErr := machineKey.GetStringValue("MachineGuid")
	machineKey.Close()
	if identityErr != nil || strings.TrimSpace(machineID) == "" {
		return "", fmt.Errorf("Windows installation identity unavailable")
	}

	return machineID, nil
}
func newServiceJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
