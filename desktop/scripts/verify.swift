import AppKit
import ApplicationServices

struct VerificationError: LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}

func run(_ binary: String, _ arguments: [String]) throws {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: binary)
    process.arguments = arguments
    try process.run()
    process.waitUntilExit()
    guard process.terminationStatus == 0 else {
        throw VerificationError("\(binary) exited with status \(process.terminationStatus)")
    }
}

func waitFor<T>(_ description: String, seconds: TimeInterval = 15, _ read: () -> T?) throws -> T {
    let deadline = Date().addingTimeInterval(seconds)
    repeat {
        if let value = read() { return value }
        Thread.sleep(forTimeInterval: 0.2)
    } while Date() < deadline
    throw VerificationError("Timed out waiting for \(description).")
}

func attribute<T>(_ element: AXUIElement, _ name: String) -> T? {
    var value: CFTypeRef?
    guard AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success else {
        return nil
    }
    return value as? T
}

func children(_ element: AXUIElement) -> [AXUIElement] {
    attribute(element, kAXChildrenAttribute) ?? []
}

func identifier(_ element: AXUIElement) -> String? {
    attribute(element, kAXIdentifierAttribute) ?? attribute(element, "AXDOMIdentifier")
}

func find<T>(_ root: AXUIElement, skippingLogOutput: Bool = true,
             _ match: (AXUIElement) -> T?) -> T? {
    if let result = match(root) { return result }
    if skippingLogOutput && identifier(root) == "log-output" { return nil }
    for child in children(root) {
        if let result = find(child, skippingLogOutput: skippingLogOutput, match) { return result }
    }
    return nil
}

func childCount(_ element: AXUIElement) -> Int {
    var count = 0
    AXUIElementGetAttributeValueCount(element, kAXChildrenAttribute as CFString, &count)
    return count
}

func recentChildren(_ element: AXUIElement, limit: Int) -> [AXUIElement] {
    let count = childCount(element)
    guard count > 0 else { return [] }
    var values: CFArray?
    let length = min(count, limit)
    guard AXUIElementCopyAttributeValues(element, kAXChildrenAttribute as CFString,
                                         count - length, length, &values) == .success else { return [] }
    return values as? [AXUIElement] ?? []
}

func output(_ binary: String, _ arguments: [String]) throws -> (Int32, String) {
    let process = Process()
    let pipe = Pipe()
    process.executableURL = URL(fileURLWithPath: binary)
    process.arguments = arguments
    process.standardOutput = pipe
    try process.run()
    let data = pipe.fileHandleForReading.readDataToEndOfFile()
    process.waitUntilExit()
    return (process.terminationStatus, String(decoding: data, as: UTF8.self))
}

