#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
parent_root="$repo_root/tmp/ci-parent-root"

rm -rf "$parent_root"
mkdir -p "$parent_root"
ln -s "$repo_root" "$parent_root/.agent-layer"

trap '[[ "${PARENT_ROOT_KEEP_TEMP:-0}" == "1" ]] || rm -rf "$parent_root"' EXIT INT TERM

(cd "$parent_root" && node .agent-layer/src/sync/sync.mjs)

"$repo_root/tests/run.sh" --parent-root "$parent_root" --run-from-repo-root

bash "$repo_root/tests/coverage.sh"
