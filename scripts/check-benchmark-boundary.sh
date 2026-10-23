#!/usr/bin/env bash
set -euo pipefail

# Check the actual transitive build graph, rather than one direct import. Core
# releases must stay independent of the optional benchmark engine and assets.
dependencies="$(go list -deps ./cmd/al)"
if grep -Eq '^github.com/conn-castle/agent-layer/internal/benchmark(/|$)' <<<"${dependencies}"; then
  echo 'ERROR: cmd/al links the optional benchmark engine' >&2
  exit 1
fi
echo 'Core executable excludes the optional benchmark engine.'
