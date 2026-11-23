package desktop

import (
	"syscall"
	"unsafe"
)

const (
	captionMinimize = 140
	captionClose    = 141
)

func roundWindow(window uintptr) {
	region, _, _ := gdi.NewProc("CreateRoundRectRgn").Call(0, 0, windowWidth+1, windowHeight+1, windowCurve, windowCurve)
	if region != 0 && win("SetWindowRgn", window, region, 0) == 0 {
		// Windows owns the region only after SetWindowRgn succeeds.
		gdi.NewProc("DeleteObject").Call(region)
	}
}

type windowCaption struct {
	buttons [2]uintptr
	hover   uintptr
}

func (c *windowCaption) hitTest(window, position uintptr) uintptr {
	p := point{X: int32(int16(position)), Y: int32(int16(position >> 16))}
	screen := p
	win("ScreenToClient", window, uintptr(unsafe.Pointer(&p)))
	if p.Y < 0 || p.Y >= headerHeight {
		return 1 // HTCLIENT
	}
	for _, button := range c.buttons {
		var bounds rect
		if win("GetWindowRect", button, uintptr(unsafe.Pointer(&bounds))) != 0 &&
			screen.X >= bounds.left && screen.X < bounds.right && screen.Y >= bounds.top && screen.Y < bounds.bottom {
			return 1 // let the native button handle mouse input
		}
	}
	return 2 // HTCAPTION: native move and system menu
}

func (c *windowCaption) attach(minimize, closeButton uintptr) {
	c.buttons = [2]uintptr{minimize, closeButton}
	defaultProc := commonControls.NewProc("DefSubclassProc")
	proc := syscall.NewCallback(func(button uintptr, id uint32, w, l, _, _ uintptr) uintptr {
		switch id {
		case 0x200: // WM_MOUSEMOVE
			if c.hover != button {
				previous := c.hover
				c.hover = button
				if previous != 0 {
					win("InvalidateRect", previous, 0, 0)
				}
				tracking := struct {
					size, flags uint32
					window      uintptr
					time        uint32
				}{flags: 2, window: button} // TME_LEAVE
				tracking.size = uint32(unsafe.Sizeof(tracking))
				win("TrackMouseEvent", uintptr(unsafe.Pointer(&tracking)))
				win("InvalidateRect", button, 0, 0)
			}
		case 0x2a3: // WM_MOUSELEAVE
			if c.hover == button {
				c.hover = 0
				win("InvalidateRect", button, 0, 0)
			}
		}
		result, _, _ := defaultProc.Call(button, uintptr(id), w, l)
		return result
	})
	for _, button := range c.buttons {
		commonControls.NewProc("SetWindowSubclass").Call(button, proc, 1, 0)
	}
}

func (t windowTheme) captionButton(item *drawItem, hover bool) {
	box := item.bounds
	win("FillRect", item.dc, uintptr(unsafe.Pointer(&box)), t.backgroundBrush)
	p := t.colors
	fill, stroke, inkColor := p.surface.win(), p.border.win(), p.muted.win()
	if hover || item.state&1 != 0 {
		fill, stroke, inkColor = p.elevated.win(), p.border.win(), p.ink.win()
		if item.id == captionClose {
			fill, stroke, inkColor = p.closeFill.win(), p.closeBorder.win(), p.closeText.win()
		}
		if item.state&1 != 0 {
			fill = stroke
		}
	}
	if item.state&0x10 != 0 {
		stroke = p.accent.win()
	}
	box = rect{box.left + 1, box.top + 1, box.right - 1, box.bottom - 1}
	roundedRect(item.dc, box, 14, fill, stroke)
	x, y := (box.left+box.right)/2, (box.top+box.bottom)/2
	pen, _, _ := gdi.NewProc("CreatePen").Call(0, 1, inkColor)
	previous, _, _ := gdi.NewProc("SelectObject").Call(item.dc, pen)
	defer gdi.NewProc("DeleteObject").Call(pen)
	defer gdi.NewProc("SelectObject").Call(item.dc, previous)
	if item.id == captionMinimize {
		gdi.NewProc("MoveToEx").Call(item.dc, uintptr(x-5), uintptr(y+3), 0)
		gdi.NewProc("LineTo").Call(item.dc, uintptr(x+6), uintptr(y+3))
		return
	}
	gdi.NewProc("MoveToEx").Call(item.dc, uintptr(x-4), uintptr(y-4), 0)
	gdi.NewProc("LineTo").Call(item.dc, uintptr(x+5), uintptr(y+5))
	gdi.NewProc("MoveToEx").Call(item.dc, uintptr(x+4), uintptr(y-4), 0)
	gdi.NewProc("LineTo").Call(item.dc, uintptr(x-5), uintptr(y+5))
}
