# Helper functions for upgrade-docs validation tests in scripts/test-release.sh.

run_upgrade_docs_script_tests() {
  section "Upgrade Docs Script Tests"

  local script_path="$ROOT_DIR/scripts/check-upgrade-docs.sh"
  if [[ ! -f "$script_path" ]]; then
    fail "check-upgrade-docs.sh not found"
    return
  fi

  if [[ ! -x "$script_path" ]]; then
    fail "check-upgrade-docs.sh is not executable"
  else
    pass "check-upgrade-docs.sh is executable"
  fi

  local changelog_file="$tmp_dir/upgrade-docs-changelog.md"
  local upgrades_ok="$tmp_dir/upgrade-docs-ok.mdx"
  local upgrades_placeholder="$tmp_dir/upgrade-docs-placeholder.mdx"

  cat > "$changelog_file" <<'EOF'
## v0.7.0 - 2026-02-08
- Docs updates and non-breaking cleanup.

## v0.6.1 - 2026-02-07
- breaking/manual: users must update one config key manually.
EOF

  cat > "$upgrades_ok" <<'EOF'
| Target release | Supported source release lines | Event summary | Required manual actions | Notes |
| --- | --- | --- | --- | --- |
| `v0.7.0` | `0.6.x` | No additional migration rules yet beyond standard `al init` upgrade flow. | None currently. | Update this row when release-specific rules are defined. |
| `v0.6.1` | `0.6.0` | Breaking config key rename. | Rename `OLD_KEY` to `NEW_KEY` before running `al init`. | Required before sync. |
EOF

  cat > "$upgrades_placeholder" <<'EOF'
| Target release | Supported source release lines | Event summary | Required manual actions | Notes |
| --- | --- | --- | --- | --- |
| `v0.6.1` | `0.6.0` | No additional migration rules yet beyond standard `al init` upgrade flow. | None currently. | Update this row when release-specific rules are defined. |
EOF

  if "$script_path" --tag v0.7.0 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" >/dev/null 2>&1; then
    pass "check-upgrade-docs: passes when row exists and changelog has no breaking/manual signal"
  else
    fail "check-upgrade-docs: unexpected failure for non-breaking release row"
  fi

  if "$script_path" --tag v0.9.9 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail when release row is missing"
  else
    pass "check-upgrade-docs: fails when release row is missing"
  fi

  if "$script_path" --tag v0.6.1 --upgrades-file "$upgrades_placeholder" --changelog-file "$changelog_file" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail when placeholder row exists for breaking/manual changelog section"
  else
    pass "check-upgrade-docs: fails when placeholder row is used for breaking/manual release"
  fi

  if "$script_path" --tag v0.6.1 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" >/dev/null 2>&1; then
    pass "check-upgrade-docs: passes when breaking/manual release has non-placeholder migration guidance"
  else
    fail "check-upgrade-docs: unexpected failure for explicit breaking/manual migration row"
  fi

  if "$script_path" --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail when --tag is missing"
  else
    pass "check-upgrade-docs: fails when --tag is missing"
  fi
}
