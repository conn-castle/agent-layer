#!/usr/bin/env bats

# Tests for the sync generator and inspection tooling.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

setup() {
  AGENT_CONFIG_BACKUP="$(backup_agent_config)"
  write_agent_config "$AGENT_LAYER_ROOT/config/agents.json" true true true true
}

teardown() {
  restore_agent_config "$AGENT_CONFIG_BACKUP"
  cleanup_test_temp_dirs
}

# Test: sync generates Codex config and instructions
@test "sync generates Codex config and instructions" {
  local root
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  [ -f "$root/.codex/config.toml" ]
  [ -f "$root/.codex/AGENTS.md" ]
  grep -q '^\[mcp_servers\.' "$root/.codex/config.toml"
  grep -q 'GENERATED FILE' "$root/.codex/AGENTS.md"

  rm -rf "$root"
}

# Test: sync emits YAML-folded descriptions for Codex skills
@test "sync emits YAML-folded descriptions for Codex skills" {
  local root skill
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  skill="$root/.codex/skills/find-issues/SKILL.md"
  [ -f "$skill" ]
  run rg -n "^description: >-$" "$skill"
  [ "$status" -eq 0 ]
  run rg -n "^  .*Report-first:" "$skill"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync generates VS Code prompt files
@test "sync generates VS Code prompt files" {
  local root prompt
  root="$(create_sync_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  prompt="$root/.vscode/prompts/find-issues.prompt.md"
  [ -f "$prompt" ]
  run rg -n "^name: find-issues$" "$prompt"
  [ "$status" -eq 0 ]
  run rg -n "GENERATED FILE" "$prompt"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync skips outputs for disabled agents
@test "sync skips outputs for disabled agents" {
  local root
  root="$(create_sync_parent_root)"
  write_agent_config "$root/.agent-layer/config/agents.json" true true false false

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  [ -f "$root/AGENTS.md" ]
  [ -f "$root/CLAUDE.md" ]
  [ -f "$root/GEMINI.md" ]
  [ ! -f "$root/.codex/AGENTS.md" ]
  [ ! -f "$root/.codex/config.toml" ]
  [ ! -f "$root/.vscode/settings.json" ]
  [ ! -d "$root/.vscode/prompts" ]

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer --check"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync removes stale generated VS Code prompt files
@test "sync removes stale generated VS Code prompt files" {
  local root prompt_dir stale_prompt
  root="$(create_sync_parent_root)"

  prompt_dir="$root/.vscode/prompts"
  mkdir -p "$prompt_dir"
  stale_prompt="$prompt_dir/stale.prompt.md"
  cat >"$stale_prompt" <<'EOF'
---
name: stale
---
<!--
  GENERATED FILE
  Source: .agent-layer/config/workflows/stale.md
  Regenerate: ./al --sync
-->
Stale prompt body.
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]
  [ ! -f "$stale_prompt" ]

  rm -rf "$root"
}

# Test: sync --check fails when a VS Code prompt file is missing
@test "sync --check fails when a VS Code prompt file is missing" {
  local root prompt
  root="$(create_sync_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  prompt="$root/.vscode/prompts/find-issues.prompt.md"
  rm -f "$prompt"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer --check"
  [ "$status" -ne 0 ]
  [[ "$output" == *"VS Code prompt files are generated from .agent-layer/config/workflows/*.md."* ]]

  rm -rf "$root"
}

# Test: sync handles workflow frontmatter with UTF-8 BOM
@test "sync handles workflow frontmatter with UTF-8 BOM" {
  local root workflow_file
  root="$(create_sync_parent_root)"

  workflow_file="$root/.agent-layer/config/workflows/bom-workflow.md"
  printf '\xEF\xBB\xBF' > "$workflow_file"
  cat >>"$workflow_file" <<'EOF'
---
description: BOM workflow
---
# BOM workflow
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  [ -f "$root/.codex/skills/bom-workflow/SKILL.md" ]
  run rg -n "BOM workflow" "$root/.codex/skills/bom-workflow/SKILL.md"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync defaults VS Code MCP envFile to .agent-layer/.env
@test "sync defaults VS Code MCP envFile to .agent-layer/.env" {
  local root
  root="$(create_sync_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  run rg -n "\"envFile\": \"\\$\\{workspaceFolder\\}/\\.agent-layer/\\.env\"" \
    "$root/.vscode/mcp.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync filters agent-layer MCP server for VS Code and Codex
@test "sync filters agent-layer MCP server for VS Code and Codex" {
  local root
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  run rg -n "\"agent-layer\"" "$root/.vscode/mcp.json"
  [ "$status" -ne 0 ]
  run rg -n "^\\[mcp_servers\\.agent-layer\\]" "$root/.codex/config.toml"
  [ "$status" -ne 0 ]

  run rg -n "\"agent-layer\"" "$root/.mcp.json"
  [ "$status" -eq 0 ]
  run rg -n "\"agent-layer\"" "$root/.gemini/settings.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync removes filtered MCP servers from existing VS Code and Codex configs
@test "sync removes filtered MCP servers from existing VS Code and Codex configs" {
  local root
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  run node -e '
const fs = require("fs");
const file = process.argv[1];
const data = JSON.parse(fs.readFileSync(file, "utf8"));
data.servers = data.servers || {};
data.servers["agent-layer"] = {
  type: "stdio",
  command: "node",
  args: [
    "./.agent-layer/src/cli.mjs",
    "mcp-prompts",
    "--parent-root",
    ".",
    "--agent-layer-root",
    "./.agent-layer",
  ],
};
fs.writeFileSync(file, JSON.stringify(data, null, 2) + "\n");
' "$root/.vscode/mcp.json"
  [ "$status" -eq 0 ]

  cat >>"$root/.codex/config.toml" <<'EOF'
[mcp_servers.agent-layer]
command = "node"
args = ["./.agent-layer/src/cli.mjs", "mcp-prompts", "--parent-root", ".", "--agent-layer-root", "./.agent-layer"]
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  run rg -n "\"agent-layer\"" "$root/.vscode/mcp.json"
  [ "$status" -ne 0 ]
  run rg -n "^\\[mcp_servers\\.agent-layer\\]" "$root/.codex/config.toml"
  [ "$status" -ne 0 ]

  rm -rf "$root"
}

# Test: sync ignores MCP server key order differences
@test "sync ignores MCP server key order differences" {
  local root baseline
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  [ -f "$root/.vscode/mcp.json" ]
  baseline="$root/baseline-vscode-mcp.json"
  cp "$root/.vscode/mcp.json" "$baseline"

  run node -e '
const fs = require("fs");
const file = process.argv[1];
const reorder = (value) => {
  if (Array.isArray(value)) return value.map(reorder);
  if (value && typeof value === "object") {
    const keys = Object.keys(value).sort().reverse();
    const out = {};
    for (const key of keys) out[key] = reorder(value[key]);
    return out;
  }
  return value;
};
const data = JSON.parse(fs.readFileSync(file, "utf8"));
const reordered = reorder(data);
fs.writeFileSync(file, JSON.stringify(reordered, null, 2) + "\n");
' "$root/.vscode/mcp.json"
  [ "$status" -eq 0 ]

  run cmp -s "$baseline" "$root/.vscode/mcp.json"
  [ "$status" -ne 0 ]

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  run diff -u "$baseline" "$root/.vscode/mcp.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync overwrites command allowlists from policy
@test "sync overwrites command allowlists from policy" {
  local root
  root="$(create_parent_root)"

  mkdir -p "$root/.gemini" "$root/.claude" "$root/.vscode"
  cat >"$root/.gemini/settings.json" <<'EOF'
{
  "tools": { "allowed": ["run_shell_command(bad)", "some_tool"], "extra": true }
}
EOF
  cat >"$root/.claude/settings.json" <<'EOF'
{
  "permissions": { "allow": ["Bash(bad:*)", "mcp__bad__*", "Edit"], "extra": true }
}
EOF
  cat >"$root/.vscode/settings.json" <<'EOF'
{
  "chat.tools.terminal.autoApprove": { "/^bad(\\b.*)?$/": true },
  "other": 1
}
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  run rg -n "run_shell_command\\(bad\\)" "$root/.gemini/settings.json"
  [ "$status" -eq 0 ]
  run rg -n "run_shell_command\\(git status\\)" "$root/.gemini/settings.json"
  [ "$status" -eq 0 ]
  run rg -n "run_shell_command\\(ls\\)" "$root/.gemini/settings.json"
  [ "$status" -eq 0 ]
  run rg -n "\"some_tool\"" "$root/.gemini/settings.json"
  [ "$status" -eq 0 ]
  run rg -n "\"extra\": true" "$root/.gemini/settings.json"
  [ "$status" -eq 0 ]

  run rg -n "Bash\\(bad:\\*\\)" "$root/.claude/settings.json"
  [ "$status" -eq 0 ]
  run rg -F "mcp__bad__*" "$root/.claude/settings.json"
  [ "$status" -eq 0 ]
  run rg -n "Bash\\(git status:\\*\\)" "$root/.claude/settings.json"
  [ "$status" -eq 0 ]
  run rg -n "Bash\\(ls:\\*\\)" "$root/.claude/settings.json"
  [ "$status" -eq 0 ]
  run rg -F "mcp__context7__*" "$root/.claude/settings.json"
  [ "$status" -eq 0 ]
  run rg -F "\"Edit\"" "$root/.claude/settings.json"
  [ "$status" -eq 0 ]
  run rg -n "\"extra\": true" "$root/.claude/settings.json"
  [ "$status" -eq 0 ]

  run rg -F 'bad(\\b.*)?$' "$root/.vscode/settings.json"
  [ "$status" -eq 0 ]
  run rg -F 'git status(\\b.*)?$' "$root/.vscode/settings.json"
  [ "$status" -eq 0 ]
  run rg -F 'ls(\\b.*)?$' "$root/.vscode/settings.json"
  [ "$status" -eq 0 ]
  run rg -n "\"other\": 1" "$root/.vscode/settings.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync --check passes after sync when outputs are clean
@test "sync --check passes after sync when outputs are clean" {
  local root
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer --check"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync --check fails when outputs are missing
@test "sync --check fails when outputs are missing" {
  local root
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer --check"
  [ "$status" -ne 0 ]
  [[ "$output" == *"WARNING: generated files are out of date."* ]]

  rm -rf "$root"
}

# Test: sync rejects --overwrite with --interactive
@test "sync rejects --overwrite with --interactive" {
  local root
  root="$(create_sync_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer --overwrite --interactive"
  [ "$status" -ne 0 ]
  [[ "$output" == *"choose only one of --overwrite or --interactive."* ]]

  rm -rf "$root"
}

# Test: sync rejects --check with --interactive
@test "sync rejects --check with --interactive" {
  local root
  root="$(create_sync_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer --check --interactive"
  [ "$status" -ne 0 ]
  [[ "$output" == *"--interactive cannot be used with --check."* ]]

  rm -rf "$root"
}

# Test: sync --interactive fails without a TTY
@test "sync --interactive fails without a TTY" {
  local root
  root="$(create_parent_root)"

  mkdir -p "$root/.gemini"
  cat >"$root/.gemini/settings.json" <<'EOF'
{
  "tools": { "allowed": ["run_shell_command(bad)"] }
}
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer --interactive"
  [ "$status" -ne 0 ]
  [[ "$output" == *"--interactive requires a TTY."* ]]

  rm -rf "$root"
}

# Test: sync fails when policy contains unsafe argv token
@test "sync fails when policy contains unsafe argv token" {
  local root
  root="$(create_sync_parent_root)"

  cat >"$root/.agent-layer/config/policy/commands.json" <<'EOF'
{
  "version": 1,
  "allowed": [
    { "argv": ["git", "status|all"] }
  ]
}
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"unsupported characters"* ]]

  rm -rf "$root"
}

# Test: sync --overwrite removes divergent allowlists and mcp entries
@test "sync --overwrite removes divergent allowlists and mcp entries" {
  local root
  root="$(create_parent_root)"

  mkdir -p "$root/.gemini" "$root/.claude" "$root/.vscode" "$root/.codex/rules"
  cat >"$root/.gemini/settings.json" <<'EOF'
{
  "tools": { "allowed": ["run_shell_command(bad)", "some_tool"], "extra": true },
  "mcpServers": { "extra": { "command": "node", "args": [] } }
}
EOF
  cat >"$root/.claude/settings.json" <<'EOF'
{
  "permissions": { "allow": ["Bash(bad:*)", "mcp__bad__*", "Edit"], "extra": true }
}
EOF
  cat >"$root/.vscode/settings.json" <<'EOF'
{
  "chat.tools.terminal.autoApprove": { "/^bad(\\b.*)?$/": true },
  "other": 1
}
EOF
  cat >"$root/.mcp.json" <<'EOF'
{
  "mcpServers": { "extra": { "command": "node", "args": [] } }
}
EOF
  cat >"$root/.vscode/mcp.json" <<'EOF'
{
  "servers": { "extra": { "type": "stdio", "command": "node", "args": [] } }
}
EOF
  mkdir -p "$root/.codex"
  cat >"$root/.codex/config.toml" <<'EOF'
# GENERATED FILE
[mcp_servers.extra]
command = "node"
EOF
  cat >"$root/.codex/rules/default.rules" <<'EOF'
prefix_rule(pattern=["bad"], decision="allow", justification="legacy")
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer --overwrite"
  [ "$status" -eq 0 ]

  run rg -n "run_shell_command\\(bad\\)" "$root/.gemini/settings.json"
  [ "$status" -ne 0 ]
  run rg -n "Bash\\(bad:\\*\\)" "$root/.claude/settings.json"
  [ "$status" -ne 0 ]
  run rg -F 'bad(\\b.*)?$' "$root/.vscode/settings.json"
  [ "$status" -ne 0 ]

  run rg -n "\"extra\": \\{" "$root/.gemini/settings.json"
  [ "$status" -ne 0 ]
  run rg -n "\"extra\": \\{" "$root/.mcp.json"
  [ "$status" -ne 0 ]
  run rg -n "\"extra\": \\{" "$root/.vscode/mcp.json"
  [ "$status" -ne 0 ]
  run rg -n "mcp_servers\\.extra" "$root/.codex/config.toml"
  [ "$status" -ne 0 ]
  run rg -n "\\[\"bad\"\\]" "$root/.codex/rules/default.rules"
  [ "$status" -ne 0 ]

  rm -rf "$root"
}

# Test: sync warns with readable divergence sections
@test "sync warns with readable divergence sections" {
  local root
  root="$(create_parent_root)"

  mkdir -p "$root/.gemini" "$root/.claude" "$root/.vscode" "$root/.codex/rules"
  cat >"$root/.gemini/settings.json" <<'EOF'
{
  "tools": { "allowed": ["run_shell_command(bad)", "some_tool"], "extra": true },
  "mcpServers": { "extra": { "command": "node", "args": [] } }
}
EOF
  cat >"$root/.claude/settings.json" <<'EOF'
{
  "permissions": { "allow": ["Bash(bad:*)", "mcp__bad__*", "Edit"], "extra": true }
}
EOF
  cat >"$root/.vscode/settings.json" <<'EOF'
{
  "chat.tools.terminal.autoApprove": { "/^bad(\\b.*)?$/": true },
  "other": 1
}
EOF
  cat >"$root/.mcp.json" <<'EOF'
{
  "mcpServers": { "extra": { "command": "node", "args": [] } }
}
EOF
  cat >"$root/.vscode/mcp.json" <<'EOF'
{
  "servers": { "extra": { "type": "stdio", "command": "node", "args": [] } }
}
EOF
  mkdir -p "$root/.codex"
  cat >"$root/.codex/config.toml" <<'EOF'
# GENERATED FILE
[mcp_servers.extra]
command = "node"
EOF
  cat >"$root/.codex/rules/default.rules" <<'EOF'
prefix_rule(pattern=["bad"], decision="allow", justification="legacy")
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]
  [[ "$output" == *"client configs diverge"* ]]
  [[ "$output" == *"Details:"* ]]
  [[ "$output" == *"Next steps:"* ]]
  [[ "$output" == *"./al --inspect"* ]]
  [[ "$output" == *"- approvals:"* ]]
  [[ "$output" == *"- mcp:"* ]]
  [[ "$output" == *"Sync preserves existing client entries"* ]]

  rm -rf "$root"
}

# Test: sync --check warns and points to divergence report when outputs are stale
@test "sync --check warns and points to divergence report when outputs are stale" {
  local root
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  printf '\n# test\n' >> "$root/AGENTS.md"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer --check"
  [ "$status" -ne 0 ]
  [[ "$output" == *"WARNING: generated files are out of date."* ]]
  [[ "$output" == *"divergence"* ]]
  [[ "$output" == *"./al --inspect"* ]]

  rm -rf "$root"
}

# Test: sync fails when instructions directory is missing
@test "sync fails when instructions directory is missing" {
  local root
  root="$(create_sync_parent_root)"

  rm -rf "$root/.agent-layer/config/instructions"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"missing instructions directory"* ]]

  rm -rf "$root"
}

# Test: sync fails when instructions directory has no markdown files
@test "sync fails when instructions directory has no markdown files" {
  local root
  root="$(create_sync_parent_root)"

  rm -f "$root/.agent-layer/config/instructions/"*.md

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"no instruction files found"* ]]

  rm -rf "$root"
}

# Test: sync fails when workflows directory is missing
@test "sync fails when workflows directory is missing" {
  local root
  root="$(create_sync_parent_root)"

  rm -rf "$root/.agent-layer/config/workflows"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"missing workflows directory"* ]]

  rm -rf "$root"
}

