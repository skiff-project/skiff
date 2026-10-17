//go:build darwin && cgo

package urlhandler

/*
#cgo LDFLAGS: -framework CoreServices -framework CoreFoundation
#include <stdlib.h>
#include <CoreServices/CoreServices.h>

static CFStringRef cfstr(const char *s) {
	return CFStringCreateWithCString(kCFAllocatorDefault, s, kCFStringEncodingUTF8);
}

// the bundle identifier of the application that opens a scheme, "" none
static int defaultHandler(const char *scheme, char *out, int size) {
	CFStringRef s = cfstr(scheme);
	CFStringRef h = LSCopyDefaultHandlerForURLScheme(s);
	CFRelease(s);
	out[0] = 0;
	if (h == NULL) return 0;
	Boolean ok = CFStringGetCString(h, out, size, kCFStringEncodingUTF8);
	CFRelease(h);
	return ok ? 1 : 0;
}

static int setDefaultHandler(const char *scheme, const char *bundle) {
	CFStringRef s = cfstr(scheme), b = cfstr(bundle);
	OSStatus st = LSSetDefaultHandlerForURLScheme(s, b);
	CFRelease(s);
	CFRelease(b);
	return (int)st;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// The macOS way: Skiff.app lists the schemes in its Info.plist
// (CFBundleURLTypes), and LaunchServices remembers the application of each.
// The URLs come to the running application (desktop.Options.OnURL).

const bundleID = "app.skiff.desktop"

func handlerOf(scheme string) string {
	cs := C.CString(scheme)
	defer C.free(unsafe.Pointer(cs))
	buf := make([]byte, 512)
	if C.defaultHandler(cs, (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf))) == 0 {
		return ""
	}
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
}

func get() Status {
	// LaunchServices cannot forget a handler: another application takes
	// the links back
	st := Status{Supported: true}
	for _, s := range Schemes {
		h := handlerOf(s)
		x := Scheme{Scheme: s, Skiff: h == bundleID, Handler: h}
		if x.Skiff {
			x.Handler = "Skiff"
		}
		st.Schemes = append(st.Schemes, x)
	}
	return st
}

func register(exe string, icon []byte) error {
	b := C.CString(bundleID)
	defer C.free(unsafe.Pointer(b))
	for _, s := range Schemes {
		cs := C.CString(s)
		st := C.setDefaultHandler(cs, b)
		C.free(unsafe.Pointer(cs))
		if st != 0 {
			return fmt.Errorf("LaunchServices refused %s:// (error %d): is Skiff in the Applications folder?", s, int(st))
		}
	}
	return nil
}

func remove(exe string) error { return ErrUnsupported }

func openSettings() error { return ErrUnsupported }
