#!/usr/bin/env bash
# Scenario 084: Commands run without al init give a clear error message.

run_scenario_not_initialized_error() {
  section "Not initialized error"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  # Do NOT run al init — repo has .git/ but no .agent-layer/

  # al claude should fail with a clear message
  local claude_output rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || rc=$?
  if [[ $rc -ne 0 ]]; then
    pass "al claude exits nonzero without init"
  else
    fail "al claude should fail without init, but got exit 0"
  fi

  assert_output_contains "$claude_output" "isn't initialized" \
    "error says not initialized"
  assert_output_contains "$claude_output" "al init" \
    "error suggests al init"

  # al sync should also fail with same message
  local sync_output sync_rc=0
  sync_output=$(cd "$repo_dir" && al sync 2>&1) || sync_rc=$?
  if [[ $sync_rc -ne 0 ]]; then
    pass "al sync exits nonzero without init"
  else
    fail "al sync should fail without init, but got exit 0"
  fi

  assert_output_contains "$sync_output" "isn't initialized" \
    "sync error says not initialized"

  # al doctor should also fail with same message
  local doctor_output doctor_rc=0
  doctor_output=$(cd "$repo_dir" && al doctor 2>&1) || doctor_rc=$?
  if [[ $doctor_rc -ne 0 ]]; then
    pass "al doctor exits nonzero without init"
  else
    fail "al doctor should fail without init, but got exit 0"
  fi

  assert_output_contains "$doctor_output" "isn't initialized" \
    "doctor error says not initialized"

  cleanup_scenario_dir "$repo_dir"
}
