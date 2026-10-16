#!/usr/bin/env bash
# Test-process startup only. Source this file and call isolate_test_git before
# creating fixtures; deliberate test URL rewrites may be added afterward.
isolate_test_git() {
  local names name temp_parent
  # Git documents clearing these variables when a hook accesses a foreign repo.
  # Capture first so failure cannot silently leave part of the context in place.
  names="$(git rev-parse --local-env-vars)" || return 1
  while IFS= read -r name; do
    unset "$name"
  done <<< "$names"
  unset GIT_CEILING_DIRECTORIES GIT_DISCOVERY_ACROSS_FILESYSTEM GIT_NAMESPACE GIT_TEMPLATE_DIR
  while IFS= read -r name; do
    [[ -z "$name" ]] || unset "$name"
  done < <(compgen -A variable GIT_CONFIG_KEY_ || :; compgen -A variable GIT_CONFIG_VALUE_ || :)
  export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1
  export GIT_OPTIONAL_LOCKS=0
  temp_parent="$(isolated_test_temp_parent)" || return 1
  export TMPDIR="$temp_parent" TMP="$temp_parent" TEMP="$temp_parent"
}

# Select a physical parent before mktemp: CLI root discovery also recognizes
# Agent Layer/Go roots and lexical ancestors of symlinks, independently of Git.
isolated_test_temp_parent() (
  local candidate physical dir unsafe
  for candidate in "${TMPDIR:-/tmp}" /tmp /var/tmp; do
    physical="$(cd -- "$candidate" 2>/dev/null && pwd -P)" || continue
    [[ -w "$physical" ]] || continue
    dir="$physical"
    unsafe=0
    while :; do
      if [[ -e "$dir/.git" || -L "$dir/.git" ||
            -e "$dir/.agent-layer" || -L "$dir/.agent-layer" ||
            -e "$dir/go.mod" || -L "$dir/go.mod" ]] ||
         [[ -e "$dir/HEAD" && -e "$dir/objects" && -e "$dir/refs" ]]; then
        unsafe=1
        break
      fi
      [[ "$dir" == / ]] && break
      dir="${dir%/*}"
      [[ -n "$dir" ]] || dir=/
    done
    if [[ "$unsafe" == 0 ]]; then
      printf '%s\n' "$physical"
      return 0
    fi
  done
  echo "no writable temporary location outside a Git or Agent Layer repository" >&2
  return 1
)
