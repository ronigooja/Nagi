import Foundation
import Darwin

/// All engine and configuration operations pass through the installed CLI.
struct NagiCLIClient {
    static let installedExecutable = URL(fileURLWithPath: "/usr/local/bin/nagi")
    let executable: URL
    private let timeout: TimeInterval
    init(executable: URL = installedExecutable, timeout: TimeInterval = 45) {
        self.executable = executable; self.timeout = timeout
    }

    func run(_ arguments: [String]) async throws -> JSONValue {
        try await run(arguments, stdoutLimit: 1_048_576, as: JSONValue.self)
    }

    /// Connections are decoded directly on the worker queue, without building a
    /// generic JSON tree or doing per-connection work on the main actor.
    func connections() async throws -> [Connection] {
        let result = try await run(["connections", "list"], stdoutLimit: 8_388_608, as: ConnectionList.self)
        return result.connections
    }

    private func run<Value: Decodable>(_ arguments: [String], stdoutLimit: Int, as type: Value.Type) async throws -> Value {
        guard FileManager.default.isExecutableFile(atPath: executable.path) else {
            throw CLIError.missingCLI(executable.path)
        }
        let holder = ProcessHolder()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                DispatchQueue.global(qos: .userInitiated).async {
                    do { continuation.resume(returning: try runBlocking(arguments, holder: holder, stdoutLimit: stdoutLimit, as: type)) }
                    catch { continuation.resume(throwing: error) }
                }
            }
        } onCancel: { holder.stop() }
    }

    func trafficStream(_ sample: @escaping (JSONValue) -> Void) throws -> TrafficStream {
        guard FileManager.default.isExecutableFile(atPath: executable.path) else {
            throw CLIError.missingCLI(executable.path)
        }
        return try TrafficStream(executable: executable, sample: sample)
    }

    func subscriptionWatch() throws -> SubscriptionWatch {
        guard FileManager.default.isExecutableFile(atPath: executable.path) else { throw CLIError.missingCLI(executable.path) }
        return try SubscriptionWatch(executable: executable)
    }

    private func runBlocking<Value: Decodable>(_ arguments: [String], holder: ProcessHolder, stdoutLimit: Int, as type: Value.Type) throws -> Value {
        let process = Process()
        process.executableURL = executable
        process.arguments = ["--json"] + arguments
        let stdout = Pipe(), stderr = Pipe()
        process.standardOutput = stdout; process.standardError = stderr
        let outReader = try CommandOutputReader(stdout.fileHandleForReading, limit: stdoutLimit)
        let errReader = try CommandOutputReader(stderr.fileHandleForReading)
        var writersClosed = false
        defer {
            if !writersClosed { stdout.fileHandleForWriting.closeFile(); stderr.fileHandleForWriting.closeFile() }
            outReader.finish(); errReader.finish()
            process.terminationHandler = nil
        }
        // Install the notification before launch: a very short command can exit
        // before run() returns. The semaphore retains that notification.
        process.terminationHandler = { _ in holder.didExit() }
        try holder.start(process)
        stdout.fileHandleForWriting.closeFile(); stderr.fileHandleForWriting.closeFile()
        writersClosed = true
        holder.waitForExit(timeout: timeout)
        // Drain bytes already written before decoding. Do not wait for EOF:
        // a detached descendant may still hold an inherited pipe writer.
        outReader.finish(); errReader.finish()
        if holder.timedOut { throw CLIError.timeout }
        if holder.cancelled { throw CancellationError() }
        let (out, outOverflow) = outReader.snapshot()
        let (err, errOverflow) = errReader.snapshot()
        if stdoutLimit > 1_048_576 {
            if outOverflow { throw CLIError.outputTooLarge(stdoutLimit) }
            if errOverflow { throw CLIError.invalidJSON("CLI stderr exceeded 1 MiB.") }
        } else if outOverflow || errOverflow {
            throw CLIError.invalidJSON("CLI output exceeded 1 MiB.")
        }
        let responseData = out.isEmpty ? err : out
        guard let response = try? JSONDecoder().decode(CLIEnvelope<Value>.self, from: responseData) else {
            let diagnostic = String(data: err, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines)
            throw CLIError.invalidJSON(diagnostic?.isEmpty == false ? diagnostic! : "The CLI returned invalid JSON.")
        }
        if response.ok && process.terminationStatus == 0 {
            guard let data = response.data else { throw CLIError.invalidJSON("The CLI omitted the data field.") }
            return data
        }
        throw CLIError.command(response.error?.code ?? "command_failed",
                               response.error?.message ?? "The CLI exited with status \(process.terminationStatus).")
    }
}

enum WatchExit {
    case stopped, duplicate, failed(Error)
}

