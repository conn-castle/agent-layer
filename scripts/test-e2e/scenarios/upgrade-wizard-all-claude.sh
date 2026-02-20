#!/usr/bin/env bash
# Upgrade from oldest version, wizard with all MCP servers enabled,
# al claude works. This is the highest-risk upgrade path: old config +
# config migration + all servers enabled + secret resolution + sync + launch.

run_scenario_upgrade_wizard_all_claude() {
  section "Upgrade + wizard everything-enabled + al claude"

  if skip_if_no_oldest_binary; then return; fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  setup_old_version_via_binary "$repo_dir" "$E2E_OLDEST_BINARY"
  assert_al_version_content "$repo_dir" "$E2E_OLDEST_VERSION"

  # Upgrade first
  local upgrade_output rc=0
  upgrade_output=$(cd "$repo_dir" && al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al upgrade from $E2E_OLDEST_VERSION"
  else
    fail "al upgrade from $E2E_OLDEST_VERSION (exit code: $rc)"
    echo "  output (first 10 lines):"
    echo "$upgrade_output" | head -10 | sed 's/^/    /'
  fi

  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"

  # MCP servers reference secrets from .agent-layer/.env (not os.Environ).
  # Write test values so wizard sync can resolve placeholders.
  cat > "$repo_dir/.agent-layer/.env" <<'ENVEOF'
AL_CONTEXT7_API_KEY=e2e-test
AL_GITHUB_PERSONAL_ACCESS_TOKEN=e2e-test
AL_TAVILY_API_KEY=e2e-test
ENVEOF

  # Apply everything-enabled profile — this is the critical test:
  # can a freshly-upgraded old config accept all MCP servers and sync?
  local wizard_output wizard_rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --profile "$E2E_FIXTURE_DIR/profiles/everything-enabled.toml" --yes 2>&1) || wizard_rc=$?
  if [[ $wizard_rc -eq 0 ]]; then
    pass "al wizard everything-enabled after upgrade"
  else
    fail "al wizard everything-enabled after upgrade (exit code: $wizard_rc)"
    echo "  output (first 10 lines):"
    echo "$wizard_output" | head -10 | sed 's/^/    /'
  fi

  assert_no_crash_markers "$wizard_output" "no crash markers in wizard output after upgrade"
  assert_output_contains "$wizard_output" "Wizard completed" \
    "wizard output says completed after upgrade"

  # Verify all MCP servers landed in .mcp.json
  assert_file_contains "$repo_dir/.mcp.json" '"context7"' \
    ".mcp.json has context7 after upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"github"' \
    ".mcp.json has github after upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"tavily"' \
    ".mcp.json has tavily after upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"fetch"' \
    ".mcp.json has fetch after upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"playwright"' \
    ".mcp.json has playwright after upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"ripgrep"' \
    ".mcp.json has ripgrep after upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"filesystem"' \
    ".mcp.json has filesystem after upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"agent-layer"' \
    ".mcp.json has built-in agent-layer after upgrade+wizard"

  # Verify settings.json has MCP permissions for ALL servers (8 total)
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__agent-layer__" \
    "settings.json has agent-layer permission after upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__context7__" \
    "settings.json has context7 permission after upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__fetch__" \
    "settings.json has fetch permission after upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__filesystem__" \
    "settings.json has filesystem permission after upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__github__" \
    "settings.json has github permission after upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__playwright__" \
    "settings.json has playwright permission after upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__ripgrep__" \
    "settings.json has ripgrep permission after upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__tavily__" \
    "settings.json has tavily permission after upgrade+wizard"

  install_mock_claude "$repo_dir"

  # The ultimate test: does al claude actually work after all of this?
  local claude_output claude_rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || claude_rc=$?
  if [[ $claude_rc -eq 0 ]]; then
    pass "al claude after upgrade + wizard everything-enabled"
  else
    fail "al claude after upgrade + wizard everything-enabled (exit code: $claude_rc)"
    echo "  output (first 10 lines):"
    echo "$claude_output" | head -10 | sed 's/^/    /'
  fi

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_ID"
  assert_generated_artifacts "$repo_dir"

  # Verify CLAUDE.md has instruction content
  assert_file_contains "$repo_dir/CLAUDE.md" "BEGIN: 00_base.md" \
    "CLAUDE.md has instruction blocks after upgrade+wizard+all"

  cleanup_scenario_dir "$repo_dir"
}
