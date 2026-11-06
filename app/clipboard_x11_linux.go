//go:build linux && !android

package app

import (
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/sushvibez/gfx/ffi"
)

// The X11 clipboard.
//
// X has no clipboard to put text in. The CLIPBOARD selection is owned by a
// client, and a client that wants to paste asks the owner to convert it:
// the owner writes the text into a property on the asker's window and
// tells it so. So copying is claiming ownership and then answering every
// request for as long as we hold it, and pasting is asking whoever holds
// it and waiting for the answer.
//
// That runs here on a connection of its own, on one goroutine locked to
// one OS thread, with a little window nobody sees: a request can arrive at
// any time, from any program, and must be answered whatever the windows
// the app shows are doing — including when it has none (a headless
// tool copying a result). ClipboardText and SetClipboardText talk to that
// goroutine. Without a display — headless, a test box — they keep the
// text in the process, as before.

// X11 selection event types and constants.
const (
	xSelectionClear   = 29
	xSelectionRequest = 30
	xSelectionNotify  = 31
	xPropModeReplace  = 0
	xAnyPropertyType  = 0
	xCurrentTime      = 0
	xNone             = 0
	xaAtom            = 4 // the predefined ATOM type
	xaString          = 31
)

var (
	xSetSelectionOwner func(d, selection, owner, time uintptr) int32
	xGetSelectionOwner func(d, selection uintptr) uintptr
	xConvertSelection  func(d, selection, target, property, requestor, time uintptr) int32
	xGetWindowProperty func(d, w, property uintptr, offset, length int64, del int32, reqType uintptr,
		actualType *uintptr, actualFormat *int32, nitems, bytesAfter *uint64, prop **byte) int32
	xDeleteProperty func(d, w, property uintptr) int32
	xFree           func(p unsafe.Pointer) int32
)

var loadClipboardX11 = sync.OnceValue(func() error {
	if err := loadX11(); err != nil {
		return err
	}
	lib, err := ffi.Open("libX11.so.6", "libX11.so")
	if err != nil {
		return err
	}
	lib.Bind(&xSetSelectionOwner, "XSetSelectionOwner")
	lib.Bind(&xGetSelectionOwner, "XGetSelectionOwner")
	lib.Bind(&xConvertSelection, "XConvertSelection")
	lib.Bind(&xGetWindowProperty, "XGetWindowProperty")
	lib.Bind(&xDeleteProperty, "XDeleteProperty")
	lib.Bind(&xFree, "XFree")
	return nil
})

// clip is the clipboard goroutine's mailbox.
type clipRequest struct {
	set   bool
	text  string
	reply chan string
}

var (
	clipOnce sync.Once
	clipCh   chan clipRequest // nil when there is no display
)

// startClipboard opens the clipboard's connection and its goroutine, once.
func startClipboard() {
	clipOnce.Do(func() {
		if loadClipboardX11() != nil {
			return
		}
		ready := make(chan bool)
		ch := make(chan clipRequest)
		go func() {
			runtime.LockOSThread()
			c := newClipConn()
			ready <- c != nil
			if c != nil {
				c.serve(ch)
			}
		}()
		if <-ready {
			clipCh = ch
		}
	})
}

// ClipboardText is the text on the system clipboard (the CLIPBOARD
// selection), whoever put it there.
func ClipboardText() string {
	startClipboard()
	if clipCh == nil {
		return clipboardFallback
	}
	r := make(chan string, 1)
	clipCh <- clipRequest{reply: r}
	return <-r
}

// SetClipboardText puts text on the system clipboard, where other
// programs can paste it from for as long as this one runs (or until
// something else is copied).
func SetClipboardText(s string) {
	clipboardFallback = s
	startClipboard()
	if clipCh == nil {
		return
	}
	r := make(chan string, 1)
	clipCh <- clipRequest{set: true, text: s, reply: r}
	<-r
}

// clipConn is the clipboard's own X connection.
type clipConn struct {
	dpy, win                                       uintptr
	clipboard, utf8, targets, text, property, incr uintptr
	owned                                          string
	owning                                         bool
}

func newClipConn() *clipConn {
	dpy := xOpenDisplay(0)
	if dpy == 0 {
		return nil
	}
	root := xDefaultRootWindow(dpy)
	win := xCreateSimpleWindow(dpy, root, 0, 0, 1, 1, 0, 0, 0)
	c := &clipConn{dpy: dpy, win: win,
		clipboard: xInternAtom(dpy, "CLIPBOARD", 0),
		utf8:      xInternAtom(dpy, "UTF8_STRING", 0),
		targets:   xInternAtom(dpy, "TARGETS", 0),
		text:      xInternAtom(dpy, "TEXT", 0),
		property:  xInternAtom(dpy, "GFX_CLIPBOARD", 0),
		incr:      xInternAtom(dpy, "INCR", 0),
	}
	xFlush(dpy)
	return c
}

