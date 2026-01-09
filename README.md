# Agent Layer (repo-local agent standardization)

Agent Layer is an opinionated framework for AI‑assisted development: one set of instructions, workflows, MCP servers, and project memory (features, roadmap, backlog, issues, decisions) keeps Claude, Gemini, VS Code/Copilot, and Codex aligned. It turns “vibe coding” into "agent orchestration" by giving every tool the same context and direction.

## What this is for

**Goal:** make agentic tooling consistent across Claude Code CLI, Gemini CLI, VS Code/Copilot, and Codex by keeping a **single source of truth** in the repo, then generating the per-client shim/config files those tools require.

**Primary uses**
- A unified instruction set (system/developer-style guidance) usable across tools.
- Repeatable “workflows” exposed as:
  - MCP prompts (slash commands) in clients that support MCP prompts.
  - Codex Skills (repo-local) for Codex.
- A repo-committed MCP server catalog, projected into each client’s config format.
- A repo-owned allowlist of safe shell command prefixes, projected into each client's auto-approval settings.
- A lightweight setup flow that works in any project repo.

### Support matrix

| Client | System instructions | Slash commands | MCP servers | Approved command list | Approved MCP tools |
| --- | --- | --- | --- | --- | --- |
| Gemini CLI | ✅ | ✅ | ✅ | ✅ | ✅ |
| Claude Code CLI | ✅ | ✅ | ✅ | ✅ | ✅ |
| VS Code / Copilot Chat | ✅ | ✅ | ✅ | ✅ | ❌ |
| Codex CLI | ✅ | ✅ | ✅ | ✅ | ❌ |
| Codex VS Code extension | ✅ | ✅ | ✅ | ✅ | ❌ |
| Antigravity | ❌ | ❌ | ❌ | ❌ | ❌ |

Note: Codex artifacts live in `.codex/`. The CLI uses them when launched via `./al codex` (repo-local `CODEX_HOME`). The VS Code extension only uses them if the extension host sees the same `CODEX_HOME` (see "Codex (CLI / VS Code extension)" below). Antigravity is not supported yet; if you're experimenting there, try the same `CODEX_HOME` setup.

## Prerequisites

Required:
- **Node.js + npm** (LTS >=20 recommended; `.nvmrc` included for devs; can use `mise`, `asdf`, `volta`, or `nvm`)
- **git** (recommended; required for dev hooks)
- **curl** (for the install script below)

Optional (depending on which clients you use):
- VS Code (Copilot Chat)
- Gemini CLI
- Claude Code CLI
- Codex CLI / Codex VS Code extension

Note: This tooling is built for macOS. Other operating systems are untested, and the `./al` symlink workflow is not supported on Windows (symlinks require extra setup).

Contributors using `nvm` can run `nvm use` inside `.agent-layer/` to match `.nvmrc`.

## Install in your repo (working repo root)

From your working repo root:

```bash
curl -fsSL https://raw.githubusercontent.com/nicholasjconn/agent-layer/main/agent-layer-install.sh -o agent-layer-install.sh
chmod +x agent-layer-install.sh
./agent-layer-install.sh
```

This creates `.agent-layer/`, adds a managed `.gitignore` block, creates `./al`,
and ensures the project memory files exist under `docs/` (`ISSUES.md`, `FEATURES.md`,
`ROADMAP.md`, `DECISIONS.md`). Templates live in `.agent-layer/templates/docs`; if a file
already exists, the installer prompts to keep it (default yes). In non-interactive
runs, existing files are kept.

If you already have this repo checked out locally:

```bash
/path/to/.agent-layer/agent-layer-install.sh
```

Upgrade an existing `.agent-layer` to the latest tagged release:

```bash
./agent-layer-install.sh --upgrade
```

Notes:
- Requires a clean `.agent-layer` working tree (commit or stash local changes first).
- Checks out the latest tag (detached HEAD) and prints the commit list since the current version.
- If no `origin` remote is configured, pass `--repo-url` or set `AGENTLAYER_REPO_URL`.

Update an existing `.agent-layer` to the latest commit of a branch (for developers):

```bash
./agent-layer-install.sh --latest-branch main
```

Notes:
- Requires a clean `.agent-layer` working tree (commit or stash local changes first).
- Fetches from the remote only; checks out the latest commit in detached HEAD mode.
- Re-run the command to pull the newest commit again.

