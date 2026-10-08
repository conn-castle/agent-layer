#!/usr/bin/env bash
# A genuine old release initializes the project. Current v1 preserves a retired
# local tree during deletion-enabled upgrade, then explicitly adopts it.
run_scenario_upgrade_legacy_adoption() {
  section "Old release upgrade preserves legacy customization; wizard adopts it"
  if skip_if_no_latest_binary; then return; fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))
  setup_catalog_git_fixture

  local repo_dir legacy imported expected source_commit
  repo_dir="$(setup_scenario_dir)"
  setup_old_version_via_binary "$repo_dir" "$E2E_LATEST_BINARY"
  assert_al_version_content "$repo_dir" "$E2E_LATEST_VERSION"
  legacy="$repo_dir/.agent-layer/skills/implement"
  imported="$repo_dir/.agent-layer/skills-imported/implement"
  expected="$(mktemp -d "$E2E_TMP_ROOT/legacy-evidence.XXXXXX")"
  mkdir -p "$legacy/references"
  # Minimal hermetic content, never a second copy of a canonical moved body.
  (set -o noclobber
    printf -- '---\nname: implement\ndescription: Customized legacy fixture.\n---\n\n' > "$expected/SKILL.md"
    for index in 1 2 3 4 5 6 7 8; do
      printf 'Full local workflow line %s, with CRLF and exact trailing spaces.  \r\n' "$index"
    done >> "$expected/SKILL.md"
    printf 'Repository-specific policy bytes.\r\n' > "$expected/policy.txt"
    printf 'Executable local helper.\n' > "$expected/helper.sh"
  )
  chmod 0644 "$expected/SKILL.md" "$expected/policy.txt"
  chmod 0755 "$expected/helper.sh"
  cp -p "$expected/SKILL.md" "$legacy/SKILL.md"
  cp -p "$expected/policy.txt" "$legacy/references/policy.txt"
  cp -p "$expected/helper.sh" "$legacy/references/helper.sh"

  assert_exit_zero_in "$repo_dir" "v1 upgrade preserves retired local tree" \
    al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions
  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"
  assert_exit_zero "upgrade preserves complete legacy body" cmp "$expected/SKILL.md" "$legacy/SKILL.md"
  assert_exit_zero "upgrade preserves extra local policy" cmp "$expected/policy.txt" "$legacy/references/policy.txt"
  assert_exit_zero "upgrade preserves local file modes" diff <(LC_ALL=C ls -l "$expected/helper.sh" | awk '{print $1}') <(LC_ALL=C ls -l "$legacy/references/helper.sh" | awk '{print $1}')
  assert_file_not_exists "$imported/SKILL.md" "upgrade does not implicitly adopt"

  local answers_file="$repo_dir/adoption-answers.json"
  (set -o noclobber
    cat > "$answers_file" <<'JSON'
{
  "select": {
    "Approval Mode": "all - Auto-approve shell commands and MCP tool calls (where supported).",
    "Agent instructions": "None — no Agent Layer instruction files"
  },
  "multi_select": {
    "Enable Agents": [],
    "Track the following Agent Layer folders in git? (checked = tracked; unchecked = gitignored)": [],
    "Enable skills": ["Development skills (/implement, /ship-pr, etc.)"],
    "Enable Default MCP Servers": []
  },
  "confirm": {
    "Enable warnings for performance and usage issues?": true,
    "Apply these config, secret, skills, instructions, memory-file, gitignore-source, and statusline-source changes?": true
  }
}
JSON
  )
  local wizard_output wizard_rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --answers "$answers_file" 2>&1) || wizard_rc=$?
  assert_output_equals "$wizard_rc" "0" "explicit wizard adoption succeeds"
  if [[ "$wizard_rc" != 0 ]]; then
    echo "$wizard_output"
    echo "Retained failed adoption fixture: $repo_dir; $expected"
    return
  fi
  # Wizard unit tests cover preview wording; this binary path checks
  # the complete adoption result from a genuine old release.
  assert_output_contains "$wizard_output" "Wizard completed" "scripted adoption completes"
  assert_file_not_exists "$legacy/SKILL.md" "adoption retires the old active copy"
  assert_exit_zero "adoption preserves complete imported body" cmp "$expected/SKILL.md" "$imported/SKILL.md"
  assert_exit_zero "adoption preserves extra imported policy" cmp "$expected/policy.txt" "$imported/references/policy.txt"
  assert_exit_zero "adoption preserves executable helper bytes" cmp "$expected/helper.sh" "$imported/references/helper.sh"
  assert_exit_zero "adoption preserves executable helper mode" diff <(LC_ALL=C ls -l "$expected/helper.sh" | awk '{print $1}') <(LC_ALL=C ls -l "$imported/references/helper.sh" | awk '{print $1}')
  if [[ ! -e "$legacy" && ! -L "$legacy" ]]; then pass "only imported source slot remains"; else fail "legacy source slot survived adoption"; fi

  source_commit="$(git -C "$E2E_TMP_ROOT/catalog-git-source" rev-parse HEAD)"
  assert_file_contains "$repo_dir/.agent-layer/skills.lock.json" "$source_commit" "lock records genuine fetched fixture commit"
  assert_file_contains "$repo_dir/.agent-layer/skills.lock.json" '"selected_path": "skills/development/implement"' "lock records canonical selector"
  assert_file_contains "$repo_dir/.agent-layer/skills.lock.json" '"tree_hash": "sha256:' "lock records upstream canonical tree hash"
  local status_output
  status_output=$(cd "$repo_dir" && al skills status --all 2>&1)
  assert_output_contains "$status_output" "1 modified" "custom legacy copy is honestly modified"
  assert_output_contains "$status_output" "1 total" "unchanged partial bundle adopts only its legacy member"

  echo "Legacy adoption evidence: $repo_dir; $expected"
}
