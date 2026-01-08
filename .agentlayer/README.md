# Agentlayer (repo-local agent standardization)

This repository includes a repo-local standardization layer under `.agentlayer/`.

## What this is for

**Goal:** make agentic tooling consistent across Claude Code, Gemini CLI, VS Code/Copilot, and Codex by keeping a **single source of truth** in the repo, then generating the per-client shim/config files those tools require.

**Primary uses**
- A unified instruction set (system/developer-style guidance) usable across tools.
- Repeatable “workflows” exposed as:
  - MCP prompts (slash commands) in clients that support MCP prompts.
  - Codex Skills (repo-local) for Codex.
- A repo-committed MCP server catalog, projected into each client’s config format.
- A repo-owned allowlist of safe shell command prefixes, projected into each client's auto-approval settings.
- A lightweight setup flow that works in any project repo.

## Prerequisites

Required:
- **Node.js + npm** (recommended: manage a pinned version via `mise`, `asdf`, `volta`, or `nvm`)
- **git** (recommended; required for enabling hooks)

Optional (depending on which clients you use):
- VS Code (Copilot Chat)
- Gemini CLI
- Claude Code
- Codex CLI / Codex VS Code extension

## Quickstart

From repo root:

1) **Run setup (installs deps, enables hooks, verifies everything)**

```bash
chmod +x .agentlayer/setup.sh
./.agentlayer/setup.sh
```

2) **Create your Agentlayer env file (recommended: agent-only secrets)**

```bash
# Recommended: keep agent-only secrets separate from project env vars
cp .env.example .agentlayer/.env
# edit .agentlayer/.env; do not commit it
```

If you also use a project/dev `.env` for your application, keep it in `./.env` and do not mix agent-only tokens into it.

3) **Create the repo-local launcher `./al` (recommended)**

```bash
chmod +x ./al
```

Default behavior: sync every run via `node .agentlayer/sync/sync.mjs`, then load `.agentlayer/.env` and exec the command (via `.agentlayer/with-env.sh`).

Examples:

```bash
./al gemini
./al claude
./al codex
```

4) **Edit sources of truth**
- Unified instructions: `.agentlayer/instructions/*.md`
- Workflows: `.agentlayer/workflows/*.md`
- MCP server catalog: `.agentlayer/mcp/servers.json`
- Command allowlist: `.agentlayer/policy/commands.json`

5) **Regenerate after changes**

```bash
node .agentlayer/sync/sync.mjs
```

## How to use (day-to-day)

### Prefer `./al` for running CLIs

`./al` is intentionally minimal. By default it:

1) Runs `node .agentlayer/sync/sync.mjs` (or `--check` then regenerates if out of date, depending on your `al`)
2) Loads `.agentlayer/.env` via `.agentlayer/with-env.sh`
3) Executes the command

Examples:

```bash
./al gemini
./al claude
./al codex
```

For a one-off run that also includes project env (`./.env`), use:

```bash
./.agentlayer/with-env.sh --project-env gemini
```

`with-env.sh` resolves the repo root for env file paths and does not change your working directory.

### Debugging trick (verify env vars are being loaded)

```bash
./al env | grep -E 'GITHUB_TOKEN|CONTEXT7_API_KEY'
```

### What files you should and should not edit

**Edit these (sources of truth):**
- `.agentlayer/instructions/*.md`
- `.agentlayer/workflows/*.md`
- `.agentlayer/mcp/servers.json`
- `.agentlayer/policy/commands.json`

**Do not edit these directly (generated):**
- `AGENTS.md`
- `CLAUDE.md`
- `GEMINI.md`
- `.github/copilot-instructions.md`
- `.mcp.json`
- `.gemini/settings.json`
- `.claude/settings.json`
- `.vscode/mcp.json`
- `.vscode/settings.json`
- `.codex/rules/agentlayer.rules`
- `.codex/skills/*/SKILL.md`

