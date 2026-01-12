#!/usr/bin/env bats

# Tests for the sync generator and inspection tooling.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Test: sync generates Codex config and instructions
@test "sync generates Codex config and instructions" {
  local root
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
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

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
  [ "$status" -eq 0 ]

  skill="$root/.codex/skills/find-issues/SKILL.md"
  [ -f "$skill" ]
  run rg -n "^description: >-$" "$skill"
  [ "$status" -eq 0 ]
  run rg -n "^  .*Report-first:" "$skill"
  [ "$status" -eq 0 ]

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

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
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

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
  [ "$status" -eq 0 ]

  run rg -n "\"envFile\": \"\\$\\{workspaceFolder\\}/\\.agent-layer/\\.env\"" \
    "$root/.vscode/mcp.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync ignores MCP server key order differences
@test "sync ignores MCP server key order differences" {
  local root baseline
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
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

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
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

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
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

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
  [ "$status" -eq 0 ]

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs --check"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: sync --check fails when outputs are missing
@test "sync --check fails when outputs are missing" {
  local root
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs --check"
  [ "$status" -ne 0 ]
  [[ "$output" == *"WARNING: generated files are out of date."* ]]

  rm -rf "$root"
}

# Test: sync rejects --overwrite with --interactive
@test "sync rejects --overwrite with --interactive" {
  local root
  root="$(create_sync_parent_root)"

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs --overwrite --interactive"
  [ "$status" -ne 0 ]
  [[ "$output" == *"choose only one of --overwrite or --interactive."* ]]

  rm -rf "$root"
}

# Test: sync rejects --check with --interactive
@test "sync rejects --check with --interactive" {
  local root
  root="$(create_sync_parent_root)"

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs --check --interactive"
  [ "$status" -ne 0 ]
  [[ "$output" == *"--interactive cannot be used with --check."* ]]

  rm -rf "$root"
}

# Test: sync --codex fails when CODEX_HOME points outside repo
@test "sync --codex fails when CODEX_HOME points outside repo" {
  local root external
  root="$(create_parent_root)"
  external="$(make_tmp_dir)"

  run bash -c "cd \"$root\" && CODEX_HOME=\"$external\" node .agent-layer/src/sync/sync.mjs --codex"
  [ "$status" -ne 0 ]
  [[ "$output" == *"CODEX_HOME must point to the repo-local .codex"* ]]

  rm -rf "$root" "$external"
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

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs --interactive"
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

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
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
# GENERATED FILE - DO NOT EDIT DIRECTLY
[mcp_servers.extra]
command = "node"
EOF
  cat >"$root/.codex/rules/default.rules" <<'EOF'
prefix_rule(pattern=["bad"], decision="allow", justification="legacy")
EOF

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs --overwrite"
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

# Test: sync --check warns and points to divergence report when outputs are stale
@test "sync --check warns and points to divergence report when outputs are stale" {
  local root
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
  [ "$status" -eq 0 ]

  printf '\n# test\n' >> "$root/AGENTS.md"

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs --check"
  [ "$status" -ne 0 ]
  [[ "$output" == *"WARNING: generated files are out of date."* ]]
  [[ "$output" == *"divergence"* ]]
  [[ "$output" == *"inspect.mjs"* ]]

  rm -rf "$root"
}

# Test: sync fails when instructions directory is missing
@test "sync fails when instructions directory is missing" {
  local root
  root="$(create_sync_parent_root)"

  rm -rf "$root/.agent-layer/config/instructions"

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
  [ "$status" -ne 0 ]
  [[ "$output" == *"missing instructions directory"* ]]

  rm -rf "$root"
}

# Test: sync fails when instructions directory has no markdown files
@test "sync fails when instructions directory has no markdown files" {
  local root
  root="$(create_sync_parent_root)"

  rm -f "$root/.agent-layer/config/instructions/"*.md

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
  [ "$status" -ne 0 ]
  [[ "$output" == *"no instruction files found"* ]]

  rm -rf "$root"
}

# Test: sync fails when workflows directory is missing
@test "sync fails when workflows directory is missing" {
  local root
  root="$(create_sync_parent_root)"

  rm -rf "$root/.agent-layer/config/workflows"

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
  [ "$status" -ne 0 ]
  [[ "$output" == *"missing workflows directory"* ]]

  rm -rf "$root"
}

# Test: sync fails when workflows directory has no markdown files
@test "sync fails when workflows directory has no markdown files" {
  local root
  root="$(create_sync_parent_root)"

  rm -f "$root/.agent-layer/config/workflows/"*.md

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
  [ "$status" -ne 0 ]
  [[ "$output" == *"no workflow files found"* ]]

  rm -rf "$root"
}

# Test: sync fails when MCP server catalog is missing
@test "sync fails when MCP server catalog is missing" {
  local root
  root="$(create_sync_parent_root)"

  rm -f "$root/.agent-layer/config/mcp-servers.json"

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
  [ "$status" -ne 0 ]
  [[ "$output" == *"servers.json not found"* ]]

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

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
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

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
  [ "$status" -ne 0 ]
  [[ "$output" == *"bad-server.geminiTrust is not supported"* ]]

  rm -rf "$root"
}

# Test: inspect ignores Codex env var comments
@test "inspect ignores Codex env var comments" {
  local root
  root="$(create_parent_root)"

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/sync.mjs"
  [ "$status" -eq 0 ]

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/inspect.mjs > \"$root/out.json\""
  [ "$status" -eq 0 ]

  run node -e "const data=require(process.argv[1]); if (data.summary.approvals !== 0 || data.summary.mcp !== 0) process.exit(1);" "$root/out.json"
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

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/inspect.mjs > \"$root/out.json\""
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
# GENERATED FILE - DO NOT EDIT DIRECTLY
[mcp_servers.empty-args]
command = "node"
EOF

  run bash -c "cd \"$root\" && node .agent-layer/src/sync/inspect.mjs > \"$root/out.json\""
  [ "$status" -eq 0 ]

  run node -e "const data=require(process.argv[1]); if (data.summary.mcp !== 0) process.exit(1);" "$root/out.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}
