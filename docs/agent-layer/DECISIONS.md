# Decisions

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
A rolling log of important, non-obvious decisions that materially affect future work (constraints, deferrals, irreversible tradeoffs). Only record decisions that future developers/agents would not learn just by reading the code. Do not log routine choices or standard best-practice decisions; if it is obvious from the code, leave it out.

## Format
- Keep entries brief and durable (avoid restating obvious defaults).
- Keep the oldest decisions near the top and add new entries at the bottom.
- Insert entries under `<!-- ENTRIES START -->`.
- Line 1 starts with `- Decision YYYY-MM-DD <id>:` and a short title.
- Lines 2–4 are indented by **4 spaces** and use `Key: Value`.
- Keep **exactly one blank line** between entries.
- If a decision is superseded, replace the old entry with the new one. Fold the old entry's tradeoff context into the new entry's `Reason` field when it is still valuable, then remove the old entry.
- Periodically consolidate: remove entries that are now self-evident from the codebase (the decision is embodied in code, tests, or docs and a reader would learn it without the log). When removing, verify the tradeoff information is not uniquely preserved in the log.

### Entry template
```text
- Decision YYYY-MM-DD short-slug: Short title
    Decision: <what was chosen>
    Reason: <why it was chosen>
    Tradeoffs: <what is gained and what is lost>
```

## Decision Log

<!-- ENTRIES START -->

- Decision 2026-01-22 f1e2d3c: Distribution model (global CLI with per-repo pinning)
    Decision: Ship a single globally installed `al` CLI with per-repo version pinning via `.agent-layer/al.version` and cached binaries.
    Reason: A single entrypoint reduces support burden while pinning keeps multi-repo setups reproducible.
    Tradeoffs: Pin management adds operational complexity and requires explicit upgrade flows.

- Decision 2026-01-24 a1b2c3d: Ignore unexpected working tree changes
    Decision: Agents will not pause, warn, or stop due to unexpected working tree changes (unstaged or staged files not created by the agent).
    Reason: The user works in parallel with agents, making concurrent changes a normal operating condition.
    Tradeoffs: Increases risk of edit conflicts if both user and agent modify the same file simultaneously; relies on git for resolution.

- Decision 2026-01-25 7e2c9f4: Agent-only workflow artifacts live in `.agent-layer/tmp`
    Decision: Workflow artifacts are written to `.agent-layer/tmp` using a unique per-invocation filename: `.agent-layer/tmp/<workflow>.<run-id>.<type>.md` with `run-id = YYYYMMDD-HHMMSS-<short-rand>`; no path overrides.
    Reason: Keeps artifacts invisible to humans while avoiding collisions for concurrent agents without relying on env vars or per-chat IDs.
    Tradeoffs: Files can accumulate until manually cleaned; agents must echo paths in chat to retain context.

- Decision 2026-02-03 docs-curated-pages: Curated single-page docs are the source of truth (supersedes c7d2a1f + d9e3a7b)
    Decision: Stop generating CLI docs during website publish. The curated `site/docs/reference.mdx` (and the consolidated Concepts, Getting started, Reference, and Troubleshooting single-page sections under `site/docs/`) are the source of truth. Users rely on `al --help` for authoritative flag output.
    Reason: Help-output dumps duplicated content and reduced usability; fragmented per-topic pages hurt cohesion. Curation gives a single editable home for each topic.
    Tradeoffs: The guide can drift from exact flags. Old per-topic URLs broke; cross-links must use anchors on the consolidated pages.

- Decision 2026-02-10 p1c-init-upgrade-ownership: Init scaffolding only; user-owned config/env; agent-only .gitignore
    Decision: `al init` is one-time scaffolding (errors if `.agent-layer/` already exists). Upgrades/repairs are done via `al upgrade plan` + `al upgrade`. `.agent-layer/.env` and `.agent-layer/config.toml` are user-owned and seeded only when missing (never overwritten by init/upgrade). `.agent-layer/.gitignore` is agent-owned internal and is always overwritten and excluded from upgrade plans/diffs.
    Reason: Avoid accidental clobbering of user-specific configuration, reduce cognitive load in upgrade plans, and simplify init semantics by removing upgrade behavior.
    Tradeoffs: Changes to `.agent-layer/.gitignore` cannot be preserved; repos without baseline evidence will require an `al upgrade` run to establish it (supersedes earlier init-overwrite guidance).

