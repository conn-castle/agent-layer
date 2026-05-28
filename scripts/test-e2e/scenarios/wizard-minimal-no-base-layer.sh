#!/usr/bin/env bash
# Scenario: al init --minimal-layout seeds only a placeholder instruction file
# (no bundled instructions, memory templates, or skills) and the resulting
# repo still supports `al claude` end-to-end.

run_scenario_wizard_minimal_no_base_layer() {
  section "al init --minimal-layout produces a runnable minimal repo"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --minimal-layout --no-wizard" \
    al init --minimal-layout --no-wizard

  # Only the placeholder is present under instructions/.
  assert_file_exists "$repo_dir/.agent-layer/instructions/00_instructions.md" \
    "placeholder 00_instructions.md is seeded"
  if [[ -s "$repo_dir/.agent-layer/instructions/00_instructions.md" ]]; then
    fail "placeholder 00_instructions.md should be zero-byte"
  else
    pass "placeholder 00_instructions.md is zero-byte"
  fi
  for name in 00_rules.md 01_base.md 02_memory.md 03_tools.md 04_conventions.md; do
    assert_file_not_exists "$repo_dir/.agent-layer/instructions/$name" \
      "$name is not seeded under minimal layout"
  done

  # Skills directory exists but is empty.
  assert_dir_exists "$repo_dir/.agent-layer/skills" \
    "skills directory exists under minimal layout"
  if [[ -z "$(ls -A "$repo_dir/.agent-layer/skills")" ]]; then
    pass "skills directory is empty under minimal layout"
  else
    fail "skills directory should be empty under minimal layout"
  fi

  # Memory templates are not written.
  for name in ISSUES.md BACKLOG.md ROADMAP.md DECISIONS.md COMMANDS.md CONTEXT.md; do
    assert_file_not_exists "$repo_dir/docs/agent-layer/$name" \
      "$name not seeded under minimal layout"
  done

  # The config and env files are still seeded (user-owned seed files).
  assert_file_exists "$repo_dir/.agent-layer/config.toml" \
    "config.toml seeded under minimal layout"
  assert_file_exists "$repo_dir/.agent-layer/.env" \
    ".env seeded under minimal layout"

  # al claude still launches end-to-end against the mock binary.
  install_mock_claude "$repo_dir"
  assert_exit_zero_in "$repo_dir" "al claude on minimal layout" al claude
  assert_claude_mock_called "$MOCK_CLAUDE_LOG"

  cleanup_scenario_dir "$repo_dir"
}
