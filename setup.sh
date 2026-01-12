#!/usr/bin/env bash
set -euo pipefail

# .agent-layer/setup.sh
# One-shot setup for agent-layer in this repo:
# - installs MCP prompt server deps
# - runs sync to generate shims/configs/skills
# - enables & tests git hooks
#
# Usage:
#   ./setup.sh [--skip-checks] [--temp-work-root] [--work-root <path>]

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

# Parse CLI flags and reject unknown options.
SKIP_CHECKS="0"
work_root=""
use_temp_work_root="0"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --temp-work-root)
      use_temp_work_root="1"
      ;;
    --skip-checks)
      SKIP_CHECKS="1"
      ;;
    --work-root)
      shift
      if [[ $# -eq 0 || -z "${1:-}" ]]; then
        die "--work-root requires a path."
      fi
      work_root="$1"
      ;;
    --work-root=*)
      work_root="${1#*=}"
      if [[ -z "$work_root" ]]; then
        die "--work-root requires a path."
      fi
      ;;
    --help | -h)
      cat << 'EOF'
Usage: ./setup.sh [--skip-checks] [--temp-work-root] [--work-root <path>]

In a consumer repo, run without work-root flags.
In the agent-layer repo (no .agent-layer/), a temp work root is used by default
(system temp or tmp/agent-layer-temp-work-root).
EOF
      exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
  shift
done

# Resolve work roots for consumer and agent-layer layouts.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOTS_HELPER="$SCRIPT_DIR/src/lib/discover-root.sh"
if [[ ! -f "$ROOTS_HELPER" ]]; then
  die "Missing src/lib/discover-root.sh (expected near setup.sh)."
fi
# shellcheck disable=SC1090
source "$ROOTS_HELPER"
ROOTS_DEFAULT_TEMP_WORK_ROOT="1" \
  ROOTS_ALLOW_CONSUMER_WORK_ROOT="0" \
  ROOTS_REQUIRE_WORK_ROOT_IN_AGENT_LAYER="0" \
  WORK_ROOT="$work_root" \
  USE_TEMP_WORK_ROOT="$use_temp_work_root" \
  resolve_roots

if [[ "$TEMP_WORK_ROOT_CREATED" == "1" ]]; then
  trap 'rm -rf "$WORKING_ROOT"' EXIT
  say "==> Using temporary work root: $WORKING_ROOT"
fi

# Run from the repo root so all relative paths are stable.
cd "$WORKING_ROOT"

# Validate required agent-layer files and system tools.
[[ -d "$AGENTLAYER_ROOT" ]] || die "Missing agent-layer root: $AGENTLAYER_ROOT"
[[ -f "$AGENTLAYER_ROOT/src/sync/sync.mjs" ]] || die "Missing src/sync/sync.mjs under $AGENTLAYER_ROOT."

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

# Generate all agent-layer outputs from config sources.
say "==> Running agent-layer sync"
AGENTLAYER_SYNC_ROOTS=1 node "$AGENTLAYER_ROOT/src/sync/sync.mjs"

# Install MCP prompt server dependencies used by the runtime.
say "==> Installing MCP prompt server dependencies"
if [[ -f "$AGENTLAYER_ROOT/src/mcp/agent-layer-prompts/package.json" ]]; then
  pushd "$AGENTLAYER_ROOT/src/mcp/agent-layer-prompts" > /dev/null
  npm install
  popd > /dev/null
else
  die "Missing src/mcp/agent-layer-prompts/package.json under $AGENTLAYER_ROOT"
fi

# Explain hook behavior based on repo state (hook enable is dev-only).
if [[ "$IN_GIT_REPO" == "1" ]]; then
  say "Skipping hook enable/test (dev-only; run .agent-layer/dev/bootstrap.sh)."
else
  say "Skipping hook enable/test (not a git repo)."
fi

# Optionally verify that sync outputs are clean.
if [[ "$SKIP_CHECKS" == "1" ]]; then
  say "==> Skipping sync check (--skip-checks)"
else
  say "==> Verifying sync is up-to-date (check mode)"
  AGENTLAYER_SYNC_ROOTS=1 node "$AGENTLAYER_ROOT/src/sync/sync.mjs" --check
fi

# Provide manual configuration steps for first-time setup.
say ""
say "Setup complete (manual steps below are required)."
say ""
if [[ "$IS_CONSUMER_LAYOUT" == "1" ]]; then
  say "Required manual steps (do all of these):"
  say "  1) Create/fill .agent-layer/.env (copy from .env.example; do not commit)"
  say "  2) Edit instructions: .agent-layer/config/instructions/*.md"
  say "  3) Edit workflows:    .agent-layer/config/workflows/*.md"
  say "  4) Edit MCP servers:  .agent-layer/config/mcp-servers.json"
  say ""
  say "Note: ./al automatically runs sync before each command."
  say "If you do not use ./al, regenerate manually:"
  say "  node .agent-layer/src/sync/sync.mjs"
else
  say "Note: running from the agent-layer repo wrote outputs into: $WORKING_ROOT"
  say "Edit sources in config/ and re-run as needed."
  say "Manual regen:"
  say "  AGENTLAYER_SYNC_ROOTS=1 node src/sync/sync.mjs"
fi
