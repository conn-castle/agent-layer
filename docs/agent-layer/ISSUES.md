# Issues

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Deferred defects, maintainability refactors, technical debt, risks, and engineering concerns. Add an entry only when you are not fixing it now.

## Format
- Insert new entries immediately below `<!-- ENTRIES START -->` (most recent first).
- Keep each entry **3–5 lines**.
- Line 1 starts with `- Issue YYYY-MM-DD <id>:` and a short title.
- Lines 2–5 are indented by **4 spaces** and use `Key: Value`.
- Keep **exactly one blank line** between entries.
- Prevent duplicates: search the file and merge/rewrite instead of adding near-duplicates.
- When fixed, remove the entry from this file.
- Describe the problem without choosing a solution or listing options.
- Use `Next step` only when the action is useful regardless of the eventual solution. Otherwise, use `Open question: <decision needed>`.

### Entry template
```text
- Issue YYYY-MM-DD short-slug: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies/constraints>
```

## Open issues

<!-- ENTRIES START -->

- Issue 2026-08-04 skill-import-block-identity-name-ordered: A block's locked identity is taken from its alphabetically first skill
    Priority: Low. Area: skill imports / recorded state
    Description: `state.blockLockedIdentity` returns `entriesForBlock(block)[0]`, and a per-skill pull failure can leave one block's entries carrying different commits and configured refs. `openBlock` reads the commit and the retarget decision from that same entry, so every reachable path converges forward rather than reverting, but which recorded generation a block reconciles onto depends on skill names rather than on anything meaningful.
    Open question: Should a block's entries be required to share one commit, or should each skill reconcile against its own recorded commit?
    Notes: Raised in PR #170 review; the current reasoning is documented on `blockLockedIdentity`. No incorrect outcome has been demonstrated.

- Issue 2026-08-04 permission-based-test-fixtures-assume-non-root: chmod-driven failure fixtures silently pass under root
    Priority: Low. Area: test suite
    Description: Tests across internal/install, internal/clients, and internal/skillimport drive production error paths by removing a directory's write bit. A process with CAP_DAC_OVERRIDE — root in many containers — writes anyway, so the operation under test succeeds and the assertion fails for an environment reason. CI runs on ubuntu-latest as a non-root user, so nothing currently fails.
    Next step: If root execution is ever supported, add one shared writability probe helper and apply it to every chmod-based fixture rather than to individual tests.
    Notes: Raised in PR #170 review against two skillimport tests; the pattern is repository-wide, so a per-test fix would be inconsistent.

- Issue 2026-08-04 benchmark-treatment-skill-mapping-stale: Skills-treatment conformance requires skills that no longer ship
    Priority: High. Area: benchmarks / treatment conformance
    Description: `dispatchSkillForRole` in internal/benchmark/normalize.go returns `review-plan`, `implement-plan`, and `review-uncommitted-code`, and assets/workflow-prompt.md instructs `$plan-work` then `$fully-implement-plan`. All five skills were removed in PR #167, so `dispatchConformance` can never reach `len(completedRoles) == len(required)`. A `TreatmentInstructionsAndSkills` run therefore scores as an underperforming treatment instead of failing, producing invalid comparison data.
    Open question: Which skill should each required role map to now that the workflow is `$implement`, or should the gate stop keying on skill name at all?
    Notes: feat/benchmark-campaigns already updates workflow-prompt.md to `$implement` but leaves dispatchSkillForRole unchanged, so merging that branch does not by itself resolve this.

- Issue 2026-08-04 template-consumer-skill-names-unverified: Nothing checks that in-repo consumers name skills that exist
    Priority: Medium. Area: templates / test coverage
    Description: internal/templates guards the producer side with TestRemovedSkillTemplatesStayRemoved, but no test verifies that code and assets referencing skill names resolve to shipped skills. internal/benchmark/runtime_test.go builds its dispatch records from the same three string literals the production switch returns, so it passes whether or not those skills exist, and assets/workflow-prompt.md has no content assertion at all.
    Next step: Add a test asserting every skill name referenced by benchmark code and assets resolves to a directory under internal/templates/skills.
    Notes: This gap is why removing 16 skill templates in PR #167 left CI fully green.

