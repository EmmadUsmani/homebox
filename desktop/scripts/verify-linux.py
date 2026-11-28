"""Drive the built GTK app through AT-SPI and save XWayland window captures."""

import ctypes
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path

import gi

gi.require_version("Atspi", "2.0")
gi.require_version("Gdk", "3.0")
gi.require_version("GdkX11", "3.0")
from gi.repository import Atspi, Gdk, GdkX11


def descendants(node):
    yield node
    for index in range(node.get_child_count()):
        child = node.get_child_at_index(index)
        if child is not None:
            yield from descendants(child)


def wait_for(find, label, timeout=12):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        result = find()
        if result:
            return result
        time.sleep(0.15)
    raise RuntimeError(f"Timed out waiting for {label}")


def accessible_app(pid):
    desktop = Atspi.get_desktop(0)
    for index in range(desktop.get_child_count()):
        app = desktop.get_child_at_index(index)
        if app.get_process_id() == pid:
            return app
    return None


def app_window(pid):
    app = accessible_app(pid)
    return app.get_child_at_index(0) if app and app.get_child_count() else None


def named(window, name, role=None, timeout=12):
    return wait_for(
        lambda: next(
            (node for node in descendants(window)
             if re.fullmatch(name, node.get_name()) and (not role or node.get_role_name() == role)),
            None,
        ),
        name,
        timeout,
    )


def visible_text(window, text, timeout=12):
    def find():
        for node in descendants(window):
            if text in node.get_name():
                return node
            interface = node.get_text_iface()
            if interface and text in (Atspi.Text.get_text(node, 0, -1) or ""):
                return node
        return None
    return wait_for(find, text, timeout)


def press(window, name, role="button"):
    node = named(window, name, role)
    if not node.do_action(0):
        raise RuntimeError(f"Failed to click {name}")


def x11_window(pid, title):
    display = Gdk.Display.get_default()
    tree = subprocess.check_output(["xwininfo", "-root", "-tree"], text=True)
    for xid in re.findall(rf'^\s*(0x[0-9a-f]+) "{re.escape(title)}":', tree, re.MULTILINE):
        prop = subprocess.check_output(["xprop", "-id", xid, "_NET_WM_PID"], text=True)
        if prop.strip().endswith(f"= {pid}"):
            window = GdkX11.X11Window.foreign_new_for_display(display, int(xid, 16))
            if window.get_width() < 100:
                continue
            return int(xid, 16), window
    raise RuntimeError(f"No X11 window found for {title} ({pid})")


def screenshot(pid, title, path):
    # Capture the app window alone, even if other windows are on the desktop.
    _, window = x11_window(pid, title)
    pixbuf = Gdk.pixbuf_get_from_window(window, 0, 0, window.get_width(), window.get_height())
    if not pixbuf:
        raise RuntimeError(f"Could not capture {title}")
    pixbuf.savev(str(path), "png", [], [])



class Keyboard:
    """Send actual X11 keyboard events to the focused app."""

    def __init__(self, pid):
        self.x11 = ctypes.CDLL("libX11.so.6")
        self.xtst = ctypes.CDLL("libXtst.so.6")
        self.x11.XOpenDisplay.argtypes = [ctypes.c_char_p]
        self.x11.XOpenDisplay.restype = ctypes.c_void_p
        self.x11.XStringToKeysym.argtypes = [ctypes.c_char_p]
        self.x11.XStringToKeysym.restype = ctypes.c_ulong
        self.x11.XKeysymToKeycode.argtypes = [ctypes.c_void_p, ctypes.c_ulong]
        self.x11.XKeysymToKeycode.restype = ctypes.c_uint
        self.x11.XSetInputFocus.argtypes = [
            ctypes.c_void_p, ctypes.c_ulong, ctypes.c_int, ctypes.c_ulong,
        ]
        self.x11.XFlush.argtypes = [ctypes.c_void_p]
        self.xtst.XTestFakeKeyEvent.argtypes = [ctypes.c_void_p, ctypes.c_uint, ctypes.c_int, ctypes.c_ulong]
        self.display = self.x11.XOpenDisplay(None)
        if not self.display:
            raise RuntimeError("Could not open the test X display")
        xid, window = x11_window(pid, "homebox")
        window.focus(Gdk.CURRENT_TIME)
        self.x11.XSetInputFocus(self.display, xid, 2, 0)
        self.x11.XFlush(self.display)

    def key(self, name, *modifiers):
        keys = [self.x11.XKeysymToKeycode(self.display, self.x11.XStringToKeysym(key.encode()))
                for key in (*modifiers, name)]
        if not all(keys):
            raise RuntimeError(f"Unknown X11 key {name}")
        for code in keys:
            self.xtst.XTestFakeKeyEvent(self.display, code, True, 0)
        for code in reversed(keys):
            self.xtst.XTestFakeKeyEvent(self.display, code, False, 0)
        self.x11.XFlush(self.display)

    def type(self, text):
        for letter in text:
            self.key({" ": "space", "-": "minus"}.get(letter, letter))


