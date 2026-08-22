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

- Issue 2026-08-21 make-dev-slow-execution-time: make dev execution time is excessively slow for local workflows
    Priority: Medium. Area: developer experience / build tooling
    Description: `make dev` runs full formatting, linting, global coverage enforcement, DeepSWE verification, and release test suites sequentially, taking several minutes per invocation and slowing local iteration.
    Next step: Profile individual target durations in `make dev` and evaluate separating fast local iteration from full whole-repo verification.

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

- Issue 2026-07-28 dispatch-mcp-start-transport-window: An MCP dispatch_start disconnect can orphan a handle
    Priority: Medium. Area: Agent Dispatch MCP interface
    Description: `dispatch_start` is an RPC acknowledgement rather than a direct write to the caller's terminal. If the transport disconnects after the backend starts but before the client observes the response, the dispatch keeps running durably while the caller never learns its handle. This slice deliberately added no idempotency state and no listing API.
    Open question: Has this been observed in practice, justifying a caller-supplied idempotency key or narrow handle-recovery read?
    Notes: Evidence remains under `.agent-layer/tmp/runs/`; documented in docs/AGENT-DISPATCH.md and the dispatch-mcp-interface decision.
