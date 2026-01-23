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
PREFIX="${HOME}/.local"
NO_COMPLETIONS=false
SHELL_OVERRIDE=""
ASSET_ROOT="${AL_INSTALL_ASSET_ROOT:-}"

usage() {
  cat <<'USAGE' >&2
Usage: al-install.sh [--version <tag>] [--prefix <dir>] [--no-completions] [--shell <bash|zsh|fish>] [--asset-root <dir-or-url>]
USAGE
}

normalize_version() {
  local v="$1"
  v="${v#v}"
  if [[ ! "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    fail "Invalid version: $1 (expected vX.Y.Z or X.Y.Z)"
  fi
  echo "$v"
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
    --prefix)
      if [[ $# -lt 2 ]]; then
        usage
        exit 1
      fi
      PREFIX="$2"
      shift 2
      ;;
    --no-completions)
      NO_COMPLETIONS=true
      shift
      ;;
    --shell)
      if [[ $# -lt 2 ]]; then
        usage
        exit 1
      fi
      SHELL_OVERRIDE="$2"
      shift 2
      ;;
    --asset-root)
      if [[ $# -lt 2 ]]; then
        usage
        exit 1
      fi
      ASSET_ROOT="$2"
      shift 2
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
  fail "curl is required to install Agent Layer."
fi

checksum_tool=""
if command -v sha256sum >/dev/null 2>&1; then
  checksum_tool="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  checksum_tool="shasum"
else
  fail "sha256sum or shasum is required to verify checksums."
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
    fail "Unsupported OS: $OS"
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
    fail "Unsupported architecture: $ARCH"
    ;;
 esac

ASSET="al-${OS}-${ARCH}"
BASE_URL="https://github.com/conn-castle/agent-layer/releases"

TAG="latest"
if [[ "$VERSION" != "latest" ]]; then
  NORMALIZED_VERSION="$(normalize_version "$VERSION")"
  TAG="v${NORMALIZED_VERSION}"
fi

if [[ -n "$ASSET_ROOT" ]]; then
  if [[ "$ASSET_ROOT" == http://* || "$ASSET_ROOT" == https://* || "$ASSET_ROOT" == file://* ]]; then
    :
  elif [[ -d "$ASSET_ROOT" ]]; then
    ASSET_ROOT="file://${ASSET_ROOT}"
  else
    fail "Asset root must be a URL or an existing directory: $ASSET_ROOT"
  fi
  URL="${ASSET_ROOT}/${ASSET}"
  SUMS_URL="${ASSET_ROOT}/checksums.txt"
else
  if [[ "$TAG" == "latest" ]]; then
    URL="${BASE_URL}/latest/download/${ASSET}"
    SUMS_URL="${BASE_URL}/latest/download/checksums.txt"
  else
    URL="${BASE_URL}/download/${TAG}/${ASSET}"
    SUMS_URL="${BASE_URL}/download/${TAG}/checksums.txt"
  fi
fi

bin_dir="${PREFIX}/bin"
mkdir -p "$bin_dir"

archive_tmp="$(mktemp)"
checksums_tmp="$(mktemp)"
cleanup() {
  rm -f "$archive_tmp" "$checksums_tmp"
}
trap cleanup EXIT

if ! curl -fsSL "$URL" -o "$archive_tmp"; then
  fail "Failed to download ${ASSET} from ${URL}."
fi
if ! curl -fsSL "$SUMS_URL" -o "$checksums_tmp"; then
  fail "Failed to download checksums from ${SUMS_URL}."
fi

expected="$(awk -v asset="$ASSET" '{path=$2; sub(/^\.\//, "", path); sub(/^\*/, "", path); if (path == asset) {print $1; exit}}' "$checksums_tmp")"
if [[ -z "$expected" ]]; then
  fail "Checksum for ${ASSET} not found in ${SUMS_URL}."
fi

if [[ "$checksum_tool" == "sha256sum" ]]; then
  if ! echo "${expected}  ${archive_tmp}" | sha256sum -c -; then
    fail "Checksum verification failed for ${ASSET}."
  fi
else
  if ! echo "${expected}  ${archive_tmp}" | shasum -a 256 -c -; then
    fail "Checksum verification failed for ${ASSET}."
  fi
fi

install_path="${bin_dir}/al"
if ! mv "$archive_tmp" "$install_path"; then
  fail "Failed to move al into ${bin_dir}."
fi
if ! chmod +x "$install_path"; then
  fail "Failed to mark ${install_path} as executable."
fi

if [[ ":$PATH:" != *":${bin_dir}:"* ]]; then
  echo "Add ${bin_dir} to your PATH (e.g., export PATH=\"${bin_dir}:\$PATH\")."
fi

echo "Installed al (${TAG}) to ${install_path}"

if [[ "$NO_COMPLETIONS" == "true" ]]; then
  exit 0
fi

shell_name=""
if [[ -n "$SHELL_OVERRIDE" ]]; then
  shell_name="$SHELL_OVERRIDE"
  case "$shell_name" in
    bash|zsh|fish)
      :
      ;;
    *)
      fail "Unsupported shell override: $shell_name (expected bash, zsh, or fish)"
      ;;
  esac
else
  if [[ -n "${SHELL:-}" ]]; then
    shell_name="$(basename "$SHELL")"
  fi
fi

case "$shell_name" in
  bash|zsh|fish)
    :
    ;;
  "")
    echo "Skipping completion install (no shell detected)."
    exit 0
    ;;
  *)
    echo "Skipping completion install (unsupported shell: $shell_name)."
    exit 0
    ;;
esac

if [[ ! -t 0 || ! -t 1 ]]; then
  echo "Skipping completion install (non-interactive shell)."
  exit 0
fi

echo "Installing ${shell_name} completions..."
if ! "$install_path" completion "$shell_name" --install; then
  echo "Completion install failed. You can retry with: ${install_path} completion ${shell_name} --install" >&2
fi
