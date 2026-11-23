package desktop

import (
	"bytes"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var gdi = windows.NewLazySystemDLL("gdi32.dll")
var gdiPlus = windows.NewLazySystemDLL("gdiplus.dll")

func rgb(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

func (c themeColor) win() uintptr { return rgb(byte(c>>16), byte(c>>8), byte(c)) }

func systemDark() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	light, _, err := key.GetIntegerValue("AppsUseLightTheme")
	return err == nil && light == 0
}

func applyWindowMode(window uintptr, dark bool) {
	var enabled int32
	if dark {
		enabled = 1
	}
	dwm := windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	if result, _, _ := dwm.Call(window, 20, uintptr(unsafe.Pointer(&enabled)), 4); result != 0 {
		dwm.Call(window, 19, uintptr(unsafe.Pointer(&enabled)), 4)
	}
}

type rect struct{ left, top, right, bottom int32 }

type paintStruct struct {
	dc     uintptr
	erase  int32
	bounds rect
	flags  [40]byte
}

type drawItem struct {
	controlType, id, item, action, state uint32
	window, dc                           uintptr
	bounds                               rect
	data                                 uintptr
}

type bitmapHeader struct {
	size          uint32
	width, height int32
	planes, bits  uint16
	rest          [24]byte
}

type windowTheme struct {
	colors                                    palette
	dark                                      bool
	backgroundBrush, surfaceBrush, fieldBrush uintptr
	body, heading, title, caption             uintptr
	fontPath                                  string
	gdiToken                                  uintptr
	bannerHeader                              bitmapHeader
	bannerPixels                              []uint32
}

func nativeFont(family string, height, weight int) uintptr {
	name := wide(family)
	handle, _, _ := gdi.NewProc("CreateFontW").Call(uintptr(-height), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(name)))
	return handle
}

func solidBrush(color uintptr) uintptr {
	result, _, _ := gdi.NewProc("CreateSolidBrush").Call(color)
	return result
}

func newWindowTheme(dark bool) windowTheme {
	var t windowTheme
	t.setColors(dark)
	family := "Segoe UI"
	if exe, err := os.Executable(); err == nil {
		path := filepath.Join(filepath.Dir(exe), "Manrope.ttf")
		if added, _, _ := gdi.NewProc("AddFontResourceExW").Call(uintptr(unsafe.Pointer(wide(path))), 0x10, 0); added != 0 {
			family, t.fontPath = "Manrope", path
		}
	}
	t.body, t.heading = nativeFont(family, 15, 500), nativeFont(family, 21, 700)
	t.title, t.caption = nativeFont(family, 25, 700), nativeFont(family, 13, 500)
	var startup struct {
		version  uint32
		callback uintptr
		flags    [2]uint32
	}
	startup.version = 1
	gdiPlus.NewProc("GdiplusStartup").Call(uintptr(unsafe.Pointer(&t.gdiToken)), uintptr(unsafe.Pointer(&startup)), 0)
	return t
}

func (t *windowTheme) setColors(dark bool) {
	t.closeBrushes()
	t.dark = dark
	t.colors = desktopPalette(dark)
	t.backgroundBrush = solidBrush(t.colors.background.win())
	t.surfaceBrush = solidBrush(t.colors.surface.win())
	t.fieldBrush = solidBrush(t.colors.elevated.win())
	image := lightBanner
	if dark {
		image = banner
	}
	t.bannerPixels = nil
	if img, err := png.Decode(bytes.NewReader(image)); err == nil {
		b := img.Bounds()
		// The shared banner's left half contains the icon tile used by GTK.
		b.Min.X += b.Dx() / 2
		t.bannerHeader = bitmapHeader{size: 40, width: int32(b.Dx()), height: -int32(b.Dy()), planes: 1, bits: 32}
		t.bannerPixels = make([]uint32, 0, b.Dx()*b.Dy())
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				p := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				blend := func(channel, base byte) uint32 {
					a := uint32(p.A)
					return (uint32(channel)*a + uint32(base)*(255-a)) / 255
				}
				base := t.colors.background
				t.bannerPixels = append(t.bannerPixels, blend(p.R, byte(base>>16))<<16|blend(p.G, byte(base>>8))<<8|blend(p.B, byte(base)))
			}
		}
	}
}

