import AppKit
import XCTest
@testable import NagiApp

@MainActor final class NativeWindowLayoutTests: XCTestCase {
    func testInitialAndMinimumFramesFitSmallAndOffsetScreens() {
        for layout in [ConnectionsWindowController.layout, CaptureDraftWindowController.layout] {
            for screen in [NSRect(x: 0, y: 40, width: 1280, height: 720),
                           NSRect(x: -1024, y: 200, width: 1024, height: 640),
                           NSRect(x: 1920, y: -720, width: 960, height: 600),
                           NSRect(x: 0, y: 0, width: 2560, height: 1440),
                           NSRect(x: -1024, y: 63, width: 1024, height: 674)] {
                let frame = layout.initialFrame(in: screen)
                let minimum = layout.minimumFrameSize(in: screen)
                XCTAssertTrue(screen.contains(frame))
                XCTAssertLessThan(frame.width, screen.width)
                XCTAssertLessThan(frame.height, screen.height)
                XCTAssertLessThanOrEqual(frame.width, layout.preferredSize.width)
                XCTAssertLessThanOrEqual(frame.height, layout.preferredSize.height)
                // Keep the fractional policy exact; only realized AppKit frames
                // receive rounding allowance in the mounted-window tests.
                XCTAssertLessThanOrEqual(frame.width, screen.width * 0.9)
                XCTAssertLessThanOrEqual(frame.height, screen.height * 0.9)
                XCTAssertLessThanOrEqual(minimum.width, screen.width * 0.8)
                XCTAssertLessThanOrEqual(minimum.height, screen.height * 0.8)
                XCTAssertLessThan(minimum.width, frame.width)
                XCTAssertLessThan(minimum.height, frame.height)
                XCTAssertEqual(frame.midX, screen.midX, accuracy: 1)
                XCTAssertEqual(frame.midY, screen.midY, accuracy: 1)
            }
        }
    }
}