## Quickstart

From the agent-layer repo root (inside `.agent-layer/` in your working repo):

1) **Run setup (installs MCP prompt server deps, runs sync, checks for drift)**

```bash
chmod +x setup.sh
./setup.sh
```

2) **Review/update your Agent Layer env file (recommended: agent-only secrets)**

```bash
# Recommended: keep agent-only secrets separate from project env vars
# If the installer did not create .env yet:
cp .env.example .env
# edit .env; do not commit it
```

The installer creates `.env` from `.env.example` if it is missing. If you also use a project/dev `.env` for your application, keep it separate and do not mix agent-only tokens into it.

3) **Ensure the repo-local launcher `./al` exists (recommended)**

If you used the installer, `./al` already exists (wrapper script). You can replace it with a symlink if you prefer:

```bash
chmod +x al
# from the working repo root:
ln -s .agent-layer/al ./al
```

This symlink is intended to live at the working repo root.

Default behavior: sync every run via `node sync/sync.mjs`, then load `.env` and exec the command (via `with-env.sh`).

Examples:

```bash
./al gemini
./al claude
./al codex
```

4) **Edit sources of truth**
- Unified instructions: `instructions/*.md`
- Workflows: `workflows/*.md`
- MCP server catalog: `mcp/servers.json`
- Command allowlist: `policy/commands.json`

Note: allowlist outputs are authoritative for shell command approvals. Sync replaces managed allowlist entries; non-managed allow entries are preserved for Gemini/Claude, while VS Code terminal auto-approve rules are replaced.

5) **Regenerate after changes (optional if you use `./al`)**

```bash
# ./al runs sync automatically; use this only if you want to regenerate without launching a CLI
node sync/sync.mjs
```

## Conventions

This repository is the agent-layer and is intended to live at `.agent-layer/` inside a working repo.
Paths in this README are relative to the agent-layer repo root unless noted as working-repo outputs; prefix with `.agent-layer/` when running from the working repo root.

### Repository layout

- Working repo root: your app plus generated shims/configs (for example, `AGENTS.md`, `.mcp.json`, `.codex/`).
- `.agent-layer/`: sources of truth (`instructions/`, `workflows/`, `mcp/`, `policy/`) and scripts (`setup.sh`, `sync/`, `clean.sh`).
- `./al`: launcher in the working repo root that syncs and forwards to CLIs.
- For the full list, see "What's inside this repository" below.

## Environment variables

`./al` and `with-env.sh` load `.agent-layer/.env` when it exists. The default MCP servers in `mcp/servers.json` expect:
- `GITHUB_TOKEN` (GitHub MCP server)
- `CONTEXT7_API_KEY` (Context7 MCP server)

VS Code MCP config uses the generated `.vscode/mcp.json` `envFile`, which defaults to `.agent-layer/.env`.

## How to use (day-to-day)

### Prefer `./al` for running CLIs

`./al` is intentionally minimal. By default it:

1) Runs `node sync/sync.mjs` (or `--check` then regenerates if out of date, depending on your `al`)
2) Loads `.env` via `with-env.sh`
3) Executes the command

Examples:

```bash
./al gemini
./al claude
./al codex
```

For a one-off run that also includes project env (if configured), from the working repo root use:

```bash
./.agent-layer/with-env.sh --project-env gemini
```

`with-env.sh` resolves the repo root for env file paths and does not change your working directory.

### Debugging trick (verify env vars are being loaded)

```bash
./al env | grep -E 'GITHUB_TOKEN|CONTEXT7_API_KEY'
```

### What files you should and should not edit

**Edit these (sources of truth):**
- `instructions/*.md`
- `workflows/*.md`
- `mcp/servers.json`
- `policy/commands.json`

**Do not edit these directly (generated in the working repo root):**
- `AGENTS.md`
- `CLAUDE.md`
- `GEMINI.md`
- `.github/copilot-instructions.md`
- `.mcp.json`
- `.gemini/settings.json`
- `.claude/settings.json`
- `.vscode/mcp.json`
- `.vscode/settings.json`
- `.codex/AGENTS.md`
- `.codex/config.toml`
- `.codex/rules/agent-layer.rules`
- `.codex/skills/*/SKILL.md`

If you accidentally edited a generated file, delete it and re-sync (example from the working repo root):

