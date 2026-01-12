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
Usage: tests/run.sh [--parent-root <path>] [--temp-parent-root]

Run formatting checks and the Bats suite.

In the agent-layer repo (no .agent-layer/ directory), use --temp-parent-root
or pass --parent-root to a temp directory. In a consumer repo, --parent-root
must point to the repo root that contains .agent-layer/.
USAGE
}

parent_root=""
use_temp_parent_root="0"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --temp-parent-root)
      use_temp_parent_root="1"
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
ROOTS_HELPER="$SCRIPT_DIR/../src/lib/parent-root.sh"
if [[ ! -f "$ROOTS_HELPER" ]]; then
  die "Missing src/lib/parent-root.sh (expected near tests/)."
fi
# shellcheck disable=SC1090
source "$ROOTS_HELPER"
ROOTS_PARENT_ROOT="$parent_root" \
  ROOTS_USE_TEMP_PARENT_ROOT="$use_temp_parent_root" \
  resolve_parent_root || exit $?

if [[ "$TEMP_PARENT_ROOT_CREATED" == "1" ]]; then
  # shellcheck disable=SC2153
  trap '[[ "${PARENT_ROOT_KEEP_TEMP:-0}" == "1" ]] || rm -rf "$PARENT_ROOT"' EXIT INT TERM
fi

cd "$PARENT_ROOT"

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

# Collect shell sources for formatting and linting.
say "==> Shell format check (shfmt)"
shell_files=()
while IFS= read -r -d '' file; do
  shell_files+=("$file")
done < <(
  find "$AGENT_LAYER_ROOT" \
    \( -type d \( -name node_modules -o -name .git -o -name tmp \) -prune \) -o \
    -type f \( -name "*.sh" -o -path "$AGENT_LAYER_ROOT/al" -o -path "$AGENT_LAYER_ROOT/.githooks/pre-commit" \) \
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
