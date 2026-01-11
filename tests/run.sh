#!/usr/bin/env bash
set -euo pipefail

# Run formatting checks and the Bats test suite.

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

# Parse --work-root so the runner can be invoked from the agent-layer repo.
usage() {
  cat <<'EOF'
Usage: tests/run.sh [--work-root <path>]

Run formatting checks and the Bats suite. When running from inside the
agent-layer repo itself, pass --work-root to a consumer root that contains
a .agent-layer/ directory.
EOF
}

work_root=""
while [[ $# -gt 0 ]]; do
  case "$1" in
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
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "Unknown option: $1 (run --help for usage)."
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PATHS_SH="$REPO_ROOT/src/lib/paths.sh"
if [[ ! -f "$PATHS_SH" ]]; then
  die "Missing src/lib/paths.sh (expected in the agent-layer repo)."
fi
# shellcheck disable=SC1090
source "$PATHS_SH"
if [[ -n "$work_root" ]]; then
  if [[ ! -d "$work_root" ]]; then
    die "--work-root does not exist: $work_root"
  fi
  work_root="$(cd "$work_root" && pwd)"
  if [[ ! -d "$work_root/.agent-layer" ]]; then
    die "--work-root must contain a .agent-layer directory: $work_root"
  fi
  cd "$work_root"
else
  if ! resolve_working_root "$SCRIPT_DIR" "$PWD" > /dev/null; then
    die "Missing .agent-layer/ directory in this path or any parent. Re-run with --work-root <path>."
  fi
fi

# Resolve entrypoint helpers so the runner works from any directory.
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

# Require external tools used by formatting and tests.
require_cmd() {
  local cmd="$1" hint="$2"
  if ! command -v "$cmd" > /dev/null 2>&1; then
    die "$cmd not found. $hint"
  fi
}

require_cmd git "Install git (dev-only)."
require_cmd node "Install Node.js (dev-only)."
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
    -path "$AGENTLAYER_ROOT/node_modules" -prune -o \
    -path "$AGENTLAYER_ROOT/.git" -prune -o \
    -path "$AGENTLAYER_ROOT/tmp" -prune -o \
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
    -path "$AGENTLAYER_ROOT/node_modules" -prune -o \
    -path "$AGENTLAYER_ROOT/.git" -prune -o \
    -path "$AGENTLAYER_ROOT/tmp" -prune -o \
    -type f \( -name "*.mjs" -o -name "*.js" \) \
    -print0
)
if [[ "${#js_files[@]}" -gt 0 ]]; then
  "$PRETTIER" --check "${js_files[@]}"
fi

# Run the Bats test suite.
say "==> Tests (bats)"
"$BATS_BIN" "$AGENTLAYER_ROOT/tests"