final class SubscriptionWatch: @unchecked Sendable {
    private let process: Process
    private let stdout: Pipe
    private let stderr: Pipe
    private let lock = NSLock()
    private var stopped = false
    private var announced = false
    private var outputError: Error?
    private let readers = DispatchGroup()
    private let exited = DispatchGroup()
    init(executable: URL) throws {
        process = Process(); process.executableURL = executable
        process.arguments = ["--json", "subscription", "watch"]
        stdout = Pipe(); stderr = Pipe()
        process.standardOutput = stdout; process.standardError = stderr
        // A group retains a fast exit notification and supports repeated waits.
        exited.enter()
        process.terminationHandler = { [exited] _ in exited.leave() }
        do { try process.run() }
        catch { exited.leave(); throw CLIError.launch(error.localizedDescription) }
        stdout.fileHandleForWriting.closeFile(); stderr.fileHandleForWriting.closeFile()
        readers.enter()
        DispatchQueue.global(qos: .utility).async { [self] in
            readLines(stdout.fileHandleForReading, isError: false)
            readers.leave()
        }
        readers.enter()
        DispatchQueue.global(qos: .utility).async { [self] in
            readLines(stderr.fileHandleForReading, isError: true)
            readers.leave()
        }
    }
    private func readLines(_ handle: FileHandle, isError: Bool) {
        var pending = Data()
        while true {
            let chunk = handle.availableData
            if chunk.isEmpty { break }
            pending.append(chunk)
            while let newline = pending.firstIndex(of: 10) {
                if newline > 65_536 {
                    lock.lock(); outputError = CLIError.invalidJSON("Scheduler line exceeded 64 KiB."); lock.unlock()
                    terminateAfterFailure(); return
                }
                let line = Data(pending.prefix(upTo: newline))
                pending.removeSubrange(...newline)
                guard !line.isEmpty else { continue }
                parse(line, isError: isError)
            }
            if pending.count > 65_536 { lock.lock(); outputError = CLIError.invalidJSON("Scheduler line exceeded 64 KiB."); lock.unlock(); terminateAfterFailure(); return }
        }
        if !pending.isEmpty { parse(pending, isError: isError) }
    }
    private func parse(_ data: Data, isError: Bool) {
        guard let envelope = try? JSONDecoder().decode(CLIEnvelope<JSONValue>.self, from: data) else { return }
        lock.lock(); defer { lock.unlock() }
        if !isError && envelope.ok && envelope.data?["scheduler_running"]?.bool == true { announced = true }
        if isError, let error = envelope.error { outputError = CLIError.command(error.code, error.message) }
    }
    func stop() {
        lock.lock(); stopped = true; lock.unlock()
        guard process.isRunning else { return }
        process.terminate()
        DispatchQueue.global().asyncAfter(deadline: .now() + 1) { [process] in
            if process.isRunning { _ = Darwin.kill(process.processIdentifier, SIGKILL) }
        }
    }
    private func terminateAfterFailure() {
        guard process.isRunning else { return }
        process.terminate()
        DispatchQueue.global().asyncAfter(deadline: .now() + 1) { [process] in
            if process.isRunning { _ = Darwin.kill(process.processIdentifier, SIGKILL) }
        }
    }
    func wait() async -> WatchExit {
        await withCheckedContinuation { continuation in
            DispatchQueue.global(qos: .utility).async { [self] in
                exited.wait()
                readers.wait()
                lock.lock(); let intentional = stopped, started = announced, failure = outputError; lock.unlock()
                if intentional { continuation.resume(returning: .stopped) }
                else if let command = failure as? CLIError, case .command(let code, _) = command, code == "scheduler_already_running" {
                    continuation.resume(returning: .duplicate)
                } else if let failure { continuation.resume(returning: .failed(failure)) }
                else if !started { continuation.resume(returning: .failed(CLIError.invalidJSON("Scheduler exited before announcing readiness."))) }
                else { continuation.resume(returning: .failed(CLIError.invalidJSON("Scheduler exited unexpectedly."))) }
            }
        }
    }
}

private final class ProcessHolder: @unchecked Sendable {
    private let lock = NSLock()
    private let exited = DispatchSemaphore(value: 0)
    private var process: Process?
    private var cancelledFlag = false
    private var timedOutFlag = false
    private var exitedFlag = false
    var cancelled: Bool { lock.lock(); defer { lock.unlock() }; return cancelledFlag }
    var timedOut: Bool { lock.lock(); defer { lock.unlock() }; return timedOutFlag }
    func start(_ value: Process) throws {
        lock.lock()
        defer { lock.unlock() }
        if cancelledFlag { throw CancellationError() }
        do { try value.run(); process = value }
        catch { throw CLIError.launch(error.localizedDescription) }
    }
    func didExit() {
        lock.lock(); exitedFlag = true; lock.unlock()
        exited.signal()
    }
    func waitForExit(timeout: TimeInterval) {
        if exited.wait(timeout: .now() + timeout) == .success { return }
        lock.lock()
        // Whichever event won the lock determines the error classification.
        // Cancellation after a timeout must not replace the timeout error.
        timedOutFlag = !cancelledFlag && !exitedFlag
        let value = process
        lock.unlock()
        Self.terminate(value)
        exited.wait()
    }
    func stop() {
        lock.lock(); cancelledFlag = true
        let value = process
        lock.unlock()
        Self.terminate(value)
    }
    private static func terminate(_ value: Process?) {
        guard let value, value.isRunning else { return }
        value.terminate()
        // A child ignoring SIGTERM must not keep the app's task alive indefinitely.
        DispatchQueue.global().asyncAfter(deadline: .now() + 1) {
            if value.isRunning { _ = Darwin.kill(value.processIdentifier, SIGKILL) }
        }
    }
}

