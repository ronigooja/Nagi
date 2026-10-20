import Foundation

/// A current connection from the CLI. All display fields are derived while
/// decoding on the CLI worker queue; counters are cumulative byte counts.
struct Connection: Decodable, Equatable, Sendable {
    var ended = false
    var observationInterrupted = false
    var encodedBytes = 0
    let id: String
    let destination: String
    let destinationIP: String
    let source: String
    let process: String
    let processPath: String
    let network: String
    let inbound: String
    let chains: [String]
    let chain: String
    let rule: String
    let rulePayload: String
    let upload: Int64?
    let download: Int64?
    private let searchText: String

    private enum CodingKeys: String, CodingKey { case id, metadata, chains, rule, rulePayload, upload, download }
    private struct Metadata: Decodable {
        let host, destinationIP, sourceIP, process, processPath, network, type: String?
        let destinationPort, sourcePort: Port?
    }
    /// Pinned mihomo uses string ports; also accept integer ports from fixtures
    /// and compatible CLI versions without losing the address representation.
    private struct Port: Decodable {
        let value: String
        init(from decoder: Decoder) throws {
            let c = try decoder.singleValueContainer()
            if let string = try? c.decode(String.self) { value = string }
            else { value = String(try c.decode(UInt16.self)) }
        }
    }
    init(from decoder: Decoder) throws {
        encodedBytes = try connectionEncodedBytes(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        let metadata = try c.decodeIfPresent(Metadata.self, forKey: .metadata)
        let host = Self.known(metadata?.host)
        let ip = Self.known(metadata?.destinationIP)
        let sourceIP = Self.known(metadata?.sourceIP)
        destination = Self.address(host ?? ip, port: metadata?.destinationPort?.value)
        destinationIP = Self.address(ip, port: metadata?.destinationPort?.value)
        source = Self.address(sourceIP, port: metadata?.sourcePort?.value)
        processPath = Self.known(metadata?.processPath) ?? "unknown"
        process = Self.known(metadata?.process) ?? "unknown"
        network = Self.known(metadata?.network)?.uppercased() ?? "unknown"
        inbound = Self.known(metadata?.type) ?? "unknown"
        chains = try c.decodeIfPresent([String].self, forKey: .chains) ?? []
        chain = chains.isEmpty ? "unknown" : chains.joined(separator: " → ")
        rule = Self.known(try c.decodeIfPresent(String.self, forKey: .rule)) ?? "unknown"
        rulePayload = Self.known(try c.decodeIfPresent(String.self, forKey: .rulePayload)) ?? "unknown"
        upload = try c.decodeIfPresent(Int64.self, forKey: .upload).flatMap { $0 >= 0 ? $0 : nil }
        download = try c.decodeIfPresent(Int64.self, forKey: .download).flatMap { $0 >= 0 ? $0 : nil }
        searchText = [id, destination, destinationIP, source, process, processPath, chain].joined(separator: "\n").lowercased()
    }

    private static func known(_ value: String?) -> String? {
        guard let value, !value.isEmpty else { return nil }
        return value
    }
    private static func address(_ host: String?, port: String?) -> String {
        guard let host else { return "unknown" }
        guard let port = known(port) else { return host }
        return host.contains(":") && !host.hasPrefix("[") ? "[\(host)]:\(port)" : "\(host):\(port)"
    }
    func matches(_ query: String) -> Bool { query.isEmpty || searchText.contains(query.lowercased()) }
}

struct ConnectionList: Decodable { let connections: [Connection] }

enum ConnectionSortColumn: String, Sendable {
    case status, destination, process, network, chains, upload, download
}

/// This function runs in detached tasks, including every search and sort.
func connectionRows(_ connections: [Connection], query: String, column: ConnectionSortColumn, ascending: Bool) throws -> [Connection] {
    let query = query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    return try connections.filter {
        try Task.checkCancellation()
        return $0.matches(query)
    }.sorted { lhs, rhs in
        try Task.checkCancellation()
        let order: ComparisonResult
        switch column {
        case .upload: order = compareConnectionBytes(lhs.upload, rhs.upload)
        case .download: order = compareConnectionBytes(lhs.download, rhs.download)
        default:
            let strings: (String, String)
            switch column {
            case .status: strings = (lhs.ended ? "ended" : "active", rhs.ended ? "ended" : "active")
            case .destination: strings = (lhs.destination, rhs.destination)
            case .process: strings = (lhs.process, rhs.process)
            case .network: strings = (lhs.network, rhs.network)
            case .chains: strings = (lhs.chain, rhs.chain)
            default: strings = ("", "")
            }
            order = strings.0.compare(strings.1, options: [.caseInsensitive, .numeric], locale: Locale(identifier: "en_US_POSIX"))
        }
        if order == .orderedSame { return lhs.id < rhs.id }
        return ascending ? order == .orderedAscending : order == .orderedDescending
    }
}

private func compareConnectionBytes(_ lhs: Int64?, _ rhs: Int64?) -> ComparisonResult {
    switch (lhs, rhs) {
    case (nil, nil): return .orderedSame
    case (nil, _): return .orderedAscending
    case (_, nil): return .orderedDescending
    case (let l?, let r?): return l == r ? .orderedSame : (l < r ? .orderedAscending : .orderedDescending)
    }
}
