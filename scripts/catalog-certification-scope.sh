#!/usr/bin/env bash
set -euo pipefail

die() {
  echo "ERROR: $*" >&2
  exit 1
}

head_ref="${1:-HEAD}"
head_sha="$(git rev-parse --verify "${head_ref}^{commit}")" || die "could not resolve ${head_ref}"
base_tag=""

while IFS= read -r tag; do
  [[ "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || continue
  tag_sha="$(git rev-parse --verify "${tag}^{commit}")" || continue
  [[ "${tag_sha}" != "${head_sha}" ]] || continue
  if git merge-base --is-ancestor "${tag_sha}" "${head_sha}"; then
    base_tag="${tag}"
    break
  fi
done < <(git tag --sort=-version:refname)

if [[ -z "${base_tag}" ]]; then
  echo "No prior stable release tag is reachable from ${head_sha}; full catalog certification is required." >&2
  echo true
  exit 0
fi

while IFS= read -r path; do
  case "${path}" in
    internal/benchmark/*|cmd/al/benchmark.go|go.mod|go.sum|.github/workflows/release-catalog-certification.yml|scripts/catalog-certification-scope.sh)
      echo "Catalog-critical path ${path} changed since ${base_tag}; full catalog certification is required." >&2
      echo true
      exit 0
      ;;
  esac
done < <(git diff --name-only "${base_tag}..${head_sha}")

echo "No catalog-critical paths changed since ${base_tag}; full catalog certification is not required." >&2
echo false