- Issue 2026-07-28 antigravity-probe-headless-permission-denial: Probe stdout is empty on agy 1.1.8, so transcript-derived capabilities are unmeasurable
    Priority: Medium. Area: Antigravity capability probe
    Description: On agy 1.1.8 the headless probe run exits 0 with empty stdout and stderr `no output produced — a tool required the "command" permission that headless mode cannot prompt for`. Every stdout-derived capability (`instructions_loaded`, `skill_names_visible`, `mcp_config_names_visible`, `shared_skill_dedup_observed`) therefore reports false regardless of real behavior. Confirmed pre-existing: the same result occurs with the previous prompt and `/usr/bin/true` fixture.
    Next step: Seed the probe's `settings.json` with the specific allow-rules the probe prompt needs, or run the probe with an explicit auto-approve flag, then re-establish the observed baseline in CONTEXT.md.
    Notes: Log-derived capabilities (`permissions_loaded`, `mcp_config_migrated`, `mcp_runtime_discovery`) and the new marker-derived `mcp_tool_invoked` are unaffected.

- Issue 2026-07-28 dispatch-mcp-start-transport-window: An MCP dispatch_start disconnect can orphan a handle
    Priority: Medium. Area: Agent Dispatch MCP interface
    Description: `dispatch_start` is an RPC acknowledgement rather than a direct write to the caller's terminal. If the transport disconnects after the backend starts but before the client observes the response, the dispatch keeps running durably while the caller never learns its handle. This slice deliberately added no idempotency state and no listing API.
    Open question: Has this been observed in practice, justifying a caller-supplied idempotency key or narrow handle-recovery read?
    Notes: Evidence remains under `.agent-layer/tmp/runs/`; documented in docs/AGENT-DISPATCH.md and the dispatch-mcp-interface decision.

- Issue 2026-07-27 benchmark-cancelled-session-cost: Pre-request cancellation invalidates an otherwise-scored treatment run
    Priority: High. Area: benchmarks / treatment cost accounting
    Description: A Koota treatment completed and scored 37/51, but normalization rejected it because one Codex child dispatch was explicitly cancelled before a request and its session had identity but no token-count event.
    Next step: Exclude only sessions proven by dispatch evidence to be caller-cancelled before any token-usage event, while retaining strict failure for ambiguous or incomplete billable sessions.
    Notes: The consolidated executor now preserves every successful cell and continues the remaining queue after failures; only the no-token cancelled-session accounting case remains.

- Issue 2026-07-23 benchmark-provider-auth-preflight: Candidate authentication is not validated before a costly benchmark launch
    Priority: High. Area: benchmarks / provider setup
    Description: The twelve-task `opus-high-bare-calibration-20260722` run prepared every task, then all Claude candidates failed before inference because the copied OAuth session had expired; the current no-model helper preflight did not exercise Claude authentication.
    Next step: Add a provider-specific, non-billing credential-validity preflight (or fail the run before task setup when one is unavailable), preserve its evidence in the experiment report, and cover expiry behavior offline.
    Notes: Every rejected invocation recorded zero tokens and a complete provider-reported $0 cost; no task score is valid and no automatic rerun is authorized.

- Issue 2026-07-22 benchmark-treatment-dispatch-fidelity: Benchmark treatment can substitute native subagents for required external dispatches
    Priority: High. Area: benchmarks / treatment workflow
    Description: Claude Opus Low completed the configured plan-review, implementation, code-review, and fix workflow with five native Agent tool calls and zero `al dispatch` calls, despite those skills requiring external dispatch; the harness scores the result without flagging this protocol violation.
    Next step: Define and enforce a treatment-execution contract that records required role dispatches and marks a pair nonconformant when configured external roles are not actually dispatched, while retaining all candidate evidence.
    Notes: Confirmed in `opus-low-jsonpath-20260722`; `al` was installed and the Agent Dispatch skill was available, so this was not a provider failure.
