#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "Error: $*" >&2
  exit 1
}

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${AL_E2E_VERSION:-0.0.0}"
normalized_version="${version#v}"
if [[ -z "${AL_E2E_VERSION:-}" ]]; then
  echo "Info: AL_E2E_VERSION not set; using ${version} for E2E build."
fi

tmp_root="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_root"
}
trap cleanup EXIT

dist_dir="$tmp_root/dist"
AL_VERSION="$version" DIST_DIR="$dist_dir" "$root_dir/scripts/build-release.sh"

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Darwin)
    os="darwin"
    ;;
  Linux)
    os="linux"
    ;;
  *)
    fail "unsupported OS: $os"
    ;;
 esac

case "$arch" in
  x86_64|amd64)
    arch="amd64"
    ;;
  arm64|aarch64)
    arch="arm64"
    ;;
  *)
    fail "unsupported architecture: $arch"
    ;;
 esac

asset="al-${os}-${arch}"
bin_path="$dist_dir/$asset"

if [[ ! -f "$bin_path" ]]; then
  fail "missing built binary: $bin_path"
fi

version_out="$($bin_path --version)"
if [[ "$version_out" != "$version" ]]; then
  fail "expected version $version, got $version_out"
fi

help_out="$($bin_path --help)"
if ! echo "$help_out" | grep -q "init"; then
  fail "expected help output to mention init"
fi

copy_prefix="$tmp_root/copy-prefix"
mkdir -p "$copy_prefix/bin"
cp "$bin_path" "$copy_prefix/bin/al"

version_out="$(PATH="$copy_prefix/bin:$PATH" al --version)"
if [[ "$version_out" != "$version" ]]; then
  fail "expected copied install version $version, got $version_out"
fi

install_prefix="$tmp_root/install-prefix"
bash "$root_dir/al-install.sh" --version "$version" --prefix "$install_prefix" --no-completions --asset-root "$dist_dir"

if [[ ! -x "$install_prefix/bin/al" ]]; then
  fail "installed binary missing at $install_prefix/bin/al"
fi

version_out="$(PATH="$install_prefix/bin:$PATH" al --version)"
if [[ "$version_out" != "$version" ]]; then
  fail "expected installer version $version, got $version_out"
fi

repo_dir="$tmp_root/repo"
mkdir -p "$repo_dir/.git"

(
  cd "$repo_dir"
  AL_NO_NETWORK=1 "$install_prefix/bin/al" init --no-wizard

  if [[ ! -f ".agent-layer/config.toml" ]]; then
    fail "init did not create .agent-layer/config.toml"
  fi

  if [[ ! -f ".agent-layer/al.version" ]]; then
    fail "init did not create .agent-layer/al.version"
  fi

  pinned="$(cat .agent-layer/al.version)"
  if [[ "$pinned" != "$normalized_version" ]]; then
    fail "expected pinned version $normalized_version, got $pinned"
  fi

  AL_NO_NETWORK=1 PATH="$install_prefix/bin:$PATH" "$install_prefix/bin/al" sync
  AL_CONTEXT7_API_KEY="e2e-test" AL_GITHUB_PERSONAL_ACCESS_TOKEN="e2e-test" AL_TAVILY_API_KEY="e2e-test" \
    AL_NO_NETWORK=1 "$install_prefix/bin/al" doctor
)

echo "E2E checks passed."
