import AppKit
import ApplicationServices

struct Failure: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

func waitFor<T>(_ description: String, seconds: TimeInterval = 75, _ read: () throws -> T?) throws -> T {
    let deadline = Date().addingTimeInterval(seconds)
    repeat {
        if let result = try read() { return result }
        Thread.sleep(forTimeInterval: 0.2)
    } while Date() < deadline
    throw Failure(message: "Timed out waiting for \(description)")
}

func attribute<T>(_ element: AXUIElement, _ name: String) -> T? {
    var value: CFTypeRef?
    guard AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success else { return nil }
    return value as? T
}

func descendants(_ element: AXUIElement) -> [AXUIElement] {
    let children: [AXUIElement] = attribute(element, kAXChildrenAttribute) ?? []
    return [element] + children.flatMap(descendants)
}

func windowArea(_ window: [String: Any]) -> CGFloat {
    guard let bounds = window[kCGWindowBounds as String] as? [String: CGFloat] else { return 0 }
    return (bounds["Width"] ?? 0) * (bounds["Height"] ?? 0)
}

func command(_ path: String, _ arguments: [String], environment: [String: String]? = nil) throws -> (Int32, String) {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: path)
    process.arguments = arguments
    process.environment = environment
    let output = Pipe()
    process.standardOutput = output
    try process.run()
    let data = output.fileHandleForReading.readDataToEndOfFile()
    process.waitUntilExit()
    return (process.terminationStatus, String(decoding: data, as: UTF8.self))
}

