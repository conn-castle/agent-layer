#!/usr/bin/env bash
# Scenario: Scripted wizard installs a CLI catalog skill, then doctor probes
# the corresponding binary on PATH.

run_scenario_wizard_cli_catalog_skills() {
  section "Scripted wizard + CLI catalog skills: doctor binary check"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # The catalog skills are NOT seeded automatically by al init.
  for id in tavily-web playwright-cli find-docs agent-dispatch; do
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
      "Tavily web search"
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

  local skill_dir="$repo_dir/.agent-layer/skills/tavily-web"
  assert_file_exists "$skill_dir/SKILL.md" \
    "scripted wizard installed tavily-web catalog skill"
  assert_file_contains "$skill_dir/SKILL.md" "Use the Tavily CLI" \
    "tavily-web catalog skill has real embedded content"
  for id in playwright-cli find-docs agent-dispatch; do
    assert_file_not_exists "$repo_dir/.agent-layer/skills/$id/SKILL.md" \
      "$id catalog skill remains absent when unselected"
  done

  # Doctor flags missing `tvly` binary when tavily-web is present and binary
  # is not on PATH. Keep this PATH hermetic so a host-installed tvly cannot
  # turn the missing-binary assertion into a machine-dependent result.
  local doctor_output rc=0
  doctor_output=$(cd "$repo_dir" && PATH="$E2E_INSTALL_PREFIX/bin" al doctor 2>&1) || rc=$?
  if [[ $rc -ne 0 ]]; then
    pass "doctor exits nonzero when tavily-web binary is missing"
  else
    fail "doctor should exit nonzero when tavily-web binary is missing"
  fi
  assert_output_contains "$doctor_output" "[FAIL]" \
    "doctor reports failure when tavily-web binary is missing"
  assert_output_contains "$doctor_output" "tavily-web" \
    "doctor mentions tavily-web when binary is missing"
  assert_output_contains "$doctor_output" "tvly" \
    "doctor mentions missing tvly binary"
  assert_output_contains "$doctor_output" "Some checks failed" \
    "doctor prints failure summary for missing tavily-web binary"
  assert_output_not_contains "$doctor_output" "All systems go" \
    "doctor does not print healthy summary for missing tavily-web binary"

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
  if [[ $rc_ok -eq 0 ]]; then
    pass "doctor exits zero when tvly is on PATH"
  else
    fail "doctor should exit zero when tvly is on PATH (rc=$rc_ok)"
  fi
  if grep -qF -- "tavily-web found" <<<"$doctor_output_ok"; then
    pass "doctor reports tavily-web found when tvly is on PATH"
  else
    fail "doctor did not report tavily-web found; rc=$rc_ok"
    echo "$doctor_output_ok" | head -20 | sed 's/^/    /'
  fi
  assert_output_not_contains "$doctor_output_ok" "[FAIL]" \
    "doctor has no failures when tvly is on PATH"

  # Removing the catalog skill directory makes doctor stop reporting tavily-web.
  rm -rf "$skill_dir"
  local doctor_output_silent rc_silent=0
  doctor_output_silent=$(cd "$repo_dir" && PATH="$E2E_INSTALL_PREFIX/bin" al doctor 2>&1) || rc_silent=$?
  if [[ $rc_silent -eq 0 ]]; then
    pass "doctor exits zero after tavily-web directory removal"
  else
    fail "doctor should exit zero after tavily-web directory removal (rc=$rc_silent)"
  fi
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
