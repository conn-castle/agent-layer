# Helper functions for Go tool tests in scripts/test-release.sh.

run_go_tool_tests_extractchecksum() {
  section "Go Tool Tests: extractchecksum"

  extract_tool="./internal/tools/extractchecksum"
  extract_bin="$tmp_dir/extractchecksum"
  extract_ok=1

  if [[ ! -f "$ROOT_DIR/$extract_tool/main.go" ]]; then
    fail "extractchecksum tool not found"
  else
    # These test-only binaries do not consume VCS metadata. Disabling stamping
    # also keeps the lane valid in linked worktrees nested below another Git root.
    if (cd "$ROOT_DIR" && go build -buildvcs=false -tags tools -o "$extract_bin" "$extract_tool"); then
      pass "extractchecksum tool built"
    else
      fail "extractchecksum tool build failed"
      extract_ok=0
    fi

    run_extract_checksum() {
      "$extract_bin" "$@"
    }

    if [[ "$extract_ok" -ne 1 ]]; then
      warn "Skipping extractchecksum tests because build failed"
    else
      # Create test checksums file
      test_checksums="$tmp_dir/test-checksums.txt"
      cat > "$test_checksums" << 'EOF'
abc123def456abc123def456abc123def456abc123def456abc123def456abc12345  file1.tar.gz
sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba987654321  file2.tar.gz
1111111111111111111111111111111111111111111111111111111111111111  ./path/to/file3.bin
2222222222222222222222222222222222222222222222222222222222222222  *./path/with spaces/file 4.bin
3333333333333333333333333333333333333333333333333333333333333333  .hidden-file.bin
EOF

      # Test 1: Extract checksum for existing file (standard format)
      result=$(run_extract_checksum "$test_checksums" "file1.tar.gz" 2>/dev/null) || true
      if [[ "$result" == "abc123def456abc123def456abc123def456abc123def456abc123def456abc12345" ]]; then
        pass "extractchecksum: extracts standard format checksum"
      else
        fail "extractchecksum: failed to extract standard format checksum (got: $result)"
      fi

      # Test 2: Extract checksum for file with sha256: prefix
      result=$(run_extract_checksum "$test_checksums" "file2.tar.gz" 2>/dev/null) || true
      if [[ "$result" == "fedcba9876543210fedcba9876543210fedcba9876543210fedcba987654321" ]]; then
        pass "extractchecksum: strips sha256: prefix"
      else
        fail "extractchecksum: failed to strip sha256: prefix (got: $result)"
      fi

      # Test 3: Extract checksum for file with ./ prefix in checksums
      result=$(run_extract_checksum "$test_checksums" "path/to/file3.bin" 2>/dev/null) || true
      if [[ "$result" == "1111111111111111111111111111111111111111111111111111111111111111" ]]; then
        pass "extractchecksum: handles ./ prefix in checksums file"
      else
        fail "extractchecksum: failed to handle ./ prefix (got: $result)"
      fi

      # Test 4: Extract checksum for filename with spaces
      result=$(run_extract_checksum "$test_checksums" "path/with spaces/file 4.bin" 2>/dev/null) || true
      if [[ "$result" == "2222222222222222222222222222222222222222222222222222222222222222" ]]; then
        pass "extractchecksum: handles filenames with spaces"
      else
        fail "extractchecksum: failed to handle filenames with spaces (got: $result)"
      fi

      # Test 5: Exit code 1 when file not found in checksums
      if run_extract_checksum "$test_checksums" "nonexistent.tar.gz" >/dev/null 2>&1; then
        fail "extractchecksum: should exit 1 when file not found"
      else
        pass "extractchecksum: exits 1 when file not found"
      fi

      # Test 6: Exit code 1 when checksums file doesn't exist
      if run_extract_checksum "$tmp_dir/no-such-file.txt" "file1.tar.gz" >/dev/null 2>&1; then
        fail "extractchecksum: should exit 1 when checksums file missing"
      else
        pass "extractchecksum: exits 1 when checksums file missing"
      fi

      # Test 7: Exit code 1 when wrong number of arguments
      if run_extract_checksum "$test_checksums" >/dev/null 2>&1; then
        fail "extractchecksum: should exit 1 with wrong argument count"
      else
        pass "extractchecksum: exits 1 with wrong argument count"
      fi

      # Test 8: Dotfile leading "." must be preserved, not stripped as a cutset.
      # A "./" prefix is a path marker (stripped); a leading dot in the basename
      # is part of the name and must survive (regression guard for TrimLeft cutset bug).
      result=$(run_extract_checksum "$test_checksums" ".hidden-file.bin" 2>/dev/null) || true
      if [[ "$result" == "3333333333333333333333333333333333333333333333333333333333333333" ]]; then
        pass "extractchecksum: preserves leading dot in dotfile names"
      else
        fail "extractchecksum: mangled dotfile name (got: $result)"
      fi
    fi
  fi
}

