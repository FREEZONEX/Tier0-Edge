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

const nativeProductVersion = "macos-native-edge"
const nativeHTTPListenHost = "0.0.0.0"

func binaryName(s string) string { return s }
func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}
func opcuaSocket(dir, id string) string  { return filepath.Join(dir, "o.sock") }
func configureCommand(c *exec.Cmd)       { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
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
func openBrowser(url string) error { return exec.Command("/usr/bin/open", url).Run() }
func lockInstallation(root, id string) (func(), error) {
	if os.Geteuid() == 0 {
		return nil, fmt.Errorf("run as the installing user, not root")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	control := filepath.Join(cache, "Tier0 Edge")
	if err = os.MkdirAll(control, 0700); err != nil {
		return nil, err
	}
	global, err := os.OpenFile(filepath.Join(control, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(global.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		global.Close()
		return nil, fmt.Errorf("another Tier0 Edge instance is running; stop it before launching or upgrading")
	}
	keepGlobal := false
	defer func() {
		if !keepGlobal {
			global.Close()
		}
	}()
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
	keepGlobal = true
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		_ = syscall.Flock(int(global.Fd()), syscall.LOCK_UN)
		_ = global.Close()
	}, nil
}
func protectState(root string) error { return os.Chmod(filepath.Join(root, "state"), 0700) }
func hostIdentity() (string, error) {
	raw, err := exec.Command("/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, `"IOPlatformUUID"`) {
			_, v, ok := strings.Cut(line, "=")
			if ok {
				return strings.Trim(strings.TrimSpace(v), `"`), nil
			}
		}
	}
	return "", fmt.Errorf("macOS platform UUID unavailable")
}
