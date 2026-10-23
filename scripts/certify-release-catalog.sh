#!/usr/bin/env bash
set -euo pipefail
# Compatibility entry point; catalog access is no longer a core release gate.
exec "$(dirname "${BASH_SOURCE[0]}")/certify-release.sh" "$@"