func verify(bundle: String, scratch: String, screenshot: String, mode: String, name: String?) throws {
    guard AXIsProcessTrusted() else {
        throw VerificationError("Grant Accessibility to the terminal or agent running this script.")
    }
    let bundleURL = URL(fileURLWithPath: bundle).resolvingSymlinksInPath().path
    if name != nil {
        let scratchURL = URL(fileURLWithPath: scratch).resolvingSymlinksInPath()
        guard scratchURL.deletingLastPathComponent().path == NSHomeDirectory(),
              scratchURL.lastPathComponent.hasPrefix(".hb-j-"), name == "scratch-j" else {
            throw VerificationError("Lifecycle verification requires the scratch-j VM in a disposable ~/.hb-j-* directory.")
        }
    }
    let bundleID = "dev.homebox.desktop"
    func matchingApps() -> [NSRunningApplication] {
        NSRunningApplication.runningApplications(withBundleIdentifier: bundleID).filter {
            $0.bundleURL?.resolvingSymlinksInPath().path == bundleURL
        }
    }
    guard matchingApps().isEmpty else {
        throw VerificationError("Close this copy of homebox before running verification.")
    }
    let environment = ProcessInfo.processInfo.environment
    let args = ["-n", "--env", "HOMEBOX_HOME=\(scratch)"]
    if mode == "finder" {
        try run("/usr/bin/env", ["-i", "HOME=\(NSHomeDirectory())", "/usr/bin/open"] + args + [bundle])
    } else {
        try run("/usr/bin/open", args + ["--env", "PATH=\(environment["PATH"] ?? "/usr/bin:/bin")", bundle])
    }
    defer {
        for app in matchingApps() {
            app.terminate()
            if (try? waitFor("homebox to quit", seconds: 3, {
                kill(app.processIdentifier, 0) == -1 && errno == ESRCH ? true : nil
            })) == nil {
                app.forceTerminate()
            }
        }
    }
    let app = try waitFor("homebox to launch") { matchingApps().first }
    let root = AXUIElementCreateApplication(app.processIdentifier)
    let window: AXUIElement = try waitFor("homebox window") {
        let windows: [AXUIElement] = attribute(root, kAXWindowsAttribute) ?? []
        return windows.first
    }
    let webArea: AXUIElement = try waitFor("WKWebView accessibility tree") {
        find(window) { attribute($0, kAXRoleAttribute) == "AXWebArea" ? $0 : nil }
    }

    let (_, process) = try output("/bin/ps", ["-wwE", "-p", String(app.processIdentifier), "-o", "command="])
    let launchPath = process.split(separator: " ").first { $0.hasPrefix("PATH=") }?.split(separator: "\n").first
    if mode == "finder" {
        print("Launch finder: \(launchPath.map(String.init) ?? "no PATH variable")")
    } else {
        print("Launch normal: explicit shell PATH")
    }
    if mode == "finder", let launchPath, launchPath != "PATH=/usr/bin:/bin:/usr/sbin:/sbin" {
        throw VerificationError("Finder-style launch inherited a non-system path.")
    }

    func text(_ element: AXUIElement) -> [String] {
        [kAXValueAttribute, kAXTitleAttribute, kAXDescriptionAttribute].compactMap { attribute(element, $0) }
    }
    func scope(_ id: String) -> AXUIElement? {
        find(webArea) { identifier($0) == id ? $0 : nil }
    }
    func read(_ phrase: String, seconds: TimeInterval = 20) throws {
        let match = try waitFor(phrase, seconds: seconds) {
            find(webArea) { text($0).first { $0.contains(phrase) } }
        }
        print("AX read: \(match)")
    }
    func readLog(_ phrase: String) throws {
        _ = try waitFor(phrase) {
            guard let pane = scope("log-output") else { return nil as Bool? }
            return recentChildren(pane, limit: 128).reversed().contains { row in
                find(row, skippingLogOutput: false) {
                    text($0).contains(where: { $0.contains(phrase) }) ? true : nil
                } == true
            } ? true : nil
        }
        print("AX log read: \(phrase)")
    }
    func control(_ title: String, in group: String?) -> AXUIElement? {
        guard let container = group == nil ? webArea : group.flatMap(scope) else { return nil }
        return find(container) { element in
            let role: String? = attribute(element, kAXRoleAttribute)
            guard role == kAXButtonRole || role == kAXRadioButtonRole || role == kAXCheckBoxRole ||
                    role == "AXTab" || role == "AXToggleButton",
                  text(element).contains(title), attribute(element, kAXEnabledAttribute) != false else { return nil }
            return element
        }
    }
    func press(_ title: String, in group: String? = nil) throws {
        let button = try waitFor(title) { control(title, in: group) }
        guard AXUIElementPerformAction(button, kAXPressAction as CFString) == .success else {
            throw VerificationError("AXPress failed for \(title).")
        }
        print("AX press: \(title)")
    }
    func noListeners() throws {
        let (status, listeners) = try output("/usr/sbin/lsof", ["-nP", "-a", "-p", String(app.processIdentifier), "-iTCP", "-sTCP:LISTEN"])
        guard status == 1 && listeners.isEmpty else {
            throw VerificationError("Unexpected desktop TCP listener: \(listeners)")
        }
        print("lsof: no desktop TCP listeners")
    }
    func scratchSSH(unit: String? = nil) -> Bool {
        guard let (_, processes) = try? output("/usr/bin/pgrep", ["-fl", "ssh"]) else { return false }
        return processes.split(separator: "\n").contains { line in
            line.contains(scratch) && line.contains("journalctl") &&
                (unit.map { line.contains("--unit=\($0)") } ?? true)
        }
    }
    let windowID: Int = try waitFor("window ID") {
        let windows = CGWindowListCopyWindowInfo(.optionOnScreenOnly, kCGNullWindowID)
            as? [[String: Any]] ?? []
        return windows.first {
            $0[kCGWindowOwnerPID as String] as? pid_t == app.processIdentifier &&
                $0[kCGWindowLayer as String] as? Int == 0
        }?[kCGWindowNumber as String] as? Int
    }
    func capture(_ name: String) throws {
        let path = name.isEmpty ? screenshot : URL(fileURLWithPath: screenshot).deletingPathExtension().path + "-\(name).png"
        try run("/usr/sbin/screencapture", ["-x", "-o", "-l", String(windowID), path])
        print("Screenshot: \(path)")
    }

    func closeWindow() throws {
        let closeButton: AXUIElement? = attribute(window, kAXCloseButtonAttribute)
        guard let closeButton, AXUIElementPerformAction(closeButton, kAXPressAction as CFString) == .success else {
            throw VerificationError("Could not close the homebox window.")
        }
    }

    try noListeners()
    guard let name else {
        try read("No VMs on this Mac")
        try read("box new NAME")
        try capture("")
        try closeWindow()
        return
    }

    try read(name)
    try read("CPU (seen from the Mac)")
    try press("Stop", in: "vm-header")
    try read("Stopped", seconds: 120)
    try press("Start", in: "vm-header")
    try read("Running", seconds: 120)
    let resultPath = URL(fileURLWithPath: scratch).appendingPathComponent("boxes/\(name)/doctor.json")
    let previousDoctor = try? Data(contentsOf: resultPath)
    try press("Run doctor", in: "vm-header")
    try read("Running isolation checks")
    _ = try waitFor("doctor to finish and save its new result", seconds: 120) {
        guard let data = try? Data(contentsOf: resultPath), data != previousDoctor,
              let result = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              result["failed"] as? Int == 0,
              control("Run doctor", in: "vm-header") != nil else { return nil as Bool? }
        return true
    }
    try read("Isolation checks passed")
    try read("GiB on this Mac")
    try noListeners()
    try capture("")
    try press("See all in Logs")
    try readLog("egress denied")

    if environment["HOMEBOX_VERIFY_BUSY_LOGS"] == "1" {
        let count = 2_400
        let log = URL(fileURLWithPath: scratch).appendingPathComponent("boxes/\(name)/run/vmnet.log")
        let descriptor = open(log.path, O_WRONLY | O_APPEND)
        guard descriptor >= 0 else {
            throw VerificationError("Could not append benchmark entries to the scratch VM's network log.")
        }
        let file = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
        defer { try? file.close() }
        let time = ISO8601DateFormatter().string(from: Date())
        let label = "Desktop verification \(UUID().uuidString.prefix(8)) entry"
        let lines = (1...count).map {
            "{\"time\":\"\(time)\",\"level\":\"INFO\",\"msg\":\"\(label) \($0)\"}\n"
        }
        try file.write(contentsOf: Data(lines.joined().utf8))
        try readLog("\(label) \(count)")
        let exposed = scope("log-output").map(childCount) ?? 0
        guard exposed >= count else {
            throw VerificationError("Expected at least \(count) log children, got \(exposed).")
        }
        print("Busy log: \(count) streamed entries, \(exposed) direct AX children")
        try capture("busy")
        let start = ProcessInfo.processInfo.systemUptime
        try press("Guest", in: "log-toolbar")
        print(String(format: "Busy log Guest control lookup and press: %.3f s",
                     ProcessInfo.processInfo.systemUptime - start))
    } else {
        try capture("network")
        try press("Guest", in: "log-toolbar")
    }

    let input = try waitFor("Unit text field") {
        guard let toolbar = scope("log-toolbar") else { return nil as AXUIElement? }
        return find(toolbar) {
            attribute($0, kAXRoleAttribute) == kAXTextFieldRole &&
                attribute($0, kAXEnabledAttribute) == true ? $0 : nil
        }
    }
    guard AXUIElementSetAttributeValue(input, kAXFocusedAttribute as CFString, kCFBooleanTrue) == .success else {
        throw VerificationError("Could not focus the guest unit filter.")
    }
    let unit = "ssh.service"
    let unicode = Array(unit.utf16)
    guard let keyDown = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true),
          let keyUp = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false) else {
        throw VerificationError("Could not create input events.")
    }
    keyDown.keyboardSetUnicodeString(stringLength: unicode.count, unicodeString: unicode)
    keyDown.postToPid(app.processIdentifier)
    keyUp.postToPid(app.processIdentifier)
    _ = try waitFor("guest unit field containing ssh.service") {
        attribute(input, kAXValueAttribute) as String? == unit ? true : nil
    }
    print("AX set: Unit = ssh.service")
    try press("Apply", in: "log-toolbar")
    _ = try waitFor("a filtered guest journal SSH session") { scratchSSH(unit: unit) ? true : nil }
    try readLog("Server listening")
    try capture("guest")
    try press("Follow", in: "log-toolbar")
    _ = try waitFor("SSH to close when Follow is unchecked") { !scratchSSH() ? true : nil }
    try press("Follow", in: "log-toolbar")
    _ = try waitFor("SSH to reopen") { scratchSSH() ? true : nil }
    try press("Overview", in: "vm-tabs")
    _ = try waitFor("SSH to close after leaving Logs") { !scratchSSH() ? true : nil }
    print("pgrep: guest SSH closed after toggling Follow and leaving Logs")
    try press("Logs", in: "vm-tabs")
    _ = try waitFor("SSH to reopen before close") { scratchSSH() ? true : nil }
    try closeWindow()
    _ = try waitFor("SSH to close when the window closes") { !scratchSSH() ? true : nil }
    print("pgrep: guest SSH closed with the window")
}

setbuf(stdout, nil)
do {
    let start = ProcessInfo.processInfo.systemUptime
    try verify(bundle: CommandLine.arguments[1], scratch: CommandLine.arguments[2],
               screenshot: CommandLine.arguments[3],
               mode: CommandLine.arguments.count > 4 ? CommandLine.arguments[4] : "normal",
               name: CommandLine.arguments.count > 5 ? CommandLine.arguments[5] : nil)
    print(String(format: "Total verification time: %.2f s", ProcessInfo.processInfo.systemUptime - start))
} catch {
    FileHandle.standardError.write(Data("\(error.localizedDescription)\n".utf8))
    exit(1)
}
