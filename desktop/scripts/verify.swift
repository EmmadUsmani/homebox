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

func verify(bundle: String, scratch: String, screenshot: String) throws {
    guard AXIsProcessTrusted() else {
        throw VerificationError("Grant Accessibility to the terminal or agent running this script.")
    }
    let bundleURL = URL(fileURLWithPath: bundle).resolvingSymlinksInPath().path
    let identifier = "dev.homebox.desktop"
    func matchingApps() -> [NSRunningApplication] {
        NSRunningApplication.runningApplications(withBundleIdentifier: identifier).filter {
            $0.bundleURL?.resolvingSymlinksInPath().path == bundleURL
        }
    }
    guard matchingApps().isEmpty else {
        throw VerificationError("Close this copy of homebox before running verification.")
    }
    let path = ProcessInfo.processInfo.environment["PATH"] ?? "/usr/bin:/bin"
    try run("/usr/bin/open", ["-n", "--env", "PATH=\(path)", "--env",
                              "HOMEBOX_HOME=\(scratch)", bundle])
    defer {
        for app in matchingApps() {
            app.terminate()
            if (try? waitFor("homebox to quit", { app.isTerminated ? true : nil })) == nil {
                app.forceTerminate()
            }
        }
    }
    let app = try waitFor("homebox to launch") { matchingApps().first }
    let root = AXUIElementCreateApplication(app.processIdentifier)
    let window: AXUIElement = try waitFor("homebox window") {
        let windows: [AXUIElement] = attribute(root, kAXWindowsAttribute) ?? []
        return windows.first { attribute($0, kAXTitleAttribute) == "homebox" }
    }
    let webArea: AXUIElement = try waitFor("WKWebView accessibility tree") {
        descendants(window).first { attribute($0, kAXRoleAttribute) == "AXWebArea" }
    }

    func verifyStatus(_ number: Int) throws {
        let status = "Status loaded (\(number))"
        let data: String = try waitFor("\(status) and JSON in WKWebView") {
            let values: [String] = descendants(webArea).compactMap {
                attribute($0, kAXValueAttribute)
            }
            return values.contains(status) && values.contains("[]") ? "[]" : nil
        }
        print("AX read: \(status), JSON \(data)")
    }

    try verifyStatus(1)
    let button: AXUIElement = try waitFor("Refresh status button") {
        descendants(webArea).first {
            attribute($0, kAXRoleAttribute) == kAXButtonRole &&
                (attribute($0, kAXTitleAttribute) == "Refresh status" ||
                 attribute($0, kAXDescriptionAttribute) == "Refresh status")
        }
    }
    guard AXUIElementPerformAction(button, kAXPressAction as CFString) == .success else {
        throw VerificationError("AXPress failed for Refresh status.")
    }
    print("AX press: Refresh status")
    try verifyStatus(2)
    let windowID: Int = try waitFor("window ID") {
        let windows = CGWindowListCopyWindowInfo(.optionOnScreenOnly, kCGNullWindowID)
            as? [[String: Any]] ?? []
        return windows.first {
            $0[kCGWindowOwnerPID as String] as? pid_t == app.processIdentifier &&
                $0[kCGWindowLayer as String] as? Int == 0
        }?[kCGWindowNumber as String] as? Int
    }
    try run("/usr/sbin/screencapture", ["-x", "-o", "-l", String(windowID), screenshot])
    print("Screenshot: \(screenshot)")
}

do {
    try verify(bundle: CommandLine.arguments[1], scratch: CommandLine.arguments[2],
               screenshot: CommandLine.arguments[3])
} catch {
    FileHandle.standardError.write(Data("\(error.localizedDescription)\n".utf8))
    exit(1)
}
