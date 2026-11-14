import XCTest
@testable import NagiApp

final class StatusRenderCacheTests: XCTestCase {
    func testFirstRenderAlwaysDraws() {
        var cache = StatusRenderCache()
        XCTAssertTrue(cache.imageChanged(rates: nil, opacity: 1))
        var other = StatusRenderCache()
        XCTAssertTrue(other.labelChanged(""))
    }

    func testUnchangedImageInputsSkipRedraw() {
        var cache = StatusRenderCache()
        XCTAssertTrue(cache.imageChanged(rates: "  0 B/s\n  0 B/s", opacity: 1))
        XCTAssertFalse(cache.imageChanged(rates: "  0 B/s\n  0 B/s", opacity: 1))
        XCTAssertFalse(cache.imageChanged(rates: "  0 B/s\n  0 B/s", opacity: 1))
    }

    func testRateOrOpacityChangeRedrawsAndStaleRatesFallBackToZero() {
        var cache = StatusRenderCache()
        XCTAssertTrue(cache.imageChanged(rates: "1.2 KB/s\n  0 B/s", opacity: 1))
        XCTAssertTrue(cache.imageChanged(rates: "  0 B/s\n  0 B/s", opacity: 1))
        XCTAssertTrue(cache.imageChanged(rates: "  0 B/s\n  0 B/s", opacity: 0.5))
        XCTAssertFalse(cache.imageChanged(rates: "  0 B/s\n  0 B/s", opacity: 0.5))
    }

    func testDisplayModeSwitchBetweenIconAndRatesRedraws() {
        var cache = StatusRenderCache()
        XCTAssertTrue(cache.imageChanged(rates: nil, opacity: 1))
        XCTAssertFalse(cache.imageChanged(rates: nil, opacity: 1))
        XCTAssertTrue(cache.imageChanged(rates: "  0 B/s\n  0 B/s", opacity: 1))
        XCTAssertTrue(cache.imageChanged(rates: nil, opacity: 1))
    }

    func testImageAndLabelAreTrackedIndependently() {
        var cache = StatusRenderCache()
        XCTAssertTrue(cache.imageChanged(rates: nil, opacity: 1))
        XCTAssertTrue(cache.labelChanged("Nagi, running"))
        XCTAssertFalse(cache.labelChanged("Nagi, running"))
        XCTAssertFalse(cache.imageChanged(rates: nil, opacity: 1))
        XCTAssertTrue(cache.labelChanged("Nagi, traffic unavailable"))
        XCTAssertFalse(cache.imageChanged(rates: nil, opacity: 1))
    }
}
