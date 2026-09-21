#!/usr/bin/env bash
# test-harness-auth.sh — regression tests for fixture isolation and release
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

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "=== Harness Self-Test Summary ==="
echo "Tests: $((E2E_PASS_COUNT + E2E_FAIL_COUNT)) total, ${E2E_PASS_COUNT} passed, ${E2E_FAIL_COUNT} failed"

if [[ $E2E_FAIL_COUNT -gt 0 ]]; then
  exit 1
fi
