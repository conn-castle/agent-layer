#!/usr/bin/env bash
set -Eeuo pipefail

fail() {
  trap - ERR
  echo "Error: $*" >&2
  exit 1
}

on_error() {
  local exit_code=$?
  local line_no="$1"
  local cmd="$2"
  echo "Error: installer failed (exit code ${exit_code}) at line ${line_no}: ${cmd}" >&2
  exit "$exit_code"
}

trap 'on_error ${LINENO} "$BASH_COMMAND"' ERR

VERSION="latest"
NO_WIZARD=false

usage() {
  echo "Usage: $0 [--version <tag>] [--no-wizard]" >&2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      if [[ $# -lt 2 ]]; then
        usage
        exit 1
      fi
      VERSION="$2"
      shift 2
      ;;
    --no-wizard)
      NO_WIZARD=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to install Agent Layer." >&2
  exit 1
fi

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin)
    OS="darwin"
    ;;
  Linux)
    OS="linux"
    ;;
  *)
    echo "Unsupported OS: $OS" >&2
    exit 1
    ;;
 esac

case "$ARCH" in
  x86_64|amd64)
    ARCH="amd64"
    ;;
  arm64|aarch64)
    ARCH="arm64"
    ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
 esac

ASSET="al-${OS}-${ARCH}"
BASE_URL="https://github.com/nicholasjconn/agent-layer/releases"
if [[ "$VERSION" == "latest" ]]; then
  URL="${BASE_URL}/latest/download/${ASSET}"
  SUMS_URL="${BASE_URL}/latest/download/SHA256SUMS"
else
  URL="${BASE_URL}/download/${VERSION}/${ASSET}"
  SUMS_URL="${BASE_URL}/download/${VERSION}/SHA256SUMS"
fi

if ! curl -fsSL "$URL" -o ./al; then
  fail "Failed to download ${ASSET} from ${URL}. Check your network or try again."
fi
if ! chmod +x ./al; then
  fail "Failed to mark ./al as executable."
fi

verify_checksum_shasum() {
  local sums_file
  sums_file="$(mktemp)"
  if ! curl -fsSL "$SUMS_URL" -o "$sums_file"; then
    echo "Warning: unable to download SHA256SUMS; skipping checksum verification." >&2
    rm -f "$sums_file"
    return 0
  fi
  local expected
  expected="$(awk -v asset="$ASSET" '{path=$2; sub(/^\.\//, "", path); if (path == asset) {print $1; exit}}' "$sums_file")"
  rm -f "$sums_file"
  if [[ -z "$expected" ]]; then
    echo "Warning: checksum for ${ASSET} not found; skipping verification." >&2
    return 0
  fi
  if ! echo "${expected}  ./al" | shasum -a 256 -c -; then
    fail "Checksum verification failed for ${ASSET}. Delete ./al and retry."
  fi
}

verify_checksum_sha256sum() {
  local sums_file
  sums_file="$(mktemp)"
  if ! curl -fsSL "$SUMS_URL" -o "$sums_file"; then
    echo "Warning: unable to download SHA256SUMS; skipping checksum verification." >&2
    rm -f "$sums_file"
    return 0
  fi
  local expected
  expected="$(awk -v asset="$ASSET" '{path=$2; sub(/^\.\//, "", path); if (path == asset) {print $1; exit}}' "$sums_file")"
  rm -f "$sums_file"
  if [[ -z "$expected" ]]; then
    echo "Warning: checksum for ${ASSET} not found; skipping verification." >&2
    return 0
  fi
  if ! echo "${expected}  ./al" | sha256sum -c -; then
    fail "Checksum verification failed for ${ASSET}. Delete ./al and retry."
  fi
}

if command -v shasum >/dev/null 2>&1; then
  verify_checksum_shasum
elif command -v sha256sum >/dev/null 2>&1; then
  verify_checksum_sha256sum
else
  echo "Warning: shasum/sha256sum not available; skipping checksum verification." >&2
fi

install_args=()
if [[ "$NO_WIZARD" == "true" ]]; then
  install_args+=(--no-wizard)
fi

if ! ./al install "${install_args[@]}"; then
  fail "Agent Layer install failed. Run this from the repo root where you want .agent-layer/ created."
fi
