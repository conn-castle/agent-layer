<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/conn-castle/agent-layer-web/main/static/img/branding/logo-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/conn-castle/agent-layer-web/main/static/img/branding/logo.svg">
  <img src="https://raw.githubusercontent.com/conn-castle/agent-layer-web/main/static/img/branding/logo.svg" alt="Agent Layer logo" width="120">
</picture>

# Agent Layer

One repo-local source of truth for instructions, skills, MCP servers, and approvals across coding agents.

Agent Layer keeps AI-assisted development consistent across tools by generating each client’s required config from a single `.agent-layer/` folder. Install `al` once per machine, run `al init` per repo, then run `al <client>` (e.g., `al claude`) to sync and launch.

Key properties:
- Local-first, no telemetry
- Deterministic outputs from canonical inputs
- Explicit approvals and command allowlists
- Optional per-repo runtime isolation (Codex auth/sessions/logs; Claude settings and caches with auth pending upstream fix)

Comparison:

| Manual per-client setup | Agent Layer |
| --- | --- |
| duplicate instructions across multiple formats | one canonical source under `.agent-layer/` |
| inconsistent approvals and command policies | consistent approvals and allowlists |
| MCP servers added in one client and forgotten in another | generated MCP config for supported MCP clients |
| shared global state across repos | opt-in per-repo Codex auth/sessions/logs; opt-in Claude settings and caches isolation |
| no single place to review or audit changes | audit in version control |

If Agent Layer improves your workflow, please consider starring the repository. Stars help new users discover the project.

---

## Supported clients

MCP = Model Context Protocol (tool/data servers).

| Client | Instructions | Skills | MCP servers | Approved commands |
|---|---:|---:|---:|---:|
| Antigravity | ✅ | ✅ | ❌* | ✅** |
| Claude Code CLI | ✅ | ✅ | ✅ | ✅ |
| VS Code / Copilot Chat | ✅ | ✅ | ✅ | ✅ |
| Codex CLI | ✅ | ✅ | ✅ | ✅ |
| Copilot CLI | ✅ | ✅ | ✅ | ✅ |

Notes:
- Codex, Antigravity, VS Code/Copilot, and Copilot CLI consume shared skills from `.agents/skills/<name>/SKILL.md`.
- Claude Code consumes skills from `.claude/skills/<name>/SKILL.md`.
- Claude Code and Codex VS Code extension support is handled through `al vscode`.
- Auto-approval capabilities vary by client; `approvals.mode` is applied on a best-effort basis.
- *Antigravity MCP config is written to `.agy/antigravity-cli/mcp_config.json` using the supported `serverUrl` shape. `agy` v1.0.0 migrates that file to `<gemini_dir>/config/mcp_config.json`, but runtime MCP registration has not been observed yet. Run `al probe agy` to see the current capability matrix.
- **Antigravity approved commands are written to `.agy/antigravity-cli/settings.json` as a managed `permissions.allow` list. User passthrough in `agents.antigravity.agent_specific` can add other settings keys, including `permissions.deny`; runtime enforcement is reported by `al probe agy`.

---

## Install

Install once per machine; choose one:

### Homebrew (macOS/Linux)

```bash
brew install conn-castle/tap/agent-layer
```

### Script (macOS/Linux)

```bash
curl -fsSL https://github.com/conn-castle/agent-layer/releases/latest/download/al-install.sh | bash
```

Verify:

```bash
al --version
```

---

## Quick start

Initialize a repo (run from any subdirectory):

```bash
cd /path/to/repo
al init
```

Then run an agent:

```bash
al agy
```

Optional health check:

```bash
al doctor
```

Notes:
- `al init` prompts to run `al wizard` after creating the bare operational scaffold. Use `al init --no-wizard` to skip; non-interactive shells skip automatically.
- `al init` is intended to be run once per repo. If the repo is already initialized, use `al upgrade plan` and `al upgrade` to refresh template-managed files.
- By default `al init` first walks up for an ancestor `.agent-layer/`, then for an ancestor `.git`. To install a separate Agent Layer in a subfolder of an existing repo (for example a sub-project that needs its own `.agent-layer/`), pass `al init --here` to target the current directory.
- `al upgrade` is the recommended path. For CI-safe non-interactive apply, use `al upgrade --yes --apply-managed-updates`. Add `--apply-memory-updates` and/or `--apply-deletions` only when you explicitly want those categories. `--apply-deletions` never removes files under `.agent-layer/tmp/`; to clean up those ephemeral agent run artifacts, use `--apply-tmp-deletions` (destructive — requires explicit double confirmation unless combined with `--yes`).
- `al upgrade` automatically creates a managed-file snapshot and rolls changes back if an upgrade step fails. Snapshots are written under `.agent-layer/state/upgrade-snapshots/`.
- Agent Layer does not install clients. Install the target client CLI and ensure it is on your `PATH` (Antigravity `agy`, Claude Code CLI, Codex, Copilot CLI, VS Code, etc.).

---

## Documentation sets

