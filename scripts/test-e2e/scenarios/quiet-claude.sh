#!/usr/bin/env bash
# Scenario 076: Claude quiet mode — verifies al claude --quiet produces no
# agent-layer stderr output while still launching the client.

run_scenario_quiet_claude() {
  section "Claude quiet mode"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  install_mock_claude "$repo_dir"

  local outfile="$repo_dir/.agent-layer/tmp/quiet-out"
  mkdir -p "$(dirname "$outfile")"

  local rc=0
  (cd "$repo_dir" && al claude --quiet -p "hello" > "$outfile" 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al claude --quiet"
  else
    fail "al claude --quiet (exit code: $rc)"
    head -5 "$outfile" | sed 's/^/    /'
  fi

  local size
  size=$(wc -c < "$outfile")
  if [[ $size -eq 0 ]]; then
    pass "al claude --quiet produces zero bytes of output"
  else
    fail "al claude --quiet produces zero bytes of output (got $size bytes)"
    echo "  content:"
    head -5 "$outfile" | sed 's/^/    /'
  fi
  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_lacks_arg "$MOCK_CLAUDE_LOG" "--quiet"

  cleanup_scenario_dir "$repo_dir"
}