def verify_vm(pid, output, vm, binary, created=False):
    window = wait_for(lambda: app_window(pid), "Homebox window")
    press(window, re.escape(vm) + ".*")
    named(window, "Stop", "button")
    screenshot(pid, "homebox", output / "linux-vm-overview.png")

    def ssh(command):
        return subprocess.run([str(Path(binary).with_name("box")), "ssh", vm, "--", command],
                              capture_output=True, text=True, timeout=5)

    def check_terminal(marker):
        press(window, "Open terminal")
        terminal = named(window, "Terminal input")
        keyboard = Keyboard(pid)
        Atspi.Component.grab_focus(terminal)
        keyboard.type("touch " + marker)
        keyboard.key("Return")
        wait_for(lambda: ssh("test -f " + marker).returncode == 0,
                 "command executed in the guest terminal")
        ssh("rm -f -- " + marker).check_returncode()

    marker = f"linuxterminal{os.getpid()}"
    check_terminal(marker)
    screenshot(pid, "homebox", output / "linux-vm-terminal.png")

    press(window, "Files", "page tab")
    press(window, re.escape("homebox-linux-fixture.txt"))
    editor = named(window, "File editor")
    keyboard = Keyboard(pid)
    Atspi.Component.grab_focus(editor)
    keyboard.key("a", "Control_L")
    keyboard.type("linux editor verified")
    keyboard.key("s", "Control_L")
    def saved_text():
        result = ssh('cat "$HOME/homebox-linux-fixture.txt"')
        return result.returncode == 0 and result.stdout.rstrip() == "linux editor verified"
    wait_for(saved_text, "editor save over SFTP")
    screenshot(pid, "homebox", output / "linux-vm-editor.png")

    if os.getenv("HOMEBOX_VERIFY_NATIVE_DIALOGS") == "1":
        app = accessible_app(pid)
        for action, title in [("Upload", "Upload file to VM"), ("Download", "Download file from VM")]:
            wait_for(lambda: named(window, action, "button").get_state_set().contains(Atspi.StateType.ENABLED),
                     f"enabled {action} action")
            press(window, action)
            chooser = named(app, title)
            screenshot(pid, title, output / f"linux-vm-{action.lower()}.png")
            press(chooser, r"_?Cancel", None)

    press(window, "Desktop", "page tab")
    press(window, "Connect")
    if created:
        press(window, "Install desktop")
        visible_text(window, "Installing the desktop layer")
        screenshot(pid, "homebox", output / "linux-desktop-install.png")
        enabled(window, "Connect", timeout=900)
        press(window, "Connect")
    named(window, "Copy from VM", "button", 60)
    time.sleep(3)
    screenshot(pid, "homebox", output / "linux-vm-desktop.png")

    if os.getenv("HOMEBOX_VERIFY_CLIPBOARD") == "1":
        # This opt-in replaces the clipboard on the test X11 display.
        gi.require_version("Gtk", "3.0")
        from gi.repository import Gtk
        clipboard = Gtk.Clipboard.get(Gdk.SELECTION_CLIPBOARD)
        command = "/usr/local/lib/homebox-desktop/clipboard "
        ssh("printf 'linux clipboard from guest' | " + command + "paste").check_returncode()
        press(window, "Copy from VM")
        visible_text(window, "Text copied to host clipboard.")
        if clipboard.wait_for_text() != "linux clipboard from guest":
            raise RuntimeError("Guest clipboard text did not reach the host")
        clipboard.set_text("linux clipboard from host", -1)
        press(window, "Paste to VM")
        def guest_clipboard_matches():
            while Gtk.events_pending():
                Gtk.main_iteration_do(False)
            result = ssh(command + "copy")
            return result.returncode == 0 and result.stdout == "linux clipboard from host"
        wait_for(guest_clipboard_matches, "clipboard text pasted into guest")
        screenshot(pid, "homebox", output / "linux-vm-clipboard.png")
    press(window, "Overview", "page tab")
    press(window, "Desktop", "page tab")
    press(window, "Connect")
    named(window, "Copy from VM", "button")
    press(window, "Overview", "page tab")

    remote = os.getenv("HOMEBOX_VERIFY_REMOTE_ALIAS")
    if remote and not created:
        press(window, re.escape(remote) + r".*\breachable")
        check_terminal(marker + "remote")
        screenshot(pid, "homebox", output / "linux-vm-pinned-ssh.png")

    if not created and os.getenv("HOMEBOX_VERIFY_LIFECYCLE") == "1":
        press(window, re.escape(vm) + ".*")
        press(window, "Stop")
        named(window, re.escape(vm) + r".*\bstopped", "button", 60)
        press(window, "Start")
        def ssh_ready():
            try:
                return ssh("true").returncode == 0
            except subprocess.TimeoutExpired:
                return False
        wait_for(ssh_ready, "SSH after restarting the guest", 120)
        named(window, re.escape(vm) + r".*\brunning", "button", 60)
        screenshot(pid, "homebox", output / "linux-vm-restarted.png")

    print("Linux VM desktop smoke passed")

