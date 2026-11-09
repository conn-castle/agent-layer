#!/usr/bin/env bash
# A project created before v1.0.0 and upgraded through 1.0.0 and 1.1.0 without
# wizard adoption holds unmodified shipped rules, memory, and development skills.
run_scenario_upgrade_v110_pristine_conversion() {
  section "Upgrade a 1.1.0 project by converting unmodified shipped copies to Git imports"
  if [[ ! -x "${E2E_PREFLOOR_BINARY:-}" || ! -x "${E2E_TRANSITION_BINARY:-}" || ! -x "${E2E_V110_BINARY:-}" ]]; then
    if [[ "${AL_E2E_REQUIRE_UPGRADE:-}" == "1" ]]; then
      fail "0.24.3, 1.0.0, and 1.1.0 binaries are required for 1.1.0 pristine conversion"
    else
      warn "1.1.0 pristine conversion skipped: cache release 0.24.3, 1.0.0, and 1.1.0 binaries (AL_E2E_ONLINE=1)"
    fi
    return
  fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))
  local repo_dir output name
  repo_dir="$(setup_scenario_dir)"
  setup_old_version_via_binary "$repo_dir" "$E2E_PREFLOOR_BINARY"
  local answers="$E2E_TMP_ROOT/v110-conversion-answers.json"
  cat > "$answers" <<'ANSWERS_EOF'
{
  "select": {
    "Approval Mode": "all - Auto-approve shell commands and MCP tool calls (where supported).",
    "Agent instructions": "Rules and memory — rules, plus docs/agent-layer/ (CONTEXT, DECISIONS, ISSUES, BACKLOG, COMMANDS)"
  },
  "multi_select": {
    "Enable Agents": [],
    "Track the following Agent Layer folders in git? (checked = tracked; unchecked = gitignored)": [".agent-layer/", "docs/agent-layer/"],
    "Enable skills": ["Agent Layer development skills (/implement, /ship-pr, etc.)"],
    "Enable Default MCP Servers": []
  },
  "confirm": {
    "Enable warnings for performance and usage issues?": false,
    "Apply these config, secret, skills, instructions, memory-file, gitignore-source, and statusline-source changes?": true
  }
}
ANSWERS_EOF
  assert_exit_zero_in "$repo_dir" "0.24.3 wizard installs rules, memory, and development skills" \
    env AL_VERSION=0.24.3 "$E2E_PREFLOOR_BINARY" wizard --answers "$answers"
  assert_exit_zero_in "$repo_dir" "1.0.0 upgrade without adoption" \
    env AL_VERSION=1.0.0 "$E2E_TRANSITION_BINARY" upgrade --yes --apply-managed-updates --apply-memory-updates
  assert_exit_zero_in "$repo_dir" "1.1.0 upgrade without adoption" \
    env AL_VERSION=1.1.0 "$E2E_V110_BINARY" upgrade --yes --apply-managed-updates --apply-memory-updates
  assert_al_version_content "$repo_dir" "1.1.0"
  assert_file_exists "$repo_dir/.agent-layer/instructions/00_rules.md" "1.1.0 keeps the shipped rules"
  assert_file_exists "$repo_dir/.agent-layer/skills/implement/SKILL.md" "1.1.0 keeps the shipped implement skill"

  setup_catalog_git_fixture
  output=$(cd "$repo_dir" && al upgrade plan 2>&1) || true
  assert_output_contains "$output" "requires network access to github.com/nicholasjconn/skills" "upgrade plan notes network access"
  assert_output_contains "$output" ".agent-layer/instructions/00_rules.md -> instructions/rules.md" "upgrade plan lists the rules conversion"
  assert_output_contains "$output" ".agent-layer/skills/ship-pr -> skills/development/ship-pr" "upgrade plan lists the ship-pr conversion"

  assert_exit_zero_in "$repo_dir" "1.1.0 project upgrades without the wizard" \
    al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions
  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"
  for name in 00_rules.md 01_memory.md; do
    assert_file_not_exists "$repo_dir/.agent-layer/instructions/$name" "$name local copy removed"
  done
  for name in rules.md memory.md; do
    assert_file_exists "$repo_dir/.agent-layer/instructions-imported/$name" "$name imported"
  done
  for name in implement ship-pr auto-skill-loop audit-documentation audit-memory audit-tests interface-audit; do
    assert_file_not_exists "$repo_dir/.agent-layer/skills/$name/SKILL.md" "$name local copy removed"
    assert_file_exists "$repo_dir/.agent-layer/skills-imported/$name/SKILL.md" "$name imported"
  done
  assert_file_contains "$repo_dir/AGENTS.md" "Follow project conventions. Validate changes." "imported rules are projected"
  assert_exit_zero_in "$repo_dir" "al doctor is clean after conversion" al doctor
  cleanup_scenario_dir "$repo_dir"
}
