#!/usr/bin/env bash
set -euo pipefail

# .agent-layer/clean.sh
# Remove generated files produced by agent-layer sync.
# Usage:
#   ./.agent-layer/clean.sh

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

cd "$WORKING_ROOT"

[[ -f "$AGENTLAYER_ROOT/src/sync/sync.mjs" ]] || die "Missing .agent-layer/src/sync/sync.mjs."

managed_settings_files=(
  ".gemini/settings.json"
  ".claude/settings.json"
  ".vscode/settings.json"
)

should_clean_settings="0"
for path in "${managed_settings_files[@]}"; do
  if [[ -f "$path" ]]; then
    should_clean_settings="1"
    break
  fi
done

if [[ "$should_clean_settings" == "1" ]]; then
  command -v node > /dev/null 2>&1 || die "Node.js is required (node not found). Install Node, then re-run."
  [[ -f "$AGENTLAYER_ROOT/src/sync/clean.mjs" ]] || die "Missing .agent-layer/src/sync/clean.mjs."
  say "==> Removing agent-layer-managed settings"
  node "$AGENTLAYER_ROOT/src/sync/clean.mjs"
fi

generated_files=(
  "AGENTS.md"
  "CLAUDE.md"
  "GEMINI.md"
  ".github/copilot-instructions.md"
  ".mcp.json"
  ".vscode/mcp.json"
  ".codex/AGENTS.md"
  ".codex/config.toml"
  ".codex/rules/default.rules"
)

shopt -s nullglob
skill_files=(.codex/skills/*/SKILL.md)
shopt -u nullglob

removed=()
missing=()

for path in "${generated_files[@]}" "${skill_files[@]}"; do
  if [[ -e "$path" ]]; then
    rm -- "$path"
    removed+=("$path")
  else
    missing+=("$path")
  fi
done

for skill_file in "${skill_files[@]}"; do
  skill_dir="$(dirname "$skill_file")"
  if [[ -d "$skill_dir" ]] && [[ -z "$(ls -A "$skill_dir")" ]]; then
    rmdir -- "$skill_dir"
    removed+=("${skill_dir}/")
  fi
done

if [[ -d ".codex/skills" ]] && [[ -z "$(ls -A ".codex/skills")" ]]; then
  rmdir -- ".codex/skills"
  removed+=(".codex/skills/")
fi

if [[ "${#removed[@]}" -eq 0 ]]; then
  say "No generated files removed."
else
  say "Removed generated files:"
  for path in "${removed[@]}"; do
    say "  - $path"
  done
fi

if [[ "${#missing[@]}" -gt 0 ]]; then
  say ""
  say "Not found (already clean or never generated):"
  for path in "${missing[@]}"; do
    say "  - $path"
  done
fi
