package installation

// The adapter sends installer operations to one update-scoped privileged
// worker. Only the worker launch uses Authorization Services.
const nativeUpdateSudo = `#!/bin/bash
set -euo pipefail
bridge=$(cd "$(dirname "$0")" && pwd)
if [[ "$#" == 2 && "$1" == -n && "$2" == true ]]; then
    [[ -s "$bridge/ready" && ! -e "$bridge/ended" ]]
    exit
fi
if [[ "$#" == 1 && "$1" == -v ]]; then
    set -- /usr/bin/true
fi
if [[ "$#" == 0 || "$1" == -* ]]; then
    echo 'Unsupported update authorization operation' >&2
    exit 2
fi
# Serialize startup and requests, including installer pipelines.
while ! mkdir "$bridge/lock" 2>/dev/null; do
    [[ -d "$bridge" ]] || exit 1
    sleep 0.1
done
trap 'rmdir "$bridge/lock"' EXIT
if [[ ! -s "$bridge/ready" ]]; then
    # Authorization Services starts one worker for this update.
    [[ ! -e "$bridge/ended" ]] || exit 1
    : > "$bridge/ready"
    touch "$bridge/alive"
    session_uid=$(/usr/bin/id -u)
    session_gid=$(/usr/bin/id -g)
    session_user=$(/usr/bin/id -un)
    printf -v command '%q ' /usr/bin/env \
        "SUDO_UID=$session_uid" "SUDO_GID=$session_gid" \
        "SUDO_USER=$session_user" 'PATH=/usr/bin:/bin:/usr/sbin:/sbin' \
        /bin/bash "$bridge/worker" "$bridge" "$NAGI_UPDATE_OWNER_PID"
    (
        set +e
        /usr/bin/osascript -e 'on run argv
            do shell script ("/bin/bash -c " & quoted form of (item 1 of argv)) with administrator privileges with prompt "Nagi needs administrator permission to install the update."
        end run' "$command"
        touch "$bridge/ended"
    ) &
    while [[ ! -s "$bridge/ready" ]]; do
        [[ ! -e "$bridge/ended" ]] || exit 1
        sleep 0.1
    done
fi
[[ ! -e "$bridge/ended" ]] || exit 1
request=$(mktemp -d "$bridge/request.XXXXXXXX")
trap 'rm -rf "$request"; rmdir "$bridge/lock"' EXIT
printf -v command '%q ' "$@"
printf '%s\n' "$command" > "$request/command"
cat > "$request/input"
: > "$request/output"
: > "$request/error"
: > "$request/status"
printf '%s\n' "$request" > "$bridge/pending.new"
mv "$bridge/pending.new" "$bridge/pending"
while [[ ! -s "$request/status" ]]; do
    [[ ! -e "$bridge/ended" ]] || exit 1
    sleep 0.1
done
cat "$request/output"
cat "$request/error" >&2
exit "$(cat "$request/status")"
`

// Removing alive closes the session. Parent death and a hard lifetime bound
// also release privilege if the updater is interrupted without running traps.
const nativeUpdateWorker = `#!/bin/bash
set -uo pipefail
bridge=$1
owner_pid=$2
printf '%s\n' "$$" > "$bridge/ready"
deadline=$((SECONDS + 1800))
while [[ -e "$bridge/alive" ]] && kill -0 "$owner_pid" 2>/dev/null && (( SECONDS < deadline )); do
    if [[ ! -f "$bridge/pending" ]]; then sleep 0.1; continue; fi
    IFS= read -r request < "$bridge/pending"
    rm -f "$bridge/pending"
    case "$request" in "$bridge"/request.*) ;; *) exit 2 ;; esac
    /bin/bash "$request/command" < "$request/input" > "$request/output" 2> "$request/error"
    printf '%s\n' "$?" > "$request/status"
done
`
