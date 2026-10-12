#!/usr/bin/env bash
# Install locally produced or downloaded Nagi artifacts without knowing how they were built.
set -euo pipefail

usage() {
    cat <<'USAGE'
Usage: scripts/installer.sh install|update|uninstall|app-install|app-uninstall [--artifact-dir DIR] [--prefix DIR] [--app-dest DIR] [--purge-data]
  install/update use bin/ beside this script by default, containing nagi and mihomo;
  on macOS it may also contain Nagi.app. The default prefix is /usr/local.
  App installation requires --prefix /usr/local; its default destination is
  /Applications/Nagi.app. Install and update replace the default profile.
  On macOS with the default prefix, install selects the privileged TUN helper;
  update reinstalls it and preserves any existing backend selection. Uninstall
  removes it. No network access is performed.
  app-install/app-uninstall change only the App and its installation record.
  uninstall preserves user data unless --purge-data is specified.
USAGE
}
fail() { printf 'nagi installer: %s\n' "$*" >&2; exit 1; }
purge_fail() { printf 'NAGI_PURGE_DATA_INCOMPLETE: %s\n' "$*" >&2; exit 3; }
[[ $# -gt 0 ]] || { usage >&2; exit 2; }
action=$1
shift
app_only=
case "$action" in
    install|update|uninstall) ;;
    app-install) app_only=install; action=update ;;
    app-uninstall) app_only=uninstall; action=uninstall ;;
    *) usage >&2; exit 2 ;;
esac
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
artifact_dir=$script_dir/../bin
artifact_dir_given=0
prefix=/usr/local
app_dest=/Applications/Nagi.app
app_dest_given=0
verify_only=0
purge_data=0
while (( $# )); do
    case "$1" in
        --artifact-dir|--prefix|--app-dest)
            [[ $# -ge 2 ]] || fail "missing value for $1"
            case "$1" in
                --artifact-dir) artifact_dir=$2; artifact_dir_given=1 ;;
                --prefix) prefix=$2 ;;
                --app-dest) app_dest=$2; app_dest_given=1 ;;
            esac
            shift 2 ;;
        --verify) verify_only=1; shift ;;
        --purge-data) purge_data=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) fail "unknown option: $1" ;;
    esac
done
if (( purge_data )) && [[ "$action" != uninstall || -n "$app_only" || verify_only -eq 1 ]]; then
    fail "--purge-data is valid only for a whole uninstall"
