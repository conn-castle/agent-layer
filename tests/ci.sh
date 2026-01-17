#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
parent_root="$repo_root/tmp/ci-parent-root"

rm -rf "$parent_root"
mkdir -p "$parent_root"
ln -s "$repo_root" "$parent_root/.agent-layer"

trap '[[ "${PARENT_ROOT_KEEP_TEMP:-0}" == "1" ]] || rm -rf "$parent_root"' EXIT INT TERM

(cd "$parent_root" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer)

# Install MCP prompt server deps for SDK-required tests.
mcp_sdk_dir="$repo_root/src/mcp/agent-layer-prompts/node_modules/@modelcontextprotocol/sdk"
if [[ ! -d "$mcp_sdk_dir" ]]; then
  (cd "$repo_root/src/mcp/agent-layer-prompts" && npm install)
fi

"$repo_root/tests/run.sh" --parent-root "$parent_root" --run-from-repo-root
