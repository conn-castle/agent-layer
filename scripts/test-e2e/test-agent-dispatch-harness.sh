#!/usr/bin/env bash
# test-agent-dispatch-harness.sh — mocked parallel-dispatch scenario self-tests.
# Run: bash scripts/test-e2e/test-agent-dispatch-harness.sh
# Exit: 0 on all pass, 1 on any failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=harness.sh
source "$SCRIPT_DIR/harness.sh"
isolate_test_git

TEST_TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_TMP_DIR"' EXIT
MOCK_BIN_DIR="$TEST_TMP_DIR/bin"
mkdir -p "$MOCK_BIN_DIR"
export PATH="$MOCK_BIN_DIR:$PATH"

# Exercise the actual scenario block, retaining the harness's accumulating
# pass/fail helpers. Expected scenario failures stay inside a subshell.
# shellcheck source=scenarios/agent-dispatch.sh
source "$SCRIPT_DIR/scenarios/agent-dispatch.sh"
section "Harness dispatch: parallel conversation diagnostics"

cat > "$MOCK_BIN_DIR/al" <<'MOCK_AL'
#!/usr/bin/env bash
set -euo pipefail
# One write per invocation preserves argument boundaries even for empty handles.
printf '%s\n' "$(printf '<%s>' "$@")" >> "$MOCK_PARALLEL_LOG"
[[ "$1" == dispatch ]]
case "$2" in
  start)
    while [[ $# -gt 0 && "$1" != --prompt ]]; do shift; done
    slot="${2#Review }"
    if [[ "${MOCK_START_FAILURE_SLOT:-}" == all || "$slot" == "${MOCK_START_FAILURE_SLOT:-}" ]]; then
      echo "start-error-slot-$slot" >&2
      # A handle must not override the failed command's exit status.
      printf '{"handle":"slot-%s"}\n' "$slot"
      exit 23
    fi
    if [[ "$slot" == "${MOCK_EMPTY_HANDLE_SLOT:-}" ]]; then
      echo "empty-handle-slot-$slot" >&2
      echo '{}'
      exit 0
    fi
    printf '{"handle":"slot-%s"}\n' "$slot"
    ;;
  wait)
    slot="${3#slot-}"
    if [[ "$slot" == "${MOCK_WAIT_FAILURE_SLOT:-}" ]]; then
      echo "wait-error-slot-$slot" >&2
      # Completed-looking output must not hide a nonzero exit status.
      echo '{"state":"completed"}'
      exit 37
    fi
    if [[ "$slot" == "${MOCK_INCOMPLETE_SLOT:-}" ]]; then
      echo "incomplete-slot-$slot" >&2
      echo '{"state":"running"}'
      exit 0
    fi
    if [[ "${MOCK_WAIT_BARRIER:-0}" == 1 ]]; then
      # Arrival files make concurrency necessary without timing assertions.
      : > "$MOCK_PARALLEL_BARRIER/$slot"
      deadline=$((SECONDS + 15))
      while :; do
        arrived=0
        for waiter in {1..8}; do
          if [[ -f "$MOCK_PARALLEL_BARRIER/$waiter" ]]; then
            arrived=$((arrived + 1))
          fi
        done
        [[ $arrived -eq 8 ]] && break
        if [[ -f "$MOCK_PARALLEL_BARRIER/timeout" || $SECONDS -ge $deadline ]]; then
          : > "$MOCK_PARALLEL_BARRIER/timeout"
          echo "barrier timeout: parallel slot $slot saw $arrived of 8 waiters" >&2
          exit 47
        fi
        sleep 0.05
      done
    fi
    echo '{"state":"completed"}'
    ;;
  *) exit 99 ;;
esac
MOCK_AL
chmod +x "$MOCK_BIN_DIR/al"

# Capture counters in the same subshell that runs the function, since command
# substitution alone would discard them. Do not turn scenario failures into
# shell exits: fail() records assertions and deliberately returns success.
run_parallel_case() {
  local case_dir="$1"
  local case_pid case_rc=0
  mkdir -p "$case_dir/barrier"
  rm -f "$case_dir/counts"
  (
    E2E_PASS_COUNT=0
    E2E_FAIL_COUNT=0
    # The harness may have been sourced on a terminal before redirection.
    _RED='' _GREEN='' _YELLOW='' _NC=''
    export MOCK_PARALLEL_LOG="$case_dir/invocations"
    export MOCK_PARALLEL_BARRIER="$case_dir/barrier"
    : > "$MOCK_PARALLEL_LOG"
    # Seed stale outputs, including a valid slot whose start will fail.
    local slot
    for slot in {1..9}; do
      printf '{"state":"completed"}\n' > "$case_dir/parallel-$slot-wait.json"
    done
    check_dispatch_parallel_conversations "$case_dir"
    printf '%s %s\n' "$E2E_PASS_COUNT" "$E2E_FAIL_COUNT" > "$case_dir/counts"
  ) > "$case_dir/output" 2>&1 &
  case_pid=$!
  # Testing wait's status here preserves errexit inside the background case.
  wait "$case_pid" || case_rc=$?
  if [[ $case_rc -ne 0 || ! -f "$case_dir/counts" ]]; then
    fail "parallel case ${case_dir##*/} did not finish (exit status: $case_rc; counters required)"
    dispatch_show_tail output "$case_dir/output" 20
  fi
}

