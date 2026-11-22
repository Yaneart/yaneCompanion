package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/ebitengine/purego"
)

const themeCSS = `
#companion-window, #companion-window window { background: #101112; color: #f1f0ec; }
#companion-window * { font-family: Manrope, sans-serif; font-size: 14px; }
#companion-window label { color: #f1f0ec; }
#companion-window #hero-title { font-size: 25px; font-weight: 700; }
#companion-window #hero-caption, #companion-window #secondary { color: #999b9d; }
#companion-window #heading { font-size: 21px; font-weight: 700; }
#companion-window #site-address { color: #9dbaf2; }
#companion-window #diagnostic-report {
	background-color: #222325; border: 1px solid #303235; border-radius: 12px;
	padding: 10px 12px;
}
#companion-window #panel {
	background: #191a1b; border: 1px solid #303235; border-radius: 22px;
}
#companion-window button, #companion-window entry {
	background-image: none; background-color: #222325; color: #f1f0ec;
	border: 1px solid #303235; border-radius: 12px; box-shadow: none;
	text-shadow: none; padding: 6px 15px; min-height: 23px;
}
#companion-window button:hover { background-color: #292b2d; }
#companion-window button:focus, #companion-window entry:focus { border-color: #6f94df; }
#companion-window button:disabled { background-color: #191a1b; color: #999b9d; }
#companion-window #primary {
	background-image: none; background-color: #b5cdfa; color: #101c32;
	border-color: #b5cdfa;
}
#companion-window #primary:hover { background-color: #cadcfc; }
#companion-window #primary label { color: #101c32; font-weight: 700; }
#companion-window button:disabled label { color: #999b9d; }
#companion-window #nav-active { background-color: #202c42; border-color: #34496c; }
#companion-window #nav-active label { color: #bdd3fb; }
`

func copyReport(lib uintptr, report string) bool {
	var api struct {
		Atom func(string, int32) uintptr  `symbol:"gdk_atom_intern"`
		Get  func(uintptr) uintptr        `symbol:"gtk_clipboard_get"`
		Set  func(uintptr, string, int32) `symbol:"gtk_clipboard_set_text"`
	}
	loadFunctions(lib, &api)
	clipboard := api.Get(api.Atom("CLIPBOARD", 0))
	if clipboard == 0 {
		return false
	}
	api.Set(clipboard, report, -1)
	return true
}

func loadDesktopFont() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	lib, err := purego.Dlopen("libfontconfig.so.1", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return
	}
	defer purego.Dlclose(lib)
	var addFont func(uintptr, string) int32
	purego.RegisterLibFunc(&addFont, lib, "FcConfigAppFontAddFile")
	addFont(0, filepath.Join(filepath.Dir(exe), "Manrope.ttf"))
}

func installTheme(lib uintptr) error {
	var theme struct {
		NewProvider func() uintptr                            `symbol:"gtk_css_provider_new"`
		Load        func(uintptr, string, int, uintptr) int32 `symbol:"gtk_css_provider_load_from_data"`
		Screen      func() uintptr                            `symbol:"gdk_screen_get_default"`
		Apply       func(uintptr, uintptr, uint32)            `symbol:"gtk_style_context_add_provider_for_screen"`
		Unref       func(uintptr)                             `symbol:"g_object_unref"`
	}
	loadFunctions(lib, &theme)
	provider := theme.NewProvider()
	defer theme.Unref(provider)
	if theme.Load(provider, themeCSS, -1, 0) == 0 {
		return errors.New("DESKTOP_THEME_UNAVAILABLE")
	}
	theme.Apply(theme.Screen(), provider, 600)
	return nil
}

func bannerWidget(lib uintptr) uintptr {
	var media struct {
		New    func() uintptr                                        `symbol:"gdk_pixbuf_loader_new"`
		Size   func(uintptr, int32, int32)                           `symbol:"gdk_pixbuf_loader_set_size"`
		Write  func(uintptr, unsafe.Pointer, uintptr, uintptr) int32 `symbol:"gdk_pixbuf_loader_write"`
		Close  func(uintptr, uintptr) int32                          `symbol:"gdk_pixbuf_loader_close"`
		Pixbuf func(uintptr) uintptr                                 `symbol:"gdk_pixbuf_loader_get_pixbuf"`
		Image  func(uintptr) uintptr                                 `symbol:"gtk_image_new_from_pixbuf"`
		Unref  func(uintptr)                                         `symbol:"g_object_unref"`
	}
	loadFunctions(lib, &media)
	loader := media.New()
	defer media.Unref(loader)
	media.Size(loader, 512, 84)
	written := media.Write(loader, unsafe.Pointer(&banner[0]), uintptr(len(banner)), 0)
	if media.Close(loader, 0) == 0 || written == 0 {
		return 0
	}
	return media.Image(media.Pixbuf(loader))
}
