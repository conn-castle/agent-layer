# Context

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Persistent project-specific knowledge that does not belong in ISSUES, BACKLOG, ROADMAP, DECISIONS, or COMMANDS. Read this file before starting work on a task.

Record three categories of information here:
1. **Project context** — domain concepts, architectural invariants, naming conventions, external dependencies, environment setup notes, team norms, and any other stable facts an agent needs to work effectively in this repository.
2. **Project-specific nuances** — non-obvious behaviors, implicit conventions, or user-provided clarifications that an agent would not discover from reading the code alone. When a user corrects a misunderstanding or explains how something actually works in this project, record it here.
3. **Lessons learned** — repeated mistakes, surprising behaviors, non-obvious gotchas, and corrective patterns discovered during development. When an error recurs or a workaround is needed more than once, record it here so future agents avoid the same mistake.

Do not duplicate information that belongs in other memory files:
- Deferred bugs or tech debt → ISSUES.md
- Planned features → BACKLOG.md
- Workflow commands → COMMANDS.md
- Non-obvious decisions → DECISIONS.md
- Phased plans → ROADMAP.md

## Format
- Organize by topic using headings (`##`, `###`).
- Prefer concise bullet points. State facts directly; omit hedging language.
- Before adding an entry, search this file for existing coverage. Merge into or update an existing section instead of creating a near-duplicate.
- Remove or update entries when the underlying facts change.
- Insert all content below `<!-- ENTRIES START -->`.

<!-- ENTRIES START -->

## Skill sources

- Agent Layer projects two skill source tiers: user-managed `.agent-layer/skills/` and Git-imported `.agent-layer/imported-skills/`. Both project identically. The imported tier is fully managed: a directory without a `.agent-layer/skills.lock.json` entry, or a name owned by both tiers, is a loud error rather than a silent shadow.
- The project lock (`.agent-layer/sync.lock`, owned by `internal/projectlock`) serializes skill-source reads and mutations with projection. Production entry points load their source snapshot **inside** that lock; loading before acquiring it can observe a half-applied `al skills` mutation.
- Both source tiers use one strict `skilltree.Tree` policy: uppercase `SKILL.md` is required, unknown frontmatter fields remain opaque, and symlinks plus every other non-directory, non-regular node are rejected. The immutable loaded tree is reused for validation, hashing, import behavior, and every client projection.
- `.agents/skills/` and `.claude/skills/` are exclusively owned disposable projections. Sync builds each complete root at a fixed sibling staging path, replaces the live root wholesale, and discards interrupted staging/output state on retry; it never preserves direct edits, extra entries, or ownership markers in those roots.
- `.agent-layer/skills.lock.json` is a trust boundary, not just a cache: its values become filesystem paths inside destination checkouts and merge bases for Git operations, so `internal/skilllock` strictly validates every persisted invariant at parse *and* marshal time.
- Every imported skill's lock entry is its sole upstream merge base and convergence state. Import blocks are configuration shorthand only; partial success may leave their members at different commits, and new membership locks immediately at the operation's current resolved target.
- Failure scope for `al skills` operations: a source-level fetch, authentication, or ref failure blocks its whole block (and every blocked skill is still reported individually); a per-skill validation, merge, or freshness failure blocks only that skill. `al skills add` and `al skills remove` are the exception — they preflight the entire desired-set change and leave local state untouched if any part of it fails.

## DeepSWE benchmark

- The website planner is the canonical task-selection surface. It exports arbitrary selected tasks with two to four repetitions per arm; two is the minimum because the observed report estimates each task-arm sample variance. The Go benchmark commands validate and execute the exact pasted or downloaded JSON allocation and never optimize tasks or repetitions. The earlier locked nine-task suite is superseded.
- The embedded public v1.1 snapshot is a reviewed offline input, not runtime web data. Its results use `mini-swe-agent`, so published-only output is reference-relative and cannot attribute a difference to Agent Layer.
- Baseline identity excludes Agent Layer treatment content and scheduler task concurrency. A changed effective instruction/skill bundle creates a new treatment version while compatible local baseline attempts remain reusable whether missing tasks execute sequentially or with bounded workers.
- DeepSWE cost evidence counts each coordinator and child session once. Codex 0.144.6 does not expose which non-cache-read input tokens were cache writes, so both baseline and treatment use the same token-derived API-equivalent minimum/maximum bounds; their midpoints are presentation-only. Claude reports an exact coordinator cost through Pier and an exact `total_cost_usd` for each dispatched child, so no Claude pricing estimate is used. Reports render horizontal cost-accounting bounds; verifier build failures remain scored evidence and are labeled on task rows.
- The DeepSWE instructions-and-skills arm uses Pier's 4× agent-timeout multiplier so its complete multi-agent workflow is not truncated by the upstream 15-minute single-agent budget. This execution-affecting value is treatment-only, persisted in run identity, and shown in report provenance.
- Benchmark optimization is an iterative campaign: keep the selected tasks, repetitions, model/reasoning level, and compatible bare-model baseline fixed; audit the current Agent Layer treatment's cost; make the smallest evidence-based instruction or skill change; run only the revised treatment; and append it to the same campaign report. Compare every treatment with the shared baseline using its observed score, decision threshold, cost range, and cost multiple. Task reselection and baseline reruns are unnecessary unless their compatibility inputs change.
- Product-development sequence: first merge the production planner/runner/report delivery; then deeply analyze the completed Luna-low treatment for evidence-backed cost reductions that preserve quality; port the user's selected skills/instructions changes from the other repository; run the revised treatment against the same compatible baseline and plan; and evaluate the real multi-iteration campaign. Only after that evidence should the planner UI and campaign report receive their major general-consumption redesign, followed by complete user documentation and publication.

