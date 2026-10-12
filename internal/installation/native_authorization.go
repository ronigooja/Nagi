package installation

// The release installer elevates individual operations through sudo. This
// private, update-scoped adapter preserves that interface while delegating
// authentication to macOS. The CLI and installer themselves stay unprivileged.
// Only the sudo argument forms used by the installer are supported.
const nativeUpdateSudo = `#!/bin/bash
set -euo pipefail
if [[ "$#" == 2 && "$1" == -n && "$2" == true ]]; then
    # A sudo timestamp does not establish macOS Authorization Services rights.
    exit 1
fi
if [[ "$#" == 1 && "$1" == -v ]]; then
    set -- /usr/bin/true
fi
if [[ "$#" == 0 || "$1" == -* ]]; then
    echo 'Unsupported update authorization operation' >&2
    exit 2
fi
# Preserve the session owner required by privileged-helper install. Do not
# forward the adapter PATH into the privileged process.
printf -v command '%q ' /usr/bin/env \
    "SUDO_UID=$(/usr/bin/id -u)" "SUDO_GID=$(/usr/bin/id -g)" \
    "SUDO_USER=$(/usr/bin/id -un)" 'PATH=/usr/bin:/bin:/usr/sbin:/sbin' "$@"
exec /usr/bin/osascript -e 'on run argv
    do shell script ("/bin/bash -c " & quoted form of (item 1 of argv)) with administrator privileges with prompt "Nagi needs administrator permission to install the update."
end run' "$command"
`
