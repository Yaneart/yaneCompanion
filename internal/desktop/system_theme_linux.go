package desktop

import (
	"context"
	"time"
)

func gtkSystemTheme(lib, window uintptr) func() bool {
	var gtk struct {
		Settings func() uintptr                           `symbol:"gtk_settings_get_default"`
		Get      func(uintptr, string, *int32, uintptr)   `symbol:"g_object_get"`
		Style    func(uintptr) uintptr                    `symbol:"gtk_widget_get_style_context"`
		Color    func(uintptr, string, *[4]float64) int32 `symbol:"gtk_style_context_lookup_color"`
	}
	loadFunctions(lib, &gtk)
	return func() bool {
		var preferDark int32
		gtk.Get(gtk.Settings(), "gtk-application-prefer-dark-theme", &preferDark, 0)
		if preferDark != 0 {
			return true
		}
		var bg [4]float64
		return gtk.Color(gtk.Style(window), "theme_bg_color", &bg) != 0 &&
			0.2126*bg[0]+0.7152*bg[1]+0.0722*bg[2] < 0.5
	}
}

// Use the desktop portal when it supplies a preference. The worker keeps
// D-Bus timeouts off the GTK thread, including on desktops without a portal.
func watchPortalTheme(ctx context.Context, lib uintptr, changes chan<- uint32) {
	var bus struct {
		Connect   func(int32, uintptr, uintptr) uintptr                                                                   `symbol:"g_bus_get_sync"`
		Call      func(uintptr, string, string, string, string, uintptr, uintptr, int32, int32, uintptr, uintptr) uintptr `symbol:"g_dbus_connection_call_sync"`
		Parse     func(uintptr, string, uintptr, uintptr, uintptr) uintptr                                                `symbol:"g_variant_parse"`
		NewType   func(string) uintptr                                                                                    `symbol:"g_variant_type_new"`
		FreeType  func(uintptr)                                                                                           `symbol:"g_variant_type_free"`
		RefSink   func(uintptr) uintptr                                                                                   `symbol:"g_variant_ref_sink"`
		Child     func(uintptr, uintptr) uintptr                                                                          `symbol:"g_variant_get_child_value"`
		Class     func(uintptr) int32                                                                                     `symbol:"g_variant_classify"`
		Unwrap    func(uintptr) uintptr                                                                                   `symbol:"g_variant_get_variant"`
		Uint      func(uintptr) uint32                                                                                    `symbol:"g_variant_get_uint32"`
		Unref     func(uintptr)                                                                                           `symbol:"g_variant_unref"`
		UnrefConn func(uintptr)                                                                                           `symbol:"g_object_unref"`
	}
	loadFunctions(lib, &bus)
	conn := bus.Connect(2, 0, 0) // G_BUS_TYPE_SESSION
	if conn == 0 {
		return
	}
	defer bus.UnrefConn(conn)
	parameters := bus.RefSink(bus.Parse(0, "('org.freedesktop.appearance', 'color-scheme')", 0, 0, 0))
	defer bus.Unref(parameters)
	replyType := bus.NewType("(v)")
	defer bus.FreeType(replyType)
	call := func(method string) uintptr {
		return bus.Call(conn, "org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop",
			"org.freedesktop.portal.Settings", method, parameters, replyType, 0, 400, 0, 0)
	}
	read := func() uint32 {
		reply := call("ReadOne")
		if reply == 0 {
			reply = call("Read") // Settings portal v1 wraps the value twice.
		}
		if reply == 0 {
			return 0
		}
		defer bus.Unref(reply)
		value := bus.Child(reply, 0)
		for bus.Class(value) == 'v' {
			next := bus.Unwrap(value)
			bus.Unref(value)
			value = next
		}
		defer bus.Unref(value)
		if bus.Class(value) == 'u' {
			return bus.Uint(value)
		}
		return 0
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		scheme := read()
		select {
		case changes <- scheme:
		case <-ctx.Done():
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}
