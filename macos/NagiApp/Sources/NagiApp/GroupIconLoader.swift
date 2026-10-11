import AppKit
import Foundation

/// Presentation-only images from the icon string returned by `proxy groups`.
@MainActor final class GroupIconLoader {
    private let cache = NSCache<NSString, NSImage>()
    private let failures = NSCache<NSString, NSNumber>()
    private var pending: [String: [(NSImage?) -> Void]] = [:]

    init() { cache.countLimit = 64; failures.countLimit = 64 }

    func image(for value: String, completion: @escaping (NSImage?) -> Void) -> NSImage? {
        if value.hasPrefix("system:") {
            return NSImage(systemSymbolName: String(value.dropFirst(7)), accessibilityDescription: nil)
        }
        guard let url = Self.allowedURL(value) else { return nil }
        if let cached = cache.object(forKey: value as NSString) { return cached }
        if failures.object(forKey: value as NSString) != nil { return nil }
        if pending[value] != nil { pending[value]?.append(completion); return nil }
        pending[value] = [completion]
        let receiver = IconRequest(url: url) { [weak self] image in
            Task { @MainActor in
                guard let self else { return }
                let callbacks = self.pending.removeValue(forKey: value) ?? []
                if let image { self.cache.setObject(image, forKey: value as NSString) }
                else { self.failures.setObject(1, forKey: value as NSString) }
                callbacks.forEach { $0(image) }
            }
        }
        receiver.start()
        return nil
    }

    nonisolated static func allowedURL(_ value: String) -> URL? {
        guard let components = URLComponents(string: value),
              let scheme = components.scheme?.lowercased(), ["http", "https"].contains(scheme),
              components.host != nil, components.user == nil, components.password == nil,
              let url = components.url else { return nil }
        return url
    }

    nonisolated static func allowedRedirect(from original: URL, to target: URL) -> Bool {
        allowedURL(target.absoluteString) != nil &&
            target.scheme?.lowercased() == original.scheme?.lowercased()
    }
}

private final class IconRequest: NSObject, URLSessionDataDelegate, URLSessionTaskDelegate, @unchecked Sendable {
    private let url: URL
    private let completion: (NSImage?) -> Void
    private var bytes = Data()
    private var session: URLSession?
    private static let limit = 256 * 1024

    init(url: URL, completion: @escaping (NSImage?) -> Void) {
        self.url = url; self.completion = completion
    }

    func start() {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 5
        config.timeoutIntervalForResource = 8
        config.urlCache = nil
        config.httpShouldSetCookies = false
        let session = URLSession(configuration: config, delegate: self, delegateQueue: nil)
        self.session = session
        session.dataTask(with: url).resume()
    }

    func urlSession(_ session: URLSession, task: URLSessionTask,
                    willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest,
                    completionHandler: @escaping (URLRequest?) -> Void) {
        guard let target = request.url, GroupIconLoader.allowedRedirect(from: url, to: target) else {
            completionHandler(nil); task.cancel(); return
        }
        completionHandler(request)
    }

    func urlSession(_ session: URLSession, dataTask: URLSessionDataTask,
                    didReceive response: URLResponse, completionHandler: @escaping (URLSession.ResponseDisposition) -> Void) {
        guard let response = response as? HTTPURLResponse, response.statusCode == 200,
              response.expectedContentLength <= Self.limit else {
            completionHandler(.cancel); return
        }
        completionHandler(.allow)
    }

    func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        guard bytes.count + data.count <= Self.limit else { dataTask.cancel(); return }
        bytes.append(data)
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        var image: NSImage?
        if error == nil, let bitmap = NSBitmapImageRep(data: bytes),
           bitmap.pixelsWide > 0, bitmap.pixelsHigh > 0,
           bitmap.pixelsWide <= 1024, bitmap.pixelsHigh <= 1024 {
            image = NSImage(data: bytes)
            image?.size = NSSize(width: 14, height: 14)
        }
        completion(image)
        session.invalidateAndCancel()
        self.session = nil
    }
}
