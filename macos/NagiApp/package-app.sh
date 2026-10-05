#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
swift build --package-path "$project_dir" -c release
bin_dir=$(swift build --package-path "$project_dir" -c release --show-bin-path)
app_dir="$project_dir/dist/Nagi.app"
mkdir -p "$app_dir/Contents/MacOS"
cp "$bin_dir/NagiApp" "$app_dir/Contents/MacOS/NagiApp"
cat > "$app_dir/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleDevelopmentRegion</key><string>en</string>
  <key>CFBundleExecutable</key><string>NagiApp</string>
  <key>CFBundleIdentifier</key><string>com.nagi.menuapp</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleName</key><string>Nagi</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSUIElement</key><true/>
</dict></plist>
PLIST
echo "$app_dir"