- Decision 2026-02-12 chlog-immutable: CHANGELOG entries are historical and immutable
    Decision: Never modify published CHANGELOG entries. They record what happened at the time of release and are treated as fixed historical records, even if terminology or paths have since changed.
    Reason: Changing historical entries undermines trust in the changelog as a factual record and can confuse readers comparing old entries against old tags.
    Tradeoffs: Stale references in old entries (e.g., renamed files) remain; readers must consult current docs for the latest names.

- Decision 2026-02-14 p2b-upgrade-apply-flags: Remove `--force`; require explicit apply categories
    Decision: `al upgrade` no longer supports `--force`. Non-interactive runs require `--yes` plus one or more explicit apply flags (`--apply-managed-updates`, `--apply-memory-updates`, `--apply-deletions`), and deletion remains gated behind explicit `--apply-deletions`.
    Reason: Prevent accidental destructive upgrades and make non-interactive intent explicit per mutation category.
    Tradeoffs: Existing `al upgrade --force` automation breaks and must be migrated to explicit apply flags.

- Decision 2026-02-17 p12-yolo-mode: Approvals policy expanded to 5-mode system (supersedes f6a7b8c)
    Decision: Add `yolo` as a fifth `approvals.mode` value. YOLO mode auto-approves commands and MCP (like `all`) and also sends full-auto flags to each client: Claude `--dangerously-skip-permissions`, Gemini `--approval-mode=yolo`, Codex `approval_policy=never` + `sandbox_mode=danger-full-access`, VS Code `chat.tools.global.autoApprove=true`.
    Reason: Users running in sandboxed/ephemeral environments want to skip all permission prompts without per-client manual configuration.
    Tradeoffs: YOLO bypasses all safety prompts; a single-line `[yolo]` acknowledgement on stderr (not a structured warning) informs users on every sync and launch. The template config comment and documentation carry the risk explanation. No `al doctor` warning — YOLO is a deliberate choice, not a health issue.

- Decision 2026-02-18 config-resilience: Strict-by-default parsing with lenient repair path (supersedes config-migrate, unknown-key-repairable)
    Decision: Remove `Config.Migrate()` entirely. Runtime commands (`al sync`, `al claude`, etc.) parse strictly via `ParseConfig`/`LoadConfig` and reject unknown TOML keys (including in enable-only agent sections). `al wizard` and `al doctor` use `ParseConfigLenient`/`LoadConfigLenient` to fall back to lenient loading so they always work on broken configs. `al upgrade` uses `config_set_default` migration operations that prompt the user interactively for new required field values. Unknown-key failures are wrapped as `ErrConfigValidation` so repair tools can still load leniently and guide repair.
    Reason: Silent defaults violate the "no silent fallbacks" rule. Repair tools must always be runnable. Every config value should come from an explicit user choice. Silent unknown-key acceptance hid invalid config; hard-failing repair tools made recovery impossible.
    Tradeoffs: Users must run `al wizard` or `al upgrade` to fix broken configs instead of having them auto-repaired; runtime commands fail fast on unknown keys until users repair through wizard/doctor or manual edits. Accepted because explicit consent is a design principle.

- Decision 2026-02-20 gemini-auto-trust: `al sync` auto-trusts repo in `~/.gemini/trustedFolders.json`
    Decision: When Gemini is enabled, `al sync` writes the repo root as `TRUST_FOLDER` to `~/.gemini/trustedFolders.json` (outside the repo). Failures produce a non-fatal warning, never a sync error.
    Reason: Gemini CLI's Trusted Folders feature silently replaces untrusted project settings with `{}`, discarding all MCP servers. Users already expressed trust by enabling Gemini in `config.toml`; propagating that trust to the Gemini runtime is the expected behavior.
    Tradeoffs: Writes to a file outside the repo boundary (`~/.gemini/`). Acceptable because this is a user-level runtime config (analogous to existing `~/.codex/` writes) and failure is non-fatal.

