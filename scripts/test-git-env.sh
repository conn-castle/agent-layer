#!/usr/bin/env bash
# Call at test startup, before installing fixture-specific Git configuration.
isolate_test_git() {
  local name candidate physical dir unsafe
  while IFS= read -r name; do unset "$name" || return 1; done < <(compgen -A variable GIT_)
  export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_OPTIONAL_LOCKS=0
  for candidate in "${TMPDIR:-/tmp}" /tmp /var/tmp; do
    physical="$(cd -- "$candidate" 2>/dev/null && pwd -P)" || continue
    [[ -w "$physical" ]] || continue
    dir="$physical"
    unsafe=0
    while :; do
      if [[ -e "$dir/.git" || -L "$dir/.git" || -e "$dir/.agent-layer" || -L "$dir/.agent-layer" ||
            -e "$dir/go.mod" || -L "$dir/go.mod" || ( -e "$dir/HEAD" && -e "$dir/objects" && -e "$dir/refs" ) ]]; then
        unsafe=1
        break
      fi
      [[ "$dir" == / ]] && break
      dir="${dir%/*}"
      [[ -n "$dir" ]] || dir=/
    done
    if [[ "$unsafe" == 0 ]]; then
      export TMPDIR="$physical"
      return 0
    fi
  done
  echo "no writable temporary location outside a Git or Agent Layer repository" >&2
  return 1
}
