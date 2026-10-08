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
if [[ -n "${MOCK_PREFLIGHT_EVENT_LOG:-}" ]]; then
  printf 'build|%s|%s|%s\n' "${GOOS:-}" "${GOARCH:-}" "$output" >> "$MOCK_PREFLIGHT_EVENT_LOG"
fi

if [[ "${MOCK_GO_FAIL_ON:-}" == "${GOOS:-}/${GOARCH:-}" ]]; then
  echo "Error: Mock go configured to fail for ${GOOS:-}/${GOARCH:-}" >&2
  exit 1
fi

mkdir -p "$(dirname "$output")"
build_version="${ldflags##*=}"
cat > "$output" <<MOCK_BINARY
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "\$*" >> "\${MOCK_RELEASE_BINARY_LOG:-/dev/null}"
case "\${1:-}" in
  --version)
    printf '%s\n' "\${MOCK_RELEASE_VERSION_OVERRIDE:-$build_version}"
    ;;
  init)
    mkdir -p .agent-layer
    printf '%s\n' "\${MOCK_RELEASE_PIN_OVERRIDE:-${build_version#v}}" > .agent-layer/al.version
    ;;
  upgrade)
    [[ "\${2:-}" == "plan" ]]
    ;;
  *)
    exit 1
    ;;
