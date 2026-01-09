#!/usr/bin/env bash
set -euo pipefail

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PATHS_SH="$SCRIPT_DIR/.agent-layer/lib/paths.sh"
if [[ ! -f "$PATHS_SH" ]]; then
  PATHS_SH="$SCRIPT_DIR/lib/paths.sh"
fi
if [[ ! -f "$PATHS_SH" ]]; then
  PATHS_SH="$SCRIPT_DIR/../lib/paths.sh"
fi
if [[ ! -f "$PATHS_SH" ]]; then
  die "Missing lib/paths.sh (expected near .agent-layer/)."
fi
# shellcheck disable=SC1090
source "$PATHS_SH"

WORKING_ROOT="$(resolve_working_root "$SCRIPT_DIR" "$PWD" || true)"
[[ -n "$WORKING_ROOT" ]] || die "Missing .agent-layer/ directory in this path or any parent."

AGENTLAYER_ROOT="$WORKING_ROOT/.agent-layer"

require_cmd() {
  local cmd="$1" hint="$2"
  if ! command -v "$cmd" > /dev/null 2>&1; then
    die "$cmd not found. $hint"
  fi
}

require_cmd shfmt "Install shfmt (macOS: brew install shfmt; Ubuntu: apt-get install shfmt)."

PRETTIER_BIN="$AGENTLAYER_ROOT/node_modules/.bin/prettier"
if [[ -x "$PRETTIER_BIN" ]]; then
  PRETTIER="$PRETTIER_BIN"
elif command -v prettier > /dev/null 2>&1; then
  PRETTIER="$(command -v prettier)"
else
  die "prettier not found. Run: (cd .agent-layer && npm install) or install globally."
fi

say "==> Shell format (shfmt)"
shell_files=()
while IFS= read -r -d '' file; do
  shell_files+=("$file")
done < <(
  find "$AGENTLAYER_ROOT" \
    -path "$AGENTLAYER_ROOT/node_modules" -prune -o \
    -path "$AGENTLAYER_ROOT/.git" -prune -o \
    -type f \( -name "*.sh" -o -path "$AGENTLAYER_ROOT/al" -o -path "$AGENTLAYER_ROOT/.githooks/pre-commit" \) \
    -print0
)
if [[ "${#shell_files[@]}" -gt 0 ]]; then
  shfmt -w -i 2 -ci -sr "${shell_files[@]}"
fi

say "==> JS format (prettier)"
js_files=()
while IFS= read -r -d '' file; do
  js_files+=("$file")
done < <(
  find "$AGENTLAYER_ROOT" \
    -path "$AGENTLAYER_ROOT/node_modules" -prune -o \
    -path "$AGENTLAYER_ROOT/.git" -prune -o \
    -type f \( -name "*.mjs" -o -name "*.js" \) \
    -print0
)
if [[ "${#js_files[@]}" -gt 0 ]]; then
  "$PRETTIER" --write "${js_files[@]}"
fi
