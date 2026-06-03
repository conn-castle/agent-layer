#!/usr/bin/env bash
# Scenario 015: Fresh install, standalone al sync.
# Preserves existing coverage from the original test-e2e.sh.

run_scenario_fresh_install_sync() {
  section "Fresh install + standalone sync"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  assert_al_init_structure "$repo_dir"

  assert_exit_zero_in "$repo_dir" "al sync" al sync

  assert_generated_artifacts "$repo_dir"

  # .mcp.json should be valid JSON
  assert_json_valid "$repo_dir/.mcp.json" ".mcp.json is valid JSON"
  assert_file_contains "$repo_dir/.mcp.json" '"_generatedBy"' \
    ".mcp.json has provenance marker"

  # Claude skills should be synced natively
  assert_dir_exists "$repo_dir/.claude/skills" \
    ".claude/skills directory exists after sync"

  # settings.json should also be valid JSON
  assert_json_valid "$repo_dir/.claude/settings.json" "settings.json is valid JSON"

  cleanup_scenario_dir "$repo_dir"
}
