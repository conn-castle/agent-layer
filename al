#!/usr/bin/env bash
set -euo pipefail

# ./al - repo-local launcher
#
# Purpose:
#   Select a single default behavior for the Agent Layer runner.
#
# Instructions:
#   - Uncomment exactly one option in the "Options" section below.
#   - Keep all other options commented.
#   - Do not edit the "path glue" section below.
#   - Do not leave multiple options uncommented.

# +------------------------------------------------------------+
# | al path glue (do not edit)                                 |
# | Keeps ./al working as a symlink or as .agent-layer/al.      |
# | Resolves RUNNER to the correct .agent-layer/run.sh.         |
# +------------------------------------------------------------+
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# Prefer a runner alongside this script (when invoked via .agent-layer/al).
RUNNER="$SCRIPT_DIR/run.sh"
# Fall back to the repo-root .agent-layer/run.sh (when invoked via ./al).
if [[ ! -f "$RUNNER" ]]; then
  # Use the repo-root runner when the local path is missing.
  RUNNER="$SCRIPT_DIR/.agent-layer/run.sh"
fi
RUNNER_DIR="$(cd "$(dirname "$RUNNER")" && pwd)"

# Optional update check. Comment this out to skip update checks.
"$RUNNER_DIR/check-updates.sh" || true

# Options (choose one). Keep exactly one "exec" line uncommented.
#
# Option A (default): sync every run, load only .agent-layer/.env, then exec.
exec "$RUNNER" "$@"

# Option B: load .agent-layer/.env only (no sync).
# exec "$RUNNER" --env-only "$@"

# Option C: sync only (do not load any .env files).
# exec "$RUNNER" --sync-only "$@"

# Option D: check sync state and regenerate if stale, then env-only.
# exec "$RUNNER" --check-env "$@"

# Option E: sync every run, load .agent-layer/.env and .env, then exec.
# exec "$RUNNER" --project-env "$@"
