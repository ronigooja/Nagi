#!/usr/bin/env bash
# Exercise the installer against isolated artifacts and a writable temporary prefix.
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
installer=${INSTALLER_SCRIPT:-$script_dir/installer.sh}
[[ -f "$installer" ]] || { printf 'installer not found: %s\n' "$installer" >&2; exit 1; }

test_dir=$(mktemp -d)
test_dir=$(cd "$test_dir" && pwd -P)
trap 'rm -rf -- "$test_dir"' EXIT
# Use the production dispatcher for filesystem durability; command/status fixtures
# below replace only unrelated lifecycle operations.
if [[ -z ${NAGI_TEST_INSTALL_HELPER:-} ]]; then
    NAGI_TEST_INSTALL_HELPER=$test_dir/transaction-helper
    (cd "$script_dir/.." && "${GO:-go}" build -o "$NAGI_TEST_INSTALL_HELPER" ./cmd/nagi)
fi
export NAGI_TEST_INSTALL_HELPER
export HOME=$test_dir/home
export XDG_CONFIG_HOME=$test_dir/xdg-config
if [[ $(uname -s) == Darwin ]]; then
    profile_dir=$HOME/Library/Application\ Support/Nagi/config/profiles
else
    profile_dir=$XDG_CONFIG_HOME/nagi/profiles
fi
prefix=$test_dir/prefix
artifact_v1=$test_dir/artifact-v1
artifact_v2=$test_dir/artifact-v2
log=$test_dir/installer.log
mkdir -p "$artifact_v1" "$artifact_v2"

make_artifact() {
    local dir=$1 version=$2
    cat > "$dir/nagi" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == __install-transaction ]]; then
    exec "$NAGI_TEST_INSTALL_HELPER" "$@"
fi
if [[ "${1:-}" == __default-profile ]]; then
    if [[ "${NAGI_TEST_EMPTY_DEFAULT:-0}" == 1 ]]; then exit 0; fi
    tail -n 1 "$0" | sed 's/^# fixture-version: /mode: /'
    exit 0
fi
[[ "${1:-}" == --json ]] || exit 2
case "${2:-}" in
    version)
        if [[ "${NAGI_TEST_FAIL_INSTALLED_VERSION:-0}" == 1 && "$0" == */bin/nagi ]]; then exit 17; fi
        printf '{"ok":true,"nagi":"fixture","mihomo_commit":"fixture"}\n'
        ;;
    status)
        if [[ "${NAGI_TEST_RUNNING:-0}" == 1 ]]; then
            printf '{"ok":true,"running":true}\n'
        else
            printf '{"ok":true,"running":false}\n'
        fi
        ;;
    service)
        [[ "${3:-}" == status ]] || exit 2
        if [[ "${NAGI_TEST_SERVICE_ACTIVE:-0}" == 1 ]]; then
            printf '{"ok":true,"installed":true,"active":true}\n'
        elif [[ "${NAGI_TEST_SERVICE_INSTALLED:-0}" == 1 ]]; then
            printf '{"ok":true,"installed":true,"active":false}\n'
        else
            printf '{"ok":true,"installed":false,"active":false}\n'
        fi
        ;;
    privileged-helper)
        [[ "${3:-}" == status ]] || exit 2
        printf '{"ok":true,"installed":false,"active":false}\n'
        ;;
    *) exit 2 ;;
esac
SH
    printf '# fixture-version: %s\n' "$version" >> "$dir/nagi"
    cat > "$dir/mihomo" <<'SH'
#!/usr/bin/env bash
[[ "${1:-}" == -v ]] || exit 2
printf 'mihomo fixture\n'
SH
    printf '# fixture-version: %s\n' "$version" >> "$dir/mihomo"
    chmod 0755 "$dir/nagi" "$dir/mihomo"
}

