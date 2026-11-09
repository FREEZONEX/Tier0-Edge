package nativessh

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

func supported() error {
	return windows.NewLazySystemDLL("kernel32.dll").NewProc("CreatePseudoConsole").Find()
}

type windowsTerminal struct {
	in, out   *os.File
	job       windows.Handle
	mu        sync.Mutex
	console   windows.Handle
	closeOnce sync.Once
	done      chan struct{}
	code      uint32
}

func startTerminal(cols, rows uint32, command string) (terminal, error) {
	var inputRead, inputWrite, outputRead, outputWrite windows.Handle
	if err := windows.CreatePipe(&inputRead, &inputWrite, nil, 0); err != nil {
		return nil, err
	}
	defer windows.CloseHandle(inputRead)
	if err := windows.CreatePipe(&outputRead, &outputWrite, nil, 0); err != nil {
		windows.CloseHandle(inputWrite)
		return nil, err
	}
	defer windows.CloseHandle(outputWrite)
	t := &windowsTerminal{in: os.NewFile(uintptr(inputWrite), "terminal-input"), out: os.NewFile(uintptr(outputRead), "terminal-output"), done: make(chan struct{})}
	ok := false
	defer func() {
		if !ok {
			_ = t.Close()
		}
	}()
	if err := windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inputRead, outputWrite, 0, &t.console); err != nil {
		return nil, err
	}
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	defer attrs.Delete()
	if err = attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(t.console), unsafe.Sizeof(t.console)); err != nil {
		return nil, err
	}
	t.job, err = windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(t.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, err
	}
	shell, err := exec.LookPath("pwsh.exe")
	args := []string{"-NoLogo", "-NoProfile"}
	if err != nil {
		shell = os.Getenv("COMSPEC")
		args = []string{"/d"}
		if command != "" {
			args = append(args, "/s", "/c", command)
		}
	} else if command != "" {
		args = append(args, "-Command", command)
	}
	application, err := windows.UTF16PtrFromString(shell)
	if err != nil {
		return nil, err
	}
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{shell}, args...)))
	if err != nil {
		return nil, err
	}
	// Explicitly exclude the Agent's DB, MQTT and Fleet environment credentials.
	env := []string{}
	for _, name := range []string{"SYSTEMROOT", "WINDIR", "SYSTEMDRIVE", "COMSPEC", "PATH", "PATHEXT", "TEMP", "TMP", "USERPROFILE", "USERNAME", "USERDOMAIN", "APPDATA", "LOCALAPPDATA", "PROGRAMDATA", "PROGRAMFILES", "PROGRAMFILES(X86)", "NUMBER_OF_PROCESSORS"} {
		if value, exists := os.LookupEnv(name); exists {
			env = append(env, name+"="+value)
		}
	}
	env = append(env, "TERM=xterm-256color")
	environment := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	dir, err := windows.UTF16PtrFromString(os.Getenv("USERPROFILE"))
	if err != nil {
		return nil, err
	}
	si := windows.StartupInfoEx{}
	si.Cb = uint32(unsafe.Sizeof(si))
	// Do not inherit the Agent's redirected log handles. ConPTY supplies
	// the console handles when these explicit standard handles are null.
	si.Flags = windows.STARTF_USESTDHANDLES
	si.ProcThreadAttributeList = attrs.List()
	var pi windows.ProcessInformation
	err = windows.CreateProcess(application, line, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_SUSPENDED, &environment[0], dir, &si.StartupInfo, &pi)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(pi.Thread)
	if err = windows.AssignProcessToJobObject(t.job, pi.Process); err != nil {
		windows.TerminateProcess(pi.Process, 1)
		windows.CloseHandle(pi.Process)
		return nil, err
	}
	if _, err = windows.ResumeThread(pi.Thread); err != nil {
		windows.TerminateProcess(pi.Process, 1)
		windows.CloseHandle(pi.Process)
		return nil, err
	}
	go func() {
		_, _ = windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		t.code = 1
		_ = windows.GetExitCodeProcess(pi.Process, &t.code)
		_ = windows.CloseHandle(pi.Process)
		t.closeConsole() // Output is drained concurrently by the SSH session.
		close(t.done)
	}()
	ok = true
	return t, nil
}

func (t *windowsTerminal) Read(p []byte) (int, error)  { return t.out.Read(p) }
func (t *windowsTerminal) Write(p []byte) (int, error) { return t.in.Write(p) }
func (t *windowsTerminal) Wait() uint32                { <-t.done; return t.code }
func (t *windowsTerminal) Resize(cols, rows uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.console == 0 {
		return os.ErrClosed
	}
	return windows.ResizePseudoConsole(t.console, windows.Coord{X: int16(cols), Y: int16(rows)})
}
func (t *windowsTerminal) closeConsole() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.console != 0 {
		windows.ClosePseudoConsole(t.console)
		t.console = 0
	}
}
func (t *windowsTerminal) Close() error {
	t.closeOnce.Do(func() {
		if t.job != 0 {
			_ = windows.CloseHandle(t.job)
		}
		_ = t.in.Close()
		_ = t.out.Close()
		t.closeConsole()
	})
	return nil
}
