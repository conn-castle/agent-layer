#!/usr/bin/env bash
set -euo pipefail

# .agent-layer/clean.sh
# Remove generated files produced by agent-layer sync.
# Usage:
#   ./.agent-layer/clean.sh

say() { printf "%s\n" "$*"; }
die() { printf "ERROR: %s\n" "$*" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PATHS_SH="$SCRIPT_DIR/lib/paths.sh"
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

cd "$WORKING_ROOT"

[[ -f "$AGENTLAYER_ROOT/sync/sync.mjs" ]] || die "Missing .agent-layer/sync/sync.mjs."

generated_files=(
  "AGENTS.md"
  "CLAUDE.md"
  "GEMINI.md"
  ".github/copilot-instructions.md"
  ".mcp.json"
  ".gemini/settings.json"
  ".claude/settings.json"
  ".vscode/mcp.json"
  ".vscode/settings.json"
  ".codex/AGENTS.md"
  ".codex/config.toml"
  ".codex/rules/agent-layer.rules"
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
