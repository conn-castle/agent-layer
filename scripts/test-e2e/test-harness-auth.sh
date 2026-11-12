#!/usr/bin/env bash
# test-harness-auth.sh — regression tests for fixture isolation and release
# authentication and parallel dispatch diagnostics. Validates the release
# Authorization header when GITHUB_TOKEN (or GH_TOKEN) is available, preventing
# GitHub API rate-limit failures in CI.
#
# Run: bash scripts/test-e2e/test-harness-auth.sh
# Exit: 0 on all pass, 1 on any failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Minimal harness bootstrap — we only need the function under test plus
# the pass/fail/section helpers. Source the full harness.
# shellcheck source=harness.sh
source "$SCRIPT_DIR/harness.sh"
isolate_test_git

# ---------------------------------------------------------------------------
# Mock curl — simulates GitHub API auth/rate limiting responses.
# Mode is controlled by MOCK_CURL_MODE:
# - auth-required (default): succeed only with Authorization header.
# - auth-fails: fail when Authorization header is present, succeed otherwise.
# - unauth-ok: succeed regardless of Authorization header.
# ---------------------------------------------------------------------------
MOCK_BIN_DIR="$(mktemp -d)"
trap 'rm -rf "$MOCK_BIN_DIR"' EXIT

cat > "$MOCK_BIN_DIR/curl" <<'MOCK_CURL'
#!/usr/bin/env bash
# Mock curl behavior controlled by MOCK_CURL_MODE.
mode="${MOCK_CURL_MODE:-auth-required}"
has_auth=0