## Secrets

- Codex is the one exception to "never embed secrets in generated configs": it embeds secrets in URLs/env via `bearer_token_env_var`, and shell environment takes precedence over `.agent-layer/.env`. All other clients use placeholder syntax in generated configs.
- `.codex/config.toml` is shared Codex state patched by `al sync`, not a fully generated artifact. It keeps a `PARTIALLY GENERATED FILE` warning because it may contain resolved secrets, but sync preserves unrelated Codex/user runtime entries and only refreshes known Agent Layer-owned paths.

## VS Code and editor integrations

- The Codex VS Code extension reads `CODEX_HOME` only at process startup. Agent Layer launchers set repo-local `CODEX_HOME` only when `agents.codex.local_config_dir = true`; absent/false preserves inherited `CODEX_HOME`.
- The Claude VS Code extension shares MCP scope with Claude CLI: both read the same `.mcp.json`/`.claude/settings.json`. Configuring separate MCP server sets for the two surfaces is not possible. `[agents.claude_vscode]` is config-only.
- `.vscode/settings.json` updates only validate JSONC content inside the managed markers. Invalid JSONC outside the markers is not detected once the markers are present.

## Codex MCP headers

- Codex MCP header projection accepts exactly three placeholder formats: `bearer_token_env_var` for `Authorization: Bearer ${VAR}`, `env_http_headers` for exact `${VAR}` values, and `http_headers` for literal strings. Mixed literal + env placeholder (e.g. `Token ${VAR}`) is rejected and must be restructured.
- Codex project trust is seeded only when the exact `[projects."<absolute repo root>"]` entry is absent. Existing exact project entries are preserved so local trust edits survive. Malformed `agents.codex.agent_specific.projects` shapes fail sync loudly.

## Wizard feature-disable toggles → client keys

- The wizard's per-feature "disable" toggles map to these `agent_specific` keys (written only when the user opts in; absence = client default):
  - Codex (`agents.codex.agent_specific.features`, appended to `.codex/config.toml`): `browser_use`/`in_app_browser`/`computer_use = false` (browser/computer-use), `apps = false` (built-in apps).
  - Claude (`agents.claude.agent_specific`, deep-merged into `.claude/settings.json`): `env.CLAUDE_CODE_AUTO_CONNECT_IDE = "false"` (IDE open-file reading), `env.ENABLE_CLAUDEAI_MCP_SERVERS = "false"` (claude.ai connectors), `autoMemoryEnabled = false` (auto-memory).
  - AskUserQuestion is the exception: it is a typed `agents.claude.disable_question_tool` bool, not an `agent_specific` key (its value is array-shaped — a `permissions.deny` entry and a `PreToolUse` hook — which the line-based wizard patcher cannot safely union with user entries). When true, `buildClaudeSettings` (`internal/sync/claude_question_tool.go`) injects the deny entry (array-union/dedup) and the PreToolUse hook (dedup by matcher) into the generated settings, after the agent_specific merge so user deny/hooks are preserved. The hook is always emitted (enforces the block under YOLO, where `permissions.deny` is skipped).
- Value types: settings.json `env` values are JSON strings, so the wizard writes the quoted string `"false"`; `autoMemoryEnabled` and the Codex `features.*` flags are booleans (`false`). `CLAUDE_CODE_DISABLE_AUTO_MEMORY` is deliberately not used (it takes `1`/`0`, not `false`).
- The Claude patch writer (`applyClaudeAgentSpecificUpdate`, `internal/wizard/patch.go`) writes dotted `agent_specific.*` keys into `[agents.claude]` unless the user expanded `agent_specific` into explicit sub-tables (`[agents.claude.agent_specific(.env)]`), in which case it writes the leaf into that section to avoid a TOML duplicate-table error.

## Antigravity

