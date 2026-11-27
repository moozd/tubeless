//go:build cef

// Package cef runs offscreen Chromium browsers inside the tubeless
// window through the Chromium Embedded Framework. Pages render into a
// pixel buffer that the window uploads as a texture; input goes to the
// page directly. Build with the `cef` tag and CEF_ROOT set (see the
// Makefile); without the tag, every call reports ErrUnavailable.
//
// Every function must run on the window's main thread, the one that
// owns the GLFW event loop: CEF runs its UI thread there through an
// external message pump (see Pump).
package cef

/*
#cgo CFLAGS: -DCEF_API_VERSION=15800
#cgo LDFLAGS: -lcef
#include <stdlib.h>
#include "shim.h"
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// Available reports whether this build can host browsers at all.
func Available() bool { return true }

// Init starts CEF. It must run once, before any NewBrowser.
func Init(o Options) error {
	strs := []*C.char{
		C.CString(o.SubprocessPath), C.CString(o.ResourcesDir),
		C.CString(o.LocalesDir), C.CString(o.CacheDir),
		C.CString(o.LogFile),
	}
	defer freeStrings(strs)
	opts := C.tb_options{
		subprocess_path: strs[0],
		resources_dir:   strs[1],
		locales_dir:     strs[2],
		cache_dir:       strs[3],
		log_file:        strs[4],
		background:      C.uint32_t(o.Background),
	}
	if C.tb_cef_init(&opts) == 0 {
		return fmt.Errorf("cef: initialize failed (see %s)", o.LogFile)
	}
	return nil
}

func freeStrings(strs []*C.char) {
	for _, s := range strs {
		C.free(unsafe.Pointer(s))
	}
}

// Shutdown stops CEF. Close every browser and pump until they report
// closed first.
func Shutdown() { C.tb_cef_shutdown() }

// pumpAt is when CEF next asked for message-loop work, as Unix nanos;
// zero means it did not ask. CEF sets it from any thread.
var pumpAt atomic.Int64

var wake atomic.Pointer[func()]

// SetWake installs fn, run (on any thread) when CEF schedules work, so
// the event loop can stop waiting early. glfw.PostEmptyEvent fits.
func SetWake(fn func()) { wake.Store(&fn) }

//export goSchedulePump
func goSchedulePump(delayMs C.int64_t) {
	at := time.Now().Add(time.Duration(delayMs) * time.Millisecond)
	pumpAt.Store(at.UnixNano())
	if fn := wake.Load(); fn != nil {
		(*fn)()
	}
}

// NextPump returns how long until CEF wants work, and false if it
// asked for none.
func NextPump(now time.Time) (time.Duration, bool) {
	at := pumpAt.Load()
	if at == 0 {
		return 0, false
	}
	return max(0, time.Unix(0, at).Sub(now)), true
}

// Pump runs one slice of CEF's message loop. Run it when NextPump says
// work is due, and on a steady tick while any browser is open.
func Pump() {
	pumpAt.Store(0)
	C.tb_cef_do_work()
}

// Browser is one offscreen page.
type Browser struct {
	c      *C.tb_browser
	handle cgo.Handle
	id     atomic.Int32

	mu     sync.Mutex
	frame  Frame
	fresh  bool
	closed bool
}

var schemeHandler atomic.Pointer[func(SchemeRequest) SchemeResponse]

// SetSchemeHandler installs fn to answer every tubeless:// request. CEF
// calls it on its IO thread, so fn must be safe for concurrent use.
func SetSchemeHandler(fn func(SchemeRequest) SchemeResponse) {
	schemeHandler.Store(&fn)
}

//export goServeScheme
func goServeScheme(browserID C.int, url, method *C.char, body *C.uint8_t, bodyLen C.size_t, out *C.tb_response) {
	req := SchemeRequest{
		BrowserID: int(browserID),
		URL:       C.GoString(url),
		Method:    C.GoString(method),
		Body:      C.GoBytes(unsafe.Pointer(body), C.int(bodyLen)),
	}
	resp := SchemeResponse{Status: 404, MIME: "text/plain", Body: []byte("no handler")}
	if fn := schemeHandler.Load(); fn != nil {
		resp = (*fn)(req)
	}
	out.status = C.int(resp.Status)
	out.mime = C.CString(resp.MIME)
	out.len = C.size_t(len(resp.Body))
	out.data = (*C.uint8_t)(C.CBytes(resp.Body))
}

// ID is the page's CEF browser identifier, or 0 until CEF created it.
func (b *Browser) ID() int { return int(b.id.Load()) }

// ExecJS runs code in the page's main frame.
func (b *Browser) ExecJS(code string) {
	ccode := C.CString(code)
	defer C.free(unsafe.Pointer(ccode))
	C.tb_browser_exec_js(b.c, ccode)
}

// NewBrowser opens url in a w x h pixel view at the given device scale.
// fps caps how often the page paints.
func NewBrowser(url string, w, h int, scale float32, background uint32, fps int) (*Browser, error) {
	b := &Browser{}
	b.handle = cgo.NewHandle(b)
	curl := C.CString(url)
	defer C.free(unsafe.Pointer(curl))
	b.c = C.tb_browser_create(C.uintptr_t(b.handle), curl, C.int(w), C.int(h), C.float(scale), C.uint32_t(background), C.int(fps))
	if b.c == nil {
		b.handle.Delete()
		return nil, fmt.Errorf("cef: create browser for %s", url)
	}
	return b, nil
}

//export goOnCreated
func goOnCreated(h C.uintptr_t, browserID C.int) {
	cgo.Handle(h).Value().(*Browser).id.Store(int32(browserID))
}

// consoleHandler receives every page's console messages.
var consoleHandler atomic.Pointer[func(b *Browser, level int, msg, source string, line int)]

// SetConsoleHandler installs fn to receive console.log and friends from
// every page, and uncaught errors. It runs on the main thread.
func SetConsoleHandler(fn func(b *Browser, level int, msg, source string, line int)) {
	consoleHandler.Store(&fn)
}

//export goOnConsole
func goOnConsole(h C.uintptr_t, level C.int, msg, source *C.char, line C.int) {
	fn := consoleHandler.Load()
	if fn == nil {
		return
	}
	b := cgo.Handle(h).Value().(*Browser)
	(*fn)(b, int(level), C.GoString(msg), C.GoString(source), int(line))
}

//export goOnClosed
func goOnClosed(h C.uintptr_t) {
	b := cgo.Handle(h).Value().(*Browser)
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
}

//export goOnPaint
func goOnPaint(h C.uintptr_t, rects *C.int, n C.int, buf unsafe.Pointer, w, ht C.int) {
	b := cgo.Handle(h).Value().(*Browser)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.frame.store(unsafe.Slice((*int32)(unsafe.Pointer(rects)), int(n)*4), unsafe.Slice((*byte)(buf), int(w)*int(ht)*4), int(w), int(ht))
	b.fresh = true
}

// TakeFrame hands the newest painted frame to fn, if one arrived since
// the last call. With all set, it hands over any frame there is and
// marks all of it dirty (a new texture must get every pixel). fn must
// not keep the frame's slices.
func (b *Browser) TakeFrame(all bool, fn func(f *Frame)) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.frame.W == 0 || (!b.fresh && !all) {
		return false
	}
	if all {
		b.frame.Dirty = append(b.frame.Dirty[:0], Rect{0, 0, b.frame.W, b.frame.H})
	}
	fn(&b.frame)
	b.frame.Dirty = b.frame.Dirty[:0]
	b.fresh = false
	return true
}

// Closed reports whether CEF finished closing the page.
func (b *Browser) Closed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// Resize sets the view's pixel size and device scale.
func (b *Browser) Resize(w, h int, scale float32) {
	C.tb_browser_resize(b.c, C.int(w), C.int(h), C.float(scale))
}

// Close asks CEF to close the page; Closed turns true once it did.
func (b *Browser) Close() { C.tb_browser_close(b.c) }

// Release frees the browser after Closed reports true.
func (b *Browser) Release() {
	C.tb_browser_free(b.c)
	b.handle.Delete()
}

// Focus gives or takes keyboard focus.
func (b *Browser) Focus(focused bool) {
	C.tb_browser_focus(b.c, cbool(focused))
}

// MouseMove moves the pointer to (x, y) in view DIPs.
func (b *Browser) MouseMove(x, y int, mods uint32, leave bool) {
	C.tb_browser_mouse_move(b.c, C.int(x), C.int(y), C.uint32_t(mods), cbool(leave))
}

// MouseClick presses or releases button at (x, y) in view DIPs.
func (b *Browser) MouseClick(x, y int, mods uint32, button MouseButton, up bool, count int) {
	C.tb_browser_mouse_click(b.c, C.int(x), C.int(y), C.uint32_t(mods), C.int(button), cbool(up), C.int(count))
}

// MouseWheel scrolls by (dx, dy) pixels at (x, y) in view DIPs.
func (b *Browser) MouseWheel(x, y int, mods uint32, dx, dy int) {
	C.tb_browser_mouse_wheel(b.c, C.int(x), C.int(y), C.uint32_t(mods), C.int(dx), C.int(dy))
}

// Key sends one keyboard event.
func (b *Browser) Key(k KeyEvent) {
	ck := C.tb_key{
		_type:                C.int(k.Type),
		modifiers:            C.uint32_t(k.Modifiers),
		windows_key_code:     C.int(k.WindowsKeyCode),
		native_key_code:      C.int(k.NativeKeyCode),
		character:            C.uint16_t(k.Character),
		unmodified_character: C.uint16_t(k.UnmodifiedCharacter),
	}
	C.tb_browser_key(b.c, &ck)
}

func cbool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}