- Decision 2026-02-22 claude-config-dir: Opt-in repo-local `CLAUDE_CONFIG_DIR` via `local_config_dir` config field
    Decision: Gate `CLAUDE_CONFIG_DIR=<repo>/.claude-config` behind `[agents.claude] local_config_dir = true` (opt-in, default false). When enabled, `al claude` uses warn-and-preserve on mismatch and `al vscode` uses config-flag-based set/unset (matching `CODEX_HOME` pattern). Use `.claude-config/` (not `.claude/`) to avoid collision with the project-level `.claude/settings.json` generated by `al sync`. Keep `.claude-config/` in the gitignore block unconditionally.
    Reason: Claude Code writes a user-level `settings.json` to `CLAUDE_CONFIG_DIR` that would collide with Agent Layer's project-level `.claude/settings.json`. The UX cost (two `.claude*` directories) makes always-on inappropriate. Users who want per-repo settings and caches isolation enable it explicitly.
    Tradeoffs: Disabled by default means no isolation unless configured. Note: Claude Code stores auth in the OS credential store (macOS Keychain service `"Claude Code-credentials"`; Linux libsecret/gnome-keyring) regardless of `CLAUDE_CONFIG_DIR` (upstream limitation), so `local_config_dir` currently isolates settings and caches but not auth.

- Decision 2026-02-24 quiet-supersedes-p7a: Warnings verbosity policy (`reduce`, `quiet`, and `--quiet`)
    Decision: `warnings.noise_mode` supports `reduce` and `quiet`, and `--quiet`/`-q` provides command-line suppression. `reduce` suppresses only non-critical suppressible warnings; `quiet` suppresses all warnings (including critical) except `al doctor`, which always prints warnings.
    Reason: Users need one coherent verbosity model that serves both safer daily use (`reduce`) and zero-noise scripted flows (`quiet`/`--quiet`).
    Tradeoffs: Quiet mode can hide high-risk warnings by design, and older pinned binaries may ignore quiet behavior until upgraded.

- Decision 2026-03-02 user-owned-conventions: Extract project-specific conventions to user-managed `04_conventions.md`
    Decision: Move 5 project-specific instruction items (frontend rules, test coverage thresholds, package policies, typing requirements, schema safety) from `00_base.md` into a new `04_conventions.md` that is user-managed: seeded on `al init`, never overwritten on `al upgrade`, and excluded from managed diffs. A new `append_to_file` migration kind enables delivering new conventions to existing users via opt-in migration entries.
    Reason: Project-specific conventions create noise for projects where they don't apply; separating them lets users curate their own conventions while universal instructions remain managed.
    Tradeoffs: Conventions are no longer auto-updated during upgrades; new defaults require explicit migration entries to reach existing users.

- Decision 2026-03-02 instruction-reorder-dedup: Reorder instruction files and deduplicate cross-file instructions
    Decision: Rename instruction files to `00_rules.md`, `01_base.md`, `02_memory.md` (primacy effect: hard constraints load first). Remove 6 cross-file duplicates (keeping one canonical copy each), compress verbose sections (~50% fewer tokens), move UTC-only and No system Python from rules to conventions, add no-over-engineering and verification-closure instructions.
    Reason: Research shows instruction count and ordering affect model compliance; deduplication reduces ~57 to ~50 instructions and primacy-ordered hard constraints improve adherence.
    Tradeoffs: File renames require a v0.9.1 migration with 3 `rename_file` + 2 `append_to_file` operations; existing repos must run `al upgrade` to apply renames.

- Decision 2026-03-06 improve-codebase-separate-skill: Create `improve-codebase` as independent skill, not a mode of `audit-and-fix-uncommitted-changes`
    Decision: Create a new `improve-codebase` skill for whole-repository audit-and-fix sweeps rather than adding a scope-mode parameter to the existing `audit-and-fix-uncommitted-changes` skill. Accept structural duplication of the audit loop between the two skills.
    Reason: Research shows (1) prompt length alone degrades accuracy starting ~3K tokens (ACL 2024, 2025), (2) conditional branching is among the hardest instruction types (ComplexBench 2024), (3) constraint interference is a primary failure mode in multi-constraint prompts (ComplexBench 2024), (4) Anthropic recommends the routing pattern over mode-switching, (5) Rule of Three says wait until N=3 before extracting shared abstractions (Fowler). The two skills have different domain knowledge (target selection, phases, delegation targets) despite sharing an audit loop shape.
    Tradeoffs: ~70% structural similarity between the two skills is accepted as accidental duplication. If a third audit-loop skill appears, that triggers extraction per Rule of Three. See `docs/SKILL-DESIGN.md` for full research references.

