# Backlog

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Unscheduled user-visible features and tasks (distinct from issues; not refactors). Maintainability refactors belong in ISSUES.md.

## Format
- Insert new entries immediately below `<!-- ENTRIES START -->` (most recent first).
- Keep each entry **3–5 lines**.
- Line 1 starts with `- Backlog YYYY-MM-DD <id>:` and a short title.
- Lines 2–5 are indented by **4 spaces** and use `Key: Value`.
- Keep **exactly one blank line** between entries.
- Prevent duplicates: search the file and merge/rewrite instead of adding near-duplicates.
- When scheduled into ROADMAP.md, move the work into ROADMAP.md and remove it from this file.
- When implemented, remove the entry from this file.

### Entry template
```text
- Backlog YYYY-MM-DD short-slug: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <what the user should be able to do>
    Acceptance criteria: <clear condition to consider it done>
    Notes: <optional dependencies/constraints>
```

## Features and tasks (not scheduled)

<!-- ENTRIES START -->

- Backlog 2026-05-26 shipped-skill-update-channel: Keep bundled skills current
    Priority: High. Area: skills / templates
    Description: Define a repeatable way to track, review, and refresh Tavily and other third-party or bundled skills shipped with Agent Layer.
    Acceptance criteria: Agent Layer has an update workflow that detects upstream skill changes, preserves local policy edits, and verifies refreshed skills before release.
    Notes: Include license/notice preservation, routing quality, and repo-specific guardrails in the refresh process.

- Backlog 2026-05-22 antigravity-slash-skill-verification: Re-verify Antigravity slash skill dispatch syntax
    Priority: Medium. Area: providers / antigravity
    Description: Re-check that `agy --print "/skill-name"` still invokes projected skills when Antigravity changes its CLI or skill behavior, especially on minor version bumps.
    Acceptance criteria: Compatibility/probe coverage catches a broken Antigravity slash-skill invocation contract before Agent Dispatch relies on it in a release.
    Notes: Official Antigravity docs say skills can be mentioned by name but do not currently guarantee slash syntax as a CLI contract.

- Backlog 2026-05-22 codex-app-server-stability: Revisit Codex App Server when it becomes stable
    Priority: Medium. Area: providers / codex
    Description: Track OpenAI Codex CLI releases and documentation for when `codex app-server` is no longer experimental, then reassess whether Agent Dispatch should switch Codex from `codex exec` final-answer output to App Server's streamed `item/agentMessage/delta` protocol.
    Acceptance criteria: A future review confirms Codex App Server stability status from official docs/local help and records whether Agent Dispatch should migrate to it.
    Notes: Current dispatch design uses stable `codex exec` despite final-answer-only assistant output because the true streaming App Server path is still experimental.

- Backlog 2026-05-22 agent-cli-compatibility-tests: Comprehensive automated compatibility testing for agent CLIs
    Priority: High. Area: testing / providers
    Description: Build a comprehensive, automated compatibility test suite that exercises each supported agent CLI (Claude Code, Codex, Antigravity, and any future providers) against the concrete assumptions Agent Layer makes — config file formats and paths, settings/hook schemas, skill discovery layout, memory file locations, MCP server registration, command/flag surface, exit codes, stdout/stderr conventions, and session/launcher behavior. The suite should run against pinned and latest CLI versions on a schedule (CI matrix) so that when a new release breaks an Agent Layer assumption, the failure is detected and attributed to a specific CLI version before users hit it. Build on the existing `al probe agy` pattern (stable JSON contract, forensic workspace, non-zero exit on failure) by generalizing it to `al probe claude` and `al probe codex`, then drive the CI matrix off probe output diffed against pinned baselines.
    Acceptance criteria: A dedicated compatibility test suite exists and runs in CI against at least Claude Code, Codex, and Antigravity; each supported assumption (config paths, schema fields, command surface, sync/launch behavior) is covered by an explicit test; the suite runs against both a pinned baseline and the latest released version of each CLI on a recurring schedule; failures report which CLI, which version, and which Agent Layer assumption broke.
    Notes: Should be cheap to extend when a new provider lands. Consider version-matrix CI (e.g., GitHub Actions matrix over CLI versions) and snapshotting expected config/output shapes. Coordinate with ROADMAP for scheduling — this is foundational reliability work, not a user-visible feature, but it directly protects user-visible features from silent breakage. The Antigravity probe (see COMMANDS.md "Run the Antigravity capability probe") is the established precedent — extend it rather than invent a parallel mechanism. Part of this work is factoring the probe pattern into a shared primitive other providers can adopt.