# Test: sync fails when workflows directory has no markdown files
@test "sync fails when workflows directory has no markdown files" {
  local root
  root="$(create_sync_parent_root)"

  rm -f "$root/.agent-layer/config/workflows/"*.md

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"no workflow files found"* ]]

  rm -rf "$root"
}

# Test: sync fails when MCP server catalog is missing
@test "sync fails when MCP server catalog is missing" {
  local root
  root="$(create_sync_parent_root)"

  rm -f "$root/.agent-layer/config/mcp-servers.json"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"servers.json not found"* ]]

  rm -rf "$root"
}

# Test: sync fails when default args are positional
@test "sync fails when default args are positional" {
  local root
  root="$(create_sync_parent_root)"

  cat >"$root/.agent-layer/config/agents.json" <<'EOF'
{
  "gemini": { "enabled": true },
  "claude": { "enabled": true },
  "codex": { "enabled": true, "defaultArgs": ["gpt-5.2-codex"] },
  "vscode": { "enabled": true }
}
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer 2>&1"
  [ "$status" -ne 0 ]
  [[ "$output" == *"defaultArgs[0] must follow a --flag"* ]]

  rm -rf "$root"
}

# Test: sync fails when MCP defaults include geminiTrust
@test "sync fails when MCP defaults include geminiTrust" {
  local root
  root="$(create_sync_parent_root)"

  cat >"$root/.agent-layer/config/mcp-servers.json" <<'EOF'
{
  "version": 1,
  "defaults": {
    "geminiTrust": true
  },
  "servers": [
    {
      "name": "bad-defaults",
      "command": "node"
    }
  ]
}
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"defaults.geminiTrust is not supported"* ]]

  rm -rf "$root"
}

