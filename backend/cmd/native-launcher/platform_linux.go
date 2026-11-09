package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

type processJob int

const nativeProductVersion = "linux-native-edge"
const nativeHTTPListenHost = "0.0.0.0"

func binaryName(s string) string { return s }
func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}
func opcuaSocket(dir, id string) string { return filepath.Join(dir, "o.sock") }
func configureCommand(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}
func newServiceJob() (processJob, error) { return 0, nil }
func closeJob(processJob) error          { return nil }
func startInJob(c *exec.Cmd, _ processJob, _ ...processJob) error {
	configureCommand(c)
	return c.Start()
}
func terminateService(s *managedService) error {
	err := syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
func openBrowser(url string) error { return exec.Command("xdg-open", url).Run() }
func lockInstallation(root, id string) (func(), error) {
	if os.Geteuid() == 0 {
		return nil, fmt.Errorf("run Tier0 Edge as a dedicated non-root user (PostgreSQL cannot run as root)")
	}
	if err := os.MkdirAll(filepath.Join(root, "state"), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(root, "state", "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("this installation is already running: %w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
func protectState(root string) error { return os.Chmod(filepath.Join(root, "state"), 0700) }
func hostIdentity() (string, error) {
	raw, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(raw))
	if len(id) != 32 {
		return "", fmt.Errorf("valid /etc/machine-id required")
	}
	return id, nil
}
