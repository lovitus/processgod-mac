import AppKit
import Foundation
import ServiceManagement

enum ServiceSelection: String, CaseIterable, Identifiable {
    case user, system
    var id: String { rawValue }
}

@MainActor
final class ServiceController {
    static let userPlist = "com.lovitus.processgod.mac.guardian.user.plist"
    static let systemPlist = "com.lovitus.processgod.mac.guardian.system.plist"

    let userService = SMAppService.agent(plistName: userPlist)
    let systemService = SMAppService.daemon(plistName: systemPlist)

    private var pendingLegacyMigrations: [ServiceSelection: String] = [:]

    var hasPendingStartupMigration: Bool { pendingLegacyMigrations[.user] != nil }

    var userSocket: String {
        FileManager.default.homeDirectoryForCurrentUser
            .appending(path: "Library/Application Support/ProcessGodMac/run/user.sock").path
    }
    let systemSocket = "/var/run/processgod-mac/system.sock"

    func ensureDefaultUserService() throws {
        try backupLegacyConfig()
        guard systemService.status != .enabled else { return }
        guard userService.status == .notRegistered else { return }
        try beginLegacyMigration(.user)
        do {
            try userService.register()
        } catch {
            try? restoreLegacyMigration(.user)
            throw error
        }
    }

    func activeSelection() -> ServiceSelection {
        Self.activeSelection(systemEnabled: systemService.status == .enabled)
    }

    static func activeSelection(systemEnabled: Bool) -> ServiceSelection { systemEnabled ? .system : .user }

    func finishStartupMigration() throws {
        try finalizeLegacyMigration(.user)
    }

    static func shouldFinalizeLegacyMigration(hasPendingMigration: Bool, legacyPlistExists: Bool) -> Bool {
        hasPendingMigration || legacyPlistExists
    }

    func rollbackStartupMigration() async {
        if userService.status != .notRegistered { try? await unregister(userService) }
        try? restoreLegacyMigration(.user)
    }

    // The user agent remains active until the system daemon is healthy and owns
    // the imported configuration, avoiding a task outage during approval.
    func prepareSystemService() async throws -> Bool {
        guard currentUserIsAdmin else {
            throw RPCErrorPayload(code: "admin_required", message: "System mode requires an administrator account")
        }
        try backupLegacyConfig()
        if systemService.status == .notRegistered {
            try beginLegacyMigration(.system)
            do {
                try systemService.register()
            } catch {
                try? restoreLegacyMigration(.system)
                throw error
            }
        }
        if systemService.status == .requiresApproval {
            // Approval can be deferred indefinitely; keep the legacy daemon as
            // the pre-login rollback service until the new daemon is enabled.
            try? restoreLegacyMigration(.system)
            SMAppService.openSystemSettingsLoginItems()
            return false
        }
        if systemService.status == .enabled && pendingLegacyMigrations[.system] == nil {
            try beginLegacyMigration(.system)
        }
        return systemService.status == .enabled
    }

    func finishSystemSwitch() async throws {
        try finalizeLegacyMigration(.system)
        if userService.status != .notRegistered { try await unregister(userService) }
    }

    func rollbackSystemSwitch() async {
        if systemService.status != .notRegistered { try? await unregister(systemService) }
        try? restoreLegacyMigration(.system)
    }

    func prepareUserService() throws {
        if userService.status == .notRegistered {
            try beginLegacyMigration(.user)
            do {
                try userService.register()
            } catch {
                try? restoreLegacyMigration(.user)
                throw error
            }
        }
    }

    func finishUserSwitch() async throws {
        try finalizeLegacyMigration(.user)
        if systemService.status != .notRegistered { try await unregister(systemService) }
    }

    func rollbackUserSwitch() async {
        if userService.status != .notRegistered { try? await unregister(userService) }
        try? restoreLegacyMigration(.user)
    }

    func openApprovalSettings() {
        SMAppService.openSystemSettingsLoginItems()
    }

    func legacySystemConfig() throws -> JSONValue? {
        guard let path = legacySystemConfigPath(), FileManager.default.fileExists(atPath: path) else { return nil }
        let data = try Data(contentsOf: URL(fileURLWithPath: path))
        return try JSONDecoder().decode(JSONValue.self, from: data)
    }

