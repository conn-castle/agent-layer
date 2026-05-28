#!/usr/bin/env bash
# Scenario: CLI catalog skill flow — by default a fresh `al init` does not
# install any CLI catalog skills (they're opt-in via wizard); doctor reports
# nothing about them. When the user later creates a catalog skill directory,
# doctor checks the corresponding binary on PATH.

run_scenario_wizard_cli_catalog_skills() {
  section "CLI catalog skills: doctor binary check"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # The catalog skills are NOT seeded automatically by al init.
  for id in tavily-web playwright-cli find-docs agent-dispatch; do
    assert_file_not_exists "$repo_dir/.agent-layer/skills/$id/SKILL.md" \
      "$id catalog skill is not seeded by al init"
  done

  # Simulate Q2=tavily by manually creating the directory with a placeholder
  # SKILL.md (sufficient for the doctor binary probe; full e2e wizard
  # interactivity is exercised by unit tests).
  local skill_dir="$repo_dir/.agent-layer/skills/tavily-web"
  mkdir -p "$skill_dir"
  cat > "$skill_dir/SKILL.md" <<'SKILL'
---
name: tavily-web
description: Tavily web search.
---
Placeholder for catalog skill content.
SKILL

  # Doctor flags missing `tvly` binary when tavily-web is present and binary
  # is not on PATH. Keep this PATH hermetic so a host-installed tvly cannot
  # turn the missing-binary assertion into a machine-dependent result.
  local doctor_output rc=0
  doctor_output=$(cd "$repo_dir" && PATH="$E2E_INSTALL_PREFIX/bin" al doctor 2>&1) || rc=$?
  if grep -qF -- "tavily-web" <<<"$doctor_output"; then
    pass "doctor mentions tavily-web when binary is missing"
  else
    fail "doctor did not mention tavily-web with missing binary; rc=$rc"
    echo "$doctor_output" | head -20 | sed 's/^/    /'
  fi

  # Drop a stub tvly into a mock bin dir, prepend to PATH, re-run doctor:
  # doctor should report OK for tavily-web's binary check.
  local mock_bin_with_tvly="$E2E_TMP_ROOT/catalog-skills-with-tvly-bin"
  mkdir -p "$mock_bin_with_tvly"
  cat > "$mock_bin_with_tvly/tvly" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
  chmod +x "$mock_bin_with_tvly/tvly"

  local doctor_output_ok rc_ok=0
  doctor_output_ok=$(cd "$repo_dir" && PATH="$mock_bin_with_tvly:$E2E_INSTALL_PREFIX/bin" al doctor 2>&1) || rc_ok=$?
  if grep -qF -- "tavily-web found" <<<"$doctor_output_ok"; then
    pass "doctor reports tavily-web found when tvly is on PATH"
  else
    fail "doctor did not report tavily-web found; rc=$rc_ok"
    echo "$doctor_output_ok" | head -20 | sed 's/^/    /'
  fi

  # Removing the catalog skill directory makes doctor stop reporting tavily-web.
  rm -rf "$skill_dir"
  local doctor_output_silent rc_silent=0
  doctor_output_silent=$(cd "$repo_dir" && PATH="$E2E_INSTALL_PREFIX/bin" al doctor 2>&1) || rc_silent=$?
  if grep -qF -- "tavily-web" <<<"$doctor_output_silent"; then
    fail "doctor still mentions tavily-web after directory removal"
    echo "$doctor_output_silent" | head -20 | sed 's/^/    /'
  else
    pass "doctor no longer mentions tavily-web after removal"
  fi

  # The doctor failures must never gate the agent launch.
  install_mock_claude "$repo_dir"
  assert_exit_zero_in "$repo_dir" "al claude after catalog-skill probe" al claude

  cleanup_scenario_dir "$repo_dir"
}
