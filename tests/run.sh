#!/usr/bin/env bash
set -euo pipefail

# Run formatting checks and the Bats test suite.

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

# Parse parent-root flags so the runner can be invoked from the agent-layer repo.
usage() {
  cat << 'USAGE'
Usage: tests/run.sh [--parent-root <path>] [--temp-parent-root] [--run-from-repo-root] [--setup-only]

Run formatting checks and the Bats suite.

In the agent-layer repo (no .agent-layer/ directory), use --temp-parent-root
or pass --parent-root to a temp directory. In a consumer repo, --parent-root
must point to the repo root that contains .agent-layer/.

Use --run-from-repo-root to keep the current working directory unchanged.
Use --setup-only to validate setup and resolve paths without running tests.
USAGE
}

parent_root=""
use_temp_parent_root="0"
run_from_repo_root="0"
setup_only="0"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --temp-parent-root)
      use_temp_parent_root="1"
      shift
      ;;
    --run-from-repo-root)
      run_from_repo_root="1"
      shift
      ;;
    --setup-only)
      setup_only="1"
      shift
      ;;
    --parent-root)
      shift
      if [[ $# -eq 0 || -z "${1:-}" ]]; then
        die "--parent-root requires a path."
      fi
      parent_root="$1"
      shift
      ;;
    --parent-root=*)
      parent_root="${1#*=}"
      if [[ -z "$parent_root" ]]; then
        die "--parent-root requires a path."
      fi
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      die "Unknown option: $1 (run --help for usage)."
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
AGENT_LAYER_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -P)"
ROOTS_OUTPUT="$(
  cd "$AGENT_LAYER_ROOT" &&
    ROOTS_PARENT_ROOT="$parent_root" \
      ROOTS_USE_TEMP_PARENT_ROOT="$use_temp_parent_root" \
      ROOTS_AGENT_LAYER_ROOT="$AGENT_LAYER_ROOT" \
      node --input-type=module << 'NODE' 2>&1
import { resolveParentRoot } from "./src/lib/roots.mjs";
const parentRoot = process.env.ROOTS_PARENT_ROOT || null;
const useTempParentRoot = process.env.ROOTS_USE_TEMP_PARENT_ROOT === "1";
const agentLayerRoot = process.env.ROOTS_AGENT_LAYER_ROOT || null;
const roots = resolveParentRoot({ parentRoot, useTempParentRoot, agentLayerRoot, cwd: process.cwd() });
console.log(`PARENT_ROOT=${roots.parentRoot}`);
console.log(`AGENT_LAYER_ROOT=${roots.agentLayerRoot}`);
console.log(`TEMP_PARENT_ROOT_CREATED=${roots.tempParentRootCreated ? "1" : "0"}`);
NODE
)" || {
  printf "%s\n" "$ROOTS_OUTPUT" >&2
  exit 1
}

PARENT_ROOT=""
AGENT_LAYER_ROOT=""
TEMP_PARENT_ROOT_CREATED="0"
while IFS= read -r line; do
  case "$line" in
    PARENT_ROOT=*) PARENT_ROOT="${line#PARENT_ROOT=}" ;;
    AGENT_LAYER_ROOT=*) AGENT_LAYER_ROOT="${line#AGENT_LAYER_ROOT=}" ;;
    TEMP_PARENT_ROOT_CREATED=*) TEMP_PARENT_ROOT_CREATED="${line#TEMP_PARENT_ROOT_CREATED=}" ;;
  esac
done <<< "$ROOTS_OUTPUT"

if [[ -z "$PARENT_ROOT" || -z "$AGENT_LAYER_ROOT" ]]; then
  printf "ERROR: Failed to resolve roots.\n" >&2
  printf "%s\n" "$ROOTS_OUTPUT" >&2
  exit 1
fi

if [[ "$TEMP_PARENT_ROOT_CREATED" == "1" ]]; then
  # shellcheck disable=SC2153
  trap '[[ "${PARENT_ROOT_KEEP_TEMP:-0}" == "1" ]] || rm -rf "$PARENT_ROOT"' EXIT INT TERM
fi

if [[ "$run_from_repo_root" != "1" ]]; then
  cd "$PARENT_ROOT"
fi

# Require external tools used by formatting and tests.
require_cmd() {
  local cmd="$1" hint="$2"
  if ! command -v "$cmd" > /dev/null 2>&1; then
    die "$cmd not found. $hint"
  fi
}

