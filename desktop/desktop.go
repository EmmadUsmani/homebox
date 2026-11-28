//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"github.com/EmmadUsmani/homebox/desktop/session"
	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/displayproxy"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/crypto/ssh"
)

func registerDesktop(desktop *application.App, window *application.WebviewWindow, box *app.Service, pool *session.Pool) {
	desktop.RegisterService(application.NewService(&Desktop{box: box, pool: pool, window: window, emit: desktop.Event.Emit}))
}

type Desktop struct {
	box    *app.Service
	pool   *session.Pool
	window *application.WebviewWindow
	emit   func(string, ...any) bool
	active *desktopView // Access on the UI thread only.
}

type desktopView struct {
	id     string
	handle unsafe.Pointer
	client *ssh.Client
	ctx    context.Context
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
		handle := openDesktopView(d.window.NativeWindow(), proxy.BootstrapURL)
		if handle == nil {
			return nil
		}
		d.closeView()
		view := &desktopView{id: id, handle: handle, client: client, ctx: tunnel}
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

func (d *Desktop) PasteToVM(ctx context.Context, id string) error {
	view := application.InvokeSyncWithResult(func() *desktopView { return d.active })
	if view == nil || view.id != id || view.ctx.Err() != nil || ctx.Err() != nil {
		return errors.New("Desktop is disconnected")
	}
	value, err := readDesktopClipboard()
	if err != nil {
		return err
	}
	if ctx.Err() != nil || view.ctx.Err() != nil ||
		!application.InvokeSyncWithResult(func() bool { return d.active == view }) {
		return errors.New("Desktop is disconnected")
	}
	return displayproxy.PasteClipboard(view.ctx, view.client, value)
}

func (d *Desktop) CopyFromVM(ctx context.Context, id string) error {
	view := application.InvokeSyncWithResult(func() *desktopView { return d.active })
	if view == nil || view.id != id || view.ctx.Err() != nil || ctx.Err() != nil {
		return errors.New("Desktop is disconnected")
	}
	value, err := displayproxy.CopyClipboard(view.ctx, view.client)
	if err != nil {
		return err
	}
	return application.InvokeSyncWithResult(func() error {
		if d.active != view || view.ctx.Err() != nil || ctx.Err() != nil {
			return errors.New("Desktop is disconnected")
		}
		if !writeDesktopClipboard(value) {
			return errors.New("Could not write to the host clipboard")
		}
		return nil
	})
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
	layoutDesktopView(d.active.handle, bounds)
}

func (d *Desktop) closeView() {
	if d.active != nil {
		closeDesktopView(d.active.handle)
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