run_go_tool_tests_updateformula() {
  section "Go Tool Tests: updateformula"

  update_tool="./internal/tools/updateformula"
  update_bin="$tmp_dir/updateformula"
  update_ok=1

  if [[ ! -f "$ROOT_DIR/$update_tool/main.go" ]]; then
    fail "updateformula tool not found"
  else
    if (cd "$ROOT_DIR" && go build -buildvcs=false -tags tools -o "$update_bin" "$update_tool"); then
      pass "updateformula tool built"
    else
      fail "updateformula tool build failed"
      update_ok=0
    fi

    run_update_formula() {
      "$update_bin" "$@"
    }

    if [[ "$update_ok" -ne 1 ]]; then
      warn "Skipping updateformula tests because build failed"
    else
      # Test 1: Successfully render the binary formula
      valid_formula="$tmp_dir/valid-formula.rb"
      cat > "$valid_formula" << 'EOF'
class AgentLayer < Formula
  url "https://example.com/old-url.tar.gz"
end
EOF

      formula_checksums="$tmp_dir/formula-checksums.txt"
      cat > "$formula_checksums" << 'EOF'
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  ./al-darwin-arm64
eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee  al-darwin-amd64
bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  al-linux-arm64
cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc  al-linux-amd64
dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd  agent-layer-1.2.3.tar.gz
EOF

      if run_update_formula "$valid_formula" "v1.2.3" "$formula_checksums" 2>/dev/null; then
        if grep -q 'url "https://github.com/conn-castle/agent-layer/releases/download/v1.2.3/al-darwin-arm64", using: :nounzip' "$valid_formula" && \
           grep -q 'sha256 "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"' "$valid_formula" && \
           grep -q 'al-darwin-amd64", using: :nounzip' "$valid_formula" && \
           grep -q 'sha256 "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"' "$valid_formula" && \
           grep -q 'al-linux-arm64", using: :nounzip' "$valid_formula" && \
           grep -q 'sha256 "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' "$valid_formula" && \
           grep -q 'al-linux-amd64", using: :nounzip' "$valid_formula" && \
           grep -q 'sha256 "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"' "$valid_formula"; then
          pass "updateformula: renders tagged binary asset urls and sha256 values"
        else
          fail "updateformula: binary formula content is missing expected asset urls or sha256 values"
        fi
      else
        fail "updateformula: failed on valid formula render"
      fi

      # Test 2: Verify the formula keeps required Homebrew shape.
      if grep -q 'on_macos do' "$valid_formula" && \
         grep -q 'on_arm do' "$valid_formula" && \
         grep -q 'on_intel do' "$valid_formula" && \
         grep -q 'chmod 0555, bin/"al"' "$valid_formula" && \
         grep -q 'generate_completions_from_executable(bin/"al", "completion")' "$valid_formula" && \
         grep -q 'test do' "$valid_formula"; then
        pass "updateformula: keeps required arch, executable mode, completion, and test blocks"
      else
        fail "updateformula: missing required arch, executable mode, completion, or test block"
      fi

      # Test 3: Verify removed source-build formula features stay removed.
      if ! grep -q '^  version ' "$valid_formula" && ! grep -q 'depends_on "go"' "$valid_formula" && ! grep -q 'bottle do' "$valid_formula" && ! grep -q 'agent-layer-1.2.3.tar.gz' "$valid_formula"; then
        pass "updateformula: drops redundant version and source-build-only content"
      else
        fail "updateformula: rendered formula still contains redundant version or source-build-only content"
      fi

      # Test 4: Exit code 1 when a required checksum is missing
      missing_checksum_file="$tmp_dir/missing-checksums.txt"
      cat > "$missing_checksum_file" << 'EOF'
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  al-darwin-arm64
bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  al-linux-arm64
EOF

      if run_update_formula "$valid_formula" "v1.2.3" "$missing_checksum_file" >/dev/null 2>&1; then
        fail "updateformula: should exit 1 when a required checksum is missing"
      else
        pass "updateformula: exits 1 when a required checksum is missing"
      fi

      # Test 5: Exit code 1 when formula file doesn't exist
      if run_update_formula "$tmp_dir/no-such-formula.rb" "v1.2.3" "$formula_checksums" >/dev/null 2>&1; then
        fail "updateformula: should exit 1 when formula file missing"
      else
        pass "updateformula: exits 1 when formula file missing"
      fi

      # Test 6: Exit code 1 when checksums file doesn't exist
      if run_update_formula "$valid_formula" "v1.2.3" "$tmp_dir/no-such-checksums.txt" >/dev/null 2>&1; then
        fail "updateformula: should exit 1 when checksums file missing"
      else
        pass "updateformula: exits 1 when checksums file missing"
      fi

      # Test 7: Exit code 1 when wrong number of arguments
      if run_update_formula "$valid_formula" "v1.2.3" >/dev/null 2>&1; then
        fail "updateformula: should exit 1 with wrong argument count"
      else
        pass "updateformula: exits 1 with wrong argument count"
      fi
    fi
  fi
}

