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

- Issue 2026-09-23 go-test-truncated-package-undetected: Test runs pass when a test binary is replaced or exits at the syscall level
    Priority: Low. Area: Test suite / Makefile
    Description: When a test process is replaced (`syscall.Exec`) or exits through `syscall.Exit` before its package finishes, `go test` still reports `ok` and test2json emits no package-level result; `os.Exit(0)` is caught by `-test.paniconexit0`, but these low-level paths are not. `make test` and `make coverage` (which `make ci` uses) rely on that exit status alone, so the `cmd/al` suite ran 1 of 292 tests from 2026-07-02 to 2026-09-23 without any failure signal.
    Open question: Is a post-check in `make test` and `make coverage` that fails when a started package lacks a package-level `pass`/`fail`/`skip` event in `go-test.jsonl` worth adding?
    Notes: The `cmd/al` exec-handoff tests now re-execute the test binary; a full `make test` emits a package-level result for all 49 packages (47 `pass`, 2 `skip` with no test files). `grok` and `copilot_cli` also launch through `clients.ExecHandoff`.

- Issue 2026-09-21 release-dispatch-probe-fragility: Live release compatibility probes are very fragile
    Priority: Medium. Area: Release validation / Agent Dispatch
    Description: Probe outcomes depend on local authentication state and nondeterministic model formatting. Local runs hit a transient Claude OAuth refresh lock; a rerun completed all provider lifecycles but failed Codex's exact-output assertion solely because it added a trailing period. Scratch-project runs also missed repo-local sign-ins.
    Next step: Revisit the probe acceptance criteria and execution procedure against these observed failures before relying on them as a repeatable release gate.
    Notes: Local evidence: `.agent-layer/tmp/local-release-probes/report.md` and `.agent-layer/tmp/local-release-probes/retry-20260922T001725Z/report.md`.

- Issue 2026-09-20 copilot-native-project-mcp-loading: Copilot CLI no longer documents the generated project MCP path
    Priority: Medium. Area: Copilot CLI integration
    Description: Agent Layer generates `.copilot/mcp-config.json`, but installed Copilot CLI 1.0.83 documents workspace `.mcp.json` or `.github/mcp.json`; `al copilot` does not pass the generated file explicitly. Copilot-only entries can therefore be absent from native discovery. This predates the Muse rebuild.
    Next step: Verify the generated-file loading contract against supported native Copilot versions using an isolated selected-server fixture.
    Notes: Installed CLI help and startup-source evidence retained in `.agent-layer/tmp/muse-rebuild/other-mcp-audit` and the rebuild worktree's `.agent-layer/tmp/muse-critical`.

- Issue 2026-07-28 dispatch-mcp-start-transport-window: An MCP dispatch_start disconnect can orphan a handle
    Priority: Medium. Area: Agent Dispatch MCP interface
    Description: `dispatch_start` is an RPC acknowledgement rather than a direct write to the caller's terminal. If the transport disconnects after the backend starts but before the client observes the response, the dispatch keeps running durably while the caller never learns its handle. This slice deliberately added no idempotency state and no listing API.
    Open question: Has this been observed in practice, justifying a caller-supplied idempotency key or narrow handle-recovery read?
    Notes: Evidence remains under `.agent-layer/tmp/runs/`; documented in docs/AGENT-DISPATCH.md and the dispatch-mcp-interface decision.
