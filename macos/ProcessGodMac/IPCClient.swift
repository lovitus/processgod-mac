import Foundation
import Network

private struct RPCRequest<Params: Encodable & Sendable>: Encodable, Sendable {
    let protocolVersion = 1
    let requestID: String
    let method: String
    let params: Params?
}

private struct RPCResponse<Result: Decodable>: Decodable {
    let protocolVersion: Int
    let requestID: String
    let ok: Bool
    let result: Result?
    let error: RPCErrorPayload?
}

private struct SubscriptionAck: Decodable, Sendable {
    let protocolVersion: Int
    let requestID: String
    let ok: Bool
    let error: RPCErrorPayload?
}

private struct EmptyParams: Codable, Sendable {}
private struct EmptyResult: Codable, Sendable {}
private final class DataBuffer: @unchecked Sendable { var data = Data() }

private final class OneShotContinuation<Value: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var finished = false
    private let continuation: CheckedContinuation<Value, Error>

    init(_ continuation: CheckedContinuation<Value, Error>) {
        self.continuation = continuation
    }

    func resume(with result: Result<Value, Error>) {
        lock.lock()
        guard !finished else {
            lock.unlock()
            return
        }
        finished = true
        lock.unlock()
        continuation.resume(with: result)
    }
}

actor DaemonClient {
    let socketPath: String
    private let queue = DispatchQueue(label: "com.lovitus.processgod.ipc")
    private let encoder = JSONEncoder()
    private let decoder = WireJSON.decoder()

    init(socketPath: String) {
        self.socketPath = socketPath
    }

    func call<Result: Decodable & Sendable>(_ method: String) async throws -> Result {
        try await call(method, params: Optional<EmptyParams>.none)
    }

    func call<Params: Encodable & Sendable, Result: Decodable & Sendable>(
        _ method: String,
        params: Params?
    ) async throws -> Result {
        let requestID = UUID().uuidString
        let request = RPCRequest(requestID: requestID, method: method, params: params)
        var data = try encoder.encode(request)
        data.append(0x0A)
        let responseData = try await exchange(data)
        let response = try decoder.decode(RPCResponse<Result>.self, from: responseData)
        guard response.protocolVersion == 1, response.requestID == requestID else {
            throw RPCErrorPayload(code: "invalid_response", message: "Daemon response identity does not match the request")
        }
        if let error = response.error { throw error }
        guard response.ok, let result = response.result else {
            throw RPCErrorPayload(code: "invalid_response", message: "Daemon returned no result")
        }
        return result
    }

    func callVoid<Params: Encodable & Sendable>(_ method: String, params: Params? = nil) async throws {
        let _: EmptyResult = try await call(method, params: params)
    }

    func events() -> AsyncThrowingStream<DaemonEvent, Error> {
        let subscriptionRequestID = UUID().uuidString
        let requestData: Data
        do {
            let request = RPCRequest<EmptyParams>(requestID: subscriptionRequestID, method: "events.subscribe", params: nil)
            var data = try encoder.encode(request)
            data.append(0x0A)
            requestData = data
        } catch {
            return AsyncThrowingStream { $0.finish(throwing: error) }
        }

        let path = socketPath
        let callbackQueue = queue
        return AsyncThrowingStream { continuation in
            let connection = NWConnection(to: .unix(path: path), using: .tcp)
            let buffer = DataBuffer()
            connection.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    connection.send(content: requestData, completion: .contentProcessed { error in
                        if let error { continuation.finish(throwing: error) }
                    })
                    Self.receiveLines(connection, buffer: buffer, continuation: continuation, expectedRequestID: subscriptionRequestID, skippedAck: false)
                case .failed(let error): continuation.finish(throwing: error)
                case .cancelled: continuation.finish()
                default: break
                }
            }
            continuation.onTermination = { _ in connection.cancel() }
            connection.start(queue: callbackQueue)
        }
    }

    private func exchange(_ data: Data) async throws -> Data {
        let connection = NWConnection(to: .unix(path: socketPath), using: .tcp)
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Data, Error>) in
                let oneShot = OneShotContinuation(continuation)
                let buffer = DataBuffer()
                connection.stateUpdateHandler = { state in
                    switch state {
                    case .ready:
                        connection.send(content: data, completion: .contentProcessed { error in
                            if let error {
                                oneShot.resume(with: .failure(error))
                                connection.cancel()
                                return
                            }
                            Self.receiveOneLine(connection, buffer: buffer) { result in
                                oneShot.resume(with: result)
                                connection.cancel()
                            }
                        })
                    case .failed(let error):
                        oneShot.resume(with: .failure(error))
                    case .cancelled:
                        oneShot.resume(with: .failure(CancellationError()))
                    default: break
                    }
                }
                connection.start(queue: queue)
            }
        } onCancel: {
            connection.cancel()
        }
    }

    private static func receiveOneLine(
        _ connection: NWConnection,
        buffer: DataBuffer,
        completion: @escaping @Sendable (Result<Data, Error>) -> Void
    ) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 65_536) { data, _, complete, error in
            if let data { buffer.data.append(data) }
            if let newline = buffer.data.firstIndex(of: 0x0A) {
                completion(.success(buffer.data.prefix(upTo: newline)))
            } else if let error {
                completion(.failure(error))
            } else if complete {
                completion(.failure(RPCErrorPayload(code: "connection_closed", message: "Daemon closed the connection")))
            } else {
                receiveOneLine(connection, buffer: buffer, completion: completion)
            }
        }
    }

    private static func receiveLines(
        _ connection: NWConnection,
        buffer: DataBuffer,
        continuation: AsyncThrowingStream<DaemonEvent, Error>.Continuation,
        expectedRequestID: String,
        skippedAck: Bool
    ) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 65_536) { data, _, complete, error in
            if let data { buffer.data.append(data) }
            var didSkipAck = skippedAck
            while let newline = buffer.data.firstIndex(of: 0x0A) {
                let line = buffer.data.prefix(upTo: newline)
                buffer.data.removeSubrange(...newline)
                if !didSkipAck {
                    didSkipAck = true
                    do {
                        let ack = try JSONDecoder().decode(SubscriptionAck.self, from: line)
                        if let error = ack.error { throw error }
                        guard ack.protocolVersion == 1, ack.requestID == expectedRequestID, ack.ok else {
                            throw RPCErrorPayload(code: "invalid_response", message: "Invalid event subscription response")
                        }
                    } catch {
                        continuation.finish(throwing: error)
                        connection.cancel()
                        return
                    }
                    continue
                }
                do {
                    continuation.yield(try WireJSON.decoder().decode(EventEnvelope.self, from: line).event)
                } catch {
                    continuation.finish(throwing: error)
                    connection.cancel()
                    return
                }
            }
            if let error { continuation.finish(throwing: error) }
            else if complete { continuation.finish() }
            else { receiveLines(connection, buffer: buffer, continuation: continuation, expectedRequestID: expectedRequestID, skippedAck: didSkipAck) }
        }
    }
}
