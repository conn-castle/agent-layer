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

# -----------------------------------------------------------------------------
# Static Analysis & Setup
# -----------------------------------------------------------------------------
section "Static Analysis & Setup"

required_files=(
  "scripts/build-release.sh"
  "al-install.sh"
  "al-install.ps1"
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
for script in "scripts/build-release.sh" "al-install.sh"; do
  if bash -n "$ROOT_DIR/$script" 2>/dev/null; then
    pass "$script has valid bash syntax"
  else
    fail "$script has invalid bash syntax"
  fi
done

# Optional: shellcheck
if command -v shellcheck >/dev/null 2>&1; then
  for script in "scripts/build-release.sh" "al-install.sh"; do
    if shellcheck -S error "$ROOT_DIR/$script" 2>/dev/null; then
      pass "$script passes shellcheck"
    else
      fail "$script has shellcheck errors"
    fi
  done
else
  warn "shellcheck not installed, skipping advanced shell linting"
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

# -----------------------------------------------------------------------------
# Functional Test: Release Generation
# -----------------------------------------------------------------------------
section "Release Generation Test"

# Create a temporary directory for testing
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
go_log="$tmp_dir/go-invocations.log"

# Mock 'go' to simulate build without compiling code
# This ensures we test the shell script logic, not the Go compiler
mock_bin="$tmp_dir/mock-bin"
mkdir -p "$mock_bin"
cat > "$mock_bin/go" << 'MOCK_GO'
#!/usr/bin/env bash
# Mock go command that creates fake binaries for testing
set -euo pipefail

log_path="${MOCK_GO_LOG:?MOCK_GO_LOG not set}"

# Simple argument parsing to capture output, ldflags, and package path
output=""
ldflags=""
pkg=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o)
      output="$2"
      shift 2
      ;;
    -ldflags)
      ldflags="$2"
      shift 2
      ;;
    *)
      pkg="$1"
      shift
      ;;
  esac
done

if [[ -z "$output" ]]; then
  echo "Error: Mock go called without -o" >&2
  exit 1
fi

if [[ -z "$pkg" ]]; then
  echo "Error: Mock go called without a package path" >&2
  exit 1
fi

printf '%s|%s|%s|%s|%s|%s\n' "${GOOS:-}" "${GOARCH:-}" "${CGO_ENABLED:-}" "$output" "$ldflags" "$pkg" >> "$log_path"

mkdir -p "$(dirname "$output")"
echo "mock binary: $output" > "$output"
chmod +x "$output"
MOCK_GO
chmod +x "$mock_bin/go"

# Run build-release.sh with mocked go
dist_dir="$tmp_dir/dist"
expected_version="test-v1.0.0"
build_success=0
echo "Running build-release.sh in test environment..."

if (
  export PATH="$mock_bin:$PATH"
  export MOCK_GO_LOG="$go_log"
  cd "$ROOT_DIR"
  # Override AL_VERSION and DIST_DIR for testing
  AL_VERSION="$expected_version" DIST_DIR="$dist_dir" ./scripts/build-release.sh
) > "$tmp_dir/build.log" 2>&1; then
  build_success=1
  pass "build-release.sh executed successfully"
else
  build_exit_code=$?
  fail "build-release.sh failed (exit code $build_exit_code)"
  echo "--- Build Log ---"
  cat "$tmp_dir/build.log"
  echo "-----------------"
fi

# -----------------------------------------------------------------------------
# Verification: Build Invocation Details
# -----------------------------------------------------------------------------
section "Build Invocation Details"

if [[ $build_success -ne 1 ]]; then
  warn "Skipping build invocation verification because build-release.sh failed"
elif [[ ! -s "$go_log" ]]; then
  fail "No go build invocations recorded by the mock"
else
  invocation_count=$(wc -l < "$go_log" | tr -d ' ')
  if [[ "$invocation_count" -eq 5 ]]; then
    pass "Expected number of go build invocations (5)"
  else
    fail "Unexpected go build invocation count: $invocation_count"
  fi

  seen_darwin_arm64=0
  seen_darwin_amd64=0
  seen_linux_arm64=0
  seen_linux_amd64=0
  seen_windows_amd64=0

  while IFS='|' read -r goos goarch cgo output ldflags pkg; do
    if [[ -z "$goos" || -z "$goarch" ]]; then
      fail "GOOS/GOARCH not set for output: $output"
    fi

    if [[ "$cgo" != "0" ]]; then
      fail "CGO_ENABLED is not 0 for $goos/$goarch ($output)"
    fi

    if [[ "$pkg" != "./cmd/al" ]]; then
      fail "go build package mismatch: $pkg"
    fi

    if [[ "$ldflags" != *"-X main.Version=$expected_version"* ]]; then
      fail "Missing version ldflags for $goos/$goarch ($output)"
    fi

    if [[ "$ldflags" != *"-s"* || "$ldflags" != *"-w"* ]]; then
      fail "Missing strip flags (-s -w) for $goos/$goarch ($output)"
    fi

    case "$goos/$goarch/$output" in
      "darwin/arm64/$dist_dir/al-darwin-arm64")
        seen_darwin_arm64=1
        ;;
      "darwin/amd64/$dist_dir/al-darwin-amd64")
        seen_darwin_amd64=1
        ;;
      "linux/arm64/$dist_dir/al-linux-arm64")
        seen_linux_arm64=1
        ;;
      "linux/amd64/$dist_dir/al-linux-amd64")
        seen_linux_amd64=1
        ;;
      "windows/amd64/$dist_dir/al-windows-amd64.exe")
        seen_windows_amd64=1
        ;;
      *)
        fail "Unexpected build target: GOOS=$goos GOARCH=$goarch output=$output"
        ;;
    esac
  done < "$go_log"

  if [[ "$seen_darwin_arm64" -eq 1 ]]; then
    pass "Build target present: darwin/arm64"
  else
    fail "Missing build target: darwin/arm64"
  fi

  if [[ "$seen_darwin_amd64" -eq 1 ]]; then
    pass "Build target present: darwin/amd64"
  else
    fail "Missing build target: darwin/amd64"
  fi

  if [[ "$seen_linux_arm64" -eq 1 ]]; then
    pass "Build target present: linux/arm64"
  else
    fail "Missing build target: linux/arm64"
  fi

  if [[ "$seen_linux_amd64" -eq 1 ]]; then
    pass "Build target present: linux/amd64"
  else
    fail "Missing build target: linux/amd64"
  fi

  if [[ "$seen_windows_amd64" -eq 1 ]]; then
    pass "Build target present: windows/amd64"
  else
    fail "Missing build target: windows/amd64"
  fi