Universal best practices:
- [Skill Design](https://agent-layer.dev/skill-design) (canonical source: `docs/SKILL-DESIGN.md`)
- [CLI Skill Design](https://agent-layer.dev/cli-skill-design) (canonical source: `docs/CLI-SKILL-DESIGN.md`)
- [Instruction Design](https://agent-layer.dev/instruction-design) (canonical source: `docs/INSTRUCTION-DESIGN.md`)

Agent Layer approach:
- [Skills approach](https://agent-layer.dev/docs/skills-approach) (canonical source: `site/docs/skills-approach.mdx`)

---

## MCP server requirements (external tools)

Some MCP servers require a specific runtime or launcher to be installed locally. Agent Layer does not install these dependencies; it only runs the command you configure.

Examples:
- **Node-based servers** often use `npx` in the `command` field (requires Node.js + npm).
- **Python/uv-based servers** often use `uvx` in the `command` field (requires `uv`/`uvx` on your `PATH`).

If a server fails to start with “No such file or directory,” verify the `command` exists and is on your `PATH`, or set `command` to the full path of the executable.

### Doctor MCP checks

`al doctor` connects to each enabled MCP server and lists tools. It waits up to **30 seconds per server** before warning about connectivity, and prints a short progress indicator while checks run.
When config validation fails due to unrecognized keys, `al doctor` reports the detected key paths, schema hints (allowed keys where applicable), and repair options (`al upgrade`, `al wizard`, or manual edits).
When agents are enabled, it verifies generated client configs (for example `.mcp.json`, `.agy/antigravity-cli/settings.json`) are in sync; run `al sync` if they are missing or stale.

### Doctor size summary

`al doctor` always prints a **context size summary** after the checks, even when every value is under its threshold and even with `noise_mode = "quiet"` or `al --quiet doctor` (it is informational, not a warning):

- **Instructions** — estimated tokens of the combined instruction payload, shown against `instruction_token_threshold`.
- **Skills** — estimated tokens for the skill catalog metadata (all skill names plus descriptions, loaded on every run), shown against a ~4,000-token budget.
- **MCP servers** — totals only: enabled servers, total tools, and total tool-schema tokens, each against its configured threshold.
- **Total** — the estimated sum of the always-loaded token costs (instructions + skill catalog + MCP tool schemas); any component that can't be measured is named in an `(excludes …)` note instead of being counted as zero.

A metric whose threshold is omitted shows `(no limit set)` rather than a default. If MCP servers can't be resolved the line reports the size as unavailable; if some servers are unreachable, a note states that the tool and schema totals exclude them.

---

## FAQ

### Why do MCP servers fail to start in VS Code on macOS?

If MCP servers that use `npx` are failing in VS Code, your GUI environment may not see a user-directory Node install. Install Node via Homebrew (`brew install node`) so VS Code can find `node` and `npx`, and avoid per-user installs that only exist in shell profiles.

### Why did some VS Code settings disappear after `al sync`?

Some VS Code extensions (for example Peacock) write settings through the VS Code configuration API in a way that can land inside the Agent Layer-managed block in `.vscode/settings.json`.

If that happens, Agent Layer will replace that managed block on the next `al sync`, and those extension-written settings will be removed.

Fix:
1. Manually edit `.vscode/settings.json` and move extension-owned settings outside the managed marker block (`// >>> agent-layer` to `// <<< agent-layer`).
2. If the managed block is currently the last block in the file, add a user-owned tail anchor key after it:

```jsonc
{
  // >>> agent-layer
  // ... Agent Layer managed settings ...
  // <<< agent-layer
  "__settingsTailAnchor": 0
}
```

This keeps a stable non-managed tail position for extension writes.

---

## Version pinning (per repo, required)

Version pinning keeps everyone on the same Agent Layer release and lets `al` download the right binary automatically.

Upgrade contract details (event model, compatibility guarantees, migration rules, OS/shell matrix) are maintained in one canonical location: the [upgrade contract](https://conn-castle.github.io/agent-layer-web/docs/upgrades) (source: `site/docs/upgrades.mdx`).

When a release version is available, `al init` writes `.agent-layer/al.version` (for example, `0.6.0`). You can also edit it manually, or set the initial pin with `al init --version X.Y.Z` (or `--version latest`).

When you run `al` inside a repo, it locates `.agent-layer/`, reads the pinned version when present, and dispatches to that version automatically. `al init` and `al upgrade` are exceptions: they run on the invoking CLI version so pin updates and upgrade planning are not blocked by an older repo pin.

By default, `al init` enables pinning in release builds by writing `.agent-layer/al.version`. Think of this file like a lockfile: it prevents the repo from silently changing behavior when you update your globally installed `al`.

Agent Layer treats `.agent-layer/al.version` as required for supported usage. Do not delete it. If it is missing or invalid, install a release build and run `al upgrade` to repair it.

Pin format:
- `0.6.0` or `v0.6.0` (both are accepted)

Pin parser behavior:
- blank lines and `#` comments are ignored
- exactly one non-comment version line is expected
- empty/invalid/multi-version pin files produce a warning and dispatch falls back to the current CLI version until repaired

Cache location (per user):
- default: user cache dir (for example `~/.cache/agent-layer/versions/<version>/<os>-<arch>/al-<os>-<arch>` on Linux)
- override: `AL_CACHE_DIR=/path/to/cache`

Overrides:
- `AL_VERSION=0.6.0` forces a version (overrides the repo pin)
- `AL_NO_NETWORK=1` disables downloads (fails if the pinned version is missing)

---

## Updating Agent Layer

Update the global CLI:
- Homebrew: `brew upgrade conn-castle/tap/agent-layer` (updates the installed formula)
- Script (macOS/Linux): re-run the install script from Install (downloads and replaces `al`)

Run `al upgrade plan` and then `al upgrade` inside the repo to apply template-managed updates. This updates `.agent-layer/al.version` to match the currently installed `al` binary and refreshes template-managed files.
For a concise runbook (interactive + CI), use the one-page [upgrade checklist](https://conn-castle.github.io/agent-layer-web/docs/upgrade-checklist).

Each `al upgrade` run writes an automatic snapshot of managed upgrade targets and auto-rolls back if an upgrade step fails. Snapshots are retained under `.agent-layer/state/upgrade-snapshots/` for rollback history.
To manually restore an applied snapshot, run `al upgrade rollback <snapshot-id>` (use the JSON filename stem from `.agent-layer/state/upgrade-snapshots/` as `<snapshot-id>`).

To pre-warm a release binary in cache (for offline/air-gapped runs), use `al upgrade prefetch --version X.Y.Z`.

`al upgrade` also executes embedded per-release migration manifests before template writes (for example file renames/deletes and config key transitions). If the prior source version cannot be resolved, source-agnostic migrations still run and source-gated migrations are skipped with explicit report output.

Upgrade previews now include line-level diffs in both `al upgrade plan` and interactive `al upgrade` prompts. By default, each file preview is truncated to 40 lines; raise this with `--diff-lines N` when needed. In interactive terminals, diff hunks are colorized for scanability; non-interactive and no-color runs stay plain text.


`al doctor` always checks for newer releases and warns if you're behind. `al init` also warns when your installed CLI is out of date, unless you set `--version`, `AL_VERSION`, or `AL_NO_NETWORK`.

Compatibility guarantee:
- Guaranteed upgrade path is sequential release lines only (`N-1` to `N`; example: `0.6.x` to `0.7.x`).
- Skipping release lines is best effort and may require additional manual migration.
- See the [upgrade contract](https://conn-castle.github.io/agent-layer-web/docs/upgrades) (source: `site/docs/upgrades.mdx`) for event categories and release-versioned migration rules.

---

## Interactive setup (optional, `al wizard`)

Run `al wizard` any time to interactively configure the most important settings:

- **Approvals Mode** (all, mcp, commands, none, yolo)
- **Agent Enablement** (Antigravity, Claude, Codex, VS Code, Copilot CLI)
- **Model Selection** (optional; leave blank to use client defaults. Antigravity model names include reasoning level in the `agy models` display string; Codex and Claude expose reasoning effort separately where supported)
- **Feature toggles** — folded into the model step as two per-agent multi-selects (one Claude, one Codex). Each feature is a checkbox where **checked = keep enabled** and unchecking disables it; checkboxes are pre-checked to match your current config, so re-running the wizard without changes makes no edits.
    - *Claude:* IDE open-file reading, auto-memory, claude.ai connectors, the AskUserQuestion tool, and the Claude status line.
    - *Codex:* built-in apps (GitHub, Gmail, etc.), browser/computer-use, and the Codex status line.
    - Unchecking writes the matching `agent_specific` disable key; re-checking removes it, keeping the client's native default — except Codex **apps**, which defaults unchecked and always writes an explicit `features.apps`.
    - The AskUserQuestion toggle instead writes a typed `agents.claude.disable_question_tool` flag, and `al sync` injects the `permissions.deny` entry plus a `PreToolUse` hook (merged with, never replacing, your own deny/hook entries).
    - Status line checkboxes write explicit `statusline = true` or `statusline = false`; enabling one creates the missing editable source file once and never overwrites an existing source.
- **Workflow bundle** (yes/no when the bundle is absent — creates missing bundled workflow skills, managed instruction files, memory docs/templates, and `04_conventions.md`; existing files are left unchanged. Use `al upgrade` when you want managed workflow updates.)
- **Git tracking** (choose whether `.agent-layer/` and `docs/agent-layer/` stay trackable or are ignored through the managed `.agent-layer/gitignore.block` source)
- **CLI skills** (opt-in catalog: `tavily-web`, `playwright-cli`, `find-docs`, `agent-dispatch`; some require their own CLI on PATH; `al doctor` reports missing binaries without blocking agent launch)
- **MCP Servers & Secrets** (toggle default servers; safely write secrets to `.agent-layer/.env`)
- **Warnings** (enable/disable warning checks; threshold values use template defaults)

Non-interactive profile mode is also available:

```bash
# Preview-only diff against current .agent-layer/config.toml
al wizard --profile /path/to/profile.toml

# Apply profile + run sync
al wizard --profile /path/to/profile.toml --yes

# Remove wizard backups when no longer needed
al wizard --cleanup-backups
```

**Controls:**
- **Arrow keys**: Navigate
- **Space**: Toggle selection (multi-select)
- **Enter**: Confirm/Continue
- **Esc**: Go back to the previous wizard step (first step asks for exit confirmation)
- **Ctrl+C**: Cancel immediately

The wizard rewrites `config.toml` in a deterministic preferred section order and creates backups (`.bak`) before modifying `.agent-layer/config.toml` or `.agent-layer/.env`. Inline comments on modified lines may be moved to leading comments or removed; the original formatting is preserved in the backup files.
When prompted for required MCP secrets, type `skip` to disable that server for this run or `cancel` to exit without applying changes.

---

## What gets created in your repo

Bare `al init` creates the operational scaffold. The optional workflow-bundle wizard step creates missing project memory and bundled workflow files, and `al sync` creates generated client files.

### User configuration (gitignored by default, but can be committed)
  - `.agent-layer/`
  - `config.toml` (main configuration; human-editable)
  - `al.version` (repo pin; required)
  - `instructions/` (created empty by bare init; workflow bundle can add numbered `*.md` fragments)
  - `skills/` (created empty by bare init; workflow bundle and CLI catalog can add `<name>/SKILL.md` directories)
  - `tmp/runs/` (runtime scratch directory)
  - `commands.allow` (approved shell commands; line-based)
  - `gitignore.block` (managed `.gitignore` block template; customize here)
  - `.gitignore` (ignores repo-local launchers, template copies, and backups inside `.agent-layer/`)
  - `.env` (tokens/secrets; gitignored)

Repo-local launchers and template copies live under `.agent-layer/` and are ignored by `.agent-layer/.gitignore`.

### Project memory (optional; teams can commit or ignore)
The workflow bundle creates missing memory docs and templates. Bare init does not create `docs/agent-layer/`.

Common memory files include:
- `docs/agent-layer/ISSUES.md`
- `docs/agent-layer/BACKLOG.md`
- `docs/agent-layer/ROADMAP.md`
- `docs/agent-layer/DECISIONS.md`
- `docs/agent-layer/COMMANDS.md`
- `docs/agent-layer/CONTEXT.md`

### Generated client files (gitignored by default)
Generated outputs are written into the repo in client-specific formats (examples):

- Instruction shims: `AGENTS.md`, `CLAUDE.md`, `.github/copilot-instructions.md`
- MCP + client configs: `.mcp.json`, `.agy/antigravity-cli/settings.json`, `.agy/antigravity-cli/mcp_config.json`, `.claude/settings.json`, `.codex/`, `.copilot/mcp-config.json`
- Shared skills: `.agents/skills/`
- Antigravity notification plugin: `.agents/plugins/agent-layer-chime/`
- Claude skills: `.claude/skills/`
- VS Code integration: `.vscode/mcp.json` and an Agent Layer-managed block in `.vscode/settings.json`

---

## Configuration (human-editable)

You can edit all configuration files by hand. `al wizard` updates `config.toml` (approvals, agents/models, MCP servers, warnings), `.agent-layer/.env` (secrets), and `.agent-layer/gitignore.block` (Agent Layer folder tracking). It can also install missing workflow-bundle files and seed missing statusline source files; it does not refresh existing workflow-bundle files, overwrite existing statusline sources, or touch `commands.allow`.

### `.agent-layer/config.toml`

Edit this file directly or use `al wizard` to update it. This is the **only** structured config file.

Example:

```toml
[approvals]
# one of: "all", "mcp", "commands", "none", "yolo"
mode = "all"

[notifications]
# Optional local chime when supported providers fire their Stop lifecycle hook.
# Absent or false disables it. The default command uses macOS afplay.
# This is a lifecycle signal, not proof that the turn was correct.
chime = false

[agents.antigravity]
enabled = true
# Antigravity model/reasoning selection is a single agy model display string.
# al wizard writes this key and fresh wizard setups default it to high reasoning.
model = "Gemini 3.1 Pro (High)"

[agents.claude]
enabled = true
# model is optional; when omitted, Agent Layer does not pass a model flag and the client uses its default.
# model = "..."
# reasoning_effort is optional; Claude Code applies it where the active model supports it.
# reasoning_effort = "medium" # low | medium | high | xhigh | max (custom values pass through with a warning)
# Note: "max" is session-only (passed via --effort CLI flag) and is not written to .claude/settings.json.
# disable_question_tool blocks Claude Code's AskUserQuestion tool. When true, al sync injects
# permissions.deny + a PreToolUse hook into .claude/settings.json (merged with any agent_specific
# entries; the hook also enforces the block under YOLO). Run `al wizard` to set it.
# disable_question_tool = true
# statusline writes a Claude Code status line. Run `al wizard` or interactive
# `al upgrade` to enable it and seed .agent-layer/claude-statusline.sh once; on
# sync that source is copied to .claude/claude-statusline.sh and statusLine is
# wired into .claude/settings.json. Absent means disabled. Requires jq on PATH.
# statusline = true
# Optional `agent_specific` passthrough for Claude (arbitrary JSON keys).
# Object values are deep-merged into .claude/settings.json; arrays and scalar values are replaced at their key.
# Overlapping managed keys, such as permissions.allow, override Agent Layer-managed
# values and trigger a sync warning. permissions.deny is additive and does not warn.
# Optional "disable" toggles (run `al wizard` to set these). Each keeps Claude
# Code's native default until enabled; uncomment to opt in.
# agent_specific.env.CLAUDE_CODE_AUTO_CONNECT_IDE = "false" # stop reading files open in the IDE
# agent_specific.env.ENABLE_CLAUDEAI_MCP_SERVERS = "false"  # disable claude.ai app connectors
# agent_specific.autoMemoryEnabled = false                  # disable auto-memory (does not affect CLAUDE.md)
# [agents.claude.agent_specific]

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true
# model is optional; when omitted, Agent Layer does not pass a model setting and the client uses its default.
# model = "gpt-5.3-codex"
# reasoning_effort is optional; when omitted, the client uses its default.
# reasoning_effort = "xhigh" # codex only
# local_config_dir sets CODEX_HOME=<repo>/.codex for per-repo auth, sessions,
# logs, and other Codex runtime state. When absent or false, Agent Layer does
# not set CODEX_HOME, so Codex uses its normal global/project config layering.
# local_config_dir = false
# statusline writes Codex's native status line from the editable
# .agent-layer/codex-statusline.toml fragment. Run `al wizard` or interactive
# `al upgrade` to enable it and seed the source once. Absent means disabled.
# statusline = true
# Optional `agent_specific` passthrough for Codex (arbitrary TOML tables/keys).
# These are patched into .codex/config.toml and can override top-level managed keys.
# Agent Layer seeds [projects."<repo root>"] trust_level = "trusted" only when
# that exact project entry is absent.
# [agents.codex.agent_specific]
# [agents.codex.agent_specific.features]
# apps = false              # disable built-in Codex apps (GitHub, Gmail, etc.) to reduce tool surface
# browser_use = false       # disable Codex browser/computer-use tools
# in_app_browser = false    # disable the in-app browser
# computer_use = false      # disable screen/computer control
# multi_agent = true
# prevent_idle_sleep = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = true
# model is optional; when omitted, Agent Layer does not pass a model flag and the client uses its default.
# model = "..."
# reasoning_effort is not currently supported for Copilot CLI in Agent Layer.

[mcp]
# Secrets belong in .agent-layer/.env (never in config.toml).
# MCP servers here are the *external tool servers* that get projected into client configs.
# A fresh `al init` ships no servers; run `al wizard` to pick from a curated catalog (context7, tavily, fetch, playwright) or hand-author your own block as shown below.

[[mcp.servers]]
id = "example-api"
enabled = true
transport = "http"
# http_transport = "sse" # optional: "sse" (default) or "streamable"
url = "https://example.com/mcp"
headers = { Authorization = "Bearer ${AL_EXAMPLE_TOKEN}" }

[[mcp.servers]]
id = "local-mcp"
enabled = false
transport = "stdio"
command = "my-mcp-server"
args = ["--flag", "value"]
env = { MY_TOKEN = "${AL_MY_TOKEN}" }

[warnings]
# Optional thresholds for warning checks. Omit or comment out to disable.
# Warn when a newer Agent Layer version is available during sync runs.
version_update_on_sync = true
# Warning output noise control: "default" keeps all warnings, "reduce" hides suppressible non-critical warnings,
# "quiet" suppresses agent-layer informational output (warnings, update checks, dispatch banners). Configured
# quiet does not hide `al doctor` output; use `al --quiet doctor` to suppress warning-only doctor output.
noise_mode = "default"
instruction_token_threshold = 10000
mcp_server_threshold = 15
mcp_tools_total_threshold = 60
mcp_server_tools_threshold = 25
mcp_schema_tokens_total_threshold = 30000
mcp_schema_tokens_server_threshold = 20000
```

`agent_specific` passthrough keys are copied into provider-native settings. Codex and Claude warn when supported passthrough keys collide with Agent Layer-managed keys; Antigravity rejects `agents.antigravity.agent_specific.model` because `agents.antigravity.model` is the only model source. `.codex/config.toml` is shared Codex state: `al sync` refreshes known Agent Layer-managed entries, preserves unrelated Codex/user runtime entries, and seeds current repo trust only when that exact project entry is absent.

#### Notifications (`[notifications]`)

Set `chime = true` to project a local turn-stop chime into enabled Claude, Codex, and Antigravity clients. The generated hooks use each provider's native `Stop` lifecycle event. `Stop` means the provider says the assistant turn stopped; it is not a correctness or task-completion signal.

The managed command uses macOS `/usr/bin/afplay` with the system Blow sound. On non-macOS systems, leave the setting disabled until platform-specific sound support is added. Codex project hooks also require trusted project config and enabled hooks. Claude hooks can be disabled by `--bare`, `--safe-mode`, or policy settings. Codex and Antigravity hook commands must print valid JSON, which Agent Layer's managed commands do. Gemini CLI is not projected because Antigravity is Agent Layer's supported Google client.

#### Built-in placeholders

Agent Layer provides a built-in `${AL_REPO_ROOT}` placeholder for file paths in MCP server configs.
It expands to the absolute repo root during `al sync` and `al doctor`, and it does **not** need to be in `.env`.
Paths that start with `${AL_REPO_ROOT}` or `~` are expanded and normalized; other relative paths are passed through as-is.

Example:

```toml
[[mcp.servers]]
id = "filesystem"
enabled = false
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "${AL_REPO_ROOT}/."]
```

#### Restricting a server to specific clients

Use the optional `clients` field on an `[[mcp.servers]]` entry to control which clients receive a server. If you omit `clients`, the server is projected to all supported clients.

```toml
clients = ["antigravity", "claude", "codex", "copilot"]  # omit "vscode" to skip VS Code
```

This is useful when a client already covers the capability natively — for example, excluding VS Code/Copilot Chat for a file-search or filesystem server, where an MCP server would only duplicate built-in functionality and increase context window usage.

#### HTTP transport (`http_transport`)

For HTTP MCP servers, `http_transport` controls how `al doctor` connects:

- `sse` (default)
- `streamable`

Omit `http_transport` to default to `sse`.

#### Warning thresholds (`[warnings]`)

Warning thresholds are optional. When a threshold is omitted, its warning is disabled. Values must be positive integers (zero/negative are rejected by config validation). `al sync` uses `instruction_token_threshold` and, when `version_update_on_sync = true`, warns if a newer Agent Layer release is available. `al doctor` evaluates all configured MCP warning thresholds.

Set `version_update_on_sync = true` to opt in to update warnings during `al sync` and `al <client>`; omit it or set it to `false` to keep update warnings limited to `al init`, `al doctor`, and `al wizard`.
Set `noise_mode = "default"` to keep all warnings (recommended), `noise_mode = "reduce"` to hide only suppressible non-critical warnings, or `noise_mode = "quiet"` to suppress agent-layer informational output. Errors still print, client output is unaffected, and `al doctor` prints warnings by default regardless of noise mode. For one-off doctor runs, `al --quiet doctor` suppresses warning-only doctor output while still showing failures. The `al doctor` size summary is informational and always prints, regardless of noise mode or `--quiet`.

Use `al <client> --quiet` (or `-q`) for one-off quiet runs; the flag always wins over config.

#### Approvals modes (`approvals.mode`)

These modes control whether the agent is allowed to run shell commands and/or MCP tools without prompting. Edit them to match your team's preferences; `al wizard` can update `approvals.mode`.

- `all`: auto-approve **both** shell commands and MCP tool calls (where supported)
- `mcp`: auto-approve **only** MCP tool calls; shell commands still require approval (or are restricted)
- `commands`: auto-approve **only** shell commands; MCP tool calls still require approval
- `none`: approve **nothing** automatically
- `yolo`: skip **all** permission prompts where the client supports it (sends `--dangerously-skip-permissions` to Claude and Antigravity, `approval_policy=never` + `sandbox_mode=danger-full-access` + `web_search=live` to Codex, `--yolo` to Copilot CLI); intended for sandboxed/ephemeral environments

Client notes:
- Some clients do not support all approval types; Agent Layer generates the closest supported behavior per client.

Codex may still deny or override these settings if its `requirements.toml` disallows them.

### Secrets: `.agent-layer/.env`

API tokens and other secrets live in `.agent-layer/.env` (always gitignored).

**Important:** Only environment variables that start with the `AL_` prefix are sourced from `.env` (others are ignored). This convention avoids conflicts with your shell environment and ensures Agent Layer's variables don't override existing environment variables when VS Code terminals inherit the process environment.

Example keys:
- `AL_CONTEXT7_API_KEY`
- `AL_TAVILY_API_KEY`
- `AL_GITHUB_PERSONAL_ACCESS_TOKEN` (only when using the optional GitHub MCP server)

Your existing process environment takes precedence. `.agent-layer/.env` fills missing keys only, and empty values in `.agent-layer/.env` are ignored (so template entries cannot override real tokens). This behavior is consistent whether launching via `al` commands or repo-local launchers like `open-vscode.app`, `open-vscode.sh`, or `open-vscode.command`.

### Instructions: `.agent-layer/instructions/`

These files are user-editable; customize them for your team's preferences.

- Files are concatenated in **lexicographic order**
- Use numeric prefixes for stable priority (e.g., `00_core.md`, `10_style.md`, `20_repo.md`)

### Skills: `.agent-layer/skills/`

These files are user-editable; define the workflows you want your agents to run.

Agent Layer aligns with the [Agent Skills specification](https://agentskills.io/specification), and `al doctor` validates configured skills against agentskills-aligned conventions.

- Source format:
  - Directory: `.agent-layer/skills/<name>/SKILL.md` (canonical; `skill.md` is accepted as a compatibility fallback)
- Frontmatter fields:
  - Required: `name`, `description`
  - Optional: `license`, `compatibility`, `metadata`, `allowed-tools`
- `name` validation (`al doctor`):
  - NFKC-normalized and compared against the canonical source name (filename stem or directory name) using normalization-aware matching
  - Maximum 64 Unicode characters, lowercase letters/digits/hyphens only, and no leading/trailing/consecutive hyphens
- Length warnings (`al doctor`): `description` max 1024 Unicode characters per skill; `compatibility` max 500 Unicode characters.
- Catalog warning (`al doctor`): all skill names plus descriptions (the always-loaded catalog metadata) should stay at or below an estimated ~4,000 tokens.
- Size recommendation (`al doctor`): warns when a skill source exceeds 500 lines.
- Backward compatibility: skills with missing `name` still load (name derived from path), but `al doctor` warns.
- Missing or empty `description` is a load/sync error (fail-loud); it is not warning-only.
- Directory-format skills should use `SKILL.md`; lowercase `skill.md` loads but triggers an `al doctor` warning.
- Shared-skill clients consume these from `.agents/skills/<name>/SKILL.md`; Claude consumes them from `.claude/skills/<name>/SKILL.md`.
- Workflow guidance is provided by individual skill sources under `.agent-layer/skills/`.

### Approved commands: `.agent-layer/commands.allow`

- One command prefix per line.
- Used to generate each client’s “allowed commands” configuration where supported.

---

## Skill sync

Skills are synced natively to Agent Skills directories with full subdirectory support (`scripts/`, `references/`, `assets/`). Shared-skill clients use `.agents/skills/`; Claude uses `.claude/skills/`. No MCP server is involved; `al sync` copies skill sources directly into enabled client skill folders. See `docs/SKILL-CLIENT-SPEC.md` for the source-cited client support matrix.

---

## Agent Dispatch

`al dispatch` lets an Agent Layer-launched agent run another supported headless target (`codex`, `claude`, or `antigravity`) for a focused text task.

Examples:

```bash
al dispatch --agent codex "Review this plan."
al dispatch --agent random --skill review-plan "Review this plan."
al dispatch --agent antigravity --model "Gemini 3.1 Pro (High)" "Review this plan."
cat prompt.md | al dispatch --agent claude
al dispatch options --json
```

Dispatch writes target answer text to stdout and wrapper status/errors to stderr. It defaults to depth 2, so a dispatched child can make one nested `al dispatch` call before the depth guard blocks further nesting. Codex, Claude, and Antigravity support per-run `--model` dispatch overrides; Antigravity does not support separate `--reasoning-effort` because agy encodes effort in model display strings. For the full contract, including exit codes, random selection, config defaults, and skill prefixing, see `docs/AGENT-DISPATCH.md`.

---

## VS Code (Codex + Claude extensions)

`al vscode` is the single command for launching VS Code with both Codex and Claude extension support. It is enabled when either `[agents.vscode]` or `[agents.claude_vscode]` is set to `enabled = true` in `config.toml`.

- When `[agents.vscode]` is enabled and `[agents.codex] local_config_dir = true` is set, `CODEX_HOME=<repo>/.codex` is set for the Codex extension. Otherwise Agent Layer preserves any inherited `CODEX_HOME`.
- When `[agents.claude_vscode]` is enabled, Claude files (`.mcp.json`, `.claude/settings.json`) are generated. YOLO mode sets `claudeCode.allowDangerouslySkipPermissions` in `.vscode/settings.json`.
- When `[agents.claude] local_config_dir = true` is set, `al claude` sets `CLAUDE_CONFIG_DIR` for per-repo settings and caches isolation. For `al vscode`, `CLAUDE_CONFIG_DIR` is set only when **both** `local_config_dir = true` and `[agents.claude_vscode]` is enabled; otherwise `al vscode` clears only stale repo-local values and preserves user-defined non-repo values. This is opt-in; when disabled (the default), Claude uses your global `~/.claude/` configuration. For `al claude` only, a user-set `CLAUDE_CONFIG_DIR` pointing outside the repo is preserved even when `local_config_dir` is disabled. Note: auth credentials are stored globally in Claude Code's OS credential store (macOS Keychain service `"Claude Code-credentials"`; Linux libsecret/gnome-keyring) regardless of this setting (upstream limitation).
- VS Code settings are generated when either agent is enabled.
- Supports `--no-sync` to skip sync before opening VS Code.

The Codex VS Code extension reads `CODEX_HOME` and the Claude extension reads `CLAUDE_CONFIG_DIR` from the VS Code process environment at startup.

Agent Layer provides repo-specific launchers in `.agent-layer/` that invoke `al vscode`; they set `CODEX_HOME` only when `[agents.codex] local_config_dir = true` is enabled, and set `CLAUDE_CONFIG_DIR` when both `local_config_dir` and `agents.claude_vscode` are enabled:

Launchers:
- macOS: `open-vscode.app` (recommended; VS Code in `/Applications` or `~/Applications`) or `open-vscode.command` (uses `code` CLI)
- Linux: `open-vscode.desktop` or `open-vscode.sh` (uses `code` CLI; shows a dialog if missing)

These launchers invoke `al vscode`, so the `al` CLI must be available on your PATH.
`al vscode` also runs launch preflight checks and fails fast with guidance when `code` is missing on `PATH` or `.vscode/settings.json` has a managed-block marker conflict.

If you use the CLI-based launchers, install the `code` command from inside VS Code:
- macOS: Cmd+Shift+P -> "Shell Command: Install 'code' command in PATH"
- Linux: Ctrl+Shift+P -> "Shell Command: Install 'code' command in PATH"

**Note:** Codex authentication is per repo only when `local_config_dir = true` is enabled under `[agents.codex]`; in that mode each repo uses its own `CODEX_HOME` and may require reauthentication. If `local_config_dir = true` is enabled under `[agents.claude]`, Claude settings and caches are isolated per repo (via `CLAUDE_CONFIG_DIR`). **Known upstream limitation:** Claude Code currently stores auth credentials in the OS credential store (macOS Keychain service `"Claude Code-credentials"`; Linux libsecret/gnome-keyring) regardless of `CLAUDE_CONFIG_DIR`, so authentication is always shared globally until this is fixed upstream.

For contributor-level implementation details, see `docs/architecture/vscode-launch.md`.

---

## Temporary artifacts (agent-only)

Some workflows write **agent-only** artifacts (plans, task lists, reports). These are not meant for humans to open.

Artifacts always live under `.agent-layer/tmp/` and use a unique, concurrency-safe name:

- `.agent-layer/tmp/<workflow>.<run-id>.<type>.md`
- `run-id = YYYYMMDD-HHMMSS-<short-rand>` (uses bash `$RANDOM`; bash is required)
- Multi-file workflows reuse the same `run-id` for all files.
- Common `type` values: `report`, `plan`, `task`, `context`.

Workflows echo the artifact path in the chat output. There are no path overrides or environment variables for this. Artifacts are agent-only and can be ignored; agents may clean up their own plan/task/context files when a workflow completes. If a run is interrupted, leftover files are harmless and optional to delete.

---

## CLI overview

Common usage:

```bash
al agy
al claude
al codex
al copilot
al vscode
```

### Passing flags to clients

`al <client>` forwards any extra arguments to the underlying client. If you need to use Agent Layer flags as well, place `--` before the client arguments.

Examples:

```bash
al claude -- --help
al vscode --no-sync -- --reuse-window
```

`--no-sync` is an Agent Layer flag and must appear before `--`; anything after `--` is passed directly to the client. For an explicit false value, use `--no-sync=false` (space-separated values like `--no-sync false` are not supported and will be passed through).

Other commands:

- `al init` — initialize the bare `.agent-layer/` operational scaffold and `.gitignore`
- `al upgrade` — apply template-managed updates and update the repo pin (interactive by default; non-interactive requires `--yes` plus one or more apply flags; line-level diff previews shown by default, `--diff-lines N` to raise per-file preview size; automatic snapshot + rollback on failure)
- `al upgrade plan` — preview plain-language categorized template/pin changes and readiness actions with line-level diff previews (`--diff-lines N` to raise per-file preview size)
- `al upgrade prefetch` — download and cache a release binary (use `--version X.Y.Z` on dev builds; useful for offline/CI cache warm-up)
- `al upgrade rollback <snapshot-id>` — restore an applied upgrade snapshot (snapshot IDs are JSON filenames in `.agent-layer/state/upgrade-snapshots/`; use `al upgrade rollback --list` to discover available IDs)
- `al upgrade repair-gitignore-block` — restore `.agent-layer/gitignore.block` from templates and reapply the root `.gitignore` managed block
- `al sync` — regenerate configs without launching a client
- `al probe agy` — run the Antigravity capability probe and print JSON
- `al dispatch` — run a supported headless target through Agent Dispatch (see the [Agent Dispatch](#agent-dispatch) section)
- `al doctor` — check common setup issues and warn about available updates
- `al wizard` — interactive setup wizard plus profile mode (`--profile`) and backup cleanup (`--cleanup-backups`)
- `al completion` — generate shell completion scripts (bash/zsh/fish, macOS/Linux only)

---

## Shell completion (macOS/Linux)

*The completion command is available on macOS and Linux only.*

“Shell completion output” is a snippet of shell script that enables tab-completion for `al` in your shell.

Typical behavior:
- `al completion bash` prints a Bash completion script to stdout
- `al completion zsh` prints a Zsh completion script to stdout
- `al completion fish` prints a Fish completion script to stdout
- `al completion <shell> --install` writes the completion file to the standard user location

This enables:
- `al <TAB>` to complete supported subcommands (agy/claude/codex/copilot/vscode/sync/…)

Notes:
- Zsh may require adding the install directory to `$fpath` before `compinit` (the command prints a snippet when needed).
- Bash completion requires bash-completion to be enabled in your shell.

---

## Git ignore defaults

Installer adds a managed `.gitignore` block that typically ignores:
- `.agent-layer/` (except if teams choose to commit it)
- generated client config files/directories (for example `.agents/`, `.agy/`, `.antigravitycli/`, `.claude/`, `.mcp.json`, `.codex/`, `.copilot/`, `.vscode/mcp.json`, `.vscode/settings.json`, and `.github/copilot-instructions.md`)

If you choose to commit `.agent-layer/`, keep `.agent-layer/.gitignore` so repo-local launchers, template copies, and backups stay untracked.

To commit `.agent-layer/`, remove the `/.agent-layer/` line in `.agent-layer/gitignore.block` and re-run `al sync`.

To customize the managed block, edit `.agent-layer/gitignore.block` and re-run `al sync`.

`.agent-layer/.env` is ignored by `.agent-layer/.gitignore`, not the parent repo `.gitignore`.

`docs/agent-layer/` is created by the optional workflow bundle; teams may choose to commit it or ignore it.

---

## Design goals

- Make installation and day-to-day usage trivial
- Provide consistent core features across clients (instructions, skills, config generation, MCP servers, sync-on-run)
- Reduce moving parts by shipping a single global CLI and keeping `.agent-layer/` config-first with minimal repo-local launchers

## Changelog

See `CHANGELOG.md` for release history.

## Contributing

Contributions are welcome. Please use the project's issue tracker and pull requests.

Contributor workflows live in `docs/DEVELOPMENT.md`.

## License

See `LICENSE.md`.

## Attributions

- Nicholas Conn, PhD - Conn Castle Studios
