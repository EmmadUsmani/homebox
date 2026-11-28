import OSLog
import SwiftUI

@main
struct SmokeApp: App {
    private let title = "Homebox iOS smoke"

    var body: some Scene {
        WindowGroup {
            Text(title)
                .font(.title)
                .onAppear {
                    Logger(subsystem: "dev.homebox.ios-smoke", category: "view")
                        .notice("\(title, privacy: .public)")
                }
        }
    }
}
