#!/usr/bin/env bash
# Install locally produced or downloaded Nagi artifacts without knowing how they were built.
set -euo pipefail

usage() {
    cat <<'USAGE'
Usage: scripts/install.sh install|update|uninstall [--artifact-dir DIR] [--prefix DIR] [--app-dest DIR]
  install/update require --artifact-dir containing executable nagi and mihomo;
  on macOS it may also contain Nagi.app. The default prefix is /usr/local.
  App installation requires --prefix /usr/local; its default destination is
  /Applications/Nagi.app. Install and update replace the default profile.
  No network access is performed.
USAGE
}
fail() { printf 'nagi installer: %s\n' "$*" >&2; exit 1; }
[[ $# -gt 0 ]] || { usage >&2; exit 2; }
action=$1
shift
case "$action" in install|update|uninstall) ;; *) usage >&2; exit 2 ;; esac
artifact_dir=
prefix=/usr/local
app_dest=/Applications/Nagi.app
app_dest_given=0
while (( $# )); do
    case "$1" in
        --artifact-dir|--prefix|--app-dest)
            [[ $# -ge 2 ]] || fail "missing value for $1"
            case "$1" in
                --artifact-dir) artifact_dir=$2 ;;
                --prefix) prefix=$2 ;;
                --app-dest) app_dest=$2; app_dest_given=1 ;;
            esac
            shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) fail "unknown option: $1" ;;
    esac
done
valid_path() {
    [[ "$1" == /* && "$1" != / && "$1" != */../* && "$1" != */./* && "$1" != */.. && "$1" != */. && "$1" != *$'\n'* && "$1" != *$'\r'* ]]
}
valid_path "$prefix" || fail "--prefix must be an absolute, normalized directory"
prefix=${prefix%/}
valid_path "$app_dest" || fail "--app-dest must be an absolute, normalized path"
[[ "$app_dest" == */Nagi.app ]] || fail "--app-dest must end in Nagi.app"
[[ "${SUDO_USER:-}" == "" ]] || fail "run as the intended user, not with sudo; the installer elevates file operations when needed"

bin_dir=$prefix/bin
state_dir=$prefix/share/nagi
state=$state_dir/install-state
nagi=$bin_dir/nagi
mihomo=$bin_dir/mihomo
platform=$(uname -s)
if [[ "$platform" == Darwin ]]; then
    profile_dir=$HOME/Library/Application\ Support/Nagi/config/profiles