assert_parallel_counts() {
  local case_dir="$1" expected_passes="$2" expected_failures="$3" label="$4"
  local parallel_passes='' parallel_failures='' extra=''
  if [[ -f "$case_dir/counts" ]] \
    && read -r parallel_passes parallel_failures extra < "$case_dir/counts" \
    && [[ "$parallel_passes" =~ ^[0-9]+$ && "$parallel_failures" =~ ^[0-9]+$ && -z "$extra" ]] \
    && [[ $parallel_passes -eq $expected_passes && $parallel_failures -eq $expected_failures ]]; then
    pass "$label"
  else
    fail "$label (expected $expected_passes $expected_failures; got '$parallel_passes $parallel_failures $extra')"
    dispatch_show_tail output "$case_dir/output" 20
  fi
}

assert_parallel_diagnostic() {
  local case_dir="$1" command="$2" slot="$3" code="$4" marker="$5" other_marker="${6:-}"
  local diagnostic
  diagnostic="$(awk -v label="FAIL: dispatch $command for parallel slot $slot " '
    index($0, label) == 1 { printing = 1; print; next }
    printing && /^(PASS|FAIL):/ { exit }
    printing { print }
  ' "$case_dir/output")"
  if [[ "$diagnostic" == *"exit code: $code"* && "$diagnostic" == *"$marker"* \
    && ( -z "$other_marker" || "$diagnostic" != *"$other_marker"* ) ]]; then
    pass "parallel $command slot $slot reports its exit code and own stderr"
  else
    fail "parallel $command slot $slot diagnostic missing or mixed: $diagnostic"
  fi
}

# Run this private probe in a separate script process so its expected failure
# must produce a failing exit status and still reach the normal summary.
if [[ "${1:-}" == --unexpected-error-probe ]]; then
  check_dispatch_parallel_conversations() {
    echo "unexpected-error-probe-marker"
    return 42
  }
  run_parallel_case "$TEST_TMP_DIR/unexpected-error-probe"
