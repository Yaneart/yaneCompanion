package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"yanecompanion/internal/diagnostics"
	"yanecompanion/release"
)

// GTK 3 remains available on Debian 12, Ubuntu 24.04 and Arch. No C compiler,
// browser engine or GTK development headers are needed on the viewer's machine.
type gtkAPI struct {
	StackNew         func() uintptr                                             `symbol:"gtk_stack_new"`
	StackAdd         func(uintptr, uintptr, string)                             `symbol:"gtk_stack_add_named"`
	StackShow        func(uintptr, string)                                      `symbol:"gtk_stack_set_visible_child_name"`
	StackTransition  func(uintptr, int32)                                       `symbol:"gtk_stack_set_transition_type"`
	StackDuration    func(uintptr, uint32)                                      `symbol:"gtk_stack_set_transition_duration"`
	Name             func(uintptr, string)                                      `symbol:"gtk_widget_set_name"`
	Size             func(uintptr, int32, int32)                                `symbol:"gtk_widget_set_size_request"`
	LabelWrapMode    func(uintptr, int32)                                       `symbol:"gtk_label_set_line_wrap_mode"`
	Show             func(uintptr)                                              `symbol:"gtk_widget_show"`
	InitCheck        func(uintptr, uintptr) int32                               `symbol:"gtk_init_check"`
	WindowNew        func(int32) uintptr                                        `symbol:"gtk_window_new"`
	WindowTitle      func(uintptr, string)                                      `symbol:"gtk_window_set_title"`
	WindowSize       func(uintptr, int32, int32)                                `symbol:"gtk_window_set_default_size"`
	WindowPresent    func(uintptr)                                              `symbol:"gtk_window_present"`
	WindowIcon       func(uintptr, string, uintptr) int32                       `symbol:"gtk_window_set_icon_from_file"`
	Add              func(uintptr, uintptr)                                     `symbol:"gtk_container_add"`
	LabelNew         func(string) uintptr                                       `symbol:"gtk_label_new"`
	LabelSet         func(uintptr, string)                                      `symbol:"gtk_label_set_text"`
	LabelWrap        func(uintptr, int32)                                       `symbol:"gtk_label_set_line_wrap"`
	LabelAlign       func(uintptr, float32)                                     `symbol:"gtk_label_set_xalign"`
	EntryNew         func() uintptr                                             `symbol:"gtk_entry_new"`
	EntrySet         func(uintptr, string)                                      `symbol:"gtk_entry_set_text"`
	EntryHint        func(uintptr, string)                                      `symbol:"gtk_entry_set_placeholder_text"`
	EntryGet         func(uintptr) uintptr                                      `symbol:"gtk_entry_get_text"`
	EntryLimit       func(uintptr, int32)                                       `symbol:"gtk_entry_set_max_length"`
	ButtonNew        func(string) uintptr                                       `symbol:"gtk_button_new_with_label"`
	BinChild         func(uintptr) uintptr                                      `symbol:"gtk_bin_get_child"`
	ButtonLabel      func(uintptr, string)                                      `symbol:"gtk_button_set_label"`
	CheckGet         func(uintptr) int32                                        `symbol:"gtk_toggle_button_get_active"`
	CheckSet         func(uintptr, int32)                                       `symbol:"gtk_toggle_button_set_active"`
	ToggleMode       func(uintptr, int32)                                       `symbol:"gtk_toggle_button_set_mode"`
	RadioNew         func(uintptr, string) uintptr                              `symbol:"gtk_radio_button_new_with_label_from_widget"`
	ShowAll          func(uintptr)                                              `symbol:"gtk_widget_show_all"`
	Hide             func(uintptr)                                              `symbol:"gtk_widget_hide"`
	Sensitive        func(uintptr, int32)                                       `symbol:"gtk_widget_set_sensitive"`
	MenuNew          func() uintptr                                             `symbol:"gtk_menu_new"`
	MenuItem         func(string) uintptr                                       `symbol:"gtk_menu_item_new_with_label"`
	MenuAppend       func(uintptr, uintptr)                                     `symbol:"gtk_menu_shell_append"`
	Main             func()                                                     `symbol:"gtk_main"`
	Quit             func()                                                     `symbol:"gtk_main_quit"`
	Destroy          func(uintptr)                                              `symbol:"gtk_widget_destroy"`
	RootVisual       func(uintptr, uintptr)                                     `symbol:"gtk_widget_set_visual"`
	Screen           func(uintptr) uintptr                                      `symbol:"gtk_widget_get_screen"`
	RGBAVisual       func(uintptr) uintptr                                      `symbol:"gdk_screen_get_rgba_visual"`
	Composited       func(uintptr) int32                                        `symbol:"gdk_screen_is_composited"`
	Decorated        func(uintptr, int32)                                       `symbol:"gtk_window_set_decorated"`
	Resizable        func(uintptr, int32)                                       `symbol:"gtk_window_set_resizable"`
	Iconify          func(uintptr)                                              `symbol:"gtk_window_iconify"`
	Close            func(uintptr)                                              `symbol:"gtk_window_close"`
	MoveDrag         func(uintptr, int32, int32, int32, uint32)                 `symbol:"gtk_window_begin_move_drag"`
	GrabFocus        func(uintptr)                                              `symbol:"gtk_widget_grab_focus"`
	EventBox         func() uintptr                                             `symbol:"gtk_event_box_new"`
	EventBoxVisible  func(uintptr, int32)                                       `symbol:"gtk_event_box_set_visible_window"`
	EventButton      func(uintptr, *uint32) int32                               `symbol:"gdk_event_get_button"`
	EventCoords      func(uintptr, *float64, *float64) int32                    `symbol:"gdk_event_get_root_coords"`
	EventTime        func(uintptr) uint32                                       `symbol:"gdk_event_get_time"`
	Accessible       func(uintptr) uintptr                                      `symbol:"gtk_widget_get_accessible"`
	AccessibleName   func(uintptr, string)                                      `symbol:"atk_object_set_name"`
	FixedNew         func() uintptr                                             `symbol:"gtk_fixed_new"`
	FixedPut         func(uintptr, uintptr, int32, int32)                       `symbol:"gtk_fixed_put"`
	FixedMove        func(uintptr, uintptr, int32, int32)                       `symbol:"gtk_fixed_move"`
	ListNew          func() uintptr                                             `symbol:"gtk_list_box_new"`
	ListRow          func(uintptr, int32) uintptr                               `symbol:"gtk_list_box_get_row_at_index"`
	ListSelected     func(uintptr) uintptr                                      `symbol:"gtk_list_box_get_selected_row"`
	RowIndex         func(uintptr) int32                                        `symbol:"gtk_list_box_row_get_index"`
	ScrollNew        func(uintptr, uintptr) uintptr                             `symbol:"gtk_scrolled_window_new"`
	ScrollPolicy     func(uintptr, int32, int32)                                `symbol:"gtk_scrolled_window_set_policy"`
	ScrollOverlay    func(uintptr, int32)                                       `symbol:"gtk_scrolled_window_set_overlay_scrolling"`
	TextNew          func() uintptr                                             `symbol:"gtk_text_view_new"`
	TextEditable     func(uintptr, int32)                                       `symbol:"gtk_text_view_set_editable"`
	TextCursor       func(uintptr, int32)                                       `symbol:"gtk_text_view_set_cursor_visible"`
	TextWrap         func(uintptr, int32)                                       `symbol:"gtk_text_view_set_wrap_mode"`
	TextBuffer       func(uintptr) uintptr                                      `symbol:"gtk_text_view_get_buffer"`
	BufferSet        func(uintptr, string, int32)                               `symbol:"gtk_text_buffer_set_text"`
	LabelEllipsize   func(uintptr, int32)                                       `symbol:"gtk_label_set_ellipsize"`
	LabelLines       func(uintptr, int32)                                       `symbol:"gtk_label_set_lines"`
	LabelY           func(uintptr, float32)                                     `symbol:"gtk_label_set_yalign"`
	Style            func(uintptr) uintptr                                      `symbol:"gtk_widget_get_style_context"`
	AllocatedWidth   func(uintptr) int32                                        `symbol:"gtk_widget_get_allocated_width"`
	AllocatedHeight  func(uintptr) int32                                        `symbol:"gtk_widget_get_allocated_height"`
	RenderBackground func(uintptr, uintptr, float64, float64, float64, float64) `symbol:"gtk_render_background"`
}

