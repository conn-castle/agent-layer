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
  local upgrades_placeholder_breaking_colon="$tmp_dir/upgrade-docs-placeholder-breaking-colon.mdx"

  cat > "$changelog_file" <<'EOF'
## v0.7.0 - 2026-02-08
- Docs updates and non-breaking cleanup.

## v0.6.1 - 2026-02-07
- breaking/manual: users must update one config key manually.

## v0.6.2 - 2026-02-08
- **Breaking:** users must rotate one credential before running `al init`.
EOF

  cat > "$upgrades_ok" <<'EOF'
| Target release | Supported source release lines | Event summary | Required manual actions | Notes |
| --- | --- | --- | --- | --- |
| `v0.7.0` | `0.6.x` | No additional migration rules yet beyond standard `al init` upgrade flow. | None currently. | Update this row when release-specific rules are defined. |
| `v0.6.1` | `0.6.0` | Breaking config key rename. | Rename `OLD_KEY` to `NEW_KEY` before running `al init`. | Required before sync. |
| `v0.6.2` | `0.6.1` | Breaking credential rotation requirement. | Rotate `AL_SERVICE_TOKEN` before running `al init`. | Required before sync. |
EOF

  cat > "$upgrades_placeholder" <<'EOF'
| Target release | Supported source release lines | Event summary | Required manual actions | Notes |
| --- | --- | --- | --- | --- |
| `v0.6.1` | `0.6.0` | No additional migration rules yet beyond standard `al init` upgrade flow. | None currently. | Update this row when release-specific rules are defined. |
EOF

  cat > "$upgrades_placeholder_breaking_colon" <<'EOF'
| Target release | Supported source release lines | Event summary | Required manual actions | Notes |
| --- | --- | --- | --- | --- |
| `v0.6.2` | `0.6.1` | No additional migration rules yet beyond standard `al init` upgrade flow. | None currently. | Update this row when release-specific rules are defined. |
EOF

  if stderr=$("$script_path" --tag v0.7.0 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" 2>&1 >/dev/null); then
    pass "check-upgrade-docs: passes when row exists and changelog has no breaking/manual signal"
  else
    fail "check-upgrade-docs: unexpected failure for non-breaking release row: $stderr"
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

  if stderr=$("$script_path" --tag v0.6.1 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" 2>&1 >/dev/null); then
    pass "check-upgrade-docs: passes when breaking/manual release has non-placeholder migration guidance"
  else
    fail "check-upgrade-docs: unexpected failure for explicit breaking/manual migration row: $stderr"
  fi

  if "$script_path" --tag v0.6.2 --upgrades-file "$upgrades_placeholder_breaking_colon" --changelog-file "$changelog_file" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail when placeholder row exists for Breaking: changelog section"
  else
    pass "check-upgrade-docs: fails when placeholder row is used for Breaking: release"
  fi

  if stderr=$("$script_path" --tag v0.6.2 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" 2>&1 >/dev/null); then
    pass "check-upgrade-docs: passes when Breaking: release has non-placeholder migration guidance"
  else
    fail "check-upgrade-docs: unexpected failure for explicit Breaking: migration row: $stderr"
  fi

  if "$script_path" --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail when --tag is missing"
  else
    pass "check-upgrade-docs: fails when --tag is missing"
  fi
}
