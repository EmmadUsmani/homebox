"""Drive the built GTK app through AT-SPI and save XWayland window captures."""

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


def wait_for(find, label):
    deadline = time.monotonic() + 12
    while time.monotonic() < deadline:
        result = find()
        if result:
            return result
        time.sleep(0.15)
    raise RuntimeError(f"Timed out waiting for {label}")


def app_window(pid):
    desktop = Atspi.get_desktop(0)
    for index in range(desktop.get_child_count()):
        app = desktop.get_child_at_index(index)
        if app.get_process_id() == pid:
            return app.get_child_at_index(0)
    return None


def named(window, name, role=None):
    return wait_for(
        lambda: next(
            (node for node in descendants(window)
             if re.fullmatch(name, node.get_name()) and (not role or node.get_role_name() == role)),
            None,
        ),
        name,
    )


def press(window, name, role="button"):
    node = named(window, name, role)
    if not node.do_action(0):
        raise RuntimeError(f"Failed to click {name}")


def screenshot(pid, title, path):
    # Capture the app window alone, even if other windows are on the desktop.
    display = Gdk.Display.get_default()
    tree = subprocess.check_output(["xwininfo", "-root", "-tree"], text=True)
    for xid in re.findall(rf'^\s*(0x[0-9a-f]+) "{re.escape(title)}":', tree, re.MULTILINE):
        prop = subprocess.check_output(["xprop", "-id", xid, "_NET_WM_PID"], text=True)
        if prop.strip().endswith(f"= {pid}"):
            window = GdkX11.X11Window.foreign_new_for_display(display, int(xid, 16))
            if window.get_width() < 100:
                continue
            pixbuf = Gdk.pixbuf_get_from_window(window, 0, 0, window.get_width(), window.get_height())
            if not pixbuf:
                raise RuntimeError(f"Could not capture {title}")
            pixbuf.savev(str(path), "png", [], [])
            return
    raise RuntimeError(f"No X11 window found for {title} ({pid})")


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
    named(window, "Create VM", "button")
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
    named(window, "Linux smoke network log")
    screenshot(pid, "homebox", output / "linux-logs.png")
    press(window, "Settings", "page tab")
    named(window, "Published ports", "heading")
    screenshot(pid, "homebox", output / "linux-settings.png")

    press(window, r"ssh-fixture.*")
    named(window, "Remote connection hints", "heading")
    named(window, "VM not running")
    screenshot(pid, "homebox", output / "linux-ssh-refused.png")
    print("Linux desktop smoke passed")


if __name__ == "__main__":
    verify(int(sys.argv[1]), Path(sys.argv[2]))
