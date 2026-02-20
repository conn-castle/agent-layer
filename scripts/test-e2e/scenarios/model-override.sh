#!/usr/bin/env bash
# Scenario 062: Model override — when agents.claude.model is set in config,
# al claude passes --model to the mock binary.

run_scenario_model_override() {
  section "Model override"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Create a profile with model override for claude
  local model_profile="$repo_dir/model-profile.toml"
  cat > "$model_profile" <<'PROFILE'
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true
model = "claude-sonnet-4-5-20250929"

[agents.claude-vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true
PROFILE

  assert_exit_zero_in "$repo_dir" "al wizard --profile model.toml --yes" \
    al wizard --profile "$model_profile" --yes

  # Verify config actually has the model set
  assert_file_contains "$repo_dir/.agent-layer/config.toml" \
    'model = "claude-sonnet-4-5-20250929"' \
    "config.toml has model override"

  install_mock_claude "$repo_dir"

  local claude_output rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al claude with model override"
  else
    fail "al claude with model override (exit code: $rc)"
    echo "  output (first 5 lines):"
    echo "$claude_output" | head -5 | sed 's/^/    /'
  fi

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"

  # The key assertion: --model flag was passed with the correct value
  assert_claude_mock_has_arg "$MOCK_CLAUDE_LOG" "--model"
  assert_claude_mock_has_arg "$MOCK_CLAUDE_LOG" "claude-sonnet-4-5-20250929"

  # Verify --dangerously-skip-permissions is NOT passed (mode is "all", not "yolo")
  assert_claude_mock_lacks_arg "$MOCK_CLAUDE_LOG" "--dangerously-skip-permissions"

  cleanup_scenario_dir "$repo_dir"
}