- Backlog 2026-05-22 refresh-marketing-readme: Refresh README and marketing copy for current features
    Priority: Medium. Area: docs / marketing
    Description: Update the repository README and public marketing page copy to sell the major features shipped since the older v0.6-era messaging, including newer launcher, skills, upgrade, wizard, sync, and Antigravity capabilities.
    Acceptance criteria: README and marketing page accurately present the current product value, name the strongest current features, and no longer read like the v0.6-era feature set.
    Notes: Identify the current marketing-page source during planning; likely includes the sibling website repo.

- Backlog 2026-05-07 mcp-catalog-docs-sibling-repo: Rewrite sibling-repo MCP-catalog docs after seed split
    Priority: Medium. Area: docs / agent-layer-web
    Description: After the wizard-catalog split landed (Decision 2026-05-07 mcp-catalog-seed-split), two pages in the sibling `agent-layer-web` repo are factually wrong: `docs/concepts.mdx:218` "Seeded servers (disabled by default)" list still implies the install seed ships six pre-disabled blocks, and `docs/getting-started.mdx:99` "`al init` seeds a small library of high-value MCP servers" sentence is no longer true. Rewrite both to describe the wizard-catalog model. Optionally, author a new canonical config-reference page that the slim seed's `[mcp]` URL (`https://agent-layer.dev/docs/reference#mcp-servers`) eventually points at as a dedicated page rather than a section anchor.
    Acceptance criteria: `concepts.mdx` and `getting-started.mdx` accurately describe that `al init` ships zero `[[mcp.servers]]` blocks and the wizard owns the catalog; the docs URL referenced in the slim seed renders to a real, usable page.
    Notes: Cross-repo work; out of scope for the trim PR per user direction. The current `reference#mcp-servers` anchor exists and is accurate, so the slim seed's URL is not broken — this is a follow-up for narrative consistency.

- Backlog 2026-05-07 codex-openai-yaml-skill-metadata: Support Codex `agents/openai.yaml` skill metadata
    Priority: Medium. Area: skills / codex
    Description: Add Agent Layer support for Codex-specific optional skill metadata in `agents/openai.yaml`, including UI metadata, implicit invocation policy, and tool dependency declarations when a concrete built-in or user skill needs those Codex-native extensions.
    Acceptance criteria: Agent Layer can author or project Codex `agents/openai.yaml` metadata without weakening portable Agent Skills support or requiring duplicate source-of-truth files.
    Notes: Deferred from the native skill folder alignment plan; document in the client skill spec first and implement only when there is a concrete use case.

- Backlog 2026-04-26 design-space-no-cost-prefilter: Do not pre-filter design options by implementation cost
    Priority: Medium. Area: instructions / templates
    Description: Add a principle to template instructions: when presenting design options to the user, present the full design space without pre-filtering by implementation cost, migration size, or code-change effort. Lead with quality and correctness. Cost (migration size, lines touched, test surface) is a separate dimension to lay out alongside the options, not a filter that prunes them. The user picks. Likely home: extend `01_base.md` Critical Protocol rule 3 ("Stop and ask when real tradeoffs exist") with an explicit no-cost-prefilter clause, or add a sibling rule under the same section. If neither fits cleanly, consider a new "Decision presentation" subsection.
    Acceptance criteria: Template instructions state explicitly that design-option presentation must not suppress higher-quality options on cost/effort grounds, and that cost is presented as a parallel dimension; agents surface the full design space and let the user choose.
    Notes: Source: castle-steward (the project that dogfoods agent-layer), 2026-04-26. Agent suppressed cleaner schema designs because they required larger migrations. Nick's correction: "I don't like accidental schema choices driving any decision-making. We have complete control over our design and can do anything that we want. Remove all constraints you're putting on yourself and leave those for me to decide. It is your job to tell me pros and cons and ramifications, but then I get to make the final call." General agent-behavior pattern, not Nick-specific.

