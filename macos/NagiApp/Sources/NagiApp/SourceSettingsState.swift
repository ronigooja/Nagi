import Foundation

enum SettingsSection: Hashable { case update, interval, url }

struct SettingsIntervalDraft: Equatable {
    let presets: [Int64]
    var selectedIndex: Int
    var customText: String
    init(presets: [Int64], seconds: Int64?) {
        self.presets = presets
        selectedIndex = seconds.flatMap { presets.firstIndex(of: $0) } ?? presets.count
        customText = seconds.map(String.init) ?? ""
    }
    var seconds: Int64? {
        presets.indices.contains(selectedIndex) ? presets[selectedIndex] : Int64(customText.trimmingCharacters(in: .whitespacesAndNewlines))
    }
    var isValid: Bool { seconds.map { (1...31_536_000).contains($0) } == true }
}

struct UpdateSettingsDraft: Equatable {
    var automatic: Bool
    var interval: SettingsIntervalDraft
    var value: Int64? { automatic ? interval.seconds : 0 }
    var isValid: Bool { !automatic || interval.isValid }
}

struct LatencyIntervalDraft: Equatable {
    var inherited: Bool
    var interval: SettingsIntervalDraft
    var value: Int64? { inherited ? nil : interval.seconds }
    var isValid: Bool { inherited || interval.isValid }
    func hasSameSetting(as other: Self) -> Bool {
        inherited == other.inherited && (inherited || value == other.value)
    }
}

/// Session-only UI state. The CLI remains the owner of validation and persistence.
struct SourceSettingsSession {
    var update = UpdateSettingsDraft(automatic: true, interval: .init(presets: [3600, 21600, 86400], seconds: 3600))
    var interval = LatencyIntervalDraft(inherited: true, interval: .init(presets: [300, 600, 1800, 3600], seconds: nil))
    var urlInherited = true
    var urlText = ""
    var canResetURL = true
    private(set) var updateBaseline: Int64?
    private(set) var intervalBaseline: LatencyIntervalDraft?
    private(set) var urlInheritedBaseline = true
    private(set) var busy: SettingsSection?
    private(set) var pendingRefresh: Set<SettingsSection> = []

    mutating func loadUpdate(seconds: Int64) {
        updateBaseline = seconds
        update = .init(automatic: seconds > 0, interval: .init(presets: update.interval.presets, seconds: seconds > 0 ? seconds : 3600))
    }
    mutating func loadInterval(seconds: Int64?, inherited: Bool) {
        interval = .init(inherited: inherited, interval: .init(presets: interval.interval.presets, seconds: seconds))
        intervalBaseline = interval
    }
    mutating func loadURL(inherited: Bool, canReset: Bool) {
        canResetURL = canReset
        urlInherited = inherited && canReset
        urlInheritedBaseline = urlInherited
        urlText = ""
    }
    var updateChanged: Bool { updateBaseline != nil && (update.value != updateBaseline || !update.isValid) }
    var intervalChanged: Bool { intervalBaseline.map { !interval.hasSameSetting(as: $0) || !interval.isValid } ?? false }
    var urlChanged: Bool { urlInherited != urlInheritedBaseline || (!urlInherited && !urlText.isEmpty) }
    var dirty: Bool { updateChanged || intervalChanged || urlChanged }
    var canClose: Bool { busy == nil }
    var needsDiscardConfirmation: Bool { dirty && canClose }
    var canSaveUpdate: Bool { busy == nil && !pendingRefresh.contains(.update) && updateChanged && update.isValid }
    var canSaveInterval: Bool { busy == nil && !pendingRefresh.contains(.interval) && intervalChanged && interval.isValid }
    var canSaveURL: Bool {
        busy == nil && !pendingRefresh.contains(.url) && urlChanged &&
            (urlInherited ? canResetURL : Self.validURL(urlText))
    }
    static func validURL(_ raw: String) -> Bool {
        guard raw.rangeOfCharacter(from: .whitespacesAndNewlines) == nil,
              let url = URLComponents(string: raw), ["http", "https"].contains(url.scheme?.lowercased() ?? ""),
              url.host?.isEmpty == false, url.user == nil, url.password == nil, url.fragment == nil else { return false }
        return true
    }
    @discardableResult mutating func begin(_ section: SettingsSection) -> Bool {
        guard busy == nil else { return false }
        busy = section
        return true
    }
    mutating func saveSucceeded(_ section: SettingsSection) {
        switch section {
        case .update: updateBaseline = update.value
        case .interval: intervalBaseline = interval
        case .url: urlInheritedBaseline = urlInherited; urlText = ""
        }
        pendingRefresh.insert(section)
    }
    mutating func refreshSucceeded(_ section: SettingsSection) { pendingRefresh.remove(section); busy = nil }
    mutating func failed(_ section: SettingsSection) { busy = nil }
}