fail_test() { printf 'test-install: %s\n' "$*" >&2; exit 1; }
expect_failure() {
    local expected=$1
    shift
    if "$@" > "$log" 2>&1; then
        fail_test "command unexpectedly succeeded: $*"
    fi
    if ! grep -Fq -- "$expected" "$log"; then
        cat "$log" >&2
        fail_test "expected error containing: $expected"
    fi
}
assert_installed_from() {
    local dir=$1
    cmp -s "$dir/nagi" "$prefix/bin/nagi" || fail_test 'nagi content changed unexpectedly'
    cmp -s "$dir/mihomo" "$prefix/bin/mihomo" || fail_test 'mihomo content changed unexpectedly'
    [[ $(sed -n '1p' "$prefix/share/nagi/install-state") == nagi-installer-v4 ]] || fail_test 'installer record changed unexpectedly'
    [[ $(sed -n '2p' "$prefix/share/nagi/install-state") == app= ]] || fail_test 'unexpected App record'
    [[ $(wc -l < "$prefix/share/nagi/install-state") -eq 2 ]] || fail_test 'unexpected installer record fields'
}

make_artifact "$artifact_v1" v1
make_artifact "$artifact_v2" v2

# An existing executable without an installer record belongs to someone else.
mkdir -p "$prefix/bin"
printf 'unmanaged\n' > "$prefix/bin/nagi"
expect_failure 'unmanaged executables' bash "$installer" install --artifact-dir "$artifact_v1" --prefix "$prefix"
[[ $(cat "$prefix/bin/nagi") == unmanaged ]] || fail_test 'unmanaged executable was overwritten'
rm "$prefix/bin/nagi"

# Invalid artifacts must be rejected before writing an installation record.
invalid=$test_dir/invalid
mkdir "$invalid"
cp "$artifact_v1/nagi" "$invalid/nagi"
expect_failure 'missing regular executable' bash "$installer" install --artifact-dir "$invalid" --prefix "$prefix"
[[ ! -e "$prefix/share/nagi/install-state" ]] || fail_test 'invalid artifact created an installer record'

# A candidate may be executable yet contain no runnable engine/default content.
empty_artifact=$test_dir/empty-artifact
mkdir "$empty_artifact"
cp "$artifact_v1/nagi" "$artifact_v1/mihomo" "$empty_artifact/"
: > "$empty_artifact/mihomo"
expect_failure 'missing regular executable' bash "$installer" install --artifact-dir "$empty_artifact" --prefix "$prefix"
printf '#!/bin/sh\nexit 0\n' > "$empty_artifact/mihomo"
chmod 0755 "$empty_artifact/mihomo"
expect_failure 'artifact mihomo' bash "$installer" install --artifact-dir "$empty_artifact" --prefix "$prefix"
cp "$artifact_v1/mihomo" "$empty_artifact/mihomo"
expect_failure 'default profile' env NAGI_TEST_EMPTY_DEFAULT=1 bash "$installer" install --artifact-dir "$empty_artifact" --prefix "$prefix"
[[ ! -e "$prefix/bin/nagi" && ! -e "$prefix/share/nagi/install-state" ]] || fail_test 'empty artifact changed managed installation'

# A failed first install must leave the destination and the existing default profile intact.
mkdir -p "$profile_dir"
printf 'local default\n' > "$profile_dir/default.yaml"
expect_failure 'installed nagi failed its version check' env NAGI_TEST_FAIL_INSTALLED_VERSION=1 bash "$installer" install --artifact-dir "$artifact_v1" --prefix "$prefix"
[[ ! -e "$prefix/bin/nagi" && ! -e "$prefix/bin/mihomo" && ! -e "$prefix/share/nagi/install-state" ]] || fail_test 'failed install left managed files behind'
[[ $(cat "$profile_dir/default.yaml") == 'local default' ]] || fail_test 'failed install changed default profile'

bash "$installer" install --artifact-dir "$artifact_v1" --prefix "$prefix" > "$log"
assert_installed_from "$artifact_v1"
[[ $(cat "$profile_dir/default.yaml") == 'mode: v1' ]] || fail_test 'install did not write default profile'
expect_failure 'already installed' bash "$installer" install --artifact-dir "$artifact_v2" --prefix "$prefix"

