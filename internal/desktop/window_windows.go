package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"yanecompanion/internal/diagnostics"
	"yanecompanion/release"
)

var user32 = windows.NewLazySystemDLL("user32.dll")
var shell32 = windows.NewLazySystemDLL("shell32.dll")
var commonControls = windows.NewLazySystemDLL("comctl32.dll")

const themeButtonStart = 150

var buttonProcedure = syscall.NewCallback(func(button uintptr, id uint32, w, l, _, _ uintptr) uintptr {
	if id == 0x14 { // WM_ERASEBKGND: WM_DRAWITEM fills the entire button.
		return 1
	}
	result, _, _ := commonControls.NewProc("DefSubclassProc").Call(button, uintptr(id), w, l)
	return result
})

func win(name string, args ...any) uintptr {
	values := make([]uintptr, len(args))
	for i, arg := range args {
		switch v := arg.(type) {
		case uintptr:
			values[i] = v
		case int:
			values[i] = uintptr(v)
		case *uint16:
			values[i] = uintptr(unsafe.Pointer(v))
		default:
			panic("invalid Windows UI argument")
		}
	}
	value, _, _ := user32.NewProc(name).Call(values...)
	// Retain UTF-16 buffers through the native call, including GC in callbacks.
	runtime.KeepAlive(args)
	return value
}
func wide(value string) *uint16 { return windows.StringToUTF16Ptr(value) }