func (t windowTheme) closeBrushes() {
	for _, handle := range []uintptr{t.backgroundBrush, t.surfaceBrush, t.fieldBrush} {
		if handle != 0 {
			gdi.NewProc("DeleteObject").Call(handle)
		}
	}
}

func (t windowTheme) close() {
	t.closeBrushes()
	for _, handle := range []uintptr{t.body, t.heading, t.title, t.caption} {
		gdi.NewProc("DeleteObject").Call(handle)
	}
	if t.fontPath != "" {
		gdi.NewProc("RemoveFontResourceExW").Call(uintptr(unsafe.Pointer(wide(t.fontPath))), 0x10, 0)
	}
	if t.gdiToken != 0 {
		gdiPlus.NewProc("GdiplusShutdown").Call(t.gdiToken)
	}
}

func (t windowTheme) paint(window uintptr, page int) {
	var state paintStruct
	dc := win("BeginPaint", window, uintptr(unsafe.Pointer(&state)))
	defer win("EndPaint", window, uintptr(unsafe.Pointer(&state)))
	var client rect
	win("GetClientRect", window, uintptr(unsafe.Pointer(&client)))
	buffer, _, _ := gdi.NewProc("CreateCompatibleDC").Call(dc)
	if buffer != 0 {
		defer gdi.NewProc("DeleteDC").Call(buffer)
		bitmap, _, _ := gdi.NewProc("CreateCompatibleBitmap").Call(dc, uintptr(client.right), uintptr(client.bottom))
		if bitmap != 0 {
			defer gdi.NewProc("DeleteObject").Call(bitmap)
			old, _, _ := gdi.NewProc("SelectObject").Call(buffer, bitmap)
			if old != 0 {
				defer gdi.NewProc("SelectObject").Call(buffer, old)
				t.drawPage(buffer, client, page)
				gdi.NewProc("BitBlt").Call(dc, 0, 0, uintptr(client.right), uintptr(client.bottom), buffer, 0, 0, 0x00cc0020)
				return
			}
		}
	}
	t.drawPage(dc, client, page)
}

func (t windowTheme) drawPage(dc uintptr, client rect, page int) {
	p := t.colors
	win("FillRect", dc, uintptr(unsafe.Pointer(&client)), t.backgroundBrush)
	roundedRect(dc, client, windowCurve-2, p.background.win(), p.border.win())
	if len(t.bannerPixels) > 0 {
		gdi.NewProc("SetStretchBltMode").Call(dc, 4)
		gdi.NewProc("StretchDIBits").Call(dc, 280, 12, 256, 84, 0, 0,
			uintptr(t.bannerHeader.width), uintptr(-t.bannerHeader.height), uintptr(unsafe.Pointer(&t.bannerPixels[0])),
			uintptr(unsafe.Pointer(&t.bannerHeader)), 0, 0x00cc0020)
		runtime.KeepAlive(t.bannerPixels)
	}
	drawText(dc, t.title, p.ink.win(), rect{24, 25, 365, 61}, windowTitle, 0x20)
	drawText(dc, t.caption, p.muted.win(), rect{25, 58, 365, 80}, windowSubtitle, 0x20)
	roundedRect(dc, rect{navX, headerHeight, navX + navWidth, headerHeight + navHeight}, 26, p.surface.win(), p.border.win())
	roundedRect(dc, rect{panelX, panelY, panelX + panelWidth, panelY + panelHeight}, 40, p.surface.win(), p.border.win())
	if page == 1 {
		roundedRect(dc, rect{46, 196, 391, 232}, 18, p.elevated.win(), p.border.win())
		roundedRect(dc, rect{46, 349, 377, 402}, 18, p.elevated.win(), p.border.win())
	}
	if page == 3 {
		roundedRect(dc, rect{46, 230, 512, 390}, 24, p.elevated.win(), p.border.win())
	}
}