# Walk args to find -H values (curl passes header as next arg after -H).
i=0
args=("$@")
while [[ $i -lt ${#args[@]} ]]; do
  if [[ "${args[$i]}" == "-H" ]]; then
    i=$((i + 1))
    if [[ $i -lt ${#args[@]} ]]; then
      case "${args[$i]}" in
        Authorization:\ Bearer\ *|Authorization:\ token\ *)
          has_auth=1
          ;;
      esac
    fi
  fi
  i=$((i + 1))
done

case "$mode" in
  auth-required)
    if [[ $has_auth -eq 1 ]]; then
      echo '{"tag_name": "v0.8.8", "name": "v0.8.8"}'
      exit 0
    fi
    exit 22
    ;;
  auth-fails)
    if [[ $has_auth -eq 1 ]]; then
      exit 22
    fi
    echo '{"tag_name": "v0.8.8", "name": "v0.8.8"}'
    exit 0
    ;;
  unauth-ok)
    echo '{"tag_name": "v0.8.8", "name": "v0.8.8"}'
    exit 0
    ;;
  *)
    exit 2
    ;;
esac
MOCK_CURL
chmod +x "$MOCK_BIN_DIR/curl"

# Prepend mock curl to PATH so the function finds it first.
export PATH="$MOCK_BIN_DIR:$PATH"
export AL_E2E_ONLINE=1

# ---------------------------------------------------------------------------
# Test cases
# ---------------------------------------------------------------------------
section "Harness auth: resolve_latest_release_version"

# --- Case 1: With GITHUB_TOKEN set, the function must pass the token -------
export MOCK_CURL_MODE="auth-required"
export GITHUB_TOKEN="ghp_test_mock_token_value"
unset GH_TOKEN 2>/dev/null || true

result=""
rc=0
result=$(resolve_latest_release_version) || rc=$?

if [[ $rc -eq 0 && "$result" == "0.8.8" ]]; then
  pass "resolve_latest_release_version uses GITHUB_TOKEN for auth"
else
  fail "resolve_latest_release_version should use GITHUB_TOKEN (got rc=$rc, result='$result')"
fi

# --- Case 2: With GH_TOKEN set (fallback), the function must pass it -------
export MOCK_CURL_MODE="auth-required"
unset GITHUB_TOKEN 2>/dev/null || true
export GH_TOKEN="ghp_test_gh_token_value"

result=""
rc=0
result=$(resolve_latest_release_version) || rc=$?

if [[ $rc -eq 0 && "$result" == "0.8.8" ]]; then
  pass "resolve_latest_release_version uses GH_TOKEN for auth"
else
  fail "resolve_latest_release_version should use GH_TOKEN (got rc=$rc, result='$result')"
fi

# --- Case 3: Auth fails, fallback to unauthenticated ----------------------
export MOCK_CURL_MODE="auth-fails"
export GITHUB_TOKEN="ghp_test_stale_token_value"
unset GH_TOKEN 2>/dev/null || true

result=""
rc=0
result=$(resolve_latest_release_version) || rc=$?

if [[ $rc -eq 0 && "$result" == "0.8.8" ]]; then
  pass "resolve_latest_release_version falls back when auth fails"
else
  fail "resolve_latest_release_version should fall back on auth failure (got rc=$rc, result='$result')"
fi

# --- Case 4: Without a token, use the unauthenticated API directly --------
export MOCK_CURL_MODE="unauth-ok"
unset GITHUB_TOKEN GH_TOKEN 2>/dev/null || true

result=""
rc=0
result=$(resolve_latest_release_version) || rc=$?

if [[ $rc -eq 0 && "$result" == "0.8.8" ]]; then
  pass "resolve_latest_release_version works without a configured token"
else
  fail "resolve_latest_release_version should work without a token (got rc=$rc, result='$result')"
fi

# Fixture Git initialization must remain local even when invoked from a Git hook.
section "Harness Git: inherited repository discovery"
if (
  E2E_TMP_ROOT="$MOCK_BIN_DIR/fixtures"
  mkdir -p "$E2E_TMP_ROOT" "$MOCK_BIN_DIR/external/worktree" "$MOCK_BIN_DIR/external/objects"
  init_fixture_git "$MOCK_BIN_DIR/external/worktree" || exit 1
  printf 'external index sentinel\n' > "$MOCK_BIN_DIR/external/index"
  printf 'external worktree sentinel\n' > "$MOCK_BIN_DIR/external/worktree/keep"
  cp -R "$MOCK_BIN_DIR/external" "$MOCK_BIN_DIR/external-before"
  export GIT_DIR="$MOCK_BIN_DIR/external/worktree/.git"
  export GIT_WORK_TREE="$MOCK_BIN_DIR/external/worktree"
  export GIT_COMMON_DIR="$GIT_DIR"
  export GIT_INDEX_FILE="$MOCK_BIN_DIR/external/index"
  export GIT_OBJECT_DIRECTORY="$MOCK_BIN_DIR/external/objects"
  export GIT_ALTERNATE_OBJECT_DIRECTORIES="$GIT_DIR/objects"
  export GIT_CEILING_DIRECTORIES="$MOCK_BIN_DIR"
  export GIT_DISCOVERY_ACROSS_FILESYSTEM=0 GIT_PREFIX=external/
  scenario="$(setup_scenario_dir)" || exit 1
  [[ -f "$scenario/.git/HEAD" && -f "$scenario/.git/config" ]] || exit 1
  old="$E2E_TMP_ROOT/old-version"
  mkdir -p "$old"
  cat > "$MOCK_BIN_DIR/old-al" <<'OLD_AL'
#!/usr/bin/env bash
[[ "$1" == init && "$2" == --no-wizard && -f .git/HEAD && -f .git/config ]]
OLD_AL
  chmod +x "$MOCK_BIN_DIR/old-al"
  setup_old_version_via_binary "$old" "$MOCK_BIN_DIR/old-al" || exit 1
  # Both public helpers created their own repositories, left the inherited
  # directory/index/worktree byte-identical, and did not mutate caller state.
  [[ "$GIT_DIR" == "$MOCK_BIN_DIR/external/worktree/.git" ]] || exit 1
  diff -r "$MOCK_BIN_DIR/external-before" "$MOCK_BIN_DIR/external"
); then
  pass "fixture Git initialization ignores inherited repository paths"
else
  fail "fixture Git initialization escaped its scenario or changed external state"
fi

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
  mkdir -p "$case_dir"
  (
    E2E_PASS_COUNT=0
    E2E_FAIL_COUNT=0
    # The harness may have been sourced on a terminal before redirection.
    _RED='' _GREEN='' _YELLOW='' _NC=''
    export MOCK_PARALLEL_LOG="$case_dir/invocations"
    : > "$MOCK_PARALLEL_LOG"
    # Seed stale outputs, including a valid slot whose start will fail.
    local slot
    for slot in {1..9}; do
      printf '{"state":"completed"}\n' > "$case_dir/parallel-$slot-wait.json"
    done
    check_dispatch_parallel_conversations "$case_dir"
    printf '%s %s\n' "$E2E_PASS_COUNT" "$E2E_FAIL_COUNT" > "$case_dir/counts"
  ) > "$case_dir/output" 2>&1
}