If you accidentally edited a generated file, revert it (example):

```bash
git checkout -- .mcp.json
```

### Instruction file ordering (why the numbers)

`.agentlayer/sync/sync.mjs` concatenates `.agentlayer/instructions/*.md` in **lexicographic order**.

Numeric prefixes (e.g. `00_`, `10_`, `20_`) ensure a **stable, predictable ordering** without requiring a separate manifest/config file. If you add new instruction fragments, follow the same pattern.

## Refresh / restart guidance (failure modes)

General rule:
- After changing `.agentlayer/**` → run `node .agentlayer/sync/sync.mjs` (or run your CLI via `./al ...`) → then refresh/restart the client as needed.

### MCP prompt server (workflows as “slash commands”)

Workflows are exposed as MCP prompts by:
- `.agentlayer/mcp/agentlayer-prompts/server.mjs`

**Required one-time install (per machine / per clone):**
```bash
cd .agentlayer/mcp/agentlayer-prompts
npm install
```

If you changed `.agentlayer/workflows/*.md`:
- run `node .agentlayer/sync/sync.mjs` (or `./al <cmd>`)
- then refresh MCP discovery in your client (or restart the client/session)

---

## Client-specific notes (MCP config + slash commands)

Each section below answers two questions:

1) **How do I know MCP config is being read?**
2) **How do I know workflow slash commands are available?**

### Gemini CLI

**MCP config file**
- Project MCP config is in: `.gemini/settings.json` (generated).
- Quick check:
  ```bash
  cat .gemini/settings.json
  ```
  Confirm you see `"mcpServers"` with the servers you expect (e.g., `agentlayer`, `context7`).

**Confirm the MCP server can start**
- Ensure Node deps are installed:
  ```bash
  cd .agentlayer/mcp/agentlayer-prompts && npm install && cd -
  ```
- Then run Gemini via `./al gemini`.

**Confirm slash commands (MCP prompts)**
- In Gemini, try a workflow name directly:
  - `/find-issues`
- If it’s present, it will expand and run the workflow prompt.
- If it’s missing:
  1) run `node .agentlayer/sync/sync.mjs`
  2) restart Gemini
  3) confirm `.gemini/settings.json` still lists `agentlayer` under `mcpServers`

**Common failure mode**
- If Gemini prompts for approvals on shell commands like `git status`, that is a *shell tool approval*, not MCP. (Solving this uses the repo allowlist `.agentlayer/policy/commands.json` projected into Gemini’s `tools.allowed`.)

---

### VS Code / Copilot Chat

**MCP config file**
- Project MCP config is in: `.vscode/mcp.json` (generated).
- Quick check:
  ```bash
  cat .vscode/mcp.json
  ```

**Confirm MCP server is running**
- Open the repo in VS Code.
- Ensure Copilot Chat is enabled and MCP is enabled in your environment.
- If MCP tools/prompts look stale:
  - restart MCP servers and/or run VS Code’s “Chat: Reset Cached Tools” action.

**Confirm slash commands (MCP prompts)**
- In Copilot Chat, invoke:
  - `/mcp.agentlayer.find-issues`
- If it autocompletes, the prompt is registered.

**Common failure mode**
- VS Code can cache tool lists. Reset cached tools and reload window if needed.

---

### Claude Code

**MCP config file**
- Project MCP config is in: `.mcp.json` (generated).
- Quick check:
  ```bash
  cat .mcp.json
  ```

**Confirm MCP is connected**
- Launch Claude Code from repo root:
  ```bash
  ./al claude
  ```
- If MCP servers are not available:
  1) verify `.mcp.json` exists and includes `mcpServers.agentlayer`
  2) ensure MCP prompt server deps installed:
     ```bash
     cd .agentlayer/mcp/agentlayer-prompts && npm install && cd -
     ```
  3) restart Claude Code after MCP config changes

