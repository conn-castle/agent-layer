#!/usr/bin/env bash
# Comprehensive tests for release artifacts and build-release.sh
# Run via: make test-release
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors for output (disabled if not a terminal)
if [[ -t 1 ]]; then
  RED='\033[0;31m'
  GREEN='\033[0;32m'
  YELLOW='\033[0;33m'
  NC='\033[0m' # No Color
else
  RED=''
  GREEN=''
  YELLOW=''
  NC=''
fi

pass_count=0
fail_count=0

pass() {
  echo -e "${GREEN}PASS${NC}: $1"
  pass_count=$((pass_count + 1))
}

fail() {
  echo -e "${RED}FAIL${NC}: $1"
  fail_count=$((fail_count + 1))
}

warn() {
  echo -e "${YELLOW}WARN${NC}: $1"
}

section() {
  echo ""
  echo "=== $1 ==="
}

source "$SCRIPT_DIR/test-release/release_tests.sh"
source "$SCRIPT_DIR/test-release/tool_tests.sh"
source "$SCRIPT_DIR/test-release/upgrade_docs_tests.sh"

# -----------------------------------------------------------------------------
# Static Analysis & Setup
# -----------------------------------------------------------------------------
section "Static Analysis & Setup"

required_files=(
  "scripts/build-release.sh"
  "scripts/check-upgrade-docs.sh"
  "al-install.sh"
)

for file in "${required_files[@]}"; do
  if [[ -f "$ROOT_DIR/$file" ]]; then
    pass "$file exists"
  else
    fail "$file not found"
  fi
done

if [[ -x "$ROOT_DIR/scripts/build-release.sh" ]]; then
  pass "build-release.sh is executable"
else
  fail "build-release.sh is not executable"
fi

# Shell syntax validation
for script in "scripts/build-release.sh" "scripts/check-upgrade-docs.sh" "al-install.sh"; do
  if bash -n "$ROOT_DIR/$script" 2>/dev/null; then
    pass "$script has valid bash syntax"
  else
    fail "$script has invalid bash syntax"
  fi
done

# Optional: shellcheck
if command -v shellcheck >/dev/null 2>&1; then
  for script in "scripts/build-release.sh" "scripts/check-upgrade-docs.sh" "al-install.sh"; do
    if shellcheck -S error "$ROOT_DIR/$script" 2>/dev/null; then
      pass "$script passes shellcheck"
    else
      fail "$script has shellcheck errors"
    fi
  done
else
  warn "shellcheck not installed, skipping advanced shell linting"
fi

# Ensure required tools are available for release packaging.
if command -v git >/dev/null 2>&1; then
  pass "git is available"
else
  fail "git not found; required for source tarball generation"
fi

if command -v gzip >/dev/null 2>&1; then
  pass "gzip is available"
else
  fail "gzip not found; required for source tarball generation"
fi

if command -v tar >/dev/null 2>&1; then
  pass "tar is available"
else
  fail "tar not found; required for source tarball verification"
fi

if command -v go >/dev/null 2>&1; then
  pass "go is available"
else
  fail "go not found; required for release script tests"
fi

# Ensure we can validate checksums before running the build.
if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
  fail "sha256sum/shasum not found; refusing to run build-release.sh"
fi

if [[ $fail_count -gt 0 ]]; then
  section "Summary"
  total=$((pass_count + fail_count))
  printf 'Tests: %s total, %b%s passed%b, %b%s failed%b\n' "$total" "$GREEN" "$pass_count" "$NC" "$RED" "$fail_count" "$NC"
  exit 1
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
go_log="$tmp_dir/go-invocations.log"
dist_dir="$tmp_dir/dist"
expected_version="v1.0.0"
expected_version_no_v="${expected_version#v}"

run_release_generation_test
run_build_invocation_details
run_artifact_verification
run_source_tarball_verification
run_checksum_integrity
run_upgrade_docs_script_tests
run_go_tool_tests_extractchecksum
run_go_tool_tests_updateformula

# -----------------------------------------------------------------------------
# Summary
# -----------------------------------------------------------------------------
section "Summary for Release Testing"

total=$((pass_count + fail_count))
printf 'Tests: %s total, %b%s passed%b, %b%s failed%b\n' "$total" "$GREEN" "$pass_count" "$NC" "$RED" "$fail_count" "$NC"

if [[ $fail_count -gt 0 ]]; then
  exit 1
fi
