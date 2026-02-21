#!/usr/bin/env bash
# Scenario 113: Gemini trust-folder write failures produce suppressible warnings.

run_scenario_gemini_trust_warning_noise_mode() {
  section "Gemini trust warning + noise mode"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Force trust write failure: HOME points to a file, so HOME/.gemini is invalid.
  local old_home="$HOME"
  local bad_home="$repo_dir/home-file"
  printf 'not-a-directory\n' > "$bad_home"
  export HOME="$bad_home"

  install_mock_agent "$repo_dir" "gemini"

  local gemini_output gemini_rc=0
  gemini_output=$(cd "$repo_dir" && al gemini 2>&1) || gemini_rc=$?
  if [[ $gemini_rc -eq 0 ]]; then
    pass "al gemini still launches when trust write fails"
  else
    fail "al gemini should still launch when trust write fails (exit code: $gemini_rc)"
  fi
  assert_mock_agent_called "$MOCK_AGENT_LOG"
  assert_output_contains "$gemini_output" "GEMINI_TRUST_FOLDER_FAILED" \
    "al gemini prints trust warning code in default noise mode"

  local sync_output sync_rc=0
  sync_output=$(cd "$repo_dir" && al sync 2>&1) || sync_rc=$?
  if [[ $sync_rc -ne 0 ]]; then
    pass "al sync exits nonzero when trust warning is present"
  else
    fail "al sync should exit nonzero when trust warning is present"
  fi
  assert_output_contains "$sync_output" "GEMINI_TRUST_FOLDER_FAILED" \
    "al sync prints trust warning code in default noise mode"

  # Switch to reduce mode: suppressible warning should be filtered out.
  local config_path="$repo_dir/.agent-layer/config.toml"
  local tmp_config="$repo_dir/.agent-layer/config.toml.tmp"
  sed 's/noise_mode = "default"/noise_mode = "reduce"/' "$config_path" > "$tmp_config"
  mv "$tmp_config" "$config_path"
  assert_file_contains "$config_path" 'noise_mode = "reduce"' \
    "noise mode switched to reduce"

  local sync_reduce_output sync_reduce_rc=0
  sync_reduce_output=$(cd "$repo_dir" && al sync 2>&1) || sync_reduce_rc=$?
  if [[ $sync_reduce_rc -eq 0 ]]; then
    pass "al sync succeeds when trust warning is suppressed in reduce mode"
  else
    fail "al sync should succeed in reduce mode (exit code: $sync_reduce_rc)"
  fi
  assert_output_not_contains "$sync_reduce_output" "GEMINI_TRUST_FOLDER_FAILED" \
    "al sync suppresses trust warning code in reduce mode"

  local gemini_reduce_output gemini_reduce_rc=0
  gemini_reduce_output=$(cd "$repo_dir" && al gemini 2>&1) || gemini_reduce_rc=$?
  if [[ $gemini_reduce_rc -eq 0 ]]; then
    pass "al gemini still launches in reduce mode"
  else
    fail "al gemini should launch in reduce mode (exit code: $gemini_reduce_rc)"
  fi
  assert_output_not_contains "$gemini_reduce_output" "GEMINI_TRUST_FOLDER_FAILED" \
    "al gemini suppresses trust warning code in reduce mode"

  export HOME="$old_home"
  cleanup_scenario_dir "$repo_dir"
}