/// Each reader owns its descriptor and buffer on a serial queue. Nonblocking
/// reads permit a final drain after exit even if another process retains stdout.
private final class CommandOutputReader: @unchecked Sendable {
    private let queue = DispatchQueue(label: "nagi.cli.output", qos: .userInitiated)
    private let handle: FileHandle
    private let source: DispatchSourceRead
    private let closed = DispatchGroup()
    private var bytes = Data()
    private var overflow = false
    private var finished = false
    private let limit: Int

    init(_ handle: FileHandle, limit: Int = 1_048_576) throws {
        self.handle = handle
        self.limit = limit
        let descriptor = handle.fileDescriptor
        let flags = Darwin.fcntl(descriptor, F_GETFL)
        guard flags >= 0, Darwin.fcntl(descriptor, F_SETFL, flags | O_NONBLOCK) >= 0 else {
            throw CLIError.launch("Could not prepare CLI output pipes.")
        }
        source = DispatchSource.makeReadSource(fileDescriptor: descriptor, queue: queue)
        closed.enter()
        source.setEventHandler { [weak self] in self?.drain(maximum: 262_144) }
        source.setCancelHandler { [handle, closed] in
            handle.closeFile()
            closed.leave()
        }
        source.resume()
    }

    deinit { source.cancel() }

    private func drain(maximum: Int) {
        var buffer = [UInt8](repeating: 0, count: 4096)
        var readCount = 0
        while readCount < maximum {
            let count = Darwin.read(handle.fileDescriptor, &buffer, 4096)
            if count > 0 {
                readCount += count
                if bytes.count + count <= limit { bytes.append(contentsOf: buffer.prefix(count)) }
                else { overflow = true }
            } else if count == 0 {
                source.cancel(); return
            } else if errno != EINTR {
                return
            }
        }
    }

    func finish() {
        queue.sync {
            guard !finished else { return }
            finished = true
            if !source.isCancelled {
                // More than the output bound cannot form a valid response.
                // Bound this drain even if a descendant keeps writing forever.
                drain(maximum: limit + 4096)
                source.cancel()
            }
        }
        closed.wait()
    }

    func snapshot() -> (Data, Bool) { queue.sync { (bytes, overflow) } }
}

private final class BoundedOutput {
    private let lock = NSLock()
    private var stdout = Data(), stderr = Data(), overflow = false
    func setStdout(_ value: (Data, Bool)) { lock.lock(); stdout = value.0; overflow = overflow || value.1; lock.unlock() }
    func setStderr(_ value: (Data, Bool)) { lock.lock(); stderr = value.0; overflow = overflow || value.1; lock.unlock() }
    func snapshot() -> (Data, Data, Bool) { lock.lock(); defer { lock.unlock() }; return (stdout, stderr, overflow) }
}

