#!/usr/bin/env bash
# Scenario 110: Unknown config keys are rejected by strict load paths,
# but remain repairable via doctor/wizard lenient flows.

run_scenario_config_unknown_keys_repairability() {
  section "Config unknown keys repairability"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  local config_path="$repo_dir/.agent-layer/config.toml"
  local tmp_config="$repo_dir/.agent-layer/config.toml.tmp"

  # Inject an invalid key for an enable-only agent section.
  awk '
    /^\[agents\.vscode\]$/ { print; in_vscode=1; next }
    in_vscode == 1 && /^enabled = / {
      print
      print "model = \"vscode-model-not-supported\""
      in_vscode=0
      next
    }
    { print }
  ' "$config_path" > "$tmp_config"
  mv "$tmp_config" "$config_path"

  assert_file_contains "$config_path" 'model = "vscode-model-not-supported"' \
    "invalid vscode model key injected"

  install_mock_claude "$repo_dir"

  local claude_output claude_rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || claude_rc=$?
  if [[ $claude_rc -ne 0 ]]; then
    pass "al claude fails with unknown config key"
  else
    fail "al claude should fail with unknown config key"
  fi
  assert_output_contains "$claude_output" "unrecognized config keys" \
    "al claude reports unrecognized config keys"
  assert_output_contains "$claude_output" "strict mode" \
    "al claude error indicates strict unknown-key decode"
  assert_claude_mock_not_called "$MOCK_CLAUDE_LOG"

  local sync_output sync_rc=0
  sync_output=$(cd "$repo_dir" && al sync 2>&1) || sync_rc=$?
  if [[ $sync_rc -ne 0 ]]; then
    pass "al sync fails with unknown config key"
  else
    fail "al sync should fail with unknown config key"
  fi
  assert_output_contains "$sync_output" "unrecognized config keys" \
    "al sync reports unrecognized config keys"

  local doctor_output doctor_rc=0
  doctor_output=$(cd "$repo_dir" && al doctor 2>&1) || doctor_rc=$?
  if [[ $doctor_rc -ne 0 ]]; then
    pass "al doctor exits nonzero for unknown-key config"
  else
    fail "al doctor should exit nonzero for unknown-key config"
  fi
  assert_output_contains "$doctor_output" "Checking Agent Layer health" \
    "doctor still runs health checks"
  assert_output_contains "$doctor_output" "[FAIL] Config" \
    "doctor reports config failure"
  assert_output_contains "$doctor_output" "unrecognized config keys" \
    "doctor surfaces unknown-key guidance"

  assert_exit_zero_in "$repo_dir" "al wizard --profile defaults --yes repairs unknown keys" \
    al wizard --profile "$E2E_DEFAULTS_TOML" --yes

  assert_file_not_contains "$config_path" 'model = "vscode-model-not-supported"' \
    "wizard repair removed invalid enable-only model key"

  assert_exit_zero_in "$repo_dir" "al sync succeeds after repair" al sync

  cleanup_scenario_dir "$repo_dir"
}
