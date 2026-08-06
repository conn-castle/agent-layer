#!/usr/bin/env bash
# Scenario: Scripted wizard installs a CLI catalog skill, then doctor probes
# the corresponding binary on PATH.

run_scenario_wizard_cli_catalog_skills() {
  section "Scripted wizard + CLI catalog skills: doctor binary check"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # The catalog skills are NOT seeded automatically by al init.
  for id in tavily-web playwright find-docs dispatch-agent skill-sync; do
    assert_file_not_exists "$repo_dir/.agent-layer/skills/$id/SKILL.md" \
      "$id catalog skill is not seeded by al init"
  done

  local answers_file="$repo_dir/wizard-answers.json"
  cat > "$answers_file" <<'JSON'
{
  "select": {
    "Approval Mode": "all - Auto-approve shell commands and MCP tool calls (where supported).",
    "Claude Model": "Leave blank (use client default)",
    "Claude Reasoning Effort": "Leave blank (use client default)"
  },
  "multi_select": {
    "Enable Agents": ["claude"],
    "Claude features (checked = keep enabled; uncheck to disable)": [
      "IDE open-file reading",
      "Auto-memory",
      "claude.ai connectors",
      "AskUserQuestion tool"
    ],
    "Track the following Agent Layer folders in git? (checked = tracked; unchecked = gitignored)": [
      "docs/agent-layer/"
    ],
    "Enable CLI skills (some require a CLI on PATH; doctor reports missing binaries)": [
      "Playwright browser automation",
      "Agent Layer skill sync"
    ],
    "Enable Default MCP Servers": []
  },
  "confirm": {
    "Isolate Claude settings and caches per repo? (auth remains shared globally — upstream limitation)": false,
    "Install the Agent Layer workflow bundle? (adds missing workflow skills, managed instruction files, and memory docs/templates; existing files are left unchanged)": true,
    "Enable warnings for performance and usage issues?": true,
    "Apply these config, secret, skills, instructions, memory-file, gitignore-source, and statusline-source changes?": true
  }
}
JSON

  local wizard_output rc_wizard=0
  wizard_output=$(cd "$repo_dir" && al wizard --answers "$answers_file" 2>&1) || rc_wizard=$?
  if [[ $rc_wizard -eq 0 ]]; then
    pass "al wizard --answers installs selected catalog skill"
  else
    fail "al wizard --answers should exit zero (rc=$rc_wizard)"
    echo "$wizard_output" | head -20 | sed 's/^/    /'
  fi
  assert_output_contains "$wizard_output" "Running sync" \
    "scripted wizard output says sync ran"
  assert_output_contains "$wizard_output" "Wizard completed" \
    "scripted wizard output says completed"
  assert_file_exists "$repo_dir/.agent-layer/instructions/00_rules.md" \
    "scripted wizard installed workflow instruction source"
  assert_file_exists "$repo_dir/docs/agent-layer/COMMANDS.md" \
    "scripted wizard installed memory docs"

  local skill_dir="$repo_dir/.agent-layer/skills/playwright"
  assert_file_exists "$skill_dir/SKILL.md" \
    "scripted wizard installed playwright catalog skill"
  assert_file_contains "$skill_dir/SKILL.md" "name: playwright" \
    "playwright catalog skill uses the collision-free skill id"
  assert_file_contains "$skill_dir/SKILL.md" "playwright-cli --help" \
    "playwright catalog skill preserves the playwright-cli command surface"
  local skill_sync_dir="$repo_dir/.agent-layer/skills/skill-sync"
  assert_file_exists "$skill_sync_dir/SKILL.md" \
    "scripted wizard installed skill-sync catalog skill"
  assert_file_contains "$skill_sync_dir/SKILL.md" "name: skill-sync" \
    "skill-sync catalog skill preserves its catalog id"
  assert_file_contains "$skill_sync_dir/SKILL.md" "al skills status --all" \
    "skill-sync catalog skill preserves its local status workflow"
  for id in tavily-web find-docs dispatch-agent; do
    assert_file_not_exists "$repo_dir/.agent-layer/skills/$id/SKILL.md" \
      "$id catalog skill remains absent when unselected"
  done

  # Doctor flags missing `playwright-cli` when playwright is present and the
  # binary is not on PATH. Keep this PATH hermetic so a host-installed CLI cannot
  # turn the missing-binary assertion into a machine-dependent result.
  local doctor_output rc=0
  doctor_output=$(cd "$repo_dir" && PATH="$E2E_INSTALL_PREFIX/bin" al doctor 2>&1) || rc=$?
  if [[ $rc -ne 0 ]]; then
    pass "doctor exits nonzero when playwright-cli is missing"
  else
    fail "doctor should exit nonzero when playwright-cli is missing"
  fi
  assert_output_contains "$doctor_output" "[FAIL]" \
    "doctor reports failure when playwright-cli is missing"
  assert_output_contains "$doctor_output" "playwright" \
    "doctor mentions playwright when its binary is missing"
  assert_output_contains "$doctor_output" "playwright-cli" \
    "doctor mentions the missing playwright-cli binary"
  assert_output_contains "$doctor_output" "Some checks failed" \
    "doctor prints failure summary for missing playwright-cli"
  assert_output_not_contains "$doctor_output" "All systems go" \
    "doctor does not print healthy summary for missing playwright-cli"

  # Drop a stub playwright-cli into a mock bin dir, prepend to PATH, and rerun.
  local mock_bin_with_playwright_cli="$E2E_TMP_ROOT/catalog-skills-with-playwright-cli-bin"
  mkdir -p "$mock_bin_with_playwright_cli"
  cat > "$mock_bin_with_playwright_cli/playwright-cli" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
  chmod +x "$mock_bin_with_playwright_cli/playwright-cli"

  local doctor_output_ok rc_ok=0
  doctor_output_ok=$(cd "$repo_dir" && PATH="$mock_bin_with_playwright_cli:$E2E_INSTALL_PREFIX/bin" al doctor 2>&1) || rc_ok=$?
  if [[ $rc_ok -eq 0 ]]; then
    pass "doctor exits zero when playwright-cli is on PATH"
  else
    fail "doctor should exit zero when playwright-cli is on PATH (rc=$rc_ok)"
  fi
  if grep -qF -- "playwright found" <<<"$doctor_output_ok"; then
    pass "doctor reports playwright found when playwright-cli is on PATH"
  else
    fail "doctor did not report playwright found; rc=$rc_ok"
    echo "$doctor_output_ok" | head -20 | sed 's/^/    /'
  fi
  assert_output_not_contains "$doctor_output_ok" "[FAIL]" \
    "doctor has no failures when playwright-cli is on PATH"

  # Removing the catalog skill directory makes doctor stop reporting playwright.
  rm -rf "$skill_dir"
  local doctor_output_silent rc_silent=0
  doctor_output_silent=$(cd "$repo_dir" && PATH="$E2E_INSTALL_PREFIX/bin" al doctor 2>&1) || rc_silent=$?
  if [[ $rc_silent -eq 0 ]]; then
    pass "doctor exits zero after playwright directory removal"
  else
    fail "doctor should exit zero after playwright directory removal (rc=$rc_silent)"
  fi
  if grep -qF -- "playwright" <<<"$doctor_output_silent"; then
    fail "doctor still mentions playwright after directory removal"
    echo "$doctor_output_silent" | head -20 | sed 's/^/    /'
  else
    pass "doctor no longer mentions playwright after removal"
  fi

  # The doctor failures must never gate the agent launch.
  install_mock_claude "$repo_dir"
  assert_exit_zero_in "$repo_dir" "al claude after catalog-skill probe" al claude

  cleanup_scenario_dir "$repo_dir"
}
