#!/usr/bin/env bash
set -euo pipefail

# Format shell scripts and JavaScript sources for agent-layer development.

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

# Resolve the agent-layer root from this script location.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
AGENT_LAYER_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -P)"

# Require commands that this formatter depends on.
require_cmd() {
  local cmd="$1" hint="$2"
  if ! command -v "$cmd" > /dev/null 2>&1; then
    die "$cmd not found. $hint"
  fi
}

require_cmd shfmt "Install shfmt (macOS: brew install shfmt; Ubuntu: apt-get install shfmt)."

# Resolve the Prettier binary (local install preferred).
PRETTIER_BIN="$AGENT_LAYER_ROOT/node_modules/.bin/prettier"
if [[ -x "$PRETTIER_BIN" ]]; then
  PRETTIER="$PRETTIER_BIN"
elif command -v prettier > /dev/null 2>&1; then
  PRETTIER="$(command -v prettier)"
else
  die "prettier not found. Run: (cd .agent-layer && npm install) or install globally."
fi

# Discover shell sources and format them with shfmt.
say "==> Shell format (shfmt)"
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
  shfmt -w -i 2 -ci -sr "${shell_files[@]}"
fi

# Discover JS sources and format them with Prettier.
say "==> JS format (prettier)"
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
  "$PRETTIER" --write "${js_files[@]}"
fi
