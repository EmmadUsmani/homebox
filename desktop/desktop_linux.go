package main

/*
#cgo pkg-config: gtk4 webkitgtk-6.0
#include <stdlib.h>
#include "desktop_view.h"
*/
import "C"

import (
	"errors"
	"strings"
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
	text, ok := application.Get().Clipboard.Text()
	if !ok {
		return "", errors.New("Host clipboard has no readable plain text")
	}
	if len(text) > displayproxy.ClipboardLimit {
		return "", errors.New("Clipboard text exceeds 1 MiB")
	}
	return text, nil
}

func writeDesktopClipboard(value string) bool {
	if strings.ContainsRune(value, 0) {
		return false // GDK accepts a NUL-terminated UTF-8 string.
	}
	text := C.CString(value)
	defer C.free(unsafe.Pointer(text))
	return C.desktopClipboardWrite(text, C.size_t(len(value))) != 0
}