require_cmd git "Install git (dev-only)."
require_cmd node "Install Node.js (dev-only)."
require_cmd rg "Install ripgrep (macOS: brew install ripgrep; Ubuntu: apt-get install ripgrep)."
require_cmd shfmt "Install shfmt (macOS: brew install shfmt; Ubuntu: apt-get install shfmt)."
require_cmd shellcheck "Install shellcheck (macOS: brew install shellcheck; Ubuntu: apt-get install shellcheck)."

# Resolve the Bats binary (allow override via BATS_BIN).
BATS_BIN="${BATS_BIN:-bats}"
if ! command -v "$BATS_BIN" > /dev/null 2>&1; then
  die "bats not found. Install bats-core (macOS: brew install bats-core; Ubuntu: apt-get install bats)."
fi

# Resolve Prettier (local install preferred).
PRETTIER_BIN="$AGENT_LAYER_ROOT/node_modules/.bin/prettier"
if [[ -x "$PRETTIER_BIN" ]]; then
  PRETTIER="$PRETTIER_BIN"
elif command -v prettier > /dev/null 2>&1; then
  PRETTIER="$(command -v prettier)"
else
  die "prettier not found. Run: (cd .agent-layer && npm install) or install globally."
fi

# Exit early if only validating setup.
if [[ "$setup_only" == "1" ]]; then
  say "Setup validated successfully."
  say "PARENT_ROOT=$PARENT_ROOT"
  say "AGENT_LAYER_ROOT=$AGENT_LAYER_ROOT"
  exit 0
fi

# Collect shell sources for formatting and linting.
say "==> Shell format check (shfmt)"
shell_files=()
while IFS= read -r -d '' file; do
  shell_files+=("$file")
done < <(
  find "$AGENT_LAYER_ROOT" \
    \( -type d \( -name node_modules -o -name .git -o -name tmp \) -prune \) -o \
    -type f \( -name "*.sh" -o -path "$AGENT_LAYER_ROOT/agent-layer" -o -path "$AGENT_LAYER_ROOT/.githooks/pre-commit" \) \
    -print0
)
if [[ "${#shell_files[@]}" -gt 0 ]]; then
  shfmt -d -i 2 -ci -sr "${shell_files[@]}"
fi

# Run shellcheck against the same shell sources.
say "==> Shell lint (shellcheck)"
if [[ "${#shell_files[@]}" -gt 0 ]]; then
  shellcheck "${shell_files[@]}"
fi

# Collect JS sources for formatting checks.
say "==> JS format check (prettier)"
js_files=()
while IFS= read -r -d '' file; do
  js_files+=("$file")
done < <(
  find "$AGENT_LAYER_ROOT" \
    \( -type d \( -name node_modules -o -name .git -o -name tmp \) -prune \) -o \
    -type f \( -name "*.mjs" -o -name "*.js" \) \
    -print0
)
if [[ "${#js_files[@]}" -gt 0 ]]; then
  "$PRETTIER" --check "${js_files[@]}"
fi

# Run the Bats test suite.
say "==> Tests (bats)"
"$BATS_BIN" "$AGENT_LAYER_ROOT/tests"

# Run Node unit tests (lib-*).
say "==> Node tests (node --test)"
lib_tests=("$AGENT_LAYER_ROOT/tests/lib-"*.test.mjs)
if [[ "${lib_tests[0]}" == "$AGENT_LAYER_ROOT/tests/lib-*.test.mjs" ]]; then
  say "No Node unit tests found."
else
  node --test "${lib_tests[@]}"
fi

# Run MCP server E2E test (requires SDK).
say "==> MCP E2E test"
mcp_sdk_root="$AGENT_LAYER_ROOT/node_modules/@modelcontextprotocol/sdk"
mcp_sdk_nested="$AGENT_LAYER_ROOT/src/mcp/agent-layer-prompts/node_modules/@modelcontextprotocol/sdk"
if [[ ! -d "$mcp_sdk_root" && ! -d "$mcp_sdk_nested" ]]; then
  echo "ERROR: MCP server dependencies not installed." >&2
  echo "Run: cd $AGENT_LAYER_ROOT/src/mcp/agent-layer-prompts && npm install" >&2
  exit 1
fi
node "$AGENT_LAYER_ROOT/tests/mcp-runtime.mjs"
