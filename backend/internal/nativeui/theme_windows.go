package nativeui

import (
	"strings"
	"syscall"
	"unsafe"
)

type managerTheme struct {
	styles  map[uintptr]controlStyle
	brushes map[uint32]uintptr
	fonts   []uintptr
}
type controlStyle struct {
	foreground, background uint32
	font                   uintptr
	button                 bool
	selected               bool
	serviceColor           uint32
	service                bool
}
type drawItem struct {
	Kind, ID, ItemID, Action, State uint32
	Window, DC                      uintptr
	Bounds                          rect
	Data                            uintptr
}

const (
	Ink      uint32 = 0x24201c
	Muted    uint32 = 0x80756a
	White    uint32 = 0xffffff
	Surface  uint32 = 0xf8f7f5
	LimeTint uint32 = 0xe1f8ee
	Green    uint32 = 0x338349
)

func (w *Window) ManagerTheme() {
	w.theme = &managerTheme{styles: map[uintptr]controlStyle{}, brushes: map[uint32]uintptr{}}
	// A white application surface, scoped to the service manager only.
	user32.NewProc("SetClassLongPtrW").Call(w.Handle, ^uintptr(9), w.brush(White))
}
func (w *Window) brush(color uint32) uintptr {
	if b := w.theme.brushes[color]; b != 0 {
		return b
	}
	b, _, _ := gdi32.NewProc("CreateSolidBrush").Call(uintptr(color))
	w.theme.brushes[color] = b
	return b
}
func (w *Window) Style(c Control, size int, bold bool, foreground, background uint32, family ...string) {
	weight := 400
	if bold {
		weight = 600
	}
	fontName := "Segoe UI"
	if len(family) > 0 {
		fontName = family[0]
	}
	f, _, _ := gdi32.NewProc("CreateFontW").Call(uintptr(int64(-size*w.Scale/96)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(textPointer(fontName))))
	w.theme.fonts = append(w.theme.fonts, f)
	s := w.theme.styles[c.Handle]
	s.foreground = foreground
	s.background = background
	s.font = f
	w.theme.styles[c.Handle] = s
	user32.NewProc("SendMessageW").Call(c.Handle, 0x30, f, 1)
}
func (w *Window) ManagerLabel(text string, x, y, width, height, size int, bold bool, fg, bg uint32) Control {
	c := w.Label(text, x, y, width, height)
	w.Style(c, size, bold, fg, bg)
	return c
}
func (w *Window) ManagerButton(text string, id, x, y, width, height int, primary bool) Control {
	c := w.child("BUTTON", text, 0x10000|0xb, id, x, y, width, height)
	fg, bg := Ink, Surface
	if primary {
		fg, bg = White, Ink
	}
	w.Style(c, 15, true, fg, bg)
	s := w.theme.styles[c.Handle]
	s.button = true
	w.theme.styles[c.Handle] = s
	return c
}
func (w *Window) SelectButton(c Control, selected bool) {
	s := w.theme.styles[c.Handle]
	if s.selected == selected {
		return
	}
	s.selected = selected
	w.theme.styles[c.Handle] = s
	user32.NewProc("InvalidateRect").Call(c.Handle, 0, 1)
}
func (w *Window) ServiceState(c Control, state string) {
	s := w.theme.styles[c.Handle]
	color := Muted
	if state == "ready" || state == "running" {
		color = 0x32b58a
	}
	if state == "failed" {
		color = 0x4545ca
	}
	if state == "starting" {
		color = 0x36a3da
	}
	if s.service && s.serviceColor == color {
		return
	}
	s.service = true
	s.serviceColor = color
	w.theme.styles[c.Handle] = s
	user32.NewProc("InvalidateRect").Call(c.Handle, 0, 1)
}
func (w *Window) Tint(c Control, color uint32) {
	s := w.theme.styles[c.Handle]
	if s.foreground == color {
		return
	}
	s.foreground = color
	w.theme.styles[c.Handle] = s
	user32.NewProc("InvalidateRect").Call(c.Handle, 0, 1)
}
func (c *Control) TailText(text string) {
	if c.text == text {
		return
	}
	c.SetText(text)
	n, _, _ := user32.NewProc("GetWindowTextLengthW").Call(c.Handle)
	user32.NewProc("SendMessageW").Call(c.Handle, 0xb1, n, n)
	user32.NewProc("SendMessageW").Call(c.Handle, 0xb7, 0, 0)
}
func (w *Window) ManagerIcon(x, y, size int) {
	c := w.child("STATIC", "", 3, 0, x, y, size, size)
	icon, _, _ := user32.NewProc("CopyImage").Call(w.icon, 1, w.px(size), w.px(size), 0)
	user32.NewProc("SendMessageW").Call(c.Handle, 0x170, icon, 0)
	// Window owns the copied image until destruction.
	if w.OnDestroy == nil {
		w.OnDestroy = func() { user32.NewProc("DestroyIcon").Call(icon) }
	}
}
func (w *Window) themeMessage(message uint32, wp, lp uintptr) (uintptr, bool) {
	if w.theme == nil {
		return 0, false
	}
	switch message {
	case 0x138, 0x133:
		s, ok := w.theme.styles[lp]
		if !ok {
			return 0, false
		}
		gdi32.NewProc("SetTextColor").Call(wp, uintptr(s.foreground))
		gdi32.NewProc("SetBkColor").Call(wp, uintptr(s.background))
		return w.brush(s.background), true
	case 0x2b:
		d := (*drawItem)(unsafe.Pointer(lp))
		s, ok := w.theme.styles[d.Window]
		if !ok || !s.button {
			return 0, false
		}
		bg, fg := s.background, s.foreground
		if s.selected {
			bg = LimeTint
			fg = Ink
		}
		if d.State&4 != 0 {
			fg = Muted
		}
		if d.State&1 != 0 {
			bg = 0xe6e9e8
		}
		user32.NewProc("FillRect").Call(d.DC, uintptr(unsafe.Pointer(&d.Bounds)), w.brush(bg))
		gdi32.NewProc("SetBkMode").Call(d.DC, 1)
		gdi32.NewProc("SetTextColor").Call(d.DC, uintptr(fg))
		old, _, _ := gdi32.NewProc("SelectObject").Call(d.DC, s.font)
		text := (Control{Handle: d.Window}).Text()
		r := d.Bounds
		flags := uintptr(0x1 | 0x4 | 0x20)
		if s.service {
			text, _, _ = strings.Cut(text, "  ·  ")
			r.Left += int32(w.px(36))
			flags = 0x4 | 0x20
			brush, _, _ := gdi32.NewProc("SelectObject").Call(d.DC, w.brush(s.serviceColor))
			pen, _, _ := gdi32.NewProc("GetStockObject").Call(8)
			oldPen, _, _ := gdi32.NewProc("SelectObject").Call(d.DC, pen)
			cy := uintptr((d.Bounds.Bottom - d.Bounds.Top) / 2)
			gdi32.NewProc("Ellipse").Call(d.DC, w.px(16), cy-w.px(4), w.px(24), cy+w.px(4))
			gdi32.NewProc("SelectObject").Call(d.DC, brush)
			gdi32.NewProc("SelectObject").Call(d.DC, oldPen)
		}
		user32.NewProc("DrawTextW").Call(d.DC, uintptr(unsafe.Pointer(textPointer(text))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags)
		gdi32.NewProc("SelectObject").Call(d.DC, old)
		if d.State&0x10 != 0 {
			r := d.Bounds
			r.Left += 3
			r.Right -= 3
			r.Top += 3
			r.Bottom -= 3
			user32.NewProc("DrawFocusRect").Call(d.DC, uintptr(unsafe.Pointer(&r)))
		}
		return 1, true
	case 2:
		for _, f := range w.theme.fonts {
			gdi32.NewProc("DeleteObject").Call(f)
		}
		// Class brush stays valid until the class is unregistered/process exits.
	}
	return 0, false
}

func CopyText(owner uintptr, text string) bool {
	chars, _ := syscall.UTF16FromString(text)
	ok, _, _ := user32.NewProc("OpenClipboard").Call(owner)
	if ok == 0 {
		return false
	}
	defer user32.NewProc("CloseClipboard").Call()
	h, _, _ := kernel32.NewProc("GlobalAlloc").Call(0x42, uintptr(len(chars)*2))
	if h == 0 {
		return false
	}
	p, _, _ := kernel32.NewProc("GlobalLock").Call(h)
	if p == 0 {
		kernel32.NewProc("GlobalFree").Call(h)
		return false
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(chars)), chars)
	kernel32.NewProc("GlobalUnlock").Call(h)
	user32.NewProc("EmptyClipboard").Call()
	ok, _, _ = user32.NewProc("SetClipboardData").Call(13, h)
	if ok == 0 {
		kernel32.NewProc("GlobalFree").Call(h)
	}
	return ok != 0
}
