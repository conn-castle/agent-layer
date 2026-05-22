#!/usr/bin/env bash
# Scenario 112: Sync writes Antigravity repo-local files without creating
# Gemini trusted-folder state in HOME.

run_scenario_antigravity_sync_containment() {
  section "Antigravity sync containment"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  local old_home="$HOME"
  local fake_home="$repo_dir/home"
  mkdir -p "$fake_home"
  export HOME="$fake_home"

  local config_path="$repo_dir/.agent-layer/config.toml"
  local config_tmp="$repo_dir/.agent-layer/config.toml.tmp"
  sed '/^\[agents\.antigravity\]/,/^\[/ s/^enabled = .*/enabled = true/' \
    "$config_path" > "$config_tmp"
  mv "$config_tmp" "$config_path"

  assert_exit_zero_in "$repo_dir" "al sync writes Antigravity files" al sync

  local settings_file="$repo_dir/.agy/antigravity-cli/settings.json"
  local mcp_file="$repo_dir/.agy/antigravity-cli/mcp_config.json"
  assert_file_exists "$settings_file" "Antigravity settings created"
  assert_json_valid "$settings_file" "Antigravity settings is valid JSON"
  assert_file_exists "$mcp_file" "Antigravity MCP config created"
  assert_json_valid "$mcp_file" "Antigravity MCP config is valid JSON"

  local before_hash after_hash
  before_hash="$(portable_sha256 "$settings_file" | awk '{print $1}')"

  assert_exit_zero_in "$repo_dir" "al sync remains idempotent for Antigravity files" al sync

  assert_json_valid "$settings_file" "Antigravity settings remains valid JSON after second sync"
  after_hash="$(portable_sha256 "$settings_file" | awk '{print $1}')"
  assert_output_equals "$after_hash" "$before_hash" \
    "Antigravity settings content unchanged after second sync"

  if [[ -e "$HOME/.gemini/trustedFolders.json" ]]; then
    fail "al sync should not create Gemini trustedFolders.json"
  else
    pass "al sync does not create Gemini trustedFolders.json"
  fi

  export HOME="$old_home"
  cleanup_scenario_dir "$repo_dir"
}
