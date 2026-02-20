#!/usr/bin/env bash
# Scenario 020: Fresh install, wizard with defaults, al claude works.

run_scenario_fresh_wizard_defaults_claude() {
  section "Fresh install + wizard defaults + al claude"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Capture wizard output to verify it actually ran
  local wizard_output rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --profile "$E2E_DEFAULTS_TOML" --yes 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al wizard --profile defaults.toml --yes"
  else
    fail "al wizard --profile defaults.toml --yes (exit code: $rc)"
    echo "  output (first 5 lines):"
    echo "$wizard_output" | head -5 | sed 's/^/    /'
  fi

  assert_no_crash_markers "$wizard_output" "no crash markers in wizard output"

  # Wizard should print completion message
  assert_output_contains "$wizard_output" "Wizard completed" \
    "wizard output says completed"

  # Verify config still has default approvals mode
  assert_file_contains "$repo_dir/.agent-layer/config.toml" 'mode = "all"' \
    "config.toml approvals mode is all (default)"

  # No MCP servers should be enabled with defaults — check ALL servers
  assert_file_not_contains "$repo_dir/.mcp.json" '"context7"' \
    ".mcp.json has no context7 after defaults wizard"
  assert_file_not_contains "$repo_dir/.mcp.json" '"github"' \
    ".mcp.json has no github after defaults wizard"
  assert_file_not_contains "$repo_dir/.mcp.json" '"tavily"' \
    ".mcp.json has no tavily after defaults wizard"
  assert_file_not_contains "$repo_dir/.mcp.json" '"fetch"' \
    ".mcp.json has no fetch after defaults wizard"
  assert_file_not_contains "$repo_dir/.mcp.json" '"playwright"' \
    ".mcp.json has no playwright after defaults wizard"
  assert_file_not_contains "$repo_dir/.mcp.json" '"ripgrep"' \
    ".mcp.json has no ripgrep after defaults wizard"
  assert_file_not_contains "$repo_dir/.mcp.json" '"filesystem"' \
    ".mcp.json has no filesystem after defaults wizard"

  # settings.json should have no external MCP permissions
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__context7__" \
    "settings.json has no context7 permission after defaults wizard"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__github__" \
    "settings.json has no github permission after defaults wizard"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__tavily__" \
    "settings.json has no tavily permission after defaults wizard"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__fetch__" \
    "settings.json has no fetch permission after defaults wizard"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__playwright__" \
    "settings.json has no playwright permission after defaults wizard"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__ripgrep__" \
    "settings.json has no ripgrep permission after defaults wizard"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__filesystem__" \
    "settings.json has no filesystem permission after defaults wizard"

  install_mock_claude "$repo_dir"

  assert_exit_zero_in "$repo_dir" "al claude with mock" al claude

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_ID"
  assert_generated_artifacts "$repo_dir"

  cleanup_scenario_dir "$repo_dir"
}
