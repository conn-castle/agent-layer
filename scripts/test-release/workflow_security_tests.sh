# Security contracts for the declarative release pipeline.

release_workflow_job() {
  local workflow="$1"
  local job="$2"
  awk -v job="$job" '
    $0 == "  " job ":" { capture = 1 }
    capture {
      if ($0 != "  " job ":" && /^  [^[:space:]]/) exit
      print
    }
  ' "$workflow"
}

release_step_line() {
  local job="$1"
  local step="$2"
  grep -nF "name: $step" <<<"$job" | head -n1 | cut -d: -f1 || true
}

run_release_workflow_security_tests() {
  section "Release Workflow Security Tests"

  local workflow="$ROOT_DIR/.github/workflows/release.yml"
  if [[ ! -f "$workflow" ]]; then
    fail "release-security: release workflow is missing"
    return
  fi

  local build_job
  build_job=$(release_workflow_job "$workflow" "build-release")
  if [[ -z "$build_job" ]]; then
    fail "release-security: build-release job is missing"
    return
  fi

  if grep -q '^    needs: catalog-readiness$' <<<"$build_job"; then
    pass "release-security: release build requires exact-commit catalog readiness"
  else
    fail "release-security: build-release must require catalog-readiness"
  fi

  if grep -Eq 'uses: Apple-Actions/import-codesign-certs@[0-9a-f]{40}([[:space:]]|$)' <<<"$build_job" &&
     grep -q 'MACOS_CERT_P12_BASE64' <<<"$build_job" &&
     grep -q 'MACOS_CERT_P12_PASSWORD' <<<"$build_job"; then
    pass "release-security: certificate import is commit-pinned and secret-backed"
  else
    fail "release-security: certificate import must be commit-pinned and secret-backed"
  fi

  if grep -q 'AL_REQUIRE_CODESIGN: "1"' <<<"$build_job" &&
     grep -q 'AL_CODESIGN_IDENTITY:' <<<"$build_job"; then
    pass "release-security: release artifacts require code signing"
  else
    fail "release-security: release artifacts must require a signing identity"
  fi

  local validate_line scan_line notarize_line upload_line publish_line
  validate_line=$(release_step_line "$build_job" "Validate stable release tag format")
  scan_line=$(release_step_line "$build_job" "Scan release binaries for known vulnerable symbols")
  notarize_line=$(release_step_line "$build_job" "Notarize darwin binaries")
  upload_line=$(release_step_line "$build_job" "Upload dist artifacts for downstream jobs")
  publish_line=$(release_step_line "$build_job" "Publish release")

  if [[ -n "$validate_line" && -n "$publish_line" ]] && (( validate_line < publish_line )); then
    pass "release-security: stable tag validation precedes publication"
  else
    fail "release-security: stable tag validation must precede publication"
  fi

  if [[ -n "$scan_line" && -n "$notarize_line" && -n "$upload_line" && -n "$publish_line" ]] &&
     (( scan_line < notarize_line && scan_line < upload_line && scan_line < publish_line )) &&
     grep -q 'make release-vuln-check DIST_DIR=dist' <<<"$build_job"; then
    pass "release-security: vulnerability scanning gates every artifact publication path"
  else
    fail "release-security: vulnerability scanning must gate notarization, upload, and publication"
  fi
}
