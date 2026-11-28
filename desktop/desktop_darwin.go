package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework WebKit
#include <stdlib.h>
#include "desktop_view.h"
*/
import "C"

import (
	"errors"
	"unsafe"

	"github.com/EmmadUsmani/homebox/internal/displayproxy"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func openDesktopView(window unsafe.Pointer, address string) unsafe.Pointer {
	url := C.CString(address)
	defer C.free(unsafe.Pointer(url))
	return C.desktopViewOpen(window, url)
}

func layoutDesktopView(view unsafe.Pointer, bounds DesktopBounds) {
	C.desktopViewLayout(view, C.double(bounds.X), C.double(bounds.Y), C.double(bounds.Width), C.double(bounds.Height))
}

func closeDesktopView(view unsafe.Pointer) {
	C.desktopViewClose(view)
}

func readDesktopClipboard() (string, error) {
	return application.InvokeSyncWithResultAndError(func() (string, error) {
		var length C.size_t
		text := C.desktopClipboardRead(displayproxy.ClipboardLimit, &length)
		if length > displayproxy.ClipboardLimit {
			return "", errors.New("Clipboard text exceeds 1 MiB")
		}
		if text == nil {
			return "", errors.New("Mac clipboard has no readable plain text")
		}
		defer C.free(unsafe.Pointer(text))
		return C.GoStringN(text, C.int(length)), nil
	})
}

func writeDesktopClipboard(value string) bool {
	text := C.CString(value)
	defer C.free(unsafe.Pointer(text))
	return C.desktopClipboardWrite(text, C.size_t(len(value))) != 0
}