- Decision 2026-03-06 skill-design-closures: Considered-and-rejected skill design refactors (review-scope split, orchestrator enumeration)
    Decision: Keep `review-scope` as a single skill with explicit-scope and proactive-hotspot modes (do not split). Keep the "when no broader orchestrator already owns closeout" phrasing in `implement-plan`, `fix-issues`, and `debug-issue` (do not enumerate parent skills).
    Reason: For `review-scope`: modes share Phases 2–3 and diverge only in Phase 1; splitting would duplicate ~45% of the skill; constraint count (~46) is under the 50-constraint threshold; target-resolution is a sequential 4-step cascade (not nested branching); ComplexBench shows splitting skills has worse interference than conditional branching within thresholds. For "broader orchestrator" phrasing: it is reliably determinable from conversation context; enumerating parent skills would couple to the orchestrator roster and require updates whenever an orchestrator is added/removed.
    Tradeoffs: `review-scope` Phase 1 has 3 target-type paths (mutually exclusive and well-scoped). The orchestrator phrasing requires the model to reason about invocation context, but this is standard MCP/delegation behavior. Both kept open as closures to prevent re-litigation.

- Decision 2026-03-21 native-skill-sync: Replace MCP prompt delivery with native skill directory sync
    Decision: Removed the internal `al mcp-prompts` MCP server. Claude receives skills via `.claude/skills/`; Codex, Gemini, Antigravity, VS Code/Copilot, and Copilot CLI share `.agents/skills/`. Full subdirectory support (`scripts/`, `references/`, `assets/`) is preserved. Supersedes Decision 2026-01-17 e5f6a7b and Decision 2026-02-23 mcp-prompts-dispatch-bypass.
    Reason: MCP prompts return flat text only, while current client docs support directory-format Agent Skills and several clients now document `.agents/skills/` as an interoperable project path. Shared sync avoids duplicate skill catalogs.
    Tradeoffs: Lost the unified MCP prompt API for skill delivery and stopped generating legacy client-specific skill folders; gained full Agent Skills resource support and fewer duplicate projections.

- Decision 2026-05-07 mcp-catalog-seed-split: MCP server catalog moves to wizard-only embedded file; install seed ships zero `[[mcp.servers]]`
    Decision: Default MCP blocks now live in a wizard-only embedded catalog; the install seed ships only an empty `[mcp]` section with docs guidance. Interactive wizard selection inserts selected catalog blocks and prunes disabled catalog IDs, including customized variants. Profile mode remains verbatim and does not prune.
    Reason: Fresh `al init` should produce a minimal config instead of ~70 disabled MCP lines, while the wizard remains the place to discover curated defaults.
    Tradeoffs: Unticking a customized catalog ID in the interactive wizard removes that block after diff preview confirmation. Profile-mode users keep full responsibility for whatever their profile contains.

- Decision 2026-05-07 reasoning-effort-capability-matrix: Reasoning effort is per-client and custom-tolerant where upstream cadence demands it
    Decision: Support Codex and Claude `reasoning_effort` as custom-tolerant fields; reject Gemini/Copilot CLI effort until a verified control exists; for Claude, require Opus, pass all values via `--effort`, and write settings `effortLevel` only for non-`max` values.
    Reason: Silent omission breaks fail-loud behavior, but strict Claude catalogs broke when upstream added values such as `xhigh`; `max` is session-only while low/medium/high/xhigh are cataloged.
    Tradeoffs: Typos surface as sync warnings instead of hard errors for Claude/Codex; Gemini/Copilot configs still fail fast until support is intentionally added.

