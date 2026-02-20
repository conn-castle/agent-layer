#!/usr/bin/env bash
# Scenario 110: Doctor detects missing secrets and fails with clear messaging.

run_scenario_doctor_missing_secrets() {
  section "Doctor missing secrets"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Directly install the everything-enabled config (instead of wizard, since
  # wizard runs sync which needs the secrets we intentionally omit).
  cp "$E2E_FIXTURE_DIR/profiles/everything-enabled.toml" \
    "$repo_dir/.agent-layer/config.toml"

  # Write .env with ONLY context7 key — leave github and tavily missing
  cat > "$repo_dir/.agent-layer/.env" <<'ENVEOF'
AL_CONTEXT7_API_KEY=e2e-test
ENVEOF

  # Doctor should detect missing secrets and exit nonzero
  local doctor_output rc=0
  doctor_output=$(cd "$repo_dir" && al doctor 2>&1) || rc=$?
  if [[ $rc -ne 0 ]]; then
    pass "al doctor exits nonzero when secrets are missing (exit code: $rc)"
  else
    fail "al doctor should exit nonzero when secrets are missing, but got exit 0"
    echo "  output (first 10 lines):"
    echo "$doctor_output" | head -10 | sed 's/^/    /'
  fi

  # Verify doctor reported the failure with [FAIL] label
  assert_output_contains "$doctor_output" "[FAIL]" \
    "doctor output contains [FAIL] label"

  # Verify doctor identified the Secrets check
  assert_output_contains "$doctor_output" "Secrets" \
    "doctor output identifies Secrets check"

  # Verify doctor named a specific missing secret
  assert_output_contains "$doctor_output" "AL_GITHUB_PERSONAL_ACCESS_TOKEN" \
    "doctor output names missing github secret"

  # Verify doctor printed the recommendation hint
  assert_output_contains "$doctor_output" ".agent-layer/.env" \
    "doctor output recommends .agent-layer/.env"

  # Verify doctor printed the failure summary (not "All systems go")
  assert_output_contains "$doctor_output" "Some checks failed" \
    "doctor summary says some checks failed"
  assert_output_not_contains "$doctor_output" "All systems go" \
    "doctor summary does NOT say all systems go"

  # Verify doctor still ran the other checks before failing
  assert_output_contains "$doctor_output" "Checking Agent Layer health" \
    "doctor still printed health check header"
  assert_output_contains "$doctor_output" "Structure" \
    "doctor still checked structure"
  assert_output_contains "$doctor_output" "Config" \
    "doctor still checked config"

  cleanup_scenario_dir "$repo_dir"
}
