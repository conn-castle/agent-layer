#!/usr/bin/env bash
# Scenario: statusline-enabled config with user-owned sources syncs Claude and
# Codex statusline projections, and both launch paths still work.

_statusline_assert_files_identical() {
  local expected="$1" actual="$2" label="$3"
  if cmp -s "$expected" "$actual"; then
    pass "$label"
  else
    fail "$label (files differ: expected $expected, actual $actual)"
    diff -u "$expected" "$actual" | head -40 | sed 's/^/    /' || true
  fi
}

_statusline_assert_executable() {
  local path="$1" label="$2"
  if [[ -x "$path" ]]; then
    pass "$label"
  else
    fail "$label (not executable: $path)"
  fi
}

run_scenario_statusline_enabled() {
  section "Statusline enabled sync + launch"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  local profile="$repo_dir/statusline-profile.toml"
  sed \
    -e '/^\[agents\.claude\]/,/^\[/ s/^# statusline = true/statusline = true/' \
    -e '/^\[agents\.codex\]/,/^\[/ s/^# statusline = true/statusline = true/' \
    "$E2E_DEFAULTS_TOML" > "$profile"

  local missing_source_output missing_source_rc=0
  missing_source_output=$(cd "$repo_dir" && al wizard --profile "$profile" --yes 2>&1) || missing_source_rc=$?
  if [[ $missing_source_rc -ne 0 ]]; then
    pass "al wizard fails when statusline sources are missing"
  else
    fail "al wizard fails when statusline sources are missing"
  fi
  assert_output_contains "$missing_source_output" "statusline" \
    "missing statusline source output names statusline"
  assert_output_contains "$missing_source_output" "is missing" \
    "missing statusline source output explains missing source"
  assert_output_contains "$missing_source_output" "al wizard" \
    "missing statusline source output gives wizard recovery guidance"

  local claude_source="$repo_dir/.agent-layer/claude-statusline.sh"
  local codex_source="$repo_dir/.agent-layer/codex-statusline.toml"
  cp "$ROOT_DIR/internal/templates/claude-statusline.sh" "$claude_source"
  chmod 755 "$claude_source"
  cp "$ROOT_DIR/internal/templates/codex-statusline.toml" "$codex_source"

  local wizard_output wizard_rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --profile "$profile" --yes 2>&1) || wizard_rc=$?
  if [[ $wizard_rc -eq 0 ]]; then
    pass "al wizard --profile statusline-profile.toml --yes"
  else
    fail "al wizard --profile statusline-profile.toml --yes (exit code: $wizard_rc)"
    echo "$wizard_output" | head -20 | sed 's/^/    /'
  fi
  assert_no_crash_markers "$wizard_output" "statusline wizard output has no crash markers"
  assert_output_contains "$wizard_output" "Running sync" \
    "statusline wizard output says sync ran"
  assert_output_contains "$wizard_output" "Wizard completed" \
    "statusline wizard output says completed"
  assert_output_not_contains "$wizard_output" "Warning:" \
    "statusline wizard output has no warnings"

  assert_file_contains "$repo_dir/.agent-layer/config.toml" 'statusline = true' \
    "statusline profile enables at least one statusline"
  _statusline_assert_files_identical "$ROOT_DIR/internal/templates/claude-statusline.sh" \
    "$repo_dir/.agent-layer/claude-statusline.sh" \
    "Claude statusline source matches template"
  _statusline_assert_files_identical "$ROOT_DIR/internal/templates/codex-statusline.toml" \
    "$repo_dir/.agent-layer/codex-statusline.toml" \
    "Codex statusline source matches template"

  _statusline_assert_files_identical "$repo_dir/.agent-layer/claude-statusline.sh" \
    "$repo_dir/.claude/claude-statusline.sh" \
    "Claude statusline projection matches source"
  _statusline_assert_executable "$repo_dir/.claude/claude-statusline.sh" \
    "Claude statusline projection is executable"
  assert_file_contains "$repo_dir/.claude/settings.json" '"statusLine"' \
    "Claude settings has statusLine block"
  assert_file_contains "$repo_dir/.claude/settings.json" "claude-statusline.sh" \
    "Claude settings points at projected statusline script"

  assert_file_contains "$repo_dir/.codex/config.toml" "Sources:" \
    "Codex config uses statusline-aware generated header"
  assert_file_contains "$repo_dir/.codex/config.toml" ".agent-layer/codex-statusline.toml" \
    "Codex config records statusline source"
  assert_file_contains "$repo_dir/.codex/config.toml" "status_line = [" \
    "Codex config has native status_line block"
  assert_file_contains "$repo_dir/.codex/config.toml" "weekly-limit" \
    "Codex statusline includes template item"

  install_mock_claude "$repo_dir"
  assert_exit_zero_in "$repo_dir" "al claude with statusline enabled" al claude
  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_DISPATCH_CALLER_AGENT" "claude"

  install_mock_agent "$repo_dir" "codex"
  assert_exit_zero_in "$repo_dir" "al codex with statusline enabled" al codex
  assert_mock_agent_called "$MOCK_AGENT_LOG"
  assert_mock_agent_env "$MOCK_AGENT_LOG" "AL_DISPATCH_CALLER_AGENT" "codex"
  assert_mock_agent_env "$MOCK_AGENT_LOG" "CODEX_HOME" "$repo_dir/.codex"

  cleanup_scenario_dir "$repo_dir"
}
