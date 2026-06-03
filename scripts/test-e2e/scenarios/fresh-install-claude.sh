#!/usr/bin/env bash
# Scenario 010: Fresh install, al claude works with mock.

run_scenario_fresh_install_claude() {
  section "Fresh install + al claude"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  assert_al_init_structure "$repo_dir"
  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"

  # Bare init creates operational directories only; the workflow bundle is
  # installed explicitly through the wizard or preserved during upgrade.
  for name in 00_rules.md 01_base.md 02_memory.md 03_tools.md 04_conventions.md; do
    assert_file_not_exists "$repo_dir/.agent-layer/instructions/$name" \
      "$name is not seeded by bare init"
  done

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
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_DISPATCH_CALLER_AGENT" "claude"

  # Default config does not enable local_config_dir, so CLAUDE_CONFIG_DIR
  # should NOT be set by the launcher.
  assert_claude_mock_env_not_set "$MOCK_CLAUDE_LOG" "CLAUDE_CONFIG_DIR"

  # Verify generated artifacts exist AND have correct content
  assert_generated_artifacts "$repo_dir"

  # .mcp.json and settings.json should be valid JSON
  assert_json_valid "$repo_dir/.mcp.json" ".mcp.json is valid JSON"
  assert_json_valid "$repo_dir/.claude/settings.json" "settings.json is valid JSON"

  # .mcp.json should have no MCP servers enabled by default
  assert_file_not_contains "$repo_dir/.mcp.json" '"context7"' \
    ".mcp.json has no context7 (not enabled)"
  assert_file_not_contains "$repo_dir/.mcp.json" '"github"' \
    ".mcp.json has no github (not enabled)"
  assert_file_not_contains "$repo_dir/.mcp.json" '"tavily"' \
    ".mcp.json has no tavily (not enabled)"
  assert_file_not_contains "$repo_dir/.mcp.json" '"fetch"' \
    ".mcp.json has no fetch (not enabled)"
  assert_file_not_contains "$repo_dir/.mcp.json" '"playwright"' \
    ".mcp.json has no playwright (not enabled)"

  # .claude/settings.json should have permissions but no external MCP
  assert_file_contains "$repo_dir/.claude/settings.json" '"permissions"' \
    "settings.json has permissions block"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__context7__" \
    "settings.json has no context7 permission (not enabled)"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__github__" \
    "settings.json has no github permission (not enabled)"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__tavily__" \
    "settings.json has no tavily permission (not enabled)"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__fetch__" \
    "settings.json has no fetch permission (not enabled)"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__playwright__" \
    "settings.json has no playwright permission (not enabled)"

  cleanup_scenario_dir "$repo_dir"
}
