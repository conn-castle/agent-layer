#!/usr/bin/env bash
# Scenario 086: Running al <agent> when the agent is disabled gives a clear error.

run_scenario_disabled_agent_error() {
  section "Disabled agent error"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Create a profile with claude disabled
  local disabled_profile="$repo_dir/disabled-profile.toml"
  cat > "$disabled_profile" <<'PROFILE'
[approvals]
mode = "all"

[agents.antigravity]
enabled = true

[agents.claude]
enabled = false

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = false

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = true
PROFILE

  assert_exit_zero_in "$repo_dir" "al wizard --profile disabled.toml --yes" \
    al wizard --profile "$disabled_profile" --yes

  # Verify exact agent enable/disable states in config
  local config="$repo_dir/.agent-layer/config.toml"
  local claude_section codex_section antigravity_section

  # Extract each agent section (up to the next section header)
  claude_section=$(sed -n '/^\[agents\.claude\]$/,/^\[/p' "$config" | head -5)
  codex_section=$(sed -n '/^\[agents\.codex\]$/,/^\[/p' "$config" | head -5)
  antigravity_section=$(sed -n '/^\[agents\.antigravity\]$/,/^\[/p' "$config" | head -5)

  if echo "$claude_section" | grep -qF 'enabled = false'; then
    pass "config.toml [agents.claude] has enabled = false"
  else
    fail "config.toml [agents.claude] should have enabled = false"
  fi

  if echo "$codex_section" | grep -qF 'enabled = false'; then
    pass "config.toml [agents.codex] has enabled = false"
  else
    fail "config.toml [agents.codex] should have enabled = false"
  fi

  if echo "$antigravity_section" | grep -qF 'enabled = true'; then
    pass "config.toml [agents.antigravity] has enabled = true"
  else
    fail "config.toml [agents.antigravity] should have enabled = true"
  fi

  install_mock_claude "$repo_dir"

  # al claude should fail because claude is disabled
  local claude_output rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || rc=$?
  if [[ $rc -ne 0 ]]; then
    pass "al claude exits nonzero when disabled"
  else
    fail "al claude should fail when disabled, but got exit 0"
  fi

  assert_output_contains "$claude_output" "disabled" \
    "error says agent is disabled"
  assert_output_contains "$claude_output" "claude" \
    "error mentions claude"

  # Verify the mock claude binary was NOT invoked (disabled agents should bail
  # out before exec). This catches regressions where disabled agents are still
  # spawned.
  assert_claude_mock_not_called "$MOCK_CLAUDE_LOG"

  # al codex should also fail because codex is disabled
  install_mock_agent "$repo_dir" "codex"

  local codex_output codex_rc=0
  codex_output=$(cd "$repo_dir" && al codex 2>&1) || codex_rc=$?
  if [[ $codex_rc -ne 0 ]]; then
    pass "al codex exits nonzero when disabled"
  else
    fail "al codex should fail when disabled, but got exit 0"
  fi

  assert_output_contains "$codex_output" "disabled" \
    "codex error says agent is disabled"

  # Verify the mock codex binary was NOT invoked
  assert_mock_agent_not_called "$MOCK_AGENT_LOG" \
    "mock codex was not called (agent is disabled)"

  # al antigravity should work (it's enabled)
  install_mock_agent "$repo_dir" "agy"

  local agy_output agy_rc=0
  agy_output=$(cd "$repo_dir" && al antigravity 2>&1) || agy_rc=$?
  if [[ $agy_rc -eq 0 ]]; then
    pass "al antigravity works when enabled (claude is disabled)"
  else
    fail "al antigravity should work when enabled (exit code: $agy_rc)"
    echo "  output (first 5 lines):"
    echo "$agy_output" | head -5 | sed 's/^/    /'
  fi

  assert_mock_agent_called "$MOCK_AGENT_LOG"

  cleanup_scenario_dir "$repo_dir"
}
