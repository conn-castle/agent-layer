#!/usr/bin/env bash
# Upgrade from v1.0.0 (oldest supported), al claude works.

run_scenario_upgrade_claude() {
  section "Upgrade from old version + al claude"

  if skip_if_no_oldest_binary; then return; fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  setup_old_version_via_binary "$repo_dir" "$E2E_OLDEST_BINARY"
  assert_al_version_content "$repo_dir" "$E2E_OLDEST_VERSION"

  local expected="$E2E_TMP_ROOT/custom-instruction.md"
  printf 'Project-owned instructions.\n' > "$expected"
  cp "$expected" "$repo_dir/.agent-layer/instructions/project.md"
  cat >> "$repo_dir/.agent-layer/config.toml" <<'CONFIG_EOF'

[[instructions.local]]
selectors = ["project.md"]
order = 0
CONFIG_EOF

  # Capture upgrade output to verify it prints expected messages
  local upgrade_output rc=0
  upgrade_output=$(cd "$repo_dir" && al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al upgrade from $E2E_OLDEST_VERSION"
  else
    fail "al upgrade from $E2E_OLDEST_VERSION (exit code: $rc)"
    echo "  output (first 10 lines):"
    echo "$upgrade_output" | head -10 | sed 's/^/    /'
  fi

  assert_no_crash_markers "$upgrade_output" "no crash markers in upgrade output"

  # Upgrade should create a snapshot
  assert_output_contains "$upgrade_output" "Created upgrade snapshot" \
    "upgrade output mentions snapshot creation"

  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"

  # Non-interactive `--yes` upgrade must stay minimal: it must not enable the new
  # status lines or seed their editable sources. Status lines are opt-in via
  # `al wizard` or interactive `al upgrade` only.
  assert_exit_zero "non-interactive upgrade does not enable a status line" \
    awk '/^[[:space:]]*statusline[[:space:]]*=[[:space:]]*true/ { found=1 } END { exit found }' "$repo_dir/.agent-layer/config.toml"
  assert_file_not_exists "$repo_dir/.agent-layer/claude-statusline.sh" \
    "non-interactive upgrade does not seed the Claude status line source"
  assert_file_not_exists "$repo_dir/.agent-layer/codex-statusline.toml" \
    "non-interactive upgrade does not seed the Codex status line source"
  assert_file_not_exists "$repo_dir/.claude/claude-statusline.sh" \
    "non-interactive upgrade does not project a Claude status line"

  install_mock_claude "$repo_dir"

  assert_exit_zero_in "$repo_dir" "al claude after upgrade from $E2E_OLDEST_VERSION" al claude

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_env_non_empty "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"
  assert_claude_mock_env_non_empty "$MOCK_CLAUDE_LOG" "AL_RUN_ID"
  assert_generated_artifacts "$repo_dir"

  assert_exit_zero "upgrade preserves user instruction bytes" \
    cmp "$expected" "$repo_dir/.agent-layer/instructions/project.md"
  assert_file_contains "$repo_dir/.claude/CLAUDE.md" 'BEGIN: project.md' \
    "configured user instruction is projected"

  cleanup_scenario_dir "$repo_dir"
}
