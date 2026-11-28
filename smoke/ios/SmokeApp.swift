import Foundation
import SwiftUI

@main
struct SmokeApp: App {
    private let title = "Homebox iOS smoke"

    var body: some Scene {
        WindowGroup {
            Text(title)
                .font(.title)
                .onAppear {
                    FileHandle.standardOutput.write(Data("\(title)\n".utf8))
                }
        }
    }
}
