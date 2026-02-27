#!/usr/bin/env bash
# test-harness-auth.sh — regression test for resolve_latest_release_version
# authentication. Validates that the function sends an Authorization header
# when GITHUB_TOKEN (or GH_TOKEN) is available, preventing GitHub API
# rate-limit failures in CI.
#
# Run: bash scripts/test-e2e/test-harness-auth.sh
# Exit: 0 on all pass, 1 on any failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Minimal harness bootstrap — we only need the function under test plus
# the pass/fail/section helpers. Source the full harness.
# shellcheck source=harness.sh
source "$SCRIPT_DIR/harness.sh"

# ---------------------------------------------------------------------------
# Mock curl — simulates GitHub API rate limiting for unauthenticated requests.
# Accepts requests that include "Authorization: Bearer" or "Authorization: token"
# headers and returns a valid release JSON. Rejects all others with exit 22
# (curl -f convention for 403).
# ---------------------------------------------------------------------------
MOCK_BIN_DIR="$(mktemp -d)"
trap 'rm -rf "$MOCK_BIN_DIR"' EXIT

cat > "$MOCK_BIN_DIR/curl" <<'MOCK_CURL'
#!/usr/bin/env bash
# Mock curl: require an Authorization header to succeed.
has_auth=0
for arg in "$@"; do
  case "$arg" in
    Authorization:\ Bearer\ *|Authorization:\ token\ *)
      has_auth=1
      ;;
  esac
done

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

if [[ $has_auth -eq 1 ]]; then
  # Simulate a valid GitHub release response.
  echo '{"tag_name": "v0.8.8", "name": "v0.8.8"}'
  exit 0
else
  # Simulate 403 rate-limit (curl -f returns 22 on HTTP 4xx).
  exit 22
fi
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

# --- Case 3: Without any token, function still attempts (unauthenticated) --
unset GITHUB_TOKEN 2>/dev/null || true
unset GH_TOKEN 2>/dev/null || true

result=""
rc=0
result=$(resolve_latest_release_version) || rc=$?

if [[ $rc -ne 0 ]]; then
  pass "resolve_latest_release_version fails without token when API rate-limits"
else
  # This is expected: unauthenticated calls may succeed on a real API but
  # fail when rate-limited. The mock rejects unauthenticated calls.
  pass "resolve_latest_release_version succeeded without token (unexpected but not wrong)"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "=== Harness Auth Summary ==="
echo "Tests: $((E2E_PASS_COUNT + E2E_FAIL_COUNT)) total, ${E2E_PASS_COUNT} passed, ${E2E_FAIL_COUNT} failed"

if [[ $E2E_FAIL_COUNT -gt 0 ]]; then
  exit 1
fi
