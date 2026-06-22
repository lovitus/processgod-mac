import Foundation

enum WireJSON {
    static func decoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let value = try container.decode(String.self)
            if let date = parseGoTimestamp(value) {
                return date
            }
            throw DecodingError.dataCorruptedError(
                in: container,
                debugDescription: "Expected RFC3339/RFC3339Nano timestamp"
            )
        }
        return decoder
    }

    private static func parseGoTimestamp(_ value: String) -> Date? {
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = fractional.date(from: value) { return date }

        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        return plain.date(from: value)
    }
}

enum ProcessMode: String, Codable, CaseIterable, Identifiable, Sendable {
    case guardMode = "guard"
    case once
    case cronRun
    case cronRestart

    var id: String { rawValue }
    var titleKey: String {
        switch self {
        case .guardMode: "mode.guard"
        case .once: "mode.once"
        case .cronRun: "mode.cronRun"
        case .cronRestart: "mode.cronRestart"
        }
    }
    var usesCron: Bool { self == .cronRun || self == .cronRestart }
}

struct ProcessDefinition: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String
    var command: String
    var arguments: String = ""
    var workingDirectory: String = ""
    var environment: [String: String] = [:]
    var mode: ProcessMode = .guardMode
    var cronExpression: String = ""
    var enabled: Bool = true

    init(
        id: String,
        name: String,
        command: String,
        arguments: String = "",
        workingDirectory: String = "",
        environment: [String: String] = [:],
        mode: ProcessMode = .guardMode,
        cronExpression: String = "",
        enabled: Bool = true
    ) {
        self.id = id
        self.name = name
        self.command = command
        self.arguments = arguments
        self.workingDirectory = workingDirectory
        self.environment = environment
        self.mode = mode
        self.cronExpression = cronExpression
        self.enabled = enabled
    }

    private enum CodingKeys: String, CodingKey {
        case id, name, command, arguments, workingDirectory, environment, mode, cronExpression, enabled
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        id = try values.decode(String.self, forKey: .id)
        name = try values.decode(String.self, forKey: .name)
        command = try values.decode(String.self, forKey: .command)
        arguments = try values.decodeIfPresent(String.self, forKey: .arguments) ?? ""
        workingDirectory = try values.decodeIfPresent(String.self, forKey: .workingDirectory) ?? ""
        environment = try values.decodeIfPresent([String: String].self, forKey: .environment) ?? [:]
        mode = try values.decodeIfPresent(ProcessMode.self, forKey: .mode) ?? .guardMode
        cronExpression = try values.decodeIfPresent(String.self, forKey: .cronExpression) ?? ""
        enabled = try values.decodeIfPresent(Bool.self, forKey: .enabled) ?? false
    }

    static var empty: ProcessDefinition {
        .init(id: "", name: "", command: "")
    }
}

struct ConfigSnapshot: Codable, Sendable {
    var schemaVersion: Int
    var revision: UInt64
    var pathEnv: String
    var guardianPaused: Bool
    var processes: [ProcessDefinition]
}

enum ProcessEditorValidation {
    static func canSave(_ process: ProcessDefinition, cronValid: Bool) -> Bool {
        !process.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
            !process.command.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
            (!process.mode.usesCron || (!process.cronExpression.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && cronValid))
    }
}

enum ProcessState: String, Codable, Sendable {
    case disabled, starting, running, waiting, completed, error
}

struct ProcessRuntime: Codable, Identifiable, Sendable {
    var id: String
    var name: String
    var state: ProcessState
    var pid: Int?
    var lastStart: Date?
    var lastExit: Date?
    var errorCode: String?
    var error: String?

    init(id: String, name: String, state: ProcessState, pid: Int? = nil, lastStart: Date? = nil, lastExit: Date? = nil, errorCode: String? = nil, error: String? = nil) {
        self.id = id
        self.name = name
        self.state = state
        self.pid = pid
        self.lastStart = lastStart
        self.lastExit = lastExit
        self.errorCode = errorCode
        self.error = error
    }
}

struct RuntimeSnapshot: Codable, Sendable {
    var mode: String
    var paused: Bool
    var healthy: Bool
    var error: String?
    var processes: [ProcessRuntime]
}

struct PopoverSummary: Equatable, Sendable {
    let running: Int
    let enabled: Int

    init(running: Int, enabled: Int) {
        self.running = running
        self.enabled = enabled
    }

    init(config: ConfigSnapshot, runtime: RuntimeSnapshot) {
        running = runtime.processes.filter { $0.state == .running }.count
        enabled = config.processes.filter(\.enabled).count
    }
}

struct LogEntry: Codable, Identifiable, Sendable {
    var sequence: Int64
    var timestamp: Date
    var source: String
    var category: String
    var text: String
    var id: Int64 { sequence }
}

struct LogBuffer: Codable, Sendable {
    var capacity: Int
    var kept: Int
    var entries: [LogEntry]
}

struct LogSnapshot: Codable, Sendable {
    var processID: String
    var totalSeen: Int64
    var lineMaxBytes: Int
    var errorWarning: LogBuffer
    var standardOther: LogBuffer
}

struct DaemonEvent: Codable, Sendable {
    var sequence: UInt64?
    var type: String
    var processID: String?
    var revision: UInt64?
}

struct EventEnvelope: Codable, Sendable {
    var protocolVersion: Int
    var event: DaemonEvent
}

struct RPCErrorPayload: Codable, Error, LocalizedError, Sendable {
    var code: String
    var message: String
    var details: [String: JSONValue]?
    var errorDescription: String? { message }
}

enum JSONValue: Codable, Sendable {
    case string(String), integer(Int64), unsigned(UInt64), number(Double), bool(Bool), object([String: JSONValue]), array([JSONValue]), null

    init(from decoder: Decoder) throws {
        let value = try decoder.singleValueContainer()
        if value.decodeNil() { self = .null }
        else if let item = try? value.decode(Bool.self) { self = .bool(item) }
        else if let item = try? value.decode(Int64.self) { self = .integer(item) }
        else if let item = try? value.decode(UInt64.self) { self = .unsigned(item) }
        else if let item = try? value.decode(Double.self) { self = .number(item) }
        else if let item = try? value.decode(String.self) { self = .string(item) }
        else if let item = try? value.decode([String: JSONValue].self) { self = .object(item) }
        else { self = .array(try value.decode([JSONValue].self)) }
    }

    func encode(to encoder: Encoder) throws {
        var value = encoder.singleValueContainer()
        switch self {
        case .string(let item): try value.encode(item)
        case .integer(let item): try value.encode(item)
        case .unsigned(let item): try value.encode(item)
        case .number(let item): try value.encode(item)
        case .bool(let item): try value.encode(item)
        case .object(let item): try value.encode(item)
        case .array(let item): try value.encode(item)
        case .null: try value.encodeNil()
        }
    }
}