assert_parallel_diagnostic() {
  local case_dir="$1" command="$2" slot="$3" code="$4" marker="$5" other_marker="$6"
  local diagnostic
  diagnostic="$(awk -v label="FAIL: dispatch $command for parallel slot $slot " '
    index($0, label) == 1 { printing = 1; print; next }
    printing && /^(PASS|FAIL):/ { exit }
    printing { print }
  ' "$case_dir/output")"
  if [[ "$diagnostic" == *"exit code: $code"* && "$diagnostic" == *"$marker"* \
    && "$diagnostic" != *"$other_marker"* ]]; then
    pass "parallel $command slot $slot reports its exit code and own stderr"
  else
    fail "parallel $command slot $slot diagnostic missing or mixed: $diagnostic"
  fi
}

parallel_case="$MOCK_BIN_DIR/parallel-failures"
export MOCK_START_FAILURE_SLOT=2 MOCK_WAIT_FAILURE_SLOT=5
run_parallel_case "$parallel_case"
read -r parallel_passes parallel_failures < "$parallel_case/counts"
if [[ $parallel_failures -gt 0 ]]; then
  pass "parallel start/wait failures increase the failure count"
else
  fail "parallel start/wait failures did not increase the failure count"
fi
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
parallel_case="$MOCK_BIN_DIR/parallel-all-starts-fail"
run_parallel_case "$parallel_case"
if [[ -f "$parallel_case/counts" ]] \
  && [[ "$(grep -c '^FAIL: dispatch start for parallel slot ' "$parallel_case/output")" -eq 8 ]]; then
  pass "all failed starts report eight diagnostics and finish writing counters"
else
  fail "all failed starts must report eight diagnostics and finish writing counters"
fi
assert_file_not_contains "$parallel_case/invocations" '<dispatch><wait>' "all failed starts issue zero waits"
assert_file_contains "$parallel_case/output" "FAIL: eight independent conversations complete in parallel" \
  "all failed starts fail the aggregate despite stale completed outputs"
for slot in {1..8}; do
  assert_parallel_diagnostic "$parallel_case" start "$slot" 23 "start-error-slot-$slot" wait-error
done

unset MOCK_START_FAILURE_SLOT
export MOCK_EMPTY_HANDLE_SLOT=3 MOCK_INCOMPLETE_SLOT=6
parallel_case="$MOCK_BIN_DIR/parallel-invalid-results"
run_parallel_case "$parallel_case"
read -r parallel_passes parallel_failures < "$parallel_case/counts"
if [[ $parallel_failures -gt 0 ]]; then
  pass "empty handle and incomplete wait increase the failure count"
else
  fail "empty handle and incomplete wait did not increase the failure count"
fi
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
parallel_case="$MOCK_BIN_DIR/parallel-wait-failure"
run_parallel_case "$parallel_case"
assert_file_contains "$parallel_case/output" "FAIL: eight independent conversations complete in parallel" \
  "nonzero wait fails the aggregate even when all eight outputs say completed"
unset MOCK_WAIT_FAILURE_SLOT
export MOCK_INCOMPLETE_SLOT=6
parallel_case="$MOCK_BIN_DIR/parallel-incomplete-wait"
run_parallel_case "$parallel_case"
assert_file_contains "$parallel_case/output" "FAIL: eight independent conversations complete in parallel" \
  "zero-exit incomplete wait alone fails the aggregate"
unset MOCK_INCOMPLETE_SLOT

parallel_case="$MOCK_BIN_DIR/parallel-success"
run_parallel_case "$parallel_case"
assert_file_contains "$parallel_case/counts" "1 0" "all eight parallel conversations pass without failures"
assert_file_contains "$parallel_case/output" "PASS: eight independent conversations complete in parallel" \
  "parallel aggregate passes with eight completions even with unrelated stale output"
for slot in {1..8}; do
  assert_file_contains "$parallel_case/invocations" "<dispatch><wait><slot-$slot>" \
    "successful parallel run waits on slot $slot"
done

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "=== Harness Self-Test Summary ==="
echo "Tests: $((E2E_PASS_COUNT + E2E_FAIL_COUNT)) total, ${E2E_PASS_COUNT} passed, ${E2E_FAIL_COUNT} failed"

if [[ $E2E_FAIL_COUNT -gt 0 ]]; then
  exit 1
fi