else
    config_home=${XDG_CONFIG_HOME:-$HOME/.config}
    [[ "$config_home" == /* ]] || config_home=$HOME/.config
    profile_dir=$config_home/nagi/profiles
fi
default_profile=$profile_dir/default.yaml
source_app=
if [[ "$action" != uninstall ]]; then
    [[ -n "$artifact_dir" ]] || fail "--artifact-dir is required for $action"
    [[ -d "$artifact_dir" ]] || fail "artifact directory does not exist: $artifact_dir"
    artifact_dir=$(cd "$artifact_dir" && pwd -P)
    for name in nagi mihomo; do
        [[ -f "$artifact_dir/$name" && -x "$artifact_dir/$name" && ! -L "$artifact_dir/$name" ]] || fail "missing regular executable: $artifact_dir/$name"
    done
    version=$(NAGI_MIHOMO_BIN="$artifact_dir/mihomo" "$artifact_dir/nagi" --json version) || fail "artifact nagi cannot report its version on this host"
    [[ "$version" == *'"ok":true'* && "$version" == *'"mihomo_commit"'* && "$version" == *'"nagi"'* ]] || fail "artifact nagi returned an invalid version response"
    "$artifact_dir/mihomo" -v >/dev/null || fail "artifact mihomo cannot run on this host"
    "$artifact_dir/nagi" __default-profile >/dev/null || fail "artifact nagi cannot provide its default profile"
    if [[ -e "$artifact_dir/Nagi.app" || -L "$artifact_dir/Nagi.app" ]]; then
        [[ "$platform" == Darwin && -d "$artifact_dir/Nagi.app" && ! -L "$artifact_dir/Nagi.app" ]] || fail "Nagi.app must be an ordinary directory on macOS"
        [[ -f "$artifact_dir/Nagi.app/Contents/Info.plist" && -x "$artifact_dir/Nagi.app/Contents/MacOS/NagiApp" ]] || fail "Nagi.app is incomplete"
        [[ "$prefix" == /usr/local ]] || fail "Nagi.app requires --prefix /usr/local because it uses /usr/local/bin/nagi"
        source_app=$artifact_dir/Nagi.app
    fi
    [[ $app_dest_given -eq 0 || -n "$source_app" ]] || fail "--app-dest requires Nagi.app in the artifact directory"
elif [[ -n "$artifact_dir" ]]; then
    fail "--artifact-dir is only used for install or update"
fi

exists() { [[ -e "$1" || -L "$1" ]]; }
command -v shasum >/dev/null || fail "shasum is required to record and verify installed files"
hash_file() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
valid_hash() { [[ "$1" =~ ^[0-9a-f]{64}$ ]]; }
verify_hash() {
    [[ -f "$1" && ! -L "$1" && "$(hash_file "$1")" == "$2" ]] || fail "installed file changed outside the installer: $1"
}
app_hash() {
    local root=$1 entry
    (
        cd "$root"
        # Sorting lines is portable across Linux and macOS. Reject filenames that
        # could make the sorted digest ambiguous before using that representation.
        while IFS= read -r -d '' entry; do
            [[ "$entry" != *$'\n'* && "$entry" != *$'\r'* ]] || fail "App contains an unsupported newline in a path"
        done < <(find . -print0)
        find . -print | LC_ALL=C sort | while IFS= read -r entry; do
            if [[ -L "$entry" ]]; then
                printf 'link %s %s\n' "$entry" "$(readlink "$entry")"
            elif [[ -f "$entry" ]]; then
                printf 'file %s %s\n' "$entry" "$(hash_file "$entry")"
            elif [[ -d "$entry" ]]; then
                printf 'dir %s\n' "$entry"
            else
                fail "unsupported App entry: $entry"
            fi
        done
    ) | shasum -a 256 | cut -d ' ' -f 1
}
[[ ! -L "$bin_dir" && ! -L "$state_dir" && ! -L "$state" && ! -L "$nagi" && ! -L "$mihomo" ]] || fail "installation paths must not be symbolic links"
if [[ "$action" != uninstall ]]; then
    [[ ! -L "$profile_dir" && ! -L "$default_profile" ]] || fail "default profile paths must not be symbolic links"
    [[ ! -e "$default_profile" || -f "$default_profile" ]] || fail "default profile is not a regular file: $default_profile"
fi
# Refuse an unrecognized existing installation rather than removing someone else's files.
if [[ "$action" == install ]]; then
    ! exists "$state" || fail "already installed; use update"
    if exists "$nagi" || exists "$mihomo"; then fail "destination contains unmanaged executables: $bin_dir"; fi
    if [[ -n "$source_app" ]]; then
        ! exists "$app_dest" || fail "App destination already exists: $app_dest"
    fi
else
    if ! exists "$state"; then
        if [[ "$action" == uninstall && ! -e "$nagi" && ! -e "$mihomo" ]]; then
            printf 'Nagi is not installed at %s\n' "$prefix"
            exit 0
        fi
        fail "no installer record at $state; refusing to change unmanaged files"
    fi
    [[ -f "$state" && "$(sed -n '1p' "$state")" == 'nagi-installer-v3' ]] || fail "invalid installer record: $state"
    recorded_app=$(sed -n '2s/^app=//p' "$state")
    [[ $(wc -l < "$state") -eq 5 && $(sed -n '2p' "$state") == app=* ]] || fail "invalid installer record: $state"
    recorded_nagi=$(sed -n '3s/^nagi_sha256=//p' "$state")
    recorded_mihomo=$(sed -n '4s/^mihomo_sha256=//p' "$state")
    recorded_app_hash=$(sed -n '5s/^app_sha256=//p' "$state")
    if ! valid_hash "$recorded_nagi" || ! valid_hash "$recorded_mihomo"; then fail "invalid installer record: $state"; fi
    verify_hash "$nagi" "$recorded_nagi"
    verify_hash "$mihomo" "$recorded_mihomo"
    if [[ -n "$recorded_app" ]]; then
        if ! valid_path "$recorded_app" || [[ "$recorded_app" != */Nagi.app ]]; then fail "invalid App path in installer record"; fi
        [[ "$platform" == Darwin ]] || fail "App installation record is only supported on macOS"
        if (( app_dest_given )); then
            [[ "$app_dest" == "$recorded_app" ]] || fail "--app-dest differs from installed App path: $recorded_app"
        fi
        app_dest=$recorded_app
        valid_hash "$recorded_app_hash" || fail "invalid installer record: $state"
        [[ -d "$app_dest" && ! -L "$app_dest" && "$(app_hash "$app_dest")" == "$recorded_app_hash" ]] || fail "installed App changed outside the installer: $app_dest"
    elif (( app_dest_given )); then
        fail "--app-dest specified but no App is installed"
    else
        [[ -z "$recorded_app_hash" ]] || fail "invalid installer record: $state"
    fi
    if [[ "$action" == update ]]; then
        [[ -f "$nagi" && -x "$nagi" && -f "$mihomo" && -x "$mihomo" ]] || fail "installed binaries are missing; repair the installation manually"
        if [[ -n "$recorded_app" || -n "$source_app" ]]; then
            [[ -n "$recorded_app" && -n "$source_app" ]] || fail "App must be present in both the existing installation and update artifact"
            [[ -d "$app_dest" && ! -L "$app_dest" ]] || fail "installed App is missing or is a symbolic link"
        fi
    fi
    # A user service records the exact executable path. Never replace it under a
    # running monitor or mihomo process; the user can resume them afterward.
    [[ -f "$nagi" && -x "$nagi" ]] || fail "installed nagi is missing; cannot check running state"
    status=$("$nagi" --json status) || fail "cannot inspect running engine; stop it and retry"
    [[ "$status" == *'"ok":true'* ]] || fail "cannot inspect running engine"
    [[ "$status" != *'"running":true'* ]] || fail "mihomo is running; run '$nagi quit' and retry"
    service=$("$nagi" --json service status) || fail "cannot inspect login service; disable it and retry"
    [[ "$service" == *'"ok":true'* ]] || fail "cannot inspect login service"
    [[ "$service" != *'"active":true'* ]] || fail "login service is active; run '$nagi startup disable' and retry"
    [[ "$service" != *'"installed":true'* ]] || fail "login service is installed; run '$nagi service uninstall' before changing its executable"
    if [[ "$platform" == Darwin ]]; then
        helper=$("$nagi" --json privileged-helper status) || fail "cannot inspect privileged helper; stop it and retry"
        [[ "$helper" != *'"installed":true'* && "$helper" != *'"active":true'* ]] || fail "privileged helper is installed; disable and uninstall it before changing its executable"
        if [[ -n "$recorded_app" ]] && pgrep -x NagiApp >/dev/null 2>&1; then
            fail "quit Nagi.app before updating or uninstalling it"
        fi
    fi
