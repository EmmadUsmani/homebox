import AppKit
import ApplicationServices

struct Failure: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

func waitFor<T>(_ description: String, seconds: TimeInterval = 25, _ read: () throws -> T?) throws -> T {
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

func verify(_ bundle: String, _ state: String, _ name: String, _ screenshots: String) throws {
    guard AXIsProcessTrusted() else { throw Failure(message: "Accessibility permission required") }
    let url = URL(fileURLWithPath: bundle).resolvingSymlinksInPath()
    let environment = ProcessInfo.processInfo.environment
    let path = environment["PATH"] ?? "/usr/bin:/bin:/usr/sbin:/sbin"
    func running() -> NSRunningApplication? {
        NSRunningApplication.runningApplications(withBundleIdentifier: "dev.homebox.desktop").first {
            $0.bundleURL?.resolvingSymlinksInPath() == url
        }
    }
    func ssh(_ script: String) throws -> String {
        let (status, output) = try command(bundle + "/Contents/MacOS/box", ["ssh", name, "--", script],
            environment: environment.merging(["HOMEBOX_HOME": state]) { _, value in value })
        guard status == 0 else { throw Failure(message: "Guest check failed (\(status)): \(script)") }
        return output
    }
    _ = try ssh("command -v xdotool >/dev/null || { echo 'Install xdotool in the test guest' >&2; exit 1; }")
    guard running() == nil else { throw Failure(message: "This app copy is already running") }
    try FileManager.default.createDirectory(atPath: screenshots, withIntermediateDirectories: true)
    let config = screenshots + "/ssh-config"
    let knownHosts = screenshots + "/known-hosts"
    try "".write(toFile: config, atomically: true, encoding: .utf8)
    try "".write(toFile: knownHosts, atomically: true, encoding: .utf8)
    let (launched, _) = try command("/usr/bin/open", ["-n", "--env", "HOMEBOX_HOME=\(state)", "--env", "PATH=\(path)",
        "--env", "HOMEBOX_DESKTOP_SSH_CONFIG=\(config)", "--env", "HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS=\(knownHosts)",
        "--env", "HOMEBOX_DESKTOP_SETTINGS=\(screenshots)/settings.json", url.path])
    guard launched == 0 else { throw Failure(message: "Could not open the app") }
    let app = try waitFor("app launch") { running() }
    defer { app.terminate() }
    app.activate()
    let pid = app.processIdentifier
    let root = AXUIElementCreateApplication(pid)
    let window: AXUIElement = try waitFor("AX window") { (attribute(root, kAXWindowsAttribute) as [AXUIElement]?)?.first }

    func press(_ label: String, prefix: Bool = false) throws {
        let element = try waitFor(label) {
            descendants(window).first { element in
                let actions = [kAXTitleAttribute, kAXDescriptionAttribute].compactMap { key in
                    attribute(element, key) as String?
                }
                let role: String? = attribute(element, kAXRoleAttribute)
                return (role == "AXButton" || role == "AXRadioButton") &&
                    actions.contains { prefix ? $0.hasPrefix(label + " ") || $0 == label : $0 == label }
            }
        }
        guard AXUIElementPerformAction(element, kAXPressAction as CFString) == .success else {
            throw Failure(message: "AXPress failed for \(label)")
        }
    }

    func viewer() -> AXUIElement? {
        descendants(window).first { element in
            let url: URL? = attribute(element, kAXURLAttribute)
            return attribute(element, kAXRoleAttribute) == "AXWebArea" && url?.host == "127.0.0.1"
        }
    }

    func listeners() throws -> [String] {
        let (_, output) = try command("/usr/sbin/lsof", ["-a", "-nP", "-p", String(pid), "-iTCP", "-sTCP:LISTEN", "-Fn"])
        return output.split(separator: "\n").filter { $0.hasPrefix("n") }.map { String($0.dropFirst()) }
    }

    func screenshot(_ filename: String) throws {
        let windowID = try waitFor("window ID") {
            (CGWindowListCopyWindowInfo(.optionOnScreenOnly, kCGNullWindowID) as? [[String: Any]])?.first {
                $0[kCGWindowOwnerPID as String] as? pid_t == pid && $0[kCGWindowLayer as String] as? Int == 0
            }?[kCGWindowNumber as String] as? Int
        }
        let (status, _) = try command("/usr/sbin/screencapture", ["-x", "-o", "-l", String(windowID), screenshots + "/" + filename])
        guard status == 0 else { throw Failure(message: "Could not capture app window") }
    }

    func key(_ code: CGKeyCode) {
        for down in [true, false] {
            CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: down)?.postToPid(pid)
        }
    }

    try press(name, prefix: true)
    try press("Desktop")
    try screenshot("desktop-idle.png")
    func sshProcesses() throws -> Set<Substring> {
        let script = #"ps -eo pid,args | awk -v self="$PPID" '$1 != self && /[s]shd(-session)?: .*@notty$/ { print $1 }'"#
        return Set(try ssh(script).split(separator: "\n"))
    }
    let before = try sshProcesses()
    try press("Connect")
    let remote = try waitFor("native guest web area") { viewer() }
    _ = try waitFor("loopback listener") {
        let values = try listeners()
        return values.count == 1 && values[0].hasPrefix("127.0.0.1:") ? true : nil
    }
    let display = "DISPLAY=:2 XAUTHORITY=/run/homebox-desktop-" + (try ssh("id -un").trimmingCharacters(in: .whitespacesAndNewlines)) + "/Xauthority"
    let marker = ".homeboxdesktop-test"
    let absent = try ssh("test ! -e ~/\(marker) && echo absent")
    guard absent.contains("absent") else { throw Failure(message: "Guest marker already exists") }
    defer { _ = try? ssh("rm -f ~/\(marker)") }
    _ = try ssh("\(display) nohup xfce4-terminal --disable-server --title=DesktopVerification --geometry=82x20+0+0 >/dev/null 2>&1 </dev/null &")
    Thread.sleep(forTimeInterval: 3)
    var raw: CFTypeRef?
    guard AXUIElementCopyAttributeValue(remote, kAXPositionAttribute as CFString, &raw) == .success,
          let raw, CFGetTypeID(raw) == AXValueGetTypeID() else { throw Failure(message: "Guest view has no position") }
    var position = CGPoint.zero
    AXValueGetValue(raw as! AXValue, .cgPoint, &position)
    let point = CGPoint(x: position.x + 140, y: position.y + 120)
    for event in [CGEventType.leftMouseDown, .leftMouseUp] {
        CGEvent(mouseEventSource: nil, mouseType: event, mouseCursorPosition: point, mouseButton: .left)?.post(tap: .cghidEventTap)
    }
    // Type "touch .homeboxdesktop-test" in the guest terminal via hardware key codes.
    for code: CGKeyCode in [17,31,32,8,4,49,47,4,31,46,14,11,31,7,2,14,1,40,17,31,35,27,17,14,1,17,36] { key(code) }
    _ = try waitFor("marker created by the guest terminal") {
        try ssh("test ! -f ~/\(marker) || echo typed").contains("typed") ? true : nil
    }
    try screenshot("desktop-connected.png")

    let clipboard = NSPasteboard.general
    let clipboardTool = "/usr/local/lib/homebox-desktop/clipboard"
    let macText = "printf clipboard-paste > ~/\(marker)"
    let guestText = "Copied from Xfce — café"
    let previousItems = (clipboard.pasteboardItems ?? []).map { original in
        let item = NSPasteboardItem()
        for type in original.types {
            if let data = original.data(forType: type) { item.setData(data, forType: type) }
        }
        return item
    }
    defer {
        if let text = clipboard.string(forType: .string), [macText, guestText].contains(text) {
            clipboard.clearContents()
            clipboard.writeObjects(previousItems)
        }
    }
    let readGuest = "\(clipboardTool) copy 2>/dev/null || true"
    _ = try ssh("printf %s initial-guest-text | \(clipboardTool) paste")
    _ = try waitFor("initial guest clipboard") {
        try ssh(readGuest) == "initial-guest-text" ? true : nil
    }
    clipboard.clearContents()
    guard clipboard.setString(macText, forType: .string) else {
        throw Failure(message: "Could not seed the Mac clipboard")
    }
    let macChange = clipboard.changeCount
    Thread.sleep(forTimeInterval: 1)
    guard try ssh(readGuest) == "initial-guest-text", clipboard.changeCount == macChange else {
        throw Failure(message: "The Mac clipboard synced without a toolbar action")
    }

    try press("Paste to VM")
    _ = try waitFor("Mac text in VM clipboard") {
        try ssh(readGuest) == macText ? true : nil
    }
    // Let Xfce handle its own Paste shortcut on the focused terminal.
    _ = try ssh("\(display) xdotool key --clearmodifiers --delay 250 ctrl+shift+v Return")
    _ = try waitFor("clipboard text pasted into the Xfce terminal") {
        try ssh("cat ~/\(marker)") == "clipboard-paste" ? true : nil
    }
    try screenshot("desktop-clipboard-paste.png")

    _ = try ssh("printf %s '\(guestText)' | \(clipboardTool) paste")
    _ = try waitFor("new guest clipboard") {
        try ssh(readGuest) == guestText ? true : nil
    }
    Thread.sleep(forTimeInterval: 1)
    guard clipboard.changeCount == macChange, clipboard.string(forType: .string) == macText else {
        throw Failure(message: "The VM clipboard synced without a toolbar action")
    }
    try press("Copy from VM")
    _ = try waitFor("guest text in Mac clipboard") {
        clipboard.string(forType: .string) == guestText ? true : nil
    }
    try screenshot("desktop-clipboard-copy.png")

    let after = try sshProcesses()
    let connections = after.subtracting(before)
    guard connections.count == 1, let connection = connections.first, Int(connection) != nil else {
        throw Failure(message: "Could not identify the new desktop SSH process")
    }
    _ = try ssh("sudo -n kill \(connection)")
    _ = try waitFor("listener closed after SSH loss") { try listeners().isEmpty ? true : nil }
    try screenshot("desktop-disconnected.png")
    try press("Reconnect")
    _ = try waitFor("reconnected viewer") { viewer() }
    try screenshot("desktop-reconnected.png")
    try press("Overview")
    _ = try waitFor("listener closed after leaving tab") { try listeners().isEmpty ? true : nil }

    let start = "/usr/local/lib/homebox-desktop/start"
    _ = try ssh("test ! -e \(start).verify && sudo -n mv \(start) \(start).verify")
    defer {
        do { _ = try ssh("sudo -n mv \(start).verify \(start)") }
        catch { fputs("Restore the guest desktop start script: \(error)\n", stderr) }
    }
    try press("Desktop")
    try press("Connect")
    _ = try waitFor("missing layer") {
        descendants(window).first { (attribute($0, kAXValueAttribute) as String?)?.contains("The desktop layer is missing") == true }
    }
    try screenshot("desktop-missing.png")
    try press("Install desktop")
    _ = try waitFor("installer restores the desktop command", seconds: 120) {
        try ssh("test ! -x \(start) || echo installed").contains("installed") ? true : nil
    }
    try press("Connect")
    _ = try waitFor("viewer after installation") { viewer() }
    try press("Overview")
    _ = try waitFor("listener closed after installation check") { try listeners().isEmpty ? true : nil }
    print("Desktop input, explicit clipboard transfer, no automatic sync, reconnect, listener cleanup, missing layer and installation verified")
}

do {
    let args = CommandLine.arguments
    guard args.count == 5 else { throw Failure(message: "Usage: swift verify-desktop.swift APP STATE VM SCREENSHOTS") }
    try verify(args[1], args[2], args[3], args[4])
} catch {
    fputs("\(error.localizedDescription)\n", stderr)
    exit(1)
}
