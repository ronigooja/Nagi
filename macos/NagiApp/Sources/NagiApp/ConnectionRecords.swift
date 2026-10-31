import Foundation

enum ConnectionFilter: Int { case all, active, ended }

/// In-memory observations only. A failed or cancelled read never calls merge.
struct ConnectionRecords: Sendable {
    private var activeOrder: [String] = []
    private(set) var active: [String: Connection] = [:]
    private(set) var ended: [String: (connection: Connection, disappeared: Date)] = [:]
    var retention: TimeInterval = 300
    var maximumCount = 1000
    var byteBudget = 32 * 1024 * 1024
    /// Active IDs are kept newest-first. Existing active observations retain
    /// their relative order; a new or revived ID is inserted at the front.
    var values: [Connection] { activeOrder.compactMap { active[$0] } + ended.sorted { $0.key < $1.key }.map { $0.value.connection } }

    mutating func interrupt() {
        for id in active.keys { active[id]?.observationInterrupted = true }
        for id in ended.keys { ended[id]?.connection.observationInterrupted = true }
    }
    mutating func merge(_ snapshot: [Connection], at date: Date) {
        var next: [String: Connection] = [:]
        var revived = Set<String>()
        for var connection in snapshot {
            if ended[connection.id] != nil { revived.insert(connection.id) }
            connection.observationInterrupted = active[connection.id]?.observationInterrupted
                ?? ended[connection.id]?.connection.observationInterrupted ?? false
            next[connection.id] = connection
            ended.removeValue(forKey: connection.id)
        }
        for (id, var connection) in active where next[id] == nil {
            connection.ended = true
            ended[id] = (connection, date)
        }
        var seen = Set<String>()
        let observedIDs = snapshot.map(\.id).filter { seen.insert($0).inserted }
        let newest = observedIDs.filter { active[$0] == nil || revived.contains($0) }
        let stillActive = activeOrder.filter { next[$0] != nil }
        activeOrder = newest + stillActive
        active = next
        prune(at: date)
    }
    mutating func prune(at date: Date) {
        let oldest = ended.sorted {
            $0.value.disappeared == $1.value.disappeared ? $0.key < $1.key : $0.value.disappeared < $1.value.disappeared
        }
        var bytes = ended.values.reduce(0) { $0 + $1.connection.encodedBytes }
        for (id, record) in oldest {
            if date.timeIntervalSince(record.disappeared) >= retention || ended.count > maximumCount || bytes > byteBudget {
                ended.removeValue(forKey: id)
                bytes -= record.connection.encodedBytes
            }
        }
    }
}

// Count the full source record's canonical JSON encoding, including unknown
// metadata. Keep integers exact rather than passing counters through Double.
private indirect enum ConnectionJSON: Codable {
    case object([String: ConnectionJSON]), array([ConnectionJSON]), string(String), integer(Int64), number(Double), bool(Bool), null
    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let v = try? c.decode(Bool.self) { self = .bool(v) }
        else if let v = try? c.decode(Int64.self) { self = .integer(v) }
        else if let v = try? c.decode(Double.self) { self = .number(v) }
        else if let v = try? c.decode(String.self) { self = .string(v) }
        else if let v = try? c.decode([String: ConnectionJSON].self) { self = .object(v) }
        else { self = .array(try c.decode([ConnectionJSON].self)) }
    }
    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .object(let v): try c.encode(v)
        case .array(let v): try c.encode(v)
        case .string(let v): try c.encode(v)
        case .integer(let v): try c.encode(v)
        case .number(let v): try c.encode(v)
        case .bool(let v): try c.encode(v)
        case .null: try c.encodeNil()
        }
    }
    static func size(from decoder: Decoder) throws -> Int { try JSONEncoder().encode(Self(from: decoder)).count }
}

func connectionEncodedBytes(from decoder: Decoder) throws -> Int { try ConnectionJSON.size(from: decoder) }
