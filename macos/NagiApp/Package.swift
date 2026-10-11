// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "NagiApp",
    platforms: [.macOS(.v13)],
    products: [.executable(name: "NagiApp", targets: ["NagiApp"])],
    targets: [
        .executableTarget(name: "NagiApp", resources: [.copy("Resources/NagiMenuTemplate.pdf")]),
        .testTarget(name: "NagiAppTests", dependencies: ["NagiApp"])
    ]
)
