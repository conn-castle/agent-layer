#!/usr/bin/env bash
# Codex launch — verifies al codex calls the codex binary with
# AL_RUN_DIR/AL_RUN_ID env vars and generates codex-specific output.

run_scenario_codex_launch() {
  section "Codex launch"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  install_mock_agent "$repo_dir" "codex"

  local output rc=0
  output=$(cd "$repo_dir" && al codex 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al codex launches successfully"
  else
    fail "al codex launch (exit code: $rc)"
    echo "  output (first 5 lines):"
    echo "$output" | head -5 | sed 's/^/    /'
  fi

  assert_mock_agent_called "$MOCK_AGENT_LOG"

  # Verify AL_RUN_DIR and AL_RUN_ID env vars have non-empty values
  assert_mock_agent_env_non_empty "$MOCK_AGENT_LOG" "AL_RUN_DIR"
  assert_mock_agent_env_non_empty "$MOCK_AGENT_LOG" "AL_RUN_ID"

  # Verify sync generated the codex-specific output
  assert_file_exists "$repo_dir/.codex/AGENTS.md" "codex AGENTS.md generated"
  assert_file_contains "$repo_dir/.codex/AGENTS.md" "GENERATED FILE" \
    "codex AGENTS.md has managed marker"

  cleanup_scenario_dir "$repo_dir"
}
