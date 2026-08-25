//go:build darwin

// Package darwin provides macOS-specific implementations for Kyvro.
package darwin

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreFoundation -framework Carbon -framework ApplicationServices -framework Foundation -framework AppKit

#import <CoreFoundation/CoreFoundation.h>
#include <Carbon/Carbon.h>
#include <ApplicationServices/ApplicationServices.h>
#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#include <stdlib.h>

extern CGEventRef kyvroEventCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *refcon);

static CGEventRef eventTapCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *refcon) {
    return kyvroEventCallback(proxy, type, event, refcon);
}

typedef struct {
    CFMachPortRef tap;
    CFRunLoopSourceRef source;
    CFRunLoopRef runLoop;
} TapHandle;

BOOL checkAccessibility(void) {
    NSDictionary *options = @{(__bridge id)kAXTrustedCheckOptionPrompt: @NO};
    return AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)options);
}

void requestAccessibility(void) {
    NSDictionary *options = @{(__bridge id)kAXTrustedCheckOptionPrompt: @YES};
    AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)options);
}

TapHandle *createKeyboardTap(void) {
    CGEventMask mask = CGEventMaskBit(kCGEventKeyDown);
    CFMachPortRef tap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap, kCGEventTapOptionDefault, mask, eventTapCallback, NULL);
    if (tap == NULL) {
        return NULL;
    }

    CFRunLoopSourceRef source = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0);
    if (source == NULL) {
        CFRelease(tap);
        return NULL;
    }

    TapHandle *handle = (TapHandle *)calloc(1, sizeof(TapHandle));
    handle->tap = tap;
    handle->source = source;
    return handle;
}

void runKeyboardTap(TapHandle *handle) {
    if (handle == NULL) {
        return;
    }

    handle->runLoop = CFRunLoopGetCurrent();
    CFRetain(handle->runLoop);
    CFRunLoopAddSource(handle->runLoop, handle->source, kCFRunLoopCommonModes);
    CGEventTapEnable(handle->tap, true);
    CFRunLoopRun();

    CGEventTapEnable(handle->tap, false);
    CFRunLoopRemoveSource(handle->runLoop, handle->source, kCFRunLoopCommonModes);
    CFRelease(handle->runLoop);
    CFRelease(handle->source);
    CFRelease(handle->tap);
    free(handle);
}

void stopKeyboardTap(TapHandle *handle) {
    if (handle == NULL) {
        return;
    }
    if (handle->runLoop != NULL) {
        CFRunLoopStop(handle->runLoop);
    } else if (handle->tap != NULL) {
        CGEventTapEnable(handle->tap, false);
    }
}

int eventKeyCode(CGEventRef event) {
    return (int)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
}

unsigned long long eventFlags(CGEventRef event) {
    return (unsigned long long)CGEventGetFlags(event);
}

int eventText(CGEventRef event, UniChar *buffer, int maxLen) {
    UniCharCount actual = 0;
    CGEventKeyboardGetUnicodeString(event, (UniCharCount)maxLen, &actual, buffer);
    return (int)actual;
}

void postBackspaces(int count) {
    for (int i = 0; i < count; i++) {
        CGEventRef down = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)51, true);
        CGEventRef up = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)51, false);
        CGEventPost(kCGHIDEventTap, down);
        CGEventPost(kCGHIDEventTap, up);
        CFRelease(down);
        CFRelease(up);
    }
}

void postUnicodeText(UniChar *chars, int len) {
    if (len <= 0) {
        return;
    }
    CGEventRef down = CGEventCreateKeyboardEvent(NULL, 0, true);
    CGEventKeyboardSetUnicodeString(down, (UniCharCount)len, chars);
    CGEventPost(kCGHIDEventTap, down);
    CFRelease(down);
}

*/
import "C"
import (
	"errors"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unsafe"

	"kyvro/internal/core"
)

// TextExpander implements global text expansion on macOS.
type TextExpander struct {
	mu            sync.Mutex
	enabled       map[string]string
	buffer        []rune
	running       bool
	handle        *C.TapHandle
	stopDone      chan struct{}
	suppressUntil time.Time
}

var (
	activeMu       sync.RWMutex
	activeExpander *TextExpander
)

// NewTextExpander creates a new macOS text expander.
func NewTextExpander() *TextExpander {
	return &TextExpander{
		enabled: make(map[string]string),
		buffer:  make([]rune, 0, maxSnippetBuffer),
	}
}

const maxSnippetBuffer = 256