- Antigravity uses the `agy` binary and is launched with `--gemini_dir=<repo>/.agy` plus `AGY_CLI_DISABLE_AUTO_UPDATE=1` for repo-local containment. Agent Layer writes `.agy/antigravity-cli/settings.json` and `.agy/antigravity-cli/mcp_config.json`.
- Antigravity model selection is `agents.antigravity.model` and sync projects it into generated `settings.json`; `agents.antigravity.agent_specific.model` is unsupported because `model` is an Agent Layer-owned field.
- `agy` v1.0.0 migrates `.agy/antigravity-cli/mcp_config.json` into `<gemini_dir>/config/mcp_config.json`, but runtime MCP discovery remains false in the observed probe baseline. Use `al probe agy` when checking whether upstream behavior has changed. The probe now seeds a real protocol server (`al __probe-mcp-fixture`) instead of `/usr/bin/true`, so `mcp_runtime_discovery: false` and `mcp_tool_invoked: false` are evidence about `agy`, not about the fixture; agy 1.1.8 still reports both false.

## Agent Dispatch

- Agent Dispatch's public request/API shapes should stay caller- and provider-agnostic. Target-specific model or option discovery belongs behind the target/provider registry, not as fields like `AntigravityModels` on dispatch request structs.
- Agent Dispatch is asynchronous and handle-based. Its read-only `options` command discovers valid targets and overrides; its only lifecycle operations are `start`, `wait`, `continue`, and `cancel`. Parallel work uses independent conversations rather than a fanout resource.
- Agent Dispatch has two surfaces over one backend: the built-in `agent-layer` MCP server (`al dispatch mcp-server`, exposing `dispatch_options`/`dispatch_start`/`dispatch_wait`/`dispatch_continue`/`dispatch_cancel`) is the canonical agent-facing path, and the CLI is the human/scripting path. MCP handlers render through the same operations into private buffers and decode the canonical JSON result; nothing but the MCP SDK writes to the server's stdout. Cancelling an MCP request stops only that wait; only `cancel` stops provider work.
- The built-in MCP server is derived state, not an `[[mcp.servers]]` entry. `internal/projection.EffectiveMCPServers`/`EffectiveServerIDs` is the single boundary that adds it, so native projection, permission allowlists, warning accounting, and doctor all see the same set.
- Immutable run records are canonical history. Friendly names are lookup mappings, and factual workflow manifests must not carry recommendation, risk, readiness, confidence, verdict, or synthesis fields between independent stages.

## Pin file recovery

- Empty or non-semver `.agent-layer/al.version` pin files are treated as "no pin" and auto-repaired by `al init`/`al upgrade` without prompts. The user sees a warning, never a hard error. A broken pin file must never make the CLI unusable.

## Upgrade and migration internals

- When source version cannot be resolved during `al upgrade`, source-agnostic migration operations still execute; source-gated operations are skipped with deterministic report entries. Ambiguous repos may need an explicit follow-up if the skip report flags missed transitions.
- Multi-version upgrades chain migration manifests: all manifests between source (exclusive) and target (inclusive) load in order with per-operation deduplication by ID. When source is unknown, only the target manifest loads (backward compatible). Manifests must have unique operation IDs across the chain or later duplicates are silently skipped.
- The required-field migration guardrail uses a baseline allowlist for fields that predate manifest enforcement (baseline version `0.8.1`). The allowlist must be maintained when introducing new required fields; stale entries can hide drift if not reviewed.

## E2E test harness

- `scripts/test-e2e/harness.sh` authenticates GitHub API calls with `GITHUB_TOKEN`/`GH_TOKEN` when available (raises the limit from 60 req/hr to 5000 req/hr). CI exports the token to `make ci`. Unauthenticated fallback is preserved for local offline runs.

## Test policy

- Do not write tests that assert specific wording, language, headings, or prose contracts in skill and instruction templates. Those checks are tautological and brittle. Tests may verify Agent Layer mechanics such as parsing, validation, sync/projection, resource copying, file existence/absence, and generated artifacts.
- When a test must confirm a template/instruction file was seeded or written in full (not stubbed/truncated), compare the output against the template read at runtime (`templates.Read(<path>)`) and key dedup/occurrence checks on the migration op's own fields (`op.Match`/`op.From`/`op.Value`) — never hardcode template prose as the proxy. Runtime comparison tracks the source of truth, survives content edits, and still catches stub/truncation regressions (see `TestExecuteAppendToFile_*` in `internal/install`).

## Root resolution in cmd/al tests

- Root resolution (`internal/root` `FindAgentLayerRoot`/`FindRepoRoot`) walks upward from the working dir and stops only at a `.agent-layer/` or the filesystem root — there is no intermediate ceiling. `cmd/al` tests run from `t.TempDir()` (under the OS temp dir) and assume no ancestor holds a `.agent-layer`.
- A stray `.agent-layer` above the temp dir — e.g. `/tmp/.agent-layer` left by running `al init`/`al wizard`/`make al-*` while `cd`'d into `/tmp` — makes resolution escape the test sandbox. Symptoms: `cmd/al` tests fail with `got "/tmp"` or `already initialized in an ancestor directory (/tmp)`. Fix: `rm -rf /tmp/.agent-layer`, and don't run `al init` from `/tmp`. CI runners are clean, so this is a local-only gotcha.