def enabled(window, name, role="button", timeout=12):
    return wait_for(
        lambda: next((node for node in descendants(window)
                      if re.fullmatch(name, node.get_name()) and node.get_role_name() == role
                      and node.get_state_set().contains(Atspi.StateType.ENABLED)), None),
        f"enabled {name}", timeout,
    )


def enter(window, keyboard, name, value):
    node = enabled(window, re.escape(name) + r"(?: .*)?", "entry")
    if not Atspi.Component.grab_focus(node):
        raise RuntimeError(f"Failed to focus {name}")
    keyboard.key("a", "Control_L")
    keyboard.type(value)
    wait_for(lambda: Atspi.Text.get_text(node, 0, -1) == value,
             f"{name} input to receive the fixture value")


def operation(window, success, failure, timeout=1800):
    def result():
        for node in descendants(window):
            name = node.get_name()
            if node.get_text_iface() and not name:
                name = Atspi.Text.get_text(node, 0, -1) or ""
            if name.startswith(failure):
                raise RuntimeError(name)
            if name.startswith(success):
                return True
        return False

    wait_for(result, success, timeout)


def verify_create_delete(pid, output, binary, vm):
    window = wait_for(lambda: app_window(pid), "Homebox window")
    state = Path(os.environ["HOMEBOX_HOME"])
    box = Path(binary).with_name("box")

    def capture(step):
        screenshot(pid, "homebox", output / f"linux-lifecycle-{step}.png")

    def cli(*args):
        return subprocess.check_output([box, *args], text=True, timeout=15)

    named(window, "No VMs on this host", "heading")
    press(window, "Set up this host")
    operation(window, "Host setup complete", "Host setup failed", 60)
    capture("host-setup")
    enabled(window, "New VM")
    press(window, "New VM")
    enabled(window, "Create VM")
    keyboard = Keyboard(pid)
    enter(window, keyboard, "VM name", vm)
    enter(window, keyboard, "CPUs", "2")
    enter(window, keyboard, "Memory (GiB)", "4")
    capture("new-vm")
    press(window, "Create VM")
    visible_text(window, "Creating VM…")
    capture("creating")
    operation(window, "VM created", "VM creation failed")
    named(window, vm + r".*\brunning", "button", 60)
    capture("created")
    record = state / "boxes" / vm / "box.json"
    size = json.loads(record.read_text())
    if (size["cpus"], size["memory_gib"]) != (2, 4):
        raise RuntimeError("New VM did not use the entered CPU and memory values")

    cli("ssh", vm, "--", 'printf "linux editor fixture\n" > "$HOME/homebox-linux-fixture.txt"')
    verify_vm(pid, output, vm, binary, created=True)

    press(window, "More")
    press(window, "Re-run provisioning")
    visible_text(window, "Provisioning VM…")
    capture("provisioning")
    operation(window, "Provisioning complete", "Provisioning failed")
    capture("provisioned")

    press(window, "Stop")
    named(window, vm + r".*\bstopped", "button", 120)
    capture("stopped")
    press(window, "Settings", "page tab")
    enter(window, keyboard, "Memory (GiB)", "3")
    enabled(window, "Save VM size")
    press(window, "Save VM size")
    visible_text(window, "VM size saved.")
    if json.loads(record.read_text())["memory_gib"] != 3:
        raise RuntimeError("Resized memory was not persisted")
    capture("resized")
    host_port = 20000 + 100 * int(os.environ.get("HOMEBOX_FIRST_INDEX", "1"))
    enter(window, keyboard, "Guest port", "9000")
    enter(window, keyboard, "Host port", str(host_port))
    press(window, "Publish port")
    press(accessible_app(pid), "Publish")
    visible_text(window, "Published ports updated.")
    visible_text(window, f"Guest 9000 → 127.0.0.1:{host_port}")
    capture("port-published")
    press(window, "Overview", "page tab")
    press(window, "Start")
    named(window, vm + r".*\brunning", "button", 180)

    def ssh_ready():
        try:
            return subprocess.run([box, "ssh", vm, "--", "true"], capture_output=True,
                                  timeout=5).returncode == 0
        except subprocess.TimeoutExpired:
            return False

    wait_for(ssh_ready, "SSH after restarting the guest", 120)
    capture("restarted")

    press(window, "More")
    press(window, "Delete VM…")
    confirmation = f"Type {vm} to confirm"
    enter(window, keyboard, confirmation, "wrong")
    if named(window, "Delete VM", "button").get_state_set().contains(Atspi.StateType.ENABLED):
        raise RuntimeError("Delete accepted an incorrect name")
    capture("delete-disabled")
    enter(window, keyboard, confirmation, vm)
    enabled(window, "Delete VM")
    capture("delete-confirmation")
    press(window, "Delete VM")
    operation(window, "VM deleted", "VM deletion failed", 120)
    named(window, "No VMs on this host", "heading")
    if any((state / directory / vm).exists() for directory in ("boxes", "lima")):
        raise RuntimeError("VM state remains after deletion")
    capture("deleted")
    print("Linux desktop create, provision, resize, publish, restart and delete passed")