func verify(_ bundle: String, _ state: String, _ name: String, _ screenshots: String, _ hostPort: Int) throws {
    guard AXIsProcessTrusted() else { throw Failure(message: "Accessibility permission required") }
    let url = URL(fileURLWithPath: bundle).resolvingSymlinksInPath()
    let environment = ProcessInfo.processInfo.environment
    let path = environment["PATH"] ?? "/usr/bin:/bin:/usr/sbin:/sbin"
    let alreadyRunning = NSRunningApplication.runningApplications(withBundleIdentifier: "dev.homebox.desktop").contains {
        $0.bundleURL?.resolvingSymlinksInPath() == url
    }
    func ssh(_ script: String) throws -> String {
        let (status, output) = try command(bundle + "/Contents/MacOS/box", ["ssh", name, "--", script],
            environment: environment.merging(["HOMEBOX_HOME": state]) { _, value in value })
        guard status == 0 else { throw Failure(message: "Guest check failed (\(status)): \(script)") }
        return output
    }
    guard !alreadyRunning else { throw Failure(message: "This app copy is already running") }
    try FileManager.default.createDirectory(atPath: screenshots, withIntermediateDirectories: true)
    let config = screenshots + "/ssh-config"
    let knownHosts = screenshots + "/known-hosts"
    try "".write(toFile: config, atomically: true, encoding: .utf8)
    try "".write(toFile: knownHosts, atomically: true, encoding: .utf8)
    let launchConfig = NSWorkspace.OpenConfiguration()
    launchConfig.environment = [
        "HOMEBOX_HOME": state,
        "PATH": path,
        "HOMEBOX_DESKTOP_SSH_CONFIG": config,
        "HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS": knownHosts,
        "HOMEBOX_DESKTOP_SETTINGS": "\(screenshots)/settings.json",
    ]
    launchConfig.createsNewApplicationInstance = true
    var launch: Result<NSRunningApplication, Error>?
    NSWorkspace.shared.openApplication(at: url, configuration: launchConfig) { app, error in
        launch = app.map { .success($0) } ?? .failure(error ?? Failure(message: "Could not open the app"))
    }
    while launch == nil { RunLoop.current.run(until: Date().addingTimeInterval(0.1)) }
    let app = try launch!.get()
    defer {
        func exited() -> Bool? {
            kill(app.processIdentifier, 0) == -1 && errno == ESRCH ? true : nil
        }
        app.terminate()
        if (try? waitFor("app exit", seconds: 3, exited)) == nil {
            kill(app.processIdentifier, SIGKILL)
            _ = try? waitFor("app exit after force quit", seconds: 3, exited)
        }
    }
    let pid = app.processIdentifier
    let root = AXUIElementCreateApplication(pid)
    let window: AXUIElement = try waitFor("AX window") { (attribute(root, kAXWindowsAttribute) as [AXUIElement]?)?.first }

    func raiseWindow() throws {
        _ = app.activate()
        _ = AXUIElementSetAttributeValue(root, kAXFrontmostAttribute as CFString, kCFBooleanTrue)
        _ = AXUIElementPerformAction(window, kAXRaiseAction as CFString)
        do {
            _ = try waitFor("frontmost app window", seconds: 3) {
                let focused: AXUIElement? = attribute(root, kAXFocusedWindowAttribute)
                return NSWorkspace.shared.frontmostApplication?.processIdentifier == pid &&
                    focused.map { CFEqual($0, window) } == true ? true : nil
            }
        } catch {
            throw Failure(message: "App focus lost while raising the verifier window")
        }
    }

    func labels(_ element: AXUIElement) -> [String] {
        [kAXTitleAttribute, kAXDescriptionAttribute, kAXValueAttribute].compactMap { attribute(element, $0) }
    }

    func find(_ label: String, in container: AXUIElement, roles: [String], prefix: Bool = false) throws -> AXUIElement {
        try waitFor(label) {
            let windows: [AXUIElement] = attribute(container, kAXWindowsAttribute) ?? []
            let focused: AXUIElement? = attribute(container, kAXFocusedWindowAttribute)
            let visible = (focused.map { [$0] } ?? []) + windows
            return (visible.flatMap(descendants) + descendants(container)).first { element in
                let role: String = attribute(element, kAXRoleAttribute) ?? ""
                let enabled: Bool = attribute(element, kAXEnabledAttribute) ?? true
                return enabled && roles.contains(role) && labels(element).contains {
                    prefix ? $0.hasPrefix(label) : $0 == label
                }
            }
        }
    }

    func press(_ label: String, in container: AXUIElement? = nil, roles: [String] = ["AXButton", "AXRadioButton"]) throws {
        let button = try find(label, in: container ?? window, roles: roles)
        guard AXUIElementPerformAction(button, kAXPressAction as CFString) == .success else {
            throw Failure(message: "AXPress failed for \(label)")
        }
    }

    func see(_ label: String, in container: AXUIElement? = nil) throws {
        _ = try waitFor(label) {
            descendants(container ?? window).contains { element in
                labels(element).contains { $0.contains(label) }
            } ? true : nil
        }
    }

    func key(_ code: CGKeyCode, throughWindowServer: Bool = false) throws {
        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == pid else {
            throw Failure(message: "Settings focus lost before editing a field")
        }
        for down in [true, false] {
            let event = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: down)
            event?.flags = []
            if throughWindowServer { event?.post(tap: .cghidEventTap) }
            else { event?.postToPid(pid) }
            Thread.sleep(forTimeInterval: 0.05)
        }
    }

    func fill(_ label: String, _ value: String, from previous: Int? = nil) throws {
        let field = try find(label, in: window, roles: ["AXTextField"])
        func focus() throws {
            try raiseWindow()
            guard AXUIElementSetAttributeValue(field, kAXFocusedAttribute as CFString, kCFBooleanTrue) == .success else {
                throw Failure(message: "Could not focus \(label)")
            }
            _ = try waitFor("keyboard focus in \(label)", seconds: 3) {
                let focused: Bool? = attribute(field, kAXFocusedAttribute)
                return focused == true ? true : nil
            }
        }

        let inputs: [(CGKeyCode, String)]
        if let previous, let target = Int(value) {
            let direction = target > previous ? 1 : -1
            inputs = (0..<abs(target - previous)).map { offset in
                (direction > 0 ? 126 : 125, String(previous + direction * (offset + 1)))
            }
        } else {
            let digitKeys: [CGKeyCode] = [29, 18, 19, 20, 21, 23, 22, 26, 28, 25]
            inputs = value.enumerated().map { offset, digit in
                (digitKeys[digit.wholeNumberValue!], String(value.prefix(offset + 1)))
            }
        }

        try focus()
        for (code, expected) in inputs {
            func expectedValue() -> Bool? {
                let text: String? = attribute(field, kAXValueAttribute)
                let number: NSNumber? = attribute(field, kAXValueAttribute)
                return text == expected || number?.stringValue == expected ? true : nil
            }
            try key(code)
            if (try? waitFor("value in \(label)", seconds: 1, expectedValue)) == nil {
                try focus()
                try key(code, throughWindowServer: true)
                _ = try waitFor("\(label) to reach \(expected)", seconds: 4, expectedValue)
            }
        }
    }

    func screenshot(_ filename: String, dialog: Bool = false) throws {
        if !dialog { try raiseWindow() }
        let windowID = try waitFor("window ID") {
            let windows = CGWindowListCopyWindowInfo(.optionAll, kCGNullWindowID) as? [[String: Any]] ?? []
            let candidates = windows.filter {
                $0[kCGWindowOwnerPID as String] as? pid_t == pid &&
                    (dialog || $0[kCGWindowLayer as String] as? Int == 0)
            }
            let selected = dialog ? candidates.first(where: { windowArea($0) > 10000 }) :
                candidates.max { windowArea($0) < windowArea($1) }
            return selected?[kCGWindowNumber as String] as? Int
        }
        let (status, _) = try command("/usr/sbin/screencapture", ["-x", "-o", "-l", String(windowID), screenshots + "/" + filename])
        guard status == 0 else { throw Failure(message: "Could not capture app window") }
    }

    func box(_ args: [String]) throws -> String {
        let (status, output) = try command(bundle + "/Contents/MacOS/box", args,
            environment: environment.merging(["HOMEBOX_HOME": state]) { _, value in value })
        guard status == 0 else { throw Failure(message: "box \(args.first ?? "") failed (\(status))") }
        return output
    }

    func stop() throws {
        try press("Stop")
        _ = try find("Start", in: window, roles: ["AXButton"])
    }

    func start() throws {
        try press("Start")
        _ = try find("Stop", in: window, roles: ["AXButton"])
    }

    _ = try ssh("printf ready")
    let guestPort = 8123
    let urlString = "http://127.0.0.1:\(hostPort)/"
    _ = try find(name, in: window, roles: ["AXButton"], prefix: true)
    try press("Settings")
    try see("Stop the VM before resizing")
    try stop()
    try fill("Guest port", String(guestPort))
    try fill("Host port", String(hostPort))
    try press("Publish port")
    try press("Publish", in: root)
    try see("Published ports updated")
    let newMapping = try find("Copy URL for port \(guestPort)", in: window, roles: ["AXButton"])
    _ = AXUIElementSetAttributeValue(newMapping, kAXFocusedAttribute as CFString, kCFBooleanTrue)
    try screenshot("settings-ports.png")

    let oldSize = try box(["resize", name]).split(separator: " ")
    guard oldSize.count >= 9, let oldCPUs = Int(oldSize[1]), let oldMemory = Int(oldSize[3]),
          let oldDisk = Int(oldSize[6]) else { throw Failure(message: "Unexpected initial CLI size") }
    let cpus = oldCPUs == 1 ? 2 : 1
    let memory = oldMemory == 3 ? 4 : 3
    let disk = oldDisk + 1
    try fill("CPUs", String(cpus), from: oldCPUs)
    try fill("Memory (GiB)", String(memory), from: oldMemory)
    try fill("Disk (GiB)", String(disk), from: oldDisk)
    try press("Save VM size")
    try press("Grow disk", in: root)
    try see("VM size saved")
    let size = try box(["resize", name])
    guard size.contains("\(cpus) CPUs, \(memory) GiB memory, \(disk) GiB disk") else {
        throw Failure(message: "Unexpected CLI size: \(size)")
    }
    try screenshot("settings-size.png")
    try start()
    _ = try ssh("mkdir -p /tmp/homebox-settings-check; printf 'homebox-settings-check' > /tmp/homebox-settings-check/index.html; nohup python3 -m http.server \(guestPort) --bind 0.0.0.0 --directory /tmp/homebox-settings-check >/tmp/homebox-settings-http.log 2>&1 </dev/null &")
    _ = try waitFor("published HTTP response") {
        let (status, body) = try command("/usr/bin/curl", ["-fs", "--max-time", "2", urlString])
        return status == 0 && body == "homebox-settings-check" ? true : nil
    }
    try press("Open port \(guestPort) in browser")
    _ = try waitFor("browser request in guest HTTP log") {
        let count = try ssh("grep -c 'GET / HTTP/' /tmp/homebox-settings-http.log || true")
        return (Int(count.trimmingCharacters(in: .whitespacesAndNewlines)) ?? 0) >= 2 ? true : nil
    }
    try raiseWindow()
    try stop()
    try press("Unpublish port \(guestPort)")
    try press("Unpublish", in: root)
    try see("Published ports updated")
    guard !((try box(["ports", name])).contains(String(hostPort))) else {
        throw Failure(message: "CLI still reports the removed mapping")
    }

    try start()
    // Keep the separate Lima trust path valid using the new public key read
    // over the existing authenticated connection before the guest activates it.
    let newDriverKey = try ssh("(sudo -n test -f /etc/ssh/homebox-settings-ed25519.pub || sudo -n ssh-keygen -q -t ed25519 -N '' -f /etc/ssh/homebox-settings-ed25519) && sudo -n cat /etc/ssh/homebox-settings-ed25519.pub")
    let driverPins = URL(fileURLWithPath: state).appending(path: "lima/\(name)/ssh.known_hosts")
    let originalPin = try String(contentsOf: driverPins, encoding: .utf8)
    let pinFields = originalPin.split(whereSeparator: \.isWhitespace)
    guard pinFields.count == 3, pinFields[1] == "ssh-ed25519", newDriverKey.hasPrefix("ssh-ed25519 ") else {
        throw Failure(message: "Unexpected Lima pin format")
    }
    let alias = pinFields[0]
    _ = try ssh(#"sudo -n sh -c 'backup=$(mktemp -d /etc/ssh/homebox-settings-original.XXXXXX) && mv /etc/ssh/ssh_host_* "$backup/" && mv /etc/ssh/homebox-settings-ed25519 /etc/ssh/ssh_host_ed25519_key && mv /etc/ssh/homebox-settings-ed25519.pub /etc/ssh/ssh_host_ed25519_key.pub && ssh-keygen -A && systemctl reload ssh'"#)
    try "\(alias) \(newDriverKey)".write(to: driverPins, atomically: false, encoding: .utf8)
    try press("Open terminal")
    try see("Host key changed")
    try screenshot("settings-key-warning.png")
    try press("Open SSH host key settings")
    try press("Review SSH host keys")
    try see("Detected fingerprints")
    let replaceKeys = try find("Replace saved SSH keys…", in: window, roles: ["AXButton"])
    _ = AXUIElementSetAttributeValue(replaceKeys, kAXFocusedAttribute as CFString, kCFBooleanTrue)
    try screenshot("settings-key-review.png")
    try press("Replace saved SSH keys…")
    try press("Replace SSH keys", in: root)
    try see("SSH host keys updated")
    guard try ssh("printf repinned") == "repinned" else { throw Failure(message: "Pinned SSH did not recover") }

    let pasteboard = NSPasteboard.general
    let savedItems = (pasteboard.pasteboardItems ?? []).map { item in
        item.types.compactMap { type in item.data(forType: type).map { (type, $0) } }
    }
    defer {
        pasteboard.clearContents()
        pasteboard.writeObjects(savedItems.map { entries in
            let restored = NSPasteboardItem()
            for (type, data) in entries { restored.setData(data, forType: type) }
            return restored
        })
    }
    try press("Show SSH config")
    try press("Copy SSH config")
    let expectedConfig = try box(["ssh-config"])
    _ = try waitFor("SSH config copied") { pasteboard.string(forType: .string) == expectedConfig ? true : nil }
    try see("SSH config copied.")

    try press("Overview")
    let menuBar: AXUIElement = try waitFor("app menu bar") { attribute(root, kAXMenuBarAttribute) }
    try press("homebox", in: menuBar, roles: ["AXMenuBarItem"])
    try press("About homebox", in: menuBar, roles: ["AXMenuItem"])
    try see("box " + (try box(["version"])).trimmingCharacters(in: .whitespacesAndNewlines), in: root)
    try screenshot("settings-about.png", dialog: true)
    try press("OK", in: root)
    print("Verified port publish/open/unpublish, resize, SSH re-pin, SSH config copy and About.")
}

do {
    let args = CommandLine.arguments
    guard args.count == 6, args[3] == "settingsvm", let hostPort = Int(args[5]),
          FileManager.default.fileExists(atPath: args[2] + "/boxes/settingsvm/box.json"),
          URL(fileURLWithPath: args[2]).deletingLastPathComponent().lastPathComponent.hasPrefix(".hb-w-") else {
        throw Failure(message: "Usage: swift verify-settings.swift APP ~/.hb-w-XXXXXX/state settingsvm SCREENSHOTS UNUSED_HOST_PORT")
    }
    try verify(args[1], args[2], args[3], args[4], hostPort)
} catch {
    fputs("\(error.localizedDescription)\n", stderr)
    exit(1)
}
