# Agent Layer (repo-local agent standardization)

Agent Layer is an opinionated framework for AI‑assisted development: one set of instructions, workflows, MCP servers, and project memory (features, roadmap, issues, decisions) keeps Claude, Gemini, VS Code/Copilot, and Codex aligned across all your projects. It turns "vibe coding" into "agent orchestration" by giving every tool the same context and direction—without requiring team buy-in or committing config to each repo.

## What this is for

**Goal:** make agentic tooling consistent across Claude Code CLI, Gemini CLI, VS Code/Copilot, and Codex by keeping a **single source of truth** that you can use across multiple projects, then generating the per-client shim/config files those tools require.

**Primary uses**
- A unified instruction set (system/developer-style guidance) usable across tools.
- Repeatable "workflows" exposed as:
  - MCP prompts (slash commands) in Claude and Gemini.
  - VS Code prompt files for Copilot Chat.
  - Codex Skills (repo-local) for Codex.
- An MCP server catalog maintained in your agent-layer fork, projected into each client's config format.
- A safe command allowlist maintained in your agent-layer fork, projected into each client's auto-approval settings.
- A lightweight setup flow that works in any project repo.

### Support matrix

| Client | System instructions | Slash commands | MCP servers | Approved command list | Approved MCP tools |
| --- | --- | --- | --- | --- | --- |
| Gemini CLI | ✅ | ✅ | ✅ | ✅ | ✅ |
| Claude Code CLI | ✅ | ✅ | ✅ | ✅ | ✅ |
| VS Code / Copilot Chat | ✅ | ✅ | ✅ | ✅ | ❌ |
| Codex CLI | ✅ | ✅ | ✅ | ✅ | ❌ |
| Codex VS Code extension | ✅ | ✅ | ✅ | ✅ | ❌ |
| Antigravity | ✅ | ✅ | ❌ | ❌ | ❌ |

Note: Codex artifacts live in `.codex/`. The CLI uses them when launched via `./al codex`, and `./al codex` warns if `CODEX_HOME` is set to a different path (it never overrides; it only sets repo-local `.codex/` when unset). The VS Code extension only uses them if the extension host sees the same `CODEX_HOME` (see "Codex (CLI / VS Code extension)" below). Antigravity reads `GEMINI.md` for instructions and `.agent/workflows/*.md` for workflows; MCP servers and approvals are not supported.

---

## Quick Start (2 Minutes)

The fastest way to try agent-layer in your existing project:

```bash
# 1. Download and run installer (from your parent root)
curl -fsSL https://github.com/nicholasjconn/agent-layer/releases/latest/download/agent-layer-install.sh | bash

# 2. Run setup once (installer already ran this; re-run only after config changes or if dependencies were cleaned)
./al --setup

# 3. Try it with gemini, codex, or claude
./al gemini
```

Once Gemini starts, type:
```
Please explain how you handle memory.
```

If you see a response that mentions your project's memory files (issues, features, roadmap, decisions) and summarizes the agent instructions, it's working!

**What just happened?**
1. Installer created `.agent-layer/` in your project (gitignored by default)
2. Setup installed dependencies and generated configs
3. `./al gemini` synced configs and launched Gemini with your project context
4. Gemini read your instructions and can now help with your project

**Note**: `.agent-layer/` is gitignored, so you can try this without team buy-in or committing anything.

