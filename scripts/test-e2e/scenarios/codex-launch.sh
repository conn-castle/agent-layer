#!/usr/bin/env bash
# Codex launch — verifies al codex calls the codex binary with
# AL_RUN_DIR/AL_RUN_ID env vars and generates codex-specific output.

run_scenario_codex_launch() {
  section "Codex launch"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Opt into repo-local Codex home so `al codex` sets CODEX_HOME=<repo>/.codex.
  enable_codex_local_config_dir "$repo_dir"

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
  assert_mock_agent_env "$MOCK_AGENT_LOG" "CODEX_HOME" "$repo_dir/.codex"

  # Verify sync generated the codex-specific output.
  assert_file_not_exists "$repo_dir/.codex/AGENTS.md" \
    ".codex/AGENTS.md is not generated after Codex AGENTS.md retirement"
  assert_file_contains "$repo_dir/.codex/config.toml" "GENERATED FILE" \
    "codex config has managed marker"
  assert_file_contains "$repo_dir/.codex/config.toml" 'trust_level = "trusted"' \
    "codex config trusts the scenario repo"
  assert_file_contains "$repo_dir/.codex/rules/default.rules" "Source: .agent-layer/commands.allow" \
    "codex rules are generated from commands.allow"

  cleanup_scenario_dir "$repo_dir"
}
