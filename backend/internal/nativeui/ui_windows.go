// Package nativeui provides small Windows system controls without a GUI runtime dependency.
package nativeui

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	shell32         = syscall.NewLazyDLL("shell32.dll")
	gdi32           = syscall.NewLazyDLL("gdi32.dll")
	windowsByHandle = map[uintptr]*Window{}
	windowCallback  = syscall.NewCallback(dispatch)
)

const TrayMessage = 0x8000 + 77
const WakeMessage = 0x8000 + 78
const ActivateMessage = 0x8000 + 79

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type msg struct {
	HWND           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	Procedure                          uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type notifyIconData struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                [16]byte
	BalloonIcon         uintptr
}
type Window struct {
	Handle         uintptr
	Scale          int
	font, icon     uintptr
	taskbarCreated uint32
	tray           *notifyIconData
	OnCommand      func(int)
	OnTick         func()
	OnClose        func()
	OnTray         func(uint32)
	OnContextMenu  func()
	OnActivate     func()
	OnDestroy      func()
	theme          *managerTheme
}
type Control struct {
	Handle uintptr
	text   string
}
type MenuItem struct {
	ID                  int
	Text                string
	Disabled, Separator bool
}

func textPointer(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func (w *Window) px(n int) uintptr { return uintptr(n * w.Scale / 96) }

// Lock the caller's OS thread before creating windows and keep it locked through Loop.
func NewWindow(title, className, iconPath string, width, height int) (*Window, error) {
	user32.NewProc("SetProcessDPIAware").Call()
	dpi := uintptr(96)
	if p := user32.NewProc("GetDpiForSystem"); p.Find() == nil {
		if v, _, _ := p.Call(); v != 0 {
			dpi = v
		}
	}
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, 32512)
	icon, _, _ := user32.NewProc("LoadImageW").Call(0, uintptr(unsafe.Pointer(textPointer(iconPath))), 1, 0, 0, 0x10|0x40)
	if icon == 0 {
		icon, _, _ = user32.NewProc("LoadIconW").Call(0, 32512)
	}
	cls := windowClass{Style: 3, Procedure: windowCallback, Instance: instance, Icon: icon, SmallIcon: icon, Cursor: cursor, Background: 16, Name: textPointer(className)}
	cls.Size = uint32(unsafe.Sizeof(cls))
	if atom, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&cls))); atom == 0 {
		return nil, fmt.Errorf("register window: %w", err)
	}
	style := uintptr(0x00c00000 | 0x00080000 | 0x00020000) // caption, system menu, minimize
	area := rect{Right: int32(width * int(dpi) / 96), Bottom: int32(height * int(dpi) / 96)}
	user32.NewProc("AdjustWindowRectEx").Call(uintptr(unsafe.Pointer(&area)), style, 0, 0)
	screenW, _, _ := user32.NewProc("GetSystemMetrics").Call(0)
	screenH, _, _ := user32.NewProc("GetSystemMetrics").Call(1)
	realW, realH := uintptr(area.Right-area.Left), uintptr(area.Bottom-area.Top)
	hwnd, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(cls.Name)), uintptr(unsafe.Pointer(textPointer(title))), style, (screenW-realW)/2, (screenH-realH)/2, realW, realH, 0, 0, instance, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("create window: %w", err)
	}
	font, _, _ := gdi32.NewProc("CreateFontW").Call(uintptr(int64(-16*int(dpi)/96)), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(textPointer("Segoe UI"))))
	taskbarCreated, _, _ := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(textPointer("TaskbarCreated"))))
	w := &Window{Handle: hwnd, Scale: int(dpi), font: font, icon: icon, taskbarCreated: uint32(taskbarCreated)}
	windowsByHandle[hwnd] = w
	user32.NewProc("SetTimer").Call(hwnd, 1, 250, 0)
	return w, nil
}
func dispatch(hwnd uintptr, message uint32, wp, lp uintptr) uintptr {
	w := windowsByHandle[hwnd]
	if w != nil {
		if result, handled := w.themeMessage(message, wp, lp); handled {
			return result
		}
		switch message {
		case ActivateMessage:
			if w.OnActivate != nil {
				w.OnActivate()
			} else {
				w.Show()
			}
			return 0
		case 0x111:
			if w.OnCommand != nil && (wp>>16) == 0 {
				w.OnCommand(int(wp & 0xffff))
			}
			return 0
		case 0x113, WakeMessage:
			if w.OnTick != nil {
				w.OnTick()
			}
			return 0
		case 0x10:
			if w.OnClose != nil {
				w.OnClose()
			} else {
				w.Destroy()
			}
			return 0
		case TrayMessage:
			if w.OnTray != nil {
				w.OnTray(uint32(lp & 0xffff))
			}
			return 0
		case 0x7b:
			if w.OnContextMenu != nil {
				w.OnContextMenu()
				return 0
			}
		case 2:
			w.RemoveTray()
			if w.OnDestroy != nil {
				w.OnDestroy()
			}
			if w.font != 0 {
				gdi32.NewProc("DeleteObject").Call(w.font)
			}
			delete(windowsByHandle, hwnd)
			user32.NewProc("PostQuitMessage").Call(0)
			return 0
		}
		if message == w.taskbarCreated && w.tray != nil {
			shell32.NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(w.tray)))
			w.setTrayVersion()
			return 0
		}
	}
	value, _, _ := user32.NewProc("DefWindowProcW").Call(hwnd, uintptr(message), wp, lp)
	return value
}
func (w *Window) child(class, text string, style uintptr, id, x, y, width, height int) Control {
	hwnd, _, _ := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(textPointer(class))), uintptr(unsafe.Pointer(textPointer(text))), 0x40000000|0x10000000|style, w.px(x), w.px(y), w.px(width), w.px(height), w.Handle, uintptr(id), 0, 0)
	user32.NewProc("SendMessageW").Call(hwnd, 0x30, w.font, 1)
	return Control{Handle: hwnd, text: text}
}
func (w *Window) Label(text string, x, y, width, height int) Control {
	return w.child("STATIC", text, 0, 0, x, y, width, height)
}
func (w *Window) Edit(text string, id, x, y, width, height int) Control {
	return w.child("EDIT", text, 0x00800000|0x10000|0x80, id, x, y, width, height)
}
func (w *Window) TextArea(text string, id, x, y, width, height int) Control {
	return w.child("EDIT", text, 0x00800000|0x00200000|4|0x40|0x800, id, x, y, width, height)
}
func (w *Window) Button(text string, id, x, y, width, height int) Control {
	return w.child("BUTTON", text, 0x10000, id, x, y, width, height)
}
func (w *Window) Progress(x, y, width, height int) Control {
	syscall.NewLazyDLL("comctl32.dll").NewProc("InitCommonControls").Call()
	c := w.child("msctls_progress32", "", 0, 0, x, y, width, height)
	user32.NewProc("SendMessageW").Call(c.Handle, 0x406, 0, 1000)
	return c
}
func (c *Control) SetText(text string) {
	if text == c.text && c.Text() == text {
		return
	}
	c.text = text
	user32.NewProc("SetWindowTextW").Call(c.Handle, uintptr(unsafe.Pointer(textPointer(text))))
}
func (c Control) Text() string {
	n, _, _ := user32.NewProc("GetWindowTextLengthW").Call(c.Handle)
	b := make([]uint16, n+1)
	user32.NewProc("GetWindowTextW").Call(c.Handle, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b)
}
func (c Control) Enable(enabled bool) {
	v := uintptr(0)
	if enabled {
		v = 1
	}
	user32.NewProc("EnableWindow").Call(c.Handle, v)
}
func (c Control) Position(value int) {
	if value < 0 {
		value = 0
	}
	if value > 1000 {
		value = 1000
	}
	user32.NewProc("SendMessageW").Call(c.Handle, 0x402, uintptr(value), 0)
}
func (w *Window) Show() {
	user32.NewProc("ShowWindow").Call(w.Handle, 9)
	user32.NewProc("SetForegroundWindow").Call(w.Handle)
	user32.NewProc("UpdateWindow").Call(w.Handle)
}
func (w *Window) Hide()    { user32.NewProc("ShowWindow").Call(w.Handle, 0) }
func (w *Window) Destroy() { user32.NewProc("DestroyWindow").Call(w.Handle) }
func (w *Window) Wake()    { user32.NewProc("PostMessageW").Call(w.Handle, WakeMessage, 0, 0) }
func (w *Window) Loop() error {
	var m msg
	for {
		result, _, err := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(result) == -1 {
			return err
		}
		if result == 0 {
			return nil
		}
		// 控件 ID 2 在本界面不是 IDCANCEL；显式处理 Esc，避免系统对话框导航误触浏览按钮。
		if m.Message == 0x100 && m.WParam == 27 {
			if w.OnClose != nil {
				w.OnClose()
			} else {
				w.Destroy()
			}
			continue
		}
		handled, _, _ := user32.NewProc("IsDialogMessageW").Call(w.Handle, uintptr(unsafe.Pointer(&m)))
		if handled == 0 {
			user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&m)))
			user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&m)))
		}
	}
}
func (w *Window) AddTray(identity, tip string) error {
	sum := sha256.Sum256([]byte(identity))
	d := &notifyIconData{Window: w.Handle, ID: 1, Flags: 1 | 2 | 4 | 0x20 | 0x80, Callback: TrayMessage, Icon: w.icon}
	d.Size = uint32(unsafe.Sizeof(*d))
	copy(d.GUID[:], sum[:16])
	putUTF16(d.Tip[:], tip)
	if ok, _, err := shell32.NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(d))); ok == 0 {
		return fmt.Errorf("create tray icon: %w", err)
	}
	w.tray = d
	w.setTrayVersion()
	return nil
}
func (w *Window) setTrayVersion() {
	w.tray.Version = 4
	shell32.NewProc("Shell_NotifyIconW").Call(4, uintptr(unsafe.Pointer(w.tray)))
}
func (w *Window) TrayTip(tip string) {
	if w.tray == nil {
		return
	}
	putUTF16(w.tray.Tip[:], tip)
	shell32.NewProc("Shell_NotifyIconW").Call(1, uintptr(unsafe.Pointer(w.tray)))
}
func (w *Window) RemoveTray() {
	if w.tray != nil {
		shell32.NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(w.tray)))
		w.tray = nil
	}
}
func putUTF16(dst []uint16, s string) {
	clear(dst)
	chars, _ := syscall.UTF16FromString(s)
	if len(chars) > len(dst) {
		chars = chars[:len(dst)-1]
	}
	copy(dst, chars)
}
func (w *Window) Popup(items []MenuItem) int {
	menu, _, _ := user32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return 0
	}
	defer user32.NewProc("DestroyMenu").Call(menu)
	for _, item := range items {
		flags := uintptr(0)
		if item.Disabled {
			flags |= 3
		}
		if item.Separator {
			flags |= 0x800
		}
		user32.NewProc("AppendMenuW").Call(menu, flags, uintptr(item.ID), uintptr(unsafe.Pointer(textPointer(item.Text))))
	}
	var p point
	user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&p)))
	user32.NewProc("SetForegroundWindow").Call(w.Handle)
	selected, _, _ := user32.NewProc("TrackPopupMenuEx").Call(menu, 0x100|2, uintptr(p.X), uintptr(p.Y), w.Handle, 0)
	user32.NewProc("PostMessageW").Call(w.Handle, 0, 0, 0)
	return int(selected)
}
func ActivateWindow(className string) bool {
	h, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(textPointer(className))), 0)
	if h == 0 {
		return false
	}
	user32.NewProc("PostMessageW").Call(h, ActivateMessage, 0, 0)
	user32.NewProc("SetForegroundWindow").Call(h)
	return true
}
func Open(path string) error {
	v, _, err := shell32.NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(textPointer("open"))), uintptr(unsafe.Pointer(textPointer(path))), 0, 0, 1)
	if v <= 32 {
		return fmt.Errorf("open: %w", err)
	}
	return nil
}
func (w *Window) Ask(title, message string) bool {
	r, _, _ := user32.NewProc("MessageBoxW").Call(w.Handle, uintptr(unsafe.Pointer(textPointer(message))), uintptr(unsafe.Pointer(textPointer(title))), 0x4|0x20|0x100)
	return r == 6
}
func Error(title, message string) {
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(textPointer(message))), uintptr(unsafe.Pointer(textPointer(title))), 0x10)
}
func Info(title, message string) {
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(textPointer(message))), uintptr(unsafe.Pointer(textPointer(title))), 0x40)
}

