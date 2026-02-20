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

  # Verify all instruction shim files have real content, not just existence
  assert_file_contains "$repo_dir/CLAUDE.md" "BEGIN: 00_base.md" \
    "CLAUDE.md includes instruction blocks"
  assert_file_contains "$repo_dir/AGENTS.md" "GENERATED FILE" \
    "AGENTS.md has managed marker"
  assert_file_contains "$repo_dir/GEMINI.md" "GENERATED FILE" \
    "GEMINI.md has managed marker"
  assert_file_contains "$repo_dir/.github/copilot-instructions.md" "GENERATED FILE" \
    "copilot-instructions.md has managed marker"
  assert_file_contains "$repo_dir/.codex/AGENTS.md" "GENERATED FILE" \
    "codex AGENTS.md has managed marker"

  # .mcp.json should be valid JSON with agent-layer server
  assert_json_valid "$repo_dir/.mcp.json" ".mcp.json is valid JSON"
  assert_file_contains "$repo_dir/.mcp.json" '"mcpServers"' \
    ".mcp.json has mcpServers key"
  assert_file_contains "$repo_dir/.mcp.json" '"mcp-prompts"' \
    ".mcp.json has mcp-prompts arg"

  # settings.json should also be valid JSON
  assert_json_valid "$repo_dir/.claude/settings.json" "settings.json is valid JSON"

  cleanup_scenario_dir "$repo_dir"
}
