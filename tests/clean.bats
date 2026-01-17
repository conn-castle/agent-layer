#!/usr/bin/env bats

# Tests for cleaning generated outputs and managed settings.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

teardown() {
  cleanup_test_temp_dirs
}

# Test: ./al --clean removes generated outputs but keeps sources
@test "al --clean removes generated outputs but keeps sources" {
  local root
  root="$(create_isolated_parent_root)"

  mkdir -p "$root/.github" "$root/.gemini" "$root/.vscode" "$root/.claude"
  mkdir -p "$root/.codex/rules" "$root/.codex/skills/foo"

  : >"$root/AGENTS.md"
  : >"$root/CLAUDE.md"
  : >"$root/GEMINI.md"
  : >"$root/.github/copilot-instructions.md"
  cat >"$root/.mcp.json" <<'EOF'
{}
EOF
  cat >"$root/.vscode/mcp.json" <<'EOF'
{}
EOF
  : >"$root/.codex/AGENTS.md"
  : >"$root/.codex/config.toml"
  : >"$root/.codex/rules/default.rules"
  : >"$root/.codex/skills/foo/SKILL.md"

  cat >"$root/.gemini/settings.json" <<'EOF'
{
  "mcpServers": {
    "agent-layer": { "command": "node" },
    "context7": { "command": "npx" },
    "custom": { "command": "custom" }
  },
  "tools": {
    "allowed": [
      "run_shell_command(git status)",
      "OtherTool"
    ]
  },
  "ui": { "theme": "dark" }
}
EOF

  cat >"$root/.claude/settings.json" <<'EOF'
{
  "permissions": {
    "allow": [
      "Bash(git status:*)",
      "mcp__github__*",
      "Read",
      "Custom"
    ]
  },
  "ui": { "theme": "light" }
}
EOF

  cat >"$root/.vscode/settings.json" <<'EOF'
{
  "chat.tools.terminal.autoApprove": { "/^git\\b/": true },
  "editor.tabSize": 2
}
EOF

  mkdir -p "$root/.vscode/prompts"
  cat >"$root/.vscode/prompts/generated.prompt.md" <<'EOF'
---
name: generated
---
<!--
  GENERATED FILE
  Source: .agent-layer/config/workflows/generated.md
  Regenerate: ./al --sync
-->
Generated prompt body.
EOF
  cat >"$root/.vscode/prompts/custom.prompt.md" <<'EOF'
---
name: custom
---
Custom prompt body.
EOF

  mkdir -p "$root/.agent-layer/config/instructions" "$root/.agent-layer/config/workflows"
  mkdir -p "$root/.agent-layer/config/policy"
  : >"$root/.agent-layer/config/instructions/01_test.md"
  : >"$root/.agent-layer/config/workflows/01_test.md"
  cat >"$root/.agent-layer/config/mcp-servers.json" <<'EOF'
{
  "version": 1,
  "servers": [
    { "name": "agent-layer", "command": "node" },
    { "name": "context7", "command": "npx" },
    { "name": "github", "command": "npx" }
  ]
}
EOF
  : >"$root/.agent-layer/config/policy/commands.json"

  run "$root/.agent-layer/agent-layer" --clean
  [ "$status" -eq 0 ]

  [ ! -f "$root/AGENTS.md" ]
  [ ! -f "$root/CLAUDE.md" ]
  [ ! -f "$root/GEMINI.md" ]
  [ ! -f "$root/.github/copilot-instructions.md" ]
  [ ! -f "$root/.mcp.json" ]
  [ ! -f "$root/.vscode/mcp.json" ]
  [ ! -f "$root/.codex/AGENTS.md" ]
  [ ! -f "$root/.codex/config.toml" ]
  [ ! -f "$root/.codex/rules/default.rules" ]
  [ ! -f "$root/.codex/skills/foo/SKILL.md" ]
  [ ! -d "$root/.codex/skills" ]

  [ -f "$root/.gemini/settings.json" ]
  [ -f "$root/.claude/settings.json" ]
  [ -f "$root/.vscode/settings.json" ]
  ! grep -Fq "run_shell_command" "$root/.gemini/settings.json"
  grep -Fq "OtherTool" "$root/.gemini/settings.json"
  ! grep -Fq "agent-layer" "$root/.gemini/settings.json"
  ! grep -Fq "context7" "$root/.gemini/settings.json"
  grep -Fq "custom" "$root/.gemini/settings.json"

  ! grep -Fq "Bash(" "$root/.claude/settings.json"
  ! grep -Fq "mcp__" "$root/.claude/settings.json"
  grep -Fq "Read" "$root/.claude/settings.json"
  grep -Fq "Custom" "$root/.claude/settings.json"

  ! grep -Fq "chat.tools.terminal.autoApprove" "$root/.vscode/settings.json"
  grep -Fq "editor.tabSize" "$root/.vscode/settings.json"

  [ ! -f "$root/.vscode/prompts/generated.prompt.md" ]
  [ -f "$root/.vscode/prompts/custom.prompt.md" ]

  [ -f "$root/.agent-layer/config/instructions/01_test.md" ]
  [ -f "$root/.agent-layer/config/workflows/01_test.md" ]
  [ -f "$root/.agent-layer/config/mcp-servers.json" ]
  [ -f "$root/.agent-layer/config/policy/commands.json" ]

  rm -rf "$root"
}

