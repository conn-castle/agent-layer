#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

version="${AL_VERSION:-dev}"
dist_dir="${DIST_DIR:-dist}"

mkdir -p "$dist_dir"

build() {
  local goos="$1"
  local goarch="$2"
  local output="$3"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -o "${dist_dir}/${output}" -ldflags "-s -w -X main.Version=${version}" ./cmd/al
}

build darwin arm64 al-darwin-arm64
build darwin amd64 al-darwin-amd64
build linux arm64 al-linux-arm64
build linux amd64 al-linux-amd64
build windows amd64 al-windows-amd64.exe

cp al-install.sh "$dist_dir/"
cp al-install.ps1 "$dist_dir/"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$dist_dir" && rm -f checksums.txt && sha256sum ./* > checksums.txt)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$dist_dir" && rm -f checksums.txt && shasum -a 256 ./* > checksums.txt)
else
  echo "ERROR: sha256sum/shasum not found; cannot generate checksums.txt" >&2
  exit 1
fi