def verify(pid, output):
    window = wait_for(lambda: app_window(pid), "Homebox window")
    named(window, "No VMs on this host", "heading")
    screenshot(pid, "homebox", output / "linux-empty.png")
    press(window, "Set up this host")
    wait_for(lambda: Path(os.environ["HOMEBOX_HOME"], "host.json").is_file(), "saved host setup")
    wait_for(
        lambda: any(node.get_name() == "New VM" and node.get_state_set().contains(Atspi.StateType.ENABLED)
                    for node in descendants(window)),
        "enabled New VM",
    )
    press(window, "New VM")
    wait_for(lambda: any(node.get_name() == "Create VM"
                        and node.get_state_set().contains(Atspi.StateType.ENABLED)
                        for node in descendants(window)), "loaded VM defaults")
    screenshot(pid, "homebox", output / "linux-create.png")
    press(window, "Cancel")
    press(window, "Add a host")
    named(window, "Add host", "button")
    screenshot(pid, "homebox", output / "linux-remote.png")
    press(window, "Add a host")

    # Refresh real box state without starting an unconfined VM.
    state = Path(os.environ["HOMEBOX_HOME"], "boxes", "fixture")
    (state / "run").mkdir(parents=True)
    (state / "box.json").write_text(json.dumps({
        "name": "fixture", "driver": "lima", "profile": "ubuntu",
        "cpus": 2, "memory_gib": 2, "disk_gib": 20,
    }))
    (state / "run" / "vmnet.log").write_text("Linux smoke network log\n")
    press(window, r"fixture.*")
    press(window, "Logs", "page tab")
    press(window, "Network", "toggle button")
    visible_text(window, "Linux smoke network log")
    screenshot(pid, "homebox", output / "linux-logs.png")
    press(window, "Settings", "page tab")
    named(window, "Published ports", "heading")
    screenshot(pid, "homebox", output / "linux-settings.png")

    press(window, r"ssh-fixture.*")
    named(window, "Remote connection hints", "heading")
    visible_text(window, "VM not running")
    screenshot(pid, "homebox", output / "linux-ssh-refused.png")
    print("Linux desktop smoke passed")


if __name__ == "__main__":
    try:
        if len(sys.argv) > 3 and sys.argv[3] == "--create-delete":
            verify_create_delete(int(sys.argv[1]), Path(sys.argv[2]), sys.argv[4], sys.argv[5])
        elif len(sys.argv) > 3:
            verify_vm(int(sys.argv[1]), Path(sys.argv[2]), sys.argv[3], sys.argv[4])
        else:
            verify(int(sys.argv[1]), Path(sys.argv[2]))
    except RuntimeError:
        try:
            screenshot(int(sys.argv[1]), "homebox", Path(sys.argv[2]) / "linux-failure.png")
        except RuntimeError:
            pass
        window = app_window(int(sys.argv[1]))
        if window:
            for node in descendants(window):
                print(node.get_role_name(), node.get_name(), file=sys.stderr)
        raise
