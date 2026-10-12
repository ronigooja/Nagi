#!/bin/sh
set -eu

report_failure() {
    status=$?
    if [ "$status" -ne 0 ]; then
        printf 'Nagi App build failed\n' >&2
    fi
}
trap report_failure EXIT

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$project_dir/../.." && pwd)
source_version=$(git -C "$repo_dir" describe --tags --always --dirty)
app_version=$(printf '%s' "${source_version#v}" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g')
build_version=$(git -C "$repo_dir" rev-list --count HEAD)
swift build --package-path "$project_dir" -c release
bin_dir=$(swift build --package-path "$project_dir" -c release --show-bin-path)
app_dir="$repo_dir/bin/Nagi.app"
rm -rf -- "$app_dir"
mkdir -p "$app_dir/Contents/MacOS" "$app_dir/Contents/Resources"
cp "$bin_dir/NagiApp" "$app_dir/Contents/MacOS/NagiApp"
cp -R "$bin_dir/NagiApp_NagiApp.bundle" "$app_dir/Contents/Resources/"
cp "$project_dir/Resources/Nagi.icns" "$app_dir/Contents/Resources/Nagi.icns"
cat > "$app_dir/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleDevelopmentRegion</key><string>en</string>
  <key>CFBundleExecutable</key><string>NagiApp</string>
  <key>CFBundleIdentifier</key><string>com.nagi.menuapp</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleName</key><string>Nagi</string>
  <key>CFBundleIconFile</key><string>Nagi.icns</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$app_version</string>
  <key>CFBundleVersion</key><string>$build_version</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSUIElement</key><true/>
  <key>NSAppTransportSecurity</key><dict>
    <key>NSAllowsArbitraryLoads</key><true/>
  </dict>
</dict></plist>
PLIST
printf 'Nagi App build succeeded: %s\n' "$app_dir"
