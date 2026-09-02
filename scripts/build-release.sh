#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

version="${AL_VERSION:-dev}"
dist_dir="${DIST_DIR:-dist}"
version_no_v="${version#v}"
source_name="agent-layer-${version_no_v}"
source_tar="${dist_dir}/${source_name}.tar"
source_tgz="${source_tar}.gz"

stable_release=0
if [[ "$version" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  stable_release=1
  migration_manifest="internal/templates/migrations/${version_no_v}.json"
  if [[ ! -f "$migration_manifest" ]]; then
    echo "ERROR: stable release ${version} is missing migration manifest ${migration_manifest}" >&2
    exit 1
  fi
fi

mkdir -p "$dist_dir"

if ! command -v git >/dev/null 2>&1; then
  echo "ERROR: git not found; cannot generate source tarball" >&2
  exit 1
fi

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "ERROR: not inside a git repository; cannot generate source tarball" >&2
  exit 1
fi

if ! command -v gzip >/dev/null 2>&1; then
  echo "ERROR: gzip not found; cannot generate source tarball" >&2
  exit 1
fi

build() {
  local goos="$1"
  local goarch="$2"
  local output="$3"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -o "${dist_dir}/${output}" -ldflags "-s -w -X main.Version=${version}" ./cmd/al
}

smoke_native_release_binary() {
  if [[ "$stable_release" != "1" ]]; then
    return 0
  fi

  local host_os host_arch binary
  case "$(uname -s)" in
    Darwin) host_os="darwin" ;;
    Linux) host_os="linux" ;;
    *)
      echo "ERROR: unsupported host OS for release artifact smoke test: $(uname -s)" >&2
      exit 1
      ;;
  esac
  case "$(uname -m)" in
    arm64|aarch64) host_arch="arm64" ;;
    x86_64|amd64) host_arch="amd64" ;;
    *)
      echo "ERROR: unsupported host architecture for release artifact smoke test: $(uname -m)" >&2
      exit 1
      ;;
  esac
  binary="${dist_dir}/al-${host_os}-${host_arch}"
  binary="$(cd "$(dirname "$binary")" && pwd)/$(basename "$binary")"

  (
    set -euo pipefail
    local smoke_root project_root actual_version actual_pin
    smoke_root="$(mktemp -d "${TMPDIR:-/tmp}/agent-layer-release-smoke.XXXXXX")"
    trap 'rm -rf "$smoke_root"' EXIT
    project_root="${smoke_root}/project"
    mkdir -p "$project_root" "${smoke_root}/home"
    git -C "$project_root" init -q

    export HOME="${smoke_root}/home"
    export AL_NO_NETWORK=1
    unset AL_VERSION
    cd "$project_root"

    actual_version="$("$binary" --version)"
    if [[ "$actual_version" != "$version" ]]; then
      echo "ERROR: release binary version is ${actual_version}; expected ${version}" >&2
      exit 1
    fi

    "$binary" init --here --no-wizard >/dev/null
    actual_pin="$(tr -d '[:space:]' < .agent-layer/al.version)"
    if [[ "$actual_pin" != "$version_no_v" ]]; then
      echo "ERROR: release binary initialized pin ${actual_pin}; expected ${version_no_v}" >&2
      exit 1
    fi
    "$binary" upgrade plan >/dev/null
  )
}

sign_darwin_binaries() {
  local identity="${AL_CODESIGN_IDENTITY:-}"
  local require_codesign="${AL_REQUIRE_CODESIGN:-0}"

  if [[ -z "$identity" ]]; then
    if [[ "$require_codesign" == "1" ]]; then
      echo "ERROR: AL_CODESIGN_IDENTITY is required when AL_REQUIRE_CODESIGN=1" >&2
      exit 1
    fi
    return 0
  fi

  if [[ "$(uname -s)" != "Darwin" ]]; then
    if [[ "$require_codesign" == "1" ]]; then
      echo "ERROR: AL_REQUIRE_CODESIGN=1 requires running release signing on macOS" >&2
      exit 1
    fi
    echo "Skipping codesign: host is not macOS." >&2
    return 0
  fi

  ./scripts/codesign-release.sh "${dist_dir}/al-darwin-arm64"
  ./scripts/codesign-release.sh "${dist_dir}/al-darwin-amd64"
}

build darwin arm64 al-darwin-arm64
build darwin amd64 al-darwin-amd64
build linux arm64 al-linux-arm64
build linux amd64 al-linux-amd64

sign_darwin_binaries
smoke_native_release_binary

git archive --format=tar --prefix="${source_name}/" HEAD > "$source_tar"
gzip -n -f "$source_tar"

if [[ ! -f "$source_tgz" ]]; then
  echo "ERROR: source tarball was not created at ${source_tgz}" >&2
  exit 1
fi

cp al-install.sh "$dist_dir/"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$dist_dir" && rm -f checksums.txt && sha256sum ./* > checksums.txt)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$dist_dir" && rm -f checksums.txt && shasum -a 256 ./* > checksums.txt)
else
  echo "ERROR: sha256sum/shasum not found; cannot generate checksums.txt" >&2
  exit 1
fi