type point struct{ X, Y int32 }
type message struct {
	Window  uintptr
	ID      uint32
	W, L    uintptr
	Time    uint32
	Point   point
	Private uint32
}
type windowClass struct {
	Size, Style                        uint32
	Procedure                          uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type notifyIcon struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	Title               [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}

func ReportError(err error) {
	win("MessageBoxW", 0, wide("Не удалось запустить yaneCompanion.\n"+diagnostics.Message(err)), wide("yaneCompanion"), 0x10)
}

func Run(ctx context.Context, actions Actions, show <-chan struct{}, hidden bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, nil, &instance); err != nil {
		return err
	}
	icon := win("LoadIconW", 0, 32512)
	if exe, err := os.Executable(); err == nil {
		if custom := win("LoadImageW", 0, wide(filepath.Join(filepath.Dir(exe), "yanecompanion.ico")), 1, 32, 32, 0x10); custom != 0 {
			icon = custom
			defer win("DestroyIcon", icon)
		}
	}

	const (
		connections = iota
		settings
		updates
		diagnosticView
	)
	snapshot := actions.Snapshot()
	initialDark := snapshot.Theme.dark(systemDark())
	theme := newWindowTheme(initialDark)
	defer func() { theme.close() }()
	var window uintptr
	var caption windowCaption
	var fade pageFade
	controls := map[uintptr]uintptr{}
	page := connections
	pages := [4][]uintptr{}
	requestID, grantsKey := "", "\x00"
	var refresh func()
	updateColors := func(dark bool) {
		if dark != theme.dark {
			fade.stop()
			theme.setColors(dark)
			applyWindowMode(window, dark)
		}
		win("RedrawWindow", window, 0, 0, 0x1a1)
	}
	setText := func(id uintptr, value string) { win("SetWindowTextW", controls[id], wide(value)) }
	visible := func(id uintptr, show bool) {
		handle := controls[id]
		if (win("GetWindowLongW", handle, -16)&0x10000000 != 0) == show {
			return
		}
		flags := 0x009f // hide, no redraw, no move/resize/activation/Z-order
		if show {
			flags = 0x005f // show with the same positioning flags
		}
		win("SetWindowPos", handle, 0, 0, 0, 0, 0, flags)
	}
	setView := func(next int) {
		if page != next {
			setText(220, "")
		}
		page = next
		visible(220, next != diagnosticView)
		setup := snapshot.Ready && snapshot.ActiveSite() == ""
		if next == updates {
			folderX, folderY, diagnosticsX, diagnosticsY := 283, 307, 46, 350
			if !snapshot.UpdatesAvailable {
				folderX, folderY, diagnosticsX, diagnosticsY = 46, 264, 283, 264
			}
			win("SetWindowPos", controls[304], 0, folderX, folderY, 0, 0, 0x1d)
			win("SetWindowPos", controls[305], 0, diagnosticsX, diagnosticsY, 0, 0, 0x1d)
		}
		for index, ids := range pages {
			for _, id := range ids {
				show := index == next
				switch id {
				case 105, 106, 210, 211, 212, 213, 214:
					show = show && requestID != ""
				case 202, 204:
					show = show && requestID == ""
				case 217, 218:
					show = show && requestID == "" && snapshot.ActiveSite() != ""
				case 134:
					show = show && requestID == "" && setup
				case 301, 302, 303:
					show = show && snapshot.UpdatesAvailable
				}
				visible(id, show)
			}
		}
		// Paint the parent and the newly visible controls before returning to the fade timer.
		win("RedrawWindow", window, 0, 0, 0x1a1) // invalidate, no erase, all children, update now
	}
	navigate := func(next int) {
		if next != page {
			fade.start()
			setView(next)
		}
	}
	showWindow := func() {
		win("ShowWindow", window, 9)
		if win("SetForegroundWindow", window) == 0 {
			// A browser launch can leave focus with the browser. Raise the window
			// without activation, then return it to the ordinary window stack.
			const preserveBoundsAndFocus = 0x0013                                        // SWP_NOSIZE | SWP_NOMOVE | SWP_NOACTIVATE
			win("SetWindowPos", window, ^uintptr(0), 0, 0, 0, 0, preserveBoundsAndFocus) // HWND_TOPMOST
			win("SetWindowPos", window, ^uintptr(1), 0, 0, 0, 0, preserveBoundsAndFocus) // HWND_NOTOPMOST
		}
	}
	report := func(err error) {
		if err != nil {
			setText(220, err.Error())
		} else {
			setText(220, "")
		}
	}
	readText := func(id uintptr) string {
		var value [513]uint16
		win("GetWindowTextW", controls[id], &value[0], len(value))
		return windows.UTF16ToString(value[:])
	}
	tray := notifyIcon{ID: 1, Flags: 1 | 2 | 4, Callback: 0x8001, Icon: icon}
	tray.Size = uint32(unsafe.Sizeof(tray))
	copy(tray.Tip[:], windows.StringToUTF16(windowTitle))
	trayAvailable := false
	addTray := func() {
		tray.Window = window
		result, _, _ := shell32.NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(&tray)))
		trayAvailable = result != 0
	}
	taskbarCreated := uint32(win("RegisterWindowMessageW", wide("TaskbarCreated")))
	procedure := syscall.NewCallback(func(hwnd uintptr, id uint32, w, l uintptr) uintptr {
		if id == taskbarCreated {
			addTray()
			if !trayAvailable && win("IsWindowVisible", hwnd) == 0 {
				showWindow()
			}
			return 0
		}
		switch id {
		case 0x83: // WM_NCCALCSIZE: draw the caption inside the client area.
			return 0
		case 0x84: // WM_NCHITTEST
			return caption.hitTest(hwnd, l)
		case 0x14: // WM_ERASEBKGND; WM_PAINT presents a complete back buffer
			return 1
		case 0x1a: // WM_SETTINGCHANGE
			if dark := snapshot.Theme.dark(systemDark()); window != 0 && dark != theme.dark {
				updateColors(dark)
			}
		case 0x0f: // WM_PAINT
			theme.paint(hwnd, page)
			return 0
		case 0x2b: // WM_DRAWITEM
			var item drawItem
			windows.NewLazySystemDLL("kernel32.dll").NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&item)), l, unsafe.Sizeof(item))
			if item.id == captionMinimize || item.id == captionClose {
				theme.captionButton(&item, caption.hover == item.window)
				return 1
			}
			selected := item.id == 135 && page == connections || item.id == 130 && page == settings || item.id == 132 && (page == updates || page == diagnosticView)
			if index := int(item.id) - themeButtonStart; index >= 0 && index < len(themeChoices) {
				selected = themeChoices[index].mode == snapshot.Theme
			}
			theme.button(&item, selected)
			return 1
		case 0x135: // WM_CTLCOLORBTN
			return theme.buttonBackground(win("GetDlgCtrlID", l))
		case 0x133, 0x134, 0x138: // edit, list box and static control colors
			element := win("GetDlgCtrlID", l)
			p := theme.colors
			color, fill, brush := p.ink, p.surface, theme.surfaceBrush
			switch element {
			case 101, 107, 219:
				fill, brush = p.elevated, theme.fieldBrush
			case 202, 207, 211, 214, 217:
				color = p.muted
			case 218:
				color = p.accent
			case 209, 220:
				color, fill, brush = p.muted, p.background, theme.backgroundBrush
			}
			gdi.NewProc("SetTextColor").Call(w, color.win())
			gdi.NewProc("SetBkColor").Call(w, fill.win())
			return brush
		case 0x10: // WM_CLOSE
			fade.stop()
			if trayAvailable {
				win("ShowWindow", hwnd, 0)
			} else {
				win("DestroyWindow", hwnd)
			}
			return 0
		case 0x2:
			win("PostQuitMessage", 0)
			return 0
		case 0x113:
			if w == pageFadeTimer {
				fade.tick()
				return 0
			}
			select {
			case <-ctx.Done():
				win("DestroyWindow", hwnd)
				return 0
			default:
			}
			select {
			case <-show:
				showWindow()
			default:
			}
			refresh()
			return 0
		case 0x8001:
			switch uint32(l) {
			case 0x202, 0x203:
				showWindow()
			case 0x205:
				menu := win("CreatePopupMenu")
				if menu == 0 {
					return 0
				}
				defer win("DestroyMenu", menu)
				win("AppendMenuW", menu, 0, 120, wide("Открыть yaneCompanion"))
				win("AppendMenuW", menu, 0, 110, wide("Выйти"))
				var p point
				win("GetCursorPos", uintptr(unsafe.Pointer(&p)))
				win("SetForegroundWindow", hwnd)
				choice := win("TrackPopupMenu", menu, 0x100|2, uintptr(p.X), uintptr(p.Y), 0, hwnd, 0)
				win("PostMessageW", hwnd, 0, 0, 0)
				if choice != 0 {
					win("SendMessageW", hwnd, 0x111, choice, 0)
				}
			}
			return 0
		case 0x111:
			command := w & 0xffff
			if index := int(command) - themeButtonStart; index >= 0 && index < len(themeChoices) {
				report(actions.SetTheme(themeChoices[index].mode))
				refresh()
				return 0
			}
			switch command {
			case captionMinimize:
				fade.stop()
				win("SendMessageW", hwnd, 0x112, 0xf020, 0) // SC_MINIMIZE
				return 0
			case captionClose:
				win("SendMessageW", hwnd, 0x10, 0, 0)
				return 0
			case 130, 134:
				navigate(settings)
				win("SetFocus", controls[101])
			case 135:
				navigate(connections)
			case 132:
				navigate(updates)
			case 133:
				navigate(updates)
				win("SetFocus", controls[305])
			case 301:
				report(actions.CheckUpdate())
			case 302, 303:
				if err := actions.DownloadUpdate(command == 303); err != nil {
					report(errors.New(diagnostics.Message(err)))
				}
			case 304:
				report(actions.UpdateFolder())
			case 305:
				reportText := strings.TrimRight(actions.Diagnostics(), "\r\n")
				setText(219, strings.ReplaceAll(reportText, "\n", "\r\n"))
				setText(306, "Скопировать отчёт")
				navigate(diagnosticView)
				scroll := 0
				if win("SendMessageW", controls[219], 0xba, 0, 0) > 8 {
					scroll = 1
				}
				win("ShowScrollBar", controls[219], 1, scroll)
				win("SetFocus", controls[306])
			case 306:
				before := win("GetClipboardSequenceNumber")
				win("SendMessageW", controls[219], 0xb1, 0, -1)
				win("SendMessageW", controls[219], 0x301, 0, 0)
				win("SendMessageW", controls[219], 0xb1, 0, 0)
				if win("GetClipboardSequenceNumber") == before {
					setText(306, "Повторить копирование")
				} else {
					setText(306, "Отчёт скопирован")
				}
				win("InvalidateRect", controls[306], 0, 0)
			case 102:
				site := readText(101)
				if err := actions.SetSite(site); err != nil {
					report(err)
				} else {
					setText(220, "Адрес сайта сохранён.")
				}
			case 104:
				report(actions.Autostart(!snapshot.Autostart))
			case 105, 106:
				if len(snapshot.Requests) > 0 {
					report(actions.Decide(snapshot.Requests[0], command == 105))
				}
			case 107:
				win("EnableWindow", controls[108], int(int32(win("SendMessageW", controls[107], 0x188, 0, 0))+1))
				return 0
			case 108:
				index := int32(win("SendMessageW", controls[107], 0x188, 0, 0))
				if index >= 0 && int(index) < len(snapshot.Grants) {
					report(actions.Revoke(snapshot.Grants[index].ID))
				}
			case 110:
				win("DestroyWindow", hwnd)
				return 0
			case 120:
				showWindow()
				return 0
			default:
				return 0
			}
			refresh()
			return 0
		}
		return win("DefWindowProcW", hwnd, uintptr(id), w, l)
	})
	class := windowClass{Procedure: procedure, Instance: uintptr(instance), Icon: icon, SmallIcon: icon, Cursor: win("LoadCursorW", 0, 32512), Name: wide("yaneCompanionDesktop")}
	class.Size = uint32(unsafe.Sizeof(class))
	if win("RegisterClassExW", uintptr(unsafe.Pointer(&class))) == 0 {
		return errors.New("DESKTOP_UNAVAILABLE")
	}
	defer win("UnregisterClassW", uintptr(unsafe.Pointer(class.Name)), uintptr(instance))
	const style = 0x02ca0000 // keep system menu, minimize and clipped child painting
	window = win("CreateWindowExW", 0, uintptr(unsafe.Pointer(class.Name)), wide(windowTitle), style, 0x80000000, 0x80000000, windowWidth, windowHeight, 0, 0, uintptr(instance), 0)
	if window == 0 {
		return errors.New("DESKTOP_UNAVAILABLE")
	}
	applyWindowMode(window, initialDark)
	roundWindow(window)
	add := func(view int, kind, text string, id uintptr, x, y, width, height int, style, font uintptr) {
		if view < 0 {
			style |= 0x10000000
		}
		handle := win("CreateWindowExW", 0, wide(kind), wide(text), 0x40000000|style, x, y, width, height, window, id, uintptr(instance), 0)
		controls[id] = handle
		if kind == "BUTTON" {
			commonControls.NewProc("SetWindowSubclass").Call(handle, buttonProcedure, 1, 0)
		}
		win("SendMessageW", handle, 0x30, font, 1)
		if view >= 0 {
			pages[view] = append(pages[view], id)
		}
	}
	button := func(view int, text string, id uintptr, x, y, width int) {
		add(view, "BUTTON", text, id, x, y, width, 34, 0x1000b, theme.body)
	}
	label := func(view int, text string, id uintptr, x, y, width, height int, font uintptr) {
		add(view, "STATIC", text, id, x, y, width, height, 0, font)
	}
	button(-1, "Подключение", 135, 29, 101, 166)
	button(-1, "Настройки", 130, 197, 101, 166)
	button(-1, "Поддержка", 132, 365, 101, 166)
	label(connections, "", 204, 46, 190, 468, 38, theme.title)
	label(connections, "", 202, 46, 236, 444, 50, theme.body)
	label(connections, "Адрес сайта", 217, 46, 301, 468, 22, theme.caption)
	label(connections, "", 218, 46, 327, 468, 57, theme.body)
	button(connections, "Указать адрес сайта", 134, 46, 309, 192)
	label(connections, "Разрешить подключение?", 210, 46, 169, 468, 33, theme.title)
	label(connections, "Сверьте адрес и код с сайтом.", 211, 46, 207, 468, 26, theme.body)
	add(connections, "EDIT", "", 212, 46, 241, 466, 40, 0x00210844, theme.body)
	label(connections, "", 213, 46, 291, 216, 38, theme.title)
	label(connections, "", 214, 240, 297, 270, 30, theme.body)
	button(connections, "Разрешить", 105, 367, 354, 145)
	button(connections, "Отклонить", 106, 214, 354, 142)

	label(settings, "Адрес yaneMedia", 215, 46, 171, 468, 21, theme.body)
	add(settings, "EDIT", snapshot.Site, 101, 56, 203, 321, 24, 0x10080, theme.body)
	win("SendMessageW", controls[101], 0xc5, 512, 0)
	win("SendMessageW", controls[101], 0x1501, 0, wide("https://media.example"))
	button(settings, "Сохранить", 102, 401, 197, 111)
	label(settings, "Запускать при входе", 216, 46, 247, 314, 26, theme.body)
	button(settings, "", 104, 401, 239, 111)
	label(settings, "Тема", 222, 46, 289, 112, 26, theme.body)
	for i, choice := range themeChoices {
		button(settings, choice.label, uintptr(themeButtonStart+i), 171+i*114, 281, 113)
	}
	label(settings, "Разрешённые подключения", 206, 46, 326, 468, 24, theme.body)
	add(settings, "LISTBOX", "", 107, 56, 355, 313, 44, 0x210101, theme.body)
	button(settings, "Отозвать", 108, 401, 358, 111)

	label(updates, "Обновления", 208, 46, 169, 468, 32, theme.heading)
	label(updates, "", 207, 46, 207, 468, 53, theme.body)
	button(updates, "Проверить обновления", 301, 46, 264, 225)
	button(updates, "Скачать обновление", 302, 283, 264, 229)
	button(updates, "Скачать предыдущую версию", 303, 46, 307, 225)
	button(updates, "Открыть папку пакетов", 304, 283, 307, 229)
	button(updates, "Диагностика", 305, 46, 350, 225)
	label(diagnosticView, "Диагностика", 221, 46, 169, 468, 32, theme.heading)
	add(diagnosticView, "EDIT", "", 219, 56, 217, 445, 167, 0x00210844, theme.body)
	button(diagnosticView, "Назад", 133, 24, 421, 96)
	button(diagnosticView, "Скопировать отчёт", 306, 130, 421, 179)
	add(-1, "STATIC", release.Version, 209, 426, 428, 110, 20, 2, theme.caption)
	label(-1, "", 220, 24, 428, 385, 35, theme.caption)
	add(-1, "BUTTON", "Свернуть", captionMinimize, 450, 12, 40, 32, 0x1000b, theme.caption)
	add(-1, "BUTTON", "Закрыть", captionClose, 496, 12, 40, 32, 0x1000b, theme.caption)
	caption.attach(controls[captionMinimize], controls[captionClose])
	fade = newPageFade(window, uintptr(instance))

	lastText := map[uintptr]string{}
	refresh = func() {
		before := snapshot
		snapshot = actions.Snapshot()
		if before.Theme != snapshot.Theme {
			updateColors(snapshot.Theme.dark(systemDark()))
		}
		title, body := snapshot.connectionText()
		pending, origin, code, expires := "", "", "", ""
		if len(snapshot.Requests) != 0 {
			r := snapshot.Requests[0]
			pending, origin, code = r.RequestID, r.Origin, r.Code
			expires = "Действует до " + r.ExpiresAt.Local().Format("15:04:05")
		}
		newRequest := pending != "" && pending != requestID
		if pending != requestID {
			fade.stop()
			requestID = pending
			if newRequest {
				setView(connections)
			} else if page == connections {
				setView(connections)
			}
		}
		if before.Ready != snapshot.Ready || before.ActiveSite() != snapshot.ActiveSite() || before.UpdatesAvailable != snapshot.UpdatesAvailable {
			setView(page)
		}
		autostart := "Выключено"
		if snapshot.Autostart {
			autostart = "Включено"
		}
		for id, value := range map[uintptr]string{
			204: title, 202: body, 104: autostart, 207: snapshot.Update,
			212: origin, 213: code, 214: expires, 218: snapshot.ActiveSite(),
		} {
			if lastText[id] != value {
				setText(id, value)
				lastText[id] = value
				if id == 104 {
					win("InvalidateRect", controls[id], 0, 1)
				}
				if id == 212 {
					showBar := 0
					if win("SendMessageW", controls[id], 0xba, 0, 0) > 2 {
						showBar = 1
					}
					win("ShowScrollBar", controls[id], 1, showBar)
				}
			}
		}
		key := ""
		for _, g := range snapshot.Grants {
			key += g.ID
		}
		if key != grantsKey {
			win("SendMessageW", controls[107], 0x184, 0, 0)
			if len(snapshot.Grants) == 0 {
				win("SendMessageW", controls[107], 0x180, 0, wide("Нет подключений"))
			}
			for _, g := range snapshot.Grants {
				win("SendMessageW", controls[107], 0x180, 0, wide(g.Origin+" — "+g.CreatedAt.Local().Format("02.01 15:04")))
			}
			grantsKey = key
			win("EnableWindow", controls[107], len(snapshot.Grants))
		}
		index := int32(win("SendMessageW", controls[107], 0x188, 0, 0))
		win("EnableWindow", controls[108], int(index+1))
		if newRequest {
			showWindow()
		}
	}
	setView(connections)
	addTray()
	defer shell32.NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(&tray)))
	refresh()
	win("SetTimer", window, 1, 500, 0)
	if !hidden || !trayAvailable {
		win("ShowWindow", window, 5)
	}
	var msg message
	for {
		result := int32(win("GetMessageW", uintptr(unsafe.Pointer(&msg)), 0, 0, 0))
		if result == 0 {
			break
		}
		if result == -1 {
			return errors.New("DESKTOP_UNAVAILABLE")
		}
		if win("IsDialogMessageW", window, uintptr(unsafe.Pointer(&msg))) == 0 {
			win("TranslateMessage", uintptr(unsafe.Pointer(&msg)))
			win("DispatchMessageW", uintptr(unsafe.Pointer(&msg)))
		}
	}
	return nil
}
