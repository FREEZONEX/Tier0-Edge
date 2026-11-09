package main

import (
	"backend/internal/nativeinstall"
	"backend/internal/nativeui"
	"backend/internal/runtimeenv"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

func main() {
	if runtimeenv.IdentityQuery() {
		return
	}
	yes := flag.Bool("yes", false, "uninstall without confirmation; retain all user data")
	worker := flag.Bool("worker", false, "internal detached uninstall worker")
	target := flag.String("target", "", "internal installation path")
	parent := flag.Uint("parent", 0, "internal parent process")
	flag.Parse()
	exe, err := os.Executable()
	if err != nil {
		fail(err, *yes)
	}
	root := filepath.Dir(exe)
	if *worker {
		root = *target
	} else if *target != "" || *parent != 0 {
		fail(fmt.Errorf("target/parent are internal worker options"), *yes)
	}
	if err = nativeinstall.UninstallPreflight(root); err != nil {
		fail(err, *yes)
	}
	if !*worker {
		if !*yes && !(&nativeui.Window{}).Ask("卸载 "+runtimeenv.ProductName(), "停止本套服务并卸载程序？\r\n\r\n"+root+"\r\n\r\n数据库、App、配置、日志和备份保留在原目录。") {
			return
		}
		// Windows locks running executable images. Run a private temporary copy;
		// it waits for this process to exit before removing the installed binary.
		raw, e := os.ReadFile(exe)
		if e != nil {
			fail(e, *yes)
		}
		f, e := os.CreateTemp("", "tier0-uninstall-*.exe")
		if e != nil {
			fail(e, *yes)
		}
		name := f.Name()
		_, e = f.Write(raw)
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			fail(e, *yes)
		}
		args := []string{"--worker", "--target", root, "--parent", fmt.Sprint(os.Getpid())}
		if *yes {
			args = append(args, "--yes")
		}
		cmd := exec.Command(name, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
		if e = cmd.Start(); e != nil {
			_ = os.Remove(name)
			fail(e, *yes)
		}
		fmt.Printf("Uninstall started (PID %d); retained data: %s\n", cmd.Process.Pid, root)
		_ = cmd.Process.Release()
		return
	}
	if *parent != 0 {
		h, e := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(*parent))
		if e == nil {
			state, waitErr := windows.WaitForSingleObject(h, 30000)
			windows.CloseHandle(h)
			if waitErr != nil || state != windows.WAIT_OBJECT_0 {
				fail(fmt.Errorf("waiting for uninstall launcher to exit"), *yes)
			}
		}
	}
	logPath := filepath.Join(root, "logs", "uninstall.log")
	if err = nativeinstall.SafePath(logPath); err != nil {
		fail(err, *yes)
	}
	if err = os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		fail(err, *yes)
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		fail(err, *yes)
	}
	defer log.Close()
	os.Stdout, os.Stderr = log, log
	if err = nativeinstall.Uninstall(root); err != nil {
		fail(err, *yes)
	}
	fmt.Println("Uninstalled; data retained:", root)
	if !*yes {
		nativeui.Info("卸载完成", "程序已卸载。数据库、App、配置和日志保留在：\r\n"+root)
	}
}

func fail(err error, quiet bool) {
	fmt.Fprintln(os.Stderr, err)
	if !quiet {
		nativeui.Error("卸载未完成", err.Error()+"\r\n保留现场，请排查后重试卸载。")
	}
	os.Exit(1)
}
