#!/usr/bin/env bash
# Grok probe — verifies al probe grok runs grok and emits JSON.

install_mock_grok_probe() {
  local dir="$1"
  local exit_code="${2:-0}"
  local mock_bin="$dir/mock-bin"
  local log_dir="$dir/mock-logs"
  mkdir -p "$mock_bin" "$log_dir"

  MOCK_AGENT_LOG="$log_dir/grok-probe.log"
  : > "$MOCK_AGENT_LOG"

  cat > "$mock_bin/grok" <<MOCK_EOF
#!/usr/bin/env bash
log="$log_dir/grok-probe.log"

{
  echo "ARGC=\$#"
  echo "ARGS=\$*"
  i=0
  for arg in "\$@"; do
    echo "ARG_\${i}=\${arg}"
    i=\$((i + 1))
  done
  env | grep -E '^(AL_|GROK_HOME=)' | sort || true
  echo "---END---"
} >> "\$log"

if [[ "\${1:-}" == "--version" ]]; then
  echo "grok 1.0.5 (deadbeef) [stable]"
  exit 0
fi

echo '{"type":"text","data":"probe"}'
echo '{"type":"end","stopReason":"end_turn","sessionId":"probe-session","requestId":"probe-request"}'
exit $exit_code
MOCK_EOF

  chmod +x "$mock_bin/grok"
  export PATH="$mock_bin:$PATH"
}

run_scenario_grok_probe() {
  section "Grok probe"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  install_mock_grok_probe "$repo_dir" 0

  local output rc=0
  output="$(cd "$repo_dir" && al probe grok 2>&1)" || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al probe grok succeeds with mock grok"
  else
    fail "al probe grok should succeed with mock grok (exit code: $rc)"
    echo "$output" | head -20 | sed 's/^/    /'
  fi
  assert_output_contains "$output" '"grok_version": "grok 1.0.5 (deadbeef) [stable]"' \
    "probe JSON includes grok version"
  assert_output_contains "$output" '"grok_home_dir":' \
    "probe JSON includes grok_home_dir"
  assert_output_contains "$output" '"streaming_json_used": true' \
    "probe observes streaming-json stdout"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--trust"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--no-auto-update"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--always-approve"
  assert_mock_agent_has_arg "$MOCK_AGENT_LOG" "--output-format"

  install_mock_grok_probe "$repo_dir" 2
  rc=0
  output="$(cd "$repo_dir" && al probe grok 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    pass "al probe grok exits non-zero when grok fails"
  else
    fail "al probe grok should exit non-zero when grok fails"
  fi
  assert_output_contains "$output" '"exit_code": 2' \
    "failed probe still prints JSON exit_code"

  cleanup_scenario_dir "$repo_dir"
}
