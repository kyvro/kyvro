//go:build darwin

package darwin

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation

#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// Renders <path>'s application icon (NSWorkspace resolves CFBundleIconName /
// Assets.car automatically, so compiled catalogs just work) as a PNG of the
// given square size. Returns malloc'd bytes; caller frees via kyvroFree.
static unsigned char *kyvroRenderAppIconPNG(const char *path, int size, int *length) {
    @autoreleasepool {
        NSString *appPath = [NSString stringWithUTF8String:path];
        NSImage *source = [[NSWorkspace sharedWorkspace] iconForFile:appPath];
        if (source == nil) {
            *length = 0;
            return NULL;
        }

        NSImage *target = [[NSImage alloc] initWithSize:NSMakeSize(size, size)];
        [target lockFocus];
        [[NSGraphicsContext currentContext] setImageInterpolation:NSImageInterpolationHigh];
        [source drawInRect:NSMakeRect(0, 0, size, size)
                  fromRect:NSZeroRect
                 operation:NSCompositingOperationSourceOver
                  fraction:1.0];
        [target unlockFocus];

        CGImageRef cg = [target CGImageForProposedRect:NULL context:nil hints:nil];
        if (cg == NULL) {
            [target release];
            *length = 0;
            return NULL;
        }

        NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithCGImage:cg];
        NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        unsigned char *out = NULL;
        if (png != nil && png.length > 0) {
            out = (unsigned char *)malloc(png.length);
            if (out != NULL) {
                memcpy(out, png.bytes, png.length);
                *length = (int)png.length;
            } else {
                *length = 0;
            }
        } else {
            *length = 0;
        }
        [rep release];
        [target release];
        return out;
    }
}

static void kyvroFree(void *p) {
    free(p);
}

// Converts an on-disk image file (used for .icns entries whose payloads
// jackmordaunt/icns cannot decode — most notably JPEG2000-encoded
// renditions, which AppKit reads natively) to a PNG drawn at the given
// square point size — the exact pipeline as kyvroRenderAppIconPNG so both
// cache variants share one visual size. Returns malloc'd bytes; caller
// frees via kyvroFree.
static unsigned char *kyvroDecodeImagePNG(const char *path, int size, int *length) {
    @autoreleasepool {
        if (size <= 0) {
            size = 512;
        }
        NSString *p = [NSString stringWithUTF8String:path];
        NSImage *src = [[NSImage alloc] initWithContentsOfFile:p];
        if (src == nil) {
            *length = 0;
            return NULL;
        }

        // Prefer the sharpest source representation before drawing — multi-
        // size icns files otherwise expose a small default point size.
        CGFloat best = 32;
        for (NSImageRep *rep in src.representations) {
            CGFloat rw = rep.pixelsWide > 0 ? rep.pixelsWide : 0;
            CGFloat rh = rep.pixelsHigh > 0 ? rep.pixelsHigh : 0;
            if (rw > best) {
                best = rw;
            }
            if (rh > best) {
                best = rh;
            }
        }
        [src setSize:NSMakeSize(best, best)];

        NSImage *target = [[NSImage alloc] initWithSize:NSMakeSize(size, size)];
        [target lockFocus];
        [[NSGraphicsContext currentContext] setImageInterpolation:NSImageInterpolationHigh];
        [src drawInRect:NSMakeRect(0, 0, size, size)
                  fromRect:NSZeroRect
                 operation:NSCompositingOperationSourceOver
                  fraction:1.0];
        [target unlockFocus];

        CGImageRef cg = [target CGImageForProposedRect:NULL context:nil hints:nil];
        if (cg == NULL) {
            [target release];
            [src release];
            *length = 0;
            return NULL;
        }

        NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithCGImage:cg];
        NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        unsigned char *out = NULL;
        if (png != nil && png.length > 0) {
            out = (unsigned char *)malloc(png.length);
            if (out != NULL) {
                memcpy(out, png.bytes, png.length);
                *length = (int)png.length;
            } else {
                *length = 0;
            }
        } else {
            *length = 0;
        }
        [rep release];
        [target release];
        [src release];
        return out;
    }
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// renderedIconSize is the edge length in points of the NSWorkspace-rendered
// fallback icons stored in the app-icons cache — 64pt renders as 128×128 px
// on @2x displays, covering the launcher's 36px rows with retina headroom
// while keeping cache files small.
const renderedIconSize = 64

// renderAppIconPNG asks AppKit for an application's proper icon and returns
// PNG-encoded bytes, renderedIconSize px on each side. This is the fallback
// for bundles whose icon lives only in a compiled asset catalog and
// therefore has no .icns file on disk to point at.
func renderAppIconPNG(appPath string, size int) ([]byte, error) {
	if size <= 0 {
		size = renderedIconSize
	}
	cPath := C.CString(appPath)
	defer C.free(unsafe.Pointer(cPath))

	var length C.int
	data := C.kyvroRenderAppIconPNG(cPath, C.int(size), &length)
	if data == nil || length <= 0 {
		return nil, errors.New("appkit: cannot render application icon")
	}
	defer C.kyvroFree(unsafe.Pointer(data))
	return C.GoBytes(unsafe.Pointer(data), length), nil
}

// DecodeImageFilePNG rasterises an on-disk image file to a square PNG of
// size points via AppKit — the fallback for .icns files whose payloads the
// pure-Go icns decoder cannot read (JPEG2000 renditions). Callers pass 0
// to use the shared renderedIconSize so every cached icon variant matches.
func DecodeImageFilePNG(path string, size int) ([]byte, error) {
	if size <= 0 {
		size = renderedIconSize
	}
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var length C.int
	data := C.kyvroDecodeImagePNG(cPath, C.int(size), &length)
	if data == nil || length <= 0 {
		return nil, errors.New("appkit: cannot decode image file")
	}
	defer C.kyvroFree(unsafe.Pointer(data))
	return C.GoBytes(unsafe.Pointer(data), length), nil
}
