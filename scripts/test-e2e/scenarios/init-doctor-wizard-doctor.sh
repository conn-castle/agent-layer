#!/usr/bin/env bash
# Scenario: bare init, doctor, deterministic profile wizard/sync, and doctor.

_init_doctor_wizard_count_occurrences() {
  local output="$1" needle="$2"
  awk -v needle="$needle" '
    {
      line = $0
      while ((idx = index(line, needle)) > 0) {
        count++
        line = substr(line, idx + length(needle))
      }
    }
    END { print count + 0 }
  ' <<<"$output"
}

_init_doctor_wizard_assert_files_identical() {
  local expected="$1" actual="$2" label="$3"
  if cmp -s "$expected" "$actual"; then
    pass "$label"
  else
    fail "$label (files differ: expected $expected, actual $actual)"
    diff -u "$expected" "$actual" | head -40 | sed 's/^/    /' || true
  fi
}

_init_doctor_wizard_assert_dir_empty() {
  local dir="$1" label="$2"
  if [[ ! -d "$dir" ]]; then
    fail "$label (directory not found: $dir)"
    return
  fi
  if [[ -z "$(find "$dir" -mindepth 1 -print -quit)" ]]; then
    pass "$label"
  else
    fail "$label (directory is not empty: $dir)"
    find "$dir" -mindepth 1 -maxdepth 2 -print | head -20 | sed 's/^/    /'
  fi
}

_init_doctor_wizard_assert_dir_not_exists() {
  local dir="$1" label="$2"
  if [[ -d "$dir" ]]; then
    fail "$label (directory unexpectedly exists: $dir)"
  else
    pass "$label"
  fi
}

_init_doctor_wizard_assert_compact_json_equals() {
  local file="$1" expected="$2" label="$3"
  if [[ ! -f "$file" ]]; then
    fail "$label (file not found: $file)"
    return
  fi
  local actual
  actual="$(tr -d '[:space:]' < "$file")"
  assert_output_equals "$actual" "$expected" "$label"
}

_init_doctor_wizard_assert_no_enabled_statusline() {
  local file="$1" label="$2"
  if [[ ! -f "$file" ]]; then
    fail "$label (file not found: $file)"
    return
  fi
  if grep -Eq '^[[:space:]]*statusline[[:space:]]*=[[:space:]]*true([[:space:]]*(#.*)?)?$' "$file"; then
    fail "$label (found uncommented statusline = true)"
  else
    pass "$label"
  fi
}

_init_doctor_wizard_assert_expected_doctor_output() {
  local output="$1" phase="$2"
  local expected_instruction_summary="$3"

  assert_no_crash_markers "$output" "$phase doctor output has no crash markers"
  assert_output_contains "$output" "Checking Agent Layer health" \
    "$phase doctor output has health header"
  assert_output_contains "$output" "Directory exists: .agent-layer" \
    "$phase doctor verifies .agent-layer"
  assert_output_not_contains "$output" "Missing optional directory: docs/agent-layer" \
    "$phase doctor does not warn for absent optional docs"
  assert_output_contains "$output" "Configuration loaded successfully" \
    "$phase doctor config check passed"
  assert_output_contains "$output" "Update check skipped because AL_NO_NETWORK is set" \
    "$phase doctor reports expected offline update warning"
  assert_output_contains "$output" "No required secrets found in configuration." \
    "$phase doctor secrets check passed"
  assert_output_contains "$output" "Agent enabled: Claude" \
    "$phase doctor reports Claude enabled"
  assert_output_contains "$output" "Agent enabled: ClaudeVSCode" \
    "$phase doctor reports Claude VS Code enabled"
  assert_output_contains "$output" "Agent enabled: Codex" \
    "$phase doctor reports Codex enabled"
  assert_output_contains "$output" "Agent enabled: VSCode" \
    "$phase doctor reports VS Code enabled"
  assert_output_contains "$output" "Agent enabled: CopilotCLI" \
    "$phase doctor reports Copilot CLI enabled"
  assert_output_contains "$output" "Agent disabled: Antigravity" \
    "$phase doctor reports Antigravity disabled"
  assert_output_contains "$output" "No skills configured for validation." \
    "$phase doctor reports no skills"
  assert_output_contains "$output" "Running warning checks" \
    "$phase doctor runs warning checks"
  assert_output_contains "$output" "Context size summary" \
    "$phase doctor prints context summary"
  assert_output_contains "$output" "$expected_instruction_summary" \
    "$phase doctor reports expected instruction source"
  assert_output_contains "$output" "All systems go" \
    "$phase doctor summary is healthy"
  assert_output_not_contains "$output" "[FAIL]" \
    "$phase doctor output has no FAIL results"
  assert_output_not_contains "$output" "Some checks failed" \
    "$phase doctor output has no failure summary"

  local warn_count
  warn_count="$(_init_doctor_wizard_count_occurrences "$output" "[WARN]")"
  if [[ "$warn_count" == "1" ]]; then
    pass "$phase doctor has only expected warnings"
  else
    fail "$phase doctor warning count: expected 1, got $warn_count"
    echo "$output" | grep -F "[WARN]" | sed 's/^/    /' || true
  fi
}

