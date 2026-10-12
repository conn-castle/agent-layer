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
     grep -Eq '^[[:space:]]+for workflow in ci.yml release-catalog-certification.yml; do$' <<<"$catalog_check_step" &&
     grep -Eq '^[[:space:]]+--workflow "\$\{workflow\}" \\$' <<<"$catalog_check_step" &&
     grep -Eq '^[[:space:]]+--branch main \\$' <<<"$catalog_check_step" &&
     grep -Eq '^[[:space:]]+--commit "\$\{release_commit\}" \\$' <<<"$catalog_check_step" &&
     grep -Eq '^[[:space:]]+--status success \\$' <<<"$catalog_check_step"; then
    pass "release-security: readiness requires successful CI and catalog certification for the exact source commit"
  else
    fail "release-security: readiness must bind successful CI and catalog certification to the exact source commit"
  fi

  local web_job step
  web_job=$(release_workflow_job "$workflow" "publish-website-and-tap")
  for step in "Publish release" "Commit and push" "Verify release assets are available" "Create PR in tap repo"; do
    if grep -q "^        if: env.REHEARSAL != 'true'$" <<<"$(release_workflow_step "$build_job"$'\n'"$web_job" "$step")"; then
      pass "release-rehearsal: $step is disabled in rehearsal"
    else fail "release-rehearsal: $step must be disabled in rehearsal"; fi
  done
  if grep -q 'default: true' <<<"$(sed -n '/      rehearsal:/,/^permissions:/p' "$workflow")" &&
     grep -q "REHEARSAL: \${{ github.event_name == 'workflow_dispatch' && inputs.rehearsal }}" "$workflow" &&
     [[ "$(grep -c 'ref: \${{ env.RELEASE_REF }}' "$workflow")" -eq 3 ]] &&
     grep -q 'inputs.rehearsal && github.sha || format' "$workflow" &&
     grep -q 'agent-layer-web-rehearsal-{0}' <<<"$web_job" &&
     grep -q '^  contents: read$' "$workflow" && ! grep -Eq '^[[:space:]]+contents: write$' <<<"$catalog_job$web_job" &&
     grep -q '^      contents: write$' <<<"$build_job" &&
     [[ "$(grep -c "persist-credentials: \${{ env.REHEARSAL != 'true' }}" <<<"$web_job")" -eq 2 ]]; then
    pass "release-rehearsal: safe default uses dispatched SHA, isolated concurrency, and no persisted external credentials"
  else fail "release-rehearsal: manual default, source, concurrency, or credentials are unsafe"; fi
  if grep -q -- '--event workflow_dispatch' <<<"$(release_workflow_step "$catalog_job" "Verify exact-commit release rehearsal before publication")" &&
     grep -q 'Release rehearsal \${RELEASE_TAG}' <<<"$catalog_job" &&
     [[ "$(grep -c 'permission-contents: write' <<<"$web_job")" -eq 2 ]] &&
     grep -q 'permission-pull-requests: write' <<<"$web_job" &&
     grep -q 'run: ruby -c homebrew-tap/Formula/agent-layer.rb' <<<"$(release_workflow_step "$web_job" "Validate rendered formula syntax")"; then
    pass "release-rehearsal: publication requires version-bound evidence while both modes validate app permissions and formula syntax"
  else fail "release-rehearsal: evidence, app permission validation, or formula check is missing"; fi

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