fi
valid_path() {
    [[ "$1" == /* && "$1" != / && "$1" != */../* && "$1" != */./* && "$1" != */.. && "$1" != */. && "$1" != *$'\n'* && "$1" != *$'\r'* ]]
}
xdg_home() {
    local value=$1 fallback=$2
    if valid_path "$value" && [[ "$value" != "$HOME" ]]; then
        printf '%s\n' "$value"
    else
        printf '%s\n' "$fallback"
    fi
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
manage_helper=0
if [[ "$platform" == Darwin && "$prefix" == /usr/local ]]; then manage_helper=1; fi
if [[ -n "$app_only" ]]; then
    [[ "$platform" == Darwin ]] || fail 'App management is supported only on macOS'
    manage_helper=0
fi
if (( manage_helper && EUID == 0 )); then fail "run as the intended macOS user, not root; the installer uses sudo for the helper"; fi
if [[ "$platform" == Darwin ]]; then
    user_data_targets=("$HOME/Library/Application Support/Nagi")
    user_cache="$HOME/Library/Caches/Nagi"
    profile_dir=$HOME/Library/Application\ Support/Nagi/config/profiles
else
    config_home=$(xdg_home "${XDG_CONFIG_HOME:-}" "$HOME/.config")
    data_home=$(xdg_home "${XDG_DATA_HOME:-}" "$HOME/.local/share")
    user_state_home=$(xdg_home "${XDG_STATE_HOME:-}" "$HOME/.local/state")
    cache_home=$(xdg_home "${XDG_CACHE_HOME:-}" "$HOME/.cache")
    user_data_targets=("$config_home/nagi" "$data_home/nagi" "$user_state_home/nagi")
    if valid_path "${XDG_RUNTIME_DIR:-}" && [[ "$XDG_RUNTIME_DIR" != "$HOME" ]]; then
        user_data_targets+=("$XDG_RUNTIME_DIR/nagi")
    fi
    user_cache="$cache_home/Nagi"
    profile_dir=$config_home/nagi/profiles
fi
user_data_targets+=("$user_cache")
default_profile=$profile_dir/default.yaml
source_app=
if [[ "$action" != uninstall ]]; then
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
elif (( artifact_dir_given )); then
    fail "--artifact-dir is only used for install or update"
fi

exists() { [[ -e "$1" || -L "$1" ]]; }
orphan_removed=0
helper_was_installed=0
helper_was_privileged=0
remove_orphan_helper() {
    local helper_cli=$1 helper_status
    if [[ ! -f "$helper_cli" || ! -x "$helper_cli" || -L "$helper_cli" ]]; then
        if [[ -e /Library/LaunchDaemons/io.nagi.privileged-helper.plist || -L /Library/LaunchDaemons/io.nagi.privileged-helper.plist ]] || /bin/launchctl print system/io.nagi.privileged-helper >/dev/null 2>&1; then
            fail "build bin/nagi before removing an orphaned privileged helper"
        fi
        return 0
    fi
    helper_status=$("$helper_cli" --json privileged-helper status) || fail "cannot inspect privileged helper"
    [[ "$helper_status" == *'"ok":true'* ]] || fail "cannot inspect privileged helper"
    if [[ "$helper_status" == *'"installed":true'* || "$helper_status" == *'"active":true'* ]]; then
        command -v sudo >/dev/null || fail "sudo is required to remove the orphaned privileged helper"
        sudo -n true 2>/dev/null || sudo -v || fail "administrator authorization failed"
        if [[ "$helper_status" == *'"selected_backend":"privileged"'* ]]; then
            "$helper_cli" privileged-helper disable >/dev/null || fail "cannot select the user backend before removing the orphaned privileged helper"
        fi
        sudo "$helper_cli" privileged-helper uninstall >/dev/null || fail "cannot remove orphaned privileged helper"
        orphan_removed=1
    fi
}
valid_hash() { [[ "$1" =~ ^[0-9a-f]{64}$ ]]; }
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
    if (( manage_helper )); then
        helper=$("$artifact_dir/nagi" --json privileged-helper status) || fail "cannot inspect privileged helper"
        [[ "$helper" == *'"ok":true'* ]] || fail "cannot inspect privileged helper"
    fi
else
    if ! exists "$state"; then
        if [[ "$action" == uninstall && ! -e "$nagi" && ! -e "$mihomo" ]]; then
            if (( manage_helper )); then remove_orphan_helper "$script_dir/../bin/nagi"; fi
            if (( orphan_removed )); then
                printf 'Nagi uninstall complete at %s\n' "$prefix"
            else
                printf 'Nagi is not installed at %s\n' "$prefix"
            fi
            exit 0
        fi
        fail "no installer record at $state; refusing to change unmanaged files"
    fi
    [[ -f "$state" ]] || fail "invalid installer record: $state"
    record_version=$(sed -n '1p' "$state")
    recorded_app=$(sed -n '2s/^app=//p' "$state")
    [[ $(sed -n '2p' "$state") == app=* ]] || fail "invalid installer record: $state"
    case "$record_version" in
        nagi-installer-v4)
            [[ $(wc -l < "$state") -eq 2 ]] || fail "invalid installer record: $state" ;;
        nagi-installer-v3)
            [[ $(wc -l < "$state") -eq 5 ]] || fail "invalid installer record: $state"
            recorded_nagi=$(sed -n '3s/^nagi_sha256=//p' "$state")
            recorded_mihomo=$(sed -n '4s/^mihomo_sha256=//p' "$state")
            recorded_app_hash=$(sed -n '5s/^app_sha256=//p' "$state")
            valid_hash "$recorded_nagi" && valid_hash "$recorded_mihomo" || fail "invalid installer record: $state" ;;
        *) fail "invalid installer record: $state" ;;
    esac
    if [[ -n "$recorded_app" ]]; then
        if ! valid_path "$recorded_app" || [[ "$recorded_app" != */Nagi.app ]]; then fail "invalid App path in installer record"; fi
        [[ "$platform" == Darwin ]] || fail "App installation record is only supported on macOS"
        if (( app_dest_given )); then
            [[ "$app_dest" == "$recorded_app" ]] || fail "--app-dest differs from installed App path: $recorded_app"
        fi
        app_dest=$recorded_app
        if [[ "$record_version" == nagi-installer-v3 ]]; then valid_hash "$recorded_app_hash" || fail "invalid installer record: $state"; fi
    elif (( app_dest_given )) && [[ -z "$source_app" ]]; then
        fail "--app-dest specified but no App is installed"
    else
        if [[ "$record_version" == nagi-installer-v3 ]]; then [[ -z "$recorded_app_hash" ]] || fail "invalid installer record: $state"; fi
    fi
    if [[ "$action" == update ]]; then
        [[ -f "$nagi" && -x "$nagi" && -f "$mihomo" && -x "$mihomo" ]] || fail "installed binaries are missing; repair the installation manually"
        if [[ -n "$recorded_app" ]]; then
            [[ -n "$source_app" ]] || fail "App is installed but missing from the update artifact"
            [[ -d "$app_dest" && ! -L "$app_dest" ]] || fail "installed App is missing or is a symbolic link"
        elif [[ -n "$source_app" ]]; then
            ! exists "$app_dest" || fail "App destination already exists outside the installer: $app_dest"
        fi
    fi
    if (( verify_only )); then exit 0; fi
    if [[ -n "$app_only" ]]; then
        if [[ "$app_only" == install ]]; then
            [[ -z "$recorded_app" ]] || fail 'App is already installed; use nagi update'
            [[ -n "$source_app" ]] || fail 'release does not contain Nagi.app'
        elif [[ -z "$recorded_app" ]]; then
            printf 'Nagi App is not installed\n'
            exit 0
        fi
        if pgrep -x NagiApp >/dev/null 2>&1; then fail 'quit Nagi.app before changing it'; fi
        # App-only operations retain the binary files and write the current record format.
        app_needs_sudo=0
        [[ -w "$state_dir" && -w "$(dirname "$app_dest")" ]] || app_needs_sudo=1
        if (( app_needs_sudo && EUID != 0 )); then
            sudo -n true 2>/dev/null || sudo -v || fail 'administrator authorization failed'
        fi
        app_write() {
            if (( app_needs_sudo && EUID != 0 )); then sudo "$@"; else "$@"; fi
        }
        app_stage=$(app_write mktemp -d "$(dirname "$app_dest")/.nagi-app.XXXXXXXX")
        app_write chmod 0755 "$app_stage"
        state_stage=$(app_write mktemp -d "$state_dir/.nagi-app.XXXXXXXX")
        app_changed=0
        state_changed=0
        app_committed=0
        app_cleanup() {
            local result=$? rollback_failed=0
            trap - EXIT
            if (( ! app_committed )); then
                if (( state_changed )); then app_write cp -- "$state_stage/old-state" "$state" || rollback_failed=1; fi
                if (( app_changed )); then
                    if [[ "$app_only" == install ]]; then
                        app_write rm -R -- "$app_dest" || rollback_failed=1
                    else
                        app_write mv -- "$app_stage/old-app" "$app_dest" || rollback_failed=1
                    fi
                fi
            fi
            if (( rollback_failed )); then
                printf 'nagi installer: App rollback incomplete; backups retained at %s %s\n' "$app_stage" "$state_stage" >&2
                exit 1
            fi
            app_write rm -R -- "$app_stage" "$state_stage" || true
            exit "$result"
        }
        trap app_cleanup EXIT
        trap 'exit 130' INT TERM
        app_write cp -- "$state" "$state_stage/old-state"
        new_app_path=
        if [[ "$app_only" == install ]]; then
            app_write cp -R -- "$source_app" "$app_stage/new-app"
            new_app_path=$app_dest
        fi
        printf 'nagi-installer-v4\napp=%s\n' "$new_app_path" \
            | app_write tee "$state_stage/new-state" >/dev/null
        app_write chmod 0644 "$state_stage/new-state"
        if [[ "$app_only" == install ]]; then
            app_write mv -- "$app_stage/new-app" "$app_dest"; app_changed=1
        else
            app_write mv -- "$app_dest" "$app_stage/old-app"; app_changed=1
        fi
        app_write mv -- "$state_stage/new-state" "$state"; state_changed=1
        app_committed=1
        printf 'Nagi App %s complete\n' "$app_only"
        exit 0
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
        [[ "$helper" != *'"installed":true'* ]] || helper_was_installed=1
        [[ "$helper" != *'"selected_backend":"privileged"'* ]] || helper_was_privileged=1
        if (( helper_was_installed && ! manage_helper )); then
            fail "privileged helper is installed but this prefix cannot manage it"
        fi
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
if (( needs_sudo || manage_helper )) && (( EUID != 0 )); then
    command -v sudo >/dev/null || fail "sudo is required to write the installation destination"
    sudo -n true 2>/dev/null || sudo -v || fail "administrator authorization failed"