# Simulate the installing process disappearing after durable helper calls.
# The separate journal must recover even when install-state was removed, and
# read-only verification must neither recover nor discard that journal.
journal=$prefix/.nagi-transaction
"$NAGI_TEST_INSTALL_HELPER" __install-transaction prepare "$journal" \
    "$prefix/bin/nagi" "$artifact_v2/nagi" \
    "$prefix/share/nagi/install-state" -
bash "$installer" update --verify --artifact-dir "$artifact_v2" --prefix "$prefix" > "$log"
[[ -f "$journal/manifest.json" ]] || fail_test 'verification discarded prepared journal'
"$NAGI_TEST_INSTALL_HELPER" __install-transaction activate "$journal"
[[ ! -e "$prefix/share/nagi/install-state" ]] || fail_test 'interrupted transaction did not remove install-state'
if bash "$installer" update --verify --artifact-dir "$artifact_v1" --prefix "$prefix" > "$log" 2>&1; then
    fail_test 'verification accepted missing installation record'
fi
[[ -f "$journal/manifest.json" && ! -e "$prefix/share/nagi/install-state" ]] || fail_test 'verification mutated interrupted installation'
bash "$installer" update --artifact-dir "$artifact_v1" --prefix "$prefix" > "$log"
assert_installed_from "$artifact_v1"
[[ ! -e "$journal" ]] || fail_test 'successful recovery retained active journal'
"$NAGI_TEST_INSTALL_HELPER" __install-transaction recover "$journal"

# Explicit recovery must work without either the installed executable or record.
"$NAGI_TEST_INSTALL_HELPER" __install-transaction prepare "$journal" \
    "$prefix/bin/nagi" - "$prefix/share/nagi/install-state" -
"$NAGI_TEST_INSTALL_HELPER" __install-transaction activate "$journal"
[[ ! -e "$prefix/bin/nagi" && ! -e "$prefix/share/nagi/install-state" ]] || fail_test 'removal fixture did not remove recovery-independent files'
bash "$installer" recover --artifact-dir "$artifact_v1" --prefix "$prefix" > "$log"
bash "$installer" recover --artifact-dir "$artifact_v1" --prefix "$prefix" > "$log"
assert_installed_from "$artifact_v1"
# A recorded installation can be updated after its binaries change outside the installer.
printf '# outside modification\n' >> "$prefix/bin/mihomo"
printf '# outside modification\n' >> "$prefix/bin/nagi"
# Existing v3 installations are accepted once and rewritten as v4 by update.
printf 'nagi-installer-v3\napp=\nnagi_sha256=%064d\nmihomo_sha256=%064d\napp_sha256=\n' 0 0 > "$prefix/share/nagi/install-state"
cp "$prefix/bin/nagi" "$test_dir/modified-nagi"
cp "$prefix/bin/mihomo" "$test_dir/modified-mihomo"
expect_failure 'installed nagi failed its version check' env NAGI_TEST_FAIL_INSTALLED_VERSION=1 bash "$installer" update --artifact-dir "$artifact_v2" --prefix "$prefix"
cmp -s "$test_dir/modified-nagi" "$prefix/bin/nagi" || fail_test 'update rollback lost modified nagi'
cmp -s "$test_dir/modified-mihomo" "$prefix/bin/mihomo" || fail_test 'update rollback lost modified mihomo'
bash "$installer" update --artifact-dir "$artifact_v2" --prefix "$prefix" > "$log"
assert_installed_from "$artifact_v2"
bash "$installer" update --artifact-dir "$artifact_v1" --prefix "$prefix" > "$log"
assert_installed_from "$artifact_v1"

