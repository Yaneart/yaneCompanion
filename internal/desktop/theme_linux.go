package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/ebitengine/purego"
)

const themeCSS = `
#companion-window { background: transparent; }
#companion-window-opaque { background: @yane_bg; }
#window-frame {
	background: @yane_bg;
	border-radius: 12px;
	box-shadow: inset 0 0 0 1px @yane_border;
}
#window-frame * { font-family: Manrope, sans-serif; font-size: 15px; font-weight: 500; color: @yane_ink; }
#window-frame #hero-title, #window-frame #title { font-size: 25px; font-weight: 700; }
#window-frame #heading { font-size: 21px; font-weight: 700; }
#window-frame #hero-caption, #window-frame #caption, #window-frame #site-caption, #window-frame #footer { font-size: 13px; }
#window-frame #hero-caption, #window-frame #secondary, #window-frame #site-caption, #window-frame #footer { color: @yane_muted; }
#window-frame #site-address { color: @yane_accent; }
#window-frame #nav, #window-frame #panel {
	background: @yane_surface;
	box-shadow: inset 0 0 0 1px @yane_border;
	border-radius: 20px;
}
#window-frame #nav { border-radius: 13px; }
#window-frame button, #window-frame entry, #window-frame #grant-field, #window-frame #diagnostic-report {
	background: @yane_elevated;
	border: 1px solid @yane_border;
	border-radius: 9px;
	box-shadow: none;
	text-shadow: none;
	padding: 0 8px;
	min-width: 0;
	min-height: 0;
	transition: none;
}
#window-frame #diagnostic-report { border-radius: 12px; padding: 7px 9px; }
#window-frame button { padding: 0 4px; }
#window-frame #pairing-origin { background: @yane_surface; }
#window-frame textview, #window-frame textview text, #window-frame list {
	background: transparent;
}
#window-frame #pairing-origin text { padding: 0; }
#window-frame scrollbar { background: @yane_surface; min-width: 12px; }
#window-frame scrollbar slider { background: @yane_border; border: 0; min-width: 8px; }
#window-frame #grant-field row { border-radius: 4px; padding: 0; }
#window-frame #grant-field row:first-child { margin-top: 4px; }
#window-frame #grant-field row:selected { background: @yane_selected; }
#window-frame #grant-field row:selected label { color: @yane_selected_text; }
#window-frame #grant-field:disabled label, #window-frame button:disabled label { color: @yane_muted; }
#window-frame button:disabled { background: @yane_surface; }
#window-frame #primary { background: @yane_action; border-color: @yane_action; }
#window-frame #primary label { color: @yane_action_text; }
#window-frame #nav-tab { background: @yane_surface; border-color: @yane_surface; }
#window-frame #nav-tab label, #window-frame #minimize label, #window-frame #close label { color: @yane_muted; }
#window-frame #nav-active, #window-frame #theme-choice:checked {
	background: @yane_selected; border-color: @yane_selected_border;
}
#window-frame #nav-active label, #window-frame #theme-choice:checked label { color: @yane_selected_text; }
#window-frame button:active { background: @yane_pressed; }
#window-frame #minimize, #window-frame #close { background: @yane_surface; border-radius: 7px; }
#window-frame #minimize label, #window-frame #close label { font-size: 17px; }
#window-frame #minimize:hover { background: @yane_elevated; }
#window-frame #close:hover { background: @yane_close_fill; border-color: @yane_close_border; }
#window-frame #close:hover label { color: @yane_close_ink; }
#window-frame button:focus, #window-frame entry:focus { border-color: @yane_accent; outline-style: none; }
`

func stylesFor(dark bool) string {
	p := desktopPalette(dark)
	return fmt.Sprintf(`
@define-color yane_bg #%06x;
@define-color yane_surface #%06x;
@define-color yane_elevated #%06x;
@define-color yane_border #%06x;
@define-color yane_ink #%06x;
@define-color yane_muted #%06x;
@define-color yane_accent #%06x;
@define-color yane_pressed #%06x;
@define-color yane_action #%06x;
@define-color yane_action_text #%06x;
@define-color yane_selected #%06x;
@define-color yane_selected_border #%06x;
@define-color yane_selected_text #%06x;
@define-color yane_close_fill #%06x;
@define-color yane_close_border #%06x;
@define-color yane_close_ink #%06x;
`, p.background, p.surface, p.elevated, p.border, p.ink, p.muted, p.accent, p.pressed,
		p.action, p.actionText, p.selected, p.selectedBorder, p.selectedText, p.closeFill, p.closeBorder, p.closeText) + themeCSS
}

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

func installTheme(lib uintptr, dark bool) (apply func(bool), close func(), err error) {
	var theme struct {
		NewProvider func() uintptr                            `symbol:"gtk_css_provider_new"`
		Load        func(uintptr, string, int, uintptr) int32 `symbol:"gtk_css_provider_load_from_data"`
		Screen      func() uintptr                            `symbol:"gdk_screen_get_default"`
		Apply       func(uintptr, uintptr, uint32)            `symbol:"gtk_style_context_add_provider_for_screen"`
		Remove      func(uintptr, uintptr)                    `symbol:"gtk_style_context_remove_provider_for_screen"`
		Unref       func(uintptr)                             `symbol:"g_object_unref"`
	}
	loadFunctions(lib, &theme)
	provider := theme.NewProvider()
	if theme.Load(provider, stylesFor(dark), -1, 0) == 0 {
		theme.Unref(provider)
		return nil, nil, errors.New("DESKTOP_THEME_UNAVAILABLE")
	}
	screen := theme.Screen()
	theme.Apply(screen, provider, 600)
	return func(dark bool) { theme.Load(provider, stylesFor(dark), -1, 0) }, func() {
		theme.Remove(screen, provider)
		theme.Unref(provider)
	}, nil
}

func bannerWidget(lib uintptr, art []byte) uintptr {
	var media struct {
		New    func() uintptr                                        `symbol:"gdk_pixbuf_loader_new"`
		Size   func(uintptr, int32, int32)                           `symbol:"gdk_pixbuf_loader_set_size"`
		Write  func(uintptr, unsafe.Pointer, uintptr, uintptr) int32 `symbol:"gdk_pixbuf_loader_write"`
		Close  func(uintptr, uintptr) int32                          `symbol:"gdk_pixbuf_loader_close"`
		Pixbuf func(uintptr) uintptr                                 `symbol:"gdk_pixbuf_loader_get_pixbuf"`
		Crop   func(uintptr, int32, int32, int32, int32) uintptr     `symbol:"gdk_pixbuf_new_subpixbuf"`
		Image  func(uintptr) uintptr                                 `symbol:"gtk_image_new_from_pixbuf"`
		Unref  func(uintptr)                                         `symbol:"g_object_unref"`
	}
	loadFunctions(lib, &media)
	loader := media.New()
	defer media.Unref(loader)
	media.Size(loader, 512, 84)
	written := media.Write(loader, unsafe.Pointer(&art[0]), uintptr(len(art)), 0)
	if media.Close(loader, 0) == 0 || written == 0 {
		return 0
	}
	// The left half contains the icon tile. The Windows header uses just the mark.
	mark := media.Crop(media.Pixbuf(loader), 256, 0, 256, 84)
	defer media.Unref(mark)
	return media.Image(mark)
}