# Test: cli clean removes managed VS Code MCP servers
@test "cli clean removes managed VS Code MCP servers" {
  local root
  root="$(create_isolated_parent_root)"

  mkdir -p "$root/.vscode" "$root/.agent-layer/config"
  cat >"$root/.vscode/mcp.json" <<'EOF'
{
  "servers": {
    "agent-layer": { "type": "stdio", "command": "node" },
    "context7": { "type": "stdio", "command": "npx" },
    "custom": { "type": "stdio", "command": "custom" }
  },
  "other": true
}
EOF
  cat >"$root/.agent-layer/config/mcp-servers.json" <<'EOF'
{
  "version": 1,
  "servers": [
    { "name": "agent-layer", "command": "node", "clients": ["claude", "gemini"] },
    { "name": "context7", "command": "npx" }
  ]
}
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --clean --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  [ -f "$root/.vscode/mcp.json" ]
  ! grep -Fq "\"agent-layer\"" "$root/.vscode/mcp.json"
  ! grep -Fq "\"context7\"" "$root/.vscode/mcp.json"
  grep -Fq "\"custom\"" "$root/.vscode/mcp.json"
  grep -Fq "\"other\": true" "$root/.vscode/mcp.json"

  rm -rf "$root"
}

# Test: cli clean removes managed Claude MCP servers
@test "cli clean removes managed Claude MCP servers" {
  local root
  root="$(create_isolated_parent_root)"

  mkdir -p "$root/.agent-layer/config"
  cat >"$root/.mcp.json" <<'EOF'
{
  "mcpServers": {
    "agent-layer": { "command": "node" },
    "context7": { "command": "npx" },
    "custom": { "command": "custom" }
  },
  "other": true
}
EOF
  cat >"$root/.agent-layer/config/mcp-servers.json" <<'EOF'
{
  "version": 1,
  "servers": [
    { "name": "agent-layer", "command": "node", "clients": ["claude", "gemini"] },
    { "name": "context7", "command": "npx" }
  ]
}
EOF

  run bash -c "cd \"$root\" && ./.agent-layer/agent-layer --clean --parent-root . --agent-layer-root ./.agent-layer"
  [ "$status" -eq 0 ]

  [ -f "$root/.mcp.json" ]
  ! grep -Fq "\"agent-layer\"" "$root/.mcp.json"
  ! grep -Fq "\"context7\"" "$root/.mcp.json"
  grep -Fq "\"custom\"" "$root/.mcp.json"
  grep -Fq "\"other\": true" "$root/.mcp.json"

  rm -rf "$root"
}
