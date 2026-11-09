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
)

func main() {
	if runtimeenv.IdentityQuery() {
		return
	}
	stop := flag.Bool("stop", false, "stop this installation, retaining data")
	noBrowser := flag.Bool("no-browser", false, "do not open the browser")
	headless := flag.Bool("headless", false, "run supervisor without tray (diagnostics only)")
	background := flag.Bool("background", false, "start in tray while the installer displays progress")
	inspect := flag.String("inspect-root", "", "open a read-only service window for an existing installation")
	flag.Parse()
	executable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	root := filepath.Dir(executable)
	if *inspect != "" {
		if err := launchTray(*inspect, true, false, true); err != nil {
			nativeui.Error("服务状态", err.Error())
			os.Exit(1)
		}
		return
	}
	if *stop {
		if err := os.WriteFile(filepath.Join(root, "stop.request"), []byte("stop"), 0600); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		return
	}
	if !*headless {
		if err := launchTray(root, *noBrowser, *background, false); err != nil {
			nativeui.Error(runtimeenv.ProductName(), err.Error())
			os.Exit(1)
		}
		return
	}
	if err := run(root, *noBrowser); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Println("Startup failed:", err, "\nSee logs in", filepath.Join(root, "logs"))
		os.Exit(1)
	}
}
