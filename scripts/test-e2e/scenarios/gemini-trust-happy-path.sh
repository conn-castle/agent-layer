#!/usr/bin/env bash
# Scenario 112: Sync auto-trusts the repo for Gemini by writing
# ~/.gemini/trustedFolders.json when Gemini is enabled.

run_scenario_gemini_trust_happy_path() {
  section "Gemini trust happy path"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  local old_home="$HOME"
  local fake_home="$repo_dir/home"
  mkdir -p "$fake_home"
  export HOME="$fake_home"

  local trust_file="$HOME/.gemini/trustedFolders.json"

  assert_exit_zero_in "$repo_dir" "al sync writes Gemini trusted folder" al sync

  assert_file_exists "$trust_file" "trustedFolders.json created"
  assert_json_valid "$trust_file" "trustedFolders.json is valid JSON"
  assert_file_contains "$trust_file" "$repo_dir" "trustedFolders.json includes repo path"
  assert_file_contains "$trust_file" "TRUST_FOLDER" "trustedFolders.json uses TRUST_FOLDER value"

  local before_hash after_hash
  before_hash="$(portable_sha256 "$trust_file" | awk '{print $1}')"

  assert_exit_zero_in "$repo_dir" "al sync remains idempotent with trust file present" al sync

  assert_json_valid "$trust_file" "trustedFolders.json remains valid JSON after second sync"
  after_hash="$(portable_sha256 "$trust_file" | awk '{print $1}')"
  assert_output_equals "$after_hash" "$before_hash" \
    "trustedFolders.json content unchanged after second sync"

  export HOME="$old_home"
  cleanup_scenario_dir "$repo_dir"
}
