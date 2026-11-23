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

func waitFor<T>(_ description: String, _ read: () -> T?) throws -> T {
    let deadline = Date().addingTimeInterval(15)
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

func descendants(_ element: AXUIElement) -> [AXUIElement] {
    let children: [AXUIElement] = attribute(element, kAXChildrenAttribute) ?? []
    return [element] + children.flatMap(descendants)
}


func verify() throws {
    let arguments = CommandLine.arguments
    let bundle = URL(fileURLWithPath: arguments[1]).resolvingSymlinksInPath().path
    let mode = arguments[6]
    let output = arguments[7]
    guard AXIsProcessTrusted() else { throw VerificationError("Accessibility permission is required.") }
    func matchingApps() -> [NSRunningApplication] {
        NSRunningApplication.runningApplications(withBundleIdentifier: "dev.homebox.desktop").filter {
            $0.bundleURL?.resolvingSymlinksInPath().path == bundle
        }
    }
    guard matchingApps().isEmpty else { throw VerificationError("Close this copy of homebox before verification.") }
    let environment = ProcessInfo.processInfo.environment
    var openArgs = ["-n", "--env", "HOMEBOX_HOME=\(arguments[2])",
                    "--env", "HOMEBOX_DESKTOP_SSH_CONFIG=\(arguments[3])",
                    "--env", "HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS=\(arguments[4])",
                    "--env", "HOMEBOX_DESKTOP_SETTINGS=\(arguments[5])"]
    if mode == "minimal" {
        openArgs += ["--env", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", bundle]
        try run("/usr/bin/env", ["-i", "HOME=\(environment["HOME"] ?? "")", "/usr/bin/open"] + openArgs)
    } else {
        openArgs += ["--env", "PATH=\(environment["PATH"] ?? "/usr/bin:/bin")", bundle]
        try run("/usr/bin/open", openArgs)
    }
    defer {
        for app in matchingApps() {
            app.terminate()
            if (try? waitFor("homebox to quit", { app.isTerminated ? true : nil })) == nil {
                app.forceTerminate()
            }
        }
    }
    let app = try waitFor("homebox to launch") { matchingApps().first }
    let ps = Process()
    let stdout = Pipe()
    ps.executableURL = URL(fileURLWithPath: "/bin/ps")
    ps.arguments = ["-wwE", "-p", String(app.processIdentifier), "-o", "command="]
    ps.standardOutput = stdout
    try ps.run()
    let environmentLine = String(decoding: stdout.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
    ps.waitUntilExit()
    if mode == "minimal" && !environmentLine.contains("PATH=/usr/bin:/bin:/usr/sbin:/sbin") {
        throw VerificationError("The app did not launch with the requested minimal PATH.")
    }
    if mode == "minimal" { print("ps confirmed minimal PATH") }

    let lsof = Process()
    let listeners = Pipe()
    lsof.executableURL = URL(fileURLWithPath: "/usr/sbin/lsof")
    lsof.arguments = ["-nP", "-a", "-p", String(app.processIdentifier), "-iTCP", "-sTCP:LISTEN"]
    lsof.standardOutput = listeners
    try lsof.run()
    let sockets = listeners.fileHandleForReading.readDataToEndOfFile()
    lsof.waitUntilExit()
    guard lsof.terminationStatus == 1 && sockets.isEmpty else {
        throw VerificationError("The app unexpectedly opened a TCP listener.")
    }
    print("lsof confirmed no app TCP listeners")
    let root = AXUIElementCreateApplication(app.processIdentifier)
    let webArea: AXUIElement = try waitFor("WKWebView accessibility tree") {
        descendants(root).first { attribute($0, kAXRoleAttribute) == "AXWebArea" }
    }
    let windowID: Int = try waitFor("window ID") {
        let windows = CGWindowListCopyWindowInfo(.optionOnScreenOnly, kCGNullWindowID) as? [[String: Any]] ?? []
        return windows.first {
            $0[kCGWindowOwnerPID as String] as? pid_t == app.processIdentifier &&
                $0[kCGWindowLayer as String] as? Int == 0
        }?[kCGWindowNumber as String] as? Int
    }
    func labels(_ element: AXUIElement) -> [String] {
        [kAXValueAttribute, kAXTitleAttribute, kAXDescriptionAttribute].compactMap { attribute(element, $0) }
    }
    func press(_ title: String, id: String? = nil) throws {
        let button: AXUIElement = try waitFor(title) {
            descendants(webArea).first { (id != nil && attribute($0, "AXDOMIdentifier") == id) ||
                (attribute($0, kAXRoleAttribute) == kAXButtonRole && labels($0).contains(title)) }
        }
        guard AXUIElementPerformAction(button, kAXPressAction as CFString) == .success else {
            throw VerificationError("AXPress failed for \(title).")
        }
        print("AX press: \(title)")
    }
    func read(_ expected: [String]) throws {
        _ = try waitFor(expected.joined(separator: ", ")) {
            let text = descendants(webArea).flatMap(labels)
            return expected.allSatisfy { text.contains($0) } ? true : nil
        }
        print("AX read: \(expected.joined(separator: ", "))")
    }
    func save(_ alias: String) throws {
        let path = "\(output)/\(alias).png"
        try run("/usr/sbin/screencapture", ["-x", "-o", "-l", String(windowID), path])
        print("Screenshot: \(path)")
    }
    for (alias, expected) in [
        ("main", ["Guest load", "Memory", "Disk", "Servers", "Open terminal"]),
        ("unresolved", ["Unreachable", "Remote connection hints"]),
        ("stopped", ["VM not running", "Remote connection hints"]),
        ("broken", ["Connection broken", "Remote connection hints"]),
        ("wrong-pin", ["Host key changed: not connecting", "box repin wrong-pin"]),
    ] {
        try press("Select remote VM \(alias)", id: "remote-row-\(alias)")
        try read(expected)
        try save(alias)
        if alias == "main" {
            _ = try waitFor("python3 attribution for port 8000") {
                descendants(webArea).first { row in
                    labels(row).contains("Server port 8000") &&
                    descendants(row).flatMap(labels).contains { $0.hasPrefix("python3 (pid ") }
                }
            }
            let text = descendants(webArea).flatMap(labels)
            guard !text.contains("Run doctor"), !text.contains("Stop"), !text.contains("Start") else {
                throw VerificationError("Remote Overview showed a local VM action.")
            }
            print("AX read: port 8000 attributed to python3. Remote actions present.")
        }
    }
    let pasteboard = NSPasteboard.general
    let previousItems = (pasteboard.pasteboardItems ?? []).map { original in
        let item = NSPasteboardItem()
        for type in original.types {
            if let data = original.data(forType: type) { item.setData(data, forType: type) }
        }
        return item
    }
    defer {
        if pasteboard.string(forType: .string) == "box repin wrong-pin" {
            pasteboard.clearContents()
            pasteboard.writeObjects(previousItems)
        }
    }
    try press("Copy command")
    try read(["Command copied"])
    guard NSPasteboard.general.string(forType: .string) == "box repin wrong-pin" else {
        throw VerificationError("The copied repin command did not match.")
    }
    try press("Check again")
    try read(["Host key changed: not connecting"])
    try press("Add a host")
    let input: AXUIElement = try waitFor("SSH alias field") {
        descendants(webArea).first { attribute($0, kAXRoleAttribute) == kAXTextFieldRole }
    }
    AXUIElementSetAttributeValue(input, kAXFocusedAttribute as CFString, kCFBooleanTrue)
    try save("add-host")
    let alias = Array("added-fixture".utf16)
    let keyDown = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true)
    keyDown?.keyboardSetUnicodeString(stringLength: alias.count, unicodeString: alias)
    keyDown?.postToPid(app.processIdentifier)
    _ = try waitFor("typed SSH alias") { labels(input).contains("added-fixture") ? true : nil }
    try press("Add host")
    _ = try waitFor("Added alias in sidebar") {
        descendants(webArea).first { attribute($0, "AXDOMIdentifier") == "remote-row-added-fixture" }
    }
    print("AX read: Added alias in sidebar")
}

do {
    try verify()
} catch {
    FileHandle.standardError.write(Data("\(error.localizedDescription)\n".utf8))
    exit(1)
}