fi

# Validate permissions before making any changes; only file operations are elevated.
needs_sudo=0
nearest_parent() {
    local path=$1
    while [[ ! -e "$path" ]]; do path=$(dirname "$path"); done
    printf '%s\n' "$path"
}
[[ -w "$(nearest_parent "$bin_dir")" && -w "$(nearest_parent "$state_dir")" ]] || needs_sudo=1
if [[ -n "$source_app" || ( "$action" != install && -n "${recorded_app:-}" ) ]]; then
    [[ -w "$(nearest_parent "$(dirname "$app_dest")")" ]] || needs_sudo=1
    [[ ! -L "$app_dest" ]] || fail "App destination must not be a symbolic link"
fi
if (( needs_sudo )) && (( EUID != 0 )); then
    command -v sudo >/dev/null || fail "sudo is required to write the installation destination"
    sudo -v || fail "administrator authorization failed"
fi
write_fs() {
    if (( needs_sudo )) && (( EUID != 0 )); then sudo "$@"; else "$@"; fi
}
remove_fs() {
    if exists "$1"; then
        if [[ -d "$1" && ! -L "$1" ]]; then write_fs rm -R -- "$1"; else write_fs rm -- "$1"; fi
    fi
}

write_fs mkdir -p -- "$bin_dir" "$state_dir"
stage=$(write_fs mktemp -d "$prefix/.nagi-install.XXXXXXXX")
app_stage=
committed=0
changed=0
old_nagi=0
old_mihomo=0
old_state=0
old_app=0
new_nagi=0
new_mihomo=0
new_state=0
new_app=0
old_profile=0
new_profile=0
profile_stage=
cleanup() {
    local result=$?
    trap - EXIT
    local rollback_failed=0
    if (( ! committed && changed )); then
        printf 'nagi installer: operation failed; restoring previous files\n' >&2
        if (( new_app )); then remove_fs "$app_dest" || rollback_failed=1; fi
        if (( old_app )); then write_fs mv -- "$app_stage/old-app" "$app_dest" || rollback_failed=1; fi
        if (( new_state )); then remove_fs "$state" || rollback_failed=1; fi
        if (( old_state )); then write_fs mv -- "$stage/old-state" "$state" || rollback_failed=1; fi
        if (( new_nagi )); then remove_fs "$nagi" || rollback_failed=1; fi
        if (( old_nagi )); then write_fs mv -- "$stage/old-nagi" "$nagi" || rollback_failed=1; fi
        if (( new_mihomo )); then remove_fs "$mihomo" || rollback_failed=1; fi
        if (( old_mihomo )); then write_fs mv -- "$stage/old-mihomo" "$mihomo" || rollback_failed=1; fi
        if (( new_profile )); then rm -- "$default_profile" || rollback_failed=1; fi
        if (( old_profile )); then mv -- "$profile_stage/old-default.yaml" "$default_profile" || rollback_failed=1; fi
    fi
    if (( rollback_failed )); then
        printf 'nagi installer: rollback incomplete; staged backups retained at %s %s %s\n' "$stage" "$app_stage" "$profile_stage" >&2
        exit 1
    fi
    [[ -z "$app_stage" ]] || remove_fs "$app_stage" || true
    [[ -z "$profile_stage" ]] || rm -rf -- "$profile_stage" || true
    remove_fs "$stage" || true
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT TERM
if [[ -n "$source_app" || -n "${recorded_app:-}" ]]; then
    write_fs mkdir -p -- "$(dirname "$app_dest")"
    app_stage=$(write_fs mktemp -d "$(dirname "$app_dest")/.nagi-app.XXXXXXXX")
fi
if [[ "$action" != uninstall ]]; then
    mkdir -p -m 0700 -- "$profile_dir"
    profile_stage=$(mktemp -d "$profile_dir/.nagi-install.XXXXXXXX")
    "$artifact_dir/nagi" __default-profile > "$profile_stage/new-default.yaml" || fail "artifact nagi cannot provide its default profile"
    chmod 0600 "$profile_stage/new-default.yaml"
    write_fs install -m 0755 -- "$artifact_dir/nagi" "$stage/new-nagi"
    write_fs install -m 0755 -- "$artifact_dir/mihomo" "$stage/new-mihomo"
    if [[ -n "$source_app" ]]; then write_fs cp -R -- "$source_app" "$app_stage/new-app"; fi
    new_app_hash=
    if [[ -n "$source_app" ]]; then new_app_hash=$(app_hash "$app_stage/new-app"); fi
    printf 'nagi-installer-v3\napp=%s\nnagi_sha256=%s\nmihomo_sha256=%s\napp_sha256=%s\n' \
        "${source_app:+$app_dest}" "$(hash_file "$stage/new-nagi")" "$(hash_file "$stage/new-mihomo")" \
        "$new_app_hash" | write_fs tee "$stage/new-state" >/dev/null
    write_fs chmod 0644 "$stage/new-state"
fi
if [[ "$action" != install ]]; then
    if exists "$nagi"; then write_fs mv -- "$nagi" "$stage/old-nagi"; old_nagi=1; changed=1; fi
    if exists "$mihomo"; then write_fs mv -- "$mihomo" "$stage/old-mihomo"; old_mihomo=1; changed=1; fi
    if [[ -n "${recorded_app:-}" ]] && exists "$app_dest"; then write_fs mv -- "$app_dest" "$app_stage/old-app"; old_app=1; changed=1; fi
    write_fs mv -- "$state" "$stage/old-state"; old_state=1; changed=1
fi
if [[ "$action" != uninstall ]]; then
    write_fs mv -- "$stage/new-mihomo" "$mihomo"; new_mihomo=1; changed=1
    write_fs mv -- "$stage/new-nagi" "$nagi"; new_nagi=1; changed=1
    if [[ -n "$source_app" ]]; then write_fs mv -- "$app_stage/new-app" "$app_dest"; new_app=1; changed=1; fi
    NAGI_MIHOMO_BIN="$mihomo" "$nagi" --json version >/dev/null || fail "installed nagi failed its version check"
    if exists "$default_profile"; then mv -- "$default_profile" "$profile_stage/old-default.yaml"; old_profile=1; changed=1; fi
    mv -- "$profile_stage/new-default.yaml" "$default_profile"; new_profile=1; changed=1
    write_fs mv -- "$stage/new-state" "$state"; new_state=1; changed=1
fi
committed=1
printf 'Nagi %s complete at %s\n' "$action" "$prefix"
