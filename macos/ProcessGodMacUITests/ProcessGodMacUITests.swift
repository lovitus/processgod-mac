import XCTest

@MainActor
final class ProcessGodMacUITests: XCTestCase {
    func testManagerWindowLaunches() {
        let app = XCUIApplication()
        app.launchArguments = ["--uitesting"]
        app.launch()
        let windowAppeared = app.windows.firstMatch.waitForExistence(timeout: 8)
        XCTAssertTrue(windowAppeared)
    }
}
