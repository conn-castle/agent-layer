#!/usr/bin/env bash
# Scenario: Doctor detects and reports skill validation issues without crashing.
# Tests that the doctor command handles skill name mismatches gracefully and
# reports them as config failures rather than panicking.

run_scenario_doctor_skill_validation() {
  section "Doctor skill validation"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Enable claude so skills directory is populated
  local config_path="$repo_dir/.agent-layer/config.toml"
  local config_tmp="$repo_dir/.agent-layer/config.toml.tmp"
  sed \
    -e '/^\[agents\.claude\]/,/^\[/ s/^enabled = .*/enabled = true/' \
    "$config_path" > "$config_tmp"
  mv "$config_tmp" "$config_path"

  assert_exit_zero_in "$repo_dir" "al sync" al sync

  # ---- Create a well-formed custom skill ----
  local good_skill_dir="$repo_dir/.agent-layer/skills/my-good-skill"
  mkdir -p "$good_skill_dir"
  cat > "$good_skill_dir/SKILL.md" <<'SKILL'
---
name: my-good-skill
description: A well-formed test skill.
---
This skill does nothing but it is well-formed.
SKILL

  # ---- Create a skill with name mismatch (directory name != frontmatter name) ----
  # This triggers a config load failure because the loader validates that
  # skill names match their directory names.
  local mismatch_dir="$repo_dir/.agent-layer/skills/real-name"
  mkdir -p "$mismatch_dir"
  cat > "$mismatch_dir/SKILL.md" <<'SKILL'
---
name: wrong-name
description: Name does not match directory.
---
This skill has a name mismatch.
SKILL

  # Doctor should detect the config problem and report [FAIL] without crashing.
  # Doctor uses lenient config loading so it still runs health checks.
  local doctor_output doctor_rc=0
  doctor_output=$(cd "$repo_dir" && al doctor 2>&1) || doctor_rc=$?

  assert_output_contains "$doctor_output" "Checking Agent Layer health" \
    "doctor runs health check despite skill issue"
  assert_output_contains "$doctor_output" "[FAIL]" \
    "doctor reports failure for skill mismatch"
  assert_no_crash_markers "$doctor_output" "no crash markers in doctor output"
  assert_output_not_contains "$doctor_output" "panic" \
    "no panic in doctor output"

  # Remove the mismatched skill so al claude can work
  rm -rf "$mismatch_dir"

  # ---- Verify al claude works with only the good custom skill ----
  install_mock_claude "$repo_dir"

  local claude_output claude_rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || claude_rc=$?
  if [[ $claude_rc -eq 0 ]]; then
    pass "al claude works with valid custom skill"
  else
    fail "al claude should work with valid custom skill (exit code: $claude_rc)"
    echo "  output (first 5 lines):"
    echo "$claude_output" | head -5 | sed 's/^/    /'
  fi

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"

  cleanup_scenario_dir "$repo_dir"
}
