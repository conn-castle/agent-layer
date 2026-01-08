#!/usr/bin/env bash
set -euo pipefail

# ./al - repo-local launcher
#
# Edit this file to choose a single default behavior.
# Uncomment exactly one option below (leave the rest commented).

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

# Option A (default): sync every run, load only .agentlayer/.env, then exec.
node .agentlayer/sync/sync.mjs
exec "$ROOT/.agentlayer/with-env.sh" "$@"

# Option B: env-only (no sync).
# exec "$ROOT/.agentlayer/with-env.sh" "$@"

# Option C: sync-only (no env).
# exec node .agentlayer/sync/sync.mjs "$@"

# Option D: sync check + regen if stale, then env-only.
# node .agentlayer/sync/sync.mjs --check || node .agentlayer/sync/sync.mjs
# exec "$ROOT/.agentlayer/with-env.sh" "$@"

# Option E: sync every run, load .agentlayer/.env + .env, then exec.
# node .agentlayer/sync/sync.mjs
# exec "$ROOT/.agentlayer/with-env.sh" --project-env "$@"