    private func unregister(_ service: SMAppService) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            service.unregister { error in
                if let error { continuation.resume(throwing: error) }
                else { continuation.resume(returning: ()) }
            }
        }
    }

    private var currentUserIsAdmin: Bool {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/id")
        process.arguments = ["-Gn"]
        let pipe = Pipe()
        process.standardOutput = pipe
        do {
            try process.run()
            process.waitUntilExit()
            let groups = String(decoding: pipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
            return process.terminationStatus == 0 && groups.split(whereSeparator: { $0.isWhitespace }).contains("admin")
        } catch {
            return false
        }
    }

    private func backupLegacyConfig() throws {
        let root = FileManager.default.homeDirectoryForCurrentUser.appending(path: "Library/Application Support/ProcessGodMac")
        let source = root.appending(path: "config.json")
        let backup = root.appending(path: "config.json.pre-native")
        guard FileManager.default.fileExists(atPath: source.path), !FileManager.default.fileExists(atPath: backup.path) else { return }
        try FileManager.default.copyItem(at: source, to: backup)
    }

    private func legacyPlistPath(_ selection: ServiceSelection) -> String {
        switch selection {
        case .user:
            FileManager.default.homeDirectoryForCurrentUser
                .appending(path: "Library/LaunchAgents/com.lovitus.processgod.mac.plist").path
        case .system:
            "/Library/LaunchDaemons/com.lovitus.processgod.mac.plist"
        }
    }

    private func beginLegacyMigration(_ selection: ServiceSelection) throws {
        let path = legacyPlistPath(selection)
        guard FileManager.default.fileExists(atPath: path) else { return }
        if selection == .system { try backupLegacySystemConfig() }
        pendingLegacyMigrations[selection] = path
        if selection == .system {
            try runAdministratorHelper(["legacy-service", "stop", "--system"], allowFailure: true)
        } else {
            try runHelper(["legacy-service", "stop"], allowFailure: true)
        }
    }

    private func legacySystemConfigPath() -> String? {
        let plistPath = legacyPlistPath(.system)
        guard let data = FileManager.default.contents(atPath: plistPath),
              let plist = try? PropertyListSerialization.propertyList(from: data, format: nil) as? [String: Any] else { return nil }
        let environment = plist["EnvironmentVariables"] as? [String: String]
        let root = environment?["PROCESSGOD_HOME"] ?? plist["WorkingDirectory"] as? String
        guard let root, !root.isEmpty else { return nil }
        return URL(fileURLWithPath: root, isDirectory: true).appending(path: "config.json").path
    }

    private func backupLegacySystemConfig() throws {
        guard let source = legacySystemConfigPath(), FileManager.default.fileExists(atPath: source) else { return }
        let backup = source + ".pre-native"
        guard !FileManager.default.fileExists(atPath: backup) else { return }
        do {
            try FileManager.default.copyItem(atPath: source, toPath: backup)
        } catch {
            try runAdministratorCommand("/bin/cp -p \(shellQuote(source)) \(shellQuote(backup))")
        }
    }

    private func finalizeLegacyMigration(_ selection: ServiceSelection) throws {
        let pendingPath = pendingLegacyMigrations[selection]
        let path = pendingPath ?? legacyPlistPath(selection)
        let legacyExists = FileManager.default.fileExists(atPath: path)
        guard Self.shouldFinalizeLegacyMigration(hasPendingMigration: pendingPath != nil, legacyPlistExists: legacyExists) else {
            pendingLegacyMigrations.removeValue(forKey: selection)
            return
        }
        if selection == .system {
            try runAdministratorHelper(["legacy-service", "stop", "--system"], allowFailure: true)
            try runAdministratorCommand("/bin/rm -f \(shellQuote(path))")
        } else {
            try runHelper(["legacy-service", "stop"], allowFailure: true)
            if FileManager.default.fileExists(atPath: path) {
                try FileManager.default.removeItem(atPath: path)
            }
        }
        pendingLegacyMigrations.removeValue(forKey: selection)
    }

    private func restoreLegacyMigration(_ selection: ServiceSelection) throws {
        guard pendingLegacyMigrations[selection] != nil else { return }
        if selection == .system {
            try runAdministratorHelper(["legacy-service", "start", "--system"], allowFailure: false)
        } else {
            try runHelper(["legacy-service", "start"], allowFailure: false)
        }
        pendingLegacyMigrations.removeValue(forKey: selection)
    }

    private func runHelper(_ arguments: [String], allowFailure: Bool) throws {
        guard let helper = Bundle.main.url(forAuxiliaryExecutable: "processgod-mac") else {
            throw RPCErrorPayload(code: "helper_missing", message: "Bundled Go helper was not found")
        }
        let process = Process()
        process.executableURL = helper
        process.arguments = arguments
        try process.run()
        process.waitUntilExit()
        if !allowFailure && process.terminationStatus != 0 {
            throw RPCErrorPayload(code: "migration_failed", message: "Legacy service migration failed")
        }
    }

    private func runAdministratorHelper(_ arguments: [String], allowFailure: Bool) throws {
        guard let helper = Bundle.main.url(forAuxiliaryExecutable: "processgod-mac") else {
            throw RPCErrorPayload(code: "helper_missing", message: "Bundled Go helper was not found")
        }
        let command = ([shellQuote(helper.path)] + arguments.map(shellQuote)).joined(separator: " ")
        do {
            try runAdministratorCommand(command)
        } catch where allowFailure {
            return
        }
    }

    private func runAdministratorCommand(_ command: String) throws {
        let source = "do shell script \(appleScriptQuote(command)) with administrator privileges"
        var error: NSDictionary?
        NSAppleScript(source: source)?.executeAndReturnError(&error)
        if let error {
            throw NSError(domain: "ProcessGodMigration", code: 1, userInfo: error as? [String: Any])
        }
    }

    private func shellQuote(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\"'\"'") + "'"
    }

    private func appleScriptQuote(_ value: String) -> String {
        "\"" + value.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"") + "\""
    }
}
