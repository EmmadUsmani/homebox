import AppKit
import ApplicationServices

struct Failure: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

func waitFor<T>(_ description: String, seconds: TimeInterval = 15, _ read: () -> T?) throws -> T {
    let deadline = Date().addingTimeInterval(seconds)
    repeat {
        if let result = read() { return result }
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

func verifyVMLabel(_ label: String, name: String, remote: Bool) -> Bool {
    let remoteLabel = label.hasPrefix(name + " · ")
    if remote { return remoteLabel }
    return label == name || label.hasPrefix(name + " ") && !remoteLabel
}

func command(_ path: String, _ arguments: [String], environment: [String: String]? = nil) throws -> (Int32, String) {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: path)
    process.arguments = arguments
    process.environment = environment
    let stdout = Pipe()
    process.standardOutput = stdout
    try process.run()
    let data = stdout.fileHandleForReading.readDataToEndOfFile()
    process.waitUntilExit()
    return (process.terminationStatus, String(decoding: data, as: UTF8.self))
}

func verify(bundle: String, state: String, screenshot: String, mode: String, scenarios: [String], flood: Bool) throws {
    guard AXIsProcessTrusted() else { throw Failure(message: "Accessibility permission required") }
    let url = URL(fileURLWithPath: bundle).resolvingSymlinksInPath()
    func running() -> NSRunningApplication? {
        NSRunningApplication.runningApplications(withBundleIdentifier: "dev.homebox.desktop").first {
            $0.bundleURL?.resolvingSymlinksInPath() == url
        }
    }
    guard running() == nil else { throw Failure(message: "This app copy is already running") }
    let home = NSHomeDirectory()
    let empty = mode == "empty"
    let path = empty ? "/usr/bin:/bin:/usr/sbin:/sbin" : ProcessInfo.processInfo.environment["PATH"] ?? "/usr/bin:/bin"
    var args = ["-n", "--env", "HOMEBOX_HOME=\(state)", "--env", "PATH=\(path)"]
    if empty {
        args += ["--env", "SSH_AUTH_SOCK="]
    } else if let socket = ProcessInfo.processInfo.environment["SSH_AUTH_SOCK"] {
        args += ["--env", "SSH_AUTH_SOCK=\(socket)"]
    }
    args += [url.path]
    let (status, _) = try command("/usr/bin/open", args, environment: empty ? ["HOME": home] : nil)
    guard status == 0 else { throw Failure(message: "open failed (\(status))") }
    defer {
        if let app = running() {
            app.terminate()
            if (try? waitFor("application exit", seconds: 5, { kill(app.processIdentifier, 0) == -1 && errno == ESRCH ? true : nil })) == nil {
                kill(app.processIdentifier, SIGKILL)
                _ = try? waitFor("application exit after force quit", seconds: 3) {
                    kill(app.processIdentifier, 0) == -1 && errno == ESRCH ? true : nil
                }
            }
        }
    }
    let app = try waitFor("app launch") { running() }
    app.activate()
    let pid = app.processIdentifier
    let root = AXUIElementCreateApplication(pid)
    let window: AXUIElement = try waitFor("AX window") {
        (attribute(root, kAXWindowsAttribute) as [AXUIElement]?)?.first
    }
    let web = try waitFor("web area") {
        descendants(window).first { attribute($0, kAXRoleAttribute) == "AXWebArea" }
    }

    func press(_ label: String) throws {
        let button = try waitFor("button \(label)") {
            descendants(web).first { element in
                let title: String? = attribute(element, kAXTitleAttribute)
                let description: String? = attribute(element, kAXDescriptionAttribute)
                return (title == label || description == label) &&
                    attribute(element, kAXRoleAttribute) == "AXButton"
            }
        }
        guard AXUIElementPerformAction(button, kAXPressAction as CFString) == .success else {
            throw Failure(message: "AXPress failed for \(label)")
        }
    }

    func selectVM(_ name: String, remote: Bool) throws {
        let row = try waitFor("sidebar row for \(name)") {
            descendants(web).first { element in
                guard attribute(element, kAXRoleAttribute) as String? == "AXButton" else { return false }
                let labels: [String] = [kAXTitleAttribute, kAXDescriptionAttribute].compactMap {
                    attribute(element, $0)
                }
                return labels.contains { label in
                    verifyVMLabel(label, name: name, remote: remote)
                }
            }
        }
        guard AXUIElementPerformAction(row, kAXPressAction as CFString) == .success else {
            throw Failure(message: "Could not select VM \(name)")
        }
    }

    func accessibleText() -> [String] {
        descendants(web).flatMap { element in
            [kAXValueAttribute, kAXTitleAttribute, kAXDescriptionAttribute].compactMap {
                attribute(element, $0) as String?
            }
        }
    }

    func see(_ marker: String, seconds: TimeInterval = 15) throws {
        _ = try waitFor(marker, seconds: seconds) {
            accessibleText().first { $0.contains(marker) }
        }
        print("AX read: \(marker)")
    }

    func key(_ code: CGKeyCode, flags: CGEventFlags = []) {
        for down in [true, false] {
            let event = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: down)
            event?.flags = flags
            event?.postToPid(pid)
        }
    }

    func terminalInput() -> AXUIElement? {
        descendants(web).first {
            attribute($0, kAXRoleAttribute) == "AXTextArea"
        }
    }

    func focusTerminal() {
        _ = app.activate()
        _ = AXUIElementPerformAction(window, kAXRaiseAction as CFString)
        if let input = terminalInput() {
            _ = AXUIElementSetAttributeValue(input, kAXFocusedAttribute as CFString, true as CFTypeRef)
        }
    }

    let clipboard = NSPasteboard.general
    let originalClipboard = clipboard.pasteboardItems?.map { item in
        item.types.compactMap { type in item.data(forType: type).map { (type, $0) } }
    } ?? []
    var lastPaste: Int?
    defer {
        if clipboard.changeCount == lastPaste {
            clipboard.clearContents()
            let items = originalClipboard.map { fields in
                let item = NSPasteboardItem()
                for (type, data) in fields { item.setData(data, forType: type) }
                return item
            }
            if !items.isEmpty { clipboard.writeObjects(items) }
        }
    }

    func enter(_ text: String) {
        focusTerminal()
        clipboard.clearContents()
        clipboard.setString(text, forType: .string)
        lastPaste = clipboard.changeCount
        key(9, flags: .maskCommand)
        Thread.sleep(forTimeInterval: 0.5)
        key(36)
    }

    func readSize(_ tag: String) throws -> String {
        enter("printf '%s' '\(tag)'; stty size")
        return try waitFor("PTY size") {
            for text in accessibleText() {
                if let match = text.range(of: "\(tag)[0-9]+ [0-9]+", options: .regularExpression) {
                    return String(text[match].dropFirst(tag.count))
                }
            }
            return nil
        }
    }

    func sockets() throws -> Int {
        let (_, output) = try command("/usr/sbin/lsof", ["-a", "-p", String(pid), "-nP", "-iTCP", "-sTCP:ESTABLISHED", "-Fn"])
        return output.split(separator: "\n").filter { $0.hasPrefix("n") }.count
    }

    func rss() -> Int {
        let (_, output) = (try? command("/bin/ps", ["-o", "rss=", "-p", String(pid)])) ?? (1, "")
        return Int(output.trimmingCharacters(in: .whitespacesAndNewlines)) ?? 0
    }

    let (listenerStatus, listenerOutput) = try command("/usr/sbin/lsof", ["-a", "-p", String(pid), "-nP", "-iTCP", "-sTCP:LISTEN"])
    guard listenerStatus == 1 && listenerOutput.isEmpty else { throw Failure(message: "App opened a TCP listener") }
    let (_, environment) = try command("/bin/ps", ["-wwE", "-p", String(pid), "-o", "command="])
    print("Launch \(mode), requested PATH observed: \(environment.contains("PATH=\(path) "))")
    func capture(_ destination: String) throws {
        let windowID = try waitFor("window ID") {
            (CGWindowListCopyWindowInfo(.optionOnScreenOnly, kCGNullWindowID) as? [[String: Any]])?.first {
                $0[kCGWindowOwnerPID as String] as? pid_t == pid &&
                    $0[kCGWindowLayer as String] as? Int == 0
            }?[kCGWindowNumber as String] as? Int
        }
        let (status, _) = try command("/usr/sbin/screencapture", ["-x", "-o", "-l", String(windowID), destination])
        guard status == 0 else { throw Failure(message: "Screenshot capture failed") }
    }

    for scenario in scenarios {
        let parts = scenario.split(separator: "=", maxSplits: 1).map(String.init)
        guard parts.count == 2, ["local", "remote", "wrong-local", "wrong-remote"].contains(parts[0]) else {
            throw Failure(message: "Use local=NAME, remote=ALIAS, wrong-local=NAME or wrong-remote=ALIAS")
        }
        let kind = parts[0]
        let vm = parts[1]
        let local = kind.hasSuffix("local")
        try selectVM(vm, remote: !local)
        try press("Open terminal")
        if kind.hasPrefix("wrong-") {
            try see("Host key changed")
            try see(local ? "Open SSH host key settings" : "box repin \(vm)")
            guard !accessibleText().contains("Reconnect") else { throw Failure(message: "Host key warning offers reconnect") }
            try capture(screenshot + "-key-changed.png")
            continue
        }
        let shellSetup = local ? "" : "unset HISTFILE; "
        try see("Terminal connected")
        enter(shellSetup + "echo hb-$((6*7))")
        try see("hb-42")
        let before = try readSize("before-size:")
        var size = CGSize(width: 950, height: 620)
        guard let value = AXValueCreate(.cgSize, &size),
              AXUIElementSetAttributeValue(window, kAXSizeAttribute as CFString, value) == .success else {
            throw Failure(message: "AX set window size failed")
        }
        Thread.sleep(forTimeInterval: 0.5)
        let after = try readSize("after-size:")
        print("PTY size \(before) → \(after)")
        guard before != after else { throw Failure(message: "PTY size did not change") }

        let baseline = rss()
        var peak = baseline
        if flood && local {
            enter("yes | head -c 200000000; echo; echo hb-flood-$((6*7))")
        }
        let firstInput = try waitFor("first terminal input", terminalInput)
        try press("New terminal session")
        let secondInput = try waitFor("second terminal input") {
            terminalInput().flatMap { CFEqual($0, firstInput) ? nil : $0 }
        }
        try see("Terminal connected")
        enter(shellSetup + "echo hb-second-$((6*7))")
        try see("hb-second-42")
        guard try sockets() == 1 else { throw Failure(message: "Two tabs did not share one TCP transport") }
        try press("Close session 2")
        _ = try waitFor("first terminal input after closing the second tab") {
            terminalInput().flatMap { CFEqual($0, secondInput) ? nil : $0 }
        }
        if flood && local {
            let start = Date()
            _ = try waitFor("flood completion", seconds: 240) {
                peak = max(peak, rss())
                return accessibleText().first { $0.contains("hb-flood-42") }
            }
            print("Flood completed after \(Int(Date().timeIntervalSince(start))) s post-tab-check, app RSS \(baseline) → peak \(peak) KiB")
        }
        Thread.sleep(forTimeInterval: 0.5)
        try capture(screenshot + (local ? "-local.png" : "-remote.png"))
        focusTerminal()
        for code: CGKeyCode in [14, 7, 34, 17, 36] { key(code) }
        try see("Session ended")
        _ = try waitFor("SSH transport close") { (try? sockets()) == 0 ? true : nil }
        // Restore the size before testing the next VM.
        size = CGSize(width: 1150, height: 760)
        if let value = AXValueCreate(.cgSize, &size) {
            _ = AXUIElementSetAttributeValue(window, kAXSizeAttribute as CFString, value)
        }
    }

}

let args = CommandLine.arguments
do {
    guard args.count >= 6 else {
        throw Failure(message: "Usage: swift verify-terminal.swift APP STATE SCREENSHOT normal|empty SCENARIO [SCENARIO...] [--flood]")
    }
    try verify(bundle: args[1], state: args[2], screenshot: args[3], mode: args[4], scenarios: args.dropFirst(5).filter { $0 != "--flood" }, flood: args.contains("--flood"))
} catch {
    FileHandle.standardError.write(Data("\(error.localizedDescription)\n".utf8))
    exit(1)
}
