#!/usr/bin/env bash
# Scenario: bare al init seeds only operational files/directories (no bundled
# instructions, memory templates, or skills) and still supports `al claude`
# end-to-end.

run_scenario_wizard_minimal_no_base_layer() {
  section "Bare al init produces a runnable repo without workflow bundle"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  for name in 00_rules.md 01_base.md 02_memory.md 03_tools.md 04_conventions.md; do
    assert_file_not_exists "$repo_dir/.agent-layer/instructions/$name" \
      "$name is not seeded by bare init"
  done

  # Skills directory exists but is empty.
  assert_dir_exists "$repo_dir/.agent-layer/skills" \
    "skills directory exists under bare init"
  if [[ -z "$(ls -A "$repo_dir/.agent-layer/skills")" ]]; then
    pass "skills directory is empty under bare init"
  else
    fail "skills directory should be empty under bare init"
  fi

  # Memory templates are not written.
  for name in ISSUES.md BACKLOG.md ROADMAP.md DECISIONS.md COMMANDS.md CONTEXT.md; do
    assert_file_not_exists "$repo_dir/docs/agent-layer/$name" \
      "$name not seeded by bare init"
  done

  # The config and env files are still seeded (user-owned seed files).
  assert_file_exists "$repo_dir/.agent-layer/config.toml" \
    "config.toml seeded by bare init"
  assert_file_exists "$repo_dir/.agent-layer/.env" \
    ".env seeded by bare init"

  # al claude still launches end-to-end against the mock binary.
  install_mock_claude "$repo_dir"
  assert_exit_zero_in "$repo_dir" "al claude on bare init" al claude
  assert_claude_mock_called "$MOCK_CLAUDE_LOG"

  cleanup_scenario_dir "$repo_dir"
}
