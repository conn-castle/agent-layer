#!/usr/bin/env bash
set -euo pipefail

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

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

BATS_BIN="${BATS_BIN:-bats}"
if ! command -v "$BATS_BIN" > /dev/null 2>&1; then
  die "bats not found. Install bats-core (macOS: brew install bats-core; Ubuntu: apt-get install bats)."
fi

PRETTIER_BIN="$AGENTLAYER_ROOT/node_modules/.bin/prettier"
if [[ -x "$PRETTIER_BIN" ]]; then
  PRETTIER="$PRETTIER_BIN"
elif command -v prettier > /dev/null 2>&1; then
  PRETTIER="$(command -v prettier)"
else
  die "prettier not found. Run: (cd .agent-layer && npm install) or install globally."
fi

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

say "==> Shell lint (shellcheck)"
if [[ "${#shell_files[@]}" -gt 0 ]]; then
  shellcheck "${shell_files[@]}"
fi

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

say "==> Tests (bats)"
"$BATS_BIN" "$AGENTLAYER_ROOT/tests"
