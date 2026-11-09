package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var user32 = windows.NewLazySystemDLL("user32.dll")
var shell32 = windows.NewLazySystemDLL("shell32.dll")

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
	win("MessageBoxW", 0, wide("Не удалось запустить yaneCompanion.\n"+err.Error()), wide("yaneCompanion"), 0x10)
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
	var window uintptr
	controls := map[uintptr]uintptr{}
	var snapshot Snapshot
	lastRequest, lastGrants, lastStatus := "", "", ""
	var currentRequestID string
	setText := func(handle uintptr, value string) { win("SetWindowTextW", handle, wide(value)) }
	showWindow := func() { win("ShowWindow", window, 9); win("SetForegroundWindow", window) }
	report := func(err error) {
		if err != nil {
			win("MessageBoxW", window, wide(err.Error()), wide("yaneCompanion"), 0x10)
		}
	}
	tray := notifyIcon{Window: 0, ID: 1, Flags: 1 | 2 | 4, Callback: 0x8001, Icon: icon}
	tray.Size = uint32(unsafe.Sizeof(tray))
	copy(tray.Tip[:], windows.StringToUTF16("yaneCompanion — локальный помощник"))
	trayAvailable := false
	addTray := func() {
		tray.Window = window
		result, _, _ := shell32.NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(&tray)))
		trayAvailable = result != 0
	}
	hideWindow := func() {
		if trayAvailable {
			win("ShowWindow", window, 0)
		} else {
			win("ShowWindow", window, 2)
		}
	}
	taskbarCreated := uint32(win("RegisterWindowMessageW", wide("TaskbarCreated")))
	refresh := func() {
		snapshot = actions.Snapshot()
		status := snapshot.Status + "\r\n" + snapshot.Activity
		if status != lastStatus {
			setText(controls[201], status)
			lastStatus = status
		}
		requestText := "Нет запросов на подключение. Нажмите «Подключить» на сайте."
		if len(snapshot.Requests) > 0 {
			r := snapshot.Requests[0]
			requestText = r.Origin + "\r\nКод: " + r.Code + "   (до " + r.ExpiresAt.Local().Format("15:04:05") + ")\r\nСверьте адрес и код с сайтом."
			if currentRequestID != r.RequestID {
				showWindow()
				currentRequestID = r.RequestID
			}
		}
		if requestText != lastRequest {
			setText(controls[202], requestText)
			lastRequest = requestText
		}
		enabled := uintptr(0)
		if len(snapshot.Requests) > 0 {
			enabled = 1
		}
		win("EnableWindow", controls[105], enabled)
		win("EnableWindow", controls[106], enabled)
		grantsKey := ""
		for _, g := range snapshot.Grants {
			grantsKey += g.ID
		}
		if grantsKey != lastGrants {
			win("SendMessageW", controls[107], 0x184, 0, 0)
			for _, g := range snapshot.Grants {
				win("SendMessageW", controls[107], 0x180, 0, wide(g.Origin+" — "+g.CreatedAt.Local().Format("02.01 15:04")))
			}
			lastGrants = grantsKey
		}
		checked := uintptr(0)
		if snapshot.Autostart {
			checked = 1
		}
		win("SendMessageW", controls[104], 0xf1, checked, 0)
	}
	procedure := syscall.NewCallback(func(hwnd uintptr, id uint32, w, l uintptr) uintptr {
		if id == taskbarCreated {
			addTray()
			return 0
		}
		switch id {
		case 0x10:
			hideWindow()
			return 0 // closing hides; Exit stops
		case 0x2:
			win("PostQuitMessage", 0)
			return 0
		case 0x113:
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
				defer win("DestroyMenu", menu)
				win("AppendMenuW", menu, 2, 0, wide(snapshot.Status+" · "+snapshot.Activity))
				win("AppendMenuW", menu, 0x800, 0, 0)
				for _, item := range []struct {
					id    uintptr
					title string
				}{{120, "Открыть управление"}, {103, "Открыть сайт"}, {110, "Выход — остановить просмотр"}} {
					win("AppendMenuW", menu, 0, item.id, wide(item.title))
				}
				var p point
				win("GetCursorPos", uintptr(unsafe.Pointer(&p)))
				win("SetForegroundWindow", hwnd)
				choice := win("TrackPopupMenu", menu, 0x100|2, uintptr(p.X), uintptr(p.Y), 0, hwnd, 0)
				if choice != 0 {
					win("SendMessageW", hwnd, 0x111, choice, 0)
				}
			}
			return 0
		case 0x111:
			switch w & 0xffff {
			case 102:
				value := make([]uint16, 1024)
				win("GetWindowTextW", controls[101], uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)))
				report(actions.SetSite(windows.UTF16ToString(value)))
			case 103:
				report(actions.OpenSite())
			case 104:
				report(actions.Autostart(win("SendMessageW", controls[104], 0xf0, 0, 0) != 0))
			case 105, 106:
				if len(snapshot.Requests) > 0 {
					report(actions.Decide(snapshot.Requests[0], w&0xffff == 105))
				}
			case 108:
				index := int32(win("SendMessageW", controls[107], 0x188, 0, 0))
				if index >= 0 && int(index) < len(snapshot.Grants) {
					report(actions.Revoke(snapshot.Grants[index].ID))
				}
			case 109:
				hideWindow()
			case 110:
				win("DestroyWindow", hwnd)
			case 120:
				showWindow()
			}
			return 0
		}
		return win("DefWindowProcW", hwnd, uintptr(id), w, l)
	})
	class := windowClass{Procedure: procedure, Instance: uintptr(instance), Icon: icon, SmallIcon: icon, Cursor: win("LoadCursorW", 0, 32512), Background: 16, Name: windows.StringToUTF16Ptr("yaneCompanionDesktop")}
	class.Size = uint32(unsafe.Sizeof(class))
	if win("RegisterClassExW", uintptr(unsafe.Pointer(&class))) == 0 {
		return errors.New("DESKTOP_UNAVAILABLE")
	}
	defer win("UnregisterClassW", uintptr(unsafe.Pointer(class.Name)), uintptr(instance))
	window = win("CreateWindowExW", 0, uintptr(unsafe.Pointer(class.Name)), wide("yaneCompanion"), 0x00ca0000, 0x80000000, 0x80000000, 660, 650, 0, 0, uintptr(instance), 0)
	if window == 0 {
		return errors.New("DESKTOP_UNAVAILABLE")
	}
	gdi := windows.NewLazySystemDLL("gdi32.dll")
	fontName := wide("Segoe UI")
	font, _, _ := gdi.NewProc("CreateFontW").Call(^uintptr(14), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)))
	runtime.KeepAlive(fontName)
	defer gdi.NewProc("DeleteObject").Call(font)
	add := func(kind, text string, id uintptr, x, y, width, height int, style uintptr) {
		handle := win("CreateWindowExW", 0, wide(kind), wide(text), 0x50000000|style, uintptr(x), uintptr(y), uintptr(width), uintptr(height), window, id, uintptr(instance), 0)
		controls[id] = handle
		win("SendMessageW", handle, 0x30, font, 1)
	}
	add("STATIC", "", 201, 20, 16, 605, 48, 0)
	add("STATIC", "Адрес сайта (HTTPS, без пути)", 203, 20, 75, 605, 22, 0)
	add("EDIT", actions.Snapshot().Site, 101, 20, 101, 455, 27, 0x00810080)
	win("SendMessageW", controls[101], 0xc5, 512, 0)
	add("BUTTON", "Сохранить", 102, 487, 100, 130, 29, 0x10000)
	add("BUTTON", "Открыть сайт", 103, 20, 142, 165, 32, 0x10000)
	add("BUTTON", "Запускать при входе в систему", 104, 205, 142, 410, 32, 0x10003)
	add("STATIC", "Подтверждение подключения", 204, 20, 195, 600, 24, 0)
	add("EDIT", "", 202, 20, 225, 600, 76, 0x00200844)
	add("BUTTON", "Разрешить", 105, 20, 309, 140, 32, 0x10000)
	add("BUTTON", "Отклонить", 106, 175, 309, 140, 32, 0x10000)
	add("STATIC", "Разрешённые подключения (выберите для отзыва)", 205, 20, 366, 605, 24, 0)
	add("LISTBOX", "", 107, 20, 396, 600, 102, 0x00a10001)
	add("BUTTON", "Отозвать доступ", 108, 20, 508, 190, 32, 0x10000)
	add("BUTTON", "Скрыть в трей", 109, 20, 560, 190, 32, 0x10000)
	add("BUTTON", "Выход", 110, 480, 560, 140, 32, 0x10000)
	refresh()
	addTray()
	defer shell32.NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(&tray)))
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
