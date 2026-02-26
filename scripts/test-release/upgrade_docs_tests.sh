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

  # Create stub manifests so existing tests pass the new manifest checks.
  mkdir -p "$tmp_dir/internal/templates/migrations"
  mkdir -p "$tmp_dir/internal/templates/manifests"
  for ver in 0.7.0 0.6.1 0.6.2; do
    echo '{}' > "$tmp_dir/internal/templates/migrations/$ver.json"
    echo '{}' > "$tmp_dir/internal/templates/manifests/$ver.json"
  done

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

  if stderr=$("$script_path" --tag v0.7.0 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" --repo-root "$tmp_dir" 2>&1 >/dev/null); then
    pass "check-upgrade-docs: passes when row exists and changelog has no breaking/manual signal"
  else
    fail "check-upgrade-docs: unexpected failure for non-breaking release row: $stderr"
  fi

  if "$script_path" --tag v0.7.0-rc.1 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" --repo-root "$tmp_dir" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail for prerelease tags"
  else
    pass "check-upgrade-docs: fails for prerelease tags"
  fi

  if "$script_path" --tag v0.9.9 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" --repo-root "$tmp_dir" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail when release row is missing"
  else
    pass "check-upgrade-docs: fails when release row is missing"
  fi

  if "$script_path" --tag v0.6.1 --upgrades-file "$upgrades_placeholder" --changelog-file "$changelog_file" --repo-root "$tmp_dir" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail when placeholder row exists for breaking/manual changelog section"
  else
    pass "check-upgrade-docs: fails when placeholder row is used for breaking/manual release"
  fi

  if stderr=$("$script_path" --tag v0.6.1 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" --repo-root "$tmp_dir" 2>&1 >/dev/null); then
    pass "check-upgrade-docs: passes when breaking/manual release has non-placeholder migration guidance"
  else
    fail "check-upgrade-docs: unexpected failure for explicit breaking/manual migration row: $stderr"
  fi

  if "$script_path" --tag v0.6.2 --upgrades-file "$upgrades_placeholder_breaking_colon" --changelog-file "$changelog_file" --repo-root "$tmp_dir" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail when placeholder row exists for Breaking: changelog section"
  else
    pass "check-upgrade-docs: fails when placeholder row is used for Breaking: release"
  fi

  if stderr=$("$script_path" --tag v0.6.2 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" --repo-root "$tmp_dir" 2>&1 >/dev/null); then
    pass "check-upgrade-docs: passes when Breaking: release has non-placeholder migration guidance"
  else
    fail "check-upgrade-docs: unexpected failure for explicit Breaking: migration row: $stderr"
  fi

  if "$script_path" --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" --repo-root "$tmp_dir" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail when --tag is missing"
  else
    pass "check-upgrade-docs: fails when --tag is missing"
  fi

  # --- Manifest existence tests ---

  # Missing migration manifest: repo root has ownership manifest but not migration.
  local no_migration_root="$tmp_dir/no-migration-root"
  mkdir -p "$no_migration_root/internal/templates/manifests"
  echo '{}' > "$no_migration_root/internal/templates/manifests/0.7.0.json"

  if "$script_path" --tag v0.7.0 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" --repo-root "$no_migration_root" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail when migration manifest is missing"
  else
    pass "check-upgrade-docs: fails when migration manifest is missing"
  fi

  # Missing ownership manifest: repo root has migration manifest but not ownership.
  local no_ownership_root="$tmp_dir/no-ownership-root"
  mkdir -p "$no_ownership_root/internal/templates/migrations"
  echo '{}' > "$no_ownership_root/internal/templates/migrations/0.7.0.json"

  if "$script_path" --tag v0.7.0 --upgrades-file "$upgrades_ok" --changelog-file "$changelog_file" --repo-root "$no_ownership_root" >/dev/null 2>&1; then
    fail "check-upgrade-docs: should fail when ownership manifest is missing"
  else
    pass "check-upgrade-docs: fails when ownership manifest is missing"
  fi
}
