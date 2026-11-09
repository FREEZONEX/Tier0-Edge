package main

import (
	"backend/internal/nativeui"
	"backend/internal/runtimeenv"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	if runtimeenv.IdentityQuery() {
		return
	}
	rootFlag := flag.String("root", "", "persistent data directory, separate from program files")
	stop := flag.Bool("stop", false, "stop this installation and retain data")
	noBrowser := flag.Bool("no-browser", true, "headless operation (default)")
	flag.IntVar(&requestedPortOffset, "port-offset", -1, "first install: add 0..40000 to all default service ports; retained across upgrades")
	flag.Parse()
	err := func() error {
		if runtime.GOARCH != "amd64" {
			return fmt.Errorf("Linux AMD64 required")
		}
		if os.Geteuid() == 0 {
			return fmt.Errorf("run as a non-root service user")
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		root := *rootFlag
		if root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			root = filepath.Join(home, ".local", "share", "tier0-edge")
		}
		if *stop {
			// Never create a fresh directory for a stop request.
			root, err = filepath.EvalSymlinks(root)
			if err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(root, "stop.request"), []byte("stop"), 0600)
		}
		if err = os.MkdirAll(root, 0700); err != nil {
			return err
		}
		root, err = filepath.Abs(root)
		if err != nil {
			return err
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return err
		}
		last := ""
		return runManagedAt(context.Background(), root, filepath.Dir(exe), *noBrowser, func(p nativeui.Progress) {
			if p.Message != last {
				fmt.Println(p.Phase + ": " + p.Message)
				last = p.Message
			}
		})
	}()
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
