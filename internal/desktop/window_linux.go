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
	StackNew        func() uintptr                               `symbol:"gtk_stack_new"`
	StackAdd        func(uintptr, uintptr, string)               `symbol:"gtk_stack_add_named"`
	StackShow       func(uintptr, string)                        `symbol:"gtk_stack_set_visible_child_name"`
	StackTransition func(uintptr, int32)                         `symbol:"gtk_stack_set_transition_type"`
	StackDuration   func(uintptr, uint32)                        `symbol:"gtk_stack_set_transition_duration"`
	OverlayNew      func() uintptr                               `symbol:"gtk_overlay_new"`
	OverlayAdd      func(uintptr, uintptr)                       `symbol:"gtk_overlay_add_overlay"`
	Name            func(uintptr, string)                        `symbol:"gtk_widget_set_name"`
	Size            func(uintptr, int32, int32)                  `symbol:"gtk_widget_set_size_request"`
	AlignY          func(uintptr, int32)                         `symbol:"gtk_widget_set_valign"`
	AlignX          func(uintptr, int32)                         `symbol:"gtk_widget_set_halign"`
	MarginStart     func(uintptr, int32)                         `symbol:"gtk_widget_set_margin_start"`
	PackEnd         func(uintptr, uintptr, int32, int32, uint32) `symbol:"gtk_box_pack_end"`
	LabelWrapMode   func(uintptr, int32)                         `symbol:"gtk_label_set_line_wrap_mode"`
	Show            func(uintptr)                                `symbol:"gtk_widget_show"`
	InitCheck       func(uintptr, uintptr) int32                 `symbol:"gtk_init_check"`
	WindowNew       func(int32) uintptr                          `symbol:"gtk_window_new"`
	WindowTitle     func(uintptr, string)                        `symbol:"gtk_window_set_title"`
	WindowSize      func(uintptr, int32, int32)                  `symbol:"gtk_window_set_default_size"`
	WindowPresent   func(uintptr)                                `symbol:"gtk_window_present"`
	WindowIcon      func(uintptr, string, uintptr) int32         `symbol:"gtk_window_set_icon_from_file"`
	BoxNew          func(int32, int32) uintptr                   `symbol:"gtk_box_new"`
	Pack            func(uintptr, uintptr, int32, int32, uint32) `symbol:"gtk_box_pack_start"`
	Add             func(uintptr, uintptr)                       `symbol:"gtk_container_add"`
	Border          func(uintptr, uint32)                        `symbol:"gtk_container_set_border_width"`
	LabelNew        func(string) uintptr                         `symbol:"gtk_label_new"`
	LabelSet        func(uintptr, string)                        `symbol:"gtk_label_set_text"`
	LabelWrap       func(uintptr, int32)                         `symbol:"gtk_label_set_line_wrap"`
	LabelSelect     func(uintptr, int32)                         `symbol:"gtk_label_set_selectable"`
	LabelAlign      func(uintptr, float32)                       `symbol:"gtk_label_set_xalign"`
	EntryNew        func() uintptr                               `symbol:"gtk_entry_new"`
	EntrySet        func(uintptr, string)                        `symbol:"gtk_entry_set_text"`
	EntryHint       func(uintptr, string)                        `symbol:"gtk_entry_set_placeholder_text"`
	EntryGet        func(uintptr) uintptr                        `symbol:"gtk_entry_get_text"`
	EntryLimit      func(uintptr, int32)                         `symbol:"gtk_entry_set_max_length"`
	ButtonNew       func(string) uintptr                         `symbol:"gtk_button_new_with_label"`
	ButtonLabel     func(uintptr, string)                        `symbol:"gtk_button_set_label"`
	CheckNew        func(string) uintptr                         `symbol:"gtk_check_button_new_with_label"`
	CheckGet        func(uintptr) int32                          `symbol:"gtk_toggle_button_get_active"`
	CheckSet        func(uintptr, int32)                         `symbol:"gtk_toggle_button_set_active"`
	ComboNew        func() uintptr                               `symbol:"gtk_combo_box_text_new"`
	ComboAdd        func(uintptr, string)                        `symbol:"gtk_combo_box_text_append_text"`
	ComboClear      func(uintptr)                                `symbol:"gtk_combo_box_text_remove_all"`
	ComboIndex      func(uintptr) int32                          `symbol:"gtk_combo_box_get_active"`
	ComboSelect     func(uintptr, int32)                         `symbol:"gtk_combo_box_set_active"`
	ShowAll         func(uintptr)                                `symbol:"gtk_widget_show_all"`
	Hide            func(uintptr)                                `symbol:"gtk_widget_hide"`
	Sensitive       func(uintptr, int32)                         `symbol:"gtk_widget_set_sensitive"`
	MenuNew         func() uintptr                               `symbol:"gtk_menu_new"`
	MenuItem        func(string) uintptr                         `symbol:"gtk_menu_item_new_with_label"`
	MenuAppend      func(uintptr, uintptr)                       `symbol:"gtk_menu_shell_append"`
	Main            func()                                       `symbol:"gtk_main"`
	Quit            func()                                       `symbol:"gtk_main_quit"`
	Destroy         func(uintptr)                                `symbol:"gtk_widget_destroy"`
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
	if err := installTheme(lib); err != nil {
		return err
	}
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
	gtk.Name(window, "companion-window")
	gtk.WindowTitle(window, windowTitle)
	gtk.WindowSize(window, 560, 438)
	exe, _ := os.Executable()
	icon := filepath.Join(filepath.Dir(exe), "yanecompanion.png")
	gtk.WindowIcon(window, icon, 0)

	root := gtk.BoxNew(1, 16)
	gtk.Border(root, 24)
	gtk.Add(window, root)
	pack := func(parent, child uintptr) { gtk.Pack(parent, child, 0, 0, 0) }
	label := func(parent uintptr, name, text string) uintptr {
		w := gtk.LabelNew(text)
		gtk.LabelWrap(w, 1)
		gtk.LabelWrapMode(w, 2)
		gtk.LabelAlign(w, 0)
		gtk.Name(w, name)
		pack(parent, w)
		return w
	}
	hero := gtk.OverlayNew()
	gtk.Size(hero, 512, 84)
	pack(root, hero)
	if wallpaper := bannerWidget(lib); wallpaper != 0 {
		gtk.Add(hero, wallpaper)
	}
	heroCopy := gtk.BoxNew(1, 3)
	gtk.AlignX(heroCopy, 1)
	gtk.MarginStart(heroCopy, 63)
	gtk.AlignY(heroCopy, 3)
	gtk.OverlayAdd(hero, heroCopy)
	label(heroCopy, "hero-title", windowTitle)
	label(heroCopy, "hero-caption", windowSubtitle)

	stack := gtk.StackNew()
	gtk.StackDuration(stack, 160)
	nav := gtk.BoxNew(0, 10)
	pack(root, nav)
	gtk.Pack(root, stack, 1, 1, 0)
	footer := gtk.BoxNew(0, 10)
	pack(root, footer)
	version := gtk.LabelNew(release.Version)
	gtk.Name(version, "secondary")
	gtk.PackEnd(footer, version, 0, 0, 0)

	panel := func(name string) uintptr {
		frame := gtk.BoxNew(1, 0)
		gtk.Name(frame, "panel")
		gtk.StackAdd(stack, frame, name)
		box := gtk.BoxNew(1, 10)
		gtk.Border(box, 20)
		gtk.Pack(frame, box, 1, 1, 0)
		return box
	}
	button := func(parent uintptr, text string, fn func()) uintptr {
		w := gtk.ButtonNew(text)
		pack(parent, w)
		on(w, "clicked", fn)
		return w
	}
	tabs := map[string]uintptr{}
	setPage := func(name string, animate bool) {
		var transition int32
		if animate {
			transition = 1 // GTK_STACK_TRANSITION_TYPE_CROSSFADE
		}
		gtk.StackTransition(stack, transition)
		gtk.StackShow(stack, name)
		for tab, handle := range tabs {
			style := "nav-tab"
			if tab == name || tab == "updates" && name == "diagnostics" {
				style = "nav-active"
			}
			gtk.Name(handle, style)
		}
	}
	tabs["connections"] = button(nav, "Подключение", func() { setPage("connections", true) })
	tabs["settings"] = button(nav, "Настройки", func() { setPage("settings", true) })
	tabs["updates"] = button(nav, "Поддержка", func() { setPage("updates", true) })

	main := panel("connections")
	connection := gtk.StackNew()
	gtk.Pack(main, connection, 1, 1, 0)
	idle := gtk.BoxNew(1, 12)
	gtk.AlignY(idle, 3)
	gtk.StackAdd(connection, idle, "idle")
	idleTitle := label(idle, "heading", "")
	idleBody := label(idle, "secondary", "")
	siteAddress := label(idle, "site-address", "")
	setup := button(idle, "Указать адрес сайта", func() { setPage("settings", true) })
	gtk.Name(setup, "primary")
	gtk.AlignX(setup, 1)
	request := gtk.BoxNew(1, 10)
	gtk.StackAdd(connection, request, "pending")
	label(request, "heading", "Разрешить подключение?")
	label(request, "secondary", "Сверьте адрес и код с сайтом.")
	origin := label(request, "origin", "")
	codeRow := gtk.BoxNew(0, 18)
	pack(request, codeRow)
	code := label(codeRow, "heading", "")
	expires := label(codeRow, "secondary", "")
	var snapshot Snapshot
	decisionRow := gtk.BoxNew(0, 10)
	gtk.PackEnd(request, decisionRow, 0, 0, 0)
	gtk.AlignX(decisionRow, 2)
	var mainFeedback, settingsFeedback, updateFeedback uintptr
	report := func(target uintptr, err error) {
		text := ""
		if err != nil {
			text = err.Error()
		}
		gtk.LabelSet(target, text)
	}
	decide := func(allow bool) {
		if len(snapshot.Requests) > 0 {
			report(mainFeedback, actions.Decide(snapshot.Requests[0], allow))
		}
	}
	button(decisionRow, "Отклонить", func() { decide(false) })
	gtk.Name(button(decisionRow, "Разрешить", func() { decide(true) }), "primary")
	mainFeedback = label(request, "secondary", "")

	settings := panel("settings")
	label(settings, "heading", "Настройки")
	label(settings, "secondary", "Адрес yaneMedia")
	row := gtk.BoxNew(0, 10)
	pack(settings, row)
	entry := gtk.EntryNew()
	gtk.EntryLimit(entry, 512)
	gtk.EntryHint(entry, "https://media.example")
	gtk.EntrySet(entry, actions.Snapshot().Site)
	gtk.Pack(row, entry, 1, 1, 0)
	save := func() {
		err := actions.SetSite(actionsEntry(gtk.EntryGet(entry)))
		report(settingsFeedback, err)
		if err == nil {
			gtk.LabelSet(settingsFeedback, "Адрес сайта сохранён.")
		}
	}
	gtk.Name(button(row, "Сохранить", save), "primary")
	on(entry, "activate", save)
	autostart := gtk.CheckNew("Запускать при входе в систему")
	pack(settings, autostart)
	reconciling := false
	on(autostart, "toggled", func() {
		if !reconciling {
			report(settingsFeedback, actions.Autostart(gtk.CheckGet(autostart) != 0))
		}
	})
	label(settings, "heading", "Разрешённые подключения")
	grantRow := gtk.BoxNew(0, 10)
	pack(settings, grantRow)
	combo := gtk.ComboNew()
	gtk.Pack(grantRow, combo, 1, 1, 0)
	revoke := button(grantRow, "Отозвать", func() {
		index := gtk.ComboIndex(combo)
		if index >= 0 && int(index) < len(snapshot.Grants) {
			report(settingsFeedback, actions.Revoke(snapshot.Grants[index].ID))
		}
	})
	syncRevoke := func() {
		enabled := int32(0)
		if gtk.ComboIndex(combo) >= 0 && len(snapshot.Grants) > 0 {
			enabled = 1
		}
		gtk.Sensitive(revoke, enabled)
	}
	on(combo, "changed", syncRevoke)
	settingsFeedback = label(settings, "secondary", "")

	updates := panel("updates")
	label(updates, "heading", "Обновления")
	updateLabel := label(updates, "secondary", "")
	var updateButtons []uintptr
	for _, pair := range [][2]struct {
		text string
		run  func() error
	}{
		{{"Проверить обновления", actions.CheckUpdate}, {"Скачать обновление", func() error { return actions.DownloadUpdate(false) }}},
	} {
		row := gtk.BoxNew(0, 10)
		pack(updates, row)
		for _, item := range pair {
			updateButtons = append(updateButtons, button(row, item.text, func() {
				err := item.run()
				if err != nil {
					err = errors.New(diagnostics.Message(err))
				}
				report(updateFeedback, err)
			}))
		}
	}
	folderRow := gtk.BoxNew(0, 10)
	pack(updates, folderRow)
	updateButtons = append(updateButtons, button(folderRow, "Скачать предыдущую версию", func() {
		if err := actions.DownloadUpdate(true); err != nil {
			report(updateFeedback, errors.New(diagnostics.Message(err)))
		} else {
			report(updateFeedback, nil)
		}
	}))
	button(folderRow, "Открыть папку пакетов", func() {
		if err := actions.UpdateFolder(); err != nil {
			report(updateFeedback, err)
		} else {
			gtk.LabelSet(updateFeedback, "Папка пакетов открыта.")
		}
	})
	updateFeedback = label(updates, "secondary", "")
	diagnosis := panel("diagnostics")
	label(diagnosis, "heading", "Диагностика")
	label(diagnosis, "secondary", "Технический отчёт о работе помощника.")
	reportText := label(diagnosis, "diagnostic-report", "")
	gtk.LabelSelect(reportText, 1)
	diagnosticNav := gtk.BoxNew(0, 10)
	gtk.PackEnd(diagnosis, diagnosticNav, 0, 0, 0)
	button(diagnosticNav, "Назад", func() { setPage("updates", true) })
	var displayedReport string
	var copyButton uintptr
	copyButton = button(diagnosticNav, "Скопировать отчёт", func() {
		if copyReport(lib, displayedReport) {
			gtk.ButtonLabel(copyButton, "Отчёт скопирован")
		} else {
			gtk.ButtonLabel(copyButton, "Повторить копирование")
		}
	})
	button(updates, "Диагностика", func() {
		displayedReport = actions.Diagnostics()
		gtk.LabelSet(reportText, displayedReport)
		gtk.ButtonLabel(copyButton, "Скопировать отчёт")
		setPage("diagnostics", true)
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
	refresh := func() {
		snapshot = actions.Snapshot()
		setText(updateLabel, snapshot.Update)
		for _, widget := range updateButtons {
			if snapshot.UpdatesAvailable {
				gtk.Show(widget)
			} else {
				gtk.Hide(widget)
			}
		}
		title, body := snapshot.connectionText()
		setText(idleTitle, title)
		setText(idleBody, body)
		setText(siteAddress, snapshot.ActiveSite())
		if snapshot.Ready && snapshot.ActiveSite() == "" {
			gtk.Show(setup)
		} else {
			gtk.Hide(setup)
		}
		if len(snapshot.Requests) == 0 {
			gtk.StackShow(connection, "idle")
			lastRequest = ""
		} else {
			r := snapshot.Requests[0]
			setText(origin, r.Origin)
			setText(code, r.Code)
			setText(expires, "Действует до "+r.ExpiresAt.Local().Format("15:04:05"))
			gtk.StackShow(connection, "pending")
			if r.RequestID != lastRequest {
				lastRequest = r.RequestID
				gtk.LabelSet(mainFeedback, "")
				setPage("connections", false)
				present()
			}
		}
		reconciling = true
		checked := int32(0)
		if snapshot.Autostart {
			checked = 1
		}
		gtk.CheckSet(autostart, checked)
		reconciling = false
		key := ""
		for _, g := range snapshot.Grants {
			key += g.ID
		}
		if key != lastGrants {
			gtk.ComboClear(combo)
			if len(snapshot.Grants) == 0 {
				gtk.ComboAdd(combo, "Нет подключений")
				gtk.ComboSelect(combo, 0)
			}
			for _, g := range snapshot.Grants {
				gtk.ComboAdd(combo, g.Origin+" — "+g.CreatedAt.Local().Format("02.01 15:04"))
			}
			lastGrants = key
			enabled := int32(0)
			if len(snapshot.Grants) > 0 {
				enabled = 1
			}
			gtk.Sensitive(combo, enabled)
		}
		syncRevoke()
	}
	gtk.ShowAll(root)
	setPage("connections", false)
	refresh()
	firstTick := true
	source := timeout(500, purego.NewCallback(func(uintptr) int32 {
		select {
		case <-ctx.Done():
			gtk.Quit()
			return 1
		default:
		}
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
		refresh()
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
