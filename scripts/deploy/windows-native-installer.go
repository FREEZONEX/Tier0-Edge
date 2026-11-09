//go:build ignore

// 由封装脚本复制到 backend/.build 下，嵌入单平台 payload.zip 后编译。
package main

import (
	"archive/zip"
	"backend/internal/nativeinstall"
	"backend/internal/nativeui"
	"backend/internal/runtimeenv"
	"bytes"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed payload.zip
var payload []byte

type installState struct {
	phase, message, details string
	position                int
	done                    bool
	err                     error
	root                    string
}
type reporter func(installState)

func main() {
	target := flag.String("target", "", "new installation directory")
	noStart := flag.Bool("no-start", false, "extract without launching")
	upgrade := flag.Bool("upgrade", false, "update an existing installation with backup and readiness verification")
	recover := flag.Bool("recover", false, "restore an interrupted update")
	flag.Parse()
	if len(os.Args) == 1 {
		if err := installWindow(); err != nil {
			nativeui.Error(runtimeenv.ProductName()+" 安装器", err.Error())
			os.Exit(1)
		}
		return
	}
	if *target == "" {
		fmt.Fprintln(os.Stderr, "--target is required for command-line installation")
		os.Exit(2)
	}
	if (*upgrade || *recover) && *noStart {
		fmt.Fprintln(os.Stderr, "updates require startup verification; --no-start is only for new installations")
		os.Exit(2)
	}
	report := func(s installState) { fmt.Println(s.message) }
	var err error
	if *recover {
		err = recoverUpdate(*target, report)
	} else if *upgrade {
		err = updateInstallation(context.Background(), *target, report)
	} else {
		err = install(context.Background(), *target, *noStart, true, report)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Installation failed:", err)
		os.Exit(1)
	}
}
func archiveInfo() (*zip.Reader, uint64, error) {
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return nil, 0, err
	}
	var total uint64
	for _, file := range archive.File {
		total += file.UncompressedSize64
	}
	return archive, total, nil
}
func installWindow() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	_, total, err := archiveInfo()
	if err != nil {
		return err
	}
	initial, hint := nativeui.DefaultInstallDirectory(total)
	window, err := nativeui.NewWindow(runtimeenv.ProductName()+" · Windows 安装", "Tier0NativeInstaller", "", 720, 550)
	if err != nil {
		return err
	}
	window.Label("安装 "+runtimeenv.ProductName()+" · "+runtimeenv.BuildPackageVersion, 24, 18, 670, 28)
	window.Label(hint, 24, 54, 670, 40)
	window.Label("新目录用于安装；选择已有 Tier0 目录可更新并保留数据", 24, 101, 670, 24)
	directory := window.Edit(initial, 20, 24, 130, 548, 30)
	browse := window.Button("浏览…", 2, 584, 130, 112, 30)
	phase := window.Label("准备安装", 24, 180, 670, 28)
	progress := window.Progress(24, 216, 672, 24)
	details := window.TextArea("仅包含 Windows x64 组件，不需要 Docker / WSL 或预装 Node.js。", 21, 24, 255, 672, 210)
	installButton := window.Button("安装 / 更新", 1, 450, 487, 118, 34)
	closeButton := window.Button("取消", 3, 580, 487, 116, 34)
	var mu sync.Mutex
	state := installState{phase: "idle", message: "准备安装"}
	active := false
	var cancel context.CancelFunc
	report := func(s installState) { mu.Lock(); state = s; mu.Unlock(); window.Wake() }
	stop := func() {
		if active && cancel != nil {
			if window.Ask("取消安装 / 更新", "取消新装会保留文件；取消更新会恢复备份。恢复期间请勿关闭电脑。确定取消？") {
				cancel()
				closeButton.Enable(false)
				phase.SetText("正在取消，请稍候…")
			}
		} else {
			window.Destroy()
		}
	}
	window.OnClose = stop
	window.OnCommand = func(id int) {
		switch id {
		case 1:
			mu.Lock()
			completed := state.done
			logRoot := state.root
			mu.Unlock()
			if completed {
				if e := nativeui.Open(filepath.Join(logRoot, "logs")); e != nil {
					nativeui.Error("日志", e.Error())
				}
				return
			}
			if active {
				return
			}
			root := strings.TrimSpace(directory.Text())
			if root == "" || !filepath.IsAbs(root) || strings.HasPrefix(root, `\\`) {
				nativeui.Error("安装目录", "请选择本机的完整安装目录。")
				return
			}
			recovering := updatePending(root)
			_, statErr := os.Lstat(root)
			updating := statErr == nil && !retainedInstallation(root)
			if recovering {
				if !window.Ask("恢复未完成的更新", "此目录存在未完成的更新。恢复升级前的程序和数据并启动？") {
					return
				}
			} else if updating {
				info, e := updatePreflight(root)
				if e != nil {
					nativeui.Error("无法更新", e.Error())
					return
				}
				if !window.Ask("更新 Tier0", info+"\r\n\r\n将短暂停机，完整备份并保留数据库、App、配置和云端身份。请暂停外部业务写入；验证失败将恢复升级前快照。继续更新？") {
					return
				}
			} else if statErr != nil && !os.IsNotExist(statErr) {
				nativeui.Error("安装目录", statErr.Error())
				return
			}
			ctx, c := context.WithCancel(context.Background())
			cancel = c
			active = true
			directory.Enable(false)
			browse.Enable(false)
			installButton.Enable(false)
			phase.SetText("开始安装…")
			go func() {
				var err error
				if recovering {
					err = recoverUpdate(root, report)
				} else if updating {
					err = updateInstallation(ctx, root, report)
				} else {
					err = install(ctx, root, false, true, report)
				}
				mu.Lock()
				state.done = true
				state.err = err
				state.root = root
				if err != nil {
					state.message = "安装或启动未完成"
					state.details = err.Error() + "\r\n\r\n安装目录：" + root + "\r\n更新中断请重新运行本安装器并选择原目录恢复；诊断日志在 logs 文件夹。"
				} else {
					state.message = "安装 / 更新完成，Tier0 已在托盘运行"
					state.position = 1000
					state.details += "\r\n可通过右下角 Tier0 图标打开平台、查看服务状态和日志、启停或退出。\r\n关闭此安装窗口不会停止 Tier0。\r\n安装目录：" + root
				}
				mu.Unlock()
				window.Wake()
			}()
		case 2:
			current := directory.Text()
			parent := filepath.Dir(current)
			selected, e := nativeui.ChooseFolder(window.Handle, parent)
			if e != nil {
				nativeui.Error("选择目录", e.Error())
			} else if selected != "" {
				if _, e := os.Stat(filepath.Join(selected, "package.json")); e == nil || updatePending(selected) {
					directory.SetText(selected)
				} else {
					directory.SetText(filepath.Join(selected, filepath.Base(current)))
				}
			}
		case 3:
			stop()
		}
	}
	window.OnTick = func() {
		mu.Lock()
		s := state
		mu.Unlock()
		if s.phase != "idle" || s.done {
			phase.SetText(s.message)
			progress.Position(s.position)
			details.SetText(s.details)
		}
		if s.done && active {
			active = false
			closeButton.SetText("关闭")
			closeButton.Enable(true)
			if s.err != nil {
				installButton.SetText("查看日志")
				installButton.Enable(true)
			}
		}
	}
	window.Show()
	return window.Loop()
}

