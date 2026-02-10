#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

usage() {
  cat <<USAGE
Usage:
  scripts/generate-template-manifest.sh --tag vX.Y.Z [--output internal/templates/manifests/X.Y.Z.json]

Description:
  Generates a template ownership manifest from a release tag and writes it to the
  repository manifest directory.
USAGE
}

tag=""
output=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag)
      [[ $# -ge 2 ]] || { echo "--tag requires a value" >&2; exit 1; }
      tag="$2"
      shift 2
      ;;
    --output)
      [[ $# -ge 2 ]] || { echo "--output requires a value" >&2; exit 1; }
      output="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "$tag" ]]; then
  echo "--tag is required" >&2
  usage
  exit 1
fi

normalized="${tag#v}"
if [[ -z "$output" ]]; then
  output="internal/templates/manifests/${normalized}.json"
fi

cd "$ROOT_DIR"
go run -tags tools ./internal/tools/gentemplatemanifest --tag "$tag" --output "$output" --repo-root "$ROOT_DIR"
