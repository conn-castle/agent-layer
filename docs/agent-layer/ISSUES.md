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

- Issue 2026-08-18 antigravity-async-prefetch-test-flake: `TestPromptModelsAntigravityUsesReadyAsyncPrefetch` is load-sensitive
    Priority: Low. Area: test suite / wizard
    Description: `waitForAntigravityModelDiscoveryReady` (`internal/wizard/option_discovery_test.go:438`) waits a hardcoded 1 second for async discovery that forks a shell `agy` stub. Under full-suite parallel load the fork can exceed that deadline, failing `make test`; the test passes repeatedly in isolation.
    Next step: Replace the fixed deadline with a generous bound or a synchronization signal from the prefetch goroutine rather than wall-clock polling.
    Notes: Observed once during PR #188 (`make test` via pre-commit); re-ran 5x isolated with no failure. Unrelated to the Grok change.

- Issue 2026-08-18 grok-duplicate-root-instructions: Grok loads both generated instruction shims
    Priority: Low. Area: providers / grok / instructions / warnings
    Description: Grok 1.0.5 loads both byte-identical generated `AGENTS.md` and `CLAUDE.md`, duplicating project instructions while the shared instruction-token warning counts the source once.
    Next step: Revisit if Grok adds a compatibility toggle or Agent Layer gains a client-aware instruction projection and warning model.
    Notes: Current Grok documentation says both files contribute and exposes no toggle that suppresses only the root `CLAUDE.md`; the limitation is documented in the reference.

- Issue 2026-08-18 codex-trust-symlink-path: Codex trust uses a lexical rather than canonical repository path
    Priority: Low. Area: providers / codex / trust
    Description: Codex trust seeding uses `filepath.Abs` without resolving symlinks, so a repository opened through a symlink may not match the client-observed canonical path.
    Next step: Reproduce at the installed Codex boundary and canonicalize the trust key with regression coverage if confirmed.
    Notes: Found while fixing the equivalent Grok integration defect; outside the Grok change scope.

- Issue 2026-08-05 coverage-remainder-is-error-injection-only: Coverage above ~91.4% requires failure-injection tests
    Priority: Low. Area: test suite / coverage
    Description: After a behavior-focused pass raised total coverage from 90.02% to 91.42%, every remaining uncovered region is a block of 1–5 statements. They are overwhelmingly `if err != nil` wrappers around filesystem, git, and process calls, plus platform-unreachable branches (device/socket nodes in skilltree.describeNode, non-finite floats that `encoding/json` cannot decode, and defensive duplicate-selector checks that config validation already rejects). No untested feature-level behavior remains in a single block larger than 5 statements.
    Open question: whether a higher coverage threshold is worth adding fault-injection seams (an injectable filesystem or forced-error hooks) to the packages that currently call `os` directly.
    Notes: `internal/agentdispatch` (~430 missed) and `internal/benchmark` (~300 missed) hold most of the remainder; both are dominated by provider/Docker orchestration failure paths.

- Issue 2026-08-04 permission-based-test-fixtures-assume-non-root: chmod-driven failure fixtures silently pass under root
    Priority: Low. Area: test suite
    Description: Pre-existing tests in internal/install and internal/clients drive production error paths by removing a directory's write bit. A process with CAP_DAC_OVERRIDE — root in many containers — writes anyway, so the operation under test succeeds and the assertion fails for an environment reason. CI runs on ubuntu-latest as a non-root user, so nothing currently fails.
    Next step: If root execution is ever supported, add one shared writability probe helper and apply it to every chmod-based fixture rather than to individual tests.
    Notes: The skill-import fixtures raised in PR #170 were removed or replaced with deterministic injected failures; this issue now covers only the older remaining fixtures.

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