# Test: sync fails when an MCP server includes geminiTrust
@test "sync fails when an MCP server includes geminiTrust" {
  local root
  root="$(create_sync_parent_root)"

  cat >"$root/.agent-layer/config/mcp-servers.json" <<'EOF'
{
  "version": 1,
  "servers": [
    {
      "name": "bad-server",
      "command": "node",
      "geminiTrust": true
    }
  ]
}
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"bad-server.geminiTrust is not supported"* ]]

  rm -rf "$root"
}

# Test: sync fails when an MCP server clients list includes unknown values
@test "sync fails when an MCP server clients list includes unknown values" {
  local root
  root="$(create_sync_parent_root)"

  cat >"$root/.agent-layer/config/mcp-servers.json" <<'EOF'
{
  "version": 1,
  "servers": [
    {
      "name": "bad-client",
      "command": "node",
      "clients": ["unknown"]
    }
  ]
}
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"clients contains unknown client"* ]]

  rm -rf "$root"
}

# Test: sync fails when HTTP server is missing url
@test "sync fails when HTTP server is missing url" {
  local root
  root="$(create_sync_parent_root)"

  cat >"$root/.agent-layer/config/mcp-servers.json" <<'EOF'
{
  "version": 1,
  "servers": [
    {
      "name": "missing-url",
      "transport": "http"
    }
  ]
}
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"missing-url.url must be a non-empty string"* ]]

  rm -rf "$root"
}

