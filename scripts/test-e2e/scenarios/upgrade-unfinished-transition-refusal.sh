#!/usr/bin/env bash
# Exercise the supported-source boundary and pristine conversion with authentic published release bytes.
run_scenario_upgrade_unfinished_transition_refusal() {
  section "Historical transition boundary, pristine conversion, and user-managed preservation"
  if [[ ! -x "${E2E_PREFLOOR_BINARY:-}" || ! -x "${E2E_TRANSITION_BINARY:-}" ]]; then
    if [[ "${AL_E2E_REQUIRE_UPGRADE:-}" == "1" ]]; then
      fail "0.24.3 and 1.0.0 binaries are required for the transition boundary"
    else
      warn "transition boundary skipped: cache release 0.24.3 and 1.0.0 binaries (AL_E2E_ONLINE=1)"
    fi
    return
  fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))
  local repo_dir before after output rc legacy
  repo_dir="$(setup_scenario_dir)"
  setup_old_version_via_binary "$repo_dir" "$E2E_PREFLOOR_BINARY"
  local answers="$E2E_TMP_ROOT/pre-floor-answers.json"
  cat > "$answers" <<'ANSWERS_EOF'
{
  "select": {
    "Approval Mode": "all - Auto-approve shell commands and MCP tool calls (where supported).",
    "Agent instructions": "None — no Agent Layer instruction files"
  },
  "multi_select": {
    "Enable Agents": [],
    "Track the following Agent Layer folders in git? (checked = tracked; unchecked = gitignored)": [],
    "Enable skills": ["Agent Layer development skills (/implement, /ship-pr, etc.)"],
    "Enable Default MCP Servers": []
  },
  "confirm": {
    "Enable warnings for performance and usage issues?": false,
    "Apply these config, secret, skills, instructions, memory-file, gitignore-source, and statusline-source changes?": true
  }
}
ANSWERS_EOF
  assert_exit_zero_in "$repo_dir" "0.24.3 wizard installs pristine moved skills" \
    env AL_VERSION=0.24.3 "$E2E_PREFLOOR_BINARY" wizard --answers "$answers"
  legacy="$repo_dir/.agent-layer/skills/implement/SKILL.md"
  assert_file_exists "$legacy" "0.24.3 provides pristine moved skill bytes"
  if [[ ! -f "$legacy" ]]; then return; fi
  before="$E2E_TMP_ROOT/transition-before.txt"
  after="$E2E_TMP_ROOT/transition-after.txt"
  snapshot_transition_tree "$repo_dir" > "$before"
  rc=0
  output=$(cd "$repo_dir" && al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions 2>&1) || rc=$?
  if [[ "$rc" -ne 0 ]]; then pass "direct 0.24.3 upgrade refused"; else fail "direct pre-floor upgrade was accepted"; fi
  snapshot_transition_tree "$repo_dir" > "$after"
  assert_exit_zero "pre-floor refusal preserves complete tree" cmp "$before" "$after"
  assert_output_contains "$output" 'releases/download/v1.0.0/al-install.sh | bash -s -- --version v1.0.0 --prefix "$tmp" --no-completions' \
    "pre-floor refusal gives the exact v1.0.0 install command"
  assert_output_contains "$output" '"$tmp/bin/al" upgrade' "pre-floor refusal gives the exact v1.0.0 upgrade command"

  assert_exit_zero_in "$repo_dir" "1.0.0 completes historical upgrade" \
    env AL_VERSION=1.0.0 "$E2E_TRANSITION_BINARY" upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions
  assert_al_version_content "$repo_dir" "1.0.0"
  assert_file_exists "$legacy" "1.0.0 preserves the pristine moved skill"

  # Adoption evidence is genuinely blocking: refuse before any write.
  local blocked_dir
  blocked_dir="$(setup_scenario_dir)"
  cp -a "$repo_dir/." "$blocked_dir/"
  mkdir -p "$blocked_dir/.agent-layer/skills-imported/.staging"
  printf '{"version":2}\n' > "$blocked_dir/.agent-layer/skills-imported/.staging/journal.json"
  snapshot_transition_tree "$blocked_dir" > "$before"
  rc=0
  output=$(cd "$blocked_dir" && al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions 2>&1) || rc=$?
  if [[ "$rc" -ne 0 ]]; then pass "adoption journal refused"; else fail "adoption journal was accepted"; fi
  snapshot_transition_tree "$blocked_dir" > "$after"
  assert_exit_zero "adoption journal refusal preserves complete tree" cmp "$before" "$after"
  assert_output_contains "$output" "AL_VERSION=1.1.0 al sync" "adoption journal refusal gives pin-correct recovery"
  assert_output_not_contains "$output" "v1.0.0 executable" "1.0.0 project is not sent back to v1.0.0"
  cleanup_scenario_dir "$blocked_dir"

  # Customize every moved source retained by 1.0.0; each is now user content.
  local customized_dir
  customized_dir="$(setup_scenario_dir)"
  cp -a "$repo_dir/." "$customized_dir/"
  local name
  for name in implement ship-pr auto-skill-loop audit-documentation audit-memory audit-tests interface-audit find-docs playwright tavily-web; do
    if [[ -f "$customized_dir/.agent-layer/skills/$name/SKILL.md" ]]; then
      printf '\nProject-specific workflow customization.\n' >> "$customized_dir/.agent-layer/skills/$name/SKILL.md"
    fi
  done
  for name in 00_rules.md 01_memory.md; do
    if [[ -f "$customized_dir/.agent-layer/instructions/$name" ]]; then
      printf '\nProject-specific instruction customization.\n' >> "$customized_dir/.agent-layer/instructions/$name"
    fi
  done
  cp "$customized_dir/.agent-layer/skills/implement/SKILL.md" "$E2E_TMP_ROOT/customized-implement.md"
  assert_exit_zero_in "$customized_dir" "customized moved-name copies upgrade successfully" \
    al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions
  assert_exit_zero "customized skill bytes survive deletion-enabled upgrade" \
    cmp "$E2E_TMP_ROOT/customized-implement.md" "$customized_dir/.agent-layer/skills/implement/SKILL.md"
  cleanup_scenario_dir "$customized_dir"

  # Unmodified shipped copies convert to the imports a fresh setup creates.
  setup_catalog_git_fixture
  assert_exit_zero_in "$repo_dir" "1.0.0 project with pristine copies upgrades" \
    al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions
  assert_file_not_exists "$legacy" "pristine local copy removed after conversion"
  for name in implement ship-pr auto-skill-loop audit-documentation audit-memory audit-tests interface-audit; do
    assert_file_exists "$repo_dir/.agent-layer/skills-imported/$name/SKILL.md" "$name imported from the catalog"
  done
  assert_file_exists "$repo_dir/.agent-layer/skills.lock.json" "conversion records the skills lock"
  assert_exit_zero_in "$repo_dir" "al doctor is clean after conversion" al doctor
  cleanup_scenario_dir "$repo_dir"
}

# Include dirents and modes as well as checksums; do not follow symbolic links.
snapshot_transition_tree() {
  local dir="$1"
  (
    cd "$dir"
    find . -not -path './.git/*' -not -path './.git' -print | LC_ALL=C sort | while IFS= read -r path; do
      LC_ALL=C ls -ld "$path" | awk '{print $1, $NF}'
      if [[ -f "$path" && ! -L "$path" ]]; then portable_sha256 "$path"; fi
    done
  )
}
