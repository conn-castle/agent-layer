#!/usr/bin/env bash
# Grok launch — verifies al grok calls the grok binary with GROK_HOME
# and writes project config plus folder trust.

run_scenario_grok_launch() {
  section "Grok launch"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  local grok_profile="$repo_dir/grok-profile.toml"
  cat > "$grok_profile" <<'PROFILE'
[approvals]
mode = "yolo"

[agents.grok]
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

[agents.antigravity]
enabled = false
PROFILE

  assert_exit_zero_in "$repo_dir" "al wizard --profile grok.toml --yes" \
    al wizard --profile "$grok_profile" --yes

  install_mock_agent "$repo_dir" "grok"

  local output rc=0
  output=$(cd "$repo_dir" && al grok 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al grok launches grok"
  else
    fail "al grok launch (exit code: $rc)"
    echo "  output (first 5 lines):"
    echo "$output" | head -5 | sed 's/^/    /'
  fi

  assert_mock_agent_called "$MOCK_AGENT_LOG"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--permission-mode"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "bypassPermissions"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--always-approve"
  assert_mock_agent_env "$MOCK_AGENT_LOG" "GROK_HOME" "$repo_dir/.grok-config"
  assert_mock_agent_env_non_empty "$MOCK_AGENT_LOG" "AL_RUN_DIR"
  assert_mock_agent_env_non_empty "$MOCK_AGENT_LOG" "AL_RUN_ID"

  assert_file_contains "$repo_dir/.grok/config.toml" "GENERATED FILE" \
    "grok project config has managed marker"
  assert_file_contains "$repo_dir/.grok-config/trusted_folders.toml" "trusted = true" \
    "grok folder trust is seeded"

  cleanup_scenario_dir "$repo_dir"
}
