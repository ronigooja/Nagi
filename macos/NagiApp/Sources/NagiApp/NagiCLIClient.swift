import Foundation
import Darwin

/// All engine and configuration operations pass through the installed CLI.
struct NagiCLIClient {
    static let executable = URL(fileURLWithPath: "/usr/local/bin/nagi")
    private let timeout: TimeInterval = 45

    func run(_ arguments: [String]) async throws -> JSONValue {
        guard FileManager.default.isExecutableFile(atPath: Self.executable.path) else {
            throw CLIError.missingCLI(Self.executable.path)
        }
        let holder = ProcessHolder()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                DispatchQueue.global(qos: .userInitiated).async {
                    do { continuation.resume(returning: try runBlocking(arguments, holder: holder)) }
                    catch { continuation.resume(throwing: error) }
                }
            }
        } onCancel: { holder.stop() }
    }

    func trafficStream(_ sample: @escaping (JSONValue) -> Void) throws -> TrafficStream {
        guard FileManager.default.isExecutableFile(atPath: Self.executable.path) else {
            throw CLIError.missingCLI(Self.executable.path)
        }
        return try TrafficStream(executable: Self.executable, sample: sample)
    }

    private func runBlocking(_ arguments: [String], holder: ProcessHolder) throws -> JSONValue {
        let process = Process()
        process.executableURL = Self.executable
        process.arguments = ["--json"] + arguments
        let stdout = Pipe(), stderr = Pipe()
        process.standardOutput = stdout; process.standardError = stderr
        try holder.start(process)
        stdout.fileHandleForWriting.closeFile(); stderr.fileHandleForWriting.closeFile()
        let output = BoundedOutput()
        let readers = DispatchGroup()
        readers.enter()
        DispatchQueue.global(qos: .userInitiated).async {
            output.setStdout(Self.readLimited(stdout.fileHandleForReading))
            readers.leave()
        }
        readers.enter()
        DispatchQueue.global(qos: .userInitiated).async {
            output.setStderr(Self.readLimited(stderr.fileHandleForReading))
            readers.leave()
        }
        let deadline = Date().addingTimeInterval(timeout)
        while process.isRunning && Date() < deadline && !holder.cancelled {
            Thread.sleep(forTimeInterval: 0.05)
        }
        let timedOut = process.isRunning && !holder.cancelled
        if process.isRunning { holder.stop() }
        process.waitUntilExit()
        readers.wait()
        if holder.cancelled && !timedOut { throw CancellationError() }
        if timedOut { throw CLIError.timeout }
        let (out, err, overflow) = output.snapshot()
        if overflow { throw CLIError.invalidJSON("CLI output exceeded 1 MiB.") }
        let responseData = out.isEmpty ? err : out
        guard let response = try? JSONDecoder().decode(CLIEnvelope.self, from: responseData) else {
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

    private static func readLimited(_ handle: FileHandle) -> (Data, Bool) {
        var bytes = Data(), overflow = false
        while let chunk = try? handle.read(upToCount: 4096), !chunk.isEmpty {
            if bytes.count + chunk.count <= 1_048_576 { bytes.append(chunk) }
            else { overflow = true }
        }
        return (bytes, overflow)
    }
}

private final class ProcessHolder {
    private let lock = NSLock()
    private var process: Process?
    private var cancelledFlag = false
    var cancelled: Bool { lock.lock(); defer { lock.unlock() }; return cancelledFlag }
    func start(_ value: Process) throws {
        lock.lock()
        defer { lock.unlock() }
        if cancelledFlag { throw CancellationError() }
        do { try value.run(); process = value }
        catch { throw CLIError.launch(error.localizedDescription) }
    }
    func stop() {
        lock.lock(); cancelledFlag = true
        let value = process
        lock.unlock()
        guard let value, value.isRunning else { return }
        value.terminate()
        // A child ignoring SIGTERM must not keep the app's task alive indefinitely.
        DispatchQueue.global().asyncAfter(deadline: .now() + 1) {
            if value.isRunning { _ = Darwin.kill(value.processIdentifier, SIGKILL) }
        }
    }
}

private final class BoundedOutput {
    private let lock = NSLock()
    private var stdout = Data(), stderr = Data(), overflow = false
    func setStdout(_ value: (Data, Bool)) { lock.lock(); stdout = value.0; overflow = overflow || value.1; lock.unlock() }
    func setStderr(_ value: (Data, Bool)) { lock.lock(); stderr = value.0; overflow = overflow || value.1; lock.unlock() }
    func snapshot() -> (Data, Data, Bool) { lock.lock(); defer { lock.unlock() }; return (stdout, stderr, overflow) }
}

final class TrafficStream {
    private let process = Process()
    private let done = DispatchSemaphore(value: 0)
    private let stderrDone = DispatchSemaphore(value: 0)
    private let output = BoundedOutput()
    private let lock = NSLock()
    private var stopped = false
    private var failure: Error?
    init(executable: URL, sample: @escaping (JSONValue) -> Void) throws {
        process.executableURL = executable
        process.arguments = ["--json", "traffic", "watch"]
        let stdout = Pipe(), stderr = Pipe()
        process.standardOutput = stdout; process.standardError = stderr
        do { try process.run() } catch { throw CLIError.launch(error.localizedDescription) }
        stdout.fileHandleForWriting.closeFile(); stderr.fileHandleForWriting.closeFile()
        DispatchQueue.global(qos: .utility).async { [self] in
            output.setStderr(Self.readStderr(stderr.fileHandleForReading))
            stderrDone.signal()
        }
        DispatchQueue.global(qos: .userInitiated).async { [self] in
            var pending = Data()
            var invalid = false
            while let chunk = try? stdout.fileHandleForReading.read(upToCount: 4096), !chunk.isEmpty {
                pending.append(chunk)
                while let newline = pending.firstIndex(of: 10) {
                    let line = pending.prefix(upTo: newline)
                    pending.removeSubrange(...newline)
                    guard !line.isEmpty else { continue }
                    guard let envelope = try? JSONDecoder().decode(CLIEnvelope.self, from: line),
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
            process.waitUntilExit()
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
            try await withCheckedThrowingContinuation { continuation in
                DispatchQueue.global(qos: .utility).async { [self] in
                    done.wait()
                    lock.lock(); let localFailure = failure, wasStopped = stopped; lock.unlock()
                    if let localFailure { continuation.resume(throwing: localFailure); return }
                    if wasStopped { continuation.resume(throwing: CancellationError()); return }
                    if process.terminationStatus == 0 { continuation.resume(returning: ()); return }
                    let (_, stderr, _) = output.snapshot()
                    if let envelope = try? JSONDecoder().decode(CLIEnvelope.self, from: stderr),
                       let error = envelope.error {
                        continuation.resume(throwing: CLIError.command(error.code, error.message))
                    } else { continuation.resume(throwing: CLIError.invalidJSON("Traffic stream exited unexpectedly.")) }
                }
            }
        } onCancel: { stop() }
    }
}

private struct CLIEnvelope: Decodable { let ok: Bool; let data: JSONValue?; let error: CLIMessage? }
private struct CLIMessage: Decodable { let code: String; let message: String }
enum CLIError: LocalizedError {
    case missingCLI(String), launch(String), timeout, invalidJSON(String), command(String, String)
    var errorDescription: String? {
        switch self {
        case .missingCLI(let path): return "Nagi CLI is missing at \(path). Install the CLI there."
        case .launch(let reason): return "Could not start Nagi CLI: \(reason)"
        case .timeout: return "Nagi CLI did not respond within 45 seconds."
        case .invalidJSON(let reason): return "Nagi CLI response error: \(reason)"
        case .command(let code, let message): return "\(message) (\(code))"
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
    var int64: Int64? { if case .numberValue(let value) = self, value.isFinite, value >= 0, value <= Double(Int64.max) { return Int64(value) }; return nil }
}
