# Helper functions for Go tool tests in scripts/test-release.sh.

run_go_tool_tests_extractchecksum() {
  section "Go Tool Tests: extractchecksum"

  extract_tool="./internal/tools/extractchecksum"
  extract_bin="$tmp_dir/extractchecksum"
  extract_ok=1

  if [[ ! -f "$ROOT_DIR/$extract_tool/main.go" ]]; then
    fail "extractchecksum tool not found"
  else
    if (cd "$ROOT_DIR" && go build -tags tools -o "$extract_bin" "$extract_tool"); then
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
    if (cd "$ROOT_DIR" && go build -tags tools -o "$update_bin" "$update_tool"); then
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
bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  al-linux-arm64
cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc  al-linux-amd64
dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd  agent-layer-1.2.3.tar.gz
EOF

      if run_update_formula "$valid_formula" "v1.2.3" "$formula_checksums" 2>/dev/null; then
        if grep -q 'version "1.2.3"' "$valid_formula" && \
           grep -q 'al-darwin-arm64", using: :nounzip' "$valid_formula" && \
           grep -q 'sha256 "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"' "$valid_formula" && \
           grep -q 'al-linux-arm64", using: :nounzip' "$valid_formula" && \
           grep -q 'sha256 "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' "$valid_formula" && \
           grep -q 'al-linux-amd64", using: :nounzip' "$valid_formula" && \
           grep -q 'sha256 "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"' "$valid_formula"; then
          pass "updateformula: renders binary asset urls and sha256 values"
        else
          fail "updateformula: binary formula content is missing expected asset urls or sha256 values"
        fi
      else
        fail "updateformula: failed on valid formula render"
      fi

      # Test 2: Verify the formula keeps required Homebrew shape.
      if grep -q 'depends_on arch: :arm64' "$valid_formula" && \
         grep -q 'generate_completions_from_executable(bin/"al", "completion")' "$valid_formula" && \
         grep -q 'test do' "$valid_formula"; then
        pass "updateformula: keeps required arch, completion, and test blocks"
      else
        fail "updateformula: missing required arch, completion, or test block"
      fi

      # Test 3: Verify removed source-build formula features stay removed.
      if ! grep -q 'depends_on "go"' "$valid_formula" && ! grep -q 'bottle do' "$valid_formula" && ! grep -q 'agent-layer-1.2.3.tar.gz' "$valid_formula"; then
        pass "updateformula: drops source-build dependency, bottle block, and tarball url"
      else
        fail "updateformula: rendered formula still contains source-build-only content"
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