# Test: sync fails when stdio server includes url
@test "sync fails when stdio server includes url" {
  local root
  root="$(create_sync_parent_root)"

  cat >"$root/.agent-layer/config/mcp-servers.json" <<'EOF'
{
  "version": 1,
  "servers": [
    {
      "name": "bad-stdio",
      "command": "node",
      "url": "https://example.com"
    }
  ]
}
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"bad-stdio.url is not allowed for stdio servers"* ]]

  rm -rf "$root"
}

# Test: sync fails when Gemini HTTP server token is missing
@test "sync fails when Gemini HTTP server token is missing" {
  local root
  root="$(create_sync_parent_root)"

  rm -f "$root/.agent-layer/.env"

  run bash -c "cd \"$root\" && env -u GITHUB_PERSONAL_ACCESS_TOKEN -u AGENT_LAYER_ROOT ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -ne 0 ]
  [[ "$output" == *"Missing GITHUB_PERSONAL_ACCESS_TOKEN"* ]]

  rm -rf "$root"
}

# Test: sync succeeds without Gemini HTTP token when Gemini is disabled
@test "sync succeeds without Gemini HTTP token when Gemini is disabled" {
  local root
  root="$(create_sync_parent_root)"
  write_agent_config "$root/.agent-layer/config/agents.json" false true true true

  run bash -c "cd \"$root\" && env -u GITHUB_PERSONAL_ACCESS_TOKEN -u AGENT_LAYER_ROOT ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]
  [ ! -f "$root/.gemini/settings.json" ]

  rm -rf "$root"
}

