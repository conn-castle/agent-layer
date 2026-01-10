#!/usr/bin/env bash
set -euo pipefail

# .agent-layer/setup.sh
# One-shot setup for agent-layer in this repo:
# - installs MCP prompt server deps
# - runs sync to generate shims/configs/skills
# - enables & tests git hooks
#
# Usage:
#   ./.agent-layer/setup.sh [--skip-checks]

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

SKIP_CHECKS="0"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-checks)
      SKIP_CHECKS="1"
      ;;
    --help | -h)
      say "Usage: ./.agent-layer/setup.sh [--skip-checks]"
      exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
  shift
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTRYPOINT_SH="$SCRIPT_DIR/.agent-layer/src/lib/entrypoint.sh"
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  ENTRYPOINT_SH="$SCRIPT_DIR/src/lib/entrypoint.sh"
fi
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  ENTRYPOINT_SH="$SCRIPT_DIR/../src/lib/entrypoint.sh"
fi
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  die "Missing src/lib/entrypoint.sh (expected near .agent-layer/)."
fi
# shellcheck disable=SC1090
source "$ENTRYPOINT_SH"
resolve_entrypoint_root || exit $?

cd "$WORKING_ROOT"

[[ -d "$AGENTLAYER_ROOT" ]] || die "Missing .agent-layer/ directory. Run the bootstrap script in the repo root first."
[[ -f "$AGENTLAYER_ROOT/src/sync/sync.mjs" ]] || die "Missing .agent-layer/src/sync/sync.mjs. Re-run bootstrap or restore it."

command -v node > /dev/null 2>&1 || die "Node.js is required (node not found). Install Node, then re-run."
command -v npm > /dev/null 2>&1 || die "npm is required (npm not found). Install npm/Node, then re-run."
command -v git > /dev/null 2>&1 || die "git is required (git not found)."

# Ensure we're in a git repo (recommended for hooks).
if ! git rev-parse --is-inside-work-tree > /dev/null 2>&1; then
  say "NOTE: Not inside a git repository. Hooks will not be enabled."
  IN_GIT_REPO="0"
else
  IN_GIT_REPO="1"
fi

say "==> Running agent-layer sync"
node "$AGENTLAYER_ROOT/src/sync/sync.mjs"

say "==> Installing MCP prompt server dependencies"
if [[ -f "$AGENTLAYER_ROOT/src/mcp/agent-layer-prompts/package.json" ]]; then
  pushd "$AGENTLAYER_ROOT/src/mcp/agent-layer-prompts" > /dev/null
  npm install
  popd > /dev/null
else
  die "Missing .agent-layer/src/mcp/agent-layer-prompts/package.json"
fi

if [[ "$IN_GIT_REPO" == "1" ]]; then
  say "Skipping hook enable/test (dev-only; run .agent-layer/dev/bootstrap.sh)."
else
  say "Skipping hook enable/test (not a git repo)."
fi

if [[ "$SKIP_CHECKS" == "1" ]]; then
  say "==> Skipping sync check (--skip-checks)"
else
  say "==> Verifying sync is up-to-date (check mode)"
  node "$AGENTLAYER_ROOT/src/sync/sync.mjs" --check
fi

say ""
say "Setup complete (manual steps below are required)."
say ""
say "Required manual steps (do all of these):"
say "  1) Create/fill .agent-layer/.env (copy from .env.example; do not commit)"
say "  2) Edit instructions: .agent-layer/config/instructions/*.md"
say "  3) Edit workflows:    .agent-layer/config/workflows/*.md"
say "  4) Edit MCP servers:  .agent-layer/config/mcp-servers.json"
say ""
say "Note: ./al automatically runs sync before each command."
say "If you do not use ./al, regenerate manually:"
say "  node .agent-layer/src/sync/sync.mjs"