```bash
rm .mcp.json
node .agent-layer/sync/sync.mjs
```

If the file is tracked in your repo, `git checkout -- <file>` also works.

### Instruction file ordering (why the numbers)

`sync/sync.mjs` concatenates `instructions/*.md` in **lexicographic order**.

Numeric prefixes (e.g. `00_`, `10_`, `20_`) ensure a **stable, predictable ordering** without requiring a separate manifest/config file. If you add new instruction fragments, follow the same pattern.

## Approvals and permissions

Agent Layer treats `.agent-layer/mcp/servers.json` as the source of truth for MCP tool approvals.
Set `trust: true` per server (or `defaults.trust` for the default) to auto-approve that server's
tools where supported.

Behavior by client:
- Gemini CLI: `mcpServers.<name>.trust` is generated from `trust` (with defaults fallback).
- Claude Code: `permissions.allow` includes `mcp__<server>__*` for trusted servers, alongside the
  Bash allowlist from `policy/commands.json`. Non-managed allow entries (for example, `Edit`) are
  preserved.
- Codex CLI / VS Code extension: there is no per-server MCP allowlist in generated config; use
  Codex CLI approval flags if you want to bypass prompts globally.

## Refresh / restart guidance (failure modes)

General rule:
- After changing source-of-truth files (`instructions/`, `workflows/`, `mcp/servers.json`, `policy/commands.json`) → run `node sync/sync.mjs` (or run your CLI via `./al ...`) → then refresh/restart the client as needed.

### MCP prompt server (workflows as “slash commands”)

Workflows are exposed as MCP prompts by:
- `mcp/agent-layer-prompts/server.mjs`

**Required one-time install (per machine / per clone):**
```bash
cd mcp/agent-layer-prompts
npm install
```

Dependency upgrades (maintainers):
- update `mcp/agent-layer-prompts/package.json`, then run `npm install` to refresh `package-lock.json`.

If you changed `workflows/*.md`:
- run `node sync/sync.mjs` (or `./al <cmd>`)
- then refresh MCP discovery in your client (or restart the client/session)

---

## Quick examples (per client)

Gemini CLI:
- Slash command example: `/find-issues`
- MCP check: `cat .gemini/settings.json` (look for `mcpServers["agent-layer"]`)
- Prompt example: `Summarize the repo rules in 3 bullets.`

VS Code / Copilot Chat:
- Slash command example: `/mcp.agent-layer.find-issues`
- MCP check: `cat .vscode/mcp.json` (look for `agent-layer`)
- Prompt example: `Summarize the repo rules in 3 bullets.`

Claude Code CLI:
- Slash command example: `find-issues` (via MCP prompt UI/namespace)
- MCP check: `cat .mcp.json` (look for `mcpServers["agent-layer"]`)
- Prompt example: `Summarize the repo rules in 3 bullets.`

Codex CLI:
- Slash command example: `$find-issues` (Codex Skills)
- MCP check: `cat .codex/config.toml` (look for `mcp_servers.agent-layer`)
- Prompt example: `Summarize the repo rules in 3 bullets.`

Codex VS Code extension:
- Slash command example: `$find-issues` (Codex Skills; requires `CODEX_HOME` pointing at the repo; see Codex section below)
- MCP check: `cat .codex/config.toml` (requires `CODEX_HOME` in VS Code env)
- Prompt example: `Summarize the repo rules in 3 bullets.`

Antigravity:
- Slash commands: not supported
- MCP check: not supported
- Prompt examples: not supported

## Client-specific notes (MCP config + slash commands)

Each section below answers two questions:

1) **How do I know MCP config is being read?**
2) **How do I know workflow slash commands are available?**

### Gemini CLI

**MCP config file**
- Project MCP config is in the working repo root: `.gemini/settings.json` (generated).
- Quick check:
  ```bash
  cat .gemini/settings.json
  ```
  Confirm you see `"mcpServers"` with the servers you expect (e.g., `agent-layer`, `context7`).

**Confirm the MCP server can start**
- Ensure Node deps are installed:
  ```bash
  cd mcp/agent-layer-prompts && npm install && cd -
  ```
- Then run Gemini via `./al gemini`.

**Confirm slash commands (MCP prompts)**
- In Gemini, try a workflow name directly:
  - `/find-issues`
