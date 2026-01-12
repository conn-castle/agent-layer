#!/usr/bin/env bash
set -euo pipefail

# Run formatting checks and the Bats test suite.

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

# Parse work-root flags so the runner can be invoked from the agent-layer repo.
usage() {
  cat << 'EOF'
Usage: tests/run.sh [--work-root <path>] [--temp-work-root]

Run formatting checks and the Bats suite.

In the agent-layer repo (no .agent-layer/ directory), use --temp-work-root
or pass --work-root to a temp directory. --temp-work-root uses system temp
(or tmp/agent-layer-temp-work-root). In a consumer repo, --work-root must
point to the repo root that contains .agent-layer/.
EOF
}

work_root=""
use_temp_work_root="0"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --temp-work-root)
      use_temp_work_root="1"
      shift
      ;;
    --work-root)
      shift
      if [[ $# -eq 0 || -z "${1:-}" ]]; then
        die "--work-root requires a path."
      fi
      work_root="$1"
      shift
      ;;
    --work-root=*)
      work_root="${1#*=}"
      if [[ -z "$work_root" ]]; then
        die "--work-root requires a path."
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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOTS_HELPER="$SCRIPT_DIR/../src/lib/discover-root.sh"
if [[ ! -f "$ROOTS_HELPER" ]]; then
  die "Missing src/lib/discover-root.sh (expected near tests/)."
fi
# shellcheck disable=SC1090
source "$ROOTS_HELPER"
ROOTS_ALLOW_CONSUMER_WORK_ROOT="1" \
  ROOTS_DEFAULT_TEMP_WORK_ROOT="0" \
  ROOTS_REQUIRE_WORK_ROOT_IN_AGENT_LAYER="1" \
  WORK_ROOT="$work_root" \
  USE_TEMP_WORK_ROOT="$use_temp_work_root" \
  resolve_roots

if [[ "$TEMP_WORK_ROOT_CREATED" == "1" ]]; then
  trap 'rm -rf "$WORKING_ROOT"' EXIT
fi
if [[ -n "$work_root" || "$TEMP_WORK_ROOT_CREATED" == "1" ]]; then
  cd "$WORKING_ROOT"
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
PRETTIER_BIN="$AGENTLAYER_ROOT/node_modules/.bin/prettier"
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
  find "$AGENTLAYER_ROOT" \
    \( -type d \( -name node_modules -o -name .git -o -name tmp \) -prune \) -o \
    -type f \( -name "*.sh" -o -path "$AGENTLAYER_ROOT/al" -o -path "$AGENTLAYER_ROOT/.githooks/pre-commit" \) \
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
  find "$AGENTLAYER_ROOT" \
    \( -type d \( -name node_modules -o -name .git -o -name tmp \) -prune \) -o \
    -type f \( -name "*.mjs" -o -name "*.js" \) \
    -print0
)
if [[ "${#js_files[@]}" -gt 0 ]]; then
  "$PRETTIER" --check "${js_files[@]}"
fi

# Run the Bats test suite.
say "==> Tests (bats)"
"$BATS_BIN" "$AGENTLAYER_ROOT/tests"