run_go_tool_tests_updateformula_unit() {
  section "Go Tool Tests: updateformula (unit)"

  # The updateformula package (and its tests) are guarded by the `tools` build
  # tag, so `go test ./...` (used by make coverage) skips them. Run them
  # explicitly here so the formula renderer's unit tests actually execute in CI.
  if (cd "$ROOT_DIR" && go test -tags tools ./internal/tools/updateformula/); then
    pass "updateformula unit tests passed"
  else
    fail "updateformula unit tests failed"
  fi
}

run_go_tool_tests_gentemplatemanifest() {
  section "Go Tool Tests: gentemplatemanifest"

  # The gentemplatemanifest package (and its tests) are guarded by the `tools`
  # build tag, so `go test ./...` (used by make coverage) skips them. Run them
  # explicitly here so the manifest generator's managed/excluded partition
  # completeness check actually executes in CI.
  if (cd "$ROOT_DIR" && go test -tags tools ./internal/tools/gentemplatemanifest/); then
    pass "gentemplatemanifest tests passed"
  else
    fail "gentemplatemanifest tests failed"
  fi
}

run_go_tool_tests_checktestevents() {
  section "Go Tool Tests: checktestevents"

  # The checktestevents package (and its tests) are guarded by the `tools`
  # build tag, so `go test ./...` (used by make coverage) skips them. Run them
  # explicitly here so the truncated-package check actually executes in CI.
  if (cd "$ROOT_DIR" && go test -tags tools ./internal/tools/checktestevents/); then
    pass "checktestevents tests passed"
  else
    fail "checktestevents tests failed"
  fi

  # Exercise the real recipes with a successful gotestsum run whose package
  # never finishes.
  local mock_bin="$tmp_dir/checktestevents-mock-bin"
  mkdir -p "$mock_bin"
  cat > "$mock_bin/gotestsum" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
json_file=
while [[ $# -gt 0 ]]; do
  if [[ $1 == --jsonfile ]]; then
    json_file=$2
    break
  fi
  shift
done
printf '%s\n' '{"Action":"start","Package":"example/truncated"}' > "$json_file"
EOF
  cat > "$mock_bin/go" <<'EOF'
#!/usr/bin/env bash
for arg in "$@"; do
  if [[ $arg == ./internal/tools/coverreport ]]; then
    : > "$CHECKTESTEVENTS_COVERREPORT_MARKER"
    exit 70
  fi
done
exec "$CHECKTESTEVENTS_REAL_GO" "$@"
EOF
  chmod +x "$mock_bin/gotestsum" "$mock_bin/go"

  local target run_log_dir output coverreport_marker real_go
  real_go="$(command -v go)"
  for target in test coverage; do
    run_log_dir="$tmp_dir/checktestevents-$target-logs"
    output="$tmp_dir/checktestevents-$target-output.log"
    coverreport_marker="$tmp_dir/checktestevents-coverreport-called"
    rm -f "$coverreport_marker"
    if (cd "$ROOT_DIR" && PATH="$mock_bin:$PATH" \
        CHECKTESTEVENTS_REAL_GO="$real_go" \
        CHECKTESTEVENTS_COVERREPORT_MARKER="$coverreport_marker" \
        make "$target" TOOL_BIN="$mock_bin" TEST_LOG_DIR="$run_log_dir") > "$output" 2>&1; then
      fail "make $target accepts an unfinished package"
    elif ! grep -Fq 'example/truncated' "$output" || \
         ! grep -Fq 'example/truncated' "$run_log_dir"/*/output.log; then
      fail "make $target did not report the unfinished package in both outputs"
    elif [[ $target == coverage && -e $coverreport_marker ]]; then
      fail "make coverage ran coverreport after the checker failed"
    else
      pass "make $target rejects an unfinished package"
    fi
  done
}

run_make_test_recipe_status_tests() {
  section "Make test recipe exit status"

  # The recipes pipe gotestsum through tee. macOS ships GNU Make 3.81, which
  # ignores .SHELLFLAGS, so run the real recipes with whichever make is on PATH
  # and require gotestsum's exit status to survive the pipeline.
  local mock_bin="$tmp_dir/make-status-mock-bin"
  mkdir -p "$mock_bin"
  cat > "$mock_bin/gotestsum" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
json_file=
while [[ $# -gt 0 ]]; do
  if [[ $1 == --jsonfile ]]; then
    json_file=$2
    break
  fi
  shift
done
printf '%s\n' '{"Action":"start","Package":"example/pkg"}' '{"Action":"pass","Package":"example/pkg"}' > "$json_file"
echo "mock gotestsum exit $MAKE_STATUS_GOTESTSUM_EXIT"
exit "$MAKE_STATUS_GOTESTSUM_EXIT"
EOF
  cat > "$mock_bin/go" <<'EOF'
#!/usr/bin/env bash
for arg in "$@"; do
  if [[ $arg == ./internal/tools/coverreport ]]; then
    echo "mock coverreport"
    exit 0
  fi
done
exec "$MAKE_STATUS_REAL_GO" "$@"
EOF
  chmod +x "$mock_bin/gotestsum" "$mock_bin/go"

  local target gotestsum_exit run_log_dir output make_status real_go
  real_go="$(command -v go)"
  for target in test coverage; do
    for gotestsum_exit in 0 1; do
      run_log_dir="$tmp_dir/make-status-$target-$gotestsum_exit-logs"
      output="$tmp_dir/make-status-$target-$gotestsum_exit-output.log"
      make_status=0
      (cd "$ROOT_DIR" && PATH="$mock_bin:$PATH" \
        MAKE_STATUS_REAL_GO="$real_go" \
        MAKE_STATUS_GOTESTSUM_EXIT="$gotestsum_exit" \
        make "$target" TOOL_BIN="$mock_bin" TEST_LOG_DIR="$run_log_dir") > "$output" 2>&1 || make_status=$?
      if [[ $gotestsum_exit -eq 0 && $make_status -ne 0 ]]; then
        fail "make $target failed after gotestsum passed (exit $make_status)"
      elif [[ $gotestsum_exit -ne 0 && $make_status -eq 0 ]]; then
        fail "make $target passed after gotestsum failed"
      elif ! grep -Fq "mock gotestsum exit $gotestsum_exit" "$output" || \
           ! grep -Fq "mock gotestsum exit $gotestsum_exit" "$run_log_dir"/*/output.log || \
           ! grep -Fq 'Full test logs:' "$output"; then
        fail "make $target did not keep gotestsum output in both outputs"
      else
        pass "make $target exit status follows gotestsum exit $gotestsum_exit"
      fi
    done
  done
}