type progressReader struct {
	ctx       context.Context
	r         io.Reader
	copied    *uint64
	total     uint64
	last      *time.Time
	component string
	report    reporter
}

func (r progressReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	*r.copied += uint64(n)
	if time.Since(*r.last) > 120*time.Millisecond {
		*r.last = time.Now()
		position := 0
		if r.total > 0 {
			position = int(*r.copied * 450 / r.total)
		}
		r.report(installState{phase: "extracting", message: "正在解压安装文件 · " + r.component, position: position, details: fmt.Sprintf("已解压 %.1f / %.1f MiB\r\n当前组件：%s", float64(*r.copied)/(1<<20), float64(r.total)/(1<<20), r.component)})
	}
	return n, err
}

func install(ctx context.Context, target string, noStart, register bool, report reporter) (resultErr error) {
	root, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err = nativeinstall.SafePath(root); err != nil {
		return err
	}
	unlock, err := nativeinstall.Lock(root)
	if err != nil {
		return err
	}
	defer unlock()
	archive, total, err := archiveInfo()
	if err != nil {
		return err
	}
	retained := retainedInstallation(root)
	if retained {
		if err = reinstallPreflight(root, archive); err != nil {
			return err
		}
	} else {
		if _, err = os.Lstat(root); err == nil {
			return fmt.Errorf("安装目录已存在，未覆盖任何文件：%s", root)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(root), 0700); err != nil {
			return err
		}
		if err = os.Mkdir(root, 0700); err != nil {
			return err
		}
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	acl := exec.Command("icacls.exe", root, "/inheritance:r", "/grant:r", "*"+u.Uid+":(OI)(CI)F", "*S-1-5-18:(OI)(CI)F")
	acl.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if _, err = acl.CombinedOutput(); err != nil {
		return fmt.Errorf("无法保护安装目录权限：%w", err)
	}
	marker := filepath.Join(root, ".installing")
	if err = os.WriteFile(marker, []byte("Incomplete extraction; do not start."), 0600); err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			_ = os.MkdirAll(filepath.Join(root, "logs"), 0700)
			f, e := os.OpenFile(filepath.Join(root, "logs", "install.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if e == nil {
				fmt.Fprintln(f, time.Now().Format(time.RFC3339), resultErr)
				f.Close()
			}
		}
	}()
	report(installState{phase: "extracting", message: "正在解压安装文件", details: root})
	var copied uint64
	last := time.Time{}
	for _, file := range archive.File {
		if err = ctx.Err(); err != nil {
			return err
		}
		name := filepath.FromSlash(file.Name)
		if !filepath.IsLocal(name) || file.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe archive entry")
		}
		path := filepath.Join(root, name)
		if file.FileInfo().IsDir() {
			if err = os.MkdirAll(path, 0700); err != nil {
				return err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		src, e := file.Open()
		if e != nil {
			return e
		}
		flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
		if retained && (name == "package.json" || name == "FILES.txt") {
			flags = os.O_WRONLY | os.O_TRUNC
		}
		dst, e := os.OpenFile(path, flags, 0600)
		if e != nil {
			src.Close()
			return e
		}
		component := strings.SplitN(file.Name, "/", 2)[0]
		_, copyErr := io.Copy(dst, progressReader{ctx: ctx, r: src, copied: &copied, total: total, last: &last, component: component, report: report})
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	report(installState{phase: "verifying", message: "解压完成，正在核验安装文件", position: 470, details: "检查所有文件的存在性和长度。"})
	for _, file := range archive.File {
		if err = ctx.Err(); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		info, e := os.Stat(filepath.Join(root, filepath.FromSlash(file.Name)))
		if e != nil || uint64(info.Size()) != file.UncompressedSize64 {
			return fmt.Errorf("安装文件缺失或不完整：%s。请检查 Windows 安全中心的保护历史记录，不要关闭防护。", file.Name)
		}
	}
	if err = os.Remove(marker); err != nil {
		return err
	}
	if retained {
		if err = os.Remove(filepath.Join(root, nativeinstall.RetainedMarker)); err != nil {
			return err
		}
	}
	if register {
		if err = nativeinstall.Register(root); err != nil {
			return err
		}
	}
	if noStart {
		report(installState{phase: "done", message: "安装文件已解压并核验", position: 1000, details: root})
		return nil
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return startInstalled(ctx, root, report)
}

func startInstalled(ctx context.Context, root string, report reporter) error {
	cmd := exec.Command(filepath.Join(root, "Start.exe"), "--background")
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("文件已安装，但启动器未能启动：%w", err)
	}
	exit := make(chan error, 1)
	go func() { exit <- cmd.Wait() }()
	report(installState{phase: "starting", message: "正在启动服务并执行就绪检查", position: 500, details: "正在等待本套启动器报告各组件状态。"})
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(7 * time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			stop := exec.Command(filepath.Join(root, "Start.exe"), "--stop")
			stop.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			_ = stop.Run()
			return ctx.Err()
		case e := <-exit:
			if e == nil {
				return errors.New("文件已安装，但启动器在就绪前退出；请运行 Start.exe 或查看 logs/launcher.log")
			}
			return fmt.Errorf("文件已安装，启动器异常退出：%w", e)
		case <-timeout.C:
			return errors.New("文件已安装，等待服务就绪超时。请从托盘查看状态，或检查 logs 后重试")
		case <-ticker.C:
			p, e := nativeui.ReadProgress(root)
			if e != nil || p.PID != cmd.Process.Pid {
				continue
			}
			done := 0
			for _, s := range p.Services {
				if s.State == "running" || s.State == "ready" {
					done++
				}
			}
			position := 500
			if len(p.Services) > 0 {
				position += done * 450 / len(p.Services)
			}
			report(installState{phase: p.Phase, message: p.Message, details: p.Lines(), position: position})
			if p.Phase == "ready" {
				return nil
			}
			if p.Phase == "failed" {
				return errors.New(p.Error)
			}
			if p.Phase == "stopped" {
				return errors.New("服务已停止，安装文件已保留。可从托盘或 Start.exe 重新启动")
			}
		}
	}
}

func retainedInstallation(root string) bool {
	info, err := os.Lstat(filepath.Join(root, nativeinstall.RetainedMarker))
	return err == nil && info.Mode().IsRegular()
}
func reinstallPreflight(root string, archive *zip.Reader) error {
	if updatePending(root) {
		return errors.New("请先恢复未完成的更新")
	}
	oldRaw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return err
	}
	old, err := readNativeManifest(oldRaw)
	if err != nil {
		return err
	}
	f, err := archive.Open("package.json")
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	next, err := readNativeManifest(raw)
	if err != nil {
		return err
	}
	if err = runtimeenv.ValidateDistribution(next.Distribution); err != nil {
		return err
	}
	if old.Distribution.Edition != next.Distribution.Edition || old.Distribution.Platform != next.Distribution.Platform || old.Distribution.Deployment != next.Distribution.Deployment {
		return errors.New("保留数据的产品和平台不匹配")
	}
	// Restoring the same code needs no database migration. A newer code revision
	// must use the normal upgrade transaction, which takes a complete backup.
	if old.Revision != next.Revision || old.ProductVersion != next.ProductVersion {
		return errors.New("请先用原源码/业务版本的安装包恢复程序，再通过正常更新流程备份升级")
	}
	if err = checkDataCompatibility(root, old, next); err != nil {
		return err
	}
	for _, file := range archive.File {
		name := filepath.FromSlash(file.Name)
		if !filepath.IsLocal(name) {
			return errors.New("安装包路径不安全")
		}
		path := filepath.Join(root, name)
		if err = nativeinstall.SafePath(path); err != nil {
			return err
		}
		if file.FileInfo().IsDir() || name == "package.json" || name == "FILES.txt" {
			continue
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return fmt.Errorf("原目录存在程序同名文件，未覆盖：%s；卸载中断时请先重试卸载", name)
		}
	}
	return nil
}
