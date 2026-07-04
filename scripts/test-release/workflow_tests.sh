# Helper functions for CI/release workflow consistency tests in scripts/test-release.sh.

run_workflow_consistency_tests() {
  section "Workflow Consistency Tests"

  local ci_workflow="$ROOT_DIR/.github/workflows/ci.yml"
  local release_workflow="$ROOT_DIR/.github/workflows/release.yml"

  if [[ ! -f "$ci_workflow" ]]; then
    fail "ci.yml not found"
    return
  fi

  if [[ ! -f "$release_workflow" ]]; then
    fail "release.yml not found"
    return
  fi

  if grep -q 'runs-on: macos-latest' "$release_workflow"; then
    pass "workflow-consistency: build-release runs on macos-latest"
  else
    fail "workflow-consistency: build-release must run on macos-latest for Developer ID signing"
  fi

  if grep -q 'command -v rg' "$release_workflow" && grep -q 'brew install ripgrep' "$release_workflow"; then
    pass "workflow-consistency: release workflow installs ripgrep via Homebrew when needed"
  else
    fail "workflow-consistency: release workflow must install ripgrep via Homebrew when missing"
  fi

  # Stable tag validation must happen in build-release before the release
  # publish step, otherwise prerelease tags can publish artifacts before
  # failing downstream jobs.
  local stable_tag_check_line publish_release_line
  stable_tag_check_line=$(grep -n 'name: Validate stable release tag format' "$release_workflow" | head -n1 | cut -d: -f1 || true)
  publish_release_line=$(grep -n 'name: Publish release' "$release_workflow" | head -n1 | cut -d: -f1 || true)

  if [[ -z "$stable_tag_check_line" ]]; then
    fail "workflow-consistency: missing stable release tag validation step"
  elif [[ -z "$publish_release_line" ]]; then
    fail "workflow-consistency: missing publish release step"
  elif (( stable_tag_check_line < publish_release_line )); then
    pass "workflow-consistency: stable tag validation runs before publish release"
  else
    fail "workflow-consistency: stable tag validation must run before publish release"
  fi

  local import_cert_line build_release_line notarize_line upload_line ci_checks_line
  import_cert_line=$(grep -n 'name: Import Developer ID cert' "$release_workflow" | head -n1 | cut -d: -f1 || true)
  build_release_line=$(grep -n 'name: Build release artifacts' "$release_workflow" | head -n1 | cut -d: -f1 || true)
  notarize_line=$(grep -n 'name: Notarize darwin binaries' "$release_workflow" | head -n1 | cut -d: -f1 || true)
  upload_line=$(grep -n 'name: Upload dist artifacts for downstream jobs' "$release_workflow" | head -n1 | cut -d: -f1 || true)
  ci_checks_line=$(grep -n 'name: CI checks' "$release_workflow" | head -n1 | cut -d: -f1 || true)

  if [[ -n "$ci_checks_line" && -n "$build_release_line" && "$ci_checks_line" -lt "$build_release_line" ]]; then
    pass "workflow-consistency: CI checks run before release build"
  else
    fail "workflow-consistency: CI checks must run before release build"
  fi

  if grep -q 'run: make ci' "$release_workflow" && \
     grep -q 'GITHUB_TOKEN: ${{ github.token }}' "$release_workflow"; then
    pass "workflow-consistency: release CI checks invoke make ci with GitHub token"
  else
    fail "workflow-consistency: release CI checks must invoke make ci with GitHub token"
  fi

  if [[ -n "$import_cert_line" && -n "$build_release_line" && "$import_cert_line" -lt "$build_release_line" ]]; then
    pass "workflow-consistency: Developer ID cert imports before release build"
  else
    fail "workflow-consistency: Developer ID cert import must run before release build"
  fi

  if [[ -n "$notarize_line" && -n "$build_release_line" && -n "$upload_line" && "$build_release_line" -lt "$notarize_line" && "$notarize_line" -lt "$upload_line" ]]; then
    pass "workflow-consistency: notarization runs after build and before artifact upload"
  else
    fail "workflow-consistency: notarization must run after build and before artifact upload"
  fi

  if grep -q 'Apple-Actions/import-codesign-certs@5142e029c445c10ffc7149d172e540235a065466' "$release_workflow" && \
     grep -q 'MACOS_CERT_P12_BASE64' "$release_workflow" && \
     grep -q 'MACOS_CERT_P12_PASSWORD' "$release_workflow"; then
    pass "workflow-consistency: cert import action is SHA-pinned and wired to MACOS cert secrets"
  else
    fail "workflow-consistency: cert import action must be SHA-pinned and use MACOS cert secrets"
  fi

  if grep -q 'AL_CODESIGN_IDENTITY: "Developer ID Application: Hardware Breakout LLC (DQCZX59J6D)"' "$release_workflow" && \
     grep -q 'AL_REQUIRE_CODESIGN: "1"' "$release_workflow"; then
    pass "workflow-consistency: release build requires Developer ID signing"
  else
    fail "workflow-consistency: release build must set AL_CODESIGN_IDENTITY and AL_REQUIRE_CODESIGN=1"
  fi

  if grep -q 'MACOS_NOTARY_API_KEY_ID' "$release_workflow" && \
     grep -q 'MACOS_NOTARY_API_KEY_ISSUER_ID' "$release_workflow" && \
     grep -q 'MACOS_NOTARY_API_KEY_P8_BASE64' "$release_workflow" && \
     grep -q 'scripts/notarize-release.sh' "$release_workflow"; then
    pass "workflow-consistency: notarization step uses MACOS notary secrets"
  else
    fail "workflow-consistency: notarization step must use MACOS notary secrets"
  fi

  if grep -q '"al-darwin-arm64"' "$release_workflow" && \
     grep -q '"al-linux-arm64"' "$release_workflow" && \
     grep -q '"al-linux-amd64"' "$release_workflow"; then
    pass "workflow-consistency: tap job verifies binary release assets"
  else
    fail "workflow-consistency: tap job must verify binary release assets before formula update"
  fi

  if grep -q 'go run -tags tools ./internal/tools/updateformula "${FORMULA}" "${TAG}" dist/checksums.txt' "$release_workflow"; then
    pass "workflow-consistency: tap job renders binary formula from tag and checksums"
  else
    fail "workflow-consistency: tap job must invoke updateformula with formula, tag, and checksums"
  fi

  # Structural integrity: verify files required by the release workflow exist.
  # The release workflow validates cmd/publish-site/main.go and site/ at runtime
  # (line ~97-102 of release.yml). Catching their absence here prevents a green
  # CI that later fails on tag push.

  if [[ -f "$ROOT_DIR/cmd/publish-site/main.go" ]]; then
    pass "workflow-consistency: cmd/publish-site/main.go exists"
  else
    fail "workflow-consistency: cmd/publish-site/main.go missing (required by release workflow)"
  fi

  if [[ -d "$ROOT_DIR/site" ]]; then
    pass "workflow-consistency: site/ directory exists"
  else
    fail "workflow-consistency: site/ directory missing (required by release workflow)"
  fi

  if [[ -f "$ROOT_DIR/CHANGELOG.md" ]]; then
    pass "workflow-consistency: CHANGELOG.md exists"
  else
    fail "workflow-consistency: CHANGELOG.md missing (required by release workflow for release notes)"
  fi
}