**Next**: See [Installation](#installation) for details and customization options.

---

## Table of Contents

- [Key Concepts](#key-concepts)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [First Steps](#first-steps)
- [Agent-Specific Guide](#agent-specific-guide)
- [How Commands Work in This README](#how-commands-work-in-this-readme)
- [Parent Root Resolution](#parent-root-resolution)
- [Day-to-Day Usage](#day-to-day-usage)
- [Customizing Your Agent](#customizing-your-agent)
- [Approvals and Permissions](#approvals-and-permissions)
- [Reference](#reference)
- [Troubleshooting](#troubleshooting)
- [Cleanup / Uninstall](#cleanup--uninstall)
- [Contributing](#contributing)
- [FAQ](#faq)
- [License](#license)
- [Attribution](#attribution)

## Key Concepts

Before you start, understand these three components:

### 1. Your Project (Parent Root)
This is your application's main directory - the repo you're already working in.
Example: `/Users/you/my-app/`

### 2. Agent Layer (`.agent-layer/`)
A subdirectory inside your project that contains all agent configuration.
Example: `/Users/you/my-app/.agent-layer/`
This is where instructions, workflows, and MCP server configs live.

**By default**, `.agent-layer/` is not committed to your project repo (it's gitignored). This lets you use agent-layer individually without requiring team buy-in. For team use, fork and maintain your own version of agent-layer instead of committing config to each project.

### 3. Launcher (`./al`)
A script in your parent root that syncs configs and launches AI tools. It executes `.agent-layer/agent-layer`.
Example: `./al gemini` (runs Gemini with your project's agent config)

**Mental Model**:
```
my-app/                        ← Your project (parent root)
├── .agent-layer/              ← Agent Layer (agent layer root)
│   ├── config/                ← Your instructions, workflows, MCP servers
│   │   ├── instructions/      ← System instructions (source of truth)
│   │   ├── workflows/         ← Workflow definitions (source of truth)
│   │   ├── agents.json        ← Agent enablement + default args (source of truth)
│   │   ├── mcp-servers.json   ← MCP server catalog (source of truth)
│   │   └── policy/            ← Command allowlist (source of truth)
│   ├── agent-layer            ← CLI entrypoint (used by ./al)
│   └── src/cli.mjs            ← CLI implementation (sync/inspect/clean/setup/mcp)
├── al                         ← Launcher (wrapper script, or symlink)
├── .mcp.json                  ← Generated (don't edit)
├── AGENTS.md                  ← Generated (don't edit)
├── .gemini/                   ← Generated Gemini configs
├── .claude/                   ← Generated Claude configs
├── .vscode/                   ← Generated VS Code configs
├── .codex/                    ← Generated Codex artifacts
└── docs/                      ← Project memory (ISSUES.md, FEATURES.md, etc.)
```

**How it works**:
1. You edit source files in `.agent-layer/config/` (once, for all your projects)
2. Sync generates client configs per-project (`.mcp.json`, `.gemini/settings.json`, etc.)
3. AI clients read those generated configs automatically
4. All your AI tools stay aligned with the same instructions across all projects

**Note**: Agent-layer is designed to be used across multiple projects with minimal per-project customization. You maintain one version of `.agent-layer/` and install it in each project you work on.

---

## Prerequisites

Required:
- **Node.js + npm** (LTS >=20 recommended; `.nvmrc` included for devs; can use `mise`, `asdf`, `volta`, or `nvm`)
- **curl** (for the install script)
- **git** (required for install/setup; also required if contributing to agent-layer development)

Optional (depending on which clients you use):
- VS Code (Copilot Chat)
- Gemini CLI
- Claude Code CLI
- Codex CLI / Codex VS Code extension

**Operating System**: This tooling is built for macOS. Other operating systems are untested; if you choose to use the optional `./al` symlink instead of the default wrapper script, note that symlinks require extra setup on Windows.

**Node version management**: Contributors using `nvm` can run `nvm use` in the agent-layer repo root (or in `.agent-layer/` when installed in a consumer repo) to match `.nvmrc`.

**Note for contributors**: If you're developing agent-layer itself (not just using it in your project), see the [Contributing](#contributing) section.

---

## Installation

### Install in Your Project (Parent Root)

From your parent root directory:

```bash
# Download installer
curl -fsSL https://github.com/nicholasjconn/agent-layer/releases/latest/download/agent-layer-install.sh -o agent-layer-install.sh
chmod +x agent-layer-install.sh

# Run installer
./agent-layer-install.sh
```

Fresh installs pin `.agent-layer/` to the latest tagged release by default (detached HEAD). Use `--latest-branch` for dev builds. Check the installed version with `./al --version`.

Cleanup: if you downloaded `agent-layer-install.sh`, remove it after install. The installer deletes itself when run from a downloaded file outside `.agent-layer/`.

This creates `.agent-layer/`, adds a managed `.gitignore` block (ignoring `.agent-layer/` by default), creates `./al`,
and ensures the project memory files exist under `docs/` (`ISSUES.md`, `FEATURES.md`,
`ROADMAP.md`, `DECISIONS.md`). Templates live in `.agent-layer/config/templates/docs`; if a file
already exists, the installer prompts to keep it (default yes). In non-interactive
runs, existing files are kept.
On fresh installs, the installer prompts to enable each supported agent (default yes) and writes the choices to `.agent-layer/config/agents.json`.
In non-interactive runs, all agents are enabled; edit the file later and re-run `./al --sync` to change the selection.

**Note**: `.agent-layer/` is gitignored by default so you can use it individually without team buy-in. For team use, fork agent-layer and have team members install your fork instead of committing config to each project repo.

### Upgrade Existing Installation

**Upgrade to latest tagged release**:

Fresh installs already pin to the latest tagged release; use this to update an existing `.agent-layer/`.

```bash
./.agent-layer/agent-layer-install.sh --upgrade
```

Notes:
- Preserves user config unless `--force` is passed.
- Other local changes require a clean `.agent-layer` working tree (commit or stash first).
- Checks out the latest tag (detached HEAD) and prints the commit list since the current version.
- If no `origin` remote is configured, pass `--repo-url` or set `AGENT_LAYER_REPO_URL`.

**Update to latest commit of a branch** (for developers):

```bash
./.agent-layer/agent-layer-install.sh --latest-branch main
```

Notes:
- Preserves user config unless `--force` is passed.
- Other local changes require a clean `.agent-layer` working tree (commit or stash first).
- Fetches from the remote only; checks out the latest commit in detached HEAD mode.
- Re-run the command to pull the newest commit again.

**Install a specific tagged release**:

```bash
./.agent-layer/agent-layer-install.sh --version v0.1.0
```

Notes:
- Preserves user config unless `--force` is passed.
- Other local changes require a clean `.agent-layer` working tree (commit or stash first).
- Errors if the requested tag does not exist after fetching.

---

## First Steps

After installation, follow these steps to get agent-layer working:

### Step 1: Run Setup

From your parent root:

```bash
./al --setup
```

If you installed via the installer, setup already ran once. Re-run it only after config changes or if you cleaned dependencies. It installs MCP prompt server dependencies, runs sync (generates configs), and checks for drift. `./al` still runs sync before each command.

### Step 2: Configure Environment (Required)

Agent-layer needs API tokens for MCP servers. Create `.agent-layer/.env` for agent-only secrets:

```bash
# If .env doesn't exist yet:
cp .agent-layer/.env.example .agent-layer/.env

# Edit .env with your tokens (don't commit this file)
```

The installer creates `.env` from `.env.example` if it is missing.

**Required tokens (unless you disable the servers in `.agent-layer/config/mcp-servers.json`)**:
- `GITHUB_PERSONAL_ACCESS_TOKEN` (GitHub MCP server; setup: https://github.com/github/github-mcp-server)
- `CONTEXT7_API_KEY` (Context7 MCP server; setup: https://github.com/upstash/context7)
- `TAVILY_API_KEY` (Tavily MCP server; setup: https://tavily.com/)

To disable a server, set `enabled: false` or limit `clients` in `.agent-layer/config/mcp-servers.json`.

Required: review `.agent-layer/config/mcp-servers.json` to confirm which servers are enabled.

Optional customization:
- Edit instructions: `.agent-layer/config/instructions/*.md`
- Edit workflows: `.agent-layer/config/workflows/*.md`

**Gemini note**: when the GitHub MCP server is enabled, sync inlines your personal access token into `.gemini/settings.json`. Keep `.gemini/` gitignored (the installer adds this by default).

**Note**: Keep agent-only secrets separate from your application's `.env` (if you have one). Agent-layer reads `.agent-layer/.env`, not your parent root `.env`.

### Step 3: Codex Users (Special Setup)

**If you use the Codex VS Code extension**, you need to launch VS Code with `CODEX_HOME` set.

**macOS Finder launcher**:
- Use `./al --open-vscode` to launch VS Code for this repo. If `CODEX_HOME` is set to a different path, it warns and leaves it unchanged.
- `.agent-layer` is hidden in Finder; use `Command+Shift+.` to show hidden files.
- The launcher leaves its Terminal window open after launch.
- If you need to switch repos, fully quit VS Code first so `CODEX_HOME` is re-read.

**Quick command** (from parent root):
```bash
CODEX_HOME="$PWD/.codex" code .
```

**Why**: The Codex VS Code extension only works when `CODEX_HOME` points at the repo-local `.codex/`. If you launch VS Code normally, it won't see the Codex artifacts.

See the [Codex section](#codex-cli--vs-code-extension) for full details.

### Step 4: Try It Now

Test that agent-layer is working:

```bash
# From your parent root:
./al gemini
```

Once Gemini starts, try:
```
Please explain how you handle memory.
```

**Expected result**: Gemini should summarize your project's agent instructions.

**If it doesn't work**: See [Troubleshooting](#troubleshooting).

**What just happened?**
1. `./al gemini` synced your agent config (regenerated files from `.agent-layer/config/`)
2. Generated client configs (`.gemini/settings.json`, `.mcp.json`, etc.)
3. Launched Gemini with those configs loaded
4. Gemini read your instructions and can now help with your project

---

## Agent-Specific Guide

### Quick Examples (Per Client)

Gemini CLI:
- Slash command example: `/find-issues`
- MCP check: `cat .gemini/settings.json` (look for `mcpServers["agent-layer"]`)
- Prompt example: `Summarize the repo rules in 3 bullets.`

VS Code / Copilot Chat:
- Slash command example: `/find-issues` (prompt file)
- MCP check: `cat .vscode/mcp.json` (look for tool servers like `context7` or `github`)
- Prompt example: `Summarize the repo rules in 3 bullets.`

Claude Code CLI:
- Slash command example: `find-issues` (via MCP prompt UI/namespace)
- MCP check: `cat .mcp.json` (look for `mcpServers["agent-layer"]`)
- Prompt example: `Summarize the repo rules in 3 bullets.`

Codex CLI:
- Slash command example: `$find-issues` (Codex Skills)
- MCP check: `cat .codex/config.toml` (look for `mcp_servers.context7` or `mcp_servers.github`)
- Prompt example: `Summarize the repo rules in 3 bullets.`

Codex VS Code extension:
- Slash command example: `$find-issues` (Codex Skills; requires `CODEX_HOME` pointing at the repo; see Codex section below)
- MCP check: `cat .codex/config.toml` (look for `mcp_servers.context7` or `mcp_servers.github`; requires `CODEX_HOME` in VS Code env)
- Prompt example: `Summarize the repo rules in 3 bullets.`

Antigravity:
- Instructions: `GEMINI.md` (generated)
- Slash commands: `.agent/workflows/*.md` (generated from workflows)
- MCP check: not supported
- Prompt example: `find-issues` (from `.agent/workflows/find-issues.md`)
- Format limits: description <= 250 chars, content <= 12000 chars

### Gemini CLI

**MCP config file**
- Project MCP config is in the parent root: `.gemini/settings.json` (generated).
- Quick check:
  ```bash
  cat .gemini/settings.json
  ```
  Confirm you see `"mcpServers"` with the servers you expect (e.g., `agent-layer`, `context7`).

**Confirm the MCP server can start**
- If you ran `./al --setup`, Node deps are already installed. If you skipped setup or cleaned `node_modules`, install them:
  ```bash
  cd .agent-layer/src/mcp/agent-layer-prompts && npm install && cd -
  ```
- Then run Gemini via `./al gemini`.

**Confirm slash commands (MCP prompts)**
- In Gemini, try a workflow name directly:
  - `/find-issues`
- If it's present, it will expand and run the workflow prompt.
- If it's missing:
  1) run `./al --sync`
  2) restart Gemini
  3) confirm `.gemini/settings.json` still lists `agent-layer` under `mcpServers`

**Common failure mode**
- If Gemini prompts for approvals on shell commands like `git status`, that is a *shell tool approval*, not MCP. (Solving this uses the repo allowlist `config/policy/commands.json` projected into Gemini's `tools.allowed`.)

---

### VS Code / Copilot Chat

**MCP config file**
- Project MCP config is in the parent root: `.vscode/mcp.json` (generated).
- Quick check:
  ```bash
  cat .vscode/mcp.json
  ```

**Confirm MCP server is running**
- Open the repo in VS Code.
- Ensure Copilot Chat is enabled and MCP is enabled in your environment.
- If MCP tools/prompts look stale:
  - restart MCP servers and/or run VS Code's "Chat: Reset Cached Tools" action.

**Prompt files (short slash commands)**
- Prompt files are generated into `.vscode/prompts/*.prompt.md`.
- In Copilot Chat, invoke:
  - `/find-issues`
- If it autocompletes, the prompt file is registered.

**MCP prompt server**
- VS Code MCP config only includes tool servers; workflow prompts come from `.vscode/prompts`.

**Common failure mode**
- VS Code can cache tool lists. Reset cached tools and reload window if needed.

---

### Claude Code CLI

**MCP config file**
- Project MCP config is in the parent root: `.mcp.json` (generated).
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
     cd .agent-layer/src/mcp/agent-layer-prompts && npm install && cd -
     ```
  3) restart Claude Code CLI after MCP config changes

**Confirm slash commands (MCP prompts)**
- In Claude Code CLI, invoke the MCP prompt using its MCP prompt UI/namespace (varies by client build).
- Quick sanity check: the prompt list should include your workflow prompt name (e.g., `find-issues`).
- If missing:
  1) run `./al --sync`
  2) restart Claude Code CLI
  3) ensure the MCP server process can run (Node installed, deps installed)

---

### Codex (CLI / VS Code extension)

**MCP config + system instructions**
- When launched via `./al codex`, `CODEX_HOME` should resolve to the repo-local `.codex/` (symlinks allowed); `./al codex` warns if it points elsewhere and never overrides it.
- MCP servers are generated into `.codex/config.toml` from `.agent-layer/config/mcp-servers.json` (per-client filtering skips the agent-layer prompt server).
- System instructions are generated into `.codex/AGENTS.md` from `.agent-layer/config/instructions/*.md`.
- Agent Layer also generates the project `AGENTS.md` from the same sources for clients that read it.
- Agent Layer uses **Codex Skills** (and optional rules) as the primary "workflow command" mechanism.

**Getting the Codex VS Code extension to use repo-local `CODEX_HOME`**
- The extension reads `CODEX_HOME` from the VS Code/Antigravity process environment at startup (no workspace setting).
- Set `CODEX_HOME` to the absolute path of this repo's `.codex/`, then fully restart the app.
- See First Steps step 3 for the recommended launcher commands.

Optional wrapper (handy if you work across multiple repos):
- Create a small script that exports `CODEX_HOME` and launches VS Code/Antigravity.
- If your build supports `chatgpt.cliExecutable`, point it at a wrapper that sets `CODEX_HOME` before invoking `codex`.

Quick verification inside VS Code:
```bash
echo "$CODEX_HOME"
```

**Confirm workflow "commands" (Codex Skills)**
- Skills are generated into the parent root: `.codex/skills/*/SKILL.md`
- Quick check:
  ```bash
  ls -la .codex/skills
  ```
- In Codex, skills are available under `$`:
  - run `$find-issues`
  - (if your build supports it) list skills with `$skills`

**If a skill is missing**
1) run `./al --sync`
2) verify the workflow exists: `config/workflows/<workflow>.md`
3) verify `.codex/skills/<workflow>/SKILL.md` was generated

**Common failure mode**
- Codex may require a restart to pick up new/updated skills.

---

## How Commands Work in This README

**Most commands assume you're in your parent root directory** (the parent root).

When you see commands like:
```bash
./al gemini
./al --sync
```

Run them from your parent root (where `./al` lives), not from inside `.agent-layer/`.

This README sticks to that style for consistency.

**Quick reminder**:
- `./al` means "run the al script in my current directory"
- `.agent-layer/` means "the .agent-layer subdirectory"

---

## Parent Root Resolution

Agent-layer resolves `PARENT_ROOT` in this precedence order (first match wins):
1. `--parent-root <path>`
2. `--temp-parent-root`
3. `PARENT_ROOT` in `.agent-layer/.env` (explicit config)
4. Discovery (only when the agent layer directory is named `.agent-layer`)
5. Error

All resolved paths are normalized with `pwd -P` (realpath) before comparisons.
Note: the `.env` file lives at `AGENT_LAYER_ROOT/.env` (typically `.agent-layer/.env`; in the agent-layer repo it is `./.env`).

### Scenarios (Summary)

1) **Explicit parent root (flag)**  
   Use `--parent-root <path>`. The parent root must contain a `.agent-layer` entry (dir or symlink) that resolves to the running agent-layer.

2) **Temporary parent root (flag)**  
   Use `--temp-parent-root` to create a temporary parent root, symlink `.agent-layer` into it, and clean it up on exit (unless `PARENT_ROOT_KEEP_TEMP=1`). Temp parent roots use the system temp directory and fall back to `<agent-layer-root>/tmp`; `PARENT_ROOT` does not change this location.

3) **Explicit parent root (.env)**  
   Set `PARENT_ROOT` in `.agent-layer/.env`. Relative paths are resolved from the agent-layer root. The parent root must contain a `.agent-layer` entry (dir or symlink) that resolves to the running agent-layer.

4) **Installed `.agent-layer` (discovery)**  
   You’re in a consumer repo and `.agent-layer/` is the directory name. With no flags and no `PARENT_ROOT` in `.agent-layer/.env`, agent-layer uses the parent of `.agent-layer` as `PARENT_ROOT`.

5) **No valid parent root (error)**  
   In the agent-layer dev repo (directory name `agent-layer`), discovery is blocked. You must use `--parent-root`, `--temp-parent-root`, or set `PARENT_ROOT` in `.agent-layer/.env`.

### How to Tell Which Scenario You're In

- If you passed `--parent-root`, you're in explicit parent root (Scenario 1).
- If you passed `--temp-parent-root`, you're in temp root (Scenario 2).
- If `PARENT_ROOT` is set in `.agent-layer/.env` and you didn't provide flags, you're in explicit parent root via .env (Scenario 3).
- If your agent layer directory is named `.agent-layer` and you didn't provide flags or a `PARENT_ROOT` in `.env`, you're in discovery (Scenario 4).
- If the directory name is `agent-layer` and you didn't provide flags or a `PARENT_ROOT` in `.env`, you'll get an error (Scenario 5).

### Environment Loading

- `./al` loads `$AGENT_LAYER_ROOT/.env` when it exists.
- If `.agent-layer/.env` defines `PARENT_ROOT` and no root flags are used, it becomes the explicit parent root.
- `./al` does not load the parent root `.env`; export it in your shell if needed.

---

## Day-to-Day Usage

### CLI options (short list)

| Option | What it's for |
| --- | --- |
| `./al <command>` | Sync + load `.agent-layer/.env` + run a command. |
| `./al --no-sync <command>` | Run without syncing first. |
| `./al --sync` | Regenerate configs from sources. |
| `./al --inspect` | Print divergence report (JSON). |
| `./al --clean` | Remove agent-layer-managed outputs. |
| `./al --setup` | Run setup (sync + MCP deps + check). |
| `./al --wizard` | Interactive configuration of enabled agents and model defaults. |
| `./al --mcp-prompts` | Run the MCP prompt server. |
| `./al --open-vscode` | Launch VS Code with repo-local `CODEX_HOME` when unset (warns and keeps existing value when set elsewhere). |
| `./al --version` | Print version. |
| `./al --help` | Show usage. |

Root flags for development/testing: `--parent-root <path>`, `--temp-parent-root`, `--agent-layer-root <path>`.

### Prefer `./al` for Running CLIs

`./al` runs the Node CLI entrypoint. By default it:

1) Runs sync (regenerates configs from sources)
2) Loads `.env` (API tokens and settings)
3) Executes the command

**Examples**:

```bash
./al gemini
./al claude
./al codex
```

If a launch fails with "is disabled", update `.agent-layer/config/agents.json` and re-run `./al --sync`.

If you want to skip the sync step for a fast launch when you know outputs are current:
```bash
./al --no-sync gemini
```
`./al` does not change your working directory.

### Verify Environment Variables Are Loaded

```bash
./al env | grep -E 'GITHUB_PERSONAL_ACCESS_TOKEN|CONTEXT7_API_KEY'
```

---

## Customizing Your Agent

### What Files to Edit (Sources of Truth)

Agent Layer uses a "source of truth" model:
- **You edit sources** (in `.agent-layer/config/`)
- **Sync generates outputs** (in your parent root)
- **Clients read outputs** (automatically)

**Edit these (sources of truth)**:
- `config/instructions/*.md` - System instructions for AI agents
- `config/workflows/*.md` - Workflow definitions (Claude/Gemini MCP prompts, VS Code prompt files, Codex skills)
- `config/agents.json` - Agent enablement and default CLI args (applied by `./al`)
- `config/mcp-servers.json` - MCP server catalog
- `config/policy/commands.json` - Command allowlist (safe shell commands)

**Never edit these directly (generated by sync)**:
- `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`
- `.github/copilot-instructions.md`
- `.mcp.json`, `.gemini/settings.json`, `.claude/settings.json`
- `.vscode/mcp.json`, `.vscode/settings.json`
- `.codex/AGENTS.md`, `.codex/config.toml`, `.codex/rules/default.rules`
- `.codex/skills/*/SKILL.md`

**Why this matters**: If you edit generated files, sync will overwrite them. Always edit sources, then run sync.

**Per-client MCP servers**: In `config/mcp-servers.json`, set `clients` to an allowlist of client IDs (`claude`, `gemini`, `vscode`, `codex`). If omitted, the server is included for all clients.

### Agent enablement and default args

`config/agents.json` lists every supported agent (even if disabled). Set `enabled: true` to generate its outputs and allow `./al <agent>` to launch it; set `enabled: false` to skip outputs and block launches. After changes, run `./al --sync`. If you disable an agent and already have outputs, sync warns; remove them with `./al --clean` or delete them manually.

You can also set `defaultArgs` per agent; `./al` appends them unless you already pass the same flag. `defaultArgs` must be `--flag` tokens with optional values (either as the next array entry or as `--flag=value`). Positional args and short flags are not supported; if a value starts with `-`, use `--flag=value` to avoid ambiguity.

Example snippet:
```json
{
  "codex": {
    "enabled": true,
    "defaultArgs": [
      "--model",
      "gpt-5.2-codex",
      "--config",
      "model_reasoning_effort=high"
    ]
  }
}
```

### If You Accidentally Edited a Generated File

Delete it and re-sync (example from parent root):

```bash
rm .mcp.json
./al --sync
```

### Regenerate After Changes

`./al` runs sync automatically; use this only if you want to regenerate without launching a CLI.

- `./al --sync`

### Instruction File Ordering (Why the Numbers)

`src/sync/sync.mjs` concatenates `config/instructions/*.md` in **lexicographic order**.

Numeric prefixes (e.g. `00_`, `10_`, `20_`) ensure a **stable, predictable ordering** without requiring a separate manifest/config file. If you add new instruction fragments, follow the same pattern.

---

## Approvals and Permissions

Agent Layer treats `.agent-layer/config/mcp-servers.json` as the source of truth for MCP tool approvals.
Set `trust: true` per server (or `defaults.trust` for the default) to auto-approve that server's
tools where supported.
The default config sets `defaults.trust: true`; change it to `false` if you want prompts by default.

Behavior by client:
- Gemini CLI: `mcpServers.<name>.trust` is generated from `trust` (with defaults fallback).
- Claude Code: `permissions.allow` includes `mcp__<server>__*` for trusted servers, alongside the
  Bash allowlist from `config/policy/commands.json`. Non-managed allow entries (for example, `Edit`) are
  preserved by default (use `--overwrite` to drop them).
- Codex CLI / VS Code extension: there is no per-server MCP allowlist in generated config; use
  Codex CLI approval flags if you want to bypass prompts globally.

### Inspect Divergent Configs

If you approve commands or edit MCP settings directly in a client, Agent Layer may detect divergence and print:

```
agent-layer sync: WARNING: client configs diverge from .agent-layer sources (approvals: 2, mcp: 1).

Details:
- approvals: 2
- mcp: 1

Notes:
- Sync preserves existing client entries by default; it will not overwrite them unless you pass --overwrite or choose overwrite in --interactive.

Next steps:
- Run: ./al --inspect (JSON report)
- Add them to .agent-layer/config/policy/commands.json or .agent-layer/config/mcp-servers.json, then re-run sync (`./al --sync`)
- To discard client-only entries, run: ./al --sync --overwrite
- For interactive review, run: ./al --sync --interactive
```

The inspect script emits a JSON report of divergent approvals and MCP servers and **never** edits files.
Use the report to update `.agent-layer/config/policy/commands.json` (approvals) or `.agent-layer/config/mcp-servers.json` (MCP servers),
then run `./al --sync` to regenerate outputs.
Inspect only reports divergences for enabled agents in `config/agents.json`; the report includes a note listing any disabled agents it filtered out.

To overwrite client configs instead of preserving divergent entries, run:
- `./al --sync --overwrite` (non-interactive)
- `./al --sync --interactive` (TTY only; prints divergence details and prompts)

Some entries may be flagged as `parseable: false` and require manual updates.
Codex approvals are read only from `.codex/rules/default.rules`. If other `.rules` files exist under `.codex/rules`, Agent Layer ignores them and warns so you can either integrate their entries into `.agent-layer/config/policy/commands.json` and re-sync, or delete the extra rules files to clear the warning.
Codex MCP config documents env requirements in comments only, so divergence checks ignore env var differences unless an explicit `env = { ... }` entry is present.

## Reference

### Glossary

- **Parent root**: Your project's main directory (the repo root containing `.agent-layer/`)
- **Agent layer root**: The `.agent-layer/` directory itself
- **Sync**: Regenerate all client config files from your source files (instructions, workflows, MCP servers)
- **Generated files**: Config files created automatically by sync; don't edit these directly
- **Source of truth**: Your instruction/workflow files in `.agent-layer/config/` - edit these, not generated files
- **Shim**: A small generated file that clients read (like `AGENTS.md`, `.mcp.json`)
- **MCP**: Model Context Protocol - allows AI tools to access external data sources and tools
- **Workflow**: A reusable prompt/command defined in `.agent-layer/config/workflows/`
- **Slash command**: A shortcut that invokes a workflow (e.g., `/find-issues`, `$find-issues`)

### Environment Variables

`./al` loads `.agent-layer/.env` when it exists (the parent root `.env` is not loaded). The default MCP servers in `config/mcp-servers.json` expect:
- `GITHUB_PERSONAL_ACCESS_TOKEN` (GitHub MCP server)
- `CONTEXT7_API_KEY` (Context7 MCP server)

VS Code MCP config uses the generated `.vscode/mcp.json` `envFile`, which defaults to `.agent-layer/.env`.

### File Organization

#### Source-of-Truth Directories (in `.agent-layer/`)
- `config/instructions/` - Unified instruction fragments (concatenated into shims)
- `config/workflows/` - Workflow definitions (Claude/Gemini MCP prompts, VS Code prompt files, Codex skills)
- `config/mcp-servers.json` - Canonical MCP server list (no secrets inside)
- `config/policy/` - Auto-approve command allowlist (safe shell command prefixes)

Workflow names are derived from the workflow filename (case-sensitive, without `.md`). If a workflow frontmatter includes `name`, it must match the filename exactly; use `description` for summaries.

#### Project Memory Files (in parent root `docs/`)
- `docs/ISSUES.md` - Deferred defects, maintainability refactors, technical debt, risks
- `docs/FEATURES.md` - Deferred user feature requests (near-term and backlog)
- `docs/ROADMAP.md` - Phased plan of work; guides architecture and sequencing
- `docs/DECISIONS.md` - Rolling log of important decisions (brief)
- `docs/COMMANDS.md` - Canonical, repeatable commands for this repository

#### Generated Outputs (in parent root)
- Instruction shims:
  - `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `.github/copilot-instructions.md`
- MCP configs projected per client:
  - `.mcp.json`, `.gemini/settings.json`, `.vscode/mcp.json`, `.codex/config.toml`
- Command allowlist configs projected per client:
  - `.gemini/settings.json`, `.claude/settings.json`, `.vscode/settings.json`, `.codex/rules/default.rules`
- VS Code prompt files:
  - `.vscode/prompts/*.prompt.md`
- Antigravity workflows:
  - `.agent/workflows/*.md`
- Codex system instructions:
  - `.codex/AGENTS.md`
- Codex skills:
  - `.codex/skills/*/SKILL.md`

#### Scripts (in `.agent-layer/`)
- `agent-layer-install.sh` - Install/upgrade helper for parent repos
- `agent-layer` - CLI entrypoint (invoked by parent-root `./al`)
- `src/cli.mjs` - Node CLI implementation (sync/inspect/clean/setup/mcp)
- `dev/bootstrap.sh` - Dev bootstrap (repo only)
- `dev/format.sh` - Formatting (repo only)
- `tests/run.sh` - Test runner (repo only)

### Refresh / Restart Guidance (Failure Modes)

General rule:
- After changing source-of-truth files (`config/instructions/`, `config/workflows/`, `config/mcp-servers.json`, `config/policy/commands.json`) → run `./al --sync` → then refresh/restart the client as needed.

#### MCP Prompt Server (Workflows as "Slash Commands")

Workflows are exposed as MCP prompts for Claude and Gemini by:
- `src/mcp/agent-layer-prompts/server.mjs`

VS Code prompt files are generated from the same workflows into:
- `.vscode/prompts/*.prompt.md`

Codex skills are generated from the same workflows into:
- `.codex/skills/*/SKILL.md`

**Note**: `./al --setup` automatically runs `npm install`. Only run this manually if you skipped setup or cleaned `node_modules`:
```bash
cd .agent-layer/src/mcp/agent-layer-prompts
npm install
```

Dependency upgrades (maintainers):
- update `src/mcp/agent-layer-prompts/package.json`, then run `npm install` to refresh `package-lock.json`.

If you changed `config/workflows/*.md`:
- run `./al --sync`
- then refresh MCP discovery in your client (or restart the client/session)
- VS Code prompt files update on sync; reload VS Code if prompt files do not appear
- Antigravity workflows update on sync under `.agent/workflows/*.md`

---

## Troubleshooting

### Quick Fixes (Try These First)

1. **Run `./al` instead of direct CLI** to ensure sync runs
2. **Restart your AI client** after config changes
3. **Check `.agent-layer/.env`** has required tokens (`GITHUB_PERSONAL_ACCESS_TOKEN`, `CONTEXT7_API_KEY`)
4. **Verify MCP server deps**: `cd .agent-layer/src/mcp/agent-layer-prompts && npm install`
5. **Re-run sync manually**: `./al --sync`

### Common Issues

#### "I edited generated JSON and now things are broken."
Generated JSON files (`.mcp.json`, `.vscode/mcp.json`, `.gemini/settings.json`) do not allow comments and may be strict-parsed by clients.

Fix:
1) revert the generated file(s)
2) edit the source-of-truth (`config/mcp-servers.json`)
3) run `./al --sync`

#### "I edited instructions but the agent didn't follow them."
- Did you run `./al --sync`?
- Did you restart the session/client (many tools read system instructions at session start)?
- For Gemini CLI, refresh memory (often `/memory refresh`) or start a new session.

#### "I edited workflows but the prompt/command list didn't update."
- Run `./al --sync`
- Restart/refresh MCP discovery:
  - Gemini: restart Gemini and/or run MCP refresh if available in your build
  - VS Code: restart servers / reset cached tools
  - Claude Code CLI: restart Claude Code CLI after MCP config changes

#### "Commits are failing after enabling hooks."
The hook runs the test runner with a temporary parent root when invoked from the agent-layer repo:

```bash
./tests/run.sh --temp-parent-root
```

If it fails, fix the reported issues (formatting, lint, tests, or sync), then commit again.

#### "Can I rename the instruction files?"
Yes. Keep numeric prefixes if you want stable ordering without changing `src/sync/sync.mjs`.

---

## Cleanup / Uninstall

Remove generated files and agent-layer-managed settings:
```bash
./al --clean
```
Note: `./al --clean` removes generated shims/configs/skills/workflows and agent-layer-managed settings only; it does not delete `docs/` memory files or the `.agent-layer/` directory.

To remove Agent Layer from a repo entirely:
- delete `.agent-layer/` and `./al`
- optionally remove the `# >>> agent-layer` block from `.gitignore` (harmless to leave it)

---

## Contributing

Developing agent-layer itself (not just using it in your project)?

### Prerequisites for Contributors

Dev-only prerequisites (not required to use the tool):
- `bats` (macOS: `brew install bats-core`; Ubuntu: `apt-get install bats`)
- `rg` (macOS: `brew install ripgrep`; Ubuntu: `apt-get install ripgrep`)
- `shfmt` (macOS: `brew install shfmt`; Ubuntu: `apt-get install shfmt`)
- `shellcheck` (macOS: `brew install shellcheck`; Ubuntu: `apt-get install shellcheck`)
- `npm install` (installs Prettier for JS formatting)

### Setup for Development

1) Ensure prerequisites are installed (Node LTS, git). If you use `nvm`, run `nvm use` in the agent-layer repo root (or in `.agent-layer/` when installed in a consumer repo).

2) Run the dev bootstrap (installs dev deps, enables hooks). In the agent-layer repo you must provide a parent root:
   ```bash
   ./dev/bootstrap.sh --temp-parent-root
   ```
   Use an explicit test repo instead of a temp parent root:
   ```bash
   ./dev/bootstrap.sh --parent-root /path/to/test-repo
   ```
   The test repo must contain `.agent-layer` (dir or symlink) that resolves to this repo.
   Use `./dev/bootstrap.sh --yes` for non-interactive runs. In a consumer repo, pass `--parent-root "$PWD"` when running `./.agent-layer/dev/bootstrap.sh` (use `--temp-parent-root` for an isolated temp repo).

3) Before committing:
   ```bash
   ./tests/run.sh --temp-parent-root
   ```

4) Autoformat (shell + JS) when needed:
   ```bash
   ./dev/format.sh
   ```

### Using agent-layer in this Repo (Self-Hosted Parent Root)

If you want to run `./al ...` directly in this repo and have configs/docs generated here, you can make the repo act as its own parent root. This is a dev-only setup.

1) Create the local `.agent-layer` symlink and `./al` launcher:
   ```bash
   ln -sfn . .agent-layer
   ln -sfn .agent-layer/agent-layer ./al
   ```

2) Run setup with an explicit parent root (or set `PARENT_ROOT` in `.agent-layer/.env` and omit the flag):
   ```bash
   ./al --setup --parent-root "$PWD"
   ```

3) Seed docs into the repo root (optional but recommended for memory files):
   ```bash
   mkdir -p docs
   cp config/templates/docs/ISSUES.md docs/ISSUES.md
   cp config/templates/docs/FEATURES.md docs/FEATURES.md
   cp config/templates/docs/ROADMAP.md docs/ROADMAP.md
   cp config/templates/docs/DECISIONS.md docs/DECISIONS.md
   cp config/templates/docs/COMMANDS.md docs/COMMANDS.md
   ```

Note: Ensure this repo's `.gitignore` includes the managed agent-layer block so the symlink and generated outputs stay ignored.

### Running Tests

Run tests (includes sync check + formatting/lint):
- From the agent-layer repo: `./tests/run.sh --temp-parent-root` (uses system temp; falls back to `tmp/agent-layer-temp-parent-root`)
- From a consumer repo: `./.agent-layer/tests/run.sh`
- CI and git hooks use `./tests/run.sh --temp-parent-root` when testing the agent-layer repo.

Tests require MCP prompt server deps:
```bash
# From the agent-layer repo:
cd src/mcp/agent-layer-prompts && npm install

# From a consumer repo:
cd .agent-layer/src/mcp/agent-layer-prompts && npm install
```

If you want to pass your own parent root, it must contain a `.agent-layer` entry that resolves to this repo:
```bash
repo_root="$(pwd -P)"
parent_root="$(mktemp -d "${TMPDIR:-/tmp}/agent-layer-temp-parent-root.XXXXXX")"
ln -s "$repo_root" "$parent_root/.agent-layer"
./tests/run.sh --parent-root "$parent_root"
```

### Understanding the Architecture

**Important for contributors**: Read `ARCHITECTURE.md` for the layer model and boundary guide, plus the "Parent Root Resolution" section above for:
- Root resolution specification (parent root vs agent layer root)
- Path normalization rules (`pwd -P` / realpath on comparisons)
- Terminology (PARENT_ROOT, AGENT_LAYER_ROOT)

**Development workflow**:
- Use repo-root scripts when developing agent-layer: `./dev/bootstrap.sh --temp-parent-root` (use `--parent-root <path>` for a specific test repo), `./tests/run.sh --temp-parent-root`
- When working in a consumer repo, use the `.agent-layer/` equivalents
- In the agent-layer repo, `./dev/bootstrap.sh` requires explicit parent-root flags; `./al` can use `PARENT_ROOT` in `.agent-layer/.env` when no root flags are provided. `--temp-parent-root` writes outputs into a temporary parent root (prefix `agent-layer-temp-parent-root`)
- Use `./al --setup --parent-root <path>` if you want to keep generated files in a specific repo
- Docs templates under `docs/` are created by `agent-layer-install.sh` in consumer repos

---

## FAQ

### What's the difference between agent-layer and manually configuring each client?

Agent-layer provides:
- **Single source of truth**: Edit one set of instructions, use across all your projects
- **Version control**: Your agent-layer fork is version controlled, not committed per-project
- **Consistency across projects**: Same instructions and workflows everywhere you work
- **Drift detection**: Agent-layer warns when client configs diverge from sources
- **Less boilerplate**: Write instructions once, not once per client or per project

### Can I use agent-layer with only one AI client?

Yes! Agent-layer works fine if you only use Gemini, or only use Claude, etc. You'll just ignore the configs for clients you don't use.

### Do I need to commit `.agent-layer/` to my repo?

**No.** By default, `.agent-layer/` is gitignored. This lets you:
- Use agent-layer individually without team buy-in
- Avoid nested git repos (`.agent-layer/` is its own repo)
- Work on multiple projects with the same agent config

**For individual use**: Install agent-layer in each project. It's gitignored, so your team won't see it.

**For team use**: Fork agent-layer, customize it for your team, and have team members install your fork. This avoids committing config to every project repo and prevents fragmentation.

**What you DO commit**: Generated docs like `ISSUES.md`, `FEATURES.md`, `ROADMAP.md`, `DECISIONS.md` (if you want them tracked).

### How do I share agent-layer with my team?

**Fork agent-layer** and have your team install your fork instead of committing `.agent-layer/` to each project repo.

**Why fork instead of committing?**
- Avoids nested git repos (`.agent-layer/` is its own repo with version history)
- One team fork works across all projects (no per-project config drift)
- Team members can still use it individually before full team adoption
- Easier to maintain and upgrade

**How to set up a team fork**:
1. Fork `agent-layer` on GitHub
2. Customize instructions/workflows in your fork
3. Team members install your fork: `your-fork-url/releases/latest/download/agent-layer-install.sh`
4. Update team fork periodically, team members pull updates

This approach keeps agent-layer consistent across all team projects without committing config to each one.

### Can I customize instructions per client?

Yes, but it's not the default flow. Agent-layer generates the same instructions for all clients. If you need client-specific instructions, you can:
1. Add conditional logic in `src/sync/sync.mjs`
2. Manually edit generated files (but they'll be overwritten on next sync)

Most users find it simpler to keep instructions the same across clients.

### Can I customize instructions per project?

Agent-layer is designed for **minimal per-project customization**. The goal is to use the same instructions across all your projects to avoid fragmentation.

If you need project-specific instructions:
- **Option 1**: Add project-specific details to your project's `docs/` files (ROADMAP.md, DECISIONS.md, etc.) which agents can read
- **Option 2**: Create a project-specific fork of agent-layer (not recommended - leads to maintenance burden)

Most users find it better to keep agent-layer consistent and put project-specific context in project docs instead.

### What about .env and PARENT_ROOT? (For Contributors/Advanced Users)

**User-facing**: `.agent-layer/.env` is for API tokens (like `GITHUB_PERSONAL_ACCESS_TOKEN`, `CONTEXT7_API_KEY`) used by MCP servers. This is the typical use case.

**Advanced/contributor-facing**: Parent root can be set explicitly via flags, or via `PARENT_ROOT` in `.agent-layer/.env` when no root flags are provided. This is treated as explicit configuration, not a fallback.

**Key constraint**: Discovery (automatic parent root detection) only works when the agent layer directory is named `.agent-layer`. If you're in the `agent-layer` dev repo (not named `.agent-layer`), discovery is blocked and you must either:
1. Use `--parent-root <path>` flag
2. Use `--temp-parent-root` flag (creates temporary parent root for testing)
3. Set `PARENT_ROOT` in `.agent-layer/.env`

**Why this matters**: When you're developing agent-layer itself (in a repo named `agent-layer`, not `.agent-layer`), the system can't auto-discover the parent root. You must explicitly tell it where to generate configs.

**For regular users**: You don't need to worry about this. When you install agent-layer in your project, it's named `.agent-layer/` and discovery works automatically.

---

## License

MIT license. See `LICENSE.md`.

## Attribution

- Nicholas Conn, PhD -  Conn Castle Studios