- Backlog 2026-04-25 codebase-cleanup-rules-6-7-8: Codify cleanup rules 6–8 into skills and agent instructions
    Priority: High. Area: skills / code quality
    Description: Codify three cleanup rules in audit/cleanup skills and coding conventions: remove unnecessary try/catch or defensive programming; remove deprecated, legacy, or fallback code; remove AI slop, stubs, larp, and unhelpful in-motion comments or replace them with concise useful comments.
    Acceptance criteria: Rules 6–8 appear verbatim (or as direct derivatives) in at least one audit/cleanup skill and in coding conventions; agents flag violations during code review.
    Notes: Source: @shawmakesmagic 8-subagent cleanup prompt (x.com/shawmakesmagic/status/2044269097647779990). Nick flagged these as strongly aligned with his existing values but underrepresented in current firmware.

- Backlog 2026-04-18 cross-provider-hook-matrix: Audit hook support across Claude Code, Codex, and Antigravity
    Priority: Medium. Area: providers / hooks
    Description: Claude Code has 8 documented hook types (SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Notification, Stop, SubagentStop, PreCompact). Determine what equivalent hook systems exist in Codex and Antigravity — specifically session-start, pre/post-tool, and stop hooks — and identify gaps or provider-specific capabilities worth exploiting. Produce a cross-provider hook compatibility matrix in agent-layer docs.
    Acceptance criteria: Matrix doc exists in agent-layer docs listing each Claude hook type with Codex and Antigravity equivalents (or "not supported"); firmware improvements that use hooks (e.g., auto-load git context at session start, post-tool formatting) are annotated as cross-provider or Claude-only.
    Notes: Source: The Code newsletter review session, 2026-04-18. Current hook implementations in agent-layer assume Claude-specific hook names; gaps block parity improvements for Codex and Antigravity.

- Backlog 2026-04-18 dynamic-context-injection: Audit skills for `!` dynamic context injection opportunities
    Priority: Medium. Area: skills
    Description: Claude Code SKILL.md files support a `!` prefix that auto-executes a shell command and injects the output as context before the skill runs (documented in Anthropic docs under 'Inject dynamic context'). Audit existing skills for places where `!` injection could replace manual setup steps — e.g., current branch, git log, open PRs, inbox state. Also add a `!` injection example to SKILL-DESIGN.md as a design pattern.
    Acceptance criteria: Priority skills updated to use `!` injection where applicable; SKILL-DESIGN.md documents the pattern with a concrete example (e.g., `!` + gh pr diff for a pr-summary skill).
    Notes: Source: The Code newsletter review session (2026-04-18). Open questions: whether syntax works in frontmatter vs body only, and token-cost implications for large command outputs (e.g., full git log).

- Backlog 2026-04-15 audit-skill-priority-ranking: Add priority-ranked checks to audit-style skills
    Priority: Medium. Area: skills
    Description: SKILL-DESIGN.md now requires audit-style skills to order checks by impact and label them Critical → High → Medium → Low. Retrofit `audit-documentation`, `audit-tests`, and `audit-memory` to apply this ordering throughout their check lists.
    Acceptance criteria: All three audit skills have their checks explicitly ordered and labeled by priority level; highest-impact checks appear first in each section.
    Notes: SKILL-DESIGN.md updated 2026-04-15. Leverages primacy bias — agents under context pressure apply earlier checks most reliably.

