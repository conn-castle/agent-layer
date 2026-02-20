#!/usr/bin/env bash
# Version and help output checks.

run_scenario_version_output() {
  section "Version output"

  local safe_cwd="$E2E_TMP_ROOT/safe-cwd-version"
  mkdir -p "$safe_cwd"

  # --version should exit 0 and print the build version
  local version_out rc=0
  version_out="$(cd "$safe_cwd" && "$E2E_BIN" --version 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    fail "al --version exited with code $rc"
  fi
  assert_output_equals "$version_out" "$AL_E2E_VERSION" "al --version matches AL_E2E_VERSION"

  # --help should exit 0 and mention key subcommands
  local help_out help_rc=0
  help_out="$(cd "$safe_cwd" && "$E2E_BIN" --help 2>&1)" || help_rc=$?
  if [[ $help_rc -ne 0 ]]; then
    fail "al --help exited with code $help_rc"
  fi

  local subcmds=("init" "sync" "claude" "wizard" "doctor" "upgrade")
  for cmd in "${subcmds[@]}"; do
    if echo "$help_out" | grep -q "$cmd"; then
      pass "al --help mentions $cmd"
    else
      fail "al --help does not mention $cmd"
    fi
  done
}