// Worker queues share this stream; the lock guards failure/stopped, and semaphores coordinate completion.
final class TrafficStream: @unchecked Sendable {
    private let process = Process()
    private let done = DispatchSemaphore(value: 0)
    private let stderrDone = DispatchSemaphore(value: 0)
    private let exited = DispatchGroup()
    private let output = BoundedOutput()
    private let lock = NSLock()
    private var stopped = false
    private var failure: Error?
    init(executable: URL, sample: @escaping (JSONValue) -> Void) throws {
        process.executableURL = executable
        process.arguments = ["--json", "traffic", "watch"]
        let stdout = Pipe(), stderr = Pipe()
        process.standardOutput = stdout; process.standardError = stderr
        // A group retains a fast exit notification and supports repeated waits.
        exited.enter()
        process.terminationHandler = { [exited] _ in exited.leave() }
        do { try process.run() }
        catch { exited.leave(); throw CLIError.launch(error.localizedDescription) }
        stdout.fileHandleForWriting.closeFile(); stderr.fileHandleForWriting.closeFile()
        DispatchQueue.global(qos: .utility).async { [self] in
            output.setStderr(Self.readStderr(stderr.fileHandleForReading))
            stderrDone.signal()
        }
        DispatchQueue.global(qos: .userInitiated).async { [self] in
            var pending = Data()
            var invalid = false
            while true {
                let chunk = stdout.fileHandleForReading.availableData
                if chunk.isEmpty { break }
                pending.append(chunk)
                while let newline = pending.firstIndex(of: 10) {
                    let line = pending.prefix(upTo: newline)
                    pending.removeSubrange(...newline)
                    guard !line.isEmpty else { continue }
                    guard let envelope = try? JSONDecoder().decode(CLIEnvelope<JSONValue>.self, from: line),
                          envelope.ok, let data = envelope.data else {
                        setFailure(CLIError.invalidJSON("Traffic stream returned an invalid sample."))
                        invalid = true; stop(); break
                    }
                    DispatchQueue.main.async { sample(data) }
                }
                if invalid { break }
                if pending.count > 65_536 {
                    setFailure(CLIError.invalidJSON("Traffic sample exceeded 64 KiB."))
                    stop(); break
                }
            }
            exited.wait()
            stderrDone.wait()
            done.signal()
        }
    }
    private static func readStderr(_ handle: FileHandle) -> (Data, Bool) {
        var data = Data(), overflow = false
        while let chunk = try? handle.read(upToCount: 4096), !chunk.isEmpty {
            if data.count + chunk.count <= 65_536 { data.append(chunk) } else { overflow = true }
        }
        return (data, overflow)
    }
    private func setFailure(_ error: Error) { lock.lock(); failure = error; lock.unlock() }
    func stop() {
        lock.lock(); stopped = true; lock.unlock()
        if process.isRunning {
            process.terminate()
            DispatchQueue.global().asyncAfter(deadline: .now() + 1) { [process] in
                if process.isRunning { _ = Darwin.kill(process.processIdentifier, SIGKILL) }
            }
        }
    }
    func wait() async throws {
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                DispatchQueue.global(qos: .utility).async { [self] in
                    done.wait()
                    lock.lock(); let localFailure = failure, wasStopped = stopped; lock.unlock()
                    if let localFailure { continuation.resume(throwing: localFailure); return }
                    if wasStopped { continuation.resume(throwing: CancellationError()); return }
                    if process.terminationStatus == 0 { continuation.resume(returning: ()); return }
                    let (_, stderr, _) = output.snapshot()
                    if let envelope = try? JSONDecoder().decode(CLIEnvelope<JSONValue>.self, from: stderr),
                       let error = envelope.error {
                        continuation.resume(throwing: CLIError.command(error.code, error.message))
                    } else { continuation.resume(throwing: CLIError.invalidJSON("Traffic stream exited unexpectedly.")) }
                }
            }
        } onCancel: { stop() }
    }
}

private struct CLIEnvelope<Value: Decodable>: Decodable { let ok: Bool; let data: Value?; let error: CLIMessage? }
private struct CLIMessage: Decodable { let code: String; let message: String }
enum CLIError: LocalizedError {
    case missingCLI(String), launch(String), timeout, invalidJSON(String), command(String, String), outputTooLarge(Int)
    var errorDescription: String? {
        switch self {
        case .missingCLI(let path): return "Nagi CLI is missing at \(path). Install the CLI there."
        case .launch(let reason): return "Could not start Nagi CLI: \(reason)"
        case .timeout: return "Nagi CLI did not respond within 45 seconds."
        case .invalidJSON(let reason): return "Nagi CLI response error: \(reason)"
        case .command(let code, let message): return "\(message) (\(code))"
        case .outputTooLarge(let limit): return "Nagi CLI output exceeded \(limit / 1_048_576) MiB."
        }
    }
}
indirect enum JSONValue: Decodable {
    case objectValue([String: JSONValue]), arrayValue([JSONValue]), stringValue(String)
    case numberValue(Double), boolValue(Bool), null
    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() { self = .null }
        else if let value = try? container.decode(Bool.self) { self = .boolValue(value) }
        else if let value = try? container.decode(Double.self) { self = .numberValue(value) }
        else if let value = try? container.decode(String.self) { self = .stringValue(value) }
        else if let value = try? container.decode([String: JSONValue].self) { self = .objectValue(value) }
        else { self = .arrayValue(try container.decode([JSONValue].self)) }
    }
    subscript(_ key: String) -> JSONValue? {
        guard case .objectValue(let value) = self else { return nil }
        return value[key]
    }
    var array: [JSONValue] { if case .arrayValue(let value) = self { return value }; return [] }
    var string: String? { if case .stringValue(let value) = self { return value }; return nil }
    var bool: Bool? { if case .boolValue(let value) = self { return value }; return nil }
    var int64: Int64? { if case .numberValue(let value) = self, value.isFinite, value >= 0, value < Double(Int64.max) { return Int64(value) }; return nil }
}
