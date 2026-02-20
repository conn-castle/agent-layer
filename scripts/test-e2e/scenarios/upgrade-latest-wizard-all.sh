#!/usr/bin/env bash
# Upgrade from the latest published release, wizard with everything enabled,
# al claude works. Tests the most common real-world upgrade path with the
# maximal configuration.

run_scenario_upgrade_latest_wizard_all() {
  section "Upgrade from latest release + wizard all + al claude"

  if skip_if_no_latest_binary; then return; fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  setup_old_version_via_binary "$repo_dir" "$E2E_LATEST_BINARY"
  assert_al_version_content "$repo_dir" "$E2E_LATEST_VERSION"

  assert_exit_zero_in "$repo_dir" "al upgrade from $E2E_LATEST_VERSION (latest)" \
    al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions

  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"

  # MCP servers reference secrets from .agent-layer/.env
  cat > "$repo_dir/.agent-layer/.env" <<'ENVEOF'
AL_CONTEXT7_API_KEY=e2e-test
AL_GITHUB_PERSONAL_ACCESS_TOKEN=e2e-test
AL_TAVILY_API_KEY=e2e-test
ENVEOF

  # Apply everything-enabled profile
  local wizard_output wizard_rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --profile "$E2E_FIXTURE_DIR/profiles/everything-enabled.toml" --yes 2>&1) || wizard_rc=$?
  if [[ $wizard_rc -eq 0 ]]; then
    pass "al wizard everything-enabled after latest release upgrade"
  else
    fail "al wizard everything-enabled after latest release upgrade (exit code: $wizard_rc)"
    echo "  output (first 10 lines):"
    echo "$wizard_output" | head -10 | sed 's/^/    /'
  fi

  assert_no_crash_markers "$wizard_output" "no crash markers in wizard output after latest upgrade"
  assert_output_contains "$wizard_output" "Wizard completed" \
    "wizard output says completed after latest release upgrade"

  # Verify ALL MCP servers landed in .mcp.json (7 external + agent-layer)
  assert_file_contains "$repo_dir/.mcp.json" '"context7"' \
    ".mcp.json has context7 after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"github"' \
    ".mcp.json has github after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"tavily"' \
    ".mcp.json has tavily after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"fetch"' \
    ".mcp.json has fetch after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"playwright"' \
    ".mcp.json has playwright after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"ripgrep"' \
    ".mcp.json has ripgrep after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"filesystem"' \
    ".mcp.json has filesystem after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.mcp.json" '"agent-layer"' \
    ".mcp.json has agent-layer after latest upgrade+wizard"

  # Verify settings.json has MCP permissions for ALL servers (8 total)
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__agent-layer__" \
    "settings.json has agent-layer permission after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__context7__" \
    "settings.json has context7 permission after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__fetch__" \
    "settings.json has fetch permission after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__filesystem__" \
    "settings.json has filesystem permission after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__github__" \
    "settings.json has github permission after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__playwright__" \
    "settings.json has playwright permission after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__ripgrep__" \
    "settings.json has ripgrep permission after latest upgrade+wizard"
  assert_file_contains "$repo_dir/.claude/settings.json" "mcp__tavily__" \
    "settings.json has tavily permission after latest upgrade+wizard"

  install_mock_claude "$repo_dir"

  local claude_output claude_rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || claude_rc=$?
  if [[ $claude_rc -eq 0 ]]; then
    pass "al claude after latest release upgrade + wizard all"
  else
    fail "al claude after latest release upgrade + wizard all (exit code: $claude_rc)"
    echo "  output (first 10 lines):"
    echo "$claude_output" | head -10 | sed 's/^/    /'
  fi

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"
  assert_generated_artifacts "$repo_dir"

  cleanup_scenario_dir "$repo_dir"
}
