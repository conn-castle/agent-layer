#!/usr/bin/env bash
# Scenario 113: Antigravity probe — verifies al probe agy runs agy,
# emits machine-readable JSON, and preserves JSON output on non-zero agy exit.

install_mock_agy_probe() {
  local dir="$1"
  local exit_code="${2:-0}"
  local mock_bin="$dir/mock-bin"
  local log_dir="$dir/mock-logs"
  mkdir -p "$mock_bin" "$log_dir"

  MOCK_AGENT_LOG="$log_dir/agy-probe.log"
  : > "$MOCK_AGENT_LOG"

  cat > "$mock_bin/agy" <<MOCK_EOF
#!/usr/bin/env bash
log="$log_dir/agy-probe.log"
gemini_dir=""

{
  echo "ARGC=\$#"
  echo "ARGS=\$*"
  i=0
  for arg in "\$@"; do
    echo "ARG_\${i}=\${arg}"
    if [[ "\$arg" == --gemini_dir=* ]]; then
      gemini_dir="\${arg#--gemini_dir=}"
    fi
    i=\$((i + 1))
  done
  env | grep -E '^(AL_|AGY_)' | sort || true
  echo "---END---"
} >> "\$log"

if [[ "\${1:-}" == "--version" ]]; then
  echo "agy 1.0.0"
  exit 0
fi

if [[ -n "\$gemini_dir" ]]; then
  mkdir -p "\$gemini_dir/antigravity-cli/log"
  cat > "\$gemini_dir/antigravity-cli/log/cli.log" <<'LOG_EOF'
cli_setting_manager.go:123] CLI settings initialized: permissions=command(echo PROBEALLOWMARKER)
migrate.go:12] Migrating file /tmp/probe/agycfg/antigravity-cli/mcp_config.json to /tmp/probe/agycfg/config/mcp_config.json
LOG_EOF
fi

echo "INSTRUCTIONMARKER88 global-only-skill shared-tier-dup probe-mcp-antigravity-tier"
exit $exit_code
MOCK_EOF

  chmod +x "$mock_bin/agy"
  export PATH="$mock_bin:$PATH"
}

run_scenario_antigravity_probe() {
  section "Antigravity probe"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  install_mock_agy_probe "$repo_dir" 0

  local output rc=0
  output="$(cd "$repo_dir" && al probe agy 2>&1)" || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al probe agy succeeds with mock agy"
  else
    fail "al probe agy should succeed with mock agy (exit code: $rc)"
    echo "$output" | head -20 | sed 's/^/    /'
  fi
  assert_output_contains "$output" '"agy_version": "agy 1.0.0"' \
    "probe JSON includes agy version"
  assert_output_contains "$output" '"agy_config_dir":' \
    "probe JSON uses agy_config_dir key"
  assert_output_contains "$output" '"permissions_loaded": true' \
    "probe observes permissions"
  assert_output_contains "$output" '"instructions_loaded": true' \
    "probe observes AGENTS.md instructions"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--print"

  install_mock_agy_probe "$repo_dir" 2
  rc=0
  output="$(cd "$repo_dir" && al probe agy 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    pass "al probe agy exits non-zero when agy fails"
  else
    fail "al probe agy should exit non-zero when agy fails"
  fi
  assert_output_contains "$output" '"exit_code": 2' \
    "failed probe still prints JSON exit_code"
  assert_output_contains "$output" '"error": "exit status 2"' \
    "failed probe JSON includes agy error"

  cleanup_scenario_dir "$repo_dir"
}
