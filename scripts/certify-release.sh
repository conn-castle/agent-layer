#!/usr/bin/env bash
# Compatibility wrapper for `make release-certify`: runs the nonpublishing
# signed release rehearsal via scripts/release.sh.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
export AL_RELEASE_REHEARSAL=1
exec "${script_dir}/release.sh" "$@"
