#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BATS_BIN="${BATS_BIN:-bats}"
if ! command -v "$BATS_BIN" >/dev/null 2>&1; then
  echo "ERROR: bats not found. Install bats-core (macOS: brew install bats-core; Ubuntu: apt-get install bats)." >&2
  exit 2
fi

"$BATS_BIN" "$SCRIPT_DIR"
