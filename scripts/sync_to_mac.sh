#!/usr/bin/env bash
# Copy the current source tree to the macair checkout without copying local builds.
set -euo pipefail

project_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
: "${NAGI_MAC_REMOTE:?Set NAGI_MAC_REMOTE to the SSH target, for example user@mac-host}"
: "${NAGI_MAC_REMOTE_DIR:?Set NAGI_MAC_REMOTE_DIR to the remote checkout path}"
remote=$NAGI_MAC_REMOTE
remote_dir=$NAGI_MAC_REMOTE_DIR

printf 'Syncing Nagi source to %s:%s\n' "$remote" "$remote_dir"

if ! ssh "$remote" "test -d '$remote_dir/.git'"; then
    printf 'Nagi sync failed: checkout not found or SSH unavailable\n' >&2
    exit 1
fi

if ! rsync -rlptz --delete \
    --exclude='/.git/' \
    --exclude='/bin/' \
    --exclude='/dist/' \
    --exclude='/.build/' \
    --exclude='/macos/NagiApp/.build/' \
    --exclude='.DS_Store' \
    "$project_dir/" "$remote:$remote_dir/"; then
    printf 'Nagi sync failed\n' >&2
    exit 1
fi

printf 'Nagi sync succeeded\n'
