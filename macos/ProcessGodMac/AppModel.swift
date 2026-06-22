import Foundation
import Observation
import ServiceManagement

private struct RevisionParams: Codable, Sendable { let expectedRevision: UInt64 }
private struct ProcessParams: Codable, Sendable { let expectedRevision: UInt64; let process: ProcessDefinition }
private struct UpdateProcessParams: Codable, Sendable { let expectedRevision: UInt64; let id: String; let process: ProcessDefinition }
private struct IDRevisionParams: Codable, Sendable { let expectedRevision: UInt64; let id: String }
private struct EnabledParams: Codable, Sendable { let expectedRevision: UInt64; let id: String; let enabled: Bool }
private struct IDParams: Codable, Sendable { let id: String }
private struct LogParams: Codable, Sendable { let id: String; let lines: Int }
private struct SettingsParams: Codable, Sendable { let expectedRevision: UInt64; let pathEnv: String }
private struct CronParams: Codable, Sendable { let expression: String }
private struct ValidResult: Codable, Sendable { let valid: Bool }
private struct BootstrapParams: Codable, Sendable { let config: JSONValue }
private struct ImportParams: Codable, Sendable { let expectedRevision: UInt64; let config: JSONValue }

@MainActor @Observable
final class AppModel {
    var config = ConfigSnapshot(schemaVersion: 2, revision: 0, pathEnv: "", guardianPaused: false, processes: [])
    var runtime = RuntimeSnapshot(mode: "user", paused: false, healthy: false, error: nil, processes: [])
    var connectionError: String?
    var isBusy = false
    var serviceSelection: ServiceSelection = .user
    var serviceRequiresApproval = false
    var logEventGeneration: UInt64 = 0
    var languageOverride: String {
        didSet { UserDefaults.standard.set(languageOverride, forKey: "languageOverride") }
    }

    let services: ServiceController
    private var client: DaemonClient
    private var eventTask: Task<Void, Never>?

    init() {
        let serviceController = ServiceController()
        languageOverride = UserDefaults.standard.string(forKey: "languageOverride") ?? "system"
        services = serviceController
        let selection = serviceController.activeSelection()
        serviceSelection = selection
        client = DaemonClient(socketPath: selection == .system ? serviceController.systemSocket : serviceController.userSocket)
    }

    var locale: Locale {
        switch languageOverride {
        case "en": Locale(identifier: "en")
        case "zh-Hans": Locale(identifier: "zh-Hans")
        default: .autoupdatingCurrent
        }
    }

    func localized(_ key: String) -> String {
        guard languageOverride != "system",
              let path = Bundle.main.path(forResource: languageOverride, ofType: "lproj"),
              let bundle = Bundle(path: path) else {
            return Bundle.main.localizedString(forKey: key, value: key, table: nil)
        }
        return bundle.localizedString(forKey: key, value: key, table: nil)
    }

    func start() async {
        do {
            try services.ensureDefaultUserService()
            serviceSelection = services.activeSelection()
            serviceRequiresApproval = services.systemService.status == .requiresApproval
            client = DaemonClient(socketPath: serviceSelection == .system ? services.systemSocket : services.userSocket)
            try await waitForDaemon(using: client)
            try await refresh()
            if serviceSelection == .user && services.hasPendingStartupMigration {
                guard runtime.healthy else {
                    throw RPCErrorPayload(code: "daemon_degraded", message: runtime.error ?? "Daemon configuration needs repair")
                }
                try services.finishStartupMigration()
            }
            startEvents()
        } catch let error as RPCErrorPayload {
            connectionError = localizedError(error)
            if services.hasPendingStartupMigration { await services.rollbackStartupMigration() }
        } catch {
            connectionError = error.localizedDescription
            if services.hasPendingStartupMigration { await services.rollbackStartupMigration() }
        }
    }

    func refresh() async throws {
        async let configValue: ConfigSnapshot = client.call("config.get")
        async let runtimeValue: RuntimeSnapshot = client.call("status.list")
        config = try await configValue
        runtime = try await runtimeValue
        connectionError = nil
    }

    @discardableResult
    func save(_ definition: ProcessDefinition, replacing originalID: String?) async -> Bool {
        await perform {
            if let originalID {
                self.config = try await self.client.call("process.update", params: UpdateProcessParams(expectedRevision: self.config.revision, id: originalID, process: definition))
            } else {
                self.config = try await self.client.call("process.create", params: ProcessParams(expectedRevision: self.config.revision, process: definition))
            }
            self.runtime = try await self.client.call("status.list")
        }
    }

    @discardableResult
    func delete(_ id: String) async -> Bool {
        await perform {
            self.config = try await self.client.call("process.delete", params: IDRevisionParams(expectedRevision: self.config.revision, id: id))
            self.runtime = try await self.client.call("status.list")
        }
    }

    func setEnabled(_ id: String, enabled: Bool) async {
        await perform {
            self.config = try await self.client.call("process.setEnabled", params: EnabledParams(expectedRevision: self.config.revision, id: id, enabled: enabled))
            self.runtime = try await self.client.call("status.list")
        }
    }

    func restart(_ id: String) async {
        await perform { self.runtime = try await self.client.call("process.restart", params: IDParams(id: id)) }
    }

    func setPaused(_ paused: Bool) async {
        await perform {
            let method = paused ? "guardian.pause" : "guardian.resume"
            self.config = try await self.client.call(method, params: RevisionParams(expectedRevision: self.config.revision))
            self.runtime = try await self.client.call("status.list")
        }
    }

