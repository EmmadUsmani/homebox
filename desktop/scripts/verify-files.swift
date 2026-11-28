import AppKit
import ApplicationServices
import CryptoKit

struct Failure: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

func waitFor<T>(_ description: String, seconds: TimeInterval = 15, _ read: () throws -> T?) throws -> T {
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

func verify(bundle: String, state: String, screenshots: String, empty: Bool, scenarios: [String]) throws {
    guard AXIsProcessTrusted() else { throw Failure(message: "Accessibility permission required") }
    let url = URL(fileURLWithPath: bundle).resolvingSymlinksInPath()
    let alreadyRunning = NSRunningApplication.runningApplications(withBundleIdentifier: "dev.homebox.desktop").contains {
        $0.bundleURL?.resolvingSymlinksInPath() == url
    }
    guard !alreadyRunning else { throw Failure(message: "This app copy is already running") }
    let minimumPATH = "/usr/bin:/bin:/usr/sbin:/sbin"
    try FileManager.default.createDirectory(atPath: screenshots, withIntermediateDirectories: true)
    let config = NSWorkspace.OpenConfiguration()
    var appEnvironment = [
        "HOMEBOX_HOME": state,
        "HOMEBOX_DESKTOP_SETTINGS": "\(screenshots)/settings.json",
        "PATH": empty ? minimumPATH : ProcessInfo.processInfo.environment["PATH"] ?? minimumPATH,
    ]
    appEnvironment["SSH_AUTH_SOCK"] = empty ? "" : ProcessInfo.processInfo.environment["SSH_AUTH_SOCK"]
    if scenarios.allSatisfy({ $0.hasPrefix("local=") }) {
        let sshConfig = screenshots + "/files-ssh-config"
        let knownHosts = screenshots + "/files-known-hosts"
        try "".write(toFile: sshConfig, atomically: true, encoding: .utf8)
        try "".write(toFile: knownHosts, atomically: true, encoding: .utf8)
        appEnvironment["HOMEBOX_DESKTOP_SSH_CONFIG"] = sshConfig
        appEnvironment["HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS"] = knownHosts
    }
    config.environment = appEnvironment
    config.createsNewApplicationInstance = true
    var launch: Result<NSRunningApplication, Error>?
    NSWorkspace.shared.openApplication(at: url, configuration: config) { app, error in
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
    let window: AXUIElement = try waitFor("AX window") {
        (attribute(root, kAXWindowsAttribute) as [AXUIElement]?)?.first
    }
    let web = try waitFor("web area") {
        descendants(window).first { attribute($0, kAXRoleAttribute) == "AXWebArea" }
    }
    let (listenerStatus, listenerOutput) = try command("/usr/sbin/lsof", [
        "-a", "-nP", "-iTCP", "-sTCP:LISTEN", "-p", String(pid),
    ])
    guard listenerStatus == 1 && listenerOutput.isEmpty else { throw Failure(message: "App has a TCP listener") }
    if empty {
        let (_, environment) = try command("/bin/ps", ["-wwE", "-p", String(pid), "-o", "command="])
        guard environment.contains("PATH=\(minimumPATH) ") else { throw Failure(message: "App PATH was not minimal") }
    }
    print("App launch and no TCP listener verified (\(empty ? "empty" : "normal"))")

    func activate() throws {
        if NSWorkspace.shared.frontmostApplication?.processIdentifier != pid {
            guard kill(pid, 0) == 0 else { throw Failure(message: "Files app exited before activation") }
            _ = app.unhide()
            _ = try command("/usr/bin/open", ["-a", url.path])
        }
        _ = try waitFor("frontmost Files app (focus lost)", seconds: 3) {
            NSWorkspace.shared.frontmostApplication?.processIdentifier == pid ? true : nil
        }
    }

    func raiseWindow() throws {
        try activate()
        _ = AXUIElementPerformAction(window, kAXRaiseAction as CFString)
    }

    func labels(_ element: AXUIElement) -> [String] {
        [kAXTitleAttribute, kAXDescriptionAttribute, kAXValueAttribute].compactMap { attribute(element, $0) as String? }
    }

    func press(_ description: String, in container: AXUIElement? = nil, matching: (String) -> Bool) throws {
        let control = try waitFor(description) {
            let elements = container.map { target in
                let windows: [AXUIElement] = attribute(target, kAXWindowsAttribute) ?? []
                let focused: AXUIElement? = attribute(target, kAXFocusedWindowAttribute)
                let visible = (focused.map { [$0] } ?? []) + windows
                return visible.flatMap(descendants) + descendants(target)
            } ?? descendants(web)
            return elements.first {
                let role: String? = attribute($0, kAXRoleAttribute)
                let enabled: Bool = attribute($0, kAXEnabledAttribute) ?? true
                return enabled && ["AXButton", "AXRadioButton", "AXCheckBox"].contains(role ?? "")
                    && labels($0).contains(where: matching)
            }
        }
        guard AXUIElementPerformAction(control, kAXPressAction as CFString) == .success else {
            throw Failure(message: "AXPress failed for \(description)")
        }
    }

    func button(_ title: String, in container: AXUIElement? = nil) throws {
        try press(title, in: container) { $0 == title }
    }

    func see(_ marker: String, seconds: TimeInterval = 15) throws {
        _ = try waitFor(marker, seconds: seconds) {
            descendants(web).flatMap(labels).first { $0.contains(marker) }
        }
        print("AX read: \(marker)")
    }

    func key(_ code: CGKeyCode, _ flags: CGEventFlags = [], filePicker: Bool = false) throws {
        if NSWorkspace.shared.frontmostApplication?.processIdentifier != pid {
            if filePicker { throw Failure(message: "File picker focus lost before keyboard input") }
            try raiseWindow()
        }
        for down in [true, false] {
            let event = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: down)
            event?.flags = flags
            if filePicker {
                // The system file panel handles shortcuts through the WindowServer.
                event?.post(tap: .cghidEventTap)
            } else {
                event?.postToPid(pid)
            }
            Thread.sleep(forTimeInterval: 0.05)
        }
    }

    let clipboard = NSPasteboard.general
    let originalClipboard = clipboard.pasteboardItems?.map { item in
        item.types.compactMap { type in item.data(forType: type).map { (type, $0) } }
    } ?? []
    var lastClipboard: Int?
    defer {
        if clipboard.changeCount == lastClipboard {
            clipboard.clearContents()
            let items = originalClipboard.map { fields in
                let item = NSPasteboardItem()
                for (type, data) in fields { item.setData(data, forType: type) }
                return item
            }
            if !items.isEmpty { clipboard.writeObjects(items) }
        }
    }

    func paste(_ text: String, filePicker: Bool = false) throws {
        clipboard.clearContents()
        clipboard.setString(text, forType: .string)
        lastClipboard = clipboard.changeCount
        try key(9, .maskCommand, filePicker: filePicker)
        Thread.sleep(forTimeInterval: 0.3)
    }

    func focus(_ label: String, role: String) throws {
        try raiseWindow()
        let field = try waitFor(label) {
            descendants(web).first { attribute($0, kAXRoleAttribute) == role && labels($0).contains(label) }
        }
        guard AXUIElementSetAttributeValue(field, kAXFocusedAttribute as CFString, true as CFTypeRef) == .success else {
            throw Failure(message: "Could not focus \(label)")
        }
        _ = try waitFor("keyboard focus in \(label)", seconds: 3) {
            let focused: Bool? = attribute(field, kAXFocusedAttribute)
            return focused == true ? true : nil
        }
    }

    func capture(_ name: String) throws {
        try raiseWindow()
        Thread.sleep(forTimeInterval: 0.35)
        _ = try waitFor("screenshot of \(name)", seconds: 5) { () -> Bool? in
            let windows = CGWindowListCopyWindowInfo(.optionOnScreenOnly, kCGNullWindowID) as? [[String: Any]] ?? []
            guard let windowID = windows.first(where: {
                $0[kCGWindowOwnerPID as String] as? pid_t == pid && $0[kCGWindowLayer as String] as? Int == 0
            })?[kCGWindowNumber as String] as? Int else {
                try raiseWindow()
                return nil
            }
            let (status, _) = try command("/usr/sbin/screencapture", ["-x", "-o", "-l", String(windowID), screenshots + "/" + name + ".png"])
            return status == 0 ? true : nil
        }
    }

    func chooseFolder(_ folder: String) throws {
        try activate()
        let previous: AXUIElement? = attribute(root, kAXFocusedUIElementAttribute)
        try key(5, [.maskCommand, .maskShift], filePicker: true)
        _ = try waitFor("Go to Folder text field", seconds: 4) {
            guard NSWorkspace.shared.frontmostApplication?.processIdentifier == pid else {
                throw Failure(message: "File picker focus lost while opening Go to Folder")
            }
            guard let field: AXUIElement = attribute(root, kAXFocusedUIElementAttribute),
                  previous.map({ !CFEqual($0, field) }) ?? true,
                  let role: String = attribute(field, kAXRoleAttribute),
                  ["AXTextField", "AXComboBox"].contains(role) else { return nil as AXUIElement? }
            return field
        }
        try paste(folder, filePicker: true)
        try key(36, filePicker: true)
    }

    let box = url.appendingPathComponent("Contents/MacOS/box").path
    for scenario in scenarios {
        let parts = scenario.split(separator: "=", maxSplits: 1).map(String.init)
        guard parts.count == 2, ["local", "remote"].contains(parts[0]) else {
            throw Failure(message: "Use local=NAME or remote=ALIAS")
        }
        let (kind, vm) = (parts[0], parts[1])
        let local = kind == "local"
        let fixture = "/tmp/hb-files-test.txt"
        let filename = "hb-upload-" + UUID().uuidString.lowercased() + ".json"
        func ssh(_ script: String, check: Bool = true) throws -> String {
            let env = ProcessInfo.processInfo.environment.merging(["HOMEBOX_HOME": state]) { _, value in value }
            let result = try local
                ? command(box, ["ssh", vm, script], environment: env)
                : command("/usr/bin/ssh", ["-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", vm, script])
            if check && result.0 != 0 { throw Failure(message: "\(kind) SSH command exited \(result.0)") }
            return result.1
        }
        _ = try ssh("set -C; printf 'original\n' > " + fixture)
        defer { _ = try? ssh("rm -f " + fixture + " /tmp/" + filename) }
        let scratch = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent(UUID().uuidString)
        let upload = scratch.appendingPathComponent("upload")
        let download = scratch.appendingPathComponent("download")
        for folder in [upload, download] {
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        }
        defer { try? FileManager.default.removeItem(at: scratch) }
        let input = upload.appendingPathComponent(filename)
        try Data("{\"transfer fixture\": }\n".utf8).write(to: input)

        try press("sidebar row \(kind)") { verifyVMLabel($0, name: vm, remote: !local) }
        try button("Files")
        try button("Show hidden files")
        try see(".profile")
        try button("Show hidden files")
        try capture(kind + "-home")

        try focus("VM folder path", role: "AXTextField")
        try key(0, .maskCommand)
        try paste("/tmp")
        try key(36)
        try button("hb-files-test.txt")
        try see("original")
        try capture(kind + "-editor")

        try focus("File editor", role: "AXTextArea")
        try key(0, .maskCommand)
        try paste("saved from Monaco\n")
        try see("● edited")
        try key(6, .maskCommand)
        try see("original")
        try key(0, .maskCommand)
        try paste("saved from Monaco\n")
        try key(0, .maskCommand)
        try key(8, .maskCommand)
        _ = try waitFor("Monaco copy") {
            clipboard.changeCount != lastClipboard && clipboard.string(forType: .string) == "saved from Monaco\n" ? true : nil
        }
        lastClipboard = clipboard.changeCount
        try key(1, .maskCommand)
        _ = try waitFor("saved bytes over plain SSH") {
            (try? ssh("cat " + fixture)) == "saved from Monaco\n" ? true : nil
        }
        try capture(kind + "-saved")
        try focus("File editor", role: "AXTextArea")
        try key(3, .maskCommand)
        try see("Find")
        try capture(kind + "-find")
        try key(53)

        _ = try ssh("printf 'external append\n' >> " + fixture)
        try focus("File editor", role: "AXTextArea")
        try key(0, .maskCommand)
        try paste("unsaved local draft\n")
        try key(1, .maskCommand)
        try see("Saving is blocked")
        try capture(kind + "-conflict")
        let external = try ssh("cat " + fixture)
        guard external == "saved from Monaco\nexternal append\n" else { throw Failure(message: "Conflict overwrote the VM file") }
        try button("Reload and compare")
        try see("Current VM file")
        try see("external append")
        try capture(kind + "-compare")
        try raiseWindow()
        try button("Home")
        do {
            try button("Discard changes", in: root)
        } catch {
            try? capture(kind + "-discard-timeout")
            throw error
        }

        try focus("VM folder path", role: "AXTextField")
        try key(0, .maskCommand)
        try paste("/tmp")
        try key(36)
        try button("Upload")
        _ = try waitFor("upload picker") { descendants(window).first { attribute($0, kAXRoleAttribute) == "AXSheet" } }
        try chooseFolder(input.path)
        let openSheet = try waitFor("open sheet") { descendants(window).first { attribute($0, kAXRoleAttribute) == "AXSheet" } }
        try button("Open", in: openSheet)
        try button(filename)
        try see("transfer fixture")
        try focus("File editor", role: "AXTextArea")
        try key(100) // F8 shows the JSON worker's diagnostic.
        try see("Value expected")
        try capture(kind + "-worker")
        try key(53)
        try capture(kind + "-uploaded")
        try button("Download")
        _ = try waitFor("save dialog") { descendants(window).first { attribute($0, kAXRoleAttribute) == "AXSheet" } }
        try chooseFolder(download.path)
        let sheet = try waitFor("save sheet") { descendants(window).first { attribute($0, kAXRoleAttribute) == "AXSheet" } }
        try button("Save", in: sheet)
        let retrieved = download.appendingPathComponent(filename)
        let original = try Data(contentsOf: input)
        _ = try waitFor("matching downloaded bytes") {
            (try? Data(contentsOf: retrieved)) == original ? true : nil
        }
        let expected = SHA256.hash(data: original).map { String(format: "%02x", $0) }.joined()
        let remoteHash = try ssh("sha256sum /tmp/" + filename).split(separator: " ").first
        let downloadHash = SHA256.hash(data: try Data(contentsOf: retrieved))
        guard remoteHash.map(String.init) == expected, downloadHash == SHA256.hash(data: original) else {
            throw Failure(message: "Transfer checksum mismatch")
        }
        try capture(kind + "-downloaded")
        print("\(kind): save, find, undo, copy, paste, conflict, JSON worker and transfer checks passed")
    }
}

let args = CommandLine.arguments
do {
    guard args.count >= 6, ["normal", "empty"].contains(args[4]) else {
        throw Failure(message: "Usage: swift verify-files.swift APP STATE SCREENSHOTS normal|empty SCENARIO [SCENARIO...]")
    }
    try verify(bundle: args[1], state: args[2], screenshots: args[3], empty: args[4] == "empty", scenarios: Array(args.dropFirst(5)))
} catch {
    FileHandle.standardError.write(Data("\(error.localizedDescription)\n".utf8))
    exit(1)
}
