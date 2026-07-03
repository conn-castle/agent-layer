#!/usr/bin/env bash
# Submit signed macOS CLI binaries for Apple notarization. Bare Mach-O CLI
# binaries cannot be stapled, so the release keeps the signed binary and relies
# on Gatekeeper's online notarization check for quarantined browser downloads.

set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  exit 0
fi

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="${root_dir}/dist"

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "notarize-release: $name is required." >&2
    exit 1
  fi
}

notary_status_from_output() {
  sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1" | tail -n 1
}

submission_id_from_output() {
  sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1" | sed -n '1p'
}

require_env NOTARY_KEY_ID
require_env NOTARY_ISSUER_ID
require_env NOTARY_KEY_P8_BASE64

binaries=(
  "${dist_dir}/al-darwin-arm64"
  "${dist_dir}/al-darwin-amd64"
)

for binary in "${binaries[@]}"; do
  if [[ ! -f "$binary" ]]; then
    echo "notarize-release: binary not found: $binary" >&2
    exit 1
  fi
done

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

key_path="${tmp_dir}/AuthKey_${NOTARY_KEY_ID}.p8"
printf '%s' "$NOTARY_KEY_P8_BASE64" | base64 -D >"$key_path"
chmod 600 "$key_path"

for binary in "${binaries[@]}"; do
  archive="${tmp_dir}/$(basename "$binary").zip"
  # Keep notarytool's JSON on stdout and its human-readable progress/errors on
  # stderr in separate files. Mixing them corrupts the JSON that the status/id
  # parsers read, so only log_path (stdout) is ever parsed.
  log_path="${tmp_dir}/$(basename "$binary").notarytool.json"
  err_path="${tmp_dir}/$(basename "$binary").notarytool.err"

  echo "Notarizing $(basename "$binary")..."
  ditto -c -k --keepParent "$binary" "$archive"

  if ! xcrun notarytool submit "$archive" \
    --key "$key_path" \
    --key-id "$NOTARY_KEY_ID" \
    --issuer "$NOTARY_ISSUER_ID" \
    --wait \
    --timeout 30m \
    --output-format json >"$log_path" 2>"$err_path"; then
    cat "$log_path" "$err_path" >&2
    submission_id="$(submission_id_from_output "$log_path")"
    if [[ -n "$submission_id" ]]; then
      xcrun notarytool log "$submission_id" \
        --key "$key_path" \
        --key-id "$NOTARY_KEY_ID" \
        --issuer "$NOTARY_ISSUER_ID" || true
    fi
    exit 1
  fi

  status="$(notary_status_from_output "$log_path")"
  if [[ "$status" != "Accepted" ]]; then
    echo "notarize-release: notarization for $(basename "$binary") returned status ${status:-unknown}; expected Accepted." >&2
    cat "$log_path" "$err_path" >&2
    exit 1
  fi
done
