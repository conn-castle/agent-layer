#!/usr/bin/env bash
# Scenario 100: Doctor smoke test — verify doctor runs checks, reports results,
# and does not break the pipeline (al claude still works after doctor).

run_scenario_doctor_smoke() {
  section "Doctor smoke"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Make prompt-server expectations explicit instead of relying on defaults.
  local config_path="$repo_dir/.agent-layer/config.toml"
  local config_tmp="$repo_dir/.agent-layer/config.toml.tmp"
  assert_file_contains "$config_path" "[agents.gemini]" \
    "config has [agents.gemini] section"
  assert_file_contains "$config_path" "[agents.claude]" \
    "config has [agents.claude] section"
  sed \
    -e '/^\[agents\.gemini\]/,/^\[/ s/^enabled = .*/enabled = true/' \
    -e '/^\[agents\.claude\]/,/^\[/ s/^enabled = .*/enabled = true/' \
    "$config_path" > "$config_tmp"
  mv "$config_tmp" "$config_path"

  # Doctor validates MCP server secrets from .agent-layer/.env.
  # Default init has no MCP servers enabled, so this is technically unnecessary
  # but mirrors what a real user would have.
  cat > "$repo_dir/.agent-layer/.env" <<'ENVEOF'
AL_CONTEXT7_API_KEY=e2e-test
AL_GITHUB_PERSONAL_ACCESS_TOKEN=e2e-test
AL_TAVILY_API_KEY=e2e-test
ENVEOF

  assert_exit_zero_in "$repo_dir" "al sync" al sync

  # Snapshot sync outputs + .agent-layer/ state before doctor to verify read-only behavior
  local pre_doctor_snapshot="$E2E_TMP_ROOT/doctor-pre-snapshot.txt"
  _snapshot_all_state "$repo_dir" > "$pre_doctor_snapshot"

  # Capture doctor output to verify actual check results
  local doctor_output rc=0
  doctor_output=$(cd "$repo_dir" && al doctor 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al doctor exits zero"
  else
    fail "al doctor exits zero (exit code: $rc)"
    echo "  output (first 10 lines):"
    echo "$doctor_output" | head -10 | sed 's/^/    /'
  fi

  assert_no_crash_markers "$doctor_output" "no crash markers in doctor output"

  # Verify doctor printed the health check header
  assert_output_contains "$doctor_output" "Checking Agent Layer health" \
    "doctor output has health check header"

  # Verify doctor checked structure
  assert_output_contains "$doctor_output" "Structure" \
    "doctor output checks structure"

  # Verify doctor checked config
  assert_output_contains "$doctor_output" "Config" \
    "doctor output checks config"
  assert_output_contains "$doctor_output" "Configuration loaded successfully" \
    "doctor config check passed"

  # Verify doctor checked agents
  assert_output_contains "$doctor_output" "Agents" \
    "doctor output checks agents"

  # Verify doctor checked internal prompt server + client configs
  assert_output_contains "$doctor_output" "MCPPrompts" \
    "doctor output checks internal MCP prompt server"
  assert_output_contains "$doctor_output" "MCPPromptConfig" \
    "doctor output checks MCP prompt server client config"
  assert_file_exists "$repo_dir/.mcp.json" "sync generated .mcp.json"
  assert_output_contains "$doctor_output" ".mcp.json" \
    "doctor validates .mcp.json prompt server entry"
  assert_file_exists "$repo_dir/.gemini/settings.json" "sync generated .gemini/settings.json"
  assert_output_contains "$doctor_output" ".gemini/settings.json" \
    "doctor validates Gemini prompt server entry"

  # Verify doctor ran warning checks
  assert_output_contains "$doctor_output" "Running warning checks" \
    "doctor output ran warning checks"

  # Verify doctor summary was printed (positive: all healthy)
  assert_output_contains "$doctor_output" "All systems go" \
    "doctor summary says all systems go"

  # Verify no failures were reported
  assert_output_not_contains "$doctor_output" "[FAIL]" \
    "doctor output has no FAIL results"

  # ---- Prove doctor is read-only: sync outputs + .agent-layer/ unchanged ----
  assert_all_state_unchanged "$repo_dir" "$pre_doctor_snapshot" \
    "doctor did not modify sync outputs or .agent-layer/ state"

  # ---- Prove doctor is read-only: al claude still works afterward ----
  install_mock_claude "$repo_dir"

  local claude_output claude_rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || claude_rc=$?
  if [[ $claude_rc -eq 0 ]]; then
    pass "al claude works after doctor"
  else
    fail "al claude works after doctor (exit code: $claude_rc)"
    echo "  output (first 5 lines):"
    echo "$claude_output" | head -5 | sed 's/^/    /'
  fi

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"
  assert_generated_artifacts "$repo_dir"

  cleanup_scenario_dir "$repo_dir"
}
