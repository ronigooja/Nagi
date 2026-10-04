import Foundation

/// The GUI's only interface to Nagi. No shell, configuration file, socket, or mihomo API is used.
struct NagiCLIClient {
    static let executable = URL(fileURLWithPath: "/usr/local/bin/nagi")
    private let timeout: TimeInterval = 45

    func run(_ arguments: [String]) async throws -> JSONValue {
        guard FileManager.default.isExecutableFile(atPath: Self.executable.path) else {
            throw CLIError.missingCLI(Self.executable.path)
        }
        return try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                do {
                    continuation.resume(returning: try runBlocking(arguments))
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    private func runBlocking(_ arguments: [String]) throws -> JSONValue {
        let process = Process()
        process.executableURL = Self.executable
        process.arguments = ["--json"] + arguments
        let stdout = Pipe()
        let stderr = Pipe()
        process.standardOutput = stdout
        process.standardError = stderr
        do {
            try process.run()
        } catch {
            throw CLIError.launch(error.localizedDescription)
        }
        stdout.fileHandleForWriting.closeFile()
        stderr.fileHandleForWriting.closeFile()

        let output = OutputBox()
        let readers = DispatchGroup()
        readers.enter()
        DispatchQueue.global(qos: .userInitiated).async {
            output.setStdout(stdout.fileHandleForReading.readDataToEndOfFile())
            readers.leave()
        }
        readers.enter()
        DispatchQueue.global(qos: .userInitiated).async {
            output.setStderr(stderr.fileHandleForReading.readDataToEndOfFile())
            readers.leave()
        }
        let deadline = DispatchTime.now() + timeout
        while process.isRunning && DispatchTime.now() < deadline {
            Thread.sleep(forTimeInterval: 0.05)
        }
        let timedOut = process.isRunning
        if timedOut { process.terminate() }
        process.waitUntilExit()
        readers.wait()
        if timedOut { throw CLIError.timeout }

        let (stdoutData, stderrData) = output.snapshot()
        let responseData = stdoutData.isEmpty ? stderrData : stdoutData
        guard let response = try? JSONDecoder().decode(CLIEnvelope.self, from: responseData) else {
            let diagnostic = String(data: stderrData, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines)
            throw CLIError.invalidJSON(diagnostic?.isEmpty == false ? diagnostic! : "The CLI returned an invalid JSON response.")
        }
        if response.ok && process.terminationStatus == 0 {
            guard let data = response.data else { throw CLIError.invalidJSON("The CLI omitted the data field.") }
            return data
        }
        throw CLIError.command(response.error?.code ?? "command_failed", response.error?.message ?? "The CLI exited with status \(process.terminationStatus).")
    }
}

private final class OutputBox {
    private let lock = NSLock()
    private var stdout = Data()
    private var stderr = Data()
    func setStdout(_ value: Data) { lock.lock(); stdout = value; lock.unlock() }
    func setStderr(_ value: Data) { lock.lock(); stderr = value; lock.unlock() }
    func snapshot() -> (Data, Data) { lock.lock(); defer { lock.unlock() }; return (stdout, stderr) }
}

private struct CLIEnvelope: Decodable {
    let ok: Bool
    let data: JSONValue?
    let error: CLIMessage?
}

private struct CLIMessage: Decodable {
    let code: String
    let message: String
}

enum CLIError: LocalizedError {
    case missingCLI(String)
    case launch(String)
    case timeout
    case invalidJSON(String)
    case command(String, String)

    var errorDescription: String? {
        switch self {
        case .missingCLI(let path): return "Nagi CLI is missing at \(path). Install the CLI there to use the app."
        case .launch(let reason): return "Could not start Nagi CLI: \(reason)"
        case .timeout: return "Nagi CLI did not respond within 45 seconds."
        case .invalidJSON(let reason): return "Nagi CLI response error: \(reason)"
        case .command(let code, let message): return "\(message) (\(code))"
        }
    }
}

indirect enum JSONValue: Decodable {
    case objectValue([String: JSONValue])
    case arrayValue([JSONValue])
    case stringValue(String)
    case numberValue(Double)
    case boolValue(Bool)
    case null

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
    var integer: Int? { if case .numberValue(let value) = self { return Int(value) }; return nil }
}
