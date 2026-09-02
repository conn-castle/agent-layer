#!/usr/bin/env bash
# Comprehensive tests for release artifacts and build-release.sh
# Run via: make test-release
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# These tests drive build-release.sh with a mock `go` and must never invoke real
# code signing. `make release-dist` runs this suite as its `test-release`
# prerequisite while exporting AL_CODESIGN_IDENTITY/AL_REQUIRE_CODESIGN for the
# actual build, so clear them here to keep the codesign-requirement test
# deterministic regardless of the caller's environment.
unset AL_CODESIGN_IDENTITY AL_REQUIRE_CODESIGN

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
source "$SCRIPT_DIR/test-release/catalog_certification_tests.sh"
source "$SCRIPT_DIR/test-release/workflow_security_tests.sh"

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
go_log="$tmp_dir/go-invocations.log"
dist_dir="$tmp_dir/dist"
# Stable release builds require the matching checked-in migration manifest.
expected_version="v0.18.4"
expected_version_no_v="${expected_version#v}"

run_catalog_certification_script_tests
run_release_workflow_security_tests
run_release_generation_test
run_missing_migration_manifest_test
run_release_smoke_rejection_tests
run_build_invocation_details
run_artifact_verification
run_source_tarball_verification
run_checksum_integrity
run_codesign_requirement_test
run_release_vulnerability_gate_test
run_upgrade_docs_script_tests
run_go_tool_tests_extractchecksum
run_go_tool_tests_updateformula
run_go_tool_tests_updateformula_unit
run_go_tool_tests_gentemplatemanifest

# -----------------------------------------------------------------------------
# Summary
# -----------------------------------------------------------------------------
section "Summary for Release Testing"

total=$((pass_count + fail_count))
printf 'Tests: %s total, %b%s passed%b, %b%s failed%b\n' "$total" "$GREEN" "$pass_count" "$NC" "$RED" "$fail_count" "$NC"

if [[ $fail_count -gt 0 ]]; then
  exit 1
fi
