package desktop

import (
	"time"
	"unsafe"
)

const (
	pageFadeTimer    = 2
	pageFadeDuration = 160 * time.Millisecond
	pageFadeOpacity  = 184
)

// The cover uses the panel's own background brush. Windows composites it over
// the new controls while they are already available to mouse and keyboard input.
type pageFade struct {
	window  uintptr
	cover   uintptr
	started time.Time
}

func newPageFade(window, instance uintptr) pageFade {
	// Layered, click-through, no activation; hidden until a page changes.
	cover := win("CreateWindowExW", 0x08080020, wide("STATIC"), wide(""), 0x48000000,
		41, 159, 478, 239, window, 0, instance, 0)
	return pageFade{window: window, cover: cover}
}

func (f *pageFade) start() {
	if f.cover == 0 || win("IsWindowVisible", f.window) == 0 || win("IsIconic", f.window) != 0 {
		return
	}
	var enabled int32
	if win("SystemParametersInfoW", 0x1042, 0, uintptr(unsafe.Pointer(&enabled)), 0) == 0 || enabled == 0 {
		f.stop()
		return
	}
	if win("SetLayeredWindowAttributes", f.cover, 0, pageFadeOpacity, 2) == 0 {
		f.stop()
		return
	}
	f.started = time.Now()
	win("SetWindowPos", f.cover, 0, 0, 0, 0, 0, 0x53) // top, show, keep geometry and focus
	if win("SetTimer", f.window, pageFadeTimer, 15, 0) == 0 {
		f.stop()
	}
}

func (f *pageFade) tick() {
	if f.started.IsZero() {
		return
	}
	elapsed := time.Since(f.started)
	if elapsed >= pageFadeDuration {
		f.stop()
		return
	}
	remaining := 1 - float64(elapsed)/float64(pageFadeDuration)
	opacity := int(pageFadeOpacity * remaining * remaining)
	win("SetLayeredWindowAttributes", f.cover, 0, opacity, 2)
}

func (f *pageFade) stop() {
	if f.started.IsZero() {
		return
	}
	f.started = time.Time{}
	win("KillTimer", f.window, pageFadeTimer)
	win("ShowWindow", f.cover, 0)
}
