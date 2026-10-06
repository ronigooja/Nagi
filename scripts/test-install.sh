#!/usr/bin/env bash
# Exercise the installer against isolated artifacts and a writable temporary prefix.
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
installer=${INSTALLER_SCRIPT:-$script_dir/installer.sh}
[[ -f "$installer" ]] || { printf 'installer not found: %s\n' "$installer" >&2; exit 1; }

test_dir=$(mktemp -d)
trap 'rm -rf -- "$test_dir"' EXIT
export XDG_CONFIG_HOME=$test_dir/xdg-config
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
if [[ "${1:-}" == __default-profile ]]; then
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
    [[ $(sed -n '1p' "$prefix/share/nagi/install-state") == nagi-installer-v3 ]] || fail_test 'installer record changed unexpectedly'
    [[ $(sed -n '2p' "$prefix/share/nagi/install-state") == app= ]] || fail_test 'unexpected App record'
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

# A failed first install must leave the destination and the existing default profile intact.
mkdir -p "$XDG_CONFIG_HOME/nagi/profiles"
printf 'local default\n' > "$XDG_CONFIG_HOME/nagi/profiles/default.yaml"
expect_failure 'installed nagi failed its version check' env NAGI_TEST_FAIL_INSTALLED_VERSION=1 bash "$installer" install --artifact-dir "$artifact_v1" --prefix "$prefix"
[[ ! -e "$prefix/bin/nagi" && ! -e "$prefix/bin/mihomo" && ! -e "$prefix/share/nagi/install-state" ]] || fail_test 'failed install left managed files behind'
[[ $(cat "$XDG_CONFIG_HOME/nagi/profiles/default.yaml") == 'local default' ]] || fail_test 'failed install changed default profile'

bash "$installer" install --artifact-dir "$artifact_v1" --prefix "$prefix" > "$log"
assert_installed_from "$artifact_v1"
[[ $(cat "$XDG_CONFIG_HOME/nagi/profiles/default.yaml") == 'mode: v1' ]] || fail_test 'install did not write default profile'
expect_failure 'already installed' bash "$installer" install --artifact-dir "$artifact_v2" --prefix "$prefix"
# A manually changed binary must not be overwritten or removed by the installer.
printf '# outside modification\n' >> "$prefix/bin/mihomo"
expect_failure 'installed file changed outside the installer' bash "$installer" update --artifact-dir "$artifact_v2" --prefix "$prefix"
cmp -s "$artifact_v1/nagi" "$prefix/bin/nagi" || fail_test 'nagi changed after rejected update'
cp "$artifact_v1/mihomo" "$prefix/bin/mihomo"

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
printf 'local default\n' > "$XDG_CONFIG_HOME/nagi/profiles/default.yaml"

expect_failure 'mihomo is running' env NAGI_TEST_RUNNING=1 bash "$installer" update --artifact-dir "$artifact_v2" --prefix "$prefix"
assert_installed_from "$artifact_v1"
expect_failure 'login service is active' env NAGI_TEST_SERVICE_ACTIVE=1 bash "$installer" update --artifact-dir "$artifact_v2" --prefix "$prefix"
assert_installed_from "$artifact_v1"

# A failed post-install version check must restore both previous binaries.
expect_failure 'installed nagi failed its version check' env NAGI_TEST_FAIL_INSTALLED_VERSION=1 bash "$installer" update --artifact-dir "$artifact_v2" --prefix "$prefix"
assert_installed_from "$artifact_v1"
[[ $(cat "$XDG_CONFIG_HOME/nagi/profiles/default.yaml") == 'local default' ]] || fail_test 'failed update changed default profile'

bash "$installer" update --artifact-dir "$artifact_v2" --prefix "$prefix" > "$log"
assert_installed_from "$artifact_v2"
[[ $(cat "$XDG_CONFIG_HOME/nagi/profiles/default.yaml") == 'mode: v2' ]] || fail_test 'update did not replace default profile'
[[ $(cat "$prefix/share/nagi/profiles/default.yaml") == 'user profile' ]] || fail_test 'update changed user profile'

expect_failure 'login service is installed' env NAGI_TEST_SERVICE_INSTALLED=1 bash "$installer" uninstall --prefix "$prefix"
assert_installed_from "$artifact_v2"

bash "$installer" uninstall --prefix "$prefix" > "$log"
[[ ! -e "$prefix/bin/nagi" && ! -e "$prefix/bin/mihomo" && ! -e "$prefix/share/nagi/install-state" ]] || fail_test 'uninstall left managed files behind'
[[ $(cat "$prefix/share/nagi/profiles/default.yaml") == 'user profile' ]] || fail_test 'uninstall removed user profile'
[[ $(cat "$XDG_CONFIG_HOME/nagi/profiles/default.yaml") == 'mode: v2' ]] || fail_test 'uninstall removed default profile'
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
