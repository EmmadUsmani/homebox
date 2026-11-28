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

func verify(bundle: String, screenshots: String, index: Int) throws {
    guard AXIsProcessTrusted() else {
        throw VerificationError("Grant Accessibility to the terminal or agent running this script.")
    }
    guard (1...99).contains(index) else {
        throw VerificationError("Choose an unused box index from 1 to 99 for the disposable VM.")
    }
    let files = FileManager.default
    let name = "lifecycle"
    let scratch = files.homeDirectoryForCurrentUser.appendingPathComponent(".hb-v-\(UUID().uuidString.prefix(6))")
    try files.createDirectory(at: scratch, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
    try files.createDirectory(atPath: screenshots, withIntermediateDirectories: true)
    let bundleURL = URL(fileURLWithPath: bundle).resolvingSymlinksInPath()
    let cli = bundleURL.appendingPathComponent("Contents/MacOS/box").path
    let vmDir = scratch.appendingPathComponent("lima/\(name)")
    let boxDir = scratch.appendingPathComponent("boxes/\(name)")

    func matchingApps() -> [NSRunningApplication] {
        NSRunningApplication.runningApplications(withBundleIdentifier: "dev.homebox.desktop").filter {
            $0.bundleURL?.resolvingSymlinksInPath() == bundleURL
        }
    }
    guard matchingApps().isEmpty else {
        throw VerificationError("Close this build of homebox before running verification.")
    }
    defer {
        for app in matchingApps() {
            app.terminate()
            if (try? waitFor("homebox to quit", seconds: 3, {
                kill(app.processIdentifier, 0) == -1 && errno == ESRCH ? true : nil
            })) == nil {
                kill(app.processIdentifier, SIGKILL)
                _ = try? waitFor("homebox to quit after force quit", seconds: 3) {
                    kill(app.processIdentifier, 0) == -1 && errno == ESRCH ? true : nil
                }
            }
        }
        if files.fileExists(atPath: boxDir.appendingPathComponent("box.json").path) || files.fileExists(atPath: vmDir.path) {
            print("Stopping disposable VM after failure. Its state remains at \(scratch.path).")
            try? run("/usr/bin/env", ["HOMEBOX_HOME=\(scratch.path)", "PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin",
                                      cli, "stop", name])
        } else {
            try? files.removeItem(at: scratch)
        }
    }
    try run("/usr/bin/open", [
        "-n", "--env", "HOMEBOX_HOME=\(scratch.path)", "--env", "HOMEBOX_FIRST_INDEX=\(index)",
        "--env", "HOMEBOX_DESKTOP_SSH_CONFIG=\(scratch.path)/remote-config",
        "--env", "HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS=\(scratch.path)/remote-pins",
        "--env", "HOMEBOX_DESKTOP_SETTINGS=\(scratch.path)/desktop-settings",
        bundleURL.path,
    ])
    let app = try waitFor("homebox launch") { matchingApps().first }
    let root = AXUIElementCreateApplication(app.processIdentifier)
    let window: AXUIElement = try waitFor("homebox window") {
        let windows: [AXUIElement] = attribute(root, kAXWindowsAttribute) ?? []
        return windows.first
    }
    let web: AXUIElement = try waitFor("web view") {
        find(window) { attribute($0, kAXRoleAttribute) == "AXWebArea" ? $0 : nil }
    }
    let windowID: Int = try waitFor("window ID") {
        let windows = CGWindowListCopyWindowInfo(.optionOnScreenOnly, kCGNullWindowID) as? [[String: Any]] ?? []
        return windows.first {
            $0[kCGWindowOwnerPID as String] as? pid_t == app.processIdentifier &&
                $0[kCGWindowLayer as String] as? Int == 0
        }?[kCGWindowNumber as String] as? Int
    }

    func text(_ element: AXUIElement) -> [String] {
        [kAXValueAttribute, kAXTitleAttribute, kAXDescriptionAttribute].compactMap { attribute(element, $0) }
    }
    func identified(_ id: String) -> AXUIElement? {
        find(web) { identifier($0) == id ? $0 : nil }
    }
    func button(_ title: String, in group: String? = nil) -> AXUIElement? {
        guard let container = group == nil ? web : group.flatMap(identified) else { return nil }
        return find(container) {
            attribute($0, kAXRoleAttribute) == kAXButtonRole && text($0).contains(title) &&
                attribute($0, kAXEnabledAttribute) != false ? $0 : nil
        }
    }
    func press(_ title: String, in group: String? = nil) throws {
        let control = try waitFor("button \(title)", seconds: 120) { button(title, in: group) }
        guard AXUIElementPerformAction(control, kAXPressAction as CFString) == .success else {
            throw VerificationError("Could not press \(title).")
        }
        print("Pressed \(title)")
    }
    func read(_ phrase: String, seconds: TimeInterval = 30) throws {
        _ = try waitFor(phrase, seconds: seconds) {
            find(web) { text($0).first { $0.contains(phrase) } }
        }
        print("Saw \(phrase)")
    }
    func fill(_ id: String, _ value: String, key: CGKeyCode = 0) throws {
        let field = try waitFor(id) { identified(id) }
        guard AXUIElementSetAttributeValue(field, kAXFocusedAttribute as CFString, kCFBooleanTrue) == .success,
              let selectDown = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true),
              let selectUp = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false),
              let typeDown = CGEvent(keyboardEventSource: nil, virtualKey: key, keyDown: true),
              let typeUp = CGEvent(keyboardEventSource: nil, virtualKey: key, keyDown: false) else {
            throw VerificationError("Could not focus or type into \(id).")
        }
        _ = try waitFor("focus on \(id)") {
            attribute(field, kAXFocusedAttribute) == true ? true : nil
        }
        selectDown.flags = .maskCommand
        selectUp.flags = .maskCommand
        selectDown.postToPid(app.processIdentifier)
        selectUp.postToPid(app.processIdentifier)
        let unicode = Array(value.utf16)
        typeDown.keyboardSetUnicodeString(stringLength: unicode.count, unicodeString: unicode)
        typeDown.postToPid(app.processIdentifier)
        typeUp.postToPid(app.processIdentifier)
        _ = try waitFor("\(value) in \(id)", seconds: 5) {
            let actual: AnyObject? = attribute(field, kAXValueAttribute)
            if let number = actual as? NSNumber, let expected = Double(value) {
                return number.doubleValue == expected ? true : nil
            }
            return (actual as? String) == value ? true : nil
        }
    }
    func capture(_ name: String) throws {
        let path = URL(fileURLWithPath: screenshots).appendingPathComponent(name + ".png").path
        try run("/usr/sbin/screencapture", ["-x", "-o", "-l", String(windowID), path])
        print("Screenshot: \(path)")
    }

    print("Disposable state: \(scratch.path)")
    try read("No VMs on this Mac")
    try capture("first-run")
    try press("Set up this Mac")
    try read("Host setup complete")
    try press("New VM")
    _ = try waitFor("default CPU, memory and disk fields") { identified("new-vm-DiskGiB") }
    try capture("new-vm")
    try fill("new-vm-name", name)
    do {
        try fill("new-vm-MemoryGiB", "4", key: 21)
    } catch {
        if let field = identified("new-vm-MemoryGiB") {
            let value: AnyObject? = attribute(field, kAXValueAttribute)
            print("Memory field exposes \(String(describing: value))")
        }
        try capture("form-input")
        throw error
    }
    try press("Create VM")
    try read("Creating VM…")
    _ = try waitFor("live provisioning log", seconds: 120) { identified("lifecycle-log") }
    try capture("provisioning")
    try read("VM created", seconds: 1800)
    let record = try Data(contentsOf: boxDir.appendingPathComponent("box.json"))
    guard let saved = try JSONSerialization.jsonObject(with: record) as? [String: Any],
          saved["memory_gib"] as? Int == 4, saved["provisioned"] as? Bool == true else {
        throw VerificationError("The created VM did not save the expected size and provisioned state.")
    }
    try press("Run doctor", in: "vm-header")
    try read("Isolation checks passed", seconds: 120)
    try capture("running-doctor")
    try press("Stop", in: "vm-header")
    _ = try waitFor("Start on stopped VM", seconds: 120) { button("Start", in: "vm-header") }
    try read("stopped")
    try capture("stopped")
    try press("Start", in: "vm-header")
    _ = try waitFor("Stop on restarted VM", seconds: 120) { button("Stop", in: "vm-header") }
    try press("More", in: "vm-header")
    try press("Re-run provisioning")
    try read("Provisioning complete", seconds: 1800)
    try capture("reprovisioned")
    try press("More", in: "vm-header")
    try press("Delete VM…")
    try read("This permanently deletes the VM and its disks.")
    guard button("Delete VM") == nil else {
        throw VerificationError("Delete was available before typing the VM name.")
    }
    try fill("delete-vm-name", name)
    try capture("delete-confirmation")
    try press("Delete VM")
    try read("VM deleted", seconds: 120)
    try read("No VMs on this Mac")
    guard !files.fileExists(atPath: vmDir.path), !files.fileExists(atPath: boxDir.appendingPathComponent("box.json").path) else {
        throw VerificationError("Deletion left the Lima instance or VM record behind.")
    }
    try capture("deleted")
}

setbuf(stdout, nil)
do {
    let args = CommandLine.arguments
    guard args.count == 4, let index = Int(args[3]) else {
        throw VerificationError("Usage: swift verify-lifecycle.swift HOMEBOX_APP SCREENSHOT_DIR UNUSED_BOX_INDEX")
    }
    try verify(bundle: args[1], screenshots: args[2], index: index)
} catch {
    FileHandle.standardError.write(Data("\(error.localizedDescription)\n".utf8))
    exit(1)
}
