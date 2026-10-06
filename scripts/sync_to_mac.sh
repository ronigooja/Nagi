#!/usr/bin/env bash
# Copy the current source tree to the macair checkout without copying local builds.
set -euo pipefail

project_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
remote=zhuwei@macair
remote_dir=/Users/zhuwei/terminal/Nagi

ssh "$remote" "test -d '$remote_dir/.git'" || {
    printf 'Nagi checkout not found on %s:%s\n' "$remote" "$remote_dir" >&2
    exit 1
}

rsync -rlptz --delete \
    --exclude='/.git/' \
    --exclude='/bin/' \
    --exclude='/dist/' \
    --exclude='/.build/' \
    --exclude='/macos/NagiApp/.build/' \
    --exclude='.DS_Store' \
    "$project_dir/" "$remote:$remote_dir/"