fi
if [[ "$action" == install ]] && (( manage_helper )); then
    remove_orphan_helper "$artifact_dir/nagi"
fi
write_fs() {
    if (( needs_sudo )) && (( EUID != 0 )); then sudo "$@"; else "$@"; fi
}
remove_fs() {
    if exists "$1"; then
        if [[ -d "$1" && ! -L "$1" ]]; then write_fs rm -R -- "$1"; else write_fs rm -- "$1"; fi
    fi
}
enable_helper_when_ready() {
    local attempt error
    for (( attempt=1; attempt<=20; attempt++ )); do
        if error=$("$nagi" privileged-helper enable 2>&1 >/dev/null); then return 0; fi
        if (( attempt < 20 )); then sleep 0.25; fi
    done
    printf '%s\n' "$error" >&2
    return 1
}

write_fs mkdir -p -- "$bin_dir" "$state_dir"
stage=$(write_fs mktemp -d "$prefix/.nagi-install.XXXXXXXX")
write_fs chmod 0755 "$stage"
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
helper_removed=0
helper_added=0
helper_switched=0
cleanup() {
    local result=$?
    trap - EXIT
    local rollback_failed=0
    if (( ! committed && changed )); then
        printf 'nagi installer: operation failed; restoring previous files\n' >&2
        if (( helper_added )); then
            "$nagi" privileged-helper disable >/dev/null 2>&1 || true
            sudo "$nagi" privileged-helper uninstall >/dev/null 2>&1 || rollback_failed=1
        fi
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
    if (( ! committed && helper_removed && helper_was_installed )); then
        sudo "$nagi" privileged-helper install >/dev/null || rollback_failed=1
        if (( helper_was_privileged )); then enable_helper_when_ready || rollback_failed=1; fi
    elif (( ! committed && helper_switched && ! helper_removed )); then
        enable_helper_when_ready || rollback_failed=1
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
    write_fs chmod 0755 "$app_stage"
fi
if [[ "$action" != uninstall ]]; then
    mkdir -p -m 0700 -- "$profile_dir"
    profile_stage=$(mktemp -d "$profile_dir/.nagi-install.XXXXXXXX")
    "$artifact_dir/nagi" __default-profile > "$profile_stage/new-default.yaml" || fail "artifact nagi cannot provide its default profile"
    chmod 0600 "$profile_stage/new-default.yaml"
    write_fs install -m 0755 -- "$artifact_dir/nagi" "$stage/new-nagi"
    write_fs install -m 0755 -- "$artifact_dir/mihomo" "$stage/new-mihomo"
    if [[ -n "$source_app" ]]; then write_fs cp -R -- "$source_app" "$app_stage/new-app"; fi
    printf 'nagi-installer-v4\napp=%s\n' "${source_app:+$app_dest}" \
        | write_fs tee "$stage/new-state" >/dev/null
    write_fs chmod 0644 "$stage/new-state"
fi
if (( manage_helper && ${helper_was_installed:-0} )); then
    if (( helper_was_privileged )); then "$nagi" privileged-helper disable >/dev/null; helper_switched=1; fi
    sudo "$nagi" privileged-helper uninstall >/dev/null
    helper_removed=1
fi
if [[ "$action" != install ]]; then
    if exists "$nagi"; then
        if [[ "$action" == update ]]; then
            # Keep a running CLI's executable path in bin/. Moving its inode
            # changes /proc/self/exe on Linux and breaks companion lookup.
            write_fs cp -p -- "$nagi" "$stage/old-nagi"
        else
            write_fs mv -- "$nagi" "$stage/old-nagi"
        fi
        old_nagi=1; changed=1
    fi
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
    if (( manage_helper )); then
        sudo "$nagi" privileged-helper install >/dev/null
        helper_added=1
        if [[ "$action" == install ]] || (( helper_was_privileged )); then
            enable_helper_when_ready
        fi
    fi
fi
committed=1
if (( purge_data )); then
    for data_path in "${user_data_targets[@]}"; do
        [[ "$data_path" != / && "$data_path" != "$HOME" ]] || purge_fail "refusing unsafe user data path: $data_path"
        rm -rf -- "$data_path" || purge_fail "could not remove user data at $data_path"
    done
fi
printf 'Nagi %s complete at %s\n' "$action" "$prefix"
