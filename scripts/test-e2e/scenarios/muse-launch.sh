#!/usr/bin/env bash
# Muse launch — verifies omitted-config compatibility and an enabled launch
# with unchanged native XDG roots, safety flags, and project MCP.

run_scenario_muse_launch() {
  section "Muse launch"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"
  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  local config="$repo_dir/.agent-layer/config.toml"
  sed '/^\[agents\.muse\]$/,/^\[/ { /^\[agents\.muse\]$/d; /^\[/!d; }' "$config" > "$config.tmp"
  mv "$config.tmp" "$config"
  assert_exit_zero_in "$repo_dir" "sync accepts config without agents.muse" al sync
  assert_file_not_exists "$repo_dir/.muse-config/muse/settings.json" \
    "omitted Muse config remains disabled"

  cat >> "$config" <<'CONFIG'

[agents.muse]
enabled = true
model = "muse-test-model"
reasoning_effort = "medium"
CONFIG

  install_mock_agent "$repo_dir" "muse"
  local output rc=0
  output=$(cd "$repo_dir" && XDG_CONFIG_HOME="$repo_dir/native-config" XDG_DATA_HOME="$repo_dir/native-data" al muse 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al muse launches Muse"
  else
    fail "al muse launch (exit code: $rc)"
    echo "$output" | head -5 | sed 's/^/    /'
  fi

  assert_mock_agent_called "$MOCK_AGENT_LOG"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--workspace"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "$repo_dir"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--trust-workspace"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--approval-mode"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "untrusted"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--approval-judge"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "off"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--model"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "muse-test-model"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--reasoning-effort"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "medium"
  assert_mock_agent_lacks_arg "$MOCK_AGENT_LOG" "--yolo"
  assert_mock_agent_env "$MOCK_AGENT_LOG" "XDG_CONFIG_HOME" "$repo_dir/native-config"
  assert_mock_agent_env "$MOCK_AGENT_LOG" "XDG_DATA_HOME" "$repo_dir/native-data"
  assert_file_not_exists "$repo_dir/.muse-config/muse/settings.json" \
    "Muse does not create redirected settings"
  assert_file_contains "$repo_dir/.mcp.json" '"agent-layer"' \
    "Muse project config includes Agent Dispatch MCP server"
  assert_file_contains "$repo_dir/.mcp.json" '"tool_timeout_sec"' \
    "Muse project config includes dispatch tool timeout"

  cleanup_scenario_dir "$repo_dir"
}
