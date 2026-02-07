# Helper functions for scripts/test-release.sh.

run_release_generation_test() {
  section "Release Generation Test"

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
}

run_build_invocation_details() {
  section "Build Invocation Details"

  if [[ $build_success -ne 1 ]]; then
    warn "Skipping build invocation verification because build-release.sh failed"
  elif [[ ! -s "$go_log" ]]; then
    fail "No go build invocations recorded by the mock"
  else
    invocation_count=$(wc -l < "$go_log" | tr -d ' ')
    if [[ "$invocation_count" -eq 4 ]]; then
      pass "Expected number of go build invocations (4)"
    else
      fail "Unexpected go build invocation count: $invocation_count"
    fi

    seen_darwin_arm64=0
    seen_darwin_amd64=0
    seen_linux_arm64=0
    seen_linux_amd64=0

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

  fi
}

run_artifact_verification() {
  section "Artifact Verification"

  if [[ $build_success -ne 1 ]]; then
    warn "Skipping artifact verification because build-release.sh failed"
  else
    source_tarball="agent-layer-${expected_version_no_v}.tar.gz"
    # These match the targets defined in build-release.sh
    # We verify the OUTCOME, not the script text.
    expected_artifacts=(
      "al-darwin-arm64"
      "al-darwin-amd64"
      "al-linux-arm64"
      "al-linux-amd64"
      "al-install.sh"
      "$source_tarball"
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

  fi
}

run_source_tarball_verification() {
  section "Source Tarball Verification"

  if [[ $build_success -ne 1 ]]; then
    warn "Skipping source tarball verification because build-release.sh failed"
  else
    source_tarball="agent-layer-${expected_version_no_v}.tar.gz"
    tar_list="$tmp_dir/source-tarball-contents.txt"
    prefix="agent-layer-${expected_version_no_v}/"

    if tar -tzf "$dist_dir/$source_tarball" > "$tar_list" 2>/dev/null; then
      pass "Source tarball is readable: $source_tarball"
    else
      fail "Source tarball could not be read: $source_tarball"
    fi

    if awk -v prefix="$prefix" 'index($0, prefix) != 1 { exit 1 }' "$tar_list"; then
      pass "Source tarball entries are prefixed with $prefix"
    else
      fail "Source tarball entries missing expected prefix $prefix"
    fi

    if grep -qx "${prefix}README.md" "$tar_list"; then
      pass "Source tarball includes README.md"
    else
      fail "Source tarball missing README.md"
    fi
  fi
}

run_checksum_integrity() {
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
      "al-install.sh"
      "agent-layer-${expected_version_no_v}.tar.gz"
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
}
