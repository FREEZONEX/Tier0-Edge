package main

import (
	"backend/internal/nativeui"
	"backend/internal/runtimeenv"
	"context"
	"encoding/json"
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
	rootFlag := flag.String("root", "", "persistent data directory (outside the app)")
	stop := flag.Bool("stop", false, "stop this installation and retain data")
	noBrowser := flag.Bool("no-browser", false, "do not open browser")
	status := flag.Bool("status-json", false, "read desktop status without starting services")
	service := flag.String("service", "backend", "selected desktop service")
	flag.Parse()
	if runtime.GOARCH != "arm64" {
		fmt.Fprintln(os.Stderr, "Apple Silicon ARM64 required")
		os.Exit(1)
	}
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	root := *rootFlag
	if root == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			panic(e)
		}
		root = filepath.Join(home, "Library", "Application Support", "Tier0 Edge")
	}
	if *status {
		p, e := nativeui.ReadProgress(root)
		if e != nil {
			p = nativeui.NewProgress()
			p.Phase = "unknown"
			p.Message = "等待启动状态"
		}
		if e = json.NewEncoder(os.Stdout).Encode(nativeui.Manager(root, filepath.Dir(exe), runtimeenv.ProductName(), *service, p)); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		panic(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		panic(err)
	}
	if *stop {
		err = os.WriteFile(filepath.Join(root, "stop.request"), []byte("stop"), 0600)
	} else {
		err = runManagedAt(context.Background(), root, filepath.Dir(exe), *noBrowser, nil)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
