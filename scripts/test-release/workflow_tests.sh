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

  # The release workflow runs make docs-upgrade-check, which invokes
  # check-upgrade-ctas.sh, which requires ripgrep (rg). CI installs rg
  # via apt-get. The release workflow must do the same, otherwise releases
  # fail on tag push.
  #
  # Extract apt-get install package names from both workflows and verify
  # that every package installed in CI is also installed in the release workflow.

  local ci_packages release_packages
  ci_packages=$(sed -n 's/.*apt-get install[[:space:]]*//p' "$ci_workflow" | tr -s ' ' '\n' | grep -v '^-' | grep -v '^$' | sort -u || true)
  release_packages=$(sed -n 's/.*apt-get install[[:space:]]*//p' "$release_workflow" | tr -s ' ' '\n' | grep -v '^-' | grep -v '^$' | sort -u || true)

  if [[ -z "$ci_packages" ]]; then
    pass "workflow-consistency: CI installs no apt packages (nothing to check)"
    return
  fi

  local missing=""
  while IFS= read -r pkg; do
    if ! printf '%s\n' "$release_packages" | grep -qx "$pkg"; then
      missing="$missing $pkg"
    fi
  done <<< "$ci_packages"

  if [[ -z "$missing" ]]; then
    pass "workflow-consistency: release workflow installs all CI apt packages"
  else
    fail "workflow-consistency: release workflow missing apt packages from CI:$missing"
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
