# Helper functions for CI/release workflow consistency tests in scripts/test-release.sh.

# Print the YAML block for one GitHub Actions job, from its header through the
# line before the next top-level job key.
workflow_job_block() {
  local file="$1"
  local job="$2"
  awk -v job="$job" '
    $0 == "  " job ":" { grabbing = 1 }
    grabbing {
      if ($0 != "  " job ":" && /^  [^[:space:]]/) {
        exit
      }
      print
    }
  ' "$file"
}

run_workflow_consistency_tests() {
  section "Workflow Consistency Tests"

  local ci_workflow="$ROOT_DIR/.github/workflows/ci.yml"
  local release_workflow="$ROOT_DIR/.github/workflows/release.yml"
  local certification_workflow="$ROOT_DIR/.github/workflows/release-catalog-certification.yml"

  if [[ ! -f "$ci_workflow" ]]; then
    fail "ci.yml not found"
    return
  fi

  if [[ ! -f "$release_workflow" ]]; then
    fail "release.yml not found"
    return
  fi

  if [[ ! -f "$certification_workflow" ]]; then
    fail "release-catalog-certification.yml not found"
    return
  fi

  if grep -q 'runs-on: macos-latest' "$release_workflow"; then
    pass "workflow-consistency: build-release runs on macos-latest"
  else
    fail "workflow-consistency: build-release must run on macos-latest for Developer ID signing"
  fi

  local catalog_job build_job classification_job certification_job
  catalog_job=$(workflow_job_block "$release_workflow" "catalog-readiness")
  build_job=$(workflow_job_block "$release_workflow" "build-release")
  classification_job=$(workflow_job_block "$certification_workflow" "classify")
  certification_job=$(workflow_job_block "$certification_workflow" "catalog-readiness")

  if [[ -n "$classification_job" && -n "$certification_job" ]] && \
     grep -q '^  workflow_dispatch:$' "$certification_workflow" && \
     grep -q '^  push:$' "$certification_workflow" && \
     grep -q '^  schedule:$' "$certification_workflow" && \
     grep -q 'GITHUB_REF.*refs/heads/main' "$certification_workflow" && \
     grep -q 'fetch-depth: 0' <<<"$classification_job" && \
     grep -q 'scripts/catalog-certification-scope.sh' <<<"$classification_job" && \
     grep -q "if: needs.classify.outputs.required == 'true'" <<<"$certification_job" && \
     grep -q 'ref: ${{ github.sha }}' <<<"$certification_job" && \
     grep -q 'timeout-minutes: 30' <<<"$certification_job" && \
     grep -q 'shard: \[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16\]' <<<"$certification_job" && \
     grep -q -- '--task-concurrency 1' <<<"$certification_job" && \
     grep -q -- '--remove-task-images' <<<"$certification_job" && \
     grep -q -- '--task-shard-index ${{ matrix.shard }}' <<<"$certification_job" && \
     grep -q -- '--task-shard-count 16' <<<"$certification_job" && \
     grep -q -- '--task-timeout 10m' <<<"$certification_job"; then
    pass "workflow-consistency: pre-tag catalog certification is change-sensitive and uses sixteen bounded shards"
  else
    fail "workflow-consistency: pre-tag catalog certification must classify changes and use sixteen bounded shards when required"
  fi

  if [[ -n "$catalog_job" && -n "$build_job" ]] && \
     grep -q '^  actions: read$' "$release_workflow" && \
     grep -q 'timeout-minutes: 5' <<<"$catalog_job" && \
     grep -q 'git rev-parse HEAD' <<<"$catalog_job" && \
     grep -q -- '--workflow release-catalog-certification.yml' <<<"$catalog_job" && \
     grep -q -- '--branch main' <<<"$catalog_job" && \
     grep -q -- '--commit "${release_commit}"' <<<"$catalog_job" && \
     grep -q -- '--status success' <<<"$catalog_job" && \
     ! grep -q 'benchmark readiness' <<<"$catalog_job" && \
     grep -q '^    needs: catalog-readiness$' <<<"$build_job"; then
    pass "workflow-consistency: release builds require successful pre-tag certification for the exact tag commit"
  else
    fail "workflow-consistency: release builds must verify exact-commit pre-tag catalog certification"
  fi

  local certification_script="$ROOT_DIR/scripts/certify-release-catalog.sh"
  if [[ -x "$certification_script" ]] && \
     grep -q 'git branch --show-current' "$certification_script" && \
     grep -q 'git status --porcelain' "$certification_script" && \
     grep -q 'git ls-remote --exit-code origin refs/heads/main' "$certification_script" && \
     grep -q -- '--commit "${head_sha}"' "$certification_script" && \
     grep -q 'gh workflow run "${workflow}" --ref main' "$certification_script" && \
     grep -q 'gh run watch "${active_run}" --exit-status' "$certification_script"; then
    pass "workflow-consistency: release certification helper requires clean pushed main and waits for its exact-SHA run"
  else
    fail "workflow-consistency: release certification helper must bind dispatch and success to clean pushed main"
  fi

  local catalog_validate_line catalog_checkout_line
  local build_validate_line build_checkout_line build_setup_line
  catalog_validate_line=$(grep -n 'name: Validate stable release tag format' <<<"$catalog_job" | head -n1 | cut -d: -f1 || true)
  catalog_checkout_line=$(grep -n 'uses: actions/checkout@' <<<"$catalog_job" | head -n1 | cut -d: -f1 || true)
  build_validate_line=$(grep -n 'name: Validate stable release tag format' <<<"$build_job" | head -n1 | cut -d: -f1 || true)
  build_checkout_line=$(grep -n 'uses: actions/checkout@' <<<"$build_job" | head -n1 | cut -d: -f1 || true)
  build_setup_line=$(grep -n 'uses: actions/setup-go@' <<<"$build_job" | head -n1 | cut -d: -f1 || true)

  if [[ -n "$catalog_validate_line" && -n "$catalog_checkout_line" && \
        -n "$build_validate_line" && -n "$build_checkout_line" && -n "$build_setup_line" && \
        "$catalog_validate_line" -lt "$catalog_checkout_line" && \
        "$build_validate_line" -lt "$build_checkout_line" && "$build_checkout_line" -lt "$build_setup_line" ]] && \
     grep -q 'ref: refs/tags/${{ env.RELEASE_TAG }}' <<<"$catalog_job" && \
     grep -q 'ref: refs/tags/${{ env.RELEASE_TAG }}' <<<"$build_job" && \
     awk '/uses: actions\/setup-go@/{found=1} found && /cache: false/{found=0; ok=1} found && /cache: true/{exit 1} END{exit !ok}' <<<"$certification_job" && \
     awk '/uses: actions\/setup-go@/{found=1} found && /cache: false/{found=0; ok=1} found && /cache: true/{exit 1} END{exit !ok}' <<<"$build_job"; then
    pass "workflow-consistency: release jobs validate before checkout and certification/build jobs do not cache unvalidated modules"
  else
    fail "workflow-consistency: release validation must precede checkout and certification/build setup-go caching must be disabled"
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
  stable_tag_check_line=$(grep -n 'name: Validate stable release tag format' <<<"$build_job" | head -n1 | cut -d: -f1 || true)
  publish_release_line=$(grep -n 'name: Publish release' <<<"$build_job" | head -n1 | cut -d: -f1 || true)

  if [[ -z "$stable_tag_check_line" ]]; then
    fail "workflow-consistency: missing stable release tag validation step"
  elif [[ -z "$publish_release_line" ]]; then
    fail "workflow-consistency: missing publish release step"
  elif (( stable_tag_check_line < publish_release_line )); then
    pass "workflow-consistency: stable tag validation runs before publish release"
  else
    fail "workflow-consistency: stable tag validation must run before publish release"
  fi

  local import_cert_line build_release_line release_tools_line vuln_scan_line notarize_line upload_line ci_checks_line
  import_cert_line=$(grep -n 'name: Import Developer ID cert' <<<"$build_job" | head -n1 | cut -d: -f1 || true)
  build_release_line=$(grep -n 'name: Build release artifacts' <<<"$build_job" | head -n1 | cut -d: -f1 || true)
  release_tools_line=$(grep -n 'name: Install pinned release tools' <<<"$build_job" | head -n1 | cut -d: -f1 || true)
  vuln_scan_line=$(grep -n 'name: Scan release binaries for known vulnerable symbols' <<<"$build_job" | head -n1 | cut -d: -f1 || true)
  notarize_line=$(grep -n 'name: Notarize darwin binaries' <<<"$build_job" | head -n1 | cut -d: -f1 || true)
  upload_line=$(grep -n 'name: Upload dist artifacts for downstream jobs' <<<"$build_job" | head -n1 | cut -d: -f1 || true)
  ci_checks_line=$(grep -n 'name: CI checks' <<<"$build_job" | head -n1 | cut -d: -f1 || true)

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

  if [[ -n "$vuln_scan_line" && -n "$build_release_line" && -n "$notarize_line" && -n "$upload_line" && -n "$publish_release_line" && \
        "$build_release_line" -lt "$vuln_scan_line" && "$vuln_scan_line" -lt "$notarize_line" && \
        "$vuln_scan_line" -lt "$upload_line" && "$vuln_scan_line" -lt "$publish_release_line" ]] && \
     grep -q 'make release-vuln-check DIST_DIR=dist' "$release_workflow"; then
    pass "workflow-consistency: vulnerability scan gates every release publication path"
  else
    fail "workflow-consistency: vulnerability scan must run after build and before notarization, upload, and publish"
  fi

  if [[ -n "$release_tools_line" && -n "$vuln_scan_line" && "$release_tools_line" -lt "$vuln_scan_line" ]] && \
     grep -q 'make release-tools' "$release_workflow" && ! grep -q 'release-vuln-check' "$ci_workflow"; then
    pass "workflow-consistency: release tools are installed only for the release lane"
  else
    fail "workflow-consistency: release must install its scanner before use and ordinary CI must not invoke it"
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

run_dev_loop_consistency_tests() {
  section "Dev Loop Consistency Tests"

  local make_db make_status ci_prereqs dev_recipe fmt_dry
  if make_db=$(make -C "$ROOT_DIR" -qp --no-builtin-rules --no-builtin-variables 2>/dev/null); then
    make_status=0
  else
    make_status=$?
  fi
  if (( make_status > 1 )); then
    fail "dev-loop: could not inspect the make database (exit $make_status)"
    return
  fi
  ci_prereqs=$(printf '%s\n' "$make_db" | awk '/^ci:/{print; exit}')
  if [[ "$ci_prereqs" == "ci: tidy-check fmt-check lint dead-code coverage test-deepswe-planner test-race test-release test-e2e-harness test-e2e-ci docs-cta-check" ]]; then
    pass "dev-loop: make ci retains the complete verification prerequisites"
  else
    fail "dev-loop: make ci prerequisites changed (got: $ci_prereqs)"
  fi

  dev_recipe=$(awk '
    /^dev:/ { target = 1; next }
    target && /^\t/ { sub(/^\t/, ""); print; next }
    target { exit }
  ' "$ROOT_DIR/Makefile")
  if [[ "$dev_recipe" == $'@$(MAKE) fmt\n@$(MAKE) lint' ]]; then
    pass "dev-loop: make dev runs formatting and lint only"
  else
    fail "dev-loop: make dev must run formatting and lint only (got: $dev_recipe)"
  fi

  if ! fmt_dry=$(make -C "$ROOT_DIR" -n --no-builtin-rules --no-builtin-variables fmt 2>&1); then
    fail "dev-loop: could not inspect make fmt"
    return
  fi
  if [[ "$fmt_dry" != *"-prune"* || "$fmt_dry" == *"-not -path"* ]]; then
    fail "dev-loop: formatting discovery must prune excluded directory roots"
  else
    local root missing_root=""
    for root in .git .tools .cache .claude .codex .gemini .agy .antigravitycli .agents .agent-layer tmp; do
      if [[ "$fmt_dry" != *"-path './$root'"* ]]; then
        missing_root="$root"
        break
      fi
    done
    if [[ -n "$missing_root" ]]; then
      fail "dev-loop: formatting discovery does not prune ./$missing_root"
    else
      pass "dev-loop: formatting discovery prunes every excluded directory root"
    fi
  fi
}