- If it’s present, it will expand and run the workflow prompt.
- If it’s missing:
  1) run `node sync/sync.mjs`
  2) restart Gemini
  3) confirm `.gemini/settings.json` still lists `agent-layer` under `mcpServers`

**Common failure mode**
- If Gemini prompts for approvals on shell commands like `git status`, that is a *shell tool approval*, not MCP. (Solving this uses the repo allowlist `policy/commands.json` projected into Gemini’s `tools.allowed`.)

---

### VS Code / Copilot Chat

**MCP config file**
- Project MCP config is in the working repo root: `.vscode/mcp.json` (generated).
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
  - `/mcp.agent-layer.find-issues`
- If it autocompletes, the prompt is registered.

**Common failure mode**
- VS Code can cache tool lists. Reset cached tools and reload window if needed.

---

### Claude Code CLI

**MCP config file**
- Project MCP config is in the working repo root: `.mcp.json` (generated).
- Quick check:
  ```bash
  cat .mcp.json
  ```

**Confirm MCP is connected**
- Launch Claude Code CLI from repo root:
  ```bash
  ./al claude
  ```
- If MCP servers are not available:
  1) verify `.mcp.json` exists and includes `mcpServers["agent-layer"]`
  2) ensure MCP prompt server deps installed:
     ```bash
     cd mcp/agent-layer-prompts && npm install && cd -
     ```
  3) restart Claude Code CLI after MCP config changes

**Confirm slash commands (MCP prompts)**
- In Claude Code CLI, invoke the MCP prompt using its MCP prompt UI/namespace (varies by client build).
- Quick sanity check: the prompt list should include your workflow prompt name (e.g., `find-issues`).
- If missing:
  1) run `node sync/sync.mjs`
  2) restart Claude Code CLI
  3) ensure the MCP server process can run (Node installed, deps installed)

---

### Codex (CLI / VS Code extension)

**MCP config + system instructions**
- When launched via `./al codex`, `CODEX_HOME` is set to the repo-local `.codex/`.
- MCP servers are generated into `.codex/config.toml` from `.agent-layer/mcp/servers.json`.
- System instructions are generated into `.codex/AGENTS.md` from `.agent-layer/instructions/*.md`.
- Agent Layer also generates the project `AGENTS.md` from the same sources for clients that read it.
- Agent Layer uses **Codex Skills** (and optional rules) as the primary “workflow command” mechanism.

**Getting the Codex VS Code extension to use repo-local `CODEX_HOME`**
- The extension reads `CODEX_HOME` from the VS Code/Antigravity process environment at startup (no workspace setting).
- Set `CODEX_HOME` to the absolute path of this repo's `.codex/`, then fully restart the app.

macOS (recommended; if you have the `code` CLI available):
```bash
CODEX_HOME="$PWD/.codex" code .
```

If you launch from Dock/Finder, set the GUI env once per login:
```bash
launchctl setenv CODEX_HOME "/absolute/path/to/repo/.codex"
# restart VS Code/Antigravity (log out/in to clear)
```

Optional wrapper (handy if you work across multiple repos):
- Create a small script that exports `CODEX_HOME` and launches VS Code/Antigravity.
- If your build supports `chatgpt.cliExecutable`, point it at a wrapper that sets `CODEX_HOME` before invoking `codex`.

**Confirm workflow “commands” (Codex Skills)**
- Skills are generated into the working repo root: `.codex/skills/*/SKILL.md`
- Quick check:
  ```bash
  ls -la .codex/skills
  ```
- In Codex, skills are available under `$`:
  - run `$find-issues`
  - (if your build supports it) list skills with `$skills`

**If a skill is missing**
1) run `node sync/sync.mjs`
2) verify the workflow exists: `workflows/<workflow>.md`
3) verify `.codex/skills/<workflow>/SKILL.md` was generated

**Common failure mode**
- Codex may require a restart to pick up new/updated skills.

---

## What’s inside this repository

### Source-of-truth directories
- `instructions/`  
  Unified instruction fragments (concatenated into shims).
- `workflows/`  
  Workflow definitions (exposed as MCP prompts; also used to generate Codex skills).
- `mcp/servers.json`  
  Canonical MCP server list (no secrets inside).
- `policy/`  
  Auto-approve command allowlist (safe shell command prefixes).

