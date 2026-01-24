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
      # Test 1: Successfully update a valid formula
      valid_formula="$tmp_dir/valid-formula.rb"
      cat > "$valid_formula" << 'EOF'
class AgentLayer < Formula
  desc "Agent Layer CLI"
  homepage "https://github.com/conn-castle/agent-layer"
  url "https://example.com/old-url.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
  license "MIT"

  def install
    bin.install "al"
  end
end
EOF

      new_url="https://example.com/new-url.tar.gz"
      new_sha="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

      if run_update_formula "$valid_formula" "$new_url" "$new_sha" 2>/dev/null; then
        # Verify the update
        if grep -q "url \"$new_url\"" "$valid_formula" && grep -q "sha256 \"$new_sha\"" "$valid_formula"; then
          pass "updateformula: successfully updates url and sha256"
        else
          fail "updateformula: file not updated correctly"
        fi
      else
        fail "updateformula: failed on valid formula"
      fi

      # Test 2: Verify formatting is preserved (indentation)
      if grep -q '^  url "' "$valid_formula" && grep -q '^  sha256 "' "$valid_formula"; then
        pass "updateformula: preserves indentation"
      else
        fail "updateformula: did not preserve indentation"
      fi

      # Test 3: Verify surrounding content is preserved
      if grep -q 'class AgentLayer < Formula' "$valid_formula" && \
         grep -q 'def install' "$valid_formula" && \
         grep -q 'license "MIT"' "$valid_formula"; then
        pass "updateformula: preserves surrounding content"
      else
        fail "updateformula: did not preserve surrounding content"
      fi

      # Test 4: Exit code 1 when multiple url lines exist
      multi_url_formula="$tmp_dir/multi-url-formula.rb"
      cat > "$multi_url_formula" << 'EOF'
class Test < Formula
  url "https://example.com/one.tar.gz"
  url "https://example.com/two.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
end
EOF

      if run_update_formula "$multi_url_formula" "$new_url" "$new_sha" >/dev/null 2>&1; then
        fail "updateformula: should exit 1 with multiple url lines"
      else
        pass "updateformula: exits 1 with multiple url lines"
      fi

      # Test 5: Exit code 1 when multiple sha256 lines exist
      multi_sha_formula="$tmp_dir/multi-sha-formula.rb"
      cat > "$multi_sha_formula" << 'EOF'
class Test < Formula
  url "https://example.com/test.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
  sha256 "1111111111111111111111111111111111111111111111111111111111111111"
end
EOF

      if run_update_formula "$multi_sha_formula" "$new_url" "$new_sha" >/dev/null 2>&1; then
        fail "updateformula: should exit 1 with multiple sha256 lines"
      else
        pass "updateformula: exits 1 with multiple sha256 lines"
      fi

      # Test 6: Exit code 1 when formula file doesn't exist
      if run_update_formula "$tmp_dir/no-such-formula.rb" "$new_url" "$new_sha" >/dev/null 2>&1; then
        fail "updateformula: should exit 1 when formula file missing"
      else
        pass "updateformula: exits 1 when formula file missing"
      fi

      # Test 7: Exit code 1 when wrong number of arguments
      if run_update_formula "$valid_formula" "$new_url" >/dev/null 2>&1; then
        fail "updateformula: should exit 1 with wrong argument count"
      else
        pass "updateformula: exits 1 with wrong argument count"
      fi

      # Test 8: Exit code 1 when no url line exists
      no_url_formula="$tmp_dir/no-url-formula.rb"
      cat > "$no_url_formula" << 'EOF'
class Test < Formula
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
end
EOF

      if run_update_formula "$no_url_formula" "$new_url" "$new_sha" >/dev/null 2>&1; then
        fail "updateformula: should exit 1 with no url line"
      else
        pass "updateformula: exits 1 with no url line"
      fi

      # Test 9: Exit code 1 when no sha256 line exists
      no_sha_formula="$tmp_dir/no-sha-formula.rb"
      cat > "$no_sha_formula" << 'EOF'
class Test < Formula
  url "https://example.com/test.tar.gz"
end
EOF

      if run_update_formula "$no_sha_formula" "$new_url" "$new_sha" >/dev/null 2>&1; then
        fail "updateformula: should exit 1 with no sha256 line"
      else
        pass "updateformula: exits 1 with no sha256 line"
      fi
    fi
  fi
}