**Confirm slash commands (MCP prompts)**
- In Claude Code, invoke the MCP prompt using its MCP prompt UI/namespace (varies by client build).
- Quick sanity check: the prompt list should include your workflow prompt name (e.g., `find-issues`).
- If missing:
  1) run `node .agentlayer/sync/sync.mjs`
  2) restart Claude Code
  3) ensure the MCP server process can run (Node installed, deps installed)

---

### Codex (CLI / VS Code extension)

**MCP config**
- Codex MCP configuration is typically user-level unless you deliberately set a repo-local `CODEX_HOME`.
- Agentlayer uses **Codex Skills** (and optional rules) as the primary “workflow command” mechanism.

**Confirm workflow “commands” (Codex Skills)**
- Skills are generated into: `.codex/skills/*/SKILL.md`
- Quick check:
  ```bash
  ls -la .codex/skills
  ```
- In Codex, use:
  - `/skills` to browse skills
  - then select the appropriate skill (e.g., `find-issues`)

**If a skill is missing**
1) run `node .agentlayer/sync/sync.mjs`
2) verify the workflow exists: `.agentlayer/workflows/<workflow>.md`
3) verify `.codex/skills/<workflow>/SKILL.md` was generated

**Common failure mode**
- Codex may require a restart to pick up new/updated skills.

---

## What’s inside this repository

### Source-of-truth directories
- `.agentlayer/instructions/`  
  Unified instruction fragments (concatenated into shims).
- `.agentlayer/workflows/`  
  Workflow definitions (exposed as MCP prompts; also used to generate Codex skills).
- `.agentlayer/mcp/servers.json`  
  Canonical MCP server list (no secrets inside).
- `.agentlayer/policy/`  
  Auto-approve command allowlist (safe shell command prefixes).

### Generated outputs
- Instruction shims:
  - `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `.github/copilot-instructions.md`
- MCP configs projected per client:
  - `.mcp.json`, `.gemini/settings.json`, `.vscode/mcp.json`
- Command allowlist configs projected per client:
  - `.gemini/settings.json`, `.claude/settings.json`, `.vscode/settings.json`, `.codex/rules/agentlayer.rules`
- Codex skills:
  - `.codex/skills/*/SKILL.md`

### Scripts
- `.agentlayer/setup.sh`  
  One-shot setup (install MCP deps, enable hooks, validate).
- `.agentlayer/sync/sync.mjs`  
  Generator (“build”) for all shims/configs/skills.
- `./al`  
  Repo-local launcher (sync + env load + exec).

## FAQ / Troubleshooting

### “I edited generated JSON and now things are broken.”
Generated JSON files (`.mcp.json`, `.vscode/mcp.json`, `.gemini/settings.json`) do not allow comments and may be strict-parsed by clients.

Fix:
1) revert the generated file(s)
2) edit the source-of-truth (`.agentlayer/mcp/servers.json`)
3) run `node .agentlayer/sync/sync.mjs`

### “I edited instructions but the agent didn’t follow them.”
- Did you run `node .agentlayer/sync/sync.mjs` (or run via `./al ...`)?
- Did you restart the session/client (many tools read system instructions at session start)?
- For Gemini CLI, refresh memory (often `/memory refresh`) or start a new session.

### “I edited workflows but the prompt/command list didn’t update.”
- Run `node .agentlayer/sync/sync.mjs`
- Restart/refresh MCP discovery:
  - Gemini: restart Gemini and/or run MCP refresh if available in your build
  - VS Code: restart servers / reset cached tools
  - Claude Code: restart Claude Code after MCP config changes

### “Commits are failing after enabling hooks.”
The hook runs:

```bash
node .agentlayer/sync/sync.mjs --check
```

If it fails, run:

```bash
node .agentlayer/sync/sync.mjs
```

Then commit again.

### “Can I rename the instruction files?”
Yes. Keep numeric prefixes if you want stable ordering without changing `.agentlayer/sync/sync.mjs`.