_init_doctor_wizard_assert_bare_init_files() {
  local repo_dir="$1"

  assert_al_init_structure "$repo_dir"
  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"
  assert_dir_exists "$repo_dir/.agent-layer/tmp/runs" \
    "init created tmp/runs/"
  assert_file_exists "$repo_dir/.agent-layer/.gitignore" \
    "init created .agent-layer/.gitignore"
  assert_file_contains "$repo_dir/.agent-layer/.gitignore" "config.toml.bak" \
    ".agent-layer/.gitignore ignores wizard backups"
  assert_file_exists "$repo_dir/.agent-layer/gitignore.block" \
    "init created gitignore.block"
  assert_file_contains "$repo_dir/.agent-layer/gitignore.block" "/AGENTS.md" \
    "gitignore.block ignores generated AGENTS.md"
  assert_file_contains "$repo_dir/.gitignore" "Agent Layer-generated instruction shims" \
    ".gitignore includes Agent Layer generated-files block"
  assert_file_contains "$repo_dir/.agent-layer/.env" "AL_GITHUB_PERSONAL_ACCESS_TOKEN=" \
    ".env has GitHub token placeholder"
  assert_file_contains "$repo_dir/.agent-layer/commands.allow" "git status" \
    "commands.allow has command allowlist"
  assert_file_contains "$repo_dir/.agent-layer/open-vscode.command" "al vscode --no-sync" \
    "macOS VS Code launcher delegates to al vscode"
  assert_file_contains "$repo_dir/.agent-layer/open-vscode.sh" "al vscode --no-sync" \
    "Linux VS Code launcher delegates to al vscode"
  assert_file_contains "$repo_dir/.agent-layer/open-vscode.desktop" "open-vscode.sh" \
    "desktop launcher delegates to shell launcher"
  assert_file_contains "$repo_dir/.agent-layer/open-vscode.app/Contents/Info.plist" \
    "com.agent-layer.open-vscode" \
    "macOS app bundle has expected bundle id"
  assert_file_contains "$repo_dir/.agent-layer/open-vscode.app/Contents/MacOS/open-vscode" \
    "al vscode --no-sync" \
    "macOS app executable delegates to al vscode"
  _init_doctor_wizard_assert_files_identical "$E2E_DEFAULTS_TOML" \
    "$repo_dir/.agent-layer/config.toml" \
    "init config.toml matches default template"

  _init_doctor_wizard_assert_dir_empty "$repo_dir/.agent-layer/instructions" \
    "init leaves instructions directory empty"
  _init_doctor_wizard_assert_dir_empty "$repo_dir/.agent-layer/skills" \
    "init leaves skills directory empty"

  for name in 00_rules.md 01_base.md 02_memory.md 03_tools.md 04_conventions.md; do
    assert_file_not_exists "$repo_dir/.agent-layer/instructions/$name" \
      "$name is not seeded by bare init"
  done
  for name in ISSUES.md BACKLOG.md ROADMAP.md DECISIONS.md COMMANDS.md CONTEXT.md; do
    assert_file_not_exists "$repo_dir/docs/agent-layer/$name" \
      "$name is not seeded by bare init"
    assert_file_not_exists "$repo_dir/.agent-layer/templates/docs/$name" \
      "$name template is not seeded by bare init"
  done
  assert_file_not_exists "$repo_dir/.agent-layer/claude-statusline.sh" \
    "Claude statusline source is not seeded by bare init"
  assert_file_not_exists "$repo_dir/.agent-layer/codex-statusline.toml" \
    "Codex statusline source is not seeded by bare init"

  for rel_path in \
    "AGENTS.md" \
    "CLAUDE.md" \
    ".github/copilot-instructions.md" \
    ".codex/AGENTS.md" \
    ".claude/settings.json" \
    ".mcp.json" \
    ".codex/config.toml" \
    ".codex/rules/default.rules" \
    ".copilot/mcp-config.json" \
    ".vscode/mcp.json" \
    ".vscode/settings.json"; do
    assert_file_not_exists "$repo_dir/$rel_path" \
      "$rel_path is not generated before wizard/sync"
  done
  _init_doctor_wizard_assert_dir_not_exists "$repo_dir/.agents/skills" \
    ".agents/skills is not generated before wizard/sync"
  _init_doctor_wizard_assert_dir_not_exists "$repo_dir/.claude/skills" \
    ".claude/skills is not generated before wizard/sync"
}