invalid_version=$test_dir/invalid-version
mkdir "$invalid_version"
cp "$artifact_v1/mihomo" "$invalid_version/mihomo"
printf '#!/usr/bin/env bash\nprintf "invalid version response\\n"\n' > "$invalid_version/nagi"
chmod 0755 "$invalid_version/nagi"
expect_failure 'invalid version response' bash "$installer" update --artifact-dir "$invalid_version" --prefix "$prefix"
assert_installed_from "$artifact_v1"

# Other profile and subscription files represent user data.
mkdir -p "$prefix/share/nagi/profiles" "$test_dir/user-data"
printf 'user profile\n' > "$prefix/share/nagi/profiles/default.yaml"
printf 'user subscription\n' > "$test_dir/user-data/subscription.yaml"
printf 'local default\n' > "$profile_dir/default.yaml"

expect_failure 'mihomo is running' env NAGI_TEST_RUNNING=1 bash "$installer" update --artifact-dir "$artifact_v2" --prefix "$prefix"
assert_installed_from "$artifact_v1"
expect_failure 'login service is active' env NAGI_TEST_SERVICE_ACTIVE=1 bash "$installer" update --artifact-dir "$artifact_v2" --prefix "$prefix"
assert_installed_from "$artifact_v1"

# A failed post-install version check must restore both previous binaries.
expect_failure 'installed nagi failed its version check' env NAGI_TEST_FAIL_INSTALLED_VERSION=1 bash "$installer" update --artifact-dir "$artifact_v2" --prefix "$prefix"
assert_installed_from "$artifact_v1"
[[ $(cat "$profile_dir/default.yaml") == 'local default' ]] || fail_test 'failed update changed default profile'

bash "$installer" update --artifact-dir "$artifact_v2" --prefix "$prefix" > "$log"
assert_installed_from "$artifact_v2"
[[ $(cat "$profile_dir/default.yaml") == 'mode: v2' ]] || fail_test 'update did not replace default profile'
[[ $(cat "$prefix/share/nagi/profiles/default.yaml") == 'user profile' ]] || fail_test 'update changed user profile'

expect_failure 'login service is installed' env NAGI_TEST_SERVICE_INSTALLED=1 bash "$installer" uninstall --prefix "$prefix"
assert_installed_from "$artifact_v2"

printf '# outside modification\n' >> "$prefix/bin/nagi"
printf '# outside modification\n' >> "$prefix/bin/mihomo"
# Removal also accepts an existing v3 record.
printf 'nagi-installer-v3\napp=\nnagi_sha256=%064d\nmihomo_sha256=%064d\napp_sha256=\n' 0 0 > "$prefix/share/nagi/install-state"
bash "$installer" uninstall --prefix "$prefix" > "$log"
[[ ! -e "$prefix/bin/nagi" && ! -e "$prefix/bin/mihomo" && ! -e "$prefix/share/nagi/install-state" ]] || fail_test 'uninstall left managed files behind'
[[ $(cat "$prefix/share/nagi/profiles/default.yaml") == 'user profile' ]] || fail_test 'uninstall removed user profile'
[[ $(cat "$profile_dir/default.yaml") == 'mode: v2' ]] || fail_test 'uninstall removed default profile'
[[ $(cat "$test_dir/user-data/subscription.yaml") == 'user subscription' ]] || fail_test 'uninstall removed user subscription'

# Running the installer directly should find artifacts beside its scripts directory.
default_root=$test_dir/default-layout
mkdir -p "$default_root/scripts" "$default_root/bin"
cp "$installer" "$default_root/scripts/installer.sh"
cp "$artifact_v1/nagi" "$artifact_v1/mihomo" "$default_root/bin/"
bash "$default_root/scripts/installer.sh" install --prefix "$default_root/prefix" > "$log"
cmp -s "$artifact_v1/nagi" "$default_root/prefix/bin/nagi" || fail_test 'default artifact directory was not used'
bash "$default_root/scripts/installer.sh" uninstall --prefix "$default_root/prefix" > "$log"

printf 'installer integration tests passed\n'