# Test: sync generates HTTP MCP configs for GitHub
@test "sync generates HTTP MCP configs for GitHub" {
  local root token
  root="$(create_sync_parent_root)"
  token="test-gh-token"

  run bash -c "cd \"$root\" && GITHUB_PERSONAL_ACCESS_TOKEN=\"$token\" ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  run rg -n "https://api.githubcopilot.com/mcp/" "$root/.mcp.json"
  [ "$status" -eq 0 ]
  run rg -n -- "Bearer \\$\\{GITHUB_PERSONAL_ACCESS_TOKEN\\}" "$root/.mcp.json"
  [ "$status" -eq 0 ]

  run rg -n "input:github-pat" "$root/.vscode/mcp.json"
  [ "$status" -eq 0 ]
  run rg -n "type\": \"http\"" "$root/.vscode/mcp.json"
  [ "$status" -eq 0 ]

  run rg -n "httpUrl" "$root/.gemini/settings.json"
  [ "$status" -eq 0 ]
  run rg -n "Bearer $token" "$root/.gemini/settings.json"
  [ "$status" -eq 0 ]

  run rg -n "bearer_token_env_var = \"GITHUB_PERSONAL_ACCESS_TOKEN\"" "$root/.codex/config.toml"
  [ "$status" -eq 0 ]
  run rg -n "url = \"https://api.githubcopilot.com/mcp/\"" "$root/.codex/config.toml"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync merges VS Code inputs with generated HTTP inputs
@test "sync merges VS Code inputs with generated HTTP inputs" {
  local root token
  root="$(create_sync_parent_root)"
  token="test-gh-token"

  mkdir -p "$root/.vscode"
  cat >"$root/.vscode/mcp.json" <<'EOF'
{
  "inputs": [
    {
      "type": "promptString",
      "id": "custom-token",
      "description": "Custom token",
      "password": true
    }
  ],
  "servers": {}
}
EOF

  run bash -c "cd \"$root\" && GITHUB_PERSONAL_ACCESS_TOKEN=\"$token\" ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  run rg -n "\"id\": \"custom-token\"" "$root/.vscode/mcp.json"
  [ "$status" -eq 0 ]
  run rg -n "\"id\": \"github-pat\"" "$root/.vscode/mcp.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: inspect ignores Codex env var comments
@test "inspect ignores Codex env var comments" {
  local root
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --sync --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --inspect --parent-root . --agent-layer-root ./.agent-layer > \"$root/out.json\""
  [ "$status" -eq 0 ]

  run node -e "const data=require(process.argv[1]); if (data.summary.approvals !== 0 || data.summary.mcp !== 0) process.exit(1);" "$root/out.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: inspect reports filtered agents
@test "inspect reports filtered agents" {
  local root
  root="$(create_parent_root)"
  write_agent_config "$AGENT_LAYER_ROOT/config/agents.json" true true false false

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --inspect --parent-root . --agent-layer-root ./.agent-layer > \"$root/out.json\""
  [ "$status" -eq 0 ]

  run node -e "const data=require(process.argv[1]); const note=data.notes.join('\\n'); if (!note.includes('filtered to enabled agents')) process.exit(1); if (!note.includes('disabled agents')) process.exit(1);" "$root/out.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: inspect warns when extra Codex rules files exist
@test "inspect warns when extra Codex rules files exist" {
  local root
  root="$(create_parent_root)"

  mkdir -p "$root/.codex/rules"
  cat >"$root/.codex/rules/default.rules" <<'EOF'
prefix_rule(pattern=["git","status"], decision="allow", justification="agent-layer allowlist")
EOF
  cat >"$root/.codex/rules/extra.rules" <<'EOF'
prefix_rule(pattern=["extra"], decision="allow", justification="custom")
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --inspect --parent-root . --agent-layer-root ./.agent-layer > \"$root/out.json\""
  [ "$status" -eq 0 ]

  run node -e "const data=require(process.argv[1]); const note=data.notes.join('\\n'); if (!note.includes('extra rules files')) process.exit(1); if (!note.includes('.codex/rules/extra.rules')) process.exit(1); if (!note.includes('integrate')) process.exit(1); if (!note.includes('delete')) process.exit(1);" "$root/out.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: inspect handles Codex config with empty args
@test "inspect handles Codex config with empty args" {
  local root
  root="$(create_sync_parent_root)"

  cat >"$root/.agent-layer/config/mcp-servers.json" <<'EOF'
{
  "version": 1,
  "servers": [
    {
      "name": "empty-args",
      "enabled": true,
      "transport": "stdio",
      "command": "node",
      "args": [],
      "envVars": []
    }
  ]
}
EOF

  mkdir -p "$root/.codex"
  cat >"$root/.codex/config.toml" <<'EOF'
# GENERATED FILE
[mcp_servers.empty-args]
command = "node"
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --inspect --parent-root . --agent-layer-root ./.agent-layer > \"$root/out.json\""
  [ "$status" -eq 0 ]

  run node -e "const data=require(process.argv[1]); if (data.summary.mcp !== 0) process.exit(1);" "$root/out.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}