    func updatePath(_ path: String) async {
        await perform { self.config = try await self.client.call("settings.update", params: SettingsParams(expectedRevision: self.config.revision, pathEnv: path)) }
    }

    func logs(for id: String) async throws -> LogSnapshot {
        try await client.call("logs.snapshot", params: LogParams(id: id, lines: 0))
    }

    func validateCron(_ expression: String) async -> Bool {
        do {
            let result: ValidResult = try await client.call("cron.validate", params: CronParams(expression: expression))
            return result.valid
        } catch { return false }
    }

    func switchService(to selection: ServiceSelection) async {
        guard selection != serviceSelection else { return }
        let previousClient = client
        let previousSelection = serviceSelection
        await perform {
            var rawConfig: JSONValue = try await previousClient.call("config.export")
            if selection == .system {
                if let legacyConfig = try self.services.legacySystemConfig() { rawConfig = legacyConfig }
                let enabled = try await self.services.prepareSystemService()
                self.serviceRequiresApproval = !enabled
                guard enabled else { return }
                let systemClient = DaemonClient(socketPath: self.services.systemSocket)
                try await self.waitForDaemon(using: systemClient)
                do {
                    let _: ConfigSnapshot = try await systemClient.call("system.bootstrap", params: BootstrapParams(config: rawConfig))
                } catch let error as RPCErrorPayload where error.code == "already_bootstrapped" {
                    let _: ConfigSnapshot = try await systemClient.call("config.import", params: ImportParams(expectedRevision: 0, config: rawConfig))
                }
                try await self.waitForDaemon(using: systemClient, requireHealthy: true)
                try await self.services.finishSystemSwitch()
                self.client = systemClient
            } else {
                try self.services.prepareUserService()
                let userClient = DaemonClient(socketPath: self.services.userSocket)
                try await self.waitForDaemon(using: userClient)
                let _: ConfigSnapshot = try await userClient.call("config.import", params: ImportParams(expectedRevision: 0, config: rawConfig))
                try await self.waitForDaemon(using: userClient, requireHealthy: true)
                try await self.services.finishUserSwitch()
                self.client = userClient
            }
            self.serviceSelection = selection
            try await self.refresh()
            self.startEvents()
        }
        if serviceSelection == previousSelection && selection != previousSelection && !serviceRequiresApproval {
            client = previousClient
            if selection == .system { await services.rollbackSystemSwitch() }
            else { await services.rollbackUserSwitch() }
            try? await refresh()
        }
    }

    private func waitForDaemon(using daemonClient: DaemonClient, requireHealthy: Bool = false) async throws {
        var lastError: Error?
        for delay in [200, 300, 500, 1_000, 2_000, 2_000] {
            do {
                let health: RuntimeSnapshot = try await daemonClient.call("system.health")
                if !requireHealthy || health.healthy { return }
                lastError = RPCErrorPayload(code: "daemon_degraded", message: health.error ?? "Daemon is degraded")
            } catch { lastError = error }
            try await Task.sleep(for: .milliseconds(delay))
        }
        throw lastError ?? RPCErrorPayload(code: "daemon_unavailable", message: "Daemon did not become ready")
    }

    private func startEvents() {
        eventTask?.cancel()
        eventTask = Task { [weak self] in
            guard let self else { return }
            let delays = [500, 1_000, 2_000, 5_000, 10_000]
            var attempt = 0
            while !Task.isCancelled {
                do {
                    for try await event in await self.client.events() {
                        switch event.type {
                        case "heartbeat": continue
                        case "logs.changed": self.logEventGeneration &+= 1
                        default: try await self.refresh()
                        }
                    }
                    attempt = 0
                } catch {
                    if let rpcError = error as? RPCErrorPayload { self.connectionError = self.localizedError(rpcError) }
                    else { self.connectionError = error.localizedDescription }
                    let delay = delays[min(attempt, delays.count - 1)]
                    attempt += 1
                    try? await Task.sleep(for: .milliseconds(delay))
                    try? await self.refresh()
                }
            }
        }
    }

    @discardableResult
    private func perform(_ operation: @escaping @MainActor () async throws -> Void) async -> Bool {
        isBusy = true
        defer { isBusy = false }
        do {
            try await operation()
            connectionError = nil
            return true
        } catch let error as RPCErrorPayload {
            connectionError = localizedError(error)
            if error.code == "revision_conflict" { try? await refresh() }
            return false
        } catch {
            connectionError = error.localizedDescription
            return false
        }
    }

    func localizedError(_ error: RPCErrorPayload) -> String {
        let key: String
        switch error.code {
        case "revision_conflict": key = "error.revisionConflict"
        case "admin_required": key = "error.adminRequired"
        case "permission_denied": key = "error.permissionDenied"
        case "not_bootstrapped": key = "error.notBootstrapped"
        case "invalid_cron": key = "error.invalidCron"
        case "duplicate_id": key = "error.duplicateID"
        case "not_found": key = "error.notFound"
        case "daemon_degraded": key = "error.daemonDegraded"
        case "daemon_unavailable": key = "error.daemonUnavailable"
        case "protocol_mismatch": key = "error.protocolMismatch"
        case "helper_missing": key = "error.helperMissing"
        case "migration_failed": key = "error.migrationFailed"
        case "invalid_response": key = "error.invalidResponse"
        default: return error.message
        }
        return localized(key)
    }
}
