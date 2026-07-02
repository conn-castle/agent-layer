#!/usr/bin/env bash
# Sign release-grade macOS binaries with Hardened Runtime and an Apple secure
# timestamp so the binaries can be notarized and remain valid after the signing
# certificate expires.

set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  exit 0
fi

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <binary-path>" >&2
  exit 2
fi

binary="$1"
identifier="com.conncastle.agent-layer"

if [[ ! -f "$binary" ]]; then
  echo "codesign-release: binary not found: $binary" >&2
  exit 1
fi

identity="${AL_CODESIGN_IDENTITY:-}"
if [[ -z "$identity" ]]; then
  echo "codesign-release: AL_CODESIGN_IDENTITY is required for release signing." >&2
  exit 1
fi

codesign \
  --sign "$identity" \
  --identifier "$identifier" \
  --force \
  --options runtime \
  --timestamp \
  "$binary" >/dev/null
