#!/usr/bin/env bash
# Scenario 070: Pass-through args after -- are preserved.

run_scenario_passthrough_args() {
  section "Pass-through args"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  install_mock_claude "$repo_dir"

  assert_exit_zero_in "$repo_dir" "al claude -- -p 'hello world'" al claude -- -p "hello world"

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_has_arg "$MOCK_CLAUDE_LOG" "-p"
  assert_claude_mock_has_arg "$MOCK_CLAUDE_LOG" "hello world"

  # Verify the -- separator was NOT passed through to the subprocess.
  # Cobra should consume --, passing only the arguments after it.
  assert_claude_mock_lacks_arg "$MOCK_CLAUDE_LOG" "--"

  # Verify argument ordering: -p must come before "hello world"
  local p_idx hw_idx
  p_idx=$(grep -n "^ARG_[0-9]*=-p$" "$MOCK_CLAUDE_LOG" | head -1 | cut -d: -f1)
  hw_idx=$(grep -n "^ARG_[0-9]*=hello world$" "$MOCK_CLAUDE_LOG" | head -1 | cut -d: -f1)
  if [[ -n "$p_idx" && -n "$hw_idx" && "$p_idx" -lt "$hw_idx" ]]; then
    pass "pass-through args preserve order (-p before 'hello world')"
  else
    fail "pass-through args out of order (p_idx=$p_idx, hw_idx=$hw_idx)"
  fi

  cleanup_scenario_dir "$repo_dir"
}
