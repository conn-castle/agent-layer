#!/usr/bin/env bash
set -euo pipefail

# .agentlayer/clean.sh
# Remove generated files produced by agentlayer sync.
# Usage:
#   ./.agentlayer/clean.sh

say() { printf "%s\n" "$*"; }
die() { printf "ERROR: %s\n" "$*" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "$REPO_ROOT"

[[ -d ".agentlayer" ]] || die "Missing .agentlayer/ directory."

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
  ".codex/rules/agentlayer.rules"
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
