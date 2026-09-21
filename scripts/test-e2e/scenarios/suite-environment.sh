#!/usr/bin/env bash
# Exercise suite-level native storage isolation through an actual wizard sync.
run_scenario_suite_environment() {
  section "Suite HOME/XDG isolation + Muse-enabled wizard"

  if [[ "$XDG_CONFIG_HOME" == "$HOME/.config" && "$XDG_DATA_HOME" == "$HOME/.local/share" && "$HOME" == "$E2E_TMP_ROOT/home" ]]; then
    pass "suite overrides inherited HOME and both XDG storage selectors"
  else
    fail "suite must scope HOME and both XDG selectors before running scenarios"
    return
  fi

  local repo_dir agent
  repo_dir="$(setup_scenario_dir)"
  # Never resolve an installed native client, including during model discovery.
  for agent in muse claude codex grok copilot agy code; do
    install_mock_agent "$repo_dir" "$agent"
  done
  assert_exit_zero_in "$repo_dir" "isolated init" al init --no-wizard
  cat > "$repo_dir/.agent-layer/.env" <<'ENVEOF'
AL_CONTEXT7_API_KEY=e2e-test
AL_GITHUB_PERSONAL_ACCESS_TOKEN=e2e-test
AL_TAVILY_API_KEY=e2e-test
ENVEOF
  assert_exit_zero_in "$repo_dir" "isolated Muse-enabled wizard sync" \
    al wizard --profile "$E2E_FIXTURE_DIR/profiles/everything-enabled.toml" --yes
  assert_file_contains "$XDG_CONFIG_HOME/muse/approval-policy.json" "$repo_dir" \
    "Muse workspace grants are written inside suite HOME"
  assert_file_contains "$repo_dir/.muse/agent-layer-policy.json" "$XDG_CONFIG_HOME/muse" \
    "Muse receipt names suite-local native policy storage"

  cleanup_scenario_dir "$repo_dir"
}
