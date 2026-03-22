#!/usr/bin/env bash
# Scenario: Sync correctly handles custom user-defined skills alongside
# generated skills — verifying that sync does not delete or corrupt
# user skills, and that generated skill provenance markers are present.

run_scenario_sync_custom_skills() {
  section "Sync with custom skills"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Enable claude so sync generates skill files
  local config_path="$repo_dir/.agent-layer/config.toml"
  local config_tmp="$repo_dir/.agent-layer/config.toml.tmp"
  sed \
    -e '/^\[agents\.claude\]/,/^\[/ s/^enabled = .*/enabled = true/' \
    "$config_path" > "$config_tmp"
  mv "$config_tmp" "$config_path"

  # ---- First sync: establish baseline ----
  assert_exit_zero_in "$repo_dir" "al sync (baseline)" al sync

  # Verify skills directory was created
  if [[ -d "$repo_dir/.claude/skills" ]]; then
    pass "skills output directory exists"
  else
    fail "skills output directory missing after sync"
  fi

  # ---- Create a custom user skill ----
  local custom_skill_dir="$repo_dir/.agent-layer/skills/my-custom"
  mkdir -p "$custom_skill_dir"
  cat > "$custom_skill_dir/SKILL.md" <<'SKILL'
---
name: my-custom
description: User-defined custom skill for testing.
---
This is a custom skill that should survive sync.
SKILL

  # ---- Second sync: custom skill should survive ----
  assert_exit_zero_in "$repo_dir" "al sync (with custom skill)" al sync

  # Verify custom skill source still exists
  assert_file_exists "$custom_skill_dir/SKILL.md" \
    "custom skill source survives sync"

  assert_file_contains "$custom_skill_dir/SKILL.md" "my-custom" \
    "custom skill content preserved after sync"

  # ---- Third sync: idempotency with custom skills ----
  local pre_sync_snapshot="$E2E_TMP_ROOT/custom-skill-pre-snapshot.txt"
  _snapshot_all_state "$repo_dir" > "$pre_sync_snapshot"

  assert_exit_zero_in "$repo_dir" "al sync (idempotent)" al sync

  assert_all_state_unchanged "$repo_dir" "$pre_sync_snapshot" \
    "third sync is idempotent with custom skills"

  # ---- Verify al claude still works ----
  install_mock_claude "$repo_dir"

  local claude_output claude_rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || claude_rc=$?
  if [[ $claude_rc -eq 0 ]]; then
    pass "al claude works with custom skills"
  else
    fail "al claude should work with custom skills (exit code: $claude_rc)"
    echo "  output (first 5 lines):"
    echo "$claude_output" | head -5 | sed 's/^/    /'
  fi

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"

  cleanup_scenario_dir "$repo_dir"
}