else
  probe_rc=0
  bash "$SCRIPT_DIR/test-agent-dispatch-harness.sh" --unexpected-error-probe > "$TEST_TMP_DIR/guard-probe.log" 2>&1 || probe_rc=$?
  if [[ $probe_rc -eq 1 ]]; then
    pass "unexpected parallel error makes the self-test fail"
  else
    fail "unexpected parallel error must exit 1 (got $probe_rc)"
  fi
  assert_file_contains "$TEST_TMP_DIR/guard-probe.log" \
    'FAIL: parallel case unexpected-error-probe did not finish (exit status: 42; counters required)' \
    "unexpected parallel error reports its case and exit status"
  assert_file_contains "$TEST_TMP_DIR/guard-probe.log" 'output (last 20 lines):' \
    "unexpected parallel error prints a bounded output tail"
  assert_file_contains "$TEST_TMP_DIR/guard-probe.log" unexpected-error-probe-marker \
    "unexpected parallel error retains captured diagnostics"
  assert_file_contains "$TEST_TMP_DIR/guard-probe.log" '=== Agent Dispatch Harness Self-Test Summary ===' \
    "unexpected parallel error continues to the summary"

  parallel_case="$TEST_TMP_DIR/parallel-failures"
  export MOCK_START_FAILURE_SLOT=2 MOCK_WAIT_FAILURE_SLOT=5
  run_parallel_case "$parallel_case"
  assert_parallel_counts "$parallel_case" 0 3 "parallel start/wait failures record exactly zero passes and three failures"
  assert_file_contains "$parallel_case/output" "FAIL: eight independent conversations complete in parallel" \
    "parallel aggregate fails despite stale completed outputs"
  assert_parallel_diagnostic "$parallel_case" start 2 23 start-error-slot-2 wait-error-slot-5
  assert_parallel_diagnostic "$parallel_case" wait 5 37 wait-error-slot-5 start-error-slot-2
  assert_file_not_contains "$parallel_case/invocations" '<dispatch><wait><>' "parallel never waits on an empty handle"
  assert_file_not_contains "$parallel_case/invocations" '<dispatch><wait><slot-2>' "parallel skips wait after a failed start"
  for slot in 1 3 4 5 6 7 8; do
    assert_file_contains "$parallel_case/invocations" "<dispatch><wait><slot-$slot>" \
      "parallel still waits on independent slot $slot"
  done

  unset MOCK_WAIT_FAILURE_SLOT
  export MOCK_START_FAILURE_SLOT=all
  parallel_case="$TEST_TMP_DIR/parallel-all-starts-fail"
  run_parallel_case "$parallel_case"
  if [[ "$(grep -c '^FAIL: dispatch start for parallel slot ' "$parallel_case/output")" -eq 8 ]]; then
    pass "all failed starts report eight start diagnostics"
  else
    fail "all failed starts must report eight start diagnostics"
  fi
  assert_parallel_counts "$parallel_case" 0 9 "all failed starts record exactly zero passes and nine failures"
  assert_file_not_contains "$parallel_case/invocations" '<dispatch><wait>' "all failed starts issue zero waits"
  assert_file_contains "$parallel_case/output" "FAIL: eight independent conversations complete in parallel" \
    "all failed starts fail the aggregate despite stale completed outputs"
  for slot in {1..8}; do
    assert_parallel_diagnostic "$parallel_case" start "$slot" 23 "start-error-slot-$slot" wait-error
  done

  unset MOCK_START_FAILURE_SLOT
  export MOCK_EMPTY_HANDLE_SLOT=3 MOCK_INCOMPLETE_SLOT=6
  parallel_case="$TEST_TMP_DIR/parallel-invalid-results"
  run_parallel_case "$parallel_case"
  assert_parallel_counts "$parallel_case" 0 3 "empty handle and incomplete wait record exactly zero passes and three failures"
  assert_file_contains "$parallel_case/output" "FAIL: eight independent conversations complete in parallel" \
    "empty handle and incomplete wait fail the parallel aggregate"
  assert_parallel_diagnostic "$parallel_case" start 3 0 empty-handle-slot-3 incomplete-slot-6
  assert_parallel_diagnostic "$parallel_case" wait 6 0 incomplete-slot-6 empty-handle-slot-3
  assert_file_contains "$parallel_case/output" '    {}' "empty-handle diagnostic shows the start JSON"
  assert_file_contains "$parallel_case/output" '    {"state":"running"}' "incomplete-wait diagnostic shows the wait JSON"
  assert_file_not_contains "$parallel_case/invocations" '<dispatch><wait><>' "empty start result never schedules an empty-handle wait"
  assert_file_not_contains "$parallel_case/invocations" '<dispatch><wait><slot-3>' "empty start result skips its dependent wait"

  unset MOCK_EMPTY_HANDLE_SLOT MOCK_INCOMPLETE_SLOT
  # Isolate wait failures so a failed start cannot mask incorrect aggregation of
  # a nonzero wait with completed-looking stdout, or a zero-exit incomplete wait.
  export MOCK_WAIT_FAILURE_SLOT=5
  parallel_case="$TEST_TMP_DIR/parallel-wait-failure"
  run_parallel_case "$parallel_case"
  assert_parallel_counts "$parallel_case" 0 2 "nonzero wait records exactly zero passes and two failures"
  assert_parallel_diagnostic "$parallel_case" wait 5 37 wait-error-slot-5
  assert_file_contains "$parallel_case/output" "FAIL: eight independent conversations complete in parallel" \
    "nonzero wait fails the aggregate even when all eight outputs say completed"
  unset MOCK_WAIT_FAILURE_SLOT
  export MOCK_INCOMPLETE_SLOT=6
  parallel_case="$TEST_TMP_DIR/parallel-incomplete-wait"
  run_parallel_case "$parallel_case"
  assert_parallel_counts "$parallel_case" 0 2 "incomplete wait records exactly zero passes and two failures"
  assert_parallel_diagnostic "$parallel_case" wait 6 0 incomplete-slot-6
  assert_file_contains "$parallel_case/output" "FAIL: eight independent conversations complete in parallel" \
    "zero-exit incomplete wait alone fails the aggregate"
  unset MOCK_INCOMPLETE_SLOT

  parallel_case="$TEST_TMP_DIR/parallel-success"
  export MOCK_WAIT_BARRIER=1
  run_parallel_case "$parallel_case"
  unset MOCK_WAIT_BARRIER
  assert_parallel_counts "$parallel_case" 1 0 "all eight parallel conversations pass without failures"
  assert_file_contains "$parallel_case/output" "PASS: eight independent conversations complete in parallel" \
    "parallel aggregate passes with eight completions even with unrelated stale output"
  for slot in {1..8}; do
    assert_file_contains "$parallel_case/invocations" "<dispatch><wait><slot-$slot>" \
      "successful parallel run waits on slot $slot"
  done
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "=== Agent Dispatch Harness Self-Test Summary ==="
echo "Tests: $((E2E_PASS_COUNT + E2E_FAIL_COUNT)) total, ${E2E_PASS_COUNT} passed, ${E2E_FAIL_COUNT} failed"

if [[ $E2E_FAIL_COUNT -gt 0 ]]; then
  exit 1
fi
