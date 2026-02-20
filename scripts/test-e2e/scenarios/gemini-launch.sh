#!/usr/bin/env bash
# Scenario 064: Gemini launch — verifies al gemini calls the gemini binary
# with expected args (model, yolo approval mode).

run_scenario_gemini_launch() {
  section "Gemini launch"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Create profile with gemini model + yolo mode
  local gemini_profile="$repo_dir/gemini-profile.toml"
  cat > "$gemini_profile" <<'PROFILE'
[approvals]
mode = "yolo"

[agents.gemini]
enabled = true
model = "gemini-2.5-pro"

[agents.claude]
enabled = true

[agents.claude-vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true
PROFILE

  assert_exit_zero_in "$repo_dir" "al wizard --profile gemini.toml --yes" \
    al wizard --profile "$gemini_profile" --yes

  install_mock_agent "$repo_dir" "gemini"

  local output rc=0
  output=$(cd "$repo_dir" && al gemini 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al gemini with model + yolo"
  else
    fail "al gemini with model + yolo (exit code: $rc)"
    echo "  output (first 5 lines):"
    echo "$output" | head -5 | sed 's/^/    /'
  fi

  assert_mock_agent_called "$MOCK_AGENT_LOG"

  # Verify model flag was passed
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--model"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "gemini-2.5-pro"

  # Verify yolo approval mode flag (gemini uses --approval-mode=yolo)
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--approval-mode=yolo"

  # Verify yolo acknowledgement was printed
  assert_output_contains "$output" "[yolo]" \
    "al gemini output contains yolo acknowledgement"

  # Verify env vars have non-empty values
  assert_mock_agent_env_non_empty "$MOCK_AGENT_LOG" "AL_RUN_DIR"
  assert_mock_agent_env_non_empty "$MOCK_AGENT_LOG" "AL_RUN_ID"

  cleanup_scenario_dir "$repo_dir"
}
