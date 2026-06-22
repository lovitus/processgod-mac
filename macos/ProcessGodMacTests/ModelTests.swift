import XCTest
@testable import ProcessGodMac

private struct HelloFixture: Codable, Sendable {
    let protocolVersion: Int
    let version: String
    let mode: String
    let capabilities: [String]
}

@MainActor
final class ModelTests: XCTestCase {
    func testConfigFixtureMatchesGoContract() throws {
        let json = #"{"schemaVersion":2,"revision":7,"pathEnv":"/bin","guardianPaused":false,"processes":[{"id":"echo","name":"Echo","command":"/bin/echo","arguments":"hello","mode":"guard","enabled":true}]}"#
        let snapshot = try JSONDecoder().decode(ConfigSnapshot.self, from: Data(json.utf8))
        XCTAssertEqual(snapshot.revision, 7)
        XCTAssertEqual(snapshot.processes.first?.mode, .guardMode)
        XCTAssertEqual(snapshot.processes.first?.command, "/bin/echo")
    }

    func testAllModesHaveStableWireValues() {
        XCTAssertEqual(ProcessMode.allCases.map(\.rawValue), ["guard", "once", "cronRun", "cronRestart"])
    }

    func testStructuredLogFixture() throws {
        let json = #"{"processID":"echo","totalSeen":1,"lineMaxBytes":4096,"errorWarning":{"capacity":100,"kept":0,"entries":[]},"standardOther":{"capacity":20,"kept":1,"entries":[{"sequence":1,"timestamp":"2026-06-22T00:00:00Z","source":"stdout","category":"standardOther","text":"hello"}]}}"#
        let decoder = JSONDecoder(); decoder.dateDecodingStrategy = .iso8601
        let snapshot = try decoder.decode(LogSnapshot.self, from: Data(json.utf8))
        XCTAssertEqual(snapshot.standardOther.entries.first?.text, "hello")
        XCTAssertEqual(snapshot.errorWarning.capacity, 100)
    }

    func testProcessDefinitionAcceptsOmittedOptionalFields() throws {
        let json = #"{"id":"echo","name":"Echo","command":"echo","mode":"guard","enabled":true}"#
        let process = try JSONDecoder().decode(ProcessDefinition.self, from: Data(json.utf8))
        XCTAssertEqual(process.arguments, "")
        XCTAssertEqual(process.workingDirectory, "")
        XCTAssertEqual(process.environment, [:])
    }

    func testRawConfigKeepsLargeUnsignedRevision() throws {
        let json = #"{"revision":18446744073709551615,"items":[]}"#
        let raw = try JSONDecoder().decode(JSONValue.self, from: Data(json.utf8))
        let encoded = try JSONEncoder().encode(raw)
        let object = try JSONSerialization.jsonObject(with: encoded) as? [String: Any]
        XCTAssertEqual(object?["revision"] as? UInt64, UInt64.max)
    }

    func testRealGoDaemonUnixSocketRPC() async throws {
        guard let helper = Bundle.main.url(forAuxiliaryExecutable: "processgod-mac") else {
            return XCTFail("Go helper is missing from the test host app")
        }
        let root = URL(fileURLWithPath: "/tmp", isDirectory: true).appending(path: "pg-swift-\(getpid())")
        try? FileManager.default.removeItem(at: root)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }

        let socket = root.appending(path: "daemon.sock").path
        let process = Process()
        process.executableURL = helper
        process.arguments = ["daemon", "--scope", "user"]
        var environment = ProcessInfo.processInfo.environment
        environment["PROCESSGOD_HOME"] = root.path
        environment["PROCESSGOD_SOCKET"] = socket
        process.environment = environment
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        try process.run()
        defer {
            if process.isRunning { process.terminate() }
        }

        for _ in 0..<50 where !FileManager.default.fileExists(atPath: socket) {
            try await Task.sleep(for: .milliseconds(50))
        }
        XCTAssertTrue(FileManager.default.fileExists(atPath: socket))

        let client = DaemonClient(socketPath: socket)
        let hello: HelloFixture = try await client.call("system.hello")
        XCTAssertEqual(hello.protocolVersion, 1)
        XCTAssertEqual(hello.mode, "user")
        let config: ConfigSnapshot = try await client.call("config.get")
        XCTAssertEqual(config.schemaVersion, 2)

        do {
            let _: ConfigSnapshot = try await client.call("missing.method")
            XCTFail("Unknown RPC method unexpectedly succeeded")
        } catch let error as RPCErrorPayload {
            XCTAssertEqual(error.code, "method_not_found")
        }

        process.terminate()
        for _ in 0..<40 where process.isRunning {
            try await Task.sleep(for: .milliseconds(50))
        }
        XCTAssertFalse(process.isRunning)
    }

    func testEditorValidationRequiresCronAndCommand() {
        var process = ProcessDefinition(id: "job", name: "Job", command: "node", mode: .cronRun)
        XCTAssertFalse(ProcessEditorValidation.canSave(process, cronValid: true))
        process.cronExpression = "*/5 * * * *"
        XCTAssertTrue(ProcessEditorValidation.canSave(process, cronValid: true))
        XCTAssertFalse(ProcessEditorValidation.canSave(process, cronValid: false))
        process.command = " "
        XCTAssertFalse(ProcessEditorValidation.canSave(process, cronValid: true))
    }

    func testPopoverSummaryAndServiceDecision() {
        let processes = [
            ProcessDefinition(id: "a", name: "A", command: "a", enabled: true),
            ProcessDefinition(id: "b", name: "B", command: "b", enabled: false),
        ]
        let config = ConfigSnapshot(schemaVersion: 2, revision: 1, pathEnv: "/bin", guardianPaused: false, processes: processes)
        let runtime = RuntimeSnapshot(mode: "user", paused: false, healthy: true, error: nil, processes: [
            ProcessRuntime(id: "a", name: "A", state: .running),
            ProcessRuntime(id: "b", name: "B", state: .disabled),
        ])
        XCTAssertEqual(PopoverSummary(config: config, runtime: runtime), PopoverSummary(running: 1, enabled: 1))
        XCTAssertEqual(ServiceController.activeSelection(systemEnabled: false), .user)
        XCTAssertEqual(ServiceController.activeSelection(systemEnabled: true), .system)
    }

    func testErrorCodeLocalizationInBothLanguages() {
        let model = AppModel()
        let error = RPCErrorPayload(code: "admin_required", message: "fallback")
        model.languageOverride = "en"
        XCTAssertEqual(model.localizedError(error), "System mode requires an administrator account.")
        model.languageOverride = "zh-Hans"
        XCTAssertEqual(model.localizedError(error), "系统级模式需要管理员账户。")
    }
}
