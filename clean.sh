#!/usr/bin/env bash
set -euo pipefail

# .agent-layer/clean.sh
# Remove generated files produced by agent-layer sync.
# Usage:
#   ./.agent-layer/clean.sh [--parent-root <path>] [--temp-parent-root]

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

parent_root=""
use_temp_parent_root="0"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --parent-root)
      shift
      if [[ $# -eq 0 || -z "${1:-}" ]]; then
        die "--parent-root requires a path."
      fi
      parent_root="$1"
      ;;
    --parent-root=*)
      parent_root="${1#*=}"
      if [[ -z "$parent_root" ]]; then
        die "--parent-root requires a path."
      fi
      ;;
    --temp-parent-root)
      use_temp_parent_root="1"
      ;;
    --help | -h)
      echo "Usage: ./.agent-layer/clean.sh [--parent-root <path>] [--temp-parent-root]"
      exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
  shift
done

# Resolve the entrypoint helper to locate the repo root.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
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
ROOTS_PARENT_ROOT="$parent_root" ROOTS_USE_TEMP_PARENT_ROOT="$use_temp_parent_root" resolve_entrypoint_root || exit $?

if [[ "${TEMP_PARENT_ROOT_CREATED:-0}" == "1" ]]; then
  # shellcheck disable=SC2153
  trap '[[ "${PARENT_ROOT_KEEP_TEMP:-0}" == "1" ]] || rm -rf "$PARENT_ROOT"' EXIT INT TERM
fi

# Ensure all path operations run from the repo root.
cd "$PARENT_ROOT"

# Confirm the clean helper is available before proceeding.
[[ -f "$AGENT_LAYER_ROOT/src/sync/sync.mjs" ]] || die "Missing .agent-layer/src/sync/sync.mjs."

# Detect whether any managed settings files exist before invoking Node.
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

# Clean managed settings via the Node helper when needed.
if [[ "$should_clean_settings" == "1" ]]; then
  command -v node > /dev/null 2>&1 || die "Node.js is required (node not found). Install Node, then re-run."
  [[ -f "$AGENT_LAYER_ROOT/src/sync/clean.mjs" ]] || die "Missing .agent-layer/src/sync/clean.mjs."
  say "==> Removing agent-layer-managed settings"
  node "$AGENT_LAYER_ROOT/src/sync/clean.mjs"
fi

# Enumerate generated files and Codex skills that should be removed.
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

# Remove generated files and track what was removed vs missing.
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

# Remove empty skill directories after deleting SKILL.md.
for skill_file in "${skill_files[@]}"; do
  skill_dir="$(dirname "$skill_file")"
  if [[ -d "$skill_dir" ]] && [[ -z "$(ls -A "$skill_dir")" ]]; then
    rmdir -- "$skill_dir"
    removed+=("${skill_dir}/")
  fi
done

# Remove the skills root if it is now empty.
if [[ -d ".codex/skills" ]] && [[ -z "$(ls -A ".codex/skills")" ]]; then
  rmdir -- ".codex/skills"
  removed+=(".codex/skills/")
fi

# Report removals, or confirm no action was needed.
if [[ "${#removed[@]}" -eq 0 ]]; then
  say "No generated files removed."
else
  say "Removed generated files:"
  for path in "${removed[@]}"; do
    say "  - $path"
  done
fi

# Report files that were already absent.
if [[ "${#missing[@]}" -gt 0 ]]; then
  say ""
  say "Not found (already clean or never generated):"
  for path in "${missing[@]}"; do
    say "  - $path"
  done
fi
