package main

import (
	"backend/internal/nativeui"
	"backend/internal/runtimeenv"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

func launchTray(root string, noBrowser bool, background bool, readOnly bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	sum := sha256.Sum256([]byte(strings.ToLower(root)))
	id := hex.EncodeToString(sum[:6])
	if readOnly {
		id += "-observer"
	}
	className := "Tier0NativeTray-" + id
	mutexName, _ := windows.UTF16PtrFromString(`Local\Tier0NativeTray-` + id)
	mutex, err := windows.CreateMutex(nil, false, mutexName)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if mutex != 0 {
			windows.CloseHandle(mutex)
		}
		nativeui.ActivateWindow(className)
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(mutex)
	log := os.Stderr
	if !readOnly {
		if err = os.MkdirAll(filepath.Join(root, "logs"), 0700); err != nil {
			return err
		}
		log, err = os.OpenFile(filepath.Join(root, "logs", "launcher.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer log.Close()
		os.Stdout = log
		os.Stderr = log
	}
	window, err := nativeui.NewWindow(runtimeenv.ProductName()+" · 服务管理", className, filepath.Join(root, "tier0.ico"), 980, 700)
	if err != nil {
		return err
	}
	window.ManagerTheme()
	window.ManagerLabel("", 0, 0, 240, 640, 15, false, nativeui.Ink, nativeui.Surface)
	window.ManagerIcon(24, 28, 32)
	window.ManagerLabel(runtimeenv.ProductName(), 68, 30, 165, 30, 21, true, nativeui.Ink, nativeui.Surface)
	window.ManagerLabel("本机服务", 24, 100, 190, 25, 14, false, nativeui.Muted, nativeui.Surface)
	selected := "backend"
	serviceButtons := map[string]nativeui.Control{}
	serviceIDs := []string{"backend", "postgres", "nats", "mqtt", "opcua", "modbus", "executor", "fleet", "apps"}
	for i, id := range serviceIDs {
		serviceButtons[id] = window.ManagerButton(id, 200+i, 14, 139+i*44, 212, 38, false)
	}
	version := window.ManagerLabel("", 24, 601, 190, 24, 13, false, nativeui.Muted, nativeui.Surface)
	title := window.ManagerLabel("正在启动…", 276, 34, 430, 40, 28, true, nativeui.Ink, nativeui.White)
	description := window.ManagerLabel("", 276, 81, 430, 25, 15, false, nativeui.Muted, nativeui.White)
	openButton := window.ManagerButton("打开平台", 1, 810, 38, 140, 40, true)
	statusLabel := window.ManagerLabel("", 276, 123, 650, 30, 15, true, nativeui.Green, nativeui.White)
	endpoint := window.ManagerLabel("", 276, 165, 650, 27, 15, false, nativeui.Ink, nativeui.White)
	address := window.ManagerLabel("", 276, 204, 500, 27, 15, false, nativeui.Muted, nativeui.White)
	window.ManagerButton("复制地址", 8, 824, 198, 126, 34, false)
	message := window.ManagerLabel("", 276, 242, 674, 48, 14, false, nativeui.Muted, nativeui.White)
	window.ManagerLabel("最近日志", 276, 303, 300, 30, 20, true, nativeui.Ink, nativeui.White)
	window.ManagerButton("打开日志文件", 9, 806, 300, 144, 34, false)
	details := window.TextArea("", 100, 276, 345, 674, 212)
	window.Style(details, 13, false, nativeui.Ink, nativeui.Surface, "Consolas")
	window.ManagerButton("数据目录", 10, 276, 581, 118, 34, false)
	window.ManagerButton("日志文件夹", 5, 406, 581, 118, 34, false)
	window.ManagerLabel("关闭窗口后继续在后台运行", 276, 649, 320, 24, 13, false, nativeui.Muted, nativeui.White)
	summary := window.ManagerLabel("正在启动", 24, 648, 230, 26, 14, true, nativeui.Green, nativeui.White)
	startButton := window.ManagerButton("启动全部服务", 3, 626, 641, 142, 38, false)
	stopButton := window.ManagerButton("停止全部服务", 4, 780, 641, 142, 38, false)
	window.ManagerButton("…", 11, 932, 641, 32, 38, false)
	var current nativeui.ManagerSnapshot
	var lastRefresh time.Time
	var mu sync.Mutex
	state := nativeui.NewProgress()
	running := false
	var cancel context.CancelFunc
	var runDone chan struct{}
	exiting := false
	manualShow := false
	lastPhase := ""
	var readyAt time.Time
	silent := background
	notify := func(p nativeui.Progress) { mu.Lock(); state = p; mu.Unlock(); window.Wake() }
	start := func(show bool) {
		mu.Lock()
		if running {
			mu.Unlock()
			return
		}
		running = true
		state = nativeui.NewProgress()
		ctx, c := context.WithCancel(context.Background())
		cancel = c
		done := make(chan struct{})
		runDone = done
		mu.Unlock()
		manualShow = false
		silent = !show
		readyAt = time.Time{}
		if show {
			window.Show()
		}
		go func() {
			defer close(done)
			err := runManaged(ctx, root, noBrowser, notify)
			if err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintln(log, "Supervisor failed:", err)
			}
			mu.Lock()
			if err != nil && !errors.Is(err, context.Canceled) {
				state.Phase = "failed"
				state.Error = err.Error()
				state.Message = "启动失败，请查看日志后重试"
			}
			running = false
			cancel = nil
			mu.Unlock()
			window.Wake()
		}()
	}
	defer func() {
		mu.Lock()
		c := cancel
		done := runDone
		mu.Unlock()
		if c != nil {
			c()
		}
		if done != nil {
			select {
			case <-done:
			case <-time.After(30 * time.Second):
			}
		}
	}()
	stop := func() {
		mu.Lock()
		c := cancel
		if running && c != nil {
			state.Phase = "stopping"
			state.Message = "正在停止服务并关闭数据库，请稍候…"
			c()
		}
		mu.Unlock()
		window.Wake()
	}
	openPlatform := func() {
		if err := nativeui.Open(installationURL(root)); err != nil {
			nativeui.Error(runtimeenv.ProductName(), err.Error())
		}
	}
	command := func(command int) {
		if readOnly && (command == 3 || command == 4) {
			return
		}
		if readOnly && command == 6 {
			window.Destroy()
			return
		}
		switch command {
		case 1:
			openPlatform()
		case 2:
			manualShow = true
			silent = false
			window.Show()
		case 3:
			start(true)
		case 4:
			stop()
		case 5:
			if err := nativeui.Open(filepath.Join(root, "logs")); err != nil {
				nativeui.Error(runtimeenv.ProductName(), err.Error())
			}
		case 6:
			if window.Ask("退出 "+runtimeenv.ProductName(), "退出将停止本套 Tier0 和 App，数据会保留。确定退出？") {
				exiting = true
				stop()
				window.Show()
				window.Wake()
			}
		case 7:
			window.Hide()
		case 8:
			if !nativeui.CopyText(window.Handle, current.URL) {
				nativeui.Error("复制地址", "剪贴板暂时不可用，请重试")
			}
		case 9:
			if current.LogFile != "" {
				if e := nativeui.Open(current.LogFile); e != nil {
					nativeui.Error("日志", e.Error())
				}
			}
		case 10:
			if e := nativeui.Open(root); e != nil {
				nativeui.Error("数据目录", e.Error())
			}
		}
		if command >= 200 && command < 200+len(serviceIDs) {
			selected = serviceIDs[command-200]
			lastRefresh = time.Time{}
			window.Wake()
		}
	}
	window.OnCommand = func(id int) {
		if id == 11 {
			quitLabel := "退出并停止服务"
			if readOnly {
				quitLabel = "关闭状态窗口"
			}
			command(window.Popup([]nativeui.MenuItem{{ID: 7, Text: "隐藏到托盘"}, {ID: 6, Text: quitLabel}}))
			return
		}
		command(id)
	}
	window.OnActivate = func() { command(2) }
	window.OnClose = window.Hide
	menu := func() {
		mu.Lock()
		active := running
		phase := state.Phase
		mu.Unlock()
		quitLabel := "退出并停止服务"
		if readOnly {
			quitLabel = "关闭状态窗口"
			phase = current.Phase
		}
		command(window.Popup([]nativeui.MenuItem{{ID: 1, Text: "打开 " + runtimeenv.ProductName(), Disabled: phase != "ready"}, {ID: 2, Text: "服务状态"}, {Separator: true}, {ID: 3, Text: "启动服务", Disabled: readOnly || active || exiting}, {ID: 4, Text: "停止服务", Disabled: readOnly || !active || phase == "stopping"}, {ID: 5, Text: "打开日志文件夹"}, {Separator: true}, {ID: 6, Text: quitLabel}}))
	}
	window.OnContextMenu = menu
	window.OnTray = func(event uint32) {
		switch event {
		case 0x203:
			openPlatform()
		case 0x202, 0x400, 0x401:
			manualShow = true
			silent = false
			window.Show()
		case 0x205, 0x7b:
			menu()
		}
	}
	window.OnTick = func() {
		mu.Lock()
		p := state
		active := running
		mu.Unlock()
		if readOnly {
			var e error
			p, e = nativeui.ReadProgress(root)
			if e != nil {
				p = nativeui.NewProgress()
				p.Phase = "unknown"
				p.Error = "无法读取运行状态"
			}
			if e == nil && p.Phase != "stopped" && p.Phase != "failed" {
				h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p.PID))
				var code uint32
				if err == nil {
					err = windows.GetExitCodeProcess(h, &code)
					windows.CloseHandle(h)
				}
				if err != nil || code != 259 {
					p.Phase = "unknown"
					p.Error = "监督进程不可用，当前状态无法确认"
					for i := range p.Services {
						p.Services[i].State = "unknown"
					}
				}
			}
		}
		if time.Since(lastRefresh) > time.Second {
			current = nativeui.Manager(root, root, runtimeenv.ProductName(), selected, p)
			version.SetText("v" + current.Version)
			title.SetText(current.Selected.Label)
			description.SetText(current.Description)
			statusLabel.SetText(nativeui.StateLabel(current.Selected.State))
			stateColor := nativeui.Muted
			if current.Selected.State == "ready" || current.Selected.State == "running" {
				stateColor = nativeui.Green
			}
			if current.Selected.State == "failed" {
				stateColor = 0x4545ca
			}
			window.Tint(statusLabel, stateColor)
			endpoint.SetText("服务端点    " + current.Endpoint)
			address.SetText("平台地址    " + current.URL)
			info := current.Selected.Message
			if p.Error != "" {
				info = "运行异常：" + p.Error
			}
			if p.Phase != "ready" && p.Error == "" {
				info = p.Message
			}
			message.SetText(info)
			if readOnly {
				message.SetText("只读查看 · " + info)
			}
			summary.SetText(current.Summary())
			if p.Phase == "failed" {
				window.Tint(summary, 0x4545ca)
			} else if p.Phase == "ready" {
				window.Tint(summary, nativeui.Green)
			} else {
				window.Tint(summary, nativeui.Muted)
			}
			details.TailText(strings.ReplaceAll(current.Log, "\n", "\r\n"))
			for _, s := range p.Services {
				if b, ok := serviceButtons[s.ID]; ok {
					label := s.Label
					if s.ID == "postgres" {
						label = "PostgreSQL"
					}
					if s.ID == "apps" {
						label = "App Runtime"
					}
					if s.ID == "backend" {
						label = "Tier0 Core"
					}
					b.SetText(label + "  ·  " + nativeui.StateLabel(s.State))
					window.SelectButton(b, s.ID == selected)
					window.ServiceState(b, s.State)
				}
			}
			lastRefresh = time.Now()
		}
		openButton.Enable(p.Phase == "ready")
		startButton.Enable(!readOnly && !active && !exiting)
		stopButton.Enable(!readOnly && active && p.Phase != "stopping")
		if p.Phase != lastPhase {
			window.TrayTip(runtimeenv.ProductName() + " · " + map[string]string{"starting": "启动中", "ready": "已就绪", "stopping": "正在停止", "stopped": "已停止", "failed": "运行异常"}[p.Phase])
			if p.Phase == "failed" && !silent {
				window.Show()
			}
			if p.Phase == "ready" {
				readyAt = time.Now()
			}
			lastPhase = p.Phase
		}
		if !readOnly && p.Phase == "ready" && !manualShow && !readyAt.IsZero() && time.Since(readyAt) > 2*time.Second {
			window.Hide()
			readyAt = time.Time{}
		}
		if exiting && !active {
			window.Destroy()
		}
	}
	if err = window.AddTray(root, runtimeenv.ProductName()+" · 启动中"); err != nil {
		window.Destroy()
		return err
	}
	if !background {
		window.Show()
	}
	if !readOnly {
		start(!background)
	}
	return window.Loop()
}

func installationURL(root string) string {
	port := 18080
	if raw, err := os.ReadFile(filepath.Join(root, "state", "environment.json")); err == nil {
		var settings map[string]string
		if json.Unmarshal(raw, &settings) == nil {
			if value, err := strconv.Atoi(settings["NATIVE_HTTP_PORT"]); err == nil && value > 0 && value <= 65535 {
				port = value
			}
		}
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}
