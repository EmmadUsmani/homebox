package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework WebKit
#include <stdlib.h>
#include "desktop_view.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"github.com/EmmadUsmani/homebox/desktop/session"
	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/displayproxy"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func registerDesktop(desktop *application.App, window *application.WebviewWindow, box *app.Service, pool *session.Pool) {
	desktop.RegisterService(application.NewService(&Desktop{box: box, pool: pool, window: window, emit: desktop.Event.Emit}))
}

type Desktop struct {
	box    *app.Service
	pool   *session.Pool
	window *application.WebviewWindow
	emit   func(string, ...any) bool
	active *desktopView // Access on the AppKit thread only.
}

type desktopView struct {
	id     string
	handle unsafe.Pointer
}

type DesktopBounds struct {
	X, Y, Width, Height float64
}

type DesktopResult struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

func (d *Desktop) Connect(ctx context.Context, ref session.VMRef, id string, bounds DesktopBounds) DesktopResult {
	_, err := d.checkVM(ctx, ref)
	if err != nil {
		return desktopError(err)
	}
	client, release, err := d.pool.Acquire(ctx, ref)
	if err != nil {
		return desktopError(err)
	}
	defer release()
	tunnel, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_ = client.Wait()
		cancel()
	}()
	proxy, err := displayproxy.OpenViewer(tunnel, client)
	if err != nil {
		return desktopError(err)
	}
	defer func() {
		cancel()
		_ = proxy.Wait()
	}()
	view := application.InvokeSyncWithResult(func() *desktopView {
		if tunnel.Err() != nil {
			return nil
		}
		url := C.CString(proxy.BootstrapURL)
		defer C.free(unsafe.Pointer(url))
		handle := C.desktopViewOpen(d.window.NativeWindow(), url)
		if handle == nil {
			return nil
		}
		d.closeView()
		view := &desktopView{id: id, handle: handle}
		d.active = view
		d.layout(bounds)
		return view
	})
	if view == nil {
		return desktopError(errors.New("Could not open the desktop viewer"))
	}
	defer application.InvokeSync(func() {
		if d.active == view {
			d.closeView()
		}
	})
	d.emit("desktop:connected", id)
	_ = proxy.Wait()
	return DesktopResult{State: "disconnected"}
}

func (d *Desktop) Resize(id string, bounds DesktopBounds) {
	application.InvokeSync(func() {
		if d.active != nil && d.active.id == id {
			d.layout(bounds)
		}
	})
}

func (d *Desktop) Install(ctx context.Context, ref session.VMRef) DesktopResult {
	profile, err := d.checkVM(ctx, ref)
	if err != nil {
		return desktopError(err)
	}
	client, release, err := d.pool.Acquire(ctx, ref)
	if err != nil {
		return desktopError(err)
	}
	defer release()
	if err := displayproxy.Install(ctx, client, d.box.Profiles, profile, nil); err != nil {
		return desktopError(fmt.Errorf("install desktop: %w", err))
	}
	return DesktopResult{State: "idle"}
}

func (d *Desktop) layout(bounds DesktopBounds) {
	C.desktopViewLayout(d.active.handle, C.double(bounds.X), C.double(bounds.Y), C.double(bounds.Width), C.double(bounds.Height))
}

func (d *Desktop) closeView() {
	if d.active != nil {
		C.desktopViewClose(d.active.handle)
		d.active = nil
	}
}

func (d *Desktop) checkVM(ctx context.Context, ref session.VMRef) (string, error) {
	if ref.Kind == "remote" {
		// The guest installer independently verifies that the guest runs Ubuntu.
		return "ubuntu", nil
	}
	return d.box.DesktopProfile(ctx, ref.Name)
}

func desktopError(err error) DesktopResult {
	var key *session.HostKeyChangedError
	switch {
	case errors.As(err, &key):
		return DesktopResult{State: "keyChanged"}
	case errors.Is(err, displayproxy.ErrLayerMissing):
		return DesktopResult{State: "missing"}
	default:
		return DesktopResult{State: "error", Message: err.Error()}
	}
}
