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
  : >"$root/.codex/rules/agentlayer.rules"
  : >"$root/.codex/skills/foo/SKILL.md"

  mkdir -p "$root/.agentlayer/instructions" "$root/.agentlayer/workflows"
  mkdir -p "$root/.agentlayer/mcp" "$root/.agentlayer/policy"
  : >"$root/.agentlayer/instructions/01_test.md"
  : >"$root/.agentlayer/workflows/01_test.md"
  : >"$root/.agentlayer/mcp/servers.json"
  : >"$root/.agentlayer/policy/commands.json"

  run "$root/.agentlayer/clean.sh"
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
  [ ! -f "$root/.codex/rules/agentlayer.rules" ]
  [ ! -f "$root/.codex/skills/foo/SKILL.md" ]
  [ ! -d "$root/.codex/skills" ]

  [ -f "$root/.agentlayer/instructions/01_test.md" ]
  [ -f "$root/.agentlayer/workflows/01_test.md" ]
  [ -f "$root/.agentlayer/mcp/servers.json" ]
  [ -f "$root/.agentlayer/policy/commands.json" ]

  rm -rf "$root"
}
