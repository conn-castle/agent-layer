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

release_workflow_step() {
  local job="$1"
  local step="$2"
  awk -v step="$step" '
    $0 == "      - name: " step { capture = 1 }
    capture {
      if ($0 != "      - name: " step && /^      - /) exit
      print
    }
  ' <<<"$job"
}

release_step_line() {
  local job="$1"
  local step="$2"
  awk -v step="$step" '$0 == "      - name: " step { print NR; exit }' <<<"$job"
}

run_release_workflow_security_tests() {
  section "Release Workflow Security Tests"

  local workflow="$ROOT_DIR/.github/workflows/release.yml"
  if [[ ! -f "$workflow" ]]; then
    fail "release-security: release workflow is missing"
    return
  fi

  local catalog_job build_job
  catalog_job=$(release_workflow_job "$workflow" "catalog-readiness")
  build_job=$(release_workflow_job "$workflow" "build-release")
  if [[ -z "$catalog_job" || -z "$build_job" ]]; then
    fail "release-security: catalog-readiness or build-release job is missing"
    return
  fi

  if grep -q '^    needs: catalog-readiness$' <<<"$build_job"; then
    pass "release-security: release build requires catalog readiness"
  else
    fail "release-security: build-release must require catalog-readiness"
  fi

  local catalog_check_step
  catalog_check_step=$(release_workflow_step "$catalog_job" "Verify exact-commit catalog certification")
  if grep -Eq '^[[:space:]]+release_commit="\$\(git rev-parse HEAD\)"$' <<<"$catalog_check_step" &&
     grep -Eq '^[[:space:]]+--workflow release-catalog-certification\.yml \\$' <<<"$catalog_check_step" &&
     grep -Eq '^[[:space:]]+--branch main \\$' <<<"$catalog_check_step" &&
     grep -Eq '^[[:space:]]+--commit "\$\{release_commit\}" \\$' <<<"$catalog_check_step" &&
     grep -Eq '^[[:space:]]+--status success \\$' <<<"$catalog_check_step"; then
    pass "release-security: catalog readiness requires successful certification for the exact tag commit"
  else
    fail "release-security: catalog readiness must bind successful certification to the exact tag commit"
  fi

  local certificate_step build_step scan_step
  certificate_step=$(release_workflow_step "$build_job" "Import Developer ID cert")
  build_step=$(release_workflow_step "$build_job" "Build release artifacts")
  scan_step=$(release_workflow_step "$build_job" "Scan release binaries for known vulnerable symbols")

  if grep -Eq '^        uses: Apple-Actions/import-codesign-certs@[0-9a-f]{40}([[:space:]]+#.*)?$' <<<"$certificate_step" &&
     grep -Eq '^          p12-file-base64: \$\{\{ secrets\.MACOS_CERT_P12_BASE64 \}\}$' <<<"$certificate_step" &&
     grep -Eq '^          p12-password: \$\{\{ secrets\.MACOS_CERT_P12_PASSWORD \}\}$' <<<"$certificate_step"; then
    pass "release-security: certificate import is commit-pinned and secret-backed"
  else
    fail "release-security: certificate import must be commit-pinned and secret-backed"
  fi

  if grep -Eq '^          AL_REQUIRE_CODESIGN: "1"$' <<<"$build_step" &&
     grep -Eq '^          AL_CODESIGN_IDENTITY: ".+"$' <<<"$build_step" &&
     grep -Eq '^[[:space:]]+make release-dist AL_VERSION="\$\{RELEASE_TAG\}" DIST_DIR=dist$' <<<"$build_step"; then
    pass "release-security: release artifacts require code signing"
  else
    fail "release-security: release artifacts must require a signing identity"
  fi

  local validate_line build_line scan_line notarize_line upload_line publish_line
  validate_line=$(release_step_line "$build_job" "Validate stable release tag format")
  build_line=$(release_step_line "$build_job" "Build release artifacts")
  scan_line=$(release_step_line "$build_job" "Scan release binaries for known vulnerable symbols")
  notarize_line=$(release_step_line "$build_job" "Notarize darwin binaries")
  upload_line=$(release_step_line "$build_job" "Upload dist artifacts for downstream jobs")
  publish_line=$(release_step_line "$build_job" "Publish release")

  if [[ -n "$validate_line" && -n "$publish_line" ]] && (( validate_line < publish_line )); then
    pass "release-security: stable tag validation precedes publication"
  else
    fail "release-security: stable tag validation must precede publication"
  fi

  if [[ -n "$build_line" && -n "$scan_line" && -n "$notarize_line" && -n "$upload_line" && -n "$publish_line" ]] &&
     (( build_line < scan_line && scan_line < notarize_line && scan_line < upload_line && scan_line < publish_line )) &&
     grep -Eq '^[[:space:]]+(run:[[:space:]]+)?make release-vuln-check DIST_DIR=dist[[:space:]]*$' <<<"$scan_step"; then
    pass "release-security: vulnerability scanning follows the build and gates every artifact publication path"
  else
    fail "release-security: vulnerability scanning must follow the build and gate notarization, upload, and publication"
  fi
}
