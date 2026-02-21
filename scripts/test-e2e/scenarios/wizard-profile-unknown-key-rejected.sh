#!/usr/bin/env bash
# Scenario 111: wizard profile mode rejects unknown keys and preserves current
# repo state when profile validation fails.

run_scenario_wizard_profile_unknown_key_rejected() {
  section "Wizard profile unknown key rejected"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  local bad_profile="$repo_dir/bad-profile.toml"
  local bad_profile_tmp="$repo_dir/bad-profile.toml.tmp"
  cp "$E2E_DEFAULTS_TOML" "$bad_profile"

  # Inject an invalid key for an enable-only agent section in the profile.
  awk '
    /^\[agents\.antigravity\]$/ { print; in_antigravity=1; next }
    in_antigravity == 1 && /^enabled = / {
      print
      print "model = \"not-supported\""
      in_antigravity=0
      next
    }
    { print }
  ' "$bad_profile" > "$bad_profile_tmp"
  mv "$bad_profile_tmp" "$bad_profile"

  assert_file_contains "$bad_profile" 'model = "not-supported"' \
    "invalid antigravity model key injected into profile"

  local pre_snapshot="$E2E_TMP_ROOT/wizard-profile-unknown-pre.txt"
  _snapshot_agent_layer_state "$repo_dir" > "$pre_snapshot"

  local wizard_output wizard_rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --profile "$bad_profile" --yes 2>&1) || wizard_rc=$?
  if [[ $wizard_rc -ne 0 ]]; then
    pass "al wizard --profile rejects unknown profile key"
  else
    fail "al wizard --profile should reject unknown profile key"
  fi

  assert_output_contains "$wizard_output" "invalid profile" \
    "wizard profile error marks profile as invalid"
  assert_output_contains "$wizard_output" "unrecognized config keys" \
    "wizard profile error reports unrecognized keys"

  assert_agent_layer_state_unchanged "$repo_dir" "$pre_snapshot" \
    "failed profile validation leaves .agent-layer state unchanged"

  cleanup_scenario_dir "$repo_dir"
}
