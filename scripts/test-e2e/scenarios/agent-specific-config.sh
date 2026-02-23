#!/usr/bin/env bash
# Scenario 140: Agent-specific config passthrough for Codex and Claude.

run_scenario_agent_specific_config() {
  section "Agent-specific config passthrough"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  local config_path="$repo_dir/.agent-layer/config.toml"

  cat >> "$config_path" <<'EOF'

[agents.codex.agent_specific]
approval_policy = "never"

[agents.codex.agent_specific.features]
multi_agent = true
prevent_idle_sleep = true

[agents.claude.agent_specific]
permissions = { allow = ["Bash(ls:*)"] }

[agents.claude.agent_specific.features]
multi_agent = true
EOF

  assert_file_contains "$config_path" "[agents.codex.agent_specific]" \
    "codex agent-specific section added"
  assert_file_contains "$config_path" "prevent_idle_sleep = true" \
    "codex agent-specific feature added"
  assert_file_contains "$config_path" "[agents.claude.agent_specific]" \
    "claude agent-specific section added"
  assert_file_contains "$config_path" "permissions = { allow = [\"Bash(ls:*)\"] }" \
    "claude agent-specific permissions added"

  local sync_output sync_rc=0
  sync_output=$(cd "$repo_dir" && al sync 2>&1) || sync_rc=$?
  if [[ $sync_rc -ne 0 ]]; then
    pass "al sync exits with warnings for agent-specific overrides"
  else
    fail "al sync should return nonzero when warnings are emitted"
  fi

  assert_output_contains "$sync_output" "POLICY_AGENT_SPECIFIC_OVERRIDES" \
    "sync output includes agent-specific override warning"
  assert_output_contains "$sync_output" "agents.codex.agent_specific" \
    "sync output includes codex agent-specific subject"
  assert_output_contains "$sync_output" "agents.claude.agent_specific" \
    "sync output includes claude agent-specific subject"

  assert_file_contains "$repo_dir/.codex/config.toml" "[features]" \
    "codex config includes features table"
  assert_file_contains "$repo_dir/.codex/config.toml" "multi_agent = true" \
    "codex config includes multi_agent"
  assert_file_contains "$repo_dir/.codex/config.toml" "prevent_idle_sleep = true" \
    "codex config includes prevent_idle_sleep"
  assert_file_contains "$repo_dir/.codex/config.toml" "approval_policy = 'never'" \
    "codex config includes agent-specific approval_policy"

  assert_file_contains "$repo_dir/.claude/settings.json" "\"features\"" \
    "claude settings includes features"
  assert_file_contains "$repo_dir/.claude/settings.json" "multi_agent" \
    "claude settings includes multi_agent"
  assert_file_contains "$repo_dir/.claude/settings.json" "Bash(ls:*)" \
    "claude settings includes agent-specific permissions"

  cleanup_scenario_dir "$repo_dir"
}