- Backlog 2026-04-14 lsp-integration: LSP integration for precise code navigation in agent sessions
    Priority: Low. Area: tooling / agent capabilities
    Description: Provide Language Server Protocol access as a tool for agents working in any registered project. Enables go-to-definition, find-all-references, and call hierarchies — things grep approximates but cannot do precisely (e.g., can't distinguish a function call from a same-named variable, or find all implementations of an interface across files).
    Acceptance criteria: Agents can query an LSP for a symbol and get precise cross-file results; works for at least one language (TypeScript or Python); accessible via MCP tool or CLI wrapper from al.
    Notes: Each language needs its own server (pyright, typescript-language-server, gopls). Run on-demand rather than always-on. Grep covers most cases today — this is a quality-of-life improvement for large, complex codebases.

- Backlog 2026-04-13 magic-constant-rationale-comments: Require motivation comments on magic constants
    Priority: Medium. Area: conventions / code quality
    Description: Add a convention to the agent-layer rules (or `04_conventions.md`) that any magic constant — retry counts, timeouts, failure caps, rate limits — must include an inline comment documenting the specific observed failure or reasoning that motivated it. Example pattern: `MAX_RETRIES = 3  # 2026-03-10: saw 27 consecutive failures on email triage burning ~250K API calls — capped at 3`. Makes future decisions auditable and prevents constants from being removed by someone who does not know why they exist.
    Acceptance criteria: Convention documented in the rules or conventions file with the example pattern; scope (retry counts, timeouts, failure caps, rate limits, and similar thresholds) is explicit.
    Notes: Optional follow-up: audit the existing codebase for undocumented magic constants and backfill rationale where the reasoning is known.

- Backlog 2026-04-12 worktree-isolation-for-agent-sessions: Worktree isolation mode for risky agent coding tasks
    Priority: Medium. Area: execution / safety
    Description: Add an `al worktree` command (or session flag) that creates a fresh git worktree and isolated branch before launching an agent session. Agent work happens on the branch; changes only reach main after review. Prevents incomplete or incorrect agent runs from polluting the default branch, especially in multi-step coding tasks.
    Acceptance criteria: `al worktree <project> "<prompt>"` creates a worktree, launches the agent scoped to that directory, and prompts to merge or discard on exit; existing `al claude` sessions are unaffected.
    Notes: Inspired by Claude Code's EnterWorktree/ExitWorktree tool pattern. Known upstream issue: compaction mid-session can cause nested worktrees if CWD reconstruction is wrong — design should account for this.

- Backlog 2026-03-23 disable-client-memory-config: Agent-specific config option to disable client memory systems
    Priority: Medium. Area: config / sync
    Description: Add an agent-specific config option (e.g., `disable_client_memory = true`) in config.toml that causes `al sync` to inject the appropriate memory-disable setting into each agent's generated config (e.g., `autoMemoryEnabled=false` for Claude, equivalent memory controls for Antigravity if available, etc.). Centralizes memory disable logic in config.toml rather than requiring users to know each client's specific setting name.
    Acceptance criteria: A per-agent config.toml flag exists; `al sync` translates it to the correct client-specific setting for each supported agent; documented in README or CONTEXT.md.
    Notes: Source: claude-assistant project discovered the need while disabling memory across all agents. Each client has a different mechanism — the mapping must be maintained as new clients are added.

- Backlog 2026-03-21 reassess-skill-resources: Audit template skills for scripts/references/assets opportunities
    Priority: Medium. Area: skills
    Description: Now that skill sync copies subdirectories (scripts/, references/, assets/) to all clients, audit existing template skills to identify where adding scripts, reference docs, or asset files would improve skill effectiveness (e.g., validation scripts, detailed reference guides, config templates).
    Acceptance criteria: Each template skill has been reviewed; skills that benefit from subdirectories have them added; a decision is recorded on which skills are body-only vs resource-enhanced.
    Notes: Re-evaluate against current code (post-Phase 16). Antigravity now consumes the shared `.agents/skills/` tier, so resource-enhanced skills also surface there.

- Backlog 2026-03-17 reassess-memory-pruning: Observe strengthened memory-pruning approach
    Priority: Low. Area: instructions / memory
    Description: Memory pruning was strengthened: DoD now explicitly names DECISIONS.md and CONTEXT.md pruning, a "What NOT to store" section was added to 02_memory.md, and a character-budget awareness rule (~8,000 chars/~2,000 tokens per file) was added. Observe whether agents follow these guidelines and whether memory files stay lean without excessive friction.
    Acceptance criteria: Decision recorded on whether the strengthened approach is effective, needs further tuning, or should be replaced with a periodic audit trigger.
    Notes: Strengthened 2026-03-17 based on audit of research from Anthropic, OpenAI, JetBrains Research, and Letta on memory system best practices.

- Backlog 2026-03-16 conventions-entry-markers: Add insertion markers to 04_conventions.md
    Priority: Low. Area: templates / conventions
    Description: Add `<!-- ENTRIES START -->` markers to `04_conventions.md` so that users and agents can add project-specific conventions below the marker, matching the pattern used by memory files. Keep the header and preamble above the marker as managed content.
    Acceptance criteria: `04_conventions.md` template has entry markers; `al init` and `al upgrade` preserve user entries below the marker; existing conventions in deployed repos are not lost on upgrade.
    Notes: Requires ownership policy update in `ownership_policy.go` and manifest generator in `gentemplatemanifest/main.go` to treat this file as section-aware.

- Backlog 2026-03-06 skill-line-width-warning: Warn on long lines in skill files
    Priority: Low. Area: skills / validator
    Description: Add a soft warning (not a hard error) when a skill file contains lines exceeding a character threshold (e.g., ~1000 chars). Protects against silent truncation by tools that impose per-line limits (Claude Code's Read tool truncates at 2000 chars) regardless of how skills are loaded at runtime.
    Acceptance criteria: `al doctor` emits a warning for skill files with lines over the threshold; no hard error; threshold is configurable or clearly documented.
    Notes: This is a DX/tooling concern, not an LLM performance concern — tokenization ignores line breaks. The current native skill sync path is unaffected, but future delivery mechanisms may impose different line-length limits.

- Backlog 2026-03-06 audit-security-skill: Add a security audit skill
    Priority: Medium. Area: skills
    Description: A skill that scans for dependency CVEs, code-level vulnerabilities (OWASP patterns), secrets/credentials in code, and insecure configurations, then produces a findings report.
    Acceptance criteria: Skill produces actionable findings report with severity levels; integrates with resolve-findings for remediation workflow.
    Notes: Should leverage available tools (dependency audit commands, grep-based pattern matching) rather than requiring external scanners.

- Backlog 2026-03-01 remove-skill-migrations-v010: Remove one-off skill migration code by v0.10.0
    Priority: Medium. Area: upgrade / skills
    Description: If support signals are clean by v0.10.0, remove legacy one-off skill migration code paths so we do not carry indefinite compatibility shims.
    Acceptance criteria: A v0.10.0 task removes obsolete skill migration code while preserving upgrade behavior for supported source versions.
    Notes: Gate removal on evidence that dropping the migration path does not create user-impacting upgrade regressions.

- Backlog 2026-02-25 playwright-headless-parity: Evaluate headless Playwright mode without functional regressions
    Priority: Medium. Area: test automation / Playwright runner UX
    Description: Evaluate whether Playwright can run in headless mode by default without functional regressions. Headed mode is noisy and disruptive during normal development.
    Acceptance criteria: Headless and headed modes compared for behavior parity; headless made default if parity is confirmed; explicit opt-in path preserved for headed debugging.
    Notes: Keep an explicit opt-in path for headed runs for local debugging even if headless becomes the default.

- Backlog 2026-02-16 skill-install: Install community skills from external sources
    Priority: Low. Area: skills / ecosystem
    Description: Allow users to install agentskills.io-compliant skills from GitHub repos or a registry (e.g., `al skill add <repo>` or `al skill add <name>`).
    Acceptance criteria: Users can install a skill from an external source into `.agent-layer/skills/` and it is picked up by `al sync`.
    Notes: Depends on Phase 15 (skills standard alignment). Consider validation, versioning, and update mechanisms.

- Backlog 2026-02-10 test-agents: Multi-agent test strategy and execution workflows
    Priority: High. Area: workflows/testing
    Description: Multi-agent workflow where one agent "dreams up" and documents test strategies/cases (unit/E2E/integration), while a second agent implements tests, runs them, fixes failures, and opens PRs.
    Acceptance criteria: E2E agents use tools like Playwright to actively "break things," turning failures into new tests; results are delivered as complete PRs with code and tests.
    Notes: Must distinguish between test types; requires full product access for E2E agents to explore and find edge cases.

- Backlog 2026-01-25 c3d4e5f: Auto-merge client-side edits back to agent-layer sources
    Priority: Medium. Area: config synchronization
    Description: Auto-merge client-side approvals or MCP server edits back into agent-layer sources.
    Acceptance criteria: Changes made in client configs are detected and merged back to source files.
    Notes: Needs conflict resolution strategy and user confirmation for ambiguous merges.

- Backlog 2026-01-25 deep-cluster: Aspirational ideas (low priority, kept as searchable headlines)
    Priority: Low. Area: research / aspirational
    Description: One-line headlines preserved from 2026-01-25 brainstorming (original short-slugs kept for searchability): interaction monitoring (a1b2c3d); task queue for chained ops (d4e5f6a); multi-agent chat tool (f6a7b8c); unified docs repository with MCP access (a7b8c9d); indexed/searchable chat history (b8c9d0e); per-provider conversation persistence (c9d0e1f); unified shell/command MCP for centralized allowlisting (1a2b3c4).
    Acceptance criteria: Each headline either promoted to its own 5-line entry and scheduled into ROADMAP.md, or explicitly retired with rationale in DECISIONS.md.
    Notes: Expand any single item back into its own 5-line entry only when promotion to ROADMAP looks likely.