- Decision 2026-05-07 gemini-policy-engine: Migrate Gemini allowlist to Policy Engine TOML with explicit `policyPaths`
    Decision: Stop emitting `tools.allowed` in `.gemini/settings.json`. Generate `.gemini/policies/agent-layer.toml` with one `[[rule]]` block per allowed shell command (`toolName = "run_shell_command"`, `commandPrefix = <cmd>`, `decision = "allow"`, `priority = 100`, `allowRedirection = true`), and write `policyPaths: [".gemini/policies"]` in settings.json so Gemini CLI loads the file.
    Reason: Gemini CLI v0.30+ deprecates `tools.allowed` ("removed in 1.0"). Workspace tier `.gemini/policies/` is NOT auto-loaded by Gemini CLI 0.41.2 without `policyPaths`, so an explicit pointer is required. `commandPrefix` mirrors the previous `run_shell_command(<cmd>)` substring semantics; `commandRegex` would have required escaping plain user-provided strings. `allowRedirection = true` preserves the prior `tools.allowed` behavior for headless workflows that pipe output (`git ... > out.txt`).
    Tradeoffs: Two managed artifacts per repo instead of one; users on Gemini CLI < v0.18 (pre-policy-engine) lose the auto-allow behavior, but that release is over six months old.

- Decision 2026-05-09 claude-agent-specific-deep-merge: Claude agent_specific deep-merge with additive permissions.deny default
    Decision: Claude `agent_specific` is deep-merged into `.claude/settings.json` for object values (arrays and scalars still replace at their key). `permissions.deny` is additive and silent; `permissions.allow` and `effortLevel` still trigger override warnings. Install seed ships `agent_specific.permissions.deny = ["AskUserQuestion"]` so new repos disable Claude Code's structured clarification tool by default.
    Reason: Shallow-replace forced users to repeat the entire `permissions` block (re-listing managed `allow` entries) just to add a `deny`. The default deny enforces text-only clarifications, which agents in this repo prefer. Asymmetry vs Codex `agent_specific` is intentional: Codex projection still emits TOML at the root and remains shallow.
    Tradeoffs: Override-warning logic for Claude is bespoke (`claudeAgentSpecificOverrideWarning`) instead of the generic reserved-keys helper. `cloneClaudeSettingValue` deep-clones the types Agent Layer projects (`map[string]any`, `[]any`, `[]string`); other slice types fall through to a shallow copy and would need to be added if the projection grows beyond those.

- Decision 2026-05-16 cleanup-skills-two-skill-split: Post-implementation cleanup is two diff-scoped skills (`prune-new-tests` + `simplify-new-code`), not one
    Decision: Add two narrow leaf skills wired between implementation and verification: `prune-new-tests` (burden-of-proof test deletion) and `simplify-new-code` (smell-pattern scope-creep removal). Rename `simplify-code` → `simplify-codebase` and explicitly scope it to the codebase, never the diff alone. Both new skills run automatically in `implement-plan` (between implementation and verification) and `audit-and-fix-uncommitted-changes` (before `review-scope`).
    Reason: A unified cleanup skill would embed a Selection composition (`if item is a test → rule A; if item is added code → rule B`) — the multi-mode anti-pattern in SKILL-DESIGN.md Principle 2 (ComplexBench shows Selection composition collapses GPT-4 accuracy from 0.881 to as low as 0.083). The two artifacts also have opposite default dispositions: tests are agent side-effects (delete unless justified) while code was user-requested (preserve behavior; remove only the scope creep wrapped around it). One rubric cannot encode both intents without becoming a justification framework that lets agents rationalize their own work.
    Tradeoffs: Two skills instead of one means a slightly larger catalog footprint and two reports per cleanup run; accepted because routing accuracy and single-mode rubrics are higher-value. Pre-existing `simplify-code` callouts in diff-context skills (`debug-issue`, `complete-current-phase`, `fix-issues`) now point at `simplify-new-code`; codebase-context skills (`improve-codebase`) point at `simplify-codebase`.

