//go:build darwin

package darwin

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework ApplicationServices

#include <ApplicationServices/ApplicationServices.h>
*/
import "C"
import "unsafe"

//export kyvroEventCallback
func kyvroEventCallback(proxy C.CGEventTapProxy, typ C.CGEventType, event C.CGEventRef, refcon unsafe.Pointer) C.CGEventRef {
	_, _ = proxy, refcon
	if typ != C.kCGEventKeyDown {
		return event
	}

	activeMu.RLock()
	expander := activeExpander
	activeMu.RUnlock()
	if expander == nil {
		return event
	}
	if expander.isSuppressed() {
		return event
	}

	text, ok := eventRune(event)
	if !ok {
		expander.resetBuffer()
		return event
	}

	if replacement, triggerLen, matched := expander.observe(text); matched {
		go expandText(triggerLen, replacement)
		return C.CGEventRef(unsafe.Pointer(nil))
	}
	return event
}
