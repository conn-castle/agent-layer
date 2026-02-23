#!/usr/bin/env bash
# Scenario 010: Fresh install, al claude works with mock.

run_scenario_fresh_install_claude() {
  section "Fresh install + al claude"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  assert_al_init_structure "$repo_dir"
  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"

  # Verify instruction files were created with real content
  assert_file_contains "$repo_dir/.agent-layer/instructions/00_base.md" \
    "Guiding Principles" "instructions/00_base.md has Guiding Principles"
  assert_file_contains "$repo_dir/.agent-layer/instructions/02_rules.md" \
    "Rules" "instructions/02_rules.md has Rules header"

  install_mock_claude "$repo_dir"

  local claude_output rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al claude with mock"
  else
    fail "al claude with mock (exit code: $rc)"
  fi

  # No crash markers in output
  assert_no_crash_markers "$claude_output" "no crash markers in al claude output"

  # Verify version source diagnostic line contains both the label AND the version
  # together (not matching them separately where they could appear in different contexts).
  # Format: "Agent Layer version source: X.Y.Z (source)"
  assert_output_contains "$claude_output" "Agent Layer version source: ${AL_E2E_VERSION_NO_V}" \
    "version source diagnostic shows correct version"

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"

  # Verify mock claude received critical env vars with non-empty values
  assert_claude_mock_env_non_empty "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"
  assert_claude_mock_env_non_empty "$MOCK_CLAUDE_LOG" "AL_RUN_ID"

  # Default config does not enable local_config_dir, so CLAUDE_CONFIG_DIR
  # should NOT be set by the launcher.
  assert_claude_mock_env_not_set "$MOCK_CLAUDE_LOG" "CLAUDE_CONFIG_DIR"

  # Verify generated artifacts exist AND have correct content
  assert_generated_artifacts "$repo_dir"

  # CLAUDE.md should contain actual instruction content, not just the marker
  assert_file_contains "$repo_dir/CLAUDE.md" "BEGIN: 00_base.md" \
    "CLAUDE.md includes 00_base.md instruction block"
  assert_file_contains "$repo_dir/CLAUDE.md" "Guiding Principles" \
    "CLAUDE.md has real instruction content"

  # .mcp.json and settings.json should be valid JSON
  assert_json_valid "$repo_dir/.mcp.json" ".mcp.json is valid JSON"
  assert_json_valid "$repo_dir/.claude/settings.json" "settings.json is valid JSON"

  # .mcp.json should have only the built-in agent-layer server (no MCP enabled)
  assert_file_contains "$repo_dir/.mcp.json" '"agent-layer"' \
    ".mcp.json has agent-layer prompt server"
  assert_file_not_contains "$repo_dir/.mcp.json" '"context7"' \
    ".mcp.json has no context7 (not enabled)"

  # .claude/settings.json should have permissions but no external MCP
  assert_file_contains "$repo_dir/.claude/settings.json" '"permissions"' \
    "settings.json has permissions block"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__agent-layer__" \
    "settings.json has agent-layer MCP permission"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__context7__" \
    "settings.json has no context7 permission (not enabled)"

  cleanup_scenario_dir "$repo_dir"
}
