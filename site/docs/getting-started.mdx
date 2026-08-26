---
title: Getting started
description: Install Agent Layer and run your first agent.
sidebar_position: 1
---

Agent Layer projects instructions, approvals, and MCP servers from one `.agent-layer/` configuration into each client's native format, using the closest behavior that client supports. Approval enforcement is client-dependent and best effort. Install `al`, initialize the repo, then run `al <client>` to sync and launch.

Without that, a prompt fix in one client is easy to miss in another, and an MCP server can be enabled in one place and missing everywhere else. This page gets a consistent setup running first. You can refine it afterward.

After you complete this page, you will have:

- A `.agent-layer/` folder you edit.
- A small set of generated client outputs you do not edit.
- A simple habit: run `al <client>` to sync and launch with the latest generated configuration.

Agent Layer does not replace your client, and it does not run as a hosted service. It runs locally and writes files into your repo.

Custom skills, additional MCP servers, and version pinning can wait until the basics are working.

## In this page

- [Quick start](#quick-start)
- [Repo layout](#repo-layout)
- [CLI command map](#cli-command-map)
- [Next steps](#next-steps)

Quick start is the shortest path to a working agent. You can always refine configuration once the basics are running, and each step builds on the last so you do not have to backtrack.

## Quick start

### Prerequisites

- Install the Agent Layer CLI (`al`)
- Install the target client (Antigravity `agy`, Claude, Codex, Copilot CLI, Grok Build CLI `grok`, VS Code)

Agent Layer does not install client CLIs for you. If you already have `al` installed, skip to step 2.

Install `al` once per machine, then run `al init` once per repo.

### 1) Install `al`

Pick one install method from [/install](/install), then verify:

```bash
al --version
```

### 2) Initialize a repo

Run `al init` from inside the repo:

```bash
cd /path/to/repo
al init
```

This seeds:

- `.agent-layer/` (your configuration)
- a managed `.gitignore` block

By default, `al init` prompts to run the setup wizard. The wizard can seed instruction files and `docs/agent-layer/` project memory, and can install catalog skills. Skip it with `--no-wizard` if you prefer to configure manually.

`al init` is intended to be run once per repo. If `.agent-layer/` already exists, `al init` errors; use `al upgrade plan` and `al upgrade` to refresh template-managed files.

For detailed behavior, see [Init](./reference#init), [Upgrade](./reference#upgrade), and [Wizard](./reference#wizard).

### 3) Launch an agent

```bash
al agy
```

Any client command (agy/claude/codex/copilot/grok/vscode) will:

1. read `.agent-layer/`
2. sync client configs
3. launch the client

From here on, treat `al <client>` as your entry point. Syncing happens first so you never have to remember which config file goes with which tool.

### 4) Customize behavior

Edit these files to make the agent behave the way you want:

- `.agent-layer/instructions/` for global rules and preferences
- `.agent-layer/skills/` for reusable workflows (aligned with the [agentskills.io specification](https://agentskills.io/specification))
- `.agent-layer/commands.allow` to define approved shell command prefixes
- `.agent-layer/config.toml` for approvals, dispatch limits, agent enablement, models, MCP servers, and warnings

Everything is plain text and local to the repo, which makes it easy to review changes and share them with a team when you are ready.

### 5) Enable an MCP server (optional)

MCP servers add tools the model can call: current docs, web search, and other external systems.

`al init` creates an empty `[mcp]` section. Run `al wizard` to choose from the embedded MCP catalog, or add server blocks by hand. Two common first picks are:

- `context7` for up-to-date library docs and examples (requires an API key)
- `tavily` for web search/research and recency (requires an API key)

See [MCP servers](./concepts#mcp-servers) for how they fit in and [MCP servers (configuration)](./reference#mcp-servers) for the exact config fields.

To enable a server, set `enabled = true` and add any required secrets in `.agent-layer/.env`:

```toml
[[mcp.servers]]
id = "context7"
enabled = true
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@2.1.1"]
env = { CONTEXT7_API_KEY = "${AL_CONTEXT7_API_KEY}" }
```

```env
AL_CONTEXT7_API_KEY=your-key-here
```

If a server uses `npx` or `uvx`, make sure those runtimes are installed first. Then run `al sync` or launch a client again.

You do not need to enable MCP servers to get value from Agent Layer. Many teams start with instructions and approvals, then add tools once the basics feel stable.

### 6) Run a health check

Once you have a client working, run:

```bash
al doctor
```

This validates configuration, checks for missing secrets, and probes enabled MCP servers.
See [Doctor](./reference#doctor) for exactly what it checks.

## Repo layout

After `al init`, your repo gains a few new directories and generated files. Edit the files in `.agent-layer/`. Regenerate the client files.

The resulting layout always has configuration and generated client files. Project memory is an optional third bucket, created only when you choose Rules and memory in `al wizard`.

### Configuration (`.agent-layer/`)

`.agent-layer/` is the folder you edit. Change these files directly.

Typical layout:

```text
.agent-layer/
  config.toml          # approvals, dispatch limits, agents, models, MCP servers, warnings
  instructions/        # numbered instruction fragments
  skills/             # <name>/SKILL.md
  commands.allow       # approved shell command prefixes
  al.version           # repo pin (required)
  .env                 # secrets (always gitignored)
  .gitignore           # ignores repo-local launchers, backups, templates, and state
  gitignore.block      # managed .gitignore block template
  templates/docs/      # embedded memory template snapshots
  state/managed-baseline.json # managed ownership baseline state
  tmp/                 # agent-only artifacts
```

Skill sources follow the [agentskills.io specification](https://agentskills.io/specification). User-managed skills use `.agent-layer/skills/<name>/SKILL.md`; imported skills use `.agent-layer/skills-imported/<name>/SKILL.md`. Both require the uppercase `SKILL.md` filename; lowercase `skill.md` is rejected.

### Project memory (`docs/agent-layer/`)

These files are created when you choose Rules and memory in `al wizard`. The bundled memory instructions reference them as long-lived memory for the repo:

```text
docs/agent-layer/
  ISSUES.md
  BACKLOG.md
  DECISIONS.md
  COMMANDS.md
  CONTEXT.md
```

Teams can choose to commit these files or keep them local.

### Generated client outputs

Generated files live in the repo and are gitignored by default:

Different clients require different file formats and conventions. Agent Layer generates what each client expects so you do not have to maintain separate config by hand.

```text
# Instruction shims (generated from .agent-layer/instructions/*.md)
AGENTS.md
.claude/CLAUDE.md
.github/copilot-instructions.md

# Client configs and artifacts (generated based on enabled clients)
.agents/skills/
.agy/antigravity-cli/settings.json
.agy/antigravity-cli/mcp_config.json
.claude/settings.json
.claude/skills/
.mcp.json
.codex/
.copilot/mcp-config.json
.grok/config.toml
.grok/hooks/agent-layer-chime.json
.vscode/mcp.json
.vscode/settings.json

# Repo-local launchers (VS Code, when enabled)
.agent-layer/open-vscode.command
.agent-layer/open-vscode.desktop
.agent-layer/open-vscode.sh
.agent-layer/open-vscode.app/
```

These are derived outputs. Do not edit them directly.

:::caution
If you change Agent Layer-managed generated values, those edits will be overwritten the next time you run `al sync` or `al <client>`. Native values in `.agy/antigravity-cli/settings.json` are preserved.
:::

:::note
The Codex VS Code extension reads `CODEX_HOME` at startup. Repo-local launchers set a per-repo value only when `local_config_dir = true` is enabled under `[agents.codex]`; in that mode you may need to reauthenticate with Codex when opening a different repo. If you enable `local_config_dir = true` under `[agents.claude]` (and `[agents.claude_vscode]` is enabled), the Claude extension also receives a per-repo `CLAUDE_CONFIG_DIR`, isolating settings and caches per repository.

Claude Code stores `/login` credentials in the macOS Keychain on macOS and in `.credentials.json` under `CLAUDE_CONFIG_DIR` on Linux and Windows. Other authentication modes may use external credential sources, so isolation depends on your platform and authentication method. See [Claude Code authentication](https://code.claude.com/docs/en/authentication).

If you prefer to launch VS Code without the repo-local launcher, see the Codex and Claude fallback guidance in troubleshooting: [Global MCP server fallback for VS Code (Codex)](./troubleshooting#global-mcp-server-fallback-for-vs-code-codex) and [Global config fallback for VS Code (Claude)](./troubleshooting#global-config-fallback-for-vs-code-claude).
:::

### Safe to delete

You can delete generated outputs at any time. Running `al sync` or `al <client>` will recreate them from `.agent-layer/`. The exceptions are the shared-state files `al sync` patches in place — `.codex/config.toml` and `.agy/antigravity-cli/settings.json` — which can hold native values (for Antigravity, workspace approval or trust), so keep them gitignored but do not delete them as part of cleanup. For Antigravity, Agent Layer patches only its managed model, `permissions.allow`, and `agent_specific` paths; its Antigravity MCP output remains disposable.

If you keep your own files under `.vscode/`, delete only the Agent Layer-managed files (`.vscode/mcp.json` and the managed block in `.vscode/settings.json`). Agent Layer also projects VS Code/GitHub Copilot skills through `.agents/skills/`.

### Git ignore defaults

`al init` installs a managed `.gitignore` block that ignores `.agent-layer/` and generated client configs. If your team decides to commit `.agent-layer/`, edit `.agent-layer/gitignore.block` and re-run `al sync` (or `al <client>`).

`.agent-layer/gitignore.block` is the template. It should contain only ignore patterns and comments. Agent Layer copies it verbatim and injects the managed header and template hash only when updating the root `.gitignore`. If you see managed markers or a template hash inside `.agent-layer/gitignore.block`, run `al upgrade` to restore the template file.

When you are adopting Agent Layer as a team, this is the main choice to make: keep `.agent-layer/` local for experimentation, or commit it so every developer gets the same behavior by default. Secrets still live in `.agent-layer/.env`, which remains gitignored by `.agent-layer/.gitignore`.

### What to commit (recommended)

The default `.gitignore` block is conservative so you can try Agent Layer without changing what your repo tracks. Once you want shared behavior across a team, most repos choose to commit `.agent-layer/` and keep all generated outputs ignored.

Commit:

- `.agent-layer/` (except `.agent-layer/.env`)
- `docs/agent-layer/` (project memory), if you want shared context across the team

Do not commit:

- `.agent-layer/.env` (secrets)
- `.agent-layer/state/managed-baseline.json` (machine-managed baseline state)
- `.agent-layer/tmp/` (agent-only artifacts)
- generated client outputs like `.agents/skills/`, `.agy/`, `.antigravitycli/`, `.claude/settings.json`, `.claude/skills/`, `.claude/CLAUDE.md`, `.mcp.json`, `.codex/`, `.copilot/`, `.grok/`, `.grok-config/`, `.vscode/mcp.json`, `.vscode/settings.json`, `.github/copilot-instructions.md`, `AGENTS.md`

## CLI command map

Use this as a quick refresher when you are trying to remember the entry points or decide which command to run.

This is a complete map of the CLI entry points. For details, see the [Reference](./reference).

If you only remember one pattern: `al <client>` syncs first, then launches.

| Command | Purpose | Details |
| --- | --- | --- |
| `al init` | Seed `.agent-layer/` and project memory. | [Init](./reference#init) |
| `al upgrade` | Apply template-managed updates and update the repo pin with line-level diff previews (use `--diff-lines` to raise per-file preview size). | [Upgrade](./reference#upgrade) |
| `al upgrade plan` | Preview categorized upgrade changes and line-level diffs without writing files. | [Upgrade plan](./reference#upgrade-plan) |
| `al upgrade rollback --list` | List available upgrade snapshot IDs and statuses before rollback. | [Upgrade rollback](./reference#upgrade-rollback) |
| `al upgrade rollback <snapshot-id>` | Restore a previously applied managed-file snapshot. | [Upgrade rollback](./reference#upgrade-rollback) |
| `al wizard` | Interactive configuration. | [Wizard](./reference#wizard) |
| `al sync` | Regenerate configs without launching. | [Sync](./reference#sync) |
| `al doctor` | Validate config and probe MCP servers. | [Doctor](./reference#doctor) |
| `al probe agy` | Report observed Antigravity capabilities. | [CLI guide](./reference#cli-guide) |
| `al probe grok` | Report observed Grok capabilities. | [CLI guide](./reference#cli-guide) |
| `al completion` | Print or install shell completions. | [Completion](./reference#completion) |
| `al agy` | Sync and launch Antigravity. | [Launch a client](./reference#launch-a-client) |
| `al claude` | Sync and launch Claude Code. | [Launch a client](./reference#launch-a-client) |
| `al codex` | Sync and launch Codex. | [Launch a client](./reference#launch-a-client) |
| `al vscode` | Sync and launch VS Code. | [Launch a client](./reference#launch-a-client) |
| `al copilot` | Sync and launch Copilot CLI. | [Launch a client](./reference#launch-a-client) |
| `al grok` | Sync and launch Grok. | [Launch a client](./reference#launch-a-client) |
| `al dispatch` | Start, wait for, continue, or cancel a headless provider conversation. | [Agent Dispatch](./agent-dispatch) |
| `al --version` | Print the installed version. | [Help and version](./reference#help-and-version) |
| `al help` | Show help for a command. | [Help and version](./reference#help-and-version) |

## Next steps

- Read [Concepts](./concepts) for safety boundaries and how MCP servers fit into the system.
- Use [Agent Dispatch](./agent-dispatch) to delegate asynchronous, headless provider work through MCP tools or the CLI.
- Use [Reference](./reference) when you are ready to tune configuration, wire secrets, or understand exactly what a CLI command touches.
- Read [Upgrades](./upgrades) for compatibility guarantees and migration rules.
- Visit [Troubleshooting](./troubleshooting) when something does not work; start with `al doctor`.
