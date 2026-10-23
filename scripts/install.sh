#!/usr/bin/env bash
# Download a published Nagi release from GitHub and hand it to installer.sh.
# This script intentionally contains no installation policy beyond downloading
# and verifying an archive; the bundled scripts/installer.sh remains the
# transaction owner.
set -euo pipefail

usage() {
    cat <<'USAGE'
Usage: scripts/install.sh [install|update] [--app|--no-app] [--version TAG]
                          [--repo OWNER/REPOSITORY] [--prefix DIR]

With no action, install is selected when Nagi is absent and update otherwise.
The macOS App is not installed by default. Use --app to install it on macOS.
--no-app is accepted for symmetry and keeps an already installed App on update.
USAGE
}

fail() { printf 'nagi online installer: %s\n' "$*" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || fail 'curl is required'
command -v tar >/dev/null 2>&1 || fail 'tar is required'
command -v shasum >/dev/null 2>&1 || fail 'shasum is required'

repo=${NAGI_REPOSITORY:-ronigooja/Nagi}
version=
prefix=/usr/local
action=
app_mode=default
while (( $# )); do
    case "$1" in
        install|update)
            [[ -z "$action" ]] || fail 'install/update may be specified only once'
            action=$1
            shift
            ;;
        --app)
            [[ "$app_mode" != no ]] || fail '--app and --no-app are mutually exclusive'
            app_mode=app
            shift
            ;;
        --no-app)
            [[ "$app_mode" != app ]] || fail '--app and --no-app are mutually exclusive'
            app_mode=no
            shift
            ;;
        --version)
            [[ $# -ge 2 ]] || fail 'missing value for --version'
            version=$2
            shift 2
            ;;
        --repo)
            [[ $# -ge 2 ]] || fail 'missing value for --repo'
            repo=$2
            shift 2
            ;;
        --prefix)
            [[ $# -ge 2 ]] || fail 'missing value for --prefix'
            prefix=$2
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            fail "unknown option: $1"
            ;;
    esac
done
[[ "$repo" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || fail 'repository must be OWNER/REPOSITORY'
[[ "$prefix" == /* && "$prefix" != / && "$prefix" != */../* && "$prefix" != */./* && "$prefix" != */.. && "$prefix" != */. ]] || fail '--prefix must be an absolute, normalized directory'
prefix=${prefix%/}

platform=$(uname -s)
machine=$(uname -m)
case "$platform/$machine" in
    Linux/x86_64|Linux/amd64) target=linux-amd64 ;;
    Linux/aarch64|Linux/arm64) target=linux-arm64 ;;
    Darwin/x86_64|Darwin/amd64) target=macos-amd64 ;;
    Darwin/arm64) target=macos-arm64 ;;
    *) fail "unsupported platform: $platform/$machine" ;;
esac
if [[ "$app_mode" == app && "$platform" != Darwin ]]; then
    fail '--app is supported only on macOS'
fi

state="$prefix/share/nagi/install-state"

asset="nagi-$target.tar.gz"
if [[ -n "$version" ]]; then
    [[ "$version" == v* ]] || version="v$version"
    base="https://github.com/$repo/releases/download/$version"
else
    base="https://github.com/$repo/releases/latest/download"
fi
base=${NAGI_RELEASE_BASE:-$base}

download() {
    local url=$1 destination=$2
    curl --fail --location --silent --show-error --connect-timeout 10 --speed-time 10 --speed-limit 1 \
        --output "$destination" "$url"
}

download_release() {
    printf 'Downloading %s from %s...\n' "$asset" "$1" >&2
    download "$1/$asset" "$archive" || return $?
    printf 'Downloading SHA256SUMS...\n' >&2
    download "$1/SHA256SUMS" "$checksums"
}

temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/nagi-online.XXXXXXXX")
temp_dir=$(cd "$temp_dir" && pwd -P)
cleanup() { rm -rf -- "$temp_dir"; }
trap cleanup EXIT
archive="$temp_dir/$asset"
checksums="$temp_dir/SHA256SUMS"
printf 'Downloading Nagi %s for %s...\n' "${version:-latest}" "$target"
if ! download_release "$base"; then
    # Explicit download sources remain exclusive. Retry the whole release pair
    # so an interrupted download cannot leave a partial file for verification.
    if [[ -z ${NAGI_RELEASE_BASE:-} && "$base" == https://github.com/* ]]; then
        printf 'GitHub download failed; retrying through gh-proxy.com...\n' >&2
        download_release "https://gh-proxy.com/$base" || fail 'release download failed through GitHub and gh-proxy.com'
    else
        fail 'release download failed'
    fi
fi
printf 'Verifying archive checksum...\n' >&2
expected=$(awk -v file="$asset" '$2 == file || $2 == "*" file { print $1; exit }' "$checksums")
[[ "$expected" =~ ^[0-9a-fA-F]{64}$ ]] || fail "SHA256SUMS has no entry for $asset"
actual=$(shasum -a 256 "$archive" | awk '{print $1}')
# macOS ships Bash 3.2, which does not support ${value,,}.
actual=$(printf '%s' "$actual" | tr 'A-F' 'a-f')
expected=$(printf '%s' "$expected" | tr 'A-F' 'a-f')
[[ "$actual" == "$expected" ]] || fail 'release archive checksum mismatch'

artifact_dir="$temp_dir/artifact"
printf 'Extracting release archive...\n' >&2
mkdir -p "$artifact_dir"
tar -xzf "$archive" -C "$artifact_dir"
[[ -f "$artifact_dir/nagi" && -x "$artifact_dir/nagi" ]] || fail 'release archive has no executable nagi'
[[ -f "$artifact_dir/mihomo" && -x "$artifact_dir/mihomo" ]] || fail 'release archive has no executable mihomo'
[[ -f "$artifact_dir/installer.sh" && -x "$artifact_dir/installer.sh" ]] || fail 'release archive has no installer.sh'

# Restore interrupted file changes before consulting the installation record.
# The verified artifact provides a helper even when the installed CLI is absent.
"$artifact_dir/installer.sh" recover --artifact-dir "$artifact_dir" --prefix "$prefix"
if [[ -z "$action" ]]; then
    if [[ -f "$state" ]]; then action=update; else action=install; fi
fi
if [[ "$action" == update && ! -f "$state" ]]; then
    fail "Nagi is not installed at $prefix; use install"
fi

# Releases may contain the App for macOS. A fresh CLI install does not opt in
# to it; an update of an App installation keeps it unless explicitly removed
# by the separate local installer workflow.
if [[ "$platform" == Darwin && -d "$artifact_dir/Nagi.app" ]]; then
    keep_app=0
    if [[ "$app_mode" == app ]]; then
        keep_app=1
    elif [[ "$action" == update && -f "$state" && $(sed -n '2p' "$state") != 'app=' ]]; then
        keep_app=1
    fi
    (( keep_app )) || rm -rf -- "$artifact_dir/Nagi.app"
elif [[ "$app_mode" == app ]]; then
    fail 'the selected macOS release does not contain Nagi.app'
fi

printf 'Applying installation changes...\n' >&2
exec "$artifact_dir/installer.sh" "$action" --artifact-dir "$artifact_dir" --prefix "$prefix"
