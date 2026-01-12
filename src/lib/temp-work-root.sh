#!/usr/bin/env bash
set -euo pipefail

ensure_agentlayer_link() {
  local repo_root="$1"
  local work_root="$2"
  # Make temp work roots behave like a consumer repo by linking .agent-layer.
  if [[ -e "$work_root/.agent-layer" ]]; then
    return 1
  fi
  ln -s "$repo_root" "$work_root/.agent-layer"
}

make_temp_work_root() {
  local repo_root="$1"
  local prefix="${2:-agent-layer-temp-work-root}"
  local base dir fallback

  if [[ -z "$repo_root" || ! -d "$repo_root" ]]; then
    return 1
  fi
  repo_root="$(cd "$repo_root" && pwd)"

  base="${TMPDIR:-/tmp}"
  if command -v mktemp > /dev/null 2>&1; then
    dir="$(mktemp -d "${base%/}/${prefix}.XXXXXX" 2> /dev/null || true)"
  fi
  if [[ -n "${dir:-}" && -d "$dir" ]]; then
    if ! ensure_agentlayer_link "$repo_root" "$dir"; then
      rm -rf "$dir"
      return 1
    fi
    printf "%s" "$dir"
    return 0
  fi

  fallback="$repo_root/tmp/$prefix"
  mkdir -p "$fallback"
  if command -v mktemp > /dev/null 2>&1; then
    dir="$(mktemp -d "$fallback/${prefix}.XXXXXX")"
  else
    dir="$fallback/${prefix}.$(date +%s).$$"
    mkdir -p "$dir"
  fi
  if ! ensure_agentlayer_link "$repo_root" "$dir"; then
    rm -rf "$dir"
    return 1
  fi
  printf "%s" "$dir"
}