func ChooseFolder(owner uintptr, initial string) (string, error) {
	ole := syscall.NewLazyDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2)
	if int32(hr) < 0 {
		return "", errors.New("cannot initialize folder picker")
	}
	defer ole.NewProc("CoUninitialize").Call()
	var display [260]uint16
	callback := syscall.NewCallback(func(hwnd uintptr, message uint32, lp, data uintptr) uintptr {
		if message == 1 {
			user32.NewProc("SendMessageW").Call(hwnd, 0x467, 1, data)
		}
		return 0
	})
	selected := textPointer(initial)
	info := struct {
		Owner, Root     uintptr
		Display, Title  *uint16
		Flags           uint32
		Callback, Param uintptr
		Image           int32
	}{Owner: owner, Display: &display[0], Title: textPointer("选择安装目录的父文件夹 / Choose installation parent folder"), Flags: 0x41, Callback: callback, Param: uintptr(unsafe.Pointer(selected))}
	pidl, _, _ := shell32.NewProc("SHBrowseForFolderW").Call(uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(selected)
	if pidl == 0 {
		return "", nil
	}
	defer ole.NewProc("CoTaskMemFree").Call(pidl)
	var path [32768]uint16
	ok, _, _ := shell32.NewProc("SHGetPathFromIDListEx").Call(pidl, uintptr(unsafe.Pointer(&path[0])), uintptr(len(path)), 0)
	if ok == 0 {
		return "", errors.New("please select a local filesystem folder")
	}
	return syscall.UTF16ToString(path[:]), nil
}