// serve answers the app's requests and other programs', forever.
func (c *clipConn) serve(ch chan clipRequest) {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case req := <-ch:
			if req.set {
				c.owned, c.owning = req.text, true
				xSetSelectionOwner(c.dpy, c.clipboard, c.win, xCurrentTime)
				if xGetSelectionOwner(c.dpy, c.clipboard) != c.win {
					c.owning = false // the server would not give it to us
				}
				xFlush(c.dpy)
				req.reply <- ""
				continue
			}
			req.reply <- c.paste()
		case <-tick.C:
			c.events(nil)
		}
	}
}

// events handles what the server has sent: requests for our selection,
// losing it, and — when waiting for one — the answer to a paste.
func (c *clipConn) events(answer func(ev *xEvent) bool) bool {
	var ev xEvent
	for xPending(c.dpy) > 0 {
		xNextEvent(c.dpy, &ev)
		switch ev.kind() {
		case xSelectionRequest:
			c.answer(&ev)
		case xSelectionClear:
			c.owning = false // somebody else copied something
		case xSelectionNotify:
			if answer != nil && answer(&ev) {
				return true
			}
		}
	}
	return false
}

// selectionRequest is XSelectionRequestEvent's fields, at their 64-bit
// offsets: owner 32, requestor 40, selection 48, target 56, property 64,
// time 72.
func (e *xEvent) selectionRequest() (requestor, selection, target, property, tm uintptr) {
	return uintptr(e.data[5]), uintptr(e.data[6]), uintptr(e.data[7]), uintptr(e.data[8]), uintptr(e.data[9])
}

// answer gives a requestor our text, or the list of forms we have it in,
// and tells it so — or tells it no.
func (c *clipConn) answer(req *xEvent) {
	requestor, selection, target, property, tm := req.selectionRequest()
	if property == xNone {
		property = target // an obsolete client: use the target as the property
	}
	ok := c.owning && selection == c.clipboard
	if ok {
		switch target {
		case c.targets:
			atoms := []uint64{uint64(c.targets), uint64(c.utf8), uint64(c.text), xaString}
			xChangeProperty(c.dpy, requestor, property, xaAtom, 32, xPropModeReplace,
				uintptr(unsafe.Pointer(&atoms[0])), int32(len(atoms)))
		case c.utf8, c.text, xaString:
			b := []byte(c.owned)
			var p uintptr
			if len(b) > 0 {
				p = uintptr(unsafe.Pointer(&b[0]))
			}
			typ := target
			if target == c.text {
				typ = c.utf8
			}
			xChangeProperty(c.dpy, requestor, property, typ, 8, xPropModeReplace, p, int32(len(b)))
			runtime.KeepAlive(b)
		default:
			ok = false
		}
	}
	// SelectionNotify: type 0, serial 8, send_event 16, display 24,
	// requestor 32, selection 40, target 48, property 56, time 64.
	var n xEvent
	*(*int32)(unsafe.Pointer(&n.data[0])) = xSelectionNotify
	n.data[2] = 1
	n.data[3] = uint64(c.dpy)
	n.data[4] = uint64(requestor)
	n.data[5] = uint64(selection)
	n.data[6] = uint64(target)
	n.data[7] = uint64(property)
	if !ok {
		n.data[7] = xNone
	}
	n.data[8] = uint64(tm)
	xSendEvent(c.dpy, requestor, 0, 0, &n)
	xFlush(c.dpy)
}

// clipWait is how long a paste waits for the owner to answer.
const clipWait = 500 * time.Millisecond

// paste fetches the clipboard's text from whoever owns it.
func (c *clipConn) paste() string {
	owner := xGetSelectionOwner(c.dpy, c.clipboard)
	switch {
	case owner == xNone:
		return ""
	case owner == c.win:
		return c.owned
	}
	xDeleteProperty(c.dpy, c.win, c.property)
	xConvertSelection(c.dpy, c.clipboard, c.utf8, c.property, c.win, xCurrentTime)
	xFlush(c.dpy)
	var got, refused bool
	deadline := time.Now().Add(clipWait)
	for !got && time.Now().Before(deadline) {
		got = c.events(func(ev *xEvent) bool {
			// SelectionNotify.property (56): none means the owner said no.
			refused = ev.data[7] == xNone
			return true
		})
		if !got {
			time.Sleep(2 * time.Millisecond)
		}
	}
	if !got || refused {
		return ""
	}
	return c.readProperty()
}

// readProperty reads the text the owner wrote into our property, and
// deletes it.
func (c *clipConn) readProperty() string {
	var typ uintptr
	var format int32
	var n, after uint64
	var data *byte
	if xGetWindowProperty(c.dpy, c.win, c.property, 0, 1<<24, 1, xAnyPropertyType,
		&typ, &format, &n, &after, &data) != 0 || data == nil {
		return ""
	}
	defer xFree(unsafe.Pointer(data))
	if typ == c.incr || format != 8 {
		return "" // a transfer in pieces (INCR) is for megabytes; not taken
	}
	return string(unsafe.Slice(data, n))
}