_init_doctor_wizard_assert_post_wizard_files() {
  local repo_dir="$1"

  _init_doctor_wizard_assert_files_identical "$E2E_DEFAULTS_TOML" \
    "$repo_dir/.agent-layer/config.toml" \
    "wizard profile leaves config.toml at default profile content"
  _init_doctor_wizard_assert_files_identical "$E2E_DEFAULTS_TOML" \
    "$repo_dir/.agent-layer/config.toml.bak" \
    "wizard backup captures pre-profile config"

  assert_file_contains "$repo_dir/.agent-layer/config.toml" 'mode = "all"' \
    "wizard config keeps approval mode all"
  assert_file_contains "$repo_dir/.agent-layer/config.toml" "[agents.claude]" \
    "wizard config keeps Claude section"
  assert_file_contains "$repo_dir/.agent-layer/config.toml" "[agents.codex]" \
    "wizard config keeps Codex section"
  _init_doctor_wizard_assert_no_enabled_statusline "$repo_dir/.agent-layer/config.toml" \
    "wizard default profile keeps statuslines disabled unless explicit"

  assert_generated_artifacts "$repo_dir"
  assert_file_contains "$repo_dir/.mcp.json" '"_generatedBy"' \
    ".mcp.json has provenance marker"
  assert_file_not_contains "$repo_dir/.mcp.json" '"context7"' \
    ".mcp.json has no default MCP servers enabled"
  _init_doctor_wizard_assert_compact_json_equals "$repo_dir/.copilot/mcp-config.json" '{"mcpServers":{}}' \
    "Copilot CLI MCP config has no default servers"
  _init_doctor_wizard_assert_compact_json_equals "$repo_dir/.vscode/mcp.json" '{"servers":{}}' \
    "VS Code MCP config has no default servers"
  assert_file_contains "$repo_dir/.claude/settings.json" '"permissions"' \
    "settings.json has permissions block"
  assert_file_not_contains "$repo_dir/.claude/settings.json" "mcp__context7__" \
    "settings.json has no default MCP permissions"
  assert_file_contains "$repo_dir/.codex/config.toml" "GENERATED FILE" \
    "Codex config has managed marker"
  assert_file_contains "$repo_dir/.codex/config.toml" 'trust_level = "trusted"' \
    "Codex config trusts the scenario repo"
  assert_file_contains "$repo_dir/.codex/rules/default.rules" "Source: .agent-layer/commands.allow" \
    "Codex rules are generated from commands.allow"
  assert_file_contains "$repo_dir/.codex/rules/default.rules" 'prefix_rule(pattern=["git", "status"]' \
    "Codex rules include command allowlist entries"
  assert_file_contains "$repo_dir/.vscode/settings.json" "chat.tools.terminal.autoApprove" \
    "VS Code settings include command approvals"
  assert_file_contains "$repo_dir/.vscode/settings.json" "chat.agentSkillsLocations" \
    "VS Code settings include skill locations"
  assert_file_contains "$repo_dir/.vscode/settings.json" '".agents/skills": true' \
    "VS Code settings enable shared skills"
  assert_file_contains "$repo_dir/.vscode/settings.json" '".claude/skills": false' \
    "VS Code settings disable project Claude skills"
  _init_doctor_wizard_assert_dir_empty "$repo_dir/.agents/skills" \
    "shared skills directory is empty without skill sources"
  _init_doctor_wizard_assert_dir_empty "$repo_dir/.claude/skills" \
    "Claude skills directory is empty without skill sources"
  assert_file_not_exists "$repo_dir/.claude/claude-statusline.sh" \
    "Claude statusline projection is absent by default"
  assert_file_not_exists "$repo_dir/.agent-layer/claude-statusline.sh" \
    "Claude statusline source remains absent by default"
  assert_file_not_exists "$repo_dir/.agent-layer/codex-statusline.toml" \
    "Codex statusline source remains absent by default"

  _init_doctor_wizard_assert_dir_empty "$repo_dir/.agent-layer/instructions" \
    "wizard default profile leaves instructions directory empty"
  _init_doctor_wizard_assert_dir_empty "$repo_dir/.agent-layer/skills" \
    "wizard default profile leaves skills directory empty"
}