func roundedRect(dc uintptr, box rect, diameter, fill, stroke uintptr) {
	var graphics uintptr
	call := func(name string, args ...uintptr) uintptr {
		value, _, _ := gdiPlus.NewProc(name).Call(args...)
		return value
	}
	if call("GdipCreateFromHDC", dc, uintptr(unsafe.Pointer(&graphics))) != 0 {
		roundedRectGDI(dc, box, diameter, fill, stroke)
		return
	}
	defer call("GdipDeleteGraphics", graphics)
	call("GdipSetSmoothingMode", graphics, 4)
	fillShape := func(box rect, diameter, color uintptr) {
		var path, brush uintptr
		call("GdipCreatePath", 0, uintptr(unsafe.Pointer(&path)))
		defer call("GdipDeletePath", path)
		d := int32(diameter)
		for _, corner := range []struct{ x, y, angle int32 }{
			{box.left, box.top, 180}, {box.right - d - 1, box.top, 270},
			{box.right - d - 1, box.bottom - d - 1, 0}, {box.left, box.bottom - d - 1, 90},
		} {
			call("GdipAddPathArcI", path, uintptr(corner.x), uintptr(corner.y), diameter, diameter,
				uintptr(math.Float32bits(float32(corner.angle))), uintptr(math.Float32bits(90)))
		}
		call("GdipClosePathFigure", path)
		argb := 0xff000000 | (color&0xff)<<16 | color&0xff00 | (color>>16)&0xff
		call("GdipCreateSolidFill", argb, uintptr(unsafe.Pointer(&brush)))
		defer call("GdipDeleteBrush", brush)
		call("GdipFillPath", graphics, brush, path)
	}
	if stroke != fill {
		fillShape(box, diameter, stroke)
		box = rect{box.left + 1, box.top + 1, box.right - 1, box.bottom - 1}
		diameter -= 2
	}
	fillShape(box, diameter, fill)
}

func roundedRectGDI(dc uintptr, box rect, radius, fill, stroke uintptr) {
	brush := solidBrush(fill)
	pen, _, _ := gdi.NewProc("CreatePen").Call(0, 1, stroke)
	oldBrush, _, _ := gdi.NewProc("SelectObject").Call(dc, brush)
	oldPen, _, _ := gdi.NewProc("SelectObject").Call(dc, pen)
	gdi.NewProc("RoundRect").Call(dc, uintptr(box.left), uintptr(box.top), uintptr(box.right), uintptr(box.bottom), radius, radius)
	gdi.NewProc("SelectObject").Call(dc, oldPen)
	gdi.NewProc("SelectObject").Call(dc, oldBrush)
	gdi.NewProc("DeleteObject").Call(pen)
	gdi.NewProc("DeleteObject").Call(brush)
}

func drawText(dc, font, color uintptr, box rect, text string, flags int) {
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, font)
	gdi.NewProc("SetTextColor").Call(dc, color)
	gdi.NewProc("SetBkMode").Call(dc, 1)
	win("DrawTextW", dc, wide(text), -1, uintptr(unsafe.Pointer(&box)), flags|0x800)
	gdi.NewProc("SelectObject").Call(dc, old)
}

func (t windowTheme) buttonBackground(id uintptr) uintptr {
	switch id {
	case captionMinimize, captionClose, 133, 306:
		return t.backgroundBrush
	default:
		return t.surfaceBrush
	}
}

func (t windowTheme) button(item *drawItem, selected bool) {
	if item.controlType != 4 {
		return
	}
	p := t.colors
	fill, stroke, text := p.elevated, p.border, p.ink
	if item.id == 105 || item.id == 102 || item.id == 134 {
		fill, stroke, text = p.action, p.action, p.actionText
	}
	if selected {
		fill, stroke, text = p.selected, p.selectedBorder, p.selectedText
	}
	if item.state&0x4 != 0 {
		fill, stroke, text = p.surface, p.border, p.muted
	} else if item.state&0x1 != 0 {
		fill, text = p.pressed, p.ink
	}
	if item.state&0x10 != 0 {
		stroke = p.accent
	}
	box := item.bounds
	win("FillRect", item.dc, uintptr(unsafe.Pointer(&box)), t.buttonBackground(uintptr(item.id)))
	tab := item.id == 130 || item.id == 132 || item.id == 135
	if tab && !selected && item.state&0x1 == 0 {
		fill, stroke, text = p.surface, p.surface, p.muted
		if item.state&0x10 != 0 {
			stroke = p.accent
		}
	}
	roundedRect(item.dc, box, 18, fill.win(), stroke.win())
	buf := [160]uint16{}
	win("GetWindowTextW", item.window, &buf[0], len(buf))
	drawText(item.dc, t.body, text.win(), box, windows.UTF16ToString(buf[:]), 0x1|0x4|0x20)
}
