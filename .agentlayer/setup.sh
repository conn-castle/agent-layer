#!/usr/bin/env bash
set -euo pipefail

# .agentlayer/setup.sh
# One-shot setup for agentlayer in this repo:
# - installs MCP prompt server deps
# - runs sync to generate shims/configs/skills
# - enables & tests git hooks
#
# Usage:
#   ./.agentlayer/setup.sh

say() { printf "%s\n" "$*"; }
die() { printf "ERROR: %s\n" "$*" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "$REPO_ROOT"

[[ -d ".agentlayer" ]] || die "Missing .agentlayer/ directory. Run the bootstrap script in the repo root first."
[[ -f ".agentlayer/sync/sync.mjs" ]] || die "Missing .agentlayer/sync/sync.mjs. Re-run bootstrap or restore it."

command -v node >/dev/null 2>&1 || die "Node.js is required (node not found). Install Node, then re-run."
command -v npm >/dev/null 2>&1 || die "npm is required (npm not found). Install npm/Node, then re-run."
command -v git >/dev/null 2>&1 || die "git is required (git not found)."

# Ensure we're in a git repo (recommended for hooks).
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  say "NOTE: Not inside a git repository. Hooks will not be enabled."
  IN_GIT_REPO="0"
else
  IN_GIT_REPO="1"
fi

say "==> Running agentlayer sync"
node .agentlayer/sync/sync.mjs

say "==> Installing MCP prompt server dependencies"
if [[ -f ".agentlayer/mcp/agentlayer-prompts/package.json" ]]; then
  pushd ".agentlayer/mcp/agentlayer-prompts" >/dev/null
  npm install
  popd >/dev/null
else
  die "Missing .agentlayer/mcp/agentlayer-prompts/package.json"
fi

if [[ "$IN_GIT_REPO" == "1" ]]; then
  say "==> Enabling git hooks (core.hooksPath=.githooks)"
  git config core.hooksPath .githooks

  if [[ -f ".githooks/pre-commit" ]]; then
    chmod +x ".githooks/pre-commit" 2>/dev/null || true
  else
    die "Missing .githooks/pre-commit"
  fi

  say "==> Testing pre-commit hook"
  # Run hook directly from repo root.
  ./.githooks/pre-commit
else
  say "Skipping hook enable/test (not a git repo)."
fi

say "==> Verifying sync is up-to-date (check mode)"
node .agentlayer/sync/sync.mjs --check

say ""
say "Setup complete."
say ""
say "Next:"
say "  - Edit instructions: .agentlayer/instructions/*.md"
say "  - Edit workflows:    .agentlayer/workflows/*.md"
say "  - Edit MCP servers:  .agentlayer/mcp/servers.json"
say "  - Regenerate:        node .agentlayer/sync/sync.mjs"
say ""
say "Secrets:"
say "  - Copy .env.example -> .env (do not commit)"
say "  - Use: ./.agentlayer/with-env.sh <cmd> to load .env for CLIs"
