//go:build darwin || linux

package nativessh

import (
	"errors"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

const unixShell = "/bin/bash"

func supported() error {
	if os.Geteuid() == 0 {
		return errors.New("SSH_NATIVE_ROOT_UNSUPPORTED")
	}
	shell, err := os.Stat(unixShell)
	if err != nil || !shell.Mode().IsRegular() || shell.Mode().Perm()&0111 == 0 {
		return errors.New("SSH_NATIVE_SHELL_UNAVAILABLE")
	}
	master, slave, err := pty.Open()
	if err != nil {
		return err
	}
	_ = master.Close()
	_ = slave.Close()
	return nil
}

type unixTerminal struct {
	*os.File
	cmd  *exec.Cmd
	done chan struct{}
	code uint32
}

func startTerminal(cols, rows uint32, command string) (terminal, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	args := []string{"--noprofile", "--norc"}
	if command == "" {
		args = append(args, "-i")
	} else {
		args = append(args, "-c", command)
	}
	cmd := exec.Command(unixShell, args...)
	cmd.Dir = home
	// The agent environment includes credentials for other services.
	cmd.Env = []string{"HOME=" + home, "SHELL=" + unixShell,
		"PATH=/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin", "TERM=xterm-256color"}
	for _, name := range []string{"USER", "LOGNAME", "LANG", "LC_ALL", "LC_CTYPE"} {
		if value, ok := os.LookupEnv(name); ok {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	master, err := pty.StartWithAttrs(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}, terminalProcessAttrs())
	if err != nil {
		return nil, err
	}
	t := &unixTerminal{File: master, cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		code := cmd.ProcessState.ExitCode()
		if code < 0 {
			code = 1
		}
		t.code = uint32(code)
		close(t.done)
	}()
	return t, nil
}

func (t *unixTerminal) Resize(cols, rows uint32) error {
	return pty.Setsize(t.File, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (t *unixTerminal) Wait() uint32 {
	<-t.done
	return t.code
}

func (t *unixTerminal) Close() error {
	// Closing the controlling PTY hangs up the foreground job on disconnect.
	err := t.File.Close()
	_ = t.cmd.Process.Kill()
	return err
}
