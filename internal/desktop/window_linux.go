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
	NotebookNew    func() uintptr                               `symbol:"gtk_notebook_new"`
	NotebookAppend func(uintptr, uintptr, uintptr) int32        `symbol:"gtk_notebook_append_page"`
	NotebookPage   func(uintptr, int32)                         `symbol:"gtk_notebook_set_current_page"`
	InitCheck      func(uintptr, uintptr) int32                 `symbol:"gtk_init_check"`
	WindowNew      func(int32) uintptr                          `symbol:"gtk_window_new"`
	WindowTitle    func(uintptr, string)                        `symbol:"gtk_window_set_title"`
	WindowSize     func(uintptr, int32, int32)                  `symbol:"gtk_window_set_default_size"`
	WindowPresent  func(uintptr)                                `symbol:"gtk_window_present"`
	WindowIcon     func(uintptr, string, uintptr) int32         `symbol:"gtk_window_set_icon_from_file"`
	BoxNew         func(int32, int32) uintptr                   `symbol:"gtk_box_new"`
	Pack           func(uintptr, uintptr, int32, int32, uint32) `symbol:"gtk_box_pack_start"`
	Add            func(uintptr, uintptr)                       `symbol:"gtk_container_add"`
	Border         func(uintptr, uint32)                        `symbol:"gtk_container_set_border_width"`
	LabelNew       func(string) uintptr                         `symbol:"gtk_label_new"`
	LabelSet       func(uintptr, string)                        `symbol:"gtk_label_set_text"`
	LabelWrap      func(uintptr, int32)                         `symbol:"gtk_label_set_line_wrap"`
	LabelSelect    func(uintptr, int32)                         `symbol:"gtk_label_set_selectable"`
	LabelAlign     func(uintptr, float32)                       `symbol:"gtk_label_set_xalign"`
	EntryNew       func() uintptr                               `symbol:"gtk_entry_new"`
	EntrySet       func(uintptr, string)                        `symbol:"gtk_entry_set_text"`
	EntryGet       func(uintptr) uintptr                        `symbol:"gtk_entry_get_text"`
	EntryLimit     func(uintptr, int32)                         `symbol:"gtk_entry_set_max_length"`
	ButtonNew      func(string) uintptr                         `symbol:"gtk_button_new_with_label"`
	CheckNew       func(string) uintptr                         `symbol:"gtk_check_button_new_with_label"`
	CheckGet       func(uintptr) int32                          `symbol:"gtk_toggle_button_get_active"`
	CheckSet       func(uintptr, int32)                         `symbol:"gtk_toggle_button_set_active"`
	ComboNew       func() uintptr                               `symbol:"gtk_combo_box_text_new"`
	ComboAdd       func(uintptr, string)                        `symbol:"gtk_combo_box_text_append_text"`
	ComboClear     func(uintptr)                                `symbol:"gtk_combo_box_text_remove_all"`
	ComboIndex     func(uintptr) int32                          `symbol:"gtk_combo_box_get_active"`
	ShowAll        func(uintptr)                                `symbol:"gtk_widget_show_all"`
	Hide           func(uintptr)                                `symbol:"gtk_widget_hide"`
	Sensitive      func(uintptr, int32)                         `symbol:"gtk_widget_set_sensitive"`
	Focus          func(uintptr)                                `symbol:"gtk_widget_grab_focus"`
	MenuNew        func() uintptr                               `symbol:"gtk_menu_new"`
	MenuItem       func(string) uintptr                         `symbol:"gtk_menu_item_new_with_label"`
	MenuLabel      func(uintptr, string)                        `symbol:"gtk_menu_item_set_label"`
	MenuAppend     func(uintptr, uintptr)                       `symbol:"gtk_menu_shell_append"`
	Main           func()                                       `symbol:"gtk_main"`
	Quit           func()                                       `symbol:"gtk_main_quit"`
	Destroy        func(uintptr)                                `symbol:"gtk_widget_destroy"`
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
	if gtk.InitCheck(0, 0) == 0 {
		return errors.New("DESKTOP_DISPLAY_UNAVAILABLE")
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
	gtk.WindowTitle(window, "yaneCompanion")
	gtk.WindowSize(window, 620, 650)
	exe, _ := os.Executable()
	icon := filepath.Join(filepath.Dir(exe), "yanecompanion.png")
	gtk.WindowIcon(window, icon, 0)
	box := gtk.BoxNew(1, 12)
	gtk.Border(box, 20)
	notebook := gtk.NotebookNew()
	gtk.Add(window, notebook)
	gtk.NotebookAppend(notebook, box, gtk.LabelNew("Подключения"))
	pack := func(parent, child uintptr) { gtk.Pack(parent, child, 0, 0, 0) }
	label := func(text string) uintptr {
		w := gtk.LabelNew(text)
		gtk.LabelWrap(w, 1)
		gtk.LabelAlign(w, 0)
		pack(box, w)
		return w
	}
	status := label("Запускаем yaneCompanion…")
	label("Адрес сайта (HTTPS, без пути)")
	row := gtk.BoxNew(0, 8)
	pack(box, row)
	entry := gtk.EntryNew()
	gtk.EntryLimit(entry, 512)
	gtk.EntrySet(entry, actions.Snapshot().Site)
	gtk.Pack(row, entry, 1, 1, 0)
	feedback := uintptr(0)
	serviceFeedback := uintptr(0)
	report := func(err error) {
		text := ""
		if err != nil {
			text = "Не удалось выполнить действие. " + err.Error()
		}
		if feedback != 0 {
			gtk.LabelSet(feedback, text)
		}
		if serviceFeedback != 0 {
			gtk.LabelSet(serviceFeedback, text)
		}
	}
	button := func(parent uintptr, text string, fn func()) uintptr {
		w := gtk.ButtonNew(text)
		pack(parent, w)
		on(w, "clicked", fn)
		return w
	}
	button(row, "Сохранить", func() {
		ptr := actionsEntry(gtk.EntryGet(entry))
		report(actions.SetSite(ptr))
	})
	button(box, "Открыть сайт", func() { report(actions.OpenSite()) })
	autostart := gtk.CheckNew("Запускать при входе в систему")
	pack(box, autostart)
	updating := false
	on(autostart, "toggled", func() {
		if !updating {
			report(actions.Autostart(gtk.CheckGet(autostart) != 0))
		}
	})
	label("Подтверждение подключения — сравните адрес и код с сайтом")
	requestLabel := label("")
	gtk.LabelSelect(requestLabel, 1)
	var snapshot Snapshot
	row = gtk.BoxNew(0, 8)
	pack(box, row)
	approve := button(row, "Разрешить", func() {
		if len(snapshot.Requests) > 0 {
			report(actions.Decide(snapshot.Requests[0], true))
		}
	})
	deny := button(row, "Отклонить", func() {
		if len(snapshot.Requests) > 0 {
			report(actions.Decide(snapshot.Requests[0], false))
		}
	})
	label("Разрешённые подключения")
	combo := gtk.ComboNew()
	pack(box, combo)
	button(box, "Отозвать выбранный доступ", func() {
		index := gtk.ComboIndex(combo)
		if index >= 0 && int(index) < len(snapshot.Grants) {
			report(actions.Revoke(snapshot.Grants[index].ID))
		}
	})
	feedback = label("")
	var tray uintptr
	var trayStatus uintptr
	var getProperty func(uintptr, string, *int32, uintptr)
	purego.RegisterLibFunc(&getProperty, lib, "g_object_get")
	hasTray := func() bool {
		var connected int32
		if tray != 0 {
			getProperty(tray, "connected", &connected, 0)
		}
		return connected != 0
	}
	hide := func() {
		if hasTray() {
			gtk.Hide(window)
		} else {
			gtk.LabelSet(feedback, "Трей недоступен в этом desktop. Окно остаётся открытым; его можно свернуть.")
		}
	}
	row = gtk.BoxNew(0, 8)
	pack(box, row)
	button(row, "Скрыть в трей", hide)
	button(row, "Выход — остановить просмотр", gtk.Quit)
	connect(window, "delete-event", purego.NewCallback(func(uintptr, uintptr, uintptr) int32 { hide(); return 1 }), 0, 0, 0)
	indicatorLib, err := purego.Dlopen("libayatana-appindicator3.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err == nil {
		var create func(string, string, int32) uintptr
		var setMenu func(uintptr, uintptr)
		var setStatus func(uintptr, int32)
		purego.RegisterLibFunc(&create, indicatorLib, "app_indicator_new")
		purego.RegisterLibFunc(&setMenu, indicatorLib, "app_indicator_set_menu")
		purego.RegisterLibFunc(&setStatus, indicatorLib, "app_indicator_set_status")
		tray = create("yanecompanion", icon, 0)
		menu := gtk.MenuNew()
		trayStatus = gtk.MenuItem("yaneCompanion")
		gtk.MenuAppend(menu, trayStatus)
		gtk.Sensitive(trayStatus, 0)
		for _, item := range []struct {
			label  string
			action func()
		}{{"Открыть управление", func() { gtk.WindowPresent(window) }}, {"Открыть сайт", func() { report(actions.OpenSite()) }}, {"Выход — остановить просмотр", gtk.Quit}} {
			w := gtk.MenuItem(item.label)
			gtk.MenuAppend(menu, w)
			on(w, "activate", item.action)
		}
		gtk.ShowAll(menu)
		setMenu(tray, menu)
		setStatus(tray, 1)
		defer setStatus(tray, 0)
	}
	lastRequest, lastGrants := "", ""
	refresh := func() {
		snapshot = actions.Snapshot()
		gtk.LabelSet(status, snapshot.Status+"\n"+snapshot.Activity)
		if trayStatus != 0 {
			gtk.MenuLabel(trayStatus, snapshot.Status+" · "+snapshot.Activity)
		}
		updating = true
		checked := int32(0)
		if snapshot.Autostart {
			checked = 1
		}
		gtk.CheckSet(autostart, checked)
		updating = false
		requestText := "Нет запросов. Нажмите «Подключить» на сайте."
		enabled := int32(0)
		if len(snapshot.Requests) > 0 {
			r := snapshot.Requests[0]
			enabled = 1
			requestText = r.Origin + "\nКод: " + r.Code + "   (до " + r.ExpiresAt.Local().Format("15:04:05") + ")"
			if lastRequest != r.RequestID {
				gtk.NotebookPage(notebook, 0)
				gtk.WindowPresent(window)
				lastRequest = r.RequestID
			}
		}
		gtk.LabelSet(requestLabel, requestText)
		gtk.Sensitive(approve, enabled)
		gtk.Sensitive(deny, enabled)
		key := ""
		for _, g := range snapshot.Grants {
			key += g.ID
		}
		if key != lastGrants {
			gtk.ComboClear(combo)
			for _, g := range snapshot.Grants {
				gtk.ComboAdd(combo, g.Origin+" — "+g.CreatedAt.Local().Format("02.01 15:04"))
			}
			lastGrants = key
		}
	}
	box = gtk.BoxNew(1, 12)
	gtk.Border(box, 20)
	gtk.NotebookAppend(notebook, box, gtk.LabelNew("Обслуживание"))
	label("yaneCompanion " + release.Version + " · API 1.0")
	updateLabel := label("")
	button(box, "Проверить обновления", func() { report(actions.CheckUpdate()) })
	for _, previous := range []bool{false, true} {
		text := "Скачать полный пакет"
		if previous {
			text = "Скачать предыдущую версию"
		}
		button(box, text, func() {
			err := actions.DownloadUpdate(previous)
			if err != nil {
				err = errors.New(diagnostics.Message(err))
			}
			report(err)
		})
	}
	button(box, "Открыть папку пакетов", func() { report(actions.UpdateFolder()) })
	button(box, "Диагностика", func() {
		panel := gtk.WindowNew(0)
		gtk.WindowTitle(panel, "Диагностика yaneCompanion")
		gtk.WindowSize(panel, 480, 300)
		text := gtk.LabelNew(actions.Diagnostics())
		gtk.LabelSelect(text, 1)
		gtk.Border(panel, 20)
		gtk.Add(panel, text)
		gtk.ShowAll(panel)
	})
	label("Перед удалением: выберите данные для очистки. Без флажков сохраняются все данные.")
	clearCache := gtk.CheckNew("Удалить кэш просмотра и пакеты обновления")
	clearPairings := gtk.CheckNew("Удалить сохранённые подключения сайтов")
	pack(box, clearCache)
	pack(box, clearPairings)
	button(box, "Подготовить удаление и выйти", func() {
		err := actions.PrepareRemoval(gtk.CheckGet(clearCache) != 0, gtk.CheckGet(clearPairings) != 0)
		if err != nil {
			report(errors.New(diagnostics.Message(err)))
		}
	})
	label("Затем удалите программу через настройки ОС или пакетный менеджер.")
	serviceFeedback = label("")
	previousRefresh := refresh
	refresh = func() { previousRefresh(); gtk.LabelSet(updateLabel, snapshot.Update) }
	gtk.ShowAll(window)
	gtk.Focus(entry)
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
			if hidden && hasTray() && len(snapshot.Requests) == 0 {
				gtk.Hide(window)
			}
		}
		select {
		case <-show:
			gtk.WindowPresent(window)
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