### Project memory files (in working repo)
- `docs/ISSUES.md`  
  Deferred defects, maintainability refactors, technical debt, risks.
- `docs/FEATURES.md`  
  Deferred user feature requests (near-term and backlog).
- `docs/ROADMAP.md`  
  Phased plan of work; guides architecture and sequencing.
- `docs/DECISIONS.md`  
  Rolling log of important decisions (brief).

### Generated outputs (in working repo root)
- Instruction shims:
  - `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `.github/copilot-instructions.md`
- MCP configs projected per client:
  - `.mcp.json`, `.gemini/settings.json`, `.vscode/mcp.json`, `.codex/config.toml`
- Command allowlist configs projected per client:
  - `.gemini/settings.json`, `.claude/settings.json`, `.vscode/settings.json`, `.codex/rules/agent-layer.rules`
- Codex system instructions:
  - `.codex/AGENTS.md`
- Codex skills:
  - `.codex/skills/*/SKILL.md`

### Scripts
- `agent-layer-install.sh`  
  Install/upgrade helper for working repos.
- `setup.sh`  
  One-shot setup (sync + MCP deps + check).
- `sync/sync.mjs`  
  Generator (“build”) for all shims/configs/skills.
- `clean.sh`  
  Remove generated shims/configs/skills and strip agent-layer-managed settings from client config files.
- `with-env.sh`  
  Load `.agent-layer/.env` (and optionally project `.env`) then exec a command.
- `./al`  
  Repo-local launcher (sync + env load + exec; symlink recommended at working repo root).

### Testing
Dev-only prerequisites (not required to use the tool):
- `bats` (macOS: `brew install bats-core`; Ubuntu: `apt-get install bats`)
- `shfmt` (macOS: `brew install shfmt`; Ubuntu: `apt-get install shfmt`)
- `shellcheck` (macOS: `brew install shellcheck`; Ubuntu: `apt-get install shellcheck`)
- `npm install` (installs Prettier for JS formatting)

Dev bootstrap (installs dev deps + enables git hooks):
- `./dev/bootstrap.sh`

Run tests (includes sync check + formatting/lint):
- `./tests/run.sh`

Autoformat (shell + JS):
- `./dev/format.sh`

## Cleanup / uninstall

Remove generated files and agent-layer-managed settings:
```bash
./clean.sh
```

To remove Agent Layer from a repo entirely:
- delete `.agent-layer/` and `./al`
- remove the `# >>> agent-layer` block from `.gitignore`

## FAQ / Troubleshooting

### “I edited generated JSON and now things are broken.”
Generated JSON files (`.mcp.json`, `.vscode/mcp.json`, `.gemini/settings.json`) do not allow comments and may be strict-parsed by clients.

Fix:
1) revert the generated file(s)
2) edit the source-of-truth (`mcp/servers.json`)
3) run `node sync/sync.mjs`

### “I edited instructions but the agent didn’t follow them.”
- Did you run `node sync/sync.mjs` (or run via `./al ...`)?
- Did you restart the session/client (many tools read system instructions at session start)?
- For Gemini CLI, refresh memory (often `/memory refresh`) or start a new session.

### “I edited workflows but the prompt/command list didn’t update.”
- Run `node sync/sync.mjs`
- Restart/refresh MCP discovery:
  - Gemini: restart Gemini and/or run MCP refresh if available in your build
  - VS Code: restart servers / reset cached tools
  - Claude Code CLI: restart Claude Code CLI after MCP config changes

### “Commits are failing after enabling hooks.”
The hook runs:

```bash
./tests/run.sh
```

If it fails, fix the reported issues (formatting, lint, tests, or sync), then commit again.

### “Can I rename the instruction files?”
Yes. Keep numeric prefixes if you want stable ordering without changing `sync/sync.mjs`.

## Contributing

1) Ensure prerequisites are installed (Node LTS, git). If you use `nvm`, run `nvm use` in `.agent-layer/`.
2) Run the dev bootstrap (installs dev deps, enables hooks):
   ```bash
   ./dev/bootstrap.sh
   ```
3) Before committing:
   ```bash
   ./tests/run.sh
   ```
4) Autoformat (shell + JS) when needed:
   ```bash
   ./dev/format.sh
   ```

## License

MIT license. See `LICENSE.md`.

## Attribution

- Nicholas Conn (developer)
- Conn Castle Studios (company)