esac
MOCK_BINARY
chmod +x "$output"
MOCK_GO
  chmod +x "$mock_bin/go"

  # Run build-release.sh with mocked go
  build_success=0
  echo "Running build-release.sh in test environment..."

  if (
    export PATH="$mock_bin:$PATH"
    export MOCK_GO_LOG="$go_log"
    export MOCK_RELEASE_BINARY_LOG="$tmp_dir/release-binary.log"
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

run_release_smoke_rejection_tests() {
  section "Release Artifact Smoke Rejection Tests"

  local wrong_version_dist="$tmp_dir/wrong-version-dist"
  local wrong_version_log="$tmp_dir/wrong-version.log"
  if (
    export PATH="$mock_bin:$PATH"
    export MOCK_GO_LOG="$tmp_dir/wrong-version-go.log"
    export MOCK_RELEASE_VERSION_OVERRIDE="v0.0.0"
    cd "$ROOT_DIR"
    AL_VERSION="$expected_version" DIST_DIR="$wrong_version_dist" ./scripts/build-release.sh
  ) > "$wrong_version_log" 2>&1; then
    fail "release build should reject an artifact reporting the wrong version"
  elif grep -Fq "release binary version is v0.0.0; expected $expected_version" "$wrong_version_log"; then
    pass "release build rejects an artifact reporting the wrong version"
  else
    fail "wrong-version artifact failure was not actionable"
    cat "$wrong_version_log"
  fi

  local wrong_pin_dist="$tmp_dir/wrong-pin-dist"
  local wrong_pin_log="$tmp_dir/wrong-pin.log"
  if (
    export PATH="$mock_bin:$PATH"
    export MOCK_GO_LOG="$tmp_dir/wrong-pin-go.log"
    export MOCK_RELEASE_PIN_OVERRIDE="0.0.0"
    cd "$ROOT_DIR"
    AL_VERSION="$expected_version" DIST_DIR="$wrong_pin_dist" ./scripts/build-release.sh
  ) > "$wrong_pin_log" 2>&1; then
    fail "release build should reject an artifact initializing the wrong pin"
  elif grep -Fq "release binary initialized pin 0.0.0; expected $expected_version_no_v" "$wrong_pin_log"; then
    pass "release build rejects an artifact initializing the wrong pin"
  else
    fail "wrong-pin artifact failure was not actionable"
    cat "$wrong_pin_log"
  fi
}

run_missing_migration_manifest_test() {
  section "Missing Migration Manifest Test"

  local missing_dist="$tmp_dir/missing-migration-dist"
  local missing_go_log="$tmp_dir/missing-migration-go.log"
  local missing_log="$tmp_dir/missing-migration.log"
  : > "$missing_go_log"

  if (
    export PATH="$mock_bin:$PATH"
    export MOCK_GO_LOG="$missing_go_log"
    export MOCK_RELEASE_BINARY_LOG="$tmp_dir/missing-release-binary.log"
    cd "$ROOT_DIR"
    AL_VERSION="v9.9.9" DIST_DIR="$missing_dist" ./scripts/build-release.sh
  ) > "$missing_log" 2>&1; then
    fail "stable release build should fail when its migration manifest is missing"
  elif [[ -s "$missing_go_log" ]]; then
    fail "stable release build compiled binaries before checking its migration manifest"
  elif grep -Fq "stable release v9.9.9 is missing migration manifest internal/templates/migrations/9.9.9.json" "$missing_log"; then
    pass "stable release build fails before compilation when its migration manifest is missing"
  else
    fail "missing migration manifest failure was not actionable"
    cat "$missing_log"
  fi
}

run_codesign_requirement_test() {
  section "Codesign Requirement Test"

  if [[ ! -d "${mock_bin:-}" ]]; then
    warn "Skipping codesign requirement test because mock go was not initialized"
    return
  fi

  require_dist="$tmp_dir/require-codesign-dist"
  require_go_log="$tmp_dir/require-codesign-go.log"
  require_log="$tmp_dir/require-codesign.log"

  if (
    export PATH="$mock_bin:$PATH"
    export MOCK_GO_LOG="$require_go_log"
    cd "$ROOT_DIR"
    AL_VERSION="$expected_version" DIST_DIR="$require_dist" AL_REQUIRE_CODESIGN=1 ./scripts/build-release.sh
  ) > "$require_log" 2>&1; then
    fail "AL_REQUIRE_CODESIGN=1 should fail when AL_CODESIGN_IDENTITY is unset"
    echo "--- Build Log ---"
    head -n 10 "$require_log"
    echo "-----------------"
  else
    pass "AL_REQUIRE_CODESIGN=1 fails when AL_CODESIGN_IDENTITY is unset"
  fi

  if grep -q "AL_CODESIGN_IDENTITY is required" "$require_log"; then
    pass "codesign requirement failure explains missing AL_CODESIGN_IDENTITY"
  else
    fail "codesign requirement failure did not explain missing AL_CODESIGN_IDENTITY"
    cat "$require_log"
  fi
}

run_release_vulnerability_gate_test() {
  section "Release Vulnerability Gate Test"

  local vuln_dist="$tmp_dir/vuln-dist"
  local vuln_tools="$tmp_dir/vuln-tools"
  local vuln_log="$tmp_dir/vuln-scans.log"
  local expected=(al-darwin-arm64 al-darwin-amd64 al-linux-arm64 al-linux-amd64)
  mkdir -p "$vuln_dist" "$vuln_tools"
  for binary in "${expected[@]}"; do
    touch "$vuln_dist/$binary"
  done

  cat > "$vuln_tools/govulncheck" << 'MOCK_GOVULNCHECK'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${FAKE_GOVULNCHECK_LOG:?}"
binary="${*: -1}"
if [[ -n "${FAKE_GOVULNCHECK_FAIL_ON:-}" && "$(basename "$binary")" == "$FAKE_GOVULNCHECK_FAIL_ON" ]]; then
  exit 1
fi
MOCK_GOVULNCHECK
  chmod +x "$vuln_tools/govulncheck"

  if FAKE_GOVULNCHECK_LOG="$vuln_log" make --no-print-directory -C "$ROOT_DIR" \
      release-vuln-check TOOL_BIN="$vuln_tools" DIST_DIR="$vuln_dist"; then
    pass "release-vuln-check succeeds when all four binary scans succeed"
  else
    fail "release-vuln-check failed with a complete clean binary set"
  fi

  local scan_count
  scan_count=$(wc -l < "$vuln_log" | tr -d ' ')
  if [[ "$scan_count" -eq 4 ]]; then
    pass "release-vuln-check invokes exactly four scans"
  else
    fail "release-vuln-check invoked $scan_count scans, expected 4"
  fi
  for binary in "${expected[@]}"; do
    if grep -Fxq -- "-mode=binary $vuln_dist/$binary" "$vuln_log"; then
      pass "release-vuln-check scans $binary in binary mode"
    else
      fail "release-vuln-check did not scan $binary in binary mode"
    fi
  done

  rm "$vuln_dist/al-linux-amd64"
  : > "$vuln_log"
  if FAKE_GOVULNCHECK_LOG="$vuln_log" make --no-print-directory -C "$ROOT_DIR" \
      release-vuln-check TOOL_BIN="$vuln_tools" DIST_DIR="$vuln_dist" > "$tmp_dir/vuln-missing.log" 2>&1; then
    fail "release-vuln-check should fail when a release binary is missing"
  elif [[ -s "$vuln_log" ]]; then
    fail "release-vuln-check scanned an incomplete binary set"
  else
    pass "release-vuln-check fails before scanning an incomplete binary set"
  fi

  touch "$vuln_dist/al-linux-amd64"
  : > "$vuln_log"
  if FAKE_GOVULNCHECK_LOG="$vuln_log" FAKE_GOVULNCHECK_FAIL_ON=al-darwin-amd64 \
      make --no-print-directory -C "$ROOT_DIR" release-vuln-check TOOL_BIN="$vuln_tools" DIST_DIR="$vuln_dist" \
      > "$tmp_dir/vuln-failure.log" 2>&1; then
    fail "release-vuln-check should propagate scanner failures"
  else
    pass "release-vuln-check propagates scanner failures"
  fi
}

run_release_preflight_test() {
  section "Release Preflight Test"

  local preflight_tools="$tmp_dir/preflight-tools"
  local preflight_make="$tmp_dir/preflight-make"
  local preflight_ci_log="$tmp_dir/preflight-ci.log"
  local preflight_go_log="$tmp_dir/preflight-go.log"
  local preflight_scan_log="$tmp_dir/preflight-scan.log"
  local preflight_event_log="$tmp_dir/preflight-events.log"
  local preflight_sign_log="$tmp_dir/preflight-sign.log"
  local real_make
  real_make="$(command -v make)"
  mkdir -p "$preflight_tools"

  cat > "$preflight_tools/govulncheck" << 'MOCK_GOVULNCHECK'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${FAKE_GOVULNCHECK_LOG:?}"
printf 'scan|%s\n' "${*: -1}" >> "${MOCK_PREFLIGHT_EVENT_LOG:?}"
if [[ -n "${FAKE_GOVULNCHECK_FAIL_ON:-}" && "$(basename "${*: -1}")" == "$FAKE_GOVULNCHECK_FAIL_ON" ]]; then
  exit 1
fi
MOCK_GOVULNCHECK
  chmod +x "$preflight_tools/govulncheck"

  cat > "$preflight_make" << 'PREFLIGHT_MAKE'
#!/usr/bin/env bash
set -euo pipefail
for argument in "$@"; do
  if [[ "$argument" == "ci" ]]; then
    printf 'ci\n' >> "${PREFLIGHT_CI_LOG:?}"
    printf 'ci\n' >> "${MOCK_PREFLIGHT_EVENT_LOG:?}"
    exit 0
  fi
done
exec "${REAL_MAKE:?}" "$@"
PREFLIGHT_MAKE
  chmod +x "$preflight_make"

  for tool in codesign xcrun; do
    cat > "$preflight_tools/$tool" << 'MOCK_SIGNING_TOOL'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$(basename "$0")" >> "${MOCK_PREFLIGHT_SIGN_LOG:?}"
exit 99
MOCK_SIGNING_TOOL
    chmod +x "$preflight_tools/$tool"
  done

  run_preflight() {
    local log_path="$1"
    local artifact_root="$2"
    local tag="$3"
    local go_fail_on="$4"
    local scanner_fail_on="$5"
    local require_codesign="$6"
    local codesign_identity="$7"
    shift 7

    env -u MAKEFLAGS -u MFLAGS -u MAKELEVEL -u MAKEOVERRIDES -u RELEASE_TAG \
      -u AL_VERSION -u DIST_DIR -u AL_CODESIGN_IDENTITY -u AL_REQUIRE_CODESIGN \
      REAL_MAKE="$real_make" \
      PATH="$mock_bin:$preflight_tools:$PATH" \
      PREFLIGHT_CI_LOG="$preflight_ci_log" \
      MOCK_GO_LOG="$preflight_go_log" \
      MOCK_RELEASE_BINARY_LOG="$tmp_dir/preflight-release-binary.log" \
      MOCK_PREFLIGHT_EVENT_LOG="$preflight_event_log" \
      MOCK_PREFLIGHT_SIGN_LOG="$preflight_sign_log" \
      FAKE_GOVULNCHECK_LOG="$preflight_scan_log" \
      MOCK_GO_FAIL_ON="$go_fail_on" \
      FAKE_GOVULNCHECK_FAIL_ON="$scanner_fail_on" \
      AL_REQUIRE_CODESIGN="$require_codesign" \
      AL_CODESIGN_IDENTITY="$codesign_identity" \
      "$real_make" --no-print-directory -C "$ROOT_DIR" \
      MAKE="$preflight_make" TOOL_BIN="$preflight_tools" \
      RELEASE_PREFLIGHT_ARTIFACT_ROOT="$artifact_root" \
      release-preflight RELEASE_TAG="$tag" "$@" > "$log_path" 2>&1
  }

  reset_preflight_logs() {
    : > "$preflight_ci_log"
    : > "$preflight_go_log"
    : > "$preflight_scan_log"
    : > "$preflight_event_log"
    : > "$preflight_sign_log"
  }

  local success_root="$tmp_dir/preflight-success"
  local success_log="$tmp_dir/preflight-success.log"
  reset_preflight_logs
  if run_preflight "$success_log" "$success_root" "$expected_version" "" "" "1" "test-identity"; then
    pass "release-preflight completes with caller signing credentials forced unsigned"
  else
    fail "release-preflight failed on the controlled success path"
    cat "$success_log"
  fi

  local -a artifact_dirs=()
  local found_dir
  while IFS= read -r found_dir; do
    artifact_dirs+=("$found_dir")
  done < <(find "$success_root" -mindepth 1 -maxdepth 1 -type d -print 2>/dev/null | sort)
  if [[ "${#artifact_dirs[@]}" -eq 1 ]]; then
    local artifact_dir="${artifact_dirs[0]}"
    pass "release-preflight retains one unique artifact directory"
    local expected_binary
    for expected_binary in al-darwin-arm64 al-darwin-amd64 al-linux-arm64 al-linux-amd64; do
      if [[ -f "$artifact_dir/$expected_binary" ]] &&
          grep -Fxq -- "-mode=binary $artifact_dir/$expected_binary" "$preflight_scan_log"; then
        pass "release-preflight builds and scans $expected_binary"
      else
        fail "release-preflight did not build and scan $expected_binary"
      fi
    done
    if [[ "$(wc -l < "$preflight_go_log" | tr -d ' ')" -eq 4 ]] &&
        [[ "$(wc -l < "$preflight_scan_log" | tr -d ' ')" -eq 4 ]] &&
        [[ "$(grep -Fc -- "-X main.Version=$expected_version" "$preflight_go_log")" -eq 4 ]] &&
        [[ "$(grep -Fc -- "-s -w" "$preflight_go_log")" -eq 4 ]] &&
        grep -Fq -- "darwin|arm64|0|$artifact_dir/al-darwin-arm64" "$preflight_go_log" &&
        grep -Fq -- "darwin|amd64|0|$artifact_dir/al-darwin-amd64" "$preflight_go_log" &&
        grep -Fq -- "linux|arm64|0|$artifact_dir/al-linux-arm64" "$preflight_go_log" &&
        grep -Fq -- "linux|amd64|0|$artifact_dir/al-linux-amd64" "$preflight_go_log"; then
      pass "release-preflight wires the release version and four existing build targets"
    else
      fail "release-preflight build invocation wiring was incomplete"
    fi
    local last_build_event first_scan_event
    last_build_event="$(grep -n '^build|' "$preflight_event_log" | tail -n 1 | cut -d: -f1)"
    first_scan_event="$(grep -n '^scan|' "$preflight_event_log" | head -n 1 | cut -d: -f1)"
    if [[ "$(wc -l < "$preflight_ci_log" | tr -d ' ')" -eq 1 ]] &&
        [[ "$(head -n 1 "$preflight_event_log")" == "ci" ]] &&
        [[ "$first_scan_event" -gt "$last_build_event" ]] &&
        [[ ! -s "$preflight_sign_log" ]]; then
      pass "release-preflight runs CI once before building, scans afterward, and does not sign"
    else
      fail "release-preflight CI order or unsigned behavior was incorrect"
    fi
  else
    fail "release-preflight did not retain exactly one artifact directory"
  fi

  local build_failure_root="$tmp_dir/preflight-build-failure"
  local build_failure_log="$tmp_dir/preflight-build-failure.log"
  reset_preflight_logs
  if run_preflight "$build_failure_log" "$build_failure_root" "$expected_version" "linux/arm64" "" "0" ""; then
    fail "release-preflight should propagate builder failure"
  elif [[ ! -s "$preflight_scan_log" ]] &&
      grep -Fq "Mock go configured to fail for linux/arm64" "$build_failure_log"; then
    pass "release-preflight stops before scanning when the builder fails"
  else
    fail "release-preflight scanned after a builder failure"
  fi

  local scan_failure_root="$tmp_dir/preflight-scan-failure"
  local scan_failure_log="$tmp_dir/preflight-scan-failure.log"
  reset_preflight_logs
  if run_preflight "$scan_failure_log" "$scan_failure_root" "$expected_version" "" "al-darwin-amd64" "0" ""; then
    fail "release-preflight should propagate scanner failure"
  elif [[ "$(wc -l < "$preflight_go_log" | tr -d ' ')" -eq 4 ]] &&
      grep -Fq "/al-darwin-amd64" "$preflight_scan_log"; then
    pass "release-preflight propagates scanner failure"
  else
    fail "release-preflight failed before exercising the scanner failure"
  fi

  local tag_case tag_log tag_root
  for tag_case in "" "0.24.0" "v9.9.9"; do
    tag_log="$tmp_dir/preflight-invalid-tag-${tag_case:-missing}.log"
    tag_root="$tmp_dir/preflight-invalid-tag-${tag_case:-missing}"
    reset_preflight_logs
    if run_preflight "$tag_log" "$tag_root" "$tag_case" "" "" "0" ""; then
      fail "release-preflight should reject ${tag_case:-a missing release tag}"
    elif [[ ! -s "$preflight_ci_log" && ! -s "$preflight_go_log" && ! -s "$preflight_scan_log" ]] &&
        grep -Eq "RELEASE_TAG is required|invalid tag format|missing migration-table row" "$tag_log"; then
      pass "release-preflight rejects ${tag_case:-a missing release tag} before costly work"
    else
      fail "release-preflight performed work before rejecting ${tag_case:-a missing release tag}"
    fi
  done

  reset_preflight_logs
  if run_preflight "$tmp_dir/preflight-missing-scanner.log" "$tmp_dir/preflight-missing-scanner" "$expected_version" "" "" "0" "" TOOL_BIN="$tmp_dir/no-scanner"; then
    fail "release-preflight should reject a missing scanner"
  elif [[ ! -s "$preflight_ci_log" && ! -s "$preflight_go_log" && ! -s "$preflight_scan_log" ]] &&
      grep -Fq "Run: make release-tools" "$tmp_dir/preflight-missing-scanner.log"; then
    pass "release-preflight rejects a missing scanner before costly work"
  else
    fail "release-preflight did not diagnose a missing scanner before costly work"
  fi

  reset_preflight_logs
  if run_preflight "$tmp_dir/preflight-dry-run.log" "$tmp_dir/preflight-dry-run" "$expected_version" "" "" "0" "" -n &&
      [[ ! -s "$preflight_go_log" && ! -s "$preflight_scan_log" && ! -d "$tmp_dir/preflight-dry-run" ]]; then
    pass "release-preflight dry run does not build or scan artifacts"
  else
    fail "release-preflight dry run performed artifact work"
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

    if grep -Fxq -- "--version" "$tmp_dir/release-binary.log" &&
       grep -Fxq -- "init --here --no-wizard" "$tmp_dir/release-binary.log" &&
       grep -Fxq -- "upgrade plan" "$tmp_dir/release-binary.log"; then
      pass "Stable release build smoke-tests the native artifact's version and upgrade path"
    else
      fail "Stable release build did not smoke-test the native artifact's version and upgrade path"
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
