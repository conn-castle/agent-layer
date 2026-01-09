#!/usr/bin/env bats

load "helpers.bash"

@test "clean.sh removes generated outputs but keeps sources" {
  local root
  root="$(create_isolated_working_root)"

  mkdir -p "$root/.github" "$root/.gemini" "$root/.vscode" "$root/.claude"
  mkdir -p "$root/.codex/rules" "$root/.codex/skills/foo"

  : >"$root/AGENTS.md"
  : >"$root/CLAUDE.md"
  : >"$root/GEMINI.md"
  : >"$root/.github/copilot-instructions.md"
  : >"$root/.mcp.json"
  : >"$root/.gemini/settings.json"
  : >"$root/.claude/settings.json"
  : >"$root/.vscode/mcp.json"
  : >"$root/.vscode/settings.json"
  : >"$root/.codex/AGENTS.md"
  : >"$root/.codex/config.toml"
  : >"$root/.codex/rules/agent-layer.rules"
  : >"$root/.codex/skills/foo/SKILL.md"

  mkdir -p "$root/.agent-layer/instructions" "$root/.agent-layer/workflows"
  mkdir -p "$root/.agent-layer/mcp" "$root/.agent-layer/policy"
  : >"$root/.agent-layer/instructions/01_test.md"
  : >"$root/.agent-layer/workflows/01_test.md"
  : >"$root/.agent-layer/mcp/servers.json"
  : >"$root/.agent-layer/policy/commands.json"

  run "$root/.agent-layer/clean.sh"
  [ "$status" -eq 0 ]

  [ ! -f "$root/AGENTS.md" ]
  [ ! -f "$root/CLAUDE.md" ]
  [ ! -f "$root/GEMINI.md" ]
  [ ! -f "$root/.github/copilot-instructions.md" ]
  [ ! -f "$root/.mcp.json" ]
  [ ! -f "$root/.gemini/settings.json" ]
  [ ! -f "$root/.claude/settings.json" ]
  [ ! -f "$root/.vscode/mcp.json" ]
  [ ! -f "$root/.vscode/settings.json" ]
  [ ! -f "$root/.codex/AGENTS.md" ]
  [ ! -f "$root/.codex/config.toml" ]
  [ ! -f "$root/.codex/rules/agent-layer.rules" ]
  [ ! -f "$root/.codex/skills/foo/SKILL.md" ]
  [ ! -d "$root/.codex/skills" ]

  [ -f "$root/.agent-layer/instructions/01_test.md" ]
  [ -f "$root/.agent-layer/workflows/01_test.md" ]
  [ -f "$root/.agent-layer/mcp/servers.json" ]
  [ -f "$root/.agent-layer/policy/commands.json" ]

  rm -rf "$root"
}