fi

# -----------------------------------------------------------------------------
# Verification: Artifacts
# -----------------------------------------------------------------------------
section "Artifact Verification"

if [[ $build_success -ne 1 ]]; then
  warn "Skipping artifact verification because build-release.sh failed"
else
  # These match the targets defined in build-release.sh
  # We verify the OUTCOME, not the script text.
  expected_artifacts=(
    "al-darwin-arm64"
    "al-darwin-amd64"
    "al-linux-arm64"
    "al-linux-amd64"
    "al-windows-amd64.exe"
    "al-install.sh"
    "al-install.ps1"
    "checksums.txt"
  )

  for artifact in "${expected_artifacts[@]}"; do
    if [[ -f "$dist_dir/$artifact" ]]; then
      pass "Artifact created: $artifact"
    else
      fail "Artifact missing: $artifact"
    fi
  done

  if cmp -s "$ROOT_DIR/al-install.sh" "$dist_dir/al-install.sh"; then
    pass "al-install.sh copied without changes"
  else
    fail "al-install.sh copy does not match source"
  fi

  if cmp -s "$ROOT_DIR/al-install.ps1" "$dist_dir/al-install.ps1"; then
    pass "al-install.ps1 copied without changes"
  else
    fail "al-install.ps1 copy does not match source"
  fi

fi

# -----------------------------------------------------------------------------
# Verification: Checksum Integrity
# -----------------------------------------------------------------------------
section "Checksum Integrity"

if [[ $build_success -ne 1 ]]; then
  warn "Skipping checksum verification because build-release.sh failed"
elif [[ -f "$dist_dir/checksums.txt" ]]; then
  # 1. Verify format (simple regex for SHA256)
  if grep -qE '^[a-f0-9]{64}[[:space:]]+' "$dist_dir/checksums.txt"; then
    pass "checksums.txt format is valid"
  else
    fail "checksums.txt format is invalid"
  fi

  # 2. Verify checksums match the files using the appropriate tool
  # This tests that the script generated correct hashes for the files it created.
  # We run verification from within dist_dir so relative paths match.
  (
    cd "$dist_dir"
    if command -v sha256sum >/dev/null 2>&1; then
      if sha256sum -c checksums.txt --status 2>/dev/null || sha256sum -c checksums.txt >/dev/null 2>&1; then
        pass "Checksums verified successfully (using sha256sum)"
      else
        fail "Checksum verification failed (using sha256sum)"
      fi
    elif command -v shasum >/dev/null 2>&1; then
      if shasum -a 256 -c checksums.txt >/dev/null 2>&1; then
        pass "Checksums verified successfully (using shasum)"
      else
        fail "Checksum verification failed (using shasum)"
      fi
    else
      fail "Neither sha256sum nor shasum found; cannot verify checksum content."
    fi
  )

  # 3. Verify checksums.txt includes exactly the expected files (and nothing else)
  expected_checksum_files=(
    "al-darwin-arm64"
    "al-darwin-amd64"
    "al-linux-arm64"
    "al-linux-amd64"
    "al-windows-amd64.exe"
    "al-install.sh"
    "al-install.ps1"
  )

  expected_checksum_list="$tmp_dir/expected-checksums-files.txt"
  actual_checksum_list="$tmp_dir/actual-checksums-files.txt"
  checksum_diff="$tmp_dir/checksums-files.diff"

  printf '%s\n' "${expected_checksum_files[@]}" | sort > "$expected_checksum_list"
  awk '{print $2}' "$dist_dir/checksums.txt" | sed 's|^\./||' | grep -v '^checksums.txt$' | sort > "$actual_checksum_list"

  if diff -u "$expected_checksum_list" "$actual_checksum_list" > "$checksum_diff"; then
    pass "checksums.txt entries match expected artifacts"
  else
    fail "checksums.txt entries do not match expected artifacts"
    cat "$checksum_diff"
  fi

  # 4. Idempotency Regression Test
  # Ensure checksums.txt doesn't contain a hash of itself (which happens if not deleted before glob expansion)
  if awk '{print $2}' "$dist_dir/checksums.txt" | sed 's|^\./||' | grep -qx "checksums.txt"; then
    fail "checksums.txt contains a hash of itself (regression)"
  else
    pass "checksums.txt does not include itself"
  fi

else
  fail "Skipping checksum verification (checksums.txt missing)"
fi

# -----------------------------------------------------------------------------
# Summary
# -----------------------------------------------------------------------------
section "Summary for Release Testing"

total=$((pass_count + fail_count))
printf 'Tests: %s total, %b%s passed%b, %b%s failed%b\n' "$total" "$GREEN" "$pass_count" "$NC" "$RED" "$fail_count" "$NC"

if [[ $fail_count -gt 0 ]]; then
  exit 1
fi