run_scenario_init_doctor_wizard_doctor() {
  section "Init + doctor + profile wizard + doctor lifecycle"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  local init_output init_rc=0
  init_output=$(cd "$repo_dir" && al init 2>&1) || init_rc=$?
  if [[ $init_rc -eq 0 ]]; then
    pass "al init"
  else
    fail "al init (exit code: $init_rc)"
    echo "$init_output" | head -10 | sed 's/^/    /'
  fi
  assert_no_crash_markers "$init_output" "init output has no crash markers"
  assert_output_not_contains "$init_output" "[WARN]" \
    "init output has no warning rows"
  assert_output_not_contains "$init_output" "Warning:" \
    "init output has no warnings"
  assert_output_not_contains "$init_output" "Error:" \
    "init output has no errors"
  assert_output_not_contains "$init_output" "Wizard completed" \
    "non-interactive init does not run wizard"

  _init_doctor_wizard_assert_bare_init_files "$repo_dir"

  local pre_doctor_snapshot="$E2E_TMP_ROOT/init-doctor-pre-snapshot.txt"
  _snapshot_all_state "$repo_dir" > "$pre_doctor_snapshot"

  local doctor_output doctor_rc=0
  doctor_output=$(cd "$repo_dir" && al doctor 2>&1) || doctor_rc=$?
  if [[ $doctor_rc -eq 0 ]]; then
    pass "al doctor after init exits zero"
  else
    fail "al doctor after init exits zero (exit code: $doctor_rc)"
    echo "$doctor_output" | head -20 | sed 's/^/    /'
  fi
  _init_doctor_wizard_assert_expected_doctor_output "$doctor_output" "after init" \
    "Instructions (.agent-layer/instructions/*): 0 / 10000 tokens"
  assert_all_state_unchanged "$repo_dir" "$pre_doctor_snapshot" \
    "doctor after init is read-only"

  local wizard_output wizard_rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --profile "$E2E_DEFAULTS_TOML" --yes 2>&1) || wizard_rc=$?
  if [[ $wizard_rc -eq 0 ]]; then
    pass "al wizard --profile defaults.toml --yes"
  else
    fail "al wizard --profile defaults.toml --yes (exit code: $wizard_rc)"
    echo "$wizard_output" | head -20 | sed 's/^/    /'
  fi
  assert_no_crash_markers "$wizard_output" "wizard output has no crash markers"
  assert_output_contains "$wizard_output" "Profile matches current config" \
    "wizard output confirms profile matched init config"
  assert_output_contains "$wizard_output" "Running sync" \
    "wizard output says sync ran"
  assert_output_contains "$wizard_output" "Wizard completed" \
    "wizard output says completed"
  assert_output_not_contains "$wizard_output" "Warning:" \
    "wizard output has no warnings"

  _init_doctor_wizard_assert_post_wizard_files "$repo_dir"

  local post_wizard_doctor_snapshot="$E2E_TMP_ROOT/init-doctor-post-wizard-snapshot.txt"
  _snapshot_all_state "$repo_dir" > "$post_wizard_doctor_snapshot"

  local doctor_after_wizard_output doctor_after_wizard_rc=0
  doctor_after_wizard_output=$(cd "$repo_dir" && al doctor 2>&1) || doctor_after_wizard_rc=$?
  if [[ $doctor_after_wizard_rc -eq 0 ]]; then
    pass "al doctor after wizard exits zero"
  else
    fail "al doctor after wizard exits zero (exit code: $doctor_after_wizard_rc)"
    echo "$doctor_after_wizard_output" | head -20 | sed 's/^/    /'
  fi
  _init_doctor_wizard_assert_expected_doctor_output "$doctor_after_wizard_output" "after wizard" \
    "Instructions (AGENTS.md): 0 / 10000 tokens"
  assert_all_state_unchanged "$repo_dir" "$post_wizard_doctor_snapshot" \
    "doctor after wizard is read-only"

  cleanup_scenario_dir "$repo_dir"
}
