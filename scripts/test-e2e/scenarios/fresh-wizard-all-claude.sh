#!/usr/bin/env bash
# Scenario 030: Fresh install, wizard with everything enabled, al claude works.

run_scenario_fresh_wizard_all_claude() {
  section "Fresh install + wizard everything-enabled + al claude"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # MCP servers reference env vars from .agent-layer/.env (not os.Environ).
  # Write test values so sync can resolve placeholders.
  cat > "$repo_dir/.agent-layer/.env" <<'ENVEOF'
AL_CONTEXT7_API_KEY=e2e-test
AL_GITHUB_PERSONAL_ACCESS_TOKEN=e2e-test
AL_TAVILY_API_KEY=e2e-test
ENVEOF

  # Capture wizard output
  local wizard_output rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --profile "$E2E_FIXTURE_DIR/profiles/everything-enabled.toml" --yes 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al wizard --profile everything-enabled.toml --yes"
  else
    fail "al wizard --profile everything-enabled.toml --yes (exit code: $rc)"
    echo "  output (first 5 lines):"
    echo "$wizard_output" | head -5 | sed 's/^/    /'
  fi

  assert_no_crash_markers "$wizard_output" "no crash markers in wizard output"
  assert_output_contains "$wizard_output" "Wizard completed" \
    "wizard output says completed"

  install_mock_claude "$repo_dir"

  assert_exit_zero_in "$repo_dir" "al claude with mock (all MCP enabled)" al claude

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_ID"

  # Everything-enabled profile sets local_config_dir = true, so
  # CLAUDE_CONFIG_DIR should be set to repo-local .claude-config.
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "CLAUDE_CONFIG_DIR" "$repo_dir/.claude-config"

  assert_generated_artifacts "$repo_dir"

  # Verify ALL 7 MCP servers are in .mcp.json (plus built-in agent-layer)
  assert_file_contains "$repo_dir/.mcp.json" '"context7"' \
    ".mcp.json has context7 server"
  assert_file_contains "$repo_dir/.mcp.json" '"github"' \
    ".mcp.json has github server"
  assert_file_contains "$repo_dir/.mcp.json" '"tavily"' \
    ".mcp.json has tavily server"
  assert_file_contains "$repo_dir/.mcp.json" '"fetch"' \
    ".mcp.json has fetch server"
  assert_file_contains "$repo_dir/.mcp.json" '"playwright"' \
    ".mcp.json has playwright server"
  assert_file_contains "$repo_dir/.mcp.json" '"ripgrep"' \
    ".mcp.json has ripgrep server"
  assert_file_contains "$repo_dir/.mcp.json" '"filesystem"' \
    ".mcp.json has filesystem server"
  assert_file_contains "$repo_dir/.mcp.json" '"agent-layer"' \
    ".mcp.json has built-in agent-layer server"

  # Verify settings.json has MCP permissions for ALL servers (8 total)
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__agent-layer__" \
    "settings.json has agent-layer MCP permission"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__context7__" \
    "settings.json has context7 MCP permission"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__fetch__" \
    "settings.json has fetch MCP permission"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__filesystem__" \
    "settings.json has filesystem MCP permission"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__github__" \
    "settings.json has github MCP permission"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__playwright__" \
    "settings.json has playwright MCP permission"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__ripgrep__" \
    "settings.json has ripgrep MCP permission"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__tavily__" \
    "settings.json has tavily MCP permission"

  # Verify .mcp.json has actual server config content (not just names)
  assert_file_contains "$repo_dir/.mcp.json" '"mcp-prompts"' \
    ".mcp.json agent-layer has mcp-prompts command"
  assert_file_contains "$repo_dir/.mcp.json" 'context7-mcp' \
    ".mcp.json context7 has correct package"

  cleanup_scenario_dir "$repo_dir"
}