- Decision 2026-05-16 fresh-context-reviewer-subagent: Self-grading reviewers must run in a fresh-context subagent
    Decision: Phases that re-grade work the same agent just produced are restructured to delegate the re-grade to a fresh-context reviewer subagent. The reviewer receives only the artifacts under review and an explicit rubric — no prior conversation, no implementer narrative, no rationalization commentary. Applied to: `prune-new-tests` (added tests + production code only), `simplify-new-code` (diff hunks + minimal context only), `verify-against-plan` Phase 2 (plan + post-implementation state only — plan-anchored, narrative-blind), `address-pr-comments` Phase 6 (comment + reply + named commit diff only), `improve-codebase` Phase 3 per-chunk re-audit (post-fix chunk + originating findings only).
    Reason: Pure instruction-tuning is insufficient to break self-rationalization. The same agent that wrote tests/code/replies/fixes has already paid the primacy and self-consistency taxes; re-grading from the same context inherits the rationalizations. The fresh-context contract makes the rubric the only signal, not the author's narrative. The pattern has three sub-variants: artifact-only (PR-comment audit, per-chunk re-audit), plan-anchored narrative-blind (verify-against-plan — reviewer must see the plan because that is the artifact being checked against, but must not see implementer notes), and smell-pattern (simplify-new-code — scope creep identified by pattern, not by comparison to the request).
    Tradeoffs: Each fresh-context invocation re-reads the artifacts it needs, so wall-clock latency and token usage increase versus an in-context re-grade. Accepted because the alternative (in-context self-grading) systematically produces false-positive "pass" verdicts. `audit-and-fix-uncommitted-changes` Round N+1 and `resolve-findings` Phase 5 Auditor are intentionally out of scope: both are bounded by external artifacts already produced by artifact-anchored skills, so the marginal value is lower.

- Decision 2026-05-16 verbatim-prompt-extraction: Verbatim subagent prompts live in sibling `reviewer-prompt.md` files, not inline in SKILL.md
    Decision: Skills that delegate to a fresh-context reviewer subagent (`prune-new-tests`, `simplify-new-code`, `verify-against-plan`, `address-pr-comments`, `improve-codebase`) store the verbatim prompt in a sibling `reviewer-prompt.md` file rather than inlining it in SKILL.md. The orchestrator reads the file and passes its contents to the subagent verbatim, with explicit "do not paraphrase" wording in the SKILL.md call site. The "Inputs the reviewer receives" list stays in SKILL.md as orchestrator-facing instruction.
    Reason: The prompt is payload to a different agent, not workflow for the orchestrator reading SKILL.md. File-backed pass-through enforces the verbatim contract by mechanics rather than recall discipline; SKILL.md drops to pure workflow and orchestrator scanning improves. Progressive disclosure works correctly: the prompt only enters the subagent's context when the phase fires.
    Tradeoffs: One extra file per affected skill; opening SKILL.md no longer shows the full contract at a glance. Acceptable at current sizes (184-267 lines pre-extraction, well under Anthropic's 500-line backstop). Promote a prompt to a shared top-level location only when 2+ skills reuse the same verbatim text. If a verbatim contract grows variables, simple agent-side substitution at invocation time is sufficient — no templating tooling.

- Decision 2026-05-20 lint-suppression-policy: gosec suppressions are inline-with-reason by default; staticcheck ST10xx augments revive `exported`
    Decision: `.golangci.yml` no longer carries global `gosec.excludes`. Every remaining gosec suppression is either inline at the callsite (`// #nosec <rule> -- <reason>` or an existing `//nolint:gosec // <reason>`) or a path-scoped exclude with a reason comment for repeated same-justification callsites. Test code is tightened in place (`0o755`→`0o700` for `Mkdir`, `0o644`→`0o600` for `WriteFile`/`OpenFile`/`Chmod` where the bit is incidental); the executable/traversal-bit cases are annotated, not tightened. `staticcheck.checks` mirrors golangci-lint v2's default set but re-enables `ST1020`, `ST1021`, `ST1022` so revive `exported` and staticcheck both enforce the canonical Go doc-comment form. `issues.max-issues-per-linter: 0` and `issues.max-same-issues: 0` are set so future findings are never silently capped.
    Reason: Global excludes hid both real safety findings and stale rules — once dropped, the audit surfaced 25 production callsites that all turned out to be legitimate but undocumented, and confirmed two stale path-scoped G602 entries. Dual ST10xx + revive enforcement is intentional belt-and-suspenders: either alone covers the policy, so drift between them is acceptable.
    Tradeoffs: Every new perm-literal callsite must either tighten to `0o600`/`0o700` or carry a gosec suppression reason. Future contributors adding `os.WriteFile`/`Chmod`/`OpenFile` with executable or world-readable modes will need to justify them. New exported symbols need a doc comment in canonical Go form to satisfy both linters. Suppression reason quality remains partly review-enforced; `nolintlint` is not enabled in this change.