func loadFunctions(handle uintptr, value any) {
	v := reflect.ValueOf(value).Elem()
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		purego.RegisterLibFunc(v.Field(i).Addr().Interface(), handle, t.Field(i).Tag.Get("symbol"))
	}
}

func ReportError(err error) { fmt.Fprintln(os.Stderr, "yaneCompanion:", diagnostics.Message(err)) }

func Run(ctx context.Context, actions Actions, show <-chan struct{}, hidden bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	lib, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return errors.New("DESKTOP_GTK_UNAVAILABLE: установите пакет с зависимостями GTK")
	}
	var gtk gtkAPI
	loadFunctions(lib, &gtk)
	loadDesktopFont()
	if gtk.InitCheck(0, 0) == 0 {
		return errors.New("DESKTOP_DISPLAY_UNAVAILABLE")
	}
	themeContext, stopTheme := context.WithCancel(ctx)
	defer stopTheme()
	portalChanges := make(chan uint32, 1)
	go watchPortalTheme(themeContext, lib, portalChanges)
	var connect func(uintptr, string, uintptr, uintptr, uintptr, uint32) uint64
	var timeout func(uint32, uintptr, uintptr) uint32
	var removeSource func(uint32) int32
	purego.RegisterLibFunc(&connect, lib, "g_signal_connect_data")
	purego.RegisterLibFunc(&timeout, lib, "g_timeout_add")
	purego.RegisterLibFunc(&removeSource, lib, "g_source_remove")
	on := func(widget uintptr, signal string, fn func()) {
		connect(widget, signal, purego.NewCallback(func(uintptr, uintptr) { fn() }), 0, 0, 0)
	}
	window := gtk.WindowNew(0)
	defer gtk.Destroy(window)
	snapshot := actions.Snapshot()
	systemTheme := gtkSystemTheme(lib, window)
	dark := snapshot.Theme.dark(systemTheme())
	applyTheme, closeTheme, err := installTheme(lib, dark)
	if err != nil {
		return err
	}
	defer closeTheme()
	gtk.WindowTitle(window, windowTitle)
	gtk.WindowSize(window, windowWidth, windowHeight)
	gtk.Decorated(window, 0)
	gtk.Resizable(window, 0)
	screen := gtk.Screen(window)
	if visual := gtk.RGBAVisual(screen); visual != 0 && gtk.Composited(screen) != 0 {
		gtk.RootVisual(window, visual)
		gtk.Name(window, "companion-window")
	} else {
		gtk.Name(window, "companion-window-opaque")
	}
	exe, _ := os.Executable()
	icon := filepath.Join(filepath.Dir(exe), "yanecompanion.png")
	gtk.WindowIcon(window, icon, 0)

	paintBackground := purego.NewCallback(func(widget, cairo, _ uintptr) int32 {
		gtk.RenderBackground(gtk.Style(widget), cairo, 0, 0, float64(gtk.AllocatedWidth(widget)), float64(gtk.AllocatedHeight(widget)))
		return 0 // Let GtkFixed draw its children above the background.
	})
	frame := func(name string) uintptr {
		widget := gtk.FixedNew()
		gtk.Name(widget, name)
		connect(widget, "draw", paintBackground, 0, 0, 0)
		return widget
	}
	root := frame("window-frame")
	gtk.Add(window, root)
	place := func(parent, widget uintptr, x, y, w, h int32) {
		if parent != root {
			x, y = x-panelX, y-panelY
		}
		gtk.Size(widget, w, h)
		gtk.FixedPut(parent, widget, x, y)
	}
	label := func(parent uintptr, name, text string, x, y, w, h int32, lines int32) uintptr {
		widget := gtk.LabelNew(text)
		gtk.Name(widget, name)
		gtk.LabelAlign(widget, 0)
		gtk.LabelEllipsize(widget, 3) // PANGO_ELLIPSIZE_END
		if lines > 1 {
			gtk.LabelWrap(widget, 1)
			gtk.LabelWrapMode(widget, 2)
			gtk.LabelLines(widget, lines)
			gtk.LabelY(widget, 0)
		}
		place(parent, widget, x, y, w, h)
		return widget
	}
	button := func(parent uintptr, name, text string, x, y, w int32, fn func()) uintptr {
		widget := gtk.ButtonNew(text)
		gtk.Name(widget, name)
		gtk.LabelEllipsize(gtk.BinChild(widget), 3)
		place(parent, widget, x, y, w, 34)
		on(widget, "clicked", fn)
		return widget
	}
	scroll := func(parent, child uintptr, name string, x, y, w, h int32) uintptr {
		widget := gtk.ScrollNew(0, 0)
		gtk.Name(widget, name)
		gtk.ScrollPolicy(widget, 2, 1) // Never horizontally, automatically vertically.
		gtk.ScrollOverlay(widget, 0)
		gtk.Add(widget, child)
		place(parent, widget, x, y, w, h)
		return widget
	}
	readOnly := func(parent uintptr, name string, x, y, w, h int32) uintptr {
		text := gtk.TextNew()
		gtk.TextEditable(text, 0)
		gtk.TextCursor(text, 0)
		gtk.TextWrap(text, 3) // GTK_WRAP_WORD_CHAR
		scroll(parent, text, name, x, y, w, h)
		return gtk.TextBuffer(text)
	}
	visible := func(widget uintptr, show bool) {
		if show {
			gtk.Show(widget)
		} else {
			gtk.Hide(widget)
		}
	}

	wallpapers := gtk.StackNew()
	place(root, wallpapers, 280, 12, 256, 84)
	if wallpaper := bannerWidget(lib, banner); wallpaper != 0 {
		gtk.StackAdd(wallpapers, wallpaper, "dark")
	}
	if wallpaper := bannerWidget(lib, lightBanner); wallpaper != 0 {
		gtk.StackAdd(wallpapers, wallpaper, "light")
	}
	showWallpaper := func() {
		if dark {
			gtk.StackShow(wallpapers, "dark")
		} else {
			gtk.StackShow(wallpapers, "light")
		}
	}
	label(root, "hero-title", windowTitle, 24, 25, 341, 36, 1)
	label(root, "hero-caption", windowSubtitle, 25, 58, 340, 22, 1)
	drag := gtk.EventBox()
	gtk.EventBoxVisible(drag, 0)
	place(root, drag, 0, 0, windowWidth, headerHeight)
	connect(drag, "button-press-event", purego.NewCallback(func(_, event, _ uintptr) int32 {
		var button uint32
		var x, y float64
		if gtk.EventButton(event, &button) == 0 || button != 1 || gtk.EventCoords(event, &x, &y) == 0 {
			return 0
		}
		gtk.MoveDrag(window, int32(button), int32(x), int32(y), gtk.EventTime(event))
		return 1
	}), 0, 0, 0)
	minimize := button(root, "minimize", "−", 450, 12, 40, func() { gtk.Iconify(window) })
	closeButton := button(root, "close", "×", 496, 12, 40, func() { gtk.Close(window) })
	gtk.Size(minimize, 40, 32)
	gtk.Size(closeButton, 40, 32)
	gtk.AccessibleName(gtk.Accessible(minimize), "Свернуть")
	gtk.AccessibleName(gtk.Accessible(closeButton), "Закрыть")

	place(root, frame("nav"), navX, headerHeight, navWidth, navHeight)
	place(root, frame("panel"), panelX, panelY, panelWidth, panelHeight)
	stack := gtk.StackNew()
	gtk.StackDuration(stack, 160)
	place(root, stack, panelX, panelY, panelWidth, panelHeight)
	panel := func(name string) uintptr {
		page := gtk.FixedNew()
		gtk.StackAdd(stack, page, name)
		return page
	}
	version := label(root, "footer", release.Version, 426, 428, 110, 20, 1)
	gtk.LabelAlign(version, 1)
	feedback := label(root, "footer", "", 24, 428, 385, 35, 2)
	report := func(err error) {
		text := ""
		if err != nil {
			text = err.Error()
		}
		gtk.LabelSet(feedback, text)
	}

	page := ""
	tabs := make([]uintptr, 0, 3)
	tabPages := [...]string{"connections", "settings", "updates"}
	var footerButtons [2]uintptr
	setPage := func(next string, animate bool) {
		if page != next {
			report(nil)
		}
		var transition int32
		if animate && page != next {
			transition = 1 // GTK_STACK_TRANSITION_TYPE_CROSSFADE
		}
		page = next
		target := next
		if next == "connections" && len(snapshot.Requests) != 0 {
			target = "pairing"
		}
		gtk.StackTransition(stack, transition)
		gtk.StackShow(stack, target)
		for i, tab := range tabs {
			style := "nav-tab"
			if tabPages[i] == next || tabPages[i] == "updates" && next == "diagnostics" {
				style = "nav-active"
			}
			gtk.Name(tab, style)
		}
		visible(feedback, next != "diagnostics")
		for _, widget := range footerButtons {
			visible(widget, next == "diagnostics")
		}
	}
	for i, text := range [...]string{"Подключение", "Настройки", "Поддержка"} {
		tabs = append(tabs, button(root, "nav-tab", text, 29+int32(i)*168, 101, 166, func() {
			setPage(tabPages[i], true)
		}))
	}

	idle := panel("connections")
	idleTitle := label(idle, "title", "", 46, 190, 468, 38, 1)
	idleBody := label(idle, "secondary", "", 46, 236, 444, 50, 2)
	siteLabel := label(idle, "site-caption", "Адрес сайта", 46, 299, 468, 22, 1)
	siteAddress := label(idle, "site-address", "", 46, 327, 468, 57, 2)
	setup := button(idle, "primary", "Указать адрес сайта", 46, 309, 192, func() { setPage("settings", true) })

	request := panel("pairing")
	label(request, "title", "Разрешить подключение?", 46, 169, 468, 33, 1)
	label(request, "secondary", "Сверьте адрес и код с сайтом.", 46, 207, 468, 26, 1)
	origin := readOnly(request, "pairing-origin", 46, 241, 466, 40)
	code := label(request, "title", "", 46, 291, 216, 38, 1)
	expires := label(request, "secondary", "", 240, 297, 270, 30, 1)
	decide := func(allow bool) {
		if len(snapshot.Requests) != 0 {
			report(actions.Decide(snapshot.Requests[0], allow))
		}
	}
	button(request, "button", "Отклонить", 214, 354, 142, func() { decide(false) })
	button(request, "primary", "Разрешить", 367, 354, 145, func() { decide(true) })

	settings := panel("settings")
	label(settings, "body", "Адрес yaneMedia", 46, 171, 468, 21, 1)
	entry := gtk.EntryNew()
	gtk.EntryLimit(entry, 512)
	gtk.EntryHint(entry, "https://media.example")
	gtk.EntrySet(entry, snapshot.Site)
	place(settings, entry, 46, 196, 345, 36)
	save := func() {
		err := actions.SetSite(actionsEntry(gtk.EntryGet(entry)))
		report(err)
		if err == nil {
			gtk.LabelSet(feedback, "Адрес сайта сохранён.")
		}
	}
	button(settings, "primary", "Сохранить", 401, 197, 111, save)
	on(entry, "activate", save)
	label(settings, "body", "Запускать при входе", 46, 243, 314, 26, 1)
	autostart := button(settings, "button", "Выключено", 401, 239, 111, func() {
		report(actions.Autostart(!snapshot.Autostart))
	})
	label(settings, "body", "Тема", 46, 285, 112, 26, 1)
	var themeButtons [len(themeChoices)]uintptr
	for i, choice := range themeChoices {
		widget := gtk.RadioNew(themeButtons[0], choice.label)
		themeButtons[i] = widget
		gtk.ToggleMode(widget, 0)
		gtk.Name(widget, "theme-choice")
		place(settings, widget, 171+int32(i)*114, 281, 113, 34)
	}
	var portalScheme uint32
	var reconciling bool
	refreshTheme := func(mode ThemeMode) {
		select {
		case portalScheme = <-portalChanges:
		default:
		}
		systemDark := portalScheme == 1 || portalScheme != 2 && systemTheme()
		if next := mode.dark(systemDark); dark != next {
			dark = next
			applyTheme(dark)
			showWallpaper()
		}
		reconciling = true
		for i, choice := range themeChoices {
			if mode == choice.mode || (!mode.valid() && choice.mode == ThemeSystem) {
				gtk.CheckSet(themeButtons[i], 1)
				break
			}
		}
		reconciling = false
	}
	for i, choice := range themeChoices {
		widget := themeButtons[i]
		on(widget, "toggled", func() {
			if !reconciling && gtk.CheckGet(widget) != 0 {
				report(actions.SetTheme(choice.mode))
				snapshot = actions.Snapshot()
				refreshTheme(snapshot.Theme)
			}
		})
	}
	label(settings, "body", "Разрешённые подключения", 46, 323, 468, 24, 1)
	grants := gtk.ListNew()
	grantField := scroll(settings, grants, "grant-field", 46, 349, 331, 53)
	gtk.AccessibleName(gtk.Accessible(grants), "Разрешённые подключения")
	selectedGrant := func() int32 {
		if row := gtk.ListSelected(grants); row != 0 {
			return gtk.RowIndex(row)
		}
		return -1
	}
	revoke := button(settings, "button", "Отозвать", 401, 358, 111, func() {
		if index := selectedGrant(); index >= 0 && int(index) < len(snapshot.Grants) {
			report(actions.Revoke(snapshot.Grants[index].ID))
		}
	})
	syncRevoke := func() {
		var enabled int32
		if index := selectedGrant(); index >= 0 && int(index) < len(snapshot.Grants) {
			enabled = 1
		}
		gtk.Sensitive(revoke, enabled)
	}
	on(grants, "selected-rows-changed", syncRevoke)

	updates := panel("updates")
	label(updates, "heading", "Обновления", 46, 169, 468, 32, 1)
	updateLabel := label(updates, "secondary", "", 46, 207, 468, 53, 2)
	updateAction := func(run func() error) func() {
		return func() {
			if err := run(); err != nil {
				report(errors.New(diagnostics.Message(err)))
			} else {
				report(nil)
			}
		}
	}
	updateButtons := [...]uintptr{
		button(updates, "button", "Проверить обновления", 46, 264, 225, updateAction(actions.CheckUpdate)),
		button(updates, "button", "Скачать обновление", 283, 264, 229, updateAction(func() error { return actions.DownloadUpdate(false) })),
		button(updates, "button", "Скачать предыдущую версию", 46, 307, 225, updateAction(func() error { return actions.DownloadUpdate(true) })),
	}
	folder := button(updates, "button", "Открыть папку пакетов", 283, 307, 229, func() {
		err := actions.UpdateFolder()
		report(err)
		if err == nil {
			gtk.LabelSet(feedback, "Папка пакетов открыта.")
		}
	})
	diagnosis := panel("diagnostics")
	label(diagnosis, "heading", "Диагностика", 46, 169, 468, 32, 1)
	label(diagnosis, "caption", "Технический отчёт о работе помощника.", 46, 205, 468, 22, 1)
	reportBuffer := readOnly(diagnosis, "diagnostic-report", 46, 230, 466, 160)
	var displayedReport string
	footerButtons[0] = button(root, "button", "Назад", 24, 421, 96, func() { setPage("updates", true) })
	footerButtons[1] = button(root, "button", "Скопировать отчёт", 130, 421, 179, func() {
		if copyReport(lib, displayedReport) {
			gtk.ButtonLabel(footerButtons[1], "Отчёт скопирован")
		} else {
			gtk.ButtonLabel(footerButtons[1], "Повторить копирование")
		}
	})
	diagnostic := button(updates, "button", "Диагностика", 46, 350, 225, func() {
		displayedReport = actions.Diagnostics()
		gtk.BufferSet(reportBuffer, displayedReport, -1)
		gtk.ButtonLabel(footerButtons[1], "Скопировать отчёт")
		setPage("diagnostics", true)
		gtk.GrabFocus(footerButtons[1])
	})

	var tray uintptr
	var getProperty func(uintptr, string, *int32, uintptr)
	purego.RegisterLibFunc(&getProperty, lib, "g_object_get")
	hasTray := func() bool {
		var connected int32
		if tray != 0 {
			getProperty(tray, "connected", &connected, 0)
		}
		return connected != 0
	}
	inTray := false
	present := func() {
		inTray = false
		gtk.WindowPresent(window)
	}
	hide := func() {
		gtk.Hide(window)
		inTray = true
	}
	connect(window, "delete-event", purego.NewCallback(func(uintptr, uintptr, uintptr) int32 {
		if hasTray() {
			hide()
		} else {
			gtk.Quit()
		}
		return 1
	}), 0, 0, 0)
	indicatorLib, err := purego.Dlopen("libayatana-appindicator3.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err == nil {
		var indicator struct {
			Create    func(string, string, int32) uintptr `symbol:"app_indicator_new"`
			SetMenu   func(uintptr, uintptr)              `symbol:"app_indicator_set_menu"`
			SetStatus func(uintptr, int32)                `symbol:"app_indicator_set_status"`
		}
		loadFunctions(indicatorLib, &indicator)
		tray = indicator.Create("yanecompanion", icon, 0)
		menu := gtk.MenuNew()
		open := gtk.MenuItem("Открыть yaneCompanion")
		quit := gtk.MenuItem("Выйти")
		gtk.MenuAppend(menu, open)
		gtk.MenuAppend(menu, quit)
		on(open, "activate", present)
		on(quit, "activate", gtk.Quit)
		gtk.ShowAll(menu)
		indicator.SetMenu(tray, menu)
		indicator.SetStatus(tray, 1)
		defer indicator.SetStatus(tray, 0)
	}

	lastRequest, lastGrants := "", "\x00"
	lastText := map[uintptr]string{}
	setText := func(widget uintptr, text string) {
		if lastText[widget] != text {
			gtk.LabelSet(widget, text)
			lastText[widget] = text
		}
	}
	var lastAutostart, lastUpdates bool
	refresh := func(first bool) {
		snapshot = actions.Snapshot()
		refreshTheme(snapshot.Theme)
		setText(updateLabel, snapshot.Update)
		if first || lastUpdates != snapshot.UpdatesAvailable {
			lastUpdates = snapshot.UpdatesAvailable
			for _, widget := range updateButtons {
				visible(widget, lastUpdates)
			}
			if lastUpdates {
				gtk.FixedMove(updates, folder, 283-panelX, 307-panelY)
				gtk.FixedMove(updates, diagnostic, 46-panelX, 350-panelY)
			} else {
				gtk.FixedMove(updates, folder, 46-panelX, 264-panelY)
				gtk.FixedMove(updates, diagnostic, 283-panelX, 264-panelY)
			}
		}
		title, body := snapshot.connectionText()
		site := snapshot.ActiveSite()
		setText(idleTitle, title)
		setText(idleBody, body)
		setText(siteAddress, site)
		visible(siteLabel, site != "")
		visible(siteAddress, site != "")
		visible(setup, snapshot.Ready && site == "")
		if len(snapshot.Requests) == 0 {
			if lastRequest != "" {
				lastRequest = ""
				if page == "connections" {
					setPage(page, false)
				}
			}
		} else {
			r := snapshot.Requests[0]
			setText(code, r.Code)
			setText(expires, "Действует до "+r.ExpiresAt.Local().Format("15:04:05"))
			if r.RequestID != lastRequest {
				lastRequest = r.RequestID
				gtk.BufferSet(origin, r.Origin, -1)
				report(nil)
				setPage("connections", false)
				present()
			}
		}
		if first || lastAutostart != snapshot.Autostart {
			lastAutostart = snapshot.Autostart
			text := "Выключено"
			if lastAutostart {
				text = "Включено"
			}
			gtk.ButtonLabel(autostart, text)
		}
		key := ""
		for _, g := range snapshot.Grants {
			key += g.ID
		}
		if key != lastGrants {
			for row := gtk.ListRow(grants, 0); row != 0; row = gtk.ListRow(grants, 0) {
				gtk.Destroy(row)
			}
			addGrant := func(text string) {
				widget := gtk.LabelNew(text)
				gtk.LabelAlign(widget, 0)
				gtk.LabelEllipsize(widget, 3)
				gtk.Add(grants, widget)
			}
			var enabled int32
			if len(snapshot.Grants) == 0 {
				addGrant("Нет подключений")
			} else {
				enabled = 1
				for _, g := range snapshot.Grants {
					addGrant(g.Origin + " — " + g.CreatedAt.Local().Format("02.01 15:04"))
				}
			}
			gtk.ShowAll(grants)
			gtk.Sensitive(grantField, enabled)
			lastGrants = key
		}
		syncRevoke()
	}
	gtk.ShowAll(root)
	showWallpaper()
	setPage("connections", false)
	refresh(true)

	firstTick := true
	source := timeout(500, purego.NewCallback(func(uintptr) int32 {
		select {
		case <-ctx.Done():
			gtk.Quit()
			return 1
		default:
		}
		refresh(false)
		if firstTick {
			firstTick = false
			if !hidden || !hasTray() || len(snapshot.Requests) > 0 {
				present()
			} else {
				inTray = true
			}
		}
		if inTray && !hasTray() {
			present()
		}
		select {
		case <-show:
			present()
		default:
		}
		return 1
	}), 0)
	defer removeSource(source)
	gtk.Main()
	return nil
}

func actionsEntry(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), 2049)
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return ""
}
