#!/usr/bin/env bash
# Scenario 064: Antigravity launch — verifies al antigravity calls agy
# with expected containment args and environment.

run_scenario_antigravity_launch() {
  section "Antigravity launch"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  local agy_profile="$repo_dir/antigravity-profile.toml"
  cat > "$agy_profile" <<'PROFILE'
[approvals]
mode = "yolo"

[agents.antigravity]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = true
PROFILE

  assert_exit_zero_in "$repo_dir" "al wizard --profile antigravity.toml --yes" \
    al wizard --profile "$agy_profile" --yes

  install_mock_agent "$repo_dir" "agy"

  local output rc=0
  output=$(cd "$repo_dir" && al antigravity 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al antigravity launches agy"
  else
    fail "al antigravity launch (exit code: $rc)"
    echo "  output (first 5 lines):"
    echo "$output" | head -5 | sed 's/^/    /'
  fi

  assert_mock_agent_called "$MOCK_AGENT_LOG"

  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--gemini_dir=$repo_dir/.agy"
  assert_file_contains "$MOCK_AGENT_LOG" "AGY_CLI_DISABLE_AUTO_UPDATE=1" \
    "agy auto-update disabled for contained launch"

  # Verify env vars have non-empty values
  assert_mock_agent_env_non_empty "$MOCK_AGENT_LOG" "AL_RUN_DIR"
  assert_mock_agent_env_non_empty "$MOCK_AGENT_LOG" "AL_RUN_ID"

  cleanup_scenario_dir "$repo_dir"
}