// Start begins listening for keyboard events and expanding snippets.
func (e *TextExpander) Start(enabled map[string]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.enabled = cloneSnippetMap(enabled)
	e.buffer = e.buffer[:0]
	if e.running {
		return nil
	}

	if !bool(C.checkAccessibility()) {
		C.requestAccessibility()
		return errors.New("accessibility permission not granted; grant Kyvro Accessibility access in System Settings > Privacy & Security > Accessibility")
	}

	handle := C.createKeyboardTap()
	if handle == nil {
		return errors.New("create keyboard event tap")
	}

	e.handle = handle
	e.running = true
	e.stopDone = make(chan struct{})
	activeMu.Lock()
	activeExpander = e
	activeMu.Unlock()

	go func() {
		C.runKeyboardTap(handle)
		e.mu.Lock()
		if e.handle == handle {
			e.handle = nil
			e.running = false
		}
		activeMu.Lock()
		if activeExpander == e {
			activeExpander = nil
		}
		activeMu.Unlock()
		close(e.stopDone)
		e.mu.Unlock()
	}()

	return nil
}

// Stop stops the text expansion listener.
func (e *TextExpander) Stop() error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}

	handle := e.handle
	done := e.stopDone
	e.running = false
	e.handle = nil
	e.buffer = e.buffer[:0]
	activeMu.Lock()
	if activeExpander == e {
		activeExpander = nil
	}
	activeMu.Unlock()
	e.mu.Unlock()

	C.stopKeyboardTap(handle)
	if done != nil {
		<-done
	}
	return nil
}

// IsEnabled checks if the system has granted Accessibility permissions.
func (e *TextExpander) IsEnabled() (bool, error) {
	return bool(C.checkAccessibility()), nil
}

// RequestPermissions prompts the user to grant Accessibility permissions.
func (e *TextExpander) RequestPermissions() error {
	C.requestAccessibility()
	return nil
}

func cloneSnippetMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

func eventRune(event C.CGEventRef) (rune, bool) {
	flags := uint64(C.eventFlags(event))
	const commandControlOption = uint64(C.kCGEventFlagMaskCommand | C.kCGEventFlagMaskControl | C.kCGEventFlagMaskAlternate)
	if flags&commandControlOption != 0 {
		return 0, false
	}

	var chars [8]C.UniChar
	n := int(C.eventText(event, &chars[0], C.int(len(chars))))
	if n != 1 {
		return 0, false
	}
	runes := utf16.Decode([]uint16{uint16(chars[0])})
	if len(runes) != 1 || runes[0] == 0 {
		return 0, false
	}
	return runes[0], true
}

func (e *TextExpander) resetBuffer() {
	e.mu.Lock()
	e.buffer = e.buffer[:0]
	e.mu.Unlock()
}

func (e *TextExpander) suppressSyntheticEvents() {
	e.mu.Lock()
	e.suppressUntil = time.Now().Add(200 * time.Millisecond)
	e.mu.Unlock()
}

func (e *TextExpander) isSuppressed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return time.Now().Before(e.suppressUntil)
}

func (e *TextExpander) observe(r rune) (string, int, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if unicode.IsSpace(r) {
		e.buffer = e.buffer[:0]
		return "", 0, false
	}

	candidate := append(append([]rune(nil), e.buffer...), r)
	var (
		bestReplacement string
		bestLen         int
	)
	for trigger, replacement := range e.enabled {
		triggerRunes := []rune(trigger)
		if hasSuffixRunes(candidate, triggerRunes) && startsAtBoundary(candidate, len(triggerRunes)) {
			if len(triggerRunes) > bestLen {
				bestReplacement = replacement
				bestLen = len(triggerRunes)
			}
		}
	}
	if bestLen > 0 {
		e.buffer = e.buffer[:0]
		return core.RenderSnippetReplacement(bestReplacement, time.Now()), bestLen, true
	}

	e.buffer = append(e.buffer, r)
	if len(e.buffer) > maxSnippetBuffer {
		e.buffer = e.buffer[len(e.buffer)-maxSnippetBuffer:]
	}
	return "", 0, false
}

func hasSuffixRunes(s, suffix []rune) bool {
	if len(suffix) == 0 || len(suffix) > len(s) {
		return false
	}
	for i := range suffix {
		if s[len(s)-len(suffix)+i] != suffix[i] {
			return false
		}
	}
	return true
}

func startsAtBoundary(candidate []rune, triggerLen int) bool {
	before := len(candidate) - triggerLen - 1
	return before < 0 || !isWordRune(candidate[before])
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func expandText(triggerLen int, replacement string) {
	activeMu.RLock()
	expander := activeExpander
	activeMu.RUnlock()
	if expander != nil {
		expander.suppressSyntheticEvents()
	}

	deleteCount := triggerLen - 1
	if deleteCount > 0 {
		C.postBackspaces(C.int(deleteCount))
	}
	postText(replacement)
}

func postText(text string) {
	if text == "" {
		return
	}
	encoded := utf16.Encode([]rune(text))
	for len(encoded) > 0 {
		n := len(encoded)
		if n > 64 {
			n = 64
		}
		C.postUnicodeText((*C.UniChar)(unsafe.Pointer(&encoded[0])), C.int(n))
		encoded = encoded[n:]
	}
}
